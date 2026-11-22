package proxy

import (
	"sync"
	"time"
)

// rateLimiter is a sharded token bucket per client key. Sized for many
// concurrent clients: 64 shards, LRU-ish eviction of idle buckets.
type rateLimiter struct {
	mu        sync.Mutex
	shards    [64]*bucketShard
	rps       float64
	burst     int
	created   map[string]*limiter
	getLimits func(string) (float64, int) // returns (rps, burst) for the key; (0,0) means use default
}

// limiter holds the state of a token bucket for a specific key. Its fields
// are guarded by its own mutex: buckets are shared across concurrent
// requests for the same key (F169).
type limiter struct {
	mu       sync.Mutex
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

// setGetLimits replaces the per-key limit lookup after construction (F130).
func (rl *rateLimiter) setGetLimits(fn func(string) (float64, int)) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.getLimits = fn
}

// setDefault changes the fallback rps/burst at runtime (F152). Existing
// buckets refill against the new rate from the next request.
func (rl *rateLimiter) setDefault(rps float64, burst int) {
	if rps <= 0 {
		rps = 20
	}
	if burst <= 0 {
		burst = 60
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rps = rps
	rl.burst = burst
}

// rateState describes the outcome of one allow() call so the gateway can
// emit X-RateLimit-* headers (F169).
type rateState struct {
	Limit      int           // burst size (bucket capacity)
	Remaining  int           // whole tokens left after this request
	RetryAfter time.Duration // time until one token is available (only when denied)
}

// allow reports whether the request with the given key is allowed.
// key can be empty, meaning the request is not authenticated and should use the default limits.
func (rl *rateLimiter) allow(key string) (bool, rateState) {
	rl.mu.Lock()
	l, ok := rl.created[key]
	defRps, defBurst := rl.rps, rl.burst
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
	getLimits := rl.getLimits
	rl.mu.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.lastSeen = now
	elapsed := now.Sub(l.last)
	l.last = now
	// Determine the effective rps and burst for this key.
	var rps float64
	var burst int
	if key == "" || getLimits == nil {
		rps, burst = defRps, defBurst
	} else {
		rpsTmp, burstTmp := getLimits(key)
		if rpsTmp == 0 && burstTmp == 0 {
			rps, burst = defRps, defBurst
		} else {
			rps, burst = rpsTmp, burstTmp
		}
	}
	// Token bucket algorithm.
	l.tokens += elapsed.Seconds() * rps
	if l.tokens > float64(burst) {
		l.tokens = float64(burst)
	}
	if l.tokens >= 1 {
		l.tokens--
		return true, rateState{Limit: burst, Remaining: int(l.tokens)}
	}
	wait := time.Duration((1 - l.tokens) / rps * float64(time.Second))
	return false, rateState{Limit: burst, Remaining: 0, RetryAfter: wait}
}
