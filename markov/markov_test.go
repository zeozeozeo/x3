package markov_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/zeozeozeo/x3/markov"
)

type mockRNG struct {
	values []int
	index  int
}

func (m *mockRNG) Intn(n int) int {
	if m.index >= len(m.values) {
		panic(fmt.Sprintf("mockRNG exhausted, needed value for n=%d", n))
	}
	val := m.values[m.index]
	m.index++
	if val >= n && n > 0 {
		panic(fmt.Sprintf("mockRNG value %d out of range for n=%d", val, n))
	}
	if n <= 0 && val != 0 {
		panic(fmt.Sprintf("mockRNG value %d provided but Intn called with n=%d", val, n))
	}
	if n == 1 && val != 0 {
		panic(fmt.Sprintf("mockRNG value %d provided but Intn called with n=%d, expected 0", val, n))
	}

	return val
}

func floatEquals(a, b float64) bool {
	const tolerance = 1e-9
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	if math.IsInf(a, 0) && math.IsInf(b, 0) && math.Signbit(a) == math.Signbit(b) {
		return true
	}
	return math.Abs(a-b) < tolerance
}

func TestNewChain(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(2)
	if c == nil {
		t.Fatal("NewChain returned nil")
	}
	if c.Order != 2 {
		t.Errorf("Expected order 2, got %d", c.Order)
	}

	cZero := markov.NewChain(0)
	if cZero.Order != 1 {
		t.Errorf("Expected order 1 for input 0, got %d", cZero.Order)
	}

	cNegative := markov.NewChain(-5)
	if cNegative.Order != 1 {
		t.Errorf("Expected order 1 for input -5, got %d", cNegative.Order)
	}
}

func TestAddAndGenerateOrder1(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(1)
	input := []string{"a", "b", "a", "c"}
	c.Add(input)

	tests := []struct {
		name      string
		start     markov.NGram
		rng       *mockRNG
		wantToken string
		wantErr   bool
	}{
		{
			name:      "From start token, select a",
			start:     markov.NGram{markov.StartToken},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "a",
			wantErr:   false,
		},
		{
			name:      "From a, select b",
			start:     markov.NGram{"a"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "b",
			wantErr:   false,
		},
		{
			name:      "From a, select c",
			start:     markov.NGram{"a"},
			rng:       &mockRNG{values: []int{1}},
			wantToken: "c",
			wantErr:   false,
		},
		{
			name:      "From b, select a",
			start:     markov.NGram{"b"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "a",
			wantErr:   false,
		},
		{
			name:      "From c, select end",
			start:     markov.NGram{"c"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: markov.EndToken,
			wantErr:   false,
		},
		{
			name:      "From end, generate empty",
			start:     markov.NGram{markov.EndToken},
			rng:       &mockRNG{values: []int{}},
			wantToken: "",
			wantErr:   false,
		},
		{
			name:      "Unknown state",
			start:     markov.NGram{"unknown"},
			rng:       &mockRNG{values: []int{}},
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "Wrong ngram length",
			start:     markov.NGram{"a", "b"},
			rng:       &mockRNG{values: []int{}},
			wantToken: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotToken, err := c.GenerateDeterministic(tt.start, tt.rng)

			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateDeterministic() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotToken != tt.wantToken {
				t.Errorf("GenerateDeterministic() gotToken = %v, want %v", gotToken, tt.wantToken)
			}
			if !tt.wantErr && tt.rng.index < len(tt.rng.values) {
				t.Errorf("mockRNG not fully consumed, index %d, len %d", tt.rng.index, len(tt.rng.values))
			}
		})
	}
}

func TestAddAndGenerateOrder2(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(2)
	input := []string{"the", "quick", "brown", "fox", "the", "quick", "red", "fox"}
	c.Add(input)

	tests := []struct {
		name      string
		start     markov.NGram
		rng       *mockRNG
		wantToken string
		wantErr   bool
	}{
		{
			name:      "From start tokens",
			start:     markov.NGram{markov.StartToken, markov.StartToken},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "the",
			wantErr:   false,
		},
		{
			name:      "From ^ the",
			start:     markov.NGram{markov.StartToken, "the"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "quick",
			wantErr:   false,
		},
		{
			name:      "From the quick, select brown",
			start:     markov.NGram{"the", "quick"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "brown",
			wantErr:   false,
		},
		{
			name:      "From the quick, select red",
			start:     markov.NGram{"the", "quick"},
			rng:       &mockRNG{values: []int{1}},
			wantToken: "red",
			wantErr:   false,
		},
		{
			name:      "From brown fox",
			start:     markov.NGram{"brown", "fox"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: "the",
			wantErr:   false,
		},
		{
			name:      "From red fox",
			start:     markov.NGram{"red", "fox"},
			rng:       &mockRNG{values: []int{0}},
			wantToken: markov.EndToken,
			wantErr:   false,
		},
		{
			name:      "From state ending in end token",
			start:     markov.NGram{"fox", markov.EndToken},
			rng:       &mockRNG{values: []int{}},
			wantToken: "",
			wantErr:   false,
		},
		{
			name:      "Unknown state",
			start:     markov.NGram{"unknown", "state"},
			rng:       &mockRNG{values: []int{}},
			wantToken: "",
			wantErr:   true,
		},
		{
			name:      "Wrong ngram length",
			start:     markov.NGram{"a"},
			rng:       &mockRNG{values: []int{}},
			wantToken: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotToken, err := c.GenerateDeterministic(tt.start, tt.rng)

			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateDeterministic() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gotToken != tt.wantToken {
				t.Errorf("GenerateDeterministic() gotToken = %v, want %v", gotToken, tt.wantToken)
			}
			if !tt.wantErr && tt.rng.index < len(tt.rng.values) {
				t.Errorf("mockRNG not fully consumed, index %d, len %d", tt.rng.index, len(tt.rng.values))
			}
		})
	}
}

func TestTransitionProbabilityOrder1(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(1)
	input := []string{"a", "b", "a", "c", "a", "b"}
	c.Add(input)

	tests := []struct {
		name       string
		current    markov.NGram
		next       string
		wantProb   float64
		wantErrStr string
	}{
		{
			name:     "Start to a",
			current:  markov.NGram{markov.StartToken},
			next:     "a",
			wantProb: 1.0,
		},
		{
			name:     "a to b",
			current:  markov.NGram{"a"},
			next:     "b",
			wantProb: 2.0 / 3.0,
		},
		{
			name:     "a to c",
			current:  markov.NGram{"a"},
			next:     "c",
			wantProb: 1.0 / 3.0,
		},
		{
			name:     "b to a",
			current:  markov.NGram{"b"},
			next:     "a",
			wantProb: 0.5, // Corrected: b->a (1), b->$ (1)
		},
		{
			name:     "b to end",
			current:  markov.NGram{"b"},
			next:     markov.EndToken,
			wantProb: 0.5, // Corrected: b->a (1), b->$ (1)
		},
		{
			name:     "c to a",
			current:  markov.NGram{"c"},
			next:     "a",
			wantProb: 1.0, // c->a (1)
		},
		{
			name:     "Unknown current",
			current:  markov.NGram{"x"},
			next:     "a",
			wantProb: 0.0,
		},
		{
			name:     "Unknown next",
			current:  markov.NGram{"a"},
			next:     "x",
			wantProb: 0.0,
		},
		{
			name:       "Wrong ngram length",
			current:    markov.NGram{"a", "b"},
			next:       "c",
			wantProb:   0.0,
			wantErrStr: "n-gram length does not match chain order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotProb, err := c.TransitionProbability(tt.next, tt.current)

			if tt.wantErrStr != "" {
				if err == nil {
					t.Fatalf("TransitionProbability() error = nil, wantErr %v", tt.wantErrStr)
				}
				if !strings.Contains(err.Error(), tt.wantErrStr) {
					t.Errorf("TransitionProbability() error = %q, want substring %q", err.Error(), tt.wantErrStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("TransitionProbability() unexpected error = %v", err)
			}

			if !floatEquals(gotProb, tt.wantProb) {
				t.Errorf("TransitionProbability() gotProb = %v, want %v", gotProb, tt.wantProb)
			}
		})
	}
}

func TestTransitionProbabilityOrder2(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(2)
	input := []string{"a", "b", "c", "a", "b", "d"}
	c.Add(input)

	tests := []struct {
		name       string
		current    markov.NGram
		next       string
		wantProb   float64
		wantErrStr string
	}{
		{
			name:     "Start Start to a",
			current:  markov.NGram{markov.StartToken, markov.StartToken},
			next:     "a",
			wantProb: 1.0,
		},
		{
			name:     "Start a to b",
			current:  markov.NGram{markov.StartToken, "a"},
			next:     "b",
			wantProb: 1.0,
		},
		{
			name:     "a b to c",
			current:  markov.NGram{"a", "b"},
			next:     "c",
			wantProb: 0.5,
		},
		{
			name:     "a b to d",
			current:  markov.NGram{"a", "b"},
			next:     "d",
			wantProb: 0.5,
		},
		{
			name:     "b c to a",
			current:  markov.NGram{"b", "c"},
			next:     "a",
			wantProb: 1.0,
		},
		{
			name:     "c a to b",
			current:  markov.NGram{"c", "a"},
			next:     "b",
			wantProb: 1.0,
		},
		{
			name:     "b d to end",
			current:  markov.NGram{"b", "d"},
			next:     markov.EndToken,
			wantProb: 1.0,
		},
		{
			name:     "Unknown current",
			current:  markov.NGram{"x", "y"},
			next:     "a",
			wantProb: 0.0,
		},
		{
			name:     "Unknown next",
			current:  markov.NGram{"a", "b"},
			next:     "x",
			wantProb: 0.0,
		},
		{
			name:       "Wrong ngram length",
			current:    markov.NGram{"a"},
			next:       "c",
			wantProb:   0.0,
			wantErrStr: "n-gram length does not match chain order",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotProb, err := c.TransitionProbability(tt.next, tt.current)

			if tt.wantErrStr != "" {
				if err == nil {
					t.Fatalf("TransitionProbability() error = nil, wantErr %v", tt.wantErrStr)
				}
				if !strings.Contains(err.Error(), tt.wantErrStr) {
					t.Errorf("TransitionProbability() error = %q, want substring %q", err.Error(), tt.wantErrStr)
				}
				return
			}

			if err != nil {
				t.Fatalf("TransitionProbability() unexpected error = %v", err)
			}

			if !floatEquals(gotProb, tt.wantProb) {
				t.Errorf("TransitionProbability() gotProb = %v, want %v", gotProb, tt.wantProb)
			}
		})
	}
}

func TestAddEmpty(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(1)
	c.Add([]string{})

	prob, err := c.TransitionProbability(markov.EndToken, markov.NGram{markov.StartToken})
	if err != nil {
		t.Fatalf("TransitionProbability unexpected error: %v", err)
	}
	if !floatEquals(prob, 1.0) {
		t.Errorf("Expected probability of 1.0 for ^ -> $ on empty add, got %f", prob)
	}

	// Use a mock RNG that returns 0 for the single choice (^ -> $)
	rng := &mockRNG{values: []int{0}}
	token, err := c.GenerateDeterministic(markov.NGram{markov.StartToken}, rng)
	if err != nil {
		t.Fatalf("GenerateDeterministic unexpected error: %v", err)
	}
	if token != markov.EndToken {
		t.Errorf("Expected generation of %s from ^ on empty add, got %s", markov.EndToken, token)
	}
	if rng.index != 1 {
		t.Errorf("Expected mockRNG index to be 1, got %d", rng.index)
	}
}

func TestGenerateIntegration(t *testing.T) {
	t.Parallel()
	c := markov.NewChain(2)
	input := []string{"I", "am", "Sam", "Sam", "I", "am"}
	c.Add(input)

	// ^ ^ -> I   (n=1, val=0)
	// ^ I -> am  (n=1, val=0)
	// I am -> Sam (n=2, val=0)  [other choice is $]
	// am Sam -> Sam (n=1, val=0)
	// Sam Sam -> I (n=1, val=0)
	// Sam I -> am (n=1, val=0)
	// I am -> $   (n=2, val=1)  [other choice is Sam]
	rng := &mockRNG{values: []int{0, 0, 0, 0, 0, 0, 1}}
	var result []string
	current := markov.NGram{markov.StartToken, markov.StartToken}
	maxSteps := 20

	for i := range maxSteps {
		next, err := c.GenerateDeterministic(current, rng)
		if err != nil {
			if strings.Contains(err.Error(), "unknown ngram state") {
				t.Fatalf("Generate hit unknown state %v on iteration %d: %v", current, i, err)
			} else {
				t.Fatalf("Generate error on iteration %d for state %v: %v", i, current, err)
			}
		}

		if next == "" || next == markov.EndToken {
			break
		}
		result = append(result, next)
		if len(current) != c.Order {
			t.Fatalf("Internal test error: current ngram %v length mismatch on iteration %d", current, i)
		}
		current = append(current[1:], next)
	}

	expected := []string{"I", "am", "Sam", "Sam", "I", "am"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Generated sequence mismatch:\nGot:  %v\nWant: %v", result, expected)
	}

	expectedRngConsumed := 7
	if rng.index != expectedRngConsumed {
		t.Errorf("Expected mockRNG to consume %d values, consumed %d", expectedRngConsumed, rng.index)
	}
}
