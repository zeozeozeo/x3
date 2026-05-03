package htmlrender

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractHTMLBlocks(t *testing.T) {
	display, blocks, changed := Extract("hello\n```html\n<div>card</div>\n```\nbye", 3)
	if !changed {
		t.Fatal("expected extraction to change the response")
	}
	if strings.TrimSpace(display) != "hello\n\nbye" {
		t.Fatalf("unexpected display text: %q", display)
	}
	if len(blocks) != 1 || blocks[0].HTML != "<div>card</div>" {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestExtractHTMLTagBlock(t *testing.T) {
	display, blocks, changed := Extract("hello\n<html><div>card</div></html>\nbye", 3)
	if !changed {
		t.Fatal("expected extraction to change the response")
	}
	if strings.TrimSpace(display) != "hello\n\nbye" {
		t.Fatalf("unexpected display text: %q", display)
	}
	if len(blocks) != 1 || blocks[0].HTML != "<div>card</div>" {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
}

func TestExtractFenceCleansLeakedClosingFence(t *testing.T) {
	_, blocks, changed := Extract("```html\n<div>card</div>\n```\u200b", 3)
	if !changed {
		t.Fatal("expected extraction to change the response")
	}
	if len(blocks) != 1 {
		t.Fatalf("unexpected blocks: %+v", blocks)
	}
	if strings.Contains(blocks[0].HTML, "```") || strings.Contains(blocks[0].HTML, "\u200b") {
		t.Fatalf("extracted HTML still contains fence debris: %q", blocks[0].HTML)
	}
}

func TestSanitizeRemovesActiveContent(t *testing.T) {
	got, err := Sanitize(`<style>@import url("https://bad/style.css"); .x { background: url(https://bad/bg.png); color: red; }</style><div onclick="alert(1)" style="background:url(javascript:bad); color: blue"><script>alert(1)</script><img src="https://example.com/a.png" onerror="x"><img src="http://bad/a.png"><a href="javascript:alert(1)">x</a></div>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"script", "onclick", "onerror", "javascript:", "http://bad", "@import", "url("} {
		if strings.Contains(got, bad) {
			t.Fatalf("sanitized HTML still contains %q: %s", bad, got)
		}
	}
	if !strings.Contains(got, `src="https://example.com/a.png"`) {
		t.Fatalf("safe image URL was removed: %s", got)
	}
	if !strings.Contains(got, "<style>") || !strings.Contains(got, "color: red") {
		t.Fatalf("safe style block was not preserved: %s", got)
	}
}

func TestSanitizePreservesTrailingStyleBlock(t *testing.T) {
	input := `<div class="card">
  <div class="header">
    <span class="tag"> yuki</span>
    water nymph
  </div>
</div>
<style>
  .card {
    background: #fff9c4;
    border-radius: 20px;
    padding: 20px;
    max-width: 260px;
    box-shadow: 0 4px 15px rgba(255, 200, 0, 0.4);
    font-family: 'Segoe UI', sans-serif;
    color: #6b4f00;
  }
  .header {
    display: flex;
    justify-content: space-between;
  }
  .tag {
    background: #f9d835;
    border-radius: 30px;
  }
</style>`
	got, err := Sanitize(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`class="card"`, "<style>", ".card", "box-shadow", "justify-content: space-between"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized HTML missing %q:\n%s", want, got)
		}
	}
}

func TestSanitizePreservesInlineStyledCard(t *testing.T) {
	input := `<div style="background: #2a2a3e; border: 2px solid #e6c300; border-radius: 16px; padding: 20px; max-width: 350px; font-family: 'Segoe UI', sans-serif; color: #f0e68c; box-shadow: 0 0 20px rgba(230, 195, 0, 0.3);">
  <div style="font-size: 18px; font-weight: bold; border-bottom: 1px solid #e6c300; padding-bottom: 8px; margin-bottom: 12px;"> Yurika</div>
  <div style="font-size: 14px; line-height: 1.6;">
    <b>Age:</b> 21<br>
    <b>Role:</b> asdasdasr<br>
    <b>Traits:</b> dasdasdas<br>
    <b>Bio:</b> asdsadas
  </div>
</div>`
	got, err := Sanitize(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Yurika", "background: #2a2a3e", "box-shadow", "Age:", "line-height: 1.6"} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized HTML missing %q:\n%s", want, got)
		}
	}
}

func TestSanitizeAllowsSafeSVGFilters(t *testing.T) {
	input := `<svg viewBox="0 0 120 80" width="120" height="80">
  <defs>
    <filter id="softGlow" x="-20%" y="-20%" width="140%" height="140%">
      <feGaussianBlur stdDeviation="3" result="blur"/>
      <feDropShadow dx="0" dy="2" stdDeviation="2" flood-color="#f9d835" flood-opacity="0.7"/>
      <feMerge><feMergeNode in="blur"/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
  </defs>
  <rect x="10" y="10" width="100" height="60" rx="12" fill="#2a2a3e" filter="url(#softGlow)"/>
</svg>`
	got, err := Sanitize(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<svg", "<filter", "fegaussianblur", "fedropshadow", `filter="url(#softGlow)"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized SVG missing %q:\n%s", want, got)
		}
	}
}

func TestSanitizeRejectsUnsafeSVGHelpers(t *testing.T) {
	got, err := Sanitize(`<svg><foreignObject><script>alert(1)</script></foreignObject><use href="https://example.com/x.svg#id"/></svg>`)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"foreignObject", "script", "<use", "href="} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe SVG survived sanitization: %s", got)
		}
	}
}

func TestSanitizeAllowsSVGClipMaskPatternMarker(t *testing.T) {
	input := `<svg viewBox="0 0 160 100" width="160" height="100">
  <defs>
    <clipPath id="roundClip"><rect x="10" y="10" width="90" height="60" rx="12"/></clipPath>
    <mask id="fadeMask" maskUnits="userSpaceOnUse"><rect width="160" height="100" fill="white"/></mask>
    <pattern id="dots" patternUnits="userSpaceOnUse" width="8" height="8" patternTransform="rotate(15)">
      <circle cx="2" cy="2" r="1.5" fill="#f9d835"/>
    </pattern>
    <marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto">
      <path d="M0,0 L8,4 L0,8 Z" fill="#f9d835"/>
    </marker>
  </defs>
  <rect x="10" y="10" width="90" height="60" fill="url(#dots)" clip-path="url(#roundClip)" mask="url(#fadeMask)"/>
  <line x1="20" y1="85" x2="140" y2="85" stroke="#f9d835" marker-end="url(#arrow)"/>
</svg>`
	got, err := Sanitize(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"clippath", "<mask", "<pattern", "<marker", `fill="url(#dots)"`, `clip-path="url(#roundClip)"`, `marker-end="url(#arrow)"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("sanitized SVG missing %q:\n%s", want, got)
		}
	}
}

func TestRendererPostsGotenbergMultipart(t *testing.T) {
	var sawIndex, sawFormat bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/chromium/screenshot/html" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			t.Fatalf("ParseMultipartForm failed: %v", err)
		}
		sawFormat = r.FormValue("format") == "png" && r.FormValue("omitBackground") == "true"
		files := r.MultipartForm.File["files"]
		if len(files) == 1 && files[0].Filename == "index.html" {
			file, err := files[0].Open()
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			body, _ := io.ReadAll(file)
			sawIndex = strings.Contains(string(body), "<strong>ok</strong>")
		}
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(testPNG(t))
	}))
	defer server.Close()

	renderer := &Renderer{BaseURL: server.URL, Client: server.Client()}
	data, err := renderer.Render(context.Background(), "<strong>ok</strong>")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected response image data")
	}
	if !sawIndex || !sawFormat {
		t.Fatalf("multipart request missing index/form fields: sawIndex=%v sawFormat=%v", sawIndex, sawFormat)
	}
}

func TestCropTransparentPNG(t *testing.T) {
	data := testPNG(t)
	cropped, err := cropTransparentPNG(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(cropped))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() >= 32 || img.Bounds().Dy() >= 32 {
		t.Fatalf("image was not cropped: %v", img.Bounds())
	}
}

func TestCropTransparentPNGRejectsFullyTransparentImage(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	_, err := cropTransparentPNG(buf.Bytes())
	if err == nil || !strings.Contains(err.Error(), "fully transparent") {
		t.Fatalf("expected fully transparent error, got %v", err)
	}
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 12; y < 20; y++ {
		for x := 10; x < 18; x++ {
			img.Set(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
