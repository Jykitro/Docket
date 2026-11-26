package web

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetsAreGzippedWhenAccepted(t *testing.T) {
	js := strings.Repeat("console.log('plandillo');\n", 200)
	h := handler(fstest.MapFS{"assets/app-1.js": {Data: []byte(js)}, "icon.png": {Data: []byte("png")}})
	get := func(p, enc string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", p, nil)
		if enc != "" {
			r.Header.Set("Accept-Encoding", enc)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	w := get("/assets/app-1.js", "br, gzip")
	if w.Header().Get("Content-Encoding") != "gzip" || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("headers = %v, want gzip javascript", w.Header())
	}
	if w.Header().Get("Vary") != "Accept-Encoding" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("headers = %v, want Vary and immutable cache", w.Header())
	}
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(zr); string(body) != js {
		t.Fatalf("gunzipped body differs: %d bytes, want %d", len(body), len(js))
	}

	for _, enc := range []string{"", "gzip;q=0", "deflate"} {
		w := get("/assets/app-1.js", enc)
		if w.Header().Get("Content-Encoding") != "" || w.Body.String() != js || w.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatalf("Accept-Encoding %q: encoding %q, %d bytes, Vary %q; want plain body with Vary", enc, w.Header().Get("Content-Encoding"), w.Body.Len(), w.Header().Get("Vary"))
		}
	}
	if w := get("/icon.png", "gzip"); w.Header().Get("Content-Encoding") != "" || w.Body.String() != "png" {
		t.Fatalf("png got Content-Encoding %q, want it served as is", w.Header().Get("Content-Encoding"))
	}
}

func TestRootFilesRevalidateWithETag(t *testing.T) {
	svg := "<svg>" + strings.Repeat("<path d='M0 0h10v10z'/>", 50) + "</svg>"
	h := handler(fstest.MapFS{
		"favicon.ico": {Data: []byte("ico")}, "logo.svg": {Data: []byte(svg)},
		"index.html": {Data: []byte("<!doctype html><title>app</title>")}, "assets/app-1.js": {Data: []byte("x")},
	})
	get := func(p, enc, tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("Accept-Encoding", enc)
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	for _, c := range []struct{ path, enc string }{{"/favicon.ico", ""}, {"/logo.svg", ""}, {"/logo.svg", "gzip"}, {"/today", ""}} {
		first := get(c.path, c.enc, "")
		tag := first.Header().Get("ETag")
		if first.Code != 200 || tag == "" || !strings.HasPrefix(first.Header().Get("Cache-Control"), "no-cache") {
			t.Fatalf("%s (%q): code %d, ETag %q, Cache-Control %q; want 200 with ETag and no-cache", c.path, c.enc, first.Code, tag, first.Header().Get("Cache-Control"))
		}
		if again := get(c.path, c.enc, tag); again.Code != 304 || again.Body.Len() != 0 {
			t.Fatalf("%s (%q) with If-None-Match: code %d, %d bytes; want 304 without body", c.path, c.enc, again.Code, again.Body.Len())
		}
	}
	if plain, packed := get("/logo.svg", "", "").Header().Get("ETag"), get("/logo.svg", "gzip", "").Header().Get("ETag"); plain == packed {
		t.Fatalf("plain and gzip bodies share ETag %q", plain)
	}
	if w := get("/favicon.ico", "", `"stale"`); w.Code != 200 || w.Body.String() != "ico" {
		t.Fatalf("stale ETag: code %d, body %q; want the file", w.Code, w.Body.String())
	}
	if tag := get("/assets/app-1.js", "", "").Header().Get("ETag"); tag != "" {
		t.Fatalf("hashed asset got ETag %q; it is immutable and needs none", tag)
	}
}

func TestAppShellForbidsProxyTransforms(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/today", nil))
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-transform") {
		t.Fatalf("Cache-Control = %q, want no-transform so proxies cannot inject scripts", cc)
	}
}
