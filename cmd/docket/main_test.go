package main

import (
	"context"
	"path/filepath"
	"testing"

	"docket/internal/auth"
	"docket/internal/store"
)

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
	if _, err = base.ChangeSpace(t.Context(), owner, 0, store.SpaceChange{Name: &another}, 0); err != nil {
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
