package openaiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
	"github.com/zeozeozeo/x3/opencode"
)

const (
	// maxRequestBytes is the maximum size of an incoming request body.
	maxRequestBytes = 32 << 20

	// openCodeMaxGateRounds bounds how many times the OpenCode gate tool is
	// answered internally before giving up.
	openCodeMaxGateRounds = 3

	// openCodeGateToolUnavailableResult is sent as the result of the gate
	// tool the OpenCode Zen free tier requires. The tool is a stub here, so
	// the model is told to answer without it.
	openCodeGateToolUnavailableResult = "The read tool is not available in this environment. Answer using only the conversation."
)

// handleChatCompletions implements POST /v1/chat/completions.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errTypeInvalidRequest, "method_not_allowed",
			"Only POST is supported on this endpoint.")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, errTypeInvalidRequest, "invalid_request",
			"Could not read the request body: "+err.Error())
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, http.StatusBadRequest, errTypeInvalidRequest, "invalid_json",
			"The request body is not valid JSON: "+err.Error())
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, errTypeInvalidRequest, "invalid_request",
			"The request body is not a valid chat completion request: "+err.Error())
		return
	}

	req.Model = strings.TrimSpace(req.Model)
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, errTypeInvalidRequest, "missing_model",
			"The 'model' parameter is required.")
		return
	}

	m, ok := model.GetModelByCommand(req.Model)
	if !ok {
		writeError(w, http.StatusNotFound, errTypeNotFound, "model_not_found",
			"The model '"+req.Model+"' does not exist. Use GET /v1/models for the list of available models.")
		return
	}
	if !isLLM(m) {
		writeError(w, http.StatusBadRequest, errTypeInvalidRequest, "unsupported_model",
			"The model '"+req.Model+"' is not a language model and cannot be served by the API.")
		return
	}

	extra := extraParams(raw)
	clientReasoning := clientSetReasoning(raw)

	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.Timeout)
	defer cancel()

	if req.Stream {
		s.streamChatCompletion(ctx, w, m, req, extra, clientReasoning)
		return
	}

	resp, err := s.completeChat(ctx, m, req, extra, clientReasoning)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	resp.Model = req.Model
	writeJSON(w, http.StatusOK, resp)
}

// completeChat runs a non-streaming completion through the provider router.
func (s *Server) completeChat(
	ctx context.Context,
	m model.Model,
	req openai.ChatCompletionRequest,
	extra map[string]any,
	clientReasoning bool,
) (openai.ChatCompletionResponse, error) {
	providers := providerOrder(m)
	if len(providers) == 0 {
		return openai.ChatCompletionResponse{}, fmt.Errorf("no providers are configured for model %s", m.Command)
	}

	var lastErr error
	for _, provider := range providers {
		start := time.Now()
		resp, err := s.tryProvider(ctx, m, provider.Name, req, extra, clientReasoning)
		if err == nil {
			model.RecordProviderSuccess(m, provider.Name, time.Since(start))
			return resp, nil
		}
		if ctx.Err() != nil {
			return openai.ChatCompletionResponse{}, err
		}
		if errors.Is(err, errNoClientConfig) {
			// Missing credentials are not the provider's fault.
			slog.Warn("provider has no client configuration", "model", m.Command, "provider", provider.Name)
			if lastErr == nil {
				lastErr = err
			}
			continue
		}
		lastErr = err
		model.RecordProviderFailure(m, provider.Name, classifyFailure(err))
		slog.Warn("provider failed, trying the next one",
			"model", m.Command, "provider", provider.Name, "error", err)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no providers are configured for model %s", m.Command)
	}
	return openai.ChatCompletionResponse{}, lastErr
}

// tryProvider tries every base URL, token and codename of a single provider.
func (s *Server) tryProvider(
	ctx context.Context,
	m model.Model,
	provider string,
	req openai.ChatCompletionRequest,
	extra map[string]any,
	clientReasoning bool,
) (openai.ChatCompletionResponse, error) {
	baseUrls, tokens, codenames := m.Client(provider)
	if len(baseUrls) == 0 || len(tokens) == 0 || len(codenames) == 0 {
		return openai.ChatCompletionResponse{}, fmtErrNoClient(provider)
	}

	var lastErr error
	for i, baseURL := range baseUrls {
		tokensToTry := tokens
		if len(baseUrls) > 1 && len(baseUrls) == len(tokens) {
			if i >= len(tokens) {
				continue
			}
			tokensToTry = []string{tokens[i]}
		}

		for _, token := range tokensToTry {
			client := s.upstreamClient(provider, baseURL, token)
			for attempt := 0; attempt <= s.cfg.MaxRetries; attempt++ {
				resp, err := s.callUpstream(ctx, m, provider, client, codenames, req, extra, clientReasoning)
				if err == nil {
					return resp, nil
				}
				lastErr = err
				if ctx.Err() != nil {
					return openai.ChatCompletionResponse{}, err
				}
				if !isRetryable(err) {
					break
				}
				slog.Warn("upstream request failed, retrying",
					"model", m.Command, "provider", provider, "attempt", attempt+1, "error", err)
				select {
				case <-ctx.Done():
					return openai.ChatCompletionResponse{}, ctx.Err()
				case <-time.After(retryDelay(attempt)):
				}
			}
			model.RecordTokenError(token)
		}
	}

	return openai.ChatCompletionResponse{}, fmt.Errorf("all configurations for provider %s failed: %w", provider, lastErr)
}

// callUpstream performs the actual upstream request for every codename of a
// provider until one succeeds.
func (s *Server) callUpstream(
	ctx context.Context,
	m model.Model,
	provider string,
	client *openai.Client,
	codenames []string,
	req openai.ChatCompletionRequest,
	extra map[string]any,
	clientReasoning bool,
) (openai.ChatCompletionResponse, error) {
	var lastErr error
	for _, codename := range codenames {
		upstream := req
		upstream.Model = codename
		upstream.Extra = extra
		if m.Reasoning && !clientReasoning {
			applyReasoningDefaults(&upstream, provider, codename)
		}

		slog.Info("attempting request", "model", m.Command, "provider", provider, "codename", codename)

		var (
			resp openai.ChatCompletionResponse
			err  error
		)
		if provider == model.ProviderOpencode {
			resp, err = s.callOpenCode(ctx, client, upstream)
		} else {
			resp, err = client.CreateChatCompletion(ctx, upstream)
		}
		if err != nil {
			lastErr = err
			slog.Warn("request failed, trying next codename",
				"model", m.Command, "provider", provider, "codename", codename, "error", err)
			continue
		}
		if len(resp.Choices) == 0 {
			lastErr = errors.New("empty response received")
			continue
		}
		return resp, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no codenames available")
	}
	return openai.ChatCompletionResponse{}, lastErr
}

// callOpenCode talks to the OpenCode Zen free tier. The tier requires a
// "read" gate tool in every request and only accepts streamed calls, so the
// response is aggregated and the gate tool is answered internally.
func (s *Server) callOpenCode(
	ctx context.Context,
	client *openai.Client,
	req openai.ChatCompletionRequest,
) (openai.ChatCompletionResponse, error) {
	current := req
	var resp openai.ChatCompletionResponse
	var err error

	for round := 0; round < openCodeMaxGateRounds; round++ {
		resp, err = opencode.CreateChatCompletion(ctx, client, current)
		if err != nil {
			return openai.ChatCompletionResponse{}, err
		}
		if len(resp.Choices) == 0 {
			return resp, errors.New("empty response received")
		}

		message := resp.Choices[0].Message
		gateCalls := gateToolCalls(message.ToolCalls)
		if len(gateCalls) == 0 {
			stripGateToolCalls(&resp)
			return resp, nil
		}

		slog.Info("opencode gate tool called, answering as unavailable")
		messages := append([]openai.ChatCompletionMessage(nil), current.Messages...)
		messages = append(messages, message)
		for i, call := range gateCalls {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    openCodeGateToolUnavailableResult,
				ToolCallID: toolCallID(call, i),
			})
		}
		current.Messages = messages
	}

	stripGateToolCalls(&resp)
	return resp, errors.New("opencode gate tool was called too many times")
}

// gateToolCalls returns the tool calls that target the OpenCode gate tool.
func gateToolCalls(calls []openai.ToolCall) []openai.ToolCall {
	var gate []openai.ToolCall
	for _, call := range calls {
		if call.Function.Name == opencode.GateToolName {
			gate = append(gate, call)
		}
	}
	return gate
}

// stripGateToolCalls removes the internal gate tool calls from a response.
func stripGateToolCalls(resp *openai.ChatCompletionResponse) {
	for i := range resp.Choices {
		calls := resp.Choices[i].Message.ToolCalls
		kept := calls[:0]
		for _, call := range calls {
			if call.Function.Name == opencode.GateToolName {
				continue
			}
			kept = append(kept, call)
		}
		if len(kept) == 0 {
			resp.Choices[i].Message.ToolCalls = nil
			continue
		}
		resp.Choices[i].Message.ToolCalls = kept
	}
}

// toolCallID returns the id of a tool call, generating one when the upstream
// did not provide any.
func toolCallID(call openai.ToolCall, index int) string {
	if call.ID != "" {
		return call.ID
	}
	return fmt.Sprintf("call_%d", index)
}

// retryDelay returns the backoff before the given retry attempt.
func retryDelay(attempt int) time.Duration {
	delay := time.Duration(1<<attempt) * 500 * time.Millisecond
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	return delay
}

// isRetryable reports whether an upstream error is worth retrying.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case http.StatusTooManyRequests, http.StatusInternalServerError,
			http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	return true
}

// classifyFailure converts an upstream error into a routing failure weight.
func classifyFailure(err error) model.ProviderFailure {
	const cooldown = 30 * time.Minute
	if err == nil || errors.Is(err, context.Canceled) {
		return model.ProviderFailure{}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return model.ProviderFailure{Weight: 1, Cooldown: cooldown}
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return model.ProviderFailure{Weight: 3, Cooldown: cooldown}
		case http.StatusTooManyRequests:
			return model.ProviderFailure{Weight: 2, Cooldown: cooldown}
		default:
			if apiErr.HTTPStatusCode >= http.StatusInternalServerError {
				return model.ProviderFailure{Weight: 1, Cooldown: cooldown}
			}
			if apiErr.HTTPStatusCode >= http.StatusBadRequest {
				return model.ProviderFailure{Weight: 3, Cooldown: cooldown}
			}
		}
	}
	return model.ProviderFailure{Weight: 1, Cooldown: cooldown}
}

// knownRequestFields contains the JSON names of every field of
// openai.ChatCompletionRequest.
var knownRequestFields = func() map[string]bool {
	known := map[string]bool{}
	t := reflect.TypeOf(openai.ChatCompletionRequest{})
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			continue
		}
		known[name] = true
	}
	return known
}()

// extraParams returns the request parameters that are not part of the
// OpenAI chat completion schema. They are forwarded to the provider verbatim.
func extraParams(raw map[string]json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	extra := map[string]any{}
	for key, value := range raw {
		if knownRequestFields[key] {
			continue
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			continue
		}
		extra[key] = decoded
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

// reasoningKeys are the request parameters that configure reasoning.
var reasoningKeys = []string{"reasoning_effort", "reasoning", "thinking", "chat_template_kwargs"}

// clientSetReasoning reports whether the client configured reasoning itself.
func clientSetReasoning(raw map[string]json.RawMessage) bool {
	for _, key := range reasoningKeys {
		if _, ok := raw[key]; ok {
			return true
		}
	}
	return false
}

// applyReasoningDefaults configures reasoning for providers that need
// provider specific reasoning parameters. It mirrors the way the bot itself
// configures reasoning.
func applyReasoningDefaults(req *openai.ChatCompletionRequest, provider, codename string) {
	const effort = "low"

	if provider == model.ProviderGroq {
		groqEffort := "default"
		if strings.Contains(strings.ToLower(codename), "gpt-oss") {
			groqEffort = "low"
		}
		req.ReasoningEffort = groqEffort
		return
	}

	if provider == model.ProviderGoogle && codename == "gemma-4-26b-a4b-it" {
		return // thinking level argument is invalid for this specific model
	}

	if provider == model.ProviderMistral || provider == model.ProviderCerebras ||
		provider == model.ProviderNim || provider == model.ProviderGoogle {
		req.ReasoningEffort = effort
		return
	}

	enabled := true
	req.ReasoningEffort = effort
	req.Reasoning = &openai.ReasoningConfig{
		Enabled: &enabled,
		Effort:  effort,
	}
	req.Reasoning.Exclude = new(!enabled)
	req.Thinking = &openai.ThinkingConfig{Type: "enabled"}
	req.ChatTemplateKwargs = map[string]any{"enable_thinking": enabled}
	req.ProviderOptions = map[string]any{
		"zai": map[string]any{
			"thinking": req.Thinking,
		},
		"openai": map[string]any{
			"reasoningEffort": effort,
		},
		"deepseek": map[string]any{
			"thinking": req.Thinking,
		},
	}
}
