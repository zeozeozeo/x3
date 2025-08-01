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
	azureBaseURL        = "https://models.inference.ai.azure.com"
	zjBaseURL           = "https://api.zukijourney.com/v1"
	hmBaseURL           = "https://helixmind.online/v1"
	fresedBaseURL       = "https://fresedapi.fun/v1"
	groqBaseURL         = "https://api.groq.com/openai/v1"
	googleBaseURL       = "https://generativelanguage.googleapis.com/v1beta/openai"
	openRouterBaseURL   = "https://openrouter.ai/api/v1"
	g4fBaseURL          = "http://192.168.230.44:1337/v1"
	crofBaseURL         = "https://ai.nahcrof.com/v2"
	electronBaseURL     = "https://api.electronhub.ai/v1"
	cohereBaseURL       = "https://api.cohere.ai/compatibility/v1"
	mnnBaseURL          = "https://api.mnnai.ru/v1"
	voidaiBaseURL       = "https://api.voidai.app/v1"
	zhipuBaseURL        = "https://open.bigmodel.cn/api/paas/v4"
	chutesBaseURL       = "https://llm.chutes.ai/v1"
	cerebrasBaseURL     = "https://api.cerebras.ai/v1"
	togetherBaseURL     = "https://api.together.xyz/v1"
	nineteenBaseURL     = "https://api.nineteen.ai/v1"
	hcapBaseURL         = "https://hcap.ai/v1"
	pollinationsBaseURL = "https://text.pollinations.ai/openai"
	targonBaseURL       = "https://api.targon.com/v1"
	atlasBaseURL        = "https://api.atlascloud.ai/v1"
	huggingfaceBaseURL  = "https://router.huggingface.co/featherless-ai/v1"
)

const (
	ProviderGithub       = "github"
	ProviderZukijourney  = "zukijourney"
	ProviderFresed       = "fresed"
	ProviderHelixmind    = "helixmind"
	ProviderGroq         = "groq"
	ProviderGoogle       = "google"
	ProviderOpenRouter   = "openrouter"
	ProviderG4F          = "g4f"
	ProviderCrof         = "crof"
	ProviderElectron     = "electronhub"
	ProviderCloudflare   = "cloudflare"
	ProviderCohere       = "cohere"
	ProviderMNN          = "mnn"
	ProviderSelfhosted   = "selfhosted"
	ProviderVoid         = "voidai"
	ProviderZhipu        = "zhipu"
	ProviderChutes       = "chutes"
	ProviderCerebras     = "cerebras"
	ProviderTogether     = "together"
	ProviderNineteen     = "nineteen"
	ProviderHcap         = "hcap"
	ProviderPollinations = "pollinations"
	ProviderTargon       = "targon"
	ProviderAtlas        = "atlas"
	ProviderHuggingface  = "huggingface"
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
	IsMarkov  bool
	Limited   bool // disable custom inference settings
}

type ScoredProvider struct {
	Name            string
	PreferReasoning bool
	Errors          int
}

var (
	ModelGpt4oMini = Model{
		Name:     "OpenAI GPT-4o mini",
		Command:  "mini",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"openai/gpt-4o-mini"},
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
			ProviderMNN: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4o-mini"},
			},
			ProviderHcap: {
				Codenames: []string{"gpt-4o-mini"},
			},
		},
	}

	ModelGpt4o = Model{
		Name:     "OpenAI GPT-4o",
		Command:  "gpt",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: map[string]ModelProvider{
			ProviderGithub: {
				Codenames: []string{"openai/gpt-4o"},
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
			ProviderMNN: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4o"},
			},
			ProviderHcap: {
				Codenames: []string{"gpt-4o"},
			},
		},
	}

	ModelGpt41Mini = Model{
		Name:     "OpenAI GPT-4.1 mini",
		Command:  "cmini",
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
			ProviderMNN: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderHcap: {
				Codenames: []string{"gpt-4.1-mini"},
			},
			ProviderPollinations: {
				Codenames: []string{"gpt-4.1-mini"},
			},
		},
	}

	ModelGpt41 = Model{
		Name:     "OpenAI GPT-4.1",
		Command:  "cgpt",
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
			ProviderMNN: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderHcap: {
				Codenames: []string{"gpt-4.1"},
			},
			ProviderPollinations: {
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
			ProviderMNN: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderVoid: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderHcap: {
				Codenames: []string{"gpt-4.1-nano"},
			},
			ProviderPollinations: {
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
			ProviderPollinations: {
				Codenames: []string{"mistral-small-3.1-24b-instruct"},
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
			ProviderCloudflare: {
				Codenames: []string{"@cf/meta/llama-3.2-11b-vision-instruct"},
			},
			ProviderVoid: {
				Codenames: []string{"meta-llama/Llama-3.2-11B-Vision-Instruct"},
			},
			ProviderGithub: {
				Codenames: []string{"meta/Llama-3.2-11B-Vision-Instruct"},
			},
			ProviderTogether: {
				Codenames: []string{"meta-llama/Llama-Vision-Free"},
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
			ProviderCloudflare: {
				Codenames: []string{"@cf/meta/llama-3.3-70b-instruct-fp8-fast"},
			},
			ProviderMNN: {
				Codenames: []string{"llama-3.3-70b"},
			},
			ProviderVoid: {
				Codenames: []string{"meta-llama/Llama-3.3-70B-Instruct-Turbo", "meta-llama/Llama-3.3-70B-Instruct"},
			},
			ProviderCerebras: {
				Codenames: []string{"llama-3.3-70b"},
			},
			ProviderTogether: {
				Codenames: []string{"meta-llama/Llama-3.3-70B-Instruct-Turbo-Free"},
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
			ProviderElectron: {
				Codenames: []string{"llama-3.1-8b"},
			},
			ProviderMNN: {
				Codenames: []string{"llama-3.1-8b"},
			},
			ProviderVoid: {
				Codenames: []string{"meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo"},
			},
			ProviderCerebras: {
				Codenames: []string{"llama3.1-8b"},
			},
		},
	}

	ModelLlamaScout = Model{
		Name:    "Meta Llama 4 Scout 109B/17A",
		Command: "scout",
		// no need for llama hacks, proper multimodality
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
			ProviderCerebras: {
				Codenames: []string{"llama-4-scout-17b-16e-instruct"},
			},
			ProviderChutes: {
				Codenames: []string{"chutesai/Llama-4-Scout-17B-16E-Instruct"},
			},
			ProviderPollinations: {
				Codenames: []string{"llama-4-scout-17b-16e-instruct"},
			},
			ProviderAtlas: {
				Codenames: []string{"meta-llama/Llama-4-Scout-17B-16E-Instruct"},
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
			ProviderChutes: {
				Codenames: []string{"chutesai/Llama-4-Maverick-17B-128E-Instruct-FP8"},
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
		Name:    "DeepSeek V3 671B (0324)",
		Command: "deepseek",
		Providers: map[string]ModelProvider{
			ProviderChutes: {
				Codenames: []string{"deepseek-ai/DeepSeek-V3-0324"},
			},
			ProviderZukijourney: {
				Codenames: []string{"deepseek-chat"},
			},
			ProviderFresed: {
				Codenames: []string{"deepseek-v3-0324", "deepseek-v3"},
			},
			ProviderCrof: {
				Codenames: []string{"deepseek-v3-0324"},
			},
			ProviderGithub: {
				Codenames: []string{"deepseek/DeepSeek-V3-0324"},
			},
			ProviderMNN: {
				Codenames: []string{"deepseek-v3-0324", "deepseek-v3"},
			},
			ProviderVoid: {
				Codenames: []string{"deepseek-v3-0324", "deepseek-v3"},
			},
			ProviderTargon: {
				Codenames: []string{"deepseek-ai/DeepSeek-V3-0324"},
			},
			ProviderElectron: {
				Codenames: []string{"deepseek-v3-0324"},
			},
			ProviderHcap: {
				Codenames: []string{"deepseek-v3"},
			},
			//ProviderPollinations: {
			//	Codenames: []string{"deepseek-v3"},
			//},
			ProviderAtlas: {
				Codenames: []string{"deepseek-ai/DeepSeek-V3-0324"},
			},
		},
	}

	ModelDeepSeekR1 = Model{
		Name:      "DeepSeek R1 671B (0528)",
		Command:   "r1",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderChutes: {
				Codenames: []string{"deepseek-ai/DeepSeek-R1-0528"},
			},
			ProviderCrof: {
				Codenames: []string{"deepseek-r1-0528"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"deepseek/deepseek-r1-0528:free"},
			},
			ProviderGithub: {
				Codenames: []string{"deepseek/DeepSeek-R10-0528"},
			},
			ProviderElectron: {
				Codenames: []string{"deepseek-r1-0528"},
			},
			ProviderMNN: {
				Codenames: []string{"deepseek-r1-0528"},
			},
			ProviderVoid: {
				Codenames: []string{"deepseek-r1-0528"},
			},
			ProviderHcap: {
				Codenames: []string{"deepseek-r1"},
			},
			ProviderPollinations: {
				Codenames: []string{"deepseek-r1-0528"},
			},
			ProviderTargon: {
				Codenames: []string{"deepseek-ai/DeepSeek-R1-0528"},
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

	ModelMagMell = Model{
		Name:    "Mag Mell R1 12B (RP)",
		Command: "magmell",
		Providers: map[string]ModelProvider{
			ProviderSelfhosted: {
				Codenames: []string{"MN-12B-Mag-Mell-R1"},
			},
			ProviderHuggingface: {
				Codenames: []string{"inflatebot/MN-12B-Mag-Mell-R1"},
			},
		},
	}

	ModelCommandA = Model{
		Name:    "Cohere Command A 111B",
		Command: "commanda",
		Providers: map[string]ModelProvider{
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
			ProviderChutes: {
				Codenames: []string{"THUDM/GLM-4-32B-0414"},
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

	ModelQwen332b = Model{
		Name:      "Qwen 3 32B",
		Command:   "qwen32",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderCerebras: {
				Codenames: []string{"qwen-3-32b"},
			},
			ProviderChutes: {
				Codenames: []string{"Qwen/Qwen3-32B"},
			},
			ProviderAtlas: {
				Codenames: []string{"Qwen/Qwen3-32B"},
			},
		},
	}

	ModelExaone32b = Model{
		Name:    "LG EXAONE 3.5 32B",
		Command: "exaone",
		Providers: map[string]ModelProvider{
			ProviderTogether: {
				Codenames: []string{"lgai/exaone-3-5-32b-instruct"},
			},
		},
	}

	ModelExaoneDeep = Model{
		Name:      "LG EXAONE Deep 32B",
		Reasoning: true,
		Command:   "deep",
		Providers: map[string]ModelProvider{
			ProviderTogether: {
				Codenames: []string{"lgai/exaone-deep-32b"},
			},
		},
	}

	ModelQwQ = Model{
		Name:      "QwQ 32B",
		Command:   "qwq",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderNineteen: {
				Codenames: []string{"Qwen/QwQ-32B"},
			},
		},
	}

	ModelO3 = Model{
		Name:      "OpenAI o3",
		Command:   "o3",
		Reasoning: true,
		Limited:   true,
		Providers: map[string]ModelProvider{
			ProviderHcap: {
				Codenames: []string{"o3"},
			},
			ProviderPollinations: {
				Codenames: []string{"o3"},
			},
			ProviderMNN: {
				Codenames: []string{"o3"},
			},
		},
	}

	ModelO4Mini = Model{
		Name:      "OpenAI o4-mini",
		Command:   "o4mini",
		Reasoning: true,
		Providers: map[string]ModelProvider{
			ProviderHcap: {
				Codenames: []string{"o4-mini"},
			},
			ProviderMNN: {
				Codenames: []string{"o4-mini"},
			},
		},
	}

	ModelGrok3Mini = Model{
		Name:    "xAI Grok 3 mini",
		Command: "grok",
		Providers: map[string]ModelProvider{
			ProviderPollinations: {
				Codenames: []string{"grok-3-mini"},
			},
		},
	}

	ModelKimiK2 = Model{
		Name:    "Moonshot AI Kimi K2",
		Command: "kimi",
		Providers: map[string]ModelProvider{
			ProviderMNN: {
				Codenames: []string{"kimi-k2"},
			},
			ProviderChutes: {
				Codenames: []string{"moonshotai/Kimi-K2-Instruct"},
			},
			ProviderCrof: {
				Codenames: []string{"kimi-k2"},
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
		ModelDeepSeekV3,
		ModelLlama70b,
		ModelKimiK2, // peak
		ModelGpt41,
		ModelGpt4o,
		ModelO3,
		ModelGeminiFlash,
		ModelMistralLarge,
		ModelMistralSmall,
		ModelLlamaScout,
		ModelLlamaMaverick,
		ModelLlama405b,
		ModelGemma27b,
		ModelDeepSeekR1,
		ModelCommandA,
		ModelQwen3A22b,
		ModelQwen332b,
		ModelGrok3Mini,
		ModelQwQ,
		ModelO4Mini,
		ModelMagMell,
		// discord menu cutoff (25) - only useless models should go below this
		ModelGLM4,
		ModelGLMZ1,
		ModelGLM4V,
		ModelMistralNemo,
		ModelGigaChatPro, // this is a joke
		ModelCommandRplus,
		ModelLlama8b,
		ModelLlama11b,
		ModelLlama90b,
		ModelGpt41Mini,
		ModelGpt41Nano,
		ModelGpt4oMini,
		ModelExaone32b,
		ModelExaoneDeep,
		ModelMarkovChain,

		// TODO:
		//ModelClaude3Haiku, // unstable api
	}

	DefaultModels = []string{ModelDeepSeekV3.Name, ModelLlama70b.Name, ModelLlamaMaverick.Name, ModelLlamaScout.Name, ModelGpt41.Name, ModelGeminiFlash.Name}
	// Because chutes doesnt support prefill
	NarratorModels      = []string{ModelLlama70b.Name, ModelLlamaScout.Name, ModelLlamaMaverick.Name, ModelGpt41Mini.Name, ModelGpt41.Name, ModelGpt41Nano.Name, ModelGeminiFlash.Name}
	DefaultModel        = DefaultModels[0]
	DefaultVisionModels = []string{ModelLlamaScout.Name, ModelLlamaMaverick.Name, ModelGpt41Mini.Name, ModelGpt41.Name, ModelGpt41Nano.Name, ModelGeminiFlash.Name}

	modelByName = map[string]Model{}

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		//{Name: ProviderSelfhosted},
		{Name: ProviderGroq},
		{Name: ProviderCerebras},
		//{Name: ProviderChutes},
		{Name: ProviderZhipu},
		{Name: ProviderTogether},
		{Name: ProviderNineteen},
		{Name: ProviderGoogle},
		{Name: ProviderTargon},
		{Name: ProviderCloudflare},
		{Name: ProviderCrof},
		{Name: ProviderVoid},
		{Name: ProviderMNN},
		{Name: ProviderAtlas},
		{Name: ProviderZukijourney},
		{Name: ProviderFresed},
		{Name: ProviderElectron},
		{Name: ProviderHcap},
		{Name: ProviderHelixmind},
		{Name: ProviderCohere}, // 1,000 reqs/mo limit
		{Name: ProviderOpenRouter},
		{Name: ProviderGithub},
		{Name: ProviderPollinations},
		{Name: ProviderHuggingface},
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
	case ProviderCerebras:
		tokenEnvKey, apiVar = "X3_CEREBRAS_TOKEN", cerebrasBaseURL
	case ProviderTogether:
		tokenEnvKey, apiVar = "X3_TOGETHER_TOKEN", togetherBaseURL
	case ProviderNineteen:
		tokenEnvKey, apiVar = "X3_NINETEEN_TOKEN", nineteenBaseURL
	case ProviderHcap:
		tokenEnvKey, apiVar = "X3_HCAP_TOKEN", hcapBaseURL
	case ProviderPollinations:
		tokenEnvKey, apiVar = "X3_POLLINATIONS_TOKEN", pollinationsBaseURL
	case ProviderTargon:
		tokenEnvKey, apiVar = "X3_TARGON_TOKEN", targonBaseURL
	case ProviderAtlas:
		tokenEnvKey, apiVar = "X3_ATLAS_TOKEN", atlasBaseURL
	case ProviderHuggingface:
		tokenEnvKey, apiVar = "X3_HUGGINGFACE_TOKEN", huggingfaceBaseURL
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
