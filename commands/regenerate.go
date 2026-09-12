package commands

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
)

var RegenerateCommand = discord.SlashCommandCreate{
	Name:        "regenerate",
	Description: "Regenerate the last response, optionally prefill the response",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
		discord.InteractionContextTypeBotDM,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "prepend",
			Description: "Text to start the response with",
		},
	},
}

// splitContinuationSuffix marks non-final split messages. See sendMessageSplits.
const splitContinuationSuffix = "\u200B"

const (
	// how many messages around the target to scan for split continuations
	regenerateSplitScanLimit = 25
	// max trailing splits to delete in one regenerate
	regenerateMaxSplitDeletes = 20
)

// Trailing splits of one response are sent in a burst; messages further
// apart than this are treated as separate responses.
const regenerateSplitMaxGap = 5 * time.Minute

// isSplitContinuation reports whether a message looks like a non-final split
// of a multi-message response.
func isSplitContinuation(content string) bool {
	return strings.HasSuffix(content, splitContinuationSuffix)
}

// resolveRegenerateTarget finds the first discord message of the last bot
// response and the trailing split message IDs to delete on regenerate.
// lastResponse is the newest bot message with a reply reference (the first
// split); when the last response has no reference, it falls back to walking
// back from lastAssistantID via split continuation markers.
func resolveRegenerateTarget(
	client *bot.Client,
	channelID snowflake.ID,
	lastResponse *discord.Message,
	lastAssistantID snowflake.ID,
) (snowflake.ID, []snowflake.ID) {
	botID := client.ID()
	if lastResponse != nil {
		return lastResponse.ID, trailingBotSplitIDs(client, channelID, botID, lastResponse.ID, lastResponse.CreatedAt)
	}
	if lastAssistantID != 0 {
		return lastResponseGroupBefore(client, channelID, botID, lastAssistantID)
	}
	return 0, nil
}

// trailingBotSplitIDs collects consecutive own messages right after firstID:
// the trailing splits of the same response. It stops at the first message
// that doesn't belong to the response (another user, a referenced first
// split of a newer response, a message too far apart in time, or the final
// split which carries no continuation marker).
func trailingBotSplitIDs(client *bot.Client, channelID snowflake.ID, botID, firstID snowflake.ID, firstTime time.Time) []snowflake.ID {
	msgs, err := client.Rest.GetMessages(channelID, 0, 0, firstID, regenerateSplitScanLimit)
	if err != nil {
		slog.Warn("regenerate: failed to fetch trailing messages", "err", err)
		return nil
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID < msgs[j].ID }) // oldest first
	var ids []snowflake.ID
	for _, m := range msgs {
		if m.ID <= firstID {
			continue
		}
		if m.Author.ID != botID {
			break
		}
		if m.ReferencedMessage != nil {
			break // first split of a newer response
		}
		if !firstTime.IsZero() && !m.CreatedAt.IsZero() && m.CreatedAt.Sub(firstTime) > regenerateSplitMaxGap {
			break
		}
		ids = append(ids, m.ID)
		if !isSplitContinuation(m.Content) {
			break // final split of the response
		}
		if len(ids) >= regenerateMaxSplitDeletes {
			break
		}
	}
	return ids
}

// lastResponseGroupBefore reconstructs the last response when its first split
// has no reply reference: it walks back from the newest bot message while
// messages carry split continuation markers. Returns the first (oldest)
// message ID of the group and the newer split IDs to delete.
//
// NOTE: 2000-char hard splits inside one split chunk carry no markers, so a
// very long single split may leave earlier chunks behind; the common
// double-newline splits are always covered.
func lastResponseGroupBefore(client *bot.Client, channelID snowflake.ID, botID, lastAssistantID snowflake.ID) (snowflake.ID, []snowflake.ID) {
	msgs, err := client.Rest.GetMessages(channelID, 0, lastAssistantID, 0, 10)
	if err != nil {
		return lastAssistantID, nil
	}
	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ID > msgs[j].ID }) // newest first
	firstID := lastAssistantID
	var older []snowflake.ID // older splits of the same response, newest first
	for _, m := range msgs {
		if m.ID >= lastAssistantID {
			continue
		}
		if m.Author.ID != botID {
			break
		}
		if !isSplitContinuation(m.Content) {
			break // previous response's tail (or its single message)
		}
		older = append(older, m.ID)
		firstID = m.ID
		if m.ReferencedMessage != nil {
			break // first split (has the reply reference)
		}
		if len(older) >= regenerateMaxSplitDeletes {
			break
		}
	}
	if len(older) == 0 {
		return lastAssistantID, nil
	}
	deletes := make([]snowflake.ID, 0, len(older))
	deletes = append(deletes, lastAssistantID)
	deletes = append(deletes, older[:len(older)-1]...)
	return firstID, deletes
}

// deleteDiscordMessages removes trailing split messages, using bulk delete
// outside DMs when possible and falling back to single deletes.
func deleteDiscordMessages(client *bot.Client, channelID snowflake.ID, ids []snowflake.ID, inDM bool) {
	if len(ids) == 0 {
		return
	}
	getChannelMessageHistory(channelID).removeIDs(ids)
	if !inDM && len(ids) >= 2 {
		if err := client.Rest.BulkDeleteMessages(channelID, ids); err == nil {
			return
		} else {
			slog.Warn("regenerate: bulk delete failed, falling back to single deletes", "err", err)
		}
	}
	for _, id := range ids {
		if err := client.Rest.DeleteMessage(channelID, id); err != nil {
			slog.Warn("regenerate: failed to delete trailing split", "err", err, "message_id", id.String())
		}
	}
}

// HandleRegenerate handles the /regenerate command.
func HandleRegenerate(event *handler.CommandEvent) error {
	prepend := event.SlashCommandInteractionData().String("prepend")
	prepend = strings.ReplaceAll(prepend, "\\n", "\n") // allow user to specify newlines
	if prepend != "" && !endsWithWhitespace(prepend) {
		prepend += " "
	}

	err := event.DeferCreateMessage(true)
	if err != nil {
		return err
	}

	jumpURL, _, err := handleLlmInteraction2(
		event.Client(),
		event.Channel().ID(),
		0,     // messageID is determined by handleLlmInteraction2 when regenerating
		"",    // content is empty for regeneration
		"",    // no username
		0,     // no memory
		nil,   // no attachments
		nil,   // no embeds
		false, // timeInteraction
		true,  // isRegenerate
		prepend,
		nil,   // no wg
		nil,   // no reference
		nil,   // no event
		nil,   // no system prompt override
		false, // not impersonate
		event.Channel().Type() == discord.ChannelTypeDM,
		event.GuildID(),
		interactionChannelNSFW(event.Channel()),
		context.Background(),
	)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	_, err = event.UpdateInteractionResponse(
		discord.NewMessageUpdate().
			WithFlags(discord.MessageFlagEphemeral).
			WithContentf("Regenerated message %s", jumpURL),
	)
	return err
}
