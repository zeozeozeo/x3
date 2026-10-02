package systemone

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout = 10 * time.Second
	// maxErrorBody bounds how much of a failed response we read for the error
	// message.
	maxErrorBody = 8 << 10
)

// Image is an inline image for multimodal decision models. ContentType must be
// image/png, image/jpeg, or image/webp.
type Image struct {
	ContentType string `json:"content_type"`
	Base64      string `json:"base64"`
}

// Request is a System One decision request. State may be a string, an object,
// or an array; Questions is keyed by caller-chosen ids that are echoed back in
// the answers.
type Request struct {
	Model     string              `json:"model"`
	State     any                 `json:"state"`
	Questions map[string]Question `json:"questions"`

	// Images is a clef extension to the System One API rather than part of the
	// spec. Providers that do not support it will reject the request, so only
	// send it to models documented to accept it. Clef allows at most four.
	Images []Image `json:"images,omitempty"`
}

// Answer is the discriminated union returned for each question. Only the
// fields matching Type are populated.
type Answer struct {
	Type string `json:"type"`
	// Noul is P(yes) for a noul question.
	Noul *float64 `json:"noul,omitempty"`
	// Choice is the winning option name for a choice question.
	Choice string `json:"choice,omitempty"`
	// Score is the probability-weighted level for a score question and may
	// fall between levels.
	Score *float64 `json:"score,omitempty"`
	// Probabilities is the distribution over options or levels.
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	// Confidence accompanies choice and score answers. Noul answers do not
	// return one: the probability already is the certainty measure.
	Confidence *float64 `json:"confidence,omitempty"`
	// Legend maps level index to level description for score answers.
	Legend map[string]string `json:"legend,omitempty"`
}

// NoulValue reports the probability of a yes answer.
func (a Answer) NoulValue() (float64, bool) {
	if a.Noul == nil {
		return 0, false
	}
	return *a.Noul, true
}

// Certainty reports how decisive the answer is regardless of question type.
// For noul answers that is the probability itself, since 0.5 is maximum
// uncertainty. For choice and score answers it is the reported confidence, and
// 0.5 is used as a neutral fallback when the provider omits it.
func (a Answer) Certainty() float64 {
	if a.Noul != nil {
		return *a.Noul
	}
	if a.Confidence != nil {
		return *a.Confidence
	}
	return 0.5
}

// Result is the decision returned for a request.
type Result struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
}

// Usage reports token counts for the evaluation.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Noul looks up a noul answer and its probability.
func (r Result) Noul(id string) (float64, bool) {
	answer, ok := r.Answers[id]
	if !ok {
		return 0, false
	}
	return answer.NoulValue()
}

// Selected looks up the winning option of a choice answer.
func (r Result) Selected(id string) (string, bool) {
	answer, ok := r.Answers[id]
	if !ok || answer.Choice == "" {
		return "", false
	}
	return answer.Choice, true
}

// APIError is one error reported by the provider.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e APIError) Error() string {
	return fmt.Sprintf("api error %d: %s", e.Code, e.Message)
}

// Config describes how to reach a System One provider. Callers own credential
// selection; this package reads no environment variables.
type Config struct {
	// BaseURL is the full decision endpoint, model path included.
	BaseURL string
	// Token is sent as a bearer credential.
	Token string
	// Model is the upstream model name. Defaults to ModelFlash when empty.
	Model string
	// HTTPClient overrides the default client.
	HTTPClient *http.Client
	// Timeout bounds a single call. Defaults to defaultTimeout.
	Timeout time.Duration
}

// Client calls a System One provider.
type Client struct {
	baseURL string
	token   string
	model   string
	http    *http.Client
	timeout time.Duration
}

// NewClient builds a client from cfg. It fails only on a missing endpoint, so
// credential problems surface on the first call instead of at construction.
func NewClient(cfg Config) (*Client, error) {
	baseURL := strings.TrimSpace(cfg.BaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("systemone: base url is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = ModelFlash
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
			},
			Timeout: timeout,
		}
	}
	return &Client{
		baseURL: baseURL,
		token:   strings.TrimSpace(cfg.Token),
		model:   model,
		http:    httpClient,
		timeout: timeout,
	}, nil
}

// Decide evaluates the request's questions against its state in one call. An
// empty request model defaults to the client's configured model.
func (c *Client) Decide(ctx context.Context, req Request) (Result, error) {
	if len(req.Questions) == 0 {
		return Result{}, fmt.Errorf("systemone: at least one question is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = c.model
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return Result{}, fmt.Errorf("systemone: encode request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(payload))
	if err != nil {
		return Result{}, fmt.Errorf("systemone: new request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.http.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("systemone: post %s: %w", c.baseURL, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	if err != nil {
		return Result{}, fmt.Errorf("systemone: read response: %w", err)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return Result{}, fmt.Errorf("systemone: status %d: %w", response.StatusCode, providerError(body))
	}

	result, err := parseResult(body)
	if err != nil {
		return Result{}, err
	}
	if len(result.Answers) == 0 {
		return Result{}, fmt.Errorf("systemone: response contained no answers")
	}
	if result.Model == "" {
		result.Model = req.Model
	}
	return result, nil
}

type envelope struct {
	Model    string            `json:"model"`
	Answers  map[string]Answer `json:"answers"`
	Usage    Usage             `json:"usage"`
	Result   *Result           `json:"result"`
	Success  *bool             `json:"success"`
	Errors   []APIError        `json:"errors"`
	Messages []json.RawMessage `json:"messages"`
}

func parseResult(body []byte) (Result, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return Result{}, fmt.Errorf("systemone: decode response: %w", err)
	}
	if env.Result != nil && len(env.Result.Answers) > 0 {
		return *env.Result, nil
	}
	if env.Success != nil && !*env.Success {
		return Result{}, fmt.Errorf("systemone: provider reported failure: %w", providerError(body))
	}
	if len(env.Errors) > 0 {
		return Result{}, fmt.Errorf("systemone: provider returned errors: %w", providerError(body))
	}
	return Result{Model: env.Model, Answers: env.Answers, Usage: env.Usage}, nil
}

// providerError digs a readable message out of an error body, falling back to
// a truncated copy of the raw payload.
func providerError(body []byte) error {
	var env envelope
	if err := json.Unmarshal(body, &env); err == nil && len(env.Errors) > 0 {
		msgs := make([]string, 0, len(env.Errors))
		for _, e := range env.Errors {
			msgs = append(msgs, e.Error())
		}
		return fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return fmt.Errorf("no error detail")
	}
	if len(trimmed) > 256 {
		trimmed = trimmed[:256] + "..."
	}
	return fmt.Errorf("%s", trimmed)
}
