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
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if st, err := fs.Stat(sub, p); p != "" && err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "assets/") { // vite file names are content-hashed
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "UI is not built: run `npm run build` in web/", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
