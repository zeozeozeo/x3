package commands

import (
	"fmt"
	"log/slog"
	"math/rand"
	"regexp"
	"sync"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord" // Added import
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
)

var cardMessageRegex = regexp.MustCompile(`(?i)^<card message \d+ out of \d+>`)

// isCardMessage checks if a message content indicates it's a card message.
func isCardMessage(msg discord.Message) bool {
	return cardMessageRegex.MatchString(msg.Content)
}

// handleCard checks if a character card's first message should be sent and sends it.
// Returns true if a message was sent (or an error occurred), indicating the interaction should stop.
// Returns false if no card message needed to be sent.
func handleCard(client bot.Client, channelID, messageID snowflake.ID, cache *db.ChannelCache, preMsgWg *sync.WaitGroup) (bool, error) {
	if cache.PersonaMeta.IsFirstMes && len(cache.PersonaMeta.FirstMes) > 0 {
		cache.PersonaMeta.IsFirstMes = false // Mark as sent

		// Determine which message to send (random or specific index)
		idx := rand.Intn(len(cache.PersonaMeta.FirstMes))
		if cache.PersonaMeta.NextMes != nil {
			idx = *cache.PersonaMeta.NextMes
			cache.PersonaMeta.NextMes = nil // Reset NextMes after using it
		}

		// Ensure index is valid
		if idx < 0 || idx >= len(cache.PersonaMeta.FirstMes) {
			idx = 0 // Default to first message if index is invalid
		}

		// Format the message content
		firstMes := fmt.Sprintf("<card message %d out of %d>\n\n%s", idx+1, len(cache.PersonaMeta.FirstMes), cache.PersonaMeta.FirstMes[idx])

		// Wait for any preceding operations (like typing indicator)
		if preMsgWg != nil {
			preMsgWg.Wait()
		}

		// Send the message split (using sendMessageSplits which handles >2000 chars)
		// NOTE: sendMessageSplits is defined in utils.go
		_, err := sendMessageSplits(client, messageID, nil, 0, channelID, []rune(firstMes), nil, false)
		if err != nil {
			// Don't reset IsFirstMes on error, maybe retry later? For now, return error.
			cache.PersonaMeta.IsFirstMes = true // Revert state change on error
			return true, fmt.Errorf("failed to send card message: %w", err)
		}

		// Update cache only after successful send
		if err := cache.Write(channelID); err != nil {
			// Log error but the message was already sent, so don't revert IsFirstMes
			slog.Error("Failed to write cache after sending card message", slog.Any("err", err), slog.String("channel_id", channelID.String()))
		}
		return true, nil // Message sent successfully
	}
	return false, nil // No card message needed
}
