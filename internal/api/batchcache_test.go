package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// jsonBackend answers non-streaming chat completions and records whether
// the gateway cache opt-in header arrived (F843).
type jsonBackend struct {
	mu     sync.Mutex
	header string
}

func (b *jsonBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	b.header = r.Header.Get("X-LLMCtrl-Cache")
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"id":"c1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`)
}

func (b *jsonBackend) seen() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.header
}

// TestBatchCacheHeader checks that batch/structured requests with cache:true
// make the in-process gateway call carry X-LLMCtrl-Cache: 1 (F843).
func TestBatchCacheHeader(t *testing.T) {
	be := &jsonBackend{}
	gw := newCaptureGW(t, be)
	s := &Server{gw: gw}

	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/batch", strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.batch(rec, req)
		return rec
	}

	rec := call(`{"model":"m","prompts":["hi"],"cache":true}`)
	if rec.Code != 200 {
		t.Fatalf("batch returned %d: %s", rec.Code, rec.Body)
	}
	if be.seen() != "1" {
		t.Fatalf("cache header missing with cache:true (got %q)", be.seen())
	}

	rec = call(`{"model":"m","prompts":["hi"]}`)
	if rec.Code != 200 {
		t.Fatalf("batch returned %d: %s", rec.Code, rec.Body)
	}
	if be.seen() != "" {
		t.Fatalf("cache header present without cache:true (got %q)", be.seen())
	}
}
