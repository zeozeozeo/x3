package whisper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultModel = "whisper-large-v3-turbo"
	MaxAudioBytes = 25 * 1024 * 1024
	MaxDuration = 3 * time.Minute

	defaultBaseURL = "https://api.groq.com/openai/v1"
	defaultTimeout = 120 * time.Second
	maxResponse    = 256 << 10
	maxErrorBody   = 512
)

// Config carries the Whisper endpoint settings.
type Config struct {
	Enabled       bool
	BaseURL       string
	Tokens        []string
	Model         string
	Timeout       time.Duration
	MaxAudioBytes int
}

// ConfigFromEnv reads the X3_WHISPER_* and X3_GROQ_TOKEN variables.
func ConfigFromEnv() Config {
	cfg := Config{
		Enabled:       envBool("X3_WHISPER_ENABLED", true),
		BaseURL:       strings.TrimRight(envString("X3_WHISPER_BASE_URL", defaultBaseURL), "/"),
		Tokens:        envTokens("X3_GROQ_TOKEN"),
		Model:         envString("X3_WHISPER_MODEL", DefaultModel),
		Timeout:       envDuration("X3_WHISPER_TIMEOUT", defaultTimeout),
		MaxAudioBytes: envIntBytes("X3_WHISPER_MAX_AUDIO_MB", MaxAudioBytes),
	}
	return cfg
}

// Transcribe sends audio to Groq's transcription endpoint using the
// environment configuration.
func Transcribe(ctx context.Context, filename string, audio []byte) (string, error) {
	return ConfigFromEnv().Transcribe(ctx, filename, audio)
}

// Transcribe returns the transcript for audio, failing over between the
// configured tokens when a token is rejected, rate limited or the provider
// errors.
func (c Config) Transcribe(ctx context.Context, filename string, audio []byte) (string, error) {
	if len(c.Tokens) == 0 {
		return "", errors.New("whisper: X3_GROQ_TOKEN is not set")
	}
	limit := c.MaxAudioBytes
	if limit <= 0 {
		limit = MaxAudioBytes
	}
	if len(audio) > limit {
		return "", errors.New("whisper: audio exceeds the configured size limit")
	}
	model := c.Model
	if model == "" {
		model = DefaultModel
	}
	baseURL := strings.TrimRight(c.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	client := &http.Client{Timeout: c.Timeout}
	if c.Timeout <= 0 {
		client.Timeout = defaultTimeout
	}

	var lastErr error
	for _, token := range c.Tokens {
		body, err := c.attempt(ctx, client, baseURL, token, model, filename, audio)
		if err == nil {
			return body, nil
		}
		if !retryableAuthOrServer(err) {
			return "", err
		}
		lastErr = err
	}
	return "", lastErr
}

// statusError carries the provider status code so the failover decision does
// not have to parse error strings.
type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string {
	body := strings.TrimSpace(e.body)
	if body == "" {
		return fmt.Sprintf("whisper: provider returned status %d", e.status)
	}
	return fmt.Sprintf("whisper: provider returned status %d: %s", e.status, body)
}

// retryableAuthOrServer reports whether another token may succeed.
func retryableAuthOrServer(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	switch {
	case se.status == http.StatusUnauthorized, se.status == http.StatusForbidden,
		se.status == http.StatusTooManyRequests:
		return true
	case se.status >= 500:
		return true
	default:
		return false
	}
}

func (c Config) attempt(ctx context.Context, client *http.Client, baseURL, token, model, filename string, audio []byte) (string, error) {
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	if err := form.WriteField("model", model); err != nil {
		return "", fmt.Errorf("whisper: build form: %w", err)
	}
	if err := form.WriteField("response_format", "text"); err != nil {
		return "", fmt.Errorf("whisper: build form: %w", err)
	}
	name := filepath.Base(strings.TrimSpace(filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "audio.flac"
	}
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		return "", fmt.Errorf("whisper: build form: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return "", fmt.Errorf("whisper: build form: %w", err)
	}
	if err := form.Close(); err != nil {
		return "", fmt.Errorf("whisper: build form: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/audio/transcriptions", &buf)
	if err != nil {
		return "", fmt.Errorf("whisper: build request: %w", err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "x3/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return "", fmt.Errorf("whisper: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", &statusError{status: resp.StatusCode, body: truncate(string(body), maxErrorBody)}
	}
	return strings.TrimSpace(string(body)), nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

// envTokens splits a semicolon-separated token list.
func envTokens(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	tokens := make([]string, 0, 2)
	for token := range strings.SplitSeq(raw, ";") {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// envIntBytes reads a megabyte value and returns bytes.
func envIntBytes(key string, fallbackBytes int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallbackBytes
	}
	return value * 1024 * 1024
}
