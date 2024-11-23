package model

import (
	"os"

	openai "github.com/sashabaranov/go-openai"
)

var (
	githubToken = os.Getenv("X3_GITHUB_TOKEN")
	zjToken     = os.Getenv("X3_ZJ_TOKEN")
	hmToken     = os.Getenv("X3_HM_TOKEN")
	fresedToken = os.Getenv("X3_FRESED_TOKEN")
)

const (
	azureBaseURL  = "https://models.inference.ai.azure.com"
	zjBaseURL     = "https://api.zukijourney.com/v1"
	hmBaseUrl     = "https://helixmind.online/v1"
	fresedBaseUrl = "https://fresedgpt.space/v1"
)

const (
	ProviderGithub      = "github"
	ProviderZukijourney = "zukijourney"
	ProviderFresed      = "fresed"
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
			ProviderFresed: {
				API:      fresedBaseUrl,
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "gpt-4o",
			},
			ProviderHelixmind: {
				API:      hmBaseUrl,
				Codename: "gpt-4o",
			},
		},
	}

	ModelGeminiPro = Model{
		Name:    "Google Gemini 1.5 Pro",
		Command: "geminipro",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemini-1.5-pro-latest",
			},
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "gemini-1.5-pro-latest",
			},
		},
	}

	ModelGeminiExp1114 = Model{
		Name:           "Google Gemini-Exp-1114",
		Command:        "geminiexp",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "gemini-exp-1114",
			},
			ProviderFresed: {
				API:      fresedBaseUrl,
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "claude-3-haiku-20240307",
			},
		},
	}

	ModelClaude35Sonnet = Model{
		Name:           "Anthropic Claude 3.5 Sonnet",
		Command:        "sonnet",
		Vision:         true,
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "claude-3-5-sonnet-20241022",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "claude-3.5-sonnet",
			},
		},
	}

	ModelClaude3Opus = Model{
		Name:           "Anthropic Claude 3 Opus",
		Command:        "opus",
		Vision:         true,
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "claude-3-opus-20240229",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "mistral-large-2",
			},
		},
	}

	ModelMistralNemo = Model{
		Name:    "Mistral Nemo 12B",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "mistral-nemo-12b",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "llama-3.1-405b",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "llama-3.2-90b",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "llama-3.2-11b",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "llama-3.1-70b",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "llama-3.1-8b",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "phi-3.5-moe",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "phi-3.5-mini",
			},
		},
	}

	ModelJambaLarge = Model{
		Name:    "AI21 Jamba 1.5 Large 94B active/398B total",
		Command: "jambalarge",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "AI21-Jamba-1.5-Large",
			},
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "jamba-1.5-large",
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
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "jamba-1.5-mini",
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
		ModelClaude35Sonnet,
		ModelClaude3Opus,
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

	modelByName = map[string]Model{}

	AllProviders = []string{ProviderGithub, ProviderZukijourney, ProviderFresed, ProviderHelixmind}
)

func init() {
	for _, m := range AllModels {
		modelByName[m.Name] = m
	}
}

func GetModelByName(name string) Model {
	if m, ok := modelByName[name]; ok {
		return m
	}
	return ModelGpt4oMini
}

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
	} else if provider == ProviderFresed {
		// fresed
		config := openai.DefaultConfig(fresedToken)
		config.BaseURL = m.Providers[provider].API
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	} else {
		// helixmind
		config := openai.DefaultConfig(hmToken)
		config.BaseURL = m.Providers[provider].API
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	}
}
