package htmlrender

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
	xhtml "golang.org/x/net/html"
)

const (
	DefaultWidth  = 900
	DefaultHeight = 700
)

var (
	htmlTagRegexp      = regexp.MustCompile(`(?is)<html\b[^>]*>(.*?)</html>`)
	htmlFenceRegexp    = regexp.MustCompile("(?is)```(?:html)?\\s*(.*?)(?:```|\\z)")
	strayFenceRegexp   = regexp.MustCompile("(?m)^\\s*```\\s*$")
	blankLineRegexp    = regexp.MustCompile(`\n{3,}`)
	cssImportRegexp    = regexp.MustCompile(`(?is)@import[^;]+;?`)
	cssURLRegexp       = regexp.MustCompile(`(?is)url\s*\([^)]*\)`)
	safeImageSrcRegexp = regexp.MustCompile(`(?is)^(https://|data:image/(?:png|gif|jpeg|jpg|webp|avif);base64,)`)
	svgIDRefRegexp     = regexp.MustCompile(`(?i)^url\(\s*#[a-z][a-z0-9_-]*\s*\)$`)
	svgNameRegexp      = regexp.MustCompile(`(?i)^[a-z][a-z0-9_-]{0,80}$`)
	svgNumberRegexp    = regexp.MustCompile(`(?i)^-?(?:\d+|\d*\.\d+)(?:e-?\d+)?$`)
	svgLengthRegexp    = regexp.MustCompile(`(?i)^-?(?:\d+|\d*\.\d+)(?:e-?\d+)?(?:px|em|rem|%|vh|vw)?$`)
	svgListRegexp      = regexp.MustCompile(`(?i)^[a-z0-9#%.,:;() _+\-/]+$`)
	htmlPolicy         = newHTMLPolicy()
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

	replace := func(re *regexp.Regexp, current string) string {
		count := 0
		return re.ReplaceAllStringFunc(current, func(match string) string {
			if len(blocks) >= limit {
				return match
			}
			sub := re.FindStringSubmatch(match)
			if len(sub) != 2 || strings.TrimSpace(sub[1]) == "" {
				return match
			}
			count++
			blocks = append(blocks, Block{HTML: cleanExtractedHTML(sub[1])})
			changed = true
			return "\n"
		})
	}

	display = replace(htmlTagRegexp, display)
	display = replace(htmlFenceRegexp, display)
	display = strings.TrimSpace(display)
	display = blankLineRegexp.ReplaceAllString(display, "\n\n")
	return display, blocks, changed
}

func cleanExtractedHTML(s string) string {
	s = strings.TrimSpace(strings.Trim(s, "\u200b\ufeff"))
	s = strayFenceRegexp.ReplaceAllString(s, "")
	s = strings.TrimSpace(strings.Trim(s, "\u200b\ufeff"))
	return s
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
		return data, nil
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
  width: fit-content;
  max-width: 900px;
  padding: 24px;
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
</head>
<body><main class="mes_text x3-st-render">` + bodyHTML + `</main></body>
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

func padRect(rect, bounds image.Rectangle, padding int) image.Rectangle {
	rect.Min.X = max(rect.Min.X-padding, bounds.Min.X)
	rect.Min.Y = max(rect.Min.Y-padding, bounds.Min.Y)
	rect.Max.X = min(rect.Max.X+padding, bounds.Max.X)
	rect.Max.Y = min(rect.Max.Y+padding, bounds.Max.Y)
	return rect
}

func Sanitize(input string) (string, error) {
	body, styleCSS := splitStyleBlocks(input)
	body = strings.TrimSpace(htmlPolicy.Sanitize(body))
	if styleCSS == "" {
		return body, nil
	}
	return strings.TrimSpace(body + "\n" + inlineStyleBlock(styleCSS)), nil
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
	p.AllowStyles(allowedCSSProperties()...).MatchingHandler(safeCSSValue).Globally()
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
	root, err := xhtml.Parse(strings.NewReader("<!doctype html><html><body>" + input + "</body></html>"))
	if err != nil {
		return input, ""
	}
	body := findElement(root, "body")
	if body == nil {
		return input, ""
	}
	var styles []string
	extractStyleNodes(body, &styles)

	var out strings.Builder
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		if err := xhtml.Render(&out, child); err != nil {
			return input, strings.TrimSpace(strings.Join(styles, "\n"))
		}
	}
	return strings.TrimSpace(out.String()), strings.TrimSpace(strings.Join(styles, "\n"))
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
		if child.Type == xhtml.ElementNode && strings.EqualFold(child.Data, "style") {
			if css := sanitizeCSS(textContent(child)); css != "" {
				*styles = append(*styles, css)
			}
			node.RemoveChild(child)
			child = next
			continue
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
	css = cssURLRegexp.ReplaceAllString(css, "")
	css = strings.ReplaceAll(css, "expression(", "")
	css = strings.ReplaceAll(css, "javascript:", "")
	return strings.TrimSpace(css)
}

func safeCSSValue(value string) bool {
	value = strings.ToLower(value)
	return value == sanitizeCSS(value) &&
		!strings.Contains(value, "<") &&
		!strings.Contains(value, ">") &&
		!strings.Contains(value, "{") &&
		!strings.Contains(value, "}") &&
		len(value) <= 512
}

func allowedCSSProperties() []string {
	return []string{
		"align-content", "align-items", "align-self", "background", "background-color",
		"border", "border-bottom", "border-collapse", "border-color", "border-left",
		"border-radius", "border-right", "border-spacing", "border-style", "border-top",
		"border-width", "box-shadow", "box-sizing", "caption-side", "color", "column-gap",
		"display", "filter", "flex", "flex-basis", "flex-direction", "flex-flow", "flex-grow",
		"flex-shrink", "flex-wrap", "font", "font-family", "font-size", "font-style",
		"font-weight", "gap", "grid", "grid-area", "grid-auto-columns", "grid-auto-flow",
		"grid-auto-rows", "grid-column", "grid-row", "grid-template", "grid-template-areas",
		"grid-template-columns", "grid-template-rows", "height", "justify-content",
		"justify-items", "justify-self", "letter-spacing", "line-height", "list-style",
		"margin", "margin-bottom", "margin-left", "margin-right", "margin-top", "max-height",
		"max-width", "min-height", "min-width", "opacity", "outline", "overflow", "padding",
		"padding-bottom", "padding-left", "padding-right", "padding-top", "position", "right",
		"text-align", "text-decoration", "text-transform", "top", "transform", "vertical-align",
		"white-space", "width", "word-break", "z-index",
	}
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
