package commands

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/omit"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
)

const (
	antiscamChannelName  = "antiscam"
	antiscamChannelTopic = "Type here to erase your recent message history if your account was compromised."
)

var AntiscamCommand = discord.SlashCommandCreate{
	Name:        "antiscam",
	Description: "Toggle the anti-scam channel for this server",
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionBool{
			Name:        "enabled",
			Description: "Whether the anti-scam channel is enabled",
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

	created := 0
	refreshed := 0
	if enabled {
		var err error
		created, refreshed, err = ensureAntiscamChannel(event.Client(), *guildID)
		if err != nil {
			slog.Error("failed to ensure antiscam channel", "err", err, slog.String("guild_id", guildID.String()))
			return sendInteractionError(event, "Anti-scam was enabled, but I could not create or update the channel. Check my Manage Channels and Manage Messages permissions.", true)
		}
	}

	status := "disabled"
	details := ""
	if enabled {
		status = "enabled"
		s1 := ""
		if created > 1 || created == 0 {
			s1 = "s"
		}
		s2 := ""
		if refreshed > 1 || refreshed == 0 {
			s2 = "s"
		}
		details = fmt.Sprintf("\n\nCreated **%d** channel%s and refreshed **%d** existing one%s.", created, s1, refreshed, s2)
	}

	return sendInteractionOk(event, "Anti-scam updated", fmt.Sprintf("Anti-scam has been **%s** for this server.%s", status, details), false)
}

func ensureAntiscamChannel(client *bot.Client, guildID snowflake.ID) (created int, refreshed int, err error) {
	channels, err := client.Rest.GetGuildChannels(guildID)
	if err != nil {
		return 0, 0, err
	}

	stored, err := db.GetAntiscamChannels(guildID)
	if err != nil {
		return 0, 0, err
	}
	storedByChannel := map[snowflake.ID]db.AntiscamChannel{}
	for _, channel := range stored {
		storedByChannel[channel.ChannelID] = channel
	}

	var selectedChannelID snowflake.ID
	var selectedPromptMessageID snowflake.ID
	var fallbackOldChannelID snowflake.ID
	bottomPosition := 0
	for _, channel := range channels {
		if channel.ParentID() == nil && channel.Position() >= bottomPosition {
			bottomPosition = channel.Position() + 1
		}

		if !isAntiscamSupportedChannel(channel) {
			continue
		}

		if channel.Name() == antiscamChannelName {
			selectedChannelID = channel.ID()
			if stored, ok := storedByChannel[channel.ID()]; ok {
				selectedPromptMessageID = stored.PromptMessageID
			}
			break
		}

		if _, ok := storedByChannel[channel.ID()]; ok && selectedChannelID == 0 {
			selectedChannelID = channel.ID()
			selectedPromptMessageID = storedByChannel[channel.ID()].PromptMessageID
		}

		if channel.Name() == "x3-antiscam" && fallbackOldChannelID == 0 {
			fallbackOldChannelID = channel.ID()
		}
	}

	if selectedChannelID == 0 {
		selectedChannelID = fallbackOldChannelID
	}

	if selectedChannelID == 0 {
		channel, err := client.Rest.CreateGuildChannel(guildID, discord.GuildTextChannelCreate{
			Name:             antiscamChannelName,
			Topic:            antiscamChannelTopic,
			Position:         bottomPosition,
			RateLimitPerUser: 0,
		})
		if err != nil {
			return created, refreshed, err
		}
		selectedChannelID = channel.ID()
		created = 1
	} else {
		refreshed = 1
		if err := updateAntiscamChannel(client, selectedChannelID, bottomPosition); err != nil {
			return created, refreshed, err
		}
	}

	if err := moveAntiscamChannelToBottom(client, guildID, selectedChannelID, bottomPosition); err != nil {
		slog.Warn("failed to move antiscam channel to bottom", "err", err, slog.String("guild_id", guildID.String()), slog.String("channel_id", selectedChannelID.String()))
	}

	if err := db.ReplaceAntiscamChannels(guildID, selectedChannelID, nil); err != nil {
		return created, refreshed, err
	}
	msg, err := upsertAntiscamPrompt(client, selectedChannelID, selectedPromptMessageID)
	if err != nil {
		return created, refreshed, err
	}
	if msg != nil {
		if err := db.SetAntiscamPromptMessage(selectedChannelID, msg.ID); err != nil {
			return created, refreshed, err
		}
	}

	return created, refreshed, nil
}

func updateAntiscamChannel(client *bot.Client, channelID snowflake.ID, position int) error {
	name := antiscamChannelName
	topic := antiscamChannelTopic
	_, err := client.Rest.UpdateChannel(channelID, discord.GuildTextChannelUpdate{
		Name:     &name,
		Topic:    &topic,
		Position: &position,
	})
	return err
}

func moveAntiscamChannelToBottom(client *bot.Client, guildID, channelID snowflake.ID, position int) error {
	return client.Rest.UpdateChannelPositions(guildID, []discord.GuildChannelPositionUpdate{
		{
			ID:       channelID,
			Position: omit.NewPtr(position),
		},
	})
}

func isAntiscamSupportedChannel(channel discord.GuildChannel) bool {
	return channel.Type() == discord.ChannelTypeGuildText || channel.Type() == discord.ChannelTypeGuildNews
}

func upsertAntiscamPrompt(client *bot.Client, channelID snowflake.ID, promptMessageID snowflake.ID) (*discord.Message, error) {
	message := discord.NewMessageCreate().
		AddEmbeds(antiscamPromptEmbed())

	if promptMessageID != 0 {
		_, err := client.Rest.UpdateMessage(channelID, promptMessageID, discord.NewMessageUpdate().AddEmbeds(antiscamPromptEmbed()))
		if err == nil {
			msg, getErr := client.Rest.GetMessage(channelID, promptMessageID)
			if getErr == nil {
				return msg, nil
			}
			return &discord.Message{ID: promptMessageID}, nil
		}
		slog.Warn("failed to update antiscam prompt, creating a new one", "err", err, slog.String("channel_id", channelID.String()))
	}

	msgs, err := client.Rest.GetMessages(channelID, 0, 0, 0, 25)
	if err == nil {
		for _, msg := range msgs {
			if msg.Author.ID == client.ID() && isAntiscamPromptMessage(msg) {
				_, err := client.Rest.UpdateMessage(channelID, msg.ID, discord.NewMessageUpdate().AddEmbeds(antiscamPromptEmbed()))
				if err == nil {
					return &msg, nil
				}
				break
			}
		}
	}

	return client.Rest.CreateMessage(channelID, message)
}

func antiscamPromptEmbed() discord.Embed {
	return discord.NewEmbed().
		WithColor(0xFFD700).
		WithTitle("Anti-scam").
		WithDescription("Type in this channel to get the last 5 minutes of your message history erased (scam prevention).").
		WithFooter("x3", x3Icon).
		WithTimestamp(time.Now())
}

func isAntiscamPromptMessage(msg discord.Message) bool {
	if len(msg.Embeds) == 0 {
		return false
	}
	title := strings.TrimSpace(strings.ToLower(msg.Embeds[0].Title))
	return title == "anti-scam cleanup"
}
