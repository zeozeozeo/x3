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
	groqBaseURL         = "https://api.groq.com/openai/v1"
	googleBaseURL       = "https://generativelanguage.googleapis.com/v1beta/openai"
	openRouterBaseURL   = "https://openrouter.ai/api/v1"
	g4fBaseURL          = "http://192.168.230.44:1337/v1"
	crofBaseURL         = "https://ai.nahcrof.com/v2"
	electronBaseURL     = "https://api.electronhub.ai/v1"
	cohereBaseURL       = "https://api.cohere.ai/compatibility/v1"
	mnnBaseURL          = "https://api.mnnai.ru/v1"
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
	Providers Providers
	IsMarkov  bool
	Limited   bool // disable custom inference settings
}

type ScoredProvider struct {
	Name            string
	PreferReasoning bool
	Errors          int
}

type Providers map[string]ModelProvider

var (
	ModelGpt5 = Model{
		Name:     "OpenAI GPT-5",
		Command:  "gpt",
		Vision:   true,
		Encoding: tokenizer.O200kBase,
		Providers: Providers{
			ProviderGithub: {
				Codenames: []string{"openai/gpt-5"},
			},
			ProviderZukijourney: {
				Codenames: []string{"gpt-5"},
			},
			ProviderHelixmind: {
				Codenames: []string{"gpt-5"},
			},
			ProviderG4F: {
				Codenames: []string{"gpt-5"},
			},
			ProviderElectron: {
				Codenames: []string{"gpt-5"},
			},
			ProviderMNN: {
				Codenames: []string{"gpt-5"},
			},
			ProviderHcap: {
				Codenames: []string{"gpt-5"},
			},
		},
	}

	ModelGptOss = Model{
		Name:      "OpenAI gpt-oss-120b",
		Command:   "oss",
		Reasoning: true,
		Encoding:  tokenizer.O200kBase,
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"openai/gpt-oss-120b:free"},
			},
		},
	}

	ModelGeminiFlash = Model{
		Name:    "Google Gemini 2.0 Flash Experimental",
		Command: "gemini",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"google/gemini-2.0-flash-exp:free"},
			},
		},
	}

	ModelLlama70b = Model{
		Name:    "Meta Llama 3.3 70B",
		Command: "llama",
		IsLlama: true,
		Providers: Providers{
			ProviderGroq: {
				Codenames: []string{"llama-3.3-70b-versatile"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"meta-llama/llama-3.3-70b-instruct:free"},
			},
			ProviderZukijourney: {
				Codenames: []string{"llama-3.3-70b-instruct"},
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
			ProviderCerebras: {
				Codenames: []string{"llama-3.3-70b"},
			},
			ProviderTogether: {
				Codenames: []string{"meta-llama/Llama-3.3-70B-Instruct-Turbo-Free"},
			},
		},
	}

	ModelLlamaScout = Model{
		Name:    "Meta Llama 4 Scout 109B/17A",
		Command: "scout",
		// no need for llama hacks, proper multimodality
		Vision: true,
		Providers: Providers{
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
		Providers: Providers{
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

	ModelGemma27b = Model{
		Name:    "Google Gemma 3 27B",
		Command: "gemma",
		Providers: Providers{
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
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"deepseek/deepseek-chat-v3-0324:free"},
			},
			ProviderChutes: {
				Codenames: []string{"deepseek-ai/DeepSeek-V3-0324"},
			},
			ProviderZukijourney: {
				Codenames: []string{"deepseek-chat"},
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

	ModelDeepSeekV31 = Model{
		Name:      "DeepSeek V3.1 671B",
		Command:   "deepseekv31",
		Reasoning: true,
		Providers: Providers{
			ProviderCrof: {
				Codenames: []string{"deepseek-v3.1"},
			},
			ProviderMNN: {
				Codenames: []string{"deepseek-v3.1"},
			},
			ProviderElectron: {
				Codenames: []string{"deepseek-v3.1"},
			},
			ProviderHcap: {
				Codenames: []string{"deepseek-v3.1"},
			},
			ProviderAtlas: {
				Codenames: []string{"deepseek-ai/DeepSeek-V3.1"},
			},
			ProviderOpenRouter: {
				Codenames: []string{"deepseek/deepseek-chat-v3.1:free"},
			},
		},
	}

	ModelDeepSeekR1 = Model{
		Name:      "DeepSeek R1 671B (0528)",
		Command:   "r1",
		Reasoning: true,
		Providers: Providers{
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

	ModelMagMell = Model{
		Name:    "Mag Mell R1 12B (RP)",
		Command: "magmell",
		Providers: Providers{
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
		Providers: Providers{
			ProviderCohere: {
				Codenames: []string{"command-a-03-2025"},
			},
			ProviderZukijourney: {
				Codenames: []string{"command-a"},
			},
		},
	}

	ModelO3 = Model{
		Name:      "OpenAI o3",
		Command:   "o3",
		Reasoning: true,
		Limited:   true,
		Providers: Providers{
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

	ModelKimiK2 = Model{
		Name:    "Moonshot AI Kimi K2",
		Command: "kimi",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"moonshotai/kimi-k2:free"},
			},
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

	ModelGrok4Fast = Model{
		Name:    "xAI Grok 4 Fast",
		Command: "grok",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"x-ai/grok-4-fast:free"},
			},
		},
	}

	ModelVeniceUncensored = Model{
		Name:    "Venice Uncensored",
		Command: "venice",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"cognitivecomputations/dolphin-mistral-24b-venice-edition:free"},
			},
		},
	}

	ModelQwen3Coder = Model{
		Name:    "Qwen3 Coder",
		Command: "qwen3c",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"qwen/qwen3-coder:free"},
			},
		},
	}

	ModelQwen3 = Model{
		Name:    "Qwen3 235B A22B",
		Command: "qwen3",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"qwen/qwen3-235b-a22b:free"},
			},
		},
	}

	ModelGlm45Air = Model{
		Name:    "Z.AI GLM 4.5 Air",
		Command: "glm",
		Providers: Providers{
			ProviderOpenRouter: {
				Codenames: []string{"z-ai/glm-4.5-air:free"},
			},
		},
	}

	ModelMarkovChain = Model{
		Name:     "Markov Chain",
		Command:  "markov",
		IsMarkov: true,
	}

	AllModels = []Model{
		ModelDeepSeekV3,
		ModelLlama70b,
		ModelKimiK2,
		ModelVeniceUncensored,
		ModelGrok4Fast,
		ModelDeepSeekV31,
		ModelO3,
		ModelLlamaScout,
		ModelLlamaMaverick,
		ModelGemma27b,
		ModelDeepSeekR1,
		ModelCommandA,
		//ModelMagMell,
		ModelGlm45Air,
		ModelQwen3Coder,
		ModelQwen3,
		ModelGptOss,
		ModelGeminiFlash,
		ModelMarkovChain,
	}

	DefaultModels = []string{
		ModelDeepSeekV3.Name,
		ModelGrok4Fast.Name,
		ModelLlama70b.Name,
		ModelLlamaMaverick.Name,
		ModelLlamaScout.Name,
		ModelGpt5.Name,
		ModelGeminiFlash.Name,
	}
	DefaultModel   = DefaultModels[0]
	NarratorModels = []string{
		ModelLlama70b.Name,
		ModelLlamaScout.Name,
		ModelLlamaMaverick.Name,
		ModelGpt5.Name,
		ModelGeminiFlash.Name,
	}
	DefaultVisionModels = []string{
		ModelLlamaMaverick.Name,
		ModelLlamaScout.Name,
		ModelGpt5.Name,
		ModelGrok4Fast.Name,
	}

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
		{Name: ProviderAtlas},
		//{Name: ProviderTargon},
		{Name: ProviderCloudflare},
		{Name: ProviderMNN},
		{Name: ProviderCrof},
		{Name: ProviderElectron},
		{Name: ProviderHcap},
		{Name: ProviderHelixmind},
		{Name: ProviderZukijourney},
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
