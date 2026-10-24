package api

import (
	"net/http"

	"llmctl/internal/prompt"
)

// listPrompts handles GET /api/v1/prompts.
func (s *Server) listPrompts(w http.ResponseWriter, r *http.Request) {
	category := r.URL.Query().Get("category")
	var templates []prompt.Template
	if category != "" {
		templates = prompt.FilterByCategory(category)
	} else {
		templates = prompt.Templates
	}
	writeJSON(w, 200, templates)
}

// categoriesPrompt handles GET /api/v1/prompts/categories.
func (s *Server) categoriesPrompt(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, prompt.Categories())
}

// findPrompt handles GET /api/v1/prompts/{id}.
func (s *Server) findPrompt(w http.ResponseWriter, r *http.Request) {
	t := prompt.Find(r.PathValue("id"))
	if t == nil {
		writeErr(w, 404, "prompt template not found")
		return
	}
	writeJSON(w, 200, t)
}
