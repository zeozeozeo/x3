package worldmem

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/db"
)

// StoreFacts normalizes, dedupes, chunks, embeds and stores raw <world>
// contents for a channel. It is meant to run in a background goroutine so
// replies stay fast; all failures only log.
func StoreFacts(channelKey string, worlds []string) {
	if strings.TrimSpace(channelKey) == "" || len(worlds) == 0 {
		return
	}
	// Normalize + enforce per-response cap.
	var facts []string
	for _, w := range worlds {
		if n := NormalizeWorld(w); n != "" {
			facts = append(facts, n)
		}
		if len(facts) >= MaxWorldsPerResponse {
			break
		}
	}
	if len(facts) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Chunk (usually a passthrough) and flatten.
	type unit struct {
		text     string
		parentID *int64
	}
	var units []unit
	for _, f := range facts {
		for _, c := range ChunkWorld(f) {
			units = append(units, unit{text: c})
		}
	}
	texts := make([]string, len(units))
	for i, u := range units {
		texts[i] = u.text
	}
	vecs, err := Embed(ctx, texts)
	if err != nil {
		slog.Warn("worldmem store: embedding failed", "channel", channelKey, "err", err)
		return
	}

	stored := 0
	for i, u := range units {
		vec := vecs[i]
		if len(vec) != WorldEmbeddingDim {
			slog.Warn("worldmem store: dim mismatch, skipping index rebuild",
				"got", len(vec), "want", WorldEmbeddingDim)
			continue
		}
		if dedup(ctx, channelKey, vec) {
			continue
		}
		if _, err := db.InsertWorldEntry(channelKey, u.text, vec, EstimateTokens(u.text), u.parentID, WorldEmbeddingDim); err != nil {
			slog.Warn("worldmem store: insert failed", "channel", channelKey, "err", err)
			continue
		}
		stored++
	}
	slog.Info("worldmem stored facts", "channel", channelKey, "found", len(facts), "stored", stored)
}

// dedup reports whether a near-identical fact already exists in this channel.
// Vectors are unit length, so cosine similarity >= DedupSimilarity is
// equivalent to L2 distance <= sqrt(2*(1-DedupSimilarity)).
func dedup(ctx context.Context, channelKey string, vec []float32) bool {
	hits, err := db.SearchWorldSimilar(channelKey, vec, 3)
	if err != nil {
		return false
	}
	maxDist := 0.32 // sqrt(2*(1-0.95)) ≈ 0.316
	for _, h := range hits {
		if h.Distance <= maxDist {
			return true
		}
	}
	return false
}

// RetrieveResult is one fact chosen for prompt injection.
type RetrieveResult struct {
	Text string
	ID   int64
}

// Retrieve embeds the query and returns up to DefaultTopK facts from this
// channel only, gated by similarity and token budget. On embedding failure it
// falls back to recent entries from the same channel. It never returns
// another channel's memories.
func Retrieve(ctx context.Context, channelKey, query string) []RetrieveResult {
	if strings.TrimSpace(channelKey) == "" || strings.TrimSpace(query) == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, RetrieveTimeout)
	defer cancel()

	if len(query) > 2000 {
		query = query[:2000]
	}
	qvec, err := EmbedOne(ctx, query)
	if err != nil {
		slog.Warn("worldmem retrieve: embedding failed, using recency fallback", "channel", channelKey, "err", err)
		return recentFallback(channelKey, 3)
	}

	hits, err := db.SearchWorldSimilar(channelKey, qvec, DefaultFetchK)
	if err != nil {
		slog.Warn("worldmem retrieve: search failed", "channel", channelKey, "err", err)
		return recentFallback(channelKey, 3)
	}

	var out []RetrieveResult
	usedTokens := 0
	for _, h := range hits {
		// L2 distance on unit vectors -> cosine: cos = 1 - dist^2/2.
		sim := 1 - (h.Distance*h.Distance)/2
		if sim < DefaultMinSimilarity {
			continue
		}
		tokens := EstimateTokens(h.Entry.Text)
		if h.Entry.Tokens > 0 {
			tokens = h.Entry.Tokens
		}
		if usedTokens+tokens > DefaultMaxTokens && len(out) > 0 {
			break
		}
		usedTokens += tokens
		out = append(out, RetrieveResult{Text: h.Entry.Text, ID: h.Entry.ID})
		if len(out) >= DefaultTopK {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	var ids []int64
	for _, r := range out {
		ids = append(ids, r.ID)
	}
	_ = db.TouchWorldEntries(ids)
	return out
}

func recentFallback(channelKey string, n int) []RetrieveResult {
	entries, err := db.GetRecentWorldEntries(channelKey, n)
	if err != nil || len(entries) == 0 {
		return nil
	}
	out := make([]RetrieveResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, RetrieveResult{Text: e.Text, ID: e.ID})
	}
	return out
}
