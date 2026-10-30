package exa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSearchRequestAndSnippet(t *testing.T) {
	t.Parallel()

	var (
		mu             sync.Mutex
		got            searchRequest
		gotKey         string
		gotContentType string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body searchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		got = body
		gotKey = r.Header.Get("x-api-key")
		gotContentType = r.Header.Get("Content-Type")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"One","url":"https://one.example","highlights":["  hello   world  ","second part"]},
			{"title":"Two","url":"https://two.example"}
		]}`))
	}))
	defer server.Close()

	cfg := Config{
		APIKeys:      []string{"key-1"},
		BaseURL:      server.URL,
		Timeout:      time.Second,
		SearchType:   "lite",
		SnippetChars: 50,
	}
	results, err := Search(context.Background(), cfg, "test query", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if got.Query != "test query" || got.Type != "lite" || got.NumResults != 3 {
		t.Errorf("unexpected request body: %+v", got)
	}
	if got.Contents.Highlights.MaxCharacters != 50 {
		t.Errorf("highlight cap = %d, want 50", got.Contents.Highlights.MaxCharacters)
	}
	if gotKey != "key-1" {
		t.Errorf("api key = %q, want key-1", gotKey)
	}
	if gotContentType != "application/json" {
		t.Errorf("content type = %q", gotContentType)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Snippet != "hello world second part" {
		t.Errorf("snippet = %q", results[0].Snippet)
	}
	if results[1].Snippet != "" {
		t.Errorf("snippet without highlights = %q", results[1].Snippet)
	}
}

func TestSearchTruncatesSnippet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"title":"Long","url":"https://example.com","highlights":["日本語のテキストがとても長い場合のテストです"]}]}`))
	}))
	defer server.Close()

	cfg := Config{
		APIKeys:      []string{"key"},
		BaseURL:      server.URL,
		Timeout:      time.Second,
		SearchType:   "lite",
		SnippetChars: 10,
	}
	results, err := Search(context.Background(), cfg, "query", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	snippet := results[0].Snippet
	if !utf8.ValidString(snippet) {
		t.Fatalf("snippet is not valid UTF-8: %q", snippet)
	}
	if runes := utf8.RuneCountInString(snippet); runes > 10 {
		t.Fatalf("snippet has %d runes, want <= 10: %q", runes, snippet)
	}
	if !strings.HasSuffix(snippet, "…") {
		t.Fatalf("truncated snippet = %q, want ellipsis", snippet)
	}
}

func TestSearchFailsOverKeys(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		keys []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("x-api-key")
		mu.Lock()
		keys = append(keys, key)
		mu.Unlock()
		if key == "bad-key" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Invalid API key","tag":"INVALID_API_KEY"}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"Ok","url":"https://example.com"}]}`))
	}))
	defer server.Close()

	cfg := Config{
		APIKeys:    []string{"bad-key", "good-key"},
		BaseURL:    server.URL,
		Timeout:    time.Second,
		SearchType: "lite",
	}
	results, err := Search(context.Background(), cfg, "query", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].URL != "https://example.com" {
		t.Fatalf("unexpected results: %+v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) != 2 || keys[0] != "bad-key" || keys[1] != "good-key" {
		t.Fatalf("keys tried = %v", keys)
	}
}

func TestSearchHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","tag":"INTERNAL"}`))
	}))
	defer server.Close()

	cfg := Config{
		APIKeys:    []string{"key"},
		BaseURL:    server.URL,
		Timeout:    time.Second,
		SearchType: "lite",
	}
	_, err := Search(context.Background(), cfg, "query", 1)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %v, want *HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Error(), "boom") || !strings.Contains(httpErr.Error(), "INTERNAL") {
		t.Errorf("error message = %q", httpErr.Error())
	}
}

func TestSearchNotConfigured(t *testing.T) {
	t.Parallel()

	_, err := Search(context.Background(), Config{}, "query", 1)
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func TestSearchUsesDefaultSearchType(t *testing.T) {
	t.Parallel()

	var (
		mu  sync.Mutex
		got searchRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body searchRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got = body
		mu.Unlock()
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	cfg := Config{APIKeys: []string{"key"}, BaseURL: server.URL, Timeout: time.Second}
	if _, err := Search(context.Background(), cfg, "query", 1); err != nil {
		t.Fatalf("Search: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Type != "lite" {
		t.Errorf("search type = %q, want lite", got.Type)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("X3_EXA_API_KEY", "key-1; key-2;")
	t.Setenv("X3_EXA_BASE_URL", "https://example.test/")
	t.Setenv("X3_EXA_SEARCH_TYPE", "auto")
	t.Setenv("X3_EXA_TIMEOUT", "3s")
	t.Setenv("X3_EXA_SNIPPET_CHARS", "123")

	cfg := ConfigFromEnv()
	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "key-1" || cfg.APIKeys[1] != "key-2" {
		t.Errorf("APIKeys = %v", cfg.APIKeys)
	}
	if cfg.BaseURL != "https://example.test" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.SearchType != "auto" {
		t.Errorf("SearchType = %q", cfg.SearchType)
	}
	if cfg.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
	if cfg.SnippetChars != 123 {
		t.Errorf("SnippetChars = %d", cfg.SnippetChars)
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("X3_EXA_API_KEY", "")
	t.Setenv("X3_EXA_BASE_URL", "")
	t.Setenv("X3_EXA_SEARCH_TYPE", "")
	t.Setenv("X3_EXA_TIMEOUT", "")
	t.Setenv("X3_EXA_SNIPPET_CHARS", "")

	cfg := ConfigFromEnv()
	if cfg.Enabled() {
		t.Error("config without a key must be disabled")
	}
	if cfg.BaseURL != defaultBaseURL || cfg.SearchType != "lite" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Timeout != defaultTimeout || cfg.SnippetChars != defaultSnippetChars {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}
