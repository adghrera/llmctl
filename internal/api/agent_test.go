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

// multiToolBackend emits a calculator call then a fetch_url call then final.
type multiToolBackend struct{ calls int }

func (g *multiToolBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.calls++
	var text string
	switch g.calls {
	case 1:
		text = `<tool_call>{"name": "calculator", "arguments": {"expression": "2*(3+4)^2"}}</tool_call>`
	case 2:
		text = `<tool_call>{"name": "current_time", "arguments": {}}</tool_call>`
	default:
		text = "Done: 98 and the time."
	}
	w.Header().Set("Content-Type", "text/event-stream")
	f, _ := w.(http.Flusher)
	b, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]string{"content": text}}}})
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f != nil {
		f.Flush()
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func TestChatAgentMultiTools(t *testing.T) {
	backend := httptest.NewServer(&multiToolBackend{})
	t.Cleanup(backend.Close)
	port, _ := strconv.Atoi(strings.TrimPrefix(backend.URL, "http://127.0.0.1:"))
	st := &store.State{
		Backends:  map[string]*store.BackendRecord{},
		Models:    map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"test": {ID: "test", ModelID: "m", BackendID: "fake", Port: port, Status: store.StatusRunning, APIBasePath: "/v1"},
		},
	}
	router := proxy.NewRouter(func() (*store.State, int64, error) { return st, 1, nil }, nil)
	gw, err := proxy.NewServer(proxy.Config{
		Addr: "127.0.0.1:0", RateRPS: 1000, RateBurst: 1000, MaxInflight: 64,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, router)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{gw: gw, searcher: websearch.New()} // real tools map incl. calculator + current_time
	req := httptest.NewRequest("POST", "/api/v1/chat/agent", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"math"}]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.chatAgent(rec, req)

	out := rec.Body.String()
	var toolNames []string
	var final strings.Builder
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "data: ")
		var ev struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
			Results []struct {
				Title string `json:"title"`
			} `json:"results"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "tool" {
			toolNames = append(toolNames, ev.Name)
		}
		if ev.Type == "delta" {
			final.WriteString(ev.Content)
		}
	}
	if len(toolNames) != 2 || toolNames[0] != "calculator" || toolNames[1] != "current_time" {
		t.Fatalf("tool events = %v (want calculator, current_time); body: %s", toolNames, out)
	}
	if !strings.Contains(out, `"result":"98"`) {
		t.Fatalf("calculator result missing from stream: %s", out)
	}
	if !strings.Contains(final.String(), "Done: 98") {
		t.Fatalf("final answer missing: %q", final.String())
	}
	if strings.Contains(final.String(), "tool_call") {
		t.Fatalf("raw tool markup leaked into deltas")
	}
}
