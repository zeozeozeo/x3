package minilm

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

type Embedder interface {
	Embed(text string) ([]float32, error)
}

type Model struct {
	tokenizer  Tokenizer
	session    *ort.DynamicAdvancedSession
	inputNames []string
	outputInfo ort.InputOutputInfo
	mu         sync.Mutex
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

	inputs, outputs, err := ort.GetInputOutputInfoWithONNXData(modelData)
	if err != nil {
		return nil, fmt.Errorf("read MiniLM model metadata: %w", err)
	}
	inputNames := selectMiniLMInputNames(inputs)
	if len(inputNames) == 0 {
		return nil, fmt.Errorf("MiniLM model has no supported inputs")
	}
	outputInfo, err := selectMiniLMOutput(outputs)
	if err != nil {
		return nil, err
	}

	session, err := ort.NewDynamicAdvancedSessionWithONNXData(
		modelData,
		inputNames,
		[]string{outputInfo.Name},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create MiniLM session: %w", err)
	}

	slog.Info("MiniLM continuation model loaded", "path", config.ModelPath, "inputs", inputNames, "output", outputInfo.Name, "output_shape", outputInfo.Dimensions.String())
	return &Model{tokenizer: NewTokenizer(), session: session, inputNames: inputNames, outputInfo: outputInfo}, nil
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

	outputShape := runtimeOutputShape(m.outputInfo.Dimensions, seqLen)
	output, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, fmt.Errorf("create MiniLM output tensor: %w", err)
	}
	defer output.Destroy()

	inputs, err := m.inputsForRun(inputIDs, attentionMask, tokenTypeIDs)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	err = m.session.Run(
		inputs,
		[]ort.Value{output},
	)
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("run MiniLM session: %w", err)
	}

	embedding, err := sentenceEmbeddingFromOutput(output.GetData(), output.GetShape(), attention)
	if err != nil {
		return nil, err
	}
	return NormalizeVector(embedding), nil
}

func (m *Model) inputsForRun(inputIDs, attentionMask, tokenTypeIDs ort.Value) ([]ort.Value, error) {
	inputs := make([]ort.Value, 0, len(m.inputNames))
	for _, name := range m.inputNames {
		switch name {
		case "input_ids":
			inputs = append(inputs, inputIDs)
		case "attention_mask":
			inputs = append(inputs, attentionMask)
		case "token_type_ids":
			inputs = append(inputs, tokenTypeIDs)
		default:
			return nil, fmt.Errorf("unsupported MiniLM input %q", name)
		}
	}
	return inputs, nil
}

func selectMiniLMInputNames(inputs []ort.InputOutputInfo) []string {
	byName := make(map[string]struct{}, len(inputs))
	for _, input := range inputs {
		byName[input.Name] = struct{}{}
	}
	wanted := []string{"input_ids", "attention_mask", "token_type_ids"}
	names := make([]string, 0, len(wanted))
	for _, name := range wanted {
		if _, ok := byName[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

func selectMiniLMOutput(outputs []ort.InputOutputInfo) (ort.InputOutputInfo, error) {
	if len(outputs) == 0 {
		return ort.InputOutputInfo{}, fmt.Errorf("MiniLM model has no outputs")
	}
	for _, preferred := range []string{"sentence_embedding", "sentence_embeddings", "last_hidden_state", "token_embeddings"} {
		for _, output := range outputs {
			if output.Name == preferred {
				return output, nil
			}
		}
	}
	for _, output := range outputs {
		name := strings.ToLower(output.Name)
		if strings.Contains(name, "sentence") || strings.Contains(name, "embedding") || strings.Contains(name, "hidden") {
			return output, nil
		}
	}
	return outputs[0], nil
}

func runtimeOutputShape(shape ort.Shape, seqLen int) ort.Shape {
	if len(shape) == 0 {
		return ort.NewShape(1, 384)
	}
	out := shape.Clone()
	for i, dim := range out {
		if dim > 0 {
			continue
		}
		switch i {
		case 0:
			out[i] = 1
		case 1:
			if len(out) == 3 {
				out[i] = int64(seqLen)
			} else {
				out[i] = 384
			}
		default:
			out[i] = 384
		}
	}
	return out
}

func sentenceEmbeddingFromOutput(data []float32, shape ort.Shape, attention []int64) ([]float32, error) {
	if len(shape) == 2 {
		if shape[0] != 1 {
			return nil, fmt.Errorf("unsupported sentence embedding batch size %d", shape[0])
		}
		embedding := make([]float32, int(shape[1]))
		copy(embedding, data)
		return embedding, nil
	}
	if len(shape) != 3 || shape[0] != 1 {
		return nil, fmt.Errorf("unsupported MiniLM output shape %s", shape.String())
	}
	seqLen := int(shape[1])
	dim := int(shape[2])
	if seqLen <= 0 || dim <= 0 || len(data) != seqLen*dim {
		return nil, fmt.Errorf("invalid MiniLM token embedding output shape %s with %d values", shape.String(), len(data))
	}

	embedding := make([]float32, dim)
	var count int
	for token := 0; token < seqLen && token < len(attention); token++ {
		if attention[token] == 0 {
			continue
		}
		offset := token * dim
		for i := 0; i < dim; i++ {
			embedding[i] += data[offset+i]
		}
		count++
	}
	if count == 0 {
		return nil, fmt.Errorf("MiniLM attention mask had no active tokens")
	}
	scale := float32(1.0 / float32(count))
	for i := range embedding {
		embedding[i] *= scale
	}
	return embedding, nil
}
