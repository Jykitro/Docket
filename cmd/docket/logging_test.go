package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessLog(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/boom":
			slog.ErrorContext(r.Context(), "inner failure")
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/events":
			if _, ok := w.(http.Flusher); !ok {
				w.WriteHeader(http.StatusTeapot) // the stream would break
				return
			}
			_, _ = w.Write([]byte(": ping\n\n"))
		default:
			_, _ = w.Write([]byte("ok"))
		}
	})
	tests := []struct {
		name, path, requestID string
		wantLevel, wantPath   string
		wantStatus            int
		keepID                bool
	}{
		{"api call at info", "/api/projects?token=secret", "", "INFO", "/api/projects", 200, false},
		{"web app file at debug", "/assets/index.js", "", "DEBUG", "/assets/index.js", 200, false},
		{"server error at error", "/api/boom", "", "ERROR", "/api/boom", 500, false},
		{"stream keeps Flush", "/api/events", "", "INFO", "/api/events", 200, false},
		{"invite token hidden", "/api/auth/invite/abc123secret", "", "INFO", "/api/auth/invite/…", 200, false},
		{"proxy id kept", "/api/projects", "trace-42.a_b", "INFO", "/api/projects", 200, true},
		{"unsafe id replaced", "/api/projects", "bad id\nINFO forged", "INFO", "/api/projects", 200, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(newLogger(&buf, slog.LevelDebug, true))
			t.Cleanup(func() { slog.SetDefault(prev) })

			req := httptest.NewRequest("GET", tc.path, nil)
			if tc.requestID != "" {
				req.Header.Set("X-Request-ID", tc.requestID)
			}
			rec := httptest.NewRecorder()
			accessLog(handler).ServeHTTP(rec, req)

			id := rec.Header().Get("X-Request-ID")
			if id == "" || (tc.keepID && id != tc.requestID) || (!tc.keepID && id == tc.requestID) {
				t.Fatalf("X-Request-ID %q (sent %q)", id, tc.requestID)
			}
			var access map[string]any
			for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				var rec map[string]any
				if err := json.Unmarshal([]byte(line), &rec); err != nil {
					t.Fatalf("not JSON: %q", line)
				}
				if rec["request_id"] != id {
					t.Fatalf("%q lacks request_id %s: %v", rec["msg"], id, rec)
				}
				if rec["msg"] == "http request" {
					access = rec
				}
			}
			if access == nil {
				t.Fatalf("no access line in %s", buf.String())
			}
			if access["level"] != tc.wantLevel || access["path"] != tc.wantPath || access["status"] != float64(tc.wantStatus) {
				t.Fatalf("logged %v %v %v, want %s %s %d", access["level"], access["path"], access["status"], tc.wantLevel, tc.wantPath, tc.wantStatus)
			}
			if strings.Contains(buf.String(), "secret") {
				t.Fatalf("a secret reached the log: %s", buf.String())
			}
		})
	}
}
