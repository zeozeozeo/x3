package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
	"github.com/zeozeozeo/x3/schematic"
)

var SchematicCommand = discord.SlashCommandCreate{
	Name:        "schematic",
	Description: "Generate a Minecraft schematic with the active persona models",
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
		discord.ApplicationCommandOptionString{Name: "prompt", Description: "What to build", Required: true},
		discord.ApplicationCommandOptionString{Name: "size", Description: "Canvas: 128 or 128x64x96 (1..320; default 128)", Required: false},
	},
}

func newSchematicGenerator() *schematic.Generator {
	return &schematic.Generator{Complete: func(ctx context.Context, messages *llm.Llmer, models []model.Model, settings persona.InferenceSettings) (string, llm.Usage, error) {
		return messages.RequestCompletion(models, settings, "", ctx)
	}}
}

type schematicProgressThrottle struct {
	mu        sync.Mutex
	lastStage string
	last      time.Time
}

func (t *schematicProgressThrottle) Allow(p schematic.Progress) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if p.Stage == t.lastStage && now.Sub(t.last) < 750*time.Millisecond {
		return false
	}
	t.lastStage, t.last = p.Stage, now
	return true
}

func schematicProgressText(p schematic.Progress) string {
	title := strings.ToUpper(p.Stage[:1]) + p.Stage[1:]
	if p.Detail == "" {
		return "Generating schematic: **" + title + "**"
	}
	if p.Stage == "repairing" || p.Stage == "failed" {
		return fmt.Sprintf("Generating schematic: **%s**\n```text\n%s\n```", title, ellipsisTrim(p.Detail, 1650))
	}
	return fmt.Sprintf("Generating schematic: **%s**\n%s", title, p.Detail)
}

func formatSchematicAttemptErrors(attempts []schematic.AttemptError, limit int) string {
	if len(attempts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, attempt := range attempts {
		stage := attempt.Stage
		if stage == "" {
			stage = "validation"
		}
		line := strings.TrimSpace(attempt.Err.Error())
		if before, _, ok := strings.Cut(line, "\n"); ok {
			line = before
		}
		fmt.Fprintf(&b, "Attempt %d (%s): %s\n", attempt.Attempt, stage, line)
	}
	return ellipsisTrim(strings.TrimSpace(b.String()), limit)
}

func HandleSchematic(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	prompt := strings.TrimSpace(data.String("prompt"))
	bounds, err := schematic.ParseBounds(data.String("size"))
	if err != nil {
		return sendInteractionError(event, err.Error(), true)
	}
	if err = event.DeferCreateMessage(false); err != nil {
		return err
	}
	cache := db.GetChannelCache(event.Channel().ID())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	throttle := &schematicProgressThrottle{}
	result, genErr := newSchematicGenerator().Generate(ctx, schematic.Request{Prompt: prompt, Bounds: bounds, Models: cache.PersonaMeta.GetModels(), Settings: cache.PersonaMeta.Settings}, func(p schematic.Progress) {
		if !throttle.Allow(p) {
			return
		}
		_, _ = event.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent(schematicProgressText(p)))
	})
	recordSchematicUsage(cache, event.Channel().ID(), result.Usage)
	if genErr != nil {
		slog.Error("Discord schematic generation failed", "channel_id", event.Channel().ID(), "user_id", event.User().ID, "attempts", result.Attempts, "error", genErr)
		var attempts *schematic.GenerationError
		if errors.As(genErr, &attempts) {
			debug, _ := schematic.DebugArchive(attempts.Attempts)
			details := formatSchematicAttemptErrors(attempts.Attempts, 1500)
			update := discord.NewMessageUpdate().WithContent("Schematic generation failed after five attempts.\n```text\n" + details + "\n```")
			if len(debug) > 0 {
				update = update.WithFiles(&discord.File{Name: "schematic-debug.zip", Reader: bytes.NewReader(debug)})
			}
			_, err = event.UpdateInteractionResponse(update)
			return err
		}
		return updateInteractionError(event, genErr.Error())
	}
	name := schematicArchiveName(prompt)
	content := fmt.Sprintf("Schematic generated in %d attempt(s): **%d blocks**, %dx%dx%d occupied bounds.\nTarget: Minecraft Java %s. The ZIP contains Sponge, Litematica, Axiom, MCEdit, VXL source, and a material report.", result.Attempts, result.Occupied, result.Dimensions.X, result.Dimensions.Y, result.Dimensions.Z, schematic.DefaultCatalog().MinecraftVersion)
	if len(result.Repairs) > 0 {
		content += "\n\nEarlier model errors that were repaired:\n```text\n" + formatSchematicAttemptErrors(result.Repairs, 700) + "\n```"
	}
	_, err = event.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent(content).WithFiles(&discord.File{Name: name, Reader: bytes.NewReader(result.Archive)}))
	return err
}

func recordSchematicUsage(cache *db.ChannelCache, channelID snowflake.ID, usage llm.Usage) {
	if cache == nil || usage.IsEmpty() {
		return
	}
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage
	cache.UpdateInteractionTime()
	_ = cache.Write(channelID)
	_ = db.UpdateGlobalStats(usage)
}

var schematicSlugRE = regexp.MustCompile(`[^a-z0-9]+`)

func schematicArchiveName(prompt string) string {
	s := schematicSlugRE.ReplaceAllString(strings.ToLower(prompt), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "build"
	}
	if len(s) > 48 {
		s = s[:48]
		s = strings.TrimRight(s, "-")
	}
	return "schematic-" + s + ".zip"
}
