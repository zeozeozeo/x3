package openaiapi

import (
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/opencode"
)

// opencodeHeaders returns the fingerprint headers required by the OpenCode
// Zen free tier.
func opencodeHeaders() map[string]string {
	return opencode.Headers(model.OpenCodeVersion())
}
