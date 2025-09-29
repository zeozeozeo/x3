package model

import (
	"encoding/json"
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
	Codenames []string `json:"codenames"`
}

type Model struct {
	Name        string                   `json:"name,omitempty"`
	Command     string                   `json:"command,omitempty"`
	Whitelisted bool                     `json:"whitelisted,omitempty"`
	Vision      bool                     `json:"vision,omitempty"`
	Reasoning   bool                     `json:"reasoning,omitempty"`
	Encoding    tokenizer.Encoding       `json:"encoding,omitempty"`
	Providers   map[string]ModelProvider `json:"providers,omitempty"`
	IsMarkov    bool                     `json:"is_markov,omitempty"`
	IsEliza     bool                     `json:"is_eliza,omitempty"`
	Limited     bool                     `json:"limited,omitempty"` // disable custom inference settings
}

type ScoredProvider struct {
	Name            string
	PreferReasoning bool
	Errors          int
}

var (
	AllModels           []Model
	DefaultModels       []string
	DefaultModel        string
	NarratorModels      []string
	DefaultVisionModels []string

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

type ModelsConfig struct {
	Models              []Model  `json:"models"`
	DefaultModels       []string `json:"default_models"`
	NarratorModels      []string `json:"narrator_models"`
	DefaultVisionModels []string `json:"default_vision_models"`
	ProvidersOrder      []string `json:"providers_order"`
}

func LoadModelsFromJSON() error {
	data, err := os.ReadFile("models.json")
	if err != nil {
		return err
	}
	return LoadModelsFromJSONData(data)
}

func LoadModelsFromJSONData(data []byte) error {
	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	AllModels = config.Models

	DefaultModels = config.DefaultModels
	if len(DefaultModels) > 0 {
		DefaultModel = DefaultModels[0]
	}
	NarratorModels = config.NarratorModels
	DefaultVisionModels = config.DefaultVisionModels

	if len(config.ProvidersOrder) > 0 {
		allProviders = make([]*ScoredProvider, len(config.ProvidersOrder))
		for i, providerName := range config.ProvidersOrder {
			allProviders[i] = &ScoredProvider{Name: providerName}
		}
	}

	// Update modelByName map
	modelByName = make(map[string]Model)
	for _, m := range AllModels {
		modelByName[m.Name] = m
	}

	return nil
}

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
	if err := LoadModelsFromJSON(); err != nil {
		panic("failed to load models: " + err.Error())
	}

	resetProviderScore()
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
