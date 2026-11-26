package selfhost

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"plandillo/internal/auth"
	"plandillo/internal/store"
)

// backupName matches the files store.Backup writes; anything else in the directory is never listed or served.
var backupName = regexp.MustCompile(`^docket-\d{8}-\d{4}\.db$`)

type backupFile struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *handler) backupRoutes(r chi.Router) {
	r.Get("/api/admin/backups", h.admin(h.listBackups))
	r.Post("/api/admin/backups", h.admin(h.createBackup))
	r.Get("/api/admin/backups/{name}", h.admin(h.downloadBackup))
}

// listBackups answers available=false for PostgreSQL, whose backups the operator runs (pg_dump or PITR).
func (h *handler) listBackups(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	if h.Backup == nil {
		writeJSON(w, 200, map[string]any{"available": false, "backups": []backupFile{}})
		return
	}
	entries, err := os.ReadDir(h.BackupDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		internalError(w, err)
		return
	}
	list := []backupFile{}
	for _, e := range entries {
		if !e.Type().IsRegular() || !backupName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed by retention while listing
		}
		list = append(list, backupFile{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name > list[j].Name }) // timestamped names: newest first
	writeJSON(w, 200, map[string]any{"available": true, "keep": store.BackupKeep, "backups": list})
}

func (h *handler) createBackup(w http.ResponseWriter, _ *http.Request, _ auth.User) {
	if h.Backup == nil {
		writeError(w, 404, "not_found", "backups are managed outside Plandillo for PostgreSQL")
		return
	}
	path, err := h.Backup(time.Now())
	if err != nil {
		internalError(w, err)
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, 200, backupFile{Name: filepath.Base(path), Size: info.Size(), CreatedAt: info.ModTime().UTC()})
}

// downloadBackup serves one snapshot. It holds every account and workspace, so only administrators reach it.
func (h *handler) downloadBackup(w http.ResponseWriter, r *http.Request, _ auth.User) {
	name := chi.URLParam(r, "name")
	if h.Backup == nil || !backupName.MatchString(name) {
		writeError(w, 404, "not_found", "backup not found")
		return
	}
	f, err := os.Open(filepath.Join(h.BackupDir, name))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, 404, "not_found", "backup not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		writeError(w, 404, "not_found", "backup not found")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, info.ModTime(), f)
}
