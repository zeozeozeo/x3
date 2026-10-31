package openaiapi

import (
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/opencode"
)

// opencodeHeaders returns the fingerprint headers OpenCode 2.x requires.
func opencodeHeaders() map[string]string {
	return opencode.HeadersWithChannel(model.OpenCodeVersion(), model.OpenCodeChannel())
}
