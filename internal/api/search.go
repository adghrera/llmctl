package api

import (
	"net/http"
	"strconv"
	"strings"
)

// searchHandler serves GET /api/v1/search?q=...&max=N — keyless web search
// (DuckDuckGo primary, Bing RSS fallback). Used by the UI and by the agent
// loop (F022) as the web_search tool.
func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q required (?q=query)")
		return
	}
	max := 5
	if v := r.URL.Query().Get("max"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			max = n
		}
	}
	results, err := s.searcher.Search(r.Context(), q, max)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "search failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"query":   q,
		"results": results,
	})
}
