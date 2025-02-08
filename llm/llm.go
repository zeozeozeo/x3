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

func (l Llmer) convertMessages(hasVision bool, isLlama bool) []openai.ChatCompletionMessage {
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
	return messages
}

func (l Llmer) estimateUsage(m model.Model) Usage {
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
			switch msg.Role {
			case RoleSystem:
				fallthrough
			case RoleUser:
				usage.PromptTokens += len(ids)
				if len(msg.Images) > 0 {
					numImages = len(msg.Images)
				}
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
) (string, Usage, error) {
	req := openai.ChatCompletionRequest{
		Model: codename,
		// google api doesn't support image URIs, WTF google?
		Messages: l.convertMessages(m.Vision && provider != model.ProviderGoogle, m.IsLlama),
		Stream:   true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
		Temperature:      settings.Temperature,
		TopP:             settings.TopP,
		FrequencyPenalty: settings.FrequencyPenalty,
		Seed:             settings.Seed,
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

	// if the api provider is retarded enough to use HTML escapes like &lt; in a fucking API,
	// strip the fuckers off
	unescaped := html.UnescapeString(text.String())
	unescaped = strings.TrimSpace(unescaped)

	// if the model is dumb enough to prepend usernames, cut them off
	if usernames == nil {
		usernames = map[string]bool{}
	}
	usernames["x3"] = true
	for username := range usernames {
		prefix := username + ": "
		for strings.HasPrefix(strings.ToLower(unescaped), strings.ToLower(prefix)) {
			unescaped = unescaped[len(prefix):]
		}
	}

	if m.Name == model.ModelLlama90b.Name || m.Name == model.ModelLlama70b.Name {
		// this model is so stupid that it often ignores the instruction to
		// not put a space before the tilde
		unescaped = strings.ReplaceAll(unescaped, " ~", "~")
		unescaped = strings.ReplaceAll(unescaped, ">///&", ">///<")
	}
	// Nous Hermes 3 is dumb, too
	unescaped = strings.TrimSuffix(unescaped, "@ [email protected]")
	unescaped = strings.TrimSuffix(unescaped, "[email protected] ;")
	unescaped = strings.TrimSuffix(unescaped, "[email protected];")
	// and trim spaces again after our checks, for good measure
	unescaped = strings.TrimSpace(unescaped)
	slog.Info("response", slog.String("text", text.String()), slog.String("unescaped", unescaped), slog.Duration("duration", time.Since(completionStart)), slog.String("model", m.Name), slog.String("provider", provider))

	l.Messages = append(l.Messages, Message{
		Role:    RoleAssistant,
		Content: unescaped,
	})

	return unescaped, usage, nil
}

func (l *Llmer) requestCompletionInternal(
	m model.Model,
	provider string,
	usernames map[string]bool,
	settings persona.InferenceSettings,
) (string, Usage, error) {
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

	client, codenames := m.Client(provider)
	if client == nil {
		return "", Usage{}, errors.New("no client")
	}

	for _, codename := range codenames {
		res, usage, err := l.requestCompletionInternal2(m, codename, provider, usernames, settings, client)
		if err == nil {
			return res, usage, nil
		}
	}

	return "", Usage{}, nil // all codenames errored, retry
}

func (l *Llmer) RequestCompletion(m model.Model, usernames map[string]bool, settings persona.InferenceSettings) (res string, usage Usage, err error) {
	for _, provider := range model.ScoreProviders() {
		retries := 0
	retry:
		if retries >= 3 {
			continue
		}
		if _, ok := m.Providers[provider.Name]; !ok {
			continue
		}
		slog.Info("requesting completion", slog.String("provider", provider.Name), slog.Int("providerErrors", provider.Errors), slog.Int("retries", retries))

		res, usage, err = l.requestCompletionInternal(m, provider.Name, usernames, settings.Fixup())
		if res == "" {
			slog.Warn("got an empty response from requestCompletionInternal", slog.String("provider", provider.Name))
			retries++
			provider.Errors++
			goto retry
		}

		if usage.IsEmpty() {
			usage = l.estimateUsage(m)
		} else if usage.ResponseTokens <= 1 {
			// unrealistic; openrouter api responds with response tokens set to 1
			estimatedUsage := l.estimateUsage(m)
			usage.ResponseTokens = estimatedUsage.ResponseTokens
		}

		slog.Info("request usage", slog.String("usage", usage.String()))
		if err == nil {
			return
		}
		slog.Warn("(provider tests) failed to request completion", slog.String("provider", provider.Name), slog.Any("err", err))
		provider.Errors++
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
