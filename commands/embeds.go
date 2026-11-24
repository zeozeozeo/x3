package commands

import (
	"fmt"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/dustin/go-humanize"
	fluxer "github.com/invaliduser231/fluxer-go"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/persona"
)

// Command embeds are described once here, then rendered for Discord and Fluxer.
// A command handler builds a commandEmbed and the platform sends it, so the two
// frontends cannot drift apart.
const (
	embedColorPrimary = 0x0085ff
	embedColorError   = 0xf54242
	embedColorQuote   = 0xFFD700
)

type commandEmbedField struct {
	Name   string
	Value  string
	Inline bool
}

// commandEmbed is a platform-neutral rich card.
type commandEmbed struct {
	Title       string
	Description string
	Color       int
	Fields      []commandEmbedField
	Footer      string
	FooterIcon  string
	Timestamp   time.Time
}

func newCommandEmbed(title, description string) *commandEmbed {
	return &commandEmbed{
		Title:       title,
		Description: description,
		Color:       embedColorPrimary,
		Footer:      "x3",
		FooterIcon:  x3Icon,
		Timestamp:   time.Now(),
	}
}

func (e *commandEmbed) AddField(name, value string, inline bool) *commandEmbed {
	e.Fields = append(e.Fields, commandEmbedField{Name: name, Value: value, Inline: inline})
	return e
}

func (e *commandEmbed) toDiscord() discord.Embed {
	embed := discord.NewEmbed().
		WithColor(e.Color).
		WithTimestamp(e.Timestamp)
	if e.Title != "" {
		embed = embed.WithTitle(e.Title)
	}
	if e.Description != "" {
		embed = embed.WithDescription(e.Description)
	}
	if e.Footer != "" {
		embed = embed.WithFooter(e.Footer, e.FooterIcon)
	}
	for _, field := range e.Fields {
		embed = embed.AddField(field.Name, field.Value, field.Inline)
	}
	return embed
}

func (e *commandEmbed) toFluxer() *fluxer.Embed {
	embed := &fluxer.Embed{
		Title:       e.Title,
		Description: e.Description,
		Fields:      make([]fluxer.EmbedField, 0, len(e.Fields)),
	}
	if e.Color != 0 {
		embed.SetColor(e.Color)
	}
	if !e.Timestamp.IsZero() {
		embed.SetTimestamp(e.Timestamp)
	}
	if e.Footer != "" {
		embed.Footer = &fluxer.EmbedFooter{Text: e.Footer, IconURL: e.FooterIcon}
	}
	for _, field := range e.Fields {
		embed.Fields = append(embed.Fields, fluxer.EmbedField{
			Name:   field.Name,
			Value:  field.Value,
			Inline: field.Inline,
		})
	}
	return embed
}

// commandOKEmbed is the card used for successful command results.
func commandOKEmbed(title, message string) *commandEmbed {
	return newCommandEmbed(title, ellipsisTrim(message, 1024))
}

// commandErrorEmbed is the card used for command failures.
func commandErrorEmbed(message string) *commandEmbed {
	embed := newCommandEmbed("❌ Error", toTitle(message))
	embed.Color = embedColorError
	embed.FooterIcon = x3ErrorIcon
	return embed
}

// personaInfoEmbed renders the current persona settings. The returned prompt is
// the untrimmed system prompt, so callers can attach it as a file when the card
// truncates it.
func personaInfoEmbed(cache *db.ChannelCache, username, botName string, isDM bool, promptContext persona.PromptContext, description string) (*commandEmbed, string) {
	settings := cache.PersonaMeta.Settings.Fixup()
	remapped := settings
	remapped.Remap()

	embed := newCommandEmbed("Persona", description)
	embed.AddField("Name", cache.PersonaMeta.Name, true)
	if meta, err := persona.GetMetaByName(cache.PersonaMeta.Name); err == nil && meta.Desc != "" {
		embed.AddField("Description", meta.Desc, true)
	}
	embed.AddField("Temperature", fmt.Sprintf("%s (remapped to %s)", ftoa(settings.Temperature), ftoa(remapped.Temperature)), true)
	embed.AddField("Top P", fmt.Sprintf("%s (remapped to %s)", ftoa(settings.TopP), ftoa(remapped.TopP)), true)
	embed.AddField("Flags", fmt.Sprintf("images: %s, reasoning: %s, minilm: %s, tools: %s",
		enabledDisabled(cache.PersonaMeta.EnableImages),
		enabledDisabled(settings.Reasoning),
		enabledDisabled(cache.PersonaMeta.EnableMiniLMContinuations),
		enabledDisabled(cache.PersonaMeta.ToolsEnabled()),
	), true)
	embed.AddField("Frequency Penalty", ftoa(settings.FrequencyPenalty), true)
	embed.AddField("Context length", fmt.Sprintf("%d", cache.ContextLength), true)
	if cache.Llmer != nil {
		embed.AddField("Message cache", fmt.Sprintf("%d messages", cache.Llmer.NumMessages()), true)
	}
	if cache.PersonaMeta.ChatPreset != nil {
		embed.AddField("SillyTavern preset", cache.PersonaMeta.ChatPreset.DisplayName(), true)
	}
	embed.AddField("HTML rendering", enabledDisabled(cache.PersonaMeta.RenderHTML), true)
	embed.AddField("Jailbreak", cache.PersonaMeta.JailbreakMode(), true)

	models := cache.PersonaMeta.Models
	switch len(models) {
	case 0:
	case 1:
		embed.AddField("Model", models[0], true)
	default:
		embed.AddField("Models", strings.Join(models, ", "), true)
	}

	if len(cache.Memories) > 0 {
		embed.AddField("Memories", ellipsisTrim(strings.Join(cache.Memories, "\n"), 1024), false)
	}
	if summariesEnabled() && len(cache.Summaries) > 0 {
		embed.AddField("Summary", ellipsisTrim(cache.Summaries[0].Str, 1024), false)
	}

	systemPrompt := persona.GetPersonaByMetaWithBotName(cache.PersonaMeta, username, botName, isDM, promptContext).System
	if systemPrompt != "" {
		embed.AddField("System prompt", ellipsisTrim(systemPrompt, 1024), false)
	}
	return embed, systemPrompt
}

// personaUpdateEmbed reports the settings changed by a persona command.
func personaUpdateEmbed(changes []string, creatorNotes string) *commandEmbed {
	description := "No changes made"
	if len(changes) > 0 {
		description = "Updated persona for this channel (" + strings.Join(changes, ", ") + ")"
	}
	embed := newCommandEmbed("Updated persona", description)
	if creatorNotes != "" {
		embed.AddField("Creator notes", ellipsisTrim(creatorNotes, 1024), false)
	}
	return embed
}

// statsEmbed renders per-channel and global usage. uptime and lastProcessed are
// already formatted by the caller because Discord's relative timestamps have no
// Fluxer equivalent.
func statsEmbed(cache *db.ChannelCache, stats db.GlobalStats, uptime, lastProcessed string) *commandEmbed {
	prompt, response, total := formatUsageStrings(cache.Usage)
	promptLast, responseLast, totalLast := formatUsageStrings(cache.LastUsage)
	promptTotal, responseTotal, totalTotal := formatUsageStrings(stats.Usage)

	embed := newCommandEmbed("Stats", "Per-channel and global bot stats")
	embed.AddField("Prompt tokens (channel)", prompt, true)
	embed.AddField("Response tokens (channel)", response, true)
	embed.AddField("Total tokens (channel)", total, true)
	embed.AddField("Prompt tokens (last)", promptLast, true)
	embed.AddField("Response tokens (last)", responseLast, true)
	embed.AddField("Total tokens (last)", totalLast, true)
	embed.AddField("Prompt tokens (global)", promptTotal, true)
	embed.AddField("Response tokens (global)", responseTotal, true)
	embed.AddField("Total tokens (global)", totalTotal, true)
	embed.AddField("Bot uptime", uptime, true)
	embed.AddField("Messages processed", humanize.Comma(int64(stats.MessageCount)), true)
	embed.AddField("Last message processed", lastProcessed, true)
	embed.AddField("Images generated", humanize.Comma(int64(stats.ImagesGenerated)), true)
	return embed
}

// commandEmbedPage clamps page for a deck of total items and returns the last
// page index, so card builders and their button rows agree on the page.
func commandEmbedPage(total, page, pageSize int) (int, int) {
	if pageSize < 1 {
		pageSize = 1
	}
	maxPage := max((total+pageSize-1)/pageSize-1, 0)
	if page < 0 {
		page = 0
	}
	if page > maxPage {
		page = maxPage
	}
	return page, maxPage
}

// worldmemEmbed renders one page of stored world facts.
func worldmemEmbed(entries []db.WorldEntry, page, pageSize int) *commandEmbed {
	if pageSize < 1 {
		pageSize = 1
	}
	total := len(entries)
	page, maxPage := commandEmbedPage(total, page, pageSize)
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

	embed := newCommandEmbed("World memory", ellipsisTrim(b.String(), 4096))
	embed.Footer = fmt.Sprintf("Page %d/%d • %s", page+1, maxPage+1, pluralize(total, "fact"))
	return embed
}

// chatArchiveEmbed renders one page of an imported chat archive. formatTime
// lets Discord keep its dynamic <t:...> markup while Fluxer receives a plain
// timestamp it can render.
func chatArchiveEmbed(archive chatArchive, page, pageSize int, formatTime func(time.Time) string) *commandEmbed {
	if pageSize < 1 {
		pageSize = 1
	}
	if formatTime == nil {
		formatTime = plainTimestamp
	}
	total := len(archive.Messages)
	page, maxPage := commandEmbedPage(total, page, pageSize)
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
				b.WriteString(" ")
				b.WriteString(formatTime(msg.Timestamp))
			}
			b.WriteString("\n")
			b.WriteString(ellipsisTrim(stripSplitWarning(msg.Content), 450))
			if len(msg.Images) > 0 {
				fmt.Fprintf(&b, "\n-# %s", pluralize(len(msg.Images), "image"))
			}
			if i < end-1 {
				b.WriteString("\n\n")
			}
		}
	}

	embed := newCommandEmbed("Imported chat archive", ellipsisTrim(b.String(), 4096))
	embed.Footer = fmt.Sprintf("Page %d/%d", page+1, maxPage+1)
	return embed
}

func discordTimestamp(t time.Time) string {
	return fmt.Sprintf("<t:%d:f>", t.Unix())
}

func plainTimestamp(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04")
}
