package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
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
		return err
	}
	defer ad.Close()

	ws := selfhost.NewWorkspaces(cfg.DataDir, cfg.Loc, func(svc *service.Service, bus *events.Bus) http.Handler {
		return httpapi.Router(httpapi.Deps{
			Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: ctx,
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
		w, err := ws.Open(ctx, u)
		if err != nil {
			return err
		}
		// shortcut: restart picks up users created after startup; move to a ticker over WS.All() when needed
		go maintenance(ctx, w.Svc, filepath.Join(cfg.BackupDir, strconv.FormatInt(u.ID, 10)))
	}

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
