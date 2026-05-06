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

func TestImageURLsFromContentDiscordCDNWithQuery(t *testing.T) {
	input := "look https://cdn.discordapp.com/attachments/1311367819128213554/1499764784336474222/20260412_103431.jpg?ex=1&is=2&hm=abc"
	got := imageURLsFromContent(input)
	want := []string{"https://cdn.discordapp.com/attachments/1311367819128213554/1499764784336474222/20260412_103431.jpg?ex=1&is=2&hm=abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestImageURLsFromContentTrimsPunctuationAndIgnoresNonImages(t *testing.T) {
	input := "a https://example.com/cat.png), b https://example.com/page.txt c https://example.com/dog.webp."
	got := imageURLsFromContent(input)
	want := []string{"https://example.com/cat.png", "https://example.com/dog.webp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
