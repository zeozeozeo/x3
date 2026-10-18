// Package worldmem implements the world-memory RAG system for x3.
//
// As roleplay goes on, the LLM emits <world>...</world> blocks describing one
// lasting world fact each (world event, character detail, location, item,
// rule of the setting). Those facts are embedded with a Cloudflare Workers AI
// embedding model and stored per-channel in SQLite (sqlite-vec). On later
// messages the top matches are retrieved and injected into the prompt as a
// world bible.
//
// Memories never leave their channel: every query is scoped by channel_key.
package worldmem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/zeozeozeo/x3/model"
)

const (
	// DefaultTopK is how many facts one retrieval may inject.
	DefaultTopK = 5
	// DefaultMaxTokens budgets retrieved facts so they cannot crowd out the
	// conversation.
	DefaultMaxTokens = 1000

	// EmbedTimeout bounds a single embedding account attempt.
	EmbedTimeout = 30 * time.Second

	// MaxWorldRunes caps a single stored fact.
	MaxWorldRunes = 2000
	// MaxWorldsPerResponse caps how many facts one response can store.
	MaxWorldsPerResponse = 32

	// ChunkTargetTokens / ChunkOverlapTokens apply only to overlong facts.
	ChunkTargetTokens  = 450
	ChunkOverlapTokens = 75

	// DedupSimilarity skips storing a fact this similar to an existing one.
	DedupSimilarity = 0.95
)

var httpClient = &http.Client{Timeout: EmbedTimeout}

// embedCache avoids re-embedding identical texts.
var (
	embedCacheMu sync.Mutex
	embedCache   = map[string][]float32{}
)

func cacheGet(text string) ([]float32, bool) {
	embedCacheMu.Lock()
	defer embedCacheMu.Unlock()
	v, ok := embedCache[text]
	return v, ok
}

func cacheSet(text string, vec []float32) {
	embedCacheMu.Lock()
	defer embedCacheMu.Unlock()
	if len(embedCache) > 5000 {
		embedCache = map[string][]float32{}
	}
	embedCache[text] = vec
}

type embeddingResponse struct {
	Result struct {
		Data [][]float32 `json:"data"`
	} `json:"result"`
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// EmbeddingsAvailable reports whether at least one embedding account is
// configured.
//
// Retrieval does not depend on it: lexical matching is local and needs no
// network. Without an account x3 simply cannot store new facts or run the
// semantic fallback, and it stays quiet about that instead of failing.
func EmbeddingsAvailable() bool {
	baseUrls, tokens, _ := model.WorldMemEmbeddingClients()
	return len(baseUrls) > 0 && len(tokens) > 0
}

// Embed returns one normalized embedding per input text, trying every
// configured Cloudflare account in order like llm.go does for chat.
func Embed(ctx context.Context, texts []string) ([][]float32, error) {
	cacheModel := strings.TrimSpace(model.WorldMemEmbeddingModel)
	if cacheModel == "" {
		cacheModel = model.DefaultWorldMemEmbeddingModel
	}

	cached := make([][]float32, len(texts))
	var missing []string
	var missingIdx []int
	for i, t := range texts {
		if v, ok := cacheGet(t + "\x00" + cacheModel); ok {
			cached[i] = v
		} else {
			missing = append(missing, t)
			missingIdx = append(missingIdx, i)
		}
	}
	if len(missing) == 0 {
		return cached, nil
	}

	baseUrls, tokens, apiModel := model.WorldMemEmbeddingClients()
	if len(baseUrls) == 0 || len(tokens) == 0 {
		return nil, fmt.Errorf("worldmem: no cloudflare embedding clients configured (need X3_CLOUDFLARE_WORKERS_AI_TOKEN and X3_CLOUDFLARE_ACCOUNT_ID)")
	}

	payload, err := json.Marshal(map[string]any{"text": missing})
	if err != nil {
		return nil, fmt.Errorf("worldmem: encode embedding request: %w", err)
	}

	var lastErr error
	for i, baseUrl := range baseUrls {
		token := ""
		if i < len(tokens) {
			token = tokens[i]
		} else if len(tokens) > 0 {
			token = tokens[0]
		}
		vecs, err := postEmbed(ctx, baseUrl, token, payload, len(missing))
		if err == nil {
			for j, v := range vecs {
				cached[missingIdx[j]] = v
				cacheSet(missing[j]+"\x00"+cacheModel, v)
			}
			slog.Info("worldmem embedded", "texts", len(missing), "model", apiModel, "account", i+1, "accounts", len(baseUrls))
			return cached, nil
		}
		lastErr = err
		model.RecordTokenError(token)
		slog.Warn("worldmem embedding failed, trying next account", "account", i+1, "accounts", len(baseUrls), "error", err)
	}
	return nil, fmt.Errorf("worldmem: all %d embedding accounts failed: %w", len(baseUrls), lastErr)
}

// EmbedOne embeds a single text.
func EmbedOne(ctx context.Context, text string) ([]float32, error) {
	vecs, err := Embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("worldmem: empty embedding response")
	}
	return vecs[0], nil
}

func postEmbed(ctx context.Context, baseUrl, token string, payload []byte, want int) ([][]float32, error) {
	ctx, cancel := context.WithTimeout(ctx, EmbedTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseUrl, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 256))
	}

	var out embeddingResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if !out.Success && len(out.Result.Data) == 0 {
		msgs := make([]string, 0, len(out.Errors))
		for _, e := range out.Errors {
			msgs = append(msgs, e.Message)
		}
		if len(msgs) == 0 {
			msgs = append(msgs, "provider reported failure")
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	if len(out.Result.Data) != want {
		return nil, fmt.Errorf("expected %d embeddings, got %d", want, len(out.Result.Data))
	}
	for i, v := range out.Result.Data {
		if len(v) == 0 {
			return nil, fmt.Errorf("embedding %d is empty", i)
		}
		out.Result.Data[i] = Normalize(v)
	}
	return out.Result.Data, nil
}

// Normalize returns the unit-length copy of v.
func Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := math.Sqrt(sum)
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

// Cosine returns cosine similarity in [-1, 1]. Vectors from Embed are already
// normalized; this normalizes defensively.
func Cosine(a, b []float32) float64 {
	n := min(len(b), len(a))
	var dot, na, nb float64
	for i := range n {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// topScored ranks candidate indices by similarity to query, best first.
func topScored(query []float32, candidates [][]float32) []int {
	type scored struct {
		idx int
		sim float64
	}
	scoredList := make([]scored, 0, len(candidates))
	for i, c := range candidates {
		scoredList = append(scoredList, scored{i, Cosine(query, c)})
	}
	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].sim > scoredList[j].sim })
	out := make([]int, len(scoredList))
	for i, s := range scoredList {
		out[i] = s.idx
	}
	return out
}

// Rank orders candidate indices best-first. Exported for fallback paths.
func Rank(query []float32, candidates [][]float32) []int {
	return topScored(query, candidates)
}

// NormalizeWorld cleans one raw <world> block for storage.
func NormalizeWorld(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MaxWorldRunes {
		runes := []rune(s)
		s = string(runes[:MaxWorldRunes])
	}
	return strings.TrimSpace(s)
}

// ChunkWorld splits an overlong fact into overlapping chunks. Normal facts
// (the common case: one <world> block = one atomic fact) pass through as-is.
func ChunkWorld(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// Rough token estimate: ~4 chars per token.
	if len(text) <= ChunkTargetTokens*4 {
		return []string{text}
	}
	target := ChunkTargetTokens * 4
	overlap := ChunkOverlapTokens * 4
	sentences := splitSentences(text)
	var chunks []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimSpace(cur.String())
		if s != "" {
			chunks = append(chunks, s)
		}
		cur.Reset()
	}
	for _, s := range sentences {
		if cur.Len()+len(s)+1 > target && cur.Len() > 0 {
			// Overlap: carry the tail of the current chunk forward.
			tail := cur.String()
			flush()
			if len(tail) > overlap {
				tail = tail[len(tail)-overlap:]
				if idx := strings.Index(tail, " "); idx != -1 {
					tail = tail[idx+1:]
				}
				cur.WriteString(tail + " ")
			}
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(s)
	}
	flush()
	if len(chunks) == 0 {
		return []string{text}
	}
	return chunks
}

func splitSentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c == '.' || c == '!' || c == '?' || c == '\n' {
			end := i + 1
			// Don't split decimals / abbreviations mid-word: require the next
			// char to be whitespace or end of string.
			if end < len(text) && text[end] != ' ' && text[end] != '\n' && text[end] != '\t' {
				continue
			}
			part := strings.TrimSpace(text[start:end])
			if part != "" {
				out = append(out, part)
			}
			start = end
		}
	}
	if rest := strings.TrimSpace(text[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// FormatBible renders retrieved facts as a message-tail suffix. It is
// appended to the last outgoing message (not the system prompt) so prompt
// prefix caching stays effective.
func FormatBible(entries []string) string {
	var facts []string
	for _, e := range entries {
		if t := strings.TrimSpace(e); t != "" {
			facts = append(facts, t)
		}
	}
	if len(facts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n**World bible (established canon, stay consistent with it):**\n")
	for _, f := range facts {
		sb.WriteString("- " + f + "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// EstimateTokens is a rough token count for budgeting retrieved context.
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	if t := n / 4; t > 0 {
		return t
	}
	return 1
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
