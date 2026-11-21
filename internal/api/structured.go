package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"llmctl/internal/jsonschema"
)

// extractJSON pulls the first JSON object out of a model reply: strips code
// fences, then takes the balanced {...} span.
func extractJSON(s string) (string, bool) {
	s = strings.TrimSpace(s)
	// Strip ``` fences if present.
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", false
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		switch {
		case c == '\\':
			esc = true
		case c == '"' && !esc:
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

// structuredRequest is the POST /api/v1/structured body.
type structuredRequest struct {
	Model      string         `json:"model"`
	Messages   []chatMsg      `json:"messages"`
	Schema     map[string]any `json:"schema"`
	MaxRetries int            `json:"maxRetries"` // default 2
	APIKey     string         `json:"-"`
}

type chatMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// structured generates a schema-validated JSON answer via the gateway,
// retrying with the validation error appended when the model misbehaves (F056).
func (s *Server) structured(w http.ResponseWriter, r *http.Request) {
	var req structuredRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" || len(req.Messages) == 0 || len(req.Schema) == 0 {
		writeErr(w, 400, "model, messages and schema are required")
		return
	}
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	if req.MaxRetries <= 0 || req.MaxRetries > 5 {
		req.MaxRetries = 2
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		req.APIKey = strings.TrimSpace(h[7:])
	}

	schemaJSON, _ := json.Marshal(req.Schema)
	msgs := append([]chatMsg{{
		Role:    "system",
		Content: "Respond with ONLY a single JSON value that validates against this JSON Schema. No prose, no code fences.\nSchema:\n" + string(schemaJSON),
	}}, req.Messages...)

	messages := append([]chatMsg(nil), msgs...)
	var lastErr string
	var lastRaw string
	for attempt := 1; attempt <= req.MaxRetries+1; attempt++ {
		content, err := s.gatewayChat(r.Context(), req.Model, req.APIKey, messages, 0)
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		lastRaw = content
		raw, ok := extractJSON(content)
		if !ok {
			lastErr = "no JSON object found in the reply"
		} else {
			var parsed any
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				lastErr = "invalid JSON: " + err.Error()
			} else if err := jsonschema.Validate(req.Schema, parsed); err != nil {
				lastErr = err.Error()
			} else {
				writeJSON(w, 200, map[string]any{
					"ok":       true,
					"json":     parsed,
					"attempts": attempt,
				})
				return
			}
		}
		messages = append(messages,
			chatMsg{Role: "assistant", Content: content},
			chatMsg{Role: "user", Content: "That output failed schema validation: " + lastErr + "\nReply again with ONLY valid JSON."},
		)
	}
	writeJSON(w, 422, map[string]any{
		"ok":       false,
		"error":    lastErr,
		"raw":      lastRaw,
		"attempts": req.MaxRetries + 1,
	})
}

// gatewayChat performs one non-streaming chat completion against the
// gateway in-process. maxTokens <= 0 means backend default.
func (s *Server) gatewayChat(ctx context.Context, model, key string, messages []chatMsg, maxTokens int) (string, error) {
	bodyMap := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   false,
	}
	if maxTokens > 0 {
		bodyMap["max_tokens"] = maxTokens
	}
	body, _ := json.Marshal(bodyMap)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	s.gw.Handler().ServeHTTP(rec, req)
	if rec.Code != 0 && rec.Code != http.StatusOK {
		return "", fmt.Errorf("gateway returned %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		return "", fmt.Errorf("bad gateway response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("gateway returned no choices")
	}
	return out.Choices[0].Message.Content, nil
}
