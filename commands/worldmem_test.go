package commands

import "testing"

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
