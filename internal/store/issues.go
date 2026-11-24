package store

import "strings"

func (t *Tx) OpenIssueID(projectID int64, title string) (int64, error) {
	rows, err := t.tx.Query(`SELECT t.id, t.title FROM tasks t JOIN statuses s ON s.id=t.status_id
		WHERE t.project_id=? AND t.deleted_at IS NULL AND s.category <> 'done'
		AND (t.issue_kind <> '' OR t.area='issue') ORDER BY t.id`, projectID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var candidate string
		if err := rows.Scan(&id, &candidate); err != nil {
			return 0, err
		}
		// SQLite's built-in case folding is ASCII-only. Match multilingual titles identically on both stores.
		if strings.EqualFold(candidate, title) {
			return id, nil
		}
	}
	return 0, rows.Err()
}

func (t *Tx) RecordIssueReport(id int64, fingerprint string) (bool, error) {
	res, err := t.tx.Exec(`INSERT INTO issue_reports(issue_id, fingerprint) VALUES (?,?) ON CONFLICT DO NOTHING`, id, fingerprint)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (t *Tx) SetIssueAttention(id int64, kind string, after *string) error {
	_, err := t.tx.Exec(`UPDATE tasks SET issue_kind = ?, attention_after = ? WHERE id = ?`, kind, after, id)
	return err
}

// AttentionItem is also used for the sidebar count; keep bodies and history out of it.
type AttentionItem struct {
	ID             int64  `json:"id"`
	Code           string `json:"code"`
	Title          string `json:"title"`
	Project        string `json:"project"`
	StatusName     string `json:"status_name"`
	StatusCategory string `json:"status_category"`
	Priority       string `json:"priority"`
	IssueKind      string `json:"issue_kind"`
	ReportCount    int    `json:"report_count"`
}

func (t *Tx) ListAttention(today string) ([]AttentionItem, error) {
	rows, err := t.tx.Query(`SELECT t.id, p.key || '-' || printf('%02d', t.number), t.title, p.name, s.name, s.category,
    t.priority, coalesce(nullif(t.issue_kind,''),'problem'), (SELECT count(*) FROM issue_reports ir WHERE ir.issue_id=t.id)`+
		taskFrom+` WHERE p.deleted_at IS NULL AND t.deleted_at IS NULL AND s.category <> 'done'
    AND (t.issue_kind <> '' OR t.area = 'issue') AND (t.attention_after IS NULL OR t.attention_after <= ?)
    ORDER BY CASE t.issue_kind WHEN 'blocker' THEN 0 WHEN 'decision' THEN 1 ELSE 2 END, t.priority, t.created_at, t.id`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AttentionItem{}
	for rows.Next() {
		var v AttentionItem
		if err := rows.Scan(&v.ID, &v.Code, &v.Title, &v.Project, &v.StatusName, &v.StatusCategory, &v.Priority, &v.IssueKind, &v.ReportCount); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
