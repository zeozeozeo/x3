package worldmem

import (
	"testing"

	"github.com/zeozeozeo/x3/db"
)

func TestSelectLexicalPicksMatchingFact(t *testing.T) {
	entries := []db.WorldEntry{
		{ID: 1, Text: "the council debated the harvest tithe at length"},
		{ID: 2, Text: "the zarquon device was recovered north of the river"},
		{ID: 3, Text: "rain fell steadily over the harbour for three days"},
	}
	got := selectLexical(entries, "what happened to the zarquon device?")
	if len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("selectLexical() = %#v, want the zarquon fact", got)
	}
}

func TestSelectLexicalReturnsNothingWithoutOverlap(t *testing.T) {
	entries := []db.WorldEntry{{ID: 1, Text: "the north gate closed at dusk"}}
	if got := selectLexical(entries, "pineapple statistics"); len(got) != 0 {
		t.Fatalf("selectLexical() = %#v, want nothing", got)
	}
}

func TestSelectLexicalRespectsTopK(t *testing.T) {
	var entries []db.WorldEntry
	for i := 1; i <= DefaultTopK+4; i++ {
		entries = append(entries, db.WorldEntry{
			ID:     int64(i),
			Text:   "the harvest report number varies",
			Tokens: 5,
		})
	}
	got := selectLexical(entries, "harvest")
	if len(got) != DefaultTopK {
		t.Fatalf("selectLexical() returned %d facts, want %d", len(got), DefaultTopK)
	}
}

func TestSelectLexicalRespectsTokenBudget(t *testing.T) {
	entries := []db.WorldEntry{
		{ID: 1, Text: "harvest", Tokens: DefaultMaxTokens},
		{ID: 2, Text: "harvest", Tokens: DefaultMaxTokens},
	}
	got := selectLexical(entries, "harvest")
	if len(got) != 1 {
		// The first fact is always admitted, but the budget must stop there.
		t.Fatalf("selectLexical() returned %d facts, want 1", len(got))
	}
}

func TestSelectLexicalPrefersNewerOnTie(t *testing.T) {
	entries := []db.WorldEntry{
		{ID: 10, Text: "harbour lantern"},
		{ID: 20, Text: "harbour lantern"},
	}
	got := selectLexical(entries, "harbour lantern")
	if len(got) != 2 || got[0].ID != 20 {
		t.Fatalf("selectLexical() = %#v, want the newer fact first", got)
	}
}

func TestEntryTokensUsesStoredEstimate(t *testing.T) {
	if got := entryTokens(db.WorldEntry{Text: "one two three", Tokens: 99}); got != 99 {
		t.Fatalf("entryTokens() = %d, want the stored estimate 99", got)
	}
	if got := entryTokens(db.WorldEntry{Text: "one two three"}); got != EstimateTokens("one two three") {
		t.Fatalf("entryTokens() = %d, want an estimate", got)
	}
}

func TestRetrieveRejectsSignalFreeQuery(t *testing.T) {
	// A query with no content tokens has no intent to recall anything, and the
	// check happens before any database work, so it needs no store.
	for _, q := range []string{"ok", "the a of", "?!", "2+2"} {
		if got := Retrieve("chan-a", q); got != nil {
			t.Fatalf("Retrieve(%q) = %#v, want nil", q, got)
		}
	}
	if got := Retrieve("", "the north gate"); got != nil {
		t.Fatalf("empty channel key should return nil, got %#v", got)
	}
	if got := Retrieve("chan-a", "   "); got != nil {
		t.Fatalf("blank query should return nil, got %#v", got)
	}
}

func TestEstimateTokensPositive(t *testing.T) {
	if got := EstimateTokens("the north gate closed at dusk"); got <= 0 {
		t.Fatalf("EstimateTokens() = %d, want > 0", got)
	}
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("EstimateTokens(\"\") = %d, want 0", got)
	}
}
