package llm

import (
	"context"
	"errors"
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

type Llmer struct {
	Messages []openai.ChatCompletionMessage `json:"messages"`
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

func (l *Llmer) Lobotomize() {
	l.Messages = []openai.ChatCompletionMessage{}
}

func (l *Llmer) AddMessage(role, content string) {
	if len(l.Messages) > 0 && role == RoleAssistant && l.Messages[len(l.Messages)-1].Role == RoleAssistant {
		// previous message is also an assistant message, merge this
		// (this is required when x3 splits the message up into multiple parts to bypass
		// discord's 2000 character message limit)
		l.Messages[len(l.Messages)-1].Content += content
		return
	}

	msg := openai.ChatCompletionMessage{
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
	l.Messages = append([]openai.ChatCompletionMessage{{
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
		return
	}
	if len(msg.Content) != 0 {
		msg.MultiContent = append(msg.MultiContent, openai.ChatMessagePart{
			Type: openai.ChatMessagePartTypeText,
			Text: msg.Content,
		})
		msg.Content = ""
	}
	slog.Debug("adding image to message", slog.String("url", imageURL))
	msg.MultiContent = append(msg.MultiContent, openai.ChatMessagePart{
		Type: openai.ChatMessagePartTypeImageURL,
		ImageURL: &openai.ChatMessageImageURL{
			URL: imageURL,
		},
	})
}

func (l *Llmer) requestCompletionInternal(model model.Model, provider string, rp bool) (string, error) {
	slog.Debug("request completion.. message history follows..", slog.String("model", model.Name))
	for _, msg := range l.Messages {
		slog.Debug("    message", slog.String("role", msg.Role), slog.String("content", msg.Content))
	}

	client, codename := model.Client(provider, rp)
	req := openai.ChatCompletionRequest{
		Model:    codename,
		Messages: l.Messages,
		Stream:   true,
	}

	completionStart := time.Now()

	ctx, cancel := context.WithDeadline(context.Background(), completionStart.Add(20*time.Second))
	defer cancel()

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	var text strings.Builder
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return text.String(), err
		}
		if len(response.Choices) == 0 {
			slog.Warn("empty response", slog.Any("response", response))
			continue
		}
		text.WriteString(response.Choices[0].Delta.Content)
	}

	slog.Debug("response", slog.String("text", text.String()), slog.Duration("duration", time.Since(completionStart)))

	l.Messages = append(l.Messages, openai.ChatCompletionMessage{
		Role:    RoleAssistant,
		Content: text.String(),
	})
	return text.String(), nil
}

func (l *Llmer) RequestCompletion(m model.Model, rp bool) (res string, err error) {
	for _, provider := range model.AllProviders {
		if _, ok := m.Providers[provider]; !ok {
			continue
		}
		slog.Debug("requesting completion", slog.String("provider", provider))

		res, err = l.requestCompletionInternal(m, provider, rp)
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
