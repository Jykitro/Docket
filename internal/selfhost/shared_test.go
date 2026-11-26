package selfhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"plandillo/internal/auth"
	"plandillo/internal/events"
	"plandillo/internal/httpapi"
	"plandillo/internal/mcpserver"
	"plandillo/internal/service"
	"plandillo/internal/store"
)

// Use the production shared catalog with the ordinary HTTP and MCP entrypoints.
func sharedServer(t *testing.T) *server {
	t.Helper()
	s := newServerWith(t, func(ctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		// Exercise the tool callback without starting a worker subprocess.
		mcp := mcpserver.WithEvaluator(svc, time.UTC, func(_ context.Context, code string, call func(string, json.RawMessage) (string, error)) (string, error) {
			if code == "return await codemode.create_project({name:'Test',key:'TE'})" {
				return call("create_project", json.RawMessage(`{"name":"Test","key":"TE"}`))
			}
			return call("list_projects", json.RawMessage("{}"))
		})
		return httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: hosts, MCP: mcp, Web: http.NotFoundHandler()})
	})
	base, err := store.Shared(s.db.Connection(), s.ws.dir+"/auth.db")
	if err != nil {
		t.Fatal(err)
	}
	s.ws.UseShared(base, "", 0)
	s.h = Handler(Options{Auth: s.db, WS: s.ws, Web: http.NotFoundHandler(), AllowedHosts: hosts})
	return s
}

// The switcher loads /icon?v=<updated_at> on every workspace switch (a full reload), so that URL must be cacheable.
func TestWorkspaceIconCaching(t *testing.T) {
	s := sharedServer(t)
	alice, space := s.addUser("alice")
	bob, _ := s.addUser("bob")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 128, 128))); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"icon_png": b.Bytes()})
	path := fmt.Sprintf("/api/workspaces/%d", space.ID)
	if res := s.do("PATCH", path, string(payload), alice, nil); res.Code != 200 {
		t.Fatalf("set icon: %d %s", res.Code, res.Body)
	}
	for url, want := range map[string]string{path + "/icon?v=1": "private, max-age=31536000, immutable", path + "/icon": "private, no-store"} {
		if res := s.do("GET", url, "", alice, nil); res.Code != 200 || res.Header().Get("Cache-Control") != want {
			t.Fatalf("%s: %d %q, want %q", url, res.Code, res.Header().Get("Cache-Control"), want)
		}
	}
	if res := s.do("GET", path+"/icon?v=1", "", bob, nil); res.Code != 404 {
		t.Fatalf("other user: %d", res.Code)
	}
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
	// Page addresses name the workspace by slug, and the UI sends that slug to the API.
	if second.Slug != "second" {
		t.Fatalf("slug %q", second.Slug)
	}
	if !bytes.Contains(status("GET", "/api/workspaces/second/projects", "", a, 200), []byte("Second unique")) {
		t.Fatal("slug addressed another workspace")
	}
	status("GET", "/api/workspaces/second/projects", "", b, 404)
	status("GET", "/api/auth/oauth/connections?workspace=second", "", a, 200)
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
	ws, err := s.ws.openSpace(t.Context(), sp)
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
