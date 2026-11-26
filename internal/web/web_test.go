package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAppShellForbidsProxyTransforms(t *testing.T) {
	w := httptest.NewRecorder()
	Handler().ServeHTTP(w, httptest.NewRequest("GET", "/today", nil))
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-transform") {
		t.Fatalf("Cache-Control = %q, want no-transform so proxies cannot inject scripts", cc)
	}
}
