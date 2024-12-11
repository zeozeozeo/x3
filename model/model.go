package model

import (
	"os"
	"sort"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/tiktoken-go/tokenizer"
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
	Codename string
}

type Model struct {
	Name           string
	Command        string
	NeedsWhitelist bool
	// Llama 3.2 doesn't support system prompts when images are passed, so we
	// have to detect it :/
	IsLlama   bool
	Vision    bool
	Encoding  tokenizer.Encoding
	Providers map[string]ModelProvider
}

type ScoredProvider struct {
	Name   string
	Errors int
}

var (
	ModelGpt4oMini = Model{
		Name:     "OpenAI GPT-4o mini",
		Command:  "gpt4o",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
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
		Encoding:       tokenizer.O200kBase,
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
				Codename: "gemini-1.5-pro-latest",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "gemini-1.5-pro-latest",
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
				API:      fresedBaseURL,
				Codename: "claude-3-haiku-20240307",
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:    "Google Gemini 2.0 Flash",
		Command: "gemini",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				API:      googleBaseURL,
				Codename: "gemini-2.0-flash-exp",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "google/gemini-2.0-flash-exp:free",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "gemini-2.0-flash-exp",
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
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.1-405b-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
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
		IsLlama: true,
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
		IsLlama: true,
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
		Name:    "Meta Llama 3.3 70B Instruct",
		Command: "llama70b",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:      groqBaseURL,
				Codename: "llama-3.3-70b-specdec",
			},
			ProviderOpenRouter: {
				API:      openRouterBaseURL,
				Codename: "meta-llama/llama-3.1-70b-instruct:free",
			},
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "llama-3.3-70b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "llama-3.3-70b",
			},
		},
	}

	ModelLlama8b = Model{
		Name:    "Meta Llama 3.1 8B Instruct",
		Command: "llama8b",
		IsLlama: true,
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
				Codename: "llama-3.1-8b-instruct",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "llama-3.1-8b",
			},
		},
	}

	ModelGrok2 = Model{
		Name:           "xAI Grok-2",
		Command:        "grok2",
		Vision:         true,
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "grok-2",
			},
			ProviderFresed: {
				API:      fresedBaseURL,
				Codename: "grok-2",
			},
		},
	}

	ModelGrok2Mini = Model{
		Name:    "xAI Grok-2 mini",
		Command: "grok",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:      zjBaseURL,
				Codename: "grok-2-mini",
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

	AllModels = []Model{
		ModelGpt4oMini,
		ModelGpt4o,
		ModelGeminiPro,
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
		ModelGrok2,
		ModelGrok2Mini,
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

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		{Name: ProviderGithub, Errors: 0},
		{Name: ProviderGoogle, Errors: 1},
		{Name: ProviderGroq, Errors: 2},
		{Name: ProviderZukijourney, Errors: 3},
		{Name: ProviderOpenRouter, Errors: 4},
		{Name: ProviderFresed, Errors: 5},
		{Name: ProviderHelixmind, Errors: 6},
	}

	lastScoreReset = time.Now()
)

func resetProviderScore() {
	for i, p := range allProviders {
		p.Errors = i
	}
}

func ScoreProviders() []*ScoredProvider {
	if time.Since(lastScoreReset) > 5*time.Minute {
		resetProviderScore()
		lastScoreReset = time.Now()
	}
	providers := allProviders
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Errors < providers[j].Errors
	})
	return providers
}

func init() {
	for _, m := range AllModels {
		modelByName[m.Name] = m
	}
}

func GetModelByName(name string) Model {
	if m, ok := modelByName[name]; ok {
		return m
	}
	return ModelLlama70b
}

func (m Model) Client(provider string) (*openai.Client, string) {
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
	config.BaseURL = p.API
	return openai.NewClientWithConfig(config), p.Codename
}

func (m Model) Tokenizer() tokenizer.Codec {
	encoding := tokenizer.Cl100kBase
	if m.Encoding != "" {
		encoding = m.Encoding
	}
	codec, err := tokenizer.Get(encoding)
	if err != nil {
		panic(err)
	}
	return codec
}
