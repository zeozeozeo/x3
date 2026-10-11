package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/worldmem"
)

var worldTagRegexp = regexp.MustCompile(`(?is)<world>(.*?)</world>`)

// worldMemUpdatedFooter marks responses that recorded new world facts. It is
// display-only: every history read-back path strips it.
const worldMemUpdatedFooter = "-# Updated world memory"

const worldImportMaxSize = 2 * 1024 * 1024

const worldBrowserPageSize = 5

// extractWorldTags strips hidden <world> blocks from a response and returns
// the display text plus the raw fact contents.
func extractWorldTags(response string) (string, []string) {
	matches := worldTagRegexp.FindAllStringSubmatch(response, -1)
	worlds := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if w := strings.TrimSpace(match[1]); w != "" {
			worlds = append(worlds, w)
		}
	}

	response = worldTagRegexp.ReplaceAllString(response, "")
	response = strings.TrimSpace(response)
	return response, worlds
}

// handleWorldTags extracts <world> blocks from an LLM response, strips them
// from the display text, updates the cached assistant message via setContent,
// and stores the facts in the background. channelKey scopes storage to the
// channel/room so memories never leak across channels. When facts were found,
// the visible footer "-# Updated world memory" is appended to the display
// text (history read-back paths strip it again).
func handleWorldTags(channelKey, response string, setContent func(string)) string {
	display, worlds := extractWorldTags(response)
	if display == response && len(worlds) == 0 {
		return response
	}
	if setContent != nil {
		setContent(display)
	}
	if len(worlds) > 0 {
		key := channelKey
		facts := append([]string(nil), worlds...)
		go worldmem.StoreFacts(key, facts)
		slog.Info("worldmem facts queued", "channel", key, "found", len(facts))
		if strings.TrimSpace(display) != "" {
			display += "\n" + worldMemUpdatedFooter
		}
	}
	return display
}

// stripWorldMemFooter removes display-only world-memory footer lines so they
// never reach LLM context on history read-back.
func stripWorldMemFooter(s string) string {
	if !strings.Contains(s, worldMemUpdatedFooter) {
		return s
	}
	var kept []string
	for line := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(line) == worldMemUpdatedFooter {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// worldEntriesForPrompt retrieves world-bible facts for prompt injection.
// Returns nil when disabled, when there is nothing stored (cheap local count
// check first, no embedding call), or when the query is empty.
func worldEntriesForPrompt(channelKey, query string, disabled bool) []string {
	if disabled || strings.TrimSpace(channelKey) == "" || strings.TrimSpace(query) == "" {
		return nil
	}
	n, err := db.CountWorldEntries(channelKey)
	if err != nil {
		slog.Warn("worldmem count failed", "channel", channelKey, "err", err)
		return nil
	}
	if n == 0 {
		return nil
	}
	results := worldmem.Retrieve(context.Background(), channelKey, query)
	if len(results) == 0 {
		return nil
	}
	out := make([]string, 0, len(results))
	for _, r := range results {
		if strings.TrimSpace(r.Text) != "" {
			out = append(out, r.Text)
		}
	}
	return out
}

var WorldmemCommand = discord.SlashCommandCreate{
	Name:        "worldmem",
	Description: "Manage this channel's persistent world memory (world bible)",
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
			Name:        "on",
			Description: "Enable world-bible retrieval in this channel",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "off",
			Description: "Disable world-bible retrieval (facts stay stored)",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "clear",
			Description: "Delete all world facts stored for this channel",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "export",
			Description: "Export this channel's world facts as JSON",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "import",
			Description: "Import world facts from a /worldmem export file",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionAttachment{
					Name:        "file",
					Description: "JSON file from /worldmem export",
					Required:    true,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "explore",
			Description: "Browse this channel's stored world facts",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
				},
			},
		},
	},
}

func HandleWorldmem(e *handler.CommandEvent) error {
	subcommand := e.SlashCommandInteractionData().SubCommandName
	if subcommand == nil {
		return sendInteractionError(e, "missing subcommand", true)
	}
	channelID := e.Channel().ID()
	channelKey := channelID.String()

	switch *subcommand {
	case "on":
		cache := db.GetChannelCache(channelID)
		cache.WorldMemDisabled = false
		if err := cache.Write(channelID); err != nil {
			return sendInteractionError(e, "Failed to save setting: "+err.Error(), true)
		}
		return sendInteractionOk(e, "World memory on", "World-bible retrieval is now enabled in this channel.", false)

	case "off":
		cache := db.GetChannelCache(channelID)
		cache.WorldMemDisabled = true
		if err := cache.Write(channelID); err != nil {
			return sendInteractionError(e, "Failed to save setting: "+err.Error(), true)
		}
		return sendInteractionOk(e, "World memory off", "World-bible retrieval is disabled. Stored facts are kept and new facts are still recorded.", false)

	case "clear":
		n, err := db.ClearWorldEntries(channelKey)
		if err != nil {
			return sendInteractionError(e, "Failed to clear world memory: "+err.Error(), true)
		}
		return sendInteractionOk(e, "World memory cleared", fmt.Sprintf("Deleted %s from this channel's world memory.", pluralize(n, "fact")), false)

	case "export":
		return handleWorldmemExport(e, channelKey)

	case "import":
		return handleWorldmemImport(e, channelKey)

	case "explore":
		return handleWorldmemExplore(e, channelKey)
	}

	return sendInteractionError(e, "unknown subcommand", true)
}

type worldmemBrowserSession struct {
	UserID    snowflake.ID
	ChannelID string
	Entries   []db.WorldEntry
	Page      int
	PageSize  int
}

var worldmemBrowserSessions sync.Map // string(message id) -> *worldmemBrowserSession

func worldmemBrowserMessage(entries []db.WorldEntry, page, pageSize int) discord.MessageUpdate {
	total := len(entries)
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
		b.WriteString("No world facts stored for this channel yet.")
	} else {
		for i := start; i < end; i++ {
			fmt.Fprintf(&b, "**%d.** %s", i+1, ellipsisTrim(entries[i].Text, 450))
			if i < end-1 {
				b.WriteString("\n\n")
			}
		}
	}

	embed := discord.NewEmbed().
		WithColor(0x0085ff).
		WithTitle("World memory").
		WithDescription(ellipsisTrim(b.String(), 4096)).
		WithFooter(fmt.Sprintf("Page %d/%d • %s", page+1, maxPage+1, pluralize(total, "fact")), x3Icon)

	return discord.NewMessageUpdate().
		AddEmbeds(embed).
		WithComponents(discord.NewActionRow(
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "⏮️"},
				CustomID: "/worldmem/first",
				Disabled: page <= 0,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "⬅️"},
				CustomID: "/worldmem/prev",
				Disabled: page <= 0,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "➡️"},
				CustomID: "/worldmem/next",
				Disabled: page >= maxPage,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "⏭️"},
				CustomID: "/worldmem/last",
				Disabled: page >= maxPage,
			},
			discord.ButtonComponent{
				Style:    discord.ButtonStyleSecondary,
				Emoji:    &discord.ComponentEmoji{Name: "❌"},
				CustomID: "/worldmem/close",
			},
		))
}

// sendWorldmemBrowser answers the deferred interaction with a paged fact
// browser and registers the button session. content heads the message.
func sendWorldmemBrowser(e *handler.CommandEvent, channelKey, content string) error {
	entries, err := db.GetWorldEntries(channelKey)
	if err != nil {
		return updateInteractionError(e, err.Error())
	}
	builder := worldmemBrowserMessage(entries, 0, worldBrowserPageSize)
	if content != "" {
		builder = builder.WithContent(content)
	}
	msg, err := e.UpdateInteractionResponse(builder)
	if err != nil {
		return err
	}
	if msg != nil {
		worldmemBrowserSessions.Store(msg.ID.String(), &worldmemBrowserSession{
			UserID:    e.User().ID,
			ChannelID: channelKey,
			Entries:   entries,
			PageSize:  worldBrowserPageSize,
		})
	}
	return nil
}

func handleWorldmemExplore(e *handler.CommandEvent, channelKey string) error {
	ephemeral := e.SlashCommandInteractionData().Bool("ephemeral")
	if err := e.DeferCreateMessage(ephemeral); err != nil {
		return err
	}
	return sendWorldmemBrowser(e, channelKey, "")
}

func HandleWorldmemBrowser(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	sessionAny, ok := worldmemBrowserSessions.Load(event.Message.ID.String())
	if !ok {
		return sendInteractionErrorComponent(event, "world memory browser expired", true)
	}
	session, ok := sessionAny.(*worldmemBrowserSession)
	if !ok {
		worldmemBrowserSessions.Delete(event.Message.ID.String())
		return sendInteractionErrorComponent(event, "world memory browser expired", true)
	}
	if event.User().ID != session.UserID {
		return event.CreateMessage(discord.NewMessageCreate().
			WithContent("Only the exploring user can use this browser.").
			WithEphemeral(true))
	}

	_, action, ok := strings.Cut(data.CustomID()[1:], "/")
	if !ok {
		return fmt.Errorf("invalid world browser custom id: %s", data.CustomID())
	}
	switch action {
	case "close":
		worldmemBrowserSessions.Delete(event.Message.ID.String())
		event.DeferUpdateMessage()
		return event.DeleteInteractionResponse()
	case "first":
		session.Page = 0
	case "prev":
		session.Page--
	case "next":
		session.Page++
	case "last":
		session.Page = max((len(session.Entries)+session.PageSize-1)/session.PageSize-1, 0)
	}
	totalPages := max((len(session.Entries)+session.PageSize-1)/session.PageSize, 1)
	if session.Page < 0 {
		session.Page = 0
	}
	if session.Page >= totalPages {
		session.Page = totalPages - 1
	}

	event.DeferUpdateMessage()
	_, err := event.UpdateInteractionResponse(worldmemBrowserMessage(session.Entries, session.Page, session.PageSize))
	return err
}

func worldmemExportFilename(t time.Time) string {
	if t.IsZero() {
		t = time.Now().UTC()
	}
	t = t.UTC()
	return fmt.Sprintf("x3-worldmem-%d-%d-%d.json", t.Year(), int(t.Month()), t.Day())
}

var worldmemExportFilenameRegexp = regexp.MustCompile(`^x3-worldmem-\d{4}-\d{1,2}-\d{1,2}\.json$`)

func isWorldmemExportFilename(name string) bool {
	return worldmemExportFilenameRegexp.MatchString(strings.TrimSpace(name))
}

// importWorldEntries embeds and stores facts for a channel. Shared by the
// Discord /worldmem import handler and the Matrix auto-import.
func importWorldEntries(channelKey string, entries []db.WorldEntry) (int, error) {
	texts := make([]string, 0, len(entries))
	for _, en := range entries {
		if n := worldmem.NormalizeWorld(en.Text); n != "" {
			texts = append(texts, n)
		}
	}
	if len(texts) == 0 {
		return 0, fmt.Errorf("archive contains no facts")
	}
	if len(texts) > 2000 {
		return 0, fmt.Errorf("archive contains too many facts (max 2000)")
	}

	vecs, err := worldmem.Embed(context.Background(), texts)
	if err != nil {
		return 0, fmt.Errorf("embedding failed: %w", err)
	}

	imported := 0
	for i, text := range texts {
		if len(vecs[i]) != worldmem.WorldEmbeddingDim {
			continue
		}
		if _, err := db.InsertWorldEntry(channelKey, text, vecs[i], worldmem.EstimateTokens(text), nil, worldmem.WorldEmbeddingDim); err != nil {
			slog.Warn("worldmem import insert failed", "channel", channelKey, "err", err)
			continue
		}
		imported++
	}
	return imported, nil
}

func handleWorldmemExport(e *handler.CommandEvent, channelKey string) error {
	ephemeral := e.SlashCommandInteractionData().Bool("ephemeral")
	if err := e.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	data, err := db.ExportWorldEntries(channelKey)
	if err != nil {
		return updateInteractionError(e, err.Error())
	}

	var count struct {
		Entries []json.RawMessage `json:"entries"`
	}
	n := 0
	if err := json.Unmarshal(data, &count); err == nil {
		n = len(count.Entries)
	}

	_, err = e.UpdateInteractionResponse(
		discord.NewMessageUpdate().
			WithContentf("Exported %s from this channel's world memory.", pluralize(n, "fact")).
			AddFiles(&discord.File{
				Name:   worldmemExportFilename(time.Now().UTC()),
				Reader: bytes.NewReader(data),
			}),
	)
	return err
}

func handleWorldmemImport(e *handler.CommandEvent, channelKey string) error {
	data := e.SlashCommandInteractionData()
	ephemeral := data.Bool("ephemeral")
	attachment := data.Attachment("file")
	if attachment.URL == "" {
		return sendInteractionError(e, "missing archive attachment", true)
	}

	if err := e.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	body, err := downloadWorldmemAttachment(attachment)
	if err != nil {
		return updateInteractionError(e, err.Error())
	}

	entries, err := db.ParseWorldExport(body)
	if err != nil {
		return updateInteractionError(e, err.Error())
	}

	imported, err := importWorldEntries(channelKey, entries)
	if err != nil {
		return updateInteractionError(e, err.Error())
	}

	return sendWorldmemBrowser(e, channelKey, fmt.Sprintf("Imported %s into this channel's world memory.", pluralize(imported, "fact")))
}

func downloadWorldmemAttachment(attachment discord.Attachment) ([]byte, error) {
	if attachment.Size > worldImportMaxSize {
		return nil, fmt.Errorf("archive is too large; max size is %d bytes", worldImportMaxSize)
	}

	resp, err := http.Get(attachment.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to download archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download archive: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, worldImportMaxSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read archive: %w", err)
	}
	if len(body) > worldImportMaxSize {
		return nil, fmt.Errorf("archive is too large; max size is %d bytes", worldImportMaxSize)
	}
	if !utf8.Valid(body) {
		return nil, fmt.Errorf("archive is not valid UTF-8")
	}
	return body, nil
}
