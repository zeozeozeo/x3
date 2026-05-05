//go:build matrix || goolm

package commands

import (
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
	content := format.RenderMarkdownCustom(text, matrixMarkdownRenderer)
	content.Mentions = &event.Mentions{
		UserIDs: []id.UserID{},
	}
	return &content
}
