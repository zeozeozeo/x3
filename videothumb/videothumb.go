// Package videothumb extracts a thumbnail frame from videos so the LLM can
// see them like images. Discord's media proxy thumbnail and Matrix
// client-provided thumbnails are preferred (via Source.Probe / caller-side
// handling); an ffmpeg backend (sandboxed with gVisor or a host binary) is
// the fallback that decodes a frame locally.
package videothumb

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// MaxThumbnailBytes caps the JPEG frame produced for LLM context, matching
// llm.MaxImageBytes.
const MaxThumbnailBytes = 10 * 1024 * 1024

type Source struct {
	// CacheKey overrides the cache key for this video. Set it to a stable
	// identifier (e.g. a Discord attachment ID), since signed CDN URLs are
	// regenerated per API response.
	CacheKey string
	// URL of the video; downloaded (capped) when Data is empty.
	URL string
	// Data contains the raw video bytes and takes precedence over URL.
	Data []byte
	// Filename and ContentType describe the source for logging only.
	Filename    string
	ContentType string
	// Probe is an optional cheap fast path (e.g. Discord's media proxy
	// thumbnail) returning a data URI, tried before any ffmpeg run.
	Probe func(ctx context.Context) string
}

// Enabled reports whether video thumbnail support is turned on.
func Enabled() bool { return envBool("X3_VIDEO_THUMBNAILS", true) }

// BackendAvailable reports whether an ffmpeg backend (sandbox or host binary)
// could run, i.e. whether downloading a video for frame extraction can pay
// off. Cheap sources (media proxy, Matrix info thumbnails) work without it.
func BackendAvailable() bool {
	cfg := ConfigFromEnv()
	if cfg.SandboxEnabled {
		return true
	}
	return resolveFfmpegPath(cfg.FfmpegPath) != ""
}

// MaxDownloadBytes returns the cap on video bytes downloaded for frame
// extraction.
func MaxDownloadBytes() int { return ConfigFromEnv().MaxDownloadBytes }

// Thumbnail resolves a JPEG data URI for the video: cache hit, then Probe,
// then an ffmpeg backend. Returns "" when no thumbnail is available.
func Thumbnail(ctx context.Context, src Source) string {
	if !Enabled() {
		return ""
	}
	key := src.CacheKey
	if key == "" {
		if src.URL != "" {
			key = "url:" + src.URL
		} else {
			sum := sha256.Sum256(src.Data)
			key = "data:" + hex.EncodeToString(sum[:16])
		}
	}
	if uri, ok := thumbCacheInstance.get(key); ok {
		return uri
	}
	if src.Probe != nil {
		if uri := src.Probe(ctx); uri != "" {
			thumbCacheInstance.put(key, uri)
			return uri
		}
	}
	uri := extract(ctx, src)
	if uri != "" {
		thumbCacheInstance.put(key, uri)
	}
	return uri
}

var videoFetchClient = &http.Client{
	Timeout: 60 * time.Second,
}

func extract(ctx context.Context, src Source) string {
	if !BackendAvailable() {
		return ""
	}
	cfg := ConfigFromEnv()

	data := src.Data
	if len(data) == 0 {
		if src.URL == "" {
			return ""
		}
		downloaded, err := downloadVideo(ctx, src.URL, cfg.MaxDownloadBytes)
		if err != nil {
			slog.Warn("failed to download video for thumbnail", "err", err, "url", src.URL)
			return ""
		}
		data = downloaded
	}
	if len(data) == 0 || len(data) > cfg.MaxDownloadBytes {
		slog.Warn("video exceeds size limit for thumbnail extraction",
			"filename", src.Filename, "size", len(data), "limit", cfg.MaxDownloadBytes)
		return ""
	}

	frame, err := extractFrame(ctx, cfg, data)
	if err != nil {
		slog.Warn("video thumbnail extraction failed", "err", err, "filename", src.Filename)
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(frame)
}

// extractFrame seeks one second in so fade-from-black intros are skipped and
// falls back to the first frame for clips shorter than that.
func extractFrame(ctx context.Context, cfg Config, data []byte) ([]byte, error) {
	for _, seek := range []string{"1", "0"} {
		frame, err := runFFmpegImpl(ctx, cfg, data, seek)
		if err != nil {
			return nil, err
		}
		if looksLikeJPEG(frame) {
			return frame, nil
		}
	}
	return nil, errors.New("ffmpeg produced no decodable frame")
}

// runFFmpegImpl is indirected so tests can fake the backend.
var runFFmpegImpl = runFFmpeg

func looksLikeJPEG(data []byte) bool {
	return len(data) > 3 && len(data) <= MaxThumbnailBytes &&
		bytes.HasPrefix(data, []byte{0xFF, 0xD8}) && data[2] == 0xFF
}

func downloadVideo(ctx context.Context, url string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "x3/1.0")
	resp, err := videoFetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	if resp.ContentLength > int64(limit) {
		return nil, fmt.Errorf("video exceeds limit (%d > %d bytes)", resp.ContentLength, limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("video exceeds limit after download (%d > %d bytes)", len(data), limit)
	}
	return data, nil
}

// thumbCache memoizes data URIs by source key so repeated context rebuilds do
// not re-probe or re-extract the same videos.
type thumbCache struct {
	mu      sync.Mutex
	items   map[string]*cacheEntry
	size    int
	maxSize int
	ttl     time.Duration
}

type cacheEntry struct {
	uri    string
	size   int
	expiry time.Time
}

func newThumbCache(maxSize int, ttl time.Duration) *thumbCache {
	return &thumbCache{
		items:   make(map[string]*cacheEntry),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (c *thumbCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expiry) {
		c.size -= entry.size
		delete(c.items, key)
		return "", false
	}
	return entry.uri, true
}

func (c *thumbCache) put(key, uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.items[key]; ok {
		c.size -= old.size
		delete(c.items, key)
	}
	size := len(uri)
	if size > c.maxSize {
		return
	}
	for c.size+size > c.maxSize && len(c.items) > 0 {
		var oldestKey string
		var oldest time.Time
		first := true
		for k, e := range c.items {
			if first || e.expiry.Before(oldest) {
				oldestKey, oldest, first = k, e.expiry, false
			}
		}
		c.size -= c.items[oldestKey].size
		delete(c.items, oldestKey)
	}
	c.items[key] = &cacheEntry{uri: uri, size: size, expiry: time.Now().Add(c.ttl)}
	c.size += size
}

var thumbCacheInstance = newThumbCache(64*1024*1024, 24*time.Hour)
