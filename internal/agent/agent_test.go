package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeGateway returns scripted SSE responses, one per call, and records
// the request bodies it received.
type fakeGateway struct {
	mu       sync.Mutex
	scripts  []string // full assistant text per call; "" = error 500
	calls    int
	bodies   []string
	authSeen []string
}

func (f *fakeGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	idx := f.calls
	f.calls++
	if idx >= len(f.scripts) {
		f.mu.Unlock()
		http.Error(w, "script exhausted", 500)
		return
	}
	text := f.scripts[idx]
	bstr, _ := decodeBody(r)
	f.bodies = append(f.bodies, bstr)
	f.authSeen = append(f.authSeen, r.Header.Get("Authorization"))
	f.mu.Unlock()

	if text == "" {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":{"message":"boom"}}`)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	// Stream in 7-byte pieces to exercise partial-line parsing.
	flusher, _ := w.(http.Flusher)
	full := text
	for i := 0; i < len(full); i += 7 {
		end := i + 7
		if end > len(full) {
			end = len(full)
		}
		chunk := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]string{"content": full[i:end]}},
			},
		}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func decodeBody(r *http.Request) (string, error) {
	buf := make([]byte, 1<<20)
	n, _ := r.Body.Read(buf)
	return string(buf[:n]), nil
}

// fakeTool is a deterministic search tool.
type fakeTool struct {
	mu     sync.Mutex
	queries []string
}

func (t *fakeTool) Name() string { return "web_search" }
func (t *fakeTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	var a struct{ Query string }
	json.Unmarshal([]byte(argsJSON), &a)
	t.mu.Lock()
	t.queries = append(t.queries, a.Query)
	t.mu.Unlock()
	text := fmt.Sprintf("1. Result for %q\n   https://example.com\n   example snippet", a.Query)
	return text, []map[string]string{{"title": "Result", "url": "https://example.com"}}, nil
}

func runAgent(t *testing.T, scripts []string, req Request) ([]Event, *fakeGateway, *fakeTool) {
	t.Helper()
	fw := &fakeGateway{scripts: scripts}
	tool := &fakeTool{}
	tools := map[string]Tool{"web_search": tool}
	var events []Event
	var mu sync.Mutex
	Run(context.Background(), fw, tools, req, func(e Event) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	})
	return events, fw, tool
}

func plainText(events []Event) string {
	var sb strings.Builder
	for _, e := range events {
		if e.Type == "delta" {
			sb.WriteString(e.Content)
		}
	}
	return sb.String()
}

func TestAgentPlainText(t *testing.T) {
	events, fw, tool := runAgent(t,
		[]string{"Hello! 2+2 is 4."},
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}},
	)
	if fw.calls != 1 {
		t.Fatalf("gateway calls = %d, want 1", fw.calls)
	}
	if got := plainText(events); got != "Hello! 2+2 is 4." {
		t.Fatalf("streamed text = %q", got)
	}
	last := events[len(events)-1]
	if last.Type != "done" || last.Steps != 1 {
		t.Fatalf("last event = %+v", last)
	}
	if len(tool.queries) != 0 {
		t.Fatalf("tool should not run, ran: %v", tool.queries)
	}
}

func TestAgentSearchLoop(t *testing.T) {
	script1 := "Let me look that up.<search>mu earnings</search>"
	script2 := "MU reported record revenue (https://example.com)."
	events, fw, tool := runAgent(t,
		[]string{script1, script2},
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "MU earnings?"}}, APIKey: "sk-test"},
	)
	if fw.calls != 2 {
		t.Fatalf("gateway calls = %d, want 2", fw.calls)
	}
	// Tool ran with the parsed query.
	if len(tool.queries) != 1 || tool.queries[0] != "mu earnings" {
		t.Fatalf("tool queries = %v", tool.queries)
	}
	// The <search> block must never leak into deltas.
	pt := plainText(events)
	if strings.Contains(pt, "<search>") {
		t.Fatalf("search marker leaked: %q", pt)
	}
	if !strings.Contains(pt, "Let me look that up.") {
		t.Fatalf("pre-marker text missing: %q", pt)
	}
	if !strings.Contains(pt, "record revenue") {
		t.Fatalf("final answer missing: %q", pt)
	}
	// A tool event was emitted with query + structured results.
	var toolEv *Event
	for i := range events {
		if events[i].Type == "tool" {
			toolEv = &events[i]
		}
	}
	if toolEv == nil || toolEv.Query != "mu earnings" || toolEv.Results == nil {
		t.Fatalf("missing tool event: %+v", events)
	}
	// Second request must contain the search results fed back as a user turn.
	var second map[string]any
	if err := json.Unmarshal([]byte(fw.bodies[1]), &second); err != nil {
		t.Fatal(err)
	}
	msgs, _ := second["messages"].([]any)
	foundResult := false
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "user" && strings.Contains(mm["content"].(string), "Web search results") {
			foundResult = true
		}
	}
	if !foundResult {
		t.Fatalf("search results not fed back; messages: %v", msgs)
	}
	// Auth forwarded.
	if len(fw.authSeen) < 1 || fw.authSeen[0] != "Bearer sk-test" {
		t.Fatalf("auth = %v", fw.authSeen)
	}
}

func TestAgentMultipleSearches(t *testing.T) {
	script1 := "<search>q one</search>\n<search>q two</search>"
	events, fw, tool := runAgent(t,
		[]string{script1, "done"},
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "compare x and y"}}},
	)
	if len(tool.queries) != 2 || tool.queries[0] != "q one" || tool.queries[1] != "q two" {
		t.Fatalf("queries = %v", tool.queries)
	}
	if fw.calls != 2 {
		t.Fatalf("calls = %d", fw.calls)
	}
	_ = events
}

func TestAgentMaxSteps(t *testing.T) {
	// Model keeps searching forever.
	scripts := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		scripts = append(scripts, "<search>again</search>")
	}
	events, fw, _ := runAgent(t, scripts,
		Request{Model: "m", MaxSteps: 2, Messages: []Message{{Role: "user", Content: "go"}}},
	)
	if fw.calls != 2 {
		t.Fatalf("calls = %d, want 2 (MaxSteps)", fw.calls)
	}
	pt := plainText(events)
	if !strings.Contains(pt, "maximum number of search steps") {
		t.Fatalf("no cap message: %q", pt)
	}
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Fatalf("last = %+v", last)
	}
}

func TestAgentUpstreamError(t *testing.T) {
	events, _, _ := runAgent(t,
		[]string{""}, // scripted 500
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}},
	)
	last := events[len(events)-1]
	if last.Type != "error" || !strings.Contains(last.Error, "boom") {
		t.Fatalf("last = %+v, want error containing boom", last)
	}
}

func TestExtractSearchQueries(t *testing.T) {
	qs := extractSearchQueries("a <SEARCH> X </search> b <search>y</search>")
	if len(qs) != 2 || qs[0] != "X" || qs[1] != "y" {
		t.Fatalf("got %v", qs)
	}
	if len(extractSearchQueries("no markers here")) != 0 {
		t.Fatal("expected none")
	}
}
