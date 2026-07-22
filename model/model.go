package model

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	akashBaseURL        = "https://chatapi.akash.network/api/v1"
	llm7BaseURL         = "https://api.llm7.io/v1"
	longcatBaseURL      = "https://api.longcat.chat/openai"
	navyBaseURL         = "https://api.navy/v1"
	perplexityBaseURL   = "https://api.perplexity.ai"
	routewayBaseURL     = "https://api.routeway.ai/v1"
	minimaxBaseURL      = "https://api.minimax.io/v1"
	ollamaBaseURL       = "https://ollama.com/v1"
	vercelBaseURL       = "https://ai-gateway.vercel.sh/v1"
	kivestBaseURL       = "https://ai.ezif.in/v1"
	agentrouterBaseURL  = "https://agentrouter.org/v1"
	zenmuxBaseURL       = "https://zenmux.ai/api/v1"
	deepseekBaseURL     = "https://api.deepseek.com"
	mistralBaseURL      = "https://api.mistral.ai/v1"
	zenBaseURL          = "https://opencode.ai/zen/v1"
	mimoBaseURL         = "https://token-plan-sgp.xiaomimimo.com/v1"
	makoraBaseURL       = "https://inference.makora.com/glm-5-1-fp8/v1"
	openferenceBaseURL  = "https://api.openference.com/v1"
	cloudflareBaseURLf  = "https://api.cloudflare.com/client/v4/accounts/%s/ai/v1"
	nimBaseURL          = "https://integrate.api.nvidia.com/v1"
)

const (
	ProviderGithub       = "github"
	ProviderZukijourney  = "zukijourney"
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
	ProviderAkash        = "akash"
	ProviderLLM7         = "llm7"
	ProviderLongCat      = "longcat"
	ProviderNavy         = "navy"
	ProviderPerplexity   = "perplexity"
	ProviderRouteway     = "routeway"
	ProviderMinimax      = "minimax"
	ProviderOllama       = "ollama"
	ProviderVercel       = "vercel"
	ProviderKivest       = "kivest"
	ProviderAgentrouter  = "agentrouter"
	ProviderZenmux       = "zenmux"
	ProviderDeepseek     = "deepseek"
	ProviderMistral      = "mistral"
	ProviderZen          = "zen" // OpenCode Zen
	ProviderMiMo         = "mimo"
	ProviderMakora       = "makora"
	ProviderOpenference  = "openference"
	ProviderNim          = "nim"
)

type ModelProvider struct {
	Codenames []string `json:"codenames"`
}

type Model struct {
	Name                string                   `json:"name,omitempty"`
	Command             string                   `json:"command,omitempty"`
	Whitelisted         bool                     `json:"whitelisted,omitempty"`
	Vision              bool                     `json:"vision,omitempty"`
	FallbackVisionModel string                   `json:"fallback_vision_model,omitempty"`
	Reasoning           bool                     `json:"reasoning,omitempty"`
	Encoding            tokenizer.Encoding       `json:"encoding,omitempty"`
	Providers           map[string]ModelProvider `json:"providers,omitempty"`
	IsMarkov            bool                     `json:"is_markov,omitempty"`
	IsEliza             bool                     `json:"is_eliza,omitempty"`
	IsAlice             bool                     `json:"is_alice,omitempty"`
	Limited             bool                     `json:"limited,omitempty"` // disable custom inference settings
}

func (model Model) IsVeryDumb() bool {
	return model.IsAlice || model.IsEliza || model.IsMarkov
}

type ScoredProvider struct {
	Name            string
	PreferReasoning bool
	Errors          int
}

const (
	// AdaptiveProviderSlowThreshold is the request duration that triggers a
	// one-request probe of another provider for the same model.
	AdaptiveProviderSlowThreshold = 10 * time.Second
	adaptiveProviderRouteTTL      = 10 * time.Minute
	adaptiveLatencyMinSamples     = 3
	adaptiveLatencyEWMAAlpha      = 0.35
	providerCircuitFailureLimit   = 3
)

type adaptiveProviderRoute struct {
	slowProvider  string
	probeProvider string
	slowDuration  time.Duration
	probeInFlight bool
	preferred     string
	fallback      string
	expiresAt     time.Time
}

// ProviderFailure describes how strongly a failed request should count toward
// a model/provider circuit breaker. A zero value is ignored.
type ProviderFailure struct {
	Weight   int
	Cooldown time.Duration
}

// ProviderFailureResult describes routing changes caused by a failed request.
type ProviderFailureResult struct {
	AdaptiveFallback bool
	CircuitOpened    bool
}

type providerLatency struct {
	ewma    time.Duration
	samples int
}

type providerCircuit struct {
	failureScore int
	openUntil    time.Time
}

var (
	AllModels           []Model
	DefaultModels       []string
	DefaultModel        string
	NarratorModels      []string
	DefaultVisionModels []string
	SiteModels          []string

	modelByName = map[string]Model{}

	providerSettings = map[string]ProviderSettings{}

	// default errors are set for default order of trial
	allProviders = []*ScoredProvider{
		//{Name: ProviderSelfhosted},
		{Name: ProviderRouteway},
		{Name: ProviderGroq},
		{Name: ProviderCerebras},
		//{Name: ProviderChutes},
		{Name: ProviderZhipu},
		{Name: ProviderTogether},
		{Name: ProviderNineteen},
		{Name: ProviderGoogle},
		{Name: ProviderAtlas},
		{Name: ProviderAkash},
		//{Name: ProviderTargon},
		{Name: ProviderCloudflare},
		{Name: ProviderOllama},
		{Name: ProviderMNN},
		{Name: ProviderCrof},
		{Name: ProviderElectron},
		{Name: ProviderHcap},
		{Name: ProviderZukijourney},
		{Name: ProviderCohere}, // 1,000 reqs/mo limit
		{Name: ProviderOpenRouter},
		{Name: ProviderLLM7},
		{Name: ProviderGithub},
		{Name: ProviderPollinations},
		{Name: ProviderHuggingface},
		{Name: ProviderNavy},
		{Name: ProviderPerplexity},
		{Name: ProviderMinimax},
		{Name: ProviderVercel},
		{Name: ProviderKivest},
		{Name: ProviderAgentrouter},
		{Name: ProviderZenmux},
		{Name: ProviderDeepseek},
		{Name: ProviderMistral},
		{Name: ProviderZen},
		{Name: ProviderMiMo},
		{Name: ProviderMakora},
		{Name: ProviderOpenference},
		{Name: ProviderNim},
		//{Name: ProviderG4F},
	}

	lastScoreReset = time.Now()

	tokenErrors         = map[string]int{}
	lastTokenErrorReset = time.Now()

	CurrentVersion = 35

	adaptiveProviderRoutesMu sync.Mutex
	adaptiveProviderRoutes   = map[string]adaptiveProviderRoute{}
	providerLatencies        = map[string]providerLatency{}
	providerCircuits         = map[string]providerCircuit{}
	adaptiveProviderNow      = time.Now
)

type ModelsConfig struct {
	Models              []Model                     `json:"models"`
	DefaultModels       []string                    `json:"default_models"`
	NarratorModels      []string                    `json:"narrator_models"`
	DefaultVisionModels []string                    `json:"default_vision_models"`
	SiteModels          []string                    `json:"site_models,omitempty"`
	ProvidersOrder      []string                    `json:"providers_order"`
	ProviderSettings    map[string]ProviderSettings `json:"provider_settings,omitempty"`
	CurrentVersion      int                         `json:"current_version"`
}

type ProviderSettings struct {
	NativeToolCalling bool `json:"native_tool_calling,omitempty"`
}

func LoadModelsFromJSON() error {
	path, err := findModelsJSON()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return LoadModelsFromJSONData(data)
}

func findModelsJSON() (string, error) {
	const name = "models.json"
	dir, err := os.Getwd()
	if err != nil {
		return name, nil
	}
	for {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return name, nil
		}
		dir = parent
	}
}

func LoadModelsFromJSONData(data []byte) error {
	var config ModelsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	CurrentVersion = config.CurrentVersion

	AllModels = config.Models

	DefaultModels = config.DefaultModels
	if len(DefaultModels) > 0 {
		DefaultModel = DefaultModels[0]
	}
	NarratorModels = config.NarratorModels
	DefaultVisionModels = config.DefaultVisionModels
	SiteModels = config.SiteModels
	providerSettings = config.ProviderSettings
	if providerSettings == nil {
		providerSettings = map[string]ProviderSettings{}
	}

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
	resetAdaptiveProviderRoutes()

	return nil
}

func ProviderUsesNativeToolCalling(provider string) bool {
	return providerSettings[provider].NativeToolCalling
}

func resetProviderScore() {
	for i, p := range allProviders {
		p.Errors = i
	}
}

// RecordTokenError increments the error count for a token.
func RecordTokenError(token string) {
	if token == "" {
		return
	}
	if time.Since(lastTokenErrorReset) > 10*time.Minute {
		tokenErrors = map[string]int{}
		lastTokenErrorReset = time.Now()
	}
	tokenErrors[token]++
}

// SortTokensByError sorts tokens in-place by ascending error count.
func SortTokensByError(tokens []string) {
	if len(tokens) < 2 {
		return
	}
	if time.Since(lastTokenErrorReset) > 10*time.Minute {
		tokenErrors = map[string]int{}
		lastTokenErrorReset = time.Now()
	}
	sort.SliceStable(tokens, func(i, j int) bool {
		return tokenErrors[tokens[i]] < tokenErrors[tokens[j]]
	})
}

// SortPairByTokenError sorts two parallel slices (base URLs and tokens) together
// by ascending token error count, keeping pairs intact.
func SortPairByTokenError(baseUrls, tokens []string) {
	if len(tokens) < 2 {
		return
	}
	if time.Since(lastTokenErrorReset) > 10*time.Minute {
		tokenErrors = map[string]int{}
		lastTokenErrorReset = time.Now()
	}
	type pair struct {
		baseURL string
		token   string
	}
	pairs := make([]pair, len(baseUrls))
	for i := range baseUrls {
		pairs[i] = pair{baseURL: baseUrls[i], token: tokens[i]}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return tokenErrors[pairs[i].token] < tokenErrors[pairs[j].token]
	})
	for i, p := range pairs {
		baseUrls[i] = p.baseURL
		tokens[i] = p.token
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

// ProvidersForModel returns configured providers in their normal score order,
// with any temporary model-specific adaptive route moved to the front.
// A provider that takes longer than AdaptiveProviderSlowThreshold schedules one
// probe of the next configured provider on a later request.
func ProvidersForModel(m Model, reasoning bool) []*ScoredProvider {
	providers := ScoreProviders(reasoning)
	now := adaptiveProviderNow()

	adaptiveProviderRoutesMu.Lock()
	route, ok := adaptiveProviderRoutes[m.Name]
	if ok && !route.expiresAt.IsZero() && !now.Before(route.expiresAt) {
		delete(adaptiveProviderRoutes, m.Name)
		ok = false
	}

	priority := make([]string, 0, 2)
	if ok {
		switch {
		case route.preferred != "":
			priority = append(priority, route.preferred, route.fallback)
		case !route.probeInFlight:
			route.probeInFlight = true
			adaptiveProviderRoutes[m.Name] = route
			priority = append(priority, route.probeProvider, route.slowProvider)
		}
	}
	adaptiveProviderRoutesMu.Unlock()

	ordered := make([]*ScoredProvider, 0, len(providers))
	added := make(map[string]bool, len(providers))
	appendProvider := func(name string) {
		if name == "" || added[name] {
			return
		}
		if _, configured := m.Providers[name]; !configured {
			return
		}
		if providerCircuitOpen(m.Name, name, now) {
			return
		}
		for _, provider := range providers {
			if provider.Name == name {
				ordered = append(ordered, provider)
				added[name] = true
				return
			}
		}
	}
	for _, name := range priority {
		appendProvider(name)
	}
	for _, provider := range providers {
		appendProvider(provider.Name)
	}
	return ordered
}

// RecordProviderSuccess records the duration of a successful request. A slow
// provider triggers a one-request probe of another configured provider. If the
// probe is faster than the slow request, it is preferred for ten minutes.
func RecordProviderSuccess(m Model, provider string, duration time.Duration) {
	if provider == "" {
		return
	}
	now := adaptiveProviderNow()

	adaptiveProviderRoutesMu.Lock()
	defer adaptiveProviderRoutesMu.Unlock()
	key := providerRouteKey(m.Name, provider)
	delete(providerCircuits, key)
	latency := providerLatencies[key]
	if latency.samples == 0 {
		latency.ewma = duration
	} else {
		latency.ewma = time.Duration((1-adaptiveLatencyEWMAAlpha)*float64(latency.ewma) + adaptiveLatencyEWMAAlpha*float64(duration))
	}
	latency.samples++
	providerLatencies[key] = latency
	route, probing := adaptiveProviderRoutes[m.Name]
	if probing && !route.expiresAt.IsZero() && !now.Before(route.expiresAt) {
		delete(adaptiveProviderRoutes, m.Name)
		probing = false
	}
	if probing && route.probeInFlight && route.probeProvider == provider {
		route.probeInFlight = false
		if duration < route.slowDuration {
			route.preferred = provider
			route.fallback = route.slowProvider
			route.expiresAt = now.Add(adaptiveProviderRouteTTL)
			adaptiveProviderRoutes[m.Name] = route
			return
		}
		delete(adaptiveProviderRoutes, m.Name)
		return
	}

	if probing || latency.samples < adaptiveLatencyMinSamples || latency.ewma <= AdaptiveProviderSlowThreshold {
		return
	}
	for _, candidate := range ScoreProviders(false) {
		if candidate.Name != provider {
			if _, configured := m.Providers[candidate.Name]; configured {
				adaptiveProviderRoutes[m.Name] = adaptiveProviderRoute{
					slowProvider:  provider,
					probeProvider: candidate.Name,
					slowDuration:  latency.ewma,
				}
				return
			}
		}
	}
}

// RecordProviderFailure updates the weighted circuit breaker and adaptive
// route. Callers should move to the next provider when either result is set.
func RecordProviderFailure(m Model, provider string, failure ProviderFailure) ProviderFailureResult {
	if provider == "" || failure.Weight <= 0 {
		return ProviderFailureResult{}
	}
	now := adaptiveProviderNow()
	adaptiveProviderRoutesMu.Lock()
	defer adaptiveProviderRoutesMu.Unlock()
	key := providerRouteKey(m.Name, provider)
	circuit := providerCircuits[key]
	if !circuit.openUntil.IsZero() && !now.Before(circuit.openUntil) {
		circuit = providerCircuit{}
	}
	circuit.failureScore += failure.Weight
	result := ProviderFailureResult{}
	if circuit.failureScore >= providerCircuitFailureLimit {
		circuit.failureScore = 0
		circuit.openUntil = now.Add(failure.Cooldown)
		result.CircuitOpened = true
	}
	providerCircuits[key] = circuit

	route, ok := adaptiveProviderRoutes[m.Name]
	if !ok {
		return result
	}
	if !route.expiresAt.IsZero() && !now.Before(route.expiresAt) {
		delete(adaptiveProviderRoutes, m.Name)
		return result
	}
	if route.preferred == provider {
		route.preferred = route.fallback
		route.fallback = ""
		route.expiresAt = now.Add(adaptiveProviderRouteTTL)
		adaptiveProviderRoutes[m.Name] = route
		result.AdaptiveFallback = true
		return result
	}
	if route.probeInFlight && route.probeProvider == provider {
		delete(adaptiveProviderRoutes, m.Name)
		result.AdaptiveFallback = true
		return result
	}
	return result
}

func resetAdaptiveProviderRoutes() {
	adaptiveProviderRoutesMu.Lock()
	defer adaptiveProviderRoutesMu.Unlock()
	adaptiveProviderRoutes = map[string]adaptiveProviderRoute{}
	providerLatencies = map[string]providerLatency{}
	providerCircuits = map[string]providerCircuit{}
}

func providerRouteKey(modelName, provider string) string {
	return modelName + "\x00" + provider
}

func circuitOpenLocked(modelName, provider string, now time.Time) bool {
	key := providerRouteKey(modelName, provider)
	circuit, ok := providerCircuits[key]
	if !ok || circuit.openUntil.IsZero() {
		return false
	}
	if !now.Before(circuit.openUntil) {
		delete(providerCircuits, key)
		return false
	}
	return true
}

func providerCircuitOpen(modelName, provider string, now time.Time) bool {
	adaptiveProviderRoutesMu.Lock()
	defer adaptiveProviderRoutesMu.Unlock()
	return circuitOpenLocked(modelName, provider, now)
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
		tokens = getEnvList("X3_CLOUDFLARE_WORKERS_AI_TOKEN")
		accIds := getEnvList("X3_CLOUDFLARE_ACCOUNT_ID")
		if len(tokens) != len(accIds) {
			panic("X3_CLOUDFLARE_WORKERS_AI_TOKEN and X3_CLOUDFLARE_ACCOUNT_ID lists must be the same length")
		}
		baseUrls = make([]string, len(accIds))
		for i, accId := range accIds {
			baseUrls[i] = fmt.Sprintf(cloudflareBaseURLf, accId)
		}
		SortPairByTokenError(baseUrls, tokens)
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
		SortPairByTokenError(baseUrls, tokens)
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
	case ProviderAkash:
		tokenEnvKey, apiVar = "X3_AKASH_TOKEN", akashBaseURL
	case ProviderLLM7:
		tokenEnvKey, apiVar = "X3_LLM7_TOKEN", llm7BaseURL
	case ProviderLongCat:
		tokenEnvKey, apiVar = "X3_LONGCAT_TOKEN", longcatBaseURL
	case ProviderNavy:
		tokenEnvKey, apiVar = "X3_NAVY_TOKEN", navyBaseURL
	case ProviderPerplexity:
		tokenEnvKey, apiVar = "X3_PERPLEXITY_TOKEN", perplexityBaseURL
	case ProviderRouteway:
		tokenEnvKey, apiVar = "X3_ROUTEWAY_TOKEN", routewayBaseURL
	case ProviderMinimax:
		tokenEnvKey, apiVar = "X3_MINIMAX_TOKEN", minimaxBaseURL
	case ProviderOllama:
		tokenEnvKey, apiVar = "X3_OLLAMA_TOKEN", ollamaBaseURL
	case ProviderVercel:
		tokenEnvKey, apiVar = "X3_VERCEL_TOKEN", vercelBaseURL
	case ProviderKivest:
		tokenEnvKey, apiVar = "X3_KIVEST_TOKEN", kivestBaseURL
	case ProviderAgentrouter:
		tokenEnvKey, apiVar = "X3_AGENTROUTER_TOKEN", agentrouterBaseURL
	case ProviderZenmux:
		tokenEnvKey, apiVar = "X3_ZENMUX_TOKEN", zenmuxBaseURL
	case ProviderDeepseek:
		tokenEnvKey, apiVar = "X3_DEEPSEEK_TOKEN", deepseekBaseURL
	case ProviderMistral:
		tokenEnvKey, apiVar = "X3_MISTRAL_TOKEN", mistralBaseURL
	case ProviderZen:
		tokenEnvKey, apiVar = "X3_ZEN_TOKEN", zenBaseURL
	case ProviderMiMo:
		tokenEnvKey, apiVar = "X3_MIMO_TOKEN", mimoBaseURL
	case ProviderMakora:
		tokenEnvKey, apiVar = "X3_MAKORA_TOKEN", makoraBaseURL
	case ProviderOpenference:
		tokenEnvKey, apiVar = "X3_OPENFERENCE_TOKEN", openferenceBaseURL
	case ProviderNim:
		tokenEnvKey, apiVar = "X3_NIM_TOKEN", nimBaseURL
	default:
		return nil, nil, nil
	}

	baseUrls = []string{apiVar}
	tokens = getEnvList(tokenEnvKey)
	SortTokensByError(tokens)
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
