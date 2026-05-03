package commands

import "testing"

func TestExtractMemoryTags(t *testing.T) {
	display, memories := extractMemoryTags("hello <memory>user likes rust</memory>\nworld")
	if display != "hello \nworld" {
		t.Fatalf("display = %q", display)
	}
	if len(memories) != 1 || memories[0] != "user likes rust" {
		t.Fatalf("memories = %#v", memories)
	}
}

func TestExtractMemoryTagsStripsEmptyTag(t *testing.T) {
	display, memories := extractMemoryTags("hello <memory> </memory> world")
	if display != "hello  world" {
		t.Fatalf("display = %q", display)
	}
	if len(memories) != 0 {
		t.Fatalf("memories = %#v", memories)
	}
}
