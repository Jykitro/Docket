package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
	t.Setenv("PLANDILLO_POSTGRES_DSN", "postgres://user:secret@db/docket")
	t.Setenv("PLANDILLO_MODE", "local")
	if _, err := Load(); err == nil {
		t.Fatal("PostgreSQL in local mode must fail")
	}
	t.Setenv("PLANDILLO_MODE", "selfhost")
	c, err := Load()
	if err != nil || c.PostgresDSN != "postgres://user:secret@db/docket" {
		t.Fatalf("Postgres configuration: %v", err)
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

func TestOlderDocketNamesStillApply(t *testing.T) {
	t.Setenv("PLANDILLO_MODE", "")
	t.Setenv("PLANDILLO_POSTGRES_DSN", "")
	t.Setenv("PLANDILLO_DB", "")
	t.Setenv("DOCKET_MODE", "selfhost")
	t.Setenv("DOCKET_DB", "/srv/old/docket.db")
	c, err := Load()
	if err != nil || !c.SelfHost || c.DB != "/srv/old/docket.db" {
		t.Fatalf("an existing .env with DOCKET_* names must keep its mode and database: %+v (%v)", c, err)
	}
	t.Setenv("PLANDILLO_DB", "/srv/new/plandillo.db")
	if c, _ := Load(); c.DB != "/srv/new/plandillo.db" {
		t.Fatalf("PLANDILLO_DB must win over DOCKET_DB, got %q", c.DB)
	}
}

func TestDefaultDatabaseKeepsAnOldDocketFile(t *testing.T) {
	dir := t.TempDir()
	if got := defaultDB(dir); got != filepath.Join(dir, "plandillo.db") {
		t.Fatalf("fresh install uses %q, want plandillo.db", got)
	}
	old := filepath.Join(dir, "docket.db")
	if err := os.WriteFile(old, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDB(dir); got != old {
		t.Fatalf("an existing docket.db must stay in use, got %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "plandillo.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDB(dir); got != filepath.Join(dir, "plandillo.db") {
		t.Fatalf("plandillo.db must win once it exists, got %q", got)
	}
}
