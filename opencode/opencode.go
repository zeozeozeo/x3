package opencode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const (
	// DefaultVersion is the OpenCode version advertised in the User-Agent.
	// OpenCode 2.0 changed the fingerprint format, so 2.2.0 is the version the
	// Console and free tier recognize.
	DefaultVersion = "2.2.0"

	// MinVersion is the oldest OpenCode version with the v2 fingerprint.
	MinVersion = "2.0.0"

	// DefaultChannel is the release channel embedded in the User-Agent.
	DefaultChannel = "latest"

	// ClientName is the application name embedded in the User-Agent and the
	// x-opencode-client header.
	ClientName = "opencode"

	// ProjectID is the global project id used by the CLI for untitled
	// sessions.
	ProjectID = "global"

	sessionIDPrefix = "ses_"
	sessionHexLen   = 12
	sessionB62Len   = 14
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
// characters and 14 base62 characters. OpenCode 2.x keeps this format.
func SessionID() string {
	return sessionIDPrefix + randomString(hexAlphabet, sessionHexLen) + randomString(b62Alphabet, sessionB62Len)
}

// UserAgent returns the User-Agent value OpenCode 2.x sends:
// opencode/<channel>/<version>/<name>.
func UserAgent(version string) string {
	return UserAgentWithChannel(version, DefaultChannel)
}

// UserAgentWithChannel is UserAgent with an explicit release channel.
func UserAgentWithChannel(version, channel string) string {
	version = NormalizeVersion(version)
	channel = strings.TrimSpace(channel)
	if channel == "" {
		channel = DefaultChannel
	}
	return fmt.Sprintf("opencode/%s/%s/%s", channel, version, ClientName)
}

// NormalizeVersion returns a version string the current OpenCode fingerprint
// understands. Values older than MinVersion (such as a stale 1.x setting) are
// upgraded to DefaultVersion instead of advertising the retired v1 client.
func NormalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || compareVersions(version, MinVersion) < 0 {
		return DefaultVersion
	}
	return version
}

// compareVersions compares dotted numeric versions. It returns -1, 0 or 1.
func compareVersions(a, b string) int {
	as := strings.Split(strings.TrimSpace(a), ".")
	bs := strings.Split(strings.TrimSpace(b), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av = atoi(as[i])
		}
		if i < len(bs) {
			bv = atoi(bs[i])
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// Headers returns the headers that make up the OpenCode 2.x client
// fingerprint. The same session id is used for every cache-affinity header so
// the upstream can group requests from this conversation.
func Headers(version string) map[string]string {
	return HeadersWithChannel(version, DefaultChannel)
}

// HeadersWithChannel is Headers with an explicit release channel.
func HeadersWithChannel(version, channel string) map[string]string {
	sessionID := SessionID()
	return map[string]string{
		"User-Agent":            UserAgentWithChannel(version, channel),
		"x-opencode-client":     ClientName,
		"x-opencode-project":    ProjectID,
		"x-opencode-session":    sessionID,
		"x-opencode-session-id": sessionID,
		"x-session-affinity":    sessionID,
		"X-Session-Id":          sessionID,
	}
}
