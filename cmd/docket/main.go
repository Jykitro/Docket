package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo

	"docket/internal/auth"
	"docket/internal/config"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/selfhost"
	"docket/internal/service"
	"docket/internal/store"
	"docket/internal/vercelrun"
	"docket/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "vercel", "vercel-replies":
			return runVercel(ctx, cfg, os.Args[1])
		case "setup-code":
			return printSetupCode(cfg.DataDir, os.Stdout)
		case "code-runner":
			return mcpserver.ServeRunner(ctx, cfg.RunnerSocket)
		case "code-eval":
			return mcpserver.Evaluate(os.Stdin, os.Stdout)
		default:
			return fmt.Errorf("unknown command %q", os.Args[1])
		}
	}
	if cfg.SelfHost {
		return runSelfHost(ctx, cfg, nil)
	}

	st, err := store.Open(cfg.DB)
	if err != nil {
		return err
	}
	defer st.Close()
	bus := events.NewBus()
	svc := service.New(st, bus, cfg.Loc)
	svc.SetEnvToken(cfg.MCPToken)
	if tok, err := svc.EnsureMCPSettings(ctx); err != nil {
		return err
	} else if tok != "" {
		slog.Info("MCP token generated; view it in the Integrations tab")
	}

	if cfg.Seed != "" {
		if _, err := os.Stat(cfg.Seed); err != nil {
			slog.Warn("seed file not found, skipping", "path", cfg.Seed)
		} else if imported, err := svc.Seed(ctx, cfg.Seed); err != nil {
			return err
		} else if imported {
			slog.Info("seed imported", "path", cfg.Seed)
		}
	}

	go maintenance(ctx, svc, cfg.BackupDir)

	return serve(ctx, cfg.Addr, httpapi.Router(httpapi.Deps{
		Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: ctx,
		MCP: mcpserver.IsolatedHandler(svc, cfg.Loc, mcpserver.NewRunner(cfg.RunnerSocket)), Web: web.Handler(),
	}))
}

// runSelfHost serves many users, each with their own database, behind a login. The shared env token and
// the seed are not used here: a token shared by all users would make MCP routing ambiguous.
func runSelfHost(ctx context.Context, cfg config.Config, remote *vercelrun.App) error {
	if cfg.MCPToken != "" { // DOCKET_SEED is ignored silently: .env.example sets it, and the README says so
		slog.Warn("DOCKET_MCP_TOKEN is ignored in selfhost mode: every profile has its own token")
	}
	var ad *auth.DB
	proxies, err := config.ParseTrustedProxies(os.Getenv("DOCKET_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	if cfg.PostgresDSN != "" {
		ad, err = auth.OpenPostgres(cfg.PostgresDSN)
	} else {
		ad, err = auth.Open(filepath.Join(cfg.DataDir, "auth.db"))
	}
	if err != nil {
		return fmt.Errorf("open auth database: %w", err)
	}
	defer ad.Close()

	sites, err := selfhost.PasskeySites(cfg.AllowedHosts, cfg.Addr, cfg.PasskeyOrigin)
	if err != nil {
		return err
	}
	runner := mcpserver.NewRunner(cfg.RunnerSocket)
	webHandler := web.Handler()
	buildRouter := func(wctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{
			Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: wctx, SelfHost: cfg.SelfHost,
			MCP: mcpserver.IsolatedHandler(svc, cfg.Loc, runner), Web: web.Handler(),
		})
	}
	var cluster *selfhost.Cluster
	ready := runner.Ready
	if remote != nil {
		cluster, err = selfhost.OpenCluster(ctx, cfg.PostgresDSN)
		if err != nil {
			return err
		}
		defer cluster.Close()
		remote.DB = cluster.DB
		ready = remote.Ready
		webHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/mcp" || r.URL.Path == "/mcp/code" {
				target := "/integrations"
				if r.URL.RawQuery != "" {
					target += "?" + r.URL.RawQuery
				}
				http.Redirect(w, r, target, http.StatusTemporaryRedirect)
				return
			}
			http.NotFound(w, r)
		})
		buildRouter = cloudRouter(cfg, remote, webHandler)
	}
	ws := selfhost.NewWorkspaces(ctx, cfg.DataDir, cfg.Loc, buildRouter)
	ws.UsePostgres(cfg.PostgresDSN)
	ws.DemoStorageBytes = cfg.DemoStorageBytes
	defer ws.Close()

	var code string
	var clearSetup func()
	if cluster != nil {
		if err = cluster.Initialize(ctx, ws, ad); err != nil {
			return err
		}
		n, err := ad.UserCount()
		if err != nil {
			return err
		}
		code = os.Getenv("PLANDILLO_SETUP_CODE")
		if n == 0 && len(code) < 32 {
			return errors.New("first setup requires PLANDILLO_SETUP_CODE with at least 32 characters")
		}
	} else {
		users, err := ad.ListUsers()
		if err != nil {
			return err
		}
		if err = ws.VerifyOwners(ctx, users, ad); err != nil {
			return err
		}
		for _, u := range users {
			if _, err := ws.Open(ctx, u); err != nil {
				return fmt.Errorf("open workspace for user %d: %w", u.ID, err)
			}
		}
		go maintainAll(ctx, ws, cfg.BackupDir)
		setupPath := filepath.Join(cfg.DataDir, "setup-code")
		clearSetup = func() {
			if err := os.Remove(setupPath); err != nil && !os.IsNotExist(err) {
				slog.Error("remove consumed setup code", "err", err)
			}
		}
		if len(users) == 0 {
			code, err = createSetupCode(cfg.DataDir)
			if err != nil {
				return err
			}
			slog.Info("first run: retrieve the administrator setup code with plandillo setup-code")
		} else {
			clearSetup()
		}
	}
	if cfg.PostgresDSN != "" {
		slog.Info("PostgreSQL storage enabled; configure external database backups (pg_dump or PITR)")
	}
	handler := selfhost.Handler(selfhost.Options{Cluster: cluster, VercelIngress: cluster != nil && os.Getenv("VERCEL") == "1" && (os.Getenv("VERCEL_ENV") == "production" || os.Getenv("VERCEL_ENV") == "preview"),
		SetupComplete: clearSetup, TrustedProxies: proxies, Passkeys: sites, Auth: ad, WS: ws, Web: webHandler, AllowedHosts: cfg.AllowedHosts, SetupCode: code, RunnerReady: ready,
	})
	if remote != nil && os.Getenv("VERCEL_ENV") == "development" {
		handler = developmentOrigin(handler, cfg.PasskeyOrigin)
	}
	return serve(ctx, cfg.Addr, handler)
}

// createSetupCode publishes each process's bootstrap credential only after the file is complete.
func createSetupCode(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, ".setup-code-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	code := rand.Text()
	_, err = fmt.Fprintln(f, code)
	closeErr := f.Close()
	if err = errors.Join(err, closeErr); err != nil {
		return "", err
	}
	if err = os.Rename(f.Name(), filepath.Join(dir, "setup-code")); err != nil {
		return "", err
	}
	return code, nil
}
func printSetupCode(dir string, w io.Writer) error {
	data, err := os.ReadFile(filepath.Join(dir, "setup-code"))
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("no setup code pending; start an unconfigured selfhost instance first")
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, strings.TrimSpace(string(data)))
	return err
}

// serve runs h on addr until ctx ends, then shuts down gracefully.
func serve(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx }, // ends SSE streams on shutdown
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("Plandillo listening", "addr", addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	slog.Info("stopped")
	return nil
}

// maintenance writes a backup (keeping 14) and trims the MCP call log at start and then every 24h.
func maintenance(ctx context.Context, svc *service.Service, dir string) {
	for {
		if path, err := svc.Maintenance(ctx, dir); err != nil {
			slog.Error("maintenance failed", "err", err)
		} else {
			slog.Info("backup written", "path", path)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}

// maintainAll runs maintenance for every open selfhost profile: at start and then hourly it backs up each profile
// not done in the last 24h, so profiles created at runtime are picked up and deleted ones dropped.
func maintainAll(ctx context.Context, ws *selfhost.Workspaces, backupDir string) {
	last := map[int64]time.Time{}
	for {
		maintainPass(ws.All(), last, time.Now(), func(w *selfhost.Workspace) {
			if path, err := w.Svc.Maintenance(ctx, filepath.Join(backupDir, strconv.FormatInt(w.ID, 10))); err != nil {
				slog.Error("maintenance failed", "err", err)
			} else if path != "" {
				slog.Info("backup written", "path", path)
			}
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
}

// maintainPass calls backup for each workspace never processed or processed 24h or more before now.
func maintainPass(all []*selfhost.Workspace, last map[int64]time.Time, now time.Time, backup func(*selfhost.Workspace)) {
	for _, w := range all {
		if t, ok := last[w.ID]; !ok || now.Sub(t) >= 24*time.Hour {
			backup(w)
			last[w.ID] = now
		}
	}
}
