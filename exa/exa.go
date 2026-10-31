// Package exa implements the Exa web search API.
package exa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	defaultBaseURL      = "https://api.exa.ai"
	defaultSearchType   = "lite"
	defaultTimeout      = 6 * time.Second
	defaultSnippetChars = 400
	maxResponseBytes    = 4 << 20
	maxErrorRunes       = 300
	maxNoticeRunes      = 240
	maxNotices          = 3
)

// ErrNotConfigured is returned when no Exa API key is set.
var ErrNotConfigured = errors.New("exa: no API key configured")

// Result is a single Exa search result. Snippet is built from the returned
// highlights and capped to Config.SnippetChars so the prompt stays small.
type Result struct {
	Title           string    `json:"title"`
	URL             string    `json:"url"`
	PublishedDate   string    `json:"publishedDate,omitempty"`
	Author          string    `json:"author,omitempty"`
	Highlights      []string  `json:"highlights,omitempty"`
	HighlightScores []float64 `json:"highlightScores,omitempty"`
	Snippet         string    `json:"-"`
}

// Notice is a machine-readable note about how Exa produced a response.
// Notices are written by Exa to be shown to a model; warnings are not.
type Notice struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// SearchOutput is the result of a single Exa search.
type SearchOutput struct {
	Results  []Result
	Notices  []string
	Warnings []string
	CostUSD  float64
	SearchMS float64
}

// Config controls the Exa client.
type Config struct {
	APIKeys      []string
	BaseURL      string
	Timeout      time.Duration
	SearchType   string
	SnippetChars int
	// HighlightQuery steers which page text Exa highlights towards the search
	// query. Not every search type accepts it, so a rejection disables it for
	// the rest of the process.
	HighlightQuery bool
}

// ConfigFromEnv reads the Exa settings from the environment.
func ConfigFromEnv() Config {
	cfg := Config{
		APIKeys:        envList("X3_EXA_API_KEY"),
		BaseURL:        strings.TrimSpace(os.Getenv("X3_EXA_BASE_URL")),
		Timeout:        envDuration("X3_EXA_TIMEOUT", defaultTimeout),
		SearchType:     strings.TrimSpace(os.Getenv("X3_EXA_SEARCH_TYPE")),
		SnippetChars:   envInt("X3_EXA_SNIPPET_CHARS", defaultSnippetChars),
		HighlightQuery: envBool("X3_EXA_HIGHLIGHT_QUERY", false),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.SearchType == "" {
		cfg.SearchType = defaultSearchType
	}
	if cfg.SnippetChars <= 0 {
		cfg.SnippetChars = defaultSnippetChars
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return cfg
}

// Enabled reports whether at least one API key is configured.
func (c Config) Enabled() bool {
	for _, key := range c.APIKeys {
		if strings.TrimSpace(key) != "" {
			return true
		}
	}
	return false
}

// HTTPError describes a non-successful Exa response.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("exa: HTTP %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("exa: HTTP %d", e.StatusCode)
}

type searchRequest struct {
	Query      string          `json:"query"`
	Type       string          `json:"type"`
	NumResults int             `json:"numResults"`
	Contents   contentsOptions `json:"contents"`
}

type contentsOptions struct {
	Highlights highlightsOptions `json:"highlights"`
}

type highlightsOptions struct {
	Query         string `json:"query,omitempty"`
	MaxCharacters int    `json:"maxCharacters"`
}

type searchResponse struct {
	Results    []Result `json:"results"`
	Notices    []Notice `json:"notices"`
	Warnings   []Notice `json:"warnings"`
	CostDollars struct {
		Total float64 `json:"total"`
	} `json:"costDollars"`
	SearchTime float64 `json:"searchTime"`
	Error      string  `json:"error"`
	Tag        string  `json:"tag"`
}

var httpClient = &http.Client{}

// highlightQueryUnsupported is set once Exa rejects contents.highlights.query
// so later searches skip it instead of paying for a failed request each time.
var highlightQueryUnsupported atomic.Bool

// Search runs a search against Exa. It only requests titles, URLs, and a
// capped set of highlights: full text, summaries, and extras are never
// requested, which keeps token usage close to the DuckDuckGo fallback.
func Search(ctx context.Context, cfg Config, query string, numResults int) (SearchOutput, error) {
	if !cfg.Enabled() {
		return SearchOutput{}, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return SearchOutput{}, errors.New("exa: empty query")
	}
	if numResults <= 0 {
		numResults = 10
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	if cfg.SnippetChars <= 0 {
		cfg.SnippetChars = defaultSnippetChars
	}
	if cfg.SearchType == "" {
		cfg.SearchType = defaultSearchType
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	highlightQuery := cfg.HighlightQuery && !highlightQueryUnsupported.Load()

	var lastErr error
	for _, key := range cfg.APIKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		status, out, err := searchOnce(ctx, baseURL, key, cfg, query, numResults, highlightQuery)
		if err == nil {
			return out, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return SearchOutput{}, ctxErr
		}
		if status == http.StatusBadRequest && highlightQuery {
			// The search type may not accept a guided highlight query; retry
			// once without it and stop sending it for the rest of the process.
			slog.Warn("exa rejected guided highlights, disabling highlights.query", "search_type", cfg.SearchType, "err", err)
			highlightQueryUnsupported.Store(true)
			highlightQuery = false
			status, out, err = searchOnce(ctx, baseURL, key, cfg, query, numResults, false)
			if err == nil {
				return out, nil
			}
		}

		lastErr = err
		if isKeyFailure(status) {
			continue
		}
		return SearchOutput{}, err
	}

	if lastErr == nil {
		lastErr = ErrNotConfigured
	}
	return SearchOutput{}, lastErr
}

// searchOnce performs one request with one key. It returns the HTTP status so
// the caller can decide whether the key or the request is at fault.
func searchOnce(
	ctx context.Context,
	baseURL, key string,
	cfg Config,
	query string,
	numResults int,
	highlightQuery bool,
) (int, SearchOutput, error) {
	highlights := highlightsOptions{MaxCharacters: cfg.SnippetChars}
	if highlightQuery {
		highlights.Query = query
	}
	payload, err := json.Marshal(searchRequest{
		Query:      query,
		Type:       cfg.SearchType,
		NumResults: numResults,
		Contents:   contentsOptions{Highlights: highlights},
	})
	if err != nil {
		return 0, SearchOutput{}, fmt.Errorf("exa: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/search", bytes.NewReader(payload))
	if err != nil {
		return 0, SearchOutput{}, fmt.Errorf("exa: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("User-Agent", "x3")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, SearchOutput{}, fmt.Errorf("exa: request failed: %w", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	resp.Body.Close()
	if readErr != nil {
		return resp.StatusCode, SearchOutput{}, fmt.Errorf("exa: read response: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, SearchOutput{}, &HTTPError{StatusCode: resp.StatusCode, Message: errorMessage(body)}
	}

	var out searchResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return resp.StatusCode, SearchOutput{}, fmt.Errorf("exa: decode response: %w", err)
	}
	for i := range out.Results {
		out.Results[i].Snippet = buildSnippet(out.Results[i].Highlights, out.Results[i].HighlightScores, cfg.SnippetChars)
	}
	result := SearchOutput{
		Results:  out.Results,
		Notices:  noticeMessages(out.Notices),
		Warnings: noticeMessages(out.Warnings),
		CostUSD:  out.CostDollars.Total,
		SearchMS: out.SearchTime,
	}
	return resp.StatusCode, result, nil
}

func noticeMessages(notices []Notice) []string {
	messages := make([]string, 0, len(notices))
	for _, notice := range notices {
		message := strings.Join(strings.Fields(notice.Message), " ")
		if message == "" {
			continue
		}
		messages = append(messages, truncateRunes(message, maxNoticeRunes))
		if len(messages) == maxNotices {
			break
		}
	}
	return messages
}

// buildSnippet joins the returned highlights, preferring the ones Exa scored
// as most relevant, and caps the result so the prompt stays small.
func buildSnippet(highlights []string, scores []float64, maxChars int) string {
	ordered := make([]int, 0, len(highlights))
	for i := range highlights {
		ordered = append(ordered, i)
	}
	if len(scores) == len(highlights) {
		sort.SliceStable(ordered, func(a, b int) bool {
			return scores[ordered[a]] > scores[ordered[b]]
		})
	}

	var parts []string
	for _, i := range ordered {
		highlight := strings.Join(strings.Fields(highlights[i]), " ")
		if highlight != "" {
			parts = append(parts, highlight)
		}
	}
	return truncateRunes(strings.Join(parts, " "), maxChars)
}

func truncateRunes(s string, maxChars int) string {
	if maxChars <= 0 || utf8.RuneCountInString(s) <= maxChars {
		return s
	}
	runes := []rune(s)
	if maxChars == 1 {
		return "…"
	}
	return strings.TrimRight(string(runes[:maxChars-1]), " ") + "…"
}

func errorMessage(body []byte) string {
	var envelope struct {
		Error string `json:"error"`
		Tag   string `json:"tag"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error != "" {
		msg := envelope.Error
		if envelope.Tag != "" {
			msg += " (" + envelope.Tag + ")"
		}
		return truncateRunes(msg, maxErrorRunes)
	}
	msg := strings.Join(strings.Fields(string(body)), " ")
	return truncateRunes(msg, maxErrorRunes)
}

func isKeyFailure(status int) bool {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusPaymentRequired, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func envList(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}
