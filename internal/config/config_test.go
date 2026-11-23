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
