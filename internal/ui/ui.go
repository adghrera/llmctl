// Package ui serves the embedded single-page management UI.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html
var assets embed.FS

// Handler serves the SPA.
func Handler() http.Handler {
	sub, _ := fs.Sub(assets, ".")
	return http.FileServer(http.FS(sub))
}
