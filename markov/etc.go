package markov

import (
	"sort"
	"strings"
)

// spool provides a bidirectional mapping between strings and integer IDs
type spool struct {
	stringMap map[string]int
	intMap    map[int]string
}

// add adds a string to the pool if it doesn't exist and returns its unique integer ID.
// If the string already exists, it returns the existing ID.
func (s *spool) add(str string) int {
	index, ok := s.stringMap[str]
	if ok {
		return index
	}
	index = len(s.stringMap)
	s.stringMap[str] = index
	s.intMap[index] = str
	return index
}

// get retrieves the integer ID associated with a string.
// It returns the ID and true if the string exists in the pool, otherwise 0 and false.
func (s *spool) get(str string) (int, bool) {
	index, ok := s.stringMap[str]
	return index, ok
}

// Pair represents a current state (N-gram) and the token that follows it.
type Pair struct {
	CurrentState NGram
	NextState    string
}

// NGram is a sequence of tokens representing a state in the Markov chain.
// The length of the slice is equal to the chain's order.
type NGram []string

// key generates a unique string representation for an NGram, suitable for use as a map key.
// It joins the tokens with an underscore "_". Assumes tokens themselves don't contain "_".
func (ngram NGram) key() string {
	if len(ngram) == 0 {
		return ""
	}
	var builder strings.Builder
	estimatedLen := 0
	for _, s := range ngram {
		estimatedLen += len(s)
	}
	estimatedLen += len(ngram) - 1
	builder.Grow(estimatedLen)

	builder.WriteString(ngram[0])
	for i := 1; i < len(ngram); i++ {
		builder.WriteByte('_')
		builder.WriteString(ngram[i])
	}
	return builder.String()
}

// sparseArray maps the integer ID of a next token to its frequency count.
// It represents the outgoing transitions from a specific N-gram state.
type sparseArray map[int]int

// sum calculates the total count of all transitions recorded in the sparseArray.
// This represents the total number of times the source N-gram state was observed.
func (s sparseArray) sum() int {
	sum := 0
	for _, count := range s {
		sum += count
	}
	return sum
}

// orderedKeys returns a slice of the keys (next state IDs) from the sparseArray, sorted in ascending order.
// This ensures deterministic iteration order for weighted random selection during testing.
func (s sparseArray) orderedKeys() []int {
	keys := make([]int, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// array creates a slice of strings of the given count, filled with the specified value.
// Helper function used for creating start token sequences.
func array(value string, count int) []string {
	if count <= 0 {
		return []string{}
	}
	arr := make([]string, count)
	for i := range arr {
		arr[i] = value
	}
	return arr
}

// MakePairs creates a slice of Pair structs from the provided tokens and order.
func MakePairs(tokens []string, order int) []Pair {
	if len(tokens) <= order {
		return []Pair{}
	}
	numPairs := len(tokens) - order
	pairs := make([]Pair, 0, numPairs)

	for i := range numPairs {
		pair := Pair{
			CurrentState: tokens[i : i+order],
			NextState:    tokens[i+order],
		}
		pairs = append(pairs, pair)
	}
	return pairs
}
