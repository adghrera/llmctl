package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"llmctl/internal/extract"
	"llmctl/internal/rag"
)

// ragStore lazily opens the rag store.
func (s *Server) ragStore() (*rag.Store, error) {
	if s.rag == nil {
		st, err := rag.Open()
		if err != nil {
			return nil, err
		}
		s.rag = st
	}
	return s.rag, nil
}

// listKBs handles GET /api/v1/knowledge-bases.
func (s *Server) listKBs(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
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

// createKB handles POST /api/v1/knowledge-bases.
// Body: {"name": "...", "description": "..."}
func (s *Server) createKB(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"` // free-form labels at creation (F702)
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Create(req.Name, req.Description, cleanTags(req.Tags))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rec)
}

// getKB handles GET /api/v1/knowledge-bases/{id}.
func (s *Server) getKB(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// updateKB handles PUT /api/v1/knowledge-bases/{id}.
func (s *Server) updateKB(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	rec, err := ts.Update(r.PathValue("id"), req.Name, req.Description)
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

// setKBTags handles PATCH /api/v1/knowledge-bases/{id}/tags — replace a
// KB's free-form tag list, the KB twin of model tags (F664). Tags are
// trimmed, deduped and capped (20 tags, 40 chars each).
func (s *Server) setKBTags(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
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
	rec, err := ts.SetTags(r.PathValue("id"), tags)
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "tags": rec.KB.Tags})
}

// deleteKB handles DELETE /api/v1/knowledge-bases/{id}.
func (s *Server) deleteKB(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// listKBDocs handles GET /api/v1/knowledge-bases/{id}/documents.
func (s *Server) listKBDocs(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, rec.Docs)
}

// uploadKBDoc handles POST /api/v1/knowledge-bases/{id}/documents (multipart).
func (s *Server) uploadKBDoc(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := r.ParseMultipartForm(rag.MaxDocSize); err != nil {
		writeErr(w, 400, "failed to parse multipart form: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file field required: "+err.Error())
		return
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, 500, "failed to read file: "+err.Error())
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(content)
	}
	// PDF/DOCX: store the extracted text so RAG can index it (F053).
	if extract.Supported(header.Filename) {
		text, err := extract.Text(header.Filename, content)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		content = []byte(text)
		contentType = "text/plain"
	}
	doc, err := ts.AddDoc(r.PathValue("id"), header.Filename, contentType, content)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, doc)
}

// getKBDocContent handles GET /api/v1/knowledge-bases/{kb_id}/documents/{doc_id}/content.
func (s *Server) getKBDocContent(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	doc, err := ts.GetDoc(r.PathValue("id"), r.PathValue("doc_id"))
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", `attachment; filename="`+doc.Filename+`"`)
	w.WriteHeader(200)
	w.Write([]byte(doc.Content))
}

// deleteKBDoc handles DELETE /api/v1/knowledge-bases/{kb_id}/documents/{doc_id}.
func (s *Server) deleteKBDoc(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.DeleteDoc(r.PathValue("id"), r.PathValue("doc_id")); err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// searchKBDocs handles POST /api/v1/knowledge-bases/{id}/search.
// Body: {"q": "...", "limit": 10}
func (s *Server) searchKBDocs(w http.ResponseWriter, r *http.Request) {
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
	if req.Query == "" {
		writeErr(w, 400, "q is required")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	results, err := ts.Search(r.PathValue("id"), req.Query, req.Limit)
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, results)
}

// setKBEmbed handles PUT /api/v1/knowledge-bases/{id}/embed.
// Body: {"url": "...", "model": "...", "apiKey": "..."} — empty url+model
// clears the embedding config (search falls back to TF-IDF). The url may be
// either the full embeddings endpoint or a base like http://host:port/v1.
func (s *Server) setKBEmbed(w http.ResponseWriter, r *http.Request) {
	ts, err := s.ragStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var req struct {
		URL    string `json:"url"`
		Model  string `json:"model"`
		APIKey string `json:"apiKey"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON: "+err.Error())
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	req.Model = strings.TrimSpace(req.Model)
	var cfg *rag.EmbedConfig
	if req.URL != "" || req.Model != "" {
		if req.URL != "" && !strings.HasSuffix(req.URL, "/embeddings") {
			req.URL = strings.TrimSuffix(req.URL, "/") + "/embeddings"
		}
		cfg = &rag.EmbedConfig{URL: req.URL, Model: req.Model, APIKey: req.APIKey}
	}
	rec, err := ts.SetEmbed(r.PathValue("id"), cfg)
	if err != nil {
		writeErr(w, statusForKB(err), err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

func statusForKB(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid kb id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}
