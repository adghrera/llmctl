package api

import (
	"net/http"
	"os"
	"path/filepath"

	"llmctl/internal/disk"
	"llmctl/internal/paths"
)

// diskUsage handles GET /api/v1/disk — bytes used by downloaded models and
// free space on the filesystem holding the llmctl home dir (F071).
func (s *Server) diskUsage(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Load()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	var used int64
	count := 0
	for _, m := range st.Models {
		if m.Path == "" {
			continue
		}
		fi, err := os.Stat(m.Path)
		if err != nil {
			continue // file gone (deleted out of band)
		}
		if fi.IsDir() {
			_ = filepath.WalkDir(m.Path, func(_ string, d os.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					if info, e := d.Info(); e == nil {
						used += info.Size()
					}
				}
				return nil
			})
		} else {
			used += fi.Size()
		}
		count++
	}
	home := paths.Home()
	sp, ferr := disk.Free(home)
	out := map[string]any{
		"dir":         home,
		"modelsBytes": used,
		"modelsCount": count,
	}
	if ferr == nil {
		out["freeBytes"] = sp.FreeBytes
		out["totalBytes"] = sp.TotalBytes
	}
	writeJSON(w, 200, out)
}
