package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// usageSrv is an OpenAI-compat fake that always includes usage fields
// (prompt=10, completion=5).
func usageSrv(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"object":"list","data":[{"id":"test-model","object":"model"}]}`)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`)
		default:
			http.Error(w, "not found", 404)
		}
	}))
}

func TestMetricsCountsAndTokens(t *testing.T) {
	fake := usageSrv(t)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.testHandler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("req %d: status %d: %s", i, rec.Code, rec.Body)
		}
	}
	// One failing request (unknown model).
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"nope-xyz","messages":[]}`))
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code < 400 {
		t.Fatalf("expected error status, got %d", rec.Code)
	}

	snap := srv.Metrics()
	if total, _ := snap["requests_total"].(int64); total != 4 {
		t.Fatalf("requests_total = %v, want 4", snap["requests_total"])
	}
	tokens, _ := snap["tokens"].(map[string]any)
	if tokens == nil {
		t.Fatalf("missing tokens in snapshot: %v", snap)
	}
	if pt, _ := tokens["prompt"].(int64); pt != 30 {
		t.Fatalf("prompt tokens = %v, want 30", tokens["prompt"])
	}
	if ct, _ := tokens["completion"].(int64); ct != 15 {
		t.Fatalf("completion tokens = %v, want 15", tokens["completion"])
	}
	lat, _ := snap["latency"].(map[string]any)
	if lat == nil || lat["p50_ms"] == nil {
		t.Fatalf("missing latency percentiles: %v", snap["latency"])
	}
	models, _ := snap["models"].(map[string]any)
	tm, _ := models["test-model"].(map[string]any)
	if tm == nil {
		t.Fatalf("missing per-model metrics for test-model: %v", models)
	}
	if r, _ := tm["requests"].(int64); r != 3 {
		t.Fatalf("test-model requests = %v, want 3", tm["requests"])
	}
	status, _ := snap["status_classes"].(map[string]any)
	if c, _ := status["2xx"].(int64); c != 3 {
		t.Fatalf("2xx count = %v, want 3", status["2xx"])
	}
	if c, _ := status["4xx"].(int64); c != 1 {
		t.Fatalf("4xx count = %v, want 1", status["4xx"])
	}
}

func TestMetricsStreamingUsageSSE(t *testing.T) {
	// Fake that streams SSE with a final usage chunk.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	body := `{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream not passed through: %s", rec.Body)
	}
	snap := srv.Metrics()
	tokens, _ := snap["tokens"].(map[string]any)
	if pt, _ := tokens["prompt"].(int64); pt != 7 {
		t.Fatalf("streamed prompt tokens = %v, want 7", tokens["prompt"])
	}
	if ct, _ := tokens["completion"].(int64); ct != 3 {
		t.Fatalf("streamed completion tokens = %v, want 3", tokens["completion"])
	}
}

func TestScanUsage(t *testing.T) {
	sse := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3}}\n\n" +
		"data: [DONE]\n\n")
	if p, c := scanUsageSSE(sse); p != 7 || c != 3 {
		t.Fatalf("scanUsageSSE = (%d,%d), want (7,3)", p, c)
	}
	b := []byte(`{"usage":{"prompt_tokens":1,"completion_tokens":2}}`)
	if p, c := scanUsageJSON(b); p != 1 || c != 2 {
		t.Fatalf("scanUsageJSON = (%d,%d), want (1,2)", p, c)
	}
	if p, c := scanUsageJSON([]byte(`{"no":"usage"}`)); p != 0 || c != 0 {
		t.Fatalf("scanUsageJSON no-usage = (%d,%d), want (0,0)", p, c)
	}
}
