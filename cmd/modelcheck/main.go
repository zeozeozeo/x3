// Command modelcheck probes every model/provider/codename from models.json
// with a minimal chat completion request and reports which ones work.
//
// It reads API keys from the environment (via .env in the working directory)
// and writes models-working.json containing only the models (with only the
// providers/codenames) that produced a response.
//
// At most one request per provider is in flight at any time, and each check
// gets up to -tries attempts with -retry-delay between them, to ride out
// provider rate limits.
//
// Usage:
//
//	go run ./cmd/modelcheck [-workers 8] [-timeout 90s] [-tries 3] [-retry-delay 1m] [-models models.json] [-out models-working.json]
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
)

type checkResult struct {
	ModelName string
	Provider  string // suffixed with #N when a provider has multiple base URLs
	Codename  string
	OK        bool
	Latency   time.Duration
	Detail    string // reply snippet on success, error on failure
}

func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func doRequest(ctx context.Context, timeout time.Duration, provider, baseURL, token, codename string) (string, time.Duration, error) {
	config := openai.DefaultConfig(token)
	config.BaseURL = baseURL
	if provider == model.ProviderGithub { // same special case as llm.requestCompletionInternal
		config = openai.DefaultAzureConfig(token, baseURL)
		config.APIType = openai.APITypeOpenAI
	}
	client := openai.NewClientWithConfig(config)

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	resp, err := client.CreateChatCompletion(reqCtx, openai.ChatCompletionRequest{
		Model: codename,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "Reply with exactly the word ok and nothing else."},
		},
	})
	latency := time.Since(start)
	if err != nil {
		return "", latency, err
	}
	if len(resp.Choices) == 0 {
		return "", latency, errors.New("no choices in response")
	}
	text := strings.TrimSpace(resp.Choices[0].Message.Content)
	if text == "" {
		return "", latency, errors.New("empty response")
	}
	return text, latency, nil
}

// retryable reports whether another attempt could plausibly succeed.
// Deterministic client errors (bad request, bad/expired key, not found)
// fail fast so a full sweep doesn't burn minutes on hopeless combos.
func retryable(err error) bool {
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout:
			return true
		default:
			return false
		}
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) {
		if reqErr.HTTPStatusCode == 0 {
			return true // transport-level failure
		}
		return reqErr.HTTPStatusCode == http.StatusTooManyRequests ||
			reqErr.HTTPStatusCode >= http.StatusInternalServerError
	}
	// Timeouts and schemaless failures (empty response, no choices) are transient.
	return true
}

func checkWithRetries(ctx context.Context, timeout, retryDelay time.Duration, tries int, modelName, provider, displayProvider, baseURL, token, codename string) checkResult {
	res := checkResult{ModelName: modelName, Provider: displayProvider, Codename: codename}
	if token == "" {
		res.Detail = "no API token configured"
		return res
	}
	if tries < 1 {
		tries = 1
	}

	var lastErr error
	var total time.Duration
	attempts := 0
	for attempt := 1; attempt <= tries; attempt++ {
		if attempt > 1 {
			fmt.Fprintf(os.Stderr, "... retrying %s/%s/%s (attempt %d/%d) after %s: %v\n",
				modelName, displayProvider, codename, attempt, tries, retryDelay, lastErr)
			select {
			case <-ctx.Done():
				break
			case <-time.After(retryDelay):
			}
		}
		attempts = attempt
		text, latency, err := doRequest(ctx, timeout, provider, baseURL, token, codename)
		total += latency
		if err == nil {
			res.OK = true
			res.Latency = total
			res.Detail = truncate(text, 80)
			return res
		}
		lastErr = err
		if !retryable(err) {
			break
		}
	}
	res.Latency = total
	res.Detail = truncate(fmt.Sprintf("after %d %s: %v", attempts, pluralize(attempts, "try", "tries"), lastErr), 160)
	return res
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// providerLocks serializes requests so at most one request per provider
// is in flight at any time, keeping us under provider rate limits.
var (
	providerLocksMu sync.Mutex
	providerLocks   = map[string]*sync.Mutex{}
)

func lockFor(provider string) *sync.Mutex {
	providerLocksMu.Lock()
	defer providerLocksMu.Unlock()
	if l, ok := providerLocks[provider]; ok {
		return l
	}
	l := &sync.Mutex{}
	providerLocks[provider] = l
	return l
}

func main() {
	workers := flag.Int("workers", 8, "concurrent requests across different providers (max 1 in flight per provider)")
	timeout := flag.Duration("timeout", 90*time.Second, "per-request timeout")
	tries := flag.Int("tries", 3, "attempts per model/provider/codename before giving up")
	retryDelay := flag.Duration("retry-delay", time.Minute, "wait between attempts")
	modelsPath := flag.String("models", "", "path to models.json (default: auto-discovered like the bot does)")
	outPath := flag.String("out", "models-working.json", "output path for working models")
	onlyModel := flag.String("model", "", "only check the model with this name")
	onlyProvider := flag.String("provider", "", "only check this provider")
	flag.Parse()

	_ = godotenv.Load()

	if *modelsPath != "" {
		data, err := os.ReadFile(*modelsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read models file: %v\n", err)
			os.Exit(1)
		}
		if err := model.LoadModelsFromJSONData(data); err != nil {
			fmt.Fprintf(os.Stderr, "parse models file: %v\n", err)
			os.Exit(1)
		}
	} else if err := model.LoadModelsFromJSON(); err != nil {
		fmt.Fprintf(os.Stderr, "load models.json: %v\n", err)
		os.Exit(1)
	}

	type job struct {
		modelName       string
		provider        string
		displayProvider string
		baseURL         string
		token           string
		codename        string
	}
	var jobs []job
	for _, m := range model.AllModels {
		if m.IsVeryDumb() {
			continue
		}
		if *onlyModel != "" && m.Name != *onlyModel {
			continue
		}
		providers := make([]string, 0, len(m.Providers))
		for p := range m.Providers {
			providers = append(providers, p)
		}
		sort.Strings(providers)
		for _, p := range providers {
			if *onlyProvider != "" && p != *onlyProvider {
				continue
			}
			baseURLs, tokens, codenames := m.Client(p)
			for bi, baseURL := range baseURLs {
				// Pair base URLs with tokens the same way llm does:
				// when counts match 1:1, each base URL uses its own token.
				tokensToTry := tokens
				if len(baseURLs) > 1 && len(baseURLs) == len(tokens) {
					tokensToTry = []string{tokens[bi]}
				}
				if len(tokensToTry) == 0 {
					tokensToTry = []string{""}
				}
				displayProvider := p
				if len(baseURLs) > 1 {
					displayProvider = fmt.Sprintf("%s#%d", p, bi+1)
				}
				for _, token := range tokensToTry {
					for _, codename := range codenames {
						jobs = append(jobs, job{m.Name, p, displayProvider, baseURL, token, codename})
					}
				}
			}
		}
	}
	if len(jobs) == 0 {
		fmt.Fprintln(os.Stderr, "nothing to check")
		os.Exit(1)
	}

	if *workers < 1 {
		*workers = 1
	}
	ctx := context.Background()
	jobCh := make(chan job)
	resCh := make(chan checkResult, len(jobs))
	var wg sync.WaitGroup
	for range *workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobCh {
				l := lockFor(j.provider)
				l.Lock()
				resCh <- checkWithRetries(ctx, *timeout, *retryDelay, *tries, j.modelName, j.provider, j.displayProvider, j.baseURL, j.token, j.codename)
				l.Unlock()
			}
		}()
	}
	go func() {
		for _, j := range jobs {
			jobCh <- j
		}
		close(jobCh)
	}()
	go func() {
		wg.Wait()
		close(resCh)
	}()

	var results []checkResult
	for r := range resCh {
		results = append(results, r)
		fmt.Printf("%s %-40s %-14s %-45s %s\n", statusMark(r.OK), r.ModelName, r.Provider, r.Codename, r.Detail)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].ModelName != results[j].ModelName {
			return results[i].ModelName < results[j].ModelName
		}
		if results[i].Provider != results[j].Provider {
			return results[i].Provider < results[j].Provider
		}
		return results[i].Codename < results[j].Codename
	})

	// Re-print as an aligned table.
	fmt.Println()
	widths := map[string]int{"model": 5, "provider": 8, "codename": 8}
	for _, r := range results {
		widths["model"] = max(widths["model"], len(r.ModelName))
		widths["provider"] = max(widths["provider"], len(r.Provider))
		widths["codename"] = max(widths["codename"], len(r.Codename))
	}
	fmt.Printf("%-2s %-*s %-*s %-*s %-10s %s\n", "", widths["model"], "MODEL", widths["provider"], "PROVIDER", widths["codename"], "CODENAME", "LATENCY", "DETAIL")
	okCount := 0
	for _, r := range results {
		fmt.Printf("%s %-*s %-*s %-*s %-10s %s\n",
			statusMark(r.OK), widths["model"], r.ModelName, widths["provider"], r.Provider,
			widths["codename"], r.Codename, r.Latency.Round(time.Millisecond), r.Detail)
		if r.OK {
			okCount++
		}
	}
	fmt.Printf("\n%d/%d working\n", okCount, len(results))

	if err := writeWorkingModels(*outPath, results); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *outPath)
}

func statusMark(ok bool) string {
	if ok {
		return "✅"
	}
	return "❌"
}

// writeWorkingModels emits the models that produced at least one response,
// keeping only the working provider/codename entries.
func writeWorkingModels(path string, results []checkResult) error {
	working := map[string]map[string]map[string]bool{} // model -> provider -> codename
	for _, r := range results {
		if !r.OK {
			continue
		}
		provider := r.Provider
		if i := strings.Index(provider, "#"); i != -1 {
			provider = provider[:i]
		}
		if working[r.ModelName] == nil {
			working[r.ModelName] = map[string]map[string]bool{}
		}
		if working[r.ModelName][provider] == nil {
			working[r.ModelName][provider] = map[string]bool{}
		}
		working[r.ModelName][provider][r.Codename] = true
	}

	var out []model.Model
	for _, m := range model.AllModels {
		codenamesByProvider, ok := working[m.Name]
		if !ok {
			continue
		}
		filtered := m
		filtered.Providers = make(map[string]model.ModelProvider, len(codenamesByProvider))
		providers := make([]string, 0, len(codenamesByProvider))
		for p := range codenamesByProvider {
			providers = append(providers, p)
		}
		sort.Strings(providers)
		for _, p := range providers {
			keep := []string{}
			for _, c := range m.Providers[p].Codenames {
				if codenamesByProvider[p][c] {
					keep = append(keep, c)
				}
			}
			if len(keep) > 0 {
				filtered.Providers[p] = model.ModelProvider{Codenames: keep}
			}
		}
		if len(filtered.Providers) > 0 {
			out = append(out, filtered)
		}
	}
	if out == nil {
		out = []model.Model{}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
