package store

import (
	"errors"
)

type scanner = interface{ Scan(...any) error }

func scanNote(r scanner) (Note, error) {
	var n Note
	err := r.Scan(&n.ID, &n.TaskID, &n.Author, &n.BodyMD, &n.CreatedAt)
	return n, err
}

func scanActivity(r scanner) (Activity, error) {
	var a Activity
	var diff *string
	err := r.Scan(&a.ID, &a.TaskID, &a.ProjectID, &a.Actor, &a.Action, &diff, &a.At, &a.TaskCode, &a.TaskTitle)
	if diff == nil {
		a.DiffJSON = "{}"
		a.DiffOmitted = true
	} else {
		a.DiffJSON = *diff
	}
	return a, err
}

const activitySelect = `SELECT a.id, a.task_id, a.project_id, a.actor, a.action, a.diff_json, a.at,
    coalesce(p.key || '-' || printf('%02d', t.number), ''), coalesce(t.title, '')
    FROM activity a LEFT JOIN tasks t ON t.id=a.task_id LEFT JOIN projects p ON p.id=t.project_id`

func streamRows[T any](t *Tx, query string, args []any, scan func(scanner) (T, error), emit func(any) error) error {
	rows, err := t.tx.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return err
		}
		if err := emit(item); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (t *Tx) exportProjects(id int64, emit func(any) error) error {
	// Keyset iteration leaves the transaction connection available for the icon and status counts.
	var lastID int64
	for {
		p, err := t.scanProject(t.tx.QueryRow("SELECT "+projectCols+" FROM projects WHERE workspace_id=:workspace AND deleted_at IS NULL AND id>? AND (CAST(? AS bigint)=0 OR id=?) ORDER BY id LIMIT 1", lastID, id, id))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if p.IconURL != "" {
			if p.IconPNG, err = t.ProjectIcon(p.ID); err != nil {
				return err
			}
		}
		rows, err := t.tx.Query(`SELECT s.key,s.category,count(*) FROM tasks t JOIN statuses s ON s.id=t.status_id AND t.workspace_id=:workspace WHERE t.project_id=? AND t.deleted_at IS NULL GROUP BY s.key,s.category`, p.ID)
		if err != nil {
			return err
		}
		p.Counts = map[string]int{}
		for rows.Next() {
			var key, category string
			var n int
			if err := rows.Scan(&key, &category, &n); err != nil {
				rows.Close()
				return err
			}
			p.Counts[key] = n
			if category != "done" {
				p.Open += n
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if err := emit(p); err != nil {
			return err
		}
		lastID = p.ID
	}
}

// ExportRows writes one record at a time. Scope filters apply in SQL before bodies or history leave the database.
func (t *Tx) ExportRows(section string, id int64, emit func(any) error) error {
	scope := func(col string) (string, []any) {
		if id == 0 {
			return "1=1", nil
		}
		return col + "=?", []any{id}
	}
	live, args := scope("id")
	liveProjects := " IN (SELECT id FROM projects WHERE workspace_id=:workspace AND deleted_at IS NULL AND " + live + ")"
	switch section {
	case "projects":
		return t.exportProjects(id, emit)
	case "statuses":
		return streamRows(t, "SELECT "+statusCols+" FROM statuses s WHERE s.workspace_id=:workspace AND s.project_id"+liveProjects+" ORDER BY s.project_id,s.position,s.id", args, scanStatus, emit)
	case "labels":
		return streamRows(t, "SELECT "+labelCols+" FROM labels l WHERE l.workspace_id=:workspace AND l.project_id"+liveProjects+" ORDER BY l.project_id,l.id", args, scanLabel, emit)
	case "fields":
		return streamRows(t, "SELECT "+fieldCols+" FROM custom_fields WHERE workspace_id=:workspace AND project_id"+liveProjects+" ORDER BY project_id,position,id", args, scanField, emit)
	case "pages":
		return streamRows(t, "SELECT "+pageCols+" FROM pages WHERE workspace_id=:workspace AND project_id"+liveProjects+" ORDER BY project_id,position,id", args, scanPage, emit)
	case "views":
		where, args := scope("project_id")
		return streamRows(t, "SELECT "+viewCols+" FROM saved_views WHERE workspace_id=:workspace AND (project_id IS NULL OR "+where+") ORDER BY position,id", args, scanView, emit)
	case "tasks":
		where, args := scope("t.project_id")
		return streamRows(t, "SELECT "+taskCols+taskFrom+" WHERE "+where+" ORDER BY t.id", args, scanTask, emit)
	case "notes":
		where, args := scope("t.project_id")
		return streamRows(t, "SELECT n.id,n.task_id,n.author,n.body_md,n.created_at FROM notes n JOIN tasks t ON t.id=n.task_id WHERE n.workspace_id=:workspace AND "+where+" ORDER BY n.id", args, scanNote, emit)
	case "activity":
		where, args := scope("a.project_id")
		if id != 0 {
			where += " AND (a.task_id IS NULL OR t.project_id=a.project_id)"
			// Transfer diffs can include the previous project's field values. They belong to the full export.
			if t.tx.Postgres() {
				where += " AND NOT jsonb_exists(a.diff_json::jsonb,'project_id')"
			} else {
				where += " AND json_type(a.diff_json,'$.project_id') IS NULL"
			}
		}
		return streamRows(t, activitySelect+" WHERE a.workspace_id=:workspace AND "+where+" ORDER BY a.id DESC", args, scanActivity, emit)
	case "html_documents":
		where, args := scope("p.id")
		return streamRows(t, "SELECT "+documentCols+t.revisionContent()+documentFrom+documentOwner+" WHERE "+where+" ORDER BY d.id,r.version", args, scanDocument, emit)
	}
	return errors.New("unknown export section")
}
