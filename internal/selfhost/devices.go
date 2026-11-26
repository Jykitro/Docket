package selfhost

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"plandillo/internal/auth"
)

func (h *handler) securityRoutes(r *chi.Mux) {
	r.Get("/api/auth/sessions", h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		c, _ := r.Cookie(cookieName)
		list, err := h.Auth.Devices(r.Context(), u.ID, c.Value)
		if err != nil {
			credentialError(w, err)
			return
		}
		writeJSON(w, 200, list)
	}))
	r.Delete("/api/auth/sessions/{id}", h.exclusive(h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		hash, err := h.Auth.RevokeDevice(u.ID, chi.URLParam(r, "id"))
		if err != nil {
			credentialError(w, err)
			return
		}
		h.streams.end(hash)
		c, _ := r.Cookie(cookieName)
		if hash == auth.TokenHash(c.Value) {
			setCookie(w, r, "", -1)
		}
		w.WriteHeader(204)
	})))
	r.Get("/api/auth/totp", h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		active, err := h.Auth.HasTOTP(u.ID)
		if err != nil {
			credentialError(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"available": h.Auth.TOTPAvailable(), "enabled": active})
	}))
	r.Post("/api/auth/totp/{action}", smallCredentials(h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		var in struct{ Password, Code string }
		if !decode(w, r, &in) {
			return
		}
		h.confirmed(w, r, u, in.Password, nil, func(auth.PasswordProof) {
			switch chi.URLParam(r, "action") {
			case "begin":
				out, err := h.Auth.BeginEnrollment(u)
				if err != nil {
					credentialError(w, err)
					return
				}
				writeJSON(w, 200, out)
			case "confirm", "disable":
				if !h.accountAttempts.Attempt("totp:" + ownerKey(u.ID)) {
					writeError(w, 429, "rate_limited", "too many attempts")
					return
				}
				if err := h.Auth.SetTOTP(r.Context(), u.ID, in.Code, chi.URLParam(r, "action") == "confirm"); err != nil {
					credentialError(w, err)
					return
				}
				w.WriteHeader(204)
			default:
				http.NotFound(w, r)
			}
		})
	})))
	r.Post("/api/auth/login/totp", smallCredentials(h.limitSignup("totp-login", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Challenge, Code string }
		if !decode(w, r, &in) {
			return
		}
		h.access.Lock()
		defer h.access.Unlock()
		id, err := h.Auth.ConsumeChallenge(r.Context(), in.Challenge, in.Code)
		if err != nil {
			credentialError(w, err)
			return
		}
		u, err := h.Auth.ByID(id)
		if err != nil {
			credentialError(w, err)
			return
		}
		h.loginSession(w, r, u)
	})))
}
