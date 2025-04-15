package markov

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// StartToken represents the beginning of a sequence.
const StartToken = "^"

// EndToken represents the end of a sequence.
const EndToken = "$"

var (
	// errNgramLengthMismatch is returned when an operation expects an N-gram
	// of a specific length (matching the chain's order) but receives one
	// of a different length.
	errNgramLengthMismatch = errors.New("n-gram length does not match chain order")

	ErrUnknownNgramState = errors.New("unknown ngram state")
)

// Chain represents a Markov chain of a specific order.
// It stores transition frequencies between states (N-grams).
type Chain struct {
	Order     int
	statePool *spool
	// frequencyMat stores the transition counts.
	frequencyMat map[int]sparseArray
}

type RNG interface {
	Intn(int) int
}

var defaultRng RNG = rand.New(rand.NewSource(time.Now().UnixNano()))

// NewChain creates and initializes a new Markov Chain with the specified order.
func NewChain(order int) *Chain {
	if order < 1 {
		order = 1
	}
	chain := Chain{
		Order: order,
		statePool: &spool{
			stringMap: make(map[string]int),
			intMap:    make(map[int]string),
		},
		frequencyMat: make(map[int]sparseArray),
	}
	return &chain
}

// Add processes a sequence of tokens (strings) and updates the chain's transition frequencies.
// Start and End tokens are automatically added to the beginning and end of the input sequence.
func (chain *Chain) Add(input []string) {
	startTokens := array(StartToken, chain.Order)
	endTokens := array(EndToken, 1)
	totalLen := len(startTokens) + len(input) + len(endTokens)
	tokens := make([]string, 0, totalLen)
	tokens = append(tokens, startTokens...)
	tokens = append(tokens, input...)
	tokens = append(tokens, endTokens...)

	pairs := MakePairs(tokens, chain.Order)

	for _, pair := range pairs {
		currentStateKey := pair.CurrentState.key()
		currentIndex := chain.statePool.add(currentStateKey)
		nextIndex := chain.statePool.add(pair.NextState)

		if _, ok := chain.frequencyMat[currentIndex]; !ok {
			chain.frequencyMat[currentIndex] = make(sparseArray)
		}
		chain.frequencyMat[currentIndex][nextIndex]++
	}
}

// TransitionProbability calculates the probability of transitioning to the 'next' token
// given the 'current' N-gram state.
// It returns 0 probability if the transition has not been observed or if the N-gram
// or next token are unknown.
// Returns an error if the length of the provided N-gram doesn't match the chain's order.
func (chain *Chain) TransitionProbability(next string, current NGram) (float64, error) {
	if len(current) != chain.Order {
		return 0, errNgramLengthMismatch
	}

	currentStateKey := current.key()
	currentIndex, currentExists := chain.statePool.get(currentStateKey)
	nextIndex, nextExists := chain.statePool.get(next)

	if !currentExists || !nextExists {
		return 0, nil
	}

	arr := chain.frequencyMat[currentIndex]
	if len(arr) == 0 {
		return 0, nil
	}

	sum := float64(arr.sum())
	if sum == 0 {
		return 0, nil
	}

	freq := float64(arr[nextIndex])
	return freq / sum, nil
}

// Generate predicts the next token based on the 'current' N-gram state using the default RNG.
// It returns an empty string "" without error if the current N-gram ends with the EndToken,
// indicating the end of a sequence.
// Returns an error if the length of the provided N-gram doesn't match the chain's order,
// or if the N-gram state is unknown to the chain.
func (chain *Chain) Generate(current NGram) (string, error) {
	return chain.GenerateDeterministic(current, defaultRng)
}

// GenerateDeterministic predicts the next token based on the 'current' N-gram state,
// using the provided RNG for weighted random selection over a deterministically sorted
// set of possible next states (ensuring testability).
// It returns an empty string "" without error if the current N-gram ends with the EndToken.
// Returns an error if the length of the provided N-gram doesn't match the chain's order,
// or if the N-gram state is unknown to the chain.
func (chain *Chain) GenerateDeterministic(current NGram, rng RNG) (string, error) {
	if len(current) != chain.Order {
		return "", errNgramLengthMismatch
	}

	if current[len(current)-1] == EndToken {
		return "", nil
	}

	currentStateKey := current.key()
	currentIndex, currentExists := chain.statePool.get(currentStateKey)
	if !currentExists {
		return "", ErrUnknownNgramState
	}

	arr := chain.frequencyMat[currentIndex]
	sum := arr.sum()
	if sum <= 0 {
		return "", nil
	}

	keys := arr.orderedKeys()

	randN := rng.Intn(sum)

	for _, nextIndex := range keys {
		freq := arr[nextIndex]
		randN -= freq
		if randN < 0 {
			nextStateStr, ok := chain.statePool.intMap[nextIndex]
			if !ok {
				return "", fmt.Errorf("internal inconsistency: unknown next state ID %d", nextIndex)
			}
			return nextStateStr, nil
		}
	}

	return "", fmt.Errorf("generation failed unexpectedly for state %v", current)
}
