package selfhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"docket/internal/auth"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/service"
	"docket/internal/store"
)

// Use the production shared catalog with the ordinary HTTP and MCP entrypoints.
func sharedServer(t *testing.T) *server {
	t.Helper()
	s := newServerWith(t, func(ctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		// Run a fixed list_projects callback. The subprocess protocol is exercised in runner tests.
		mcp := mcpserver.WithEvaluator(svc, time.UTC, func(_ context.Context, _ string, call func(string, json.RawMessage) (string, error)) (string, error) {
			return call("list_projects", json.RawMessage("{}"))
		})
		return httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: hosts, MCP: mcp, Web: http.NotFoundHandler()})
	})
	base, err := store.Shared(s.db.Connection(), s.ws.dir+"/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	s.ws.UseShared(base, "", 0)
	s.h = Handler(Options{Auth: s.db, WS: s.ws, Web: http.NotFoundHandler(), AllowedHosts: hosts, SetupCode: testSetupCode})
	return s
}
func TestSharedWorkspaceLifecycleAndIsolation(t *testing.T) {
	s := sharedServer(t)
	a, first := s.addUser("alice")
	b, other := s.addUser("bob")
	status := func(method, path, body string, c *http.Cookie, code int) []byte {
		t.Helper()
		res := s.do(method, path, body, c, nil)
		if res.Code != code {
			t.Fatalf("%s %s HTTP %d, want %d", method, path, res.Code, code)
		}
		return res.Body.Bytes()
	}
	var second store.Space
	if err := json.Unmarshal(status("POST", "/api/workspaces", `{"name":"Second"}`, a, 201), &second); err != nil {
		t.Fatal(err)
	}
	prefix := func(id int64) string { return fmt.Sprintf("/api/workspaces/%d", id) }
	target := prefix(second.ID)
	status("PATCH", prefix(other.ID), `{"name":"wrong"}`, a, 404)
	status("GET", target+"/projects", "", b, 404)
	for _, sp := range []struct {
		id   int64
		name string
		c    *http.Cookie
	}{{first.ID, "First unique", a}, {second.ID, "Second unique", a}, {other.ID, "Bob unique", b}} {
		path := prefix(sp.id)
		project := status("POST", path+"/projects", fmt.Sprintf(`{"name":%q,"key":"APP","slug":"common"}`, sp.name), sp.c, 201)
		var p store.Project
		if err := json.Unmarshal(project, &p); err != nil {
			t.Fatal(err)
		}
		status("POST", path+"/tasks", fmt.Sprintf(`{"title":%q,"project_id":%d}`, sp.name, p.ID), sp.c, 201)
		status("POST", fmt.Sprintf("%s/projects/%d/pages", path, p.ID), fmt.Sprintf(`{"title":"Guide","body_md":%q}`, sp.name), sp.c, 201)
	}
	var ps []store.Project
	if err := json.Unmarshal(status("GET", target+"/projects", "", a, 200), &ps); err != nil || len(ps) != 1 || ps[0].Name != "Second unique" {
		t.Fatal("project listing escaped workspace", err)
	}
	for _, route := range []string{"/tasks/APP-01", "/search?q=unique"} {
		payload := status("GET", target+route, "", a, 200)
		if !bytes.Contains(payload, []byte("Second unique")) || bytes.Contains(payload, []byte("First unique")) || bytes.Contains(payload, []byte("Bob unique")) {
			t.Fatalf("workspace scope for %s", route)
		}
	}
	var foreign []store.Project
	_ = json.Unmarshal(status("GET", prefix(other.ID)+"/projects", "", b, 200), &foreign)
	status("PATCH", target+"/projects/"+fmt.Sprint(foreign[0].ID), `{"name":"wrong"}`, a, 404)
	data := status("GET", target+"/export", "", a, 200)
	if !bytes.Contains(data, []byte("Second unique")) || bytes.Contains(data, []byte("First unique")) || bytes.Contains(data, []byte("Bob unique")) {
		t.Fatal("workspace export scope")
	}
	sp, err := s.ws.shared.Space(t.Context(), first.OwnerID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.ws.openSpace(t.Context(), sp, true)
	if err != nil {
		t.Fatal(err)
	}
	bearer := bearerFor(t, ws)
	for _, path := range []string{"/mcp", "/mcp/code"} {
		body := mcpListProjects
		if path == "/mcp/code" {
			body = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code","arguments":{"code":"return await codemode.list_projects({})"}}}`
		}
		rec := s.do("POST", path, body, nil, withAuth(mcpHdr, bearer))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Second unique") || strings.Contains(rec.Body.String(), "First unique") {
			t.Fatalf("MCP scope at %s, status %d", path, rec.Code)
		}
	}
	status("PATCH", target, `{"action":"archive"}`, a, 200)
	if res := s.do("POST", "/mcp", mcpListProjects, nil, withAuth(mcpHdr, bearer)); res.Code != 401 {
		t.Fatal("archived bearer accepted")
	}
	status("PATCH", prefix(first.ID), `{"action":"archive"}`, a, 409)
	status("PATCH", target, `{"action":"restore"}`, a, 200)
	status("GET", target+"/projects", "", a, 200)
	status("PATCH", target, `{"action":"archive"}`, a, 200)
	status("DELETE", target, `{"name":"other","password":"password-123"}`, a, 400)
	status("DELETE", target, `{"name":"Second","password":"password-123"}`, a, 204)
	status("GET", target+"/projects", "", a, 404)
	if !bytes.Contains(status("GET", prefix(first.ID)+"/projects", "", a, 200), []byte("First unique")) {
		t.Fatal("sibling lost")
	}
}
func TestSharedConcurrentQuota(t *testing.T) {
	s := sharedServer(t)
	if !s.db.Connection().Postgres {
		t.Skip("hosted demo requires PostgreSQL")
	}
	_, _ = s.addUser("admin")
	cookie, one := s.addUser("member")
	name := "Another"
	record, err := s.ws.shared.ChangeSpace(t.Context(), one.OwnerID, 0, store.SpaceChange{Name: &name}, 0)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.ws.openSpace(t.Context(), record, false)
	if err != nil {
		t.Fatal(err)
	}
	s.ws.DemoStorageBytes = 200000
	work := make(chan error, 2)
	for _, ws := range []*Workspace{one, two} {
		ws.Svc.SetStorageLimit(200000)
		p, err := ws.Svc.CreateProject(t.Context(), "mcp", service.ProjectInput{Name: "Demo", Key: "DE"})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			_, err := ws.Svc.CreateTasks(context.Background(), "mcp", []service.NewTask{{ProjectID: p.ID, Title: "payload", DescriptionMD: strings.Repeat("x", 130000)}})
			work <- err
		}()
	}
	var fits, capped int
	for range 2 {
		select {
		case err := <-work:
			if err == nil {
				fits++
			} else if service.ErrorCode(err) == "quota_exceeded" {
				capped++
			} else {
				t.Fatal(err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("quota lock timeout")
		}
	}
	if fits != 1 || capped != 1 {
		t.Fatalf("accepted %d capped %d", fits, capped)
	}
	if s.do("GET", "/api/workspaces", "", cookie, nil).Code != 200 {
		t.Fatal("quota blocked account management")
	}
}
func TestDeviceRevocationIsOwnerScoped(t *testing.T) {
	s := sharedServer(t)
	c, _ := s.addUser("one")
	other, _ := s.addUser("two")
	var devices []auth.DeviceSession
	res := s.do("GET", "/api/auth/sessions", "", c, nil)
	if err := json.Unmarshal(res.Body.Bytes(), &devices); err != nil || len(devices) != 1 || !devices[0].Current {
		t.Fatal("current session missing", err)
	}
	path := "/api/auth/sessions/" + devices[0].ID
	if s.do("DELETE", path, "", other, nil).Code != 404 || s.do("DELETE", path, "", c, nil).Code != 204 || s.do("GET", "/api/workspaces", "", c, nil).Code != 401 {
		t.Fatal("session revocation failed")
	}
}

func TestSharedSQLiteBackupRestoresOwnerAndContent(t *testing.T) {
	s := sharedServer(t)
	if s.db.Connection().Postgres {
		t.Skip("SQLite file backup")
	}
	cookie, _ := s.addUser("owner")
	var second store.Space
	res := s.do("POST", "/api/workspaces", `{"name":"Research"}`, cookie, nil)
	if res.Code != 201 || json.Unmarshal(res.Body.Bytes(), &second) != nil {
		t.Fatal("create workspace", res.Code)
	}
	path := fmt.Sprintf("/api/workspaces/%d/projects", second.ID)
	if r := s.do("POST", path, `{"name":"Restorable","key":"AA"}`, cookie, nil); r.Code != 201 {
		t.Fatal("create project", r.Code)
	}
	backup, err := s.ws.shared.Backup(t.TempDir(), 2, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := auth.Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	u, err := restored.Authenticate("owner", testPassword)
	if err != nil {
		t.Fatal("restore credentials", err)
	}
	catalog, err := store.Shared(restored.Connection(), backup)
	if err != nil {
		t.Fatal(err)
	}
	spaces, err := catalog.Spaces(t.Context(), u.ID)
	if err != nil || len(spaces) != 2 {
		t.Fatalf("restored workspace count %d, err %v", len(spaces), err)
	}
	for _, sp := range spaces {
		err = catalog.ForOwner(sp.ID, u.ID).WithTx(t.Context(), func(tx *store.Tx) error {
			_, err := tx.GetProjectByKey("AA")
			if sp.ID != second.ID && errors.Is(err, store.ErrNotFound) {
				return nil
			}
			if sp.ID != second.ID {
				return fmt.Errorf("project escaped its workspace")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
