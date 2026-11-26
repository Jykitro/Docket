package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"plandillo/internal/codeeval"
	"plandillo/internal/events"
	"plandillo/internal/httpapi"
	"plandillo/internal/mcpserver"
	"plandillo/internal/service"
	"plandillo/internal/store"
	"plandillo/skills"

	"golang.org/x/oauth2"
)

// board is a real Plandillo router (bearer guard, MCP and Code Mode) with project CL.
type board struct {
	url, token string
	svc        *service.Service
	env        map[string]string
}

func newBoard(t *testing.T) *board {
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
	if _, err := svc.CreateProject(context.Background(), service.ActorDenis, service.ProjectInput{Name: "CLI", Key: "CL"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel) // runs before the store is closed
	eval := func(ctx context.Context, code string, call func(string, json.RawMessage) (string, error)) (string, error) {
		return codeeval.Run(ctx, 10*time.Second, code, call)
	}
	srv := httptest.NewUnstartedServer(nil)
	srv.Config.Handler = httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: []string{"127.0.0.1"},
		MCP: mcpserver.WithEvaluator(svc, time.UTC, eval), Web: http.NotFoundHandler(), SelfAddr: srv.Listener.Addr().String(), Ctx: ctx})
	srv.Start()
	t.Cleanup(srv.Close)
	return &board{url: srv.URL, token: token, svc: svc,
		env: map[string]string{"PLANDILLO_URL": srv.URL, "PLANDILLO_CLI_CONFIG": filepath.Join(t.TempDir(), "cli.json"), "HOME": t.TempDir()}}
}

func (b *board) run(stdin string, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(context.Background(), args, strings.NewReader(stdin), &out, &errb, func(k string) string { return b.env[k] })
	return code, out.String(), errb.String()
}

func (b *board) must(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errs := b.run(stdin, args...)
	if code != exitOK {
		t.Fatalf("%v: exit %d, stderr %q", args, code, errs)
	}
	return out
}

func TestToolCallsWithWorkspaceToken(t *testing.T) {
	b := newBoard(t)
	b.env["PLANDILLO_TOKEN"] = b.token

	if out := b.must(t, "", "--agent", "claude", "create_tasks", `{"tasks":[{"project":"CL","title":"Ship the CLI"}]}`); !strings.Contains(out, `"CL-01"`) {
		t.Fatalf("create_tasks printed %q", out)
	}
	// The agent name reaches the task history, as with an MCP client.
	if out := b.must(t, "", "get_task", "--id", "CL-01"); !strings.Contains(out, "Ship the CLI") || !strings.Contains(out, `"actor":"claude"`) {
		t.Fatalf("get_task printed %q", out)
	}
	if out := b.must(t, `{"id":"CL-01"}`, "get_task", "-"); !strings.Contains(out, "Ship the CLI") {
		t.Fatalf("get_task from stdin printed %q", out)
	}
	if out := b.must(t, "", "search", "--q", "Ship", "--limit", "5"); !strings.Contains(out, "CL-01") {
		t.Fatalf("search printed %q", out)
	}
	if out := b.must(t, "", "--pretty", "get_task", "--id", "CL-01"); !strings.Contains(out, "\n  \"id\": \"CL-01\"") {
		t.Fatalf("--pretty printed %q", out)
	}
	if out := b.must(t, "", "code", "return (await codemode.get_task({id: 'CL-01'})).title"); !strings.Contains(out, "Ship the CLI") {
		t.Fatalf("code printed %q", out)
	}
	if out := b.must(t, "", "tools"); !strings.Contains(out, "create_tasks    write") || !strings.Contains(out, "get_today       read") {
		t.Fatalf("tools printed %q", out)
	}
}

func TestExitCodes(t *testing.T) {
	b := newBoard(t)
	b.env["PLANDILLO_TOKEN"] = b.token
	for _, tc := range []struct {
		name   string
		env    map[string]string
		args   []string
		code   int
		stderr string
	}{
		{"tool error", nil, []string{"get_task", "--id", "CL-99"}, exitTool, "not_found:"},
		{"no command", nil, nil, exitUsage, "Usage:"},
		{"unknown command", nil, []string{"frobnicate"}, exitUsage, `unknown command "frobnicate"`},
		{"unknown option", nil, []string{"get_task", "--nope", "x"}, exitUsage, "options: --id"},
		{"bad integer", nil, []string{"search", "--q", "x", "--limit", "many"}, exitUsage, "--limit"},
		{"bad JSON", nil, []string{"get_task", "{"}, exitUsage, "JSON object"},
		{"missing argument", nil, []string{"get_task"}, exitUsage, "missing --id"},
		{"wrong token", map[string]string{"PLANDILLO_TOKEN": "wrong"}, []string{"get_today"}, exitAuth, "HTTP 401"},
		{"no board", map[string]string{"PLANDILLO_URL": "http://127.0.0.1:1"}, []string{"get_today"}, exitNetwork, "cannot reach"},
		{"bad address", map[string]string{"PLANDILLO_URL": "ftp://x"}, []string{"get_today"}, exitUsage, "board address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := map[string]string{}
			for k, v := range tc.env {
				saved[k] = b.env[k]
				b.env[k] = v
			}
			defer func() {
				for k, v := range saved {
					b.env[k] = v
				}
			}()
			code, _, errs := b.run("", tc.args...)
			if code != tc.code || !strings.Contains(errs, tc.stderr) {
				t.Fatalf("exit %d, stderr %q; want %d with %q", code, errs, tc.code, tc.stderr)
			}
		})
	}
}

func TestLoginWithTokenIsSavedForThatBoardOnly(t *testing.T) {
	b := newBoard(t)
	if code, _, errs := b.run(b.token+"\n", "login", "--with-token"); code != exitOK || !strings.Contains(errs, "Signed in") {
		t.Fatalf("login: exit %d, %q", code, errs)
	}
	path := b.env["PLANDILLO_CLI_CONFIG"]
	if fi, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("config file: %v %v", fi.Mode(), err)
	}
	b.must(t, "", "get_today")
	if out := b.must(t, "", "status"); !strings.Contains(out, "sign-in: workspace token") || !strings.Contains(out, "access:  read-write") ||
		!strings.Contains(out, "skills:  not installed") {
		t.Fatalf("status printed %q", out)
	}

	// Another board never receives the saved token.
	var got []string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer other.Close()
	if code, _, _ := b.run("", "--url", other.URL, "get_today"); code != exitAuth || len(got) == 0 || strings.Join(got, "") != "" {
		t.Fatalf("other board: exit %d, Authorization headers %q", code, got)
	}

	b.must(t, "", "logout")
	if code, _, _ := b.run("", "get_today"); code != exitAuth {
		t.Fatalf("after logout: exit %d", code)
	}
}

func TestReadOnlyBoardHidesWriteTools(t *testing.T) {
	b := newBoard(t)
	b.env["PLANDILLO_TOKEN"] = b.token
	mode := service.ModeReadOnly
	if _, err := b.svc.UpdateMCPSettings(context.Background(), service.MCPSettingsPatch{AccessMode: &mode}); err != nil {
		t.Fatal(err)
	}
	if out := b.must(t, "", "status"); !strings.Contains(out, "access:  read-only") {
		t.Fatalf("status printed %q", out)
	}
	if code, _, errs := b.run("", "create_tasks", `{"tasks":[{"project":"CL","title":"x"}]}`); code != exitAuth || !strings.HasPrefix(errs, "forbidden:") {
		t.Fatalf("write in read-only mode: exit %d, %q", code, errs)
	}
}

// A saved browser sign-in is refreshed when it expires and the rotated refresh token is saved.
func TestBrowserSignInIsRefreshedAndSaved(t *testing.T) {
	b := newBoard(t)
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("grant_type") != "refresh_token" || r.FormValue("refresh_token") != "r1" {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
			return
		}
		// The local board accepts its workspace token, which stands in for a fresh access token here.
		fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","refresh_token":"r2","expires_in":3600}`, b.token)
	}))
	defer tokens.Close()
	a := &app{getenv: func(k string) string { return b.env[k] }}
	save := func(refresh string) {
		t.Helper()
		err := a.saveConfig(&config{URL: b.url, OAuth: map[string]*oauthGrant{"/mcp": {ClientID: "c", TokenURL: tokens.URL, AuthStyle: oauth2.AuthStyleInParams,
			Token: &oauth2.Token{AccessToken: "old", RefreshToken: refresh, Expiry: time.Now().Add(-time.Hour)}}}})
		if err != nil {
			t.Fatal(err)
		}
	}

	save("r1")
	b.must(t, "", "get_today")
	cfg, err := a.loadConfig()
	if err != nil || cfg.OAuth["/mcp"].Token.RefreshToken != "r2" || cfg.OAuth["/mcp"].Token.AccessToken != b.token {
		t.Fatalf("saved after refresh: %+v %v", cfg.OAuth["/mcp"].Token, err)
	}

	save("revoked")
	if code, _, errs := b.run("", "get_today"); code != exitAuth || !strings.Contains(errs, "expired") {
		t.Fatalf("revoked refresh token: exit %d, %q", code, errs)
	}
}

func TestBuildArgs(t *testing.T) {
	schema := map[string]any{"properties": map[string]any{
		"id": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer"},
		"include_done": map[string]any{"type": "boolean"}, "ids": map[string]any{"type": []any{"null", "array"}},
		"planned_for": map[string]any{"type": []any{"null", "string"}},
	}}
	got, err := buildArgs([]string{`{"id":"A","limit":1}`, "--limit", "5", "--include_done", "--ids", `["X-1"]`, "--planned_for="}, schema, nil)
	want := map[string]any{"id": "A", "limit": int64(5), "include_done": true, "ids": []any{"X-1"}, "planned_for": ""}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, %v", got, err)
	}
	file := filepath.Join(t.TempDir(), "args.json")
	os.WriteFile(file, []byte(`{"id":"B"}`), 0o600)
	if got, err := buildArgs([]string{"@" + file}, schema, nil); err != nil || got["id"] != "B" {
		t.Fatalf("@file: %#v, %v", got, err)
	}
	if _, err := buildArgs([]string{`{}`, `{}`}, schema, nil); err == nil {
		t.Fatal("two JSON arguments accepted")
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://plans.example.com/mcp":   "https://plans.example.com",
		"http://localhost:7777/mcp/code/": "http://localhost:7777",
		"https://example.com/board/mcp":   "https://example.com/board",
		"https://example.com/?x=1#y":      "https://example.com",
	} {
		if got, err := normalizeURL(in); err != nil || got != want {
			t.Errorf("%s: got %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"ftp://x", "localhost:7777", "https://user:pw@example.com"} {
		if _, err := normalizeURL(in); err == nil {
			t.Errorf("%s accepted", in)
		}
	}
}

func TestCallbackHandler(t *testing.T) {
	codes := make(chan callback, 1)
	h := callbackHandler(codes)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/callback?code=c1&state=s1&iss=https://b", nil))
	if cb := <-codes; cb.err != nil || cb.res.Code != "c1" || cb.res.State != "s1" || cb.res.Iss != "https://b" {
		t.Fatalf("code: %+v", cb)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/callback?error=access_denied", nil))
	if cb := <-codes; cb.err == nil || w.Code != http.StatusBadRequest {
		t.Fatalf("refusal: %+v %d", cb, w.Code)
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/favicon.ico", nil))
	select {
	case cb := <-codes:
		t.Fatalf("stray request delivered %+v", cb)
	default:
	}
}

// The detector compares installed skills with the ones built into the CLI; update rewrites only stale or missing files.
func TestSkillsStatusAndUpdate(t *testing.T) {
	t.Chdir(t.TempDir()) // no project skills folder
	home := t.TempDir()
	b := &board{env: map[string]string{"HOME": home}}
	skillsJSON := func(args ...string) (report struct {
		Skills   []skillState
		ToUpdate int `json:"to_update"`
		Updated  []string
	}, stderr string) {
		t.Helper()
		code, out, errs := b.run("", args...)
		if code != exitOK || json.Unmarshal([]byte(out), &report) != nil {
			t.Fatalf("%v: exit %d %q %q", args, code, out, errs)
		}
		return report, errs
	}

	if code, _, errs := b.run("", "skills", "update"); code != exitUsage || !strings.Contains(errs, "--agent claude-code") {
		t.Fatalf("update without installed skills: exit %d %q", code, errs)
	}
	claude := filepath.Join(home, ".claude", "skills")
	os.MkdirAll(filepath.Join(claude, "plandillo"), 0o755)
	os.WriteFile(filepath.Join(claude, "plandillo", "SKILL.md"), []byte("old"), 0o644)

	report, errs := skillsJSON("skills")
	if report.ToUpdate != len(skills.Names()) || !strings.Contains(errs, "skills update") {
		t.Fatalf("one outdated and the rest missing, got %+v %q", report, errs)
	}
	if report, _ := skillsJSON("skills", "update"); len(report.Updated) != len(skills.Names()) {
		t.Fatalf("update wrote %v", report.Updated)
	}
	for _, name := range skills.Names() {
		want, _ := skills.Get(name)
		if got, _ := os.ReadFile(filepath.Join(claude, name, "SKILL.md")); string(got) != want {
			t.Errorf("%s not updated", name)
		}
	}
	if report, _ := skillsJSON("skills"); report.ToUpdate != 0 {
		t.Fatalf("after update: %+v", report)
	}
	if report, _ := skillsJSON("skills", "update"); len(report.Updated) != 0 {
		t.Fatalf("a second update rewrote %v", report.Updated)
	}
	if report, _ := skillsJSON("skills", "update", "--agent", "codex"); len(report.Updated) != len(skills.Names()) {
		t.Fatalf("--agent codex installed %v", report.Updated)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "plandillo", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := b.run("", "skills", "update", "--agent", "cursor"); code != exitUsage {
		t.Fatalf("unknown agent: exit %d", code)
	}
}
