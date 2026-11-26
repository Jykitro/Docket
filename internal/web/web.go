// Package web serves the built SPA embedded in the binary, falling back to index.html.
package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed all:dist
var dist embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(dist, "dist")
	return handler(sub)
}

func handler(sub fs.FS) http.Handler {
	files := http.FileServerFS(sub)
	gz := precompress(sub)
	tags := etags(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if st, err := fs.Stat(sub, p); p != "" && err == nil && !st.IsDir() {
			if strings.HasPrefix(p, "assets/") { // vite file names are content-hashed
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if tag, ok := tags[p]; ok { // stable names (icons, manifest, sw.js): revalidate, then 304
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("ETag", tag)
			}
			if packed, ok := gz[p]; ok {
				w.Header().Add("Vary", "Accept-Encoding")
				if acceptsGzip(r.Header.Get("Accept-Encoding")) {
					if tag, ok := tags[p]; ok { // the gzip body is another representation, so it needs its own tag
						w.Header().Set("ETag", strings.TrimSuffix(tag, `"`)+`-gz"`)
					}
					w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(p)))
					w.Header().Set("Content-Encoding", "gzip")
					http.ServeContent(w, r, p, time.Time{}, bytes.NewReader(packed))
					return
				}
			}
			files.ServeHTTP(w, r)
			return
		}
		// no-transform stops proxies such as Cloudflare from injecting scripts (Web Analytics) that the CSP blocks.
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		index, err := fs.ReadFile(sub, "index.html")
		if err != nil {
			http.Error(w, "UI is not built: run `bun run build` in web/", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", tags["index.html"])
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}

// etags gives every file outside assets/ a content hash. embed.FS has no modification times,
// so without it the browser has nothing to revalidate with and downloads these files on every load.
func etags(sub fs.FS) map[string]string {
	out := map[string]string{}
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(p, "assets/") {
			return nil
		}
		if raw, err := fs.ReadFile(sub, p); err == nil {
			sum := sha256.Sum256(raw)
			out[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return nil
	})
	return out
}

// precompress gzips the text files once at startup, so a server without a reverse proxy
// does not send the bundle uncompressed. Fonts and images are compressed already.
func precompress(sub fs.FS) map[string][]byte {
	out := map[string][]byte{}
	fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
		switch path.Ext(p) {
		case ".js", ".css", ".svg", ".html":
		default:
			return nil
		}
		raw, err := fs.ReadFile(sub, p)
		if err != nil {
			return nil
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(raw)
		zw.Close()
		if buf.Len() < len(raw) {
			out[p] = buf.Bytes()
		}
		return nil
	})
	return out
}

// acceptsGzip reports whether an Accept-Encoding header allows gzip; "gzip;q=0" refuses it.
func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		if q, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			v, err := strconv.ParseFloat(q, 64)
			return err == nil && v > 0
		}
		return true
	}
	return false
}
