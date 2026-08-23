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
	"path/filepath"
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
	maxArtifacts       = 32
	maxArtifactBytes   = 8 * 1024 * 1024
	maxArtifactTotal   = 8 * 1024 * 1024
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

func (c Config) resolveExecutable() (string, error) {
	if strings.ContainsAny(c.Executable, `/\`) {
		return c.Executable, nil
	}
	if path, err := exec.LookPath(c.Executable); err == nil {
		return path, nil
	}
	for _, dir := range fallbackExecutableDirs() {
		candidate := filepath.Join(dir, c.Executable)
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf(
		"sandbox executable %q was not found: if x3 itself runs in a container, install the Docker CLI in that image and mount the host Docker socket (/var/run/docker.sock); otherwise set X3_CODE_INTERPRETER_EXECUTABLE to the binary's full path",
		c.Executable,
	)
}

func fallbackExecutableDirs() []string {
	dirs := []string{"/usr/local/bin", "/usr/bin", "/snap/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
	return dirs
}

func Run(ctx context.Context, language, code string) (Result, error) {
	return ConfigFromEnv().Run(ctx, language, code)
}

func (c Config) Run(ctx context.Context, language, code string) (Result, error) {
	if !c.Enabled {
		return Result{}, ErrDisabled
	}
	if err := c.validate(); err != nil {
		return Result{}, err
	}
	language = NormalizeLanguage(language)
	if language == "" {
		return Result{}, ErrUnsupported
	}
	if len(code) > c.MaxCodeBytes {
		return Result{}, ErrInputTooLarge
	}
	runSlotsOnce.Do(func() { runSlots = make(chan struct{}, c.MaxConcurrent) })
	select {
	case runSlots <- struct{}{}:
		defer func() { <-runSlots }()
	default:
		return Result{}, ErrBusy
	}

	executable, err := c.resolveExecutable()
	if err != nil {
		return Result{}, err
	}
	name, err := containerName()
	if err != nil {
		return Result{}, fmt.Errorf("create sandbox name: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	args := c.dockerArgs(name, language)
	cmd := exec.CommandContext(runCtx, executable, args...)
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
	_ = exec.CommandContext(cleanupCtx, executable, "rm", "--force", name).Run()

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
	if err := validateResult(&result); err != nil {
		return Result{}, fmt.Errorf("invalid sandbox response: %w", err)
	}
	return result, nil
}

func (c Config) validate() error {
	// A configurable runtime name is useful for distinct runsc profiles, but
	// accepting "runc" here would silently remove the intended security boundary.
	if c.Runtime != "runsc" && !strings.HasPrefix(c.Runtime, "runsc-") {
		return fmt.Errorf("runtime %q is not an allowed gVisor runtime name", c.Runtime)
	}
	if strings.TrimSpace(c.Image) == "" || strings.HasPrefix(c.Image, "-") || strings.ContainsAny(c.Image, " \t\r\n\x00") {
		return errors.New("invalid sandbox image reference")
	}
	if !validDockerSize(c.Memory) || !validDockerSize(c.Disk) {
		return errors.New("invalid sandbox memory or disk limit")
	}
	if c.MaxConcurrent <= 0 || c.Timeout <= 0 || c.MaxCodeBytes <= 0 || c.MaxOutputBytes <= 0 {
		return errors.New("invalid sandbox resource limits")
	}
	return nil
}

func validDockerSize(value string) bool {
	value = strings.ToLower(value)
	if len(value) < 2 {
		return false
	}
	suffix := value[len(value)-1]
	if suffix != 'b' && suffix != 'k' && suffix != 'm' && suffix != 'g' {
		return false
	}
	_, err := strconv.ParseUint(value[:len(value)-1], 10, 64)
	return err == nil && value[0] != '0'
}

func (c Config) dockerArgs(name, language string) []string {
	args := []string{
		"run", "--rm", "--interactive", "--name", name,
		"--pull", "never", "--log-driver", "none", "--restart", "no", "--stop-timeout", "1",
		"--runtime", c.Runtime,
		"--network", "none", "--ipc", "none",
		// Only the trusted supervisor receives these three capabilities so it
		// can prepare /work ownership and drop the child UID/GID. Linux clears
		// them when the child changes from UID 0 to UID 65534 before exec, and
		// non-root processes cannot exercise them.
		"--read-only", "--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "SETUID", "--cap-add", "SETGID",
		"--security-opt", "no-new-privileges=true",
		"--pids-limit", "64", "--memory", c.Memory, "--memory-swap", c.Memory,
		"--cpus", c.CPUs,
		"--ulimit", "nofile=64:64", "--ulimit", "nproc=64:64", "--ulimit", "fsize=16777216:16777216", "--ulimit", "core=0:0", "--ulimit", "memlock=0:0",
		"--tmpfs", "/work:rw,noexec,nosuid,nodev,size=" + c.Disk + ",uid=65534,gid=65534,mode=0700",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=4m,mode=01777",
		// The trusted supervisor starts as sandbox-root and permanently drops the
		// child to nobody. This prevents user code from signalling the supervisor
		// or reopening its stdout through /proc to bypass output accounting.
		"--workdir", "/work", "--user", "0:0",
		"--env", "HOME=/work", "--env", "MPLBACKEND=Agg", "--env", "MPLCONFIGDIR=/work/.mpl",
		c.Image, "python3", "/opt/x3/runner.py", language,
	}
	return args
}

func validateResult(result *Result) error {
	if len(result.Artifacts) > maxArtifacts {
		return fmt.Errorf("too many artifacts: %d", len(result.Artifacts))
	}
	total := 0
	seen := make(map[string]struct{}, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		if !safeArtifactName(artifact.Name) {
			return fmt.Errorf("unsafe artifact name %q", artifact.Name)
		}
		if _, ok := seen[artifact.Name]; ok {
			return fmt.Errorf("duplicate artifact name %q", artifact.Name)
		}
		seen[artifact.Name] = struct{}{}
		if len(artifact.Data) > maxArtifactBytes || total > maxArtifactTotal-len(artifact.Data) {
			return fmt.Errorf("artifact data exceeds limit")
		}
		total += len(artifact.Data)
	}
	return nil
}

func safeArtifactName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 255 || filepathBase(name) != name {
		return false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

// filepath.Base is OS-dependent; artifact names are protocol values and must
// reject both slash styles even when x3 is built on Unix.
func filepathBase(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(name, "/")
	return parts[len(parts)-1]
}

func NormalizeLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "py", "python", "python3":
		return "python"
	case "lua", "lua5.4":
		return "lua"
	case "js", "javascript", "node", "nodejs", "bun":
		return "javascript"
	case "ts", "typescript":
		return "typescript"
	case "rb", "ruby":
		return "ruby"
	case "php", "php8", "php8.2":
		return "php"
	case "pl", "perl", "perl5":
		return "perl"
	case "bash", "sh", "shell":
		return "shell"
	default:
		return ""
	}
}

// SupportedLanguages returns the canonical language names accepted by Run.
func SupportedLanguages() []string {
	return []string{"python", "javascript", "typescript", "lua", "ruby", "php", "perl", "shell"}
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
