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
	"strings"
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

func makeGptCommands() []discord.SlashCommandCreate {
	var commands []discord.SlashCommandCreate
	for _, model := range llm.AllModels {
		var sb strings.Builder
		sb.WriteString(model.Name)
		if model.NeedsWhitelist {
			sb.WriteString(" (Whitelist)")
		}
		if model.Vision {
			sb.WriteString(" (Vision)")
		}
		commands = append(commands, makeGptCommand(model.Command, sb.String()))
	}
	return commands
}

var (
	token    = os.Getenv("X3ZEO_DISCORD_TOKEN")
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
		// gpt commands are added in init()
	}

	db *sql.DB
)

const (
	// LLM interaction context surrounding messages
	maxContextMessages = 30
	maxRedditAttempts  = 3
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
	slog.Info("x3zeo booting up...")
	slog.Info("disgo version", slog.String("version", disgo.Version))

	r := handler.New()
	r.Use(middleware.Logger)

	// LLM commands
	registerLlm := func(r handler.Router, command string, model llm.Model) {
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
		for _, model := range llm.AllModels {
			registerLlm(r, "/"+model.Command, model)
		}
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
			msg.Interaction.Name == "lobotomy" {
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

func handleLlm(event *handler.CommandEvent, model llm.Model) error {
	data := event.SlashCommandInteractionData()
	prompt := data.String("prompt")
	ephemeral := data.Bool("ephemeral")

	var llmer *llm.Llmer

	// check if we have perms to read messages in this channel
	useCache := event.AppPermissions() != nil && !event.AppPermissions().Has(discord.PermissionReadMessageHistory)

	if useCache {
		// we are in a DM, so we cannot read surrounding messages. Instead, we use a cache
		slog.Debug("in a DM; looking up DM cache", slog.String("channel", event.Channel().ID().String()))
		var err error
		llmer, err = getLlmerFromCache(event.Channel().ID())
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return handleFollowupError(event, err, ephemeral)
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
	addImageAttachments(llmer, event.Message)

	// now we generate the LLM response
	response, err := llmer.RequestCompletion(llm.ModelGpt4oMini)
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
		if err := handleLlmInteraction(event); err != nil {
			slog.Error("failed to handle DM interaction", slog.Any("err", err))
		}
		return
	}

	for _, user := range event.Message.Mentions {
		if user.ID == event.Client().ID() {
			slog.Debug("handling @mention interaction")
			if err := handleLlmInteraction(event); err != nil {
				slog.Error("failed to handle DM interaction", slog.Any("err", err))
			}
			return
		}
	}

	if event.Message.ReferencedMessage != nil {
		// this is a response to a message...
		if event.Message.ReferencedMessage.Author.ID == event.Client().ID() {
			// ...that was created by us
			slog.Debug("handling reply interaction")
			if err := handleLlmInteraction(event); err != nil {
				slog.Error("failed to handle DM interaction", slog.Any("err", err))
			}
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

	if err := eraseLlmerFromCache(event.Channel().ID()); err != nil {
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
