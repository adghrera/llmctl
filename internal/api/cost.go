package api

import (
	"net/http"

	"llmctl/internal/cost"
)

// costStore lazily opens the cost tracker.
func (s *Server) costStore() *cost.Tracker {
	if s.cost == nil {
		s.cost = cost.NewTracker()
	}
	return s.cost
}

// costSummary handles GET /api/v1/cost.
func (s *Server) costSummary(w http.ResponseWriter, r *http.Request) {
	cs := s.costStore()
	in, out, total, cost := cs.Totals()
	writeJSON(w, 200, map[string]any{
		"input_tokens":  in,
		"output_tokens": out,
		"total_tokens":  total,
		"cost_usd":      cost,
	})
}

// costKeys handles GET /api/v1/cost/keys.
func (s *Server) costKeys(w http.ResponseWriter, r *http.Request) {
	cs := s.costStore()
	writeJSON(w, 200, cs.KeySnapshot())
}

// costModels handles GET /api/v1/cost/models.
func (s *Server) costModels(w http.ResponseWriter, r *http.Request) {
	cs := s.costStore()
	writeJSON(w, 200, cs.ModelSnapshot())
}
