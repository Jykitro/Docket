package store

import (
	"database/sql"
	"errors"
	"strings"
)

// Page is one wiki page of a project.
type Page struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"project_id"`
	ParentID  *int64  `json:"parent_id"`
	Title     string  `json:"title"`
	BodyMD    string  `json:"body_md"`
	Position  float64 `json:"position"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

const pageCols = `id, project_id, parent_id, title, body_md, position, created_at, updated_at`

func scanPage(r interface{ Scan(...any) error }) (Page, error) {
	var p Page
	err := r.Scan(&p.ID, &p.ProjectID, &p.ParentID, &p.Title, &p.BodyMD, &p.Position, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (t *Tx) ListPages(projectID int64) ([]Page, error) {
	return t.queryPages(`SELECT `+pageCols+` FROM pages WHERE project_id = ? ORDER BY position, id`, projectID)
}

func (t *Tx) queryPages(q string, args ...any) ([]Page, error) {
	rows, err := t.tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *Tx) GetPage(id int64) (Page, error) {
	return scanPage(t.tx.QueryRow(`SELECT `+pageCols+` FROM pages WHERE id = ?`, id))
}

func (t *Tx) PageByTitleKey(projectID int64, key string) (Page, error) {
	return scanPage(t.tx.QueryRow(`SELECT `+pageCols+` FROM pages WHERE project_id = ? AND title_key = ?`, projectID, key))
}

// NextPagePosition is max+1024 among the siblings, 1024 for the first.
func (t *Tx) NextPagePosition(projectID int64, parentID *int64) (float64, error) {
	var max float64
	err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM pages WHERE project_id = ? AND parent_id IS ?`, projectID, parentID).Scan(&max)
	return max + 1024, err
}

func (t *Tx) InsertPage(p Page, titleKey string) (Page, error) {
	res, err := t.tx.Exec(`INSERT INTO pages(project_id, parent_id, title, title_key, body_md, position, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)`,
		p.ProjectID, p.ParentID, p.Title, titleKey, p.BodyMD, p.Position, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.ID, err = res.LastInsertId()
	return p, err
}

func (t *Tx) UpdatePage(p Page, titleKey string) error {
	_, err := t.tx.Exec(`UPDATE pages SET parent_id = ?, title = ?, title_key = ?, body_md = ?, position = ?, updated_at = ? WHERE id = ?`,
		p.ParentID, p.Title, titleKey, p.BodyMD, p.Position, p.UpdatedAt, p.ID)
	return err
}

func (t *Tx) DeletePage(id int64) error {
	_, err := t.tx.Exec(`DELETE FROM pages WHERE id = ?`, id)
	return err
}

// ReparentPages moves the children of page from under page to (nil = top level).
func (t *Tx) ReparentPages(from int64, to *int64) error {
	_, err := t.tx.Exec(`UPDATE pages SET parent_id = ? WHERE parent_id = ?`, to, from)
	return err
}

// PagesContaining returns pages (any project) whose body contains sub as a plain substring.
func (t *Tx) PagesContaining(sub string) ([]Page, error) {
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(sub)
	return t.queryPages(`SELECT `+pageCols+` FROM pages WHERE body_md LIKE '%'||?||'%' ESCAPE '\' ORDER BY id`, esc)
}
