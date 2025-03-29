package commands

import (
	// "database/sql" // Removed unused import
	"log/slog"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
	// "github.com/disgoorg/snowflake/v2" // Removed unused import
)

// WhitelistCommand is the definition for the /whitelist command
var WhitelistCommand = discord.SlashCommandCreate{
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
	},
}

// HandleWhitelist handles the /whitelist command logic
func HandleWhitelist(event *handler.CommandEvent) error {
	// Use isInWhitelist from db_whitelist.go
	if !db.IsInWhitelist(event.User().ID) {
		return event.CreateMessage(discord.MessageCreate{Content: "You are not in the whitelist, therefore you cannot whitelist other users", Flags: discord.MessageFlagEphemeral})
	}
	data := event.SlashCommandInteractionData()
	user := data.Snowflake("user")
	remove := data.Bool("remove")

	if remove {
		slog.Debug("removing user from whitelist", slog.String("user", user.String()))
		db.RemoveFromWhitelist(user)
		return event.CreateMessage(discord.MessageCreate{Content: "Removed user from whitelist", Flags: discord.MessageFlagEphemeral})
	} else {
		slog.Debug("adding user to whitelist", slog.String("user", user.String()))
		db.AddToWhitelist(user)
		return event.CreateMessage(discord.MessageCreate{Content: "Added user to whitelist", Flags: discord.MessageFlagEphemeral})
	}
}
