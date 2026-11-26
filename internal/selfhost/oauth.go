package selfhost

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"docket/internal/auth"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/service"
	"github.com/go-chi/chi/v5"
)

const oauthRead = "mcp:read"
const oauthWrite = "mcp:write"

var oauthVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func loopbackHost(host string) bool {
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

// HostGuard checks the configured host allowlist before these handlers. Only a
// loopback installation advertises HTTP. Proxy headers cannot change the issuer.
func oauthOrigin(r *http.Request) string {
	u := url.URL{Host: r.Host, Scheme: "https"}
	if loopbackHost(u.Hostname()) && r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		u.Scheme = "http"
	}
	return u.String()
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}

func (h *handler) oauthRoutes(r *chi.Mux) {
	r.Get("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		o := oauthOrigin(r)
		writeJSON(w, 200, map[string]any{
			"issuer":                                         o,
			"authorization_endpoint":                         o + "/oauth/authorize",
			"token_endpoint":                                 o + "/oauth/token",
			"registration_endpoint":                          o + "/oauth/register",
			"code_challenge_methods_supported":               []string{"S256"},
			"response_types_supported":                       []string{"code"},
			"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
			"token_endpoint_auth_methods_supported":          []string{"none"},
			"scopes_supported":                               []string{oauthRead, oauthWrite, "offline_access"},
			"authorization_response_iss_parameter_supported": true,
		})
	})
	for _, path := range []string{"", "/mcp", "/mcp/code"} {
		r.Get("/.well-known/oauth-protected-resource"+path, func(w http.ResponseWriter, r *http.Request) {
			resource := path
			if resource == "" {
				resource = "/mcp"
			}
			o := oauthOrigin(r)
			writeJSON(w, 200, map[string]any{"resource": o + resource, "authorization_servers": []string{o}, "bearer_methods_supported": []string{"header"}, "scopes_supported": []string{oauthRead, oauthWrite}})
		})
	}
	r.Post("/oauth/register", h.oauthRegister)
	r.Post("/oauth/token", h.oauthToken)
	// /oauth/authorize is the SPA behind its usual account/passkey gate.
	r.Get("/api/auth/oauth/request", h.authed(h.oauthConsentInfo))
	r.Post("/api/auth/oauth/decision", credentialRequest(h.access.RLocker(), 8192, h.authed(h.oauthDecision)))
	r.Get("/api/auth/oauth/connections", h.authed(h.oauthConnections))
	r.Delete("/api/auth/oauth/connections/{grant}", h.exclusive(h.authed(func(w http.ResponseWriter, r *http.Request, u auth.User) {
		if err := h.Auth.RevokeOAuth(r.Context(), u.ID, chi.URLParam(r, "grant")); err != nil {
			internalError(w, err)
			return
		}
		h.streams.end(ownerKey(u.ID) + ":oauth:" + chi.URLParam(r, "grant"))
		w.WriteHeader(http.StatusNoContent)
	})))
}

// A public registration confers no access. Encode the immutable metadata into
// the client ID so anonymous registrations never allocate database records.
// The exact ID binds codes and refresh tokens to this set of callback URLs.
type oauthClient struct {
	Name      string   `json:"client_name"`
	Redirects []string `json:"redirect_uris"`
}

func validOAuthClient(c oauthClient) bool {
	if len(c.Name) > 120 || strings.IndexFunc(c.Name, unicode.IsControl) >= 0 || len(c.Redirects) == 0 || len(c.Redirects) > 4 {
		return false
	}
	for _, uri := range c.Redirects {
		u, e := url.Parse(uri)
		if e != nil || len(uri) > 500 || u.Hostname() == "" || u.User != nil || strings.ContainsAny(uri, "#\\") || !(u.Scheme == "https" || u.Scheme == "http" && loopbackHost(u.Hostname())) {
			return false
		}
	}
	return true
}

// Native apps can choose a fresh loopback port on every login (RFC 8252 §7.3).
// Keep the host, scheme, path and query from the registered redirect.
func registeredRedirect(registered []string, uri string) bool {
	if slices.Contains(registered, uri) {
		return true
	}
	target, err := url.Parse(uri)
	if err != nil || target.Scheme != "http" || !loopbackHost(target.Hostname()) || target.User != nil || strings.ContainsAny(uri, "#\\") {
		return false
	}
	port, err := strconv.ParseUint(target.Port(), 10, 16)
	if err != nil || port == 0 {
		return false
	}
	for _, raw := range registered {
		base, err := url.Parse(raw)
		if err == nil && base.Scheme == target.Scheme && strings.EqualFold(base.Hostname(), target.Hostname()) {
			base.Host = target.Host
			if base.String() == target.String() {
				return true
			}
		}
	}
	return false
}

func parseClient(id string) (oauthClient, bool) {
	var c oauthClient
	if !strings.HasPrefix(id, "plc_") || len(id) > 3400 {
		return c, false
	}
	b, err := base64.RawURLEncoding.DecodeString(id[4:])
	ok := err == nil && json.Unmarshal(b, &c) == nil && validOAuthClient(c)
	return c, ok
}
func (h *handler) oauthRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		oauthClient
		AuthMethod string `json:"token_endpoint_auth_method"`
	}
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typ != "application/json" || json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in) != nil || !validOAuthClient(in.oauthClient) || in.AuthMethod != "" && in.AuthMethod != "none" {
		oauthError(w, 400, "invalid_client_metadata", "Register a public PKCE client with an HTTPS or loopback redirect URI.")
		return
	}
	if in.Name == "" {
		in.Name = "MCP"
	}
	b, _ := json.Marshal(in.oauthClient)
	id := "plc_" + base64.RawURLEncoding.EncodeToString(b)
	if len(id) > 3400 {
		oauthError(w, 400, "invalid_client_metadata", "Registration metadata is too large.")
		return
	}
	writeJSON(w, 201, map[string]any{
		"client_id": id, "client_name": in.Name, "redirect_uris": in.Redirects,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"},
	})
}

func oauthResource(origin, raw string) (string, bool) {
	u, e := url.Parse(raw)
	if e != nil || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Scheme+"://"+u.Host != origin || (u.Path != "/mcp" && u.Path != "/mcp/code") {
		return "", false
	}
	// The optional query labels an agent; both discovery and token validation
	// use the canonical MCP resource independent of that display label.
	return origin + u.Path, true
}
func duplicateOAuthParam(values url.Values) bool {
	for _, v := range values {
		if len(v) != 1 {
			return true
		}
	}
	return false
}

func authorization(r *http.Request) (auth.OAuthRequest, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || duplicateOAuthParam(q) {
		return auth.OAuthRequest{}, false
	}
	id := q.Get("client_id")
	client, ok := parseClient(id)
	redirect := q.Get("redirect_uri")
	challenge := q.Get("code_challenge")
	b, e := base64.RawURLEncoding.DecodeString(challenge)
	if !ok || !registeredRedirect(client.Redirects, redirect) || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || e != nil || len(b) != 32 || base64.RawURLEncoding.EncodeToString(b) != challenge || len(q.Get("state")) > 1024 {
		return auth.OAuthRequest{}, false
	}
	o := oauthOrigin(r)
	resource := o + "/mcp"
	if raw := q.Get("resource"); raw != "" {
		resource, ok = oauthResource(o, raw)
		if !ok {
			return auth.OAuthRequest{}, false
		}
	}
	scope := strings.Fields(q.Get("scope"))
	for _, s := range scope {
		if s != oauthRead && s != oauthWrite && s != "offline_access" {
			return auth.OAuthRequest{}, false
		}
	}
	// Clients that omit scopes request the normal planner workflow. Consent
	// can always narrow it and workspace policy is evaluated on every call.
	permissions := oauthRead
	if len(scope) == 0 || slices.Contains(scope, oauthWrite) {
		permissions += " " + oauthWrite
	}
	if slices.Contains(scope, "offline_access") {
		permissions += " offline_access"
	}
	return auth.OAuthRequest{ClientID: id, ClientName: client.Name, RedirectURI: redirect, Resource: resource, Scope: permissions, Challenge: challenge}, true
}
func (h *handler) oauthConsentInfo(w http.ResponseWriter, r *http.Request, _ auth.User) {
	req, ok := authorization(r)
	if !ok {
		oauthError(w, 400, "invalid_request", "Invalid authorization request.")
		return
	}
	writeJSON(w, 200, map[string]any{"client": req.ClientName, "redirect_uri": req.RedirectURI, "write": slices.Contains(strings.Fields(req.Scope), oauthWrite)})
}
func (h *handler) oauthDecision(w http.ResponseWriter, r *http.Request, u auth.User) {
	origin := oauthOrigin(r)
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Header.Get("Origin") != origin || typ != "application/json" || h.LocalID != 0 && !loopbackHost((&url.URL{Host: r.Host}).Hostname()) {
		oauthError(w, 403, "access_denied", "Approve from the Plandillo origin. Local mode requires a loopback address.")
		return
	}
	req, ok := authorization(r)
	var in struct {
		Workspace    int64
		Allow, Write bool
	}
	if !ok {
		oauthError(w, 400, "invalid_request", "Invalid authorization request.")
		return
	}
	if !decode(w, r, &in) {
		return
	}
	target, _ := url.Parse(req.RedirectURI)
	q := target.Query()
	q.Set("iss", origin)
	if state := r.URL.Query().Get("state"); state != "" {
		q.Set("state", state)
	}
	if in.Allow {
		if in.Workspace <= 0 {
			oauthError(w, 400, "invalid_request", "Select a workspace.")
			return
		}
		sp, err := h.WS.shared.Space(r.Context(), u.ID, in.Workspace)
		if err != nil {
			spaceError(w, err)
			return
		}
		ws, err := h.WS.openSpace(r.Context(), sp)
		if err != nil {
			internalError(w, err)
			return
		}
		pol, err := ws.Svc.MCPPolicy(r.Context())
		if err != nil {
			internalError(w, err)
			return
		}
		if !in.Write || pol.Mode == service.ModeReadOnly {
			req.Scope = strings.Replace(req.Scope, " "+oauthWrite, "", 1)
		}
		code, err := h.Auth.AuthorizeOAuth(r.Context(), u.ID, sp.ID, req)
		if err != nil {
			credentialError(w, err)
			return
		}
		q.Set("code", code)
	} else {
		q.Set("error", "access_denied")
	}
	target.RawQuery = q.Encode()
	writeJSON(w, 200, map[string]string{"redirect": target.String()})
}

func (h *handler) oauthToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<10)
	if r.ParseForm() != nil || duplicateOAuthParam(r.PostForm) {
		oauthError(w, 400, "invalid_request", "Invalid token request.")
		return
	}
	f := r.PostForm
	req := auth.OAuthRequest{ClientID: f.Get("client_id"), RedirectURI: f.Get("redirect_uri")}
	if _, ok := parseClient(req.ClientID); !ok {
		oauthError(w, 400, "invalid_client", "Unknown public client.")
		return
	}
	o := oauthOrigin(r)
	if raw := f.Get("resource"); raw != "" {
		var ok bool
		req.Resource, ok = oauthResource(o, raw)
		if !ok {
			oauthError(w, 400, "invalid_target", "Invalid MCP resource.")
			return
		}
	}
	refresh := false
	var token string
	switch f.Get("grant_type") {
	case "authorization_code":
		v := f.Get("code_verifier")
		if !oauthVerifier.MatchString(v) {
			oauthError(w, 400, "invalid_grant", "Invalid PKCE verifier.")
			return
		}
		hash := sha256.Sum256([]byte(v))
		req.Challenge = base64.RawURLEncoding.EncodeToString(hash[:])
		token = f.Get("code")
	case "refresh_token":
		refresh = true
		req.Scope = f.Get("scope")
		token = f.Get("refresh_token")
	default:
		oauthError(w, 400, "unsupported_grant_type", "Use authorization_code or refresh_token.")
		return
	}
	out, err := h.Auth.ExchangeOAuth(r.Context(), o, refresh, token, req)
	if errors.Is(err, auth.ErrOAuthScope) {
		oauthError(w, 400, "invalid_scope", "Requested scopes must be within the approved access.")
		return
	}
	if errors.Is(err, auth.ErrNotFound) {
		oauthError(w, 400, "invalid_grant", "Grant expired, revoked or invalid.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *handler) oauthConnections(w http.ResponseWriter, r *http.Request, u auth.User) {
	ref := r.URL.Query().Get("workspace")
	sp, err := h.WS.shared.SpaceRef(r.Context(), u.ID, ref)
	if ref == "" || err != nil {
		oauthError(w, 400, "invalid_request", "Select a workspace.")
		return
	}
	out, err := h.Auth.OAuthConnections(r.Context(), u.ID, sp.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, out)
}
func (h *handler) oauthMCP(w http.ResponseWriter, r *http.Request, token string) {
	h.access.RLock()
	a, err := h.Auth.OAuthAccess(r.Context(), token, oauthOrigin(r)+r.URL.Path)
	var ctx context.Context
	if err == nil {
		var releaseOwner, releaseGrant func()
		ctx, releaseOwner = h.streams.track(r.Context(), ownerKey(a.Owner))
		ctx, releaseGrant = h.streams.track(ctx, ownerKey(a.Owner)+":oauth:"+a.ID)
		defer releaseOwner()
		defer releaseGrant()
	}
	h.access.RUnlock()
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, 401, "unauthorized", "invalid or expired access token")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	ctx, cancel := context.WithDeadline(ctx, time.Unix(a.Until, 0))
	defer cancel()
	u, err := h.Auth.ByID(a.Owner)
	if err != nil {
		credentialError(w, err)
		return
	}
	sp, err := h.WS.shared.Space(ctx, u.ID, a.Workspace)
	if err != nil {
		writeError(w, 401, "unauthorized", "workspace unavailable")
		return
	}
	ws, err := h.WS.openSpace(ctx, sp)
	if err != nil {
		internalError(w, err)
		return
	}
	if !slices.Contains(strings.Fields(a.Scope), oauthWrite) {
		ctx = service.ReadOnlyMCPContext(ctx)
	}
	// Claude Code appends the connection name in parentheses during registration.
	clientName, _, _ := strings.Cut(a.ClientName, " (")
	ctx = mcpserver.WithClientName(ctx, clientName)
	w.Header().Del("WWW-Authenticate")
	forward(ws, w, r.WithContext(httpapi.WithVerifiedMCPWorkspace(ctx, ws.Svc)))
}
