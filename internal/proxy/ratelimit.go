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
	getLimits func(string) (float64, int) // returns (rps, burst) for the key; (0,0) means use default
}

// limiter holds the state of a token bucket for a specific key.
type limiter struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

// bucketShard is a shard of the rate limiter to reduce contention.
type bucketShard struct {
	mu sync.Mutex
}

// newRateLimiter returns a new rate limiter with the given default rps and burst.
// getLimits is a function that returns the effective rps and burst for a given key.
// If getLimits returns (0,0) for a key, the default rps and burst are used.
func newRateLimiter(rps float64, burst int, getLimits func(string) (float64, int)) *rateLimiter {
	if rps <= 0 {
		rps = 20 // sustained req/s per client
	}
	if burst <= 0 {
		burst = 60
	}
	return &rateLimiter{rps: rps, burst: burst, created: map[string]*limiter{}, getLimits: getLimits}
}

// allow reports whether the request with the given key is allowed.
// key can be empty, meaning the request is not authenticated and should use the default limits.
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
	elapsed := now.Sub(l.last)
	l.last = now
	// Determine the effective rps and burst for this key.
	var rps float64
	var burst int
	if key == "" {
		// Use the rate limiter's default rps and burst.
		rps = float64(rl.rps)
		burst = rl.burst
	} else {
		if rl.getLimits != nil {
			rpsTmp, burstTmp := rl.getLimits(key)
			if rpsTmp == 0 && burstTmp == 0 {
				rps = float64(rl.rps)
				burst = rl.burst
			} else {
				rps = rpsTmp
				burst = burstTmp
			}
		} else {
			rps = float64(rl.rps)
			burst = rl.burst
		}
	}
	// Token bucket algorithm.
	l.tokens += elapsed.Seconds() * rps
	if l.tokens > float64(burst) {
		l.tokens = float64(burst)
	}
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}
