package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Page is one wiki page of a project.
type Page struct {
	ParentTitle string  `json:"-"`
	ID          int64   `json:"id"`
	ProjectID   int64   `json:"project_id"`
	ParentID    *int64  `json:"parent_id"`
	Title       string  `json:"title"`
	BodyMD      string  `json:"body_md"`
	BodyHTML    *string `json:"body_html,omitempty"`
	Position    float64 `json:"position"`
	Pinned      bool    `json:"pinned"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

const pageHeader = `id, project_id, parent_id, title, `
const pageLayout = `, position, pinned, created_at, updated_at`
const pageCols = pageHeader + `body_md, body_html` + pageLayout

func scanPage(r interface{ Scan(...any) error }) (Page, error) {
	var p Page
	err := r.Scan(&p.ID, &p.ProjectID, &p.ParentID, &p.Title, &p.BodyMD, &p.BodyHTML, &p.Position, &p.Pinned, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (t *Tx) PageHeaders(projectID int64, limit, offset int) ([]Page, error) {
	rows, err := t.tx.Query(`SELECT p.id,p.project_id,p.parent_id,p.title,CASE WHEN p.body_html IS NOT NULL THEN '' END,p.position,p.pinned,p.created_at,p.updated_at,coalesce(parent.title,'') FROM pages p LEFT JOIN pages parent ON parent.id=p.parent_id WHERE p.workspace_id=:workspace AND p.project_id=? ORDER BY p.position,p.id LIMIT ? OFFSET ?`, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.ParentID, &p.Title, &p.BodyHTML, &p.Position, &p.Pinned, &p.CreatedAt, &p.UpdatedAt, &p.ParentTitle); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *Tx) PageTitle(id int64) (title string, err error) {
	err = t.tx.QueryRow("SELECT title FROM pages WHERE workspace_id=:workspace AND id=?", id).Scan(&title)
	return title, notFoundIf(err)
}

// Markdown batches leave the connection available for in-transaction link rewrites.
func (t *Tx) MarkdownBatch(projectID, lastID int64) ([]Page, error) {
	return t.queryPages("SELECT "+pageHeader+"body_md,NULL"+pageLayout+" FROM pages WHERE workspace_id=:workspace AND project_id=? AND id>? AND body_html IS NULL ORDER BY id LIMIT 8", projectID, lastID)
}

// The predicate must match the partial index on both dialects.
func wikiMarker(postgres bool) string {
	if postgres {
		return "strpos(body_md,'[[') > 0"
	}
	return "instr(body_md,'[[') > 0"
}

// WalkBacklinkCandidates streams only pages that could contain a wiki link.
// The caller applies the Markdown parser and stops after filling its page.
func (t *Tx) WalkBacklinkCandidates(projectID, excludeID int64, visit func(id int64, title, body string) bool) error {
	rows, err := t.tx.Query("SELECT id,title,body_md FROM pages WHERE workspace_id=:workspace AND project_id=? AND id<>? AND "+wikiMarker(t.tx.Postgres())+" ORDER BY position,id", projectID, excludeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title, body string
		if err := rows.Scan(&id, &title, &body); err != nil {
			return err
		}
		if !visit(id, title, body) {
			break
		}
	}
	return rows.Err()
}

type PageRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// LinkedPageTitles uses the project's unique title index and leaves document bodies in storage.
func (t *Tx) LinkedPageTitles(projectID int64, keys []string) (map[string]PageRef, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	list, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	values := "SELECT value FROM json_each(?)"
	if t.tx.Postgres() {
		values = "SELECT jsonb_array_elements_text(?::jsonb)"
	}
	rows, err := t.tx.Query("SELECT title_key,id,title FROM pages WHERE workspace_id=:workspace AND project_id=? AND title_key IN ("+values+")", projectID, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := make(map[string]PageRef, len(keys))
	for rows.Next() {
		var key string
		var ref PageRef
		if err := rows.Scan(&key, &ref.ID, &ref.Title); err != nil {
			return nil, err
		}
		found[key] = ref
	}
	return found, rows.Err()
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
	return scanPage(t.tx.QueryRow(`SELECT `+pageCols+` FROM pages WHERE workspace_id=:workspace AND id = ?`, id))
}

func (t *Tx) PageByTitleKey(projectID int64, key string) (Page, error) {
	return scanPage(t.tx.QueryRow(`SELECT `+pageCols+` FROM pages WHERE workspace_id=:workspace AND project_id = ? AND title_key = ?`, projectID, key))
}

// NextPagePosition is max+1024 among the siblings, 1024 for the first.
func (t *Tx) NextPagePosition(projectID int64, parentID *int64) (float64, error) {
	var max float64
	err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM pages WHERE workspace_id=:workspace AND `+t.pageSiblingsFilter(), projectID, parentID).Scan(&max)
	return max + 1024, err
}

func (t *Tx) pageSiblingsFilter() string {
	if t.tx.Postgres() {
		return `project_id = ? AND parent_id IS NOT DISTINCT FROM ?::bigint`
	}
	return `project_id = ? AND parent_id IS ?`
}

func (t *Tx) PageSiblings(p Page) ([]Pos, error) {
	return t.positions(`SELECT id, position FROM pages WHERE workspace_id=:workspace AND `+t.pageSiblingsFilter()+` ORDER BY position, id`, p.ProjectID, p.ParentID)
}

func (t *Tx) SetPagePosition(id int64, pos float64) error {
	_, err := t.tx.Exec(`UPDATE pages SET position = ? WHERE workspace_id=:workspace AND id = ?`, pos, id)
	return err
}

func (t *Tx) SetPagePinned(id int64, pinned bool) error {
	_, err := t.tx.Exec(`UPDATE pages SET pinned = ? WHERE workspace_id=:workspace AND id = ?`, pinned, id)
	return err
}

func (t *Tx) InsertPage(p Page, titleKey string) (Page, error) {
	err := t.tx.QueryRow(`INSERT INTO pages(workspace_id, project_id, parent_id, title, title_key, body_md, body_html, position, created_at, updated_at) VALUES (:workspace, ?,?,?,?,?,?,?,?,?) RETURNING id`,
		p.ProjectID, p.ParentID, p.Title, titleKey, p.BodyMD, p.BodyHTML, p.Position, p.CreatedAt, p.UpdatedAt).Scan(&p.ID)
	return p, err
}

func (t *Tx) UpdatePage(p Page, titleKey string) error {
	_, err := t.tx.Exec(`UPDATE pages SET parent_id = ?, title = ?, title_key = ?, body_md = ?, body_html = ?, position = ?, updated_at = ? WHERE workspace_id=:workspace AND id = ?`,
		p.ParentID, p.Title, titleKey, p.BodyMD, p.BodyHTML, p.Position, p.UpdatedAt, p.ID)
	return err
}

func (t *Tx) DeletePage(id int64) error {
	_, err := t.tx.Exec(`DELETE FROM pages WHERE workspace_id=:workspace AND id = ?`, id)
	return err
}

// ReparentPages moves the children of page from under page to (nil = top level).
func (t *Tx) ReparentPages(from int64, to *int64) error {
	_, err := t.tx.Exec(`UPDATE pages SET parent_id = ? WHERE workspace_id=:workspace AND parent_id = ?`, to, from)
	return err
}

// WalkTaskMentions looks up live project metadata once and streams candidate Markdown.
func (t *Tx) WalkTaskMentions(code string, visit func(id int64, title, slug, body string)) error {
	containsCode := "instr(pg.body_md,?)>0"
	if t.tx.Postgres() {
		containsCode = "strpos(pg.body_md,?)>0"
	}
	rows, err := t.tx.Query("SELECT pg.id,pg.title,p.slug,pg.body_md FROM pages pg JOIN projects p ON p.id=pg.project_id WHERE pg.workspace_id=:workspace AND p.deleted_at IS NULL AND "+containsCode+" ORDER BY pg.id", code)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title, slug, body string
		if err := rows.Scan(&id, &title, &slug, &body); err != nil {
			return err
		}
		visit(id, title, slug, body)
	}
	return rows.Err()
}
