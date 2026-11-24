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
		slog.Error("failed to get global stats", "err", err)
		return sendInteractionError(event, err.Error(), true)
	}
	cache := db.GetChannelCache(event.Channel().ID())

	upSince := fmt.Sprintf("since <t:%d:R>", StartTime.Unix())
	lastProcessed := "never"
	if !stats.LastMessageTime.IsZero() {
		lastProcessed = fmt.Sprintf("<t:%d:R>", stats.LastMessageTime.Unix())
	}

	return event.CreateMessage(
		discord.NewMessageCreate().
			AddEmbeds(statsEmbed(cache, stats, upSince, lastProcessed).toDiscord()).
			WithEphemeral(ephemeral),
	)
}
