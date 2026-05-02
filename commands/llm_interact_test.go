package commands

import (
	"reflect"
	"testing"
)

func TestSplitLlmTagsBlankLines(t *testing.T) {
	got := splitLlmTags("hey\n\nwhat's up", nil)
	want := []string{"hey", "what's up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSplitLlmTagsSingleNewlineDoesNotSplit(t *testing.T) {
	got := splitLlmTags("hey\nwhat's up", nil)
	want := []string{"hey\nwhat's up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSplitLlmTagsPreservesCodeBlock(t *testing.T) {
	input := "```go\nfunc main() {\n}\n\nfmt.Println(\"x3\")\n```\n\nafter"
	got := splitLlmTags(input, nil)
	want := []string{"```go\nfunc main() {\n}\n\nfmt.Println(\"x3\")\n```", "after"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSplitLlmTagsPreservesMarkdownList(t *testing.T) {
	input := "- first\n\n- second\n  continuation\n\nplain"
	got := splitLlmTags(input, nil)
	want := []string{"- first\n\n- second\n  continuation", "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSplitLlmTagsLegacyTag(t *testing.T) {
	got := splitLlmTags("hey <new_message> what's up", nil)
	want := []string{"hey", "what's up"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
