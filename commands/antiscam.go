package commands

import (
	"fmt"
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

var AntiscamCommand = discord.SlashCommandCreate{
	Name:        "antiscam",
	Description: "Toggle the anti-scam feature for this server",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionBool{
			Name:        "enabled",
			Description: "Whether the anti-scam feature is enabled",
			Required:    true,
		},
	},
}

func HandleAntiscam(event *handler.CommandEvent) error {
	enabled := event.SlashCommandInteractionData().Bool("enabled")

	// check if user is moderator
	if event.Member() == nil || !isModerator(event.Member().Permissions) {
		return sendInteractionError(event, "You must be a moderator to use this command", true)
	}

	guildID := event.GuildID()
	if guildID == nil {
		return sendInteractionError(event, "This command can only be used in a server", true)
	}

	if err := db.SetAntiscamEnabled(*guildID, enabled); err != nil {
		slog.Error("failed to set antiscam enabled", "err", err, slog.String("guild_id", guildID.String()))
		return sendInteractionError(event, "Failed to update anti-scam settings", true)
	}

	status := "disabled"
	if enabled {
		status = "enabled"
	}

	return sendInteractionOk(event, "Anti-scam updated", fmt.Sprintf("Anti-scam has been **%s** for this server.", status), false)
}
