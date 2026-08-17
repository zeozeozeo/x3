// Package codeinterp runs untrusted snippets in a locked-down OCI container.
package codeinterp

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultImage       = "x3-code-sandbox:latest"
	defaultRuntime     = "runsc"
	defaultTimeout     = 10 * time.Second
	defaultMemory      = "256m"
	defaultCPUs        = "1"
	defaultDisk        = "32m"
	defaultMaxCode     = 32 * 1024
	defaultMaxResponse = 12 * 1024 * 1024
)

var (
	ErrDisabled       = errors.New("code interpreter is disabled")
	ErrUnsupported    = errors.New("unsupported language")
	ErrInputTooLarge  = errors.New("code exceeds the configured size limit")
	ErrResponseTooBig = errors.New("sandbox response exceeds the configured size limit")
	ErrBusy           = errors.New("all code sandbox slots are busy")
)

var (
	runSlotsOnce sync.Once
	runSlots     chan struct{}
)

type Artifact struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"data"`
}

type Result struct {
	Stdout    string     `json:"stdout"`
	Stderr    string     `json:"stderr"`
	ExitCode  int        `json:"exit_code"`
	Artifacts []Artifact `json:"artifacts"`
}

type Config struct {
	Enabled        bool
	Executable     string
	Image          string
	Runtime        string
	Timeout        time.Duration
	Memory         string
	CPUs           string
	Disk           string
	MaxCodeBytes   int
	MaxOutputBytes int64
	MaxConcurrent  int
}

func ConfigFromEnv() Config {
	return Config{
		Enabled:        envBool("X3_CODE_INTERPRETER_ENABLED", false),
		Executable:     envString("X3_CODE_INTERPRETER_EXECUTABLE", "docker"),
		Image:          envString("X3_CODE_INTERPRETER_IMAGE", defaultImage),
		Runtime:        envString("X3_CODE_INTERPRETER_RUNTIME", defaultRuntime),
		Timeout:        envDuration("X3_CODE_INTERPRETER_TIMEOUT", defaultTimeout),
		Memory:         envString("X3_CODE_INTERPRETER_MEMORY", defaultMemory),
		CPUs:           envString("X3_CODE_INTERPRETER_CPUS", defaultCPUs),
		Disk:           envString("X3_CODE_INTERPRETER_DISK", defaultDisk),
		MaxCodeBytes:   envInt("X3_CODE_INTERPRETER_MAX_CODE_BYTES", defaultMaxCode),
		MaxOutputBytes: int64(envInt("X3_CODE_INTERPRETER_MAX_OUTPUT_BYTES", defaultMaxResponse)),
		MaxConcurrent:  envInt("X3_CODE_INTERPRETER_MAX_CONCURRENT", 2),
	}
}

func Enabled() bool { return ConfigFromEnv().Enabled }

func Run(ctx context.Context, language, code string) (Result, error) {
	return ConfigFromEnv().Run(ctx, language, code)
}

func (c Config) Run(ctx context.Context, language, code string) (Result, error) {
	if !c.Enabled {
		return Result{}, ErrDisabled
	}
	language = NormalizeLanguage(language)
	if language == "" {
		return Result{}, ErrUnsupported
	}
	if len(code) > c.MaxCodeBytes {
		return Result{}, ErrInputTooLarge
	}
	if strings.TrimSpace(c.Runtime) == "" {
		return Result{}, errors.New("a hardened OCI runtime is required")
	}
	runSlotsOnce.Do(func() { runSlots = make(chan struct{}, c.MaxConcurrent) })
	select {
	case runSlots <- struct{}{}:
		defer func() { <-runSlots }()
	default:
		return Result{}, ErrBusy
	}

	name, err := containerName()
	if err != nil {
		return Result{}, fmt.Errorf("create sandbox name: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	args := c.dockerArgs(name, language)
	cmd := exec.CommandContext(runCtx, c.Executable, args...)
	cmd.Stdin = strings.NewReader(code)
	var stdout limitedBuffer
	stdout.limit = c.MaxOutputBytes
	var stderr limitedBuffer
	stderr.limit = 64 * 1024
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	// Killing the CLI does not reliably kill a container already accepted by the
	// daemon. The random, known name makes this cleanup precise and idempotent.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	_ = exec.CommandContext(cleanupCtx, c.Executable, "rm", "--force", name).Run()

	if errors.Is(stdout.err, ErrResponseTooBig) {
		return Result{}, ErrResponseTooBig
	}
	if runCtx.Err() != nil {
		return Result{}, fmt.Errorf("sandbox timed out after %s: %w", c.Timeout, runCtx.Err())
	}
	if err != nil {
		return Result{}, fmt.Errorf("sandbox failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var result Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return Result{}, fmt.Errorf("decode sandbox response: %w", err)
	}
	return result, nil
}

func (c Config) dockerArgs(name, language string) []string {
	args := []string{
		"run", "--rm", "--interactive", "--name", name,
		"--runtime", c.Runtime,
		"--network", "none", "--ipc", "none",
		"--read-only", "--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--pids-limit", "64", "--memory", c.Memory, "--memory-swap", c.Memory,
		"--cpus", c.CPUs,
		"--ulimit", "nofile=64:64", "--ulimit", "nproc=64:64", "--ulimit", "fsize=16777216:16777216",
		"--tmpfs", "/work:rw,noexec,nosuid,nodev,size=" + c.Disk,
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=4m",
		"--workdir", "/work", "--user", "65534:65534",
		"--env", "HOME=/work", "--env", "MPLBACKEND=Agg", "--env", "MPLCONFIGDIR=/work/.mpl",
		c.Image, "python3", "/opt/x3/runner.py", language,
	}
	return args
}

func NormalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "py", "python", "python3":
		return "python"
	case "lua", "lua5.4":
		return "lua"
	case "js", "javascript", "node", "nodejs":
		return "javascript"
	default:
		return ""
	}
}

func containerName() (string, error) {
	b := make([]byte, 12)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return "x3-code-" + hex.EncodeToString(b), nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int64
	err   error
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.err = ErrResponseTooBig
		return 0, b.err
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.err = ErrResponseTooBig
		return int(remaining), b.err
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }

func envString(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

var _ io.Writer = (*limitedBuffer)(nil)
