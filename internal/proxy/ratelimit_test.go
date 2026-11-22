package proxy

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiterState(t *testing.T) {
	rl := newRateLimiter(1, 2, nil)
	ok, st := rl.allow("k")
	if !ok || st.Limit != 2 || st.Remaining != 1 {
		t.Fatalf("first allow = %v %+v, want true limit 2 remaining 1", ok, st)
	}
	ok, st = rl.allow("k")
	if !ok || st.Remaining != 0 {
		t.Fatalf("second allow = %v %+v, want true remaining 0", ok, st)
	}
	ok, st = rl.allow("k")
	if ok {
		t.Fatal("third allow should be denied")
	}
	if st.Remaining != 0 || st.RetryAfter <= 0 || st.RetryAfter > time.Second {
		t.Fatalf("denied state = %+v, want remaining 0 and retry in (0, 1s]", st)
	}
	// Refill: after ~1s one token is back.
	time.Sleep(1100 * time.Millisecond)
	if ok, _ := rl.allow("k"); !ok {
		t.Fatal("allow after refill should succeed")
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	// Hammer one bucket from many goroutines; -race flags unsynchronized
	// bucket access (F169).
	rl := newRateLimiter(1000, 100, nil)
	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if ok, _ := rl.allow("hot"); ok {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	if allowed == 0 {
		t.Fatal("no requests allowed")
	}
}
