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
		if mm["role"] == "user" && strings.Contains(mm["content"].(string), "Tool results") {
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
	if !strings.Contains(pt, "maximum number of tool steps") {
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
	qs := extractToolCalls("a <SEARCH> X </search> b <search>y</search>")
	if len(qs) != 2 || qs[0].Query != "X" || qs[1].Query != "y" {
		t.Fatalf("got %v", qs)
	}
	if len(extractToolCalls("no markers here")) != 0 {
		t.Fatal("expected none")
	}
}

func TestExtractToolCallsNativeFormats(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantName string
		wantQ    string
	}{
		{
			name:     "hermes JSON tool_call tag",
			text:     `Let me check. <tool_call>{"name": "web_search", "arguments": {"query": "mu earnings q4"}}</tool_call> done`,
			wantName: "web_search",
			wantQ:    "mu earnings q4",
		},
		{
			name:     "tool_call tag stringified arguments",
			text:     `<tool_call>{"name":"web_search","arguments":"{\"query\": \"gold price\"}"}</tool_call>`,
			wantName: "web_search",
			wantQ:    "gold price",
		},
		{
			name:     "Qwen XML function inside tool_call",
			text:     "<tool_call>\n<function=web_search>\n<parameter=query>\nopenai news today\n</parameter>\n</function>\n</tool_call>",
			wantName: "web_search",
			wantQ:    "openai news today",
		},
		{
			name:     "bare function XML no wrapper",
			text:     "<function=search>\n<parameter=q>\ntesla deliveries\n</parameter>\n</function>",
			wantName: "search",
			wantQ:    "tesla deliveries",
		},
		{
			name:     "tool_call fence JSON",
			text:     "checking:\n```tool_call\n{\"name\": \"web_search\", \"arguments\": {\"query\": \"fed rate\"}}\n```",
			wantName: "web_search",
			wantQ:    "fed rate",
		},
		{
			name:     "bare JSON name+arguments",
			text:     `{"name": "web_search", "arguments": {"query": "micron stock"}}`,
			wantName: "web_search",
			wantQ:    "micron stock",
		},
		{
			name:     "name+parameters variant",
			text:     `<tool_call>{"name": "web_search", "parameters": {"query": "nvda earnings"}}</tool_call>`,
			wantName: "web_search",
			wantQ:    "nvda earnings",
		},
	}
	for _, c := range cases {
		calls := extractToolCalls(c.text)
		if len(calls) != 1 {
			t.Errorf("%s: got %d calls: %+v", c.name, len(calls), calls)
			continue
		}
		_, _, ok := resolveTool(map[string]Tool{"web_search": &fakeTool{}}, calls[0].Name)
		if !ok {
			t.Errorf("%s: name %q does not resolve", c.name, calls[0].Name)
		}
		if calls[0].Query != c.wantQ {
			t.Errorf("%s: query = %q want %q", c.name, calls[0].Query, c.wantQ)
		}
		// strip must leave no tool markup behind
		stripped := stripToolCalls(c.text)
		for _, marker := range []string{"tool_call", "<function", "<parameter", `"arguments"`} {
			if strings.Contains(strings.ToLower(stripped), marker) {
				t.Errorf("%s: strip left %q in %q", c.name, marker, stripped)
			}
		}
	}
}

func TestExtractToolCallsPythonic(t *testing.T) {
	// exact user-reported format: Llama-4 pythonic with two calls
	text := `<|tool_call_start|>[web_search(query='best stocks to buy next week 2026 stock market recommendations'), web_search(query='top stock picks for next week 2026 market analysis')]<|tool_call_end|>`
	calls := extractToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("got %d calls: %+v", len(calls), calls)
	}
	if calls[0].Name != "web_search" || calls[0].Query != "best stocks to buy next week 2026 stock market recommendations" {
		t.Errorf("call 0 = %+v", calls[0])
	}
	if calls[1].Name != "web_search" || calls[1].Query != "top stock picks for next week 2026 market analysis" {
		t.Errorf("call 1 = %+v", calls[1])
	}
	if stripped := stripToolCalls(text); stripped != "" {
		t.Errorf("strip left %q", stripped)
	}
	// missing end tag tolerated
	calls = extractToolCalls(`<|tool_call_start|>[web_search(query='fed rate'])]`)
	if len(calls) != 1 || calls[0].Query != "fed rate" {
		t.Fatalf("no-end-tag: %+v", calls)
	}
	// double quotes + positional arg
	calls = extractToolCalls(`<|tool_call_start|>[web_search("gold price 2026")]<|tool_call_end|>`)
	if len(calls) != 1 || calls[0].Query != "gold price 2026" {
		t.Fatalf("positional: %+v", calls)
	}
	// prose with parens must not be stripped or parsed
	if calls := extractToolCalls("I looked (today) at web_search (a search engine)"); len(calls) != 0 {
		t.Errorf("false positive: %+v", calls)
	}
	if s := stripToolCalls("I looked (today)"); s != "I looked (today)" {
		t.Errorf("prose stripped: %q", s)
	}
}

func TestAgentPythonicLoop(t *testing.T) {
	script1 := `<|tool_call_start|>[web_search(query='best stocks next week'), web_search(query='market analysis next week')]<|tool_call_end|>`
	script2 := "Based on the searches: watch MU and NVDA."
	events, fw, tool := runAgent(t,
		[]string{script1, script2},
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "stocks next week?"}}},
	)
	if fw.calls != 2 {
		t.Fatalf("calls = %d", fw.calls)
	}
	if len(tool.queries) != 2 || tool.queries[0] != "best stocks next week" || tool.queries[1] != "market analysis next week" {
		t.Fatalf("queries = %v", tool.queries)
	}
	pt := plainText(events)
	if strings.Contains(pt, "tool_call_start") || strings.Contains(pt, "web_search(") {
		t.Fatalf("raw markup leaked: %q", pt)
	}
	if !strings.Contains(pt, "watch MU and NVDA") {
		t.Fatalf("answer missing: %q", pt)
	}
}

func TestExtractToolCallsNoFalsePositives(t *testing.T) {
	// ordinary prose and JSON examples must NOT be treated as calls
	plain := []string{
		`The config looks like {"name": "Alice", "age": 30}.`,
		"Here is a code block:\n```json\n{\"name\": \"myapp\", \"version\": \"1.0\"}\n```",
		"We should search the web for that. (I can't though.)",
	}
	for _, p := range plain {
		if calls := extractToolCalls(p); len(calls) != 0 {
			t.Errorf("false positive on %q: %+v", p, calls)
		}
	}
}

func TestAgentNativeToolCallLoop(t *testing.T) {
	// Model emits a Hermes-style native tool call; loop must execute it.
	script1 := `Looking it up. <tool_call>{"name": "web_search", "arguments": {"query": "record revenue"}}</tool_call>`
	script2 := "MU reported record revenue."
	events, fw, tool := runAgent(t,
		[]string{script1, script2},
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "MU earnings?"}}},
	)
	if fw.calls != 2 {
		t.Fatalf("gateway calls = %d, want 2", fw.calls)
	}
	if len(tool.queries) != 1 || tool.queries[0] != "record revenue" {
		t.Fatalf("tool queries = %v", tool.queries)
	}
	pt := plainText(events)
	if strings.Contains(pt, "tool_call") || strings.Contains(pt, "arguments") {
		t.Fatalf("raw tool call leaked to UI: %q", pt)
	}
	if !strings.Contains(pt, "Looking it up.") || !strings.Contains(pt, "record revenue") {
		t.Fatalf("text missing: %q", pt)
	}
}

func TestAgentUnknownToolFedBack(t *testing.T) {
	script1 := `<tool_call>{"name": "get_weather", "arguments": {"city": "Paris"}}</tool_call>`
	script2 := "Sorry, I can't check weather."
	events, fw, _ := runAgent(t,
		[]string{script1, script2},
		Request{Model: "m", Messages: []Message{{Role: "user", Content: "weather in Paris?"}}},
	)
	if fw.calls != 2 {
		t.Fatalf("calls = %d", fw.calls)
	}
	var sawErr bool
	for _, e := range events {
		if e.Type == "tool" && strings.Contains(e.Error, "unknown tool") {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatalf("no unknown-tool error event: %+v", events)
	}
	if !strings.Contains(plainText(events), "can't check weather") {
		t.Fatalf("final answer missing")
	}
}

// stubTool is a no-op agent.Tool for resolution tests.
type stubTool struct{ name string }

func (s *stubTool) Name() string { return s.name }
func (s *stubTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	return "ok", nil, nil
}

func TestResolveInstalledToolFuzzy(t *testing.T) {
	tools := map[string]Tool{
		"web_search":                &stubTool{"web_search"},
		"tool:stock-quotes-686039":  &stubTool{"tool:stock-quotes-686039"},
		"tool:word-counter-9476f4":  &stubTool{"tool:word-counter-9476f4"},
	}
	cases := []struct {
		in   string
		want string // "" = must NOT resolve
	}{
		{"tool:stock-quotes-686039", "tool:stock-quotes-686039"}, // exact
		{"stock-quotes-686039", "tool:stock-quotes-686039"},      // no prefix
		{"stock-quotes", "tool:stock-quotes-686039"},             // slug only
		{"stock_quotes", "tool:stock-quotes-686039"},              // underscore
		{"Stock Quotes", "tool:stock-quotes-686039"},              // display name w/ space
		{"STOCK.QUOTES", "tool:stock-quotes-686039"},              // dots + case
		{"word-counter", "tool:word-counter-9476f4"},
		{"calculator", ""}, // built-in not present here — must not fuzzy-match
		{"completely-unknown", ""},
	}
	for _, c := range cases {
		name, _, ok := resolveTool(tools, c.in)
		if c.want == "" {
			if ok {
				t.Errorf("resolveTool(%q) resolved to %q, want no match", c.in, name)
			}
			continue
		}
		if !ok || name != c.want {
			t.Errorf("resolveTool(%q) = %q, %v; want %q", c.in, name, ok, c.want)
		}
	}
}

func TestStripPartialMarkers(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<tool_<tool_Based on the tool, MU is $856.", "Based on the tool, MU is $856."},
		{"<tool_<tool_call>\nMU is $856.", "MU is $856."},
		{"Answer: 42<|tool_call_start|>", "Answer: 42"},
		{"plain answer stays", "plain answer stays"},
		{"<function=stock", ""},
	}
	for _, c := range cases {
		if got := stripToolCalls(c.in); got != c.want {
			t.Errorf("stripToolCalls(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
