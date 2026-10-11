package worldmem

import (
	"math"
	"strings"
	"testing"
)

func TestCosine(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	if got := Cosine(a, b); math.Abs(got) > 1e-6 {
		t.Fatalf("orthogonal cosine = %v, want 0", got)
	}
	if got := Cosine(a, a); math.Abs(got-1) > 1e-6 {
		t.Fatalf("identical cosine = %v, want 1", got)
	}
	if got := Cosine(a, []float32{-1, 0, 0}); math.Abs(got+1) > 1e-6 {
		t.Fatalf("opposite cosine = %v, want -1", got)
	}
	if got := Cosine(a, []float32{0, 0, 0}); got != 0 {
		t.Fatalf("zero vector cosine = %v, want 0", got)
	}
}

func TestNormalize(t *testing.T) {
	v := Normalize([]float32{3, 4})
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(sum)-1) > 1e-6 {
		t.Fatalf("normalized length = %v, want 1", math.Sqrt(sum))
	}
}

func TestNormalizeWorld(t *testing.T) {
	if got := NormalizeWorld("  hello   world \n"); got != "hello world" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizeWorld("   "); got != "" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a", MaxWorldRunes+100)
	if got := NormalizeWorld(long); len([]rune(got)) != MaxWorldRunes {
		t.Fatalf("long fact not capped: %d runes", len([]rune(got)))
	}
}

func TestChunkWorldShortPassthrough(t *testing.T) {
	text := "The tower fell at dawn. Kael escaped through the crypt."
	chunks := ChunkWorld(text)
	if len(chunks) != 1 || chunks[0] != text {
		t.Fatalf("short fact should pass through as one chunk: %#v", chunks)
	}
}

func TestChunkWorldLongSplits(t *testing.T) {
	var sb strings.Builder
	for range 120 {
		sb.WriteString("The ancient tower of Veridia stood tall against the storm. ")
	}
	chunks := ChunkWorld(sb.String())
	if len(chunks) < 2 {
		t.Fatalf("long fact should split, got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if strings.TrimSpace(c) == "" {
			t.Fatalf("empty chunk produced")
		}
		if len(c) > ChunkTargetTokens*4+500 {
			t.Fatalf("chunk too large: %d bytes", len(c))
		}
	}
}

func TestRank(t *testing.T) {
	q := []float32{1, 0}
	cands := [][]float32{{0, 1}, {1, 0}, {0.7, 0.7}}
	order := Rank(q, cands)
	if len(order) != 3 || order[0] != 1 {
		t.Fatalf("best match should be index 1: %v", order)
	}
}

func TestFormatBible(t *testing.T) {
	if got := FormatBible(nil); got != "" {
		t.Fatalf("empty entries should give empty suffix: %q", got)
	}
	got := FormatBible([]string{"the tower fell", " ", "kael fled"})
	if !strings.HasPrefix(got, "\n\n**World bible") {
		t.Fatalf("suffix should start with header: %q", got)
	}
	if !strings.Contains(got, "- the tower fell") || !strings.Contains(got, "- kael fled") {
		t.Fatalf("facts missing: %q", got)
	}
}
