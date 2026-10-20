package api

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"llmctl/internal/store"
)

// Instance presets: named save/restore of a start configuration
// (model + backend + port + vars). Starting a preset reuses the normal
// start flow, so per-start vars override the preset's.

func (s *Server) listPresets(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := make([]store.PresetSpec, 0, len(st.Presets))
	for _, p := range st.Presets {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, 200, out)
}

// savePreset creates or replaces a preset. Body: PresetSpec JSON with
// numeric vars values accepted (coerced to strings).
func (s *Server) savePreset(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Name      string `json:"name"`
		Instance  string `json:"instance"`
		ModelID   string `json:"modelId"`
		BackendID string `json:"backendId"`
		Port      int    `json:"port"`
		Vars      any    `json:"vars"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1 << 20)).Decode(&raw); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	vars, verr := coerceVars(raw.Vars)
	if verr != nil {
		writeErr(w, 400, "invalid vars: "+verr.Error())
		return
	}
	name := strings.TrimSpace(raw.Name)
	if name == "" {
		writeErr(w, 400, "preset name required")
		return
	}
	if len(name) > 64 || strings.ContainsAny(name, `/\:*?"<>|`) {
		writeErr(w, 400, "preset name must be 1-64 chars without path characters")
		return
	}
	if strings.TrimSpace(raw.ModelID) == "" {
		writeErr(w, 400, "modelId required")
		return
	}
	st, _ := s.store.Load()
	if _, ok := st.Models[raw.ModelID]; !ok {
		writeErr(w, 404, "unknown model: "+raw.ModelID)
		return
	}
	p := &store.PresetSpec{Name: name, Instance: raw.Instance, ModelID: raw.ModelID, BackendID: raw.BackendID, Port: raw.Port, Vars: vars}
	if err := s.store.Update(func(st *store.State) error {
		if st.Presets == nil {
			st.Presets = map[string]*store.PresetSpec{}
		}
		st.Presets[name] = p
		return nil
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) deletePreset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, _ := s.store.Load()
	if _, ok := st.Presets[name]; !ok {
		writeErr(w, 404, "unknown preset: " + name)
		return
	}
	if err := s.store.Update(func(st *store.State) error {
		delete(st.Presets, name)
		return nil
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// startPreset starts an instance from a saved preset. Optional body may
// override port and vars for this start only.
func (s *Server) startPreset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, _ := s.store.Load()
	p, ok := st.Presets[name]
	if !ok {
		writeErr(w, 404, "unknown preset: "+name)
		return
	}
	// Optional per-start overrides.
	var override struct {
		Port *int            `json:"port"`
		Vars map[string]string `json:"vars"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1 << 20)).Decode(&override) // body optional
	}
	port := p.Port
	if override.Port != nil {
		port = *override.Port
	}
	// vars: preset first, per-start wins
	vars := map[string]string{}
	for k, v := range p.Vars {
		vars[k] = v
	}
	for k, v := range override.Vars {
		vars[k] = v
	}
	if len(vars) == 0 {
		vars = nil
	}
	id := p.Instance
	if id == "" {
		id = p.ModelID
	}
	// Reuse the shared start path (id derivation for replicas included).
	req2 := startInstanceReq{Instance: id, Model: p.ModelID, Backend: p.BackendID, Port: port}
	s.startInstanceInternal(w, r, req2, vars, id)
}
