package schematic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

const (
	DefaultSize       = 128
	MaxAxisSize       = 320
	MaxOccupiedBlocks = 4_000_000
	MaxWriteAttempts  = 16_000_000
	MaxPrimitiveCalls = 250_000
	MaxLoopIterations = 1_000_000
	MaxTemplatePastes = 100_000
	MaxSourceBytes    = 128 << 10
	MaxNestingDepth   = 8
	MaxAttempts       = 5
)

type Bounds struct{ X, Y, Z int }

func ParseBounds(raw string) (Bounds, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return Bounds{DefaultSize, DefaultSize, DefaultSize}, nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == 'x' || r == '×' || r == ',' })
	if len(parts) != 1 && len(parts) != 3 {
		return Bounds{}, fmt.Errorf("size must be one number or WIDTHxHEIGHTxDEPTH")
	}
	values := make([]int, len(parts))
	for i, part := range parts {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%d", &n); err != nil {
			return Bounds{}, fmt.Errorf("invalid size component %q", part)
		}
		if n < 1 || n > MaxAxisSize {
			return Bounds{}, fmt.Errorf("size components must be between 1 and %d", MaxAxisSize)
		}
		values[i] = n
	}
	if len(values) == 1 {
		return Bounds{values[0], values[0], values[0]}, nil
	}
	return Bounds{values[0], values[1], values[2]}, nil
}

type Progress struct {
	Stage   string
	Detail  string
	Attempt int
	Total   int
}

type ProgressFunc func(Progress)

type Request struct {
	Prompt   string
	Bounds   Bounds
	Models   []model.Model
	Settings persona.InferenceSettings
}

type Result struct {
	Archive       []byte
	Source        string
	Usage         llm.Usage
	Attempts      int
	Occupied      int
	Dimensions    Bounds
	MaterialCount int
	Repairs       []AttemptError
}

type AttemptError struct {
	Attempt int
	Stage   string
	Source  string
	Err     error
}

type GenerationError struct{ Attempts []AttemptError }

func (e *GenerationError) Error() string {
	if len(e.Attempts) == 0 {
		return "schematic generation failed"
	}
	last := e.Attempts[len(e.Attempts)-1]
	return fmt.Sprintf("schematic generation failed after %d attempts: %v", len(e.Attempts), last.Err)
}

func (e *GenerationError) Unwrap() error {
	if len(e.Attempts) == 0 {
		return nil
	}
	return e.Attempts[len(e.Attempts)-1].Err
}

var ErrNoBlocks = errors.New("program did not create any blocks")

type CompletionFunc func(ctx context.Context, messages *llm.Llmer, models []model.Model, settings persona.InferenceSettings) (string, llm.Usage, error)

type Generator struct{ Complete CompletionFunc }
