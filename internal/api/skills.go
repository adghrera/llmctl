package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"llmctl/internal/skills"
)

// skillStore lazily opens the skill store.
func (s *Server) skillStore() (*skills.Store, error) {
	if s.skills == nil {
		st, err := skills.Open()
		if err != nil {
			return nil, err
		}
		s.skills = st
	}
	return s.skills, nil
}

// listSkills handles GET /api/v1/skills.
func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	ts, err := s.skillStore()
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

// createSkill handles POST /api/v1/skills.
// Body: {"short_name": "...", "name": "...", "description": "...",
//        "prompt_template": "...", "tags": [...]}
func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	ts, err := s.skillStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var m skills.Manifest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Create(m)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rec)
}

// getSkill handles GET /api/v1/skills/{id}.
func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	ts, err := s.skillStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForSkill(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// updateSkill handles PUT /api/v1/skills/{id}.
func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	ts, err := s.skillStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var m skills.Manifest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Update(r.PathValue("id"), m)
	if err != nil {
		writeErr(w, statusForSkill(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// deleteSkill handles DELETE /api/v1/skills/{id}.
func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	ts, err := s.skillStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, statusForSkill(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// setSkillEnabled handles POST /api/v1/skills/{id}/enabled.
// Body: {"enabled": true|false}
func (s *Server) setSkillEnabled(w http.ResponseWriter, r *http.Request) {
	ts, err := s.skillStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		writeErr(w, 400, `expected {"enabled": true|false}`)
		return
	}
	if err := ts.SetEnabled(r.PathValue("id"), *req.Enabled); err != nil {
		writeErr(w, statusForSkill(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func statusForSkill(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid skill id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}
