package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"docket/internal/events"
	"docket/internal/service"
	"docket/internal/store"
)

// Handler panics at startup when a service.MCPTools entry has no registration (or the reverse); this builds
// it and drives the wiki tools through JSON-RPC in-process (httptest recorder, no port).
func TestWikiToolsOverMCP(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
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
		body := `{"jsonrpc":"2.0","id":` + string(rune('0'+id)) + `,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}`
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
}
