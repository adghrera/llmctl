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
To search the web for current information, reply with ONLY lines of the form:
<search>your query here</search>
You may issue up to 3 searches in one reply. After search results arrive, answer using them, citing URLs inline.
Rules:
- Search when the question needs current facts (news, prices, releases, versions) or when you are unsure.
- Never invent search results or URLs.
- If no search is needed, just answer normally.`

var searchRe = regexp.MustCompile(`(?is)<search>\s*(.*?)\s*</search>`)

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

		// One model turn. Deltas stream to the UI until a <search marker
		// appears; from then on output is suppressed and buffered so raw
		// tool-call text never leaks. emittedLen tracks how much has
		// already gone to the UI so a false-positive marker can be
		// re-flushed without duplication.
		var full strings.Builder
		suppressed := false
		emittedLen := 0
		onDelta := func(d string) {
			full.WriteString(d)
			if !suppressed && strings.Contains(full.String(), "<search") {
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
		queries := extractSearchQueries(answer)
		if len(queries) == 0 {
			// Plain answer. If a false-positive "<search" suppressed the
			// tail, flush what was never sent.
			if suppressed && len(answer) > emittedLen {
				emit(Event{Type: "delta", Content: answer[emittedLen:]})
			}
			emit(Event{Type: "done", Steps: step + 1})
			return
		}

		// Execute searches (cap 3 per step).
		if len(queries) > 3 {
			queries = queries[:3]
		}
		var sb strings.Builder
		sb.WriteString("Web search results:\n")
		for _, q := range queries {
			tool, ok := tools["web_search"]
			if !ok {
				sb.WriteString(fmt.Sprintf("\nQuery: %s\n(no web_search tool configured)\n", q))
				continue
			}
			args, _ := json.Marshal(map[string]any{"query": q})
			text, structured, terr := tool.Execute(ctx, string(args))
			if terr != nil {
				text = "search error: " + terr.Error()
			}
			emit(Event{Type: "tool", Step: step + 1, Name: "web_search", Query: q, Results: structured, Error: errStr(terr)})
			sb.WriteString(fmt.Sprintf("\nQuery: %s\n%s\n", q, text))
		}
		// Append the assistant turn (stripped of search blocks) and the
		// tool results as a user turn (works with every chat template).
		if cleaned := strings.TrimSpace(searchRe.ReplaceAllString(answer, "")); cleaned != "" {
			messages = append(messages, Message{Role: "assistant", Content: cleaned})
		}
		messages = append(messages, Message{Role: "user", Content: sb.String()})
	}
	emit(Event{Type: "delta", Content: "\n\n(stopped: reached the maximum number of search steps)"})
	emit(Event{Type: "done", Steps: step})
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func extractSearchQueries(text string) []string {
	ms := searchRe.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if q := strings.TrimSpace(m[1]); q != "" {
			out = append(out, q)
		}
	}
	return out
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
