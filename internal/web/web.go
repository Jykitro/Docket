// Package web serves the built SPA embedded in the binary, falling back to index.html.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	files := http.FileServerFS(sub)
	// Existing PWA manifests and bookmarks can request the icons shipped before the rename.
	legacyIcons := map[string]string{
		"docket-icon-v2.svg": "/plandillo-icon.svg", "docket-192.png": "/plandillo-icon-192.png",
		"docket-512.png": "/plandillo-icon-512.png", "docket-maskable.png": "/plandillo-maskable.png",
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if target, ok := legacyIcons[p]; ok {
			http.Redirect(w, r, target, http.StatusPermanentRedirect)
			return
		}
		if st, err := fs.Stat(sub, p); p != "" && err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "assets/") { // vite file names are content-hashed
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "UI is not built: run `bun run build` in web/", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
