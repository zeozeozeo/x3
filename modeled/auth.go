package modeled

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBindAddr    = "127.0.0.1:6741"
	defaultUsername    = "admin"
	defaultSessionTTL  = 7 * 24 * time.Hour
	sessionCookieName  = "x3_admin_session"
	loginAttemptLimit  = 10
	loginAttemptWindow = 15 * time.Minute
)

var loginTemplate = template.Must(template.ParseFS(templateFiles, "templates/login.html"))

// Config controls the model editor HTTP server and its admin authentication.
//
// Password empty disables authentication entirely (only meant for local
// development). SecretPath, when set, hides the whole interface behind an
// unguessable URL prefix such as "/x3-8f2c1a9d".
type Config struct {
	BindAddr      string
	SecretPath    string
	Username      string
	Password      string
	SessionSecret []byte
	SessionTTL    time.Duration
}

// ConfigFromEnv reads the X3_MODELED_* environment variables.
func ConfigFromEnv() Config {
	cfg := Config{
		BindAddr:   strings.TrimSpace(os.Getenv("X3_MODELED_BIND_ADDR")),
		SecretPath: normalizeSecretPath(os.Getenv("X3_MODELED_SECRET_PATH")),
		Username:   strings.TrimSpace(os.Getenv("X3_MODELED_USER")),
		Password:   os.Getenv("X3_MODELED_PASSWORD"),
		SessionTTL: defaultSessionTTL,
	}
	if cfg.BindAddr == "" {
		cfg.BindAddr = defaultBindAddr
	}
	if cfg.Username == "" {
		cfg.Username = defaultUsername
	}
	if raw := strings.TrimSpace(os.Getenv("X3_MODELED_SESSION_SECRET")); raw != "" {
		cfg.SessionSecret = []byte(raw)
	} else {
		cfg.SessionSecret = randomBytes(32)
	}
	if raw := strings.TrimSpace(os.Getenv("X3_MODELED_SESSION_TTL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.SessionTTL = d
		} else {
			slog.Warn("invalid X3_MODELED_SESSION_TTL, using default", "value", raw, "default", defaultSessionTTL)
		}
	}
	return cfg
}

// normalizeSecretPath turns "" or "/" into "" (serve at the root) and otherwise
// returns a clean absolute path with no trailing slash.
func normalizeSecretPath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	if p == "/" || strings.ContainsAny(p, "?#") {
		return ""
	}
	return p
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// appPath is the URL path of the editor root, e.g. "/x3-secret/".
func (s *Server) appPath() string {
	return s.cfg.SecretPath + "/"
}

// loginPath is the URL path of the login page, e.g. "/x3-secret/login".
func (s *Server) loginPath() string {
	return s.cfg.SecretPath + "/login"
}

// cookiePath scopes the session cookie to the secret prefix when one is set.
func (s *Server) cookiePath() string {
	if s.cfg.SecretPath == "" {
		return "/"
	}
	return s.cfg.SecretPath + "/"
}

// URL returns the local address the editor is reachable at, for logging.
func (s *Server) URL() string {
	host := s.cfg.BindAddr
	if strings.HasPrefix(host, "0.0.0.0:") {
		host = "127.0.0.1:" + strings.TrimPrefix(host, "0.0.0.0:")
	}
	return "http://" + host + s.appPath()
}

// secretPathHandler hides the wrapped handler behind an unguessable prefix:
// only requests under the prefix are served, and the prefix is stripped before
// the request reaches the editor. Everything else is a 404.
func secretPathHandler(prefix string, next http.Handler) http.Handler {
	stripped := http.StripPrefix(prefix, next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == prefix:
			http.Redirect(w, r, prefix+"/", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, prefix+"/"):
			w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
			w.Header().Set("Cache-Control", "no-store")
			stripped.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// protect requires a valid admin session for every route except the login page.
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.Password == "" {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/login" || s.authenticated(r) {
			next.ServeHTTP(w, r)
			return
		}
		if wantsHTML(r) {
			http.Redirect(w, r, s.loginPath(), http.StatusFound)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func wantsHTML(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

func (s *Server) authenticated(r *http.Request) bool {
	if s.cfg.Password == "" {
		return true
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	return s.validSessionToken(cookie.Value)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Password == "" {
		http.Redirect(w, r, s.appPath(), http.StatusFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if s.authenticated(r) {
			http.Redirect(w, r, s.appPath(), http.StatusFound)
			return
		}
		s.renderLogin(w, http.StatusOK, "", s.cfg.Username)
	case http.MethodPost:
		ip := clientIP(r)
		if !s.limiter.allow(ip) {
			s.renderLogin(w, http.StatusTooManyRequests, "Too many attempts. Try again later.", s.cfg.Username)
			return
		}
		if err := r.ParseForm(); err != nil {
			s.renderLogin(w, http.StatusBadRequest, "Invalid form submission.", s.cfg.Username)
			return
		}
		username := r.PostFormValue("username")
		password := r.PostFormValue("password")
		if !constantTimeEqual(username, s.cfg.Username) || !constantTimeEqual(password, s.cfg.Password) {
			slog.Warn("model editor login failed", "ip", ip, "user", username)
			s.renderLogin(w, http.StatusUnauthorized, "Incorrect username or password.", username)
			return
		}
		s.limiter.reset(ip)
		s.setSessionCookie(w, r)
		http.Redirect(w, r, s.appPath(), http.StatusFound)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     s.cookiePath(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	target := s.loginPath()
	if s.cfg.Password == "" {
		target = s.appPath()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) renderLogin(w http.ResponseWriter, status int, errMsg, username string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet")
	w.WriteHeader(status)
	if err := loginTemplate.Execute(w, loginPageData{Error: errMsg, Username: username}); err != nil {
		slog.Error("failed to render login page", "err", err)
	}
}

type loginPageData struct {
	Error    string
	Username string
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	expiry := time.Now().Add(s.cfg.SessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.newSessionToken(expiry),
		Path:     s.cookiePath(),
		Expires:  expiry,
		MaxAge:   int(s.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

// newSessionToken returns a stateless, tamper-proof token of the form
// "<unix-expiry>.<nonce>.<hmac>". Nothing is stored server side.
func (s *Server) newSessionToken(expiry time.Time) string {
	payload := strconv.FormatInt(expiry.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(randomBytes(16))
	return payload + "." + s.sign(payload)
}

func (s *Server) sign(payload string) string {
	mac := hmac.New(sha256.New, s.cfg.SessionSecret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) validSessionToken(token string) bool {
	idx := strings.LastIndexByte(token, '.')
	if idx <= 0 {
		return false
	}
	payload, mac := token[:idx], token[idx+1:]
	if subtle.ConstantTimeCompare([]byte(mac), []byte(s.sign(payload))) != 1 {
		return false
	}
	dot := strings.IndexByte(payload, '.')
	if dot <= 0 {
		return false
	}
	exp, err := strconv.ParseInt(payload[:dot], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < exp
}

// constantTimeEqual hashes both inputs first so comparisons do not leak length.
func constantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(ha[:], hb[:]) == 1
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if idx := strings.IndexByte(fwd, ','); idx >= 0 {
			fwd = fwd[:idx]
		}
		if ip := strings.TrimSpace(fwd); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttempt
	limit    int
	window   time.Duration
}

type loginAttempt struct {
	count int
	start time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		attempts: make(map[string]*loginAttempt),
		limit:    loginAttemptLimit,
		window:   loginAttemptWindow,
	}
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	attempt := l.attempts[ip]
	if attempt == nil || now.Sub(attempt.start) > l.window {
		l.attempts[ip] = &loginAttempt{count: 1, start: now}
		return true
	}
	if attempt.count >= l.limit {
		return false
	}
	attempt.count++
	return true
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, ip)
}
