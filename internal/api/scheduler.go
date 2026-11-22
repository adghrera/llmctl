package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"llmctl/internal/events"
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

// schedView is a scheduler record enriched with its most recent run from the
// in-memory ring (status/detail), for the list endpoint (F184).
type schedView struct {
	scheduler.Record
	LastRun *scheduler.Run `json:"lastRun,omitempty"`
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
	out := make([]schedView, 0, len(list))
	for _, rec := range list {
		out = append(out, schedView{Record: rec, LastRun: scheduler.LastRun(rec.ID)})
	}
	writeJSON(w, 200, out)
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

// setScheduleTags handles PATCH /api/v1/scheduler/{id}/tags — replace a
// task's free-form tag list, the scheduler twin of the other tag endpoints
// (F681). Tags are trimmed, deduped and capped (20 tags, 40 chars each).
func (s *Server) setScheduleTags(w http.ResponseWriter, r *http.Request) {
	ts, err := s.schedStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body")
		return
	}
	seen := map[string]bool{}
	tags := []string{}
	for _, t := range req.Tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		if len(t) > 40 {
			writeErr(w, 400, "tag too long (max 40 chars): "+t)
			return
		}
		seen[t] = true
		tags = append(tags, t)
	}
	if len(tags) > 20 {
		writeErr(w, 400, "too many tags (max 20)")
		return
	}
	if len(tags) == 0 {
		tags = nil
	}
	if err := ts.SetTags(r.PathValue("id"), tags); err != nil {
		writeErr(w, statusForSched(err), err.Error())
		return
	}
	out := tags
	if out == nil {
		out = []string{}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "tags": out})
}

// schedulerRuns handles GET /api/v1/scheduler/runs — recent task executions,
// newest first (F145).
func (s *Server) schedulerRuns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, scheduler.Runs(50))
}

// runScheduleNow handles POST /api/v1/scheduler/{id}/run — execute a task// immediately (async), regardless of its cron schedule (F084).
func (s *Server) runScheduleNow(w http.ResponseWriter, r *http.Request) {
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
	if s.schedRun == nil {
		writeErr(w, 503, "scheduler runner unavailable")
		return
	}
	go s.schedRun(*rec)
	writeJSON(w, 202, map[string]any{"ok": true, "started": rec.ID})
}

// SetScheduleRunner wires the daemon's task executor so the API can trigger
// immediate runs (F084).
func (s *Server) SetScheduleRunner(fn func(scheduler.Record)) { s.schedRun = fn }

// SetEvents installs the daemon's event ring for GET /api/v1/events (F099).
func (s *Server) SetEvents(r *events.Ring) { s.events = r }

func statusForSched(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid schedule id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}
