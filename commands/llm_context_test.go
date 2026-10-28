package commands

import (
	"testing"

	"github.com/zeozeozeo/x3/llm"
)

func testContextMessages() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{Role: llm.RoleUser, Content: "one", MessageID: "m1"},
		{Role: llm.RoleAssistant, Content: "two", MessageID: "m2"},
		{Role: llm.RoleUser, Content: "three", MessageID: "m3"},
	}
}

func TestLastNonSystemMessageID(t *testing.T) {
	messages := testContextMessages()
	if got := lastNonSystemMessageID(messages); got != "m3" {
		t.Fatalf("expected newest non-system ID m3, got %q", got)
	}

	onlySystem := []llm.Message{{Role: llm.RoleSystem, Content: "system"}}
	if got := lastNonSystemMessageID(onlySystem); got != "" {
		t.Fatalf("system-only history should have no ID, got %q", got)
	}
	if got := lastNonSystemMessageID(nil); got != "" {
		t.Fatalf("empty history should have no ID, got %q", got)
	}
}

func TestMessagesAfterMessageID(t *testing.T) {
	messages := testContextMessages()

	newer, found := messagesAfterMessageID(messages, "m1")
	if !found {
		t.Fatalf("expected m1 to be found")
	}
	if len(newer) != 2 || newer[0].MessageID != "m2" || newer[1].MessageID != "m3" {
		t.Fatalf("expected only messages after m1, got %#v", newer)
	}

	newer, found = messagesAfterMessageID(messages, "m3")
	if !found || len(newer) != 0 {
		t.Fatalf("newest message should have no newer messages, got %#v (found=%v)", newer, found)
	}

	if _, found := messagesAfterMessageID(messages, "missing"); found {
		t.Fatalf("unknown message ID should not be found")
	}
	if _, found := messagesAfterMessageID(messages, ""); found {
		t.Fatalf("empty message ID should not be found")
	}

	// The returned slice must not alias the original history.
	newer, _ = messagesAfterMessageID(messages, "m1")
	newer[0].Content = "changed"
	if messages[2].Content != "two" {
		t.Fatalf("messagesAfterMessageID returned an aliased slice")
	}
}
