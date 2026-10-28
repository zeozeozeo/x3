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

var sessionIDPattern = regexp.MustCompile(`^ses_[0-9a-f]{12}[A-Za-z0-9]{14}$`)

func TestSessionIDFormat(t *testing.T) {
	for range 20 {
		id := SessionID()
		if !sessionIDPattern.MatchString(id) {
			t.Fatalf("session id %q does not match the expected format", id)
		}
	}
}

func TestUserAgentUsesV2Format(t *testing.T) {
	ua := UserAgent("")
	want := "opencode/latest/" + DefaultVersion + "/opencode"
	if ua != want {
		t.Fatalf("unexpected user agent %q, want %q", ua, want)
	}
}

func TestUserAgentUpgradesStaleV1Version(t *testing.T) {
	ua := UserAgent("1.19.3")
	if strings.Contains(ua, "/1.19.3/") {
		t.Fatalf("stale v1 version leaked into the user agent: %q", ua)
	}
	if !strings.Contains(ua, "/"+DefaultVersion+"/") {
		t.Fatalf("user agent did not fall back to %s: %q", DefaultVersion, ua)
	}
}

func TestHeadersContainV2Fingerprint(t *testing.T) {
	headers := Headers("2.2.0")

	required := []string{
		"User-Agent",
		"x-opencode-client",
		"x-opencode-project",
		"x-opencode-session",
		"x-opencode-session-id",
		"x-session-affinity",
		"X-Session-Id",
	}
	for _, key := range required {
		if headers[key] == "" {
			t.Fatalf("header %q is missing", key)
		}
	}

	if !strings.HasPrefix(headers["User-Agent"], "opencode/latest/2.2.0/opencode") {
		t.Fatalf("unexpected user agent %q", headers["User-Agent"])
	}
	if headers["x-opencode-client"] != ClientName {
		t.Fatalf("unexpected client %q", headers["x-opencode-client"])
	}
	if headers["x-opencode-project"] != ProjectID {
		t.Fatalf("unexpected project %q", headers["x-opencode-project"])
	}
	sessionID := headers["x-opencode-session"]
	if !sessionIDPattern.MatchString(sessionID) {
		t.Fatalf("unexpected session id %q", sessionID)
	}
	if headers["x-opencode-session-id"] != sessionID {
		t.Fatalf("session-id header %q does not match session %q", headers["x-opencode-session-id"], sessionID)
	}
	if headers["x-session-affinity"] != sessionID || headers["X-Session-Id"] != sessionID {
		t.Fatalf("affinity headers do not match the session id")
	}
	if _, ok := headers["x-opencode-request"]; ok {
		t.Fatalf("the v1 request id header is still present")
	}
}

// TestCreateChatCompletionSatisfiesFingerprint checks the request the client
// sends: the v2 fingerprint headers, a streamed body, and no injected tools.
func TestCreateChatCompletionSatisfiesFingerprint(t *testing.T) {
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

	if got := gotHeaders.Get("User-Agent"); got != "opencode/latest/2.2.0/opencode" {
		t.Fatalf("unexpected user agent %q", got)
	}
	if got := gotHeaders.Get("x-opencode-session"); !sessionIDPattern.MatchString(got) {
		t.Fatalf("unexpected session header %q", got)
	}
	if got := gotHeaders.Get("x-opencode-session-id"); got == "" {
		t.Fatalf("missing session-id header")
	}
	if got := gotHeaders.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("unexpected authorization header %q", got)
	}

	stream, ok := gotBody["stream"].(bool)
	if !ok || !stream {
		t.Fatalf("request body does not contain stream: true: %v", gotBody)
	}
	if _, ok := gotBody["tools"]; ok {
		t.Fatalf("the client injected a tool into the request body: %v", gotBody)
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
