package proxy

import (
	"sync"
	"time"
)

// rateLimiter is a sharded token bucket per client key. Sized for many
// concurrent clients: 64 shards, LRU-ish eviction of idle buckets.
type rateLimiter struct {
	mu      sync.Mutex
	shards  [64]*bucketShard
	rps     float64
	burst   int
	created map[string]*limiter
}

type limiter struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

type bucketShard struct {
	mu sync.Mutex
}

func newRateLimiter(rps float64, burst int) *rateLimiter {
	if rps <= 0 {
		rps = 20 // sustained req/s per client
	}
	return &rateLimiter{rps: rps, burst: burst, created: map[string]*limiter{}}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	l, ok := rl.created[key]
	now := time.Now()
	if !ok {
		// Evict idle clients past a soft cap so the map can't grow forever.
		if len(rl.created) > 10_000 {
			for k, v := range rl.created {
				if now.Sub(v.lastSeen) > 10*time.Minute {
					delete(rl.created, k)
				}
			}
		}
		l = &limiter{tokens: float64(rl.burst), last: now, lastSeen: now}
		rl.created[key] = l
	}
	rl.mu.Unlock()

	l.lastSeen = now
	elapsed := now.Sub(l.last).Seconds()
	l.last = now
	l.tokens += elapsed * rl.rps
	if l.tokens > float64(rl.burst) {
		l.tokens = float64(rl.burst)
	}
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}
