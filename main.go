package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

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
	"github.com/dustin/go-humanize"
	"github.com/lithammer/fuzzysearch/fuzzy"
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
		choices = append(choices, discord.ApplicationCommandOptionChoiceString{Name: p.String(), Value: p.Name})
	}
	return choices
}

func makeModelOptionChoices() []discord.ApplicationCommandOptionChoiceString {
	var choices []discord.ApplicationCommandOptionChoiceString
	for _, m := range model.AllModels {
		if len(choices) >= 25 {
			// TODO: discord limits us to 25 choices...
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
				discord.ApplicationCommandOptionInt{
					Name:        "amount",
					Description: "The amount of last messages to forget. By default, removes all",
					Required:    false,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "reset_persona",
					Description: "Also set the persona to the default one",
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
				discord.ApplicationCommandOptionBool{
					Name:        "roleplay",
					Description: "Set roleplay mode for this chat",
					Required:    false,
				},
				discord.ApplicationCommandOptionInt{
					Name:        "context",
					Description: "Amount of surrounding messages to use as context. Pass a negative number to reset",
					Required:    false,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
					Required:    false,
				},
			},
		},
		discord.SlashCommandCreate{
			Name:        "stats",
			Description: "Bot and per-chat usage stats",
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
					Description: "If the response should only be visible to you (default: true)",
					Required:    false,
				},
			},
		},
		discord.SlashCommandCreate{
			Name:        "quote",
			Description: "Get a server quote. Reply to a message with \"x3 quote\" to make a new quote",
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
				discord.ApplicationCommandOptionSubCommand{
					Name:        "get",
					Description: "Get a quote by name",
					Options: []discord.ApplicationCommandOption{
						discord.ApplicationCommandOptionString{
							// autocompleted
							Name:         "name",
							Description:  "Name of the quote",
							Required:     true,
							Autocomplete: true,
						},
						discord.ApplicationCommandOptionBool{
							Name:        "ephemeral",
							Description: "If the response should only be visible to you",
							Required:    false,
						},
					},
				},
				discord.ApplicationCommandOptionSubCommand{
					Name:        "random",
					Description: "Get a random quote",
					Options: []discord.ApplicationCommandOption{
						discord.ApplicationCommandOptionBool{
							Name:        "ephemeral",
							Description: "If the response should only be visible to you",
							Required:    false,
						},
					},
				},
				discord.ApplicationCommandOptionSubCommand{
					Name:        "new",
					Description: "Make a quote of your own. Available in DMs",
					Options: []discord.ApplicationCommandOption{
						discord.ApplicationCommandOptionString{
							Name:        "text",
							Description: "Text of the quote",
							Required:    true,
						},
						discord.ApplicationCommandOptionBool{
							Name:        "ephemeral",
							Description: "If the response should only be visible to you",
							Required:    false,
						},
					},
				},
			},
		},
		// gpt commands are added in init(), except for this one
		makeGptCommand("chat", "Chat with the current persona"),
	}

	db *sql.DB

	startTime = time.Now()
)

const (
	// LLM interaction context surrounding messages
	defaultContextMessages = 30
	maxRedditAttempts      = 3
)

type ChannelCache struct {
	// in channels where the bot cannot read messages this is set for caching messages
	Llmer         *llm.Llmer          `json:"llmer"`
	PersonaMeta   persona.PersonaMeta `json:"persona_meta"`
	Usage         llm.Usage           `json:"usage,omitempty"`
	LastUsage     llm.Usage           `json:"last_usage,omitempty"`
	ContextLength int                 `json:"context_length"`
}

func newChannelCache() *ChannelCache {
	return &ChannelCache{PersonaMeta: persona.PersonaProto, ContextLength: defaultContextMessages}
}

func unmarshalChannelCache(data []byte) (*ChannelCache, error) {
	cache := ChannelCache{
		ContextLength: defaultContextMessages,
	}
	err := json.Unmarshal(data, &cache)
	return &cache, err
}

func (cache ChannelCache) write(id snowflake.ID) error {
	slog.Debug("writing channel cache", slog.String("channel_id", id.String()))
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT OR REPLACE INTO channel_cache (channel_id, cache) VALUES (?, ?)", id.String(), data)
	return err
}

type GlobalStats struct {
	Usage llm.Usage `json:"usage"`
	// total number of messages processed
	MessageCount uint `json:"message_count"`
}

func unmarshalGlobalStats(data []byte) (GlobalStats, error) {
	var stats GlobalStats
	err := json.Unmarshal(data, &stats)
	return stats, err
}

func (s GlobalStats) write() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = db.Exec("UPDATE global_stats SET stats = ? WHERE EXISTS (SELECT 1 FROM global_stats)", data)
	return err
}

type Quote struct {
	MessageID     snowflake.ID `json:"message_id"`
	Quoter        snowflake.ID `json:"quoter"`
	AuthorID      snowflake.ID `json:"author_id"`
	AuthorUser    string       `json:"author_user"`
	Channel       snowflake.ID `json:"channel"`
	Text          string       `json:"text"`
	AttachmentURL string       `json:"attachment_url"`
	Timestamp     time.Time    `json:"timestamp"`
}

type ServerStats struct {
	Quotes []Quote `json:"quotes"`
}

func (s ServerStats) QuoteExists(quote Quote) (bool, int) {
	for i, q := range s.Quotes {
		if quote.MessageID == 0 {
			if q.Channel == quote.Channel && q.AuthorID == quote.AuthorID && q.Text == quote.Text {
				return true, i
			}
		} else if q.MessageID == quote.MessageID || q.Timestamp.Equal(quote.Timestamp) {
			return true, i
		}
	}
	return false, 0
}

func (s *ServerStats) AddQuote(quote Quote) int {
	s.Quotes = append(s.Quotes, quote)
	return len(s.Quotes)
}

func unmarshalServerStats(data []byte) (ServerStats, error) {
	var stats ServerStats
	err := json.Unmarshal(data, &stats)
	return stats, err
}

func (s ServerStats) write(serverID snowflake.ID) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = db.Exec("INSERT OR REPLACE INTO server_stats (server_id, stats) VALUES (?, ?)", serverID.String(), data)
	return err
}

func getServerStats(serverID snowflake.ID) (ServerStats, error) {
	var data []byte
	err := db.QueryRow("SELECT stats FROM server_stats WHERE server_id = ?", serverID.String()).Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ServerStats{}, nil
		}
		return ServerStats{}, err
	}
	return unmarshalServerStats(data)
}

func getGlobalStats() (GlobalStats, error) {
	var data []byte
	err := db.QueryRow("SELECT stats FROM global_stats").Scan(&data)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GlobalStats{}, nil
		}
		return GlobalStats{}, err
	}
	return unmarshalGlobalStats(data)
}

func updateGlobalStats(usage llm.Usage) error {
	stats, err := getGlobalStats()
	if err != nil {
		slog.Error("updateGlobalStats: failed to get global stats", slog.Any("err", err))
		return err
	}
	stats.Usage = stats.Usage.Add(usage)
	stats.MessageCount++
	if err := stats.write(); err != nil {
		slog.Error("updateGlobalStats: failed to write global stats", slog.Any("err", err))
		return err
	}
	return nil
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
	// if this fails this is actually fine, we expect most of calls to this to fail
	// (because only slash command interactions are cached)
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
		cache.write(id)
	}
	return cache
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

	// global stats (a single value that stores json for the GlobalStats struct)
	_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS global_stats (
				stats BLOB
			)
		`)
	if err != nil {
		panic(err)
	}

	// check if the global stats table has any rows, if not, create one
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM global_stats").Scan(&count)
	if err != nil {
		panic(err)
	}
	if count == 0 {
		data, err := json.Marshal(GlobalStats{})
		if err != nil {
			panic(err)
		}
		_, err = db.Exec("INSERT INTO global_stats (stats) VALUES (?)", data)
		if err != nil {
			panic(err)
		}
	}

	// server stats
	_, err = db.Exec(`
			CREATE TABLE IF NOT EXISTS server_stats (
				server_id TEXT PRIMARY KEY,
				stats BLOB
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

func levelFromString(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	defer db.Close()

	slog.SetLogLoggerLevel(levelFromString(os.Getenv("X3_LOG_LEVEL")))
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
			return handleLlm(event, &model)
		})
	}
	for _, model := range model.AllModels {
		registerLlm(r, "/"+model.Command, model)
	}
	r.Command("/chat", func(e *handler.CommandEvent) error {
		return handleLlm(e, nil)
	})

	// utils
	r.Command("/whitelist", handleWhitelist)
	r.Command("/lobotomy", handleLobotomy)
	r.Command("/persona", handlePersona)
	r.Command("/stats", handleStats)

	// quote
	r.Route("/quote", func(r handler.Router) {
		r.Autocomplete("/get", handleQuoteGetAutocomplete)
		r.Command("/get", handleQuoteGet)
		r.Command("/random", handleQuoteRandom)
		r.Command("/new", handleQuoteNew)
	})

	// image
	r.Command("/boykisser", handleBoykisser)

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

func formatMsg(msg, username string, formatUsernames bool) string {
	if formatUsernames {
		return fmt.Sprintf("%s: %s", username, msg)
	}
	return msg
}

func isImageAttachment(attachment discord.Attachment) bool {
	return attachment.ContentType != nil && strings.HasPrefix(*attachment.ContentType, "image/")
}

func addImageAttachments(llmer *llm.Llmer, msg discord.Message) {
	for _, attachment := range msg.Attachments {
		if isImageAttachment(attachment) {
			slog.Debug("adding image attachment", slog.String("url", attachment.URL))
			llmer.AddImage(attachment.URL)
		}
	}
}

func isLobotomyMessage(msg discord.Message) bool {
	return msg.Interaction != nil &&
		(msg.Interaction.Name == "lobotomy" || msg.Interaction.Name == "persona")
}

var lobotomyMessagesRegex = regexp.MustCompile(`Removed last (\d+) messages from the context`)

func getLobotomyAmountFromMessage(msg discord.Message) int {
	// get a number from a string line "Removed last N messages from the context
	matches := lobotomyMessagesRegex.FindStringSubmatch(msg.Content)
	if len(matches) != 2 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return n
}

// returns whether a lobotomy was performed
func addContextMessagesIfPossible(client bot.Client, llmer *llm.Llmer, channelID, messageID snowflake.ID, formatUsernames bool, contextLen int) bool {
	messages, err := client.Rest().GetMessages(channelID, 0, messageID, 0, contextLen)
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
		if isLobotomyMessage(msg) {
			amount := getLobotomyAmountFromMessage(msg)
			llmer.Lobotomize(amount)
			slog.Debug("handled lobotomy history", slog.Int("amount", amount), slog.Int("num_messages", llmer.NumMessages()))
			continue
		}

		role := llm.RoleUser
		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
		} else if interaction, err := getMessageInteractionPrompt(msg.ID); err == nil {
			// the prompt used for this response is in the interaction cache
			llmer.AddMessage(llm.RoleUser, interaction)
		}

		llmer.AddMessage(role, formatMsg(msg.Content, msg.Author.EffectiveName(), formatUsernames))

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

func handleLlm(event *handler.CommandEvent, m *model.Model) error {
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
	persona := persona.GetPersonaByMeta(cache.PersonaMeta)
	llmer.SetPersona(persona)
	if m == nil {
		model := model.GetModelByName(cache.PersonaMeta.Model)
		m = &model
	}

	// add context if possible
	lastMessage := event.Channel().MessageChannel.LastMessageID()
	if !useCache && lastMessage != nil {
		addContextMessagesIfPossible(event.Client(), llmer, event.Channel().ID(), *lastMessage, persona.FormatUsernames, cache.ContextLength)

		// and we also want the last message in the channel
		msg, err := event.Client().Rest().GetMessage(event.Channel().ID(), *lastMessage)
		if err == nil && msg != nil {
			if isLobotomyMessage(*msg) {
				llmer.Lobotomize(getLobotomyAmountFromMessage(*msg))
			} else {
				llmer.AddMessage(llm.RoleUser, formatMsg(msg.Content, msg.Author.EffectiveName(), persona.FormatUsernames))
			}
		}
	}

	slog.Debug("handleLlm: got context messages", slog.Int("count", llmer.NumMessages()))

	// and we also want the actual slash command prompt
	llmer.AddMessage(llm.RoleUser, formatMsg(prompt, event.User().EffectiveName(), persona.FormatUsernames))

	// discord only gives us 3s to respond unless we do this (x3 is thinking...)
	event.DeferCreateMessage(ephemeral)

	response, usage, err := llmer.RequestCompletion(*m, cache.PersonaMeta.Roleplay)
	if err != nil {
		slog.Error("failed to generate response", slog.Any("err", err))
		return updateInteractionError(event, err.Error())
	}

	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage
	slog.Debug("usage stats", slog.String("usage", usage.String()))

	if len(strings.TrimSpace(response)) == 0 {
		response = "<empty response>\n-# If this is unexpected, try changing the model and/or system prompt?"
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

	if err != nil || botMessage == nil {
		return updateInteractionError(event, err.Error())
	}

	// only clients can query options passed to commands, so we cache the action interaction
	writeMessageInteractionPrompt(botMessage.ID, prompt)

	if useCache {
		// make sure we write message history in channels we cant read
		cache.Llmer = llmer
	}
	// but update cache anyway incase we got new usage stats
	if err := cache.write(event.Channel().ID()); err != nil {
		// is fine, don't sweat
		slog.Error("failed to save channel cache", slog.Any("err", err))
	}

	updateGlobalStats(usage)

	return nil
}

var containsX3Regex = regexp.MustCompile(`(?i)(^|\P{L})[Xx]3(\P{L}|$)`)

func stripX3(s string) string {
	return strings.TrimSpace(containsX3Regex.ReplaceAllString(s, ""))
}

func handleLlmInteraction(event *events.MessageCreate, eraseX3 bool) error {
	if err := event.Client().Rest().SendTyping(event.ChannelID); err != nil {
		slog.Error("failed to SendTyping", slog.Any("err", err))
	}

	cache := getChannelCache(event.ChannelID)
	persona := persona.GetPersonaByMeta(cache.PersonaMeta)

	// the interaction happened in a server or in a bot DM, so we can get the surrounding messages
	llmer := llm.NewLlmer()
	addContextMessagesIfPossible(event.Client(), llmer, event.ChannelID, event.MessageID, persona.FormatUsernames, cache.ContextLength)
	slog.Debug("interaction; added context messages", slog.Int("count", llmer.NumMessages()))

	// and we also want the event message
	var content string
	if eraseX3 {
		content = stripX3(event.Message.Content)
	} else {
		content = event.Message.Content
	}
	llmer.AddMessage(llm.RoleUser, formatMsg(content, event.Message.Author.EffectiveName(), persona.FormatUsernames))
	addImageAttachments(llmer, event.Message)

	llmer.SetPersona(persona)

	// now we generate the LLM response
	response, usage, err := llmer.RequestCompletion(model.GetModelByName(cache.PersonaMeta.Model), cache.PersonaMeta.Roleplay)
	if err != nil {
		slog.Error("failed to generate response", slog.Any("err", err))
		return err
	}

	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage

	// and send the response
	sendMessageSplits(event.Client(), event.MessageID, nil, 0, event.ChannelID, []rune(response))

	// update cache incase we got token usage stats
	cache.write(event.ChannelID)
	updateGlobalStats(usage)

	return nil
}

var containsProtogenRegex = regexp.MustCompile(`(?i)(^|\W)(protogen|протоген)($|\W)`)

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
		trimmed := strings.TrimSpace(event.Message.Content)
		if trimmed == "x3 quote" || trimmed == "x3 quote this" {
			handleQuote(event)
			return
		}
		slog.Debug("handling x3 interaction")
		if err := handleLlmInteraction(event, true); err != nil {
			slog.Error("failed to handle x3 interaction", slog.Any("err", err))
		}
		return
	}

	// check if "protogen" is mentioned
	if containsProtogenRegex.MatchString(event.Message.Content) {
		_, err := event.Client().Rest().CreateMessage(
			event.ChannelID,
			discord.MessageCreate{
				Content: "https://tenor.com/view/protogen-vrchat-hello-hi-jumping-gif-18406743932972249866",
				MessageReference: &discord.MessageReference{
					MessageID: &event.MessageID,
				},
				AllowedMentions: &discord.AllowedMentions{
					RepliedUser: false,
				},
			},
		)
		if err != nil {
			slog.Error("failed to send protogen response", slog.Any("err", err))
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
	data := event.SlashCommandInteractionData()
	ephemeral := data.Bool("ephemeral")
	amount := data.Int("amount")
	resetPersona := data.Bool("reset_persona")

	cache := getChannelCache(event.Channel().ID())

	writeCache := false
	if resetPersona {
		cache.PersonaMeta = newChannelCache().PersonaMeta
		writeCache = true
	}
	if cache.Llmer != nil {
		cache.Llmer.Lobotomize(amount)
		writeCache = true
	}
	if writeCache {
		if err := cache.write(event.Channel().ID()); err != nil {
			return sendInteractionError(event, err.Error())
		}
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	if amount > 0 {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Removed last %d messages from the context", amount),
			Flags:   flags,
		})
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
		return updateInteractionError(event, err.Error())
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

func handlePersonaInfo(event *handler.CommandEvent, ephemeral bool) error {
	cache := getChannelCache(event.Channel().ID())
	builder := discord.NewEmbedBuilder().
		SetTitle("Persona").
		SetColor(0x0085ff).
		SetDescription("Current persona settings in channel. Use `/stats` to view usage stats").
		SetFooter("x3", "https://i.imgur.com/ckpztZY.png").
		SetTimestamp(time.Now()).
		AddField("Name", cache.PersonaMeta.Name, true).
		AddField("Description", cache.PersonaMeta.Desc, true).
		AddField("Model", cache.PersonaMeta.Model, true)
	if cache.PersonaMeta.System != "" {
		builder.AddField("System prompt", cache.PersonaMeta.System, true)
	}
	builder.AddField("Roleplay", fmt.Sprintf("%v", cache.PersonaMeta.Roleplay), true).
		AddField("Context length", fmt.Sprintf("%d", cache.ContextLength), true)
	if cache.Llmer != nil {
		builder.AddField("Message cache", fmt.Sprintf("%d messages", cache.Llmer.NumMessages()), true)
	}
	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			AddEmbeds(builder.Build()).
			SetEphemeral(ephemeral).
			Build(),
	)
}

func handlePersona(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	dataPersona := data.String("persona")
	dataModel := data.String("model")
	dataSystem := data.String("system")
	dataRoleplay, hasRoleplay := data.OptBool("roleplay")
	dataContext, hasContext := data.OptInt("context")
	ephemeral := data.Bool("ephemeral")

	if dataPersona == "" && dataModel == "" && dataSystem == "" && !hasRoleplay && !hasContext {
		return handlePersonaInfo(event, ephemeral)
	}

	m := model.GetModelByName(dataModel)

	// only query whitelist if we need to
	inWhitelist := false
	if m.NeedsWhitelist || dataContext > 50 {
		inWhitelist = isInWhitelist(event.User().ID)
	}

	if m.NeedsWhitelist && !inWhitelist {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContentf("You need to be whitelisted to set the model `%s`. Try `%s`", dataModel, model.ModelGpt4oMini.Name).
				SetEphemeral(true).
				Build(),
		)
	}
	if dataContext > 50 && !inWhitelist {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContent("The maximum allowed context length for users outside the whitelist is 50").
				SetEphemeral(true).
				Build(),
		)
	}

	cache := getChannelCache(event.Channel().ID())
	personaMeta, err := persona.GetMetaByName(dataPersona)
	if err != nil {
		personaMeta = cache.PersonaMeta
	}

	// update persona meta in channel cache
	prevMeta := cache.PersonaMeta
	cache.PersonaMeta = personaMeta
	if dataPersona != "" {
		cache.PersonaMeta.Name = dataPersona
	}
	if dataSystem != "" {
		cache.PersonaMeta.System = dataSystem
	}
	if dataModel != "" {
		cache.PersonaMeta.Model = dataModel
	}
	cache.PersonaMeta.Roleplay = dataRoleplay
	prevContextLen := cache.ContextLength
	if hasContext {
		if dataContext < 0 {
			dataContext = defaultContextMessages
		}
		cache.ContextLength = dataContext
	}

	if err := cache.write(event.Channel().ID()); err != nil {
		return sendInteractionError(event, err.Error())
	}

	var sb strings.Builder
	sb.WriteString("Updated persona for this channel")
	didWhat := []string{}
	if cache.PersonaMeta.Name != prevMeta.Name {
		didWhat = append(didWhat, fmt.Sprintf("set persona to `%s`", cache.PersonaMeta.Name))
	}
	if cache.PersonaMeta.Model != prevMeta.Model {
		didWhat = append(didWhat, fmt.Sprintf("set model to `%s`", cache.PersonaMeta.Model))
	}
	if cache.PersonaMeta.System != prevMeta.System {
		didWhat = append(didWhat, "updated the system prompt")
	}
	if cache.PersonaMeta.Roleplay != prevMeta.Roleplay {
		if cache.PersonaMeta.Roleplay {
			didWhat = append(didWhat, "enabled roleplay mode")
		} else {
			didWhat = append(didWhat, "disabled roleplay mode")
		}
	}
	if cache.ContextLength != prevContextLen {
		didWhat = append(didWhat, fmt.Sprintf("updated context length %d → %d", prevContextLen, cache.ContextLength))
	}

	if len(didWhat) > 0 {
		sb.WriteString(" (")
		sb.WriteString(strings.Join(didWhat, ", "))
		sb.WriteString(")")
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetContent(sb.String()).
			SetEphemeral(ephemeral).
			Build(),
	)
}

func formatUsageStrings(usage llm.Usage) (string, string, string) {
	prompt := "no data"
	response := "no data"
	total := "no data"
	if usage.PromptTokens > 0 {
		prompt = humanize.Comma(int64(usage.PromptTokens))
	}
	if usage.ResponseTokens > 0 {
		response = humanize.Comma(int64(usage.ResponseTokens))
	}
	if usage.TotalTokens > 0 {
		total = humanize.Comma(int64(usage.TotalTokens))
	}
	return prompt, response, total
}

func handleStats(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	ephemeral, ok := data.OptBool("ephemeral")
	if !ok {
		// ephemeral is true by default for this command
		ephemeral = true
	}

	stats, err := getGlobalStats()
	if err != nil {
		slog.Error("failed to get global stats", slog.Any("err", err))
		return sendInteractionError(event, err.Error())
	}
	cache := getChannelCache(event.Channel().ID())

	prompt, response, total := formatUsageStrings(cache.Usage)
	promptLast, responseLast, totalLast := formatUsageStrings(cache.LastUsage)
	promptTotal, responseTotal, totalTotal := formatUsageStrings(stats.Usage)
	upSince := "since " + humanize.Time(startTime)

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetTitle("Stats").
					SetColor(0x0085ff).
					SetDescription("Per-channel and global bot stats").
					SetFooter("x3", "https://i.imgur.com/ckpztZY.png").
					SetTimestamp(time.Now()).
					AddField("Prompt tokens (channel)", prompt, true).
					AddField("Response tokens (channel)", response, true).
					AddField("Total tokens (channel)", total, true).
					AddField("Prompt tokens (last)", promptLast, true).
					AddField("Response tokens (last)", responseLast, true).
					AddField("Total tokens (last)", totalLast, true).
					AddField("Prompt tokens (global)", promptTotal, true).
					AddField("Response tokens (global)", responseTotal, true).
					AddField("Total tokens (global)", totalTotal, true).
					AddField("Bot uptime", upSince, true).
					AddField("Messages processed", humanize.Comma(int64(stats.MessageCount)), true).
					Build(),
			).
			SetEphemeral(ephemeral).
			Build(),
	)
}

func toTitle(str string) string {
	if len(str) == 0 {
		return str
	}
	runes := []rune(str)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func sendPrettyError(client bot.Client, msg string, channelID, messageID snowflake.ID) error {
	_, err := client.Rest().CreateMessage(
		channelID,
		discord.NewMessageCreateBuilder().
			SetMessageReferenceByID(messageID).
			SetAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0xf54242).
					SetTitle("❌ Error").
					SetFooter("x3", "https://i.imgur.com/hCF06SC.png").
					SetTimestamp(time.Now()).
					SetDescription(toTitle(msg)).
					Build(),
			).
			Build(),
	)
	return err
}

func sendInteractionError(event *handler.CommandEvent, msg string) error {
	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0xf54242).
					SetTitle("❌ Error").
					SetFooter("x3", "https://i.imgur.com/hCF06SC.png").
					SetTimestamp(time.Now()).
					SetDescription(toTitle(msg)).
					Build(),
			).
			Build(),
	)
}

func updateInteractionError(event *handler.CommandEvent, msg string) error {
	_, err := event.UpdateInteractionResponse(
		discord.NewMessageUpdateBuilder().
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0xf54242).
					SetTitle("❌ Error").
					SetFooter("x3", "https://i.imgur.com/hCF06SC.png").
					SetTimestamp(time.Now()).
					SetDescription(toTitle(msg)).
					Build(),
			).
			Build(),
	)
	return err
}

func sendQuote(event *handler.CommandEvent, client bot.Client, channelID, messageID snowflake.ID, quote Quote, nr int) error {
	text := fmt.Sprintf(
		"“%s”\n\n\\- <@%d> in <#%d>, quoted by <@%d>",
		quote.Text,
		quote.AuthorID,
		quote.Channel,
		quote.Quoter,
	)

	builder := discord.NewEmbedBuilder().
		SetColor(0xFFD700).
		SetTitle(fmt.Sprintf("📜 Quote #%d", nr)).
		SetTimestamp(quote.Timestamp).
		SetDescription(text)
	if quote.AttachmentURL != "" {
		builder.SetImage(quote.AttachmentURL)
	}

	if channelID != 0 && messageID != 0 {
		_, err := client.Rest().CreateMessage(
			channelID,
			discord.NewMessageCreateBuilder().
				SetMessageReferenceByID(messageID).
				SetAllowedMentions(&discord.AllowedMentions{
					RepliedUser: false,
				}).
				AddEmbeds(builder.Build()).
				Build(),
		)
		return err
	} else if event != nil {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				AddEmbeds(builder.Build()).
				SetEphemeral(event.SlashCommandInteractionData().Bool("ephemeral")).
				Build(),
		)
	}
	return nil
}

func handleQuote(event *events.MessageCreate) error {
	if event.Message.ReferencedMessage == nil {
		return sendPrettyError(
			event.Client(),
			"You must reply to a message to quote it",
			event.ChannelID,
			event.MessageID,
		)
	}

	var serverID snowflake.ID
	if event.GuildID != nil {
		serverID = *event.GuildID
	} else {
		serverID = event.ChannelID // in dm, probably
	}

	server, err := getServerStats(serverID)
	if err != nil {
		slog.Error("failed to get server stats", slog.Any("err", err))
		return sendPrettyError(event.Client(), err.Error(), event.ChannelID, event.MessageID)
	}

	var attachmentURL string
	if len(event.Message.ReferencedMessage.Attachments) > 0 {
		attachmentURL = event.Message.ReferencedMessage.Attachments[0].URL
	}

	quote := Quote{
		MessageID:     event.Message.ReferencedMessage.ID,
		Quoter:        event.Message.Author.ID,
		AuthorID:      event.Message.ReferencedMessage.Author.ID,
		AuthorUser:    event.Message.ReferencedMessage.Author.Username,
		Channel:       event.Message.ReferencedMessage.ChannelID,
		Text:          event.Message.ReferencedMessage.Content,
		AttachmentURL: attachmentURL,
		Timestamp:     event.Message.ReferencedMessage.CreatedAt,
	}

	if exists, nr := server.QuoteExists(quote); exists {
		return sendPrettyError(
			event.Client(),
			fmt.Sprintf("Quote #%d already exists", nr+1),
			event.ChannelID,
			event.MessageID,
		)
	}

	nr := server.AddQuote(quote)

	if err := server.write(serverID); err != nil {
		slog.Error("failed to save server stats", slog.Any("err", err))
		return sendPrettyError(event.Client(), err.Error(), event.ChannelID, event.MessageID)
	}

	return sendQuote(
		nil,
		event.Client(),
		event.ChannelID,
		event.MessageID,
		server.Quotes[len(server.Quotes)-1],
		nr,
	)
}

func ellipsisTrim(s string, length int) string {
	r := []rune(s)
	if len(r) > length {
		return string(r[:length-1]) + "…"
	}
	return s
}

func handleQuoteGetAutocomplete(event *handler.AutocompleteEvent) error {
	var serverID snowflake.ID
	if event.GuildID() != nil {
		serverID = *event.GuildID()
	} else {
		serverID = event.Channel().ID() // in dm, probably
	}

	server, err := getServerStats(serverID)
	if err != nil {
		slog.Error("failed to get server stats", slog.Any("err", err))
		return err
	}

	name := event.Data.String("name")
	slog.Debug("handling autocomplete", slog.String("name", name))

	var names []string
	for _, quote := range server.Quotes {
		names = append(names, fmt.Sprintf("%s %s", quote.Text, quote.AuthorUser))
	}

	matches := fuzzy.RankFindNormalizedFold(name, names)
	sort.Sort(matches)

	var choices []discord.AutocompleteChoice
	for _, match := range matches {
		if len(choices) >= 25 {
			break
		}
		quote := server.Quotes[match.OriginalIndex]
		res := fmt.Sprintf("%d: %s (%s)", match.OriginalIndex+1, quote.Text, quote.AuthorUser)
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  ellipsisTrim(res, 100),
			Value: fmt.Sprintf("%d", match.OriginalIndex+1),
		})
	}

	return event.AutocompleteResult(choices)
}

func getServerFromEvent(event *handler.CommandEvent) (ServerStats, snowflake.ID, error) {
	var serverID snowflake.ID
	if event.GuildID() != nil {
		serverID = *event.GuildID()
	} else {
		serverID = event.Channel().ID() // in dm, probably
	}

	server, err := getServerStats(serverID)
	if err != nil {
		slog.Error("failed to get server stats", slog.Any("err", err))
	}
	return server, serverID, err
}

func handleQuoteGet(event *handler.CommandEvent) error {
	idx, err := strconv.Atoi(event.SlashCommandInteractionData().String("name"))
	if err != nil {
		return err
	}

	// 1-indexed
	idx--

	server, _, err := getServerFromEvent(event)
	if err != nil {
		return err
	}

	if idx > len(server.Quotes)-1 || idx < 0 {
		return sendInteractionError(event, fmt.Sprintf("quote #%d does not exist", idx+1))
	}

	return sendQuote(event, event.Client(), 0, 0, server.Quotes[idx], idx+1)
}

func handleQuoteRandom(event *handler.CommandEvent) error {
	server, _, err := getServerFromEvent(event)
	if err != nil {
		return err
	}

	if len(server.Quotes) == 0 {
		return sendInteractionError(event, "no quotes in this server")
	}

	nr := rand.Intn(len(server.Quotes))
	return sendQuote(event, event.Client(), 0, 0, server.Quotes[nr], nr+1)
}

func handleQuoteNew(event *handler.CommandEvent) error {
	text := event.SlashCommandInteractionData().String("text")

	if len(strings.TrimSpace(text)) == 0 {
		sendInteractionError(event, "can't make a quote with no text")
		return nil
	}

	server, serverID, err := getServerFromEvent(event)
	if err != nil {
		return err
	}

	quote := Quote{
		Quoter:     event.User().ID,
		AuthorID:   event.User().ID,
		AuthorUser: event.User().EffectiveName(),
		Channel:    event.Channel().ID(),
		Text:       text,
		Timestamp:  event.CreatedAt(),
	}

	if exists, nr := server.QuoteExists(quote); exists {
		return sendInteractionError(event, fmt.Sprintf("quote #%d already exists", nr+1))
	}

	nr := server.AddQuote(quote)

	if err := server.write(serverID); err != nil {
		slog.Error("failed to save server stats", slog.Any("err", err))
		return sendInteractionError(event, err.Error())
	}

	return sendQuote(event, event.Client(), 0, 0, quote, nr)
}
