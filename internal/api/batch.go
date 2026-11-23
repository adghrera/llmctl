package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

// maxBatchPrompts bounds one batch request.
const maxBatchPrompts = 200

type batchRequest struct {
	Model       string   `json:"model"`
	System      string   `json:"system,omitempty"`
	Prompts     []string `json:"prompts"`
	MaxTokens   int      `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"` // nil = model default (F343)
	Seed        *int64   `json:"seed,omitempty"`        // reproducible sampling seed (F795)
	TopP        *float64 `json:"topP,omitempty"`        // nucleus sampling top_p (F799)
	FreqPen     *float64 `json:"freqPenalty,omitempty"` // frequency penalty (F805)
	PresPen     *float64 `json:"presPenalty,omitempty"` // presence penalty (F805)
	Concurrency int      `json:"concurrency,omitempty"` // default 4, max 8
	APIKey      string   `json:"-"`
}

type batchResult struct {
	Prompt  string `json:"prompt"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

// batch runs many prompts through the gateway concurrently (F062). Each
// prompt is an independent single-turn completion; failures are per-prompt,
// never aborting the batch.
func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	var req batchRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" || len(req.Prompts) == 0 {
		writeErr(w, 400, "model and prompts are required")
		return
	}
	if len(req.Prompts) > maxBatchPrompts {
		writeErr(w, 400, "too many prompts (max 200)")
		return
	}
	if s.gw == nil {
		writeErr(w, 503, "gateway not running")
		return
	}
	if req.Concurrency <= 0 {
		req.Concurrency = 4
	}
	if req.Concurrency > 8 {
		req.Concurrency = 8
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		req.APIKey = strings.TrimSpace(h[7:])
	}

	results := make([]batchResult, len(req.Prompts))
	var next sync.Mutex
	idx := 0
	var wg sync.WaitGroup
	for worker := 0; worker < req.Concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				next.Lock()
				i := idx
				idx++
				next.Unlock()
				if i >= len(req.Prompts) {
					return
				}
				var msgs []chatMsg
				if strings.TrimSpace(req.System) != "" {
					msgs = append(msgs, chatMsg{Role: "system", Content: req.System})
				}
				msgs = append(msgs, chatMsg{Role: "user", Content: req.Prompts[i]})
				content, err := s.gatewayChat(r.Context(), req.Model, req.APIKey, msgs, chatOpts{MaxTokens: req.MaxTokens, Temp: req.Temperature, Seed: req.Seed, TopP: req.TopP, FreqPen: req.FreqPen, PresPen: req.PresPen})
				results[i] = batchResult{Prompt: req.Prompts[i], Content: content}
				if err != nil {
					results[i].Error = err.Error()
				}
			}
		}()
	}
	wg.Wait()

	okCount := 0
	for _, res := range results {
		if res.Error == "" {
			okCount++
		}
	}
	writeJSON(w, 200, map[string]any{
		"ok":          okCount,
		"failed":      len(results) - okCount,
		"concurrency": req.Concurrency,
		"results":     results,
	})
}
