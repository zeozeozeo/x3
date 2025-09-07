package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

var BlacklistCommand = discord.SlashCommandCreate{
	Name:        "blacklist",
	Description: "For server moderators: blacklist a channel from the bot",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
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
			Name:        "image_generation",
			Description: "Only blacklist image generation",
		},
		discord.ApplicationCommandOptionBool{
			Name:        "ephemeral",
			Description: "If the response should only be visible to you",
		},
	},
}

// HandleBlacklist handles the /blacklist command
func HandleBlacklist(event *handler.CommandEvent) error {
	if event.Member() == nil {
		return sendInteractionError(event, "This command can only be used in a server.", true)
	}
	if !isModerator(event.Member().Permissions) {
		return sendInteractionError(event, "You must be a moderator to use this command.", true)
	}

	data := event.SlashCommandInteractionData()
	channelID := data.Snowflake("channel")
	ephemeral := data.Bool("ephemeral")
	imageBlacklist := data.Bool("image_generation")

	var actionMessage string
	if imageBlacklist {
		if db.IsChannelInImageBlacklist(channelID) {
			err := db.RemoveChannelFromImageBlacklist(channelID)
			if err != nil {
				return sendInteractionError(event, "Failed to remove channel from image blacklist.", true)
			}
			actionMessage = "Removed channel <#%d> from the image blacklist."
		} else {
			err := db.AddChannelToImageBlacklist(channelID)
			if err != nil {
				return sendInteractionError(event, "Failed to add channel to image blacklist.", true)
			}
			actionMessage = "Added channel <#%d> to the image blacklist. Image generation will be ignored in this channel."
		}
	} else {
		if db.IsChannelInBlacklist(channelID) {
			err := db.RemoveChannelFromBlacklist(channelID)
			if err != nil {
				return sendInteractionError(event, "Failed to remove channel from blacklist.", true)
			}
			actionMessage = "Removed channel <#%d> from the blacklist."
		} else {
			err := db.AddChannelToBlacklist(channelID)
			if err != nil {
				return sendInteractionError(event, "Failed to add channel to blacklist.", true)
			}
			actionMessage = "Added channel <#%d> to the blacklist. The bot will ignore messages in this channel."
		}
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetContentf(actionMessage, channelID).
			SetEphemeral(ephemeral).
			Build(),
	)
}
