package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestResponseCache exercises the opt-in gateway response cache (F841):
// identical non-streaming chat requests with X-LLMCtrl-Cache: 1 hit the
// upstream once; streaming and unheadered requests never join the cache.
func TestResponseCache(t *testing.T) {
	var hits int64
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"object":"list","data":[{"id":"test-model","object":"model"}]}`)
		case "/v1/chat/completions":
			n := atomic.AddInt64(&hits, 1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"cmpl-%d","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`, n)
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	call := func(body string, cacheHdr string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		if cacheHdr != "" {
			req.Header.Set("X-LLMCtrl-Cache", cacheHdr)
		}
		rec := httptest.NewRecorder()
		srv.testHandler().ServeHTTP(rec, req)
		return rec
	}

	body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`

	// No header: never cached.
	call(body, "")
	call(body, "")
	if n := atomic.LoadInt64(&hits); n != 2 {
		t.Fatalf("unheadered requests should always reach upstream, got %d hits", n)
	}

	// Opt-in: first miss stores, second is served from memory.
	r1 := call(body, "1")
	if r1.Header().Get("X-LLMCtrl-Cache") != "miss" {
		t.Fatalf("first opt-in request should be a miss: %q", r1.Header().Get("X-LLMCtrl-Cache"))
	}
	r2 := call(body, "1")
	if r2.Header().Get("X-LLMCtrl-Cache") != "hit" {
		t.Fatalf("second opt-in request should be a hit: %q", r2.Header().Get("X-LLMCtrl-Cache"))
	}
	if r2.Body.String() != r1.Body.String() {
		t.Fatalf("cached reply differs:\n%s\nvs\n%s", r1.Body, r2.Body)
	}
	if n := atomic.LoadInt64(&hits); n != 3 {
		t.Fatalf("cache hit must not reach upstream, got %d upstream calls", n)
	}

	// Different body: separate key, another miss.
	call(`{"model":"test-model","messages":[{"role":"user","content":"other"}]}`, "1")
	if n := atomic.LoadInt64(&hits); n != 4 {
		t.Fatalf("different body must miss the cache, got %d upstream calls", n)
	}

	// Stats reflect the traffic.
	st := srv.CacheStats()
	if st["hits"] != 1 || st["misses"].(int) < 2 || st["stores"].(int) < 2 {
		t.Fatalf("unexpected stats: %+v", st)
	}

	// Clear drops entries; the next request misses again.
	if n := srv.CacheClear(); n < 2 {
		t.Fatalf("clear removed %d entries", n)
	}
	if r := call(body, "1"); r.Header().Get("X-LLMCtrl-Cache") != "miss" {
		t.Fatal("after clear the request should miss")
	}

	// Cache-hit log entries carry zero tokens and the hit marker (F841).
	entries := srv.collector.reqLog.recent(10)
	var sawHit bool
	for _, e := range entries {
		if e.Cache == "hit" {
			sawHit = true
			if e.PromptTok != 0 || e.CompTok != 0 {
				t.Fatalf("cache-hit entry must log zero tokens: %+v", e)
			}
		}
	}
	if !sawHit {
		t.Fatalf("no cache-hit entry in the request log: %+v", entries)
	}

	// Streaming requests never join the cache even with the header.
	sreq := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"test-model","stream":true,"messages":[]}`))
	sreq.Header.Set("X-LLMCtrl-Cache", "1")
	srec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(srec, sreq)
	if srec.Header().Get("X-LLMCtrl-Cache") != "" {
		t.Fatal("streaming requests must not carry the cache header")
	}
	_ = json.Marshal // keep the import honest if the body shape changes
}
