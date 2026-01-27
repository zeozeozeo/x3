package llm

import (
	"container/list"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type cacheEntry struct {
	uri       string
	dataURI   string
	size      uint64
	expiresAt time.Time
}

type imageCache struct {
	mu          sync.Mutex
	items       map[string]*list.Element
	evictList   *list.List
	currentSize uint64
	maxSize     uint64        // e.g., 100 * 1024 * 1024 (100MB)
	ttl         time.Duration // e.g., 24 * time.Hour
}

func NewImageCache(maxSize uint64, ttl time.Duration) *imageCache {
	return &imageCache{
		items:     make(map[string]*list.Element),
		evictList: list.New(),
		maxSize:   maxSize,
		ttl:       ttl,
	}
}

// extractExtensionFromURL extracts the file extension from a URL,
// handling query parameters and fragments properly.
func extractExtensionFromURL(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL '%s': %w", rawURL, err)
	}

	urlPath := parsedURL.Path
	if urlPath == "" || urlPath == "/" {
		return "", fmt.Errorf("URL '%s' has no path component", rawURL)
	}

	ext := filepath.Ext(urlPath)
	if ext == "" {
		return "", fmt.Errorf("no file extension found in URL '%s'", rawURL)
	}

	return strings.ToLower(ext), nil
}

// getMimeTypeFromExtension converts a file extension to a MIME type
func getMimeTypeFromExtension(ext string) string {
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}

	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		switch ext {
		case ".webp":
			return "image/webp"
		case ".avif":
			return "image/avif"
		case ".svg":
			return "image/svg+xml"
		default:
			return "application/octet-stream"
		}
	}

	if strings.HasPrefix(mimeType, "image/") {
		if idx := strings.Index(mimeType, ";"); idx > 0 {
			mimeType = mimeType[:idx]
		}
	}

	return mimeType
}

// buildDataURI creates a proper data URI from binary data and URL.
func buildDataURI(data []byte, sourceURL string) (string, error) {
	ext, err := extractExtensionFromURL(sourceURL)
	if err != nil {
		slog.Warn("Failed to extract extension, using default", "url", sourceURL, "error", err)
		ext = ".bin"
	}

	mimeType := getMimeTypeFromExtension(ext)
	encoded := base64.StdEncoding.EncodeToString(data)

	return fmt.Sprintf("data:%s;base64,%s", mimeType, encoded), nil
}

// fetchImage retrieves an image from a URL, restricted to a 10MB limit.
func fetchImage(uri string) []byte {
	const maxLimit = 10 * 1024 * 1024 // 10MB

	resp, err := http.Get(uri)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.ContentLength > maxLimit {
		return nil
	}

	lr := io.LimitReader(resp.Body, maxLimit+1)
	data, err := io.ReadAll(lr)
	if err != nil || len(data) > maxLimit {
		return nil
	}

	return data
}

func (ic *imageCache) MemoizedImageBase64(uri string) string {
	ic.mu.Lock()
	defer ic.mu.Unlock()

	if ent, ok := ic.items[uri]; ok {
		entry := ent.Value.(*cacheEntry)

		if time.Now().After(entry.expiresAt) {
			ic.removeElement(ent)
		} else {
			ic.evictList.MoveToFront(ent)
			slog.Debug("MemoizedImageBase64 cache hit!")
			return entry.dataURI
		}
	}

	ic.mu.Unlock()
	slog.Info("MemoizedImageBase64 CACHE MISS: fetching image")
	data := fetchImage(uri)
	ic.mu.Lock()

	if data == nil {
		return ""
	}

	dataURI, err := buildDataURI(data, uri)
	if err != nil {
		slog.Error("Failed to build data URI", "uri", uri, "error", err)
		return ""
	}

	newSize := uint64(len(dataURI))

	for ic.currentSize+newSize > ic.maxSize && ic.evictList.Len() > 0 {
		ic.removeOldest()
	}

	entry := &cacheEntry{
		uri:       uri,
		dataURI:   dataURI,
		size:      newSize,
		expiresAt: time.Now().Add(ic.ttl),
	}
	element := ic.evictList.PushFront(entry)
	ic.items[uri] = element
	ic.currentSize += newSize

	return dataURI
}

func (ic *imageCache) removeOldest() {
	ent := ic.evictList.Back()
	if ent != nil {
		ic.removeElement(ent)
	}
}

func (ic *imageCache) removeElement(e *list.Element) {
	ic.evictList.Remove(e)
	entry := e.Value.(*cacheEntry)
	delete(ic.items, entry.uri)
	ic.currentSize -= entry.size
}
