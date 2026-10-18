package modeled

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		BindAddr:      "127.0.0.1:0",
		SecretPath:    "/x3-secret",
		Username:      "admin",
		Password:      "hunter2",
		SessionSecret: []byte("test-session-secret"),
		SessionTTL:    time.Hour,
	}
}

func newTestServer(t *testing.T, cfg Config) (*Server, *httptest.Server) {
	t.Helper()
	s := newServerWithConfig(filepath.Join(t.TempDir(), "x3.db"), cfg)
	ts := httptest.NewServer(s.httpServer.Handler)
	t.Cleanup(ts.Close)
	return s, ts
}

func noRedirectClient() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func doRequest(t *testing.T, client *http.Client, method, rawURL, body string, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

func TestNormalizeSecretPath(t *testing.T) {
	cases := map[string]string{
		"":           "",
		"/":          "",
		"   ":        "",
		"x3-admin":   "/x3-admin",
		"/x3-admin":  "/x3-admin",
		"/x3-admin/": "/x3-admin",
		"/a/b/":      "/a/b",
		"/../etc":    "/etc",
		"/x3?drop":   "",
	}
	for input, want := range cases {
		if got := normalizeSecretPath(input); got != want {
			t.Errorf("normalizeSecretPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSecretPathHidesInterface(t *testing.T) {
	_, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	for _, hidden := range []string{"/", "/admin", "/x3-secre", "/static/style.css"} {
		resp := doRequest(t, client, http.MethodGet, ts.URL+hidden, "", nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", hidden, resp.StatusCode)
		}
	}

	resp := doRequest(t, client, http.MethodGet, ts.URL+"/x3-secret", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("GET /x3-secret = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/x3-secret/" {
		t.Errorf("redirect location = %q, want /x3-secret/", loc)
	}
}

func TestUnauthenticatedRequestsAreBlocked(t *testing.T) {
	_, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	resp := doRequest(t, client, http.MethodGet, ts.URL+"/x3-secret/", "", map[string]string{"Accept": "text/html"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("document request = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/x3-secret/login" {
		t.Errorf("redirect location = %q, want /x3-secret/login", loc)
	}

	resp = doRequest(t, client, http.MethodGet, ts.URL+"/x3-secret/api/models", "", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("api request = %d, want 401", resp.StatusCode)
	}
}

func TestLoginFlow(t *testing.T) {
	_, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	resp := doRequest(t, client, http.MethodPost, ts.URL+"/x3-secret/login",
		url.Values{"username": {"admin"}, "password": {"hunter2"}}.Encode(),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/x3-secret/" {
		t.Errorf("login redirect = %q, want /x3-secret/", loc)
	}
	cookie := sessionCookie(resp)
	if cookie == nil {
		t.Fatal("login did not set a session cookie")
	}
	if !cookie.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}

	authed := noRedirectClient()
	resp = doRequest(t, authed, http.MethodGet, ts.URL+"/x3-secret/", "", map[string]string{
		"Accept": "text/html",
		"Cookie": cookie.Name + "=" + cookie.Value,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authed index = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Model Configuration Editor") {
		t.Error("authed index did not return the editor page")
	}
}

func TestLoginRejectsBadCredentials(t *testing.T) {
	_, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	resp := doRequest(t, client, http.MethodPost, ts.URL+"/x3-secret/login",
		url.Values{"username": {"admin"}, "password": {"wrong"}}.Encode(),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login = %d, want 401", resp.StatusCode)
	}
	if sessionCookie(resp) != nil {
		t.Error("bad login set a session cookie")
	}
}

func TestSessionTokenValidation(t *testing.T) {
	s, _ := newTestServer(t, testConfig())

	valid := s.newSessionToken(time.Now().Add(time.Hour))
	if !s.validSessionToken(valid) {
		t.Fatal("freshly minted token should be valid")
	}
	if s.validSessionToken(valid + "x") {
		t.Error("tampered token should be rejected")
	}
	if s.validSessionToken("") || s.validSessionToken("garbage") {
		t.Error("malformed tokens should be rejected")
	}

	expired := s.newSessionToken(time.Now().Add(-time.Minute))
	if s.validSessionToken(expired) {
		t.Error("expired token should be rejected")
	}
}

func TestTamperedCookieRedirectsToLogin(t *testing.T) {
	s, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	token := s.newSessionToken(time.Now().Add(time.Hour))
	resp := doRequest(t, client, http.MethodGet, ts.URL+"/x3-secret/", "", map[string]string{
		"Accept": "text/html",
		"Cookie": sessionCookieName + "=" + token + "tampered",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("tampered cookie = %d, want 302", resp.StatusCode)
	}
}

func TestLogoutClearsSession(t *testing.T) {
	s, ts := newTestServer(t, testConfig())
	client := noRedirectClient()

	token := s.newSessionToken(time.Now().Add(time.Hour))
	resp := doRequest(t, client, http.MethodPost, ts.URL+"/x3-secret/logout", "", map[string]string{
		"Cookie": sessionCookieName + "=" + token,
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("logout = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/x3-secret/login" {
		t.Errorf("logout redirect = %q, want /x3-secret/login", loc)
	}
	cookie := sessionCookie(resp)
	if cookie == nil || cookie.MaxAge >= 0 {
		t.Error("logout did not expire the session cookie")
	}
}

func TestLoginAtRootWithoutSecretPath(t *testing.T) {
	cfg := testConfig()
	cfg.SecretPath = ""
	_, ts := newTestServer(t, cfg)
	client := noRedirectClient()

	resp := doRequest(t, client, http.MethodGet, ts.URL+"/", "", map[string]string{"Accept": "text/html"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("root document = %d, want 302", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("redirect location = %q, want /login", loc)
	}
}

func TestAuthDisabledServesInterface(t *testing.T) {
	cfg := testConfig()
	cfg.Password = ""
	_, ts := newTestServer(t, cfg)
	client := noRedirectClient()

	resp := doRequest(t, client, http.MethodGet, ts.URL+"/x3-secret/", "", map[string]string{"Accept": "text/html"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("auth-disabled index = %d, want 200", resp.StatusCode)
	}
}
