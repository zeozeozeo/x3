package model

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tiktoken-go/tokenizer"
	"github.com/zeozeozeo/x3/opencode"
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
	azureBaseURL                = "https://models.inference.ai.azure.com"
	zjBaseURL                   = "https://api.zukijourney.com/v1"
	groqBaseURL                 = "https://api.groq.com/openai/v1"
	googleBaseURL               = "https://generativelanguage.googleapis.com/v1beta/openai"
	openRouterBaseURL           = "https://openrouter.ai/api/v1"
	g4fBaseURL                  = "http://192.168.230.44:1337/v1"
	crofBaseURL                 = "https://ai.nahcrof.com/v2"
	electronBaseURL             = "https://api.electronhub.ai/v1"
	cohereBaseURL               = "https://api.cohere.ai/compatibility/v1"
	mnnBaseURL                  = "https://api.mnnai.ru/v1"
	zhipuBaseURL                = "https://open.bigmodel.cn/api/paas/v4"
	chutesBaseURL               = "https://llm.chutes.ai/v1"
	cerebrasBaseURL             = "https://api.cerebras.ai/v1"
	togetherBaseURL             = "https://api.together.xyz/v1"
	nineteenBaseURL             = "https://api.nineteen.ai/v1"
	hcapBaseURL                 = "https://hcap.ai/v1"
	pollinationsBaseURL         = "https://text.pollinations.ai/openai"
	targonBaseURL               = "https://api.targon.com/v1"
	atlasBaseURL                = "https://api.atlascloud.ai/v1"
	huggingfaceBaseURL          = "https://router.huggingface.co/featherless-ai/v1"
	akashBaseURL                = "https://chatapi.akash.network/api/v1"
	llm7BaseURL                 = "https://api.llm7.io/v1"
	longcatBaseURL              = "https://api.longcat.chat/openai"
	navyBaseURL                 = "https://api.navy/v1"
	perplexityBaseURL           = "https://api.perplexity.ai"
	routewayBaseURL             = "https://api.routeway.ai/v1"
	minimaxBaseURL              = "https://api.minimax.io/v1"
	ollamaBaseURL               = "https://ollama.com/v1"
	vercelBaseURL               = "https://ai-gateway.vercel.sh/v1"
	kivestBaseURL               = "https://ai.ezif.in/v1"
	agentrouterBaseURL          = "https://agentrouter.org/v1"
	zenmuxBaseURL               = "https://zenmux.ai/api/v1"
	deepseekBaseURL             = "https://api.deepseek.com"
	mistralBaseURL              = "https://api.mistral.ai/v1"
	zenBaseURL                  = "https://opencode.ai/zen/v1"
	opencodeBaseURL             = "https://opencode.ai/inference/openai/v1"
	opencodeGoBaseURL           = "https://opencode.ai/inference/go/openai/v1"
	mimoBaseURL                 = "https://token-plan-sgp.xiaomimimo.com/v1"
	makoraBaseURL               = "https://inference.makora.com/glm-5-1-fp8/v1"
	openferenceBaseURL          = "https://api.openference.com/v1"
	cloudflareBaseURLf          = "https://api.cloudflare.com/client/v4/accounts/%s/ai/v1"
	cloudflareSystemOneBaseURLf = "https://api.cloudflare.com/client/v4/accounts/%s/ai/run/@cf/cloudflare/%s"
	cloudflareWorkersAIBaseURLf = "https://api.cloudflare.com/client/v4/accounts/%s/ai/run/%s"
	nimBaseURL                  = "https://integrate.api.nvidia.com/v1"
	modelscopeBaseURL           = "https://api-inference.modelscope.ai/v1" // note: this is different from the chinese site (.cn), apikeys are not shared between them
	tokenrouterBaseUrl          = "https://api.tokenrouter.com/v1"
	inferxBaseUrl               = "https://model.inferx.net/endpoints/v1"
	tokenharborBaseUrl          = "https://tokenharbor.ai/v1"
	orcarouterBaseUrl           = "https://api.orcarouter.ai/v1"
	aionBaseUrl                 = "https://api.aionlabs.ai/v1"
	unorouterBaseUrl            = "https://api.unorouter.com/v1"
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
	ProviderZen          = "zen"      // OpenCode Zen
	ProviderOpencode     = "opencode" // OpenCode Console / free tier
	ProviderOpencodeGo   = "opencode-go"
	ProviderMiMo         = "mimo"
	ProviderMakora       = "makora"
	ProviderOpenference  = "openference"
	ProviderNim          = "nim"
	ProviderModelscope   = "modelscope"
	ProviderTokenrouter  = "tokenrouter"
	ProviderInferx       = "inferx"
	ProviderTokenharbor  = "tokenharbor"
	ProviderOrcarouter   = "orcarouter"
	ProviderAion         = "aion"
	ProviderUnorouter    = "unorouter"
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
	// AdaptiveProviderSlowThreshold is the request duration that marks a
	// provider success as slow. Slow samples feed the streak counter that
	// schedules a one-request probe of another provider.
	AdaptiveProviderSlowThreshold = 10 * time.Second
	// adaptiveSlowStreakNeeded is how many consecutive slow successes it
	// takes to schedule a probe. A single slow outlier (queue spike, cold
	// start) must not flip traffic on its own.
	adaptiveSlowStreakNeeded     = 2
	adaptiveProviderRouteTTL     = 2 * time.Minute
	providerCircuitFailureLimit  = 3
	providerCircuitProbeInterval = 2 * time.Minute
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

type providerCircuit struct {
	failureScore int
	openUntil    time.Time
	nextProbeAt  time.Time
}

// DefaultWorldMemEmbeddingModel is the Workers AI embedding model used by the
// worldmem RAG system when world_mem_embedding_model is not configured. It is
// also the value the model editor shows as the baseline.
//
// Changing it changes the width of the vector index, which worldmem.Reindex
// repairs by re-embedding every stored fact. Keep models.json, the model editor
// and this constant in agreement.
const DefaultWorldMemEmbeddingModel = "@cf/google/embeddinggemma-300m"

var (
	AllModels           []Model
	DefaultModels       []string
	DefaultModel        string
	NarratorModels      []string
	DefaultVisionModels []string
	SiteModels          []string
	ABPool              []string

	// WorldMemEmbeddingModel is the Workers AI embedding model used by the
	// worldmem RAG system. Configurable via models.json / modeled.
	WorldMemEmbeddingModel = DefaultWorldMemEmbeddingModel

	modelByName = map[string]Model{}

	// modelByCommand maps the short codename (the /slash-command name, the
	// `command` field in models.json) to a model.
	modelByCommand = map[string]Model{}

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
		{Name: ProviderOpencode},
		{Name: ProviderOpencodeGo},
		{Name: ProviderMiMo},
		{Name: ProviderMakora},
		{Name: ProviderOpenference},
		{Name: ProviderNim},
		{Name: ProviderModelscope},
		//{Name: ProviderG4F},
	}

	lastScoreReset = time.Now()

	tokenErrors         = map[string]int{}
	lastTokenErrorReset = time.Now()

	CurrentVersion = 35

	adaptiveProviderRoutesMu sync.Mutex
	adaptiveProviderRoutes   = map[string]adaptiveProviderRoute{}
	providerCircuits         = map[string]providerCircuit{}
	providerSlowStreak       = map[string]int{}
	adaptiveProviderNow      = time.Now
)

type ModelsConfig struct {
	Models                 []Model                     `json:"models"`
	DefaultModels          []string                    `json:"default_models"`
	NarratorModels         []string                    `json:"narrator_models"`
	DefaultVisionModels    []string                    `json:"default_vision_models"`
	SiteModels             []string                    `json:"site_models,omitempty"`
	ABPool                 []string                    `json:"ab_pool,omitempty"`
	WorldMemEmbeddingModel string                      `json:"world_mem_embedding_model,omitempty"`
	ProvidersOrder         []string                    `json:"providers_order"`
	ProviderSettings       map[string]ProviderSettings `json:"provider_settings,omitempty"`
	CurrentVersion         int                         `json:"current_version"`
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

	{
		// ensure that all models have unique short names
		nameSet := make(map[string]struct{})
		for _, m := range config.Models {
			if _, exists := nameSet[m.Command]; exists {
				return fmt.Errorf("duplicate model name found: %s", m.Name)
			}
			nameSet[m.Name] = struct{}{}
		}
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
	ABPool = config.ABPool
	WorldMemEmbeddingModel = strings.TrimSpace(config.WorldMemEmbeddingModel)
	if WorldMemEmbeddingModel == "" {
		WorldMemEmbeddingModel = DefaultWorldMemEmbeddingModel
	}
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

	// Update modelByCommand map
	modelByCommand = make(map[string]Model)
	for _, m := range AllModels {
		if m.Command == "" {
			continue
		}
		modelByCommand[m.Command] = m
	}
	resetAdaptiveProviderRoutes()

	return nil
}

func ProviderUsesNativeToolCalling(provider string) bool {
	return providerSettings[provider].NativeToolCalling
}

func OpenCodeVersion() string {
	version := strings.TrimSpace(os.Getenv("X3_OPENCODE_VERSION"))
	return opencode.NormalizeVersion(version)
}

// OpenCodeChannel returns the release channel embedded in the OpenCode
// User-Agent.
func OpenCodeChannel() string {
	channel := strings.TrimSpace(os.Getenv("X3_OPENCODE_CHANNEL"))
	if channel == "" {
		return opencode.DefaultChannel
	}
	return channel
}

// IsOpenCodeProvider reports whether provider is served by OpenCode. Both the
// free/Console endpoint and OpenCode Go share the same client fingerprint and
// streamed completion path.
func IsOpenCodeProvider(provider string) bool {
	return provider == ProviderOpencode || provider == ProviderOpencodeGo
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

// RecordProviderSuccess records the duration of a successful request. Slow
// successes feed a streak counter; once it reaches the threshold, one probe
// of another configured provider is scheduled on a later request. A probe
// that wins by a clear margin is preferred temporarily, otherwise routing
// returns to score order. A preferred provider that goes slow itself is
// demoted instead of sticking for the full TTL.
func RecordProviderSuccess(m Model, provider string, duration time.Duration) {
	if provider == "" {
		return
	}
	now := adaptiveProviderNow()

	adaptiveProviderRoutesMu.Lock()
	defer adaptiveProviderRoutesMu.Unlock()
	key := providerRouteKey(m.Name, provider)
	delete(providerCircuits, key)
	route, probing := adaptiveProviderRoutes[m.Name]
	if probing && !route.expiresAt.IsZero() && !now.Before(route.expiresAt) {
		delete(adaptiveProviderRoutes, m.Name)
		probing = false
	}
	if probing && route.probeInFlight && route.preferred == "" {
		if route.probeProvider == provider {
			route.probeInFlight = false
			providerSlowStreak[key] = 0
			if probeBeatsSlow(duration, route.slowDuration) {
				route.preferred = provider
				route.fallback = route.slowProvider
				route.expiresAt = now.Add(adaptiveProviderRouteTTL)
				adaptiveProviderRoutes[m.Name] = route
				slog.Info("adaptive routing: probe won, preferring provider", "model", m.Name, "provider", provider, "duration", duration, "slowDuration", route.slowDuration)
				return
			}
			slog.Info("adaptive routing: probe not faster, keeping order", "model", m.Name, "provider", provider, "duration", duration, "slowDuration", route.slowDuration)
			delete(adaptiveProviderRoutes, m.Name)
			return
		}
		// Another provider succeeded while a probe is outstanding elsewhere
		// (or the probe never resolved, e.g. its request was cancelled and
		// recorded nothing): drop the stale probe route instead of leaking
		// it and blocking future probes forever.
		delete(adaptiveProviderRoutes, m.Name)
		probing = false
	}

	slow := duration > AdaptiveProviderSlowThreshold
	if !slow {
		providerSlowStreak[key] = 0
	}

	if probing && route.preferred == provider {
		if !slow {
			return
		}
		// The preferred provider is slow itself: demote it and look for
		// something faster instead of sticking to it for the full TTL.
		slog.Info("adaptive routing: preferred provider slow, re-probing", "model", m.Name, "provider", provider, "duration", duration)
		providerSlowStreak[key] = 0
		scheduleAdaptiveProbe(m, provider, duration)
		return
	}
	if probing || !slow {
		return
	}
	providerSlowStreak[key]++
	if providerSlowStreak[key] < adaptiveSlowStreakNeeded {
		return
	}
	providerSlowStreak[key] = 0
	scheduleAdaptiveProbe(m, provider, duration)
}

// probeBeatsSlow reports whether a probe duration wins clearly enough over
// the slow sample to justify preferring it: strictly faster by at least one
// second or ten percent, so noise doesn't decide minutes of traffic.
func probeBeatsSlow(probe, slow time.Duration) bool {
	if probe >= slow {
		return false
	}
	return slow-probe >= max(time.Second, slow/10)
}

// scheduleAdaptiveProbe records a one-request trial of the next configured
// provider for a slow provider. Callers must hold adaptiveProviderRoutesMu.
func scheduleAdaptiveProbe(m Model, slowProvider string, slowDuration time.Duration) {
	for _, candidate := range ScoreProviders(false) {
		if candidate.Name == slowProvider {
			continue
		}
		if _, configured := m.Providers[candidate.Name]; !configured {
			continue
		}
		adaptiveProviderRoutes[m.Name] = adaptiveProviderRoute{
			slowProvider:  slowProvider,
			probeProvider: candidate.Name,
			slowDuration:  slowDuration,
		}
		slog.Info("adaptive routing: scheduled probe", "model", m.Name, "slowProvider", slowProvider, "slowDuration", slowDuration, "probe", candidate.Name)
		return
	}
}

// AdaptiveRouteDescribe returns a short description of the active adaptive
// route for a model for logging, or "" when routing is plain score order.
func AdaptiveRouteDescribe(m Model) string {
	adaptiveProviderRoutesMu.Lock()
	defer adaptiveProviderRoutesMu.Unlock()
	route, ok := adaptiveProviderRoutes[m.Name]
	if !ok {
		return ""
	}
	if route.preferred != "" {
		return "preferred=" + route.preferred
	}
	if route.probeInFlight {
		return "probing=" + route.probeProvider
	}
	if route.probeProvider != "" {
		return "probe-scheduled=" + route.probeProvider
	}
	return "adaptive?"
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
	providerSlowStreak[key] = 0
	circuit := providerCircuits[key]
	if !circuit.openUntil.IsZero() && !now.Before(circuit.openUntil) {
		circuit = providerCircuit{}
	}
	circuit.failureScore += failure.Weight
	result := ProviderFailureResult{}
	if circuit.failureScore >= providerCircuitFailureLimit {
		circuit.failureScore = 0
		circuit.openUntil = now.Add(failure.Cooldown)
		circuit.nextProbeAt = now.Add(providerCircuitProbeInterval)
		result.CircuitOpened = true
		slog.Warn("provider circuit opened",
			"model", m.Name, "provider", provider,
			"cooldown", failure.Cooldown, "next_probe", providerCircuitProbeInterval)
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
	providerCircuits = map[string]providerCircuit{}
	providerSlowStreak = map[string]int{}
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
	if circuit.nextProbeAt.IsZero() || !now.Before(circuit.nextProbeAt) {
		circuit.nextProbeAt = now.Add(providerCircuitProbeInterval)
		providerCircuits[key] = circuit
		slog.Info("provider circuit half-open: allowing recovery probe",
			"model", modelName, "provider", provider,
			"cooldown_remaining", circuit.openUntil.Sub(now).Round(time.Second))
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

// GetModelByCommand looks a model up by its short codename (the `command`
// field in models.json, e.g. "dsflash"). It reports whether the codename is
// known; unlike GetModelByName it never falls back to a default model.
func GetModelByCommand(command string) (Model, bool) {
	m, ok := modelByCommand[command]
	return m, ok
}

// ModelsByCommand returns every model that has a short codename, in the order
// they appear in models.json.
func ModelsByCommand() []Model {
	models := make([]Model, 0, len(modelByCommand))
	for _, m := range AllModels {
		if m.Command == "" {
			continue
		}
		models = append(models, m)
	}
	return models
}

func cloudflareAccounts(urlf string, apiModel string) (baseUrls []string, tokens []string) {
	tokens = getEnvList("X3_CLOUDFLARE_WORKERS_AI_TOKEN")
	accIds := getEnvList("X3_CLOUDFLARE_ACCOUNT_ID")
	if len(tokens) != len(accIds) {
		panic("X3_CLOUDFLARE_WORKERS_AI_TOKEN and X3_CLOUDFLARE_ACCOUNT_ID lists must be the same length")
	}
	baseUrls = make([]string, len(accIds))
	for i, accId := range accIds {
		if apiModel != "" {
			baseUrls[i] = fmt.Sprintf(urlf, accId, apiModel)
		} else {
			baseUrls[i] = fmt.Sprintf(urlf, accId)
		}
	}
	return
}

func SystemOneClients(apiModel string) (baseUrls []string, tokens []string, models []string) {
	if strings.TrimSpace(apiModel) == "" {
		return nil, nil, nil
	}
	baseUrls, tokens = cloudflareAccounts(cloudflareSystemOneBaseURLf, apiModel)
	if len(baseUrls) == 0 || len(tokens) == 0 {
		return nil, nil, nil
	}
	SortPairByTokenError(baseUrls, tokens)
	models = make([]string, len(baseUrls))
	for i := range models {
		models[i] = apiModel
	}
	return
}

// WorldMemEmbeddingClients returns one run-endpoint base URL per configured
// Cloudflare account for the worldmem embedding model, with the paired token
// per URL. Callers must try every pair (like llm.go does for chat) so all
// added accounts are used for failover.
func WorldMemEmbeddingClients() (baseUrls []string, tokens []string, apiModel string) {
	apiModel = strings.TrimSpace(WorldMemEmbeddingModel)
	if apiModel == "" {
		return nil, nil, ""
	}
	baseUrls, tokens = cloudflareAccounts(cloudflareWorkersAIBaseURLf, apiModel)
	if len(baseUrls) == 0 || len(tokens) == 0 {
		return nil, nil, ""
	}
	SortPairByTokenError(baseUrls, tokens)
	return baseUrls, tokens, apiModel
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
		baseUrls, tokens = cloudflareAccounts(cloudflareBaseURLf, "")
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
	case ProviderOpencode:
		tokens = getEnvList("X3_OPENCODE_TOKEN")
		if len(tokens) == 0 {
			return nil, nil, nil
		}
		baseUrls = []string{opencodeBaseURL}
		SortTokensByError(tokens)
		return
	case ProviderOpencodeGo:
		tokens = getEnvList("X3_OPENCODE_GO_TOKEN")
		if len(tokens) == 0 {
			// OpenCode Console API keys work for both the free/Console and
			// Go endpoints, so reuse the regular token when no Go-specific
			// token is configured.
			tokens = getEnvList("X3_OPENCODE_TOKEN")
		}
		if len(tokens) == 0 {
			return nil, nil, nil
		}
		baseUrls = []string{opencodeGoBaseURL}
		SortTokensByError(tokens)
		return
	case ProviderMiMo:
		tokenEnvKey, apiVar = "X3_MIMO_TOKEN", mimoBaseURL
	case ProviderMakora:
		tokenEnvKey, apiVar = "X3_MAKORA_TOKEN", makoraBaseURL
	case ProviderOpenference:
		tokenEnvKey, apiVar = "X3_OPENFERENCE_TOKEN", openferenceBaseURL
	case ProviderNim:
		tokenEnvKey, apiVar = "X3_NIM_TOKEN", nimBaseURL
	case ProviderModelscope:
		tokenEnvKey, apiVar = "X3_MODELSCOPE_TOKEN", modelscopeBaseURL
	case ProviderTokenrouter:
		tokenEnvKey, apiVar = "X3_TOKENROUTER_TOKEN", tokenrouterBaseUrl
	case ProviderInferx:
		tokenEnvKey, apiVar = "X3_INFERX_TOKEN", inferxBaseUrl
	case ProviderTokenharbor:
		tokenEnvKey, apiVar = "X3_TOKENHARBOR_TOKEN", tokenharborBaseUrl
	case ProviderOrcarouter:
		tokenEnvKey, apiVar = "X3_ORCAROUTER_TOKEN", orcarouterBaseUrl
	case ProviderAion:
		tokenEnvKey, apiVar = "X3_AION_TOKEN", aionBaseUrl
	case ProviderUnorouter:
		tokenEnvKey, apiVar = "X3_UNOROUTER_TOKEN", unorouterBaseUrl
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
