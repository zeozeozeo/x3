package commands

import (
	"os"
	"strings"
)

func summariesEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("X3_ENABLE_SUMMARIES")))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
