package api

import (
	"io"
	"net/http"
	"strings"

	"llmctl/internal/files"
)

// fileStore lazily opens the files store.
func (s *Server) fileStore() (*files.Store, error) {
	if s.files == nil {
		st, err := files.Open()
		if err != nil {
			return nil, err
		}
		s.files = st
	}
	return s.files, nil
}

// listFiles handles GET /api/v1/files.
func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	ts, err := s.fileStore()
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

// uploadFile handles POST /api/v1/files (multipart/form-data).
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	ts, err := s.fileStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := r.ParseMultipartForm(files.MaxFileSize); err != nil {
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
	rec, err := ts.Create(header.Filename, contentType, content)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 201, rec)
}

// getFileContent handles GET /api/v1/files/{id}/content.
func (s *Server) getFileContent(w http.ResponseWriter, r *http.Request) {
	ts, err := s.fileStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	rec, err := ts.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, statusForFile(err), err.Error())
		return
	}
	content, err := rec.ReadContent()
	if err != nil {
		writeErr(w, 500, "failed to read file: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", rec.Entry.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+rec.Entry.Filename+`"`)
	w.WriteHeader(200)
	w.Write(content)
}

// deleteFile handles DELETE /api/v1/files/{id}.
func (s *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	ts, err := s.fileStore()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if err := ts.Delete(r.PathValue("id")); err != nil {
		writeErr(w, statusForFile(err), err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func statusForFile(err error) int {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "invalid file id") || strings.Contains(msg, "not found") || strings.Contains(msg, "no such file") {
		return 404
	}
	return 400
}
