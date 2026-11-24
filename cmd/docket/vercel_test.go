package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDevelopmentOriginRequiresConfiguredPublicAuthority(t *testing.T) {
	handler := developmentOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Request-Host", r.Host) }), "http://localhost:3000")
	for _, tc := range []struct{ host, forwarded, want string }{
		{"127.0.0.1:59871", "localhost:3000", "localhost:3000"},
		{"127.0.0.1:59871", "localhost:4000", "127.0.0.1:59871"},
		{"outside.example", "localhost:3000", "outside.example"},
	} {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.Host = tc.host
		r.Header.Set("X-Vercel-Deployment-Url", tc.forwarded)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if got := w.Header().Get("Request-Host"); got != tc.want {
			t.Errorf("request host %q, want %q", got, tc.want)
		}
	}
}
