package llm

import (
	"container/list"
	"encoding/base64"
	"io"
	"net/http"
	"sync"
	"time"
)

type cacheEntry struct {
	uri       string
	base64    string
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
			return entry.base64
		}
	}

	ic.mu.Unlock()
	data := fetchImage(uri)
	ic.mu.Lock()

	if data == nil {
		return ""
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	newSize := uint64(len(encoded))

	for ic.currentSize+newSize > ic.maxSize && ic.evictList.Len() > 0 {
		ic.removeOldest()
	}

	entry := &cacheEntry{
		uri:       uri,
		base64:    encoded,
		size:      newSize,
		expiresAt: time.Now().Add(ic.ttl),
	}
	element := ic.evictList.PushFront(entry)
	ic.items[uri] = element
	ic.currentSize += newSize

	return encoded
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
