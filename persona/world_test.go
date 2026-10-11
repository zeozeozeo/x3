package persona

import (
	"strings"
	"testing"
)

func TestBuildBlockHasNoWorldSection(t *testing.T) {
	// Retrieved world facts must not render into the system prompt: they ride
	// on the last message instead so prompt prefix caching stays effective.
	ctx := PromptContext{
		Memories: []string{"user likes rust"},
		Context:  []string{"speak like a pirate"},
	}
	block := ctx.BuildBlock()
	if strings.Contains(block, "World bible") {
		t.Fatalf("world section must not be in the system prompt:\n%s", block)
	}
	if !strings.Contains(block, "**Chat memories:**") {
		t.Fatalf("memories section missing:\n%s", block)
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
