package minilm

import (
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/zeozeozeo/x3/llm"
)

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

func ContinuationScores(embedder Embedder, candidate string, refs []string) (historyScore, defaultScore float32, err error) {
	candidate = Clean(candidate)
	if len([]rune(candidate)) < 3 || onlyURLRegexp.MatchString(candidate) {
		return 0, 0, nil
	}
	if embedder == nil {
		var err error
		embedder, err = GlobalEmbedder()
		if err != nil {
			return 0, 0, err
		}
	}

	candidateEmbedding, err := embedCached(embedder, candidate)
	if err != nil {
		return 0, 0, err
	}

	return bestSimilarity(embedder, candidateEmbedding, refs),
		bestSimilarity(embedder, candidateEmbedding, defaultContinuationPrompts),
		nil
}

// IsLowSignal reports whether a message carries too little to judge, either
// because it is nearly empty or because it is only link dumps.
func IsLowSignal(candidate string) bool {
	candidate = Clean(candidate)
	if candidate == "" {
		return true
	}
	return onlyURLRegexp.MatchString(candidate)
}

func bestSimilarity(embedder Embedder, candidate []float32, refs []string) float32 {
	var best float32
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		refEmbedding, err := embedCached(embedder, ref)
		if err != nil {
			slog.Warn("minilm reference embedding failed", "err", err)
			continue
		}
		if score := Cosine(candidate, refEmbedding); score > best {
			best = score
		}
	}
	return best
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

// Clean normalizes whitespace in a candidate or history message.
func Clean(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// References picks the messages a continuation should be compared against: the
// last assistant reply, the user message that led to it, and the pair joined.
func References(history []llm.Message) []string {
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

	assistant := Clean(history[lastAssistant].Content)
	var refs []string
	if assistant != "" {
		refs = append(refs, assistant)
	}

	lastUser := ""
	for i := lastAssistant - 1; i >= 0; i-- {
		if history[i].Role == llm.RoleUser && strings.TrimSpace(history[i].Content) != "" {
			lastUser = Clean(history[i].Content)
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
