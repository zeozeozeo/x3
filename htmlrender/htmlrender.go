package htmlrender

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	xhtml "golang.org/x/net/html"
)

const (
	DefaultWidth        = 900
	DefaultHeight       = 1600
	maxInlineImageBytes = 10 * 1024 * 1024
	maxInlineImageCount = 8
)

var (
	htmlTagRegexp       = regexp.MustCompile(`(?is)<html\b[^>]*>(.*?)</html>`)
	strayFenceRegexp    = regexp.MustCompile("(?m)^\\s*```\\s*$")
	blankLineRegexp     = regexp.MustCompile(`\n{3,}`)
	cssImportRegexp     = regexp.MustCompile(`(?is)@import[^;]+;?`)
	cssURLRegexp        = regexp.MustCompile(`(?is)url\s*\(\s*['"]?\s*[^'"\s#)][^)]*\)`)
	cssExprRegexp       = regexp.MustCompile(`(?is)expression\s*\(|javascript:`)
	safeImageSrcRegexp  = regexp.MustCompile(`(?is)^(https://|data:image/(?:png|gif|jpeg|jpg|webp|avif);base64,)`)
	safeDataImageRegexp = regexp.MustCompile(`(?is)^data:image/(?:png|gif|jpeg|jpg|webp|avif);base64,`)
	svgIDRefRegexp      = regexp.MustCompile(`(?i)^url\(\s*#[a-z][a-z0-9_-]*\s*\)$`)
	svgNameRegexp       = regexp.MustCompile(`(?i)^[a-z][a-z0-9_-]{0,80}$`)
	svgNumberRegexp     = regexp.MustCompile(`(?i)^-?(?:\d+|\d*\.\d+)(?:e-?\d+)?$`)
	svgLengthRegexp     = regexp.MustCompile(`(?i)^-?(?:\d+|\d*\.\d+)(?:e-?\d+)?(?:px|em|rem|%|vh|vw)?$`)
	svgListRegexp       = regexp.MustCompile(`(?i)^[a-z0-9#%.,:;() _+\-/]+$`)
	htmlPolicy          = newHTMLPolicy()
	htmlMarkdown        = goldmark.New()
	blockedIPPrefixes   = mustParsePrefixes(
		"0.0.0.0/8",
		"10.0.0.0/8",
		"100.64.0.0/10",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"172.16.0.0/12",
		"192.0.0.0/24",
		"192.0.2.0/24",
		"192.168.0.0/16",
		"198.18.0.0/15",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"224.0.0.0/4",
		"240.0.0.0/4",
		"::/128",
		"::1/128",
		"::ffff:0:0/96",
		"64:ff9b:1::/48",
		"100::/64",
		"2001:db8::/32",
		"fc00::/7",
		"fe80::/10",
		"ff00::/8",
	)
)

type Block struct {
	HTML string
}

type RenderedBlock struct {
	Data     []byte
	Filename string
}

type Result struct {
	DisplayText string
	Blocks      []RenderedBlock
	Changed     bool
}

type Renderer struct {
	BaseURL string
	Client  *http.Client
	Width   int
	Height  int
}

func NewRendererFromEnv() *Renderer {
	rawURL := strings.TrimSpace(os.Getenv("X3_GOTENBERG_URL"))
	if rawURL == "" {
		return nil
	}
	return &Renderer{
		BaseURL: strings.TrimRight(rawURL, "/"),
		Client:  &http.Client{Timeout: 30 * time.Second},
		Width:   DefaultWidth,
		Height:  DefaultHeight,
	}
}

func Extract(response string, limit int) (display string, blocks []Block, changed bool) {
	display = response
	if limit <= 0 {
		limit = 3
	}

	var ranges []sourceRange
	ranges, blocks = extractMarkdownHTMLBlocks(response, limit)
	if len(ranges) > 0 {
		display = removeSourceRanges(response, ranges)
		changed = true
	}

	if len(blocks) < limit {
		var legacyBlocks []Block
		var legacyChanged bool
		display, legacyBlocks, legacyChanged = extractLegacyHTMLTagBlocks(display, limit-len(blocks))
		if legacyChanged {
			blocks = append(blocks, legacyBlocks...)
			changed = true
		}
	}

	display = strings.TrimSpace(display)
	display = blankLineRegexp.ReplaceAllString(display, "\n\n")
	return display, blocks, changed
}

type sourceRange struct {
	start int
	end   int
}

func extractMarkdownHTMLBlocks(response string, limit int) ([]sourceRange, []Block) {
	source := []byte(response)
	doc := htmlMarkdown.Parser().Parse(text.NewReader(source))
	var ranges []sourceRange
	var blocks []Block
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || len(blocks) >= limit {
			return ast.WalkContinue, nil
		}
		fence, ok := n.(*ast.FencedCodeBlock)
		if !ok {
			return ast.WalkContinue, nil
		}
		content := cleanExtractedHTML(string(fence.Text(source)))
		if content == "" || !isHTMLFence(fence, source, content) {
			return ast.WalkContinue, nil
		}
		blocks = append(blocks, Block{HTML: content})
		if r, ok := fencedCodeBlockSourceRange(source, fence); ok {
			ranges = append(ranges, r)
		}
		return ast.WalkSkipChildren, nil
	})
	return ranges, blocks
}

func isHTMLFence(fence *ast.FencedCodeBlock, source []byte, content string) bool {
	lang := strings.ToLower(strings.TrimSpace(string(fence.Language(source))))
	if lang == "html" || lang == "htm" {
		return true
	}
	if lang != "" {
		return false
	}
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "<") && strings.Contains(trimmed, ">")
}

func fencedCodeBlockSourceRange(source []byte, fence *ast.FencedCodeBlock) (sourceRange, bool) {
	if len(source) == 0 {
		return sourceRange{}, false
	}

	openingLineStart := 0
	if fence.Info != nil {
		openingLineStart = lineStart(source, fence.Info.Segment.Start)
	} else if fence.Lines().Len() > 0 {
		openingLineStart = lineStart(source, fence.Lines().At(0).Start)
	} else if pos := fence.Pos(); pos >= 0 {
		openingLineStart = lineStart(source, pos)
	} else {
		return sourceRange{}, false
	}
	openingLineEnd := lineEnd(source, openingLineStart)
	openingLine := strings.TrimLeft(string(source[openingLineStart:openingLineEnd]), " \t")
	if openingLine == "" || openingLine[0] != '`' && openingLine[0] != '~' {
		return sourceRange{}, false
	}
	marker := openingLine[0]
	fenceLen := 0
	for fenceLen < len(openingLine) && openingLine[fenceLen] == marker {
		fenceLen++
	}
	if fenceLen < 3 {
		return sourceRange{}, false
	}

	searchFrom := openingLineEnd
	if fence.Lines().Len() > 0 {
		last := fence.Lines().At(fence.Lines().Len() - 1)
		searchFrom = max(last.Stop, searchFrom)
	}
	for searchFrom < len(source) {
		currentLineStart := lineStart(source, searchFrom)
		currentLineEnd := lineEnd(source, currentLineStart)
		line := strings.TrimSpace(string(source[currentLineStart:currentLineEnd]))
		if strings.HasPrefix(line, strings.Repeat(string(marker), fenceLen)) {
			return sourceRange{start: openingLineStart, end: includeLineBreak(source, currentLineEnd)}, true
		}
		next := includeLineBreak(source, currentLineEnd)
		if next <= searchFrom {
			break
		}
		searchFrom = next
	}
	return sourceRange{start: openingLineStart, end: includeLineBreak(source, openingLineEnd)}, true
}

func lineStart(source []byte, pos int) int {
	pos = min(max(pos, 0), len(source))
	if i := strings.LastIndexByte(string(source[:pos]), '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

func lineEnd(source []byte, pos int) int {
	pos = min(max(pos, 0), len(source))
	if i := strings.IndexByte(string(source[pos:]), '\n'); i >= 0 {
		return pos + i
	}
	return len(source)
}

func includeLineBreak(source []byte, pos int) int {
	if pos < len(source) && source[pos] == '\n' {
		return pos + 1
	}
	return pos
}

func removeSourceRanges(input string, ranges []sourceRange) string {
	sort.Slice(ranges, func(i, j int) bool {
		return ranges[i].start > ranges[j].start
	})
	for _, r := range ranges {
		if r.start < 0 || r.end < r.start || r.end > len(input) {
			continue
		}
		input = input[:r.start] + "\n" + input[r.end:]
	}
	return input
}

func extractLegacyHTMLTagBlocks(display string, limit int) (string, []Block, bool) {
	var blocks []Block
	changed := false
	display = htmlTagRegexp.ReplaceAllStringFunc(display, func(match string) string {
		if len(blocks) >= limit {
			return match
		}
		sub := htmlTagRegexp.FindStringSubmatch(match)
		if len(sub) != 2 || strings.TrimSpace(sub[1]) == "" {
			return match
		}
		blocks = append(blocks, Block{HTML: cleanExtractedHTML(sub[1])})
		changed = true
		return "\n"
	})
	return display, blocks, changed
}

func cleanExtractedHTML(s string) string {
	s = trimHTMLFenceDebris(s)
	s = strayFenceRegexp.ReplaceAllString(s, "")
	s = trimHTMLFenceDebris(s)
	return s
}

func trimHTMLFenceDebris(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\u200b\ufeff")
	return strings.TrimSpace(s)
}

func RenderResponse(ctx context.Context, renderer *Renderer, response string, limit int) (Result, error) {
	display, blocks, changed := Extract(response, limit)
	result := Result{DisplayText: display, Changed: changed}
	if !changed {
		result.DisplayText = response
		return result, nil
	}
	if renderer == nil {
		return Result{DisplayText: response, Changed: false}, fmt.Errorf("HTML rendering is enabled but X3_GOTENBERG_URL is not configured")
	}

	for i, block := range blocks {
		sanitized, err := Sanitize(block.HTML)
		if err != nil {
			return Result{DisplayText: response, Changed: false}, err
		}
		sanitized, err = inlineRemoteImages(ctx, renderer, sanitized)
		if err != nil {
			return Result{DisplayText: response, Changed: false}, err
		}
		data, err := renderer.Render(ctx, sanitized)
		if err != nil {
			return Result{DisplayText: response, Changed: false}, err
		}
		result.Blocks = append(result.Blocks, RenderedBlock{
			Data:     data,
			Filename: fmt.Sprintf("x3-render-%d.png", i+1),
		})
	}
	return result, nil
}

func (r *Renderer) Render(ctx context.Context, bodyHTML string) ([]byte, error) {
	if r == nil || strings.TrimSpace(r.BaseURL) == "" {
		return nil, fmt.Errorf("gotenberg renderer is not configured")
	}
	client := r.Client
	if client == nil {
		client = http.DefaultClient
	}
	width := r.Width
	if width <= 0 {
		width = DefaultWidth
	}
	height := r.Height
	if height <= 0 {
		height = DefaultHeight
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writeFileField(writer, "files", "index.html", []byte(wrapDocument(bodyHTML))); err != nil {
		return nil, err
	}
	fields := map[string]string{
		"width":          fmt.Sprintf("%d", width),
		"height":         fmt.Sprintf("%d", height),
		"clip":           "false",
		"format":         "png",
		"omitBackground": "true",
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+"/forms/chromium/screenshot/html", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Gotenberg-Output-Filename", "x3-render")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gotenberg render failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if len(data) > 8*1024*1024 {
		return nil, fmt.Errorf("rendered image exceeds 8MB limit")
	}
	cropped, err := cropTransparentPNG(data)
	if err != nil {
		return nil, err
	}
	return cropped, nil
}

func writeFileField(writer *multipart.Writer, field, filename string, data []byte) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, field, filename))
	header.Set("Content-Type", "text/html; charset=utf-8")
	part, err := writer.CreatePart(header)
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func wrapDocument(fragment string) string {
	bodyHTML, styleCSS := splitStyleBlocks(fragment)
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root {
  color-scheme: dark;
  --SmartThemeBodyColor: #d8dee9;
  --SmartThemeEmColor: #d8dee9;
  --SmartThemeQuoteColor: #a7b1c2;
  --SmartThemeBlurTintColor: rgba(20, 23, 31, 0.92);
  --SmartThemeBorderColor: rgba(255, 255, 255, 0.18);
  --SmartThemeUserMesBlurTintColor: rgba(47, 53, 68, 0.72);
  --SmartThemeBotMesBlurTintColor: rgba(28, 32, 43, 0.78);
  --SmartThemeShadowColor: rgba(0, 0, 0, 0.35);
}
html, body {
  margin: 0;
  background: transparent;
}
body {
  box-sizing: border-box;
  font-family: "Noto Sans", Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  font-size: 16px;
  line-height: 1.45;
  color: var(--SmartThemeBodyColor);
  -webkit-font-smoothing: antialiased;
}
* {
  box-sizing: border-box;
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}
.x3-st-render {
  width: fit-content;
  max-width: 100%;
  color: var(--SmartThemeBodyColor);
  overflow-wrap: anywhere;
}
.mes_text {
  color: var(--SmartThemeBodyColor);
  text-shadow: 0 1px 2px var(--SmartThemeShadowColor);
}
.mes_text em,
.mes_text i {
  color: var(--SmartThemeEmColor);
}
.mes_text blockquote {
  margin: 0.6em 0;
  padding: 0.15em 0 0.15em 0.8em;
  border-left: 3px solid var(--SmartThemeBorderColor);
  color: var(--SmartThemeQuoteColor);
}
.mes_text code,
.mes_text pre {
  border: 1px solid var(--SmartThemeBorderColor);
  border-radius: 6px;
  background: rgba(0, 0, 0, 0.26);
  font-family: ui-monospace, "SFMono-Regular", Consolas, "Liberation Mono", monospace;
}
.mes_text code {
  padding: 0.08em 0.28em;
}
.mes_text pre {
  padding: 10px 12px;
  overflow: hidden;
}
.mes_text table {
  border-collapse: collapse;
  max-width: 100%;
}
.mes_text th,
.mes_text td {
  padding: 6px 9px;
  border: 1px solid var(--SmartThemeBorderColor);
}
.mes_text img {
  max-width: 100%;
  height: auto;
}
</style>` + inlineStyleBlock(styleCSS) + `
<style>
html,
body {
  margin: 0 !important;
  width: max-content !important;
  min-width: 0 !important;
  max-width: none !important;
  min-height: 0 !important;
  background: transparent !important;
  overflow: visible !important;
}
body {
  display: block !important;
  padding: 0 !important;
}
.x3-render-capture {
  display: block !important;
  box-sizing: border-box !important;
  width: max-content !important;
  max-width: 900px !important;
  min-width: 1px !important;
  min-height: 1px !important;
  margin: 0 !important;
  padding: 24px !important;
  background: transparent !important;
  overflow: visible !important;
}
</style>
</head>
<body><main class="mes_text x3-st-render x3-render-capture">` + bodyHTML + `</main></body>
</html>`
}

func cropTransparentPNG(data []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := alphaBounds(img)
	if bounds.Empty() {
		return nil, fmt.Errorf("rendered image was fully transparent")
	}
	if bounds.Eq(img.Bounds()) {
		if backgroundRemoved, ok := removeSolidEdgeBackground(img); ok {
			if cropped, err := cropTransparentPNGFromImage(backgroundRemoved); err == nil {
				return cropped, nil
			}
		}
		// Full-canvas artifacts are valid sometimes; send them as-is when the
		// background cannot be identified safely.
		return data, nil
	}
	return cropTransparentPNGFromImage(img)
}

func cropTransparentPNGFromImage(img image.Image) ([]byte, error) {
	bounds := alphaBounds(img)
	if bounds.Empty() {
		return nil, fmt.Errorf("rendered image was fully transparent")
	}
	padded := padRect(bounds, img.Bounds(), 1)
	cropped := image.NewNRGBA(image.Rect(0, 0, padded.Dx(), padded.Dy()))
	for y := 0; y < padded.Dy(); y++ {
		for x := 0; x < padded.Dx(); x++ {
			cropped.Set(x, y, img.At(padded.Min.X+x, padded.Min.Y+y))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, cropped); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func alphaBounds(img image.Image) image.Rectangle {
	bounds := img.Bounds()
	minX, minY := bounds.Max.X, bounds.Max.Y
	maxX, maxY := bounds.Min.X, bounds.Min.Y
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x+1 > maxX {
				maxX = x + 1
			}
			if y+1 > maxY {
				maxY = y + 1
			}
			found = true
		}
	}
	if !found {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

func removeSolidEdgeBackground(img image.Image) (image.Image, bool) {
	bounds := img.Bounds()
	bg := img.At(bounds.Min.X, bounds.Min.Y)
	if !edgeMatchesColor(img, bg) {
		return nil, false
	}
	out := image.NewNRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.At(x, y)
			if colorsClose(c, bg) {
				out.Set(x, y, colorTransparent)
			} else {
				out.Set(x, y, c)
			}
		}
	}
	if alphaBounds(out).Empty() {
		return nil, false
	}
	return out, true
}

var colorTransparent = image.NewUniform(image.Transparent).C

func edgeMatchesColor(img image.Image, bg color.Color) bool {
	bounds := img.Bounds()
	total := 0
	matches := 0
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		total += 2
		if colorsClose(img.At(x, bounds.Min.Y), bg) {
			matches++
		}
		if colorsClose(img.At(x, bounds.Max.Y-1), bg) {
			matches++
		}
	}
	for y := bounds.Min.Y + 1; y < bounds.Max.Y-1; y++ {
		total += 2
		if colorsClose(img.At(bounds.Min.X, y), bg) {
			matches++
		}
		if colorsClose(img.At(bounds.Max.X-1, y), bg) {
			matches++
		}
	}
	return total > 0 && float64(matches)/float64(total) >= 0.96
}

func colorsClose(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	const tolerance = 3 * 0x101
	return absDiff32(ar, br) <= tolerance &&
		absDiff32(ag, bg) <= tolerance &&
		absDiff32(ab, bb) <= tolerance &&
		absDiff32(aa, ba) <= tolerance
}

func absDiff32(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func padRect(rect, bounds image.Rectangle, padding int) image.Rectangle {
	rect.Min.X = max(rect.Min.X-padding, bounds.Min.X)
	rect.Min.Y = max(rect.Min.Y-padding, bounds.Min.Y)
	rect.Max.X = min(rect.Max.X+padding, bounds.Max.X)
	rect.Max.Y = min(rect.Max.Y+padding, bounds.Max.Y)
	return rect
}

func Sanitize(input string) (string, error) {
	bodyHTML, styleCSS := splitStyleBlocks(input)
	sanitizedBody := strings.TrimSpace(stripUnsafeHTMLURLs(htmlPolicy.Sanitize(bodyHTML)))
	if styleCSS == "" {
		return sanitizedBody, nil
	}
	return strings.TrimSpace(sanitizedBody + "\n" + inlineStyleBlock(styleCSS)), nil
}

func newHTMLPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowURLSchemes("https", "data")
	p.RequireParseableURLs(true)
	p.AllowDataURIImages()
	p.AllowElements(
		"a", "abbr", "article", "aside", "b", "blockquote", "br", "caption", "cite",
		"code", "col", "colgroup", "dd", "del", "details", "dfn", "div", "dl", "dt",
		"em", "figcaption", "figure", "footer", "h1", "h2", "h3", "h4", "h5", "h6",
		"header", "hr", "i", "img", "ins", "kbd", "li", "main", "mark", "nav", "ol",
		"p", "pre", "q", "s", "section", "small", "span", "strong", "sub",
		"summary", "sup", "table", "tbody", "td", "tfoot", "th", "thead", "time", "tr",
		"u", "ul", "var",
	)
	p.AllowElements(svgElements()...)
	p.AllowAttrs("class", "id", "title", "role", "aria-label").Matching(bluemonday.Paragraph).Globally()
	p.AllowAttrs("colspan", "rowspan").Matching(bluemonday.Integer).OnElements("td", "th")
	p.AllowAttrs("width", "height").Matching(bluemonday.NumberOrPercent).OnElements("img", "table", "td", "th")
	p.AllowAttrs("alt").Matching(bluemonday.Paragraph).OnElements("img")
	p.AllowAttrs("src").Matching(safeImageSrcRegexp).OnElements("img")
	p.AllowAttrs("style").Globally()
	p.AllowAttrs("id").Matching(svgNameRegexp).OnElements(svgElements()...)
	p.AllowAttrs("x", "y", "x1", "x2", "y1", "y2", "cx", "cy", "r", "rx", "ry", "width", "height", "dx", "dy", "offset").Matching(svgLengthRegexp).OnElements(svgElements()...)
	p.AllowAttrs("opacity", "fill-opacity", "stroke-opacity", "flood-opacity", "stop-opacity", "stdDeviation", "stddeviation").Matching(svgNumberRegexp).OnElements(svgElements()...)
	p.AllowAttrs("viewBox", "viewbox", "points", "d", "transform", "gradientTransform", "gradienttransform", "patternTransform", "patterntransform", "values", "result", "in", "in2", "operator", "mode", "type", "baseFrequency", "basefrequency", "numOctaves", "numoctaves", "seed", "scale", "surfaceScale", "surfacescale", "lighting-color", "preserveAspectRatio", "preserveaspectratio", "orient", "markerUnits", "markerunits", "patternUnits", "patternunits", "patternContentUnits", "patterncontentunits", "refX", "refx", "refY", "refy", "markerWidth", "markerwidth", "markerHeight", "markerheight", "maskUnits", "maskunits", "maskContentUnits", "maskcontentunits", "clipPathUnits", "clippathunits", "fill-rule", "clip-rule").Matching(svgListRegexp).OnElements(svgElements()...)
	p.AllowAttrs("fill", "stroke", "color", "flood-color", "stop-color").Matching(svgListRegexp).OnElements(svgElements()...)
	p.AllowAttrs("stroke-width", "stroke-linecap", "stroke-linejoin", "stroke-dasharray", "font-size", "font-family", "font-weight", "text-anchor", "dominant-baseline").Matching(svgListRegexp).OnElements(svgElements()...)
	p.AllowAttrs("filter", "clip-path", "mask", "marker-start", "marker-mid", "marker-end", "fill").Matching(svgIDRefRegexp).OnElements(svgElements()...)
	return p
}

func splitStyleBlocks(input string) (bodyHTML, styleCSS string) {
	root, err := xhtml.Parse(strings.NewReader(input))
	if err != nil {
		return input, ""
	}

	var styles []string
	extractStylesAndSanitize(root, &styles)

	body := findElement(root, "body")
	if body == nil {
		body = root
	}

	var out strings.Builder
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := xhtml.Render(&out, child); err != nil {
			return input, strings.TrimSpace(strings.Join(styles, "\n"))
		}
	}
	return strings.TrimSpace(out.String()), strings.TrimSpace(strings.Join(styles, "\n"))
}

func extractStylesAndSanitize(node *xhtml.Node, styles *[]string) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == xhtml.ElementNode {
			if strings.EqualFold(child.Data, "style") {
				if css := sanitizeCSS(textContent(child)); css != "" {
					*styles = append(*styles, css)
				}
				node.RemoveChild(child)
				child = next
				continue
			}

			for i := range child.Attr {
				if strings.EqualFold(child.Attr[i].Key, "style") {
					child.Attr[i].Val = sanitizeCSS(child.Attr[i].Val)
				}
			}
		}
		extractStylesAndSanitize(child, styles)
		child = next
	}
}

func findElement(node *xhtml.Node, name string) *xhtml.Node {
	if node == nil {
		return nil
	}
	if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, name) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func extractStyleNodes(node *xhtml.Node, styles *[]string) {
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == xhtml.ElementNode {
			if strings.EqualFold(child.Data, "style") {
				if css := sanitizeCSS(textContent(child)); css != "" {
					*styles = append(*styles, css)
				}
				node.RemoveChild(child)
				child = next
				continue
			}

			for i := range child.Attr {
				if strings.EqualFold(child.Attr[i].Key, "style") {
					child.Attr[i].Val = sanitizeCSS(child.Attr[i].Val)
				}
			}
		}
		extractStyleNodes(child, styles)
		child = next
	}
}

func textContent(node *xhtml.Node) string {
	var out strings.Builder
	var walk func(*xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.TextNode {
			out.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return out.String()
}

func inlineStyleBlock(styleCSS string) string {
	styleCSS = strings.TrimSpace(styleCSS)
	if styleCSS == "" {
		return ""
	}
	return "<style>\n" + styleCSS + "\n</style>"
}

func sanitizeCSS(css string) string {
	css = cssImportRegexp.ReplaceAllString(css, "")
	css = cssExprRegexp.ReplaceAllString(css, "")

	css = cssURLRegexp.ReplaceAllStringFunc(css, func(match string) string {
		start := strings.Index(match, "(")
		end := strings.LastIndex(match, ")")
		if start == -1 || end == -1 {
			return ""
		}
		val := strings.TrimSpace(match[start+1 : end])
		val = strings.Trim(val, `"'`)

		if strings.HasPrefix(val, "#") || isSafeDataImageURL(val) {
			return match
		}
		return ""
	})

	return strings.TrimSpace(css)
}

func stripUnsafeHTMLURLs(input string) string {
	root, err := xhtml.Parse(strings.NewReader("<body>" + input + "</body>"))
	if err != nil {
		return input
	}
	body := findElement(root, "body")
	if body == nil {
		return input
	}
	removeUnsafeURLAttrs(body)

	var out strings.Builder
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := xhtml.Render(&out, child); err != nil {
			return input
		}
	}
	return out.String()
}

func inlineRemoteImages(ctx context.Context, renderer *Renderer, input string) (string, error) {
	root, err := xhtml.Parse(strings.NewReader("<body>" + input + "</body>"))
	if err != nil {
		return input, nil
	}
	body := findElement(root, "body")
	if body == nil {
		return input, nil
	}

	client := http.DefaultClient
	if renderer != nil && renderer.Client != nil {
		client = renderer.Client
	}

	inlined := 0
	var walk func(*xhtml.Node) error
	walk = func(node *xhtml.Node) error {
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "img") && inlined < maxInlineImageCount {
			for i := range node.Attr {
				if !strings.EqualFold(node.Attr[i].Key, "src") {
					continue
				}
				src := strings.TrimSpace(node.Attr[i].Val)
				if src == "" || isSafeDataImageURL(src) || !isSafeRenderURL(src, true) {
					break
				}
				dataURL, err := fetchImageAsDataURL(ctx, client, src)
				if err != nil {
					return err
				}
				node.Attr[i].Val = dataURL
				inlined++
				break
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(body); err != nil {
		return "", err
	}

	var out strings.Builder
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := xhtml.Render(&out, child); err != nil {
			return input, nil
		}
	}
	return out.String(), nil
}

func fetchImageAsDataURL(ctx context.Context, client *http.Client, raw string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "x3-htmlrender/1.0")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/png,image/jpeg,image/gif,image/*;q=0.8,*/*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("image fetch failed: HTTP %d", resp.StatusCode)
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if !isSupportedInlineImageType(mediaType) {
		return "", fmt.Errorf("unsupported image content type %q", mediaType)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxInlineImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("image response was empty")
	}
	if len(data) > maxInlineImageBytes {
		return "", fmt.Errorf("image exceeds %d byte inline limit", maxInlineImageBytes)
	}

	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func isSupportedInlineImageType(mediaType string) bool {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp", "image/avif":
		return true
	default:
		return false
	}
}

func removeUnsafeURLAttrs(node *xhtml.Node) {
	if node.Type == xhtml.ElementNode {
		attrs := node.Attr[:0]
		for _, attr := range node.Attr {
			if isURLAttr(attr.Key) && !isSafeRenderURL(attr.Val, strings.EqualFold(attr.Key, "src")) {
				continue
			}
			attrs = append(attrs, attr)
		}
		node.Attr = attrs
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		removeUnsafeURLAttrs(child)
	}
}

func isURLAttr(key string) bool {
	switch strings.ToLower(key) {
	case "src", "href", "xlink:href", "poster":
		return true
	default:
		return false
	}
}

func isSafeRenderURL(raw string, allowDataImage bool) bool {
	raw = strings.TrimSpace(raw)
	if allowDataImage && isSafeDataImageURL(raw) {
		return true
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return false
	}
	if u.User != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || isLocalHostname(host) {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return isPublicIP(ip)
	}
	if looksLikeIPAddressHost(host) {
		return false
	}
	return true
}

func isSafeDataImageURL(raw string) bool {
	return safeDataImageRegexp.MatchString(strings.TrimSpace(raw))
}

func isLocalHostname(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".home") || strings.HasSuffix(host, ".lan") {
		return true
	}
	return false
}

func looksLikeIPAddressHost(host string) bool {
	if strings.Contains(host, ":") {
		return true
	}
	labels := strings.Split(host, ".")
	allNumeric := true
	anyNumeric := false
	for _, label := range labels {
		if label == "" {
			continue
		}
		if strings.HasPrefix(label, "0x") {
			return true
		}
		for _, r := range label {
			if r < '0' || r > '9' {
				allNumeric = false
				break
			}
		}
		if allNumeric {
			anyNumeric = true
		}
	}
	return allNumeric && anyNumeric
}

func isPublicIP(ip netip.Addr) bool {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	for _, prefix := range blockedIPPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func mustParsePrefixes(raw ...string) []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}

func svgElements() []string {
	return []string{
		"svg", "defs", "g", "path", "rect", "circle", "ellipse", "line", "polyline",
		"polygon", "text", "tspan", "clipPath", "clippath", "mask", "pattern", "marker",
		"symbol", "linearGradient", "lineargradient", "radialGradient",
		"radialgradient", "stop", "filter", "feBlend", "feblend", "feColorMatrix",
		"fecolormatrix", "feComponentTransfer", "fecomponenttransfer", "feComposite",
		"fecomposite", "feDisplacementMap", "fedisplacementmap", "feDropShadow",
		"fedropshadow", "feFlood", "feflood", "feGaussianBlur", "fegaussianblur",
		"feMerge", "femerge", "feMergeNode", "femergenode",
		"feMorphology", "femorphology", "feOffset", "feoffset", "feTurbulence",
		"feturbulence",
	}
}
