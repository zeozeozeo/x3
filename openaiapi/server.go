package openaiapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
)

// Server is a local OpenAI-compatible API server. It exposes the models
// configured in models.json by their short codename and routes requests
// through x3's provider router.
type Server struct {
	cfg        Config
	httpServer *http.Server
	created    int64

	// baseURLHook allows tests to redirect upstream requests to a local
	// server. It is nil in production.
	baseURLHook func(provider, baseURL string) string
}

// NewServerFromEnv creates a proxy server configured from the environment.
func NewServerFromEnv() *Server {
	return NewServer(ConfigFromEnv())
}

// NewServer creates a proxy server with the given configuration.
func NewServer(cfg Config) *Server {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}

	s := &Server{
		cfg:     cfg,
		created: time.Now().Unix(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/models", s.authenticate(s.handleModels))
	mux.HandleFunc("/v1/models/", s.authenticate(s.handleModel))
	mux.HandleFunc("/v1/chat/completions", s.authenticate(s.handleChatCompletions))

	s.httpServer = &http.Server{
		Addr:              cfg.BindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	return s
}

// URL returns the base URL of the running server.
func (s *Server) URL() string {
	if s == nil || s.httpServer == nil {
		return ""
	}
	addr := s.httpServer.Addr
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	return "http://" + addr
}

// Start starts the HTTP server. It blocks until the server stops.
func (s *Server) Start() error {
	if s == nil || s.httpServer == nil {
		return errors.New("openai proxy server is not configured")
	}
	if len(s.cfg.APIKeys) == 0 {
		return errors.New("openai proxy requires at least one API key (set X3_OPENAI_PROXY_API_KEYS)")
	}
	slog.Info("starting openai proxy server", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Stop gracefully shuts the server down.
func (s *Server) Stop(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

// authenticate checks the bearer token of the request.
func (s *Server) authenticate(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, errTypeAuthentication, "invalid_api_key",
				"Missing or invalid API key. Pass it as 'Authorization: Bearer <key>'.")
			return
		}
		next(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	token := ""
	if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
		token = strings.TrimSpace(header[7:])
	} else if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		token = key
	}
	if token == "" {
		return false
	}
	for _, key := range s.cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(token)) == 1 {
			return true
		}
	}
	return false
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// upstreamClient builds an OpenAI client for the given provider, base URL and
// token, mirroring the way the bot itself talks to providers.
func (s *Server) upstreamClient(provider, baseURL, token string) *openai.Client {
	if s.baseURLHook != nil {
		baseURL = s.baseURLHook(provider, baseURL)
	}
	config := openai.DefaultConfig(token)
	config.BaseURL = baseURL
	if provider == model.ProviderGithub { // for github we need an azure config, idfk why
		config = openai.DefaultAzureConfig(token, baseURL)
		config.APIType = openai.APITypeOpenAI
	}
	if provider == model.ProviderOpencode {
		config.DefaultHeaders = opencodeHeaders()
	}
	return openai.NewClientWithConfig(config)
}

// errNoClientConfig is returned when a provider has no usable credentials.
var errNoClientConfig = errors.New("no valid client configurations")

func fmtErrNoClient(provider string) error {
	return fmt.Errorf("%w for provider %s", errNoClientConfig, provider)
}
