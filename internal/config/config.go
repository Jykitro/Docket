// Package config reads the PLANDILLO_* environment. Older installations may still use the DOCKET_* names.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	DB             string
	BackupDir      string
	MCPToken       string // optional: when set it overrides the token stored in the database
	AllowedHosts   []string
	PasskeyOrigin  string
	Seed           string
	Loc            *time.Location
	SelfHost       bool   // PLANDILLO_MODE=selfhost: many users, each with their own database
	PostgresDSN    string // optional selfhost PostgreSQL database; empty keeps SQLite
	PostgresConns  int    // PostgreSQL connections kept open and reused between transactions
	RunnerSocket   string
	DataDir        string // directory of DB; holds auth.db and users/<id>.db in selfhost mode
	TrustedProxies string
	LogLevel       slog.Level // PLANDILLO_LOG_LEVEL: debug, info (default), warn or error
	LogJSON        bool       // PLANDILLO_LOG_FORMAT=json: one JSON object per line, for log collectors
	TLSCert        string     // optional certificate file: serve HTTPS (and so HTTP/2 to browsers) without a proxy
	TLSKey         string     // its private key; both or neither
	Push           bool       // PLANDILLO_PUSH=on: Web Push notifications, sent through the browsers' push services
}

func Load() (Config, error) {
	c := Config{
		Addr:           env("ADDR", "127.0.0.1:7777"),
		DB:             env("DB", defaultDB("/data")),
		BackupDir:      env("BACKUP_DIR", "/data/backups"),
		MCPToken:       env("MCP_TOKEN", ""),
		RunnerSocket:   env("CODE_SOCKET", "/run/plandillo-code/runner.sock"),
		PostgresDSN:    env("POSTGRES_DSN", ""),
		PasskeyOrigin:  env("PASSKEY_ORIGIN", ""),
		AllowedHosts:   strings.Split(env("ALLOWED_HOSTS", "localhost,127.0.0.1,plandillo.localhost"), ","),
		Seed:           env("SEED", ""),
		TrustedProxies: env("TRUSTED_PROXIES", ""),
		TLSCert:        env("TLS_CERT", ""),
		TLSKey:         env("TLS_KEY", ""),
		Loc:            time.Local,
	}
	conns, err := strconv.Atoi(env("POSTGRES_MAX_CONNS", "10"))
	if err != nil || conns < 1 || conns > 500 {
		return c, fmt.Errorf("PLANDILLO_POSTGRES_MAX_CONNS: want a whole number from 1 to 500")
	}
	c.PostgresConns = conns
	if err := c.LogLevel.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("PLANDILLO_LOG_LEVEL: want debug, info, warn or error")
	}
	switch f := env("LOG_FORMAT", "text"); f {
	case "text":
	case "json":
		c.LogJSON = true
	default:
		return c, fmt.Errorf("PLANDILLO_LOG_FORMAT: unknown format %q (want text or json)", f)
	}
	switch v := env("PUSH", "off"); v {
	case "off":
	case "on":
		c.Push = true
	default:
		return c, fmt.Errorf("PLANDILLO_PUSH: want on or off, got %q", v)
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return c, fmt.Errorf("PLANDILLO_TLS_CERT and PLANDILLO_TLS_KEY must be set together")
	}
	c.DataDir = filepath.Dir(c.DB)
	switch m := env("MODE", ""); m {
	case "", "local":
	case "selfhost":
		c.SelfHost = true
	default:
		return c, fmt.Errorf("PLANDILLO_MODE: unknown mode %q (want local or selfhost)", m)
	}
	if c.PostgresDSN != "" && !c.SelfHost {
		return c, fmt.Errorf("PLANDILLO_POSTGRES_DSN requires PLANDILLO_MODE=selfhost")
	}
	if tz := os.Getenv("TZ"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return c, err
		}
		c.Loc = loc
	}
	return c, nil
}

// env reads PLANDILLO_<name>, then the older DOCKET_<name>, so an existing .env keeps its database and settings.
func env(name, def string) string {
	for _, prefix := range []string{"PLANDILLO_", "DOCKET_"} {
		if v := os.Getenv(prefix + name); v != "" {
			return v
		}
	}
	return def
}

// defaultDB is plandillo.db in dir, except that an installation from the Docket days keeps its docket.db.
func defaultDB(dir string) string {
	current, old := filepath.Join(dir, "plandillo.db"), filepath.Join(dir, "docket.db")
	if _, err := os.Stat(current); os.IsNotExist(err) {
		if _, err := os.Stat(old); err == nil {
			return old
		}
	}
	return current
}
