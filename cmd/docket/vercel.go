package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"

	"docket/internal/config"
	"docket/internal/events"
	"docket/internal/httpapi"
	"docket/internal/mcpserver"
	"docket/internal/selfhost"
	"docket/internal/service"
	"docket/internal/vercelrun"
)

func runVercel(ctx context.Context, cfg config.Config) error {
	if cfg.PostgresDSN == "" {
		cfg.PostgresDSN = os.Getenv("DATABASE_URL_UNPOOLED")
	}
	if !cfg.SelfHost || cfg.PostgresDSN == "" {
		return errors.New("Vercel requires DOCKET_MODE=selfhost and DOCKET_POSTGRES_DSN or DATABASE_URL_UNPOOLED")
	}
	if os.Getenv("DOCKET_ALLOWED_HOSTS") == "" {
		return errors.New("configure DOCKET_ALLOWED_HOSTS for the planner domain")
	}
	var err error
	cfg.Addr, err = vercelrun.ListenPort(os.Getenv("PORT"))
	if err != nil {
		return err
	}
	remote := &vercelrun.App{URL: os.Getenv("PLANDILLO_RUNNER_URL"), Secret: os.Getenv("PLANDILLO_RUNNER_SECRET")}
	if err = remote.Validate(); err != nil {
		return err
	}
	return runSelfHost(ctx, cfg, remote)
}

func cloudRouter(cfg config.Config, remote *vercelrun.App, web http.Handler) func(context.Context, *service.Service, *events.Bus) http.Handler {
	return func(ctx context.Context, svc *service.Service, bus *events.Bus) http.Handler {
		owner := selfhost.WorkspaceOwner(ctx)
		mcp := mcpserver.WithEvaluator(svc, cfg.Loc, func(ctx context.Context, code string, call func(string, json.RawMessage) (string, error)) (string, error) {
			return remote.Execute(ctx, owner, code, call)
		})
		return httpapi.Router(httpapi.Deps{Svc: svc, Bus: bus, AllowedHosts: cfg.AllowedHosts, SelfAddr: cfg.Addr, Ctx: ctx, SelfHost: true, Web: web, MCP: mcp, OnDemandCheck: true})
	}
}

// vercel dev replaces Host with the container's ephemeral host port. The explicit
// WebAuthn origin also pins the browser-facing development origin for CSRF checks.
func developmentOrigin(handler http.Handler, configured string) http.Handler {
	public, err := url.Parse(configured)
	if err != nil || public.Scheme != "http" || public.Hostname() != "localhost" || public.Host == "" {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		loopback := host == "localhost" || net.ParseIP(host).IsLoopback()
		if loopback && r.Header.Get("X-Vercel-Deployment-Url") == public.Host {
			clone := r.Clone(r.Context())
			clone.Host = public.Host
			r = clone
		}
		handler.ServeHTTP(w, r)
	})
}
