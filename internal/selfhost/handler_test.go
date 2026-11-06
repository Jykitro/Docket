package selfhost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"docket/internal/auth"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/service"
)

const (
	testPassword  = "password-123"
	testSetupCode = "setup-code-xyz"
)

type server struct {
	t  *testing.T
	h  http.Handler
	db *auth.DB
	ws *Workspaces
}

var hosts = []string{"localhost", "127.0.0.1"}

func newServer(t *testing.T) *server {
	return newServerWith(t, func(_ context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: hosts,
			MCP: mcpserver.Handler(svc, time.UTC), Web: http.NotFoundHandler()})
	})
}

// newServerWith uses build for every user's router.
func newServerWith(t *testing.T, build func(context.Context, *service.Service, *events.Bus) http.Handler) *server {
	t.Helper()
	dir := t.TempDir()
	db, err := auth.Open(filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ws := NewWorkspaces(context.Background(), dir, time.UTC, build)
	t.Cleanup(ws.Close)
	h := Handler(Options{Auth: db, WS: ws, Web: http.NotFoundHandler(), AllowedHosts: hosts, SetupCode: testSetupCode})
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

func TestPathVariantsNeverReachUserRouter(t *testing.T) {
	s := newServer(t)
	_, _ = s.addUser("alice")
	cb, _ := s.addUser("bob")
	// Bob has a project, so a request that slipped through to his router would return project JSON.
	if rec := s.do("POST", "/api/projects", `{"name":"Leak"}`, cb, nil); rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{"/api/admin", "/api/admin/", "/api/admin/users/3", "/api//admin/users",
		"/api/../api/projects", "//api/projects", "/API/projects", "/api/projects/"} {
		rec := s.do("GET", path, "", nil, nil)
		if (rec.Code != 401 && rec.Code != 403 && rec.Code != 404) || strings.Contains(rec.Body.String(), `"slug"`) {
			t.Errorf("anonymous %s: %d %s", path, rec.Code, rec.Body)
		}
	}
	for _, path := range []string{"/api/admin", "/api/admin/"} {
		if rec := s.do("GET", path, "", cb, nil); (rec.Code != 403 && rec.Code != 404) || strings.Contains(rec.Body.String(), `"slug"`) {
			t.Errorf("non-admin %s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestLoginOriginGuard(t *testing.T) {
	s := newServer(t)
	s.addUser("alice")
	body := jsonBody("username", "alice", "password", testPassword)
	rec := s.do("POST", "/api/auth/login", body, nil, map[string]string{"Origin": "https://evil.example"})
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"forbidden_origin"`) || sessionCookie(rec) != nil {
		t.Fatalf("cross-origin login: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("POST", "/api/auth/login", body, nil, map[string]string{"Origin": "http://localhost"}); rec.Code != 200 {
		t.Fatalf("same-origin login: %d %s", rec.Code, rec.Body)
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
	s.addUser("cc")
	for i := 0; i < 4; i++ {
		s.login("cc", "wrong-password", nil)
	}
	if rec := s.login("cc", testPassword, nil); rec.Code != 200 {
		t.Fatalf("correct login after 4 failures: %d", rec.Code)
	}
	if rec := s.login("cc", "wrong-password", nil); rec.Code != 401 {
		t.Fatalf("success did not reset the limit: %d", rec.Code)
	}
}

func TestLoginOverlongUsername(t *testing.T) {
	s := newServer(t)
	s.addUser("alice")
	wrong := s.login("alice", "wrong-password", nil)
	long := strings.Repeat("a", 1000)
	// The same name 20 times: through the limiter the 6th would be a 429, so all-401 shows it is not counted.
	for i := 0; i < 20; i++ {
		if rec := s.login(long, testPassword, nil); rec.Code != 401 || rec.Body.String() != wrong.Body.String() {
			t.Fatalf("attempt %d: %d %s, want %d %s", i+1, rec.Code, rec.Body, wrong.Code, wrong.Body)
		}
	}
	if rec := s.login("alice", testPassword, nil); rec.Code != 200 {
		t.Fatalf("real login after over-long names: %d %s", rec.Code, rec.Body)
	}
}

func TestLoginRateLimitParallel(t *testing.T) {
	s := newServer(t)
	s.addUser("aa")
	const n = 20
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"aa","password":"wrong-password"}`))
			req.Host = "localhost"
			req.RemoteAddr = "127.0.0.1:1234"
			rec := httptest.NewRecorder()
			s.h.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}()
	}
	wg.Wait()
	var unauthorized, limited int
	for _, c := range codes {
		switch c {
		case 401:
			unauthorized++
		case 429:
			limited++
		}
	}
	if unauthorized != 5 || limited != n-5 {
		t.Fatalf("401=%d 429=%d of %d: %v", unauthorized, limited, n, codes)
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

func TestMcpDuplicateTokenRefused(t *testing.T) {
	s := newServer(t)
	_, wsA := s.addUser("alice")
	_, wsB := s.addUser("bob")
	_, wsC := s.addUser("carol")
	if rec := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, bearerFor(t, wsC))); rec.Code != 200 {
		t.Fatalf("distinct token: %d %s", rec.Code, rec.Body)
	}
	none := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, "Bearer nope"))
	wsA.Svc.SetEnvToken("shared-token")
	wsB.Svc.SetEnvToken("shared-token")
	rec := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, "Bearer shared-token"))
	if rec.Code != 401 || rec.Body.String() != none.Body.String() {
		t.Fatalf("shared token: %d %s, want 401 %s", rec.Code, rec.Body, none.Body)
	}
	if rec := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, bearerFor(t, wsC))); rec.Code != 200 {
		t.Fatalf("distinct token after clash: %d %s", rec.Code, rec.Body)
	}
}

func jsonBody(kv ...string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestSetupFlow(t *testing.T) {
	s := newServer(t)
	rec := s.do("POST", "/api/auth/setup", jsonBody("code", "wrong", "username", "alice", "password", testPassword), nil, nil)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"forbidden"`) || sessionCookie(rec) != nil {
		t.Fatalf("wrong code: %d %s", rec.Code, rec.Body)
	}
	if n, _ := s.db.UserCount(); n != 0 {
		t.Fatalf("wrong code created %d users", n)
	}
	rec = s.do("POST", "/api/auth/setup", jsonBody("code", testSetupCode, "username", "alice", "password", testPassword), nil, nil)
	c := sessionCookie(rec)
	if rec.Code != 200 || c == nil || !strings.Contains(rec.Body.String(), `"is_admin":true`) {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("GET", "/api/admin/users", "", c, nil); rec.Code != 200 {
		t.Fatalf("admin cookie: %d %s", rec.Code, rec.Body)
	}
	if _, ok := s.ws.Get(1); !ok {
		t.Error("workspace not opened")
	}
	rec = s.do("POST", "/api/auth/setup", jsonBody("code", testSetupCode, "username", "mallory", "password", testPassword), nil, nil)
	if rec.Code != 404 || sessionCookie(rec) != nil {
		t.Fatalf("second setup: %d %s", rec.Code, rec.Body)
	}
}

func TestSetupEmptyCodeRefuses(t *testing.T) {
	s := newServer(t)
	h := Handler(Options{Auth: s.db, WS: s.ws, Web: http.NotFoundHandler(), AllowedHosts: []string{"localhost"}})
	for _, body := range []string{
		jsonBody("code", "", "username", "alice", "password", testPassword),
		jsonBody("username", "alice", "password", testPassword),
	} {
		req := httptest.NewRequest("POST", "/api/auth/setup", strings.NewReader(body))
		req.Host = "localhost"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("empty configured code: %d %s", rec.Code, rec.Body)
		}
	}
	if n, _ := s.db.UserCount(); n != 0 {
		t.Fatalf("created %d users", n)
	}
}

// invite has the admin create an invite and returns its token.
func (s *server) invite(admin *http.Cookie) string {
	s.t.Helper()
	rec := s.do("POST", "/api/admin/invites", "", admin, nil)
	var out struct {
		URL       string `json:"url"`
		ExpiresAt string `json:"expires_at"`
	}
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || !strings.HasPrefix(out.URL, "/invite/") {
		s.t.Fatalf("create invite: %d %s", rec.Code, rec.Body)
	}
	if _, err := time.Parse(time.RFC3339, out.ExpiresAt); err != nil || !strings.Contains(rec.Body.String(), `"expires_at"`) {
		s.t.Fatalf("expires_at: %s", rec.Body)
	}
	return strings.TrimPrefix(out.URL, "/invite/")
}

func TestInviteFlow(t *testing.T) {
	s := newServer(t)
	admin, _ := s.addUser("alice")
	tok := s.invite(admin)
	if rec := s.do("GET", "/api/admin/invites", "", admin, nil); rec.Code != 200 || strings.Count(rec.Body.String(), `"expires_at"`) != 1 || strings.Contains(rec.Body.String(), tok) {
		t.Fatalf("list invites: %d %s", rec.Code, rec.Body)
	}
	rec := s.do("POST", "/api/auth/invite/"+tok, jsonBody("username", "bob", "password", testPassword), nil, nil)
	c := sessionCookie(rec)
	if rec.Code != 200 || c == nil || !strings.Contains(rec.Body.String(), `"is_admin":false`) {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("GET", "/api/projects", "", c, nil); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("new user projects: %d %s", rec.Code, rec.Body)
	}
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/admin/users"}, {"GET", "/api/admin/invites"}, {"POST", "/api/admin/invites"}, {"DELETE", "/api/admin/users/1"},
	} {
		if rec := s.do(r.method, r.path, "", c, nil); rec.Code != 403 || !strings.Contains(rec.Body.String(), `"forbidden"`) {
			t.Errorf("non-admin %s %s: %d %s", r.method, r.path, rec.Code, rec.Body)
		}
		if rec := s.do(r.method, r.path, "", nil, nil); rec.Code != 401 {
			t.Errorf("anonymous %s %s: %d", r.method, r.path, rec.Code)
		}
	}
	if rec := s.do("GET", "/api/admin/nope", "", nil, nil); rec.Code != 401 {
		t.Errorf("anonymous unknown admin path: %d", rec.Code)
	}
	if rec := s.do("GET", "/api/admin/users", "", admin, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"bob"`) {
		t.Fatalf("admin users: %d %s", rec.Code, rec.Body)
	}
}

func TestInviteReuseAndExpiry(t *testing.T) {
	s := newServer(t)
	admin, _ := s.addUser("alice")
	tok := s.invite(admin)
	if rec := s.do("POST", "/api/auth/invite/"+tok, jsonBody("username", "bob", "password", testPassword), nil, nil); rec.Code != 200 {
		t.Fatalf("first accept: %d %s", rec.Code, rec.Body)
	}
	reused := s.do("POST", "/api/auth/invite/"+tok, jsonBody("username", "carol", "password", testPassword), nil, nil)
	garbage := s.do("POST", "/api/auth/invite/garbage", jsonBody("username", "carol", "password", testPassword), nil, nil)
	for _, rec := range []*httptest.ResponseRecorder{reused, garbage} {
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"validation"`) || sessionCookie(rec) != nil {
			t.Fatalf("bad token: %d %s", rec.Code, rec.Body)
		}
	}
	if reused.Body.String() != garbage.Body.String() {
		t.Errorf("used and unknown tokens are distinguishable: %s / %s", reused.Body, garbage.Body)
	}
	// A taken username and a weak password do not burn the invite.
	tok = s.invite(admin)
	if rec := s.do("POST", "/api/auth/invite/"+tok, jsonBody("username", "alice", "password", testPassword), nil, nil); rec.Code != 400 || !strings.Contains(rec.Body.String(), "username is taken") {
		t.Fatalf("taken: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("POST", "/api/auth/invite/"+tok, jsonBody("username", "carol", "password", "short"), nil, nil); rec.Code != 400 || !strings.Contains(rec.Body.String(), "password must be") {
		t.Fatalf("weak password: %d %s", rec.Code, rec.Body)
	}
}

func TestAdminDeleteUser(t *testing.T) {
	s := newServer(t)
	ca, _ := s.addUser("alice")
	cb, wsB := s.addUser("bob")
	if rec := s.do("GET", "/api/projects", "", cb, nil); rec.Code != 200 {
		t.Fatalf("bob before: %d", rec.Code)
	}
	file := s.ws.path(wsB.ID)
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{"/api/admin/users/1": 400, "/api/admin/users/abc": 400, "/api/admin/users/99": 404} {
		if rec := s.do("DELETE", path, "", ca, nil); rec.Code != want {
			t.Errorf("DELETE %s: %d %s, want %d", path, rec.Code, rec.Body, want)
		}
	}
	if rec := s.do("DELETE", "/api/admin/users/"+strconv.FormatInt(wsB.ID, 10), "", ca, nil); rec.Code != 204 {
		t.Fatalf("delete bob: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("GET", "/api/projects", "", cb, nil); rec.Code != 401 {
		t.Fatalf("bob's old cookie: %d, want 401", rec.Code)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bob's database still there: %v", err)
	}
	if _, ok := s.ws.Get(wsB.ID); ok {
		t.Error("bob's workspace still open")
	}
	if rec := s.do("GET", "/api/projects", "", ca, nil); rec.Code != 200 {
		t.Fatalf("alice after: %d", rec.Code)
	}
}

func TestChangePasswordEndpoint(t *testing.T) {
	s := newServer(t)
	c, _ := s.addUser("alice")
	other := sessionCookie(s.login("alice", testPassword, nil))
	const newPw = "a-brand-new-password"
	if rec := s.do("PUT", "/api/auth/password", jsonBody("old", testPassword, "new", newPw), nil, nil); rec.Code != 401 {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	rec := s.do("PUT", "/api/auth/password", jsonBody("old", "wrong-password", "new", newPw), c, nil)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"invalid_credentials"`) {
		t.Fatalf("wrong old: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("PUT", "/api/auth/password", jsonBody("old", testPassword, "new", "short"), c, nil); rec.Code != 400 || !strings.Contains(rec.Body.String(), `"validation"`) {
		t.Fatalf("short new: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("PUT", "/api/auth/password", jsonBody("old", testPassword, "new", newPw), c, nil); rec.Code != 204 {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("GET", "/api/projects", "", other, nil); rec.Code != 401 {
		t.Fatalf("other session after change: %d, want 401", rec.Code)
	}
	if rec := s.login("alice", newPw, nil); rec.Code != 200 {
		t.Fatalf("login with new: %d %s", rec.Code, rec.Body)
	}
	if rec := s.login("alice", testPassword, nil); rec.Code != 401 {
		t.Fatalf("login with old: %d", rec.Code)
	}
	if rec := s.do("GET", "/api/projects", "", c, nil); rec.Code != 200 {
		t.Fatalf("current session after change: %d", rec.Code)
	}
}

// streamServer's user routers answer GET /api/events by blocking until the request context ends.
func streamServer(t *testing.T) *server {
	return newServerWith(t, func(context.Context, *service.Service, *events.Bus) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/events" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(200)
			<-r.Context().Done()
		})
	})
}

// stream opens /api/events with c in the background; the channel closes when the request returns.
func (s *server) stream(c *http.Cookie) <-chan struct{} {
	done := make(chan struct{})
	req := httptest.NewRequest("GET", "/api/events", nil)
	req.Host = "localhost"
	req.RemoteAddr = "127.0.0.1:1234"
	req.AddCookie(c)
	go func() {
		defer close(done)
		s.h.ServeHTTP(httptest.NewRecorder(), req)
	}()
	select { // a stream that ends at once (e.g. 401) is caught here
	case <-done:
		s.t.Fatal("stream returned immediately")
	case <-time.After(100 * time.Millisecond):
	}
	return done
}

func ended(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: stream still open", what)
	}
}

func TestLogoutEndsStream(t *testing.T) {
	s := streamServer(t)
	c, _ := s.addUser("alice")
	done := s.stream(c)
	if rec := s.do("POST", "/api/auth/logout", "", c, nil); rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
	ended(t, done, "after logout")
}

func TestDeleteUserEndsStream(t *testing.T) {
	s := streamServer(t)
	admin, _ := s.addUser("alice")
	cb, wsB := s.addUser("bob")
	done := s.stream(cb)
	if rec := s.do("DELETE", "/api/admin/users/"+strconv.FormatInt(wsB.ID, 10), "", admin, nil); rec.Code != 204 {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	ended(t, done, "after user deletion")
}

func TestPasswordChangeEndsOtherStreams(t *testing.T) {
	s := streamServer(t)
	c, _ := s.addUser("alice")
	other := sessionCookie(s.login("alice", testPassword, nil))
	mine, theirs := s.stream(c), s.stream(other)
	if rec := s.do("PUT", "/api/auth/password", jsonBody("old", testPassword, "new", "a-brand-new-password"), c, nil); rec.Code != 204 {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	ended(t, theirs, "other session after password change")
	select {
	case <-mine:
		t.Fatal("current session's stream ended")
	case <-time.After(200 * time.Millisecond):
	}
	s.do("POST", "/api/auth/logout", "", c, nil)
	ended(t, mine, "current session after logout")
}

func TestStreamsRelease(t *testing.T) {
	var s streams
	_, done := s.track(context.Background(), "a")
	ctx, done2 := s.track(context.Background(), "b")
	done()
	s.end("b")
	if ctx.Err() == nil {
		t.Fatal("end did not cancel")
	}
	done2()
	if len(s.m) != 0 {
		t.Fatalf("entries left: %v", s.m)
	}
}
