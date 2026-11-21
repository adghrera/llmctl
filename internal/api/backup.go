package api

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"net/http"
	"time"

	"llmctl/internal/backup"
)

// downloadBackup handles GET /api/v1/backup — streams a tar.gz of the state
// directory (config, chats, knowledge, memory, skills, tools, scheduler,
// plugins; excludes binaries/models/logs) as a file download.
func (s *Server) downloadBackup(w http.ResponseWriter, r *http.Request) {
	name := "llmctl-backup-" + time.Now().UTC().Format("20060102-150405") + ".tar.gz"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if err := backup.Create(w); err != nil {
		// Headers already sent; just log.
		s.logger.Warn("backup create failed", "err", err)
	}
}

// restoreBackup handles POST /api/v1/restore (multipart "file" field with a
// tar.gz produced by the backup endpoint). Extracts into the state directory.
func (s *Server) restoreBackup(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, 400, "failed to parse multipart form: "+err.Error())
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file field required (tar.gz backup): "+err.Error())
		return
	}
	defer file.Close()
	// Validate it looks like a gzip/tar before touching the state dir.
	gz, err := gzip.NewReader(io.LimitReader(file, 1<<20))
	if err != nil {
		writeErr(w, 400, "not a gzip archive: "+err.Error())
		return
	}
	if _, err := tar.NewReader(gz).Next(); err != nil && err != io.EOF {
		writeErr(w, 400, "not a tar archive: "+err.Error())
		return
	}
	gz.Close()
	// Rewind and restore for real.
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		writeErr(w, 500, "rewind: "+err.Error())
		return
	}
	n, err := backup.Restore(file)
	if err != nil {
		writeErr(w, 400, "restore failed: "+err.Error())
		return
	}
	s.logger.Info("restored backup", "files", n)
	writeJSON(w, 200, map[string]any{"ok": true, "files": n})
}
