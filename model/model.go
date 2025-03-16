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
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gemini-2.0-flash"},
			},
		},
	}

	ModelCommandRplus = Model{
		Name:    "Cohere Command R+ 104B",
		Command: "commandr",
		Providers: map[string]ModelProvider{
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"command-r-plus"},
			},
			ProviderG4F: {
				API:       g4fBaseURL,
				Codenames: []string{"command-r-plus"},
			},
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Cohere-command-r-plus-08-2024"},
			},
		},
	}

	ModelMistralLarge = Model{
		Name:    "Mistral Large 123B",
		Command: "mistral",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Mistral-Large-2411"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"mistral-large"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"mistral-large"},
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

	ModelMistralSaba = Model{
		Name:    "Mistral Saba 24B",
		Command: "saba",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"mistral-saba-24b"},
			},
		},
	}

	ModelLlama405b = Model{
		Name:    "Meta Llama 3.1 405B",
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
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Meta-Llama-3.1-405B-Instruct"},
			},
		},
	}

	ModelLlama90b = Model{
		Name:    "Meta Llama 3.2 90B",
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
		Name:    "Meta Llama 3.2 11B",
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
		Name:    "Meta Llama 3.3 70B",
		Command: "llama",
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
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Llama-3.3-70B-Instruct"},
			},
		},
	}

	ModelLlama8b = Model{
		Name:    "Meta Llama 3.1 8B",
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
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gemma-2-9b"},
			},
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"gemma2-9b-it"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"gemma-2-9b-it"},
			},
			ProviderGoogle: {
				API:       googleBaseURL,
				Codenames: []string{"gemma-2-9b-it"},
			},
		},
	}

	ModelGemma27b = Model{
		Name:    "Google Gemma 3 27B",
		Command: "gemma",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"google/gemma-3-27b-it:free"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"gemma-3-27b-it"},
			},
			ProviderGoogle: {
				API:       googleBaseURL,
				Codenames: []string{"gemma-3-27b-it"},
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
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"DeepSeek-V3"},
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
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"DeepSeek-R1"},
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
		Name:    "Qwen2.5 32B",
		Command: "qwen",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"qwen-2.5-32b"},
			},
		},
	}

	ModelQwenCoder = Model{
		Name:    "Qwen2.5 Coder 32B",
		Command: "coder",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"qwen-2.5-coder-32b"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"qwen/qwen-2.5-coder-32b-instruct:free"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
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

	ModelDolphin3R1Mistral = Model{
		Name:      "Dolphin3.0 R1 Mistral 24B (RP)",
		Command:   "r1d",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"cognitivecomputations/dolphin3.0-r1-mistral-24b:free"},
			},
		},
	}

	ModelDeepSeekR1DistillLlama70b = Model{
		Name:      "DeepSeek R1 Distill Llama 70B",
		Command:   "r1l",
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

	ModelDeepSeekR1DistillQwen32b = Model{
		Name:      "DeepSeek R1 Distill Qwen 32B",
		Command:   "r1q",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"deepseek/deepseek-r1-distill-qwen-32b:free"},
			},
			ProviderGroq: {
				API:       groqBaseURL,
				Codenames: []string{"deepseek-r1-distill-qwen-32b"},
			},
			ProviderCrof: {
				API:       crofBaseURL,
				Codenames: []string{"deepseek-r1-distill-qwen-32b"},
			},
		},
	}

	ModelRekaFlash3 = Model{
		Name:      "Reka Flash 3",
		Command:   "reka",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"rekaai/reka-flash-3:free"},
			},
		},
	}

	ModelGeminiFlashThinking = Model{
		Name:      "Gemini 2.0 Flash Thinking",
		Command:   "thinking",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				API:       googleBaseURL,
				Codenames: []string{"gemini-2.0-flash-thinking-exp-01-21"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"gemini-2.0-flash-thinking-exp"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gemini-2.0-flash-thinking-exp-01-21"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"google/gemini-2.0-flash-thinking-exp:free"},
			},
		},
	}

	ModelGeminiPro = Model{
		Name:           "Gemini 2.0 Pro",
		Command:        "pro",
		NeedsWhitelist: true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				API:       googleBaseURL,
				Codenames: []string{"gemini-2.0-pro-exp-02-05"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"gemini-2.0-pro-exp-02-05"},
			},
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"google/gemini-2.0-pro-exp-02-05:free"},
			},
		},
	}

	ModelPhi4 = Model{
		Name:    "Microsoft Phi-4 14B",
		Command: "phi",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				API:       azureBaseURL,
				Codenames: []string{"Phi-4"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"phi-4"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"phi-4"},
			},
		},
	}

	ModelOlympicCoder32b = Model{
		Name:      "Open-R1 OlympicCoder 32B",
		Command:   "olympic",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"open-r1/olympiccoder-32b:free"},
			},
		},
	}

	ModelQwenVL72b = Model{
		Name:    "Qwen2.5-VL 72B",
		Command: "vl",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"qwen/qwen2.5-vl-72b-instruct:free"},
			},
		},
	}

	ModelQwen72b = Model{
		Name:    "Qwen2.5 72B",
		Command: "qwen72b",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"qwen/qwen-2.5-72b-instruct:free"},
			},
			ProviderZukijourney: {
				API:       zjBaseURL,
				Codenames: []string{"qwen2.5-72b-instruct"},
			},
			ProviderFresed: {
				API:       fresedBaseURL,
				Codenames: []string{"qwen-2.5-72b"},
			},
		},
	}

	ModelMoonlight16bA3b = Model{
		Name:    "Moonshot AI Moonlight 16B A3B",
		Command: "moonlight",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"moonshotai/moonlight-16b-a3b-instruct:free"},
			},
		},
	}

	ModelToppyM7b = Model{
		Name:    "Toppy M 7B",
		Command: "toppy",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"undi95/toppy-m-7b:free"},
			},
		},
	}

	ModelMythoMax13b = Model{
		Name:    "MythoMax 13B",
		Command: "mytho",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				API:       openRouterBaseURL,
				Codenames: []string{"gryphe/mythomax-l2-13b:free"},
			},
		},
	}

	AllModels = []Model{
		ModelGpt4oMini,           // gptslop
		ModelGpt4o,               // too expensive gptslop
		ModelGeminiFlash,         // this is insanely bad for coding
		ModelGeminiFlashThinking, // better for creative writing, probably even worse for coding
		ModelGeminiPro,           // too expensive
		ModelMistralLarge,
		ModelMistralNemo,
		ModelMistralSaba,
		ModelLlama405b, // unstable api
		ModelLlama90b,  // very bad vision capabilities
		ModelLlama70b,  // default - fastest with specdec, mostly uncensored, good for RP
		ModelLlama11b,  // even worse vision capabilities
		ModelLlama8b,
		ModelRogueRose, // good RP model
		ModelGemma9b,
		ModelGemma27b,   // this is balls
		ModelDeepSeekR1, // groq often cuts off the response
		ModelDeepSeekV3, // pretty good but slow
		ModelQwQ,        // groq often cuts off the response
		ModelQwen,       // very good and fast, default qwen model
		ModelQwen72b,    // is this different from qwen2.5-max? idk
		ModelQwenVL72b,
		ModelDeepSeekR1DistillLlama70b, // slightly better than qwq at writing
		ModelCommandRplus,              // unstable api, good RAG
		ModelDolphin3Mistral,           // fully uncensored, good
		// discord menu cutoff (25) - only useless models should go below this
		ModelDolphin3R1Mistral,        // pretty bad compared to the llama 70b distill
		ModelDeepSeekR1DistillQwen32b, // useless when qwq is available
		ModelRekaFlash3,               // only good for english, worse than qwq
		ModelPhi4,                     // synthetically trained microsoft slop
		ModelOlympicCoder32b,          // marginally better than qwq
		ModelMythoMax13b,              // ancient llama 2 finetune used by chub.ai
		ModelGigaChatPro,              // this is a joke
		ModelMoonlight16bA3b,          // insanely bad model
		ModelToppyM7b,                 // this is really fucking bad

		// TODO:
		//ModelClaude3Haiku, // unstable api
	}

	modelByName = map[string]Model{}

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		{Name: ProviderGroq},
		{Name: ProviderGithub},
		{Name: ProviderGoogle},
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
	if time.Since(lastScoreReset) > 15*time.Minute {
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
