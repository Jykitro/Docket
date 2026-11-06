package selfhost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"docket/internal/auth"
	"docket/internal/httpapi"
)

const (
	cookieName = "docket_session"
	cookieAge  = 30 * 24 * 60 * 60 // seconds; matches the auth session TTL
)

type Options struct {
	Auth         *auth.DB
	WS           *Workspaces
	Web          http.Handler
	AllowedHosts []string
	SetupCode    string
}

type handler struct {
	Options
	limiter *auth.Limiter
}

// Handler serves the multi-user server: auth routes, then each user's own httpapi.Router behind their session
// (or, for /mcp, behind their MCP token).
func Handler(o Options) http.Handler {
	h := &handler{Options: o, limiter: auth.NewLimiter(5, 10*time.Minute)}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer, httpapi.HostGuard(o.AllowedHosts))
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	r.Get("/api/auth/me", h.me)
	r.Post("/api/auth/login", h.login)
	r.Post("/api/auth/logout", h.logout)
	r.Handle("/api/*", http.HandlerFunc(h.api))
	r.Handle("/mcp", http.HandlerFunc(h.mcp))
	r.Handle("/*", o.Web)
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

func internalError(w http.ResponseWriter, err error) {
	slog.Error("selfhost", "err", err)
	writeError(w, 500, "internal", "internal error")
}

// session returns the cookie's user; ok is false when there is no valid session (err is then a real failure).
func (h *handler) session(r *http.Request) (u auth.User, ok bool, err error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return u, false, nil
	}
	u, err = h.Auth.UserBySession(c.Value)
	if errors.Is(err, auth.ErrNotFound) {
		return u, false, nil
	}
	return u, err == nil, err
}

func (h *handler) me(w http.ResponseWriter, r *http.Request) {
	u, ok, err := h.session(r)
	if err != nil {
		internalError(w, err)
		return
	}
	if ok {
		writeJSON(w, 200, map[string]any{"mode": "selfhost", "user": u})
		return
	}
	n, err := h.Auth.UserCount()
	if err != nil {
		internalError(w, err)
		return
	}
	if n == 0 {
		writeJSON(w, 200, map[string]any{"mode": "selfhost", "setup": true})
		return
	}
	writeError(w, 401, "unauthorized", "not logged in")
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, 400, "validation", "invalid JSON body")
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	key := host + "|" + strings.ToLower(strings.TrimSpace(in.Username))
	if h.limiter.Blocked(key) {
		writeError(w, 429, "rate_limited", "too many failed attempts, try again later")
		return
	}
	u, err := h.Auth.Authenticate(in.Username, in.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		h.limiter.Fail(key)
		writeError(w, 401, "invalid_credentials", "invalid username or password")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	h.limiter.Reset(key)
	token, err := h.Auth.NewSession(u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	setCookie(w, r, token, cookieAge)
	writeJSON(w, 200, u)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := h.Auth.DeleteSession(c.Value); err != nil {
			internalError(w, err)
			return
		}
	}
	setCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
}

// api hands any other /api/* request to the session user's own router.
func (h *handler) api(w http.ResponseWriter, r *http.Request) {
	u, ok, err := h.session(r)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok {
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	ws, err := h.WS.Open(r.Context(), u) // returns the already-open workspace when there is one
	if err != nil {
		internalError(w, err)
		return
	}
	forward(ws.Router, w, r)
}

// mcp is both the MCP endpoint and a screen of the web app: a browser navigation gets the app. MCP clients are
// routed to the open workspace whose token matches; that workspace's router checks the token again.
func (h *handler) mcp(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
		h.Web.ServeHTTP(w, r)
		return
	}
	got := []byte(r.Header.Get("Authorization"))
	var match *Workspace
	for _, ws := range h.WS.All() { // compare against every token so timing does not reveal which one matched
		token, err := ws.Svc.MCPToken(r.Context())
		if err == nil && token != "" && subtle.ConstantTimeCompare(got, []byte("Bearer "+token)) == 1 {
			match = ws
		}
	}
	if match == nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	forward(match.Router, w, r)
}

// forward drops this mux's chi route context so the user's router routes the request from scratch.
func forward(next http.Handler, w http.ResponseWriter, r *http.Request) {
	next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, nil)))
}
