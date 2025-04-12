package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/zeozeozeo/x3/db"
)

// InitiateDMInteraction periodically checks cached channels and initiates a proactive DM interaction
// if conditions are met (channel is DM, user hasn't opted out, sufficient time passed).
// This function should be called periodically in a goroutine from main.
func InitiateDMInteraction(client bot.Client) {
	// Get all channel IDs from the cache
	channels, err := db.GetCachedChannelIDs() // getCachedChannelIDs is in cache.go
	if err != nil {
		slog.Error("InitiateDMInteraction: failed to get cached channel IDs", slog.Any("err", err))
		return
	}
	slog.Info("InitiateDMInteraction check", slog.Int("cached_channels", len(channels)))

	if len(channels) == 0 {
		return // No channels in cache to check
	}

	// Iterate through channels randomly to avoid always picking the same ones
	rand.Shuffle(len(channels), func(i, j int) {
		channels[i], channels[j] = channels[j], channels[i]
	})

	// Check one channel per invocation to spread out interactions
	for _, id := range channels {
		cache := db.GetChannelCache(id) // getChannelCache is in cache.go

		// --- Conditions to skip this channel ---
		if cache.Llmer != nil {
			continue // Skip channels with active LLM cache (likely non-DM or complex state)
		}
		if cache.KnownNonDM {
			continue // Skip channels previously identified as non-DM
		}
		if cache.IsLastRandomDM {
			continue // Skip if the last message *sent* by the bot was a random DM
		}
		if cache.NoRandomDMs {
			continue // Skip if user opted out
		}

		// Check time since last interaction
		if !cache.LastInteraction.IsZero() {
			minWait := 3 * time.Hour
			maxWait := 6 * time.Hour
			randomWait := minWait + time.Duration(rand.Int63n(int64(maxWait-minWait)))
			respondTime := cache.LastInteraction.Add(randomWait)

			if !time.Now().After(respondTime) {
				// slog.Debug("skipping DM interaction: too soon since last interaction", slog.String("channel_id", id.String()), slog.Time("last_interaction", cache.LastInteraction), slog.Time("respond_after", respondTime))
				continue // Skip if not enough time has passed
			}
		}
		// --- End Skip Conditions ---

		// Verify channel type (as a final check)
		channel, err := client.Rest().GetChannel(id)
		if err != nil {
			slog.Warn("InitiateDMInteraction: failed to get channel info; marking as non-DM", slog.Any("err", err), slog.String("channel_id", id.String()))
			cache.KnownNonDM = true
			cache.Write(id) // Persist the KnownNonDM flag
			continue
		}
		if channel.Type() != discord.ChannelTypeDM {
			// Use fmt.Sprintf to log the channel type integer
			slog.Info("InitiateDMInteraction: marking non-DM channel", slog.String("channel_id", id.String()), slog.String("type", fmt.Sprintf("%d", channel.Type())))
			cache.KnownNonDM = true
			cache.Write(id) // Persist the KnownNonDM flag
			continue
		}

		// --- Initiate Interaction ---
		slog.Info("Initiating proactive DM interaction", slog.String("channel", id.String()))

		// Send typing indicator
		var wg sync.WaitGroup
		wg.Add(1)
		go sendTypingWithLog(client, id, &wg) // sendTypingWithLog is in utils.go

		// Call core LLM logic with timeInteraction flag
		// handleLlmInteraction2 is in llm_interact.go
		_, _, err = handleLlmInteraction2(
			client,
			id, // Channel ID
			0,  // Message ID (not replying)
			"<you are encouraged to interact with the user after some inactivity>", // System instruction/trigger
			"system message", // Placeholder username
			0,                // User ID will be determined by handleLlmInteraction2 from history
			nil,              // No attachments
			true,             // timeInteraction flag
			false,            // Not a regenerate
			"",               // No regenerate prepend
			&wg,              // Pass WaitGroup
			nil,              // No specific message reference
			nil,              // No event
			nil,
		)

		// Handle errors from the interaction attempt
		if errors.Is(err, errTimeInteractionNoMessages) {
			slog.Warn("InitiateDMInteraction: cannot interact in empty DM channel", slog.String("channel_id", id.String()))
			// No action needed, just skip this channel for now
		} else if err != nil {
			slog.Error("InitiateDMInteraction: failed to handle LLM interaction", slog.Any("err", err), slog.String("channel_id", id.String()))
			// Logged error, potentially try again later
		} else {
			slog.Info("Proactive DM interaction sent successfully", slog.String("channel_id", id.String()))
		}

		// Only attempt one interaction per function call to avoid rate limits / spam
		return
	}

	slog.Info("InitiateDMInteraction: no suitable channels found for interaction in this cycle")
}
