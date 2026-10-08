package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"llmctl/internal/store"
)

// fakeBackend is a minimal OpenAI-compatible server used for tests.
func fakeBackend(t *testing.T, streaming bool) *httptest.Server {
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"object":"list","data":[{"id":"test-model","object":"model"}]}`)
		case "/v1/chat/completions":
			var req map[string]any
			json.NewDecoder(r.Body).Decode(&req)
			mu.Lock()
			defer mu.Unlock()
			if req["model"] == "test-model" && streaming {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				flusher := w.(http.Flusher)
				for _, tok := range []string{"Hello", " world"} {
					chunk := map[string]any{
						"object": "chat.completion.chunk",
						"choices": []map[string]any{{
							"delta": map[string]string{"content": tok},
						}},
					}
					b, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", b)
					flusher.Flush()
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"cmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"}}]}`)
		default:
			http.Error(w, "not found", 404)
		}
	}))
	return srv
}

func portOf(url string) int {
	p := strings.TrimPrefix(url, "http://127.0.0.1:")
	var n int
	fmt.Sscanf(p, "%d", &n)
	return n
}

func testRouter(t *testing.T, port int) *Router {
	st := &store.State{
		Backends:  map[string]*store.BackendRecord{},
		Models:    map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"test": {
				ID: "test", ModelID: "test-model", BackendID: "fake",
				Port: port, Status: store.StatusRunning, APIBasePath: "/v1",
			},
		},
	}
	return NewRouter(func() (*store.State, int64, error) { return st, 1, nil })
}

func newTestServer(t *testing.T, port int, keys []string) *Server {
	srv, err := NewServer(Config{
		Addr: "127.0.0.1:0", APIKeys: keys, RateRPS: 1000, RateBurst: 1000,
		MaxInflight: 64, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, testRouter(t, port))
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func (s *Server) testHandler() http.Handler { return s.httpSrv.Handler }

func TestChatNonStreaming(t *testing.T) {
	fake := fakeBackend(t, false)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	body := `{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "chat.completion") {
		t.Fatalf("unexpected body: %s", rec.Body)
	}
}

func TestChatStreamingSSE(t *testing.T) {
	fake := fakeBackend(t, true)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	body := `{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)

	out := rec.Body.String()
	if !strings.Contains(out, "data: [DONE]") {
		t.Fatalf("missing DONE sentinel: %s", out)
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "world") {
		t.Fatalf("missing streamed tokens: %s", out)
	}
}

func TestAuth(t *testing.T) {
	fake := fakeBackend(t, false)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), []string{"sk-secret"})

	body := `{"model":"test-model","messages":[]}`
	// no key
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	// wrong key
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-wrong")
	rec = httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("want 401, got %d", rec.Code)
	}
	// right key
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-secret")
	rec = httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body)
	}
}

func TestModelNotFound(t *testing.T) {
	fake := fakeBackend(t, false)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	body := `{"model":"nope","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("want 404, got %d: %s", rec.Code, rec.Body)
	}
}

func TestRateLimit(t *testing.T) {
	fake := fakeBackend(t, false)
	defer fake.Close()
	srv, err := NewServer(Config{
		Addr: "127.0.0.1:0", RateRPS: 5, RateBurst: 5, MaxInflight: 64,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, testRouter(t, portOf(fake.URL)))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"model":"test-model","messages":[]}`
	got429 := false
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		req.RemoteAddr = "9.9.9.9:1234" // same client
		rec := httptest.NewRecorder()
		srv.testHandler().ServeHTTP(rec, req)
		if rec.Code == 429 {
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("rate limiter never returned 429")
	}
}

func TestListModels(t *testing.T) {
	fake := fakeBackend(t, false)
	defer fake.Close()
	srv := newTestServer(t, portOf(fake.URL), nil)

	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	srv.testHandler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "test-model") {
		t.Fatalf("model list missing test-model: %s", rec.Body)
	}
}

// TestConcurrentClients hammers the gateway to prove the SSE passthrough
// and shared state hold up under load.
func TestConcurrentClients(t *testing.T) {
	fake := fakeBackend(t, true)
	defer fake.Close()
	srv, err := NewServer(Config{
		Addr: "127.0.0.1:0", RateRPS: 100000, RateBurst: 100000, MaxInflight: 1024,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, testRouter(t, portOf(fake.URL)))
	if err != nil {
		t.Fatal(err)
	}
	h := srv.testHandler()

	const n = 100
	var ok, failed atomic.Int64
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := `{"model":"test-model","stream":true,"messages":[{"role":"user","content":"hi"}]}`
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code == 200 && strings.Contains(rec.Body.String(), "[DONE]") {
				ok.Add(1)
			} else {
				failed.Add(1)
				t.Logf("fail: status=%d body=%.200s", rec.Code, rec.Body.String())
			}
		}()
	}
	wg.Wait()
	t.Logf("%d ok, %d failed in %v", ok.Load(), failed.Load(), time.Since(start).Round(time.Millisecond))
	if failed.Load() != 0 {
		t.Fatalf("%d concurrent requests failed", failed.Load())
	}
}
