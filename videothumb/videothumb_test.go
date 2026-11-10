package videothumb

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zeozeozeo/x3/ffmpegbox"
)

// jpegHeader is the start of a minimal JPEG stream (SOI + next marker byte).
var jpegHeader = []byte{0xFF, 0xD8, 0xFF, 0xE0}

func TestEnabledFromEnv(t *testing.T) {
	t.Setenv("X3_VIDEO_THUMBNAILS", "false")
	if Enabled() {
		t.Error("Enabled() should honor X3_VIDEO_THUMBNAILS=false")
	}
	t.Setenv("X3_VIDEO_THUMBNAILS", "true")
	if !Enabled() {
		t.Error("Enabled() should honor X3_VIDEO_THUMBNAILS=true")
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
	runFFmpegImpl = func(ctx context.Context, cfg ffmpegbox.Config, data []byte, seek string) ([]byte, error) {
		seeks = append(seeks, seek)
		if seek == "1" {
			return nil, nil // clip shorter than one second: no frame
		}
		return jpegHeader, nil
	}
	defer func() { runFFmpegImpl = original }()

	frame, err := extractFrame(context.Background(), ffmpegbox.Config{}, []byte("video"))
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
	runFFmpegImpl = func(ctx context.Context, cfg ffmpegbox.Config, data []byte, seek string) ([]byte, error) {
		seeks = append(seeks, seek)
		return nil, errors.New("ffmpeg failed")
	}
	defer func() { runFFmpegImpl = original }()

	if _, err := extractFrame(context.Background(), ffmpegbox.Config{}, []byte("video")); err == nil {
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
