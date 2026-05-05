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
