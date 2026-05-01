package commands

import (
	"sync"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
)

const maxCachedHistoryMessages = 512

type channelMessageHistory struct {
	mu       sync.RWMutex
	messages []discord.Message // newest first
}

var channelMessageHistories sync.Map // snowflake.ID -> *channelMessageHistory

func getChannelMessageHistory(channelID snowflake.ID) *channelMessageHistory {
	if cached, ok := channelMessageHistories.Load(channelID); ok {
		if history, ok := cached.(*channelMessageHistory); ok {
			return history
		}
	}

	history := &channelMessageHistory{}
	if cached, loaded := channelMessageHistories.LoadOrStore(channelID, history); loaded {
		if existing, ok := cached.(*channelMessageHistory); ok {
			return existing
		}
	}
	return history
}

func recordChannelMessageHistory(channelID snowflake.ID, message discord.Message) {
	if message.ID == 0 {
		return
	}
	getChannelMessageHistory(channelID).upsert(message)
}

func (h *channelMessageHistory) upsert(message discord.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i, existing := range h.messages {
		if existing.ID == message.ID {
			h.messages[i] = message
			return
		}
	}

	h.messages = append([]discord.Message{message}, h.messages...)
	if len(h.messages) > maxCachedHistoryMessages {
		h.messages = h.messages[:maxCachedHistoryMessages]
	}
}

func (h *channelMessageHistory) snapshotBefore(beforeID snowflake.ID, wanted int) []discord.Message {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if wanted <= 0 || len(h.messages) == 0 {
		return nil
	}

	start := 0
	if beforeID != 0 {
		for i, message := range h.messages {
			if message.ID == beforeID {
				start = i + 1
				break
			}
		}
	}

	if start >= len(h.messages) {
		return nil
	}

	end := min(len(h.messages), start+wanted)
	out := make([]discord.Message, end-start)
	copy(out, h.messages[start:end])
	return out
}

func (h *channelMessageHistory) appendOlder(messages []discord.Message) {
	if len(messages) == 0 {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, message := range messages {
		if message.ID == 0 {
			continue
		}

		duplicate := false
		for _, existing := range h.messages {
			if existing.ID == message.ID {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}

		h.messages = append(h.messages, message)
		if len(h.messages) >= maxCachedHistoryMessages {
			if len(h.messages) > maxCachedHistoryMessages {
				h.messages = h.messages[:maxCachedHistoryMessages]
			}
			return
		}
	}
}
