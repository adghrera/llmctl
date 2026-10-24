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
	Model          string
	Messages       []Message // conversation WITHOUT system prompt
	MaxSteps       int       // default 5, cap 10
	MaxTokens      int
	Temperature    *float64
	APIKey         string // forwarded to the gateway as Bearer
	ExtraToolsText string // appended to the system prompt (installed tools, F033)
	CtxSize        int    // model context window (tokens); 0 = unknown, no compaction
}

const systemPrompt = `You are llmctl, a helpful assistant with tools.
Available tools — to call one, reply with ONLY one or more lines of the form:
  <search>your query here</search>                                          (web_search)
  <tool_call>{"name": "web_search", "arguments": {"query": "..."}}</tool_call>
  <tool_call>{"name": "fetch_url", "arguments": {"url": "https://..."}}</tool_call>
  <tool_call>{"name": "calculator", "arguments": {"expression": "2*(3+4)^2"}}</tool_call>
  <tool_call>{"name": "current_time", "arguments": {}}</tool_call>
You may issue up to 3 tool calls in one reply. After results arrive, answer using them, citing URLs inline.
Rules:
- web_search when the question needs current facts (news, prices, releases, versions) or when you are unsure.
- fetch_url to read a specific page (e.g. a search result that looks most relevant, or a URL the user gave).
- calculator for ANY arithmetic — never compute mentally.
- current_time for questions about today's date or the current time.
- Never invent tool results or URLs.
- If no tool is needed, just answer normally.`

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
	// <|tool_call_start|> [fn(query='x'), fn2(query="y")] <|tool_call_end|>
	// (Llama-4 / Ollama "pythonic" style). Tolerates a missing end tag.
	pySentinelRe = regexp.MustCompile(`(?is)<\|\s*tool_call_start\s*\|>(.*?)(?:<\|\s*tool_call_end\s*\|>|$)`)
	// one pythonic call: name(args) with up to one level of nested parens
	pyCallRe = regexp.MustCompile(`([a-zA-Z0-9_.\-]+)\s*\(((?:[^()]|\([^()]*\))*)\)`)
	// one kwarg inside a pythonic call (quoted value or bare token)
	pyArgRe = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)\s*=\s*('(?:\\.|[^'])*'|"(?:\\.|[^"])*"|[^,()]+)`)
	// bare JSON object with name+arguments (up to 3 levels of nesting)
	bareJSONRe = regexp.MustCompile(`(?s)\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*"name"\s*:\s*"[a-zA-Z0-9_.\-]+"\s*,(?:[^{}]|\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\})*"arguments"\s*:\s*(?:\{(?:[^{}]|\{[^{}]*\})*\}|"(?:\\.|[^"\\])*")(?:[^{}]|\{(?:[^{}]|\{(?:[^{}]|\{[^{}]*\})*\})*\})*\}`)
)

// suppressionMarkers: if any appears in streamed text, stop emitting deltas
// until we know whether it was a real tool call (false positives are
// re-flushed by the caller).
var suppressionMarkers = []string{"<search", "<tool_call", "<tool_", "<function=", "```tool", `"arguments"`, "<|tool_call"}

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

	// 6. <|tool_call_start|> [fn(query='x'), …] <|tool_call_end|> (pythonic)
	for _, m := range pySentinelRe.FindAllStringSubmatch(text, -1) {
		for _, c := range parsePythonicCalls(m[1]) {
			c.Raw = m[0]
			add(c)
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

// parsePythonicCalls parses a pythonic call list body such as
// "[web_search(query='a'), other(x=1, y='two')]" into tool calls.
// Positional string args become "query"; quoted literals are unquoted.
func parsePythonicCalls(body string) []toolCall {
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "[")
	body = strings.TrimSuffix(body, "]")
	var out []toolCall
	for _, m := range pyCallRe.FindAllStringSubmatch(body, -1) {
		name := strings.ToLower(strings.TrimSpace(m[1]))
		argsRaw := strings.TrimSpace(m[2])
		args := map[string]string{}
		var positional []string
		if argsRaw != "" {
			for _, a := range pyArgRe.FindAllStringSubmatch(argsRaw, -1) {
				key := strings.ToLower(strings.TrimSpace(a[1]))
				val := unquotePy(strings.TrimSpace(a[2]))
				if key == "self" {
					continue
				}
				args[key] = val
			}
			// pyArgRe only catches kwargs; grab positional args too by
			// splitting top-level commas and taking bare values.
			positional = positionalPyArgs(argsRaw)
			if len(args) == 0 {
				for i, p := range positional {
					if i == 0 {
						args["query"] = p
					} else {
						args[fmt.Sprintf("arg%d", i)] = p
					}
				}
			}
		}
		q := args["query"]
		if q == "" {
			q = args["q"]
		}
		if q == "" && len(positional) > 0 {
			q = positional[0]
		}
		argsJSON, _ := json.Marshal(args)
		out = append(out, toolCall{Name: name, Query: q, ArgsJSON: string(argsJSON)})
	}
	return out
}

// positionalPyArgs returns unquoted positional (non-kwarg) argument values.
func positionalPyArgs(argsRaw string) []string {
	var out []string
	depth := 0
	inStr := byte(0)
	start := 0
	emit := func(part string) {
		part = strings.TrimSpace(part)
		if part == "" {
			return
		}
		// kwarg? (identifier = …) at top level
		if i := strings.IndexByte(part, '='); i > 0 && strings.IndexByte(part, '(') < 0 {
			// check all before '=' is identifier chars
			isKw := true
			for j := 0; j < i; j++ {
				c := part[j]
				if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || j == 0 && (c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z')) {
					isKw = false
					break
				}
			}
			if isKw {
				return
			}
		}
		out = append(out, unquotePy(part))
	}
	for i := 0; i < len(argsRaw); i++ {
		c := argsRaw[i]
		if inStr != 0 {
			if c == '\\' {
				i++
			} else if c == inStr {
				inStr = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			inStr = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				emit(argsRaw[start:i])
				start = i + 1
			}
		}
	}
	emit(argsRaw[start:])
	return out
}

// unquotePy strips matching single/double quotes from a Python literal.
func unquotePy(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			inner := s[1 : len(s)-1]
			inner = strings.ReplaceAll(inner, `\'`, `'`)
			inner = strings.ReplaceAll(inner, `\"`, `"`)
			inner = strings.ReplaceAll(inner, `\\`, `\`)
			return inner
		}
	}
	return s
}

// stripToolCalls removes every recognized tool-call block from model text.
func stripToolCalls(text string) string {
	s := searchRe.ReplaceAllString(text, "")
	s = toolCallTagRe.ReplaceAllString(s, "")
	s = funcFenceRe.ReplaceAllString(s, "")
	s = toolFenceRe.ReplaceAllString(s, "")
	s = pySentinelRe.ReplaceAllString(s, "")
	s = bareJSONRe.ReplaceAllString(s, "")
	// Truncated marker fragments: small models sometimes emit a partial
	// "<tool_" (token cut mid-marker) with no closing tag, which survives the
	// full-pattern strips above and renders as garbage at the start of the
	// answer. Remove any leftover marker openers.
	s = partialMarkerRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// partialMarkerRe matches leftover tool-call marker openers, including the
// truncated forms ("<tool_", "<|tool_call", "<function=", "<parameter=").
// Alternatives are explicit literal truncations of each marker (RE2 has no
// lookahead and open-ended classes would swallow following words).
var partialMarkerRe = regexp.MustCompile(`(?is)` +
	`<\|\s*tool_call\s*_?\s*start\s*\|?\s*>?|<\|\s*tool_call\s*_?\s*end\s*\|?\s*>?|<\|\s*tool_call[a-z_]*|<\|\s*tool[a-z_]*|<\|>` +
	`|<tool_call[^>]*>?|<tool_c(?:a(?:l(?:l)?)?)?(?:_[a-z]+)?>?|<tool_` +
	`|<search\b[^>]*|</search\s*>` +
	`|<function\s*=?[^>]*|</function\s*>` +
	`|<parameter\s*=?[^>]*|</parameter\s*>` +
	`|<\|\s*im_start\s*\|>[a-z]*|<\|\s*im_end\s*\|>`)

// resolveTool maps a parsed call to a registered tool. Empty names default
// to web_search; exact names win; search-ish names fall back to web_search.
func resolveTool(tools map[string]Tool, name string) (string, Tool, bool) {
	if name == "" {
		t, ok := tools["web_search"]
		return "web_search", t, ok
	}
	name = strings.TrimSpace(name)
	norm := strings.TrimPrefix(name, "tool:")
	if t, ok := tools[norm]; ok {
		return norm, t, true
	}
	if t, ok := tools["tool:"+norm]; ok {
		return "tool:" + norm, t, true
	}
	// Installed tools carry a random id suffix (word-counter-9476f4); models
	// naturally drop it and emit stock_quotes, stock-quotes, or Stock Quotes.
	// Match on the normalized name against every "tool:" entry.
	if key, ok := matchInstalledTool(tools, norm); ok {
		return key, tools[key], true
	}
	if _, ok := tools["web_search"]; ok && strings.Contains(norm, "search") {
		return "web_search", tools["web_search"], true
	}
	return name, nil, false
}

// normalizeToolKey lowercases and collapses _/-/space runs so "Stock Quotes",
// "stock_quotes" and "stock-quotes" all compare equal.
func normalizeToolKey(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch r {
		case '_', '-', ' ', '.':
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		default:
			b.WriteRune(r)
			prevDash = false
		}
	}
	return strings.Trim(b.String(), "-")
}

// matchInstalledTool finds a "tool:<id>" entry whose id equals the wanted
// name, starts with "<name>-" (id suffix dropped by the model), or whose
// normalized form matches. Ambiguous prefixes resolve to the shortest id.
func matchInstalledTool(tools map[string]Tool, wanted string) (string, bool) {
	wantNorm := normalizeToolKey(wanted)
	if wantNorm == "" {
		return "", false
	}
	best := ""
	var bestID string
	for key := range tools {
		if !strings.HasPrefix(key, "tool:") {
			continue
		}
		id := key[len("tool:"):]
		if id == wanted || normalizeToolKey(id) == wantNorm {
			return key, true
		}
		// id is "<slug>-<6hex>"; the model emits just the slug
		if base, ok := stripIDSuffix(id); ok {
			if normalizeToolKey(base) == wantNorm {
				if best == "" || len(id) < len(bestID) {
					best, bestID = key, id
				}
			}
		}
	}
	return best, best != ""
}

// idSuffixRe matches the random 6-hex suffix genID appends to tool ids.
var idSuffixRe = regexp.MustCompile(`-[0-9a-f]{6}$`)

// stripIDSuffix removes the genID hex suffix: "stock-quotes-686039" ->
// "stock-quotes".
func stripIDSuffix(id string) (string, bool) {
	if m := idSuffixRe.FindString(id); m != "" {
		return strings.TrimSuffix(id, m), true
	}
	return "", false
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
	sysPrompt := systemPrompt
	if req.ExtraToolsText != "" {
		sysPrompt += "\n" + req.ExtraToolsText
	}
	messages = append(messages, Message{Role: "system", Content: sysPrompt})
	messages = append(messages, req.Messages...)

	step := 0
	for ; step < maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			emit(Event{Type: "error", Error: "cancelled"})
			return
		}

		// Auto-compact: when context usage reaches 98% of the model's
		// context window, trim the oldest user/assistant message pairs
		// (keeping the system prompt and most recent turns).
		if req.CtxSize > 0 {
			var trimmed bool
			messages, trimmed = compactMessages(messages, req.CtxSize)
			if trimmed {
				emit(Event{Type: "system", Content: "(context window near limit — older messages trimmed)"})
			}
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

// estimateTokens estimates token count from text using the ~4 chars/token
// heuristic. Returns at least 1 for non-empty text.
func estimateTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	n := (len(text) + 3) / 4
	if n < 1 {
		n = 1
	}
	return n
}

// compactMessages trims the oldest user/assistant message pairs when total
// estimated token usage reaches 98% of ctxSize. The system message (index 0)
// is never removed. Returns the (possibly shortened) slice and true if any
// messages were removed.
func compactMessages(msgs []Message, ctxSize int) ([]Message, bool) {
	const reserve = 256 // reserve tokens for the model's response
	limit := ctxSize - reserve
	if limit <= 0 || len(msgs) < 3 {
		return msgs, false
	}

	total := estimateTokens(msgs[0].Content) // system prompt
	for i := 1; i < len(msgs); i++ {
		total += estimateTokens(msgs[i].Content)
	}
	if float64(total)/float64(ctxSize)*100 < 98 {
		return msgs, false
	}

	// Remove oldest user+assistant pairs (indices 1,2 then 3,4, etc.)
	// until under the limit or only system + one turn remains.
	for len(msgs) > 2 {
		msgs = append(msgs[:1], msgs[3:]...)
		total = estimateTokens(msgs[0].Content)
		for i := 1; i < len(msgs); i++ {
			total += estimateTokens(msgs[i].Content)
		}
		if float64(total)/float64(ctxSize)*100 < 98 {
			break
		}
	}
	return msgs, true
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

// ResolveToolForTest exposes resolveTool for cross-package tests.
func ResolveToolForTest(tools map[string]Tool, name string) (string, Tool, bool) {
	return resolveTool(tools, name)
}
