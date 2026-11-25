package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo

	"docket/internal/auth"
	"docket/internal/config"
	"docket/internal/database"
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
		case "code-runner":
			return mcpserver.ServeRunner(ctx, cfg.RunnerSocket)
		case "code-eval":
			return mcpserver.Evaluate(os.Stdin, os.Stdout)
		default:
			return fmt.Errorf("unknown command %q", os.Args[1])
		}
	}
	return runSelfHost(ctx, cfg, nil)
}

// Both modes share the catalog. Selfhost resolves owners from authenticated sessions.
func runSelfHost(ctx context.Context, cfg config.Config, remote *vercelrun.App) error {
	if cfg.SelfHost && cfg.MCPToken != "" { // DOCKET_SEED is ignored silently: .env.example sets it, and the README says so
		slog.Warn("DOCKET_MCP_TOKEN is ignored in selfhost mode: every workspace has its own token")
	}
	var ad *auth.DB
	proxies, err := config.ParseTrustedProxies(os.Getenv("DOCKET_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	if cfg.PostgresDSN != "" {
		ad, err = auth.OpenPostgres(cfg.PostgresDSN)
	} else {
		for _, name := range []string{"auth.db", "users"} {
			legacy := filepath.Join(cfg.DataDir, name)
			if filepath.Clean(legacy) == filepath.Clean(cfg.DB) {
				continue
			}
			_, statErr := os.Stat(legacy)
			if statErr == nil {
				return fmt.Errorf("previous selfhost layout detected in %s; choose an empty data directory", cfg.DataDir)
			}
			if !os.IsNotExist(statErr) {
				return statErr
			}
		}
		if err = database.CheckSQLiteLayout(cfg.DB); err != nil {
			return err
		}
		ad, err = auth.Open(cfg.DB)
	}
	if err != nil {
		return fmt.Errorf("open auth database: %w", err)
	}
	defer ad.Close()
	mode := "selfhost"
	if !cfg.SelfHost {
		mode = "local"
	}
	if err = ad.PinMode(mode); err != nil {
		return err
	}
	if cfg.SelfHost {
		key, err := totpKey(cfg.DataDir, cfg.PostgresDSN != "")
		if err != nil {
			return err
		}
		if err = ad.ConfigureTOTP(key); err != nil {
			return err
		}
	}
	if cfg.PostgresDSN != "" {
		ad.Connection().SetMaxOpenConns(8)
	}
	shared, err := store.Shared(ad.Connection(), cfg.DB)
	if err != nil {
		return err
	}
	var localID, firstID int64
	if !cfg.SelfHost {
		localID, err = store.EnsureLocalUser(ad.Connection())
		if err != nil {
			return err
		}
		firstID = 1 // immutable first workspace of a clean local installation
	}

	sites, err := selfhost.PasskeySites(cfg.AllowedHosts, cfg.Addr, cfg.PasskeyOrigin)
	if err != nil {
		return err
	}
	runner := mcpserver.NewRunner(cfg.RunnerSocket)
	webHandler := web.Handler()
	buildRouter := func(wctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{
			Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: wctx, SelfHost: cfg.SelfHost, OnDemandCheck: true,
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
	envToken := ""
	if localID != 0 {
		envToken = cfg.MCPToken
	}
	ws.UseShared(shared, envToken, firstID)
	ws.DemoStorageBytes = cfg.DemoStorageBytes
	defer ws.Close()

	code := os.Getenv("PLANDILLO_SETUP_CODE") // optional; without it the first sign-up becomes the administrator
	if code != "" && len(code) < 32 {
		return errors.New("PLANDILLO_SETUP_CODE must have at least 32 characters")
	}
	if cluster != nil {
		if err = cluster.Initialize(ctx, ws, ad); err != nil {
			return err
		}
	} else {
		userCount, err := ad.UserCount()
		if err != nil {
			return err
		}

		if localID != 0 && cfg.Seed != "" {
			u, err := ad.ByID(localID)
			if err != nil {
				return err
			}
			ws, err := ws.Open(ctx, u)
			if err != nil {
				return err
			}
			if _, statErr := os.Stat(cfg.Seed); statErr == nil {
				if _, err = ws.Svc.Seed(ctx, cfg.Seed); err != nil {
					return err
				}
			}
		}
		go maintenanceShared(ctx, shared, cfg.BackupDir)
		// Earlier versions kept a one-time setup code here; it is no longer used.
		if err := os.Remove(filepath.Join(cfg.DataDir, "setup-code")); err != nil && !os.IsNotExist(err) {
			slog.Error("remove old setup code", "err", err)
		}
		if userCount == 0 && localID == 0 {
			slog.Info("first run: open Plandillo in a browser; the first account you create becomes the administrator")
		}
	}
	if cfg.PostgresDSN != "" {
		slog.Info("PostgreSQL storage enabled; configure external database backups (pg_dump or PITR)")
	}
	handler := selfhost.Handler(selfhost.Options{LocalID: localID, Cluster: cluster, VercelIngress: cluster != nil && os.Getenv("VERCEL") == "1" && (os.Getenv("VERCEL_ENV") == "production" || os.Getenv("VERCEL_ENV") == "preview"),
		TrustedProxies: proxies, Passkeys: sites, Auth: ad, WS: ws, Web: webHandler, AllowedHosts: cfg.AllowedHosts, SetupCode: code, RunnerReady: ready,
	})
	if remote != nil && os.Getenv("VERCEL_ENV") == "development" {
		handler = developmentOrigin(handler, cfg.PasskeyOrigin)
	}
	return serve(ctx, cfg.Addr, handler)
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

func maintenanceShared(ctx context.Context, base *store.Store, dir string) {
	for {
		now := time.Now()
		if path, err := base.Backup(dir, 14, now); err != nil {
			slog.Error("database backup failed", "err", err)
		} else if path != "" {
			if err := base.RecordBackup(now); err != nil {
				slog.Error("record database backup", "err", err)
			}
		}
		if err := base.PurgeExpiredCalls(ctx, now); err != nil {
			slog.Error("clean MCP log", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(24 * time.Hour):
		}
	}
}
