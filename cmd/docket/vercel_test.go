package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestVercelFrontendRoutes(t *testing.T) {
	var config struct {
		Services map[string]struct {
			Root, BuildCommand, OutputDirectory string
			Runtime, Entrypoint                 string
			Rewrites                            []json.RawMessage
		}
		Rewrites []struct {
			Source       string
			Has, Missing []json.RawMessage
			Destination  struct{ Service string }
		}
	}
	data, err := os.ReadFile("../../vercel.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	landing, ok := config.Services["landing"]
	if !ok || len(config.Services) != 1 {
		t.Fatalf("expected only the landing service, got %v", config.Services)
	}
	if landing.Root != "landing" || landing.BuildCommand != "make build" || landing.OutputDirectory != "dist" {
		t.Fatalf("unexpected landing build settings: %+v", landing)
	}
	if landing.Runtime != "" || landing.Entrypoint != "" || len(landing.Rewrites) != 0 {
		t.Fatalf("expected a static landing without backend routes: %+v", landing)
	}
	if len(config.Rewrites) != 1 {
		t.Fatalf("expected one landing route, got %d", len(config.Rewrites))
	}
	route := config.Rewrites[0]
	if route.Source != "/(.*)" || route.Destination.Service != "landing" || len(route.Has) != 0 || len(route.Missing) != 0 {
		t.Fatalf("expected every request to route to the landing: %+v", route)
	}
}
