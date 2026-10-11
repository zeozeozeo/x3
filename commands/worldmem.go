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
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/worldmem"
)

var worldTagRegexp = regexp.MustCompile(`(?is)<world>(.*?)</world>`)

const worldImportMaxSize = 2 * 1024 * 1024

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
// channel/room so memories never leak across channels.
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
	}
	return display
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
	}

	return sendInteractionError(e, "unknown subcommand", true)
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

	_, err = e.UpdateInteractionResponse(
		discord.NewMessageUpdate().
			WithContentf("Imported %s into this channel's world memory.", pluralize(imported, "fact")),
	)
	return err
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
