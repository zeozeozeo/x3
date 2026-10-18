// Package logstream captures the process log output so other components (the
// model editor's log tab) can display the same lines the container prints, live.
//
// Install replaces the default slog logger with one that writes to both stderr
// (which Docker captures) and an in-memory hub. Everything the container shows
// in `docker logs` is therefore also available to in-process subscribers.
package logstream

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

const (
	defaultCapacity = 2000
	// maxPartialBytes bounds the buffer of a line that never received a newline
	// so a misbehaving writer cannot grow memory without limit.
	maxPartialBytes = 64 << 10
)

// Hub stores the most recent log lines and fans new ones out to subscribers.
// It is safe for concurrent use and implements io.Writer.
type Hub struct {
	mu       sync.Mutex
	capacity int
	lines    []string
	partial  []byte
	subs     map[int]chan string
	nextID   int
}

// Default is the process-wide hub wired up by Install.
var Default = NewHub(defaultCapacity)

var levelVar = new(slog.LevelVar)

// NewHub returns a hub that keeps at most capacity lines.
func NewHub(capacity int) *Hub {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &Hub{
		capacity: capacity,
		subs:     make(map[int]chan string),
	}
}

// Install points the default slog logger at stderr and the default hub. It must
// be called before the first log line is emitted.
func Install(level slog.Level) {
	levelVar.Set(level)
	slog.SetDefault(slog.New(slog.NewTextHandler(
		io.MultiWriter(os.Stderr, Default),
		&slog.HandlerOptions{Level: levelVar},
	)))
	// Keep logs routed through the standard "log" package (Printf, etc.) aligned
	// with the same level.
	slog.SetLogLoggerLevel(level)
}

// SetLevel updates the minimum level for the default logger at runtime, e.g.
// after X3_LOG_LEVEL changes.
func SetLevel(level slog.Level) {
	levelVar.Set(level)
	slog.SetLogLoggerLevel(level)
}

// Write implements io.Writer. Incoming data is split on newlines and each
// complete line is stored and published. A trailing partial line is buffered
// until the next write.
func (h *Hub) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.partial = append(h.partial, p...)
	for {
		idx := indexByte(h.partial, '\n')
		if idx < 0 {
			break
		}
		h.publishLocked(string(h.partial[:idx]))
		h.partial = h.partial[idx+1:]
	}
	if len(h.partial) == 0 {
		h.partial = nil
	} else if len(h.partial) > maxPartialBytes {
		h.publishLocked(string(h.partial))
		h.partial = nil
	}
	return len(p), nil
}

func (h *Hub) publishLocked(line string) {
	line = strings.TrimRight(line, "\r")
	h.lines = append(h.lines, line)
	// Trim in batches to keep appends amortized O(1).
	if len(h.lines) > h.capacity*2 {
		h.lines = append([]string(nil), h.lines[len(h.lines)-h.capacity:]...)
	}
	for _, ch := range h.subs {
		select {
		case ch <- line:
		default:
			// A slow subscriber drops lines rather than blocking logging.
		}
	}
}

// Lines returns a copy of the retained lines, oldest first.
func (h *Hub) Lines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	start := 0
	if len(h.lines) > h.capacity {
		start = len(h.lines) - h.capacity
	}
	return append([]string(nil), h.lines[start:]...)
}

// Subscribe returns a channel of new lines and a function that unsubscribes.
// The returned channel is closed by the unsubscribe function.
func (h *Hub) Subscribe() (<-chan string, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.nextID
	h.nextID++
	ch := make(chan string, 512)
	h.subs[id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(ch)
		}
	}
}

// Lines returns a copy of the default hub's retained lines.
func Lines() []string { return Default.Lines() }

// Subscribe subscribes to the default hub.
func Subscribe() (<-chan string, func()) { return Default.Subscribe() }

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
