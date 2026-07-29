package public

import (
	"container/list"
	"context"
	"sync"
	"time"
)

const (
	catalogCacheTTL             = time.Minute
	catalogCacheCleanupInterval = 30 * time.Second
	catalogCacheEntryOverhead   = int64(96)
)

type catalogPayload struct {
	Body []byte
	ETag string
}

type catalogCacheEntry struct {
	key       string
	payload   catalogPayload
	expiresAt time.Time
	size      int64
	element   *list.Element
}

type catalogCacheCall struct {
	done    chan struct{}
	payload catalogPayload
	err     error
	version uint64
}

type catalogResultCache struct {
	mu       sync.Mutex
	maxBytes int64
	used     int64
	entries  map[string]*catalogCacheEntry
	lru      *list.List
	inflight map[string]*catalogCacheCall
	now      func() time.Time
	stop     chan struct{}
	done     chan struct{}
	version  uint64
}

func newCatalogResultCache(maxBytes int64) *catalogResultCache {
	cache := &catalogResultCache{
		maxBytes: maxBytes, entries: map[string]*catalogCacheEntry{},
		lru: list.New(), inflight: map[string]*catalogCacheCall{},
		now: time.Now, stop: make(chan struct{}), done: make(chan struct{}),
	}
	go cache.cleanupLoop()
	return cache
}

func (c *catalogResultCache) getOrLoad(ctx context.Context, key string,
	load func(context.Context) (catalogPayload, error)) (catalogPayload, error) {
	if payload, ok := c.get(key); ok {
		return payload, nil
	}
	c.mu.Lock()
	if call := c.inflight[key]; call != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return catalogPayload{}, ctx.Err()
		case <-call.done:
			return call.payload, call.err
		}
	}
	call := &catalogCacheCall{done: make(chan struct{}), version: c.version}
	c.inflight[key] = call
	c.mu.Unlock()

	payload, err := load(ctx)
	if err == nil {
		c.putVersion(key, payload, call.version)
	}
	c.mu.Lock()
	call.payload, call.err = payload, err
	delete(c.inflight, key)
	close(call.done)
	c.mu.Unlock()
	return payload, err
}

func (c *catalogResultCache) get(key string) (catalogPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.entries[key]
	if entry == nil {
		return catalogPayload{}, false
	}
	if !c.now().Before(entry.expiresAt) {
		c.removeLocked(entry)
		return catalogPayload{}, false
	}
	c.lru.MoveToFront(entry.element)
	return entry.payload, true
}

func (c *catalogResultCache) put(key string, payload catalogPayload) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, payload)
}

func (c *catalogResultCache) putVersion(key string, payload catalogPayload, version uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if version != c.version {
		return
	}
	c.putLocked(key, payload)
}

func (c *catalogResultCache) putLocked(key string, payload catalogPayload) {
	size := int64(len(key)+len(payload.Body)+len(payload.ETag)) + catalogCacheEntryOverhead
	if c.maxBytes <= 0 || size > c.maxBytes {
		return
	}
	if current := c.entries[key]; current != nil {
		c.removeLocked(current)
	}
	entry := &catalogCacheEntry{
		key: key, payload: payload, expiresAt: c.now().Add(catalogCacheTTL), size: size,
	}
	entry.element = c.lru.PushFront(entry)
	c.entries[key] = entry
	c.used += size
	c.evictLocked()
}

func (c *catalogResultCache) setMaxBytes(maxBytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxBytes = maxBytes
	c.evictLocked()
}

func (c *catalogResultCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*catalogCacheEntry{}
	c.lru.Init()
	c.used = 0
	c.version++
}

func (c *catalogResultCache) cleanup(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			c.removeLocked(entry)
		}
	}
}

func (c *catalogResultCache) evictLocked() {
	for c.used > c.maxBytes && c.lru.Len() > 0 {
		c.removeLocked(c.lru.Back().Value.(*catalogCacheEntry))
	}
}

func (c *catalogResultCache) removeLocked(entry *catalogCacheEntry) {
	delete(c.entries, entry.key)
	c.lru.Remove(entry.element)
	c.used -= entry.size
}

func (c *catalogResultCache) cleanupLoop() {
	ticker := time.NewTicker(catalogCacheCleanupInterval)
	defer ticker.Stop()
	defer close(c.done)
	for {
		select {
		case <-ticker.C:
			c.cleanup(c.now())
		case <-c.stop:
			return
		}
	}
}

func (c *catalogResultCache) close() {
	if c == nil {
		return
	}
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	<-c.done
}
