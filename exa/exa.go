// Package exa implements the Exa web search API.
package exa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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
)

// ErrNotConfigured is returned when no Exa API key is set.
var ErrNotConfigured = errors.New("exa: no API key configured")

// Result is a single Exa search result. Snippet is built from the returned
// highlights and capped to Config.SnippetChars so the prompt stays small.
type Result struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	PublishedDate string   `json:"publishedDate,omitempty"`
	Highlights    []string `json:"highlights,omitempty"`
	Snippet       string   `json:"-"`
}

// Config controls the Exa client.
type Config struct {
	APIKeys      []string
	BaseURL      string
	Timeout      time.Duration
	SearchType   string
	SnippetChars int
}

// ConfigFromEnv reads the Exa settings from the environment.
func ConfigFromEnv() Config {
	cfg := Config{
		APIKeys:      envList("X3_EXA_API_KEY"),
		BaseURL:      strings.TrimSpace(os.Getenv("X3_EXA_BASE_URL")),
		Timeout:      envDuration("X3_EXA_TIMEOUT", defaultTimeout),
		SearchType:   strings.TrimSpace(os.Getenv("X3_EXA_SEARCH_TYPE")),
		SnippetChars: envInt("X3_EXA_SNIPPET_CHARS", defaultSnippetChars),
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
	MaxCharacters int `json:"maxCharacters"`
}

type searchResponse struct {
	Results []Result `json:"results"`
	Error   string   `json:"error"`
	Tag     string   `json:"tag"`
}

var httpClient = &http.Client{}

// Search runs a search against Exa. It only requests titles, URLs, and a
// capped set of highlights: full text, summaries, and extras are never
// requested, which keeps token usage close to the DuckDuckGo fallback.
func Search(ctx context.Context, cfg Config, query string, numResults int) ([]Result, error) {
	if !cfg.Enabled() {
		return nil, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("exa: empty query")
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

	payload, err := json.Marshal(searchRequest{
		Query:      query,
		Type:       cfg.SearchType,
		NumResults: numResults,
		Contents: contentsOptions{
			Highlights: highlightsOptions{MaxCharacters: cfg.SnippetChars},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("exa: encode request: %w", err)
	}

	var lastErr error
	for _, key := range cfg.APIKeys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/search", bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("exa: new request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", key)
		req.Header.Set("User-Agent", "x3")

		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("exa: request failed: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("exa: read response: %w", readErr)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			httpErr := &HTTPError{StatusCode: resp.StatusCode, Message: errorMessage(body)}
			lastErr = httpErr
			if isKeyFailure(resp.StatusCode) {
				continue
			}
			return nil, httpErr
		}

		var out searchResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return nil, fmt.Errorf("exa: decode response: %w", err)
		}
		for i := range out.Results {
			out.Results[i].Snippet = buildSnippet(out.Results[i].Highlights, cfg.SnippetChars)
		}
		return out.Results, nil
	}

	if lastErr == nil {
		lastErr = ErrNotConfigured
	}
	return nil, lastErr
}

func buildSnippet(highlights []string, maxChars int) string {
	var parts []string
	for _, highlight := range highlights {
		highlight = strings.Join(strings.Fields(highlight), " ")
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
