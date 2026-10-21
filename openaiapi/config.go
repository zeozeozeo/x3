package openaiapi

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBindAddr listens on every interface so other devices on the
	// local network can reach the proxy. The proxy always requires a bearer
	// key, see ConfigFromEnv.
	DefaultBindAddr = "0.0.0.0:6745"

	// DefaultTimeout is the maximum duration of a single upstream request.
	DefaultTimeout = 5 * time.Minute

	// DefaultMaxRetries is how many extra attempts are made per provider
	// before the request is failed over to the next provider.
	DefaultMaxRetries = 2
)

// Config holds the configuration of the local OpenAI-compatible proxy.
type Config struct {
	// BindAddr is the address the HTTP server listens on.
	BindAddr string

	// APIKeys are the accepted bearer tokens. The proxy refuses to start
	// when this list is empty.
	APIKeys []string

	// Timeout is the deadline applied to a single upstream request.
	Timeout time.Duration

	// MaxRetries is the number of retries per provider configuration.
	MaxRetries int
}

// ConfigFromEnv reads the proxy configuration from the environment.
func ConfigFromEnv() Config {
	bindAddr := strings.TrimSpace(os.Getenv("X3_OPENAI_PROXY_BIND_ADDR"))
	if bindAddr == "" {
		bindAddr = DefaultBindAddr
	}

	timeout := DefaultTimeout
	if raw := strings.TrimSpace(os.Getenv("X3_OPENAI_PROXY_TIMEOUT")); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			timeout = parsed
		}
	}

	maxRetries := DefaultMaxRetries
	if raw := strings.TrimSpace(os.Getenv("X3_OPENAI_PROXY_MAX_RETRIES")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 {
			maxRetries = parsed
		}
	}

	return Config{
		BindAddr:   bindAddr,
		APIKeys:    parseAPIKeys(os.Getenv("X3_OPENAI_PROXY_API_KEYS")),
		Timeout:    timeout,
		MaxRetries: maxRetries,
	}
}

// parseAPIKeys splits a comma (or whitespace) separated list of bearer keys.
func parseAPIKeys(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		if field = strings.TrimSpace(field); field != "" {
			keys = append(keys, field)
		}
	}
	return keys
}
