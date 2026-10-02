package commands

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/decide"
	"github.com/zeozeozeo/x3/llm"
)

// candidate describes the message the respond decision is being made about.
type candidate struct {
	// ID keys the verdict cache so one message is never judged twice.
	ID string
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
