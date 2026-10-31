package websearch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeozeozeo/x3/exa"
)

func stubProviders(
	t *testing.T,
	cfg exa.Config,
	exaFn func(context.Context, exa.Config, string, int) (exa.SearchOutput, error),
	ddgFn func(string, int) ([]Result, error),
	cooldown time.Duration,
) {
	t.Helper()
	oldConfig, oldExa, oldDDG, oldCooldown := exaConfig, exaSearch, ddgSearch, failureCooldown
	t.Cleanup(func() {
		exaConfig, exaSearch, ddgSearch, failureCooldown = oldConfig, oldExa, oldDDG, oldCooldown
		cooldownUntil.Store(0)
	})

	exaConfig = cfg
	if exaFn != nil {
		exaSearch = exaFn
	}
	if ddgFn != nil {
		ddgSearch = ddgFn
	}
	failureCooldown = cooldown
	cooldownUntil.Store(0)
}

func enabledConfig() exa.Config {
	return exa.Config{APIKeys: []string{"test-key"}, BaseURL: "https://exa.test", SearchType: "lite"}
}

func TestQueryUsesExa(t *testing.T) {
	exaCalled := false
	stubProviders(t, enabledConfig(),
		func(_ context.Context, _ exa.Config, query string, maxResults int) (exa.SearchOutput, error) {
			exaCalled = true
			if query != "golang" || maxResults != 5 {
				t.Errorf("query = %q, maxResults = %d", query, maxResults)
			}
			return exa.SearchOutput{
				Results: []exa.Result{
					{Title: " Exa ", URL: " https://exa.example ", Snippet: "snip", PublishedDate: "2026-01-02", Author: "gopher"},
					{Title: "No URL"},
				},
				Notices: []string{"results were relaxed"},
			}, nil
		},
		func(string, int) ([]Result, error) {
			t.Error("duckduckgo must not be called when exa succeeds")
			return nil, nil
		},
		time.Minute,
	)

	resp, err := Query(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !exaCalled {
		t.Error("exa was not called")
	}
	if resp.Provider != "exa" {
		t.Errorf("provider = %q, want exa", resp.Provider)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(resp.Results))
	}
	got := resp.Results[0]
	if got.Title != "Exa" || got.URL != "https://exa.example" || got.Info != "snip" {
		t.Errorf("unexpected result: %+v", got)
	}
	if got.Published != "2026-01-02" || got.Author != "gopher" {
		t.Errorf("metadata lost: %+v", got)
	}
	if len(resp.Notices) != 1 || resp.Notices[0] != "results were relaxed" {
		t.Errorf("notices = %v", resp.Notices)
	}
}

func TestQueryFallsBackToDuckDuckGo(t *testing.T) {
	tests := []struct {
		name  string
		exaFn func(context.Context, exa.Config, string, int) (exa.SearchOutput, error)
	}{
		{
			name: "exa error",
			exaFn: func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
				return exa.SearchOutput{}, errors.New("exa is down")
			},
		},
		{
			name: "exa empty",
			exaFn: func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
				return exa.SearchOutput{}, nil
			},
		},
		{
			name: "exa results without urls",
			exaFn: func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
				return exa.SearchOutput{Results: []exa.Result{{Title: "No URL"}}}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ddgCalled := false
			stubProviders(t, enabledConfig(), tt.exaFn,
				func(string, int) ([]Result, error) {
					ddgCalled = true
					return []Result{{Title: "DDG", URL: "https://ddg.example"}}, nil
				},
				time.Minute,
			)

			resp, err := Query(context.Background(), "query", 10)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if !ddgCalled {
				t.Error("duckduckgo was not called")
			}
			if resp.Provider != "duckduckgo" {
				t.Errorf("provider = %q, want duckduckgo", resp.Provider)
			}
			if len(resp.Results) != 1 || resp.Results[0].Title != "DDG" {
				t.Errorf("unexpected results: %+v", resp.Results)
			}
		})
	}
}

func TestQuerySkipsExaDuringCooldown(t *testing.T) {
	exaCalls := 0
	stubProviders(t, enabledConfig(),
		func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
			exaCalls++
			if exaCalls == 1 {
				return exa.SearchOutput{}, errors.New("network down")
			}
			return exa.SearchOutput{Results: []exa.Result{{Title: "Exa", URL: "https://exa.example"}}}, nil
		},
		func(string, int) ([]Result, error) {
			return []Result{{Title: "DDG", URL: "https://ddg.example"}}, nil
		},
		time.Minute,
	)

	if resp, err := Query(context.Background(), "first", 10); err != nil || resp.Provider != "duckduckgo" {
		t.Fatalf("first query: provider = %q, err = %v", resp.Provider, err)
	}
	if resp, err := Query(context.Background(), "second", 10); err != nil || resp.Provider != "duckduckgo" {
		t.Fatalf("second query: provider = %q, err = %v", resp.Provider, err)
	}
	if exaCalls != 1 {
		t.Fatalf("exa calls = %d, want 1 while cooling down", exaCalls)
	}

	cooldownUntil.Store(0)
	if resp, err := Query(context.Background(), "third", 10); err != nil || resp.Provider != "exa" {
		t.Fatalf("third query: provider = %q, err = %v", resp.Provider, err)
	}
	if exaCalls != 2 {
		t.Fatalf("exa calls = %d, want 2 after cooldown", exaCalls)
	}
}

func TestQueryAuthFailureUsesLongCooldown(t *testing.T) {
	stubProviders(t, enabledConfig(),
		func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
			return exa.SearchOutput{}, &exa.HTTPError{StatusCode: 401, Message: "invalid key"}
		},
		func(string, int) ([]Result, error) {
			return []Result{{Title: "DDG", URL: "https://ddg.example"}}, nil
		},
		time.Minute,
	)

	if _, err := Query(context.Background(), "query", 10); err != nil {
		t.Fatalf("Query: %v", err)
	}
	remaining := time.Until(time.Unix(0, cooldownUntil.Load()))
	if remaining < authRetryAfter/2 {
		t.Fatalf("cooldown = %v, want roughly %v", remaining, authRetryAfter)
	}
}

func TestQueryBothProvidersFail(t *testing.T) {
	ddgCalls := 0
	stubProviders(t, enabledConfig(),
		func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
			return exa.SearchOutput{}, errors.New("exa is down")
		},
		func(string, int) ([]Result, error) {
			ddgCalls++
			return nil, errors.New("duckduckgo is down")
		},
		time.Minute,
	)

	resp, err := Query(context.Background(), "query", 10)
	if err == nil {
		t.Fatal("expected an error")
	}
	if resp.Provider != "" {
		t.Errorf("provider = %q, want empty", resp.Provider)
	}
	if !strings.Contains(err.Error(), "duckduckgo") {
		t.Errorf("error = %v, want duckduckgo context", err)
	}
	if ddgCalls != ddgAttempts {
		t.Errorf("duckduckgo calls = %d, want %d", ddgCalls, ddgAttempts)
	}
}

func TestQueryRetriesDuckDuckGo(t *testing.T) {
	ddgCalls := 0
	stubProviders(t, exa.Config{},
		func(context.Context, exa.Config, string, int) (exa.SearchOutput, error) {
			t.Error("exa must not be called without a key")
			return exa.SearchOutput{}, nil
		},
		func(string, int) ([]Result, error) {
			ddgCalls++
			if ddgCalls < 3 {
				return nil, errors.New("temporary failure")
			}
			return []Result{{Title: "DDG", URL: "https://ddg.example"}}, nil
		},
		time.Minute,
	)

	if resp, err := Query(context.Background(), "query", 10); err != nil || resp.Provider != "duckduckgo" {
		t.Fatalf("provider = %q, err = %v", resp.Provider, err)
	}
	if ddgCalls != 3 {
		t.Errorf("duckduckgo calls = %d, want 3", ddgCalls)
	}
}

func TestQueryEmpty(t *testing.T) {
	stubProviders(t, enabledConfig(), nil, nil, time.Minute)

	if _, err := Query(context.Background(), "  ", 10); err == nil {
		t.Fatal("expected an error for an empty query")
	}
}

func TestQueryCanceledContext(t *testing.T) {
	stubProviders(t, exa.Config{},
		nil,
		func(string, int) ([]Result, error) {
			t.Error("duckduckgo must not be called with a canceled context")
			return nil, nil
		},
		time.Minute,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Query(ctx, "query", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
