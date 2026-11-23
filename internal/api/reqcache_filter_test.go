package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestsCacheFilter checks the ?cache=1|0 filter on GET /api/v1/requests
// splits logged requests by response-cache hits (F847).
func TestRequestsCacheFilter(t *testing.T) {
	be := &jsonBackend{}
	gw := newCaptureGW(t, be)
	s := &Server{gw: gw}

	body := `{"model":"m","messages":[{"role":"user","content":"x"}]}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LLMCtrl-Cache", "1")
		rec := httptest.NewRecorder()
		gw.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("gateway returned %d: %s", rec.Code, rec.Body.String())
		}
	}

	count := func(query string) int {
		req := httptest.NewRequest("GET", "/api/v1/requests?"+query, nil)
		rec := httptest.NewRecorder()
		s.recentRequests(rec, req)
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", query, err)
		}
		return len(out)
	}

	if got := count(""); got != 2 {
		t.Fatalf("no filter: want 2 entries, got %d", got)
	}
	if got := count("cache=1"); got != 1 {
		t.Fatalf("cache=1: want 1 hit, got %d", got)
	}
	if got := count("cache=0"); got != 1 {
		t.Fatalf("cache=0: want 1 miss, got %d", got)
	}
}
