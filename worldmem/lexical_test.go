package worldmem

import "testing"

func TestTokenize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"drops stop words", "the quick brown fox jumps over the lazy dog", []string{"quick", "brown", "fox", "jumps", "lazy", "dog"}},
		{"splits on punctuation", "north-gate: closed... at dusk?", []string{"north", "gate", "closed", "dusk"}},
		{"lowercases", "Zarquon DEVICE", []string{"zarquon", "device"}},
		{"drops single characters", "a b c d", nil},
		{"empty input", "   ", nil},
		{"keeps digits", "release 42 shipped", []string{"release", "42", "shipped"}},
		{"keeps non-ascii letters", "канал reopened", []string{"канал", "reopened"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Tokenize(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("Tokenize(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Tokenize(%q) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

func TestLexicalIndexPrefersRareToken(t *testing.T) {
	docs := []string{
		// Many generic words, nothing distinctive.
		"the council met again and the council spoke at length about the road and the gate",
		// One rare token the query also uses.
		"the zarquon device was recovered north of the river",
	}
	ix := NewLexicalIndex(docs)
	if ix.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", ix.Len())
	}
	rank := ix.Rank("where is the zarquon device")
	if len(rank) == 0 || rank[0] != 1 {
		t.Fatalf("rare-token document should rank first, got %v", rank)
	}
}

func TestLexicalIndexNoOverlapScoresZero(t *testing.T) {
	docs := []string{"the north gate closed at dusk"}
	ix := NewLexicalIndex(docs)

	scores := ix.Scores("pineapple statistics")
	if len(scores) != 1 || scores[0] != 0 {
		t.Fatalf("Scores() = %v, want [0]", scores)
	}
	if got := ix.Rank("pineapple statistics"); len(got) != 0 {
		t.Fatalf("Rank() = %v, want no matches", got)
	}
}

func TestLexicalIndexEmptyInputs(t *testing.T) {
	ix := NewLexicalIndex(nil)
	if ix.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", ix.Len())
	}
	if got := ix.Scores("anything"); len(got) != 0 {
		t.Fatalf("Scores() = %v, want empty", got)
	}
	if got := ix.Rank("anything"); len(got) != 0 {
		t.Fatalf("Rank() = %v, want empty", got)
	}

	ix = NewLexicalIndex([]string{"the north gate closed at dusk"})
	if got := ix.Rank(""); len(got) != 0 {
		t.Fatalf("empty query should match nothing, got %v", got)
	}
}

func TestLexicalIndexTiesKeepDocumentOrder(t *testing.T) {
	ix := NewLexicalIndex([]string{"harbour lantern", "harbour lantern"})
	rank := ix.Rank("harbour lantern")
	if len(rank) != 2 || rank[0] != 0 || rank[1] != 1 {
		t.Fatalf("equal scores should keep document order, got %v", rank)
	}
}

func TestLexicalIndexRepeatedQueryTermScoresHigher(t *testing.T) {
	docs := []string{"the lantern", "lantern lantern lantern"}
	ix := NewLexicalIndex(docs)
	rank := ix.Rank("lantern")
	if len(rank) != 2 || rank[0] != 1 {
		t.Fatalf("term frequency should dominate, got %v", rank)
	}
}
