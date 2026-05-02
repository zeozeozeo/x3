package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
)

const chatArchiveVersion = 1

type chatArchiveMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Author    string    `json:"author,omitempty"`
	MessageID string    `json:"message_id,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Images    []string  `json:"images,omitempty"`
}

type chatArchive struct {
	Version    int                  `json:"version"`
	ExportedAt time.Time            `json:"exported_at"`
	ChannelID  string               `json:"channel_id,omitempty"`
	GuildID    string               `json:"guild_id,omitempty"`
	Messages   []chatArchiveMessage `json:"messages"`
	Summaries  []persona.Summary    `json:"summaries,omitempty"`
	Context    []string             `json:"context,omitempty"`
}

type chatArchiveBrowserSession struct {
	UserID   snowflake.ID
	Archive  chatArchive
	Page     int
	PageSize int
}

var chatArchiveBrowserSessions sync.Map // string(message id) -> *chatArchiveBrowserSession

var ChatArchiveCommand = discord.SlashCommandCreate{
	Name:        "chatlog",
	Description: "Export or import this chat's LLM context",
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
			Name:        "export",
			Description: "Export visible chat context to a JSON archive",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "import",
			Description: "Import a JSON chat archive into cached context",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionAttachment{
					Name:        "file",
					Description: "JSON archive from /chatlog export",
					Required:    true,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
				},
			},
		},
	},
}

func HandleChatArchive(event *handler.CommandEvent) error {
	subcommand := event.SlashCommandInteractionData().SubCommandName
	if subcommand == nil {
		return sendInteractionError(event, "missing subcommand", true)
	}

	switch *subcommand {
	case "export":
		return handleChatArchiveExport(event)
	case "import":
		return handleChatArchiveImport(event)
	default:
		return sendInteractionError(event, "unknown subcommand", true)
	}
}

func handleChatArchiveExport(event *handler.CommandEvent) error {
	ephemeral := event.SlashCommandInteractionData().Bool("ephemeral")
	if err := event.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	archive, err := buildChatArchive(event)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	data, err := marshalChatArchive(archive)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	_, err = event.UpdateInteractionResponse(
		discord.NewMessageUpdate().
			WithContentf("Exported %s.", pluralize(len(archive.Messages), "message")).
			AddFiles(newChatArchiveFile(data)),
	)
	return err
}

func marshalChatArchive(archive chatArchive) ([]byte, error) {
	return json.MarshalIndent(archive, "", "  ")
}

func newChatArchiveFile(data []byte) *discord.File {
	return &discord.File{
		Name:   "x3-chatlog.json",
		Reader: bytes.NewReader(data),
	}
}

func handleChatArchiveImport(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	ephemeral := data.Bool("ephemeral")
	attachment := data.Attachment("file")
	if attachment.URL == "" {
		return sendInteractionError(event, "missing archive attachment", true)
	}

	if err := event.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	body, err := downloadChatArchiveAttachment(attachment)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	var archive chatArchive
	if err := json.Unmarshal(body, &archive); err != nil {
		return updateInteractionError(event, "invalid archive JSON: "+err.Error())
	}
	if archive.Version != chatArchiveVersion {
		return updateInteractionError(event, fmt.Sprintf("unsupported archive version %d", archive.Version))
	}

	importedMessages := archive.toLLMMessages(event.Channel().ID())
	cache := db.GetChannelCache(event.Channel().ID())
	cache.Llmer = llm.NewLlmer(event.Channel().ID())
	cache.Llmer.Messages = importedMessages
	cache.ImportedHistory = &db.ImportedChatHistory{Messages: append([]llm.Message(nil), importedMessages...)}
	cache.Summaries = append([]persona.Summary(nil), archive.Summaries...)
	cache.Context = append([]string(nil), archive.Context...)
	cache.UpdateInteractionTime()
	if err := cache.Write(event.Channel().ID()); err != nil {
		return updateInteractionError(event, err.Error())
	}

	builder := chatArchiveBrowserMessage(archive, 0, 5)
	msg, err := event.UpdateInteractionResponse(builder.
		WithContentf("Imported %s into cached context.", pluralize(len(importedMessages), "message")),
	)
	if err != nil {
		return err
	}

	chatArchiveBrowserSessions.Store(msg.ID.String(), &chatArchiveBrowserSession{
		UserID:   event.User().ID,
		Archive:  archive,
		Page:     0,
		PageSize: 5,
	})
	return nil
}

func downloadChatArchiveAttachment(attachment discord.Attachment) ([]byte, error) {
	const maxArchiveSize = 2 * 1024 * 1024
	if attachment.Size > maxArchiveSize {
		return nil, fmt.Errorf("archive is too large; max size is %d bytes", maxArchiveSize)
	}

	resp, err := http.Get(attachment.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to download archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download archive: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read archive: %w", err)
	}
	if len(body) > maxArchiveSize {
		return nil, fmt.Errorf("archive is too large; max size is %d bytes", maxArchiveSize)
	}
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("archive is not valid UTF-8")
	}
	return body, nil
}

func buildChatArchive(event *handler.CommandEvent) (chatArchive, error) {
	cache := db.GetChannelCache(event.Channel().ID())
	if cache.ImportedHistory != nil {
		return buildChatArchiveFromLLMMessages(event.Channel().ID(), event.GuildID(), cacheHistoryMessages(cache), cache.Summaries, cache.Context), nil
	}

	rawMessages, err := fetchMessagesForArchive(event)
	if err != nil {
		if shouldUseCachedChatArchive(event, cache) {
			return buildChatArchiveFromLLMMessages(event.Channel().ID(), event.GuildID(), cacheHistoryMessages(cache), cache.Summaries, cache.Context), nil
		}
		return chatArchive{}, err
	}

	archive := chatArchive{
		Version:    chatArchiveVersion,
		ExportedAt: time.Now().UTC(),
		ChannelID:  event.Channel().ID().String(),
		Messages:   replayMessagesForArchive(rawMessages, event.Client().ID()),
		Summaries:  append([]persona.Summary(nil), cache.Summaries...),
		Context:    append([]string(nil), cache.Context...),
	}
	if guildID := event.GuildID(); guildID != nil {
		archive.GuildID = guildID.String()
	}
	if shouldUseCachedChatArchive(event, cache) {
		cachedArchive := buildChatArchiveFromLLMMessages(event.Channel().ID(), event.GuildID(), cacheHistoryMessages(cache), cache.Summaries, cache.Context)
		if len(cachedArchive.Messages) > len(archive.Messages) {
			return cachedArchive, nil
		}
	}
	return archive, nil
}

func shouldUseCachedChatArchive(event *handler.CommandEvent, cache *db.ChannelCache) bool {
	if cache.ImportedHistory != nil {
		return true
	}
	if cache.Llmer == nil {
		return false
	}
	return event.GuildID() == nil || (event.AppPermissions() != nil && !event.AppPermissions().Has(discord.PermissionReadMessageHistory))
}

func cacheHistoryMessages(cache *db.ChannelCache) []llm.Message {
	if cache.ImportedHistory != nil {
		return cache.ImportedHistory.Messages
	}
	if cache.Llmer != nil {
		return cache.Llmer.Messages
	}
	return nil
}

func buildChatArchiveFromLLMMessages(channelID snowflake.ID, guildID *snowflake.ID, messages []llm.Message, summaries []persona.Summary, contextItems []string) chatArchive {
	archive := chatArchive{
		Version:    chatArchiveVersion,
		ExportedAt: time.Now().UTC(),
		ChannelID:  channelID.String(),
		Messages:   archiveMessagesFromLLM(messages),
		Summaries:  append([]persona.Summary(nil), summaries...),
		Context:    append([]string(nil), contextItems...),
	}
	if guildID != nil {
		archive.GuildID = guildID.String()
	}
	return archive
}

func archiveMessagesFromLLM(messages []llm.Message) []chatArchiveMessage {
	out := make([]chatArchiveMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == llm.RoleSystem || strings.TrimSpace(msg.Content) == "" && len(msg.Images) == 0 {
			continue
		}
		out = append(out, chatArchiveMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Author:    firstNonEmpty(msg.Author, msg.Role),
			MessageID: msg.MessageID,
			Timestamp: msg.Timestamp,
			Images:    append([]string(nil), msg.Images...),
		})
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func fetchMessagesForArchive(event *handler.CommandEvent) ([]discord.Message, error) {
	channelID := event.Channel().ID()
	var messages []discord.Message
	var before snowflake.ID

	for {
		batch, err := event.Client().Rest.GetMessages(channelID, 0, before, 0, 100)
		if err != nil {
			if len(messages) > 0 {
				return messages, nil
			}
			return nil, fmt.Errorf("failed to fetch channel messages: %w", err)
		}
		if len(batch) == 0 {
			break
		}

		stop := false
		for _, msg := range batch {
			messages = append(messages, msg)
			if isLobotomyMessage(msg) && strings.TrimSpace(msg.Content) != "" && getLobotomyAmountFromMessage(msg) == 0 {
				stop = true
				break
			}
		}
		if stop || len(batch) < 100 {
			break
		}
		before = batch[len(batch)-1].ID
	}

	return messages, nil
}

func replayMessagesForArchive(messages []discord.Message, botID snowflake.ID) []chatArchiveMessage {
	var archiveMessages []chatArchiveMessage
	var lastAssistantID snowflake.ID

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.ID == 0 || msg.Content == "" && len(msg.Attachments) == 0 {
			continue
		}

		if msg.Author.ID == botID {
			if isLobotomyMessage(msg) {
				if strings.TrimSpace(msg.Content) == "" {
					continue
				}
				amount := getLobotomyAmountFromMessage(msg)
				if amount > 0 {
					if amount >= len(archiveMessages) {
						archiveMessages = nil
					} else {
						archiveMessages = archiveMessages[:len(archiveMessages)-amount]
					}
				} else {
					archiveMessages = nil
				}
				continue
			}
			if isChatlogMessage(msg) || isCardMessage(msg) || isNarrationMessage(msg) {
				continue
			}

			content := cleanupCites(getMessageContent(msg))
			if strings.HasPrefix(content, latexAPI) {
				content = fromLatexAPI(content)
			}
			content = strings.TrimSuffix(content, interactionReminder)
			content = strings.ReplaceAll(content, "\u200B", "")
			if strings.TrimSpace(content) == "" {
				continue
			}
			lastAssistantID = msg.ID
			archiveMessages = appendArchiveMessage(archiveMessages, chatArchiveMessage{
				Role:      llm.RoleAssistant,
				Content:   content,
				Author:    msg.Author.EffectiveName(),
				MessageID: msg.ID.String(),
				Timestamp: msg.CreatedAt,
			})
			continue
		}

		content := getMessageContent(msg)
		reference := msg.ReferencedMessage
		if reference != nil && reference.ID == lastAssistantID {
			reference = nil
		}
		content = formatMsg(content, msg.Author.EffectiveName(), reference)
		if strings.TrimSpace(content) == "" && len(msg.Attachments) == 0 {
			continue
		}

		archiveMessages = appendArchiveMessage(archiveMessages, chatArchiveMessage{
			Role:      llm.RoleUser,
			Content:   content,
			Author:    msg.Author.EffectiveName(),
			MessageID: msg.ID.String(),
			Timestamp: msg.CreatedAt,
			Images:    imageAttachmentURLs(msg.Attachments),
		})
	}

	return archiveMessages
}

func appendArchiveMessage(messages []chatArchiveMessage, message chatArchiveMessage) []chatArchiveMessage {
	if len(messages) > 0 && message.Role == llm.RoleAssistant && messages[len(messages)-1].Role == llm.RoleAssistant {
		if !strings.HasSuffix(messages[len(messages)-1].Content, "\n") {
			messages[len(messages)-1].Content += "\n"
		}
		messages[len(messages)-1].Content += message.Content
		return messages
	}
	return append(messages, message)
}

func imageAttachmentURLs(attachments []discord.Attachment) []string {
	var urls []string
	for _, attachment := range attachments {
		if isImageAttachment(attachment) {
			urls = append(urls, attachment.URL)
		}
	}
	return urls
}

func (a chatArchive) toLLMMessages(channelID snowflake.ID) []llm.Message {
	messages := make([]llm.Message, 0, len(a.Messages))
	for _, msg := range a.Messages {
		if strings.TrimSpace(msg.Content) == "" && len(msg.Images) == 0 {
			continue
		}
		messages = append(messages, llm.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Images:    append([]string(nil), msg.Images...),
			Author:    msg.Author,
			Timestamp: msg.Timestamp,
			MessageID: msg.MessageID,
		})
	}
	return messages
}

func chatArchiveBrowserMessage(archive chatArchive, page, pageSize int) discord.MessageUpdate {
	total := len(archive.Messages)
	maxPage := max((total+pageSize-1)/pageSize-1, 0)
	if page < 0 {
		page = 0
	}
	if page > maxPage {
		page = maxPage
	}
	start := page * pageSize
	end := min(start+pageSize, total)

	var b strings.Builder
	if total == 0 {
		b.WriteString("Archive contains no messages.")
	} else {
		for i := start; i < end; i++ {
			msg := archive.Messages[i]
			author := msg.Author
			if author == "" {
				author = msg.Role
			}
			fmt.Fprintf(&b, "**%d. %s**", i+1, author)
			if !msg.Timestamp.IsZero() {
				fmt.Fprintf(&b, " <t:%d:f>", msg.Timestamp.Unix())
			}
			b.WriteString("\n")
			b.WriteString(ellipsisTrim(msg.Content, 450))
			if len(msg.Images) > 0 {
				fmt.Fprintf(&b, "\n-# %s", pluralize(len(msg.Images), "image"))
			}
			if i < end-1 {
				b.WriteString("\n\n")
			}
		}
	}

	embed := discord.NewEmbed().
		WithColor(0x0085ff).
		WithTitle("Imported chat archive").
		WithDescription(ellipsisTrim(b.String(), 4096)).
		WithFooter(fmt.Sprintf("Page %d/%d", page+1, maxPage+1), x3Icon)

	return discord.NewMessageUpdate().
		AddEmbeds(embed).
		WithComponents(discord.NewActionRow(
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "⏮️"},
				CustomID: "/chatlog/first",
				Disabled: page <= 0,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "⬅️"},
				CustomID: "/chatlog/prev",
				Disabled: page <= 0,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "➡️"},
				CustomID: "/chatlog/next",
				Disabled: page >= maxPage,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "⏭️"},
				CustomID: "/chatlog/last",
				Disabled: page >= maxPage,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "❌"},
				CustomID: "/chatlog/close",
			},
		))
}

func HandleChatArchiveBrowser(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	sessionAny, ok := chatArchiveBrowserSessions.Load(event.Message.ID.String())
	if !ok {
		return sendInteractionErrorComponent(event, "archive browser expired", true)
	}
	session, ok := sessionAny.(*chatArchiveBrowserSession)
	if !ok {
		chatArchiveBrowserSessions.Delete(event.Message.ID.String())
		return sendInteractionErrorComponent(event, "archive browser expired", true)
	}
	if event.User().ID != session.UserID {
		return event.CreateMessage(discord.NewMessageCreate().
			WithContent("Only the importing user can use this browser.").
			WithEphemeral(true))
	}

	_, action, ok := strings.Cut(data.CustomID()[1:], "/")
	if !ok {
		return fmt.Errorf("invalid archive browser custom id: %s", data.CustomID())
	}
	switch action {
	case "close":
		chatArchiveBrowserSessions.Delete(event.Message.ID.String())
		event.DeferUpdateMessage()
		_, err := event.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithContent("").
			ClearEmbeds().
			ClearComponents())
		return err
	case "first":
		session.Page = 0
	case "prev":
		session.Page--
	case "next":
		session.Page++
	case "last":
		session.Page = max((len(session.Archive.Messages)+session.PageSize-1)/session.PageSize-1, 0)
	}
	totalPages := max((len(session.Archive.Messages)+session.PageSize-1)/session.PageSize, 1)
	if session.Page < 0 {
		session.Page = 0
	}
	if session.Page >= totalPages {
		session.Page = totalPages - 1
	}

	event.DeferUpdateMessage()
	_, err := event.UpdateInteractionResponse(chatArchiveBrowserMessage(session.Archive, session.Page, session.PageSize))
	return err
}
