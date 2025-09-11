package commands

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/dustin/go-humanize"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
)

// StartTime records when the bot started.
var StartTime = time.Now()

var StatsCommand = discord.SlashCommandCreate{
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

// HandleStats handles the /stats command.
func HandleStats(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	ephemeral, ok := data.OptBool("ephemeral")
	if !ok {
		// ephemeral is true by default for this command
		ephemeral = true
	}

	stats, err := db.GetGlobalStats()
	if err != nil {
		slog.Error("failed to get global stats", slog.Any("err", err))
		return sendInteractionError(event, err.Error(), true)
	}
	cache := db.GetChannelCache(event.Channel().ID())

	prompt, response, total := formatUsageStrings(cache.Usage)
	promptLast, responseLast, totalLast := formatUsageStrings(cache.LastUsage)
	promptTotal, responseTotal, totalTotal := formatUsageStrings(stats.Usage)
	upSince := fmt.Sprintf("since <t:%d:R>", StartTime.Unix())
	lastProcessed := "never"
	if !stats.LastMessageTime.IsZero() {
		lastProcessed = fmt.Sprintf("<t:%d:R>", stats.LastMessageTime.Unix())
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetTitle("Stats").
					SetColor(0x0085ff).
					SetDescription("Per-channel and global bot stats").
					SetFooter("x3", x3Icon).
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
					AddField("Last message processed", lastProcessed, true).
					AddField("Images generated", humanize.Comma(int64(stats.ImagesGenerated)), true).
					Build(),
			).
			SetEphemeral(ephemeral).
			Build(),
	)
}
