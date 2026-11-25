package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestVercelDeploysOnlyTheStaticLanding(t *testing.T) {
	var config struct {
		Services                                     map[string]json.RawMessage
		Rewrites                                     []json.RawMessage
		BuildCommand, OutputDirectory, IgnoreCommand string
	}
	data, err := os.ReadFile("../../vercel.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Services) != 0 || len(config.Rewrites) != 0 {
		t.Fatalf("expected a plain static deployment without services or rewrites: %s", data)
	}
	if config.BuildCommand != "make -C landing build" || config.OutputDirectory != "landing/dist" {
		t.Fatalf("unexpected landing build settings: %q %q", config.BuildCommand, config.OutputDirectory)
	}
	// Root-relative pathspecs keep the skip rule right whatever directory Vercel runs it from.
	if !strings.Contains(config.IgnoreCommand, "-- :/landing ") {
		t.Fatalf("landing changes must still trigger a build: %q", config.IgnoreCommand)
	}
	ignore, err := os.ReadFile("../../.vercelignore")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(ignore), "\n") {
		if strings.TrimSpace(line) == ".git/" || strings.TrimSpace(line) == ".git" {
			t.Fatal(".vercelignore must keep .git: without it the ignoreCommand cannot diff and every push builds")
		}
	}
}
