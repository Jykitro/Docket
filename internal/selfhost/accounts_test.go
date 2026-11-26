package selfhost

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"plandillo/internal/events"
	"plandillo/internal/service"
	"strings"
	"testing"
	"time"
)

func TestRegistrationModesAndRecovery(t *testing.T) {
	s := newServer(t)
	body := jsonBody("username", "owner", "password", testPassword)
	admin, _ := s.addUser("admin")
	mode := func(m string) {
		t.Helper()
		if r := s.do("PUT", "/api/admin/registration", jsonBody("mode", m), admin, nil); r.Code != 204 {
			t.Fatalf("set %s: %d", m, r.Code)
		}
	}
	expect := func(method, path, body string, c *http.Cookie, status int) {
		t.Helper()
		if r := s.do(method, path, body, c, nil); r.Code != status {
			t.Fatalf("%s %s returned %d, expected %d", method, strings.Split(path, "/invite/")[0], r.Code, status)
		}
	}
	expect("POST", "/api/auth/register", body, nil, 403)
	invite := s.invite(admin)
	mode("closed")
	expect("POST", "/api/auth/invite/"+invite, body, nil, 403)
	expect("POST", "/api/admin/invites", "", admin, 403)
	mode("invite")
	r := s.do("POST", "/api/auth/invite/"+invite, body, nil, nil)
	if r.Code != 200 {
		t.Fatalf("resume invite: %d", r.Code)
	}
	var cred struct {
		Recovery string `json:"recovery_code"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &cred); err != nil || cred.Recovery == "" {
		t.Fatal("missing recovery code")
	}
	owner := sessionCookie(r)
	ws, _ := s.ws.Get(2)
	token := bearerFor(t, ws)
	expect("PUT", "/api/admin/registration", jsonBody("mode", "open"), owner, 403)
	mode("open")
	expect("POST", "/api/auth/register", jsonBody("username", "next", "password", testPassword), nil, 200)
	expect("POST", "/api/auth/invite/"+invite, body, nil, 400)
	expect("PUT", "/api/admin/users/2/blocked", `{"blocked":true}`, admin, 204)
	expect("GET", "/api/projects", "", owner, 401)
	if s.login("owner", testPassword, nil).Code != 401 {
		t.Fatal("blocked login")
	}
	if s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, token)).Code != 401 {
		t.Fatal("blocked MCP")
	}
	reset := jsonBody("username", "owner", "code", cred.Recovery, "password", "new-password-123")
	expect("POST", "/api/auth/recover", reset, nil, 401)
	expect("PUT", "/api/admin/users/2/blocked", `{"blocked":false}`, admin, 204)
	expect("GET", "/api/projects", "", owner, 401)
	if s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, token)).Code != 401 {
		t.Fatal("old MCP token revived")
	}
	before := sessionCookie(s.login("owner", testPassword, nil))
	token = bearerFor(t, ws)
	r = s.do("POST", "/api/auth/recover", reset, nil, nil)
	oldCode := cred.Recovery
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &cred) != nil || cred.Recovery == oldCode || cred.Recovery == "" {
		t.Fatal("recovery did not replace credential")
	}
	expect("POST", "/api/auth/recover", reset, nil, 401)
	expect("GET", "/api/projects", "", before, 401)
	if s.do("POST", "/mcp/code", mcpListProjects, nil, withAuth(mcpHdr, token)).Code != 401 {
		t.Fatal("recovered account accepted previous token")
	}
	if s.login("owner", "new-password-123", nil).Code != 200 {
		t.Fatal("new password failed")
	}
	expect("DELETE", "/api/auth/account", jsonBody("password", testPassword), admin, 403)
	expect("PUT", "/api/admin/users/1/blocked", `{"blocked":true}`, admin, 403)
	if got := s.do("POST", "/api/auth/register", body, nil, map[string]string{"Origin": "http://localhost:9999"}); got.Code != 403 {
		t.Fatal("foreign port origin accepted")
	}
}

func TestOwnerIsolationAcrossChannels(t *testing.T) {
	s := newServer(t)
	a, wa := s.addUser("one")
	b, wb := s.addUser("two")
	for _, fixture := range []struct {
		cookie *http.Cookie
		marker string
	}{{a, "Owner One"}, {b, "Owner Two"}} {
		r := s.do("POST", "/api/projects", jsonBody("name", fixture.marker, "key", "ONE"), fixture.cookie, nil)
		if r.Code != 201 {
			t.Fatalf("project: %d", r.Code)
		}
		r = s.do("POST", "/api/tasks", jsonBody("project", "ONE", "title", fixture.marker), fixture.cookie, nil)
		if r.Code != 201 {
			t.Fatalf("task: %d", r.Code)
		}
		r = s.do("POST", "/api/projects/1/pages", jsonBody("title", fixture.marker, "body_md", fixture.marker), fixture.cookie, nil)
		if r.Code != 201 {
			t.Fatalf("page: %d", r.Code)
		}
	}
	for _, u := range []struct {
		c           *http.Cookie
		ws          *Workspace
		mine, other string
	}{{a, wa, "Owner One", "Owner Two"}, {b, wb, "Owner Two", "Owner One"}} {
		for _, path := range []string{"/api/projects", "/api/tasks", "/api/projects/1/pages", "/api/search?q=Owner", "/api/export"} {
			r := s.do("GET", path, "", u.c, nil)
			if r.Code != 200 || !strings.Contains(r.Body.String(), u.mine) || strings.Contains(r.Body.String(), u.other) {
				t.Fatalf("isolation on %s: %d", path, r.Code)
			}
		}
		r := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, bearerFor(t, u.ws)))
		if r.Code != 200 || !strings.Contains(r.Body.String(), u.mine) || strings.Contains(r.Body.String(), u.other) {
			t.Fatal("classic MCP isolation")
		}
	}
	if r := s.do("DELETE", "/api/tasks/ONE-01", "", a, nil); r.Code != 200 {
		t.Fatalf("task delete: %d", r.Code)
	}
	if r := s.do("GET", "/api/tasks/ONE-01", "", b, nil); r.Code != 200 || !strings.Contains(r.Body.String(), "Owner Two") {
		t.Fatal("owner deletion crossed workspace")
	}
}

func TestRejectOrphanedStorage(t *testing.T) {
	s := newServer(t)
	s.addUser("owner")
	users, err := s.db.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ws.VerifyOwners(context.Background(), users, s.db); err != nil {
		t.Fatal(err)
	}
	if err := s.ws.VerifyOwners(context.Background(), nil, s.db); err == nil {
		t.Fatal("orphan storage accepted")
	}
}

func TestProvisioningRetry(t *testing.T) {
	s := newServer(t)
	if s.ws.postgresDSN != "" {
		t.Skip("SQLite filesystem fault")
	}
	admin, _ := s.addUser("admin")
	if err := os.Mkdir(s.ws.path(2), 0700); err != nil {
		t.Fatal(err)
	}
	token := s.invite(admin)
	r := s.do("POST", "/api/auth/invite/"+token, jsonBody("username", "retry", "password", testPassword), nil, nil)
	if r.Code != 500 || sessionCookie(r) != nil {
		t.Fatal("failed provisioning issued a session")
	}
	if err := os.Remove(s.ws.path(2)); err != nil {
		t.Fatal(err)
	}
	c := sessionCookie(s.login("retry", testPassword, nil))
	if c == nil {
		t.Fatal("login failed to retry provisioning")
	}
	if r := s.do("GET", "/api/projects", "", c, nil); r.Code != 200 {
		t.Fatal("reopened workspace unavailable")
	}
	n, err := s.db.UserCount()
	if err != nil || n != 2 {
		t.Fatalf("unexpected user count: %d", n)
	}
}

func TestBlockingCancelsInFlightMCPAndSSE(t *testing.T) {
	ready := make(chan struct{}, 2)
	s := newServerWith(t, func(context.Context, *service.Service, *events.Bus) http.Handler {
		return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ready <- struct{}{}; <-r.Context().Done() })
	})
	admin, _ := s.addUser("admin")
	owner, ws := s.addUser("owner")
	token := bearerFor(t, ws)
	done := make(chan struct{}, 2)
	go func() { s.do("GET", "/api/events", "", owner, nil); done <- struct{}{} }()
	go func() { s.do("POST", "/mcp/code", "{}", nil, withAuth(mcpHdr, token)); done <- struct{}{} }()
	for range 2 {
		select {
		case <-ready:
		case <-time.After(time.Second):
			t.Fatal("requests did not reach workspace")
		}
	}
	if r := s.do("PUT", "/api/admin/users/2/blocked", `{"blocked":true}`, admin, nil); r.Code != 204 {
		t.Fatalf("block: %d", r.Code)
	}
	for range 2 {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("request survived block")
		}
	}
}

func TestSSESelectsOwnersBus(t *testing.T) {
	s := newServer(t)
	_, a := s.addUser("alice")
	c, b := s.addUser("bob")
	srv := httptest.NewServer(s.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(c)
	response, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	lines := bufio.NewScanner(response.Body)
	if !lines.Scan() || lines.Text() != ": connected" {
		t.Fatal("stream did not subscribe")
	}
	if _, err = a.Svc.CreateProject(ctx, "agent-alice", service.ProjectInput{Name: "Project A"}); err != nil {
		t.Fatal(err)
	}
	if _, err = b.Svc.CreateProject(ctx, "agent-bob", service.ProjectInput{Name: "Project B"}); err != nil {
		t.Fatal(err)
	}
	for lines.Scan() {
		if strings.HasPrefix(lines.Text(), "data: ") {
			if !strings.Contains(lines.Text(), `"actor":"agent-bob"`) {
				t.Fatal("private event crossed workspaces")
			}
			return
		}
	}
	t.Fatal("owner's event did not arrive")
}

func TestInterruptedSelfDeletionChecksCatalogBeforeCleanup(t *testing.T) {
	s := newServer(t)
	s.addUser("admin")
	_, owner := s.addUser("owner")
	confirmation, err := owner.confirmDeletion(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.DeleteUser(owner.ID, confirmation); err != nil {
		t.Fatal(err)
	}
	users, err := s.db.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	s.ws.Close()
	ctx := context.Background()
	if err = s.ws.VerifyOwners(ctx, nil, s.db); err == nil {
		t.Fatal("incomplete catalog accepted")
	}
	pending, err := s.db.PendingDeletions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatal("partial catalog changed pending deletion")
	}
	if err = s.ws.VerifyOwners(ctx, users, s.db); err != nil {
		t.Fatal(err)
	}
	pending, err = s.db.PendingDeletions(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatal("completed deletion was not acknowledged")
	}
}
