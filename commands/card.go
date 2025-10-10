package commands

import (
	"fmt"
	"log/slog"
	"math/rand"
	"regexp"
	"sync"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
)

var cardMessageRegex = regexp.MustCompile(`(?i)^<card message \d+ out of \d+>`)

func isCardMessage(msg discord.Message) bool {
	return cardMessageRegex.MatchString(msg.Content)
}

// handleCard checks if a character card's first message should be sent and sends it
func handleCard(client bot.Client, channelID, messageID snowflake.ID, cache *db.ChannelCache, preMsgWg *sync.WaitGroup) (bool, error) {
	if cache.PersonaMeta.IsFirstMes && len(cache.PersonaMeta.FirstMes) > 0 {
		cache.PersonaMeta.IsFirstMes = false // mark as sent

		// determine which one to send
		idx := rand.Intn(len(cache.PersonaMeta.FirstMes))
		if cache.PersonaMeta.NextMes != nil {
			idx = *cache.PersonaMeta.NextMes
			cache.PersonaMeta.NextMes = nil
		}

		if idx < 0 || idx >= len(cache.PersonaMeta.FirstMes) {
			idx = 0
		}

		// isCardMessage checks for this exact format
		firstMes := fmt.Sprintf("<card message %d out of %d>\n\n%s", idx+1, len(cache.PersonaMeta.FirstMes), cache.PersonaMeta.FirstMes[idx])

		if preMsgWg != nil {
			preMsgWg.Wait()
		}

		_, err := sendMessageSplits(client, messageID, nil, 0, channelID, firstMes, nil, false, nil)
		if err != nil {
			cache.PersonaMeta.IsFirstMes = true
			return true, fmt.Errorf("failed to send card message: %w", err)
		}

		if err := cache.Write(channelID); err != nil {
			slog.Error("failed to write cache after sending card message", "err", err, "channel_id", channelID.String())
		}
		return true, nil // message sent
	}
	return false, nil // no card message needed
}
