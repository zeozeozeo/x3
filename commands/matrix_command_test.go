//go:build matrix || goolm

package commands

import (
	"strings"
	"testing"
)

func TestMatrixParseCommandRequiresBotPrefixOutsideDM(t *testing.T) {
	bot := &MatrixBot{prefix: "!x3"}
	if raw, ok := bot.parseCommand("!x3 persona", false); !ok || raw != "persona" {
		t.Fatalf("expected prefixed room command, got raw=%q ok=%v", raw, ok)
	}
	if raw, ok := bot.parseCommand("!persona", false); ok || raw != "" {
		t.Fatalf("expected short room command to be ignored, got raw=%q ok=%v", raw, ok)
	}
}

func TestMatrixParseCommandAllowsShortPrefixInDM(t *testing.T) {
	bot := &MatrixBot{prefix: "!x3"}
	if raw, ok := bot.parseCommand("!persona set yuki", true); !ok || raw != "persona set yuki" {
		t.Fatalf("expected short DM command, got raw=%q ok=%v", raw, ok)
	}
	if raw, ok := bot.parseCommand("!x3 persona", true); !ok || raw != "persona" {
		t.Fatalf("expected normal DM command to keep working, got raw=%q ok=%v", raw, ok)
	}
}

func TestMatrixHelpUsesShortDMCommands(t *testing.T) {
	bot := &MatrixBot{prefix: "!x3"}
	got := bot.helpText(true)
	for _, want := range []string{"!chat <prompt>", "!persona set <name>", "!chatlog export|import"} {
		if !strings.Contains(got, want) {
			t.Fatalf("DM help missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "!x3 chat <prompt>") {
		t.Fatalf("DM help unexpectedly contains room prefix:\n%s", got)
	}
}
