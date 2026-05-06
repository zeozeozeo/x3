//go:build matrix || goolm

package commands

import (
	"strings"
	"testing"

	"github.com/zeozeozeo/x3/persona"
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

func TestParseMatrixCommandArgsKeepsPositions(t *testing.T) {
	parsed := parseMatrixCommandArgs(`model=glm5`)
	if len(parsed.Args) != 1 {
		t.Fatalf("args = %#v, want one arg", parsed.Args)
	}
	if got := parsed.Args[0]; got.Text != "model=glm5" || got.Start != 0 || got.End != len("model=glm5") {
		t.Fatalf("arg = %#v", got)
	}
}

func TestMatrixCommandDiagnosticPointsAtToken(t *testing.T) {
	parsed := parseMatrixCommandArgs(`model=glm5`)
	got := matrixCommandDiagnostic(parsed.Raw, parsed.Args[0], "bad syntax", "use spaces")
	for _, want := range []string{"error: bad syntax", "help: use spaces", "model=glm5", "^^^^^^^^^^ bad syntax"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic missing %q:\n%s", want, got)
		}
	}
}

func TestMatrixCommandDiagnosticSupportsFullCommandOffset(t *testing.T) {
	bot := &MatrixBot{prefix: "!x3"}
	rest := "model=glm5"
	ctx := bot.matrixCommandContext("persona", rest, true)
	parsed := parseMatrixCommandArgs(rest)
	got := matrixCommandDiagnostic(ctx.Raw(rest), ctx.Token(parsed.Args[0]), "invalid persona command syntax", "use spaces")
	for _, want := range []string{"!persona model=glm5", "         ^^^^^^^^^^ invalid persona command syntax"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic missing %q:\n%s", want, got)
		}
	}
}

func TestMatrixCommandDiagnosticMultipleSpans(t *testing.T) {
	got := matrixCommandDiagnosticMulti("context edit nope", []matrixDiagnosticSpan{
		{Token: matrixCommandArg{Text: "edit", Start: 8, End: 12}, Issue: "while parsing edit action", Primary: false},
		{Token: matrixCommandArg{Text: "nope", Start: 13, End: 17}, Issue: "invalid integer", Primary: true},
	}, "expected a numeric index")
	for _, want := range []string{"note: while parsing edit action", "error: invalid integer", "        ---- while parsing edit action", "             ^^^^ invalid integer"} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagnostic missing %q:\n%s", want, got)
		}
	}
}

func TestMatrixPersonaHelpText(t *testing.T) {
	bot := &MatrixBot{prefix: "!x3"}
	got := bot.personaHelpText(true)
	for _, want := range []string{"Persona command:", "!persona model <model>", "Use spaces, not key=value"} {
		if !strings.Contains(got, want) {
			t.Fatalf("persona help missing %q:\n%s", want, got)
		}
	}
}

func TestParseMatrixCommandArgsQuotedValue(t *testing.T) {
	parsed := parseMatrixCommandArgs(`edit 2 "new value"`)
	if len(parsed.Args) != 3 {
		t.Fatalf("args = %#v, want three args", parsed.Args)
	}
	if parsed.Args[2].Text != "new value" {
		t.Fatalf("quoted arg text = %q", parsed.Args[2].Text)
	}
	if got := parsed.RestAfter(1); got != `"new value"` {
		t.Fatalf("RestAfter(1) = %q", got)
	}
}

func TestParseMatrixBoolStrict(t *testing.T) {
	if got, ok := parseMatrixBool("on"); !ok || !got {
		t.Fatalf("on parsed as got=%v ok=%v", got, ok)
	}
	if got, ok := parseMatrixBool("off"); !ok || got {
		t.Fatalf("off parsed as got=%v ok=%v", got, ok)
	}
	if _, ok := parseMatrixBool("maybe"); ok {
		t.Fatalf("maybe unexpectedly parsed")
	}
}

func TestFindMatrixPersonaAcceptsNumberAndAlias(t *testing.T) {
	if got, ok := findMatrixPersona("1"); !ok || got.Name != persona.PersonaProto.Name {
		t.Fatalf("persona 1 = %#v ok=%v", got, ok)
	}
	if got, ok := findMatrixPersona("Protogen"); !ok || got.Name != persona.PersonaProto.Name {
		t.Fatalf("persona Protogen = %#v ok=%v", got, ok)
	}
	if got, ok := findMatrixPersona("Protogen (Default)"); !ok || got.Name != persona.PersonaProto.Name {
		t.Fatalf("persona Protogen (Default) = %#v ok=%v", got, ok)
	}
}
