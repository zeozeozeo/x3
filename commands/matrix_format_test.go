//go:build matrix || goolm

package commands

import (
	"strings"
	"testing"

	"maunium.net/go/mautrix/event"
)

func TestMatrixTextContentRendersMarkdownAndMath(t *testing.T) {
	content := matrixTextContent("*italic* and $E=mc^2$")
	if content.Format != event.FormatHTML {
		t.Fatalf("expected Matrix HTML formatting, got %q", content.Format)
	}
	for _, want := range []string{"<em>italic</em>", `data-mx-maths="E=mc^2"`} {
		if !strings.Contains(content.FormattedBody, want) {
			t.Fatalf("formatted body missing %q: %s", want, content.FormattedBody)
		}
	}
}

func TestMatrixTextContentNormalizesSameLineDisplayMath(t *testing.T) {
	content := matrixTextContent(`$$\int_{0}^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}$$`)
	if !strings.Contains(content.FormattedBody, `data-mx-maths="\int_{0}^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}"`) {
		t.Fatalf("formatted body did not contain display math:\n%s", content.FormattedBody)
	}
}

func TestMatrixTextContentNormalizesUnclosedDisplayMath(t *testing.T) {
	content := matrixTextContent(`$$\int_{0}^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}`)
	if !strings.Contains(content.FormattedBody, `data-mx-maths="\int_{0}^{\infty} e^{-x^2} dx = \frac{\sqrt{\pi}}{2}"`) {
		t.Fatalf("formatted body did not contain unclosed display math:\n%s", content.FormattedBody)
	}
}

func TestMatrixTextContentNormalizesSlashMathDelimiters(t *testing.T) {
	content := matrixTextContent(`inline \(x^2\) and display \[\frac{1}{2}\]`)
	for _, want := range []string{`data-mx-maths="x^2"`, `data-mx-maths="\frac{1}{2}"`} {
		if !strings.Contains(content.FormattedBody, want) {
			t.Fatalf("formatted body missing %q:\n%s", want, content.FormattedBody)
		}
	}
}

func TestMatrixTextContentLeavesUnclosedInlineDollarLiteral(t *testing.T) {
	content := matrixTextContent(`this costs $5 and that costs $$10`)
	if strings.Contains(content.FormattedBody, "data-mx-maths") {
		t.Fatalf("unclosed inline-ish dollars should remain literal:\n%s", content.FormattedBody)
	}
}

func TestMatrixTextContentDoesNotNormalizeMathInCodeFence(t *testing.T) {
	content := matrixTextContent("```text\n$$x^2$$\n```")
	if strings.Contains(content.FormattedBody, "data-mx-maths") {
		t.Fatalf("code fence math should remain literal:\n%s", content.FormattedBody)
	}
}
