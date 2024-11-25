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
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images"` // image URIs
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
		endIdx := len(l.Messages) - removeN
		if endIdx < startIdx {
			endIdx = startIdx
		}
		l.Messages = l.Messages[startIdx:endIdx]
	} else {
		// if amount <= 0, remove all messages except the system prompt
		l.Messages = l.Messages[:startIdx]
	}
}

func (l *Llmer) AddMessage(role, content string) {
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
	}
	l.Messages = append(l.Messages, msg)
}

func (l *Llmer) SetPersona(persona persona.Persona) {
	if len(persona.System) == 0 {
		return
	}

	// remove system prompt if there is one
	if len(l.Messages) > 0 && l.Messages[0].Role == RoleSystem {
		l.Messages = l.Messages[1:]
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

func (l Llmer) convertMessages(hasVision bool) []openai.ChatCompletionMessage {
	var messages []openai.ChatCompletionMessage
	for _, msg := range l.Messages {
		if len(msg.Images) == 0 || !hasVision {
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		} else {
			// must structure as a multipart message if we have images
			parts := []openai.ChatMessagePart{
				{
					Type: openai.ChatMessagePartTypeText,
					Text: msg.Content,
				},
			}
			for _, imageURL := range msg.Images {
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{
						URL: imageURL,
					},
				})
			}

			messages = append(messages, openai.ChatCompletionMessage{
				Role:         msg.Role,
				MultiContent: parts,
			})
		}
	}
	return messages
}

func (l Llmer) estimateUsage(m model.Model) Usage {
	start := time.Now()
	var usage Usage
	codec := m.Tokenizer()
	for _, msg := range l.Messages {
		if ids, _, err := codec.Encode(msg.Content); err == nil {
			switch msg.Role {
			case RoleSystem:
				fallthrough
			case RoleUser:
				usage.PromptTokens += len(ids)
			default:
				usage.ResponseTokens = len(ids) // NB: not +=!
			}
		}
	}
	usage.TotalTokens = usage.PromptTokens + usage.ResponseTokens
	slog.Debug("estimated token usage", slog.String("usage", usage.String()), slog.Duration("in", time.Since(start)))
	return usage
}

func (l *Llmer) requestCompletionInternal(m model.Model, provider string, rp bool) (string, Usage, error) {
	slog.Debug("request completion.. message history follows..", slog.String("model", m.Name))
	for _, msg := range l.Messages {
		slog.Debug("    message", slog.String("role", msg.Role), slog.String("content", msg.Content))
	}

	client, codename := m.Client(provider, rp)
	req := openai.ChatCompletionRequest{
		Model: codename,
		// google api doesn't support image URIs, WTF google?
		Messages: l.convertMessages(m.Vision && provider != model.ProviderGoogle),
		Stream:   true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	completionStart := time.Now()

	ctx, cancel := context.WithDeadline(context.Background(), completionStart.Add(20*time.Second))
	defer cancel()

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", Usage{}, err
	}
	defer stream.Close()

	var text strings.Builder
	usage := Usage{}

	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return text.String(), Usage{}, err
		}
		if response.Usage != nil {
			usage = Usage{
				PromptTokens:   response.Usage.PromptTokens,
				ResponseTokens: response.Usage.CompletionTokens,
				TotalTokens:    response.Usage.TotalTokens,
			}
		}
		if len(response.Choices) == 0 {
			slog.Warn("empty response", slog.Any("response", response))
			continue
		}
		text.WriteString(response.Choices[0].Delta.Content)
	}

	slog.Debug("response", slog.String("text", text.String()), slog.Duration("duration", time.Since(completionStart)))

	// if the api provider is retarded enough to use HTML escapes like &lt; in a fucking API,
	// strip the fuckers off
	unescaped := html.UnescapeString(text.String())
	// if the model is dumb enough to respond with `x3: `, cut it off here
	unescaped = strings.TrimPrefix(unescaped, "x3: ")

	l.Messages = append(l.Messages, Message{
		Role:    RoleAssistant,
		Content: unescaped,
	})

	return unescaped, usage, nil
}

func (l *Llmer) RequestCompletion(m model.Model, rp bool) (res string, usage Usage, err error) {
	for _, provider := range model.AllProviders {
		if _, ok := m.Providers[provider]; !ok {
			continue
		}
		slog.Info("requesting completion", slog.String("provider", provider))

		res, usage, err = l.requestCompletionInternal(m, provider, rp)

		if usage.IsEmpty() {
			usage = l.estimateUsage(m)
		}

		slog.Info("request usage", slog.String("usage", usage.String()))
		if err == nil {
			return
		}
		slog.Warn("(provider tests) failed to request completion", slog.String("provider", provider), slog.Any("err", err))
	}

	// If we're here, we're probably censored
	// Not good, especially for a bot that deals with random conversations, which is what we are
	// so remove the latest message
	if len(l.Messages) > 0 {
		slog.Warn("removing last message due to censorship")
		l.Messages = l.Messages[:len(l.Messages)-1]
	}

	return
}
