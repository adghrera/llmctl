package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"llmctl/internal/scheduler"
)

// schedStore lazily opens the scheduler store.
func (s *Server) schedStore() (*scheduler.Store, error) {
	if s.sched == nil {
		st, err := scheduler.Open()
		if err != nil {
			return nil, err
		}
		s.sched = st
	}
	return s.sched, nil
}

// listSchedules handles GET /api/v1/scheduler.
func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
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

// createSchedule handles POST /api/v1/scheduler.
// Body: {"name": "...", "cron_expr": "...", "model": "...", "prompt": "..."}
func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var spec scheduler.Spec
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&spec); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Create(spec)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rec)
}

// getSchedule handles GET /api/v1/scheduler/{id}.
func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForSched(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// updateSchedule handles PUT /api/v1/scheduler/{id}.
func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var spec scheduler.Spec
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&spec); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Update(r.PathValue("id"), spec)
	if err != nil {
		writeErr(w, statusForSched(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// deleteSchedule handles DELETE /api/v1/scheduler/{id}.
func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, statusForSched(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// setScheduleEnabled handles POST /api/v1/scheduler/{id}/enabled.
// Body: {"enabled": true|false}
func (s *Server) setScheduleEnabled(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
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
		writeErr(w, statusForSched(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func statusForSched(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid schedule id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}
