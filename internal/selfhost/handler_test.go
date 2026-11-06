package selfhost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docket/internal/auth"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/service"
)

const testPassword = "password-123"

type server struct {
	t  *testing.T
	h  http.Handler
	db *auth.DB
	ws *Workspaces
}

func newServer(t *testing.T) *server {
	t.Helper()
	dir := t.TempDir()
	db, err := auth.Open(filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	hosts := []string{"localhost", "127.0.0.1"}
	ws := NewWorkspaces(dir, time.UTC, func(svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: hosts,
			MCP: mcpserver.Handler(svc, time.UTC), Web: http.NotFoundHandler()})
	})
	t.Cleanup(ws.Close)
	h := Handler(Options{Auth: db, WS: ws, Web: http.NotFoundHandler(), AllowedHosts: hosts})
	return &server{t: t, h: h, db: db, ws: ws}
}

// do sends a request from 127.0.0.1:1234 unless hdr overrides "RemoteAddr".
func (s *server) do(method, path, body string, c *http.Cookie, hdr map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = "localhost"
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Content-Type", "application/json")
	if c != nil {
		req.AddCookie(c)
	}
	for k, v := range hdr {
		if k == "RemoteAddr" {
			req.RemoteAddr = v
		} else {
			req.Header.Set(k, v)
		}
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	return rec
}

func (s *server) login(name, pw string, hdr map[string]string) *httptest.ResponseRecorder {
	s.t.Helper()
	b, _ := json.Marshal(map[string]string{"username": name, "password": pw})
	return s.do("POST", "/api/auth/login", string(b), nil, hdr)
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "docket_session" {
			return c
		}
	}
	return nil
}

// addUser creates the user (the first one via setup), opens its workspace and returns a logged-in cookie.
func (s *server) addUser(name string) (*http.Cookie, *Workspace) {
	s.t.Helper()
	u, err := s.db.SetupAdmin(name, testPassword)
	if errors.Is(err, auth.ErrSetupDone) {
		var tok string
		if tok, _, err = s.db.NewInvite(1); err == nil {
			u, err = s.db.AcceptInvite(tok, name, testPassword)
		}
	}
	if err != nil {
		s.t.Fatal(err)
	}
	ws, err := s.ws.Open(context.Background(), u)
	if err != nil {
		s.t.Fatal(err)
	}
	rec := s.login(name, testPassword, nil)
	c := sessionCookie(rec)
	if rec.Code != 200 || c == nil {
		s.t.Fatalf("login %s: %d %s", name, rec.Code, rec.Body)
	}
	return c, ws
}

func bearerFor(t *testing.T, ws *Workspace) string {
	t.Helper()
	tok, err := ws.Svc.MCPToken(context.Background())
	if err != nil || tok == "" {
		t.Fatalf("token %q: %v", tok, err)
	}
	return "Bearer " + tok
}

const mcpListProjects = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_projects","arguments":{}}}`

var mcpHdr = map[string]string{"Accept": "application/json, text/event-stream"}

func withAuth(hdr map[string]string, authz string) map[string]string {
	m := map[string]string{"Authorization": authz}
	for k, v := range hdr {
		m[k] = v
	}
	return m
}

func TestAnonymousBlocked(t *testing.T) {
	s := newServer(t)
	s.addUser("alice")
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/projects"}, {"GET", "/api/events"}, {"GET", "/api/export"}, {"POST", "/mcp"},
	} {
		if rec := s.do(c.method, c.path, `{}`, nil, mcpHdr); rec.Code != 401 {
			t.Errorf("%s %s: %d, want 401", c.method, c.path, rec.Code)
		}
	}
	if rec := s.do("POST", "/mcp", `{}`, nil, mcpHdr); rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Error("/mcp 401 lacks WWW-Authenticate: Bearer")
	}
	if rec := s.do("GET", "/healthz", "", nil, nil); rec.Code != 200 {
		t.Errorf("/healthz: %d", rec.Code)
	}
	if rec := s.do("GET", "/api/projects", "", nil, map[string]string{"Cookie": "docket_session=forged"}); rec.Code != 401 {
		t.Errorf("forged cookie: %d, want 401", rec.Code)
	}
}

func TestForeignHostBlocked(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/healthz", nil)
	req.Host = "evil.com"
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("foreign host: %d, want 403", rec.Code)
	}
}

func TestMeStates(t *testing.T) {
	s := newServer(t)
	rec := s.do("GET", "/api/auth/me", "", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"setup":true`) || !strings.Contains(rec.Body.String(), `"mode":"selfhost"`) {
		t.Fatalf("empty db: %d %s", rec.Code, rec.Body)
	}
	c, _ := s.addUser("alice")
	if rec := s.do("GET", "/api/auth/me", "", nil, nil); rec.Code != 401 {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	rec = s.do("GET", "/api/auth/me", "", c, nil)
	var me struct {
		Mode string
		User auth.User
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &me) != nil || me.User.Username != "alice" || me.Mode != "selfhost" {
		t.Fatalf("logged in: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginLogout(t *testing.T) {
	s := newServer(t)
	s.addUser("alice")
	rec := s.login("alice", testPassword, nil)
	c := sessionCookie(rec)
	if rec.Code != 200 || c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Secure || c.Path != "/" {
		t.Fatalf("login: %d %+v", rec.Code, c)
	}
	if !strings.Contains(rec.Body.String(), `"username":"alice"`) {
		t.Errorf("login body: %s", rec.Body)
	}
	if rec := s.do("GET", "/api/projects", "", c, nil); rec.Code != 200 {
		t.Fatalf("projects with session: %d %s", rec.Code, rec.Body)
	}
	rec = s.do("POST", "/api/auth/logout", "", c, nil)
	if rec.Code >= 300 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if out := sessionCookie(rec); out == nil || out.MaxAge >= 0 {
		t.Errorf("logout did not expire the cookie: %+v", out)
	}
	if rec := s.do("GET", "/api/projects", "", c, nil); rec.Code != 401 {
		t.Fatalf("after logout: %d", rec.Code)
	}
	rec = s.login("alice", testPassword, map[string]string{"X-Forwarded-Proto": "https"})
	if c := sessionCookie(rec); c == nil || !c.Secure {
		t.Fatalf("https login cookie not Secure: %+v", c)
	}
}

func TestLoginErrorsIdentical(t *testing.T) {
	s := newServer(t)
	s.addUser("alice")
	unknown := s.login("nobody", testPassword, nil)
	wrong := s.login("alice", "wrong-password", nil)
	if unknown.Code != 401 || wrong.Code != 401 || unknown.Body.String() != wrong.Body.String() {
		t.Fatalf("unknown %d %s / wrong %d %s", unknown.Code, unknown.Body, wrong.Code, wrong.Body)
	}
	if !strings.Contains(wrong.Body.String(), `"invalid_credentials"`) {
		t.Errorf("code: %s", wrong.Body)
	}
	if sessionCookie(wrong) != nil {
		t.Error("failed login set a cookie")
	}
}

func TestLoginRateLimit(t *testing.T) {
	s := newServer(t)
	s.addUser("aa")
	s.addUser("bb")
	for i := 0; i < 5; i++ {
		if rec := s.login("aa", "wrong-password", nil); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i+1, rec.Code)
		}
	}
	rec := s.login("aa", testPassword, nil)
	if rec.Code != 429 || !strings.Contains(rec.Body.String(), `"rate_limited"`) || sessionCookie(rec) != nil {
		t.Fatalf("6th attempt: %d %s", rec.Code, rec.Body)
	}
	if rec := s.login("AA ", testPassword, nil); rec.Code != 429 {
		t.Fatalf("case/space variant bypassed the limit: %d", rec.Code)
	}
	if rec := s.login("aa", testPassword, map[string]string{"RemoteAddr": "10.0.0.9:555"}); rec.Code != 200 {
		t.Fatalf("other client: %d", rec.Code)
	}
	if rec := s.login("bb", testPassword, nil); rec.Code != 200 {
		t.Fatalf("user bb: %d %s", rec.Code, rec.Body)
	}
}

func TestIsolation(t *testing.T) {
	s := newServer(t)
	ca, wsA := s.addUser("alice")
	cb, wsB := s.addUser("bob")
	if rec := s.do("POST", "/api/projects", `{"name":"AliceSecret"}`, ca, nil); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("GET", "/api/projects", "", ca, nil); !strings.Contains(rec.Body.String(), "AliceSecret") {
		t.Fatalf("alice cannot see her project: %s", rec.Body)
	}
	for _, path := range []string{"/api/projects", "/api/export", "/api/search?q=AliceSecret"} {
		if rec := s.do("GET", path, "", cb, nil); rec.Code != 200 || strings.Contains(rec.Body.String(), "AliceSecret") {
			t.Fatalf("bob %s: %d %s", path, rec.Code, rec.Body)
		}
	}
	rec := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, bearerFor(t, wsA)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "AliceSecret") {
		t.Fatalf("alice via MCP: %d %s", rec.Code, rec.Body)
	}
	rec = s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, bearerFor(t, wsB)))
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "AliceSecret") {
		t.Fatalf("bob via MCP: %d %s", rec.Code, rec.Body)
	}
}

func TestMcpBearerRouting(t *testing.T) {
	s := newServer(t)
	_, wsA := s.addUser("alice")
	for _, authz := range []string{"", "Bearer unknown", strings.TrimPrefix(bearerFor(t, wsA), "Bearer ")} {
		if rec := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, authz)); rec.Code != 401 {
			t.Errorf("Authorization %q: %d, want 401", authz, rec.Code)
		}
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	if rec := s.do("POST", "/mcp", init, nil, withAuth(mcpHdr, bearerFor(t, wsA))); rec.Code == 401 {
		t.Fatalf("right token refused: %s", rec.Body)
	}
	if rec := s.do("GET", "/mcp", "", nil, map[string]string{"Accept": "text/html"}); rec.Code == 401 {
		t.Fatal("browser navigation to /mcp must reach the app")
	}
}
