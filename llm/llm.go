package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

var (
	githubToken = os.Getenv("X3ZEO_GITHUB_TOKEN")
	zjToken     = os.Getenv("X3ZEO_ZJ_TOKEN")
)

const (
	azureBaseURL = "https://models.inference.ai.azure.com"
	zjBaseURL    = "https://api.zukijourney.com/v1"
)

const (
	RoleUser      = openai.ChatMessageRoleUser
	RoleAssistant = openai.ChatMessageRoleAssistant
)

type Model struct {
	// Model name
	Name string
	// API base
	API string
}

const (
	ModelGpt4o                    = "gpt-4o"
	ModelMistralNemo              = "Mistral-Nemo"
	ModelCohereCommandR082024     = "Cohere-command-r-08-2024"
	ModelLlama11bVision           = "Llama-3.2-11B-Vision-Instruct"
	ModelGpt4oMini                = "gpt-4o-mini"
	ModelLlama405b                = "Meta-Llama-3.1-405B-Instruct"
	ModelMistralLarge             = "Mistral-large-2407"
	ModelCohereCommandRPlus082024 = "Cohere-command-r-plus-08-2024"
	ModelLlama90bVision           = "Llama-3.2-90B-Vision-Instruct"
)

type Llmer struct {
	Messages []openai.ChatCompletionMessage `json:"messages"`
}

func NewLlmer() *Llmer {
	return &Llmer{}
}

func UnmarshalLlmer(data []byte) (*Llmer, error) {
	var llmer Llmer
	err := json.Unmarshal(data, &llmer)
	return &llmer, err
}

func newClient() *openai.Client {
	config := openai.DefaultAzureConfig(githubToken, azureBaseURL)
	config.APIType = openai.APITypeOpenAI
	return openai.NewClientWithConfig(config)
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
	msg := openai.ChatCompletionMessage{
		Role:    role,
		Content: content,
	}
	l.Messages = append(l.Messages, msg)
}

// Add an image by URL to the latest message.
func (l *Llmer) AddImage(imageURL string) {
	if len(l.Messages) == 0 {
		return
	}
	msg := &l.Messages[len(l.Messages)-1]
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

func (l *Llmer) RequestCompletion(model string) (string, error) {
	//return "BOT TESTING", nil

	slog.Debug("request completion.. message history follows..", slog.String("model", model))
	for _, msg := range l.Messages {
		slog.Debug("    message", slog.String("role", msg.Role), slog.String("content", msg.Content))
	}

	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: l.Messages,
		Stream:   true,
	}

	completionStart := time.Now()
	client := newClient()

	stream, err := client.CreateChatCompletionStream(context.Background(), req)
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
		text.WriteString(response.Choices[0].Delta.Content)
	}

	slog.Debug("response", slog.String("text", text.String()), slog.Duration("duration", time.Since(completionStart)))

	l.Messages = append(l.Messages, openai.ChatCompletionMessage{
		Role:    RoleAssistant,
		Content: text.String(),
	})
	return text.String(), nil
}
