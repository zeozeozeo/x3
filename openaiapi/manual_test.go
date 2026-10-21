package openaiapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// TestManual exercises the proxy against the real providers. It is skipped
// unless X3_MANUAL=1 is set.
func TestManual(t *testing.T) {
	if os.Getenv("X3_MANUAL") != "1" {
		t.Skip("set X3_MANUAL=1 to run")
	}
	if err := godotenv.Load("../.env"); err != nil {
		t.Fatalf("failed to load .env: %v", err)
	}

	cfg := ConfigFromEnv()
	cfg.BindAddr = "127.0.0.1:16745"
	srv := NewServer(cfg)
	go func() {
		if err := srv.Start(); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()
	time.Sleep(500 * time.Millisecond)

	client := &http.Client{Timeout: 2 * time.Minute}

	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:16745/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.APIKeys[0])
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("models request failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("models status: %d", resp.StatusCode)
	var list map[string]any
	_ = json.Unmarshal(body, &list)
	entries, _ := list["data"].([]any)
	t.Logf("models: %d entries, first: %v", len(entries), entries[0])

	for _, modelName := range []string{"qwen", "glmflash", "scout", "gemini"} {
		payload := map[string]any{
			"model":    modelName,
			"messages": []map[string]string{{"role": "user", "content": "Say hello in exactly three words."}},
		}
		raw, _ := json.Marshal(payload)
		req, _ = http.NewRequest(http.MethodPost, "http://127.0.0.1:16745/v1/chat/completions", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cfg.APIKeys[0])
		resp, err = client.Do(req)
		if err != nil {
			t.Logf("%s: request failed: %v", modelName, err)
			continue
		}
		body, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("%s: status %d body %s", modelName, resp.StatusCode, string(body))
		if resp.StatusCode == http.StatusOK {
			break
		}
	}

	// streaming
	payload := map[string]any{
		"model":                 "qwen",
		"messages":              []map[string]string{{"role": "user", "content": "Count from one to five."}},
		"stream":                true,
		"max_completion_tokens": 64,
	}
	raw, _ := json.Marshal(payload)
	req, _ = http.NewRequest(http.MethodPost, "http://127.0.0.1:16745/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKeys[0])
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("stream request failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("stream status: %d body %s", resp.StatusCode, string(body))
}
