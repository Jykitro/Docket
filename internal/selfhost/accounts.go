package selfhost

import (
	"bytes"
	"io"

	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"docket/internal/auth"
	"github.com/go-chi/chi/v5"
)

// browserGuard keeps browser writes on the same public origin, including its port.
func browserGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; font-src 'self' data:; frame-src 'self' about:; object-src 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
					scheme = "https"
				}
				if err != nil || u.Scheme != scheme || !strings.EqualFold(u.Host, r.Host) || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
					writeError(w, 403, "forbidden_origin", "origin not allowed")
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, 403, "forbidden_origin", "origin not allowed")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func ownerKey(id int64) string { return "owner:" + strconv.FormatInt(id, 10) }

// Consume the small credentials body before acquiring the access-transition lock.
func credentialRequest(lock sync.Locker, limit int64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctl := http.NewResponseController(w)
		_ = ctl.SetReadDeadline(time.Now().Add(10 * time.Second))
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		_ = ctl.SetReadDeadline(time.Time{})
		if err != nil {
			writeError(w, 400, "validation", "invalid credentials body")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if lock != nil {
			lock.Lock()
			defer lock.Unlock()
		}
		next(w, r)
	}
}
func smallCredentials(next http.HandlerFunc) http.HandlerFunc {
	return credentialRequest(nil, 8192, next)
}
func (h *handler) exclusive(next http.HandlerFunc) http.HandlerFunc {
	return credentialRequest(&h.access, 8192, next)
}

func (h *handler) admitCredential(w http.ResponseWriter) bool {
	select {
	case h.credentials <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		writeError(w, 429, "rate_limited", "too many requests, try again later")
		return false
	}
}

func (h *handler) limitSignup(operation string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.clients.Attempt(operation + ":" + h.clientIP(r)) {
			w.Header().Set("Retry-After", "600")
			writeError(w, 429, "rate_limited", "too many requests, try again later")
			return
		}
		if !h.admitCredential(w) {
			return
		}
		defer func() { <-h.credentials }()
		next(w, r)
	}
}

// Reauthenticate outside the transition lock. Recheck both the password version and the
// initiating session under the lock before a sensitive account mutation may commit.
func (h *handler) confirmed(w http.ResponseWriter, r *http.Request, u auth.User, password string, prepare func() error, action func(auth.PasswordProof)) {
	key := "confirm:" + ownerKey(u.ID)
	if !h.limiter.Attempt(key) {
		w.Header().Set("Retry-After", "600")
		writeError(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
	if !h.admitCredential(w) {
		return
	}
	proof, err := h.Auth.VerifyPassword(u.Username, password)
	if err == nil && prepare != nil {
		err = prepare()
	}
	<-h.credentials
	if err != nil {
		credentialError(w, err)
		return
	}
	h.access.Lock()
	defer h.access.Unlock()
	current, ok, err := h.session(r)
	if err != nil {
		internalError(w, err)
		return
	}
	if !ok || current.ID != u.ID {
		writeError(w, 401, "unauthorized", "not logged in")
		return
	}
	if err = h.Auth.PasswordCurrent(proof); err != nil {
		credentialError(w, err)
		return
	}
	h.limiter.Reset(key)
	action(proof)
}

func (h *handler) registration(w http.ResponseWriter, r *http.Request) {
	mode, err := h.Auth.Registration(r.Context())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"mode": mode, "passkeys": h.passkeySite(r) != nil})
}

func (h *handler) setRegistration(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var in struct{ Mode string }
	if !decode(w, r, &in) {
		return
	}
	if in.Mode == "open" && (h.RunnerReady == nil || h.RunnerReady(r.Context()) != nil) {
		writeError(w, 503, "unavailable", "isolated Code Mode runner is unavailable")
		return
	}
	if err := h.Auth.SetRegistration(r.Context(), in.Mode); err != nil {
		credentialError(w, err)
		return
	}
	w.WriteHeader(204)
}

func (h *handler) setBlocked(w http.ResponseWriter, r *http.Request, _ auth.User) {
	var in struct{ Blocked *bool }
	if !decode(w, r, &in) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 || in.Blocked == nil {
		writeError(w, 400, "validation", "user id and blocked flag required")
		return
	}
	if *in.Blocked {
		if err = h.Auth.ProtectAdministrator(r.Context(), id); err != nil {
			credentialError(w, err)
			return
		}
	}
	if !*in.Blocked && h.WS.shared == nil {
		active, err := h.Auth.Active(r.Context(), id)
		if err != nil {
			internalError(w, err)
			return
		}
		if !active {
			if ws, ok := h.WS.Get(id); ok {
				if _, err = ws.Svc.RotateMCPToken(r.Context()); err != nil {
					internalError(w, err)
					return
				}
			}
		}
	}
	if err = h.Auth.SetBlocked(r.Context(), id, *in.Blocked); err != nil {
		credentialError(w, err)
		return
	}
	if *in.Blocked {
		if h.WS.shared != nil {
			h.WS.forgetOwner(id)
		}
		h.streams.end(ownerKey(id))
	}
	w.WriteHeader(204)
}

func (h *handler) issueRecovery(w http.ResponseWriter, r *http.Request, u auth.User) {
	var in struct{ Password string }
	if !decode(w, r, &in) {
		return
	}
	h.confirmed(w, r, u, in.Password, nil, func(auth.PasswordProof) {
		code, err := h.Auth.IssueRecovery(r.Context(), u.ID)
		if err != nil {
			internalError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"recovery_code": code})
	})
}

func (h *handler) recoverAccount(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Code, Password string }
	if !decode(w, r, &in) {
		return
	}
	ctx := r.Context()
	name := strings.ToLower(strings.TrimSpace(in.Username))
	if len(name) > 32 {
		credentialError(w, auth.ErrBadCredentials)
		return
	}
	if !h.accountAttempts.Attempt("recover:" + name) {
		writeError(w, 429, "rate_limited", "too many attempts, try again later")
		return
	}
	digest, err := h.Auth.PrepareRecovery(ctx, name, in.Code, in.Password)
	if err != nil {
		credentialError(w, err)
		return
	}
	h.access.Lock()
	defer h.access.Unlock()
	callback := func(u auth.User) error {
		ws, e := h.WS.Open(ctx, u)
		if e != nil {
			return e
		}
		h.streams.end(ownerKey(u.ID))
		_, e = ws.Svc.RotateMCPToken(ctx)
		return e
	}
	var owner int64
	if h.WS.shared != nil {
		callback = func(u auth.User) error { owner = u.ID; return nil }
	}
	code, err := h.Auth.CompleteRecovery(ctx, name, in.Code, digest, callback)
	if err != nil {
		credentialError(w, err)
		return
	}
	if owner != 0 {
		h.WS.forgetOwner(owner)
		h.streams.end(ownerKey(owner))
	}
	writeJSON(w, 200, map[string]string{"recovery_code": code})
}
