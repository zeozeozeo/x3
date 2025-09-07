package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

var RandomDMsCommand = discord.SlashCommandCreate{
	Name:        "random_dms",
	Description: "Choose if the bot should DM you randomly",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeBotDM,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionBool{
			Name:        "enable",
			Description: "If the bot should DM you randomly",
			Required:    true,
		},
	},
}

// HandleRandomDMs handles the /random_dms command.
func HandleRandomDMs(event *handler.CommandEvent) error {
	enable := event.SlashCommandInteractionData().Bool("enable")
	cache := db.GetChannelCache(event.Channel().ID())
	cache.NoRandomDMs = !enable
	cache.EverUsedRandomDMs = true
	cache.Write(event.Channel().ID())

	var content string
	if enable {
		content = "Random DMs enabled. The bot may DM you at times (max 1 message per day)."
	} else {
		content = "Random DMs disabled. Use `/random_dms enable:true` if you wish to opt-in again."
	}
	return event.CreateMessage(discord.NewMessageCreateBuilder().SetContent(content).Build())
}
