package commands

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

// GlobalCommand configures a guild-wide single-channel mode for the bot.
var GlobalCommand = discord.SlashCommandCreate{
	Name:        "global",
	Description: "Limit the bot to one channel and control persona editing",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionChannel{
			Name:        "mainchannel",
			Description: "The only channel where the bot accepts messages",
			Required:    false,
			ChannelTypes: []discord.ChannelType{
				discord.ChannelTypeGuildText,
				discord.ChannelTypeGuildNews,
				discord.ChannelTypeGuildPublicThread,
				discord.ChannelTypeGuildPrivateThread,
				discord.ChannelTypeGuildNewsThread,
				discord.ChannelTypeGuildForum,
				discord.ChannelTypeGuildMedia,
			},
		},
		discord.ApplicationCommandOptionBool{
			Name:        "blockpersonaedit",
			Description: "Only moderators, administrators, and the owner may use /persona",
			Required:    false,
		},
	},
}

// HandleGlobal handles the /global command.
func HandleGlobal(event *handler.CommandEvent) error {
	if event.Member() == nil || !isModeratorOrGuildOwner(event) {
		return sendInteractionError(event, "You must be a moderator, administrator, or the server owner to use this command.", true)
	}

	guildID := event.GuildID()
	if guildID == nil {
		return sendInteractionError(event, "This command can only be used in a server.", true)
	}

	settings, err := db.GetGlobalSettings(*guildID)
	if err != nil {
		return sendInteractionError(event, "Failed to read global settings.", true)
	}

	data := event.SlashCommandInteractionData()
	if mainChannelID, ok := data.OptSnowflake("mainchannel"); ok {
		settings.MainChannelID = &mainChannelID
	}
	if blockPersonaEdit, ok := data.OptBool("blockpersonaedit"); ok {
		settings.BlockPersonaEdit = blockPersonaEdit
	}

	if !dataHasOption(data, "mainchannel") && !dataHasOption(data, "blockpersonaedit") {
		return event.CreateMessage(discord.NewMessageCreate().
			WithContent(globalSettingsDescription(settings)).
			WithEphemeral(true))
	}

	if err := db.SetGlobalSettings(*guildID, settings); err != nil {
		return sendInteractionError(event, "Failed to save global settings.", true)
	}

	return event.CreateMessage(discord.NewMessageCreate().
		WithContent(globalSettingsDescription(settings)).
		WithEphemeral(true))
}

func dataHasOption(data discord.SlashCommandInteractionData, name string) bool {
	_, ok := data.Option(name)
	return ok
}

func globalSettingsDescription(settings db.GlobalSettings) string {
	mainChannel := "disabled"
	if settings.MainChannelID != nil {
		mainChannel = fmt.Sprintf("<#%s>", settings.MainChannelID.String())
	}
	personaEditing := "allowed for everyone"
	if settings.BlockPersonaEdit {
		personaEditing = "restricted to moderators, administrators, and the server owner"
	}
	return fmt.Sprintf("Global mode: %s\nPersona editing: %s", mainChannel, personaEditing)
}

func isModeratorOrGuildOwner(event *handler.CommandEvent) bool {
	member := event.Member()
	if member == nil {
		return false
	}
	if isModerator(member.Permissions) {
		return true
	}
	guild, ok := event.Guild()
	return ok && guild.OwnerID == event.User().ID
}
