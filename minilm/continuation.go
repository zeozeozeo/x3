package minilm

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zeozeozeo/x3/llm"
)

type DecisionInput struct {
	Enabled         bool
	Now             time.Time
	LastInteraction time.Time
	Candidate       string
	History         []llm.Message
	Config          Config
	Embedder        Embedder
}

type Decision struct {
	Trigger bool
	Reason  string
	Score   float32
}

var onlyURLRegexp = regexp.MustCompile(`(?is)^\s*(?:https?://\S+\s*)+$`)

var embeddingCache = struct {
	mu      sync.Mutex
	entries map[string][]float32
}{
	entries: make(map[string][]float32),
}

var defaultContinuationPrompts = []string{
	"are you listening to me",
	"are you there",
	"are you still there",
	"can you hear me",
	"did you hear me",
	"did you see my message",
	"please answer me",
	"answer me",
	"respond to me",
	"why are you ignoring me",
	"hello are you there",
}

func ShouldTrigger(input DecisionInput) Decision {
	if !input.Enabled {
		return Decision{Reason: "disabled"}
	}
	cfg := input.Config
	if cfg.GraceWindow == 0 {
		cfg = LoadConfig()
	}
	if cfg.DefaultSimilarity == 0 {
		cfg.DefaultSimilarity = defaultDefaultSimilarity
	}
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	if input.LastInteraction.IsZero() {
		return Decision{Reason: "no_last_interaction"}
	}

	elapsed := input.Now.Sub(input.LastInteraction)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed <= cfg.GraceWindow {
		return Decision{Trigger: true, Reason: "grace_window"}
	}
	if elapsed > cfg.ContinuationWindow {
		return Decision{Reason: "outside_window"}
	}

	candidate := cleanCandidate(input.Candidate)
	if len([]rune(candidate)) < 3 || onlyURLRegexp.MatchString(candidate) {
		return Decision{Reason: "empty_or_low_signal"}
	}

	embedder := input.Embedder
	if embedder == nil {
		var err error
		embedder, err = GlobalEmbedder()
		if err != nil {
			slog.Warn("MiniLM continuation check skipped", "err", err)
			return Decision{Reason: "minilm_unavailable"}
		}
	}

	candidateEmbedding, err := embedCached(embedder, candidate)
	if err != nil {
		slog.Warn("MiniLM candidate embedding failed", "err", err)
		return Decision{Reason: "embedding_failed"}
	}

	var best float32
	refs := continuationReferences(input.History)
	for _, ref := range refs {
		refEmbedding, err := embedCached(embedder, ref)
		if err != nil {
			slog.Warn("MiniLM reference embedding failed", "err", err)
			continue
		}
		if score := Cosine(candidateEmbedding, refEmbedding); score > best {
			best = score
		}
	}

	if best >= cfg.Similarity {
		return Decision{Trigger: true, Reason: "similarity", Score: best}
	}

	var bestDefault float32
	for _, ref := range defaultContinuationPrompts {
		refEmbedding, err := embedCached(embedder, ref)
		if err != nil {
			slog.Warn("MiniLM default continuation embedding failed", "err", err)
			continue
		}
		if score := Cosine(candidateEmbedding, refEmbedding); score > bestDefault {
			bestDefault = score
		}
	}
	if bestDefault >= cfg.DefaultSimilarity {
		return Decision{Trigger: true, Reason: "default_similarity", Score: bestDefault}
	}

	if len(refs) == 0 {
		return Decision{Reason: "no_reference", Score: bestDefault}
	}
	return Decision{Reason: "below_threshold", Score: best}
}

func embedCached(embedder Embedder, text string) ([]float32, error) {
	if _, ok := embedder.(*Model); !ok {
		embedding, err := embedder.Embed(text)
		if err != nil {
			return nil, err
		}
		return NormalizeVector(embedding), nil
	}

	embeddingCache.mu.Lock()
	if cached, ok := embeddingCache.entries[text]; ok {
		out := append([]float32(nil), cached...)
		embeddingCache.mu.Unlock()
		return out, nil
	}
	embeddingCache.mu.Unlock()

	embedding, err := embedder.Embed(text)
	if err != nil {
		return nil, err
	}
	embedding = NormalizeVector(append([]float32(nil), embedding...))

	embeddingCache.mu.Lock()
	if len(embeddingCache.entries) > 512 {
		embeddingCache.entries = make(map[string][]float32)
	}
	embeddingCache.entries[text] = append([]float32(nil), embedding...)
	embeddingCache.mu.Unlock()

	return embedding, nil
}

func cleanCandidate(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func continuationReferences(history []llm.Message) []string {
	if len(history) == 0 {
		return nil
	}
	lastAssistant := -1
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleAssistant && strings.TrimSpace(history[i].Content) != "" {
			lastAssistant = i
			break
		}
	}
	if lastAssistant == -1 {
		return nil
	}

	assistant := cleanCandidate(history[lastAssistant].Content)
	var refs []string
	if assistant != "" {
		refs = append(refs, assistant)
	}

	lastUser := ""
	for i := lastAssistant - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser && strings.TrimSpace(history[i].Content) != "" {
			lastUser = cleanCandidate(history[i].Content)
			break
		}
	}
	if lastUser != "" {
		refs = append(refs, lastUser)
		if assistant != "" {
			refs = append(refs, lastUser+"\n"+assistant)
		}
	}
	return refs
}
