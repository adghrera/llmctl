package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"llmctl/internal/proxy"
	"llmctl/internal/store"
	"llmctl/internal/toolstore"
	"llmctl/internal/websearch"
)

// captureBackend records the system message of the chat request it receives
// and answers with a plain reply (no tool call).
type captureBackend struct {
	system string
	body   map[string]any // full decoded request body (F829)
}

func (g *captureBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	var in struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(b, &in)
	if len(in.Messages) > 0 {
		g.system = in.Messages[0].Content
	}
	_ = json.Unmarshal(b, &g.body)
	w.Header().Set("Content-Type", "text/event-stream")
	chunk := map[string]any{"choices": []map[string]any{
		{"delta": map[string]string{"content": "answer."}},
	}}
	cb, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", cb)
}

func newCaptureGW(t *testing.T, be http.Handler) *proxy.Server {
	t.Helper()
	srv := httptest.NewServer(be)
	t.Cleanup(srv.Close)
	u := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	port, _ := strconv.Atoi(u)
	st := &store.State{
		Backends: map[string]*store.BackendRecord{},
		Models:   map[string]*store.ModelRecord{},
		Instances: map[string]*store.InstanceRecord{
			"test": {ID: "test", ModelID: "m", BackendID: "fake",
				Port: port, Status: store.StatusRunning, APIBasePath: "/v1"},
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
	return gw
}

// TestChatAgentToolFilter checks the per-chat tools filter (F820) also
// covers installed tools (F823): the system prompt must advertise only the
// enabled tools.
func TestChatAgentToolFilter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LLMCTL_HOME", home)
	src := filepath.Join(home, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "llmctl-tool.json"),
		[]byte(`{"name":"fancycalc","description":"fancy","entry":"run.sh","runtime":"command"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\necho {}\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ts, err := toolstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.InstallFromLocalDir(src, "fancy1"); err != nil {
		t.Fatal(err)
	}

	run := func(body string) string {
		be := &captureBackend{}
		s := &Server{gw: newCaptureGW(t, be), searcher: websearch.New(), agentTool: &stubTool{}}
		req := httptest.NewRequest("POST", "/api/v1/chat/agent", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.chatAgent(rec, req)
		if rec.Code != 200 {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		return be.system
	}

	// Calculator only: no other built-in, installed tool filtered out.
	sys := run(`{"model":"m","messages":[{"role":"user","content":"x"}],"tools":["calculator"]}`)
	if !strings.Contains(sys, "calculator") {
		t.Fatalf("calculator missing from prompt: %q", sys)
	}
	if strings.Contains(sys, "web_search") || strings.Contains(sys, "fancy1") {
		t.Fatalf("disabled tools leaked into prompt: %q", sys)
	}

	// Installed tool only: built-ins gone, installed tool advertised.
	sys = run(`{"model":"m","messages":[{"role":"user","content":"x"}],"tools":["tool:fancy1"]}`)
	if !strings.Contains(sys, "fancy1") {
		t.Fatalf("installed tool missing from prompt: %q", sys)
	}
	if strings.Contains(sys, "calculator") || strings.Contains(sys, "web_search") {
		t.Fatalf("disabled built-ins leaked into prompt: %q", sys)
	}

	// No tools field: everything stays.
	sys = run(`{"model":"m","messages":[{"role":"user","content":"x"}]}`)
	for _, want := range []string{"web_search", "calculator", "fancy1"} {
		if !strings.Contains(sys, want) {
			t.Fatalf("%s missing from full prompt: %q", want, sys)
		}
	}
}

// TestChatAgentSamplingPassthrough checks that maxTokens/temperature and the
// seed/top_p/penalty extras sent by the client reach the gateway request
// body (F829, F830).
func TestChatAgentSamplingPassthrough(t *testing.T) {
	be := &captureBackend{}
	s := &Server{gw: newCaptureGW(t, be), searcher: websearch.New(), agentTool: &stubTool{}}
	req := httptest.NewRequest("POST", "/api/v1/chat/agent",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"x"}],"maxTokens":123,"temperature":0.42,"seed":77,"top_p":0.9,"frequency_penalty":0.5,"presence_penalty":-0.5}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.chatAgent(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if mt, _ := be.body["max_tokens"].(float64); mt != 123 {
		t.Fatalf("max_tokens = %v, want 123", be.body["max_tokens"])
	}
	if tp, _ := be.body["temperature"].(float64); tp != 0.42 {
		t.Fatalf("temperature = %v, want 0.42", be.body["temperature"])
	}
	if sd, _ := be.body["seed"].(float64); sd != 77 {
		t.Fatalf("seed = %v, want 77", be.body["seed"])
	}
	if pp, _ := be.body["top_p"].(float64); pp != 0.9 {
		t.Fatalf("top_p = %v, want 0.9", be.body["top_p"])
	}
	if fp, _ := be.body["frequency_penalty"].(float64); fp != 0.5 {
		t.Fatalf("frequency_penalty = %v, want 0.5", be.body["frequency_penalty"])
	}
	if pp, _ := be.body["presence_penalty"].(float64); pp != -0.5 {
		t.Fatalf("presence_penalty = %v, want -0.5", be.body["presence_penalty"])
	}
}
