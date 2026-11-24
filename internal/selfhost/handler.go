package selfhost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-webauthn/webauthn/webauthn"

	"docket/internal/auth"
	"docket/internal/httpapi"
)

const (
	cookieName = "docket_session"
	cookieAge  = 30 * 24 * 60 * 60 // seconds; matches the auth session TTL
)

type Options struct {
	Auth           *auth.DB
	WS             *Workspaces
	Web            http.Handler
	AllowedHosts   []string
	TrustedProxies []netip.Prefix
	Passkeys       map[string]*webauthn.WebAuthn
	SetupCode      string
	SetupComplete  func()
	RunnerReady    func(context.Context) error
}

type handler struct {
	Options
	limiter         *auth.Limiter
	clients         *auth.Limiter
	accountAttempts *auth.Limiter
	credentials     chan struct{}
	access          sync.RWMutex
	flows           passkeyRequests
	passkeyLimit    *auth.Limiter
	streams         streams
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
	h := &handler{Options: o, passkeyLimit: auth.NewLimiter(120, 10*time.Minute), limiter: auth.NewLimiter(5, 10*time.Minute), clients: auth.NewLimiter(20, 10*time.Minute), accountAttempts: auth.NewLimiter(20, 10*time.Minute), credentials: make(chan struct{}, 8)}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer, httpapi.HostGuard(o.AllowedHosts), browserGuard)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	r.Get("/api/auth/me", h.me)
	r.Get("/api/auth/registration", h.registration)
	r.Post("/api/auth/register", smallCredentials(h.limitSignup("signup", h.acceptInvite)))
	r.Post("/api/auth/recover", smallCredentials(h.limitSignup("recover", h.recoverAccount)))
	r.Post("/api/auth/recovery-code", smallCredentials(h.authed(h.issueRecovery)))
	r.Delete("/api/auth/account", smallCredentials(h.authed(h.deleteUser)))
	r.Put("/api/admin/registration", h.admin(h.setRegistration))
	r.Put("/api/admin/users/{id}/blocked", h.exclusive(h.admin(h.setBlocked)))
	r.Get("/api/auth/passkeys", h.authed(h.listPasskeys))
	r.Delete("/api/auth/passkeys/{id}", smallCredentials(h.authed(h.revokePasskey)))
	r.Post("/api/auth/passkeys/register/begin", smallCredentials(h.passkeyRoute(h.beginPasskeyRegistration)))
	r.Post("/api/auth/passkeys/register/finish", credentialRequest(&h.access, 64<<10, h.passkeyRoute(h.finishPasskeyRegistration)))
	r.Post("/api/auth/passkeys/login/begin", h.passkeyRoute(h.beginPasskeyLogin))
	r.Post("/api/auth/passkeys/login/finish", credentialRequest(&h.access, 64<<10, h.passkeyRoute(h.finishPasskeyLogin)))
	r.Post("/api/auth/login", smallCredentials(h.login))
	r.Post("/api/auth/logout", h.logout)
	r.Post("/api/auth/setup", smallCredentials(h.setup))
	r.Post("/api/auth/invite/{token}", smallCredentials(h.limitSignup("signup", h.acceptInvite)))
	r.Put("/api/auth/password", smallCredentials(h.authed(h.changePassword)))
	// Registered explicitly so no /api/admin path ever reaches a user's own router.
	r.Get("/api/admin/users", h.admin(h.listUsers))
	r.Get("/api/admin/invites", h.admin(h.listInvites))
	r.Post("/api/admin/invites", h.admin(h.createInvite))
	r.Delete("/api/admin/invites/{id}", h.admin(h.deleteInvite))
	adminNotFound := h.admin(func(w http.ResponseWriter, _ *http.Request, _ auth.User) {
		writeError(w, 404, "not_found", "no such endpoint")
	})
	r.Handle("/api/admin", adminNotFound)
	r.Handle("/api/admin/*", adminNotFound)
	r.Get("/api/mcp/install.sh", httpapi.InstallScript) // public: no secrets, curl has no session
	r.Handle("/api/*", http.HandlerFunc(h.api))
	r.Handle("/mcp", http.HandlerFunc(h.mcp))
	r.Handle("/mcp/code", http.HandlerFunc(h.mcp))
	r.Handle("/*", o.Web)
	return r
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Cache-Control", "no-store")
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
	peer := h.clientIP(r)
	key := "login:" + peer + "|" + name
	if !h.clients.Attempt("login:"+peer) || !h.limiter.Attempt(key) || !h.accountAttempts.Attempt("login:"+name) { // counted before the password check, so parallel guesses cannot outrun it
		writeError(w, 429, "rate_limited", "too many failed attempts, try again later")
		return
	}
	if !h.admitCredential(w) {
		return
	}
	defer func() { <-h.credentials }()
	proof, err := h.Auth.VerifyPassword(in.Username, in.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		writeError(w, 401, "invalid_credentials", "invalid username or password")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	h.access.RLock()
	defer h.access.RUnlock()
	if err := h.Auth.PasswordCurrent(proof); err != nil {
		credentialError(w, err)
		return
	}
	h.limiter.Reset(key)
	h.accountAttempts.Reset("login:" + name)
	h.loginSession(w, r, proof.User)
}

func (h *handler) loginSession(w http.ResponseWriter, r *http.Request, u auth.User) {
	if _, err := h.WS.Open(r.Context(), u); err != nil {
		internalError(w, err)
		return
	}
	token, ended, err := h.Auth.StartSession(u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	h.streams.end(ended...)
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

// A browser can pin a request to the owner whose data it is displaying during a cross-tab login.
func matchesWorkspace(w http.ResponseWriter, r *http.Request, u auth.User) bool {
	if expected := r.Header.Get("X-Docket-User"); expected != "" && expected != strconv.FormatInt(u.ID, 10) {
		writeError(w, 409, "session_changed", "the signed-in user changed")
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
		if !matchesWorkspace(w, r, u) {
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
	h.access.RLock()
	defer h.access.RUnlock()
	active, err := h.Auth.Active(r.Context(), u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if !active {
		credentialError(w, auth.ErrBadCredentials)
		return
	}
	if _, err := h.WS.Open(r.Context(), u); err != nil {
		slog.Warn("workspace provisioning failed", "user_id", u.ID)
		internalError(w, err)
		return
	}
	code, err := h.Auth.IssueRecovery(r.Context(), u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	token, ended, err := h.Auth.StartSession(u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	slog.Info("workspace signup completed", "user_id", u.ID, "route", chi.RouteContext(r.Context()).RoutePattern())
	h.streams.end(ended...)
	setCookie(w, r, token, cookieAge)
	writeJSON(w, 200, struct {
		auth.User
		RecoveryCode string `json:"recovery_code"`
	}{u, code})
}

// credentialError answers a failed setup, invite or password change.
func credentialError(w http.ResponseWriter, err error) {
	var ve auth.ValidationError
	switch {
	case errors.Is(err, auth.ErrRegistration), errors.Is(err, auth.ErrLastAdmin):
		writeError(w, 403, "forbidden", err.Error())
	case errors.Is(err, auth.ErrCapacity):
		writeError(w, 409, "capacity", err.Error())
	case errors.Is(err, auth.ErrBadCredentials):
		writeError(w, 401, "invalid_credentials", "invalid credentials")
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, 404, "not_found", "no such user")
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
	if !h.admitCredential(w) {
		return
	}
	defer func() { <-h.credentials }()
	u, err := h.Auth.SetupAdmin(in.Username, in.Password) // re-checks emptiness atomically
	if errors.Is(err, auth.ErrSetupDone) {
		writeError(w, 404, "not_found", "setup already completed")
		return
	}
	if err != nil {
		credentialError(w, err)
		return
	}
	if h.SetupComplete != nil {
		h.SetupComplete()
	}
	h.startSession(w, r, u)
}

func (h *handler) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password string }
	if !decode(w, r, &in) {
		return
	}
	u, err := h.Auth.Register(r.Context(), chi.URLParam(r, "token"), in.Username, in.Password)
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
	var digest auth.PasswordDigest
	h.confirmed(w, r, u, in.Old, func() error {
		var err error
		digest, err = auth.PreparePassword(in.New)
		return err
	}, func(proof auth.PasswordProof) {
		c, _ := r.Cookie(cookieName)
		ended, err := h.Auth.ReplacePassword(proof, digest, c.Value)
		if err != nil {
			credentialError(w, err)
			return
		}
		h.streams.end(ended...)
		w.WriteHeader(http.StatusNoContent)
	})
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
		credentialError(w, err)
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
	var in struct{ Password string }
	if !decode(w, r, &in) {
		return
	}
	h.confirmed(w, r, u, in.Password, nil, func(auth.PasswordProof) {
		id := u.ID
		if err := h.Auth.ProtectAdministrator(r.Context(), id); err != nil {
			credentialError(w, err)
			return
		}
		ws, err := h.WS.Open(r.Context(), u)
		if err != nil {
			internalError(w, err)
			return
		}
		confirmation, err := ws.confirmDeletion(r.Context())
		if err != nil {
			internalError(w, err)
			return
		}
		err = h.Auth.DeleteUser(id, confirmation) // revokes sessions before removing the workspace
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, 404, "not_found", "no such user")
			return
		}
		if err != nil {
			internalError(w, err)
			return
		}
		h.streams.end(ownerKey(id))
		if err := h.WS.Remove(id); err != nil { // also ends the user's in-flight requests via the workspace ctx
			internalError(w, err)
			return
		}
		if err = h.Auth.FinishDeletion(r.Context(), id); err != nil {
			internalError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	h.access.Lock()
	defer h.access.Unlock()
	if c, err := r.Cookie(cookieName); err == nil {
		if r.Header.Get("X-Docket-User") != "" {
			u, ok, err := h.session(r)
			if err != nil {
				internalError(w, err)
				return
			}
			if ok && !matchesWorkspace(w, r, u) {
				return
			}
		}
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
	w.Header().Set("Cache-Control", "private, no-store")
	c, err := r.Cookie(cookieName)
	if err != nil {
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	// Tracked before the session check, so a session that ends right after the check still ends this request.
	ctx, done := h.streams.track(r.Context(), auth.TokenHash(c.Value))
	defer done()
	h.access.RLock()
	u, expires, err := h.Auth.Session(c.Value)
	if err == nil {
		var release func()
		ctx, release = h.streams.track(ctx, ownerKey(u.ID))
		defer release()
	}
	h.access.RUnlock()
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if !matchesWorkspace(w, r, u) {
		return
	}
	ws, err := h.WS.Open(ctx, u)    // returns the already-open workspace when there is one
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
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	match, tokenCtx := h.WS.tokens.lookup(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if match == nil {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(tokenCtx, cancel)
	defer stop()
	ctx, done := h.streams.track(ctx, ownerKey(match.ID))
	defer done()
	active, err := h.Auth.Active(ctx, match.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	if !active {
		writeError(w, 401, "unauthorized", "inactive account")
		return
	}
	forward(match, w, r.WithContext(ctx))
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
