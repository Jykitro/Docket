package selfhost

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"docket/internal/auth"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/service"
	"docket/internal/testutil"
	"github.com/go-webauthn/webauthn/webauthn"
)

func clusterReplica(t *testing.T, dsn string) (*server, *Cluster) {
	t.Helper()
	ctx := t.Context()
	db, err := auth.OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cluster, err := OpenCluster(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	ws := NewWorkspaces(ctx, t.TempDir(), time.UTC, func(_ context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: hosts, SelfHost: true, MCP: mcpserver.Handler(svc, time.UTC), Web: http.NotFoundHandler()})
	})
	ws.UsePostgres(dsn)
	if err = cluster.Initialize(ctx, ws, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ws.Close)
	h := Handler(Options{Cluster: cluster, WS: ws, Auth: db, AllowedHosts: hosts, SetupCode: testSetupCode, Web: http.NotFoundHandler()})
	return &server{t: t, h: h, ws: ws, db: db}, cluster
}

func TestClusterReplicas(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	a, ca := clusterReplica(t, dsn)
	b, cb := clusterReplica(t, dsn)
	setup := a.do("POST", "/api/auth/setup", `{"username":"admin","password":"password-123","code":"setup-code-xyz"}`, nil, nil)
	cookie := sessionCookie(setup)
	if setup.Code != 200 || cookie == nil {
		t.Fatalf("setup HTTP %d", setup.Code)
	}
	ws, ok := a.ws.Get(1)
	if !ok {
		t.Fatal("missing workspace")
	}
	old := bearerFor(t, ws)
	mcpStatus := func(s *server, token string) int {
		return s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, token)).Code
	}
	if got := mcpStatus(b, old); got != 200 {
		t.Fatalf("new account on other replica: %d", got)
	}
	rot := b.do("POST", "/api/mcp/token/rotate", "{}", cookie, nil)
	var token service.TokenInfo
	if rot.Code != 200 || json.Unmarshal(rot.Body.Bytes(), &token) != nil || token.Token == "" {
		t.Fatal("rotation failed")
	}
	if mcpStatus(a, old) != 401 || mcpStatus(a, "Bearer "+token.Token) != 200 {
		t.Fatal("stale token routing after rotation")
	}

	// A stream connected to B must observe a write on A and terminate after logout on A.
	srv := httptest.NewServer(b.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	r.AddCookie(cookie)
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("stream HTTP %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	if line, _ := reader.ReadString('\n'); line != ": connected\n" {
		t.Fatal("stream did not open")
	}
	if rec := a.do("POST", "/api/projects", `{"name":"Shared board"}`, cookie, nil); rec.Code != 201 {
		t.Fatalf("create project: %d", rec.Code)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			if !strings.Contains(line, "project_changed") {
				t.Fatalf("unexpected event %s", line)
			}
			break
		}
	}
	if rec := a.do("POST", "/api/auth/logout", "{}", cookie, nil); rec.Code != 204 {
		t.Fatalf("logout: %d", rec.Code)
	}
	if _, err = io.Copy(io.Discard, reader); err != nil {
		t.Fatal("stream failed to end normally after remote revocation:", err)
	}
	if b.do("GET", "/api/projects", "", cookie, nil).Code != 401 {
		t.Fatal("revoked session accepted")
	}

	for i, s := range []*server{a, b, a, b, a, b} {
		want := 401
		if i == 5 {
			want = 429
		}
		if got := s.login("missing", "wrong", nil).Code; got != want {
			t.Fatalf("shared login attempt %d: got %d want %d", i, got, want)
		}
	}
	sites, err := PasskeySites(hosts, ":7777", "")
	if err != nil {
		t.Fatal(err)
	}
	site := sites["http://localhost:7777"]
	first, second := passkeyRequests{db: ca.DB, auth: a.db}, passkeyRequests{db: cb.DB, auth: b.db}
	req := httptest.NewRequest("POST", "http://localhost:7777/", nil)
	rec := httptest.NewRecorder()
	first.begin(rec, req, passkeyRequest{site: site, session: &webauthn.SessionData{Challenge: "cross-replica"}}, nil)
	if rec.Code != 200 {
		t.Fatal("begin challenge", rec.Code)
	}
	req.AddCookie(rec.Result().Cookies()[0])
	got, ok := second.take(httptest.NewRecorder(), req, site)
	if !ok || got.session.Challenge != "cross-replica" {
		t.Fatal("challenge not available on second replica")
	}
	if _, ok = first.take(httptest.NewRecorder(), req, site); ok {
		t.Fatal("challenge was reused")
	}
}

func TestClusterAccountChangesReachOtherReplica(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	a, _ := clusterReplica(t, dsn)
	b, _ := clusterReplica(t, dsn)
	setup := a.do("POST", "/api/auth/setup", `{"username":"admin","password":"password-123","code":"setup-code-xyz"}`, nil, nil)
	admin := sessionCookie(setup)
	if admin == nil {
		t.Fatal("admin setup failed")
	}
	invite := a.do("POST", "/api/admin/invites", "{}", admin, nil)
	var issued struct{ URL string }
	if invite.Code != 201 || json.Unmarshal(invite.Body.Bytes(), &issued) != nil || !strings.HasPrefix(issued.URL, "/invite/") {
		t.Fatal("invite failed")
	}
	reg := b.do("POST", "/api/auth"+issued.URL, `{"username":"bob","password":"password-123"}`, nil, nil)
	bob := sessionCookie(reg)
	if reg.Code != 200 || bob == nil {
		t.Fatalf("signup on replica B: %d", reg.Code)
	}
	member, ok := b.ws.Get(2)
	if !ok {
		t.Fatal("new workspace missing")
	}
	original := bearerFor(t, member)
	mcpStatus := func(s *server, token string) int {
		return s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, token)).Code
	}
	if mcpStatus(a, original) != 200 {
		t.Fatal("new member unavailable on A")
	}
	block := a.do("PUT", "/api/admin/users/2/blocked", `{"blocked":true}`, admin, nil)
	if block.Code != 204 || mcpStatus(b, original) != 401 || b.do("GET", "/api/projects", "", bob, nil).Code != 401 {
		t.Fatal("member access survived remote block")
	}
	if got := b.do("PUT", "/api/admin/users/2/blocked", `{"blocked":false}`, admin, nil).Code; got != 204 {
		t.Fatal("unblock failed", got)
	}
	fresh := bearerFor(t, member)
	if fresh == original || mcpStatus(a, original) != 401 || mcpStatus(a, fresh) != 200 {
		t.Fatal("unblock failed to rotate member credentials")
	}
	bob = sessionCookie(b.login("bob", testPassword, nil))
	if bob == nil {
		t.Fatal("login after unblock")
	}
	if got := a.do("DELETE", "/api/auth/account", `{"password":"password-123"}`, bob, nil).Code; got != 204 {
		t.Fatalf("delete account: %d", got)
	}
	if mcpStatus(b, fresh) != 401 || b.do("GET", "/api/projects", "", bob, nil).Code != 401 {
		t.Fatal("member access survived remote deletion")
	}
	if _, ok = b.ws.Get(2); ok {
		t.Fatal("deleted workspace still cached")
	}
	if a.do("GET", "/api/projects", "", admin, nil).Code != 200 {
		t.Fatal("other workspace affected")
	}
}
