package codeinterp

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeLanguage(t *testing.T) {
	tests := map[string]string{"py": "python", "Python3": "python", "lua": "lua", "js": "javascript", "node": "javascript", "ruby": ""}
	for input, want := range tests {
		if got := NormalizeLanguage(input); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDockerArgsAreHardened(t *testing.T) {
	c := Config{Runtime: "runsc", Image: "sandbox:test", Memory: "128m", CPUs: "0.5", Disk: "8m"}
	joined := strings.Join(c.dockerArgs("x3-code-test", "python"), " ")
	for _, required := range []string{"--runtime runsc", "--network none", "--read-only", "--cap-drop ALL", "no-new-privileges", "--pids-limit 64", "--memory 128m", "--memory-swap 128m", "/work:rw,noexec,nosuid,nodev,size=8m", "--user 65534:65534"} {
		if !strings.Contains(joined, required) {
			t.Errorf("docker args missing %q: %s", required, joined)
		}
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := limitedBuffer{limit: 3}
	n, err := b.Write([]byte("hello"))
	if n != 3 || !errors.Is(err, ErrResponseTooBig) || b.String() != "hel" {
		t.Fatalf("Write = (%d, %v, %q)", n, err, b.String())
	}
}
