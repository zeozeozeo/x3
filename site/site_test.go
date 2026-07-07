package site

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeIntentFallsBackToHref(t *testing.T) {
	if got := normalizeIntent("", " /hall-of-mirrors "); got != "/hall-of-mirrors" {
		t.Fatalf("normalizeIntent fallback = %q", got)
	}
}

func TestTryHandoffLockedPromotesActiveFollower(t *testing.T) {
	m := NewManager(Config{BaseURL: "https://example.com"})
	t.Cleanup(m.Close)

	now := time.Now()
	session := &Session{
		ID:            "site1",
		CreatorID:     "user1",
		Capability:    "cap",
		RootPageID:    "root",
		OwnerViewerID: "owner",
		OwnerPageID:   "root",
		OwnerSeenAt:   now.Add(-time.Minute),
		ExpiresAt:     now.Add(time.Minute),
		Pages: map[string]*Page{
			"root": {ID: "root", Children: map[string]string{}},
			"next": {ID: "next", ParentID: "root", Children: map[string]string{}},
		},
		Viewers: map[string]*Viewer{
			"owner":    {ID: "owner", LastSeenAt: now.Add(-time.Minute)},
			"follower": {ID: "follower", CurrentPageID: "next", LastSeenAt: now},
		},
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.tryHandoffLocked(session)
	m.mu.Unlock()

	if got, want := session.OwnerViewerID, "follower"; got != want {
		t.Fatalf("OwnerViewerID = %q, want %q", got, want)
	}
	if got, want := session.OwnerPageID, "next"; got != want {
		t.Fatalf("OwnerPageID = %q, want %q", got, want)
	}
}

func TestInjectBootstrapAddsRuntimeScript(t *testing.T) {
	doc := `<!doctype html><html><body><main>Hello</main></body></html>`
	out := injectBootstrap(doc, bootstrapConfig{SiteID: "site1", PageID: "page1", Token: "cap", Tree: []pageTreeNode{{ID: "page1", Label: "Root", Depth: 0}}})
	if !strings.Contains(out, "x3-site-bootstrap") {
		t.Fatalf("bootstrap marker missing from output")
	}
	if !strings.Contains(out, `endpoint("navigate")`) {
		t.Fatalf("navigate endpoint missing from bootstrap")
	}
	if !strings.Contains(out, "Read-only mode: you are watching someone else control this page.") {
		t.Fatalf("readonly toast missing from bootstrap")
	}
	if !strings.Contains(out, "You are now controlling the page.") {
		t.Fatalf("ownership toast missing from bootstrap")
	}
	if !strings.Contains(out, "x3-site-progress-spinner") {
		t.Fatalf("progress spinner missing from bootstrap")
	}
	if !strings.Contains(out, `showToast(msg.error || "Failed to generate the page.", "error")`) {
		t.Fatalf("leveled error toast missing from bootstrap")
	}
	if !strings.Contains(out, "window.alert = function () {}") {
		t.Fatalf("alert override missing from bootstrap")
	}
	if !strings.Contains(out, "Generated Pages") {
		t.Fatalf("tree menu missing from bootstrap")
	}
	if !strings.Contains(out, "iconSVG(level)") {
		t.Fatalf("toast svg icon renderer missing from bootstrap")
	}
	if !strings.Contains(out, "Search pages or history") {
		t.Fatalf("search box missing from bootstrap")
	}
	if strings.Contains(out, ">Tree</span>") {
		t.Fatalf("toggle button still contains tree text")
	}
	if !strings.Contains(out, `treeSearch.addEventListener("input"`) {
		t.Fatalf("search filtering missing from bootstrap")
	}
	if strings.Contains(out, "viewer_id: viewerId") || strings.Contains(out, "&viewer=") {
		t.Fatalf("bootstrap still exposes client-controlled viewer identity")
	}
}

func TestBuildSitePromptIncludesAdditionalContext(t *testing.T) {
	prompt := buildSitePrompt(
		"haunted library",
		[]string{"Never use bright colors.", "Keep everything diegetic."},
		"",
		nil,
		"",
		false,
	)
	if !strings.Contains(prompt, "Additionally, you must follow these user-provided context instructions:") {
		t.Fatalf("prompt missing additional context heading")
	}
	if !strings.Contains(prompt, "1. Never use bright colors.") || !strings.Contains(prompt, "2. Keep everything diegetic.") {
		t.Fatalf("prompt missing context items: %s", prompt)
	}
}

func TestExtractHTMLDocumentWrapsFragment(t *testing.T) {
	out := extractHTMLDocument(`<main>Hello</main>`)
	if !strings.Contains(strings.ToLower(out), "<html") {
		t.Fatalf("normalized document missing html tag: %s", out)
	}
	if !strings.Contains(out, "<main>Hello</main>") {
		t.Fatalf("normalized document missing original content: %s", out)
	}
}

func TestHandleSiteServesPageForSiteIDAndPageIDPath(t *testing.T) {
	m := NewManager(Config{BaseURL: "http://127.0.0.1:6743"})
	t.Cleanup(m.Close)

	session := &Session{
		ID:         "site1",
		CreatorID:  "user1",
		Capability: "cap1",
		RootPageID: "page1",
		RootHTML:   "<!doctype html><html><body><main>Hello</main></body></html>",
		ExpiresAt:  time.Now().Add(time.Minute),
		Pages: map[string]*Page{
			"page1": {ID: "page1", HTML: "<!doctype html><html><body><main>Hello</main></body></html>", Children: map[string]string{}},
		},
		Viewers: map[string]*Viewer{},
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/site/site1/page1?t=cap1", nil)
	rr := httptest.NewRecorder()
	m.handleSite(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "x3-site-bootstrap") {
		t.Fatalf("response body missing bootstrap runtime")
	}
	if cookies := rr.Result().Cookies(); len(cookies) == 0 || cookies[0].Name != viewerCookieName {
		t.Fatalf("viewer cookie missing from response: %+v", cookies)
	}
	if got := rr.Header().Get("X-Robots-Tag"); !strings.Contains(got, "noindex") {
		t.Fatalf("missing noindex header: %q", got)
	}
}

func TestHandleSiteDoesNotRevealPrivateSiteExistence(t *testing.T) {
	m := NewManager(Config{BaseURL: "http://127.0.0.1:6743"})
	t.Cleanup(m.Close)

	session := &Session{
		ID:         "site1",
		CreatorID:  "user1",
		Capability: "cap1",
		RootPageID: "page1",
		RootHTML:   "<!doctype html><html><body><main>Hello</main></body></html>",
		ExpiresAt:  time.Now().Add(time.Minute),
		Pages: map[string]*Page{
			"page1": {ID: "page1", HTML: "<!doctype html><html><body><main>Hello</main></body></html>", Children: map[string]string{}},
		},
		Viewers: map[string]*Viewer{},
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/site/site1/page1?t=wrong", nil)
	rr := httptest.NewRecorder()
	m.handleSite(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
	if body := rr.Body.String(); !strings.Contains(body, "This private site is unavailable.") {
		t.Fatalf("unexpected body: %s", body)
	}
	if strings.Contains(strings.ToLower(rr.Body.String()), "invalid capability") || strings.Contains(strings.ToLower(rr.Body.String()), "site not found") {
		t.Fatalf("response leaked internal state: %s", rr.Body.String())
	}
}

func TestHandleNavigateRequiresSameOriginAndViewerCookie(t *testing.T) {
	m := NewManager(Config{BaseURL: "https://example.com"})
	t.Cleanup(m.Close)

	session := &Session{
		ID:          "site1",
		CreatorID:   "user1",
		Capability:  "cap1",
		RootPageID:  "page1",
		RootHTML:    "<!doctype html><html><body><main>Hello</main></body></html>",
		OwnerPageID: "page1",
		ExpiresAt:   time.Now().Add(time.Minute),
		Pages: map[string]*Page{
			"page1": {ID: "page1", HTML: "<!doctype html><html><body><main>Hello</main></body></html>", Children: map[string]string{}},
		},
		Viewers: map[string]*Viewer{},
	}

	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/site/site1/navigate?t=cap1", bytes.NewBufferString(`{"page_id":"page1","intent":"next"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	m.handleSite(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status without origin = %d, want %d", rr.Code, http.StatusForbidden)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/site/site1/page1?t=cap1", nil)
	pageReq.Header.Set("Host", "example.com")
	pageRR := httptest.NewRecorder()
	m.handleSite(pageRR, pageReq)

	var viewerCookie *http.Cookie
	for _, cookie := range pageRR.Result().Cookies() {
		if cookie.Name == viewerCookieName {
			viewerCookie = cookie
			break
		}
	}
	if viewerCookie == nil {
		t.Fatal("viewer cookie was not issued")
	}

	m.mu.Lock()
	var issuedViewer *Viewer
	for _, viewer := range session.Viewers {
		issuedViewer = viewer
		break
	}
	if issuedViewer == nil {
		m.mu.Unlock()
		t.Fatal("viewer was not registered in session")
	}
	session.OwnerViewerID = issuedViewer.ID
	session.OwnerPageID = "page1"
	session.Pages["page1"].Children["next"] = "page2"
	session.Pages["page2"] = &Page{ID: "page2", ParentID: "page1", Intent: "next", Children: map[string]string{}}
	m.mu.Unlock()

	originReq := httptest.NewRequest(http.MethodPost, "/site/site1/navigate?t=cap1", bytes.NewBufferString(`{"page_id":"page1","intent":"next"}`))
	originReq.Header.Set("Content-Type", "application/json")
	originReq.Header.Set("Origin", "https://example.com")
	originReq.Host = "example.com"
	originReq.AddCookie(viewerCookie)
	originRR := httptest.NewRecorder()
	m.handleSite(originRR, originReq)

	if originRR.Code != http.StatusOK {
		t.Fatalf("status with origin+cookie = %d, want %d; body=%s", originRR.Code, http.StatusOK, originRR.Body.String())
	}
}
