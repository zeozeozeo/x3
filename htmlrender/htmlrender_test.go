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
