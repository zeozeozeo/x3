package persona

import (
	"strings"
	"testing"
)

func TestBuildBlockWorldSection(t *testing.T) {
	ctx := PromptContext{
		Memories: []string{"user likes rust"},
		World:    []string{"the tower fell at dawn"},
	}
	block := ctx.BuildBlock()
	if !strings.Contains(block, "**World bible") {
		t.Fatalf("missing world section:\n%s", block)
	}
	if !strings.Contains(block, "the tower fell at dawn") {
		t.Fatalf("missing world fact:\n%s", block)
	}
	if !strings.Contains(block, "**Chat memories:**") {
		t.Fatalf("memories section should be separate:\n%s", block)
	}
	if strings.Count(block, "the tower fell at dawn") != 1 {
		t.Fatalf("world fact duplicated:\n%s", block)
	}
}

func TestWorldPromptAppended(t *testing.T) {
	p := appendWorldPrompt("hello", true)
	if !strings.Contains(p, "<world>") {
		t.Fatalf("world prompt missing:\n%s", p)
	}
	if got := appendWorldPrompt("hello", false); got != "hello" {
		t.Fatalf("disabled should be passthrough: %q", got)
	}
}
