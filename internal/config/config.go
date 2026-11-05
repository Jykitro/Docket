// Package config reads the DOCKET_* environment.
package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Addr         string
	DB           string
	BackupDir    string
	MCPToken     string // optional: when set it overrides the token stored in the database
	AllowedHosts []string
	Seed         string
	Loc          *time.Location
}

func Load() (Config, error) {
	c := Config{
		Addr:         env("DOCKET_ADDR", ":7777"),
		DB:           env("DOCKET_DB", "/data/docket.db"),
		BackupDir:    env("DOCKET_BACKUP_DIR", "/data/backups"),
		MCPToken:     os.Getenv("DOCKET_MCP_TOKEN"),
		AllowedHosts: strings.Split(env("DOCKET_ALLOWED_HOSTS", "localhost,127.0.0.1,docket.localhost"), ","),
		Seed:         os.Getenv("DOCKET_SEED"),
		Loc:          time.Local,
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
