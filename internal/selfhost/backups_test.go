package selfhost

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupsAdminOnlyListCreateDownload(t *testing.T) {
	s := newServer(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.db"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot := func(now time.Time) (string, error) {
		path := filepath.Join(dir, "plandillo-"+now.Format("20060102-1504")+".db")
		return path, os.WriteFile(path, []byte("sqlite"), 0o600)
	}
	s.h = Handler(Options{Auth: s.db, WS: s.ws, Web: http.NotFoundHandler(), AllowedHosts: hosts, BackupDir: dir, Backup: snapshot})
	admin, _ := s.addUser("alice")
	member, _ := s.addUser("bob")

	if rec := s.do("GET", "/api/admin/backups", "", nil, nil); rec.Code != 401 {
		t.Fatalf("anonymous list: %d, want 401", rec.Code)
	}
	if rec := s.do("POST", "/api/admin/backups", "", member, nil); rec.Code != 403 {
		t.Fatalf("member create: %d, want 403", rec.Code)
	}

	rec := s.do("POST", "/api/admin/backups", "", admin, nil)
	var made backupFile
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &made) != nil || !backupName.MatchString(made.Name) || made.Size != 6 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}

	rec = s.do("GET", "/api/admin/backups", "", admin, nil)
	var list struct {
		Available bool         `json:"available"`
		Keep      int          `json:"keep"`
		Backups   []backupFile `json:"backups"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &list) != nil || !list.Available || list.Keep != 14 || len(list.Backups) != 1 || list.Backups[0].Name != made.Name {
		t.Fatalf("list (notes.txt must be hidden): %d %s", rec.Code, rec.Body)
	}

	rec = s.do("GET", "/api/admin/backups/"+made.Name, "", admin, nil)
	if rec.Code != 200 || rec.Body.String() != "sqlite" || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("download: %d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if rec := s.do("GET", "/api/admin/backups/"+made.Name, "", member, nil); rec.Code != 403 {
		t.Fatalf("member download: %d, want 403", rec.Code)
	}
	for _, name := range []string{"notes.txt", "..%2Fsecret.db", "plandillo-20990101-0000.db"} {
		if rec := s.do("GET", "/api/admin/backups/"+name, "", admin, nil); rec.Code != 404 {
			t.Fatalf("download %q: %d %s, want 404", name, rec.Code, rec.Body)
		}
	}
}

func TestBackupsUnavailableWithoutSnapshots(t *testing.T) {
	s := newServer(t)
	s.h = Handler(Options{Auth: s.db, WS: s.ws, Web: http.NotFoundHandler(), AllowedHosts: hosts})
	admin, _ := s.addUser("alice")
	if rec := s.do("GET", "/api/admin/backups", "", admin, nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("POST", "/api/admin/backups", "", admin, nil); rec.Code != 404 {
		t.Fatalf("create: %d %s, want 404", rec.Code, rec.Body)
	}
}
