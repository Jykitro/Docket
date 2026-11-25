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
const documentFrom = " FROM html_documents d JOIN html_revisions r ON r.document_id = d.id AND d.workspace_id=:workspace"
const documentOwner = " LEFT JOIN tasks t ON t.id=d.task_id LEFT JOIN pages w ON w.id=d.page_id JOIN projects p ON p.id=coalesce(t.project_id,w.project_id)"

func scanDocument(row interface{ Scan(...any) error }) (d HTMLDocument, err error) {
	var rawBytes sql.NullInt64
	err = row.Scan(&d.ID, &d.TaskID, &d.PageID, &d.Title, &d.Version, &d.Author, &d.CreatedAt, &d.HTML, &rawBytes)
	if err == nil && rawBytes.Valid {
		d.HTML, err = decompressRevision(d.HTML, rawBytes.Int64)
	}
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

func (t *Tx) Documents(taskID, pageID int64, limit, offset int) ([]HTMLDocument, error) {
	return t.queryDocuments("SELECT "+documentCols+", '', NULL"+documentFrom+
		" WHERE r.version=d.version AND (d.task_id=? OR d.page_id=?) ORDER BY d.id LIMIT ? OFFSET ?", taskID, pageID, limit, offset)
}

// The presence of the original size identifies gzip data unambiguously.
func (t *Tx) revisionContent() string {
	if t.tx.Postgres() {
		return ", r.html, NULL"
	}
	return ", coalesce(r.html_gzip,r.html), r.html_raw_bytes"
}

func (t *Tx) Document(id int64, version int) (HTMLDocument, error) {
	return scanDocument(t.tx.QueryRow("SELECT "+documentCols+t.revisionContent()+documentFrom+documentOwner+
		" WHERE d.id=? AND r.version=CASE WHEN ? = 0 THEN d.version ELSE ? END AND p.deleted_at IS NULL AND t.deleted_at IS NULL", id, version, version))
}

// Quotas count original UTF-8 bytes, independent of the physical representation.
func (t *Tx) RevisionUsage(maxRevisions int) (used int64, revisions int, err error) {
	size := "octet_length(html)"
	if !t.tx.Postgres() {
		size = "coalesce(html_raw_bytes,octet_length(html))"
	}
	err = t.tx.QueryRow("SELECT coalesce(sum(bytes),0),count(*) FROM (SELECT "+size+" AS bytes FROM html_revisions WHERE workspace_id=:workspace LIMIT ?) r", maxRevisions).Scan(&used, &revisions)
	return
}

func (t *Tx) DocumentByTitle(taskID, pageID int64, key string) (HTMLDocument, error) {
	return scanDocument(t.tx.QueryRow("SELECT "+documentCols+", '', NULL"+documentFrom+" WHERE r.version=d.version AND (d.task_id=? OR d.page_id=?) AND d.title_key=?", taskID, pageID, key))
}

func (t *Tx) SaveDocument(d *HTMLDocument, key string) error {
	if d.ID == 0 {
		if err := t.tx.QueryRow("INSERT INTO html_documents(workspace_id, task_id,page_id,title,title_key) VALUES (:workspace, ?,?,?,?) RETURNING id", d.TaskID, d.PageID, d.Title, key).Scan(&d.ID); err != nil {
			return err
		}
	}
	if err := t.insertRevision(d); err != nil {
		return err
	}
	_, err := t.tx.Exec("UPDATE html_documents SET version=? WHERE workspace_id=:workspace AND id=?", d.Version, d.ID)
	return err
}

func (t *Tx) insertRevision(d *HTMLDocument) error {
	if !t.tx.Postgres() {
		packed, err := compressRevision(d.HTML)
		if err != nil {
			return err
		}
		if packed != nil {
			_, err = t.tx.Exec("INSERT INTO html_revisions(workspace_id,document_id,version,author,created_at,html,html_gzip,html_raw_bytes) VALUES (:workspace,?,?,?,?, '',?,?)", d.ID, d.Version, d.Author, d.CreatedAt, packed, len(d.HTML))
			return err
		}
	}
	_, err := t.tx.Exec("INSERT INTO html_revisions(workspace_id,document_id,version,author,created_at,html) VALUES (:workspace,?,?,?,?,?)", d.ID, d.Version, d.Author, d.CreatedAt, d.HTML)
	return err
}

func (t *Tx) DeleteDocument(id int64) error {
	_, err := t.tx.Exec("DELETE FROM html_documents WHERE workspace_id=:workspace AND id=?", id)
	return err
}
