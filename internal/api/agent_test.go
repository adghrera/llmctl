package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"llmctl/internal/agent"
	"llmctl/internal/proxy"
	"llmctl/internal/store"
	"llmctl/internal/websearch"
)

// stubTool is a deterministic web_search for tests (no live network).
type stubTool struct{ queries []string }

func (s *stubTool) Name() string { return "web_search" }
func (s *stubTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	var a struct{ Query string }
	json.Unmarshal([]byte(argsJSON), &a)
	s.queries = append(s.queries, a.Query)
	text := fmt.Sprintf("1. Stub result\n   https://example.com\n   snippet for %q", a.Query)
	return text, []map[string]string{{"title": "Stub", "url": "https://example.com", "snippet": "sn"}}, nil
}

// scriptedBackend emits a fixed two-step script: search call, then final answer.
type scriptedBackend struct{ calls int }

func (g *scriptedBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.calls++
	var text string
	if g.calls == 1 {
		text = "checking.<search>golang testing</search>"
	} else {
		text = "Here is the answer."
	}
	w.Header().Set("Content-Type", "text/event-stream")
	f, _ := w.(http.Flusher)
	for i := 0; i < len(text); i += 9 {
		end := i + 9
		if end > len(text) {
			end = len(text)
		}
		chunk := map[string]any{"choices": []map[string]any{
			{"delta": map[string]string{"content": text[i:end]}},
		}}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if f != nil {
			f.Flush()
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func newAgentTestGW(t *testing.T) *proxy.Server {
	t.Helper()
	backend := httptest.NewServer(&scriptedBackend{})
	t.Cleanup(backend.Close)
	u := strings.TrimPrefix(backend.URL, "http://127.0.0.1:")
	port, _ := strconv.Atoi(u)
	st := &store.State{
		Backends:  map[string]*store.BackendRecord{},
		Models:    map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"test": {ID: "test", ModelID: "m", BackendID: "fake",
				Port: port, Status: store.StatusRunning, APIBasePath: "/v1"},
		},
	}
	router := proxy.NewRouter(func() (*store.State, int64, error) { return st, 1, nil }, nil)
	srv, err := proxy.NewServer(proxy.Config{
		Addr: "127.0.0.1:0", RateRPS: 1000, RateBurst: 1000, MaxInflight: 64,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, router)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestChatAgentEndpoint(t *testing.T) {
	stub := &stubTool{}
	s := &Server{gw: newAgentTestGW(t), searcher: websearch.New(), agentTool: stub}
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest("POST", "/api/v1/chat/agent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	s.chatAgent(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	var deltas strings.Builder
	var toolEvents, doneEvents, errorEvents int
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev agent.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("bad SSE payload %q: %v", line, err)
		}
		switch ev.Type {
		case "delta":
			deltas.WriteString(ev.Content)
		case "tool":
			toolEvents++
			if ev.Query != "golang testing" {
				t.Fatalf("tool query = %q", ev.Query)
			}
		case "done":
			doneEvents++
		case "error":
			errorEvents++
			t.Fatalf("unexpected error event: %v", ev.Error)
		}
	}
	if toolEvents != 1 || doneEvents != 1 {
		t.Fatalf("tool=%d done=%d body=%s", toolEvents, doneEvents, rec.Body.String())
	}
	pt := deltas.String()
	if strings.Contains(pt, "<search>") {
		t.Fatalf("marker leaked: %q", pt)
	}
	if !strings.Contains(pt, "Here is the answer.") {
		t.Fatalf("final answer missing: %q", pt)
	}
}

func TestChatAgentValidation(t *testing.T) {
	s := &Server{gw: newAgentTestGW(t), searcher: websearch.New(), agentTool: &stubTool{}}
	cases := []struct{ body, want string }{
		{`{}`, "400"},
		{`{"model":"m"}`, "400"},
		{`not json`, "400"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("POST", "/api/v1/chat/agent", strings.NewReader(c.body))
		rec := httptest.NewRecorder()
		s.chatAgent(rec, req)
		if fmt.Sprint(rec.Code) != c.want {
			t.Fatalf("body %q: status = %d want %s", c.body, rec.Code, c.want)
		}
	}
	// No gateway -> 503
	s2 := &Server{searcher: websearch.New()}
	req := httptest.NewRequest("POST", "/api/v1/chat/agent", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"x"}]}`))
	rec := httptest.NewRecorder()
	s2.chatAgent(rec, req)
	if rec.Code != 503 {
		t.Fatalf("no-gw status = %d want 503", rec.Code)
	}
}
