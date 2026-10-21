package openaiapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
)

// chatStream is a stream of chat completion chunks.
type chatStream interface {
	Recv() (openai.ChatCompletionStreamResponse, error)
	Close() error
}

// sdkStream adapts the vendored OpenAI SDK stream.
type sdkStream struct {
	stream *openai.ChatCompletionStream
}

func (s *sdkStream) Recv() (openai.ChatCompletionStreamResponse, error) {
	return s.stream.Recv()
}

func (s *sdkStream) Close() error {
	return s.stream.Close()
}

// chunkStream replays a fixed list of chunks. It is used for providers that
// cannot be streamed directly, like the OpenCode Zen free tier.
type chunkStream struct {
	chunks []openai.ChatCompletionStreamResponse
	index  int
}

func (s *chunkStream) Recv() (openai.ChatCompletionStreamResponse, error) {
	if s.index >= len(s.chunks) {
		return openai.ChatCompletionStreamResponse{}, io.EOF
	}
	chunk := s.chunks[s.index]
	s.index++
	return chunk, nil
}

func (s *chunkStream) Close() error {
	return nil
}

// streamChatCompletion runs a streaming completion through the provider
// router and relays the chunks to the client as server-sent events.
func (s *Server) streamChatCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	m model.Model,
	req openai.ChatCompletionRequest,
	extra map[string]any,
	clientReasoning bool,
) {
	providers := providerOrder(m)
	if len(providers) == 0 {
		writeError(w, http.StatusServiceUnavailable, errTypeAPI, "no_providers",
			"No providers are configured for model "+m.Command+".")
		return
	}

	var lastErr error
	for _, provider := range providers {
		start := time.Now()
		stream, err := s.openStream(ctx, m, provider.Name, req, extra, clientReasoning)
		if err == nil {
			model.RecordProviderSuccess(m, provider.Name, time.Since(start))
			defer stream.Close()
			s.relayStream(w, req, stream)
			return
		}

		if ctx.Err() != nil {
			break
		}
		if errors.Is(err, errNoClientConfig) {
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
	writeUpstreamError(w, lastErr)
}

// openStream opens an upstream stream for a single provider.
func (s *Server) openStream(
	ctx context.Context,
	m model.Model,
	provider string,
	req openai.ChatCompletionRequest,
	extra map[string]any,
	clientReasoning bool,
) (chatStream, error) {
	baseUrls, tokens, codenames := m.Client(provider)
	if len(baseUrls) == 0 || len(tokens) == 0 || len(codenames) == 0 {
		return nil, fmtErrNoClient(provider)
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
			for _, codename := range codenames {
				upstream := req
				upstream.Model = codename
				upstream.Extra = extra
				upstream.Stream = true
				if m.Reasoning && !clientReasoning {
					applyReasoningDefaults(&upstream, provider, codename)
				}

				slog.Info("attempting streaming request",
					"model", m.Command, "provider", provider, "codename", codename)

				if provider == model.ProviderOpencode {
					resp, err := s.callOpenCode(ctx, client, upstream)
					if err != nil {
						lastErr = err
						continue
					}
					return &chunkStream{chunks: synthesizeChunks(resp, upstream)}, nil
				}

				stream, err := client.CreateChatCompletionStream(ctx, upstream)
				if err != nil {
					lastErr = err
					slog.Warn("streaming request failed, trying next codename",
						"model", m.Command, "provider", provider, "codename", codename, "error", err)
					continue
				}
				return &sdkStream{stream: stream}, nil
			}
			model.RecordTokenError(token)
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no codenames available for provider %s", provider)
	}
	return nil, fmt.Errorf("all configurations for provider %s failed: %w", provider, lastErr)
}

// synthesizeChunks turns an aggregated response into stream chunks.
func synthesizeChunks(resp openai.ChatCompletionResponse, req openai.ChatCompletionRequest) []openai.ChatCompletionStreamResponse {
	base := openai.ChatCompletionStreamResponse{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: resp.Created,
		Model:   resp.Model,
	}

	chunks := make([]openai.ChatCompletionStreamResponse, 0, len(resp.Choices)+1)
	for i, choice := range resp.Choices {
		chunk := base
		chunk.Choices = []openai.ChatCompletionStreamChoice{{
			Index: i,
			Delta: openai.ChatCompletionStreamChoiceDelta{
				Role:             choice.Message.Role,
				Content:          choice.Message.Content,
				ReasoningContent: choice.Message.ReasoningContent,
				Reasoning:        choice.Message.Reasoning,
				ToolCalls:        choice.Message.ToolCalls,
				Refusal:          choice.Message.Refusal,
			},
			FinishReason: choice.FinishReason,
		}}
		chunks = append(chunks, chunk)
	}

	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		usage := resp.Usage
		chunk := base
		chunk.Choices = []openai.ChatCompletionStreamChoice{}
		chunk.Usage = &usage
		chunks = append(chunks, chunk)
	}

	return chunks
}

// relayStream writes the chunks of a stream to the client.
func (s *Server) relayStream(w http.ResponseWriter, req openai.ChatCompletionRequest, stream chatStream) {
	flusher, _ := w.(http.Flusher)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if flusher != nil {
		flusher.Flush()
	}

	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			slog.Warn("stream failed", "model", req.Model, "error", err)
			writeSSEError(w, err)
			return
		}
		if chunk.Object == "" {
			chunk.Object = "chat.completion.chunk"
		}
		chunk.Model = req.Model
		if !writeSSEChunk(w, chunk) {
			return
		}
	}

	writeSSEDone(w)
}

// writeSSEChunk writes a single chunk as a server-sent event.
func writeSSEChunk(w http.ResponseWriter, chunk openai.ChatCompletionStreamResponse) bool {
	data, err := json.Marshal(chunk)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return false
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return true
}

// writeSSEError writes an error event into an open stream.
func writeSSEError(w http.ResponseWriter, err error) {
	payload := errorEnvelope{
		Error: errorBody{
			Message: err.Error(),
			Type:    errTypeAPI,
			Code:    "upstream_error",
		},
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// writeSSEDone terminates a server-sent event stream.
func writeSSEDone(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
