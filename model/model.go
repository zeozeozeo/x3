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
	electronToken   = os.Getenv("X3_ELECTRONHUB_TOKEN")
	cablyToken      = os.Getenv("X3_CABLYAI_TOKEN")
	meowToken       = os.Getenv("X3_MEOWAPI_TOKEN")
	cfBaseURL       = os.Getenv("X3_CLOUDFLARE_API_BASE")
	cfToken         = os.Getenv("X3_CLOUDFLARE_API_TOKEN")
	cohereToken     = os.Getenv("X3_COHERE_TOKEN")
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
	electronBaseURL   = "https://api.electronhub.top/v1"
	cablyBaseURL      = "https://cablyai.com/v1"
	meowBaseURL       = "https://meow.cablyai.com/v1"
	cohereBaseURL     = "https://api.cohere.ai/compatibility/v1"
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
	ProviderElectron    = "electronhub"
	ProviderCably       = "cablyai"
	ProviderMeow        = "meowapi"
	ProviderCloudflare  = "cloudflare"
	ProviderCohere      = "cohere"
)

type ModelProvider struct {
	Codenames []string
}

type Model struct {
	Name        string
	Command     string
	Whitelisted bool
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
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderFresed: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderHelixmind: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderG4F: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderElectron: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderCably: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderMeow: {
				Codenames: []string{"gpt-4o-mini"},
			},
		},
	}

	ModelGpt4o = Model{
		Name:     "OpenAI GPT-4o",
		Command:  "gpt4",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderFresed: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderHelixmind: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderG4F: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderElectron: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderCably: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderMeow: {
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
				Codenames: []string{"claude-3-haiku"},
			},
			ProviderFresed: {
				Codenames: []string{"claude-3-haiku-20240307"},
			},
			ProviderG4F: {
				Codenames: []string{"claude-3-haiku"},
			},
			ProviderElectron: {
				Codenames: []string{"claude-3-haiku-20240307"},
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:    "Google Gemini 2.0 Flash",
		Command: "gemini",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				Codenames: []string{"gemini-2.0-flash"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"google/gemini-2.0-flash:free"},
			},
			ProviderFresed: {
				Codenames: []string{"gemini-2.0-flash"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gemini-2.0-flash"},
			},
			ProviderMeow: {
				Codenames: []string{"gemini-2.0-flash"},
			},
		},
	}

	ModelCommandRplus = Model{
		Name:    "Cohere Command R+ 104B",
		Command: "commandr",
		Providers: map[string]ModelProvider{
			ProviderFresed: {
				Codenames: []string{"command-r-plus"},
			},
			ProviderG4F: {
				Codenames: []string{"command-r-plus"},
			},
			ProviderGithub: {
				Codenames: []string{"Cohere-command-r-plus-08-2024"},
			},
			ProviderCohere: {
				Codenames: []string{"command-r-plus"},
			},
			ProviderZukijourney: {
				Codenames: []string{"command-r-plus"},
			},
		},
	}

	ModelMistralLarge = Model{
		Name:    "Mistral Large 123B",
		Command: "mistral",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"Mistral-Large-2411"},
			},
			ProviderZukijourney: {
				Codenames: []string{"mistral-large"},
			},
			ProviderFresed: {
				Codenames: []string{"mistral-large"},
			},
			ProviderElectron: {
				Codenames: []string{"mistral-large-latest"},
			},
		},
	}

	ModelMistralSmall = Model{
		Name:    "Mistral Small 3.1 24B",
		Command: "small",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"mistralai/mistral-small-3.1-24b-instruct:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"mistral-small"},
			},
			ProviderGithub: {
				Codenames: []string{"mistral-small-2503"},
			},
		},
	}

	ModelMistralNemo = Model{
		Name:    "Mistral Nemo 12B",
		Command: "nemo",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"Mistral-Nemo"},
			},
			ProviderZukijourney: {
				Codenames: []string{"mistral-nemo"},
			},
			ProviderFresed: {
				Codenames: []string{"mistral-nemo-12b"},
			},
			ProviderMeow: {
				Codenames: []string{"mistral-nemo-12b"},
			},
		},
	}

	ModelMistralSaba = Model{
		Name:    "Mistral Saba 24B (Arabic)",
		Command: "saba",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"mistral-saba-24b"},
			},
			ProviderElectron: {
				Codenames: []string{"mistral-saba-latest"},
			},
		},
	}

	ModelLlama405b = Model{
		Name:    "Meta Llama 3.1 405B",
		Command: "llama405b",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				Codenames: []string{"llama-3.1-405b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"llama-3.1-405b"},
			},
			ProviderG4F: {
				Codenames: []string{"llama-3.1-405b"},
			},
			ProviderCrof: {
				Codenames: []string{"llama3.1-405b", "llama3.1-tulu3-405b"},
			},
			ProviderGithub: {
				Codenames: []string{"Meta-Llama-3.1-405B-Instruct"},
			},
			ProviderMeow: {
				Codenames: []string{"llama-3.1-405b"},
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
				Codenames: []string{"llama-3.2-90b-vision-preview"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-3.2-90b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"llama-3.2-90b"},
			},
			ProviderG4F: {
				Codenames: []string{"llama-3.2-90b"},
			},
			ProviderElectron: {
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
				Codenames: []string{"llama-3.2-11b-vision-preview"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-3.2-11b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"llama-3.2-11b"},
			},
			ProviderG4F: {
				Codenames: []string{"llama-3.2-11b"},
			},
			ProviderElectron: {
				Codenames: []string{"llama-3.2-11b"},
			},
			ProviderMeow: {
				Codenames: []string{"llama-3.2-11b-instruct"},
			},
			ProviderCloudflare: {
				Codenames: []string{"@cf/meta/llama-3.2-11b-vision-instruct"},
			},
		},
	}

	ModelLlama70b = Model{
		Name:    "Meta Llama 3.3 70B",
		Command: "llama",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"llama-3.3-70b-specdec", "llama-3.3-70b-versatile"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"meta-llama/llama-3.1-70b-instruct:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-3.3-70b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"llama-3.3-70b"},
			},
			ProviderCrof: {
				Codenames: []string{"llama3.3-70b"},
			},
			ProviderGithub: {
				Codenames: []string{"Llama-3.3-70B-Instruct"},
			},
			ProviderElectron: {
				Codenames: []string{"llama-3.3-70b-instruct"},
			},
			ProviderMeow: {
				Codenames: []string{"llama-3.3-70b-instruct-fp8-fast"},
			},
			ProviderCloudflare: {
				Codenames: []string{"@cf/meta/llama-3.3-70b-instruct-fp8-fast"},
			},
		},
	}

	ModelLlama8b = Model{
		Name:    "Meta Llama 3.1 8B",
		Command: "llama8b",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"llama-3.1-8b-instant"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"meta-llama/llama-3.1-8b-instruct:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-3.1-8b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"llama-3.1-8b"},
			},
			ProviderG4F: {
				Codenames: []string{"llama-3.1-8b"},
			},
			ProviderCably: {
				Codenames: []string{"llama-3.1-8b-instruct"},
			},
			ProviderElectron: {
				Codenames: []string{"llama-3.1-8b"},
			},
		},
	}

	ModelGigaChatPro = Model{
		Name:    "Sberbank GigaChat Pro (Russian)",
		Command: "gigachat",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				Codenames: []string{"GigaChat-Pro"},
			},
		},
	}

	ModelGemma9b = Model{
		Name:    "Google Gemma 2 9B",
		Command: "gemma9b",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				Codenames: []string{"gemma-2-9b"},
			},
			ProviderGroq: {
				Codenames: []string{"gemma2-9b-it"},
			},
			ProviderFresed: {
				Codenames: []string{"gemma-2-9b-it"},
			},
			ProviderGoogle: {
				Codenames: []string{"gemma-2-9b-it"},
			},
		},
	}

	ModelGemma27b = Model{
		Name:    "Google Gemma 3 27B",
		Command: "gemma",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"google/gemma-3-27b-it:free"},
			},
			ProviderCrof: {
				Codenames: []string{"gemma-3-27b-it"},
			},
			ProviderGoogle: {
				Codenames: []string{"gemma-3-27b-it"},
			},
		},
	}

	ModelDeepSeekV3 = Model{
		Name:    "DeepSeek-V3 671B",
		Command: "deepseek",
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				Codenames: []string{"deepseek-chat"},
			},
			ProviderFresed: {
				Codenames: []string{"deepseek-v3"},
			},
			ProviderCrof: {
				Codenames: []string{"deepseek-v3"},
			},
			ProviderGithub: {
				Codenames: []string{"DeepSeek-V3"},
			},
			ProviderCably: {
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
				Codenames: []string{"deepseek-r1"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"deepseek/deepseek-r1:free"},
			},
			ProviderGithub: {
				Codenames: []string{"DeepSeek-R1"},
			},
			ProviderElectron: {
				Codenames: []string{"deepseek-r1"},
			},
			ProviderMeow: {
				Codenames: []string{"deepseek-r1"},
			},
		},
	}

	ModelQwQ = Model{
		Name:      "QwQ 32B",
		Command:   "qwq",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderZukijourney: {
				Codenames: []string{"qwq-32b"},
			},
			ProviderGroq: {
				Codenames: []string{"qwen-qwq-32b"},
			},
			ProviderFresed: {
				Codenames: []string{"qwq-32b"},
			},
			ProviderCrof: {
				Codenames: []string{"qwen-qwq-32b"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"qwen/qwq-32b:free"},
			},
			ProviderElectron: {
				Codenames: []string{"qwq-32b"},
			},
		},
	}

	ModelQwen = Model{
		Name:    "Qwen2.5 32B",
		Command: "qwen",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"qwen-2.5-32b"},
			},
		},
	}

	ModelQwenCoder = Model{
		Name:    "Qwen2.5 Coder 32B",
		Command: "coder",
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"qwen-2.5-coder-32b"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"qwen/qwen-2.5-coder-32b-instruct:free"},
			},
			ProviderFresed: {
				Codenames: []string{"qwen-2.5-coder-32b"},
			},
		},
	}

	ModelRogueRose = Model{
		Name:    "Rogue Rose 103B v0.2 (RP)",
		Command: "rose",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"sophosympatheia/rogue-rose-103b-v0.2:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"rogue-rose-103b-v0.2"},
			},
		},
	}

	ModelDolphin3Mistral = Model{
		Name:    "Dolphin3.0 Mistral 24B (RP)",
		Command: "dolphin",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"cognitivecomputations/dolphin3.0-mistral-24b:free"},
			},
			ProviderElectron: {
				Codenames: []string{"dolphin3.0-mistral-24b"},
			},
		},
	}

	ModelDolphin3R1Mistral = Model{
		Name:      "Dolphin3.0 R1 Mistral 24B (RP)",
		Command:   "r1d",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"cognitivecomputations/dolphin3.0-r1-mistral-24b:free"},
			},
			ProviderElectron: {
				Codenames: []string{"dolphin3.0-r1-mistral-24b"},
			},
		},
	}

	ModelDeepSeekR1DistillLlama70b = Model{
		Name:      "DeepSeek R1 Distill Llama 70B",
		Command:   "r1l",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"deepseek/deepseek-r1-distill-llama-70b:free"},
			},
			ProviderCrof: {
				Codenames: []string{"deepseek-r1-distill-llama-70b"},
			},
			ProviderGroq: {
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
				Codenames: []string{"deepseek/deepseek-r1-distill-qwen-32b:free"},
			},
			ProviderGroq: {
				Codenames: []string{"deepseek-r1-distill-qwen-32b"},
			},
			ProviderCrof: {
				Codenames: []string{"deepseek-r1-distill-qwen-32b"},
			},
			ProviderMeow: {
				Codenames: []string{"deepseek-r1-distill-qwen-32b"},
			},
			ProviderCloudflare: {
				Codenames: []string{"@cf/deepseek-ai/deepseek-r1-distill-qwen-32b"},
			},
		},
	}

	ModelRekaFlash3 = Model{
		Name:      "Reka Flash 3",
		Command:   "reka",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
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
				Codenames: []string{"gemini-2.0-flash-thinking-exp-01-21"},
			},
			ProviderFresed: {
				Codenames: []string{"gemini-2.0-flash-thinking-exp"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gemini-2.0-flash-thinking-exp-01-21"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"google/gemini-2.0-flash-thinking-exp:free"},
			},
		},
	}

	ModelGeminiPro = Model{
		Name:        "Gemini 2.0 Pro",
		Command:     "pro",
		Whitelisted: true,
		Providers: map[string]ModelProvider{
			ProviderGoogle: {
				Codenames: []string{"gemini-2.0-pro-exp-02-05"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gemini-2.0-pro-exp-02-05"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"google/gemini-2.0-pro-exp-02-05:free"},
			},
		},
	}

	ModelPhi4 = Model{
		Name:    "Microsoft Phi-4 14B",
		Command: "phi",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"Phi-4"},
			},
			ProviderZukijourney: {
				Codenames: []string{"phi-4"},
			},
			ProviderFresed: {
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
				Codenames: []string{"open-r1/olympiccoder-32b:free"},
			},
			ProviderElectron: {
				Codenames: []string{"olympiccoder-32b"},
			},
		},
	}

	ModelQwenVL72b = Model{
		Name:    "Qwen2.5-VL 72B",
		Command: "vl",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"qwen/qwen2.5-vl-72b-instruct:free"},
			},
			ProviderElectron: {
				Codenames: []string{"qwen2.5-vl-72b-instruct"},
			},
		},
	}

	ModelQwen72b = Model{
		Name:    "Qwen2.5 72B",
		Command: "qwen72b",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"qwen/qwen-2.5-72b-instruct:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"qwen2.5-72b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"qwen-2.5-72b"},
			},
		},
	}

	ModelMoonlight16bA3b = Model{
		Name:    "Moonshot AI Moonlight 16B A3B",
		Command: "moonlight",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"moonshotai/moonlight-16b-a3b-instruct:free"},
			},
		},
	}

	ModelToppyM7b = Model{
		Name:    "Toppy M 7B",
		Command: "toppy",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"undi95/toppy-m-7b:free"},
			},
			ProviderElectron: {
				Codenames: []string{"toppy-m-7b"},
			},
		},
	}

	ModelMythoMax13b = Model{
		Name:    "MythoMax 13B",
		Command: "mytho",
		Providers: map[string]ModelProvider{
			ProviderOpenRouter: {
				Codenames: []string{"gryphe/mythomax-l2-13b:free"},
			},
			ProviderElectron: {
				Codenames: []string{"mytho-max-l2-13b"},
			},
		},
	}

	ModelLunaris8b = Model{
		Name:    "Llama 3 Lunaris 8B (RP)",
		Command: "lunaris",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"l3-lunaris-8b"},
			},
		},
	}

	ModelAnubisPro105b = Model{
		Name:    "Llama 3.3 Anubis Pro 105B (RP)",
		Command: "anubis",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"anubis-pro-105b-v1"},
			},
		},
	}

	ModelLumimaid70b = Model{
		Name:    "Llama 3.1 Lumimaid 70B (RP)",
		Command: "lumimaid",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"llama-3.1-lumimaid-70b"},
			},
		},
	}

	ModelMagnum72b = Model{
		Name:    "Qwen2.5 Magnum V4 72B (RP)",
		Command: "magnum",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"magnum-v4-72b"},
			},
		},
	}

	ModelHanamiX1 = Model{
		Name:    "Llama 3.1 Hanami X1 70B (RP)",
		Command: "hanami",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"l3.1-70b-hanami-x1"},
			},
		},
	}

	ModelEva70b = Model{
		Name:    "Llama 3.3 EVA v0.1 70B (RP)",
		Command: "eva",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"eva-llama-3.33-70b-v0.1"},
			},
		},
	}

	ModelEvaQwen32b = Model{
		Name:    "Qwen2.5 EVA v0.2 32B (RP)",
		Command: "qeva",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"eva-qwen2.5-32b-v0.2"},
			},
		},
	}

	ModelEvaQwen72b = Model{
		Name:    "Qwen2.5 EVA v0.2 72B (RP)",
		Command: "evaq",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"eva-qwen2.5-72b"},
			},
		},
	}

	ModelSaigaNemo12b = Model{
		Name:    "Saiga Mistral Nemo 12B (Russian)",
		Command: "saiga",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"saiga-nemo-12b"},
			},
		},
	}

	ModelElectra70b = Model{
		Name:      "Llama 3.3 Electra R1 70B (RP)",
		Command:   "electra",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"l3.3-electra-r1-70b"},
			},
		},
	}

	ModelMai70b = Model{
		Name:      "Llama 3.3 Mai R1 70B (RP)",
		Command:   "mai",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"l3.3-cu-mai-r1-70b"},
			},
		},
	}

	ModelQwQAbliterated = Model{
		Name:        "QwQ Abliterated 32B (RP)",
		Command:     "qwqa",
		Reasoning:   true,
		Whitelisted: true, // eats tokens
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"qwq-32b-abliterated"},
			},
		},
	}

	ModelEuryale70b = Model{
		Name:    "Llama 3.3 Euryale v2.3 70B (RP)",
		Command: "euryale",
		Providers: map[string]ModelProvider{
			ProviderElectron: {
				Codenames: []string{"l3.3-70b-euryale-v2.3"},
			},
			ProviderZukijourney: {
				Codenames: []string{"euryale-70b"},
			},
		},
	}

	ModelDeepSeekR1Uncensored = Model{
		Name:      "DeepSeek R1 Uncensored 671B",
		Command:   "r1u",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderCably: {
				Codenames: []string{"deepseek-r1-uncensored"},
			},
		},
	}

	ModelO3MiniLow = Model{
		Name:      "OpenAI o3-mini-low",
		Command:   "o3",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderCably: {
				Codenames: []string{"o3-mini-low"},
			},
		},
	}

	ModelClaudeSonnet = Model{
		Name:    "Anthropic Claude 3.7 Sonnet",
		Command: "sonnet",
		Providers: map[string]ModelProvider{
			ProviderCably: {
				Codenames: []string{"claude-3-7-sonnet-20250219"},
			},
			ProviderMeow: {
				Codenames: []string{"claude-3.7-sonnet"},
			},
		},
	}

	ModelCommandA = Model{
		Name:    "Cohere Command A 111B",
		Command: "commanda",
		Providers: map[string]ModelProvider{
			ProviderCably: {
				Codenames: []string{"command-a"},
			},
			ProviderMeow: {
				Codenames: []string{"command-a"},
			},
			ProviderCohere: {
				Codenames: []string{"command-a-03-2025"},
			},
			ProviderZukijourney: {
				Codenames: []string{"command-a"},
			},
		},
	}

	ModelGpt45 = Model{
		Name:        "OpenAI GPT-4.5",
		Command:     "gpt5",
		Whitelisted: true,
		Providers: map[string]ModelProvider{
			ProviderMeow: {
				Codenames: []string{"gpt-4.5", "gpt-4.5-preview"},
			},
		},
	}

	ModelO1 = Model{
		Name:        "OpenAI o1",
		Command:     "o1",
		Whitelisted: true,
		Reasoning:   true,
		Providers: map[string]ModelProvider{
			ProviderMeow: {
				Codenames: []string{"o1"},
			},
		},
	}

	ModelGrok3 = Model{
		Name:        "xAI Grok 3",
		Command:     "grok3",
		Whitelisted: true,
		Providers: map[string]ModelProvider{
			ProviderMeow: {
				Codenames: []string{"grok-3"},
			},
			ProviderFresed: {
				Codenames: []string{"grok-3"},
			},
		},
	}

	ModelGrok2 = Model{
		Name:    "xAI Grok 2",
		Command: "grok",
		Providers: map[string]ModelProvider{
			ProviderMeow: {
				Codenames: []string{"grok-2"},
			},
			ProviderZukijourney: {
				Codenames: []string{"grok-2-1212"},
			},
			ProviderFresed: {
				Codenames: []string{"grok-2"},
			},
			ProviderElectron: {
				Codenames: []string{"grok-2"},
			},
		},
	}

	AllModels = []Model{
		ModelGpt4oMini,           // gptslop
		ModelGpt4o,               // overly expensive gptslop
		ModelGeminiFlash,         // this is insanely bad for coding
		ModelGeminiFlashThinking, // better for creative writing, probably even worse for coding
		ModelGeminiPro,           // too expensive
		ModelMistralLarge,        // uncensored; i wish i could make this the default, but it's very slow
		ModelMistralSmall,
		ModelLlama405b, // unstable api
		ModelLlama90b,  // very bad vision capabilities
		ModelLlama70b,  // default - fastest with specdec, mostly uncensored, good for RP
		ModelLlama11b,  // even worse vision capabilities
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
		ModelCommandA,
		ModelDolphin3Mistral, // fully uncensored, good
		ModelO3MiniLow,       // unstable
		ModelClaudeSonnet,
		// discord menu cutoff (25) - only useless models should go below this
		ModelMistralNemo,
		ModelLunaris8b, // best 8b rp model
		ModelAnubisPro105b,
		ModelEuryale70b, // very unstable api
		ModelLumimaid70b,
		ModelHanamiX1,
		ModelElectra70b,
		ModelMai70b,
		ModelMagnum72b,
		ModelEva70b,
		ModelEvaQwen32b,
		ModelEvaQwen72b,
		ModelDolphin3R1Mistral,        // pretty bad compared to the llama 70b distill
		ModelDeepSeekR1DistillQwen32b, // useless when qwq is available
		ModelRekaFlash3,               // only good for english, worse than qwq
		ModelPhi4,                     // synthetically trained microsoft slop
		ModelOlympicCoder32b,          // marginally better than qwq
		ModelMythoMax13b,              // ancient llama 2 finetune used by chub.ai
		ModelGigaChatPro,              // this is a joke
		ModelSaigaNemo12b,
		ModelMoonlight16bA3b, // insanely bad model
		ModelToppyM7b,        // this is really fucking bad
		ModelQwQAbliterated,
		ModelDeepSeekR1Uncensored,
		ModelCommandRplus,
		ModelGpt45,
		ModelO1,
		ModelGrok2,
		ModelGrok3,
		ModelMistralSaba,
		ModelLlama8b,

		// TODO:
		//ModelClaude3Haiku, // unstable api
	}

	DefaultModel = ModelLlama70b

	modelByName = map[string]Model{}

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		{Name: ProviderGroq},
		{Name: ProviderGithub},
		{Name: ProviderGoogle},
		{Name: ProviderCrof, PreferReasoning: true}, // above groq when reasoning
		{Name: ProviderZukijourney},
		{Name: ProviderOpenRouter},
		{Name: ProviderCably},
		{Name: ProviderMeow},
		{Name: ProviderCloudflare},
		{Name: ProviderFresed},
		{Name: ProviderElectron},
		{Name: ProviderHelixmind},
		{Name: ProviderCohere}, // 1,000 reqs/mo limit
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
	return DefaultModel
}

func (m Model) Client(provider string) (*openai.Client, []string) {
	if provider == ProviderGithub {
		// github marketplace requires special tweaks
		config := openai.DefaultAzureConfig(githubToken, azureBaseURL)
		config.APIType = openai.APITypeOpenAI
		return openai.NewClientWithConfig(config), m.Providers[provider].Codenames
	}

	var token, api string
	switch provider {
	case ProviderZukijourney:
		token, api = zjToken, zjBaseURL
	case ProviderFresed:
		token, api = fresedToken, fresedBaseURL
	case ProviderHelixmind:
		token, api = hmToken, hmBaseURL
	case ProviderGroq:
		token, api = groqToken, groqBaseURL
	case ProviderGoogle:
		token, api = googleToken, googleBaseURL
	case ProviderOpenRouter:
		token, api = openRouterToken, openRouterBaseURL
	case ProviderG4F:
		token, api = g4fToken, g4fBaseURL
	case ProviderCrof:
		token, api = crofToken, crofBaseURL
	case ProviderElectron:
		token, api = electronToken, electronBaseURL
	case ProviderCably:
		token, api = cablyToken, cablyBaseURL
	case ProviderMeow:
		token, api = meowToken, meowBaseURL
	case ProviderCloudflare:
		token, api = cfToken, cfBaseURL
	case ProviderCohere:
		token, api = cohereToken, cohereBaseURL
	default:
		token, api = githubToken, azureBaseURL
	}

	p := m.Providers[provider]

	config := openai.DefaultConfig(token)
	config.BaseURL = api
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
