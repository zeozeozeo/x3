package minilm

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultGraceWindow        = 20 * time.Second
	defaultContinuationWindow = 10 * time.Minute
	defaultSimilarity         = 0.3
	defaultModelPath          = "models/minilm/all-MiniLM-L6-v2.onnx"
)

type Config struct {
	GraceWindow        time.Duration
	ContinuationWindow time.Duration
	Similarity         float32
	ModelPath          string
	RuntimeLibraryPath string
}

func LoadConfig() Config {
	return Config{
		GraceWindow:        envDuration("X3_CONTINUATION_GRACE", defaultGraceWindow),
		ContinuationWindow: envDuration("X3_CONTINUATION_WINDOW", defaultContinuationWindow),
		Similarity:         envFloat32("X3_MINILM_SIMILARITY", defaultSimilarity),
		ModelPath:          envString("X3_MINILM_MODEL_PATH", defaultModelPath),
		RuntimeLibraryPath: firstNonEmpty(strings.TrimSpace(os.Getenv("X3_MINILM_ONNX_RUNTIME_LIB")), strings.TrimSpace(os.Getenv("ONNXRUNTIME_LIB_PATH")), "libonnxruntime.so"),
	}
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
