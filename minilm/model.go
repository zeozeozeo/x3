package minilm

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

type Embedder interface {
	Embed(text string) ([]float32, error)
}

type Model struct {
	tokenizer Tokenizer
	session   *ort.DynamicAdvancedSession
	mu        sync.Mutex
}

func NewModel(config Config) (*Model, error) {
	if config.RuntimeLibraryPath != "" {
		ort.SetSharedLibraryPath(config.RuntimeLibraryPath)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
	}

	modelData, err := os.ReadFile(config.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("read MiniLM model %q: %w", config.ModelPath, err)
	}

	session, err := ort.NewDynamicAdvancedSessionWithONNXData(
		modelData,
		[]string{"input_ids", "attention_mask", "token_type_ids"},
		[]string{"sentence_embedding"},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create MiniLM session: %w", err)
	}

	slog.Info("MiniLM continuation model loaded", "path", config.ModelPath)
	return &Model{tokenizer: NewTokenizer(), session: session}, nil
}

func (m *Model) Close() {
	if m == nil || m.session == nil {
		return
	}
	m.session.Destroy()
}

func (m *Model) Embed(text string) ([]float32, error) {
	if m == nil || m.session == nil {
		return nil, fmt.Errorf("MiniLM model is not initialized")
	}

	ids, attention, tokenTypes := m.tokenizer.Encode(text)
	seqLen := len(ids)
	if seqLen == 0 {
		return nil, fmt.Errorf("MiniLM tokenizer produced no tokens")
	}
	inputShape := ort.NewShape(1, int64(seqLen))

	inputIDs, err := ort.NewTensor(inputShape, ids)
	if err != nil {
		return nil, fmt.Errorf("create input_ids tensor: %w", err)
	}
	defer inputIDs.Destroy()

	attentionMask, err := ort.NewTensor(inputShape, attention)
	if err != nil {
		return nil, fmt.Errorf("create attention_mask tensor: %w", err)
	}
	defer attentionMask.Destroy()

	tokenTypeIDs, err := ort.NewTensor(inputShape, tokenTypes)
	if err != nil {
		return nil, fmt.Errorf("create token_type_ids tensor: %w", err)
	}
	defer tokenTypeIDs.Destroy()

	output, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 384))
	if err != nil {
		return nil, fmt.Errorf("create sentence_embedding tensor: %w", err)
	}
	defer output.Destroy()

	m.mu.Lock()
	err = m.session.Run(
		[]ort.Value{inputIDs, attentionMask, tokenTypeIDs},
		[]ort.Value{output},
	)
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("run MiniLM session: %w", err)
	}

	data := output.GetData()
	if len(data) != 384 {
		return nil, fmt.Errorf("unexpected MiniLM output size: got %d, want 384", len(data))
	}
	embedding := make([]float32, len(data))
	copy(embedding, data)
	return NormalizeVector(embedding), nil
}
