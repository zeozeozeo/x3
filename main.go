package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"syscall"

	// load .env before importing our modules
	_ "github.com/joho/godotenv/autoload"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/disgo/handler/middleware"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
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

func formatModel(model model.Model) string {
	var sb strings.Builder
	sb.WriteString(model.Name)
	if model.NeedsWhitelist {
		sb.WriteString(" (Whitelist)")
	}
	if model.Vision {
		sb.WriteString(" (Vision)")
	}
	return sb.String()
}

func makeGptCommands() []discord.SlashCommandCreate {
	var commands []discord.SlashCommandCreate
	for _, model := range model.AllModels {
		commands = append(commands, makeGptCommand(model.Command, formatModel(model)))
	}
	return commands
}

func makePersonaOptionChoices() []discord.ApplicationCommandOptionChoiceString {
	var choices []discord.ApplicationCommandOptionChoiceString
	for _, p := range persona.AllPersonas {
		choices = append(choices, discord.ApplicationCommandOptionChoiceString{Name: p.Name, Value: p.Name})
	}
	return choices
}

func makeModelOptionChoices() []discord.ApplicationCommandOptionChoiceString {
	var choices []discord.ApplicationCommandOptionChoiceString
	for _, m := range model.AllModels {
		if len(choices) >= 25 {
			break
		}
		name := formatModel(m)
		choices = append(choices, discord.ApplicationCommandOptionChoiceString{Name: name, Value: m.Name})
	}
	return choices
}

var (
	token    = os.Getenv("X3_DISCORD_TOKEN")
	commands = []discord.ApplicationCommandCreate{
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
				discord.ApplicationCommandOptionBool{
					Name:        "check",
					Description: "Check if the user is in the whitelist",
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
		discord.SlashCommandCreate{
			Name:        "persona",
			Description: "Set persona, model or system prompt for this channel",
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
					Name:        "persona",
					Description: "Choose a pre-made persona for this chat",
					Choices:     makePersonaOptionChoices(),
					Required:    false,
				},
				discord.ApplicationCommandOptionString{
					Name:        "system",
					Description: "Set a custom system prompt for this chat",
					Required:    false,
				},
				discord.ApplicationCommandOptionString{
					Name:        "model",
					Description: "Set a model to use for this chat",
					Choices:     makeModelOptionChoices(),
					Required:    false,
				},
			},
		},
		// gpt commands are added in init()
	}

	db *sql.DB
)

const (
	// LLM interaction context surrounding messages
	maxContextMessages = 30
	maxRedditAttempts  = 3
)

type ChannelCache struct {
	// in channels where the bot cannot read messages this is set for caching messages
	Llmer       *llm.Llmer          `json:"llmer"`
	PersonaMeta persona.PersonaMeta `json:"persona_meta"`
}

func newChannelCache() *ChannelCache {
	return &ChannelCache{PersonaMeta: persona.PersonaX3}
}

func unmarshalChannelCache(data []byte) (*ChannelCache, error) {
	var cache ChannelCache
	err := json.Unmarshal(data, &cache)
	return &cache, err
}

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

// never returns nil
func getChannelCache(id snowflake.ID) *ChannelCache {
	var data []byte
	err := db.QueryRow("SELECT cache FROM channel_cache WHERE channel_id = ?", id.String()).Scan(&data)
	if err != nil {
		slog.Warn("failed to get channel cache", slog.Any("err", err))
		return newChannelCache()
	}
	// decode json
	cache, err := unmarshalChannelCache(data)
	if err != nil {
		slog.Warn("failed to unmarshal channel cache", slog.Any("err", err))
		cache = newChannelCache()
		writeChannelCache(id, cache)
	}
	return cache
}

func writeChannelCache(id snowflake.ID, cache *ChannelCache) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	// write json
	_, err = db.Exec("INSERT OR REPLACE INTO channel_cache (channel_id, cache) VALUES (?, ?)", id.String(), data)
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
			CREATE TABLE IF NOT EXISTS channel_cache (
				channel_id TEXT PRIMARY KEY,
				cache BLOB
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

	// default db state
	addToWhitelist(890686470556356619)

	// gpt commands
	for _, command := range makeGptCommands() {
		commands = append(commands, command)
	}
}

func main() {
	defer db.Close()

	slog.SetLogLoggerLevel(slog.LevelDebug)
	slog.Info("x3 booting up...")
	slog.Info("disgo version", slog.String("version", disgo.Version))

	r := handler.New()
	r.Use(middleware.Logger)

	// LLM commands
	registerLlm := func(r handler.Router, command string, model model.Model) {
		r.Command(command, func(event *handler.CommandEvent) error {
			if model.NeedsWhitelist && !isInWhitelist(event.User().ID) {
				return event.CreateMessage(discord.MessageCreate{
					Content: "You are not in the whitelist, therefore you cannot use this command. Try `/gpt4o`.",
					Flags:   discord.MessageFlagEphemeral,
				})
			}
			return handleLlm(event, model)
		})
	}
	r.Group(func(r handler.Router) {
		for _, model := range model.AllModels {
			registerLlm(r, "/"+model.Command, model)
		}
	})

	// utils
	r.Group(func(r handler.Router) {
		r.Command("/whitelist", handleWhitelist)
		r.Command("/lobotomy", handleLobotomy)
		r.Command("/persona", handlePersona)
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

	slog.Info("x3 is running. ctrl+c to stop")
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}

func handleNotFound(event *handler.InteractionEvent) error {
	return event.CreateMessage(discord.MessageCreate{Content: "Command not found", Flags: discord.MessageFlagEphemeral})
}

func handleFollowupError(event *handler.CommandEvent, err error, ephemeral bool) error {
	slog.Error("handleFollowupError", slog.Any("err", err))
	if ephemeral {
		content := fmt.Sprintf("Error: %v", err)
		flags := discord.MessageFlagEphemeral
		_, err = event.UpdateInteractionResponse(discord.MessageUpdate{
			Content: &content,
			Flags:   &flags,
		})
		return err
	} else {
		return event.DeleteInteractionResponse()
	}
}

func formatMsg(msg, username string) string {
	return msg
}

func isImageAttachment(attachment discord.Attachment) bool {
	return attachment.ContentType != nil && strings.HasPrefix(*attachment.ContentType, "image/")
}

func addImageAttachments(llmer *llm.Llmer, msg discord.Message) {
	for _, attachment := range msg.Attachments {
		if isImageAttachment(attachment) {
			llmer.AddImage(attachment.URL)
		}
	}
}

// returns whether a lobotomy was performed
func addContextMessagesIfPossible(client bot.Client, llmer *llm.Llmer, channelID, messageID snowflake.ID) bool {
	messages, err := client.Rest().GetMessages(channelID, 0, messageID, 0, maxContextMessages)
	if err != nil {
		return false
	}

	latestImageAttachmentIdx := -1

	// newest to oldest!
outer:
	for i, msg := range messages {
		for _, attachment := range msg.Attachments {
			if isImageAttachment(attachment) {
				latestImageAttachmentIdx = i
				break outer
			}
		}
	}

	// discord returns surrounding message history from newest to oldest, but we want oldest to newest
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Interaction != nil &&
			msg.Interaction.Type == discord.InteractionTypeApplicationCommand &&
			(msg.Interaction.Name == "lobotomy" || msg.Interaction.Name == "persona") {
			//slog.Debug("found lobotomy interaction", slog.String("channel", channelID.String()), slog.String("message", msg.ID.String()))
			//llmer.Lobotomize()
			// but we keep adding new messages from this point
			// TODO
			continue
		}

		role := llm.RoleUser
		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
		} else if interaction, err := getMessageInteractionPrompt(msg.ID); err == nil {
			// the prompt used for this response is in the interaction cache
			llmer.AddMessage(llm.RoleUser, interaction)
		}

		llmer.AddMessage(role, formatMsg(msg.Content, msg.Author.Username))

		// if this is the last message with an image we add, check for images
		if i == latestImageAttachmentIdx {
			addImageAttachments(llmer, msg)
		}
	}
	return false
}

func sendMessageSplits(client bot.Client, messageID snowflake.ID, event *handler.CommandEvent, flags discord.MessageFlags, channelID snowflake.ID, runes []rune) (*discord.Message, error) {
	// if messageID != 0, first respond to the message with the first 2000 characters, then
	// send the remaining 2000character-splits as regular messages.
	// if messageID == 0, send the 2000character-splits as separate messages.
	messageLen := len(runes)
	numMessages := (messageLen + 2000 - 1) / 2000
	var botMessage *discord.Message

	for i := 0; i < numMessages; i++ {
		start := i * 2000
		end := start + 2000
		if end > messageLen {
			end = messageLen
		}
		segment := string(runes[start:end])

		var message *discord.Message
		var err error
		if i == 0 {
			if messageID != 0 {
				message, err = client.Rest().CreateMessage(
					channelID,
					discord.MessageCreate{
						Content: segment,
						Flags:   flags,
						MessageReference: &discord.MessageReference{
							MessageID: &messageID,
						},
						AllowedMentions: &discord.AllowedMentions{
							RepliedUser: false,
						},
					},
				)
			} else if event != nil {
				message, err = event.UpdateInteractionResponse(discord.MessageUpdate{
					Content: &segment,
					Flags:   &flags,
				})
			}
		} else {
			message, err = client.Rest().CreateMessage(channelID, discord.MessageCreate{Content: segment})
		}

		if err != nil {
			return nil, err
		}
		if botMessage == nil {
			botMessage = message
		}
	}

	return botMessage, nil
}

func handleLlm(event *handler.CommandEvent, model model.Model) error {
	data := event.SlashCommandInteractionData()
	prompt := data.String("prompt")
	ephemeral := data.Bool("ephemeral")

	var llmer *llm.Llmer
	cache := getChannelCache(event.Channel().ID())

	// check if we have perms to read messages in this channel
	useCache := event.AppPermissions() != nil && !event.AppPermissions().Has(discord.PermissionReadMessageHistory)

	if useCache {
		// we are in a DM, so we cannot read surrounding messages. Instead, we use a cache
		slog.Debug("in a DM; looking up DM cache", slog.String("channel", event.Channel().ID().String()))
		llmer = cache.Llmer

		if llmer == nil {
			// not in cache, so create (but write it later)
			slog.Debug("not in dmCache; creating new llmer", slog.String("channel", event.Channel().ID().String()))
			llmer = llm.NewLlmer()
		}
	} else {
		// we are not in a DM, so we can read surrounding messages
		llmer = llm.NewLlmer()
	}

	// set persona
	llmer.SetPersona(persona.GetPersonaByMeta(cache.PersonaMeta))

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

	// discord only gives us 3s to respond unless we do this (x3 is thinking...)
	event.DeferCreateMessage(ephemeral)

	response, err := llmer.RequestCompletion(model)
	if err != nil {
		slog.Error("failed to generate response", slog.Any("err", err))
		return handleFollowupError(event, err, ephemeral)
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	var botMessage *discord.Message
	responseRunes := []rune(response)
	if !useCache && !ephemeral {
		botMessage, err = sendMessageSplits(event.Client(), 0, event, flags, event.Channel().ID(), responseRunes)
	} else if len(responseRunes) > 2000 {
		// send as file
		botMessage, err = event.UpdateInteractionResponse(discord.MessageUpdate{
			Files: []*discord.File{
				{
					Name:   fmt.Sprintf("response-%v.txt", event.ID()),
					Reader: strings.NewReader(response),
				},
			},
		})
	} else {
		// less or equal to 2000, no need to split/txt
		botMessage, err = event.UpdateInteractionResponse(discord.MessageUpdate{
			Content: &response,
			Flags:   &flags,
		})
	}

	if err != nil {
		return handleFollowupError(event, err, ephemeral)
	}

	// only clients can query options passed to commands, so we cache the action interaction
	writeMessageInteractionPrompt(botMessage.ID, prompt)

	if useCache {
		cache.Llmer = llmer
		if err := writeChannelCache(event.Channel().ID(), cache); err != nil {
			// is fine, don't sweat
			slog.Error("failed to save channel cache", slog.Any("err", err))
		}
	}

	return nil
}

var containsX3Regex = regexp.MustCompile(`\b[Xx]3\b`)

func handleLlmInteraction(event *events.MessageCreate, eraseX3 bool) error {
	if err := event.Client().Rest().SendTyping(event.ChannelID); err != nil {
		slog.Error("failed to SendTyping", slog.Any("err", err))
	}

	// the interaction happened in a server, so we can get the surrounding messages
	llmer := llm.NewLlmer()
	addContextMessagesIfPossible(event.Client(), llmer, event.ChannelID, event.MessageID)
	slog.Debug("interaction; added context messages", slog.Int("count", llmer.NumMessages()))

	// and we also want the event message
	var content string
	if eraseX3 {
		content = containsX3Regex.ReplaceAllString(event.Message.Content, "")
		content = strings.TrimSpace(content)
	} else {
		content = event.Message.Content
	}
	llmer.AddMessage(llm.RoleUser, formatMsg(content, event.Message.Author.Username))
	addImageAttachments(llmer, event.Message)

	// get channel cache to get the channel persona
	cache := getChannelCache(event.ChannelID)
	llmer.SetPersona(persona.GetPersonaByMeta(cache.PersonaMeta))

	// now we generate the LLM response
	response, err := llmer.RequestCompletion(model.GetModelByName(cache.PersonaMeta.Model))
	if err != nil {
		slog.Error("failed to generate response", slog.Any("err", err))
		return err
	}

	// and send the response
	sendMessageSplits(event.Client(), event.MessageID, nil, 0, event.ChannelID, []rune(response))

	return nil
}

func onMessageCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	if event.Message.GuildID == nil {
		// DM
		if err := handleLlmInteraction(event, false); err != nil {
			slog.Error("failed to handle DM interaction", slog.Any("err", err))
		}
		return
	}

	for _, user := range event.Message.Mentions {
		if user.ID == event.Client().ID() {
			slog.Debug("handling @mention interaction")
			if err := handleLlmInteraction(event, false); err != nil {
				slog.Error("failed to handle mention interaction", slog.Any("err", err))
			}
			return
		}
	}

	if event.Message.ReferencedMessage != nil {
		// this is a response to a message...
		if event.Message.ReferencedMessage.Author.ID == event.Client().ID() {
			// ...that was created by us
			slog.Debug("handling reply interaction")
			if err := handleLlmInteraction(event, false); err != nil {
				slog.Error("failed to handle reply interaction", slog.Any("err", err))
			}
			return
		}
	}

	// check if "x3" is mentioned
	if containsX3Regex.MatchString(event.Message.Content) {
		slog.Debug("handling x3 interaction")
		if err := handleLlmInteraction(event, true); err != nil {
			slog.Error("failed to handle x3 interaction", slog.Any("err", err))
		}
		return
	}
}

func handleWhitelist(event *handler.CommandEvent) error {
	if !isInWhitelist(event.User().ID) {
		return event.CreateMessage(discord.MessageCreate{Content: "You are not in the whitelist, therefore you cannot whitelist other users", Flags: discord.MessageFlagEphemeral})
	}
	data := event.SlashCommandInteractionData()
	user := data.Snowflake("user")
	remove := data.Bool("remove")
	check := data.Bool("check")

	if check {
		msg := "User is not in whitelist"
		if isInWhitelist(user) {
			msg = "User is in whitelist"
		}
		return event.CreateMessage(discord.MessageCreate{Content: msg, Flags: discord.MessageFlagEphemeral})
	}

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

	if err := writeChannelCache(event.Channel().ID(), newChannelCache()); err != nil {
		return handleFollowupError(event, err, ephemeral)
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

func fetchBoykisser(attempts int) (*http.Response, reddit.Post, error) {
	slog.Info("fetchBoykisser", slog.Int("attempts", attempts))
	//if attempts > 1 {
	//	// perhaps reddit ratelimits us
	//	time.Sleep(500 * time.Millisecond)
	//}

	post, err := reddit.GetRandomImageFromSubreddits("boykisser", "boykisser2", "boykissermemes", "wholesomeboykissers")
	if err != nil {
		if attempts < maxRedditAttempts {
			return fetchBoykisser(attempts + 1)
		}
		return nil, post, err
	}

	url := post.Data.GetRandomImage()

	// silly discord thing: we can't make image attachments using the URL;
	// we actually have to fetch the file and upload it as an octet stream
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		if attempts < maxRedditAttempts {
			return fetchBoykisser(attempts + 1)
		}
		return nil, post, err
	}
	req.Header.Set("User-Agent", reddit.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		if attempts < maxRedditAttempts {
			return fetchBoykisser(attempts + 1)
		}
		return nil, post, err
	}

	return resp, post, nil
}

func handleBoykisser(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	ephemeral := data.Bool("ephemeral")

	event.DeferCreateMessage(ephemeral)

	resp, post, err := fetchBoykisser(1)
	if err != nil {
		return handleFollowupError(event, err, ephemeral)
	}
	defer resp.Body.Close()

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	url := post.Data.GetRandomImage()

	_, err = event.UpdateInteractionResponse(discord.MessageUpdate{
		Files: []*discord.File{
			{
				Name:   path.Base(url),
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
		Flags: &flags,
	})
	return err
}

func handleBoykisserRefresh(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	event.DeferUpdateMessage()
	resp, post, err := fetchBoykisser(1)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	url := post.Data.GetRandomImage()

	_, err = event.UpdateInteractionResponse(discord.MessageUpdate{
		Files: []*discord.File{
			{
				Name:   path.Base(url),
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
	return err
}

func handlePersona(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	dataPersona := data.String("persona")
	dataModel := data.String("model")
	dataSystem := data.String("system")

	m := model.GetModelByName(dataModel)
	if m.NeedsWhitelist && !isInWhitelist(event.User().ID) {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContentf("You need to be whitelisted to set the model `%s`. Try `%s`", dataModel, model.ModelGpt4oMini.Name).
				Build(),
		)
	}

	cache := getChannelCache(event.Channel().ID())

	// update non-empty slash command fields
	if dataPersona != "" {
		cache.PersonaMeta.Name = dataPersona
	}
	if dataSystem != "" {
		cache.PersonaMeta.System = dataSystem
	}
	if dataModel != "" {
		cache.PersonaMeta.Model = dataModel
	}

	if err := writeChannelCache(event.Channel().ID(), cache); err != nil {
		return handleFollowupError(event, err, false)
	}

	var sb strings.Builder
	sb.WriteString("Updated persona for this channel")
	didWhat := []string{}
	if dataPersona != "" {
		didWhat = append(didWhat, fmt.Sprintf("set persona to `%s`", cache.PersonaMeta.Name))
	}
	if dataModel != "" {
		didWhat = append(didWhat, fmt.Sprintf("set model to `%s`", cache.PersonaMeta.Model))
	}
	if dataSystem != "" {
		didWhat = append(didWhat, "updated the system prompt")
	}

	if len(didWhat) > 0 {
		sb.WriteString(fmt.Sprintf(" (%s)", strings.Join(didWhat, ", ")))
	}
	sb.WriteString(". Use `/lobotomy` to reset.")

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetContent(sb.String()).
			Build(),
	)
}
