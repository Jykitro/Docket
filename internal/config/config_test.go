package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A developer's shell may still export DOCKET_* names; Load is tested without them.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "DOCKET_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

func TestLoadMode(t *testing.T) {
	t.Setenv("PLANDILLO_POSTGRES_DSN", "")
	db := filepath.Join(t.TempDir(), "sub", "plandillo.db")
	for _, tc := range []struct {
		mode     string
		selfhost bool
		wantErr  bool
	}{
		{"", false, false},
		{"local", false, false},
		{"selfhost", true, false},
		{"bogus", false, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv("PLANDILLO_MODE", tc.mode)
			t.Setenv("PLANDILLO_DB", db)
			c, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if c.SelfHost != tc.selfhost {
				t.Fatalf("SelfHost = %v, want %v", c.SelfHost, tc.selfhost)
			}
			if !tc.wantErr && c.DataDir != filepath.Dir(db) {
				t.Fatalf("DataDir = %q, want %q", c.DataDir, filepath.Dir(db))
			}
		})
	}
}

func TestLoadPostgres(t *testing.T) {
	t.Setenv("PLANDILLO_POSTGRES_DSN", "postgres://user:secret@db/plandillo")
	t.Setenv("PLANDILLO_MODE", "local")
	if _, err := Load(); err == nil {
		t.Fatal("PostgreSQL in local mode must fail")
	}
	t.Setenv("PLANDILLO_MODE", "selfhost")
	c, err := Load()
	if err != nil || c.PostgresDSN != "postgres://user:secret@db/plandillo" {
		t.Fatalf("Postgres configuration: %v", err)
	}
}

func TestLoadPostgresPoolSize(t *testing.T) {
	t.Setenv("PLANDILLO_MODE", "local")
	t.Setenv("PLANDILLO_POSTGRES_DSN", "")
	for _, tc := range []struct {
		value   string
		want    int
		wantErr bool
	}{
		{"", 10, false},
		{"20", 20, false},
		{"1", 1, false},
		{"0", 0, true},
		{"501", 0, true},
		{"twenty", 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("PLANDILLO_POSTGRES_MAX_CONNS", tc.value)
			c, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && c.PostgresConns != tc.want {
				t.Fatalf("PostgresConns = %d, want %d", c.PostgresConns, tc.want)
			}
		})
	}
}

func TestLoadLogging(t *testing.T) {
	t.Setenv("PLANDILLO_MODE", "local")
	t.Setenv("PLANDILLO_POSTGRES_DSN", "")
	for _, tc := range []struct {
		name, level, format string
		want                slog.Level
		json, wantErr       bool
	}{
		{"defaults", "", "", slog.LevelInfo, false, false},
		{"debug json", "debug", "json", slog.LevelDebug, true, false},
		{"upper case", "WARN", "text", slog.LevelWarn, false, false},
		{"bad level", "loud", "", 0, false, true},
		{"bad format", "", "xml", 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PLANDILLO_LOG_LEVEL", tc.level)
			t.Setenv("PLANDILLO_LOG_FORMAT", tc.format)
			c, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && (c.LogLevel != tc.want || c.LogJSON != tc.json) {
				t.Fatalf("level %v json %v, want %v %v", c.LogLevel, c.LogJSON, tc.want, tc.json)
			}
		})
	}
}

func TestLoadTLSPair(t *testing.T) {
	t.Setenv("PLANDILLO_MODE", "local")
	t.Setenv("PLANDILLO_POSTGRES_DSN", "")
	for _, tc := range []struct {
		name, cert, key string
		wantErr         bool
	}{
		{"neither", "", "", false},
		{"both", "/tls/cert.pem", "/tls/key.pem", false},
		{"cert only", "/tls/cert.pem", "", true},
		{"key only", "", "/tls/key.pem", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PLANDILLO_TLS_CERT", tc.cert)
			t.Setenv("PLANDILLO_TLS_KEY", tc.key)
			c, err := Load()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && (c.TLSCert != tc.cert || c.TLSKey != tc.key) {
				t.Fatalf("TLS = %q / %q", c.TLSCert, c.TLSKey)
			}
		})
	}
}

func TestDefaultListenAddressIsLoopback(t *testing.T) {
	t.Setenv("PLANDILLO_MODE", "local")
	t.Setenv("PLANDILLO_POSTGRES_DSN", "")
	t.Setenv("PLANDILLO_ADDR", "")
	c, err := Load()
	if err != nil || c.Addr != "127.0.0.1:7777" {
		t.Fatalf("default address %q (%v)", c.Addr, err)
	}
	t.Setenv("PLANDILLO_ADDR", ":8888")
	c, err = Load()
	if err != nil || c.Addr != ":8888" {
		t.Fatalf("explicit address %q (%v)", c.Addr, err)
	}
}

// Docket names are no longer read; a start with them must stop instead of silently using defaults.
func TestRetiredDocketNamesStopTheStart(t *testing.T) {
	t.Setenv("PLANDILLO_DB", "")
	dir := t.TempDir()
	if err := retiredNames([]string{"PLANDILLO_ADDR=:7777", "HOME=/root", "DOCKET_SEED="}, dir); err != nil {
		t.Fatalf("current names rejected: %v", err)
	}
	if err := retiredNames([]string{"DOCKET_MCP_TOKEN=x"}, dir); err == nil || !strings.Contains(err.Error(), "PLANDILLO_MCP_TOKEN") {
		t.Fatalf("DOCKET_ variable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docket.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := retiredNames(nil, dir); err == nil || !strings.Contains(err.Error(), "PLANDILLO_DB=") {
		t.Fatalf("docket.db without PLANDILLO_DB: %v", err)
	}
	t.Setenv("PLANDILLO_DB", filepath.Join(dir, "docket.db"))
	if err := retiredNames(nil, dir); err != nil {
		t.Fatalf("docket.db chosen with PLANDILLO_DB: %v", err)
	}
}

func TestPushIsOptIn(t *testing.T) {
	if c, err := Load(); err != nil || c.Push {
		t.Fatalf("push must be off by default: %v %v", c.Push, err)
	}
	t.Setenv("PLANDILLO_PUSH", "on")
	if c, err := Load(); err != nil || !c.Push {
		t.Fatalf("PLANDILLO_PUSH=on: %v %v", c.Push, err)
	}
	t.Setenv("PLANDILLO_PUSH", "yes")
	if _, err := Load(); err == nil {
		t.Fatal("an unknown PLANDILLO_PUSH value must be refused")
	}
}
