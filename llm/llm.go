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

const (
	ProviderGithub      = "github"
	ProviderZukijourney = "zukijourney"
)

type ModelProvider struct {
	API      string
	Codename string
}

type Model struct {
	Name           string
	Command        string
	NeedsWhitelist bool
	Vision         bool
	Providers      map[string]ModelProvider
}

var (
	ModelGpt4oMini = Model{
		Name:           "OpenAI GPT-4o mini",
		Command:        "gpt4o",
		NeedsWhitelist: false,
		Vision:         true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "gpt-4o-mini",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gpt-4o-mini",
			},
		},
	}

	ModelGpt4o = Model{
		Name:           "OpenAI GPT-4o",
		Command:        "gpt4",
		NeedsWhitelist: true,
		Vision:         true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "gpt-4o",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gpt-4o",
			},
		},
	}

	ModelGeminiPro = Model{
		Name:           "Google Gemini 1.5 Pro",
		Command:        "geminipro",
		NeedsWhitelist: true,
		Vision:         true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemini-1.5-pro-latest",
			},
		},
	}

	ModelGeminiExp1114 = Model{
		Name:           "Google gemini-exp-1114",
		Command:        "geminiexp",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemini-exp-1114",
			},
		},
	}

	ModelClaude3Haiku = Model{
		Name:           "Anthropic Claude 3 Haiku",
		Command:        "haiku",
		NeedsWhitelist: false,
		Vision:         true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "claude-3-haiku",
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:           "Google Gemini 1.5 Flash",
		Command:        "gemini",
		NeedsWhitelist: false,
		Vision:         true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemini-1.5-flash-latest",
			},
		},
	}

	ModelCommandRplus = Model{
		Name:           "Cohere Command R+",
		Command:        "commandrplus",
		NeedsWhitelist: false,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "command-r-plus",
			},
		},
	}

	ModelMixtral8x7b = Model{
		Name:           "Mistral Mixtral 8x7B Instruct",
		Command:        "mixtral",
		NeedsWhitelist: false,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "mixtral-8x7b-instruct",
			},
		},
	}

	ModelMistralLarge = Model{
		Name:           "Mistral Large (2407) 123B",
		Command:        "mistral_large",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Mistral-large-2407",
			},
		},
	}

	ModelMistralNemo = Model{
		Name:           "Mistral Nemo",
		Command:        "nemo",
		NeedsWhitelist: false,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Mistral-Nemo",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "mistral-nemo",
			},
		},
	}

	ModelLlama405b = Model{
		Name:           "Meta Llama 3.1 405B Instruct",
		Command:        "llama405b",
		NeedsWhitelist: false,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.1-405b-instruct",
			},
			// github doesn't work for some reason
		},
	}

	ModelLlama90b = Model{
		Name:           "Meta Llama 3.2 90B Instruct",
		Command:        "llama90b",
		NeedsWhitelist: false,
		Vision:         true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.2-90b-instruct",
			},
		},
	}

	ModelLlama70b = Model{
		Name:    "Meta Llama 3.1 70B Instruct",
		Command: "llama70b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.1-70b-instruct",
			},
		},
	}

	ModelYandexGPT4Pro = Model{
		Name:           "Yandex GPT-4 Pro",
		Command:        "yagpt4pro",
		NeedsWhitelist: false,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "yandex-gpt-4-pro",
			},
		},
	}

	ModelGigaChatPro = Model{
		Name:           "Sberbank GigaChat Pro",
		Command:        "gigachatpro",
		NeedsWhitelist: false,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "GigaChat-Pro",
			},
		},
	}

	AllModels = []Model{
		ModelGpt4oMini,
		ModelGpt4o,
		ModelGeminiPro,
		ModelGeminiExp1114,
		ModelClaude3Haiku,
		ModelGeminiFlash,
		ModelCommandRplus,
		ModelMixtral8x7b,
		ModelMistralLarge,
		ModelMistralNemo,
		ModelLlama405b,
		ModelLlama90b,
		ModelLlama70b,
		ModelYandexGPT4Pro,
		ModelGigaChatPro,
	}
)

func (m Model) Client(provider string) (*openai.Client, string) {
	_, hasGithub := m.Providers[ProviderGithub]
	if provider == ProviderGithub && hasGithub {
		// github marketplace
		config := openai.DefaultAzureConfig(githubToken, m.Providers[provider].API)
		config.APIType = openai.APITypeOpenAI
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	} else {
		// zukijourney
		config := openai.DefaultConfig(zjToken)
		config.BaseURL = m.Providers[provider].API
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	}
}

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

func (l *Llmer) requestCompletionInternal(model Model, provider string) (string, error) {
	slog.Debug("request completion.. message history follows..", slog.String("model", model.Name))
	for _, msg := range l.Messages {
		slog.Debug("    message", slog.String("role", msg.Role), slog.String("content", msg.Content))
	}

	client, codename := model.Client(provider)
	req := openai.ChatCompletionRequest{
		Model:    codename,
		Messages: l.Messages,
		Stream:   true,
	}

	completionStart := time.Now()

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

func (l *Llmer) RequestCompletion(model Model) (res string, err error) {
	// check if has github first
	if _, ok := model.Providers[ProviderGithub]; ok {
		res, err = l.requestCompletionInternal(model, ProviderGithub)
		if err == nil {
			return
		}
		// if we're here, we have an error. continue on to try zukijourney
	}

	if _, ok := model.Providers[ProviderZukijourney]; ok {
		res, err = l.requestCompletionInternal(model, ProviderZukijourney)
		if err == nil {
			return
		}
	}

	return
}
