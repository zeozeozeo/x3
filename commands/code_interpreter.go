package commands

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/codeinterp"
)

var (
	codeFenceRegexp = regexp.MustCompile("(?s)```[ \\t]*([A-Za-z0-9.+_-]+)[ \\t]*\\r?\\n(.*?)```")
	fileTagRegexp   = regexp.MustCompile(`(?i)<file>\s*([^<>]+?)\s*</file>`)
)

const codeRunResultMarker = "\u2063\u200b\u2063\u200c\u2063"

type runnableCodeBlock struct {
	Language string
	Code     string
}

func runnableCodeBlocks(content string) []runnableCodeBlock {
	matches := codeFenceRegexp.FindAllStringSubmatch(content, -1)
	blocks := make([]runnableCodeBlock, 0, len(matches))
	for _, match := range matches {
		language := codeinterp.NormalizeLanguage(match[1])
		if language == "" {
			continue
		}
		blocks = append(blocks, runnableCodeBlock{Language: language, Code: match[2]})
	}
	return blocks
}

func runnableCodeComponents(content string) []discord.LayoutComponent {
	if !codeinterp.Enabled() {
		return nil
	}
	blocks := runnableCodeBlocks(content)
	if len(blocks) == 0 {
		return nil
	}
	if len(blocks) > 5 {
		blocks = blocks[:5]
	}
	buttons := make([]discord.InteractiveComponent, 0, len(blocks))
	for i, block := range blocks {
		label := "Run " + languageDisplayName(block.Language)
		buttons = append(buttons, discord.ButtonComponent{
			Style:    discord.ButtonStyleSecondary,
			Label:    label,
			Emoji:    &discord.ComponentEmoji{Name: "▶️"},
			CustomID: fmt.Sprintf("/code-run/%s/%d", block.Language, i),
		})
	}
	return []discord.LayoutComponent{discord.NewActionRow(buttons...)}
}

func languageDisplayName(language string) string {
	switch language {
	case "javascript":
		return "JavaScript"
	case "typescript":
		return "TypeScript"
	case "php":
		return "PHP"
	case "shell":
		return "Bash"
	default:
		if language == "" {
			return "Code"
		}
		return strings.ToUpper(language[:1]) + language[1:]
	}
}

func consumeGeneratedArtifacts(response string, artifacts []codeinterp.Artifact) (string, []*discord.File) {
	byName := make(map[string]codeinterp.Artifact, len(artifacts))
	for _, artifact := range artifacts {
		name := filepath.Base(artifact.Name)
		if name == artifact.Name && name != "." && name != "" {
			byName[name] = artifact
		}
	}
	seen := make(map[string]struct{})
	files := make([]*discord.File, 0)
	display := fileTagRegexp.ReplaceAllStringFunc(response, func(tag string) string {
		match := fileTagRegexp.FindStringSubmatch(tag)
		name := filepath.Base(strings.TrimSpace(match[1]))
		artifact, ok := byName[name]
		if !ok {
			return ""
		}
		if _, exists := seen[name]; !exists {
			seen[name] = struct{}{}
			files = append(files, &discord.File{Name: name, Reader: bytes.NewReader(artifact.Data)})
		}
		return ""
	})
	return strings.TrimSpace(display), files
}

func HandleCodeRunButton(_ discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	if !codeinterp.Enabled() {
		return sendInteractionErrorComponent(event, "The code interpreter is disabled.", true)
	}
	index, err := strconv.Atoi(event.Vars["index"])
	if err != nil || index < 0 {
		return sendInteractionErrorComponent(event, "Invalid code block.", true)
	}
	blocks := runnableCodeBlocks(event.Message.Content)
	if index >= len(blocks) || blocks[index].Language != codeinterp.NormalizeLanguage(event.Vars["language"]) {
		return sendInteractionErrorComponent(event, "That code block is no longer available.", true)
	}
	if err := event.DeferCreateMessage(true); err != nil {
		return err
	}
	result, runErr := codeinterp.Run(context.Background(), blocks[index].Language, blocks[index].Code)
	if runErr != nil {
		_, err = event.UpdateInteractionResponse(discord.NewMessageUpdate().WithContent("Code execution failed: " + runErr.Error()))
		return err
	}

	content := formatCodeRunResult(result)
	files := make([]*discord.File, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		files = append(files, &discord.File{Name: filepath.Base(artifact.Name), Reader: bytes.NewReader(artifact.Data)})
	}
	update := discord.NewMessageUpdate().WithContent(content).AddFiles(files...)
	_, err = event.UpdateInteractionResponse(update)
	return err
}

func formatCodeRunResult(result codeinterp.Result) string {
	var b strings.Builder
	b.WriteString(codeRunResultMarker)
	fmt.Fprintf(&b, "Exited with code %d.", result.ExitCode)
	if result.Stdout != "" {
		fmt.Fprintf(&b, "\n\nstdout:\n```text\n%s\n```", escapeCodeFence(result.Stdout))
	}
	if result.Stderr != "" {
		fmt.Fprintf(&b, "\n\nstderr:\n```text\n%s\n```", escapeCodeFence(result.Stderr))
	}
	content := b.String()
	if len([]rune(content)) > 1900 {
		content = string([]rune(content)[:1900]) + "\n… (output truncated)"
	}
	return content
}

func escapeCodeFence(value string) string {
	return strings.ReplaceAll(value, "```", "` ` `")
}
