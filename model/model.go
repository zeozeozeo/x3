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
	groqToken   = os.Getenv("X3_GROQ_TOKEN")
)

const (
	azureBaseURL  = "https://models.inference.ai.azure.com"
	zjBaseURL     = "https://api.zukijourney.com/v1"
	zjRPBaseUrl   = "https://api.zukijourney.com/unf/chat/completions"
	hmBaseUrl     = "https://helixmind.online/v1"
	fresedBaseUrl = "https://fresedgpt.space/v1"
	groqBaseUrl   = "https://api.groq.com/openai/v1/chat/completions"
)

const (
	ProviderGithub      = "github"
	ProviderZukijourney = "zukijourney"
	ProviderFresed      = "fresed"
	ProviderHelixmind   = "helixmind"
	ProviderGroq        = "groq"
)

type ModelProvider struct {
	API      string
	RPApi    string // API for roleplay, if any
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
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
				Codename: "claude-3-haiku",
			},
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "claude-3-haiku-20240307",
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
				RPApi:    zjRPBaseUrl,
				Codename: "gemini-1.5-flash-latest",
			},
			ProviderFresed: {
				API:      fresedBaseUrl,
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
			ProviderGroq: {
				API:      groqBaseUrl,
				Codename: "mixtral-8x7b-32768",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
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
			ProviderGroq: {
				API:      groqBaseUrl,
				Codename: "llama-3.2-90b-vision-preview",
			},
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
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:      groqBaseUrl,
				Codename: "llama-3.2-11b-vision-preview",
			},
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
			ProviderGroq: {
				API:      groqBaseUrl,
				Codename: "llama-3.1-70b-versatile",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseUrl,
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
			ProviderGroq: {
				API:      groqBaseUrl,
				Codename: "llama-3.1-8b-instant",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseUrl,
				Codename: "llama-3.1-8b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseUrl,
				Codename: "llama-3.1-8b",
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
			ProviderZukijourney: {
				API:      zjBaseURL,
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

	ModelGemma9b = Model{
		Name:    "Google Gemma 2 9B",
		Command: "gemma9b",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:      groqBaseUrl,
				Codename: "gemma2-9b-it",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseUrl,
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
				RPApi:    zjRPBaseUrl,
				Codename: "gemma-2-27b",
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
		ModelGigaChatPro,
		ModelPhi35MoE,
		ModelPhi35Vision,
		ModelPhi35Mini,
		ModelJambaLarge,
		ModelJambaMini,
		ModelGemma9b,
		ModelGemma27b,
	}

	modelByName = map[string]Model{}

	// In order of trial
	AllProviders = []string{
		ProviderGithub,
		ProviderGroq,
		ProviderZukijourney,
		ProviderFresed,
		ProviderHelixmind,
	}
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

func (m Model) Client(provider string, rp bool) (*openai.Client, string) {
	if provider == ProviderGithub {
		// github marketplace requires special tweaks
		config := openai.DefaultAzureConfig(githubToken, m.Providers[provider].API)
		config.APIType = openai.APITypeOpenAI
		return openai.NewClientWithConfig(config), m.Providers[provider].Codename
	}

	var token string
	switch provider {
	case ProviderZukijourney:
		token = zjToken
	case ProviderFresed:
		token = fresedToken
	case ProviderHelixmind:
		token = hmToken
	case ProviderGroq:
		token = groqToken
	default:
		token = githubToken
	}

	p := m.Providers[provider]

	config := openai.DefaultConfig(token)
	if rp && p.RPApi != "" {
		config.BaseURL = p.RPApi
	} else {
		config.BaseURL = p.API
	}
	return openai.NewClientWithConfig(config), p.Codename
}
