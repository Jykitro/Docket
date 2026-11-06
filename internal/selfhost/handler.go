package selfhost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
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
	streams streams
}

// streams holds the cancel funcs of in-flight forwarded requests by session token hash, so ending a session
// ends its open SSE streams too.
type streams struct {
	mu sync.Mutex
	m  map[string]map[*context.CancelFunc]struct{}
}

// track returns a context cancelled by end(hash); done releases the entry and must run when the request ends.
func (s *streams) track(parent context.Context, hash string) (ctx context.Context, done func()) {
	ctx, cancel := context.WithCancel(parent)
	key := &cancel
	s.mu.Lock()
	if s.m == nil {
		s.m = map[string]map[*context.CancelFunc]struct{}{}
	}
	if s.m[hash] == nil {
		s.m[hash] = map[*context.CancelFunc]struct{}{}
	}
	s.m[hash][key] = struct{}{}
	s.mu.Unlock()
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.m[hash], key)
		if len(s.m[hash]) == 0 {
			delete(s.m, hash)
		}
		s.mu.Unlock()
	}
}

// end cancels every tracked request of the sessions with these token hashes.
func (s *streams) end(hashes ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range hashes {
		for cancel := range s.m[h] {
			(*cancel)()
		}
		delete(s.m, h)
	}
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
	r.Post("/api/auth/setup", h.setup)
	r.Post("/api/auth/invite/{token}", h.acceptInvite)
	r.Put("/api/auth/password", h.authed(h.changePassword))
	// Registered explicitly so no /api/admin path ever reaches a user's own router.
	r.Get("/api/admin/users", h.admin(h.listUsers))
	r.Delete("/api/admin/users/{id}", h.admin(h.deleteUser))
	r.Get("/api/admin/invites", h.admin(h.listInvites))
	r.Post("/api/admin/invites", h.admin(h.createInvite))
	r.Delete("/api/admin/invites/{id}", h.admin(h.deleteInvite))
	adminNotFound := h.admin(func(w http.ResponseWriter, _ *http.Request, _ auth.User) {
		writeError(w, 404, "not_found", "no such endpoint")
	})
	r.Handle("/api/admin", adminNotFound)
	r.Handle("/api/admin/*", adminNotFound)
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
	name := strings.ToLower(strings.TrimSpace(in.Username))
	if len(name) > 32 { // no valid username is longer; kept out of the limiter so junk names cannot grow its map
		writeError(w, 401, "invalid_credentials", "invalid username or password")
		return
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	key := host + "|" + name
	if !h.limiter.Attempt(key) { // counted before the password check, so parallel guesses cannot outrun it
		writeError(w, 429, "rate_limited", "too many failed attempts, try again later")
		return
	}
	u, err := h.Auth.Authenticate(in.Username, in.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
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

// decode reads a JSON body of at most 1 MiB; on failure it has already written the 400.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeError(w, 400, "validation", "invalid JSON body")
		return false
	}
	return true
}

// authed runs next for a request with a valid session.
func (h *handler) authed(next func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, ok, err := h.session(r)
		if err != nil {
			internalError(w, err)
			return
		}
		if !ok {
			writeError(w, 401, "unauthorized", "not logged in")
			return
		}
		next(w, r, u)
	}
}

// admin is authed plus an admin check.
func (h *handler) admin(next func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc {
	return h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		if !u.IsAdmin {
			writeError(w, 403, "forbidden", "admin only")
			return
		}
		next(w, r, u)
	})
}

// startSession opens the new user's workspace, logs them in and answers with the user.
func (h *handler) startSession(w http.ResponseWriter, r *http.Request, u auth.User) {
	if _, err := h.WS.Open(r.Context(), u); err != nil {
		internalError(w, err)
		return
	}
	token, err := h.Auth.NewSession(u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	setCookie(w, r, token, cookieAge)
	writeJSON(w, 200, u)
}

// credentialError answers a failed setup, invite or password change.
func credentialError(w http.ResponseWriter, err error) {
	var ve auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrInvalidInvite):
		writeError(w, 400, "validation", "invalid or expired invite") // one message for unknown, used and expired
	case errors.Is(err, auth.ErrTaken):
		writeError(w, 400, "validation", "username is taken")
	case errors.As(err, &ve):
		writeError(w, 400, "validation", ve.Error())
	default:
		internalError(w, err)
	}
}

func (h *handler) setup(w http.ResponseWriter, r *http.Request) {
	n, err := h.Auth.UserCount()
	if err != nil {
		internalError(w, err)
		return
	}
	if n > 0 { // before decoding, so a finished setup looks the same whatever the body
		writeError(w, 404, "not_found", "setup already completed")
		return
	}
	var in struct{ Code, Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	// An unset code matches nothing (ConstantTimeCompare of two empty strings is 1).
	if h.SetupCode == "" || subtle.ConstantTimeCompare([]byte(in.Code), []byte(h.SetupCode)) != 1 {
		writeError(w, 403, "forbidden", "invalid setup code")
		return
	}
	u, err := h.Auth.SetupAdmin(in.Username, in.Password) // re-checks emptiness atomically
	if errors.Is(err, auth.ErrSetupDone) {
		writeError(w, 404, "not_found", "setup already completed")
		return
	}
	if err != nil {
		credentialError(w, err)
		return
	}
	h.startSession(w, r, u)
}

func (h *handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	u, err := h.Auth.AcceptInvite(chi.URLParam(r, "token"), in.Username, in.Password)
	if err != nil {
		credentialError(w, err)
		return
	}
	h.startSession(w, r, u)
}

func (h *handler) changePassword(w http.ResponseWriter, r *http.Request, u auth.User) {
	var in struct{ Old, New string }
	if !decode(w, r, &in) {
		return
	}
	c, _ := r.Cookie(cookieName) // present: authed checked it
	ended, err := h.Auth.ChangePassword(u.ID, in.Old, in.New, c.Value)
	if errors.Is(err, auth.ErrBadCredentials) {
		writeError(w, 401, "invalid_credentials", "current password is wrong")
		return
	}
	if err != nil {
		credentialError(w, err)
		return
	}
	h.streams.end(ended...)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) listUsers(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	users, err := h.Auth.ListUsers()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, users)
}

func (h *handler) listInvites(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	invites, err := h.Auth.ListInvites()
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, invites)
}

func (h *handler) createInvite(w http.ResponseWriter, _ *http.Request, u auth.User) {
	token, expires, err := h.Auth.NewInvite(u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 201, map[string]string{"url": "/invite/" + token, "expires_at": expires.UTC().Format(time.RFC3339)})
}

func (h *handler) deleteInvite(w http.ResponseWriter, r *http.Request, _ auth.User) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 400, "validation", "invalid invite id")
		return
	}
	err = h.Auth.DeleteInvite(id)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, 404, "not_found", "no such invite")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) deleteUser(w http.ResponseWriter, r *http.Request, u auth.User) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, 400, "validation", "invalid user id")
		return
	}
	if id == u.ID {
		writeError(w, 400, "validation", "cannot delete yourself")
		return
	}
	err = h.Auth.DeleteUser(id) // first: this kills their sessions, so no request can reopen the workspace
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, 404, "not_found", "no such user")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if err := h.WS.Remove(id); err != nil { // also ends the user's in-flight requests via the workspace ctx
		internalError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := h.Auth.DeleteSession(c.Value); err != nil {
			internalError(w, err)
			return
		}
		h.streams.end(auth.TokenHash(c.Value))
	}
	setCookie(w, r, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
}

// api hands any other /api/* request to the session user's own router. The request ends when the session does
// (logout, password change elsewhere, expiry) or when the user is deleted.
func (h *handler) api(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	// Tracked before the session check, so a session that ends right after the check still ends this request.
	ctx, done := h.streams.track(r.Context(), auth.TokenHash(c.Value))
	defer done()
	u, expires, err := h.Auth.Session(c.Value)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	ws, err := h.WS.Open(ctx, u) // returns the already-open workspace when there is one
	if errors.Is(err, ErrRemoved) { // the user was deleted while this request was on its way
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	ctx, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	forward(ws, w, r.WithContext(ctx))
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
	matches := 0
	for _, ws := range h.WS.All() { // compare against every token so timing does not reveal which one matched
		token, err := ws.Svc.MCPToken(r.Context())
		if err == nil && token != "" && subtle.ConstantTimeCompare(got, []byte("Bearer "+token)) == 1 {
			match = ws
			matches++
		}
	}
	if matches > 1 {
		slog.Error("selfhost: several profiles share one MCP token; refusing it until it is rotated", "profiles", matches)
	}
	if matches != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	forward(match, w, r)
}

// forward hands r to the workspace's router with a context that also ends when the workspace is removed or
// closed. It drops this mux's chi route context so the user's router routes the request from scratch.
func forward(ws *Workspace, w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(context.WithValue(r.Context(), chi.RouteCtxKey, nil))
	defer cancel()
	stop := context.AfterFunc(ws.ctx, cancel)
	defer stop()
	ws.Router.ServeHTTP(w, r.WithContext(ctx))
}
