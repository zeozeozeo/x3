//go:build matrix || goolm

package commands

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/format"
	"maunium.net/go/mautrix/format/mdext"
	"maunium.net/go/mautrix/id"
)

var matrixMarkdownRenderer = goldmark.New(
	goldmark.WithExtensions(
		extension.Strikethrough,
		extension.Table,
		mdext.Spoiler,
		mdext.Math,
		mdext.EscapeHTML,
	),
	goldmark.WithRendererOptions(html.WithHardWraps(), html.WithUnsafe()),
)

func matrixTextContent(text string) *event.MessageEventContent {
	text = normalizeMatrixMathDelimiters(text)
	content := format.RenderMarkdownCustom(text, matrixMarkdownRenderer)
	content.Mentions = &event.Mentions{
		UserIDs: []id.UserID{},
	}
	return &content
}

func normalizeMatrixMathDelimiters(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")

	var out strings.Builder
	var prose strings.Builder
	inCodeFence := false
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(line)
		isFence := strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
		if isFence {
			if !inCodeFence {
				out.WriteString(normalizeMatrixMathInProse(prose.String()))
				prose.Reset()
			}
			out.WriteString(line)
			inCodeFence = !inCodeFence
			continue
		}
		if inCodeFence {
			out.WriteString(line)
		} else {
			prose.WriteString(line)
		}
	}
	out.WriteString(normalizeMatrixMathInProse(prose.String()))
	return out.String()
}

func normalizeMatrixMathInProse(text string) string {
	parts := parseLatexSegments(text, latexParseOptions{
		AllowSlashDelimiters:   true,
		AllowUnclosedDisplay:   true,
		AllowUnclosedBracketed: true,
	})
	var out strings.Builder
	for _, part := range parts {
		if !part.IsLatex {
			out.WriteString(part.Content)
			continue
		}
		if part.Display {
			out.WriteString(matrixDisplayMathBlock(part.Content))
		} else {
			out.WriteString("$")
			out.WriteString(part.Content)
			out.WriteString("$")
		}
	}
	return out.String()
}

func matrixDisplayMathBlock(tex string) string {
	tex = strings.TrimSpace(tex)
	if tex == "" {
		return "$$"
	}
	return "\n$$\n" + tex + "\n$$\n"
}
