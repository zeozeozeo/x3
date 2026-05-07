package minilm

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	unknownToken = "[UNK]"
	classToken   = "[CLS]"
	sepToken     = "[SEP]"
	padToken     = "[PAD]"
	maxPieces    = 256
)

type Tokenizer struct {
	vocab map[string]int
}

type vocabEntry struct {
	Token string
	ID    int
}

func NewTokenizer() Tokenizer {
	return Tokenizer{vocab: Vocab()}
}

func (t Tokenizer) Encode(text string) (ids, attentionMask, tokenTypeIDs []int64) {
	tokens := t.wordPiece(basicTokens(text))
	limit := maxPieces - 2
	if len(tokens) > limit {
		tokens = tokens[:limit]
	}

	ids = make([]int64, 0, len(tokens)+2)
	ids = append(ids, int64(t.tokenID(classToken)))
	for _, token := range tokens {
		ids = append(ids, int64(t.tokenID(token)))
	}
	ids = append(ids, int64(t.tokenID(sepToken)))

	attentionMask = make([]int64, len(ids))
	tokenTypeIDs = make([]int64, len(ids))
	for i := range attentionMask {
		attentionMask[i] = 1
	}
	return ids, attentionMask, tokenTypeIDs
}

func (t Tokenizer) tokenID(token string) int {
	if id, ok := t.vocab[token]; ok {
		return id
	}
	return t.vocab[unknownToken]
}

func (t Tokenizer) wordPiece(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if _, ok := t.vocab[token]; ok {
			out = append(out, token)
			continue
		}
		pieces, ok := t.splitWordPiece(token)
		if !ok {
			out = append(out, unknownToken)
			continue
		}
		out = append(out, pieces...)
	}
	return out
}

func (t Tokenizer) splitWordPiece(token string) ([]string, bool) {
	runes := []rune(token)
	if len(runes) == 0 {
		return nil, false
	}
	var pieces []string
	for start := 0; start < len(runes); {
		found := ""
		foundEnd := start
		for end := len(runes); end > start; end-- {
			candidate := string(runes[start:end])
			if start > 0 {
				candidate = "##" + candidate
			}
			if _, ok := t.vocab[candidate]; ok {
				found = candidate
				foundEnd = end
				break
			}
		}
		if found == "" {
			return nil, false
		}
		pieces = append(pieces, found)
		start = foundEnd
	}
	return pieces, true
}

func basicTokens(text string) []string {
	text = strings.ToLower(stripAccents(text))
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			current.WriteRune(r)
		case unicode.IsSpace(r) || unicode.IsControl(r):
			flush()
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			flush()
			tokens = append(tokens, string(r))
		default:
			flush()
		}
	}
	flush()
	return tokens
}

func stripAccents(s string) string {
	var out strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
