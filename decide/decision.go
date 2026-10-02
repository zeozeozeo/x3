// Package decide decides whether the bot should send a message in response to a
// public channel message that did not summon it outright
package decide

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/minilm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/systemone"
)

// Decider evaluates a state against the decision questions.
type Decider interface {
	Decide(ctx context.Context, state any, questions map[string]systemone.Question) (systemone.Result, error)
}

// DecisionInput is everything needed to judge one message.
type DecisionInput struct {
	// Enabled reports whether automatic continuation is on for this channel.
	Enabled bool
	// Now and LastInteraction bracket the bot's last message in the channel.
	Now             time.Time
	LastInteraction time.Time
	// Candidate is the message under consideration and CandidateName its author.
	Candidate     string
	CandidateName string
	// History is the cached conversation, oldest first.
	History []llm.Message
	// BotNames are the names the bot answers to, used to tell the model who it is.
	BotNames []string
	// ChannelName and PersonaName give the model setting and identity.
	ChannelName string
	PersonaName string
	// CacheKey, when set, memoizes the verdict so one message is never judged
	// twice and never billed twice.
	CacheKey string

	Config  Config
	Decider Decider
}

// Decision is the verdict, with a Reason enum for logging and triage.
type Decision struct {
	Trigger bool
	Reason  string
	// Score is the model's P(should reply), or the similarity score when the
	// local fallback ran.
	Score float32
	// Addressed and Kind are the model's classification of the message, empty
	// when the fallback ran.
	Addressed string
	Kind      string
	// Invitation is the model's 0-3 rating of how strongly the message asks
	// for a reply.
	Invitation float64
	Usage      systemone.Usage
}

// errNotConfigured means no System One endpoint is available for the configured
// model, so the decision model is skipped entirely.
var errNotConfigured = errors.New("no system one endpoint configured")

// ShouldTrigger decides whether to continue the conversation. It is the only
// entry point: cheap time and signal gates first, then one decision model call,
// then local similarity scoring if that call could not be made.
func ShouldTrigger(ctx context.Context, input DecisionInput) Decision {
	if !input.Enabled {
		return Decision{Reason: "disabled"}
	}
	cfg := input.Config.withDefaults()
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	if input.LastInteraction.IsZero() {
		return Decision{Reason: "no_last_interaction"}
	}

	elapsed := input.Now.Sub(input.LastInteraction)
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed <= cfg.GraceWindow {
		return Decision{Trigger: true, Reason: "grace_window"}
	}
	if elapsed > cfg.ContinuationWindow {
		return Decision{Reason: "outside_window"}
	}
	if minilm.IsLowSignal(input.Candidate) {
		return Decision{Reason: "empty_or_low_signal"}
	}

	if cached, ok := loadCached(input.CacheKey); ok {
		return cached
	}

	decision := judge(ctx, cfg, input)
	storeCached(input.CacheKey, decision)
	return decision
}

// judge asks the decision model, falling back to local similarity scoring.
func judge(ctx context.Context, cfg Config, input DecisionInput) Decision {
	decider := input.Decider
	if decider == nil {
		var err error
		decider, err = GlobalDecider(cfg)
		if err != nil {
			// GlobalDecider already warned once when it failed to build, so
			// this is per-message noise only.
			slog.Debug("no decision model, using local similarity", "err", err)
		}
	}

	if decider != nil {
		state := BuildState(
			input.History,
			Turn{Content: minilm.Clean(input.Candidate), Name: input.CandidateName},
			input.BotNames,
			input.ChannelName,
			input.PersonaName,
			elapsedSeconds(input.Now, input.LastInteraction),
			cfg,
		)
		result, err := decider.Decide(ctx, state, Questions(input.BotNames))
		if err != nil {
			slog.Warn("decision model call failed, using local similarity", "err", err)
		} else if decision, ok := interpret(cfg, result); ok {
			return decision
		} else {
			slog.Warn("decision model response was unusable, using local similarity")
		}
	}

	return similarityDecision(cfg, input)
}

// interpret turns the model's answers into a verdict. It reports false when the
// response is missing the answer the decision depends on.
func interpret(cfg Config, result systemone.Result) (Decision, bool) {
	probability, ok := result.Noul(QuestionRespond)
	if !ok {
		return Decision{}, false
	}

	addressed, _ := result.Selected(QuestionAddressed)
	kind, _ := result.Selected(QuestionKind)
	invitation := 0.0
	if answer, ok := result.Answers[QuestionInvitation]; ok && answer.Score != nil {
		invitation = *answer.Score
	}

	decision := Decision{
		Score:      float32(probability),
		Addressed:  addressed,
		Kind:       kind,
		Invitation: invitation,
		Usage:      result.Usage,
	}

	switch {
	case probability >= cfg.RespondThreshold:
		decision.Trigger = true
		decision.Reason = "decision_respond"
	case probability >= cfg.AmbiguousFloor && addressed == AddressedDirect:
		decision.Trigger = true
		decision.Reason = "decision_direct"
	default:
		decision.Reason = "decision_declined"
	}

	slog.Info("decision model verdict",
		"respond", probability,
		"addressed", addressed,
		"kind", kind,
		"invitation", invitation,
		"inputTokens", result.Usage.InputTokens,
	)
	return decision, true
}

// similarityDecision is the fallback path: local cosine scoring against the
// cached history and the stock prompt bank, each with its own threshold.
func similarityDecision(cfg Config, input DecisionInput) Decision {
	if !cfg.MiniLMFallback {
		return Decision{Reason: "decision_unavailable"}
	}

	refs := minilm.References(input.History)
	historyScore, defaultScore, err := minilm.ContinuationScores(nil, input.Candidate, refs)
	if err != nil {
		slog.Warn("local similarity scoring failed", "err", err)
		return Decision{Reason: "minilm_fallback_unavailable"}
	}

	return similarityVerdict(minilm.LoadConfig(), refs, historyScore, defaultScore)
}

func similarityVerdict(cfg minilm.Config, refs []string, historyScore, defaultScore float32) Decision {
	if historyScore >= cfg.Similarity {
		return Decision{Trigger: true, Reason: "minilm_fallback_similarity", Score: historyScore}
	}
	if defaultScore >= cfg.DefaultSimilarity {
		return Decision{Trigger: true, Reason: "minilm_fallback_default_similarity", Score: defaultScore}
	}
	if len(refs) == 0 {
		return Decision{Reason: "minilm_fallback_no_reference", Score: defaultScore}
	}
	return Decision{Reason: "minilm_fallback_below_threshold", Score: historyScore}
}

func elapsedSeconds(now, last time.Time) int {
	if last.IsZero() {
		return 0
	}
	elapsed := now.Sub(last)
	if elapsed < 0 {
		return 0
	}
	return int(elapsed.Seconds())
}

// systemOneDecider fails over across the configured accounts and tokens, the
// same way llm does for chat completions.
type systemOneDecider struct {
	endpoints []systemOneEndpoint
	timeout   time.Duration
}

type systemOneEndpoint struct {
	baseURL string
	token   string
	model   string
}

func (d *systemOneDecider) Decide(ctx context.Context, state any, questions map[string]systemone.Question) (systemone.Result, error) {
	var lastErr error
	for _, endpoint := range d.endpoints {
		client, err := systemone.NewClient(systemone.Config{
			BaseURL: endpoint.baseURL,
			Token:   endpoint.token,
			Model:   endpoint.model,
			Timeout: d.timeout,
		})
		if err != nil {
			lastErr = err
			continue
		}
		result, err := client.Decide(ctx, systemone.Request{
			Model:     endpoint.model,
			State:     state,
			Questions: questions,
		})
		if err == nil {
			return result, nil
		}
		lastErr = err
		slog.Warn("decision request failed, trying next account", "baseUrl", endpoint.baseURL, "error", err)
	}
	if lastErr == nil {
		lastErr = errNotConfigured
	}
	return systemone.Result{}, lastErr
}

// NewSystemOneDecider builds a decider over the configured System One
// endpoints, or returns an error when none are configured.
func NewSystemOneDecider(cfg Config) (Decider, error) {
	baseUrls, tokens, models := model.SystemOneClients(cfg.Model)
	if len(baseUrls) == 0 || len(tokens) == 0 {
		return nil, fmt.Errorf("%w for model %q", errNotConfigured, cfg.Model)
	}

	var endpoints []systemOneEndpoint
	if len(baseUrls) == len(tokens) {
		for i := range baseUrls {
			endpoints = append(endpoints, systemOneEndpoint{baseURL: baseUrls[i], token: tokens[i], model: models[i]})
		}
	} else {
		for _, baseUrl := range baseUrls {
			for _, token := range tokens {
				endpoints = append(endpoints, systemOneEndpoint{baseURL: baseUrl, token: token, model: models[0]})
			}
		}
	}
	return &systemOneDecider{endpoints: endpoints, timeout: cfg.Timeout}, nil
}

var global struct {
	mu      sync.Mutex
	decider Decider
	err     error
}

// GlobalDecider returns the shared decision model, building it on first use.
// The failure is memoized alongside the decider, matching GlobalEmbedder.
func GlobalDecider(cfg Config) (Decider, error) {
	global.mu.Lock()
	defer global.mu.Unlock()
	if global.decider != nil || global.err != nil {
		return global.decider, global.err
	}
	global.decider, global.err = NewSystemOneDecider(cfg)
	if global.err != nil {
		slog.Warn("decision model unavailable", "model", cfg.Model, "err", global.err)
	}
	return global.decider, global.err
}

// SetGlobalDeciderForTest swaps in a decider and returns a restore function.
func SetGlobalDeciderForTest(decider Decider) func() {
	global.mu.Lock()
	prevDecider := global.decider
	prevErr := global.err
	global.decider = decider
	global.err = nil
	global.mu.Unlock()

	return func() {
		global.mu.Lock()
		global.decider = prevDecider
		global.err = prevErr
		global.mu.Unlock()
	}
}

// Verdict cache
const (
	cacheTTL     = 10 * time.Minute
	maxCacheSize = 512
)

var verdictCache = struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}{
	entries: make(map[string]cacheEntry),
}

type cacheEntry struct {
	decision Decision
	created  time.Time
}

func loadCached(key string) (Decision, bool) {
	if key == "" {
		return Decision{}, false
	}
	verdictCache.mu.Lock()
	defer verdictCache.mu.Unlock()
	entry, ok := verdictCache.entries[key]
	if !ok {
		return Decision{}, false
	}
	if time.Since(entry.created) > cacheTTL {
		delete(verdictCache.entries, key)
		return Decision{}, false
	}
	return entry.decision, true
}

func storeCached(key string, decision Decision) {
	if key == "" {
		return
	}
	verdictCache.mu.Lock()
	defer verdictCache.mu.Unlock()
	if len(verdictCache.entries) >= maxCacheSize {
		var oldestKey string
		var oldest time.Time
		for k, v := range verdictCache.entries {
			if oldestKey == "" || v.created.Before(oldest) {
				oldestKey = k
				oldest = v.created
			}
		}
		if oldestKey != "" {
			delete(verdictCache.entries, oldestKey)
		}
	}
	verdictCache.entries[key] = cacheEntry{decision: decision, created: time.Now()}
}
