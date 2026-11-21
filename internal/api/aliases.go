package api

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"llmctl/internal/store"
)

// Model aliases (F065): stable gateway names mapped to model ids, so
// clients can keep using e.g. "chat" while the underlying model changes.

var aliasNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

type aliasView struct {
	Alias string `json:"alias"`
	Model string `json:"model"`
}

func (s *Server) listAliases(w http.ResponseWriter, r *http.Request) {
	st, _ := s.store.Load()
	out := []aliasView{}
	for a, m := range st.Aliases {
		out = append(out, aliasView{Alias: a, Model: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
	writeJSON(w, 200, out)
}

func (s *Server) createAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias string `json:"alias"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	alias := strings.ToLower(strings.TrimSpace(req.Alias))
	model := strings.TrimSpace(req.Model)
	if !aliasNameRe.MatchString(alias) {
		writeErr(w, 400, "alias must match "+aliasNameRe.String())
		return
	}
	if model == "" {
		writeErr(w, 400, "model is required")
		return
	}
	st, _ := s.store.Load()
	if _, ok := st.Models[model]; !ok {
		writeErr(w, 404, "unknown model "+model)
		return
	}
	if _, ok := st.Models[alias]; ok {
		writeErr(w, 409, "alias collides with an installed model id")
		return
	}
	if err := s.store.Update(func(st *store.State) error {
		if st.Aliases == nil {
			st.Aliases = map[string]string{}
		}
		st.Aliases[alias] = model
		return nil
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, aliasView{Alias: alias, Model: model})
}

func (s *Server) deleteAlias(w http.ResponseWriter, r *http.Request) {
	alias := strings.ToLower(r.PathValue("alias"))
	st, _ := s.store.Load()
	if _, ok := st.Aliases[alias]; !ok {
		writeErr(w, 404, "no alias "+alias)
		return
	}
	if err := s.store.Update(func(st *store.State) error {
		delete(st.Aliases, alias)
		return nil
	}); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
