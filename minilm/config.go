package minilm

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

const (
	defaultSimilarity        = 0.3
	defaultDefaultSimilarity = 0.55
	defaultModelPath         = "models/minilm/all-MiniLM-L6-v2.onnx"
)

// Config configures the local embedding model and the thresholds used when the
// decision model is unavailable and continuation scoring takes over. The
// response windows live in the decide package, which owns the policy.
type Config struct {
	Similarity         float32
	DefaultSimilarity  float32
	ModelPath          string
	RuntimeLibraryPath string
}

func LoadConfig() Config {
	return Config{
		Similarity:         envFloat32("X3_MINILM_SIMILARITY", defaultSimilarity),
		DefaultSimilarity:  envFloat32("X3_MINILM_DEFAULT_SIMILARITY", defaultDefaultSimilarity),
		ModelPath:          envString("X3_MINILM_MODEL_PATH", defaultModelPath),
		RuntimeLibraryPath: firstNonEmpty(strings.TrimSpace(os.Getenv("X3_MINILM_ONNX_RUNTIME_LIB")), strings.TrimSpace(os.Getenv("ONNXRUNTIME_LIB_PATH"))),
	}
}

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envFloat32(key string, fallback float32) float32 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(value, 32)
	if err != nil {
		slog.Warn("invalid float env, using default", "key", key, "value", value, "err", err)
		return fallback
	}
	return float32(f)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
