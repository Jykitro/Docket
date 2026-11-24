package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"docket/internal/events"
	"docket/internal/service"
	"docket/internal/store"
	"docket/internal/testutil"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func openTestStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	if os.Getenv("DOCKET_TEST_POSTGRES_DSN") != "" {
		return store.OpenPostgres(testutil.PostgresDSN(t), 1)
	}
	return store.Open(filepath.Join(t.TempDir(), "t.db"))
}

// Handler panics at startup when a service.MCPTools entry has no registration (or the reverse); this builds
// it and drives the wiki tools through JSON-RPC in-process (httptest recorder, no port).
func TestWikiToolsOverMCP(t *testing.T) {
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := service.New(st, events.NewBus(), time.UTC)
	if _, err := svc.CreateProject(context.Background(), service.ActorDenis, service.ProjectInput{Name: "p", Slug: "p"}); err != nil {
		t.Fatal(err)
	}
	h := Handler(svc, time.UTC)

	id := 0
	call := func(tool, args string) string {
		t.Helper()
		id++
		body := `{"jsonrpc":"2.0","id":` + strconv.Itoa(id) + `,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: HTTP %d %s", tool, w.Code, w.Body)
		}
		var res struct {
			Result struct {
				IsError bool `json:"isError"`
				Content []struct{ Text string }
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || len(res.Result.Content) == 0 {
			t.Fatalf("%s: bad response %v %s", tool, err, w.Body)
		}
		if res.Result.IsError {
			return "ERR " + res.Result.Content[0].Text
		}
		return res.Result.Content[0].Text
	}

	out := call("write_page", `{"project":"p","title":"Root","body_md":"see [[Kid]] and TRK-99"}`)
	if !strings.Contains(out, `"created":true`) || !strings.Contains(out, `"updated_at":"`) {
		t.Fatalf("write_page: %s", out)
	}
	call("write_page", `{"project":"p","title":"Kid","body_md":"back to [[Root]]","parent":"Root"}`)
	if out = call("append_to_page", `{"project":"p","title":"kid","text":"more"}`); !strings.Contains(out, `"title":"Kid"`) {
		t.Fatalf("append_to_page: %s", out)
	}
	if out = call("list_pages", `{"project":"p"}`); !strings.Contains(out, `"title":"Kid","parent":"Root"`) {
		t.Fatalf("list_pages: %s", out)
	}
	out = call("read_page", `{"project":"p","title":"Kid"}`)
	if !strings.Contains(out, `"body_md":"back to [[Root]]\n\nmore"`) || !strings.Contains(out, `"parent":"Root"`) ||
		!strings.Contains(out, `"backlinks":["Root"]`) || !strings.Contains(out, `"tasks":[]`) {
		t.Fatalf("read_page: %s", out)
	}
	if out = call("read_page", `{"project":"p","title":"Nope"}`); !strings.HasPrefix(out, "ERR not_found:") {
		t.Fatalf("missing page: %s", out)
	}
	if out = call("write_page", `{"project":"p","title":"Kid","body_md":"x","expected_updated_at":"2000-01-01T00:00:00.000Z"}`); !strings.HasPrefix(out, "ERR conflict:") {
		t.Fatalf("stale write: %s", out)
	}
	calls, err := svc.ListMCPCalls(context.Background(), "error", 10)
	if err != nil || len(calls) == 0 || calls[0].ErrorCode == nil || *calls[0].ErrorCode != "conflict" {
		t.Fatalf("logged conflict: %v %+v", err, calls)
	}
	appended, err := svc.ListMCPCalls(context.Background(), "write", 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range appended {
		if c.Tool == "append_to_page" && !strings.Contains(c.ArgsSummary, `"title":"kid"`) {
			t.Fatalf("append_to_page summary lost the title: %s", c.ArgsSummary)
		}
	}
}

func TestResultCodeAndSummarize(t *testing.T) {
	conflict := &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "conflict: page changed"}}}
	if got := resultCode(conflict, nil); got != "conflict" {
		t.Errorf("resultCode = %q, want conflict", got)
	}
	got := summarize(json.RawMessage(`{"title":"Log","text":"long appended text","body_md":"b"}`))
	if strings.Contains(got, "appended") || strings.Contains(got, `"b"`) || !strings.Contains(got, `"title":"Log"`) {
		t.Errorf("summarize = %s", got)
	}
}

func TestAgentAttributionOverHTTP(t *testing.T) {
	st, err := openTestStore(t)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctx := context.Background()
	svc := service.New(st, events.NewBus(), time.UTC)
	if _, err := svc.CreateProject(ctx, service.ActorDenis, service.ProjectInput{Name: "Agents", Key: "AG"}); err != nil {
		t.Fatal(err)
	}
	h := Handler(svc, time.UTC)
	call := func(path, client, userAgent, tool string, args any) {
		t.Helper()
		params := map[string]any{"name": tool, "arguments": args}
		if client != "" {
			params["_meta"] = map[string]any{mcp.MetaKeyClientInfo: map[string]string{"name": client, "version": "1"}}
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
		r.Header.Set("User-Agent", userAgent)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json, text/event-stream")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		// Result content has interface elements, so inspect just the error flag.
		var envelope struct {
			Result *struct {
				IsError bool `json:"isError"`
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); w.Code != 200 || err != nil || envelope.Result == nil || envelope.Result.IsError {
			t.Fatalf("%s: %s", tool, w.Body)
		}
	}
	for i, tc := range []struct{ path, client, userAgent, want string }{
		{"/mcp", "codex-mcp-client", "", "codex"},
		{"/mcp?agent=claude", "proxy", "", "claude"},
		{"/mcp/code", "Gemini CLI", "", "gemini-cli"},
		{"/mcp/code?agent=codex", "", "", "codex"},
		{"/mcp/code?agent=codex-review", "", "", "codex-review"},
		{"/mcp", "", "codex_cli_rs/1.0", "codex"},
		{"/mcp", "", "generic-http/1.0", "mcp"},
		{"/mcp?agent=denis", "", "", "mcp"},
		{"/mcp/code", "Мой агент", "", "мой-агент"},
	} {
		id := fmt.Sprintf("AG-%02d", i+1)
		if strings.HasPrefix(tc.path, "/mcp/code") {
			call(tc.path, tc.client, tc.userAgent, "code", map[string]string{"code": fmt.Sprintf(`await codemode.create_tasks({tasks:[{project:"AG",title:"Test"}]}); await codemode.add_note({task_id:%q,body_md:"hello"});`, id)})
		} else {
			call(tc.path, tc.client, tc.userAgent, "create_tasks", map[string]any{"tasks": []map[string]string{{"project": "AG", "title": "Test"}}})
			call(tc.path, tc.client, tc.userAgent, "add_note", map[string]string{"task_id": id, "body_md": "hello"})
		}
		d, err := svc.GetTask(ctx, id, 20)
		if err != nil {
			t.Fatal(err)
		}
		if d.CreatedBy != tc.want || len(d.Notes) != 1 || d.Notes[0].Author != tc.want || len(d.Activity) != 2 {
			t.Fatalf("%s %q: creator=%s, notes=%v, activity=%v, want %s", tc.path, tc.client, d.CreatedBy, d.Notes, d.Activity, tc.want)
		}
		for _, a := range d.Activity {
			if a.Actor != tc.want {
				t.Fatalf("history actor %q, want %q", a.Actor, tc.want)
			}
		}
		n := 2
		if strings.HasPrefix(tc.path, "/mcp/code") {
			n++
		}
		calls, err := svc.ListMCPCalls(ctx, "all", n)
		if err != nil || len(calls) != n {
			t.Fatalf("calls: %v, %v", calls, err)
		}
		for _, c := range calls {
			if c.Actor != tc.want {
				t.Fatalf("%s logged as %q, want %q", c.Tool, c.Actor, tc.want)
			}
		}
	}
}
