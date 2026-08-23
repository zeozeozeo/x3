package codeinterp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeLanguage(t *testing.T) {
	tests := map[string]string{
		"py": "python", "Python3": "python", "lua": "lua", "js": "javascript",
		"node": "javascript", "bun": "javascript", "ts": "typescript", "rb": "ruby",
		"php8": "php", "pl": "perl", "bash": "shell", "brainfuck": "",
	}
	for input, want := range tests {
		if got := NormalizeLanguage(input); got != want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSupportedLanguagesNormalizeToThemselves(t *testing.T) {
	for _, language := range SupportedLanguages() {
		if got := NormalizeLanguage(language); got != language {
			t.Errorf("NormalizeLanguage(%q) = %q", language, got)
		}
	}
}

func TestDockerArgsAreHardened(t *testing.T) {
	c := Config{Runtime: "runsc", Image: "sandbox:test", Memory: "128m", CPUs: "0.5", Disk: "8m"}
	joined := strings.Join(c.dockerArgs("x3-code-test", "python"), " ")
	for _, required := range []string{"--runtime runsc", "--network none", "--read-only", "--cap-drop ALL", "--cap-add SETUID", "--cap-add SETGID", "no-new-privileges=true", "--pids-limit 64", "--memory 128m", "--memory-swap 128m", "/work:rw,noexec,nosuid,nodev,size=8m", "--user 0:0", "--pull never", "--log-driver none", "core=0:0", "memlock=0:0"} {
		if !strings.Contains(joined, required) {
			t.Errorf("docker args missing %q: %s", required, joined)
		}
	}
}

func TestValidateResultRejectsUnsafeArtifacts(t *testing.T) {
	tests := []string{"../secret", `..\\secret`, "a/b", "bad\nname", ""}
	for _, name := range tests {
		result := Result{Artifacts: []Artifact{{Name: name}}}
		if err := validateResult(&result); err == nil {
			t.Errorf("validateResult accepted artifact name %q", name)
		}
	}
	if err := validateResult(&Result{Artifacts: []Artifact{{Name: "plot.png", Data: []byte("ok")}}}); err != nil {
		t.Fatalf("validateResult rejected safe artifact: %v", err)
	}
}

func TestConfigRejectsNonGVisorRuntimeAndOptionLikeImage(t *testing.T) {
	base := Config{
		Runtime: "runsc", Image: "sandbox:test", Memory: "128m", Disk: "8m", Timeout: time.Second,
		MaxConcurrent: 1, MaxCodeBytes: 1, MaxOutputBytes: 1,
	}
	badRuntime := base
	badRuntime.Runtime = "runc"
	if err := badRuntime.validate(); err == nil {
		t.Fatal("accepted runc as a sandbox runtime")
	}
	badImage := base
	badImage.Image = "--privileged"
	if err := badImage.validate(); err == nil {
		t.Fatal("accepted option-like image reference")
	}
	badDisk := base
	badDisk.Disk = "8m,exec"
	if err := badDisk.validate(); err == nil {
		t.Fatal("accepted mount-option injection in disk limit")
	}
	customRunsc := base
	customRunsc.Runtime = "runsc-systrap"
	if err := customRunsc.validate(); err != nil {
		t.Fatalf("rejected named runsc profile: %v", err)
	}
}

func TestLimitedBuffer(t *testing.T) {
	b := limitedBuffer{limit: 3}
	n, err := b.Write([]byte("hello"))
	if n != 3 || !errors.Is(err, ErrResponseTooBig) || b.String() != "hel" {
		t.Fatalf("Write = (%d, %v, %q)", n, err, b.String())
	}
}

func TestResolveExecutablePassesThroughPaths(t *testing.T) {
	c := Config{Executable: "/opt/docker/bin/docker"}
	got, err := c.resolveExecutable()
	if err != nil || got != c.Executable {
		t.Fatalf("resolveExecutable = (%q, %v)", got, err)
	}
}

func TestResolveExecutableFallsBackToHomeBin(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "x3-fake-docker.exe"
	fake := filepath.Join(binDir, name)
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	c := Config{Executable: name}
	got, err := c.resolveExecutable()
	if err != nil {
		t.Fatalf("resolveExecutable: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(fake) {
		t.Fatalf("resolveExecutable = %q, want %q", got, fake)
	}
}

func TestResolveExecutableErrorMentionsFixes(t *testing.T) {
	c := Config{Executable: "x3-definitely-missing-cli"}
	_, err := c.resolveExecutable()
	if err == nil {
		t.Fatal("expected an error for a missing sandbox executable")
	}
	for _, want := range []string{"X3_CODE_INTERPRETER_EXECUTABLE", "Docker socket"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
