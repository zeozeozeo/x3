package commands

import (
	"strings"
	"testing"

	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
)

func TestExtractWorldTags(t *testing.T) {
	display, worlds := extractWorldTags("hello <world>the tower fell at dawn</world>\nworld")
	if display != "hello \nworld" {
		t.Fatalf("display = %q", display)
	}
	if len(worlds) != 1 || worlds[0] != "the tower fell at dawn" {
		t.Fatalf("worlds = %#v", worlds)
	}
}

func TestExtractWorldTagsStripsEmptyTag(t *testing.T) {
	display, worlds := extractWorldTags("hello <world> </world> world")
	if display != "hello  world" {
		t.Fatalf("display = %q", display)
	}
	if len(worlds) != 0 {
		t.Fatalf("worlds = %#v", worlds)
	}
}

func TestExtractWorldTagsMultiple(t *testing.T) {
	display, worlds := extractWorldTags("<world>first fact</world> visible <world>second fact</world>")
	if display != "visible" {
		t.Fatalf("display = %q", display)
	}
	if len(worlds) != 2 || worlds[0] != "first fact" || worlds[1] != "second fact" {
		t.Fatalf("worlds = %#v", worlds)
	}
}

func TestExtractWorldTagsMultiline(t *testing.T) {
	display, worlds := extractWorldTags("story\n<world>line one\nline two with detail</world>\nmore")
	if display != "story\n\nmore" {
		t.Fatalf("display = %q", display)
	}
	if len(worlds) != 1 || worlds[0] != "line one\nline two with detail" {
		t.Fatalf("worlds = %#v", worlds)
	}
}

func TestExtractWorldTagsNone(t *testing.T) {
	display, worlds := extractWorldTags("plain response")
	if display != "plain response" {
		t.Fatalf("display = %q", display)
	}
	if len(worlds) != 0 {
		t.Fatalf("worlds = %#v", worlds)
	}
}

func TestIsWorldmemExportFilename(t *testing.T) {
	if !isWorldmemExportFilename("x3-worldmem-2026-10-12.json") {
		t.Fatalf("should match dated export filename")
	}
	if isWorldmemExportFilename("x3-chatlog-2026-10-12.json") {
		t.Fatalf("should not match chatlog filename")
	}
	if isWorldmemExportFilename("random.json") {
		t.Fatalf("should not match random filename")
	}
}

func TestHandleWorldTagsAppendsFooter(t *testing.T) {
	var cached string
	display := handleWorldTags("chan-x", "story <world>the tower fell at dawn</world>", func(s string) {
		cached = s
	})
	if display != "story\n"+worldMemUpdatedFooter {
		t.Fatalf("display = %q", display)
	}
	if cached != "story" {
		t.Fatalf("cached content must not include the footer: %q", cached)
	}
}

func TestHandleWorldTagsNoFooterWithoutFacts(t *testing.T) {
	display := handleWorldTags("chan-x", "plain story", nil)
	if display != "plain story" {
		t.Fatalf("display = %q", display)
	}
}

func TestStripWorldMemFooter(t *testing.T) {
	in := "story line\n" + worldMemUpdatedFooter
	if got := stripWorldMemFooter(in); got != "story line" {
		t.Fatalf("got %q", got)
	}
	if got := stripWorldMemFooter("plain story"); got != "plain story" {
		t.Fatalf("got %q", got)
	}
}

func TestWorldmemBrowserMessagePaging(t *testing.T) {
	entries := make([]db.WorldEntry, 12)
	for i := range entries {
		entries[i].Text = "fact"
	}
	first := worldmemBrowserMessage(entries, 0, worldBrowserPageSize)
	if first.Embeds == nil || len(*first.Embeds) != 1 {
		t.Fatalf("expected one embed")
	}
	last := worldmemBrowserMessage(entries, 99, worldBrowserPageSize)
	if last.Embeds == nil || len(*last.Embeds) != 1 {
		t.Fatalf("expected one embed on clamped page")
	}
}

func TestComposeWorldQueryKeepsClearMessage(t *testing.T) {
	current := "north gate closed"
	got := composeWorldQuery(current, []string{"unrelated recent line"})
	if got != current {
		t.Fatalf("composeWorldQuery() = %q, want the message unchanged", got)
	}
}

func TestComposeWorldQueryBorrowsNewestFirst(t *testing.T) {
	got := composeWorldQuery("gate?", []string{"one", "two", "three", "four"})
	want := "gate?\nfour\nthree"
	if got != want {
		t.Fatalf("composeWorldQuery() = %q, want %q", got, want)
	}
}

func TestComposeWorldQueryBorrowsForAmbiguousMessage(t *testing.T) {
	got := composeWorldQuery("the gate?", []string{"older line", "the north gate closed at dusk"})
	want := "the gate?\nthe north gate closed at dusk\nolder line"
	if got != want {
		t.Fatalf("composeWorldQuery() = %q, want %q", got, want)
	}
}

func TestComposeWorldQueryIgnoresSignalFreeChatter(t *testing.T) {
	// Pure chatter carries no intent to recall anything, so it must not drag
	// the conversation (and with it, unrelated canon) into the query.
	for _, current := range []string{"ok", "the a of", "!!!", "2+2"} {
		got := composeWorldQuery(current, []string{"the north gate closed at dusk"})
		if got != current {
			t.Fatalf("composeWorldQuery(%q) = %q, want it unchanged", current, got)
		}
	}
}

func TestComposeWorldQueryTruncatesBorrowedContext(t *testing.T) {
	long := strings.Repeat("x", worldQueryContextRunes+120)
	got := composeWorldQuery("gate?", []string{long})
	parts := strings.Split(got, "\n")
	if len(parts) != 2 {
		t.Fatalf("composeWorldQuery() = %q, want two lines", got)
	}
	if n := len([]rune(parts[1])); n != worldQueryContextRunes {
		t.Fatalf("borrowed context = %d runes, want %d", n, worldQueryContextRunes)
	}
}

func TestRecentWorldQueryContext(t *testing.T) {
	l := &llm.Llmer{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "system prompt"},
		{Role: llm.RoleUser, Content: "first"},
		{Role: llm.RoleAssistant, Content: "   "},
		{Role: llm.RoleAssistant, Content: "second"},
		{Role: llm.RoleUser, Content: "third"},
	}}
	got := recentWorldQueryContext(l, 2)
	if len(got) != 2 || got[0] != "second" || got[1] != "third" {
		t.Fatalf("recentWorldQueryContext() = %#v, want [second third] in order", got)
	}

	if got := recentWorldQueryContext(nil, 2); got != nil {
		t.Fatalf("nil llmer should give nil, got %#v", got)
	}
	if got := recentWorldQueryContext(l, 0); got != nil {
		t.Fatalf("n=0 should give nil, got %#v", got)
	}
}
