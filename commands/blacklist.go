package commands

import (
	// "fmt" // Removed unused import

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

// BlacklistCommand is the definition for the /blacklist command
var BlacklistCommand = discord.SlashCommandCreate{
	Name:        "blacklist",
	Description: "For server moderators: blacklist a channel from the bot",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall, // Guild only
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild, // Guild only
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionChannel{
			Name:        "channel",
			Description: "Channel to blacklist. If already in the blacklist, removes it instead",
			Required:    true,
			ChannelTypes: []discord.ChannelType{
				discord.ChannelTypeGuildText,
				discord.ChannelTypeGuildVoice,
				discord.ChannelTypeGuildCategory,
				discord.ChannelTypeGuildNews,
				discord.ChannelTypeGuildNewsThread,
				discord.ChannelTypeGuildPublicThread,
				discord.ChannelTypeGuildPrivateThread,
				discord.ChannelTypeGuildStageVoice,
				discord.ChannelTypeGuildDirectory,
				discord.ChannelTypeGuildForum,
				discord.ChannelTypeGuildMedia,
			},
		},
		discord.ApplicationCommandOptionBool{
			Name:        "ephemeral",
			Description: "If the response should only be visible to you",
		},
	},
}

// HandleBlacklist handles the /blacklist command logic.
func HandleBlacklist(event *handler.CommandEvent) error {
	// This command is guild-only, so Member should not be nil
	if event.Member() == nil {
		return sendInteractionError(event, "This command can only be used in a server.", true)
	}
	if !isModerator(event.Member().Permissions) {
		return sendInteractionError(event, "You must be a moderator to use this command.", true)
	}

	data := event.SlashCommandInteractionData()
	channelID := data.Snowflake("channel")
	ephemeral := data.Bool("ephemeral") // Defaults to false if not provided

	if db.IsChannelInBlacklist(channelID) {
		// Channel is already blacklisted, remove it
		err := db.RemoveChannelFromBlacklist(channelID)
		if err != nil {
			return sendInteractionError(event, "Failed to remove channel from blacklist.", true)
		}
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContentf("Removed channel <#%d> from the blacklist.", channelID).
				SetEphemeral(ephemeral).
				Build(),
		)
	} else {
		// Channel is not blacklisted, add it
		err := db.AddChannelToBlacklist(channelID)
		if err != nil {
			return sendInteractionError(event, "Failed to add channel to blacklist.", true)
		}
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContentf("Added channel <#%d> to the blacklist. The bot will ignore messages in this channel.", channelID).
				SetEphemeral(ephemeral).
				Build(),
		)
	}
}
