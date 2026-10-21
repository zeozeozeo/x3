package opencode

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/zeozeozeo/x3/openai"
)

var (
	sessionIDPattern = regexp.MustCompile(`^ses_[0-9a-f]{12}[A-Za-z0-9]{14}$`)
	requestIDPattern = regexp.MustCompile(`^msg_[A-Za-z0-9]{26}$`)
)

func TestSessionIDFormat(t *testing.T) {
	for range 20 {
		id := SessionID()
		if !sessionIDPattern.MatchString(id) {
			t.Fatalf("session id %q does not match the expected format", id)
		}
	}
}

func TestRequestIDFormat(t *testing.T) {
	for range 20 {
		id := RequestID()
		if !requestIDPattern.MatchString(id) {
			t.Fatalf("request id %q does not match the expected format", id)
		}
	}
}

func TestHeadersContainFingerprint(t *testing.T) {
	headers := Headers("1.19.3")

	required := []string{
		"User-Agent",
		"x-opencode-client",
		"x-opencode-project",
		"x-opencode-session",
		"x-opencode-request",
	}
	for _, key := range required {
		if headers[key] == "" {
			t.Fatalf("header %q is missing", key)
		}
	}

	if !strings.HasPrefix(headers["User-Agent"], "opencode/1.19.3") {
		t.Fatalf("unexpected user agent %q", headers["User-Agent"])
	}
	if !sessionIDPattern.MatchString(headers["x-opencode-session"]) {
		t.Fatalf("unexpected session id %q", headers["x-opencode-session"])
	}
	if !requestIDPattern.MatchString(headers["x-opencode-request"]) {
		t.Fatalf("unexpected request id %q", headers["x-opencode-request"])
	}
}

func TestUserAgentDefaultsToSupportedVersion(t *testing.T) {
	ua := UserAgent("")
	if !strings.HasPrefix(ua, "opencode/"+DefaultVersion) {
		t.Fatalf("unexpected user agent %q", ua)
	}
}

func TestEnsureGateTool(t *testing.T) {
	tools := EnsureGateTool(nil)
	if len(tools) != 1 || tools[0].Function == nil || tools[0].Function.Name != GateToolName {
		t.Fatalf("gate tool was not added: %+v", tools)
	}

	again := EnsureGateTool(tools)
	if len(again) != 1 {
		t.Fatalf("gate tool was added twice: %+v", again)
	}
}

// TestCreateChatCompletionSatisfiesGate checks the request the client sends:
// the fingerprint headers, a streamed body and a tool named "read".
func TestCreateChatCompletionSatisfiesGate(t *testing.T) {
	var (
		gotHeaders http.Header
		gotBody    map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
			return
		}
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("failed to parse body: %v", err)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"id":"1","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
			`{"id":"1","object":"chat.completion.chunk","model":"test-model","choices":[{"index":0,"delta":{"content":" world"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
		}
		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	config := openai.DefaultConfig("test-token")
	config.BaseURL = server.URL
	config.DefaultHeaders = Headers(DefaultVersion)
	client := openai.NewClientWithConfig(config)

	req := openai.ChatCompletionRequest{
		Model:    "test-model",
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "hi"}},
	}

	response, err := CreateChatCompletion(t.Context(), client, req)
	if err != nil {
		t.Fatalf("completion failed: %v", err)
	}

	if got := gotHeaders.Get("User-Agent"); !strings.HasPrefix(got, "opencode/") {
		t.Fatalf("unexpected user agent %q", got)
	}
	if got := gotHeaders.Get("x-opencode-session"); !sessionIDPattern.MatchString(got) {
		t.Fatalf("unexpected session header %q", got)
	}
	if got := gotHeaders.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("unexpected authorization header %q", got)
	}

	stream, ok := gotBody["stream"].(bool)
	if !ok || !stream {
		t.Fatalf("request body does not contain stream: true: %v", gotBody)
	}

	tools, ok := gotBody["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("request body does not contain a tools array: %v", gotBody)
	}
	found := false
	for _, tool := range tools {
		toolMap, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		function, ok := toolMap["function"].(map[string]any)
		if !ok {
			continue
		}
		if function["name"] == GateToolName {
			found = true
		}
	}
	if !found {
		t.Fatalf("request body does not contain a tool named %q: %v", GateToolName, gotBody)
	}

	if len(response.Choices) != 1 {
		t.Fatalf("unexpected choices: %+v", response.Choices)
	}
	if response.Choices[0].Message.Content != "Hello world" {
		t.Fatalf("unexpected content %q", response.Choices[0].Message.Content)
	}
	if response.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected usage %+v", response.Usage)
	}
}
