package worldmem

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Lexical (BM25) retrieval is the primary way x3 chooses which world facts to
// put in front of the model.
//
// Why lexical first: world facts are full of rare proper nouns (server names,
// handles, projects, characters), and people refer to those same words when
// they ask about them. BM25 matches that case exactly, costs nothing, needs no
// network call, and is deterministic enough to unit test. Embedding similarity
// is kept only as a fallback for messages that share no vocabulary at all (see
// Retrieve).
//
// A single fixed cosine threshold cannot do this job: the right cutoff depends
// on both the embedding model and how many facts a channel has accumulated, and
// the old 0.55 gate silently discarded most genuine matches.

// lexicalStopWords are high-frequency words that carry no retrieval signal for
// world facts. Dropping them keeps BM25 focused on the tokens that actually
// identify a memory.
var lexicalStopWords = map[string]struct{}{
	"a": {}, "about": {}, "after": {}, "again": {}, "all": {}, "also": {},
	"am": {}, "an": {}, "and": {}, "any": {}, "anyone": {}, "are": {},
	"aren": {}, "as": {}, "at": {}, "back": {}, "be": {}, "because": {},
	"been": {}, "before": {}, "being": {}, "but": {}, "by": {}, "can": {},
	"cant": {}, "could": {}, "did": {}, "didn": {}, "do": {}, "does": {},
	"doesn": {}, "doing": {}, "don": {}, "dont": {}, "for": {}, "from": {},
	"get": {}, "gets": {}, "getting": {}, "go": {}, "going": {}, "gonna": {},
	"got": {}, "had": {}, "has": {}, "have": {}, "he": {}, "her": {}, "here": {},
	"hers": {}, "him": {}, "his": {}, "how": {}, "i": {}, "if": {}, "im": {},
	"in": {}, "into": {}, "is": {}, "isn": {}, "it": {}, "its": {}, "ive": {},
	"just": {}, "know": {}, "let": {}, "like": {}, "me": {}, "more": {},
	"most": {}, "my": {}, "no": {}, "not": {}, "now": {}, "of": {}, "ok": {},
	"okay": {}, "on": {}, "one": {}, "only": {}, "or": {}, "other": {},
	"our": {}, "out": {}, "over": {}, "own": {}, "re": {}, "really": {},
	"s": {}, "said": {}, "same": {}, "say": {}, "see": {}, "she": {}, "should": {},
	"so": {}, "some": {}, "still": {}, "such": {}, "t": {}, "than": {},
	"that": {}, "the": {}, "their": {}, "them": {}, "then": {}, "there": {},
	"these": {}, "they": {}, "thing": {}, "things": {}, "this": {}, "those": {},
	"through": {}, "to": {}, "too": {}, "up": {}, "us": {}, "use": {},
	"used": {}, "very": {}, "was": {}, "we": {}, "well": {}, "were": {},
	"what": {}, "whats": {}, "when": {}, "where": {}, "which": {}, "who": {},
	"whos": {}, "why": {}, "will": {}, "with": {}, "would": {}, "x3": {},
	"yeah": {}, "yes": {}, "you": {}, "your": {}, "youre": {},
}

// Tokenize lowercases s and splits it into content tokens, dropping single
// characters and stop words. It is exported so callers can reuse the same
// tokenization when deciding whether a message carries any retrieval signal.
func Tokenize(s string) []string {
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		w := strings.ToLower(cur.String())
		cur.Reset()
		if len([]rune(w)) < 2 {
			return
		}
		if _, stop := lexicalStopWords[w]; stop {
			return
		}
		out = append(out, w)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// LexicalIndex is an in-memory BM25 index over one channel's facts. Channels
// hold a handful of facts (tens at most), so building it per query is cheap and
// avoids any cross-channel state to invalidate.
type LexicalIndex struct {
	docs  [][]string
	df    map[string]int
	avgdl float64
}

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// NewLexicalIndex builds a BM25 index over the given documents.
func NewLexicalIndex(docs []string) *LexicalIndex {
	ix := &LexicalIndex{df: map[string]int{}}
	for _, d := range docs {
		toks := Tokenize(d)
		ix.docs = append(ix.docs, toks)
		seen := make(map[string]struct{}, len(toks))
		for _, t := range toks {
			if _, ok := seen[t]; ok {
				continue
			}
			seen[t] = struct{}{}
			ix.df[t]++
		}
		ix.avgdl += float64(len(toks))
	}
	if n := len(docs); n > 0 {
		ix.avgdl /= float64(n)
	}
	return ix
}

// Len reports how many documents the index holds.
func (ix *LexicalIndex) Len() int { return len(ix.docs) }

// Scores returns the BM25 score of query against every document, in the same
// order the documents were supplied. Documents sharing no token with the query
// score 0.
func (ix *LexicalIndex) Scores(query string) []float64 {
	scores := make([]float64, len(ix.docs))
	if len(ix.docs) == 0 {
		return scores
	}
	qtoks := Tokenize(query)
	if len(qtoks) == 0 {
		return scores
	}
	total := float64(len(ix.docs))
	avgdl := ix.avgdl
	if avgdl <= 0 {
		avgdl = 1
	}
	for i, doc := range ix.docs {
		if len(doc) == 0 {
			continue
		}
		var score float64
		for _, q := range qtoks {
			var tf float64
			for _, t := range doc {
				if t == q {
					tf++
				}
			}
			if tf == 0 {
				continue
			}
			df := float64(ix.df[q])
			idf := math.Log(1 + (total-df+0.5)/(df+0.5))
			score += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*(1-bm25B+bm25B*float64(len(doc))/avgdl))
		}
		scores[i] = score
	}
	return scores
}

// Rank returns document indices ordered best first. Documents that share no
// token with the query are omitted. Ties fall back to ascending index, which
// callers pass in oldest-first order, so equal-scoring facts stay stable.
func (ix *LexicalIndex) Rank(query string) []int {
	scores := ix.Scores(query)
	idx := make([]int, 0, len(scores))
	for i, s := range scores {
		if s > 0 {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		sa, sb := scores[idx[a]], scores[idx[b]]
		if sa != sb {
			return sa > sb
		}
		return idx[a] < idx[b]
	})
	return idx
}
