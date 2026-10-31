// Package websearch runs web searches through Exa, falling back to
// DuckDuckGo's HTML endpoint when Exa is not configured or unreachable.
package websearch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zeozeozeo/x3/exa"
)

// Result is the common search result shape used by the LLM prompt builder.
// Published and Author are optional and only come from Exa.
type Result struct {
	Title     string
	Info      string
	URL       string
	Published string
	Author    string
}

// Response is the outcome of a search, including any notes the provider wants
// shown to the model.
type Response struct {
	Results  []Result
	Provider string
	Notices  []string
}

const (
	defaultRetryAfter = 2 * time.Minute
	authRetryAfter    = 10 * time.Minute
	ddgAttempts       = 3
	defaultResults    = 10
)

var (
	// These are variables so tests can inject fake providers.
	exaSearch = exa.Search
	ddgSearch = queryDDG

	exaConfig       = exa.ConfigFromEnv()
	failureCooldown = envDuration("X3_EXA_RETRY_AFTER", defaultRetryAfter)

	cooldownUntil atomic.Int64
)

// Query searches the web, preferring Exa and falling back to DuckDuckGo.
// The returned provider is "exa" or "duckduckgo".
func Query(ctx context.Context, query string, maxResults int) (Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return Response{}, errors.New("search query was empty")
	}
	if maxResults <= 0 {
		maxResults = defaultResults
	}

	if exaConfig.Enabled() {
		if until := cooldownUntil.Load(); until > time.Now().UnixNano() {
			slog.Debug("skipping exa during cooldown", "until", time.Unix(0, until))
		} else {
			out, err := exaSearch(ctx, exaConfig, query, maxResults)
			if err == nil {
				if converted := fromExa(out.Results); len(converted) > 0 {
					slog.Debug("exa search usage",
						"query", query,
						"cost_usd", out.CostUSD,
						"search_ms", out.SearchMS,
						"notices", len(out.Notices))
					if len(out.Warnings) > 0 {
						slog.Warn("exa search warnings", "query", query, "warnings", strings.Join(out.Warnings, "; "))
					}
					return Response{Results: converted, Provider: "exa", Notices: out.Notices}, nil
				}
				slog.Info("exa returned no results, falling back to duckduckgo", "query", query)
			} else if ctxErr := ctx.Err(); ctxErr != nil {
				return Response{}, ctxErr
			} else {
				cooldown := failureCooldown
				var httpErr *exa.HTTPError
				if errors.As(err, &httpErr) && isAuthFailure(httpErr.StatusCode) {
					cooldown = authRetryAfter
				}
				if cooldown > 0 {
					cooldownUntil.Store(time.Now().Add(cooldown).UnixNano())
				}
				slog.Warn("exa search failed, falling back to duckduckgo",
					"query", query, "retry_after", cooldown, "err", err)
			}
		}
	}

	var attempts []error
	for range ddgAttempts {
		if err := ctx.Err(); err != nil {
			return Response{}, err
		}
		results, err := ddgSearch(query, maxResults)
		if err == nil {
			return Response{Results: results, Provider: "duckduckgo"}, nil
		}
		attempts = append(attempts, err)
	}
	return Response{}, fmt.Errorf("duckduckgo: %w", errors.Join(attempts...))
}

func fromExa(results []exa.Result) []Result {
	converted := make([]Result, 0, len(results))
	for _, result := range results {
		resultURL := strings.TrimSpace(result.URL)
		if resultURL == "" {
			continue
		}
		converted = append(converted, Result{
			Title:     strings.TrimSpace(result.Title),
			Info:      result.Snippet,
			URL:       resultURL,
			Published: strings.TrimSpace(result.PublishedDate),
			Author:    strings.TrimSpace(result.Author),
		})
	}
	return converted
}

func isAuthFailure(status int) bool {
	switch status {
	case 401, 402, 403, 429:
		return true
	default:
		return false
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}
