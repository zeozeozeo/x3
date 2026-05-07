package minilm

import "testing"

func TestTokenizerEncodeAddsSpecialTokens(t *testing.T) {
	tokenizer := NewTokenizer()
	ids, attention, tokenTypes := tokenizer.Encode("hello world")
	if len(ids) < 4 {
		t.Fatalf("expected special tokens plus words, got %v", ids)
	}
	if ids[0] != int64(tokenizer.tokenID(classToken)) {
		t.Fatalf("expected first token to be CLS, got %d", ids[0])
	}
	if ids[len(ids)-1] != int64(tokenizer.tokenID(sepToken)) {
		t.Fatalf("expected last token to be SEP, got %d", ids[len(ids)-1])
	}
	if len(attention) != len(ids) || len(tokenTypes) != len(ids) {
		t.Fatalf("mask lengths do not match ids")
	}
	for i, mask := range attention {
		if mask != 1 {
			t.Fatalf("attention mask %d = %d, want 1", i, mask)
		}
	}
}
