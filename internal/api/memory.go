package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"llmctl/internal/memory"
)

// memoryStore lazily opens the memory store.
func (s *Server) memoryStore() (*memory.Store, error) {
	if s.memory == nil {
		st, err := memory.Open()
		if err != nil {
			return nil, err
		}
		s.memory = st
	}
	return s.memory, nil
}

// listMemory handles GET /api/v1/memory.
func (s *Server) listMemory(w http.ResponseWriter, r *http.Request) {
	ts, err := s.memoryStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	list, err := ts.List()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, list)
}

// createMemory handles POST /api/v1/memory.
// Body: {"content": "...", "tags": [...], "source": "..."}
func (s *Server) createMemory(w http.ResponseWriter, r *http.Request) {
	ts, err := s.memoryStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags,omitempty"`
		Source  string   `json:"source,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Create(req.Content, req.Tags, req.Source)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rec)
}

// getMemory handles GET /api/v1/memory/{id}.
func (s *Server) getMemory(w http.ResponseWriter, r *http.Request) {
	ts, err := s.memoryStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForMemory(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// updateMemory handles PUT /api/v1/memory/{id}.
func (s *Server) updateMemory(w http.ResponseWriter, r *http.Request) {
	ts, err := s.memoryStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Content string   `json:"content"`
		Tags    []string `json:"tags,omitempty"`
		Source  string   `json:"source,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Update(r.PathValue("id"), req.Content, req.Tags, req.Source)
	if err != nil {
		writeErr(w, statusForMemory(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// deleteMemory handles DELETE /api/v1/memory/{id}.
func (s *Server) deleteMemory(w http.ResponseWriter, r *http.Request) {
	ts, err := s.memoryStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, statusForMemory(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// searchMemory handles GET /api/v1/memory/search?q=.
func (s *Server) searchMemory(w http.ResponseWriter, r *http.Request) {
	ts, err := s.memoryStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, 400, "q parameter required")
		return
	}
	limit := 10
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	results, err := ts.Search(q, limit)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, results)
}

func statusForMemory(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid memory id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}
