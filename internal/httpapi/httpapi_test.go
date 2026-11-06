package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"docket/internal/events"
	"docket/internal/mcpserver"
	"docket/internal/service"
	"docket/internal/store"
)

type env struct {
	srv   *httptest.Server
	svc   *service.Service
	token string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	bus := events.NewBus()
	svc := service.New(st, bus, time.UTC)
	token, err := svc.EnsureMCPSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // runs before the store is closed
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = Router(Deps{Svc: svc, Bus: bus, AllowedHosts: []string{"localhost", "127.0.0.1", "docket.localhost"},
		MCP: mcpserver.Handler(svc, time.UTC), Web: http.NotFoundHandler(), SelfAddr: srv.Listener.Addr().String(), Ctx: ctx})
	srv.Start()
	t.Cleanup(srv.Close)
	return &env{srv: srv, svc: svc, token: token}
}

func do(t *testing.T, method, url, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestMCPRequiresToken(t *testing.T) {
	e := newEnv(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, auth := range []string{"", "Bearer wrong", e.token} { // the bare token lacks the "Bearer " prefix
		res := do(t, "POST", e.srv.URL+"/mcp", body, map[string]string{"Authorization": auth})
		if res.StatusCode != 401 {
			t.Errorf("Authorization %q: status %d, want 401", auth, res.StatusCode)
		}
	}
	res := do(t, "POST", e.srv.URL+"/mcp", body, map[string]string{"Authorization": "Bearer " + e.token, "Accept": "application/json, text/event-stream"})
	if res.StatusCode == 401 {
		t.Error("the right token was refused")
	}
}

func TestMCPPathServesAppToBrowsers(t *testing.T) {
	e := newEnv(t)
	res := do(t, "GET", e.srv.URL+"/mcp", "", map[string]string{"Accept": "text/html,application/xhtml+xml"})
	if res.StatusCode == 401 {
		t.Fatal("a browser navigation to /mcp must reach the app, not the token check")
	}
	res = do(t, "GET", e.srv.URL+"/mcp", "", map[string]string{"Accept": "text/event-stream"})
	if res.StatusCode != 401 {
		t.Fatalf("an MCP client without a token got %d, want 401", res.StatusCode)
	}
}

func TestForeignHostAndOrigin(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		method, path string
		hdr          map[string]string
		want         int
	}{
		{"GET", "/api/projects", map[string]string{"Host": "evil.com:7777"}, 403},
		{"POST", "/mcp", map[string]string{"Host": "attacker.example", "Authorization": "Bearer " + e.token}, 403},
		{"POST", "/api/projects", map[string]string{"Origin": "https://evil.com"}, 403},
		{"GET", "/api/projects", map[string]string{"Host": "docket.localhost:7777"}, 200},
		{"POST", "/api/projects", map[string]string{"Origin": "http://docket.localhost:7777"}, 201},
	}
	for _, c := range cases {
		res := do(t, c.method, e.srv.URL+c.path, `{"name":"X"}`, c.hdr)
		if res.StatusCode != c.want {
			t.Errorf("%s %s %v: status %d, want %d", c.method, c.path, c.hdr, res.StatusCode, c.want)
		}
	}
}

func TestRESTErrorsAndStatusConflict(t *testing.T) {
	e := newEnv(t)
	res := do(t, "POST", e.srv.URL+"/api/tasks", `{"project_id":99,"title":"x"}`, nil)
	var er struct {
		Error struct{ Code, Message string }
	}
	json.NewDecoder(res.Body).Decode(&er)
	if res.StatusCode != 400 || er.Error.Code != "validation" {
		t.Errorf("unknown project: %d %+v", res.StatusCode, er)
	}
	if res := do(t, "GET", e.srv.URL+"/api/tasks/12345", "", nil); res.StatusCode != 404 {
		t.Errorf("missing task: %d", res.StatusCode)
	}

	do(t, "POST", e.srv.URL+"/api/projects", `{"name":"Proj","slug":"proj","key":"PRJ"}`, nil)
	if res := do(t, "POST", e.srv.URL+"/api/tasks", `{"project":"PRJ","title":"t","status":"doing"}`, nil); res.StatusCode != 201 {
		t.Fatalf("create task: %d", res.StatusCode)
	}
	if res := do(t, "GET", e.srv.URL+"/api/tasks/prj-01", "", nil); res.StatusCode != 200 {
		t.Errorf("get by code: %d", res.StatusCode)
	}
	var sts []struct {
		ID  int64
		Key string
	}
	res = do(t, "GET", e.srv.URL+"/api/projects/1/statuses", "", nil)
	json.NewDecoder(res.Body).Decode(&sts)
	ids := map[string]int64{}
	for _, s := range sts {
		ids[s.Key] = s.ID
	}
	url := e.srv.URL + "/api/statuses/" + id(ids["doing"])
	res = do(t, "DELETE", url, "", nil)
	json.NewDecoder(res.Body).Decode(&er)
	if res.StatusCode != 409 || er.Error.Code != "status_in_use" {
		t.Errorf("deleting a busy status: %d %+v, want 409 status_in_use", res.StatusCode, er)
	}
	if res := do(t, "DELETE", url+"?move_to="+id(ids["todo"]), "", nil); res.StatusCode != 204 {
		t.Errorf("deleting with move_to: %d", res.StatusCode)
	}
}

func id(n int64) string { b, _ := json.Marshal(n); return string(b) }

type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(r)
}

func connect(t *testing.T, e *env) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: authTransport{e.token}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *mcp.ClientSession) map[string]bool {
	t.Helper()
	tl, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, x := range tl.Tools {
		out[x.Name] = true
	}
	return out
}

func TestMCPToolsPolicyAndCallLog(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.CreateProject(ctx, service.ActorDenis, service.ProjectInput{Name: "Demo Tracker", Slug: "tracker", Key: "TRK"})
	cs := connect(t, e)

	// delete_tasks is off by default and hidden from tools/list.
	names := toolNames(t, cs)
	if len(names) != 11 || names["delete_tasks"] {
		t.Fatalf("default tools = %v", names)
	}

	call := func(name string, args any) (string, bool) {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res.Content[0].(*mcp.TextContent).Text, res.IsError
	}
	text, isErr := call("create_tasks", map[string]any{"tasks": []map[string]any{
		{"project": "TRK", "title": "Epic", "ref": "e", "planned_for": "2026-11-04", "status": "todo"},
		{"project": "tracker", "title": "Sub", "parent_ref": "e", "planned_for": "2026-11-04", "priority": "P0"},
	}})
	if isErr || !strings.Contains(text, `"id":"TRK-01"`) || !strings.Contains(text, `"id":"TRK-02"`) {
		t.Fatalf("create_tasks = %s (error=%v)", text, isErr)
	}
	out, _ := call("get_today", map[string]any{"date": "2026-11-04"})
	if !strings.Contains(out, `"id":"TRK-01"`) || !strings.Contains(out, `"parent":"TRK-01"`) || !strings.Contains(out, `"project":"tracker"`) {
		t.Fatalf("get_today = %s", out)
	}
	if strings.Contains(out, "description_md") || strings.Contains(out, "created_at") {
		t.Fatalf("get_today is not compact: %s", out)
	}
	ts, _ := e.svc.ListTasks(ctx, store.TaskFilter{})
	if ts[0].CreatedBy != "claude" {
		t.Fatalf("MCP task created_by = %s", ts[0].CreatedBy)
	}
	if out, isErr := call("get_task", map[string]any{"id": "trk-01"}); isErr || !strings.Contains(out, `"subtasks":[{"id":"TRK-02"`) {
		t.Fatalf("get_task by code = %s", out)
	}

	// Errors are isError results with a stable "code: message" text.
	if out, isErr := call("get_task", map[string]any{"id": "TRK-99"}); !isErr || !strings.HasPrefix(out, "not_found:") {
		t.Fatalf("get_task TRK-99 = %q (error=%v)", out, isErr)
	}
	// A switched-off tool answers tool_disabled.
	if out, isErr := call("delete_tasks", map[string]any{"ids": []string{"TRK-02"}}); !isErr || !strings.HasPrefix(out, "tool_disabled:") {
		t.Fatalf("delete_tasks while off = %q (error=%v)", out, isErr)
	}
	// Read-only mode hides and refuses every write tool.
	if _, err := e.svc.UpdateMCPSettings(ctx, service.MCPSettingsPatch{AccessMode: ptr("ro")}); err != nil {
		t.Fatal(err)
	}
	names = toolNames(t, cs)
	if len(names) != 5 || names["create_tasks"] || !names["get_board"] {
		t.Fatalf("read-only tools = %v", names)
	}
	if out, isErr := call("create_tasks", map[string]any{"tasks": []map[string]any{{"project": "TRK", "title": "nope"}}}); !isErr || !strings.HasPrefix(out, "forbidden:") {
		t.Fatalf("create_tasks in ro = %q (error=%v)", out, isErr)
	}
	if all, _ := e.svc.ListTasks(ctx, store.TaskFilter{IncludeDone: true}); len(all) != 2 {
		t.Fatalf("a refused write changed data: %d tasks", len(all))
	}
	// Switching delete_tasks on (and rw back) exposes it again.
	if _, err := e.svc.UpdateMCPSettings(ctx, service.MCPSettingsPatch{AccessMode: ptr("rw"), Tools: map[string]bool{"delete_tasks": true}}); err != nil {
		t.Fatal(err)
	}
	if !toolNames(t, cs)["delete_tasks"] {
		t.Fatal("delete_tasks not listed after it was switched on")
	}

	// Every tools/call is in the log, refused ones included; tools/list is not.
	calls, _ := e.svc.ListMCPCalls(ctx, "all", 50)
	want := []struct{ tool, status, code string }{
		{"create_tasks", "error", "forbidden"}, {"delete_tasks", "error", "tool_disabled"}, {"get_task", "error", "not_found"},
		{"get_task", "ok", ""}, {"get_today", "ok", ""}, {"create_tasks", "ok", ""},
	}
	if len(calls) != len(want) {
		t.Fatalf("log has %d lines, want %d: %+v", len(calls), len(want), calls)
	}
	for i, w := range want { // newest first
		c := calls[i]
		code := ""
		if c.ErrorCode != nil {
			code = *c.ErrorCode
		}
		if c.Tool != w.tool || c.Status != w.status || code != w.code {
			t.Errorf("log[%d] = %s/%s/%s, want %s/%s/%s", i, c.Tool, c.Status, code, w.tool, w.status, w.code)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// The skill is both installed by the user and sent as server instructions; Claude Code cuts instructions at 2048 bytes.
func TestDocketSkillIsServedAndSentAsInstructions(t *testing.T) {
	e := newEnv(t)
	res := do(t, "GET", e.srv.URL+"/api/mcp/skill", "", nil)
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(string(body), "---\nname: docket\n") {
		t.Fatalf("skill endpoint: %d %.40q", res.StatusCode, body)
	}
	ins := connect(t, e).InitializeResult().Instructions
	if ins == "" || strings.Contains(ins, "name: docket") || len(ins) > 2048 {
		t.Fatalf("instructions must be the skill body within 2048 bytes, got %d bytes", len(ins))
	}
}

func TestMCPCheckAndStatus(t *testing.T) {
	e := newEnv(t)
	res := do(t, "POST", e.srv.URL+"/api/mcp/check", "", nil)
	var r CheckResult
	json.NewDecoder(res.Body).Decode(&r)
	if res.StatusCode != 200 || len(r.Checks) != 5 {
		t.Fatalf("check: %d %+v", res.StatusCode, r)
	}
	for _, c := range r.Checks {
		if !c.OK {
			t.Errorf("check %s failed: %s", c.Key, c.Detail)
		}
	}
	if !r.OK {
		t.Fatal("overall check not ok")
	}
	var st struct {
		Online bool
		Check  *CheckResult
	}
	res = do(t, "GET", e.srv.URL+"/api/mcp/status", "", nil)
	json.NewDecoder(res.Body).Decode(&st)
	if !st.Online || st.Check == nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestMCPTokenRotation(t *testing.T) {
	e := newEnv(t)
	res := do(t, "POST", e.srv.URL+"/api/mcp/token/rotate", "", nil)
	var tok service.TokenInfo
	json.NewDecoder(res.Body).Decode(&tok)
	if res.StatusCode != 200 || tok.Token == "" || tok.Token == e.token || !strings.HasPrefix(tok.Masked, "dkt_••••") {
		t.Fatalf("rotate: %d %+v", res.StatusCode, tok)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if r := do(t, "POST", e.srv.URL+"/mcp", body, map[string]string{"Authorization": "Bearer " + e.token}); r.StatusCode != 401 {
		t.Errorf("old token: %d, want 401", r.StatusCode)
	}
	if r := do(t, "POST", e.srv.URL+"/mcp", body, map[string]string{"Authorization": "Bearer " + tok.Token, "Accept": "application/json, text/event-stream"}); r.StatusCode == 401 {
		t.Error("new token refused")
	}
}

func TestSSE(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf := make([]byte, 256)
	res.Body.Read(buf) // ": connected"
	e.svc.CreateProject(context.Background(), service.ActorClaude, service.ProjectInput{Name: "P"})
	n, _ := res.Body.Read(buf)
	if got := string(buf[:n]); !strings.Contains(got, `"type":"project_changed"`) || !strings.Contains(got, `"actor":"claude"`) {
		t.Fatalf("sse got %q", got)
	}
}

func TestLocalMe(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, events.NewBus(), time.UTC)
	h := Router(Deps{Svc: svc, Bus: events.NewBus(), AllowedHosts: []string{"localhost"}, MCP: http.NotFoundHandler(), Web: http.NotFoundHandler()})
	req := httptest.NewRequest("GET", "/api/auth/me", nil)
	req.Host = "localhost"
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"mode":"local"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestSelfCheckHints(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, events.NewBus(), time.UTC)
	hints := func(selfHost bool) map[string]string {
		a := &api{svc: svc, bus: events.NewBus(), mcp: mcpserver.Handler(svc, time.UTC), selfHost: selfHost}
		m := map[string]string{}
		for _, c := range a.runCheck(context.Background()).Checks {
			m[c.Key] = c.Hint
		}
		return m
	}
	local := map[string]string{
		"server":   "Проверьте, что контейнер запущен: docker ps",
		"protocol": "Посмотрите лог контейнера: docker logs docket",
		"token":    "Задайте DOCKET_MCP_TOKEN в .env или перевыпустите токен на этой странице",
		"hosts":    "Добавьте хост в DOCKET_ALLOWED_HOSTS и перезапустите контейнер",
		"db":       "Проверьте место на диске и том ./data; бэкапы лежат в data/backups",
	}
	got := hints(false)
	for k, want := range local {
		if got[k] != want {
			t.Errorf("local %s hint = %q, want %q", k, got[k], want)
		}
	}
	for k, h := range hints(true) {
		if h == "" || h == local[k] || strings.Contains(h, "DOCKET_") || strings.Contains(h, "docker") || strings.Contains(h, ".env") {
			t.Errorf("selfhost %s hint = %q", k, h)
		}
	}
}
