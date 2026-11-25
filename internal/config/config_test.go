package config

import (
	"path/filepath"
	"testing"
)

func TestLoadMode(t *testing.T) {
	t.Setenv("DOCKET_POSTGRES_DSN", "")
	db := filepath.Join(t.TempDir(), "sub", "docket.db")
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
			t.Setenv("DOCKET_MODE", tc.mode)
			t.Setenv("DOCKET_DB", db)
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
	t.Setenv("DOCKET_POSTGRES_DSN", "postgres://user:secret@db/docket")
	t.Setenv("DOCKET_MODE", "local")
	if _, err := Load(); err == nil {
		t.Fatal("PostgreSQL in local mode must fail")
	}
	t.Setenv("DOCKET_MODE", "selfhost")
	c, err := Load()
	if err != nil || c.PostgresDSN != "postgres://user:secret@db/docket" {
		t.Fatalf("Postgres configuration: %v", err)
	}
}

func TestDefaultListenAddressIsLoopback(t *testing.T) {
	t.Setenv("DOCKET_MODE", "local")
	t.Setenv("DOCKET_POSTGRES_DSN", "")
	t.Setenv("DOCKET_ADDR", "")
	c, err := Load()
	if err != nil || c.Addr != "127.0.0.1:7777" {
		t.Fatalf("default address %q (%v)", c.Addr, err)
	}
	t.Setenv("DOCKET_ADDR", ":8888")
	c, err = Load()
	if err != nil || c.Addr != ":8888" {
		t.Fatalf("explicit address %q (%v)", c.Addr, err)
	}
}

func TestSelfhostDemoStorageLimit(t *testing.T) {
	t.Setenv("DOCKET_MODE", "selfhost")
	for _, tc := range []struct {
		value   string
		bytes   int64
		wantErr bool
	}{{"", 0, false}, {"5", 5_000_000, false}, {"0", 0, true}, {"invalid", 0, true}} {
		t.Setenv("PLANDILLO_DEMO_STORAGE_MB", tc.value)
		cfg, err := Load()
		if (err != nil) != tc.wantErr || cfg.DemoStorageBytes != tc.bytes {
			t.Errorf("quota %q got %d bytes, error %v", tc.value, cfg.DemoStorageBytes, err)
		}
	}
}
