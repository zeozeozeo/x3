package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
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
	"github.com/zeozeozeo/x3/reddit"

	"database/sql"

	_ "github.com/mattn/go-sqlite3"
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
				discord.ApplicationIntegrationTypeUserInstall,
			},
			Contexts: []discord.InteractionContextType{
				discord.InteractionContextTypeGuild,
				discord.InteractionContextTypeBotDM,
				discord.InteractionContextTypePrivateChannel,
			},
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
					Required:    false,
				},
			},
		},
		discord.SlashCommandCreate{
			Name:        "boykisser",
			Description: "Send boykisser image",
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
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
					Required:    false,
				},
			},
		},
	}

	db *sql.DB
)

const (
	// LLM interaction context surrounding messages
	maxContextMessages = 50
)

func addToWhitelist(id snowflake.ID) {
	_, err := db.Exec("INSERT OR IGNORE INTO whitelist (user_id) VALUES (?)", id.String())
	if err != nil {
		slog.Error("failed to add user to whitelist", slog.Any("err", err))
	}
}

func removeFromWhitelist(id snowflake.ID) {
	_, err := db.Exec("DELETE FROM whitelist WHERE user_id = ?", id.String())
	if err != nil {
		slog.Error("failed to remove user from whitelist", slog.Any("err", err))
	}
}

func isInWhitelist(id snowflake.ID) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM whitelist WHERE user_id = ?", id.String()).Scan(&count)
	if err != nil {
		slog.Error("failed to check if user is in whitelist", slog.Any("err", err))
	}
	return count > 0
}

func getMessageInteractionPrompt(id snowflake.ID) (string, error) {
	var prompt string
	err := db.QueryRow("SELECT prompt FROM message_interaction_cache WHERE message_id = ?", id.String()).Scan(&prompt)
	if err != nil {
		slog.Warn("failed to get message interaction prompt", slog.Any("err", err))
	}
	return prompt, err
}

func getLlmerFromCache(id snowflake.ID) (*llm.Llmer, error) {
	var data []byte
	err := db.QueryRow("SELECT llm_state FROM dm_cache WHERE channel_id = ?", id.String()).Scan(&data)
	if err != nil {
		slog.Warn("failed to get llm state from cache", slog.Any("err", err))
		return nil, err
	}
	// decode json
	return llm.UnmarshalLlmer(data)
}

func writeLlmerToCache(id snowflake.ID, llmer *llm.Llmer) error {
	data, err := json.Marshal(llmer)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT OR REPLACE INTO dm_cache (channel_id, llm_state) VALUES (?, ?)", id.String(), data)
	return err
}

func eraseLlmerFromCache(id snowflake.ID) error {
	_, err := db.Exec("DELETE FROM dm_cache WHERE channel_id = ?", id.String())
	return err
}

func writeMessageInteractionPrompt(id snowflake.ID, prompt string) error {
	_, err := db.Exec("INSERT OR REPLACE INTO message_interaction_cache (message_id, prompt) VALUES (?, ?)", id.String(), prompt)
	return err
}

func init() {
	var err error
	db, err = sql.Open("sqlite3", "x3.db")
	if err != nil {
		panic(err)
	}

	// whitelist
	_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS whitelist (
				user_id TEXT PRIMARY KEY
			)
        `)
	if err != nil {
		panic(err)
	}

	// dm (private channel) cache
	_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS dm_cache (
				channel_id TEXT PRIMARY KEY,
				llm_state BLOB
			)
        `)
	if err != nil {
		panic(err)
	}

	// message interaction cache
	_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS message_interaction_cache (
				message_id TEXT PRIMARY KEY,
				prompt TEXT
			)
		`)
	if err != nil {
		panic(err)
	}
}

func main() {
	defer db.Close()

	slog.SetLogLoggerLevel(slog.LevelInfo)
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

	// utils
	r.Group(func(r handler.Router) {
		r.Command("/whitelist", handleWhitelist)
		r.Command("/lobotomy", handleLobotomy)
	})

	// image
	r.Group(func(r handler.Router) {
		r.Command("/boykisser", handleBoykisser)
	})

	r.ButtonComponent("/refresh_boykisser", handleBoykisserRefresh)

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

func handleFollowupError(event *handler.CommandEvent, err error) error {
	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Error: %v", err),
		Flags:   discord.MessageFlagEphemeral,
	})
}

func formatMsg(msg, username string) string {
	return msg
}

// returns whether a lobotomy was performed
func addContextMessagesIfPossible(client bot.Client, llmer *llm.Llmer, channelID, messageID snowflake.ID) bool {
	messages, err := client.Rest().GetMessages(channelID, 0, messageID, 0, maxContextMessages)
	if err != nil {
		return false
	}

	// discord returns surrounding message history from newest to oldest, but we want oldest to newest
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role := llm.RoleUser
		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
		} else if interaction, err := getMessageInteractionPrompt(msg.ID); err == nil {
			// the prompt used for this response is in the interaction cache
			llmer.AddMessage(llm.RoleUser, interaction)
		}

		if msg.Interaction != nil {
			if msg.Interaction.Type == discord.InteractionTypeApplicationCommand && msg.Interaction.Name == "lobotomy" {
				slog.Debug("found lobotomy interaction", slog.String("channel", channelID.String()), slog.String("message", msg.ID.String()))
				//llmer.Lobotomize()
				// but we keep adding new messages from this point
			}
		}

		llmer.AddMessage(role, formatMsg(msg.Content, msg.Author.Username))
	}
	return false
}

func handleLlm(event *handler.CommandEvent, model string) error {
	data := event.SlashCommandInteractionData()
	prompt := data.String("prompt")
	ephemeral := data.Bool("ephemeral")

	var llmer *llm.Llmer

	// check if we have perms to read messages in this channel
	useCache := event.Channel().Permissions.Has(discord.PermissionReadMessageHistory) &&
		event.Channel().Type() != discord.ChannelTypeDM &&
		event.Channel().Type() != discord.ChannelTypeGroupDM

	if useCache {
		// we are in a DM, so we cannot read surrounding messages. Instead, we use a cache
		slog.Debug("in a DM; looking up DM cache", slog.String("channel", event.Channel().ID().String()))
		var err error
		llmer, err = getLlmerFromCache(event.Channel().ID())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return handleFollowupError(event, err)
		}

		if llmer == nil {
			// not in cache, so create
			slog.Debug("not in dmCache; creating", slog.String("channel", event.Channel().ID().String()))
			llmer = llm.NewLlmer()
			if err := writeLlmerToCache(event.Channel().ID(), llmer); err != nil {
				// is fine, don't sweat
				slog.Error("failed to save llm state to cache", slog.Any("err", err))
			}
		}
	} else {
		// we are not in a DM, so we can read surrounding messages
		llmer = llm.NewLlmer()
	}

	// add context if possible
	lastMessage := event.Channel().MessageChannel.LastMessageID()
	if !useCache && lastMessage != nil {
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
		event.DeleteInteractionResponse()
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Failed to generate response: %v", err),
			Flags:   discord.MessageFlagEphemeral,
		})
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	botMessage, err := event.UpdateInteractionResponse(discord.MessageUpdate{
		Content: &response,
		Flags:   &flags,
	})

	// only clients can query options passed to commands, so we cache the action interaction
	if err == nil {
		writeMessageInteractionPrompt(botMessage.ID, prompt)
	}

	if useCache {
		if err := writeLlmerToCache(event.Channel().ID(), llmer); err != nil {
			// is fine, don't sweat
			slog.Error("failed to save llm state to cache (2)", slog.Any("err", err))
		}
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
	ephemeral := event.SlashCommandInteractionData().Bool("ephemeral")

	if err := eraseLlmerFromCache(event.Channel().ID()); err != nil {
		return handleFollowupError(event, err)
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: "Lobotomized for this channel",
		Flags:   flags,
	})
}

func fetchBoykisser() (*http.Response, reddit.Post, error) {
	post, err := reddit.GetRandomImageFromSubreddits("boykisser", "boykisser2", "boykissermemes", "wholesomeboykissers")
	if err != nil {
		return nil, post, err
	}

	// silly discord thing: we can't make image attachments using the URL;
	// we actually have to fetch the file and upload it as an octet stream
	client := &http.Client{}
	req, err := http.NewRequest("GET", post.Data.URL, nil)
	if err != nil {
		return nil, post, err
	}
	req.Header.Set("User-Agent", reddit.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, post, err
	}

	return resp, post, nil
}

func handleBoykisser(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	ephemeral := data.Bool("ephemeral")

	resp, post, err := fetchBoykisser()
	if err != nil {
		return handleFollowupError(event, err)
	}
	defer resp.Body.Close()

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	return event.CreateMessage(discord.MessageCreate{
		Files: []*discord.File{
			{
				Name:   path.Base(post.Data.URL),
				Reader: resp.Body,
			},
		},
		Components: []discord.ContainerComponent{
			discord.ActionRowComponent{
				discord.ButtonComponent{
					Style: discord.ButtonStyleLink,
					Emoji: &discord.ComponentEmoji{
						Name: "💦",
					},
					URL: post.Data.GetPostLink(),
				},
				discord.ButtonComponent{
					Style: discord.ButtonStyleSecondary,
					Emoji: &discord.ComponentEmoji{
						Name: "🔄",
					},
					CustomID: "refresh_boykisser",
				},
			},
		},
		Flags: flags,
	})
}

func handleBoykisserRefresh(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	resp, post, err := fetchBoykisser()
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return event.UpdateMessage(discord.MessageUpdate{
		Files: []*discord.File{
			{
				Name:   path.Base(post.Data.URL),
				Reader: resp.Body,
			},
		},
		Components: &[]discord.ContainerComponent{
			discord.ActionRowComponent{
				discord.ButtonComponent{
					Style: discord.ButtonStyleLink,
					Emoji: &discord.ComponentEmoji{
						Name: "💦",
					},
					URL: post.Data.GetPostLink(),
				},
				discord.ButtonComponent{
					Style: discord.ButtonStyleSecondary,
					Emoji: &discord.ComponentEmoji{
						Name: "🔄",
					},
					CustomID: "refresh_boykisser",
				},
			},
		},
	})
}
