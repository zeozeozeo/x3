package modeled

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/logstream"
)

const logStreamHeartbeat = 15 * time.Second

// handleLogStream streams the process log as Server-Sent Events. The lines are
// exactly what the container writes to stderr, so this mirrors `docker logs`.
// Each connection first receives a "reset" event, then the buffered backlog,
// then live lines as they happen.
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeSSE(w, "reset", "")
	for _, line := range logstream.Lines() {
		writeSSE(w, "line", line)
	}
	flusher.Flush()

	lines, unsubscribe := logstream.Subscribe()
	defer unsubscribe()

	heartbeat := time.NewTicker(logStreamHeartbeat)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// Keeps proxies (cloudflared, Cloudflare) from closing idle streams.
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		case line, ok := <-lines:
			if !ok {
				return
			}
			writeSSE(w, "line", line)
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, event, data string) {
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	// A data field must not contain a literal newline, so split defensively.
	for part := range strings.SplitSeq(data, "\n") {
		fmt.Fprintf(w, "data: %s\n", part)
	}
	fmt.Fprint(w, "\n")
}
