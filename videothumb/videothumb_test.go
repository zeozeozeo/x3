package videothumb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jpegHeader is the start of a minimal JPEG stream (SOI + next marker byte).
var jpegHeader = []byte{0xFF, 0xD8, 0xFF, 0xE0}

func TestDockerArgsAreHardened(t *testing.T) {
	c := Config{
		Runtime: "runsc", Image: "x3-ffmpeg-sandbox:test", Memory: "512m", CPUs: "1", Disk: "256m",
		MaxDownloadBytes: 64 * 1024 * 1024,
	}
	joined := strings.Join(c.dockerArgs("x3-ffmpeg-test", "1"), " ")
	for _, required := range []string{
		"--runtime runsc", "--network none", "--ipc none", "--read-only", "--cap-drop ALL",
		"no-new-privileges=true", "--pids-limit 64", "--memory 512m", "--memory-swap 512m",
		"/tmp:rw,noexec,nosuid,nodev,size=256m,mode=1777", "--user 0:0",
		"--pull never", "--log-driver none", "core=0:0", "memlock=0:0",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("docker args missing %q: %s", required, joined)
		}
	}
	script := c.dockerArgs("x3-ffmpeg-test", "1")
	if script[len(script)-1] != sandboxScript("1") {
		t.Errorf("args must end with the sandbox script, got %q", script[len(script)-1])
	}
	if !strings.Contains(sandboxScript("1"), "-ss 1 ") || !strings.Contains(sandboxScript("1"), "-frames:v 1") {
		t.Errorf("sandbox script missing frame flags: %s", sandboxScript("1"))
	}
}

func TestConfigRejectsNonGVisorRuntimeAndBadLimits(t *testing.T) {
	base := Config{
		Runtime: "runsc", Image: "x3-ffmpeg-sandbox:latest", Memory: "512m", Disk: "256m",
		Timeout: time.Second, MaxConcurrent: 1, MaxDownloadBytes: 8 * 1024 * 1024,
	}
	if err := base.validate(); err != nil {
		t.Fatalf("validate rejected valid config: %v", err)
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
	badDisk.Disk = "8m"
	if err := badDisk.validate(); err == nil {
		t.Fatal("accepted a tmpfs smaller than the video download limit")
	}
	customRunsc := base
	customRunsc.Runtime = "runsc-systrap"
	if err := customRunsc.validate(); err != nil {
		t.Fatalf("rejected named runsc profile: %v", err)
	}
}

func TestDockerSizeBytes(t *testing.T) {
	tests := map[string]struct {
		want int64
		ok   bool
	}{
		"512m":    {512 * 1024 * 1024, true},
		"1g":      {1024 * 1024 * 1024, true},
		"16k":     {16 * 1024, true},
		"2b":      {2, true},
		"8m,exec": {0, false},
		"":        {0, false},
		"m":       {0, false},
		"0m":      {0, false},
		"12x":     {0, false},
	}
	for input, want := range tests {
		got, ok := dockerSizeBytes(input)
		if got != want.want || ok != want.ok {
			t.Errorf("dockerSizeBytes(%q) = (%d, %v), want (%d, %v)", input, got, ok, want.want, want.ok)
		}
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("X3_VIDEO_THUMBNAILS", "false")
	t.Setenv("X3_VIDEO_MAX_DOWNLOAD_MB", "16")
	t.Setenv("X3_FFMPEG_PATH", "/usr/local/bin/ffmpeg")
	t.Setenv("X3_FFMPEG_SANDBOX_ENABLED", "true")
	t.Setenv("X3_FFMPEG_SANDBOX_IMAGE", "custom-ffmpeg:1")
	t.Setenv("X3_FFMPEG_SANDBOX_TIMEOUT", "45s")
	t.Setenv("X3_FFMPEG_SANDBOX_DISK", "512m")

	cfg := ConfigFromEnv()
	if cfg.MaxDownloadBytes != 16*1024*1024 {
		t.Errorf("MaxDownloadBytes = %d, want 16MB", cfg.MaxDownloadBytes)
	}
	if cfg.FfmpegPath != "/usr/local/bin/ffmpeg" || !cfg.SandboxEnabled || cfg.Image != "custom-ffmpeg:1" {
		t.Errorf("unexpected config: %#v", cfg)
	}
	if cfg.Timeout != 45*time.Second || cfg.Disk != "512m" {
		t.Errorf("unexpected limits: %#v", cfg)
	}
	if Enabled() {
		t.Error("Enabled() should honor X3_VIDEO_THUMBNAILS=false")
	}
}

func TestThumbnailDisabled(t *testing.T) {
	t.Setenv("X3_VIDEO_THUMBNAILS", "false")
	if got := Thumbnail(context.Background(), Source{CacheKey: "disabled-test", Data: jpegHeader}); got != "" {
		t.Errorf("Thumbnail while disabled = %q, want empty", got)
	}
}

func TestThumbnailUsesProbeAndCache(t *testing.T) {
	t.Setenv("X3_VIDEO_THUMBNAILS", "true")
	uri := "data:image/jpeg;base64," + "QUJD"
	calls := 0
	src := Source{
		CacheKey: "probe-test",
		Data:     []byte("fake-video"),
		Probe: func(ctx context.Context) string {
			calls++
			return uri
		},
	}
	if got := Thumbnail(context.Background(), src); got != uri {
		t.Fatalf("Thumbnail via probe = %q", got)
	}
	// second resolution must come from the cache, without calling the probe
	// or any backend
	src.Probe = nil
	if got := Thumbnail(context.Background(), src); got != uri {
		t.Fatalf("Thumbnail via cache = %q", got)
	}
	if calls != 1 {
		t.Errorf("probe called %d times, want 1", calls)
	}
}

func TestExtractFrameFallsBackToFirstFrame(t *testing.T) {
	var seeks []string
	original := runFFmpegImpl
	runFFmpegImpl = func(ctx context.Context, cfg Config, data []byte, seek string) ([]byte, error) {
		seeks = append(seeks, seek)
		if seek == "1" {
			return nil, nil // clip shorter than one second: no frame
		}
		return jpegHeader, nil
	}
	defer func() { runFFmpegImpl = original }()

	frame, err := extractFrame(context.Background(), Config{}, []byte("video"))
	if err != nil {
		t.Fatalf("extractFrame: %v", err)
	}
	if !bytes.Equal(frame, jpegHeader) {
		t.Errorf("frame = %x", frame)
	}
	if len(seeks) != 2 || seeks[0] != "1" || seeks[1] != "0" {
		t.Errorf("seek attempts = %v, want [1 0]", seeks)
	}
}

func TestExtractFrameAbortsOnBackendError(t *testing.T) {
	var seeks []string
	original := runFFmpegImpl
	runFFmpegImpl = func(ctx context.Context, cfg Config, data []byte, seek string) ([]byte, error) {
		seeks = append(seeks, seek)
		return nil, errors.New("ffmpeg failed")
	}
	defer func() { runFFmpegImpl = original }()

	if _, err := extractFrame(context.Background(), Config{}, []byte("video")); err == nil {
		t.Fatal("extractFrame should propagate backend errors")
	}
	if len(seeks) != 1 {
		t.Errorf("seek attempts = %v, want a single attempt", seeks)
	}
}

func TestLooksLikeJPEG(t *testing.T) {
	if !looksLikeJPEG(jpegHeader) {
		t.Error("valid JPEG header not detected")
	}
	if looksLikeJPEG([]byte{0xFF, 0xD8, 0x00, 0xE0}) {
		t.Error("non-JPEG accepted")
	}
	if looksLikeJPEG(nil) || looksLikeJPEG(jpegHeader[:2]) {
		t.Error("truncated data accepted")
	}
}

func TestThumbCacheEvictionAndExpiry(t *testing.T) {
	c := newThumbCache(len("uri-a")+len("uri-b"), time.Millisecond)
	c.put("a", "uri-a")
	c.put("b", "uri-b")
	if _, ok := c.get("a"); !ok {
		t.Fatal("entry a missing while within capacity")
	}
	c.put("c", "uri-c") // exceeds capacity, evicts the entry expiring first (a)
	if _, ok := c.get("a"); ok {
		t.Error("oldest entry should have been evicted")
	}
	if uri, ok := c.get("b"); !ok || uri != "uri-b" {
		t.Fatalf("get(b) = (%q, %v)", uri, ok)
	}
	time.Sleep(2 * time.Millisecond)
	if _, ok := c.get("b"); ok {
		t.Error("expired entry was returned")
	}
}

func TestResolveSandboxExecutablePassesThroughPaths(t *testing.T) {
	got, err := resolveSandboxExecutable("/opt/docker/bin/docker")
	if err != nil || got != "/opt/docker/bin/docker" {
		t.Fatalf("resolveSandboxExecutable = (%q, %v)", got, err)
	}
}

func TestResolveFfmpegPathFallsBackToHomeBin(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "x3-fake-ffmpeg.exe")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	got := resolveFfmpegPath("x3-fake-ffmpeg.exe")
	if filepath.Clean(got) != filepath.Clean(fake) {
		t.Fatalf("resolveFfmpegPath = %q, want %q", got, fake)
	}
	if resolveFfmpegPath("x3-definitely-missing-ffmpeg") != "" {
		t.Fatal("resolveFfmpegPath should return empty for a missing binary")
	}
}

func TestFfmpegArgsBoundedToOneFrame(t *testing.T) {
	args := ffmpegArgs("in.mp4", "1")
	joined := strings.Join(args, " ")
	for _, required := range []string{"-ss 1", "-i in.mp4", "-frames:v 1", "-f mjpeg"} {
		if !strings.Contains(joined, required) {
			t.Errorf("ffmpeg args missing %q: %s", required, joined)
		}
	}
	if !strings.Contains(joined, "scale=w=1280") {
		t.Errorf("ffmpeg args missing downscale filter: %s", joined)
	}
}
