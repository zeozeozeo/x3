package decide

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/systemone"
)

const (
	defaultGraceWindow        = 10 * time.Second
	defaultContinuationWindow = 10 * time.Minute
	// defaultRespondThreshold is the P(yes) above which the model is taken at
	// its word. Below it but above AmbiguousFloor, a directly addressed message
	// still counts.
	defaultRespondThreshold = 0.5
	// defaultAmbiguousFloor is the band where the model is unsure. Inside it
	// the classification decides, so borderline messages addressed to the bot
	// are answered while ambient chatter is not.
	defaultAmbiguousFloor = 0.35
	defaultTimeout        = 4 * time.Second
	defaultHistoryTurns   = 6
	defaultMaxStateChars  = 4000
	defaultMiniLMFallback = true
)

// Config tunes the respond decision. LoadConfig fills it from the environment
// on every call so edits hot-reload.
type Config struct {
	// GraceWindow is how long after its own last message the bot answers
	// without consulting the decision model at all. Kept short so the model
	// owns nearly every follow-up.
	GraceWindow time.Duration
	// ContinuationWindow is how long after its last message the bot still
	// considers a message worth judging.
	ContinuationWindow time.Duration
	// RespondThreshold is the respond probability at or above which the bot
	// replies.
	RespondThreshold float64
	// AmbiguousFloor is the probability above which a directly addressed
	// message is answered even if RespondThreshold was not reached.
	AmbiguousFloor float64
	// Timeout bounds one decision model call.
	Timeout time.Duration
	// HistoryTurns is how many prior messages are included in the state.
	HistoryTurns int
	// MaxStateChars caps the rendered transcript.
	MaxStateChars int
	// MiniLMFallback enables local similarity scoring when the decision model
	// cannot be reached.
	MiniLMFallback bool
	// Model is the upstream decision model name.
	Model string
}

func LoadConfig() Config {
	return Config{
		GraceWindow:        envDuration("X3_CONTINUATION_GRACE", defaultGraceWindow),
		ContinuationWindow: envDuration("X3_CONTINUATION_WINDOW", defaultContinuationWindow),
		RespondThreshold:   envFloat64("X3_DECISION_RESPOND_THRESHOLD", defaultRespondThreshold),
		AmbiguousFloor:     envFloat64("X3_DECISION_AMBIGUOUS_FLOOR", defaultAmbiguousFloor),
		Timeout:            envDuration("X3_SYSTEMONE_TIMEOUT", defaultTimeout),
		HistoryTurns:       envInt("X3_DECISION_HISTORY_TURNS", defaultHistoryTurns),
		MaxStateChars:      envInt("X3_DECISION_MAX_STATE_CHARS", defaultMaxStateChars),
		MiniLMFallback:     envBool("X3_DECISION_MINILM_FALLBACK", defaultMiniLMFallback),
		Model:              envString("X3_SYSTEMONE_MODEL", systemone.ModelFlash),
	}.withDefaults()
}

// withDefaults fills unset fields so a zero Config is usable, which keeps tests
// from having to spell out every knob.
func (c Config) withDefaults() Config {
	if c.GraceWindow <= 0 {
		c.GraceWindow = defaultGraceWindow
	}
	if c.ContinuationWindow <= 0 {
		c.ContinuationWindow = defaultContinuationWindow
	}
	if c.RespondThreshold <= 0 {
		c.RespondThreshold = defaultRespondThreshold
	}
	if c.AmbiguousFloor <= 0 {
		c.AmbiguousFloor = defaultAmbiguousFloor
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	if c.HistoryTurns <= 0 {
		c.HistoryTurns = defaultHistoryTurns
	}
	if c.MaxStateChars <= 0 {
		c.MaxStateChars = defaultMaxStateChars
	}
	if c.Model == "" {
		c.Model = systemone.ModelFlash
	}
	return c
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		slog.Warn("invalid duration env, using default", "key", key, "value", value, "err", err)
		return fallback
	}
	return d
}

func envFloat64(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		slog.Warn("invalid float env, using default", "key", key, "value", value, "err", err)
		return fallback
	}
	return f
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	i, err := strconv.Atoi(value)
	if err != nil {
		slog.Warn("invalid int env, using default", "key", key, "value", value, "err", err)
		return fallback
	}
	return i
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		slog.Warn("invalid bool env, using default", "key", key, "value", value, "err", err)
		return fallback
	}
	return b
}
