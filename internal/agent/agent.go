// Package agent implements a server-side tool-using agent loop on top of
// the OpenAI gateway: send messages, let the model emit web_search tool
// calls (a <search>…</search> text protocol, which works with every chat
// template), execute them, feed results back, and continue until the model
// answers in plain text.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Event is one SSE-able agent event.
type Event struct {
	Type    string `json:"type"` // delta | tool | done | error
	Content string `json:"content,omitempty"` // delta text
	Step    int    `json:"step,omitempty"`
	// tool event fields:
	Name    string `json:"name,omitempty"`
	Query   string `json:"query,omitempty"`
	Results any    `json:"results,omitempty"`
	Error   string `json:"error,omitempty"`
	// done event fields:
	Steps int `json:"steps,omitempty"`
}

// Tool executes one tool call and returns a compact text result for the
// model plus structured results for the UI.
type Tool interface {
	Name() string
	Execute(ctx context.Context, argsJSON string) (text string, structured any, err error)
}

// Message is an OpenAI chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request for Run.
type Request struct {
	Model       string
	Messages    []Message // conversation WITHOUT system prompt
	MaxSteps    int       // default 5, cap 10
	MaxTokens   int
	Temperature *float64
	APIKey      string // forwarded to the gateway as Bearer
}

const systemPrompt = `You are llmctl, a helpful assistant with web access.
To search the web, reply with ONLY one or more lines of the form:
<search>your query here</search>
(You may instead use: <tool_call>{"name": "web_search", "arguments": {"query": "your query"}}</tool_call> )
You may issue up to 3 searches in one reply. After search results arrive, answer using them, citing URLs inline.
Rules:
- Search when the question needs current facts (news, prices, releases, versions) or when you are unsure.
- Never invent search results or URLs.
- If no search is needed, just answer normally.`

// --- tool-call parsing (multi-format) -------------------------------------

// toolCall is one parsed tool invocation.
type toolCall struct {
	Name     string // "" means default (web_search)
	Query    string // human-readable summary for the UI
	ArgsJSON string // arguments passed to the tool
	Raw      string // exact text to strip from the assistant turn
}

var (
	searchRe = regexp.MustCompile(`(?is)<search>\s*(.*?)\s*</search>`)
	// <tool_call> … <tool_call> (Hermes/Qwen/DeepSeek chat templates)
	toolCallTagRe = regexp.MustCompile(`(?is)<tool_call\s*>(.*?)</tool_call\s*>`)
	// <function=name> <parameter=k>v </parameter> … </function> (Qwen3 XML)
	funcFenceRe = regexp.MustCompile(`(?is)<function\s*=\s*([a-zA-Z0-9_.\-]+)\s*>(.*?)</function\s*>`)
	paramFenceRe = regexp.MustCompile(`(?is)<parameter\s*=\s*([a-zA-Z0-9_.\-]+)\s*>(.*?)</parameter\s*>`)
	// ```tool_call / ```tool / ```functions.x fenced JSON (Llama 3.x)
	toolFenceRe = regexp.MustCompile("(?is)```(?:tool_call|tool|functions?)[._a-z0-9\\-]*\\s*\\n(.*?)```")
	// bare JSON object with name+arguments (up to 3 levels of nesting)
	bareJSONRe = regexp.MustCompile(`(?s)\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*"name"\s*:\s*"[a-zA-Z0-9_.\-]+"\s*,(?:[^{}]|\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\})*"arguments"\s*:\s*(?:\{(?:[^{}]|\{[^{}]*\})*\}|"(?:\\.|[^"\\])*")(?:[^{}]|\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\})*\}`)
)

// suppressionMarkers: if any appears in streamed text, stop emitting deltas
// until we know whether it was a real tool call (false positives are
// re-flushed by the caller).
var suppressionMarkers = []string{"<search", "<tool_call", "<function=", "```tool", `"arguments"`}

// extractToolCalls finds tool calls in every supported format.
func extractToolCalls(text string) []toolCall {
	var out []toolCall
	seen := map[string]bool{}
	add := func(tc toolCall) {
		if tc.Query == "" && tc.ArgsJSON == "" {
			return
		}
		key := tc.Name + "\x00" + tc.Query + "\x00" + tc.Raw
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, tc)
	}

	// 1. <search>q</search>
	for _, m := range searchRe.FindAllStringSubmatch(text, -1) {
		q := strings.TrimSpace(m[1])
		args, _ := json.Marshal(map[string]string{"query": q})
		add(toolCall{Name: "web_search", Query: q, ArgsJSON: string(args), Raw: m[0]})
	}

	// 2. <tool_call>…</tool_call> (JSON body or XML <function=…> body)
	for _, m := range toolCallTagRe.FindAllStringSubmatch(text, -1) {
		inner := strings.TrimSpace(m[1])
		if fs := funcFenceRe.FindAllStringSubmatch(inner, -1); len(fs) > 0 {
			for _, f := range fs {
				tc := parseFuncFence(f[1], f[2])
				tc.Raw = m[0]
				add(tc)
			}
			continue
		}
		if tc, ok := parseCallJSON(inner); ok {
			tc.Raw = m[0]
			add(tc)
		}
	}

	// 3. <function=name>…</function> without a tool_call wrapper
	for _, m := range funcFenceRe.FindAllStringSubmatch(text, -1) {
		tc := parseFuncFence(m[1], m[2])
		tc.Raw = m[0]
		add(tc)
	}

	// 4. ```tool_call fences with JSON
	for _, m := range toolFenceRe.FindAllStringSubmatch(text, -1) {
		if tc, ok := parseCallJSON(strings.TrimSpace(m[1])); ok {
			tc.Raw = m[0]
			add(tc)
		}
	}

	// 5. bare {"name": …, "arguments": …} JSON objects
	for _, m := range bareJSONRe.FindAllString(text, -1) {
		if tc, ok := parseCallJSON(m); ok {
			tc.Raw = m
			add(tc)
		}
	}

	// Drop nested duplicates: an inner <function=…> or JSON object also
	// matches its own pattern outside the wrapper. Keep the outer Raw.
	filtered := out[:0]
	for i, tc := range out {
		nested := false
		for j, other := range out {
			if i != j && other.Raw != tc.Raw && strings.Contains(other.Raw, tc.Raw) {
				nested = true
				break
			}
		}
		if !nested {
			filtered = append(filtered, tc)
		}
	}
	return filtered
}

// parseFuncFence builds a toolCall from <function=NAME>body</function> where
// body contains <parameter=k>value</parameter> entries.
func parseFuncFence(name, body string) toolCall {
	params := map[string]string{}
	for _, p := range paramFenceRe.FindAllStringSubmatch(body, -1) {
		params[strings.ToLower(strings.TrimSpace(p[1]))] = strings.TrimSpace(p[2])
	}
	q := firstParam(params, "query", "q", "search_query", "searchquery", "search")
	if q == "" {
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			q = params[keys[0]]
		}
	}
	argsMap := make(map[string]string, len(params))
	for k, v := range params {
		argsMap[k] = v
	}
	if len(argsMap) == 0 && q != "" {
		argsMap["query"] = q
	}
	args, _ := json.Marshal(argsMap)
	return toolCall{Name: strings.ToLower(strings.TrimSpace(name)), Query: q, ArgsJSON: string(args)}
}

func firstParam(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// parseCallJSON parses {"name": …, "arguments": …} (arguments as object or
// JSON string) or {"name": …, "parameters": …}. Requires a name; a bare
// {"query": …} with no name is not treated as a call (too ambiguous).
func parseCallJSON(s string) (toolCall, bool) {
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &raw) != nil {
		return toolCall{}, false
	}
	var name string
	if nm, ok := raw["name"]; ok {
		_ = json.Unmarshal(nm, &name)
	}
	if name == "" {
		return toolCall{}, false
	}
	argsObj := map[string]any{}
	if a, ok := raw["arguments"]; ok {
		// arguments may be an object or a stringified object
		var asStr string
		if json.Unmarshal(a, &asStr) == nil {
			a = json.RawMessage(asStr)
		}
		if json.Unmarshal(a, &argsObj) != nil {
			argsObj = map[string]any{"query": strings.TrimSpace(string(a))}
		}
	} else if a, ok := raw["parameters"]; ok {
		_ = json.Unmarshal(a, &argsObj)
	}
	q, _ := argsObj["query"].(string)
	if q == "" {
		q, _ = argsObj["q"].(string)
	}
	if len(argsObj) == 0 && q != "" {
		argsObj = map[string]any{"query": q}
	}
	args, _ := json.Marshal(argsObj)
	return toolCall{Name: strings.ToLower(name), Query: strings.TrimSpace(q), ArgsJSON: string(args)}, true
}

// stripToolCalls removes every recognized tool-call block from model text.
func stripToolCalls(text string) string {
	s := searchRe.ReplaceAllString(text, "")
	s = toolCallTagRe.ReplaceAllString(s, "")
	s = funcFenceRe.ReplaceAllString(s, "")
	s = toolFenceRe.ReplaceAllString(s, "")
	s = bareJSONRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// resolveTool maps a parsed call to a registered tool. Empty names default
// to web_search; exact names win; search-ish names fall back to web_search.
func resolveTool(tools map[string]Tool, name string) (string, Tool, bool) {
	if name == "" {
		t, ok := tools["web_search"]
		return "web_search", t, ok
	}
	if t, ok := tools[name]; ok {
		return name, t, true
	}
	if _, ok := tools["web_search"]; ok && strings.Contains(name, "search") {
		return "web_search", tools["web_search"], true
	}
	return name, nil, false
}

func containsMarker(s string) bool {
	lower := strings.ToLower(s)
	for _, m := range suppressionMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// Run drives the agent loop, emitting events until the final answer has
// been streamed. gw is the gateway's HTTP handler (called in-process).
func Run(ctx context.Context, gw http.Handler, tools map[string]Tool, req Request, emit func(Event)) {
	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 5
	}
	if maxSteps > 10 {
		maxSteps = 10
	}

	messages := make([]Message, 0, len(req.Messages)+4)
	messages = append(messages, Message{Role: "system", Content: systemPrompt})
	messages = append(messages, req.Messages...)

	step := 0
	for ; step < maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			emit(Event{Type: "error", Error: "cancelled"})
			return
		}

		// One model turn. Deltas stream to the UI until a tool-call
		// marker appears; from then on output is suppressed and
		// buffered so raw tool-call text never leaks. emittedLen
		// tracks how much has already gone to the UI so a false
		// positive marker can be re-flushed without duplication.
		var full strings.Builder
		suppressed := false
		emittedLen := 0
		onDelta := func(d string) {
			full.WriteString(d)
			if !suppressed && containsMarker(full.String()) {
				suppressed = true
			}
			if !suppressed {
				emittedLen = full.Len()
				emit(Event{Type: "delta", Content: d})
			}
		}

		if err := callGateway(ctx, gw, req, messages, onDelta); err != nil {
			emit(Event{Type: "error", Error: err.Error()})
			return
		}

		answer := full.String()
		calls := extractToolCalls(answer)
		if len(calls) == 0 {
			// Plain answer. If a false-positive marker suppressed the
			// tail, flush what was never sent.
			if suppressed && len(answer) > emittedLen {
				emit(Event{Type: "delta", Content: answer[emittedLen:]})
			}
			emit(Event{Type: "done", Steps: step + 1})
			return
		}

		// Execute calls (cap 3 per step).
		if len(calls) > 3 {
			calls = calls[:3]
		}
		var sb strings.Builder
		sb.WriteString("Tool results:\n")
		for _, tc := range calls {
			name, tool, ok := resolveTool(tools, tc.Name)
			if !ok {
				avail := make([]string, 0, len(tools))
				for k := range tools {
					avail = append(avail, k)
				}
				sort.Strings(avail)
				msg := fmt.Sprintf("unknown tool %q (available: %s)", tc.Name, strings.Join(avail, ", "))
				sb.WriteString(fmt.Sprintf("\nTool %s: %s\n", tc.Name, msg))
				emit(Event{Type: "tool", Step: step + 1, Name: tc.Name, Query: tc.Query, Error: msg})
				continue
			}
			args := tc.ArgsJSON
			if args == "" {
				args = "{}"
			}
			text, structured, terr := tool.Execute(ctx, args)
			if terr != nil {
				text = "tool error: " + terr.Error()
			}
			emit(Event{Type: "tool", Step: step + 1, Name: name, Query: tc.Query, Results: structured, Error: errStr(terr)})
			sb.WriteString(fmt.Sprintf("\nTool %s(%s):\n%s\n", name, tc.Query, text))
		}
		// Append the assistant turn (stripped of tool-call blocks) and
		// the tool results as a user turn (works with every template).
		if cleaned := stripToolCalls(answer); cleaned != "" {
			messages = append(messages, Message{Role: "assistant", Content: cleaned})
		}
		messages = append(messages, Message{Role: "user", Content: sb.String()})
	}
	emit(Event{Type: "delta", Content: "\n\n(stopped: reached the maximum number of tool steps)"})
	emit(Event{Type: "done", Steps: step})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// callGateway makes one streaming chat completion against the gateway
// handler in-process and forwards content deltas via onDelta. ServeHTTP
// blocks for the whole SSE stream, so this is fully synchronous.
func callGateway(ctx context.Context, gw http.Handler, req Request, messages []Message, onDelta func(string)) error {
	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   true,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	b, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(b))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)
	}

	rec := &streamRecorder{onDelta: onDelta}
	gw.ServeHTTP(rec, httpReq)

	if rec.status != 0 && rec.status != http.StatusOK {
		msg := strings.TrimSpace(rec.errBody.String())
		var env struct {
			Error struct{ Message string `json:"message"` } `json:"error"`
		}
		if json.Unmarshal(rec.errBody.Bytes(), &env) == nil && env.Error.Message != "" {
			msg = env.Error.Message
		}
		if msg == "" {
			msg = fmt.Sprintf("gateway returned %d", rec.status)
		}
		return fmt.Errorf("upstream: %s", msg)
	}
	return nil
}

// streamRecorder captures a streaming chat-completion response: forwards
// content deltas live, buffers non-200 bodies for error reporting. Safe for
// the single-goroutine ServeHTTP write pattern.
type streamRecorder struct {
	onDelta func(string)
	status  int
	errBody bytes.Buffer
	partial string // partial SSE line across Writes
}

func (r *streamRecorder) Header() http.Header { return http.Header{} }

func (r *streamRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.status != http.StatusOK {
		return r.errBody.Write(p)
	}
	r.partial += string(p)
	for {
		idx := strings.IndexByte(r.partial, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(r.partial[:idx])
		r.partial = r.partial[idx+1:]
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(line[5:])
		if data == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &chunk) == nil && len(chunk.Choices) > 0 {
			if c := chunk.Choices[0].Delta.Content; c != "" {
				r.onDelta(c)
			}
		}
	}
	return len(p), nil
}

// Flush satisfies http.Flusher (the gateway flushes per SSE chunk).
func (r *streamRecorder) Flush() {}
