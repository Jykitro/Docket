package main

// Logging: one structured line per HTTP request and per MCP tool call, plus whatever the code logs while serving
// them, all carrying the same request_id. The id is also returned as X-Request-ID, so a user's report can be matched
// to the log. Request bodies, query strings, cookies and Authorization headers never reach the log.

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

type requestIDKey struct{}

// newLogger writes text (readable in `docker logs`) or JSON (for Loki, ELK and similar) at the given level.
func newLogger(w io.Writer, level slog.Level, json bool) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewTextHandler(w, opts)
	if json {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(requestIDHandler{h})
}

// requestIDHandler adds the request_id of the context to every record logged with a request's context
// (slog.ErrorContext(r.Context(), ...)), so errors deep in a handler line up with that request's access line.
type requestIDHandler struct{ slog.Handler }

func (h requestIDHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDHandler{h.Handler.WithAttrs(attrs)}
}

func (h requestIDHandler) WithGroup(name string) slog.Handler {
	return requestIDHandler{h.Handler.WithGroup(name)}
}

// accessLog logs each request when it ends. API calls log at info, server errors at error, and the web app's own
// files and the health check at debug, so the default level shows what the API is doing without the asset noise.
// The live-update stream logs once, when it closes, with how long it stayed open.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = rand.Text()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor) // keeps Flush for the event stream
		next.ServeHTTP(ww, r.WithContext(ctx))

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK // the handler wrote nothing
		}
		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case !isAPI(r.URL.Path):
			level = slog.LevelDebug
		}
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", redactPath(r.URL.Path)),
			slog.Int("status", status),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Float64("ms", float64(time.Since(start).Microseconds())/1000),
			slog.String("proto", r.Proto),
			slog.String("ip", peerIP(r.RemoteAddr)),
		}
		// Set by the reverse proxy; a client can forge it, so it sits beside the peer address instead of replacing it.
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			attrs = append(attrs, slog.String("forwarded_for", clip(f, 200)))
		}
		if ua := r.UserAgent(); ua != "" {
			attrs = append(attrs, slog.String("user_agent", clip(ua, 160)))
		}
		slog.LogAttrs(ctx, level, "http request", attrs...)
	})
}

// isAPI reports whether path is handled by the server rather than being a file of the web app.
func isAPI(path string) bool {
	for _, p := range []string{"/api/", "/mcp", "/oauth/", "/.well-known/"} {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// redactPath hides invitation tokens: new links carry them in the fragment, but links sent before still reach the
// server once as /invite/<token>, and an old open tab may still post to /api/auth/invite/<token> (answered with 404).
func redactPath(path string) string {
	if i := strings.Index(path, "/invite/"); i >= 0 {
		return path[:i] + "/invite/…"
	}
	return path
}

// validRequestID accepts an id from a proxy only when it is short and plain, so it cannot inject into the log.
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	const allowed = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_."
	for _, c := range id {
		if !strings.ContainsRune(allowed, c) {
			return false
		}
	}
	return true
}

func peerIP(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
