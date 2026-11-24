package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestPrivateSQLiteAndPublishedBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "private")
	dbPath := filepath.Join(dir, "project ?#&.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm", dir} {
		st, err := os.Stat(path)
		if err != nil || st.Mode().Perm()&0077 != 0 {
			t.Fatalf("private file %s, stat %v", filepath.Base(path), err)
		}
	}
	now := time.Now()
	backup, err := s.Backup(filepath.Join(root, "copies"), 2, now)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(backup)
	if err != nil || info.Mode().Perm() != 0600 || info.Size() == 0 {
		t.Fatal("invalid private backup", err)
	}
	restored, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	if err = restored.Close(); err != nil {
		t.Fatal(err)
	}
	if again, err := s.Backup(filepath.Dir(backup), 2, now); err != nil || again != backup {
		t.Fatal("repeated backup", err)
	}
}
