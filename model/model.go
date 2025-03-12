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
	g4fToken        = os.Getenv("X3_G4F_TOKEN")
	crofToken       = os.Getenv("X3_CROF_TOKEN")
)

const (
	azureBaseURL      = "https://models.inference.ai.azure.com"
	zjBaseURL         = "https://api.zukijourney.com/v1"
	hmBaseURL         = "https://helixmind.online/v1"
	fresedBaseURL     = "https://fresedgpt.space/v1"
	groqBaseURL       = "https://api.groq.com/openai/v1"
	googleBaseURL     = "https://generativelanguage.googleapis.com/v1beta/openai"
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	g4fBaseURL        = "http://192.168.230.44:1337/v1"
	crofBaseURL       = "https://ai.nahcrof.com/v2"
)

const (
	ProviderGithub      = "github"
	ProviderZukijourney = "zukijourney"
	ProviderFresed      = "fresed"
	ProviderHelixmind   = "helixmind"
	ProviderGroq        = "groq"
	ProviderGoogle      = "google"
	ProviderOpenRouter  = "openrouter"
	ProviderG4F         = "g4f"
	ProviderCrof        = "crof"
)

type ModelProvider struct {
	API       string
	Codenames []string
}

type Model struct {
	Name           string
	Command        string
	NeedsWhitelist bool
	// Llama 3.2 doesn't support system prompts when images are passed, so we
	// have to detect it :/
	IsLlama   bool
	Vision    bool
	Reasoning bool
	Encoding  tokenizer.Encoding
	Providers map[string]ModelProvider
}

type ScoredProvider struct {
	Name            string
	PreferReasoning bool
	Errors          int
}

var (
	ModelGpt4oMini = Model{
		Name:     "OpenAI GPT-4o mini",
		Command:  "gpt4o",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderHelixmind: {
				API:       hmBaseURL,
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"gpt-4o-mini"},
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
				API:       azureBaseURL,
				Codenames: []string{"gpt-4o"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gpt-4o"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"gpt-4o"},
			},
			ProviderHelixmind: {
				API:       hmBaseURL,
				Codenames: []string{"gpt-4o"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"gpt-4o"},
			},
		},
	}

	ModelClaude3Haiku = Model{
		Name:    "Anthropic Claude 3 Haiku",
		Command: "haiku",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"claude-3-haiku"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"claude-3-haiku-20240307"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"claude-3-haiku"},
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:    "Google Gemini 2.0 Flash",
		Command: "gemini",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				API:       googleBaseURL,
				Codenames: []string{"gemini-2.0-flash"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"google/gemini-2.0-flash:free"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"gemini-2.0-flash"},
			},
		},
	}

	ModelCommandRplus = Model{
		Name:    "Cohere Command R+ 104B",
		Command: "commandrplus",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"command-r-plus"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"command-r-plus"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"command-r-plus"},
			},
		},
	}

	ModelMixtral8x7b = Model{
		Name:    "Mistral Mixtral 8x7B Instruct",
		Command: "mixtral7b",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"mixtral-8x7b-32768"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"mixtral-8x7b-instruct"},
			},
		},
	}

	ModelMixtral8x22b = Model{
		Name:    "Mistral Mixtral 8x22B Instruct",
		Command: "mixtral22b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"caramelldansen-1"},
			},
		},
	}

	ModelMistralLarge = Model{
		Name:           "Mistral Large (2407) 123B",
		Command:        "mistral",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Mistral-large-2407"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"mistral-large-2407"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"mistral-large-2"},
			},
		},
	}

	ModelMistralNemo = Model{
		Name:    "Mistral Nemo 12B",
		Command: "nemo",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Mistral-Nemo"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"mistral-nemo"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"mistral-nemo-12b"},
			},
		},
	}

	ModelLlama405b = Model{
		Name:    "Meta Llama 3.1 405B Instruct",
		Command: "llama405b",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"meta-llama/llama-3.1-405b-instruct:free"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"llama-3.1-405b-instruct"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"llama-3.1-405b"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"llama-3.1-405b"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"llama3.1-405b", "llama3.1-tulu3-405b"},
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
				API:       groqBaseURL,
				Codenames: []string{"llama-3.2-90b-vision-preview"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"meta-llama/llama-3.2-90b-vision-instruct:free"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"llama-3.2-90b-instruct"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"llama-3.2-90b"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"llama-3.2-90b"},
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
				API:       groqBaseURL,
				Codenames: []string{"llama-3.2-11b-vision-preview"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"meta-llama/llama-3.2-11b-vision-instruct:free"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"llama-3.2-11b-instruct"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"llama-3.2-11b"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"llama-3.2-11b"},
			},
		},
	}

	ModelLlama70b = Model{
		Name:    "Meta Llama 3.3 70B Instruct",
		Command: "llama70b",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"llama-3.3-70b-specdec", "llama-3.3-70b-versatile"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"meta-llama/llama-3.1-70b-instruct:free"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"llama-3.3-70b-instruct"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"llama-3.3-70b"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"llama3.3-70b"},
			},
		},
	}

	ModelLlama8b = Model{
		Name:    "Meta Llama 3.1 8B Instruct",
		Command: "llama8b",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"llama-3.1-8b-instant"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"meta-llama/llama-3.1-8b-instruct:free"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"llama-3.1-8b-instruct"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"llama-3.1-8b"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"llama-3.1-8b"},
			},
		},
	}

	ModelGigaChatPro = Model{
		Name:    "Sberbank GigaChat Pro (Russian)",
		Command: "gigachatpro",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"GigaChat-Pro"},
			},
		},
	}

	ModelGemma9b = Model{
		Name:    "Google Gemma 2 9B",
		Command: "gemma9b",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"google/gemma-2-9b-it:free"},
			},
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"gemma2-9b-it"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gemma-2-9b"},
			},
		},
	}

	ModelGemma27b = Model{
		Name:    "Google Gemma 2 27B",
		Command: "gemma",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gemma-2-27b"},
			},
		},
	}

	ModelDeepSeekV3 = Model{
		Name:    "DeepSeek-V3 671B",
		Command: "deepseek",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"deepseek-chat"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"deepseek-v3"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"deepseek-v3"},
			},
		},
	}

	ModelDeepSeekR1 = Model{
		Name:      "DeepSeek R1 671B",
		Command:   "r1",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"deepseek-r1"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"deepseek/deepseek-r1:free"},
			},
		},
	}

	ModelQwQ = Model{
		Name:      "QwQ 32B",
		Command:   "qwq",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"qwq-32b"},
			},
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"qwen-qwq-32b"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"qwq-32b"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"qwen-qwq-32b"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"qwen/qwq-32b:free"},
			},
		},
	}

	ModelQwen = Model{
		Name:    "Qwen 2.5 32B",
		Command: "qwen",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"qwen-2.5-32b"},
			},
		},
	}

	ModelQwenCoder = Model{
		Name:    "Qwen 2.5 Coder 32B",
		Command: "coder",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"qwen-2.5-coder-32b"},
			},
		},
	}

	ModelRogueRose = Model{
		Name:    "Rogue Rose 103B v0.2 (RP)",
		Command: "rose",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"sophosympatheia/rogue-rose-103b-v0.2:free"},
			},
		},
	}

	ModelDolphin3Mistral = Model{
		Name:    "Dolphin3.0 Mistral 24B (RP)",
		Command: "dolphin",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"cognitivecomputations/dolphin3.0-mistral-24b:free"},
			},
		},
	}

	ModelDeepSeekR1DistillLlama70b = Model{
		Name:      "DeepSeek R1 Distill Llama 70B",
		Command:   "r1d",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"deepseek/deepseek-r1-distill-llama-70b:free"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"deepseek-r1-distill-llama-70b"},
			},
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"deepseek-r1-distill-llama-70b"},
			},
		},
	}

	AllModels = []Model{
		ModelGpt4oMini,
		ModelGpt4o,
		ModelClaude3Haiku,
		ModelGeminiFlash,
		ModelCommandRplus,
		ModelMixtral8x7b,
		ModelMixtral8x22b,
		ModelMistralLarge,
		ModelMistralNemo,
		ModelLlama405b,
		ModelLlama90b,
		ModelLlama70b,
		ModelLlama11b,
		ModelLlama8b,
		ModelRogueRose,
		ModelGigaChatPro,
		ModelGemma9b,
		ModelGemma27b,
		ModelDeepSeekR1,
		ModelDeepSeekV3,
		ModelQwQ,
		ModelQwen,
		ModelDeepSeekR1DistillLlama70b,
		ModelDolphin3Mistral,
	}

	modelByName = map[string]Model{}

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		{Name: ProviderGithub},
		{Name: ProviderGoogle},
		{Name: ProviderGroq},
		{Name: ProviderCrof, PreferReasoning: true}, // above groq when reasoning
		{Name: ProviderZukijourney},
		{Name: ProviderOpenRouter},
		{Name: ProviderFresed},
		{Name: ProviderHelixmind},
		//{Name: ProviderG4F},
	}

	lastScoreReset = time.Now()
)

func resetProviderScore() {
	for i, p := range allProviders {
		p.Errors = i
	}
}

func getErrors(p *ScoredProvider, reasoning bool) int {
	if p == nil {
		return 0
	}
	if reasoning && p.PreferReasoning {
		return -1
	}
	return p.Errors
}

func ScoreProviders(reasoning bool) []*ScoredProvider {
	if time.Since(lastScoreReset) > 5*time.Minute {
		resetProviderScore()
		lastScoreReset = time.Now()
	}
	providers := allProviders
	sort.Slice(providers, func(i, j int) bool {
		return getErrors(providers[i], reasoning) < getErrors(providers[j], reasoning)
	})
	return providers
}

func init() {
	resetProviderScore()
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

func (m Model) Client(provider string) (*openai.Client, []string) {
	if provider == ProviderGithub {
		// github marketplace requires special tweaks
		config := openai.DefaultAzureConfig(githubToken, m.Providers[provider].API)
		config.APIType = openai.APITypeOpenAI
		return openai.NewClientWithConfig(config), m.Providers[provider].Codenames
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
	case ProviderG4F:
		token = g4fToken
	case ProviderCrof:
		token = crofToken
	default:
		token = githubToken
	}

	p := m.Providers[provider]

	config := openai.DefaultConfig(token)
	config.BaseURL = p.API
	return openai.NewClientWithConfig(config), p.Codenames
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
