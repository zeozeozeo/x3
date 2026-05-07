package commands

import (
	"log/slog"
	"time"

	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/minilm"
)

func shouldTriggerContinuation(cache *db.ChannelCache, content string) bool {
	if cache == nil {
		return false
	}
	cfg := minilm.LoadConfig()
	now := time.Now()
	if !cache.LastInteraction.IsZero() && now.Sub(cache.LastInteraction) <= cfg.GraceWindow {
		return true
	}
	if !cache.PersonaMeta.EnableMiniLMContinuations {
		return false
	}

	decision := minilm.ShouldTrigger(minilm.DecisionInput{
		Enabled:         true,
		Now:             now,
		LastInteraction: cache.LastInteraction,
		Candidate:       content,
		History:         cacheHistoryForContinuation(cache),
		Config:          cfg,
	})
	slog.Info("continuation trigger decision", "trigger", decision.Trigger, "reason", decision.Reason, "score", decision.Score)
	return decision.Trigger
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
