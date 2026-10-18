package modeled

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeozeozeo/x3/logstream"
)

func TestLogStreamSendsBacklogAndLive(t *testing.T) {
	s, ts := newTestServer(t, testConfig())
	token := s.newSessionToken(time.Now().Add(time.Hour))

	// Seed a distinctive backlog line in the process-wide hub.
	logstream.Default.Write([]byte("log-test-backlog\n"))

	ctx := t.Context()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/x3-secret/api/logs", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Cookie", sessionCookieName+"="+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}

	done := make(chan struct{})
	defer close(done)
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			select {
			case lines <- line:
			case <-done:
				return
			}
		}
	}()

	expect := func(needle string) {
		t.Helper()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stream closed before %q arrived", needle)
				}
				if strings.Contains(line, needle) {
					return
				}
			case <-timer.C:
				t.Fatalf("timed out waiting for %q", needle)
			}
		}
	}

	expect("log-test-backlog")

	// A line written after the connection opened must be delivered live.
	logstream.Default.Write([]byte("log-test-live\n"))
	expect("log-test-live")
}

func TestLogStreamRequiresAuth(t *testing.T) {
	_, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	resp := doRequest(t, client, http.MethodGet, ts.URL+"/x3-secret/api/logs", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated log stream = %d, want 401", resp.StatusCode)
	}
}
