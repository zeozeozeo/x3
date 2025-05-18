package model

import (
	"os"
	"sort"
	"strings"
	"time"

	"github.com/tiktoken-go/tokenizer"
)

// Helper function to split env vars and filter empty strings
func getEnvList(key string) []string {
	val := os.Getenv(key)
	if val == "" {
		return nil
	}
	list := strings.Split(val, ";")
	filtered := make([]string, 0, len(list))
	for _, item := range list {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

var (
	githubToken = os.Getenv("X3_GITHUB_TOKEN")
)

const (
	azureBaseURL      = "https://models.inference.ai.azure.com"
	zjBaseURL         = "https://api.zukijourney.com/v1"
	hmBaseURL         = "https://helixmind.online/v1"
	fresedBaseURL     = "https://fresedapi.fun/v1"
	groqBaseURL       = "https://api.groq.com/openai/v1"
	googleBaseURL     = "https://generativelanguage.googleapis.com/v1beta/openai"
	openRouterBaseURL = "https://openrouter.ai/api/v1"
	g4fBaseURL        = "http://192.168.230.44:1337/v1"
	crofBaseURL       = "https://ai.nahcrof.com/v2"
	electronBaseURL   = "https://api.electronhub.top/v1"
	cablyBaseURL      = "https://cablyai.com/v1"
	meowBaseURL       = "https://meow.cablyai.com/v1"
	cohereBaseURL     = "https://api.cohere.ai/compatibility/v1"
	mnnBaseURL        = "https://api.mnnai.ru/v1"
	voidaiBaseURL     = "https://api.voidai.app/v1"
	zhipuBaseURL      = "https://open.bigmodel.cn/api/paas/v4"
	chutesBaseURL     = "https://llm.chutes.ai/v1"
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
	ProviderMNN         = "mnn"
	ProviderSelfhosted  = "selfhosted"
	ProviderVoid        = "voidai"
	ProviderZhipu       = "zhipu"
	ProviderChutes      = "chutes"
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
	IsLunaris bool
	IsMarkov  bool
}

type ScoredProvider struct {
	Name            string
	PreferReasoning bool
	Errors          int
}

var (
	ModelGpt41Mini = Model{
		Name:     "OpenAI GPT-4.1 mini",
		Command:  "mini",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"openai/gpt-4.1-mini"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderFresed: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderHelixmind: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderG4F: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderElectron: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderCably: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderMeow: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderMNN: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4.1-mini"},
			},
		},
	}

	ModelGpt41 = Model{
		Name:     "OpenAI GPT-4.1",
		Command:  "gpt",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"openai/gpt-4.1"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderFresed: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderHelixmind: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderG4F: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderElectron: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderCably: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderMeow: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderMNN: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4.1"},
			},
		},
	}

	ModelGpt41Nano = Model{
		Name:     "OpenAI GPT-4.1 nano",
		Command:  "nano",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"openai/gpt-4.1-nano"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderFresed: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderHelixmind: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderG4F: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderElectron: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderCably: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderMeow: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderMNN: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4.1-nano"},
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
			ProviderMNN: {
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
				Codenames: []string{"cohere/Cohere-command-r-plus"},
			},
			ProviderCohere: {
				Codenames: []string{"command-r-plus"},
			},
			ProviderZukijourney: {
				Codenames: []string{"command-r-plus"},
			},
			ProviderMNN: {
				Codenames: []string{"command-r-plus"},
			},
		},
	}

	ModelMistralLarge = Model{
		Name:    "Mistral Large 123B",
		Command: "mistral",
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"mistral-ai/Mistral-Large-2411"},
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
			ProviderMNN: {
				Codenames: []string{"mistral-large-latest"},
			},
			ProviderVoid: {
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
				Codenames: []string{"mistral-ai/mistral-small-2503"},
			},
			ProviderMNN: {
				Codenames: []string{"mistral-small"},
			},
			ProviderVoid: {
				Codenames: []string{"mistral-small-latest"},
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
			ProviderMNN: {
				Codenames: []string{"llama-3.2-90b"},
			},
			ProviderGithub: {
				Codenames: []string{"meta/Llama-3.2-90B-Vision-Instruct"},
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
			ProviderOpenRouter: {
				Codenames: []string{"meta-llama/llama-3.2-11b-vision-instruct:free"},
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
			ProviderVoid: {
				Codenames: []string{"meta-llama/Llama-3.2-11B-Vision-Instruct"},
			},
			ProviderGithub: {
				Codenames: []string{"meta/Llama-3.2-11B-Vision-Instruct"},
			},
		},
	}

	ModelLlama70b = Model{
		Name:    "Meta Llama 3.3 70B",
		Command: "llama",
		IsLlama: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"llama-3.3-70b-versatile"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"meta-llama/llama-3.3-70b-instruct:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-3.3-70b-instruct"},
			},
			ProviderFresed: {
				Codenames: []string{"llama-3.3-70b-turbo-free", "llama-3.3-70b-turbo"},
			},
			ProviderCrof: {
				Codenames: []string{"llama3.3-70b"},
			},
			//ProviderGithub: {
			//	Codenames: []string{"meta/Llama-3.3-70B-Instruct"},
			//},
			ProviderElectron: {
				Codenames: []string{"llama-3.3-70b-instruct"},
			},
			ProviderMeow: {
				Codenames: []string{"llama-3.3-70b-instruct-fp8-fast"},
			},
			ProviderCloudflare: {
				Codenames: []string{"@cf/meta/llama-3.3-70b-instruct-fp8-fast"},
			},
			ProviderMNN: {
				Codenames: []string{"llama-3.3-70b"},
			},
			ProviderVoid: {
				Codenames: []string{"meta-llama/Llama-3.3-70B-Instruct-Turbo", "meta-llama/Llama-3.3-70B-Instruct"},
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
			ProviderMNN: {
				Codenames: []string{"llama-3.1-8b"},
			},
			ProviderVoid: {
				Codenames: []string{"meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo"},
			},
		},
	}

	ModelLlamaScout = Model{
		Name:    "Meta Llama 4 Scout 109B/17A",
		Command: "scout",
		// no need for llamahacks, proper multimodality
		Vision: true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"meta-llama/llama-4-scout-17b-16e-instruct"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"meta-llama/llama-4-scout:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-4-scout-17b-instruct"},
			},
			ProviderElectron: {
				Codenames: []string{"llama-4-scout-17b-16e-instruct"},
			},
			ProviderCloudflare: {
				Codenames: []string{"@cf/meta/llama-4-scout-17b-16e-instruct"},
			},
			ProviderMNN: {
				Codenames: []string{"llama-4-scout"},
			},
			ProviderGithub: {
				Codenames: []string{"meta/Llama-4-Scout-17B-16E-Instruct"},
			},
			ProviderCrof: {
				Codenames: []string{"llama-4-scout-131k"},
			},
		},
	}

	ModelLlamaMaverick = Model{
		Name:    "Meta Llama 4 Maverick 400B/17A",
		Command: "maverick",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderGroq: {
				Codenames: []string{"meta-llama/llama-4-maverick-17b-128e-instruct"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-4-maverick-17b-instruct"},
			},
			ProviderElectron: {
				Codenames: []string{"llama-4-maverick-17b-128e-instruct"},
			},
			ProviderMNN: {
				Codenames: []string{"llama-4-maverick"},
			},
			ProviderGithub: {
				Codenames: []string{"meta/Llama-4-Maverick-17B-128E-Instruct-FP8"},
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
		Name:    "DeepSeek-V3 671B (0324)",
		Command: "deepseek",
		Providers: map[string]ModelProvider{
			ProviderChutes: {
				Codenames: []string{"deepseek-ai/DeepSeek-V3-0324"},
			},
			ProviderZukijourney: {
				Codenames: []string{"deepseek-chat"},
			},
			ProviderFresed: {
				Codenames: []string{"deepseek-v3"},
			},
			ProviderCrof: {
				Codenames: []string{"DeepSeek-V3-0324"},
			},
			ProviderGithub: {
				Codenames: []string{"deepseek/DeepSeek-V3-0324"},
			},
			ProviderCably: {
				Codenames: []string{"deepseek-v3"},
			},
			ProviderMNN: {
				Codenames: []string{"deepseek-v3"},
			},
			ProviderVoid: {
				Codenames: []string{"deepseek-v3-0324", "deepseek-v3"},
			},
		},
	}

	ModelDeepSeekR1 = Model{
		Name:      "DeepSeek R1 671B",
		Command:   "r1",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderChutes: {
				Codenames: []string{"deepseek-ai/DeepSeek-R1"},
			},
			ProviderCrof: {
				Codenames: []string{"deepseek-r1"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"deepseek/deepseek-r1:free"},
			},
			ProviderGithub: {
				Codenames: []string{"deepseek/DeepSeek-R1"},
			},
			ProviderElectron: {
				Codenames: []string{"deepseek-r1"},
			},
			ProviderMeow: {
				Codenames: []string{"deepseek-r1"},
			},
			ProviderMNN: {
				Codenames: []string{"deepseek-r1"},
			},
			ProviderVoid: {
				Codenames: []string{"deepseek-r1"},
			},
		},
	}

	ModelQwen3A22b = Model{
		Name:      "Qwen3 235B A22B",
		Command:   "qwen",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderChutes: {
				Codenames: []string{"Qwen/Qwen3-235B-A22B"},
			},
			ProviderVoid: {
				Codenames: []string{"Qwen/Qwen3-235B-A22B"},
			},
			ProviderCrof: {
				Codenames: []string{"Qwen3-235B-A22B"},
			},
		},
	}

	ModelLunaris8b = Model{
		Name:      "Llama 3 Lunaris 8B (Selfhosted, RP)",
		Command:   "lunaris",
		IsLunaris: true,
		Providers: map[string]ModelProvider{
			ProviderSelfhosted: {
				Codenames: []string{"L3-8B-Lunaris-v1-Q4_K_M"}, // https://huggingface.co/bartowski/L3-8B-Lunaris-v1-GGUF/blob/main/L3-8B-Lunaris-v1-Q4_K_M.gguf
			},
			ProviderElectron: {
				Codenames: []string{"l3-lunaris-8b"},
			},
		},
	}

	ModelClaudeSonnet = Model{
		Name:        "Anthropic Claude 3.7 Sonnet",
		Command:     "sonnet",
		Whitelisted: true,
		Providers: map[string]ModelProvider{
			ProviderMNN: {
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

	ModelGLM4 = Model{
		Name:    "THUDM GLM-4-0414 32B",
		Command: "glm4",
		Providers: map[string]ModelProvider{
			ProviderZhipu: {
				Codenames: []string{"glm-4-flash"},
			},
		},
	}

	ModelGLM4V = Model{
		Name:    "THUDM GLM-4V",
		Command: "glm4v",
		Vision:  true,
		Providers: map[string]ModelProvider{
			ProviderZhipu: {
				Codenames: []string{"glm-4v-flash"},
			},
		},
	}

	ModelGLMZ1 = Model{
		Name:      "THUDM GLM-Z1-0414 32B",
		Command:   "z1",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderZhipu: {
				Codenames: []string{"glm-z1-flash"},
			},
		},
	}

	ModelMarkovChain = Model{
		Name:      "Markov Chain",
		Command:   "markov",
		IsMarkov:  true,
		Providers: map[string]ModelProvider{},
	}

	AllModels = []Model{
		ModelLunaris8b,
		ModelLlama70b,  // default - fastest with specdec, mostly uncensored, good for RP
		ModelGpt41Mini, // gptslop
		ModelGpt41,     // overly expensive gptslop
		ModelGpt41Nano,
		ModelGeminiFlash,
		ModelMistralLarge,
		ModelMistralSmall,
		ModelLlamaScout,
		ModelLlamaMaverick,
		ModelLlama405b,  // unstable api
		ModelGemma27b,   // this is balls
		ModelDeepSeekR1, // groq often cuts off the response
		ModelDeepSeekV3, // pretty good but slow
		ModelCommandA,
		ModelQwen3A22b,
		// discord menu cutoff (25) - only useless models should go below this
		ModelGLM4,
		ModelGLMZ1,
		ModelGLM4V,
		ModelClaudeSonnet,
		ModelMistralNemo,
		ModelGigaChatPro, // this is a joke
		ModelCommandRplus,
		ModelLlama8b,
		ModelLlama11b,
		ModelLlama90b,
		ModelMarkovChain,

		// TODO:
		//ModelClaude3Haiku, // unstable api
	}

	DefaultModels       = []string{ModelLlama70b.Name, ModelLlamaScout.Name, ModelLlamaMaverick.Name, ModelGpt41Mini.Name, ModelGpt41.Name, ModelGpt41Nano.Name, ModelGeminiFlash.Name}
	DefaultModel        = DefaultModels[0]
	DefaultVisionModels = []string{ModelLlamaScout.Name, ModelLlamaMaverick.Name, ModelGpt41Mini.Name, ModelGpt41.Name, ModelGpt41Nano.Name, ModelGeminiFlash.Name}

	modelByName = map[string]Model{}

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		{Name: ProviderSelfhosted},
		{Name: ProviderGroq},
		{Name: ProviderZhipu},
		{Name: ProviderChutes},
		{Name: ProviderGithub},
		{Name: ProviderGoogle},
		{Name: ProviderCrof, PreferReasoning: true}, // above groq when reasoning
		{Name: ProviderCloudflare},
		{Name: ProviderZukijourney},
		{Name: ProviderCably},
		//{Name: ProviderMeow},
		{Name: ProviderOpenRouter},
		{Name: ProviderFresed},
		{Name: ProviderMNN},
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
	return GetModelByName(DefaultModel)
}

func GetModelsByNames(names []string) []Model {
	models := make([]Model, len(names))
	for i, name := range names {
		models[i] = GetModelByName(name)
	}
	return models
}

// Client returns lists of base URLs, tokens, and codenames for the given provider.
// For most providers, the base URL and token lists will contain only one element.
// For Cloudflare, it can return multiple base URLs and corresponding tokens.
func (m Model) Client(provider string) (baseUrls []string, tokens []string, codenames []string) {
	p, ok := m.Providers[provider]
	if !ok {
		return nil, nil, nil
	}
	codenames = p.Codenames

	if provider == ProviderGithub {
		baseUrls = []string{azureBaseURL}
		tokens = []string{githubToken}
		return
	}

	var tokenEnvKey, apiVar string
	switch provider {
	case ProviderZukijourney:
		tokenEnvKey, apiVar = "X3_ZJ_TOKEN", zjBaseURL
	case ProviderFresed:
		tokenEnvKey, apiVar = "X3_FRESED_TOKEN", fresedBaseURL
	case ProviderHelixmind:
		tokenEnvKey, apiVar = "X3_HM_TOKEN", hmBaseURL
	case ProviderGroq:
		tokenEnvKey, apiVar = "X3_GROQ_TOKEN", groqBaseURL
	case ProviderGoogle:
		tokenEnvKey, apiVar = "X3_GOOGLE_AISTUDIO_TOKEN", googleBaseURL
	case ProviderOpenRouter:
		tokenEnvKey, apiVar = "X3_OPENROUTER_TOKEN", openRouterBaseURL
	case ProviderG4F:
		tokenEnvKey, apiVar = "X3_G4F_TOKEN", g4fBaseURL
	case ProviderCrof:
		tokenEnvKey, apiVar = "X3_CROF_TOKEN", crofBaseURL
	case ProviderElectron:
		tokenEnvKey, apiVar = "X3_ELECTRONHUB_TOKEN", electronBaseURL
	case ProviderCably:
		tokenEnvKey, apiVar = "X3_CABLYAI_TOKEN", cablyBaseURL
	case ProviderMeow:
		tokenEnvKey, apiVar = "X3_MEOWAPI_TOKEN", meowBaseURL
	case ProviderCloudflare:
		baseUrls = getEnvList("X3_CLOUDFLARE_API_BASE")
		tokens = getEnvList("X3_CLOUDFLARE_API_TOKEN")
		if len(baseUrls) != len(tokens) {
			panic("X3_CLOUDFLARE_API_BASE and X3_CLOUDFLARE_API_TOKEN lists must be the same length")
		}
		return
	case ProviderCohere:
		tokenEnvKey, apiVar = "X3_COHERE_TOKEN", cohereBaseURL
	case ProviderMNN:
		tokenEnvKey, apiVar = "X3_MNN_TOKEN", mnnBaseURL
	case ProviderSelfhosted:
		baseUrls = getEnvList("X3_SELFHOSTED_API_BASE")
		tokens = getEnvList("X3_SELFHOSTED_API_TOKEN")
		if len(baseUrls) != len(tokens) {
			panic("X3_SELFHOSTED_API_BASE and X3_SELFHOSTED_API_TOKEN lists must be the same length")
		}
		return
	case ProviderVoid:
		tokenEnvKey, apiVar = "X3_VOIDAI_TOKEN", voidaiBaseURL
	case ProviderZhipu:
		tokenEnvKey, apiVar = "X3_BIGMODEL_TOKEN", zhipuBaseURL
	case ProviderChutes:
		tokenEnvKey, apiVar = "X3_CHUTES_TOKEN", chutesBaseURL
	default:
		return nil, nil, nil
	}

	baseUrls = []string{apiVar}
	tokens = getEnvList(tokenEnvKey)
	return
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
