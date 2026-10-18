package worldmem

import (
	"context"
	"log/slog"
	"sort"
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
		if !EmbeddingsAvailable() {
			// No embedding account: x3 simply does not accumulate new world
			// facts. That is a configuration choice, not a failure.
			slog.Debug("worldmem store skipped: no embedding accounts configured", "channel", channelKey)
			return
		}
		slog.Warn("worldmem store: embedding failed", "channel", channelKey, "err", err)
		return
	}

	stored := 0
	for i, u := range units {
		vec := vecs[i]
		if len(vec) == 0 {
			continue
		}
		if dedup(ctx, channelKey, vec) {
			continue
		}
		if _, err := db.InsertWorldEntry(channelKey, u.text, vec, EstimateTokens(u.text), u.parentID, len(vec)); err != nil {
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

// Retrieve returns up to DefaultTopK world facts from this channel for prompt
// injection, ranked by BM25 lexical match.
//
// Retrieval is lexical-only, deliberately. BM25 is free, local and
// deterministic, it needs no network call, and world facts are built from
// exactly the rare words people use when they ask about them. It also means
// reading memories never competes with chat for embedding quota.
//
// The consequence is deliberate too: a message that shares no content token
// with any stored fact retrieves nothing, rather than guessing at a near match.
// Embeddings are still produced when facts are *stored*, because they power
// duplicate detection.
func Retrieve(channelKey, query string) []RetrieveResult {
	if strings.TrimSpace(channelKey) == "" || strings.TrimSpace(query) == "" {
		return nil
	}
	// A message with no content tokens has no intent to recall anything
	// ("ok", "?!"), so do not push canon at pure chatter.
	if len(Tokenize(query)) == 0 {
		return nil
	}

	entries, err := db.GetWorldEntries(channelKey)
	if err != nil {
		slog.Warn("worldmem retrieve: listing facts failed", "channel", channelKey, "err", err)
		return nil
	}
	if len(entries) == 0 {
		return nil
	}

	out := selectLexical(entries, query)
	if len(out) == 0 {
		slog.Debug("worldmem retrieve: no lexical match",
			"channel", channelKey, "candidates", len(entries))
		return nil
	}
	slog.Debug("worldmem retrieve: lexical match",
		"channel", channelKey, "candidates", len(entries), "kept", len(out))
	touchRetrieved(out)
	return out
}

// selectLexical ranks facts by BM25 score over query. Facts sharing no token
// with the query are dropped, so an empty result means "no lexical signal".
func selectLexical(entries []db.WorldEntry, query string) []RetrieveResult {
	docs := make([]string, len(entries))
	for i, e := range entries {
		docs[i] = e.Text
	}
	ix := NewLexicalIndex(docs)
	scores := ix.Scores(query)

	type scored struct {
		idx   int
		score float64
	}
	ranked := make([]scored, 0, len(entries))
	for i, s := range scores {
		if s > 0 {
			ranked = append(ranked, scored{i, s})
		}
	}
	// Highest score first; ties go to the newer fact, which is usually the one
	// the conversation is actually about.
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].score != ranked[b].score {
			return ranked[a].score > ranked[b].score
		}
		return entries[ranked[a].idx].ID > entries[ranked[b].idx].ID
	})

	var out []RetrieveResult
	usedTokens := 0
	for _, r := range ranked {
		e := entries[r.idx]
		tokens := entryTokens(e)
		if len(out) > 0 && usedTokens+tokens > DefaultMaxTokens {
			break
		}
		usedTokens += tokens
		out = append(out, RetrieveResult{Text: e.Text, ID: e.ID})
		if len(out) >= DefaultTopK {
			break
		}
	}
	return out
}

// entryTokens prefers the stored token estimate and falls back to estimating.
func entryTokens(e db.WorldEntry) int {
	if e.Tokens > 0 {
		return e.Tokens
	}
	return EstimateTokens(e.Text)
}

func touchRetrieved(out []RetrieveResult) {
	ids := make([]int64, 0, len(out))
	for _, r := range out {
		ids = append(ids, r.ID)
	}
	if err := db.TouchWorldEntries(ids); err != nil {
		slog.Warn("worldmem retrieve: recording usage failed", "err", err)
	}
}
