package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"llmctl/internal/backup"
	"llmctl/internal/paths"
)

// Scheduled-backup snapshots (created by the "backup" scheduler kind, F077).

var backupNameRe = regexp.MustCompile(`^llmctl-[0-9]{8}-[0-9]{6}\.tar\.gz$`)

type backupView struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Created string `json:"created"`
}

// listBackups handles GET /api/v1/backups — snapshots in $LLMCTL_HOME/backups.
func (s *Server) listBackups(w http.ResponseWriter, r *http.Request) {
	out := []backupView{}
	matches, _ := filepath.Glob(filepath.Join(paths.BackupsDir(), "llmctl-*.tar.gz"))
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			continue
		}
		out = append(out, backupView{Name: filepath.Base(m), Size: fi.Size(), Created: fi.ModTime().UTC().Format(time.RFC3339)})
	}
	writeJSON(w, 200, out)
}

// createBackupNow handles POST /api/v1/backups — write a snapshot on demand
// (same pruning as scheduled backups, keep defaults to 7). F088.
func (s *Server) createBackupNow(w http.ResponseWriter, r *http.Request) {
	name, size, err := backup.CreateSnapshot(paths.BackupsDir(), 7)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, backupView{
		Name: name, Size: size,
		Created: time.Now().UTC().Format(time.RFC3339),
	})
}

// backupPath resolves and validates a snapshot name inside the backups dir.
func backupPath(name string) (string, error) {
	if !backupNameRe.MatchString(name) {
		return "", fmt.Errorf("invalid backup name: %q", name)
	}
	return filepath.Join(paths.BackupsDir(), name), nil
}

// downloadBackupFile handles GET /api/v1/backups/{name}.
func (s *Server) downloadBackupFile(w http.ResponseWriter, r *http.Request) {
	path, err := backupPath(r.PathValue("name"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if _, err := os.Stat(path); err != nil {
		writeErr(w, 404, "no such backup")
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(path)))
	http.ServeFile(w, r, path)
}

// restoreBackupByName handles POST /api/v1/backups/{name}/restore — import a
// stored snapshot (e.g. from a scheduled backup) back into the state dir (F175).
func (s *Server) restoreBackupByName(w http.ResponseWriter, r *http.Request) {
	path, err := backupPath(r.PathValue("name"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, 404, "no such backup")
		return
	}
	defer f.Close()
	n, err := backup.Restore(f)
	if err != nil {
		writeErr(w, 400, "restore failed: "+err.Error())
		return
	}
	s.logger.Info("restored stored backup", "name", filepath.Base(path), "files", n)
	writeJSON(w, 200, map[string]any{"ok": true, "files": n})
}

// deleteBackupFile handles DELETE /api/v1/backups/{name}.
func (s *Server) deleteBackupFile(w http.ResponseWriter, r *http.Request) {
	path, err := backupPath(r.PathValue("name"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := os.Remove(path); err != nil {
		writeErr(w, 404, "no such backup")
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
