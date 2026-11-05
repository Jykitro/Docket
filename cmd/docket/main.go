package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo

	"docket/internal/config"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
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

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.Router(httpapi.Deps{
			Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: ctx,
			MCP: mcpserver.Handler(svc, cfg.Loc), Web: web.Handler(),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx }, // ends SSE streams on shutdown
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("docket listening", "addr", cfg.Addr)

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
