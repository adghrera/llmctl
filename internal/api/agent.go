package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"llmctl/internal/agent"
)

// agentChatReq is the POST /api/v1/chat/agent body.
type agentChatReq struct {
	Model       string             `json:"model"`
	Messages    []agent.Message    `json:"messages"`
	MaxSteps    int                `json:"maxSteps"`    // default 5
	MaxTokens   int                `json:"maxTokens"`
	Temperature *float64           `json:"temperature"`
}

// agentSearchTool returns the web_search tool for the agent loop. Tests
// override s.agentTool to avoid live network calls.
func (s *Server) agentSearchTool() agent.Tool {
	if s.agentTool != nil {
		return s.agentTool
	}
	return &agent.WebSearchTool{S: s.searcher}
}

// chatAgent runs a tool-using agent turn server-side and streams progress
// as SSE: {"type":"delta",...} | {"type":"tool",...} | {"type":"done",...}
// | {"type":"error",...}. The model may call web_search (free, keyless);
// results are fed back and the loop continues until a plain answer.
func (s *Server) chatAgent(w http.ResponseWriter, r *http.Request) {
	var req agentChatReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeErr(w, 400, "model required")
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, 400, "messages required")
		return
	}
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}

	// Gateway auth: reuse the caller's Authorization header verbatim.
	apiKey := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		apiKey = strings.TrimSpace(h[7:])
	}

	tools := map[string]agent.Tool{
		"web_search": s.agentSearchTool(),
	}
	areq := agent.Request{
		Model:       req.Model,
		Messages:    req.Messages,
		MaxSteps:    req.MaxSteps,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		APIKey:      apiKey,
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	flusher.Flush()

	send := func(ev agent.Event) {
		b, _ := json.Marshal(ev)
		io.WriteString(w, "data: "+string(b)+"\n\n")
		flusher.Flush()
	}

	agent.Run(r.Context(), s.gw.Handler(), tools, areq, send)
}
