package openaiapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
)

const testAPIKey = "test-secret-key"

// loadTestModels installs a model configuration for the tests.
func loadTestModels(t *testing.T, config string) {
	t.Helper()
	if err := model.LoadModelsFromJSONData([]byte(config)); err != nil {
		t.Fatalf("failed to load test models: %v", err)
	}
}

func newTestServer(t *testing.T, cfg Config, hook func(provider, baseURL string) string) *Server {
	t.Helper()
	if cfg.APIKeys == nil {
		cfg.APIKeys = []string{testAPIKey}
	}
	srv := NewServer(cfg)
	srv.baseURLHook = hook
	return srv
}

func doRequest(t *testing.T, srv *Server, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	return rec
}

func TestModelsListUsesShortCodenames(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Test Model", "command": "testmodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Test Model"],
		"providers_order": ["groq"]
	}`)

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0", Timeout: 0}, nil)

	rec := doRequest(t, srv, http.MethodGet, "/v1/models", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var list modelList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if list.Object != "list" || len(list.Data) != 1 {
		t.Fatalf("unexpected model list: %+v", list)
	}
	if list.Data[0].ID != "testmodel" {
		t.Fatalf("expected the short codename, got %q", list.Data[0].ID)
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/models/testmodel", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = doRequest(t, srv, http.MethodGet, "/v1/models/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "model_not_found") {
		t.Fatalf("expected a model_not_found error, got %s", rec.Body.String())
	}
}

func TestUnknownModelIsRejected(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Test Model", "command": "testmodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Test Model"],
		"providers_order": ["groq"]
	}`)

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"}, nil)
	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions",
		`{"model": "does-not-exist", "messages": [{"role": "user", "content": "hi"}]}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAuthenticationIsRequired(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Test Model", "command": "testmodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Test Model"],
		"providers_order": ["groq"]
	}`)

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"}, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model": "testmodel", "messages": [{"role": "user", "content": "hi"}]}`))
	rec := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// The model list is protected as well.
	req = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for the model list, got %d", rec.Code)
	}

	// The health endpoint stays open.
	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for the health endpoint, got %d", rec.Code)
	}
}

func TestChatCompletionForwardsEveryParameter(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Test Model", "command": "testmodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Test Model"],
		"providers_order": ["groq"]
	}`)
	t.Setenv("X3_GROQ_TOKEN", "test-token")

	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "cmpl-1",
			"object": "chat.completion",
			"created": 1,
			"model": "test-codename",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "hello there"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 3, "completion_tokens": 2, "total_tokens": 5}
		}`))
	}))
	defer upstream.Close()

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"},
		func(provider, baseURL string) string { return upstream.URL })

	body := `{
		"model": "testmodel",
		"messages": [{"role": "user", "content": "hi"}],
		"temperature": 0.5,
		"top_p": 0.9,
		"max_completion_tokens": 128,
		"frequency_penalty": 0.1,
		"presence_penalty": 0.2,
		"seed": 42,
		"stop": ["\n\n"],
		"logit_bias": {"123": 5},
		"response_format": {"type": "json_object"},
		"tools": [{"type": "function", "function": {"name": "get_weather", "parameters": {"type": "object"}}}],
		"tool_choice": "auto",
		"parallel_tool_calls": false,
		"metadata": {"trace": "abc"},
		"user": "tester",
		"some_unknown_parameter": {"nested": true}
	}`

	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if received["model"] != "test-codename" {
		t.Fatalf("expected the upstream codename, got %v", received["model"])
	}
	if received["temperature"] != 0.5 {
		t.Fatalf("temperature was not forwarded: %v", received["temperature"])
	}
	if received["max_completion_tokens"] != float64(128) {
		t.Fatalf("max_completion_tokens was not forwarded: %v", received["max_completion_tokens"])
	}
	if received["logit_bias"] == nil || received["response_format"] == nil {
		t.Fatalf("logit_bias/response_format were not forwarded: %v", received)
	}
	if _, ok := received["tools"]; !ok {
		t.Fatalf("tools were not forwarded: %v", received)
	}
	if received["tool_choice"] != "auto" {
		t.Fatalf("tool_choice was not forwarded: %v", received["tool_choice"])
	}
	unknown, ok := received["some_unknown_parameter"].(map[string]any)
	if !ok || unknown["nested"] != true {
		t.Fatalf("unknown parameters were not forwarded verbatim: %v", received["some_unknown_parameter"])
	}
	if received["user"] != "tester" || received["metadata"] == nil {
		t.Fatalf("user/metadata were not forwarded: %v", received)
	}

	var resp struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Model != "testmodel" {
		t.Fatalf("expected the requested codename in the response, got %q", resp.Model)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "hello there" {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestToolCallsAreForwarded(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Test Model", "command": "testmodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Test Model"],
		"providers_order": ["groq"]
	}`)
	t.Setenv("X3_GROQ_TOKEN", "test-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "cmpl-2",
			"object": "chat.completion",
			"created": 1,
			"model": "test-codename",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": null, "tool_calls": [{"id": "call_1", "type": "function", "function": {"name": "get_weather", "arguments": "{\"city\":\"Berlin\"}"}}]}, "finish_reason": "tool_calls"}]
		}`))
	}))
	defer upstream.Close()

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"},
		func(provider, baseURL string) string { return upstream.URL })

	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions",
		`{"model": "testmodel", "messages": [{"role": "user", "content": "weather?"}], "tools": [{"type": "function", "function": {"name": "get_weather", "parameters": {"type": "object"}}}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tool_calls"`) ||
		!strings.Contains(rec.Body.String(), `"get_weather"`) {
		t.Fatalf("tool calls were not returned: %s", rec.Body.String())
	}
}

func TestStreamingChatCompletion(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Stream Model", "command": "streammodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Stream Model"],
		"providers_order": ["groq"]
	}`)
	t.Setenv("X3_GROQ_TOKEN", "test-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"id\":\"cmpl-3\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-codename\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"he\"},\"finish_reason\":\"null\"}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: {\"id\":\"cmpl-3\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"test-codename\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"llo\"},\"finish_reason\":\"stop\"}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"},
		func(provider, baseURL string) string { return upstream.URL })

	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions",
		`{"model": "streammodel", "messages": [{"role": "user", "content": "hi"}], "stream": true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("expected an event stream, got %q", ct)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"content":"he"`) || !strings.Contains(body, `"content":"llo"`) {
		t.Fatalf("the stream did not contain the chunks: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("the stream was not terminated: %s", body)
	}
	if !strings.Contains(body, `"model":"streammodel"`) {
		t.Fatalf("the stream did not report the requested model: %s", body)
	}
}

func TestProviderFailover(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Failover Model", "command": "failovermodel", "providers": {"groq": {"codenames": ["test-codename"]}, "openrouter": {"codenames": ["test-codename"]}}}],
		"default_models": ["Failover Model"],
		"providers_order": ["groq", "openrouter"]
	}`)
	t.Setenv("X3_GROQ_TOKEN", "test-token")
	t.Setenv("X3_OPENROUTER_TOKEN", "test-token")

	groq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": {"message": "boom", "type": "server_error"}}`))
	}))
	defer groq.Close()

	openrouter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "cmpl-4",
			"object": "chat.completion",
			"created": 1,
			"model": "test-codename",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "second provider"}, "finish_reason": "stop"}]
		}`))
	}))
	defer openrouter.Close()

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0", MaxRetries: 0},
		func(provider, baseURL string) string {
			if provider == model.ProviderGroq {
				return groq.URL
			}
			return openrouter.URL
		})

	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions",
		`{"model": "failovermodel", "messages": [{"role": "user", "content": "hi"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "second provider") {
		t.Fatalf("the second provider was not used: %s", rec.Body.String())
	}
}

func TestExtraParametersAreForwarded(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "Extra Model", "command": "extramodel", "providers": {"groq": {"codenames": ["test-codename"]}}}],
		"default_models": ["Extra Model"],
		"providers_order": ["groq"]
	}`)
	t.Setenv("X3_GROQ_TOKEN", "test-token")

	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","created":1,"model":"test-codename","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"},
		func(provider, baseURL string) string { return upstream.URL })

	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions",
		`{"model": "extramodel", "messages": [{"role": "user", "content": "hi"}], "brand_new_param": [1, 2, 3]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	values, ok := received["brand_new_param"].([]any)
	if !ok || len(values) != 3 {
		t.Fatalf("the unknown parameter was not forwarded: %v", received["brand_new_param"])
	}
}

func TestOpenCodeGateToolIsAnsweredInternally(t *testing.T) {
	loadTestModels(t, `{
		"models": [{"name": "OpenCode Model", "command": "ocmodel", "providers": {"opencode": {"codenames": ["zen-model"]}}}],
		"default_models": ["OpenCode Model"],
		"providers_order": ["opencode"]
	}`)
	t.Setenv("X3_OPENCODE_TOKEN", "test-token")

	var mu sync.Mutex
	var requests []map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		mu.Lock()
		requests = append(requests, parsed)
		count := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		if count == 1 {
			_, _ = w.Write([]byte("data: {\"id\":\"oc-1\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"zen-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"))
			flusher.Flush()
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		_, _ = w.Write([]byte("data: {\"id\":\"oc-2\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"zen-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"all done\"},\"finish_reason\":\"stop\"}]}\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	srv := newTestServer(t, Config{BindAddr: "127.0.0.1:0"},
		func(provider, baseURL string) string { return upstream.URL })

	rec := doRequest(t, srv, http.MethodPost, "/v1/chat/completions",
		`{"model": "ocmodel", "messages": [{"role": "user", "content": "hi"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "all done") {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"read"`) {
		t.Fatalf("the internal gate tool call leaked to the client: %s", rec.Body.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(requests))
	}
	// The gate tool has to be part of every request.
	for i, request := range requests {
		tools, ok := request["tools"].([]any)
		if !ok {
			t.Fatalf("request %d has no tools: %v", i, request)
		}
		found := false
		for _, tool := range tools {
			entry, ok := tool.(map[string]any)
			if !ok {
				continue
			}
			function, ok := entry["function"].(map[string]any)
			if !ok {
				continue
			}
			if function["name"] == "read" {
				found = true
			}
		}
		if !found {
			t.Fatalf("request %d does not contain the gate tool: %v", i, tools)
		}
	}
}

func TestParseAPIKeys(t *testing.T) {
	keys := parseAPIKeys("one, two;three\nfour")
	if len(keys) != 4 {
		t.Fatalf("expected 4 keys, got %v", keys)
	}
	if keys[0] != "one" || keys[3] != "four" {
		t.Fatalf("unexpected keys: %v", keys)
	}
	if len(parseAPIKeys("  ")) != 0 {
		t.Fatalf("expected no keys")
	}
}

func TestServerRequiresAPIKeys(t *testing.T) {
	srv := NewServer(Config{BindAddr: "127.0.0.1:0"})
	err := srv.Start()
	if err == nil {
		t.Fatal("expected the server to refuse to start without API keys")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSynthesizeChunks(t *testing.T) {
	req := openai.ChatCompletionRequest{
		StreamOptions: &openai.StreamOptions{IncludeUsage: true},
	}
	resp := openai.ChatCompletionResponse{
		ID:      "cmpl-5",
		Object:  "chat.completion",
		Created: 1,
		Model:   "test-codename",
		Choices: []openai.ChatCompletionChoice{{
			Index: 0,
			Message: openai.ChatCompletionMessage{
				Role:    openai.ChatMessageRoleAssistant,
				Content: "hi",
			},
			FinishReason: openai.FinishReasonStop,
		}},
		Usage: openai.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
	}

	chunks := synthesizeChunks(resp, req)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].Choices[0].Delta.Content != "hi" {
		t.Fatalf("unexpected delta: %+v", chunks[0].Choices[0].Delta)
	}
	if chunks[1].Usage == nil {
		t.Fatal("expected a usage chunk")
	}
}

func TestClassifyFailure(t *testing.T) {
	if classifyFailure(nil).Weight != 0 {
		t.Fatal("a nil error must not be counted as a failure")
	}
	if classifyFailure(fmt.Errorf("boom")).Weight == 0 {
		t.Fatal("a generic error must be counted as a failure")
	}
}
