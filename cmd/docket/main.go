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
		case "code-runner":
			return mcpserver.ServeRunner(ctx, cfg.RunnerSocket)
		case "code-eval":
			return mcpserver.Evaluate(os.Stdin, os.Stdout)
		default:
			return fmt.Errorf("unknown command %q", os.Args[1])
		}
	}
	return runSelfHost(ctx, cfg)
}

// Both modes share the catalog. Selfhost resolves owners from authenticated sessions.
func runSelfHost(ctx context.Context, cfg config.Config) error {
	if cfg.SelfHost && cfg.MCPToken != "" { // PLANDILLO_SEED is ignored silently: .env.example sets it, and the README says so
		slog.Warn("PLANDILLO_MCP_TOKEN is ignored in selfhost mode: every workspace has its own token")
	}
	var ad *auth.DB
	proxies, err := config.ParseTrustedProxies(cfg.TrustedProxies)
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
		ad.Connection().SetPool(cfg.PostgresConns)
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
	buildRouter := func(wctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{
			Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: wctx, SelfHost: cfg.SelfHost, OnDemandCheck: true,
			MCP: mcpserver.IsolatedHandler(svc, cfg.Loc, runner), Web: web.Handler(),
		})
	}
	ws := selfhost.NewWorkspaces(ctx, cfg.DataDir, cfg.Loc, buildRouter)
	ws.UsePostgres(cfg.PostgresDSN)
	envToken := ""
	if localID != 0 {
		envToken = cfg.MCPToken
	}
	ws.UseShared(shared, envToken, firstID)
	defer ws.Close()

	{
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
	var backup func(time.Time) (string, error) // on-demand snapshots for the admin Backups page; SQLite only
	if cfg.PostgresDSN == "" {
		backup = func(now time.Time) (string, error) {
			path, err := shared.Backup(cfg.BackupDir, store.BackupKeep, now)
			if err == nil {
				err = shared.RecordBackup(now)
			}
			return path, err
		}
	}
	handler := selfhost.Handler(selfhost.Options{LocalID: localID,
		TrustedProxies: proxies, Passkeys: sites, Auth: ad, WS: ws, Web: web.Handler(), AllowedHosts: cfg.AllowedHosts, RunnerReady: runner.Ready,
		BackupDir: cfg.BackupDir, Backup: backup,
	})
	return serve(ctx, cfg, handler)
}

// newServer configures the HTTP server. It speaks HTTP/1.1 and HTTP/2: over TLS when PLANDILLO_TLS_CERT is set
// (browsers then multiplex every request and the live-update stream over one connection), and as cleartext h2c
// with prior knowledge for a reverse proxy that forwards that way (Caddy: reverse_proxy h2c://...). Go's server has
// no HTTP/3; a proxy in front provides it (docs/hosting.md).
func newServer(ctx context.Context, addr string, h http.Handler) *http.Server {
	var p http.Protocols
	p.SetHTTP1(true)
	p.SetHTTP2(true)
	p.SetUnencryptedHTTP2(true)
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		Protocols:         &p,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,                                   // drop idle keep-alive connections; open streams (SSE) are not idle
		BaseContext:       func(net.Listener) context.Context { return ctx }, // ends SSE streams on shutdown
	}
}

// serve runs h until ctx ends, then shuts down gracefully.
func serve(ctx context.Context, cfg config.Config, h http.Handler) error {
	srv := newServer(ctx, cfg.Addr, h)
	errc := make(chan error, 1)
	if cfg.TLSCert != "" {
		go func() { errc <- srv.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey) }()
	} else {
		go func() { errc <- srv.ListenAndServe() }()
	}
	slog.Info("Plandillo listening", "addr", cfg.Addr, "tls", cfg.TLSCert != "")

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
		if path, err := base.Backup(dir, store.BackupKeep, now); err != nil {
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
