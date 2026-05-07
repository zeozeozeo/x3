package minilm

import (
	"log/slog"
	"sync"
)

var global struct {
	mu       sync.Mutex
	embedder Embedder
	err      error
}

func GlobalEmbedder() (Embedder, error) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if global.embedder != nil || global.err != nil {
		return global.embedder, global.err
	}
	global.embedder, global.err = NewModel(LoadConfig())
	if global.err != nil {
		slog.Warn("MiniLM continuation model unavailable", "err", global.err)
	}
	return global.embedder, global.err
}

func SetGlobalEmbedderForTest(embedder Embedder) func() {
	global.mu.Lock()
	prevEmbedder := global.embedder
	prevErr := global.err
	global.embedder = embedder
	global.err = nil
	global.mu.Unlock()

	return func() {
		global.mu.Lock()
		global.embedder = prevEmbedder
		global.err = prevErr
		global.mu.Unlock()
	}
}
