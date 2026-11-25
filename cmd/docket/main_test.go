package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"docket/internal/auth"
	"docket/internal/store"
)

// The server must answer each protocol it offers: HTTP/1.1, h2c with prior knowledge (from a proxy) and HTTP/2 over TLS.
func TestServerProtocols(t *testing.T) {
	proto := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.Proto) })
	var h2c http.Protocols
	h2c.SetUnencryptedHTTP2(true)
	for _, tc := range []struct {
		name   string
		tls    bool
		client func(*httptest.Server) *http.Client
		want   string
	}{
		{"http/1.1", false, func(*httptest.Server) *http.Client { return &http.Client{} }, "HTTP/1.1"},
		{"h2c", false, func(*httptest.Server) *http.Client { return &http.Client{Transport: &http.Transport{Protocols: &h2c}} }, "HTTP/2.0"},
		{"h2 over tls", true, func(s *httptest.Server) *http.Client { return s.Client() }, "HTTP/2.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewUnstartedServer(nil)
			srv.Config = newServer(context.Background(), "", proto)
			if tc.tls {
				srv.EnableHTTP2 = true
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			res, err := tc.client(srv).Get(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			body, err := io.ReadAll(res.Body)
			if err != nil || string(body) != tc.want {
				t.Fatalf("served %q (%v), want %s", body, err, tc.want)
			}
		})
	}
}

func TestSharedMaintenanceWritesOneInstallationBackup(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "planner.db")
	db, err := auth.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	base, err := store.Shared(db.Connection(), file)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.EnsureLocalUser(db.Connection())
	if err != nil {
		t.Fatal(err)
	}
	another := "Other"
	if _, err = base.ChangeSpace(t.Context(), owner, 0, store.SpaceChange{Name: &another}); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "snapshots")
	// A canceled loop still performs the startup backup before checking its timer.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	maintenanceShared(ctx, base, backups)
	files, err := filepath.Glob(filepath.Join(backups, "*.db"))
	if err != nil || len(files) != 1 {
		t.Fatal("expected one database snapshot", files, err)
	}
	err = base.ForOwner(1, owner).WithTx(t.Context(), func(tx *store.Tx) error {
		stamp, found, err := tx.Setting("last_backup_at")
		if err == nil && (!found || stamp == "") {
			t.Fatal("snapshot time missing")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
