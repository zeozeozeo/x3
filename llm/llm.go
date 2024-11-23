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
	githubToken = os.Getenv("X3_GITHUB_TOKEN")
	zjToken     = os.Getenv("X3_ZJ_TOKEN")
	hmToken     = os.Getenv("X3_HM_TOKEN")
)

const (
	azureBaseURL = "https://models.inference.ai.azure.com"
	zjBaseURL    = "https://api.zukijourney.com/v1"
	hmBaseUrl    = "https://helixmind.online/v1"
)

const (
	RoleUser      = openai.ChatMessageRoleUser
	RoleAssistant = openai.ChatMessageRoleAssistant
)

const (
	ProviderGithub      = "github"
	ProviderZukijourney = "zukijourney"
	ProviderHelixmind   = "helixmind"
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
		Name:    "OpenAI GPT-4o mini",
		Command: "gpt4o",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "gpt-4o-mini",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gpt-4o-mini",
			},
			ProviderHelixmind: {
				API:      hmBaseUrl,
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
			ProviderHelixmind: {
				API:      hmBaseUrl,
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
		Name:    "Anthropic Claude 3 Haiku",
		Command: "haiku",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "claude-3-haiku",
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:    "Google Gemini 1.5 Flash",
		Command: "gemini",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemini-1.5-flash-latest",
			},
		},
	}

	ModelCommandRplus = Model{
		Name:    "Cohere Command R+ 104B",
		Command: "commandrplus",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "command-r-plus",
			},
		},
	}

	ModelMixtral8x7b = Model{
		Name:    "Mistral Mixtral 8x7B Instruct",
		Command: "mixtral7b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "mixtral-8x7b-instruct",
			},
		},
	}

	ModelMixtral8x22b = Model{
		Name:    "Mistral Mixtral 8x22B Instruct",
		Command: "mixtral22b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "caramelldansen-1",
			},
		},
	}

	ModelMinistral3b = Model{
		Name:    "Mistral Ministral 3B",
		Command: "ministral",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Ministral-3B",
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
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "mistral-large-2407",
			},
		},
	}

	ModelMistralNemo = Model{
		Name:    "Mistral Nemo",
		Command: "nemo",
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
		Name:    "Meta Llama 3.1 405B Instruct",
		Command: "llama405b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.1-405b-instruct",
			},
			// github doesn't work for some reason
		},
	}

	ModelLlama90b = Model{
		Name:    "Meta Llama 3.2 90B Instruct",
		Command: "llama90b",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.2-90b-instruct",
			},
		},
	}

	ModelLlama11b = Model{
		Name:    "Meta Llama 3.2 11B Instruct",
		Command: "llama11b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.2-11b-instruct",
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

	ModelLlama8b = Model{
		Name:    "Meta Llama 3.1 8B Instruct",
		Command: "llama8b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.1-8b-instruct",
			},
		},
	}

	ModelYandexGPT4Pro = Model{
		Name:    "Yandex GPT-4 Pro (Russian)",
		Command: "yagpt4pro",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "YandexGPT-4-Pro",
			},
		},
	}

	ModelGigaChatPro = Model{
		Name:    "Sberbank GigaChat Pro (Russian)",
		Command: "gigachatpro",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "GigaChat-Pro",
			},
		},
	}

	ModelPhi35MoE = Model{
		Name:    "Microsoft Phi-3.5-MoE Instruct 6.6B",
		Command: "phi35moe",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Phi-3.5-MoE-instruct",
			},
		},
	}

	ModelPhi35Vision = Model{
		Name:    "Microsoft Phi-3.5-Vision 4.2B",
		Command: "phi35vision",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Phi-3.5-vision-instruct",
			},
		},
	}

	ModelPhi35Mini = Model{
		Name:    "Microsoft Phi-3.5-Mini 3.8B",
		Command: "phi35mini",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Phi-3.5-mini-instruct",
			},
		},
	}

	ModelJambaLarge = Model{
		Name:           "AI21 Jamba 1.5 Large 94B active/398B total",
		Command:        "jambalarge",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "AI21-Jamba-1.5-Large",
			},
		},
	}

	ModelJambaMini = Model{
		Name:    "AI21 Jamba 1.5 Mini 12B active/52B total",
		Command: "jamba",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "AI21-Jamba-1.5-Mini",
			},
		},
	}

	ModelJais30b = Model{
		Name:    "JAIS 30b Chat (Arabic)",
		Command: "jais",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "jais-30b-chat",
			},
		},
	}

	ModelGemma9b = Model{
		Name:    "Google Gemma 2 9B",
		Command: "gemma9b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemma-2-9b",
			},
		},
	}

	ModelGemma27b = Model{
		Name:    "Google Gemma 2 27B",
		Command: "gemma",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemma-2-27b",
			},
		},
	}

	ModelLiquid40b = Model{
		Name:    "Liquid LFM 40B",
		Command: "liquid",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "liquid-40b",
			},
		},
	}

	ModelYiLightning = Model{
		Name:           "01.ai Yi Lightning",
		Command:        "yi",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "yi-lightning",
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
		ModelMixtral8x22b,
		ModelMinistral3b,
		ModelMistralLarge,
		ModelMistralNemo,
		ModelLlama405b,
		ModelLlama90b,
		ModelLlama70b,
		ModelLlama11b,
		ModelLlama8b,
		ModelYandexGPT4Pro,
		ModelGigaChatPro,
		ModelPhi35MoE,
		ModelPhi35Vision,
		ModelPhi35Mini,
		ModelJambaLarge,
		ModelJambaMini,
		ModelJais30b,
		ModelGemma9b,
		ModelGemma27b,
		ModelLiquid40b,
		ModelYiLightning,
	}

	allProviders = []string{ProviderGithub, ProviderZukijourney, ProviderHelixmind}
)

func (m Model) Client(provider string) (*openai.Client, string) {
	_, hasGithub := m.Providers[ProviderGithub]
	if provider == ProviderGithub && hasGithub {
		// github marketplace
		config := openai.DefaultAzureConfig(githubToken, m.Providers[provider].API)
		config.APIType = openai.APITypeOpenAI
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	} else if provider == ProviderZukijourney {
		// zukijourney
		config := openai.DefaultConfig(zjToken)
		config.BaseURL = m.Providers[provider].API
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	} else {
		// helixmind
		config := openai.DefaultConfig(hmToken)
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
	for _, provider := range allProviders {
		if _, ok := model.Providers[provider]; !ok {
			continue
		}
		slog.Debug("requesting completion", slog.String("provider", provider))

		res, err = l.requestCompletionInternal(model, provider)
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
