package api

import (
	"net/http"

	"llmctl/internal/proxy"
)

// Cost data comes from the gateway's live per-key/per-model token counters
// (F045; re-sourced in F074 so the numbers actually populate).

// costSummary handles GET /api/v1/cost.
func (s *Server) costSummary(w http.ResponseWriter, r *http.Request) {
	var in, out, total int64
	var cost float64
	if s.gw != nil {
		for _, u := range s.gw.KeyUsage() {
			in += u.InputTok
			out += u.OutputTok
			total += u.TotalTok
			cost += u.Cost
		}
	}
	writeJSON(w, 200, map[string]any{
		"input_tokens":  in,
		"output_tokens": out,
		"total_tokens":  total,
		"cost_usd":      cost,
	})
}

// costKeys handles GET /api/v1/cost/keys.
func (s *Server) costKeys(w http.ResponseWriter, r *http.Request) {
	out := map[string]proxy.KeyUsage{}
	if s.gw != nil {
		out = s.gw.KeyUsage()
	}
	writeJSON(w, 200, out)
}

// costModels handles GET /api/v1/cost/models.
func (s *Server) costModels(w http.ResponseWriter, r *http.Request) {
	out := map[string]proxy.KeyUsage{}
	if s.gw != nil {
		out = s.gw.ModelUsage()
	}
	writeJSON(w, 200, out)
}
