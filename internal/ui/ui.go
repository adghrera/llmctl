// Package ui serves the embedded single-page management UI.
//
// The UI is a Vite+React+TS app built from frontend/ into dist/ (see
// frontend/vite.config.ts). The build output is embedded here, so the Go
// binary stays self-contained — no separate web server.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Handler serves the SPA. Static assets are served directly; any other path
// falls back to index.html so client-side routing (e.g. /chat) works on
// refresh and deep links.
func Handler() http.Handler {
	sub, _ := fs.Sub(assets, "dist")
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		// Serve the file if it exists; otherwise fall back to the SPA.
		if f, err := sub.Open(p); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := *r
		r2.URL.Path = "/"
		http.ServeFileFS(w, &r2, sub, "index.html")
	})
}
