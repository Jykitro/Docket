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

// The production deployer rejects cycles even when vercel dev accepts the configuration.
func TestVercelServiceBindings(t *testing.T) {
	var config struct {
		Services map[string]struct {
			Bindings []struct{ Type, Service, Format, Env string }
		}
		Rewrites []struct{ Destination struct{ Service string } }
	}
	data, err := os.ReadFile("../../vercel.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"landing": "", "web": "", "app": "worker", "worker": "replies", "replies": ""}
	if len(config.Services) != len(expected) {
		t.Fatal("unexpected service graph")
	}
	for name, target := range expected {
		svc, ok := config.Services[name]
		if !ok {
			t.Fatalf("missing service %s", name)
		}
		if target == "" {
			if len(svc.Bindings) != 0 {
				t.Fatalf("%s must not declare an outgoing binding", name)
			}
			continue
		}
		if len(svc.Bindings) != 1 || svc.Bindings[0].Service != target || svc.Bindings[0].Type != "service" || svc.Bindings[0].Format != "url" {
			t.Fatalf("expected %s -> %s URL binding", name, target)
		}
	}
	for _, route := range config.Rewrites {
		if svc := route.Destination.Service; svc != "web" && svc != "app" && svc != "landing" {
			t.Fatalf("unexpected public route to %s", svc)
		}
	}
}
