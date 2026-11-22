package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"llmctl/internal/prompt"
	"llmctl/internal/store"
)

// promptOut is a template in API responses; custom entries carry custom=true (F306).
type promptOut struct {
	prompt.Template
	Custom bool `json:"custom"`
}

var promptIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// listPrompts handles GET /api/v1/prompts.
func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	var templates []prompt.Template
	if category != "" {
		templates = prompt.FilterByCategory(category)
	} else {
		templates = prompt.Templates
	}
	out := make([]promptOut, 0, len(templates))
	for _, t := range templates {
		out = append(out, promptOut{Template: t})
	}
	if st, err := s.store.Load(); err == nil {
		for _, c := range st.CustomPrompts {
			if category != "" && c.Category != category {
				continue
			}
			out = append(out, promptOut{Template: prompt.Template{
				ID: c.ID, Name: c.Name, Description: c.Description,
				Category: c.Category, Content: c.Content,
			}, Custom: true})
		}
	}
	writeJSON(w, 200, out)
}

// categoriesPrompt handles GET /api/v1/prompts/categories.
func (s *Server) categoriesPrompt(w http.ResponseWriter, r *http.Request) {
	cats := prompt.Categories()
	seen := map[string]bool{}
	for _, c := range cats {
		seen[c] = true
	}
	if st, err := s.store.Load(); err == nil {
		for _, c := range st.CustomPrompts {
			if c.Category != "" && !seen[c.Category] {
				seen[c.Category] = true
				cats = append(cats, c.Category)
			}
		}
	}
	sort.Strings(cats)
	writeJSON(w, 200, cats)
}

// findPrompt handles GET /api/v1/prompts/{id}.
func (s *Server) findPrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if t := prompt.Find(id); t != nil {
		writeJSON(w, 200, promptOut{Template: *t})
		return
	}
	if st, err := s.store.Load(); err == nil {
		if c, ok := st.CustomPrompts[id]; ok {
			writeJSON(w, 200, promptOut{Template: prompt.Template{
				ID: c.ID, Name: c.Name, Description: c.Description,
				Category: c.Category, Content: c.Content,
			}, Custom: true})
			return
		}
	}
	writeErr(w, 404, "prompt template not found")
}

// createPrompt handles POST /api/v1/prompts (F306).
func (s *Server) createPrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Content     string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.Name = strings.TrimSpace(req.Name)
	req.Content = strings.TrimSpace(req.Content)
	if !promptIDRe.MatchString(req.ID) {
		writeErr(w, 400, "id must match ^[a-z0-9][a-z0-9-]{0,40}$")
		return
	}
	if req.Name == "" || req.Content == "" {
		writeErr(w, 400, "name and content are required")
		return
	}
	if prompt.Find(req.ID) != nil {
		writeErr(w, 409, "id collides with a built-in prompt")
		return
	}
	rec := &store.CustomPrompt{
		ID:          req.ID,
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		Category:    strings.TrimSpace(req.Category),
		Content:     req.Content,
		CreatedAt:   time.Now().UTC(),
	}
	err := s.store.Update(func(st *store.State) error {
		if _, dup := st.CustomPrompts[req.ID]; dup {
			return fmt.Errorf("prompt %q already exists", req.ID)
		}
		if st.CustomPrompts == nil {
			st.CustomPrompts = map[string]*store.CustomPrompt{}
		}
		st.CustomPrompts[req.ID] = rec
		return nil
	})
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 201, promptOut{Template: prompt.Template{
		ID: rec.ID, Name: rec.Name, Description: rec.Description,
		Category: rec.Category, Content: rec.Content,
	}, Custom: true})
}

// updatePrompt handles PUT /api/v1/prompts/{id} (F308): replaces the editable
// fields of a custom template (name/category/description/content).
func (s *Server) updatePrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Name        *string `json:"name"`
		Category    *string `json:"category"`
		Description *string `json:"description"`
		Content     *string `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if prompt.Find(id) != nil {
		writeErr(w, 400, "built-in prompts cannot be edited")
		return
	}
	var updated *store.CustomPrompt
	err := s.store.Update(func(st *store.State) error {
		c, ok := st.CustomPrompts[id]
		if !ok {
			return fmt.Errorf("prompt %q not found", id)
		}
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			c.Name = strings.TrimSpace(*req.Name)
		}
		if req.Category != nil {
			c.Category = strings.TrimSpace(*req.Category)
		}
		if req.Description != nil {
			c.Description = strings.TrimSpace(*req.Description)
		}
		if req.Content != nil && strings.TrimSpace(*req.Content) != "" {
			c.Content = strings.TrimSpace(*req.Content)
		}
		c.UpdatedAt = time.Now().UTC()
		updated = c
		return nil
	})
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, promptOut{Template: prompt.Template{
		ID: updated.ID, Name: updated.Name, Description: updated.Description,
		Category: updated.Category, Content: updated.Content,
	}, Custom: true})
}

// deletePrompt handles DELETE /api/v1/prompts/{id} (F306).
func (s *Server) deletePrompt(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if prompt.Find(id) != nil {
		writeErr(w, 400, "built-in prompts cannot be deleted")
		return
	}
	err := s.store.Update(func(st *store.State) error {
		if _, ok := st.CustomPrompts[id]; !ok {
			return fmt.Errorf("prompt %q not found", id)
		}
		delete(st.CustomPrompts, id)
		return nil
	})
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
