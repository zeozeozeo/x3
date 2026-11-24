package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

// textCommandArg is one whitespace-separated token of a prefixed text command,
// with its byte span in the raw command line so diagnostics can point at it.
type textCommandArg struct {
	Text  string
	Start int
	End   int
}

// textCommandArgs is a parsed text command line: the raw input plus its tokens.
type textCommandArgs struct {
	Raw  string
	Args []textCommandArg
}

// textDiagnosticSpan marks one token of a command line as the source of a
// diagnostic; Primary marks the error, the rest are notes.
type textDiagnosticSpan struct {
	Token   textCommandArg
	Issue   string
	Primary bool
}

// textCommandContext maps token spans from a bare command tail back onto the
// full command line the user typed, so diagnostics carry the real prefix.
type textCommandContext struct {
	Display string
	Offset  int
}

func (c textCommandContext) Raw(local string) string {
	if c.Display != "" {
		return c.Display
	}
	return local
}

func (c textCommandContext) Token(token textCommandArg) textCommandArg {
	token.Start += c.Offset
	token.End += c.Offset
	return token
}

// textCommandContextFor builds the context for a command invoked as
// "<usage> <rest>".
func textCommandContextFor(usage, rest string) textCommandContext {
	display := usage
	if strings.TrimSpace(rest) != "" {
		display += " " + rest
	}
	return textCommandContext{
		Display: display,
		Offset:  len(usage) + 1,
	}
}

func parseTextCommandArgs(raw string) textCommandArgs {
	var args []textCommandArg
	for i := 0; i < len(raw); {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
			i++
		}
		if i >= len(raw) {
			break
		}
		start := i
		var sb strings.Builder
		quote := byte(0)
		for i < len(raw) {
			ch := raw[i]
			if quote == 0 && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r') {
				break
			}
			if ch == '"' || ch == '\'' {
				if quote == 0 {
					quote = ch
					i++
					continue
				}
				if quote == ch {
					quote = 0
					i++
					continue
				}
			}
			if ch == '\\' && i+1 < len(raw) {
				i++
				sb.WriteByte(raw[i])
				i++
				continue
			}
			sb.WriteByte(ch)
			i++
		}
		args = append(args, textCommandArg{Text: sb.String(), Start: start, End: i})
	}
	return textCommandArgs{Raw: raw, Args: args}
}

func (a textCommandArgs) Empty() bool {
	return len(a.Args) == 0
}

func (a textCommandArgs) At(i int) textCommandArg {
	if i < 0 || i >= len(a.Args) {
		return textCommandArg{}
	}
	return a.Args[i]
}

func (a textCommandArgs) RestAfter(i int) string {
	if i < 0 || i >= len(a.Args) {
		return ""
	}
	return strings.TrimSpace(a.Raw[a.Args[i].End:])
}

func textCommandDiagnostic(raw string, token textCommandArg, issue, hint string) string {
	return textCommandDiagnosticMulti(raw, []textDiagnosticSpan{{Token: token, Issue: issue, Primary: true}}, hint)
}

func textCommandDiagnosticMulti(raw string, spans []textDiagnosticSpan, hint string) string {
	if raw == "" {
		if len(spans) > 0 {
			raw = spans[0].Token.Text
			spans[0].Token.Start = 0
			spans[0].Token.End = len(raw)
		}
	}
	if len(spans) == 0 {
		spans = []textDiagnosticSpan{{Token: textCommandEndToken(raw), Issue: "Invalid command.", Primary: true}}
	}
	var b strings.Builder
	b.WriteString("\n```text\n")
	for _, span := range spans {
		if span.Primary {
			b.WriteString("error: ")
		} else {
			b.WriteString("note: ")
		}
		b.WriteString(span.Issue)
		b.WriteString("\n")
	}
	if hint != "" {
		b.WriteString("help: ")
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(raw)
	b.WriteString("\n")
	for _, span := range spans {
		token := span.Token
		start := max(token.Start, 0)
		end := token.End
		if end <= start {
			end = start + max(len(token.Text), 1)
		}
		if start > len(raw) {
			start = len(raw)
		}
		if end > len(raw) {
			end = len(raw)
		}
		width := max(end-start, 1)
		marker := "^"
		if !span.Primary {
			marker = "-"
		}
		b.WriteString(strings.Repeat(" ", start))
		b.WriteString(strings.Repeat(marker, width))
		if span.Issue != "" {
			b.WriteString(" ")
			b.WriteString(span.Issue)
		}
		b.WriteString("\n")
	}
	b.WriteString("```")
	return b.String()
}

func textCommandEndToken(raw string) textCommandArg {
	pos := len(raw)
	return textCommandArg{Text: "<end>", Start: pos, End: pos + 1}
}

func textCommandAction(args textCommandArgs, commandName string) (textCommandArg, string, *string) {
	if args.Empty() {
		msg := textCommandDiagnostic(args.Raw, textCommandEndToken(args.Raw), "Missing action for `"+commandName+"` command.", "Run `"+commandName+" help` for available actions.")
		return textCommandArg{}, "", &msg
	}
	token := args.At(0)
	if strings.Contains(token.Text, "=") {
		msg := textCommandDiagnostic(args.Raw, token, "Invalid `"+commandName+"` command syntax.", "Use spaces between tokens. x3 commands do not use key=value syntax.")
		return textCommandArg{}, "", &msg
	}
	action := strings.ToLower(strings.TrimSpace(token.Text))
	return token, action, nil
}

func textMissingValueDiagnostic(raw string, actionToken textCommandArg, commandName, action, expected string) string {
	return textCommandDiagnostic(raw, actionToken, "Missing value for `"+commandName+" "+action+"`.", "Expected: `"+expected+"`")
}

func textUnexpectedTokenDiagnostic(raw string, token textCommandArg, commandName string) string {
	return textCommandDiagnostic(raw, token, "Unexpected token for `"+commandName+"` command.", "This command does not accept `"+token.Text+"` here. Run `"+commandName+" help` for valid syntax.")
}

func textInvalidIntDiagnostic(raw string, token textCommandArg, name string) string {
	return textCommandDiagnostic(raw, token, "Invalid integer for `"+name+"`.", "`"+token.Text+"` is not a base-10 integer.")
}

func textInvalidFloatDiagnostic(raw string, token textCommandArg, name string) string {
	return textCommandDiagnostic(raw, token, "Invalid number for `"+name+"`.", "`"+token.Text+"` must be a decimal number.")
}

func parseTextBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enable", "enabled":
		return true, true
	case "0", "false", "no", "n", "off", "disable", "disabled":
		return false, true
	default:
		return false, false
	}
}

func textInvalidBoolDiagnostic(raw string, token textCommandArg, name string) string {
	return textCommandDiagnostic(raw, token, "Invalid boolean for `"+name+"`.", "Expected one of: `on`, `off`, `true`, `false`, `enable`, `disable`.")
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findCommandModel(value string) (model.Model, bool) {
	query := strings.TrimSpace(value)
	for _, m := range model.AllModels {
		if strings.EqualFold(m.Name, query) || strings.EqualFold(m.Command, query) {
			return m, true
		}
	}
	return model.Model{}, false
}

func findCommandPersona(value string) (persona.PersonaMeta, bool) {
	query := strings.TrimSpace(value)
	if query == "" {
		return persona.PersonaMeta{}, false
	}
	if n, err := strconv.Atoi(query); err == nil {
		idx := n - 1
		if idx >= 0 && idx < len(persona.AllPersonas) {
			return persona.AllPersonas[idx], true
		}
		return persona.PersonaMeta{}, false
	}
	normalizedQuery := normalizePersonaQuery(query)
	for _, p := range persona.AllPersonas {
		if normalizePersonaQuery(p.Name) == normalizedQuery ||
			normalizePersonaQuery(p.String()) == normalizedQuery ||
			strings.HasPrefix(normalizePersonaQuery(p.Name), normalizedQuery) ||
			strings.HasPrefix(normalizePersonaQuery(p.String()), normalizedQuery) {
			return p, true
		}
	}
	return persona.PersonaMeta{}, false
}

func normalizePersonaQuery(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("(", " ", ")", " ", "[", " ", "]", " ", "-", " ", "_", " ")
	value = replacer.Replace(value)
	parts := strings.Fields(value)
	filtered := parts[:0]
	for _, part := range parts {
		if part == "default" || part == "custom" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, " ")
}

func personaInfoText(cache *db.ChannelCache, username, botName string, isDM bool) string {
	settings := cache.PersonaMeta.Settings.Fixup()
	remapped := settings
	remapped.Remap()
	promptContext := persona.PromptContext{
		Memories: append([]string(nil), cache.Memories...),
		Context:  append([]string(nil), cache.Context...),
	}
	if summariesEnabled() {
		promptContext.Summaries = append([]persona.Summary(nil), cache.Summaries...)
	}
	system := persona.GetPersonaByMetaWithBotName(cache.PersonaMeta, username, botName, isDM, promptContext).System
	models := cache.PersonaMeta.Models
	if len(models) == 0 {
		models = persona.PersonaProto.Models
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Persona: %s\n", cache.PersonaMeta.Name)
	fmt.Fprintf(&b, "Model: %s\n", strings.Join(models, ", "))
	fmt.Fprintf(&b, "Temperature: %s (remapped to %s)\n", ftoa(settings.Temperature), ftoa(remapped.Temperature))
	fmt.Fprintf(&b, "Top P: %s (remapped to %s)\n", ftoa(settings.TopP), ftoa(remapped.TopP))
	fmt.Fprintf(&b, "Frequency penalty: %s\n", ftoa(settings.FrequencyPenalty))
	fmt.Fprintf(&b, "Context length: %d\n", cache.ContextLength)
	fmt.Fprintf(&b, "Images: %t\nReasoning: %t\nThinking traces: %t\nHTML rendering: %t\nMiniLM continuations: %t\nTools: %t\nJailbreak: %s\n", cache.PersonaMeta.EnableImages, settings.Reasoning, cache.PersonaMeta.ThinkingTraces, cache.PersonaMeta.RenderHTML, cache.PersonaMeta.EnableMiniLMContinuations, cache.PersonaMeta.ToolsEnabled(), cache.PersonaMeta.JailbreakMode())
	if cache.PersonaMeta.ChatPreset != nil {
		fmt.Fprintf(&b, "SillyTavern preset: %s\n", cache.PersonaMeta.ChatPreset.DisplayName())
	}
	if system != "" {
		fmt.Fprintf(&b, "\nSystem prompt:\n%s", ellipsisTrim(system, 1600))
	}
	return strings.TrimSpace(b.String())
}

// modelListText renders the models list for text-command platforms. The caller
// appends its own "set with" usage line.
func modelListText() string {
	var list strings.Builder
	list.WriteString("Available models:\n")
	for _, m := range model.AllModels {
		if strings.TrimSpace(m.Command) == "" {
			continue
		}
		fmt.Fprintf(&list, "%s - %s", m.Command, m.Name)
		flags := make([]string, 0, 4)
		if m.Name == model.DefaultModel {
			flags = append(flags, "default")
		}
		if m.Whitelisted {
			flags = append(flags, "whitelist")
		}
		if m.Vision {
			flags = append(flags, "vision")
		}
		if m.Reasoning {
			flags = append(flags, "reasoning")
		}
		if len(flags) > 0 {
			fmt.Fprintf(&list, " (%s)", strings.Join(flags, ", "))
		}
		list.WriteString("\n")
	}
	return strings.TrimRight(list.String(), "\n")
}
