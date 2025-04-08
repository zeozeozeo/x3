package llm

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	openai "github.com/sashabaranov/go-openai"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

const (
	RoleUser      = openai.ChatMessageRoleUser
	RoleAssistant = openai.ChatMessageRoleAssistant
	RoleSystem    = openai.ChatMessageRoleSystem
)

type Message struct {
	Role    string       `json:"role"`
	Content string       `json:"content"`
	Images  []string     `json:"images"` // image URIs
	ID      snowflake.ID `json:"-"`
}

type Usage struct {
	PromptTokens   int `json:"prompt_tokens"`
	ResponseTokens int `json:"response_tokens"`
	TotalTokens    int `json:"total_tokens"`
}

func (u Usage) String() string {
	return fmt.Sprintf("Prompt: %d, Response: %d, Total: %d", u.PromptTokens, u.ResponseTokens, u.TotalTokens)
}

func (lhs Usage) Add(rhs Usage) Usage {
	return Usage{
		PromptTokens:   lhs.PromptTokens + rhs.PromptTokens,
		ResponseTokens: lhs.ResponseTokens + rhs.ResponseTokens,
		TotalTokens:    lhs.TotalTokens + rhs.TotalTokens,
	}
}

func (u Usage) IsEmpty() bool {
	return u.PromptTokens == 0 && u.ResponseTokens == 0 && u.TotalTokens == 0
}

// StreamChunk represents a piece of data received from the LLM stream.
type StreamChunk struct {
	Content string
	Err     error
	Done    bool // Indicates the stream is finished
	Usage   Usage // Sent with the final Done chunk
}

type Llmer struct {
	Messages []Message `json:"messages"`
}

func NewLlmer() *Llmer {
	return &Llmer{}
}

func (l *Llmer) NumMessages() int {
	return len(l.Messages)
}

func (l *Llmer) TruncateMessages(max int) {
	if len(l.Messages) > max {
		l.Messages = l.Messages[len(l.Messages)-max:]
	}
}

func (l *Llmer) Lobotomize(removeN int) {
	if len(l.Messages) == 0 {
		return
	}

	startIdx := 0
	if l.Messages[0].Role == RoleSystem {
		startIdx = 1
	}

	if removeN > 0 {
		endIdx := max(len(l.Messages)-removeN, startIdx)
		l.Messages = l.Messages[startIdx:endIdx]
	} else {
		// if amount <= 0, remove all messages except the system prompt
		l.Messages = l.Messages[:startIdx]
	}
}

// this is inclusive!
func (l *Llmer) LobotomizeUntilID(id snowflake.ID) {
	if len(l.Messages) == 0 {
		return
	}

	for i := len(l.Messages) - 1; i >= 0; i-- {
		if l.Messages[i].ID == id {
			if l.Messages[i].Role == RoleSystem {
				continue // don't nuke the system prompt
			}
			l.Messages = l.Messages[:i]
			return
		}
	}
}

func (l *Llmer) AddMessage(role, content string, id snowflake.ID) {
	if len(l.Messages) > 0 && role == RoleAssistant && l.Messages[len(l.Messages)-1].Role == RoleAssistant {
		// previous message is also an assistant message, merge this
		// (this is required when x3 splits the message up into multiple parts to bypass
		// discord's 2000 character message limit)
		l.Messages[len(l.Messages)-1].Content += content
		return
	}

	msg := Message{
		Role:    role,
		Content: content,
		ID:      id,
	}
	l.Messages = append(l.Messages, msg)
}

func (l *Llmer) SetPersona(persona persona.Persona) {
	// remove system prompt if there is one
	if len(l.Messages) > 0 && l.Messages[0].Role == RoleSystem {
		l.Messages = l.Messages[1:]
	}

	if len(persona.System) == 0 {
		return
	}

	// add new system prompt as the first message
	l.Messages = append([]Message{{
		Role:    RoleSystem,
		Content: persona.System,
	}}, l.Messages...)
}

// Add an image by URL to the latest message.
func (l *Llmer) AddImage(imageURL string) {
	if len(l.Messages) == 0 {
		return
	}
	msg := &l.Messages[len(l.Messages)-1]
	if msg.Role != RoleUser {
		return // some apis crash out when assistants have images
	}
	msg.Images = append(msg.Images, imageURL)
}

func (l Llmer) convertMessages(hasVision bool, isLlama bool, prepend string) []openai.ChatCompletionMessage {
	// find the index of the last message with images
	imageIdx := -1
	for i := len(l.Messages) - 1; i >= 0; i-- {
		if len(l.Messages[i].Images) > 0 {
			imageIdx = i
			break
		}
	}

	if imageIdx != len(l.Messages)-1 && hasVision && isLlama {
		// llama 3.2 doesn't support a system prompt and an image,
		// but we can't afford to remove the system prompt in every context
		// with images; and this message is not the last one, so we're not going
		// to attach old context images
		imageIdx = -1
	} else if imageIdx != -1 && len(l.Messages)-imageIdx >= 8 {
		// older than 8 messages, we can probably let it go
		imageIdx = -1
	}

	var messages []openai.ChatCompletionMessage
	for i, msg := range l.Messages {
		if msg.Content == "" && len(msg.Images) == 0 {
			continue // skip empty messages. HACK: they seem to appear after lobotomy, this is a hack
		}
		if len(msg.Images) == 0 || !hasVision || i != imageIdx {
			role := msg.Role
			if msg.Role == RoleSystem && imageIdx != -1 && isLlama && hasVision {
				// llama 3.2 doesn't support system messages with images
				// so we're going to convert the system prompt into a user message
				slog.Debug("replacing system message -> user message because of image (llama 3.2 with image)")
				role = RoleUser
			}
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    role,
				Content: msg.Content,
			})
		} else {
			slog.Debug("adding image")
			// must structure as a multipart message if we have images
			parts := []openai.ChatMessagePart{
				{
					Type: openai.ChatMessagePartTypeText,
					Text: msg.Content,
				},
			}
			/*
				for _, imageURL := range msg.Images {
					parts = append(parts, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL: imageURL,
						},
					})
				}
			*/
			// NB: most apis seem to only support one image sadly
			// we choose the first attachment
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL: msg.Images[0],
				},
			})

			messages = append(messages, openai.ChatCompletionMessage{
				Role:         msg.Role,
				MultiContent: parts,
			})
		}
	}

	if prepend != "" {
		// https://console.groq.com/docs/prefilling
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    RoleAssistant,
			Content: prepend,
		})
	}

	return messages
}

func (l Llmer) EstimateUsage(m model.Model) Usage {
	start := time.Now()
	var usage Usage
	codec := m.Tokenizer()

	var responseMsg *Message
	numImages := 0
	for _, msg := range l.Messages {
		if msg.Role == RoleAssistant {
			responseMsg = &msg
			continue
		}
		if ids, _, err := codec.Encode(msg.Content); err == nil {
			usage.PromptTokens += len(ids)
			if len(msg.Images) > 0 {
				numImages = len(msg.Images)
			}
		}
	}

	if responseMsg != nil {
		if ids, _, err := codec.Encode(responseMsg.Content); err == nil {
			usage.ResponseTokens = len(ids)
		}
	}

	usage.TotalTokens = usage.PromptTokens + usage.ResponseTokens
	slog.Debug("estimated token usage", slog.String("usage", usage.String()), slog.Duration("in", time.Since(start)), slog.Int("images", numImages))
	return usage
}

func (l *Llmer) requestCompletionInternal2(
	m model.Model,
	codename,
	provider string,
	usernames map[string]bool,
	settings persona.InferenceSettings,
	client *openai.Client,
	prepend string,
) chan StreamChunk {
	req := openai.ChatCompletionRequest{
		Model: codename,
		// google api doesn't support image URIs, WTF google?
		Messages: l.convertMessages(m.Vision && provider != model.ProviderGoogle, m.IsLlama, prepend),
		Stream:   true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
		Temperature:      settings.Temperature,
		TopP:             settings.TopP,
		FrequencyPenalty: settings.FrequencyPenalty,
		Seed:             settings.Seed,
	}

	chunkChan := make(chan StreamChunk, 1) // Buffered channel might help slightly

	go func() {
		defer close(chunkChan)
		completionStart := time.Now()
		ctx, cancel := context.WithDeadline(context.Background(), completionStart.Add(5*time.Minute))
		defer cancel()

		stream, err := client.CreateChatCompletionStream(ctx, req)
		if err != nil {
			chunkChan <- StreamChunk{Err: fmt.Errorf("failed to create stream: %w", err), Done: true}
			return
		}
		defer stream.Close()

		var finalUsage Usage
		var fullResponseBuilder strings.Builder // Keep track for logging/debugging if needed

		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				slog.Debug("LLM stream finished", slog.Duration("duration", time.Since(completionStart)), slog.String("model", m.Name), slog.String("provider", provider))
				// Send final "Done" chunk with usage
				chunkChan <- StreamChunk{Done: true, Usage: finalUsage}
				break
			}
			if err != nil {
				chunkChan <- StreamChunk{Err: fmt.Errorf("stream recv error: %w", err), Done: true}
				return
			}

			// Process Usage if available
			if response.Usage != nil {
				finalUsage = Usage{
					PromptTokens:   response.Usage.PromptTokens,
					ResponseTokens: response.Usage.CompletionTokens, // Note: This is CompletionTokens in stream
					TotalTokens:    response.Usage.TotalTokens,
				}
				// Don't send usage mid-stream, only with the final chunk
			}

			// Process Content if available
			if len(response.Choices) > 0 {
				content := response.Choices[0].Delta.Content
				if content != "" {
					fullResponseBuilder.WriteString(content) // For potential future logging/debugging
					chunkChan <- StreamChunk{Content: content}
				}
			}
		}
		// Post-stream processing (like unescaping, trimming, history add) is MOVED to the caller.
		// Logging the full response here might be too verbose for production.
		// slog.Info("full response received", slog.String("text", fullResponseBuilder.String()), slog.Duration("duration", time.Since(completionStart)), slog.String("model", m.Name), slog.String("provider", provider))

	}()

	return chunkChan
}

// AddToHistory adds the completed message content to the LLM's history.
// This should be called by the consumer after the stream is fully processed and post-processed.
func (l *Llmer) AddToHistory(content string) {
	// Note: Merging logic (lines 109-115) might need reconsideration based on how streaming is handled upstream.
	// Assuming the caller provides the final, processed content for a single assistant turn.
	l.Messages = append(l.Messages, Message{
		Role:    RoleAssistant,
		Content: content, // Use the already processed content
	})
}

func (l *Llmer) requestCompletionInternal(
	m model.Model,
	provider string,
	usernames map[string]bool,
	settings persona.InferenceSettings,
	prepend string,
) (chan StreamChunk, error) { // Return error for immediate failures before streaming
	slog.Info(
		"request completion.. message history follows..",
		slog.String("model", m.Name),
		slog.String("provider", provider),
		slog.Float64("temperature", float64(settings.Temperature)),
		slog.Float64("top_p", float64(settings.TopP)),
		slog.Float64("frequency_penalty", float64(settings.FrequencyPenalty)),
	)
	for _, msg := range l.Messages {
		slog.Info("    message", slog.String("role", msg.Role), slog.String("content", msg.Content), slog.Int("images", len(msg.Images)))
	}

	baseUrls, tokens, codenames := m.Client(provider)
	if len(baseUrls) == 0 || len(tokens) == 0 || len(codenames) == 0 {
		return nil, fmt.Errorf("no valid client configurations for provider %s", provider)
	}

	// Validate Cloudflare configuration: must have 1 base URL or matching number of URLs and tokens
	if provider == model.ProviderCloudflare && len(baseUrls) != 1 && len(baseUrls) != len(tokens) {
		return nil, fmt.Errorf("invalid Cloudflare config: must have 1 base URL or matching number of base URLs (%d) and tokens (%d)", len(baseUrls), len(tokens))
	}

	var lastErr error
	for i, baseUrl := range baseUrls {
		tokensToTry := tokens // Default: try all tokens with this base URL

		// Special Cloudflare logic: if multiple URLs match multiple tokens, use only the corresponding token for that URL index
		if provider == model.ProviderCloudflare && len(baseUrls) > 1 && len(baseUrls) == len(tokens) {
			if i < len(tokens) {
				tokensToTry = []string{tokens[i]} // Only use the token matching this specific base URL index
			} else {
				// Should not happen due to validation above, but skip defensively
				continue
			}
		}

		for _, token := range tokensToTry {
			config := openai.DefaultConfig(token)
			config.BaseURL = baseUrl
			if provider == model.ProviderGithub { // Handle Github special case
				config = openai.DefaultAzureConfig(token, baseUrl)
				config.APIType = openai.APITypeOpenAI
			}
			client := openai.NewClientWithConfig(config)

			for _, codename := range codenames {
				slog.Info("attempting request", "provider", provider, "baseUrl", baseUrl, "codename", codename)
				// requestCompletionInternal2 now returns a channel immediately.
				// Errors during stream setup are handled inside its goroutine and sent via the channel.
				// We assume setup is successful if we get a channel back.
				chunkChan := l.requestCompletionInternal2(m, codename, provider, usernames, settings, client, prepend)
				return chunkChan, nil // Return the channel on the first successful attempt
			}
		}
	}

	return nil, fmt.Errorf("all configurations for provider %s failed: %w", provider, lastErr) // all baseUrls/tokens/codenames errored
}

// RequestCompletion attempts to get a completion stream from the best available provider.
func (l *Llmer) RequestCompletion(m model.Model, usernames map[string]bool, settings persona.InferenceSettings, prepend string) (chan StreamChunk, error) {
	settings.Remap() // remap values (1.0 temp -> 0.6 temp)

	for _, provider := range model.ScoreProviders(m.Reasoning) {
		retries := 0
	retry:
		if retries >= 3 {
			continue
		}
		if _, ok := m.Providers[provider.Name]; !ok {
			continue
		}
		slog.Info("requesting completion", slog.String("provider", provider.Name), slog.Int("providerErrors", provider.Errors), slog.Int("retries", retries))

		// requestCompletionInternal now returns (chan StreamChunk, error)
		chunkChan, err := l.requestCompletionInternal(m, provider.Name, usernames, settings.Fixup(), prepend)

		// Handle immediate errors from requestCompletionInternal (e.g., config issues)
		if err != nil {
			slog.Warn("(provider tests) failed to request completion", slog.String("provider", provider.Name), slog.Any("err", err))
			provider.Errors++
			continue // Try next provider
		}

		// If we got a channel, return it immediately. Stream errors will be handled by the consumer.
		if chunkChan != nil {
			return chunkChan, nil
		}

		// If chunkChan is nil but err is also nil (shouldn't happen with current logic, but defensively check)
		slog.Warn("requestCompletionInternal returned nil channel and nil error", slog.String("provider", provider.Name))
		retries++
		provider.Errors++
		goto retry
	}

	// If we exhausted all providers without getting a channel
	finalErr := fmt.Errorf("all providers failed to initiate stream")
	slog.Error("failed to get LLM stream from any provider", slog.Any("err", finalErr)) // Log the final consolidated error

	// Censorship handling needs rethinking in streaming context.
	// Maybe check the final error from the channel consumer?
	// For now, just return the error indicating no stream could be started.
	// if len(l.Messages) > 0 {
	// 	slog.Warn("removing last message due to censorship/failure")
	// 	l.Messages = l.Messages[:len(l.Messages)-1]
	// }

	return nil, finalErr
}

// Helper function for post-processing (moved from internal func)
func PostProcessResponse(text string, usernames map[string]bool, m model.Model) string {
	// HTML unescape
	unescaped := html.UnescapeString(text)
	unescaped = strings.TrimSpace(unescaped)

	// Username prefix trimming
	if usernames == nil {
		usernames = map[string]bool{}
	}
	usernames["x3"] = true // Ensure bot's own name is checked
	for username := range usernames {
		prefix := username + ": "
		// Case-insensitive check
		if len(unescaped) >= len(prefix) && strings.EqualFold(unescaped[:len(prefix)], prefix) {
			unescaped = unescaped[len(prefix):]
			unescaped = strings.TrimSpace(unescaped) // Trim again after removal
		}
	}

	// Model-specific quirks
	if m.Name == model.ModelLlama90b.Name || m.Name == model.ModelLlama70b.Name {
		replacer := strings.NewReplacer(
			" ~", "~",
			">///&", ">///<", // Example quirk
		)
		unescaped = replacer.Replace(unescaped)
	}
	// Other model quirks
	unescaped = strings.TrimSuffix(unescaped, "@ [email protected]") // Example quirk
	unescaped = strings.TrimSuffix(unescaped, "[email protected] ;")
	unescaped = strings.TrimSuffix(unescaped, "[email protected];")

	// Final trim
	return strings.TrimSpace(unescaped)
}
