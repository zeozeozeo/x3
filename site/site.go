package site

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/tdewolff/minify/v2"
	minifycss "github.com/tdewolff/minify/v2/css"
	minifyhtml "github.com/tdewolff/minify/v2/html"
	minifyjs "github.com/tdewolff/minify/v2/js"
	"golang.org/x/net/html"

	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/htmlrender"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

const (
	defaultBindAddr             = "127.0.0.1:6743"
	defaultRetentionTTL         = 7 * 24 * time.Hour
	defaultOwnerTimeout         = 15 * time.Second
	defaultSiteWindow           = time.Hour
	defaultSiteCreationsPerUser = 10
	defaultPageWindow           = 30 * time.Minute
	defaultPagesPerWindow       = 10
	defaultLlmRequestTimout     = 45 * time.Second
	defaultMaxJSONBodyBytes     = 8 << 10
	viewerCookieName            = "x3_site_viewer"
	rootSharedCSSStyleID        = "x3-site-root-css"
)

var (
	errSiteUnavailable = errors.New("private site unavailable")
	errSiteExpired     = errors.New("site expired")
	errViewerSession   = errors.New("viewer session is invalid; reload the page")
	htmlMIME           = regexp.MustCompile(`^text/html(?:;|$)`)
	jsMIME             = regexp.MustCompile(`^(application|text)/(x-)?(java|ecma)script(?:;|$)`)
	siteMinifier       = newSiteMinifier()
)

type Config struct {
	BaseURL              string
	BindAddr             string
	RetentionTTL         time.Duration
	OwnerTimeout         time.Duration
	SiteWindow           time.Duration
	SiteCreationsPerUser int
	PageWindow           time.Duration
	PagesPerWindow       int
}

func ConfigFromEnv() Config {
	bindAddr := strings.TrimSpace(os.Getenv("X3_SITE_BIND_ADDR"))
	if bindAddr == "" {
		bindAddr = defaultBindAddr
	}
	return Config{
		BaseURL:              strings.TrimRight(strings.TrimSpace(os.Getenv("X3_SITE_BASE_URL")), "/"),
		BindAddr:             bindAddr,
		RetentionTTL:         defaultRetentionTTL,
		OwnerTimeout:         defaultOwnerTimeout,
		SiteWindow:           defaultSiteWindow,
		SiteCreationsPerUser: defaultSiteCreationsPerUser,
		PageWindow:           defaultPageWindow,
		PagesPerWindow:       defaultPagesPerWindow,
	}
}

type Server struct {
	manager    *Manager
	httpServer *http.Server
}

func NewServerFromEnv() *Server {
	cfg := ConfigFromEnv()
	manager := NewManager(cfg)
	mux := http.NewServeMux()
	manager.RegisterRoutes(mux)
	return &Server{
		manager: manager,
		httpServer: &http.Server{
			Addr:    cfg.BindAddr,
			Handler: mux,
		},
	}
}

func (s *Server) Start() error {
	if s == nil || s.httpServer == nil {
		return fmt.Errorf("site server is not configured")
	}
	slog.Info("starting site server", "addr", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	s.manager.Close()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) Manager() *Manager {
	if s == nil {
		return nil
	}
	return s.manager
}

type Manager struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]*Session
	upgrader websocket.Upgrader
	closed   chan struct{}
	genStats generationStats
}

type Session struct {
	ID                 string
	Theme              string
	CreatorID          string
	DiscordMessageID   string
	AdditionalContext  []string
	PersonaSystem      string
	Capability         string
	RootPageID         string
	RootHTML           string
	RootTitle          string
	RootStructure      string
	RootLinkInventory  []string
	RootSharedCSS      string
	CreatedAt          time.Time
	ExpiresAt          time.Time
	OwnerViewerID      string
	OwnerPageID        string
	OwnerSeenAt        time.Time
	GeneratedPageTimes []time.Time
	Pages              map[string]*Page
	Viewers            map[string]*Viewer `json:"-"`
}

type Page struct {
	ID       string
	ParentID string
	Intent   string
	HTML     string
	History  []string
	Children map[string]string
}

type Viewer struct {
	ID            string
	Secret        string
	CurrentPageID string
	LastSeenAt    time.Time
	Conn          *websocket.Conn `json:"-"`
	WriteMu       sync.Mutex      `json:"-"`
}

type CreateResult struct {
	SiteID    string
	PageID    string
	URL       string
	ExpiresAt time.Time
}

type CreateOptions struct {
	CreatorID         string
	Theme             string
	AdditionalContext []string
	PersonaSystem     string
}

type generationStats struct {
	AvgTokensPerSecond float64
	AvgDurationMs      float64
	Samples            int
}

type generationMetrics struct {
	ResponseTokens  int
	Duration        time.Duration
	TokensPerSecond float64
}

type generationRequest struct {
	Theme             string
	AdditionalContext []string
	PersonaSystem     string
	RootTitle         string
	RootStructure     string
	RootLinkInventory []string
	RootSharedCSS     string
	History           []string
	ClickedIntent     string
	FormValues        map[string]any
}

type promptAssets struct {
	Title         string
	Structure     string
	LinkInventory []string
	SharedCSS     string
}

func newSiteMinifier() *minify.M {
	m := minify.New()
	m.Add("text/css", &minifycss.Minifier{})
	m.AddRegexp(jsMIME, &minifyjs.Minifier{})
	m.AddRegexp(htmlMIME, &minifyhtml.Minifier{KeepDocumentTags: true})
	return m
}

func NewManager(cfg Config) *Manager {
	if cfg.RetentionTTL <= 0 {
		cfg.RetentionTTL = defaultRetentionTTL
	}
	if cfg.OwnerTimeout <= 0 {
		cfg.OwnerTimeout = defaultOwnerTimeout
	}
	if cfg.SiteWindow <= 0 {
		cfg.SiteWindow = defaultSiteWindow
	}
	if cfg.SiteCreationsPerUser <= 0 {
		cfg.SiteCreationsPerUser = defaultSiteCreationsPerUser
	}
	if cfg.PageWindow <= 0 {
		cfg.PageWindow = defaultPageWindow
	}
	if cfg.PagesPerWindow <= 0 {
		cfg.PagesPerWindow = defaultPagesPerWindow
	}
	m := &Manager{
		cfg:      cfg,
		sessions: map[string]*Session{},
		closed:   make(chan struct{}),
	}
	m.upgrader = websocket.Upgrader{
		CheckOrigin: m.sameOriginRequest,
	}
	m.loadPersistedSessions()
	go m.gcLoop()
	return m
}

func (m *Manager) Close() {
	select {
	case <-m.closed:
		return
	default:
		close(m.closed)
	}
}

func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/site/", m.handleSite)
}

func (m *Manager) loadPersistedSessions() {
	records, err := db.ListSiteSessions()
	if err != nil {
		slog.Warn("failed to load persisted site sessions", "err", err)
		return
	}
	now := time.Now()
	for _, record := range records {
		if !record.ExpiresAt.IsZero() && now.After(record.ExpiresAt) {
			_ = db.DeleteSiteSession(record.SiteID)
			continue
		}
		var session Session
		if err := json.Unmarshal(record.Data, &session); err != nil {
			slog.Warn("failed to unmarshal persisted site session", "err", err, "site_id", record.SiteID)
			_ = db.DeleteSiteSession(record.SiteID)
			continue
		}
		if session.Pages == nil {
			session.Pages = map[string]*Page{}
		}
		session.Viewers = map[string]*Viewer{}
		session.DiscordMessageID = record.DiscordMessageID
		if !record.ExpiresAt.IsZero() {
			session.ExpiresAt = record.ExpiresAt
		}
		derivePromptAssetsIfMissing(&session)
		session.GeneratedPageTimes = pruneTimesSince(session.GeneratedPageTimes, now.Add(-m.cfg.PageWindow))
		m.sessions[session.ID] = &session
	}
	_ = db.DeleteExpiredSiteSessions(now)
}

func (m *Manager) CreateSite(ctx context.Context, opts CreateOptions) (*CreateResult, error) {
	theme := strings.TrimSpace(opts.Theme)
	creatorID := strings.TrimSpace(opts.CreatorID)
	additionalContext := compactPromptItems(opts.AdditionalContext, 240, 0)
	personaSystem := strings.TrimSpace(opts.PersonaSystem)
	if theme == "" {
		return nil, fmt.Errorf("theme is required")
	}
	if creatorID == "" {
		return nil, fmt.Errorf("creator id is required")
	}
	if strings.TrimSpace(m.cfg.BaseURL) == "" {
		return nil, fmt.Errorf("X3_SITE_BASE_URL is not configured")
	}
	if len(model.SiteModels) == 0 {
		return nil, fmt.Errorf("site_models is empty")
	}

	now := time.Now()
	m.mu.Lock()
	m.pruneLocked(now)
	if m.countRecentSiteCreationsByUserLocked(creatorID, now) >= m.cfg.SiteCreationsPerUser {
		m.mu.Unlock()
		return nil, fmt.Errorf("you can only create %d new sites per %s", m.cfg.SiteCreationsPerUser, humanWindow(m.cfg.SiteWindow))
	}
	dummyID := randHex(16)
	m.sessions[dummyID] = &Session{
		CreatorID:     creatorID,
		CreatedAt:     now,
		ExpiresAt:     now.Add(time.Minute),
		PersonaSystem: personaSystem,
	}
	m.mu.Unlock()

	rootHTML, metrics, err := m.generatePage(ctx, generationRequest{
		Theme:             theme,
		AdditionalContext: additionalContext,
		PersonaSystem:     personaSystem,
	})

	m.mu.Lock()
	delete(m.sessions, dummyID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.recordGenerationMetricsLocked(metrics)
	m.mu.Unlock()

	siteID := randHex(16)
	pageID := randHex(12)
	capability := randHex(24)
	assets := derivePromptAssets(rootHTML)
	session := &Session{
		ID:                 siteID,
		Theme:              theme,
		CreatorID:          creatorID,
		AdditionalContext:  append([]string(nil), additionalContext...),
		PersonaSystem:      personaSystem,
		Capability:         capability,
		RootPageID:         pageID,
		RootHTML:           rootHTML,
		RootTitle:          assets.Title,
		RootStructure:      assets.Structure,
		RootLinkInventory:  append([]string(nil), assets.LinkInventory...),
		RootSharedCSS:      assets.SharedCSS,
		CreatedAt:          now,
		ExpiresAt:          now.Add(m.cfg.RetentionTTL),
		OwnerPageID:        pageID,
		GeneratedPageTimes: nil,
		Pages: map[string]*Page{
			pageID: {
				ID:       pageID,
				HTML:     rootHTML,
				History:  nil,
				Children: map[string]string{},
			},
		},
		Viewers: map[string]*Viewer{},
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[siteID] = session
	if err := m.saveSessionLocked(session); err != nil {
		delete(m.sessions, siteID)
		return nil, err
	}

	return &CreateResult{
		SiteID:    siteID,
		PageID:    pageID,
		URL:       m.pageURL(session, pageID),
		ExpiresAt: session.ExpiresAt,
	}, nil
}

func (m *Manager) pageURL(s *Session, pageID string) string {
	return fmt.Sprintf("%s/site/%s/%s?t=%s", m.cfg.BaseURL, s.ID, pageID, s.Capability)
}

func (m *Manager) sitePath(siteID string) string {
	return "/site/" + siteID + "/"
}

func (m *Manager) sameOriginRequest(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	expected := strings.TrimSpace(m.cfg.BaseURL)
	if expected != "" {
		baseURL, err := url.Parse(expected)
		if err == nil && baseURL.Scheme != "" && baseURL.Host != "" {
			return strings.EqualFold(origin, baseURL.Scheme+"://"+baseURL.Host)
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(origin, scheme+"://"+r.Host)
}

func secureTokenEqual(a, b string) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, defaultMaxJSONBodyBytes)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected trailing data")
	}
	return nil
}

func setSiteHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet, noimageindex, notranslate")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
}

func setAPIHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet, noimageindex, notranslate")
}

func (m *Manager) setViewerCookie(w http.ResponseWriter, siteID, secret string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     viewerCookieName,
		Value:    secret,
		Path:     m.sitePath(siteID),
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   strings.HasPrefix(strings.ToLower(strings.TrimSpace(m.cfg.BaseURL)), "https://"),
		SameSite: http.SameSiteStrictMode,
	})
}

func (m *Manager) countRecentSiteCreationsByUserLocked(creatorID string, now time.Time) int {
	count := 0
	cutoff := now.Add(-m.cfg.SiteWindow)
	for _, session := range m.sessions {
		if session.CreatorID == creatorID && session.CreatedAt.After(cutoff) && now.Before(session.ExpiresAt) {
			count++
		}
	}
	return count
}

func (m *Manager) saveSessionLocked(session *Session) error {
	if session == nil {
		return nil
	}
	snapshot := *session
	snapshot.Viewers = nil
	data, err := json.Marshal(&snapshot)
	if err != nil {
		return err
	}
	return db.WriteSiteSession(db.SiteSessionRecord{
		SiteID:           session.ID,
		CreatorID:        session.CreatorID,
		DiscordMessageID: session.DiscordMessageID,
		ExpiresAt:        session.ExpiresAt,
		Data:             data,
	})
}

func (m *Manager) viewerBySecretLocked(session *Session, secret string) *Viewer {
	if session == nil || strings.TrimSpace(secret) == "" {
		return nil
	}
	for _, viewer := range session.Viewers {
		if secureTokenEqual(viewer.Secret, secret) {
			return viewer
		}
	}
	return nil
}

func (m *Manager) requestViewerSecret(r *http.Request) string {
	cookie, err := r.Cookie(viewerCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (m *Manager) ensureViewerForPageLocked(session *Session, w http.ResponseWriter, r *http.Request, pageID string) *Viewer {
	if session.Viewers == nil {
		session.Viewers = map[string]*Viewer{}
	}
	secret := m.requestViewerSecret(r)
	viewer := m.viewerBySecretLocked(session, secret)
	now := time.Now()
	if viewer == nil {
		viewer = &Viewer{
			ID:            randHex(8),
			Secret:        randHex(24),
			CurrentPageID: pageID,
			LastSeenAt:    now,
		}
		session.Viewers[viewer.ID] = viewer
	} else {
		viewer.LastSeenAt = now
		viewer.CurrentPageID = pageID
	}
	m.setViewerCookie(w, session.ID, viewer.Secret, session.ExpiresAt)
	return viewer
}

func (m *Manager) viewerFromRequestLocked(session *Session, r *http.Request) (*Viewer, error) {
	viewer := m.viewerBySecretLocked(session, m.requestViewerSecret(r))
	if viewer == nil {
		return nil, errViewerSession
	}
	viewer.LastSeenAt = time.Now()
	return viewer, nil
}

func (m *Manager) AttachDiscordMessage(siteID, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[siteID]
	if !ok {
		return fmt.Errorf("site not found")
	}
	session.DiscordMessageID = messageID
	return m.saveSessionLocked(session)
}

func (m *Manager) CancelSite(siteID, creatorID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[siteID]
	if !ok {
		return fmt.Errorf("site not found")
	}
	if session.CreatorID != creatorID {
		return fmt.Errorf("cannot cancel another user's site")
	}
	m.removeSessionLocked(session)
	return nil
}

func (m *Manager) handleSite(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/site/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	siteID := parts[0]
	action := parts[1]
	if len(parts) == 2 {
		switch action {
		case "navigate":
			m.handleNavigate(w, r, siteID)
			return
		case "route":
			m.handleRoute(w, r, siteID)
			return
		case "ws":
			m.handleWS(w, r, siteID)
			return
		}
	}
	if r.Method == http.MethodGet {
		m.handlePage(w, r, siteID, action)
		return
	}
	http.NotFound(w, r)
}

func (m *Manager) sessionForRequest(siteID, capability string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[siteID]
	if !ok {
		return nil, errSiteUnavailable
	}
	if !secureTokenEqual(session.Capability, capability) {
		return nil, errSiteUnavailable
	}
	if time.Now().After(session.ExpiresAt) {
		m.removeSessionLocked(session)
		return nil, errSiteExpired
	}
	return session, nil
}

func (m *Manager) handlePage(w http.ResponseWriter, r *http.Request, siteID, pageID string) {
	setSiteHeaders(w)
	session, err := m.sessionForRequest(siteID, r.URL.Query().Get("t"))
	if err != nil {
		if errors.Is(err, errSiteExpired) {
			m.writeExpiredPage(w, http.StatusGone, "This private site has expired.")
			return
		}
		m.writeNotFoundPage(w)
		return
	}
	m.mu.Lock()
	page, ok := session.Pages[pageID]
	if !ok {
		m.mu.Unlock()
		m.writeNotFoundPage(w)
		return
	}
	m.ensureViewerForPageLocked(session, w, r, pageID)
	html := injectBootstrap(page.HTML, bootstrapConfig{
		SiteID:            session.ID,
		PageID:            pageID,
		Token:             session.Capability,
		ExpiresAt:         session.ExpiresAt.UnixMilli(),
		DefaultEstimateMs: m.estimateGenerationDurationLocked(session, "", nil).Milliseconds(),
		Tree:              m.pageTreeLocked(session),
	})
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

type navigateRequest struct {
	PageID     string         `json:"page_id"`
	Intent     string         `json:"intent"`
	Href       string         `json:"href"`
	FormValues map[string]any `json:"form_values"`
}

type pageResponse struct {
	PageID string `json:"page_id"`
	URL    string `json:"url"`
}

func (m *Manager) handleNavigate(w http.ResponseWriter, r *http.Request, siteID string) {
	setAPIHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !m.sameOriginRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	session, err := m.sessionForRequest(siteID, r.URL.Query().Get("t"))
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, errSiteExpired) {
			status = http.StatusGone
		}
		http.Error(w, "site unavailable", status)
		return
	}
	var req navigateRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	viewer, err := m.viewerFromRequestLocked(session, r)
	m.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	intent := normalizeIntent(req.Intent, req.Href)
	if intent == "" {
		http.Error(w, "empty link intent", http.StatusBadRequest)
		return
	}

	pageURL, pageID, err := m.navigate(r.Context(), session, viewer.ID, req.PageID, intent, req.FormValues)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pageResponse{PageID: pageID, URL: pageURL})
}

func (m *Manager) navigate(ctx context.Context, session *Session, viewerID, fromPageID, intent string, formValues map[string]any) (string, string, error) {
	m.mu.Lock()
	if session.OwnerViewerID != "" && session.OwnerViewerID != viewerID {
		pageURL := m.pageURL(session, session.OwnerPageID)
		m.mu.Unlock()
		return pageURL, session.OwnerPageID, fmt.Errorf("only the current owner can navigate this site")
	}
	page, ok := session.Pages[fromPageID]
	if !ok {
		m.mu.Unlock()
		return "", "", fmt.Errorf("page not found")
	}
	if childID, ok := page.Children[intent]; ok {
		session.OwnerPageID = childID
		session.OwnerSeenAt = time.Now()
		viewer := m.ensureViewerLocked(session, viewerID)
		viewer.CurrentPageID = childID
		pageURL := m.pageURL(session, childID)
		_ = m.saveSessionLocked(session)
		m.broadcastStateLocked(session)
		m.mu.Unlock()
		return pageURL, childID, nil
	}
	now := time.Now()
	session.GeneratedPageTimes = pruneTimesSince(session.GeneratedPageTimes, now.Add(-m.cfg.PageWindow))
	if len(session.GeneratedPageTimes) >= m.cfg.PagesPerWindow {
		m.mu.Unlock()
		return "", "", fmt.Errorf("this site can only create %d new pages per %s", m.cfg.PagesPerWindow, humanWindow(m.cfg.PageWindow))
	}
	session.GeneratedPageTimes = append(session.GeneratedPageTimes, now)
	derivePromptAssetsIfMissing(session)
	history := append([]string(nil), page.History...)
	history = append(history, intent)
	if len(history) > 10 {
		history = history[len(history)-10:]
	}
	theme := session.Theme
	estimated := m.estimateGenerationDurationLocked(session, intent, history)
	m.broadcastLocked(session, wsServerMessage{
		Type:        "generation_start",
		PageID:      fromPageID,
		EstimatedMs: estimated.Milliseconds(),
	})
	m.mu.Unlock()

	nextHTML, metrics, err := m.generatePage(ctx, generationRequest{
		Theme:             theme,
		AdditionalContext: session.AdditionalContext,
		PersonaSystem:     session.PersonaSystem,
		RootTitle:         session.RootTitle,
		RootStructure:     session.RootStructure,
		RootLinkInventory: append([]string(nil), session.RootLinkInventory...),
		RootSharedCSS:     session.RootSharedCSS,
		History:           history,
		ClickedIntent:     intent,
		FormValues:        formValues,
	})
	if err != nil {
		m.mu.Lock()
		for i, t := range session.GeneratedPageTimes {
			if t.Equal(now) {
				session.GeneratedPageTimes = append(session.GeneratedPageTimes[:i], session.GeneratedPageTimes[i+1:]...)
				break
			}
		}
		m.broadcastLocked(session, wsServerMessage{
			Type:  "generation_error",
			Error: err.Error(),
		})
		m.mu.Unlock()
		return "", "", err
	}
	nextHTML = injectRootSharedCSS(nextHTML, session.RootSharedCSS)
	nextPageID := randHex(12)

	m.mu.Lock()
	defer m.mu.Unlock()
	page = session.Pages[fromPageID]
	if page == nil {
		return "", "", fmt.Errorf("page not found")
	}
	if childID, ok := page.Children[intent]; ok {
		for i, t := range session.GeneratedPageTimes {
			if t.Equal(now) {
				session.GeneratedPageTimes = append(session.GeneratedPageTimes[:i], session.GeneratedPageTimes[i+1:]...)
				break
			}
		}
		session.OwnerPageID = childID
		_ = m.saveSessionLocked(session)
		return m.pageURL(session, childID), childID, nil
	}
	page.Children[intent] = nextPageID
	session.Pages[nextPageID] = &Page{
		ID:       nextPageID,
		ParentID: fromPageID,
		Intent:   intent,
		HTML:     nextHTML,
		History:  history,
		Children: map[string]string{},
	}
	session.OwnerPageID = nextPageID
	session.OwnerSeenAt = time.Now()
	viewer := m.ensureViewerLocked(session, viewerID)
	viewer.CurrentPageID = nextPageID
	m.recordGenerationMetricsLocked(metrics)
	_ = m.saveSessionLocked(session)
	m.broadcastStateLocked(session)
	return m.pageURL(session, nextPageID), nextPageID, nil
}

type routeRequest struct {
	PageID string `json:"page_id"`
}

func (m *Manager) handleRoute(w http.ResponseWriter, r *http.Request, siteID string) {
	setAPIHeaders(w)
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !m.sameOriginRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	session, err := m.sessionForRequest(siteID, r.URL.Query().Get("t"))
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, errSiteExpired) {
			status = http.StatusGone
		}
		http.Error(w, "site unavailable", status)
		return
	}
	var req routeRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	viewer, err := m.viewerFromRequestLocked(session, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if session.OwnerViewerID != viewer.ID {
		http.Error(w, "only the current owner can change history", http.StatusForbidden)
		return
	}
	if _, ok := session.Pages[req.PageID]; !ok {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}
	session.OwnerPageID = req.PageID
	session.OwnerSeenAt = time.Now()
	viewer.CurrentPageID = req.PageID
	_ = m.saveSessionLocked(session)
	m.broadcastStateLocked(session)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pageResponse{PageID: req.PageID, URL: m.pageURL(session, req.PageID)})
}

func (m *Manager) handleWS(w http.ResponseWriter, r *http.Request, siteID string) {
	setAPIHeaders(w)
	if !m.sameOriginRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	session, err := m.sessionForRequest(siteID, r.URL.Query().Get("t"))
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, errSiteExpired) {
			status = http.StatusGone
		}
		http.Error(w, "site unavailable", status)
		return
	}
	pageID := strings.TrimSpace(r.URL.Query().Get("page"))
	if pageID == "" {
		pageID = session.RootPageID
	}

	m.mu.Lock()
	viewer, err := m.viewerFromRequestLocked(session, r)
	if err != nil {
		m.mu.Unlock()
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if _, ok := session.Pages[pageID]; !ok {
		pageID = session.RootPageID
	}
	m.mu.Unlock()

	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(defaultMaxJSONBodyBytes)

	m.mu.Lock()
	viewer.Conn = conn
	viewer.LastSeenAt = time.Now()
	viewer.CurrentPageID = pageID
	if session.OwnerViewerID == "" {
		session.OwnerViewerID = viewer.ID
		session.OwnerPageID = pageID
		session.OwnerSeenAt = viewer.LastSeenAt
		_ = m.saveSessionLocked(session)
	}
	m.broadcastStateLocked(session)
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		if s, ok := m.sessions[siteID]; ok {
			if v, ok := s.Viewers[viewer.ID]; ok && v.Conn == conn {
				v.Conn = nil
			}
			m.tryHandoffLocked(s)
			m.broadcastStateLocked(s)
		}
		m.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		var msg wsClientMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		m.handleWSMessage(siteID, viewer.ID, conn, &msg)
	}
}

type wsClientMessage struct {
	Type     string  `json:"type"`
	PageID   string  `json:"page_id,omitempty"`
	X        float64 `json:"x,omitempty"`
	Y        float64 `json:"y,omitempty"`
	XPct     float64 `json:"x_pct,omitempty"`
	YPct     float64 `json:"y_pct,omitempty"`
	Selector string  `json:"selector,omitempty"`
	RX       float64 `json:"rx,omitempty"`
	RY       float64 `json:"ry,omitempty"`
}

type wsServerMessage struct {
	Type          string         `json:"type"`
	PageID        string         `json:"page_id,omitempty"`
	PageURL       string         `json:"page_url,omitempty"`
	Role          string         `json:"role,omitempty"`
	X             float64        `json:"x,omitempty"`
	Y             float64        `json:"y,omitempty"`
	XPct          float64        `json:"x_pct,omitempty"`
	YPct          float64        `json:"y_pct,omitempty"`
	Selector      string         `json:"selector,omitempty"`
	RX            float64        `json:"rx,omitempty"`
	RY            float64        `json:"ry,omitempty"`
	Error         string         `json:"error,omitempty"`
	EstimatedMs   int64          `json:"estimated_ms,omitempty"`
	Toast         string         `json:"toast,omitempty"`
	Tree          []pageTreeNode `json:"tree,omitempty"`
	ActiveViewers int            `json:"active_viewers,omitempty"`
}

type pageTreeNode struct {
	ID       string   `json:"id"`
	ParentID string   `json:"parent_id,omitempty"`
	Label    string   `json:"label"`
	Depth    int      `json:"depth"`
	History  []string `json:"history,omitempty"`
}

func (m *Manager) handleWSMessage(siteID, viewerID string, conn *websocket.Conn, msg *wsClientMessage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[siteID]
	if !ok {
		return
	}
	viewer, ok := session.Viewers[viewerID]
	if !ok {
		return
	}
	if viewer.Conn != conn {
		return
	}
	viewer.LastSeenAt = time.Now()
	if msg.PageID != "" {
		if _, pageExists := session.Pages[msg.PageID]; pageExists {
			viewer.CurrentPageID = msg.PageID
		}
	}
	if session.OwnerViewerID == viewerID {
		session.OwnerSeenAt = viewer.LastSeenAt
	}
	switch msg.Type {
	case "cursor":
		if session.OwnerViewerID != viewerID {
			return
		}
		m.broadcastLocked(session, wsServerMessage{
			Type:     "cursor",
			PageID:   viewer.CurrentPageID,
			X:        msg.X,
			Y:        msg.Y,
			XPct:     msg.XPct,
			YPct:     msg.YPct,
			Selector: msg.Selector,
			RX:       msg.RX,
			RY:       msg.RY,
		})
	case "ping":
		m.sendViewerLocked(viewer, m.viewerStateMessageLocked(session, viewer.ID))
	}
}

func (m *Manager) ensureViewerLocked(session *Session, viewerID string) *Viewer {
	viewer := session.Viewers[viewerID]
	if viewer != nil {
		return viewer
	}
	viewer = &Viewer{ID: viewerID, LastSeenAt: time.Now(), CurrentPageID: session.OwnerPageID}
	session.Viewers[viewerID] = viewer
	return viewer
}

func viewerRole(session *Session, viewerID string) string {
	if session.OwnerViewerID == viewerID {
		return "owner"
	}
	return "follower"
}

func (m *Manager) sendViewerLocked(viewer *Viewer, msg wsServerMessage) {
	if viewer == nil || viewer.Conn == nil {
		return
	}
	viewer.WriteMu.Lock()
	defer viewer.WriteMu.Unlock()
	if err := viewer.Conn.WriteJSON(msg); err != nil {
		slog.Debug("site websocket write failed", "err", err)
	}
}

func (m *Manager) broadcastLocked(session *Session, msg wsServerMessage) {
	viewers := make([]*Viewer, 0, len(session.Viewers))
	for _, viewer := range session.Viewers {
		if viewer.Conn != nil {
			viewers = append(viewers, viewer)
		}
	}
	for _, viewer := range viewers {
		m.sendViewerLocked(viewer, msg)
	}
}

func (m *Manager) viewerStateMessageLocked(session *Session, viewerID string) wsServerMessage {
	activeViewers := 0
	for _, v := range session.Viewers {
		if v.Conn != nil {
			activeViewers++
		}
	}
	return wsServerMessage{
		Type:          "state",
		PageID:        session.OwnerPageID,
		PageURL:       m.pageURL(session, session.OwnerPageID),
		Role:          viewerRole(session, viewerID),
		Tree:          m.pageTreeLocked(session),
		ActiveViewers: activeViewers,
	}
}

func (m *Manager) broadcastStateLocked(session *Session) {
	for _, viewer := range session.Viewers {
		m.sendViewerLocked(viewer, m.viewerStateMessageLocked(session, viewer.ID))
	}
}

func (m *Manager) gcLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.closed:
			return
		case <-ticker.C:
			m.mu.Lock()
			m.pruneLocked(time.Now())
			for _, session := range m.sessions {
				m.tryHandoffLocked(session)
			}
			m.mu.Unlock()
		}
	}
}

func (m *Manager) tryHandoffLocked(session *Session) {
	if session == nil || session.OwnerViewerID == "" {
		return
	}
	owner := session.Viewers[session.OwnerViewerID]
	if owner != nil && owner.Conn != nil && time.Since(owner.LastSeenAt) <= m.cfg.OwnerTimeout {
		return
	}
	candidates := make([]*Viewer, 0, len(session.Viewers))
	for _, viewer := range session.Viewers {
		if viewer.ID == session.OwnerViewerID {
			continue
		}
		if time.Since(viewer.LastSeenAt) > m.cfg.OwnerTimeout {
			continue
		}
		candidates = append(candidates, viewer)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].LastSeenAt.Before(candidates[j].LastSeenAt)
	})
	if len(candidates) == 0 {
		return
	}
	session.OwnerViewerID = candidates[0].ID
	if candidates[0].CurrentPageID != "" {
		session.OwnerPageID = candidates[0].CurrentPageID
	}
	session.OwnerSeenAt = candidates[0].LastSeenAt
	_ = m.saveSessionLocked(session)
	m.broadcastStateLocked(session)
	m.sendViewerLocked(candidates[0], wsServerMessage{
		Type:  "ownership_changed",
		Role:  "owner",
		Toast: "You are now controlling the page.",
	})
}

func (m *Manager) pageTreeLocked(session *Session) []pageTreeNode {
	if session == nil || len(session.Pages) == 0 {
		return nil
	}
	nodes := make([]pageTreeNode, 0, len(session.Pages))
	var walk func(pageID string, depth int)
	walk = func(pageID string, depth int) {
		page := session.Pages[pageID]
		if page == nil {
			return
		}
		label := strings.TrimSpace(page.Intent)
		if pageID == session.RootPageID || label == "" {
			label = "Root"
		}
		nodes = append(nodes, pageTreeNode{
			ID:       page.ID,
			ParentID: page.ParentID,
			Label:    ellipsis(label, 80),
			Depth:    depth,
			History:  append([]string(nil), page.History...),
		})
		if len(page.Children) == 0 {
			return
		}
		type childEntry struct {
			intent string
			pageID string
		}
		children := make([]childEntry, 0, len(page.Children))
		for intent, childID := range page.Children {
			children = append(children, childEntry{intent: intent, pageID: childID})
		}
		sort.SliceStable(children, func(i, j int) bool {
			if children[i].intent == children[j].intent {
				return children[i].pageID < children[j].pageID
			}
			return children[i].intent < children[j].intent
		})
		for _, child := range children {
			walk(child.pageID, depth+1)
		}
	}
	walk(session.RootPageID, 0)
	return nodes
}

func (m *Manager) recordGenerationMetricsLocked(metrics generationMetrics) {
	if metrics.Duration <= 0 {
		return
	}
	durationMs := float64(metrics.Duration.Milliseconds())
	if durationMs <= 0 {
		durationMs = float64(metrics.Duration) / float64(time.Millisecond)
	}
	m.genStats.Samples++
	n := float64(m.genStats.Samples)
	if durationMs > 0 {
		m.genStats.AvgDurationMs = ((m.genStats.AvgDurationMs * (n - 1)) + durationMs) / n
	}
	if metrics.TokensPerSecond > 0 {
		m.genStats.AvgTokensPerSecond = ((m.genStats.AvgTokensPerSecond * (n - 1)) + metrics.TokensPerSecond) / n
	}
}

func (m *Manager) estimateGenerationDurationLocked(session *Session, intent string, history []string) time.Duration {
	estimatedTokens := 2200
	if session != nil {
		derivePromptAssetsIfMissing(session)
		estimatedTokens += len(session.RootStructure) / 10
		estimatedTokens += len(strings.Join(session.RootLinkInventory, "\n")) / 12
		estimatedTokens += len(session.RootSharedCSS) / 16
		estimatedTokens += len(session.PersonaSystem) / 12
		estimatedTokens += len(session.AdditionalContext) * 120
	}
	estimatedTokens += len(intent) * 8
	estimatedTokens += len(history) * 140

	estimateMs := 9000.0
	if m.genStats.AvgDurationMs > 0 {
		estimateMs = m.genStats.AvgDurationMs
	}
	if m.genStats.AvgTokensPerSecond > 0 {
		tpsEstimateMs := (float64(estimatedTokens) / m.genStats.AvgTokensPerSecond) * 1000
		if estimateMs > 0 {
			estimateMs = (estimateMs + tpsEstimateMs) / 2
		} else {
			estimateMs = tpsEstimateMs
		}
	}
	if estimateMs < 2500 {
		estimateMs = 2500
	}
	if estimateMs > 120000 {
		estimateMs = 120000
	}
	return time.Duration(estimateMs) * time.Millisecond
}

func pruneTimesSince(times []time.Time, cutoff time.Time) []time.Time {
	if len(times) == 0 {
		return nil
	}
	dst := times[:0]
	for _, ts := range times {
		if ts.After(cutoff) {
			dst = append(dst, ts)
		}
	}
	return dst
}

func humanWindow(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		h := int(d / time.Hour)
		if h == 1 {
			return "hour"
		}
		return fmt.Sprintf("%d hours", h)
	case d%time.Minute == 0:
		m := int(d / time.Minute)
		if m == 1 {
			return "minute"
		}
		return fmt.Sprintf("%d minutes", m)
	default:
		return d.String()
	}
}

func (m *Manager) pruneLocked(now time.Time) {
	for id, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			m.removeSessionLocked(session)
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) removeSessionLocked(session *Session) {
	for _, viewer := range session.Viewers {
		if viewer.Conn != nil {
			m.sendViewerLocked(viewer, wsServerMessage{Type: "expired", Error: "site expired"})
			_ = viewer.Conn.Close()
		}
	}
	_ = db.DeleteSiteSession(session.ID)
	delete(m.sessions, session.ID)
}

func (m *Manager) generatePage(ctx context.Context, req generationRequest) (string, generationMetrics, error) {
	requestCtx, cancel := context.WithTimeout(ctx, defaultLlmRequestTimout)
	defer cancel()
	models := model.GetModelsByNames(model.SiteModels)
	if len(models) == 0 {
		return "", generationMetrics{}, fmt.Errorf("site_models did not resolve to any models")
	}

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		llmer := llm.NewLlmerForKey("site:" + uuid.NewString())
		llmer.SetPersona(persona.Persona{System: siteSystemPrompt}, nil)
		llmer.AddMessage(llm.RoleUser, buildSitePrompt(req, attempt > 0), 0)
		start := time.Now()
		response, usage, err := llmer.RequestCompletion(models, persona.InferenceSettings{}.Fixup(), "", requestCtx)
		duration := time.Since(start)
		if err != nil {
			lastErr = err
			continue
		}
		doc := extractHTMLDocument(response)
		metrics := generationMetrics{
			ResponseTokens: usage.ResponseTokens,
			Duration:       duration,
		}
		if duration > 0 && usage.ResponseTokens > 0 {
			metrics.TokensPerSecond = float64(usage.ResponseTokens) / duration.Seconds()
		}
		return doc, metrics, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("page generation failed")
	}
	return "", generationMetrics{}, lastErr
}

const siteSystemPrompt = `You generate immersive, self-contained HTML websites.

Return exactly one complete HTML document for every request.

Rules:
- The page must be fully themed around the provided theme and navigation context.
- Include inline CSS and inline JavaScript as needed.
- Keep HTML, CSS, and JavaScript compact. Avoid giant text dumps and avoid repeating global styling when shared root CSS is already persisted.
- Put visible body content first and place scripts only after the core page markup is already complete.
- Every internal continuation link must be an <a> element with a descriptive aria-label that explains where it goes.
- Continuation links should feel natural for the page, respect the visited-link history, and keep the site explorable.
- You can include interactive elements like forms, search boxes, text inputs, textareas, checkboxes, radio buttons, and select dropdowns.
- Ensure any form/search/input interaction uses a standard HTML <form> element with action/method attributes and a submit button (or triggering submit on Enter). Inputs must have descriptive name and/or id attributes.
- The platform will intercept form submissions and send the values back to you so you can generate the resulting page (e.g. search results, dynamic feedback) matching the user's input.
- Do not use alert(), confirm(), prompt(), document.write(), or automatic redirects/popups.
- The page should be visually rich, surprising, and intentional rather than generic.
- You can use placeholder images from LoremFlickr where suitable (animals, items, but not people or selfies), using this link https://loremflickr.com/<width>/<height> (random image) or this link https://loremflickr.com/<width>/<height>/<search> (image related to search). For example: https://loremflickr.com/320/240/dog
- NSFW content of any kind is allowed.
- Do not explain the page outside the HTML document.
`

func buildSitePrompt(req generationRequest, retry bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Theme: %s\n\n", collapseWhitespace(req.Theme))
	if req.PersonaSystem != "" {
		b.WriteString("Full persona system prompt to honor:\n```text\n")
		b.WriteString(strings.TrimSpace(req.PersonaSystem))
		b.WriteString("\n```\n\n")
	}
	if items := compactPromptItems(req.AdditionalContext, 240, 8); len(items) > 0 {
		b.WriteString("Additionally, you must follow these user-provided context instructions:\n")
		for i, item := range items {
			fmt.Fprintf(&b, "%d. %s\n", i+1, item)
		}
		b.WriteString("\n")
	}
	if req.RootStructure == "" {
		b.WriteString("Generate the first page of a brand-new infinite website.\n")
	} else {
		b.WriteString("Generate the next page of an existing infinite website.\n\n")
		if req.RootTitle != "" {
			fmt.Fprintf(&b, "Root page title: %s\n\n", req.RootTitle)
		}
		if req.RootSharedCSS != "" {
			b.WriteString("The root page CSS below is already persisted across future pages. Reuse it and add new <style> tags only for deltas or page-specific additions:\n```css\n")
			b.WriteString(req.RootSharedCSS)
			b.WriteString("\n```\n\n")
		}
		b.WriteString("Root page structure summary:\n```text\n")
		b.WriteString(req.RootStructure)
		b.WriteString("\n```\n\n")
		if links := compactPromptItems(req.RootLinkInventory, 180, 14); len(links) > 0 {
			b.WriteString("Root page link inventory:\n")
			for i, item := range links {
				fmt.Fprintf(&b, "%d. %s\n", i+1, item)
			}
			b.WriteString("\n")
		}
	}
	if history := compactPromptItems(req.History, 160, 6); len(history) > 0 {
		b.WriteString("Visited link history:\n")
		for i, item := range history {
			fmt.Fprintf(&b, "%d. %s\n", i+1, item)
		}
		b.WriteString("\n")
	}
	if clickedIntent := collapseWhitespace(req.ClickedIntent); clickedIntent != "" {
		fmt.Fprintf(&b, "The clicked link intent for this transition: %s\n\n", ellipsis(clickedIntent, 240))
	}
	if len(req.FormValues) > 0 {
		b.WriteString("User input / Form values submitted:\n")
		keys := make([]string, 0, len(req.FormValues))
		for k := range req.FormValues {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := req.FormValues[k]
			switch val := v.(type) {
			case []any:
				var strVals []string
				for _, sv := range val {
					strVals = append(strVals, fmt.Sprintf("%v", sv))
				}
				fmt.Fprintf(&b, "- %s: [%s]\n", k, strings.Join(strVals, ", "))
			case []string:
				fmt.Fprintf(&b, "- %s: [%s]\n", k, strings.Join(val, ", "))
			default:
				fmt.Fprintf(&b, "- %s: %q\n", k, fmt.Sprintf("%v", val))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("The output must be one full HTML document with natural continuation links so the site can keep expanding.\n")
	if retry {
		b.WriteString("The previous attempt failed. Keep the page coherent and make sure the continuation links stay descriptive.\n")
	}
	return b.String()
}

func extractHTMLDocument(response string) string {
	response = strings.TrimSpace(response)
	_, blocks, changed := htmlrender.Extract(response, 1)
	if changed && len(blocks) > 0 {
		response = strings.TrimSpace(blocks[0].HTML)
	}
	if response == "" {
		return "<!doctype html><html><head><meta charset=\"utf-8\"></head><body></body></html>"
	}
	if !strings.Contains(strings.ToLower(response), "<html") {
		response = "<!doctype html><html><head><meta charset=\"utf-8\"></head><body>" + response + "</body></html>"
	}
	root, err := html.Parse(strings.NewReader(response))
	if err != nil {
		return minifyHTML(response)
	}
	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		return minifyHTML(response)
	}
	return minifyHTML(strings.TrimSpace(buf.String()))
}

func derivePromptAssetsIfMissing(session *Session) {
	if session == nil {
		return
	}
	if session.RootHTML != "" {
		session.RootHTML = extractHTMLDocument(session.RootHTML)
	}
	if session.RootTitle != "" && session.RootStructure != "" && session.RootSharedCSS != "" {
		return
	}
	assets := derivePromptAssets(session.RootHTML)
	if session.RootTitle == "" {
		session.RootTitle = assets.Title
	}
	if session.RootStructure == "" {
		session.RootStructure = assets.Structure
	}
	if len(session.RootLinkInventory) == 0 {
		session.RootLinkInventory = append([]string(nil), assets.LinkInventory...)
	}
	if session.RootSharedCSS == "" {
		session.RootSharedCSS = assets.SharedCSS
	}
}

func derivePromptAssets(doc string) promptAssets {
	doc = extractHTMLDocument(doc)
	if doc == "" {
		return promptAssets{}
	}
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return promptAssets{Structure: collapseWhitespace(doc)}
	}
	assets := promptAssets{Title: extractDocumentTitle(root)}
	collectAndRemoveStyleNodes(root, &assets.SharedCSS)
	collectLinkInventory(root, &assets.LinkInventory)
	removeScriptNodes(root)
	stripNodeText(root)
	stripHeadToTitle(root)
	cleanupPromotedHeadNodes(root)
	assets.Structure = buildStructureSummary(root)
	assets.SharedCSS = minifyCSS(assets.SharedCSS)
	assets.LinkInventory = compactPromptItems(assets.LinkInventory, 180, 14)
	return assets
}

func injectRootSharedCSS(doc, sharedCSS string) string {
	sharedCSS = minifyCSS(sharedCSS)
	if sharedCSS == "" {
		return doc
	}
	root, err := html.Parse(strings.NewReader(doc))
	if err != nil {
		return doc
	}
	if documentHasRootSharedCSS(root) {
		return minifyHTML(doc)
	}
	head := findFirstElement(root, "head")
	if head == nil {
		htmlNode := findFirstElement(root, "html")
		if htmlNode == nil {
			return minifyHTML(doc)
		}
		head = &html.Node{Type: html.ElementNode, Data: "head"}
		if htmlNode.FirstChild != nil {
			htmlNode.InsertBefore(head, htmlNode.FirstChild)
		} else {
			htmlNode.AppendChild(head)
		}
	}
	styleNode := &html.Node{
		Type: html.ElementNode,
		Data: "style",
		Attr: []html.Attribute{{Key: "id", Val: rootSharedCSSStyleID}},
	}
	styleNode.AppendChild(&html.Node{Type: html.TextNode, Data: sharedCSS})
	if head.FirstChild != nil {
		head.InsertBefore(styleNode, head.FirstChild)
	} else {
		head.AppendChild(styleNode)
	}
	var buf bytes.Buffer
	if err := html.Render(&buf, root); err != nil {
		return doc
	}
	return minifyHTML(strings.TrimSpace(buf.String()))
}

func documentHasRootSharedCSS(root *html.Node) bool {
	return walkHTML(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "style" && nodeHasAttrValue(node, "id", rootSharedCSSStyleID)
	})
}

func collectAndRemoveStyleNodes(root *html.Node, dst *string) {
	var collected []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.ElementNode && child.Data == "style" && !nodeHasAttrValue(child, "id", rootSharedCSSStyleID) {
				if css := strings.TrimSpace(nodeTextContent(child)); css != "" {
					collected = append(collected, css)
				}
				node.RemoveChild(child)
			} else {
				walk(child)
			}
			child = next
		}
	}
	walk(root)
	if len(collected) == 0 {
		return
	}
	if *dst != "" {
		*dst += "\n"
	}
	*dst += strings.Join(collected, "\n")
}

func collectLinkInventory(root *html.Node, dst *[]string) {
	seen := map[string]struct{}{}
	walkHTML(root, func(node *html.Node) bool {
		if node.Type != html.ElementNode || node.Data != "a" {
			return false
		}
		href := collapseWhitespace(nodeAttr(node, "href"))
		if href == "" || strings.HasPrefix(strings.ToLower(href), "javascript:") {
			return false
		}
		label := firstNonEmptyNonBlank(
			collapseWhitespace(nodeAttr(node, "aria-label")),
			collapseWhitespace(nodeAttr(node, "title")),
			collapseWhitespace(nodeTextContent(node)),
		)
		if label == "" {
			label = "link"
		}
		entry := fmt.Sprintf("%s -> %s", ellipsis(label, 72), ellipsis(href, 96))
		if _, ok := seen[entry]; ok {
			return false
		}
		seen[entry] = struct{}{}
		*dst = append(*dst, entry)
		return false
	})
}

func removeScriptNodes(root *html.Node) {
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; {
			next := child.NextSibling
			if child.Type == html.ElementNode && (child.Data == "script" || child.Data == "noscript" || child.Data == "template") {
				node.RemoveChild(child)
			} else {
				walk(child)
			}
			child = next
		}
	}
	walk(root)
}

func stripNodeText(root *html.Node) {
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if node.Type != html.TextNode {
			return
		}
		text := collapseWhitespace(node.Data)
		if text == "" {
			node.Data = ""
			return
		}
		parent := node.Parent
		if parent != nil && preserveShortTextForStructure(parent) {
			node.Data = ellipsis(text, 36)
			return
		}
		node.Data = ""
	}
	walk(root)
}

func preserveShortTextForStructure(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	switch node.Data {
	case "title", "a", "button", "label", "summary", "legend", "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	default:
		return false
	}
}

func buildStructureSummary(root *html.Node) string {
	body := findFirstElement(root, "body")
	if body == nil {
		body = root
	}
	lines := make([]string, 0, 48)
	remaining := 64
	appendStructureLines(body, 0, &remaining, &lines)
	if len(lines) == 0 {
		return "body"
	}
	if remaining <= 0 {
		lines = append(lines, "  ...")
	}
	return strings.Join(lines, "\n")
}

func appendStructureLines(node *html.Node, depth int, remaining *int, lines *[]string) {
	if node == nil || *remaining <= 0 || node.Type != html.ElementNode {
		return
	}
	include := shouldIncludeStructureNode(node) || depth == 0
	nextDepth := depth
	if include {
		*lines = append(*lines, strings.Repeat("  ", depth)+structureNodeDescriptor(node))
		*remaining = *remaining - 1
		nextDepth = depth + 1
	}
	for child := node.FirstChild; child != nil && *remaining > 0; child = child.NextSibling {
		if child.Type == html.ElementNode {
			appendStructureLines(child, nextDepth, remaining, lines)
		}
	}
}

func shouldIncludeStructureNode(node *html.Node) bool {
	if node == nil || node.Type != html.ElementNode {
		return false
	}
	switch node.Data {
	case "body", "header", "nav", "main", "section", "article", "aside", "footer", "form", "input", "button", "a", "ul", "ol", "li", "img", "video", "canvas", "svg", "table", "thead", "tbody", "tr", "td", "th", "figure", "figcaption", "dialog", "details", "summary", "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	case "div":
		return nodeAttr(node, "id") != "" || nodeAttr(node, "class") != "" || nodeAttr(node, "role") != "" || countElementChildren(node) > 1
	default:
		return nodeAttr(node, "id") != "" || nodeAttr(node, "class") != "" || nodeAttr(node, "role") != "" || nodeAttr(node, "aria-label") != ""
	}
}

func structureNodeDescriptor(node *html.Node) string {
	var b strings.Builder
	b.WriteString(node.Data)
	if id := collapseWhitespace(nodeAttr(node, "id")); id != "" {
		b.WriteString("#")
		b.WriteString(ellipsis(id, 24))
	}
	if class := collapseWhitespace(nodeAttr(node, "class")); class != "" {
		classCount := 0
		for _, part := range strings.Fields(class) {
			if part == "" {
				continue
			}
			b.WriteString(".")
			b.WriteString(ellipsis(part, 18))
			classCount++
			if classCount >= 3 {
				break
			}
		}
	}
	if role := collapseWhitespace(nodeAttr(node, "role")); role != "" {
		fmt.Fprintf(&b, "[role=%s]", ellipsis(role, 20))
	}
	if label := firstNonEmptyNonBlank(
		collapseWhitespace(nodeAttr(node, "aria-label")),
		shortNodeText(node),
	); label != "" && descriptorShouldIncludeLabel(node) {
		fmt.Fprintf(&b, "{%q}", ellipsis(label, 32))
	}
	if href := collapseWhitespace(nodeAttr(node, "href")); href != "" && node.Data == "a" {
		fmt.Fprintf(&b, "->%s", ellipsis(href, 40))
	}
	return b.String()
}

func descriptorShouldIncludeLabel(node *html.Node) bool {
	switch node.Data {
	case "a", "button", "summary", "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	default:
		return false
	}
}

func shortNodeText(node *html.Node) string {
	text := collapseWhitespace(nodeTextContent(node))
	if text == "" {
		return ""
	}
	return ellipsis(text, 36)
}

func countElementChildren(node *html.Node) int {
	count := 0
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode {
			count++
		}
	}
	return count
}

func stripHeadToTitle(root *html.Node) {
	head := findFirstElement(root, "head")
	if head == nil {
		return
	}
	for child := head.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.ElementNode && child.Data == "title" {
			child.Attr = nil
			for grandChild := child.FirstChild; grandChild != nil; grandChild = grandChild.NextSibling {
				if grandChild.Type == html.TextNode {
					grandChild.Data = collapseWhitespace(grandChild.Data)
				}
			}
			child = next
			continue
		}
		head.RemoveChild(child)
		child = next
	}
}

func cleanupPromotedHeadNodes(root *html.Node) {
	body := findFirstElement(root, "body")
	head := findFirstElement(root, "head")
	if body == nil || head == nil {
		return
	}
	hasHeadTitle := findFirstElement(head, "title") != nil
	for child := body.FirstChild; child != nil; {
		next := child.NextSibling
		if child.Type == html.TextNode && strings.TrimSpace(child.Data) == "" {
			body.RemoveChild(child)
			child = next
			continue
		}
		if child.Type != html.ElementNode {
			break
		}
		switch child.Data {
		case "title":
			if !hasHeadTitle {
				body.RemoveChild(child)
				head.AppendChild(child)
				hasHeadTitle = true
				child = next
				continue
			}
			body.RemoveChild(child)
			child = next
			continue
		case "meta", "base", "link", "script", "noscript", "template":
			body.RemoveChild(child)
			child = next
			continue
		}
		break
	}
}

func extractDocumentTitle(root *html.Node) string {
	title := findFirstElement(root, "title")
	if title == nil {
		return ""
	}
	return collapseWhitespace(nodeTextContent(title))
}

func findFirstElement(root *html.Node, tag string) *html.Node {
	var found *html.Node
	walkHTML(root, func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == tag {
			found = node
			return true
		}
		return false
	})
	return found
}

func walkHTML(root *html.Node, visit func(*html.Node) bool) bool {
	if root == nil {
		return false
	}
	if visit(root) {
		return true
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if walkHTML(child, visit) {
			return true
		}
	}
	return false
}

func nodeHasAttrValue(node *html.Node, key, value string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key && attr.Val == value {
			return true
		}
	}
	return false
}

func nodeAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func firstNonEmptyNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func nodeTextContent(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			b.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func compactPromptItems(items []string, maxItemLen, maxItems int) []string {
	if len(items) == 0 {
		return nil
	}
	start := 0
	if maxItems > 0 && len(items) > maxItems {
		start = len(items) - maxItems
	}
	compacted := make([]string, 0, len(items)-start)
	for _, item := range items[start:] {
		item = collapseWhitespace(item)
		if item == "" {
			continue
		}
		if maxItemLen > 0 {
			item = ellipsis(item, maxItemLen)
		}
		compacted = append(compacted, item)
	}
	return compacted
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func minifyHTML(doc string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	minified, err := siteMinifier.String("text/html", doc)
	if err != nil {
		return doc
	}
	return strings.TrimSpace(minified)
}

func minifyCSS(css string) string {
	css = strings.TrimSpace(css)
	if css == "" {
		return ""
	}
	minified, err := siteMinifier.String("text/css", css)
	if err != nil {
		return css
	}
	return strings.TrimSpace(minified)
}

type bootstrapConfig struct {
	SiteID            string         `json:"site_id"`
	PageID            string         `json:"page_id"`
	Token             string         `json:"token"`
	ExpiresAt         int64          `json:"expires_at"`
	DefaultEstimateMs int64          `json:"default_estimate_ms"`
	Tree              []pageTreeNode `json:"tree"`
}

//go:embed script.js
var bootstrapScript string

func injectBootstrap(doc string, cfg bootstrapConfig) string {
	payload, _ := json.Marshal(cfg)
	script := `<script id="x3-site-bootstrap" type="application/json">` + string(payload) + `</script><script>` + bootstrapScript + `</script>`
	lower := strings.ToLower(doc)
	if idx := strings.LastIndex(lower, "</body>"); idx >= 0 {
		return doc[:idx] + script + doc[idx:]
	}
	return doc + script
}
func normalizeIntent(intent, href string) string {
	intent = strings.TrimSpace(intent)
	if intent != "" {
		return ellipsis(intent, 300)
	}
	href = strings.TrimSpace(href)
	if href != "" {
		return ellipsis(href, 300)
	}
	return ""
}

func ellipsis(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

func randHex(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

func errorHTML(title, message string) string {
	return fmt.Sprintf(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>body{margin:0;font-family:system-ui,sans-serif;background:linear-gradient(135deg,#171717,#3a0f18);color:#f8f6f2;min-height:100vh;display:grid;place-items:center}.card{width:min(720px,calc(100%% - 48px));padding:32px;border:1px solid rgba(255,255,255,.12);border-radius:24px;background:rgba(0,0,0,.35);box-shadow:0 20px 80px rgba(0,0,0,.35)}h1{margin:0 0 12px;font-size:2rem}p{margin:0;color:rgba(248,246,242,.82);line-height:1.65}</style></head><body><main class="card"><h1>%s</h1><p>%s</p></main></body></html>`, title, title, message)
}

func (m *Manager) writeExpiredPage(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(errorHTML("Site Expired", reason)))
}

func (m *Manager) writeNotFoundPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(errorHTML("Private Site", "This private site is unavailable.")))
}
