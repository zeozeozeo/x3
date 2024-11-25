package model

import (
	"os"

	openai "github.com/sashabaranov/go-openai"
)

var (
	githubToken     = os.Getenv("X3_GITHUB_TOKEN")
	zjToken         = os.Getenv("X3_ZJ_TOKEN")
	hmToken         = os.Getenv("X3_HM_TOKEN")
	fresedToken     = os.Getenv("X3_FRESED_TOKEN")
	groqToken       = os.Getenv("X3_GROQ_TOKEN")
	googleToken     = os.Getenv("X3_GOOGLE_AISTUDIO_TOKEN")
	openRouterToken = os.Getenv("X3_OPENROUTER_TOKEN")
)

const (
	azureBaseURL      = "https://models.inference.ai.azure.com"
	zjBaseURL         = "https://api.zukijourney.com/v1"
	zjRPBaseURL       = "https://api.zukijourney.com/unf"
	hmBaseURL         = "https://helixmind.online/v1"
	fresedBaseURL     = "https://fresedgpt.space/v1"
	groqBaseURL       = "https://api.groq.com/openai/v1"
	googleBaseURL     = "https://generativelanguage.googleapis.com/v1beta/openai"
	openRouterBaseURL = "https://openrouter.ai/api/v1"
)

const (
	ProviderGithub      = "github"
	ProviderZukijourney = "zukijourney"
	ProviderFresed      = "fresed"
	ProviderHelixmind   = "helixmind"
	ProviderGroq        = "groq"
	ProviderGoogle      = "google"
	ProviderOpenRouter  = "openrouter"
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
				RPApi:    zjRPBaseURL,
				Codename: "gpt-4o-mini",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "gpt-4o-mini",
			},
			ProviderHelixmind: {
				API:      hmBaseURL,
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
				RPApi:    zjRPBaseURL,
				Codename: "gpt-4o",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "gpt-4o",
			},
			ProviderHelixmind: {
				API:      hmBaseURL,
				Codename: "gpt-4o",
			},
		},
	}

	ModelGeminiPro = Model{
		Name:    "Google Gemini 1.5 Pro",
		Command: "geminipro",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				API:      googleBaseURL,
				Codename: "gemini-1.5-pro",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "google/gemini-pro-1.5-exp",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
				Codename: "gemini-1.5-pro-latest",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "gemini-1.5-pro-latest",
			},
		},
	}

	ModelGeminiExp1121 = Model{
		Name:           "Google Gemini Experimental 1121",
		Command:        "gemini_exp",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codename: "google/gemini-exp-1121:free",
				API:      openRouterBaseURL,
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
				RPApi:    zjRPBaseURL,
				Codename: "claude-3-haiku",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "claude-3-haiku-20240307",
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:    "Google Gemini 1.5 Flash",
		Command: "gemini",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				API:      googleBaseURL,
				Codename: "gemini-1.5-flash",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "google/gemini-flash-1.5-exp",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
				Codename: "gemini-1.5-flash-latest",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
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
				API:      fresedBaseURL,
				Codename: "command-r-plus",
			},
		},
	}

	ModelMixtral8x7b = Model{
		Name:    "Mistral Mixtral 8x7B Instruct",
		Command: "mixtral7b",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:      groqBaseURL,
				Codename: "mixtral-8x7b-32768",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
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
		Command:        "mistral",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:      azureBaseURL,
				Codename: "Mistral-large-2407",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
				Codename: "mistral-large-2407",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
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
				RPApi:    zjRPBaseURL,
				Codename: "mistral-nemo",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "mistral-nemo-12b",
			},
		},
	}

	ModelLlama405b = Model{
		Name:    "Meta Llama 3.1 405B Instruct",
		Command: "llama405b",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.1-405b-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
				Codename: "llama-3.1-405b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
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
				API:      groqBaseURL,
				Codename: "llama-3.2-90b-vision-preview",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.2-90b-vision-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.2-90b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
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
				API:      groqBaseURL,
				Codename: "llama-3.2-11b-vision-preview",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.2-11b-vision-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.2-11b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "llama-3.2-11b",
			},
		},
	}

	ModelLlama70b = Model{
		Name:    "Meta Llama 3.1 70B Instruct",
		Command: "llama70b",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:      groqBaseURL,
				Codename: "llama-3.1-70b-versatile",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.1-70b-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
				Codename: "llama-3.1-70b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "llama-3.1-70b",
			},
		},
	}

	ModelLlama8b = Model{
		Name:    "Meta Llama 3.1 8B Instruct",
		Command: "llama8b",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:      groqBaseURL,
				Codename: "llama-3.1-8b-instant",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.1-8b-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
				Codename: "llama-3.1-8b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "llama-3.1-8b",
			},
		},
	}

	ModelNousHermes405b = Model{
		Name:    "Nous Hermes 3 405B Instruct",
		Command: "hermes",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "nousresearch/hermes-3-llama-3.1-405b:free",
			},
		},
	}

	ModelLiquidLFM40b = Model{
		Name:    "Liquid LFM 40B MoE",
		Command: "liquid",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "liquid/lfm-40b:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "liquid-40b",
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
				API:      fresedBaseURL,
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
				API:      fresedBaseURL,
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
				API:      fresedBaseURL,
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
				API:      fresedBaseURL,
				Codename: "jamba-1.5-mini",
			},
		},
	}

	ModelGemma9b = Model{
		Name:    "Google Gemma 2 9B",
		Command: "gemma9b",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "google/gemma-2-9b-it:free",
			},
			ProviderGroq: {
				API:      groqBaseURL,
				Codename: "gemma2-9b-it",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				RPApi:    zjRPBaseURL,
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
				RPApi:    zjRPBaseURL,
				Codename: "gemma-2-27b",
			},
		},
	}

	AllModels = []Model{
		ModelGpt4oMini,
		ModelGpt4o,
		ModelGeminiPro,
		ModelClaude3Haiku,
		ModelGeminiFlash,
		ModelGeminiExp1121,
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
		ModelNousHermes405b,
		ModelLiquidLFM40b,
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
		ProviderGoogle,
		ProviderGroq,
		ProviderOpenRouter,
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
	case ProviderGoogle:
		token = googleToken
	case ProviderOpenRouter:
		token = openRouterToken
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
