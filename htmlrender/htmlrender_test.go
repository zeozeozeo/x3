package htmlrender

import (
	"context"
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
		sawFormat = r.FormValue("format") == "webp"
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
		w.Header().Set("Content-Type", "image/webp")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("WEBP"))
	}))
	defer server.Close()

	renderer := &Renderer{BaseURL: server.URL, Client: server.Client()}
	data, err := renderer.Render(context.Background(), "<strong>ok</strong>")
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if string(data) != "WEBP" {
		t.Fatalf("unexpected response data: %q", data)
	}
	if !sawIndex || !sawFormat {
		t.Fatalf("multipart request missing index/form fields: sawIndex=%v sawFormat=%v", sawIndex, sawFormat)
	}
}
