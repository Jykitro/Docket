package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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

	if cfg.SelfHost {
		return runSelfHost(ctx, cfg)
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
		slog.Info("MCP token generated (shown once, see it later in the MCP tab)", "token", tok)
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
		MCP: mcpserver.Handler(svc, cfg.Loc), Web: web.Handler(),
	}))
}

// runSelfHost serves many users, each with their own database, behind a login. The shared env token and
// the seed are not used here: a token shared by all users would make MCP routing ambiguous.
func runSelfHost(ctx context.Context, cfg config.Config) error {
	if cfg.MCPToken != "" || cfg.Seed != "" {
		slog.Warn("DOCKET_MCP_TOKEN and DOCKET_SEED are ignored in selfhost mode")
	}
	ad, err := auth.Open(filepath.Join(cfg.DataDir, "auth.db"))
	if err != nil {
		return fmt.Errorf("open auth database: %w", err)
	}
	defer ad.Close()

	ws := selfhost.NewWorkspaces(ctx, cfg.DataDir, cfg.Loc, func(wctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{
			Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: wctx,
			MCP: mcpserver.Handler(svc, cfg.Loc), Web: web.Handler(),
		})
	})
	defer ws.Close()

	// Every workspace is opened now so MCP bearer lookup knows all users right after a restart.
	users, err := ad.ListUsers()
	if err != nil {
		return err
	}
	for _, u := range users {
		if _, err := ws.Open(ctx, u); err != nil {
			return fmt.Errorf("open workspace for user %d (%s): %w", u.ID, u.Username, err)
		}
	}
	go maintainAll(ctx, ws, cfg.BackupDir)

	var code string
	if len(users) == 0 {
		b := make([]byte, 18)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		code = base64.RawURLEncoding.EncodeToString(b)
		slog.Info("first run: create the admin with this setup code", "code", code)
	}

	return serve(ctx, cfg.Addr, selfhost.Handler(selfhost.Options{
		Auth: ad, WS: ws, Web: web.Handler(), AllowedHosts: cfg.AllowedHosts, SetupCode: code,
	}))
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
	slog.Info("docket listening", "addr", addr)

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
			} else {
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
