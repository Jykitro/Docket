// Package config reads the DOCKET_* environment.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DemoStorageBytes int64 // optional selfhost storage limit for non-administrator profiles
	Addr             string
	DB               string
	BackupDir        string
	MCPToken         string // optional: when set it overrides the token stored in the database
	AllowedHosts     []string
	PasskeyOrigin    string
	Seed             string
	Loc              *time.Location
	SelfHost         bool   // DOCKET_MODE=selfhost: many users, each with their own database
	PostgresDSN      string // optional selfhost PostgreSQL database; empty keeps SQLite
	RunnerSocket     string
	DataDir          string // directory of DB; holds auth.db and users/<id>.db in selfhost mode
}

func Load() (Config, error) {
	c := Config{
		Addr:          env("DOCKET_ADDR", "127.0.0.1:7777"),
		DB:            env("DOCKET_DB", "/data/docket.db"),
		BackupDir:     env("DOCKET_BACKUP_DIR", "/data/backups"),
		MCPToken:      os.Getenv("DOCKET_MCP_TOKEN"),
		RunnerSocket:  env("DOCKET_CODE_SOCKET", "/run/docket-code/runner.sock"),
		PostgresDSN:   os.Getenv("DOCKET_POSTGRES_DSN"),
		PasskeyOrigin: os.Getenv("DOCKET_PASSKEY_ORIGIN"),
		AllowedHosts:  strings.Split(env("DOCKET_ALLOWED_HOSTS", "localhost,127.0.0.1,docket.localhost"), ","),
		Seed:          os.Getenv("DOCKET_SEED"),
		Loc:           time.Local,
	}
	c.DataDir = filepath.Dir(c.DB)
	switch m := os.Getenv("DOCKET_MODE"); m {
	case "", "local":
	case "selfhost":
		c.SelfHost = true
	default:
		return c, fmt.Errorf("DOCKET_MODE: unknown mode %q (want local or selfhost)", m)
	}
	if c.PostgresDSN != "" && !c.SelfHost {
		return c, fmt.Errorf("DOCKET_POSTGRES_DSN requires DOCKET_MODE=selfhost")
	}
	if value := os.Getenv("PLANDILLO_DEMO_STORAGE_MB"); c.SelfHost && value != "" {
		mb, err := strconv.ParseInt(value, 10, 32)
		if err != nil || mb < 1 {
			return c, fmt.Errorf("PLANDILLO_DEMO_STORAGE_MB must be a positive integer")
		}
		c.DemoStorageBytes = mb * 1_000_000
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

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
