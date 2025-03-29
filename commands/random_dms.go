package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

// RandomDMsCommand is the definition for the /random_dms command
var RandomDMsCommand = discord.SlashCommandCreate{
	Name:        "random_dms",
	Description: "Choose if the bot should DM you randomly",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeUserInstall, // User install only
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeBotDM, // DM only
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionBool{
			Name:        "enable",
			Description: "If the bot should DM you randomly",
			Required:    true,
		},
	},
}

// HandleRandomDMs handles the /random_dms command logic.
func HandleRandomDMs(event *handler.CommandEvent) error {
	enable := event.SlashCommandInteractionData().Bool("enable")
	cache := db.GetChannelCache(event.Channel().ID())
	cache.NoRandomDMs = !enable
	cache.EverUsedRandomDMs = true    // Mark that the user has explicitly set this preference
	cache.Write(event.Channel().ID()) // Error handling for write is omitted here as in original code

	var content string
	if enable {
		content = "Random DMs enabled. The bot may DM you at times (max 1 message per day)."
	} else {
		content = "Random DMs disabled. Use `/random_dms enable:true` if you wish to opt-in again."
	}
	// Response is not ephemeral by default for this command
	return event.CreateMessage(discord.NewMessageCreateBuilder().SetContent(content).Build())
}
