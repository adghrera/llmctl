package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Opt-in gateway response cache (F841). A client sends the header
// "X-LLMCtrl-Cache: 1" on a non-streaming chat completion; identical
// requests within the TTL are answered from memory without touching an
// upstream. Hits are logged with zero tokens so usage and cost totals keep
// tracking upstream spend, not replays.

const (
	cacheTTL      = 10 * time.Minute
	cacheMaxEntry = 128
)

type cacheEntry struct {
	body    []byte
	created time.Time
}

// responseCache is a small TTL map with FIFO eviction and hit/miss counters.
// limits, when set, supplies live TTL/max-entry overrides from daemon
// settings; zero values fall back to the defaults (F851).
type responseCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	order   []string // insertion order for FIFO eviction
	hits    int
	misses  int
	stores  int
	limits  func() (time.Duration, int)
}

func newResponseCache() *responseCache {
	return &responseCache{entries: map[string]cacheEntry{}}
}

// curLimits returns the effective TTL and max-entry count.
func (c *responseCache) curLimits() (time.Duration, int) {
	ttl, max := time.Duration(0), 0
	if c.limits != nil {
		ttl, max = c.limits()
	}
	if ttl <= 0 {
		ttl = cacheTTL
	}
	if max <= 0 {
		max = cacheMaxEntry
	}
	return ttl, max
}

// cacheKey hashes the path plus the raw request body; identical bodies for
// the same endpoint share a slot.
func cacheKey(path string, body []byte) string {
	sum := sha256.Sum256(append([]byte(path+"\n"), body...))
	return hex.EncodeToString(sum[:])
}

func (c *responseCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl, _ := c.curLimits()
	e, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	if time.Since(e.created) > ttl {
		delete(c.entries, key)
		c.misses++
		return nil, false
	}
	c.hits++
	return e.body, true
}

// set stores a 200 JSON response body. Bodies at the recorder cap (possibly
// truncated) are skipped.
func (c *responseCache) set(key string, body []byte, truncated bool) {
	if truncated || len(body) == 0 || body[0] != '{' {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, max := c.curLimits()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= max {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
	c.entries[key] = cacheEntry{body: body, created: time.Now()}
	c.order = append(c.order, key)
	c.stores++
}

func (c *responseCache) stats() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl, max := c.curLimits()
	return map[string]any{
		"entries":    len(c.entries),
		"hits":       c.hits,
		"misses":     c.misses,
		"stores":     c.stores,
		"maxEntries": max,
		"ttlSeconds": int(ttl.Seconds()),
	}
}

func (c *responseCache) clear() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.entries)
	c.entries = map[string]cacheEntry{}
	c.order = nil
	return n
}

// CacheStats exposes the cache counters for /api/v1/cache/stats (F841).
func (s *Server) CacheStats() map[string]any { return s.respCache.stats() }

// CacheClear drops every cached response and returns how many were removed.
func (s *Server) CacheClear() int { return s.respCache.clear() }
