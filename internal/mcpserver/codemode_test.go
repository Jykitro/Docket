package mcpserver

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"docket/internal/events"
	"docket/internal/service"
)

// Code Mode end to end over /mcp/code: declarations follow the policy, codemode.* calls reach the real tools
// (and the call log), errors keep their code, and runaway code is stopped.
func TestCodeMode(t *testing.T) {
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	svc := service.New(st, events.NewBus(), time.UTC)
	if _, err := svc.EnsureMCPSettings(ctx); err != nil { // delete_tasks starts switched off
		t.Fatal(err)
	}
	if _, err := svc.CreateProject(ctx, service.ActorDenis, service.ProjectInput{Name: "p", Key: "PP", Slug: "p"}); err != nil {
		t.Fatal(err)
	}
	h := WithEvaluator(svc, time.UTC, runCode)

	id := 0
	rpc := func(method, params string) json.RawMessage {
		t.Helper()
		id++
		body := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"` + method + `","params":` + params + `}`
		r := httptest.NewRequest("POST", "/mcp/code", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var res struct{ Result json.RawMessage }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &res) != nil {
			t.Fatalf("%s: HTTP %d %s", method, w.Code, w.Body)
		}
		return res.Result
	}
	run := func(code string) string {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"code": code})
		var res struct {
			IsError bool `json:"isError"`
			Content []struct{ Text string }
		}
		if err := json.Unmarshal(rpc("tools/call", `{"name":"code","arguments":`+string(args)+`}`), &res); err != nil || len(res.Content) == 0 {
			t.Fatalf("bad result for %q: %v", code, err)
		}
		if res.IsError {
			return "ERR " + res.Content[0].Text
		}
		return res.Content[0].Text
	}

	var list struct {
		Tools []struct{ Name, Description string }
	}
	if err := json.Unmarshal(rpc("tools/list", `{}`), &list); err != nil || len(list.Tools) != 1 || list.Tools[0].Name != "code" {
		t.Fatalf("tools/list: %v %+v", err, list)
	}
	d := list.Tools[0].Description
	if !strings.Contains(d, "declare const codemode: {") || !strings.Contains(d, "  get_task(args: {\n") ||
		!strings.Contains(d, "    ids: string[] | null;") || strings.Contains(d, "delete_tasks(") {
		t.Fatalf("declarations:\n%s", d)
	}

	out := run(`async () => {
		const made = await codemode.create_tasks({ tasks: [{ project: "p", title: "a" }, { project: "p", title: "b" }] })
		console.log("made", made.length, { first: made[0].id })
		const board = await codemode.get_board({ project: "p" })
		return board.backlog.map(t => t.title)
	}`)
	if out != `{"result":["a","b"],"logs":["made 2 {\"first\":\"PP-01\"}"]}` {
		t.Fatalf("arrow function: %s", out)
	}
	if out = run(`const ps = await codemode.list_projects({}); return ps.length;`); out != `{"result":1}` {
		t.Fatalf("function body: %s", out)
	}
	if out = run(`async () => { await codemode.get_task({ id: "PP-99" }) }`); !strings.HasPrefix(out, "ERR not_found:") {
		t.Fatalf("uncaught tool error: %s", out)
	}
	if out = run(`try { await codemode.get_task({ id: "PP-99" }) } catch (e) { return e.message.split(":")[0] }`); out != `{"result":"not_found"}` {
		t.Fatalf("caught tool error: %s", out)
	}
	if out = run(`await codemode.delete_tasks({ ids: ["PP-01"] })`); !strings.HasPrefix(out, "ERR tool_disabled:") {
		t.Fatalf("switched-off tool: %s", out)
	}
	if out = run(`async () => { undefinedThing() }`); !strings.HasPrefix(out, "ERR invalid_argument: ReferenceError") {
		t.Fatalf("own mistake: %s", out)
	}
	if out = run(`async () => {`); !strings.HasPrefix(out, "ERR invalid_argument:") {
		t.Fatalf("syntax error: %s", out)
	}

	codeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { codeTimeout = 30 * time.Second })
	if out = run(`for (;;) {}`); !strings.HasPrefix(out, "ERR timeout:") {
		t.Fatalf("runaway code: %s", out)
	}
	codeTimeout = 30 * time.Second

	ro := service.ModeReadOnly
	if _, err := svc.UpdateMCPSettings(ctx, service.MCPSettingsPatch{AccessMode: &ro}); err != nil {
		t.Fatal(err)
	}
	if out = run(`await codemode.add_note({ task_id: "PP-01", body_md: "x" })`); !strings.HasPrefix(out, "ERR forbidden:") {
		t.Fatalf("read-only write: %s", out)
	}
	if err := json.Unmarshal(rpc("tools/list", `{}`), &list); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"create_tasks", "report_issue", "publish_html"} {
		if strings.Contains(list.Tools[0].Description, name+"(") {
			t.Fatalf("read-only declarations include %s", name)
		}
	}
	if !strings.Contains(list.Tools[0].Description, "read_html(") {
		t.Fatal("read-only declarations omit read_html")
	}

	calls, err := svc.ListMCPCalls(ctx, "all", 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c.Tool] = true
	}
	if !seen["code"] || !seen["create_tasks"] || !seen["get_board"] {
		t.Fatalf("call log misses Code Mode calls: %v", seen)
	}
}
