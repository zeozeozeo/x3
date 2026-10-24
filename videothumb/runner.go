package videothumb

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultSandboxImage = "x3-ffmpeg-sandbox:latest"
	defaultRuntime      = "runsc"
	defaultTimeout      = 30 * time.Second
	defaultMemory       = "512m"
	defaultCPUs         = "1"
	defaultDisk         = "256m"
	defaultMaxDownload  = 64 * 1024 * 1024
	defaultMaxConc      = 1
)

// Config carries the ffmpeg backend settings, mirroring the code interpreter
// configuration style.
type Config struct {
	FfmpegPath       string
	SandboxEnabled   bool
	Executable       string // docker CLI for the sandbox backend
	Image            string
	Runtime          string
	Timeout          time.Duration
	Memory           string
	CPUs             string
	Disk             string
	MaxDownloadBytes int
	MaxConcurrent    int
}

func ConfigFromEnv() Config {
	return Config{
		FfmpegPath:       envString("X3_FFMPEG_PATH", "ffmpeg"),
		SandboxEnabled:   envBool("X3_FFMPEG_SANDBOX_ENABLED", false),
		Executable:       envString("X3_FFMPEG_SANDBOX_EXECUTABLE", "docker"),
		Image:            envString("X3_FFMPEG_SANDBOX_IMAGE", defaultSandboxImage),
		Runtime:          envString("X3_FFMPEG_SANDBOX_RUNTIME", defaultRuntime),
		Timeout:          envDuration("X3_FFMPEG_SANDBOX_TIMEOUT", defaultTimeout),
		Memory:           envString("X3_FFMPEG_SANDBOX_MEMORY", defaultMemory),
		CPUs:             envString("X3_FFMPEG_SANDBOX_CPUS", defaultCPUs),
		Disk:             envString("X3_FFMPEG_SANDBOX_DISK", defaultDisk),
		MaxDownloadBytes: envIntBytes("X3_VIDEO_MAX_DOWNLOAD_MB", defaultMaxDownload),
		MaxConcurrent:    envInt("X3_FFMPEG_SANDBOX_MAX_CONCURRENT", defaultMaxConc),
	}
}

// runFFmpeg decodes a single frame from the video with the configured
// backend: the gVisor sandbox when enabled, otherwise the host binary.
func runFFmpeg(ctx context.Context, cfg Config, data []byte, seek string) ([]byte, error) {
	runSlotsOnce.Do(func() { runSlots = make(chan struct{}, max(cfg.MaxConcurrent, 1)) })
	select {
	case runSlots <- struct{}{}:
		defer func() { <-runSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if cfg.SandboxEnabled {
		return runSandboxFFmpeg(ctx, cfg, data, seek)
	}
	return runHostFFmpeg(ctx, cfg, data, seek)
}

// ffmpegArgs seeks into the input, grabs one frame, bounds it to 1280px wide
// and writes a single JPEG to stdout.
func ffmpegArgs(input string, seek string) []string {
	return []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-ss", seek,
		"-i", input,
		"-frames:v", "1", "-q:v", "3",
		"-vf", "scale=w=1280:h=-2:force_original_aspect_ratio=decrease",
		"-f", "mjpeg", "-",
	}
}

func runHostFFmpeg(ctx context.Context, cfg Config, data []byte, seek string) ([]byte, error) {
	path := resolveFfmpegPath(cfg.FfmpegPath)
	if path == "" {
		return nil, errors.New("ffmpeg binary not found")
	}
	if err := os.MkdirAll("x3-temp", 0755); err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	tmp, err := os.CreateTemp("x3-temp", "videothumb-*.bin")
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	inPath := tmp.Name()
	defer os.Remove(inPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp file: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path, ffmpegArgs(inPath, seek)...)
	var stdout limitedBuffer
	stdout.limit = MaxThumbnailBytes + 1
	var stderr limitedBuffer
	stderr.limit = 64 * 1024
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if runCtx.Err() != nil {
		return nil, fmt.Errorf("ffmpeg timed out after %s: %w", cfg.Timeout, runCtx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func runSandboxFFmpeg(ctx context.Context, cfg Config, data []byte, seek string) ([]byte, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	executable, err := resolveSandboxExecutable(cfg.Executable)
	if err != nil {
		return nil, err
	}
	name, err := containerName()
	if err != nil {
		return nil, fmt.Errorf("create sandbox name: %w", err)
	}
	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// The video arrives on stdin because mp4s usually need a seekable input,
	// and no host directory is mounted so this works with a mounted Docker
	// socket from inside the bot's own container.
	cmd := exec.CommandContext(runCtx, executable, cfg.dockerArgs(name, seek)...)
	cmd.Stdin = bytes.NewReader(data)
	var stdout limitedBuffer
	stdout.limit = MaxThumbnailBytes + 1
	var stderr limitedBuffer
	stderr.limit = 64 * 1024
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	// Killing the CLI does not reliably kill a container already accepted by
	// the daemon. The random, known name makes this cleanup precise.
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	_ = exec.CommandContext(cleanupCtx, executable, "rm", "--force", name).Run()

	if errors.Is(stdout.err, errOutputTooBig) {
		return nil, errOutputTooBig
	}
	if runCtx.Err() != nil {
		return nil, fmt.Errorf("sandbox timed out after %s: %w", cfg.Timeout, runCtx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("sandbox failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (c Config) validate() error {
	// A configurable runtime name is useful for distinct runsc profiles, but
	// accepting "runc" here would silently remove the intended security
	// boundary.
	if c.Runtime != "runsc" && !strings.HasPrefix(c.Runtime, "runsc-") {
		return fmt.Errorf("runtime %q is not an allowed gVisor runtime name", c.Runtime)
	}
	if strings.TrimSpace(c.Image) == "" || strings.HasPrefix(c.Image, "-") || strings.ContainsAny(c.Image, " \t\r\n\x00") {
		return errors.New("invalid sandbox image reference")
	}
	if !validDockerSize(c.Memory) || !validDockerSize(c.Disk) {
		return errors.New("invalid sandbox memory or disk limit")
	}
	if c.MaxConcurrent <= 0 || c.Timeout <= 0 || c.MaxDownloadBytes <= 0 {
		return errors.New("invalid sandbox resource limits")
	}
	diskBytes, _ := dockerSizeBytes(c.Disk)
	// The tmpfs holds the input video plus ffmpeg scratch space.
	if diskBytes > 0 && diskBytes < int64(c.MaxDownloadBytes)+8*1024*1024 {
		return fmt.Errorf("sandbox disk %s is smaller than the video download limit", c.Disk)
	}
	return nil
}

func (c Config) dockerArgs(name, seek string) []string {
	// fsize must allow the input video plus some slack.
	fsize := c.MaxDownloadBytes + 16*1024*1024
	return []string{
		"run", "--rm", "--interactive", "--name", name,
		"--pull", "never", "--log-driver", "none", "--restart", "no", "--stop-timeout", "1",
		"--runtime", c.Runtime,
		"--network", "none", "--ipc", "none",
		"--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges=true",
		"--pids-limit", "64", "--memory", c.Memory, "--memory-swap", c.Memory,
		"--cpus", c.CPUs,
		"--ulimit", "nofile=64:64", "--ulimit", "nproc=64:64",
		"--ulimit", fmt.Sprintf("fsize=%d:%d", fsize, fsize),
		"--ulimit", "core=0:0", "--ulimit", "memlock=0:0",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=" + c.Disk + ",mode=1777",
		"--workdir", "/tmp", "--user", "0:0",
		c.Image, "sh", "-c", sandboxScript(seek),
	}
}

// sandboxScript stages stdin into the tmpfs (mp4 input must be seekable) and
// writes one JPEG frame back on stdout.
func sandboxScript(seek string) string {
	return "cat > /tmp/video.bin && exec ffmpeg -hide_banner -loglevel error " +
		"-ss " + seek + " -i /tmp/video.bin -frames:v 1 -q:v 3 " +
		"-vf scale=w=1280:h=-2:force_original_aspect_ratio=decrease -f mjpeg -"
}

func containerName() (string, error) {
	b := make([]byte, 12)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return "x3-ffmpeg-" + hex.EncodeToString(b), nil
}

func resolveSandboxExecutable(executable string) (string, error) {
	if strings.ContainsAny(executable, `/\`) {
		return executable, nil
	}
	if path, err := exec.LookPath(executable); err == nil {
		return path, nil
	}
	for _, dir := range fallbackExecutableDirs() {
		candidate := filepath.Join(dir, executable)
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf(
		"sandbox executable %q was not found: set X3_FFMPEG_SANDBOX_EXECUTABLE to the docker CLI binary's full path",
		executable,
	)
}

// resolveFfmpegPath locates the host ffmpeg binary; "" when unavailable.
func resolveFfmpegPath(configured string) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = "ffmpeg"
	}
	if strings.ContainsAny(configured, `/\`) {
		if _, err := exec.LookPath(configured); err == nil {
			return configured
		}
		return ""
	}
	if path, err := exec.LookPath(configured); err == nil {
		return path
	}
	for _, dir := range fallbackExecutableDirs() {
		candidate := filepath.Join(dir, configured)
		if _, err := exec.LookPath(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func fallbackExecutableDirs() []string {
	dirs := []string{"/usr/local/bin", "/usr/bin", "/snap/bin"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"))
	}
	return dirs
}

func validDockerSize(value string) bool {
	_, ok := dockerSizeBytes(value)
	return ok
}

func dockerSizeBytes(value string) (int64, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) < 2 {
		return 0, false
	}
	mult := int64(1)
	switch value[len(value)-1] {
	case 'b':
		mult = 1
	case 'k':
		mult = 1024
	case 'm':
		mult = 1024 * 1024
	case 'g':
		mult = 1024 * 1024 * 1024
	default:
		return 0, false
	}
	n, err := strconv.ParseUint(value[:len(value)-1], 10, 64)
	if err != nil || value[0] == '0' {
		return 0, false
	}
	return int64(n) * mult, true
}

var (
	runSlotsOnce sync.Once
	runSlots     chan struct{}
)

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int64
	err   error
}

var errOutputTooBig = errors.New("output exceeds the configured limit")

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.err = errOutputTooBig
		return 0, b.err
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.err = errOutputTooBig
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

// envIntBytes reads a megabyte value and returns bytes.
func envIntBytes(key string, fallbackBytes int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallbackBytes
	}
	return value * 1024 * 1024
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
