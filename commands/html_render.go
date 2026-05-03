package commands

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/zeozeozeo/x3/htmlrender"
	"github.com/zeozeozeo/x3/persona"
)

var gotenbergRenderer = htmlrender.NewRendererFromEnv()

func prepareHTMLRenderedResponse(ctx context.Context, meta persona.PersonaMeta, response string, files []*discord.File) (display string, outFiles []*discord.File, changed bool) {
	if !meta.RenderHTML {
		return response, files, false
	}
	result, err := htmlrender.RenderResponse(ctx, gotenbergRenderer, response, 3)
	if err != nil {
		slog.Warn("HTML render failed; keeping raw response", "err", err)
		return appendHTMLRenderFailure(response, err), files, false
	}
	if !result.Changed {
		return response, files, false
	}
	for _, block := range result.Blocks {
		files = append(files, &discord.File{
			Name:   block.Filename,
			Reader: bytes.NewReader(block.Data),
		})
	}
	return result.DisplayText, files, true
}

func appendHTMLRenderFailure(response string, err error) string {
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		reason = "unknown error"
	}
	return strings.TrimSpace(response) + fmt.Sprintf("\n-# HTML render failed: %s", ellipsisTrim(reason, 300))
}
