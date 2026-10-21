package opencode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"runtime"
	"strings"

	"github.com/zeozeozeo/x3/openai"
)

const (
	// DefaultVersion is the OpenCode version advertised in the User-Agent.
	// The gate requires at least 1.18.0.
	DefaultVersion = "1.19.3"

	// MinVersion is the oldest OpenCode version the Zen free tier accepts.
	MinVersion = "1.18.0"

	// GateToolName is the tool name the free tier gate requires in the
	// request body.
	GateToolName = "read"

	sessionIDPrefix = "ses_"
	requestIDPrefix = "msg_"
	sessionHexLen   = 12
	sessionB62Len   = 14
	requestB62Len   = 26
)

const b62Alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

const hexAlphabet = "0123456789abcdef"

// randomString returns n characters picked from the given alphabet.
func randomString(alphabet string, n int) string {
	if n <= 0 {
		return ""
	}
	var sb strings.Builder
	sb.Grow(n)
	max := big.NewInt(int64(len(alphabet)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand should not fail; fall back to a fixed character
			// rather than sending a malformed fingerprint.
			sb.WriteByte(alphabet[0])
			continue
		}
		sb.WriteByte(alphabet[v.Int64()])
	}
	return sb.String()
}

// SessionID returns an OpenCode session id: ses_ followed by 12 lowercase hex
// characters and 14 base62 characters.
func SessionID() string {
	return sessionIDPrefix + randomString(hexAlphabet, sessionHexLen) + randomString(b62Alphabet, sessionB62Len)
}

// RequestID returns an OpenCode request id: msg_ followed by 26 base62
// characters.
func RequestID() string {
	return requestIDPrefix + randomString(b62Alphabet, requestB62Len)
}

// UserAgent returns the User-Agent value the Zen free tier expects.
func UserAgent(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = DefaultVersion
	}
	return fmt.Sprintf("opencode/%s (%s; %s)", version, runtime.GOOS, runtime.GOARCH)
}

// Headers returns the headers that make up the client fingerprint.
func Headers(version string) map[string]string {
	return map[string]string{
		"User-Agent":         UserAgent(version),
		"x-opencode-client":  "cli",
		"x-opencode-project": "global",
		"x-opencode-session": SessionID(),
		"x-opencode-request": RequestID(),
	}
}

// ReadTool returns the tool the free tier gate requires. The tool is a stub:
// the bot cannot read files, so it is described as unavailable.
func ReadTool() openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        GateToolName,
			Description: "Read a file from disk. This tool is not available in this environment; never call it.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

// HasGateTool reports whether the tool list already contains the tool the
// free tier gate requires.
func HasGateTool(tools []openai.Tool) bool {
	for _, tool := range tools {
		if tool.Function != nil && tool.Function.Name == GateToolName {
			return true
		}
	}
	return false
}

// EnsureGateTool appends the gate tool to the tool list if it is missing.
func EnsureGateTool(tools []openai.Tool) []openai.Tool {
	if HasGateTool(tools) {
		return tools
	}
	return append(tools, ReadTool())
}
