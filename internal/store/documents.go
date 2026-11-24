package store

import (
	"database/sql"
	"errors"
)

type HTMLDocument struct {
	ID        int64  `json:"id"`
	TaskID    *int64 `json:"task_id,omitempty"`
	PageID    *int64 `json:"page_id,omitempty"`
	Title     string `json:"title"`
	Version   int    `json:"version"`
	Author    string `json:"author"`
	CreatedAt string `json:"created_at"`
	HTML      string `json:"html,omitempty"`
}

const documentCols = "d.id, d.task_id, d.page_id, d.title, r.version, r.author, r.created_at"
const documentFrom = " FROM html_documents d JOIN html_revisions r ON r.document_id = d.id"
const documentOwner = " LEFT JOIN tasks t ON t.id=d.task_id LEFT JOIN pages w ON w.id=d.page_id JOIN projects p ON p.id=coalesce(t.project_id,w.project_id)"

func scanDocument(row interface{ Scan(...any) error }) (d HTMLDocument, err error) {
	err = row.Scan(&d.ID, &d.TaskID, &d.PageID, &d.Title, &d.Version, &d.Author, &d.CreatedAt, &d.HTML)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

func (t *Tx) queryDocuments(query string, args ...any) ([]HTMLDocument, error) {
	rows, err := t.tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HTMLDocument{}
	for rows.Next() {
		d, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (t *Tx) Documents(taskID, pageID int64) ([]HTMLDocument, error) {
	return t.queryDocuments("SELECT "+documentCols+", ''"+documentFrom+
		" WHERE r.version=d.version AND (d.task_id=? OR d.page_id=?) ORDER BY d.id", taskID, pageID)
}

func (t *Tx) Document(id int64, version int) (HTMLDocument, error) {
	return scanDocument(t.tx.QueryRow("SELECT "+documentCols+", r.html"+documentFrom+documentOwner+
		" WHERE d.id=? AND r.version=CASE WHEN ? = 0 THEN d.version ELSE ? END AND p.deleted_at IS NULL AND t.deleted_at IS NULL", id, version, version))
}

func (t *Tx) ExportDocuments(projectID int64) ([]HTMLDocument, error) {
	query := "SELECT " + documentCols + ", r.html" + documentFrom + documentOwner
	if projectID != 0 {
		return t.queryDocuments(query+" WHERE p.id=? ORDER BY d.id,r.version", projectID)
	}
	return t.queryDocuments(query + " ORDER BY d.id,r.version")
}

func (t *Tx) SaveDocument(d *HTMLDocument, key string) error {
	if d.ID == 0 {
		if err := t.tx.QueryRow("INSERT INTO html_documents(task_id,page_id,title,title_key) VALUES (?,?,?,?) RETURNING id", d.TaskID, d.PageID, d.Title, key).Scan(&d.ID); err != nil {
			return err
		}
	}
	if _, err := t.tx.Exec("INSERT INTO html_revisions(document_id,version,author,created_at,html) VALUES (?,?,?,?,?)", d.ID, d.Version, d.Author, d.CreatedAt, d.HTML); err != nil {
		return err
	}
	_, err := t.tx.Exec("UPDATE html_documents SET version=? WHERE id=?", d.Version, d.ID)
	return err
}

func (t *Tx) DeleteDocument(id int64) error {
	_, err := t.tx.Exec("DELETE FROM html_documents WHERE id=?", id)
	return err
}
