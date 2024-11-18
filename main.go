package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/handler/middleware"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/llm"
)

func makeGptCommand(name, desc string) discord.SlashCommandCreate {
	return discord.SlashCommandCreate{
		Name:        name,
		Description: desc,
		IntegrationTypes: []discord.ApplicationIntegrationType{
			discord.ApplicationIntegrationTypeGuildInstall,
			discord.ApplicationIntegrationTypeUserInstall,
		},
		Contexts: []discord.InteractionContextType{
			discord.InteractionContextTypeGuild,
			discord.InteractionContextTypeBotDM,
			discord.InteractionContextTypePrivateChannel,
		},
		Options: []discord.ApplicationCommandOption{
			discord.ApplicationCommandOptionString{
				Name:        "prompt",
				Description: "8k input ctx, 4k output",
				Required:    true,
			},
			discord.ApplicationCommandOptionBool{
				Name:        "ephemeral",
				Description: "If the response should only be visible to you",
				Required:    false,
			},
		},
	}
}

var (
	token    = os.Getenv("X3ZEO_DISCORD_TOKEN")
	commands = []discord.ApplicationCommandCreate{
		makeGptCommand("gpt4o", "OpenAI GPT-4o mini"),
		makeGptCommand("mistral_nemo", "Mistral NeMo 12B"),
		makeGptCommand("commandr", "Cohere Command R 08-2024 32B"),
		makeGptCommand("llama11b", "Llama 3.2 11B Vision Instruct"),
		makeGptCommand("gpt4", "OpenAI GPT-4o (needs whitelist)"),
		makeGptCommand("llama405b", "Meta Llama 3.1 405B Instruct (needs whitelist)"),
		makeGptCommand("mistral_large", "Mistral Large 123B (needs whitelist)"),
		makeGptCommand("commandr_plus", "Cohere Command R+ 08-2024 104B (needs whitelist)"),
		makeGptCommand("llama90b", "Llama 3.2 90B Vision Instruct (needs whitelist)"),
		discord.SlashCommandCreate{
			Name:        "whitelist",
			Description: "Add or remove yourself from the whitelist",
			IntegrationTypes: []discord.ApplicationIntegrationType{
				discord.ApplicationIntegrationTypeGuildInstall,
				discord.ApplicationIntegrationTypeUserInstall,
			},
			Contexts: []discord.InteractionContextType{
				discord.InteractionContextTypeGuild,
				discord.InteractionContextTypeBotDM,
				discord.InteractionContextTypePrivateChannel,
			},
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionUser{
					Name:        "user",
					Description: "The user to add or remove from the whitelist",
					Required:    true,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "remove",
					Description: "If the user should be removed from the whitelist",
				},
			},
		},
		discord.SlashCommandCreate{
			Name:        "lobotomy",
			Description: "Forget local context",
			IntegrationTypes: []discord.ApplicationIntegrationType{
				discord.ApplicationIntegrationTypeGuildInstall,
				discord.ApplicationIntegrationTypeUserInstall,
			},
			Contexts: []discord.InteractionContextType{
				discord.InteractionContextTypeGuild,
				discord.InteractionContextTypeBotDM,
				discord.InteractionContextTypePrivateChannel,
			},
		},
	}

	whitelistedUsers = []snowflake.ID{890686470556356619}

	// since we can't read DMs from the bot, we will cache them
	dmCache = map[snowflake.ID]*llm.Llmer{}
)

const (
	// LLM interaction context surrounding messages
	maxContextMessages = 50
	whitelistFile      = "x3whitelist.json"
	dmCacheFile        = "x3dmcache.json"
)

func loadWhitelist() {
	// if not exists, create
	if _, err := os.Stat(whitelistFile); os.IsNotExist(err) {
		f, err := os.Create(whitelistFile)
		if err != nil {
			panic(err)
		}
		f.Close()
	}

	f, err := os.Open(whitelistFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err = json.NewDecoder(f).Decode(&whitelistedUsers); err != nil {
		saveWhitelist()
	}
}

func saveWhitelist() {
	f, err := os.Create(whitelistFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err = json.NewEncoder(f).Encode(whitelistedUsers); err != nil {
		panic(err)
	}
}

func addToWhitelist(id snowflake.ID) {
	for _, v := range whitelistedUsers {
		if v == id {
			return
		}
	}
	whitelistedUsers = append(whitelistedUsers, id)
	saveWhitelist()
}

func removeFromWhitelist(id snowflake.ID) {
	for i, v := range whitelistedUsers {
		if v == id {
			whitelistedUsers = append(whitelistedUsers[:i], whitelistedUsers[i+1:]...)
			saveWhitelist()
			return
		}
	}
}

func isInWhitelist(id snowflake.ID) bool {
	for _, v := range whitelistedUsers {
		if v == id {
			return true
		}
	}
	return false
}

func saveDmCache() {
	f, err := os.Create(dmCacheFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err = json.NewEncoder(f).Encode(dmCache); err != nil {
		panic(err)
	}
}

func loadDmCache() {
	// if not exists, create
	if _, err := os.Stat(dmCacheFile); os.IsNotExist(err) {
		f, err := os.Create(dmCacheFile)
		if err != nil {
			panic(err)
		}
		f.Close()
	}

	f, err := os.Open(dmCacheFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	if err = json.NewDecoder(f).Decode(&dmCache); err != nil {
		saveDmCache()
	}
}

func init() {
	loadWhitelist()
	loadDmCache()
}

func main() {
	defer saveWhitelist()
	defer saveDmCache()

	slog.SetLogLoggerLevel(slog.LevelDebug)
	slog.Info("x3zeo booting up...")
	slog.Info("disgo version", slog.String("version", disgo.Version))

	r := handler.New()
	r.Use(middleware.Logger)

	// LLM commands
	llmWhitelistError := func(event *handler.CommandEvent) error {
		return event.CreateMessage(discord.MessageCreate{Content: "You are not in the whitelist for this command. Perhaps another whitelisted user may invite you. Try /gpt4o", Flags: discord.MessageFlagEphemeral})
	}
	r.Group(func(r handler.Router) {
		r.Command("/gpt4o", func(event *handler.CommandEvent) error {
			return handleLlm(event, llm.ModelGpt4oMini)
		})
		r.Command("/mistral_nemo", func(event *handler.CommandEvent) error {
			return handleLlm(event, llm.ModelMistralNemo)
		})
		r.Command("/commandr", func(event *handler.CommandEvent) error {
			return handleLlm(event, llm.ModelCohereCommandR082024)
		})
		r.Command("/llama11b", func(event *handler.CommandEvent) error {
			return handleLlm(event, llm.ModelLlama11bVision)
		})
		r.Command("/gpt4", func(event *handler.CommandEvent) error {
			if isInWhitelist(event.User().ID) {
				return handleLlm(event, llm.ModelGpt4o)
			}
			return llmWhitelistError(event)
		})
		r.Command("/llama405b", func(event *handler.CommandEvent) error {
			if isInWhitelist(event.User().ID) {
				return handleLlm(event, llm.ModelLlama405b)
			}
			return llmWhitelistError(event)
		})
		r.Command("/mistral_large", func(event *handler.CommandEvent) error {
			if isInWhitelist(event.User().ID) {
				return handleLlm(event, llm.ModelMistralLarge)
			}
			return llmWhitelistError(event)
		})
		r.Command("/commandr_plus", func(event *handler.CommandEvent) error {
			if isInWhitelist(event.User().ID) {
				return handleLlm(event, llm.ModelCohereCommandRPlus082024)
			}
			return llmWhitelistError(event)
		})
		r.Command("/llama90b", func(event *handler.CommandEvent) error {
			return handleLlm(event, llm.ModelLlama90bVision)
		})
	})

	// whitelist
	r.Group(func(r handler.Router) {
		r.Command("/whitelist", handleWhitelist)
		r.Command("/lobotomy", handleLobotomy)
	})

	r.NotFound(handleNotFound)

	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(gateway.WithIntents(
			gateway.IntentGuildMessages,
			gateway.IntentMessageContent,
			gateway.IntentDirectMessages,
		)),
		bot.WithEventListeners(r),
		bot.WithEventListenerFunc(onMessageCreate),
	)
	if err != nil {
		slog.Error("error while building disgo instance", slog.Any("err", err))
		return
	}

	defer client.Close(context.TODO())

	if _, err = client.Rest().SetGlobalCommands(client.ApplicationID(), commands); err != nil {
		slog.Error("error while registering commands", slog.Any("err", err))
		return
	}

	if err = client.OpenGateway(context.TODO()); err != nil {
		slog.Error("error while opening gateway", slog.Any("err", err))
		return
	}

	slog.Info("x3zeo running. ctrl+c to stop")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}

func handleNotFound(event *handler.InteractionEvent) error {
	return event.CreateMessage(discord.MessageCreate{Content: "Command not found", Flags: discord.MessageFlagEphemeral})
}

func formatMsg(msg, username string) string {
	return msg
}

func addContextMessagesIfPossible(client bot.Client, llmer *llm.Llmer, channelID, messageID snowflake.ID) {
	messages, err := client.Rest().GetMessages(channelID, 0, messageID, 0, maxContextMessages)
	if err != nil {
		return
	}

	// discord returns surrounding message history from newest to oldest, but we want oldest to newest
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role := llm.RoleUser
		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
		}
		llmer.AddMessage(role, formatMsg(msg.Content, msg.Author.Username))
	}
}

func handleLlm(event *handler.CommandEvent, model string) error {
	data := event.SlashCommandInteractionData()
	prompt := data.String("prompt")
	ephemeral := data.Bool("ephemeral")

	var llmer *llm.Llmer
	var isDm bool
	if event.Channel().Type() == discord.ChannelTypeDM || event.Channel().Type() == discord.ChannelTypeGroupDM {
		// we are in a DM, so we cannot read surrounding messages. Instead, we use a cache
		slog.Debug("in a DM; looking up DM cache", slog.String("channel", event.Channel().ID().String()))
		var ok bool
		llmer, ok = dmCache[event.Channel().ID()]
		if !ok {
			// not in cache, so create
			slog.Debug("not in dmCache; creating", slog.String("channel", event.Channel().ID().String()))
			llmer = llm.NewLlmer()
			dmCache[event.Channel().ID()] = llmer
		}
		llmer.TruncateMessages(maxContextMessages)
		isDm = true
	} else {
		// we are not in a DM, so we can read surrounding messages
		llmer = llm.NewLlmer()
	}

	// add context if possible
	lastMessage := event.Channel().MessageChannel.LastMessageID()
	if !isDm && lastMessage != nil {
		addContextMessagesIfPossible(event.Client(), llmer, event.Channel().ID(), *lastMessage)

		// and we also want the last message in the channel
		msg, err := event.Client().Rest().GetMessage(event.Channel().ID(), *lastMessage)
		if err == nil && msg != nil {
			llmer.AddMessage(llm.RoleUser, formatMsg(msg.Content, msg.Author.Username))
		}
	}

	slog.Debug("handleLlm: got context messages", slog.Int("count", llmer.NumMessages()))

	// and we also want the actual slash command prompt
	llmer.AddMessage(llm.RoleUser, formatMsg(prompt, event.User().Username))

	event.DeferCreateMessage(ephemeral)

	response, err := llmer.RequestCompletion(model)
	if err != nil {
		return err
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	event.UpdateInteractionResponse(discord.MessageUpdate{
		Content: &response,
		Flags:   &flags,
	})

	if isDm {
		saveDmCache()
	}

	return nil
}

func handleLlmInteraction(event *events.MessageCreate) error {
	if err := event.Client().Rest().SendTyping(event.ChannelID); err != nil {
		slog.Error("failed to SendTyping", slog.Any("err", err))
	}

	// the interaction happened in a server, so we can get the surrounding messages
	llmer := llm.NewLlmer()
	addContextMessagesIfPossible(event.Client(), llmer, event.ChannelID, event.MessageID)
	slog.Debug("interaction; added context messages", slog.Int("count", llmer.NumMessages()))

	// and we also want the event message
	llmer.AddMessage(llm.RoleUser, formatMsg(event.Message.Content, event.Message.Author.Username))

	// now we generate the LLM response
	response, err := llmer.RequestCompletion(llm.ModelGpt4oMini)
	if err != nil {
		return err
	}

	// and reply to the trigger message with it
	event.Client().Rest().CreateMessage(event.ChannelID, discord.MessageCreate{
		Content: response,
		MessageReference: &discord.MessageReference{
			MessageID: &event.Message.ID,
			ChannelID: &event.ChannelID,
			GuildID:   event.GuildID,
		},
		AllowedMentions: &discord.AllowedMentions{
			RepliedUser: false, // don't ping trigger message author
		},
	})

	return nil
}

func onMessageCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	for _, user := range event.Message.Mentions {
		if user.ID == event.Client().ID() {
			slog.Debug("handling @mention interaction")
			handleLlmInteraction(event)
			return
		}
	}

	if event.Message.ReferencedMessage != nil {
		// this is a response to a message...
		if event.Message.ReferencedMessage.Author.ID == event.Client().ID() {
			// ...that was created by us
			slog.Debug("handling reply interaction")
			handleLlmInteraction(event)
			return
		}
	}
}

func handleWhitelist(event *handler.CommandEvent) error {
	if !isInWhitelist(event.User().ID) {
		return event.CreateMessage(discord.MessageCreate{Content: "You are not in the whitelist, therefore you cannot whitelist other users", Flags: discord.MessageFlagEphemeral})
	}
	data := event.SlashCommandInteractionData()
	user := data.Snowflake("user")
	remove := data.Bool("remove")

	if remove {
		slog.Debug("removing user from whitelist", slog.String("user", user.String()))
		removeFromWhitelist(user)
		return event.CreateMessage(discord.MessageCreate{Content: "Removed user from whitelist", Flags: discord.MessageFlagEphemeral})
	} else {
		slog.Debug("adding user to whitelist", slog.String("user", user.String()))
		addToWhitelist(user)
		return event.CreateMessage(discord.MessageCreate{Content: "Added user to whitelist", Flags: discord.MessageFlagEphemeral})
	}
}

func handleLobotomy(event *handler.CommandEvent) error {
	if _, ok := dmCache[event.Channel().ID()]; ok {
		delete(dmCache, event.Channel().ID())
		saveDmCache()
		return event.CreateMessage(discord.MessageCreate{Content: "Lobotomized for this channel"})
	}
	return nil
}
