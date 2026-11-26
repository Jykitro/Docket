package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"plandillo/internal/events"
	"plandillo/internal/mcpserver"
	"plandillo/internal/service"
	"plandillo/internal/store"
	"plandillo/internal/testutil"
)

func openTestStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	if os.Getenv("PLANDILLO_TEST_POSTGRES_DSN") != "" {
		return store.OpenPostgres(testutil.PostgresDSN(t), 1)
	}
	return store.Open(filepath.Join(t.TempDir(), "t.db"))
}

type env struct {
	srv   *httptest.Server
	svc   *service.Service
	token string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := openTestStore(t)
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
	srv.Config.Handler = Router(Deps{Svc: svc, Bus: bus, AllowedHosts: []string{"localhost", "127.0.0.1", "plandillo.localhost"},
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
		{"GET", "/api/projects", map[string]string{"Host": "plandillo.localhost:7777"}, 200},
		{"POST", "/api/projects", map[string]string{"Origin": "http://plandillo.localhost:7777"}, 403},
		{"POST", "/api/projects", map[string]string{"Origin": e.srv.URL}, 201},
		{"POST", "/api/projects", map[string]string{"Sec-Fetch-Site": "cross-site"}, 403},
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
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL + "/mcp?agent=codex",
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
	if len(names) != len(service.MCPTools)-1 || names["delete_tasks"] {
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
	if ts[0].CreatedBy != "codex" {
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
	if len(names) != 8 || names["create_tasks"] || names["write_page"] || !names["get_board"] || !names["read_page"] {
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
		if c.Actor != "codex" {
			t.Errorf("log[%d] actor = %q, want codex", i, c.Actor)
		}
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
func TestPlandilloSkillIsServedAndSentAsInstructions(t *testing.T) {
	e := newEnv(t)
	res := do(t, "GET", e.srv.URL+"/api/mcp/skill", "", nil)
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(string(body), "---\nname: plandillo\n") {
		t.Fatalf("skill endpoint: %d %.40q", res.StatusCode, body)
	}
	for _, name := range []string{"plandillo", "plandillo-reference", "plandillo-setup", "docket", "docket-reference", "docket-setup"} {
		res := do(t, "GET", e.srv.URL+"/api/mcp/skill/"+name, "", nil)
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || !strings.HasPrefix(string(b), "---\nname: "+name+"\ndescription: Use when ") {
			t.Errorf("skill %s: %d %.60q", name, res.StatusCode, b)
		}
	}
	for _, name := range []string{"nope", "..%2Fgo.mod", "docket%2Fx"} {
		if res := do(t, "GET", e.srv.URL+"/api/mcp/skill/"+name, "", nil); res.StatusCode != 404 {
			t.Errorf("skill %q: status %d, want 404", name, res.StatusCode)
		}
	}
	res = do(t, "GET", e.srv.URL+"/api/mcp/install.sh", "", nil)
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.HasPrefix(string(body), "#!/bin/sh\n") || !strings.Contains(string(body), "name: docket-setup") {
		t.Fatalf("install.sh: %d %.40q", res.StatusCode, body)
	}
	result := connect(t, e).InitializeResult()
	if result.ServerInfo.Name != "plandillo" || result.ServerInfo.Title != "Plandillo" {
		t.Fatalf("server identity: %+v", result.ServerInfo)
	}
	ins := result.Instructions
	if ins == "" || strings.Contains(ins, "name:") || !strings.HasPrefix(ins, "# Plandillo") || len(ins) > 2048 {
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
	if res.Header.Get("X-Accel-Buffering") != "no" {
		t.Error("the stream must ask reverse proxies not to buffer it")
	}
	buf := make([]byte, 256)
	res.Body.Read(buf) // ": connected"
	e.svc.CreateProject(context.Background(), service.ActorClaude, service.ProjectInput{Name: "P"})
	n, _ := res.Body.Read(buf)
	if got := string(buf[:n]); !strings.Contains(got, `"type":"project_changed"`) || !strings.Contains(got, `"actor":"claude"`) {
		t.Fatalf("sse got %q", got)
	}
}

func TestSSEStreamBudget(t *testing.T) {
	e := newEnv(t)
	open := func() (*http.Response, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events", nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close(); cancel() })
		return res, cancel
	}
	var first context.CancelFunc
	for i := range events.MaxPerBus {
		res, cancel := open()
		if res.StatusCode != 200 {
			t.Fatalf("stream %d: status %d", i+1, res.StatusCode)
		}
		if i == 0 {
			first = cancel
		}
	}
	res, _ := open()
	var body struct{ Error struct{ Code string } }
	json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" || body.Error.Code != "too_many_streams" {
		t.Fatalf("stream over the budget: status %d, Retry-After %q, code %q", res.StatusCode, res.Header.Get("Retry-After"), body.Error.Code)
	}
	first() // the server releases the slot once it sees the request end
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if res, cancel := open(); res.StatusCode == 200 {
			cancel()
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("closing a stream did not free its slot: status %d", res.StatusCode)
		}
	}
}

func TestLocalMe(t *testing.T) {
	st, err := openTestStore(t)
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
	st, err := openTestStore(t)
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
		"protocol": "Посмотрите лог контейнера: docker logs plandillo",
		"token":    "Задайте PLANDILLO_MCP_TOKEN в .env или перевыпустите токен на этой странице",
		"hosts":    "Добавьте хост в PLANDILLO_ALLOWED_HOSTS и перезапустите контейнер",
		"db":       "Проверьте место на диске и том ./data; бэкапы лежат в data/backups",
	}
	got := hints(false)
	for k, want := range local {
		if got[k] != want {
			t.Errorf("local %s hint = %q, want %q", k, got[k], want)
		}
	}
	for k, h := range hints(true) {
		if h == "" || h == local[k] || strings.Contains(h, "PLANDILLO_") || strings.Contains(h, "docker") || strings.Contains(h, ".env") {
			t.Errorf("selfhost %s hint = %q", k, h)
		}
	}
}

func TestPagesREST(t *testing.T) {
	e := newEnv(t)
	api := e.srv.URL + "/api"
	do(t, "POST", api+"/projects", `{"name":"Proj","slug":"proj","key":"PRJ"}`, nil)
	if res := do(t, "POST", api+"/tasks", `{"project":"PRJ","title":"t"}`, nil); res.StatusCode != 201 {
		t.Fatalf("create task: %d", res.StatusCode)
	}
	type page struct {
		ID        int64
		ParentID  *int64 `json:"parent_id"`
		Title     string
		BodyMD    string `json:"body_md"`
		UpdatedAt string `json:"updated_at"`
	}
	read := func(res *http.Response, v any) {
		t.Helper()
		if err := json.NewDecoder(res.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}

	get := func(url string, v any) {
		t.Helper()
		res := do(t, "GET", url, "", nil)
		if res.StatusCode != 200 {
			t.Fatalf("GET %s: %d, want 200", url, res.StatusCode)
		}
		read(res, v)
	}

	var a, b page
	res := do(t, "POST", api+"/projects/proj/pages", `{"title":"Alpha","body_md":"see [[Beta]] and PRJ-01"}`, nil)
	if res.StatusCode != 201 {
		t.Fatalf("create page: %d", res.StatusCode)
	}
	read(res, &a)
	res = do(t, "POST", api+"/projects/proj/pages", `{"title":"Beta","parent_id":`+id(a.ID)+`}`, nil)
	if res.StatusCode != 201 {
		t.Fatalf("create child page: %d", res.StatusCode)
	}
	read(res, &b)
	if b.ParentID == nil || *b.ParentID != a.ID {
		t.Errorf("child parent_id = %v, want %d", b.ParentID, a.ID)
	}

	var list []page
	get(api+"/projects/proj/pages", &list)
	if len(list) != 2 || list[0].BodyMD != "" || list[1].BodyMD != "" {
		t.Errorf("list = %+v, want 2 pages without bodies", list)
	}

	var det struct {
		page
		Backlinks []struct{ Title string }
		Links     struct{ Pages, Tasks []struct{ Title string } }
	}
	get(api+"/pages/"+id(b.ID), &det)
	if len(det.Backlinks) != 1 || det.Backlinks[0].Title != "Alpha" {
		t.Errorf("backlinks = %+v, want Alpha", det.Backlinks)
	}
	det.Links.Pages, det.Links.Tasks = nil, nil
	get(api+"/pages/"+id(a.ID), &det)
	if len(det.Links.Pages) != 1 || len(det.Links.Tasks) != 1 {
		t.Errorf("links = %+v, want 1 page and 1 task", det.Links)
	}
	var tp []struct{ Title, Project string }
	get(api+"/tasks/prj-01/pages", &tp)
	if len(tp) != 1 || tp[0].Title != "Alpha" || tp[0].Project != "proj" {
		t.Errorf("task pages = %+v", tp)
	}

	// absent and null parent_id leave the parent alone, 0 moves to the top level
	var u page
	res = do(t, "PUT", api+"/pages/"+id(b.ID), `{"body_md":"x","parent_id":null}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("update: %d, want 200", res.StatusCode)
	}
	read(res, &u)
	if u.BodyMD != "x" || u.ParentID == nil || *u.ParentID != a.ID {
		t.Errorf("update with null parent_id: %+v", u)
	}
	res = do(t, "PUT", api+"/pages/"+id(b.ID), `{"parent_id":0}`, nil)
	if res.StatusCode != 200 {
		t.Fatalf("update parent_id 0: %d, want 200", res.StatusCode)
	}
	read(res, &u)
	if u.ParentID != nil {
		t.Errorf("parent_id 0 should make the page top level, got %v", *u.ParentID)
	}
	if res := do(t, "PUT", api+"/pages/999999", `{"body_md":"x"}`, nil); res.StatusCode != 404 {
		t.Errorf("update of a missing page: %d, want 404", res.StatusCode)
	}
	if res := do(t, "PUT", api+"/pages/"+id(b.ID), `{"parent_id":999999}`, nil); res.StatusCode != 400 {
		t.Errorf("update with a missing parent page: %d, want 400", res.StatusCode)
	}
	var z page
	res = do(t, "POST", api+"/projects/proj/pages", `{"title":"Zero","parent_id":0}`, nil)
	if res.StatusCode != 201 {
		t.Fatalf("create with parent_id 0: %d, want 201", res.StatusCode)
	}
	read(res, &z)
	if z.ParentID != nil {
		t.Errorf("parent_id 0 on create should be top level, got %v", *z.ParentID)
	}

	var er struct {
		Error struct{ Code string }
	}
	res = do(t, "PUT", api+"/pages/"+id(a.ID), `{"body_md":"y","expected_updated_at":"2000-01-01T00:00:00Z"}`, nil)
	read(res, &er)
	if res.StatusCode != 409 || er.Error.Code != "conflict" {
		t.Errorf("stale update: %d %+v, want 409 conflict", res.StatusCode, er)
	}
	if res := do(t, "PUT", api+"/pages/"+id(a.ID), `{"body_md":"y","expected_updated_at":"`+a.UpdatedAt+`"}`, nil); res.StatusCode != 200 {
		t.Errorf("update with current expected_updated_at: %d", res.StatusCode)
	}

	if res := do(t, "POST", api+"/projects/proj/pages", `{"title":"bad [title]"}`, nil); res.StatusCode != 400 {
		t.Errorf("invalid title: %d, want 400", res.StatusCode)
	}
	if res := do(t, "GET", api+"/pages/abc", "", nil); res.StatusCode != 400 {
		t.Errorf("non-numeric page id: %d, want 400", res.StatusCode)
	}
	if res := do(t, "DELETE", api+"/pages/"+id(a.ID), "", nil); res.StatusCode != 204 {
		t.Errorf("delete: %d, want 204", res.StatusCode)
	}
	if res := do(t, "GET", api+"/pages/"+id(a.ID), "", nil); res.StatusCode != 404 {
		t.Errorf("deleted page: %d, want 404", res.StatusCode)
	}
}

func TestPostgresDatabaseSelfCheck(t *testing.T) {
	st, err := store.OpenPostgres(testutil.PostgresDSN(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := service.New(st, events.NewBus(), time.UTC)
	a := &api{svc: svc}
	c := a.checkDB(context.Background())
	if !c.OK || !strings.Contains(c.Detail, "PostgreSQL") || strings.Contains(c.Detail, "quick_check") || strings.Contains(c.Detail, "бэкап") {
		t.Fatalf("check=%+v", c)
	}
}

func TestLocalBrowserBoundary(t *testing.T) {
	handler := localBrowserGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{
		{"GET", "/", "", 204},
		{"POST", "/api/projects", "http://127.0.0.1:5173", 403},
		{"POST", "/api/projects", "https://127.0.0.1:7777", 403},
		{"POST", "/api/projects", "http://127.0.0.1:7777", 204},
	} {
		req := httptest.NewRequest(tc.method, "http://127.0.0.1:7777"+tc.path, nil)
		req.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != tc.status || w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
			t.Fatal(tc, w.Code, w.Header())
		}
	}
	req := httptest.NewRequest("POST", "http://localhost:7777/api/projects", strings.NewReader("{}"))
	req.Header.Set("Origin", "http://localhost:7777")
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatal("browser body", w.Code)
	}

}

func TestTrashPurgeRequiresDisplayedVersion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	project, err := e.svc.CreateProject(ctx, service.ActorDenis, service.ProjectInput{Name: "Review"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.svc.DeleteProject(ctx, service.ActorDenis, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	url := e.srv.URL + "/api/trash"
	if got := do(t, "DELETE", url, "{}", nil).StatusCode; got != 400 {
		t.Fatal(got)
	}
	if _, err := e.svc.RestoreProject(ctx, service.ActorDenis, project.ID); err != nil {
		t.Fatal(err)
	}
	second, err := e.svc.DeleteProject(ctx, service.ActorDenis, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	body := func(stamp string) string {
		b, err := json.Marshal(service.TrashSnapshot{Tasks: []store.TrashEntry{}, Projects: []store.TrashEntry{{ID: project.ID, DeletedAt: stamp}}})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := do(t, "DELETE", url, body(*first.DeletedAt), nil).StatusCode; got != 409 {
		t.Fatal(got)
	}
	if got := do(t, "DELETE", url, body(*second.DeletedAt), nil).StatusCode; got != 200 {
		t.Fatal(got)
	}
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadline time.Time
}

func (r *deadlineRecorder) SetWriteDeadline(d time.Time) error { r.deadline = d; return nil }

func TestExportRefreshesAndCapsWriteDeadline(t *testing.T) {
	r := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	now := time.Now()
	end := now.Add(time.Minute)
	w := exportWriter{w: r, controller: http.NewResponseController(r), deadline: end}
	if _, err := w.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if r.deadline.Before(now.Add(29*time.Second)) || r.deadline.After(now.Add(31*time.Second)) {
		t.Fatal(r.deadline)
	}
	w.deadline = now.Add(time.Second)
	if _, err := w.Write([]byte("two")); err != nil || !r.deadline.Equal(w.deadline) {
		t.Fatal(r.deadline, err)
	}
}

type memPush map[string]service.PushSubscription

func (m memPush) PublicKey() string { return "BKey" }
func (m memPush) Get(_ context.Context, e string) (service.PushSubscription, bool, error) {
	s, ok := m[e]
	return s, ok, nil
}
func (m memPush) Save(_ context.Context, s service.PushSubscription) error {
	m[s.Endpoint] = s
	return nil
}
func (m memPush) Remove(_ context.Context, e string) error { delete(m, e); return nil }

func TestPushSubscriptionEndpoints(t *testing.T) {
	e := newEnv(t)
	var st service.PushState
	res := do(t, "GET", e.srv.URL+"/api/push", "", nil)
	json.NewDecoder(res.Body).Decode(&st)
	if res.StatusCode != 200 || st.Enabled {
		t.Fatalf("push off: %d %+v", res.StatusCode, st)
	}
	sub := `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc","p256dh":"k","auth":"a","agent_done":true,"morning":"08:30","tz":"Europe/Kyiv","lang":"uk"}`
	if res := do(t, "PUT", e.srv.URL+"/api/push", sub, nil); res.StatusCode != 400 {
		t.Fatalf("subscribing with push off: %d, want 400 push_disabled", res.StatusCode)
	}

	m := memPush{}
	e.svc.SetPush(m)
	evil := strings.Replace(sub, "fcm.googleapis.com", "127.0.0.1:9", 1)
	if res := do(t, "PUT", e.srv.URL+"/api/push", evil, nil); res.StatusCode != 400 || len(m) != 0 {
		t.Fatalf("an endpoint outside the push services must be refused: %d", res.StatusCode)
	}
	if res := do(t, "PUT", e.srv.URL+"/api/push", sub, map[string]string{"Host": "localhost"}); res.StatusCode != 204 {
		t.Fatalf("subscribe: %d", res.StatusCode)
	}
	if s := m["https://fcm.googleapis.com/fcm/send/abc"]; s.Origin != "https://localhost" || !s.AgentDone || s.Morning != "08:30" {
		t.Fatalf("saved %+v", s)
	}
	res = do(t, "GET", e.srv.URL+"/api/push", "", map[string]string{"X-Push-Endpoint": "https://fcm.googleapis.com/fcm/send/abc"})
	json.NewDecoder(res.Body).Decode(&st)
	if !st.Enabled || st.PublicKey != "BKey" || st.Subscription == nil || st.Subscription.Lang != "uk" {
		t.Fatalf("state %+v", st)
	}
	if res := do(t, "DELETE", e.srv.URL+"/api/push", `{"endpoint":"https://fcm.googleapis.com/fcm/send/abc"}`, nil); res.StatusCode != 204 || len(m) != 0 {
		t.Fatalf("unsubscribe: %d, left %v", res.StatusCode, m)
	}
}
