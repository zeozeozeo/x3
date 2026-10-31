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
	out, err := Search(context.Background(), cfg, "test query", 3)
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
	if len(out.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(out.Results))
	}
	if out.Results[0].Snippet != "hello world second part" {
		t.Errorf("snippet = %q", out.Results[0].Snippet)
	}
	if out.Results[1].Snippet != "" {
		t.Errorf("snippet without highlights = %q", out.Results[1].Snippet)
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
	out, err := Search(context.Background(), cfg, "query", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	snippet := out.Results[0].Snippet
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
	out, err := Search(context.Background(), cfg, "query", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(out.Results) != 1 || out.Results[0].URL != "https://example.com" {
		t.Fatalf("unexpected results: %+v", out.Results)
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
	t.Setenv("X3_EXA_HIGHLIGHT_QUERY", "1")

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
	if !cfg.HighlightQuery {
		t.Error("HighlightQuery = false, want true")
	}
}

func TestConfigFromEnvDefaults(t *testing.T) {
	t.Setenv("X3_EXA_API_KEY", "")
	t.Setenv("X3_EXA_BASE_URL", "")
	t.Setenv("X3_EXA_SEARCH_TYPE", "")
	t.Setenv("X3_EXA_TIMEOUT", "")
	t.Setenv("X3_EXA_SNIPPET_CHARS", "")
	t.Setenv("X3_EXA_HIGHLIGHT_QUERY", "")

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

func TestBuildSnippetSortsByScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		highlights []string
		scores     []float64
		maxChars   int
		want       string
	}{
		{
			name:       "most relevant first",
			highlights: []string{"weak  match", "strong match"},
			scores:     []float64{0.1, 0.9},
			maxChars:   100,
			want:       "strong match weak match",
		},
		{
			name:       "without scores keeps provider order",
			highlights: []string{"first", "second"},
			maxChars:   100,
			want:       "first second",
		},
		{
			name:       "mismatched scores keeps provider order",
			highlights: []string{"first", "second"},
			scores:     []float64{0.1},
			maxChars:   100,
			want:       "first second",
		},
		{
			name:       "truncates",
			highlights: []string{"abcdefghij"},
			maxChars:   5,
			want:       "abcd…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSnippet(tt.highlights, tt.scores, tt.maxChars); got != tt.want {
				t.Errorf("buildSnippet = %q, want %q", got, tt.want)
			}
		})
	}
}

func resetGuidedHighlights(t *testing.T) {
	t.Helper()
	highlightQueryUnsupported.Store(false)
	t.Cleanup(func() { highlightQueryUnsupported.Store(false) })
}

func TestSearchRequestsGuidedHighlights(t *testing.T) {
	resetGuidedHighlights(t)

	var (
		mu  sync.Mutex
		got searchRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body searchRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		mu.Lock()
		got = body
		mu.Unlock()
		_, _ = w.Write([]byte(`{"results":[{"title":"One","url":"https://one.example","highlights":["hi"]}]}`))
	}))
	defer server.Close()

	cfg := Config{
		APIKeys:        []string{"key"},
		BaseURL:        server.URL,
		Timeout:        time.Second,
		SearchType:     "lite",
		SnippetChars:   50,
		HighlightQuery: true,
	}
	if _, err := Search(context.Background(), cfg, "test query", 1); err != nil {
		t.Fatalf("Search: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.Contents.Highlights.Query != "test query" {
		t.Errorf("highlights.query = %q, want the search query", got.Contents.Highlights.Query)
	}
}

func TestSearchDropsGuidedHighlightsOnBadRequest(t *testing.T) {
	resetGuidedHighlights(t)

	var (
		mu       sync.Mutex
		requests int
		queries  []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body searchRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		requests++
		queries = append(queries, body.Contents.Highlights.Query)
		mu.Unlock()
		if body.Contents.Highlights.Query != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"highlights.query is not supported for type lite","tag":"INVALID_REQUEST"}`))
			return
		}
		_, _ = w.Write([]byte(`{"results":[{"title":"One","url":"https://one.example"}]}`))
	}))
	defer server.Close()

	cfg := Config{
		APIKeys:        []string{"key"},
		BaseURL:        server.URL,
		Timeout:        time.Second,
		SearchType:     "lite",
		HighlightQuery: true,
	}
	out, err := Search(context.Background(), cfg, "query", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("results = %+v, want the retry without guided highlights", out.Results)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 2 {
		t.Fatalf("requests = %d, want 2 (guided highlights, then plain)", requests)
	}
	if queries[1] != "" {
		t.Errorf("retry still sent highlights.query = %q", queries[1])
	}
	if !highlightQueryUnsupported.Load() {
		t.Error("guided highlights were not disabled after the rejection")
	}
}

func TestSearchParsesNoticesAndUsage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"results":[{"title":"One","url":"https://one.example","publishedDate":"2026-01-02","author":"Gopher"}],
			"notices":[
				{"code":"RESTRICTIONS_RELAXED","path":"/query","message":"  site filter   removed "},
				{"code":"EMPTY","path":"/query","message":"   "}
			],
			"warnings":[{"code":"IGNORED_FIELD","path":"/query","message":"ignored field"}],
			"costDollars":{"total":0.007},
			"searchTime":123
		}`))
	}))
	defer server.Close()

	cfg := Config{APIKeys: []string{"key"}, BaseURL: server.URL, Timeout: time.Second, SearchType: "lite"}
	out, err := Search(context.Background(), cfg, "query", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(out.Notices) != 1 || out.Notices[0] != "site filter removed" {
		t.Errorf("notices = %v", out.Notices)
	}
	if len(out.Warnings) != 1 || out.Warnings[0] != "ignored field" {
		t.Errorf("warnings = %v", out.Warnings)
	}
	if out.CostUSD != 0.007 {
		t.Errorf("cost = %v", out.CostUSD)
	}
	if out.SearchMS != 123 {
		t.Errorf("search time = %v", out.SearchMS)
	}
	if out.Results[0].PublishedDate != "2026-01-02" || out.Results[0].Author != "Gopher" {
		t.Errorf("metadata lost: %+v", out.Results[0])
	}
}
