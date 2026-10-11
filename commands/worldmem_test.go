package commands

import (
	"testing"

	"github.com/zeozeozeo/x3/db"
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
