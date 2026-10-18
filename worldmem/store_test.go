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

func TestFallbackKeepEmpty(t *testing.T) {
	keep, reason := fallbackKeep(nil)
	if keep != nil {
		t.Fatalf("fallbackKeep(nil) kept %v", keep)
	}
	if reason != "no-vector-hits" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestFallbackKeepRejectsBelowFloor(t *testing.T) {
	keep, reason := fallbackKeep([]float64{FallbackMinSimilarity - 0.05, 0.1})
	if keep != nil {
		t.Fatalf("kept %v, want nothing", keep)
	}
	if reason != "below-floor" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestFallbackKeepRejectsWithoutLeadOverMedian(t *testing.T) {
	// The best hit clears the floor but does not stand out from the channel:
	// this is the "unrelated chatter in a channel full of facts" case that an
	// absolute gate could not filter out.
	sims := []float64{0.60, 0.55, 0.52, 0.50}
	if got := sims[0] - sims[len(sims)/2]; got >= FallbackMinLead {
		t.Fatalf("test fixture has lead %.3f, expected below %v", got, FallbackMinLead)
	}
	keep, reason := fallbackKeep(sims)
	if keep != nil {
		t.Fatalf("kept %v, want nothing", keep)
	}
	if reason != "no-lead-over-median" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestFallbackKeepSingleFactAboveFloor(t *testing.T) {
	// One stored fact: there is no median to compare against, so the floor is
	// the only gate.
	keep, reason := fallbackKeep([]float64{0.80})
	if len(keep) != 1 || keep[0] != 0 {
		t.Fatalf("fallbackKeep() = %v, want [0]", keep)
	}
	if reason != "ok" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestFallbackKeepStrongLead(t *testing.T) {
	// best 0.70, median 0.40 -> lead 0.30 clears the bar; the 0.30 hit is
	// outside the keep margin.
	keep, reason := fallbackKeep([]float64{0.70, 0.62, 0.40, 0.30})
	if len(keep) != 2 || keep[0] != 0 || keep[1] != 1 {
		t.Fatalf("fallbackKeep() = %v, want [0 1]", keep)
	}
	if reason != "ok" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestFallbackKeepCapsAtTopK(t *testing.T) {
	// A tight cluster of strong hits above a channel of unrelated facts.
	sims := []float64{0.98, 0.97, 0.96, 0.95, 0.94}
	for len(sims) < 21 {
		sims = append(sims, 0.50)
	}
	median := sims[len(sims)/2]
	if sims[0]-median < FallbackMinLead {
		t.Fatalf("test fixture lead %.3f below %v", sims[0]-median, FallbackMinLead)
	}
	keep, _ := fallbackKeep(sims)
	if len(keep) != FallbackTopK {
		t.Fatalf("fallbackKeep() kept %d, want the cap %d", len(keep), FallbackTopK)
	}
	for i := range keep {
		if keep[i] != i {
			t.Fatalf("fallbackKeep() = %v, want the top %d in order", keep, FallbackTopK)
		}
	}
}

func TestRetrieveRejectsSignalFreeQuery(t *testing.T) {
	// A query with no content tokens cannot match, and must not fall through to
	// the embedding path either: pure chatter should never pull canon in. This
	// is checked before any database work, so it needs no store.
	for _, q := range []string{"ok", "the a of", "?!", "2+2"} {
		if got := Retrieve(t.Context(), "chan-a", q); got != nil {
			t.Fatalf("Retrieve(%q) = %#v, want nil", q, got)
		}
	}
	if got := Retrieve(t.Context(), "", "the north gate"); got != nil {
		t.Fatalf("empty channel key should return nil, got %#v", got)
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
