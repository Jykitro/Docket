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
	Pinned    bool    `json:"pinned"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

const pageCols = `id, project_id, parent_id, title, body_md, position, pinned, created_at, updated_at`

func scanPage(r interface{ Scan(...any) error }) (Page, error) {
	var p Page
	err := r.Scan(&p.ID, &p.ProjectID, &p.ParentID, &p.Title, &p.BodyMD, &p.Position, &p.Pinned, &p.CreatedAt, &p.UpdatedAt)
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
	err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM pages WHERE `+t.pageSiblingsFilter(), projectID, parentID).Scan(&max)
	return max + 1024, err
}

func (t *Tx) pageSiblingsFilter() string {
	if t.tx.Postgres() {
		return `project_id = ? AND parent_id IS NOT DISTINCT FROM ?::bigint`
	}
	return `project_id = ? AND parent_id IS ?`
}

func (t *Tx) PageSiblings(p Page) ([]Pos, error) {
	return t.positions(`SELECT id, position FROM pages WHERE `+t.pageSiblingsFilter()+` ORDER BY position, id`, p.ProjectID, p.ParentID)
}

func (t *Tx) SetPagePosition(id int64, pos float64) error {
	_, err := t.tx.Exec(`UPDATE pages SET position = ? WHERE id = ?`, pos, id)
	return err
}

func (t *Tx) SetPagePinned(id int64, pinned bool) error {
	_, err := t.tx.Exec(`UPDATE pages SET pinned = ? WHERE id = ?`, pinned, id)
	return err
}

func (t *Tx) InsertPage(p Page, titleKey string) (Page, error) {
	err := t.tx.QueryRow(`INSERT INTO pages(project_id, parent_id, title, title_key, body_md, position, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?) RETURNING id`,
		p.ProjectID, p.ParentID, p.Title, titleKey, p.BodyMD, p.Position, p.CreatedAt, p.UpdatedAt).Scan(&p.ID)
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
