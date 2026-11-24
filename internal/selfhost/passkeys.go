package selfhost

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"docket/internal/auth"
	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const passkeyTTL = 5 * time.Minute

func loopbackDomain(host string) bool {
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// PasskeySites builds the exact trusted browser origins from operator configuration.
func PasskeySites(hosts []string, addr, override string) (map[string]*webauthn.WebAuthn, error) {
	sites := map[string]*webauthn.WebAuthn{}
	allowed := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		allowed = append(allowed, strings.ToLower(strings.TrimSpace(raw)))
	}
	var origins []string
	if override != "" {
		origins = []string{override}
	} else {
		_, port, _ := net.SplitHostPort(addr)
		for _, host := range allowed {
			if protocol.ValidateRPID(host) != nil {
				continue
			}
			origin := "https://" + host
			if loopbackDomain(host) {
				origin = "http://" + host
				if port != "" && port != "80" {
					origin += ":" + port
				}
			}
			origins = append(origins, origin)
		}
	}
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil {
			return nil, errors.New("invalid DOCKET_PASSKEY_ORIGIN")
		}
		secure := u.Scheme == "https" || u.Scheme == "http" && loopbackDomain(u.Hostname())
		if !secure || !slices.Contains(allowed, u.Hostname()) || u.Scheme+"://"+u.Host != origin {
			return nil, errors.New("DOCKET_PASSKEY_ORIGIN must be an exact HTTPS origin on an allowed hostname (HTTP is supported on localhost)")
		}
		if u.Scheme == "https" && u.Port() == "443" || u.Scheme == "http" && u.Port() == "80" {
			u.Host = u.Hostname()
		}
		site, err := webauthn.New(&webauthn.Config{RPDisplayName: "Plandillo", RPID: u.Hostname(), RPOrigins: []string{u.String()}, AuthenticatorSelection: protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired}})
		if err != nil {
			return nil, err
		}
		sites[u.String()] = site
	}
	return sites, nil
}

func (h *handler) passkeySite(r *http.Request) *webauthn.WebAuthn {
	scheme := "http://"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https://"
	}
	host := strings.ToLower(r.Host)
	if scheme == "https://" {
		host = strings.TrimSuffix(host, ":443")
	} else {
		host = strings.TrimSuffix(host, ":80")
	}
	return h.Passkeys[scheme+host]
}

type passkeyRequest struct {
	site    *webauthn.WebAuthn
	session *webauthn.SessionData
	expires time.Time
	owner   auth.PasskeyUser // populated only for registration
	binding string           // hash of the browser's authenticated session for registration
	name    string
}

type passkeyRequests struct {
	db      *sql.DB
	auth    *auth.DB
	mu      sync.Mutex
	pending map[string]passkeyRequest
}

// A separate HttpOnly cookie binds even anonymous login challenges to the initiating browser.
func passkeyCookie(site *webauthn.WebAuthn) *http.Cookie {
	c := &http.Cookie{Name: "docket_passkey", Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: int(passkeyTTL.Seconds())}
	if strings.HasPrefix(site.Config.RPOrigins[0], "https://") {
		c.Name = "__Host-docket_passkey"
		c.Secure = true
	}
	return c
}

func (s *passkeyRequests) begin(w http.ResponseWriter, r *http.Request, flow passkeyRequest, options any) {
	if s.db != nil {
		s.beginShared(w, r, flow, options)
		return
	}
	now := time.Now()
	flow.expires = now.Add(passkeyTTL)
	c := passkeyCookie(flow.site)
	s.mu.Lock()
	if s.pending == nil {
		s.pending = make(map[string]passkeyRequest)
	}
	for k, old := range s.pending {
		if !now.Before(old.expires) {
			delete(s.pending, k)
		}
	}
	if old, err := r.Cookie(c.Name); err == nil {
		delete(s.pending, old.Value)
	}
	if len(s.pending) >= 1024 {
		s.mu.Unlock()
		writeError(w, 429, "rate_limited", "too many pending passkey requests")
		return
	}
	c.Value = rand.Text()
	s.pending[c.Value] = flow
	s.mu.Unlock()
	http.SetCookie(w, c)
	writeJSON(w, 200, options)
}

func (s *passkeyRequests) take(w http.ResponseWriter, r *http.Request, site *webauthn.WebAuthn) (passkeyRequest, bool) {
	c := passkeyCookie(site)
	cookie, err := r.Cookie(c.Name)
	if err != nil {
		return passkeyRequest{}, false
	}
	if s.db != nil {
		c.MaxAge = -1
		http.SetCookie(w, c)
		return s.takeShared(r, site, cookie.Value)
	}
	s.mu.Lock()
	flow, ok := s.pending[cookie.Value]
	delete(s.pending, cookie.Value)
	s.mu.Unlock()
	c.MaxAge = -1
	http.SetCookie(w, c)
	return flow, ok && flow.site == site && time.Now().Before(flow.expires)
}

func (h *handler) passkeyRoute(next func(http.ResponseWriter, *http.Request, *webauthn.WebAuthn)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		site := h.passkeySite(r)
		if site == nil || site.Config.RPOrigins[0] != r.Header.Get("Origin") {
			writeError(w, 403, "forbidden_origin", "origin not allowed")
			return
		}
		next(w, r, site)
	}
}

func (h *handler) listPasskeys(w http.ResponseWriter, r *http.Request, u auth.User) {
	keys, err := h.Auth.Passkeys(r.Context(), u.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"keys": keys, "available": h.passkeySite(r) != nil})
}

func (h *handler) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request, site *webauthn.WebAuthn) {
	h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		var in struct{ Name, Password string }
		if !decode(w, r, &in) {
			return
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || strings.ContainsRune(in.Name, 0) || utf8.RuneCountInString(in.Name) > 64 {
			writeError(w, 400, "validation", "passkey name must be 1-64 characters")
			return
		}
		h.confirmed(w, r, u, in.Password, nil, func(auth.PasswordProof) {
			owner, err := h.Auth.RegistrationUser(r.Context(), u, site.Config.RPID)
			if err != nil {
				internalError(w, err)
				return
			}
			options, session, err := site.BeginRegistration(owner,
				webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
				webauthn.WithExclusions(webauthn.Credentials(owner.WebAuthnCredentials()).CredentialDescriptors()))
			if err != nil {
				internalError(w, err)
				return
			}
			c, _ := r.Cookie(cookieName) // authed verified this cookie
			h.flows.begin(w, r, passkeyRequest{site: site, session: session, owner: owner, binding: auth.TokenHash(c.Value), name: in.Name}, options)
		})
	})(w, r)
}

func invalidPasskey(w http.ResponseWriter) {
	writeError(w, 401, "invalid_credentials", "passkey verification failed; try again")
}

func (h *handler) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request, site *webauthn.WebAuthn) {
	h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		flow, ok := h.flows.take(w, r, site)
		c, _ := r.Cookie(cookieName)
		if !ok || flow.owner.ID != u.ID || flow.binding != auth.TokenHash(c.Value) {
			invalidPasskey(w)
			return
		}
		key, err := site.FinishRegistration(flow.owner, *flow.session, r)
		if err != nil {
			invalidPasskey(w)
			return
		}
		if err = h.Auth.AddPasskey(r.Context(), u.ID, site.Config.RPID, flow.name, key); err != nil {
			credentialError(w, err)
			return
		}
		w.WriteHeader(204)
	})(w, r)
}

func (h *handler) beginPasskeyLogin(w http.ResponseWriter, r *http.Request, site *webauthn.WebAuthn) {
	if !h.passkeyLimit.Attempt(h.clientIP(r)) {
		writeError(w, 429, "rate_limited", "too many failed attempts, try again later")
		return
	}
	options, session, err := site.BeginDiscoverableLogin()
	if err != nil {
		internalError(w, err)
		return
	}
	h.flows.begin(w, r, passkeyRequest{site: site, session: session}, options)
}

func (h *handler) finishPasskeyLogin(w http.ResponseWriter, r *http.Request, site *webauthn.WebAuthn) {
	flow, ok := h.flows.take(w, r, site)
	if !ok || flow.binding != "" {
		invalidPasskey(w)
		return
	}
	var owner auth.PasskeyUser
	var lookupErr error
	_, key, err := site.FinishPasskeyLogin(func(id, handle []byte) (webauthn.User, error) {
		owner, lookupErr = h.Auth.PasskeyOwner(r.Context(), site.Config.RPID, id, handle)
		return owner, lookupErr
	}, *flow.session, r)
	if lookupErr != nil && !errors.Is(lookupErr, auth.ErrBadCredentials) {
		internalError(w, lookupErr)
		return
	}
	if err != nil || key.Authenticator.CloneWarning {
		invalidPasskey(w)
		return
	}
	if err = h.Auth.UpdatePasskey(r.Context(), owner.ID, key); err != nil {
		credentialError(w, err)
		return
	}
	h.loginSession(w, r, owner.User)
}

func (h *handler) revokePasskey(w http.ResponseWriter, r *http.Request, u auth.User) {
	var in struct{ Password string }
	if !decode(w, r, &in) {
		return
	}
	h.confirmed(w, r, u, in.Password, nil, func(auth.PasswordProof) {
		if err := h.Auth.DeletePasskey(r.Context(), u.ID, chi.URLParam(r, "id")); err != nil {
			if errors.Is(err, auth.ErrNotFound) {
				writeError(w, 404, "not_found", "passkey not found")
			} else {
				internalError(w, err)
			}
			return
		}
		w.WriteHeader(204)
	})
}
