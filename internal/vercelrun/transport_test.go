package vercelrun_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"docket/internal/auth"
	"docket/internal/selfhost"
	"docket/internal/testutil"
	"docket/internal/vercelrun"
)

func TestIsolatedWorkerWithCrossReplicaReply(t *testing.T) {
	dsn := testutil.PostgresDSN(t)
	db, err := auth.OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	caller, err := selfhost.OpenCluster(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer caller.Close()
	receiver, err := selfhost.OpenCluster(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	reply := vercelrun.Replies{DB: receiver.DB}
	callbacks := httptest.NewServer(reply)
	defer callbacks.Close()
	guest := filepath.Join(t.TempDir(), "eval.wasm")
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", guest, "../../cmd/code-guest")
	build.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build WASI guest: %v %s", err, output)
	}
	wasm, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Repeat("x", 32)
	worker, err := vercelrun.NewWorker(ctx, wasm, callbacks.URL, secret)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close(context.Background())
	endpoint := httptest.NewServer(worker)
	defer endpoint.Close()
	app := vercelrun.App{URL: endpoint.URL, Secret: secret, DB: caller.DB}
	if err = app.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	calls := 0
	result, err := app.Execute(ctx, "test-owner", "return [typeof fetch, typeof process, await codemode.list_projects({})]", func(name string, args json.RawMessage) (string, error) {
		calls++
		if name != "list_projects" || string(args) != "{}" {
			t.Errorf("unexpected callback %s", name)
		}
		return `["test-board"]`, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || !strings.Contains(result, `["undefined","undefined",["test-board"]]`) {
		t.Fatal("unexpected Code Mode result", result)
	}
	var remaining int
	if err = caller.DB.QueryRow("SELECT count(*) FROM vercel_jobs").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("worker job retained", remaining, err)
	}
}
