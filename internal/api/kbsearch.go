package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// searchAllKBs handles POST /api/v1/knowledge-bases/search — run one query
// across every knowledge base and return the merged top hits (F665).
// Body: {"q": "...", "limit": 10}. Each result carries kb_id and kb_name.
func (s *Server) searchAllKBs(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Query string `json:"q"`
		Limit int    `json:"limit"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeErr(w, 400, "q is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	results, err := ts.SearchAll(req.Query, req.Limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"query": req.Query, "results": results})
}
