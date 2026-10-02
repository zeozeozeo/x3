package commands

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/decide"
	"github.com/zeozeozeo/x3/llm"
)

// candidate describes the message the respond decision is being made about.
type candidate struct {
	// ID keys the verdict cache so one message is never judged twice.
	ID string
	// SnowflakeID identifies the message in the channel history buffer, so it
	// can be left out of the context handed to the model.
	SnowflakeID snowflake.ID
	// ChannelID and BotID locate the message and the bot in that buffer.
	ChannelID snowflake.ID
	BotID     snowflake.ID
	// Content is the message text.
	Content string
	// AuthorName is who said it.
	AuthorName string
	// ChannelName and PersonaName give the decision model its setting.
	ChannelName string
	PersonaName string
	// BotNames are the names the bot answers to.
	BotNames []string
}

// shouldTriggerContinuation decides whether the bot should keep talking in a
// channel it was not directly summoned in.
func shouldTriggerContinuation(cache *db.ChannelCache, cand candidate) bool {
	if cache == nil {
		return false
	}
	cfg := decide.LoadConfig()
	now := time.Now()
	if !cache.LastInteraction.IsZero() && now.Sub(cache.LastInteraction) <= cfg.GraceWindow {
		// Recent enough that the user is plainly still talking to us. This is
		// the only path that answers without asking the model, and it is
		// deliberately short so the model owns nearly every follow-up.
		return true
	}
	if !cache.PersonaMeta.EnableMiniLMContinuations {
		return false
	}

	decision := decide.ShouldTrigger(context.Background(), decide.DecisionInput{
		Enabled:         true,
		Now:             now,
		LastInteraction: cache.LastInteraction,
		Candidate:       cand.Content,
		CandidateName:   cand.AuthorName,
		History:         cacheHistoryForContinuation(cache),
		Unanswered:      unansweredSinceLastReply(cand, cfg.HistoryTurns),
		BotNames:        cand.BotNames,
		ChannelName:     cand.ChannelName,
		PersonaName:     cand.PersonaName,
		CacheKey:        cand.ID,
		Config:          cfg,
	})
	slog.Info("continuation trigger decision",
		"trigger", decision.Trigger,
		"reason", decision.Reason,
		"score", decision.Score,
		"addressed", decision.Addressed,
		"kind", decision.Kind,
	)
	return decision.Trigger
}

// unansweredSinceLastReply collects the messages that arrived after the bot's
// most recent reply, oldest first.
func unansweredSinceLastReply(cand candidate, wanted int) []decide.Turn {
	if wanted <= 0 || cand.ChannelID == 0 {
		return nil
	}
	newestFirst := getChannelMessageHistory(cand.ChannelID).snapshotBefore(0, maxCachedHistoryMessages)

	turns := make([]decide.Turn, 0, wanted)
	for _, message := range newestFirst {
		if message.ID == cand.SnowflakeID {
			continue // the candidate is passed separately
		}
		if cand.BotID != 0 && message.Author.ID == cand.BotID {
			break // the bot's last reply; everything older is in the cached history
		}
		role := decide.RoleUser
		if message.Author.Bot {
			role = decide.RoleBot
		}
		turns = append(turns, decide.Turn{
			Role:    role,
			Name:    message.Author.EffectiveName(),
			Content: decide.CleanTurn(role, message.Content),
		})
	}

	// The snapshot is newest first
	slices.Reverse(turns)
	if len(turns) > wanted {
		turns = turns[len(turns)-wanted:]
	}
	return turns
}

// botNamesForDecision lists the names that reach the bot, used to tell the
// decision model which messages are for it.
func botNamesForDecision(cache *db.ChannelCache) []string {
	names := []string{"x3", "clanker"}
	if cache != nil {
		// BotName is the operator override; an empty one means the platform
		// display name is used at runtime, which the model does not need.
		if name := strings.TrimSpace(cache.PersonaMeta.BotName); name != "" {
			names = append(names, name)
		}
		if name := strings.TrimSpace(cache.PersonaMeta.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cacheHistoryForContinuation(cache *db.ChannelCache) []llm.Message {
	if cache == nil {
		return nil
	}
	if cache.ImportedHistory != nil {
		return cache.ImportedHistory.Messages
	}
	if cache.Llmer != nil {
		return cache.Llmer.Messages
	}
	return nil
}
