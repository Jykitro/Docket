package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
)

var ErrNotFound = errors.New("not found")

type Project struct {
	ID          int64          `json:"id"`
	Slug        string         `json:"slug"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Icon        string         `json:"icon"`
	Color       string         `json:"color"`
	Description string         `json:"description"`
	Position    float64        `json:"position"`
	ArchivedAt  *string        `json:"archived_at"`
	DeletedAt   *string        `json:"deleted_at"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
	Counts      map[string]int `json:"counts,omitempty"` // by status key
	Open        int            `json:"open"`             // tasks outside a done-category status
}

type Task struct {
	ID             int64          `json:"id"`
	ProjectID      int64          `json:"project_id"`
	Number         int            `json:"number"`
	Code           string         `json:"code"` // TRK-07
	ParentID       *int64         `json:"parent_id"`
	ParentCode     string         `json:"parent_code"`
	Title          string         `json:"title"`
	DescriptionMD  string         `json:"description_md"`
	StatusID       int64          `json:"status_id"`
	Status         string         `json:"status"` // status key
	StatusName     string         `json:"status_name"`
	StatusCategory string         `json:"status_category"`
	Priority       string         `json:"priority"`
	Area           string         `json:"area"`
	PlannedFor     *string        `json:"planned_for"`
	Position       float64        `json:"position"`
	Custom         map[string]any `json:"custom"` // field id -> value
	ExternalID     *string        `json:"external_id"`
	CreatedBy      string         `json:"created_by"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	DoneAt         *string        `json:"done_at"`
	DeletedAt      *string        `json:"deleted_at"`
	SubtaskCount   int            `json:"subtask_count"`
	SubtaskDone    int            `json:"subtask_done"`
}

type Note struct {
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	Author    string `json:"author"`
	BodyMD    string `json:"body_md"`
	CreatedAt string `json:"created_at"`
}

type Activity struct {
	ID        int64  `json:"id"`
	TaskID    *int64 `json:"task_id"`
	ProjectID *int64 `json:"project_id"`
	Actor     string `json:"actor"`
	Action    string `json:"action"`
	DiffJSON  string `json:"diff_json"`
	At        string `json:"at"`
	TaskCode  string `json:"task_code"`
	TaskTitle string `json:"task_title"`
}

type SearchHit struct {
	ID          int64  `json:"id"`
	ProjectID   int64  `json:"project_id"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	StatusName  string `json:"status_name"`
	StatusColor string `json:"status_color"`
	Snippet     string `json:"snippet,omitempty"`
}

// ---- projects ----

const projectCols = `id, slug, key, name, icon, color, description, position, archived_at, deleted_at, created_at, updated_at`

func scanProject(r interface{ Scan(...any) error }) (Project, error) {
	var p Project
	err := r.Scan(&p.ID, &p.Slug, &p.Key, &p.Name, &p.Icon, &p.Color, &p.Description, &p.Position, &p.ArchivedAt, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

func (t *Tx) ListProjects(includeArchived bool) ([]Project, error) {
	q := `SELECT ` + projectCols + ` FROM projects WHERE deleted_at IS NULL`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	rows, err := t.tx.Query(q + ` ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		p.Counts = map[string]int{}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	crows, err := t.tx.Query(`SELECT t.project_id, s.key, s.category, count(*) FROM tasks t JOIN statuses s ON s.id = t.status_id
		WHERE t.deleted_at IS NULL GROUP BY 1, 2, 3`)
	if err != nil {
		return nil, err
	}
	defer crows.Close()
	for crows.Next() {
		var pid int64
		var key, cat string
		var n int
		if err := crows.Scan(&pid, &key, &cat, &n); err != nil {
			return nil, err
		}
		for i := range out {
			if out[i].ID == pid {
				out[i].Counts[key] = n
				if cat != "done" {
					out[i].Open += n
				}
			}
		}
	}
	return out, crows.Err()
}

// ListDeletedProjects returns the projects in the trash, newest first.
func (t *Tx) ListDeletedProjects() ([]Project, error) {
	rows, err := t.tx.Query(`SELECT ` + projectCols + ` FROM projects WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PurgeProject physically removes a project with its tasks, notes, history and configuration.
func (t *Tx) PurgeProject(id int64) (int64, error) {
	for _, q := range []string{
		`UPDATE tasks SET parent_id = NULL WHERE project_id = ?`,
		`DELETE FROM notes WHERE task_id IN (SELECT id FROM tasks WHERE project_id = ?)`,
		`DELETE FROM activity WHERE project_id = ?`,
	} {
		if _, err := t.tx.Exec(q, id); err != nil {
			return 0, err
		}
	}
	res, err := t.tx.Exec(`DELETE FROM tasks WHERE project_id = ?`, id)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	for _, q := range []string{
		`DELETE FROM statuses WHERE project_id = ?`, `DELETE FROM labels WHERE project_id = ?`,
		`DELETE FROM custom_fields WHERE project_id = ?`, `DELETE FROM saved_views WHERE project_id = ?`,
		`DELETE FROM projects WHERE id = ?`,
	} {
		if _, err := t.tx.Exec(q, id); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (t *Tx) GetProject(id int64) (Project, error) {
	return scanProject(t.tx.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

func (t *Tx) GetProjectBySlug(slug string) (Project, error) {
	return scanProject(t.tx.QueryRow(`SELECT `+projectCols+` FROM projects WHERE slug = ?`, slug))
}

func (t *Tx) GetProjectByKey(key string) (Project, error) {
	return scanProject(t.tx.QueryRow(`SELECT `+projectCols+` FROM projects WHERE key = ?`, key))
}

func (t *Tx) InsertProject(p Project) (Project, error) {
	var max float64
	if err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM projects`).Scan(&max); err != nil {
		return p, err
	}
	res, err := t.tx.Exec(`INSERT INTO projects(slug, key, name, icon, color, description, position) VALUES (?,?,?,?,?,?,?)`,
		p.Slug, p.Key, p.Name, p.Icon, p.Color, p.Description, max+1024)
	if err != nil {
		return p, err
	}
	id, _ := res.LastInsertId()
	return t.GetProject(id)
}

func (t *Tx) UpdateProject(p Project) error {
	_, err := t.tx.Exec(`UPDATE projects SET key = ?, name = ?, icon = ?, color = ?, description = ?, position = ?, archived_at = ?, deleted_at = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		p.Key, p.Name, p.Icon, p.Color, p.Description, p.Position, p.ArchivedAt, p.DeletedAt, p.ID)
	return err
}

// NextNumber hands out the next task number of a project; numbers are never reused.
func (t *Tx) NextNumber(projectID int64) (int, error) {
	var n int
	err := t.tx.QueryRow(`UPDATE projects SET task_seq = task_seq + 1 WHERE id = ? RETURNING task_seq`, projectID).Scan(&n)
	return n, err
}

// ---- tasks ----

const taskFrom = ` FROM tasks t JOIN statuses s ON s.id = t.status_id JOIN projects p ON p.id = t.project_id`

const taskCols = `t.id, t.project_id, t.number, p.key || '-' || printf('%02d', t.number), t.parent_id, t.title, t.description_md,
	t.status_id, s.key, s.name, s.category, t.priority, t.area, t.planned_for, t.position, t.custom, t.external_id, t.created_by,
	t.created_at, t.updated_at, t.done_at, t.deleted_at,
	(SELECT count(*) FROM tasks c WHERE c.parent_id = t.id AND c.deleted_at IS NULL),
	(SELECT count(*) FROM tasks c JOIN statuses cs ON cs.id = c.status_id WHERE c.parent_id = t.id AND c.deleted_at IS NULL AND cs.category = 'done'),
	coalesce((SELECT p.key || '-' || printf('%02d', pt.number) FROM tasks pt WHERE pt.id = t.parent_id), '')`

func scanTask(r interface{ Scan(...any) error }) (Task, error) {
	var x Task
	var custom string
	err := r.Scan(&x.ID, &x.ProjectID, &x.Number, &x.Code, &x.ParentID, &x.Title, &x.DescriptionMD,
		&x.StatusID, &x.Status, &x.StatusName, &x.StatusCategory, &x.Priority, &x.Area, &x.PlannedFor, &x.Position, &custom, &x.ExternalID, &x.CreatedBy,
		&x.CreatedAt, &x.UpdatedAt, &x.DoneAt, &x.DeletedAt, &x.SubtaskCount, &x.SubtaskDone, &x.ParentCode)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal([]byte(custom), &x.Custom)
	}
	if x.Custom == nil {
		x.Custom = map[string]any{}
	}
	return x, err
}

func customJSON(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// GetTask returns a task even if it is in the trash.
func (t *Tx) GetTask(id int64) (Task, error) {
	return scanTask(t.tx.QueryRow(`SELECT `+taskCols+taskFrom+` WHERE t.id = ?`, id))
}

func (t *Tx) GetTaskByNumber(projectID int64, number int) (Task, error) {
	return scanTask(t.tx.QueryRow(`SELECT `+taskCols+taskFrom+` WHERE t.project_id = ? AND t.number = ?`, projectID, number))
}

func (t *Tx) GetTaskByExternalID(ext string) (Task, error) {
	return scanTask(t.tx.QueryRow(`SELECT `+taskCols+taskFrom+` WHERE t.external_id = ?`, ext))
}

type TaskFilter struct {
	ProjectID     int64
	Status        string // status key
	PlannedFor    string
	PlannedBefore string // planned_for < value
	Area          string
	ParentID      int64
	CreatedBy     string
	Q             string
	IncludeDone   bool // ignored when Status is set
	Deleted       bool // true: only trash; false: only live tasks
}

func (t *Tx) ListTasks(f TaskFilter) ([]Task, error) {
	where := []string{"t.deleted_at IS NULL", "p.deleted_at IS NULL"}
	order := "t.project_id, s.position, t.position"
	if f.Deleted {
		where[0] = "t.deleted_at IS NOT NULL"
		order = "t.deleted_at DESC"
	}
	var args []any
	add := func(cond string, v any) { where = append(where, cond); args = append(args, v) }
	if f.ProjectID != 0 {
		add("t.project_id = ?", f.ProjectID)
	}
	if f.Status != "" {
		add("s.key = ?", f.Status)
	} else if !f.IncludeDone {
		where = append(where, "s.category != 'done'")
	}
	if f.PlannedFor != "" {
		add("t.planned_for = ?", f.PlannedFor)
	}
	if f.PlannedBefore != "" {
		add("t.planned_for < ?", f.PlannedBefore)
		order = "t.planned_for, t.project_id, t.position"
	}
	if f.Area != "" {
		add("t.area = ?", f.Area)
	}
	if f.ParentID != 0 {
		add("t.parent_id = ?", f.ParentID)
	}
	if f.CreatedBy != "" {
		add("t.created_by = ?", f.CreatedBy)
	}
	if q := ftsQuery(f.Q); q != "" {
		add("t.id IN (SELECT rowid FROM tasks_fts WHERE tasks_fts MATCH ?)", q)
	}
	rows, err := t.tx.Query(`SELECT `+taskCols+taskFrom+` WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		x, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (t *Tx) InsertTask(x Task) (Task, error) {
	res, err := t.tx.Exec(`INSERT INTO tasks(project_id, number, parent_id, title, description_md, status_id, priority, area,
		planned_for, position, custom, external_id, created_by, done_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ProjectID, x.Number, x.ParentID, x.Title, x.DescriptionMD, x.StatusID, x.Priority, x.Area,
		x.PlannedFor, x.Position, customJSON(x.Custom), x.ExternalID, x.CreatedBy, x.DoneAt)
	if err != nil {
		return x, err
	}
	id, _ := res.LastInsertId()
	return t.GetTask(id)
}

// UpdateTask writes every mutable column of x.
func (t *Tx) UpdateTask(x Task) error {
	_, err := t.tx.Exec(`UPDATE tasks SET project_id = ?, number = ?, parent_id = ?, title = ?, description_md = ?, status_id = ?,
		priority = ?, area = ?, planned_for = ?, position = ?, custom = ?, done_at = ?, deleted_at = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		x.ProjectID, x.Number, x.ParentID, x.Title, x.DescriptionMD, x.StatusID, x.Priority, x.Area,
		x.PlannedFor, x.Position, customJSON(x.Custom), x.DoneAt, x.DeletedAt, x.ID)
	return err
}

type Pos struct {
	ID       int64
	Position float64
}

// Column returns live tasks of one board column in order.
func (t *Tx) Column(projectID, statusID int64) ([]Pos, error) {
	rows, err := t.tx.Query(`SELECT id, position FROM tasks WHERE project_id = ? AND status_id = ? AND deleted_at IS NULL
		ORDER BY position, id`, projectID, statusID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pos
	for rows.Next() {
		var p Pos
		if err := rows.Scan(&p.ID, &p.Position); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *Tx) SetPosition(id int64, pos float64) error {
	_, err := t.tx.Exec(`UPDATE tasks SET position = ? WHERE id = ?`, pos, id)
	return err
}

func (t *Tx) CountTasksInStatus(statusID int64) (n int, err error) {
	err = t.tx.QueryRow(`SELECT count(*) FROM tasks WHERE status_id = ?`, statusID).Scan(&n)
	return
}

// MoveStatusTasks re-homes every task (trash included) of one status; positions go to the end of the target column.
func (t *Tx) MoveStatusTasks(from, to int64, toDone bool) error {
	var max float64
	if err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM tasks WHERE status_id = ?`, to).Scan(&max); err != nil {
		return err
	}
	done := `NULL`
	if toDone {
		done = `coalesce(done_at, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`
	}
	_, err := t.tx.Exec(`UPDATE tasks SET status_id = ?, position = position + ?, done_at = `+done+` WHERE status_id = ?`, to, max, from)
	return err
}

// Purge physically deletes everything in the trash and returns how many tasks were removed.
func (t *Tx) Purge() (int64, error) {
	const trashed = `SELECT id FROM tasks WHERE deleted_at IS NOT NULL`
	for _, q := range []string{
		`UPDATE tasks SET parent_id = NULL WHERE deleted_at IS NULL AND parent_id IN (` + trashed + `)`,
		`DELETE FROM notes WHERE task_id IN (` + trashed + `)`,
		`DELETE FROM activity WHERE task_id IN (` + trashed + `)`,
	} {
		if _, err := t.tx.Exec(q); err != nil {
			return 0, err
		}
	}
	res, err := t.tx.Exec(`DELETE FROM tasks WHERE deleted_at IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SearchTitles matches the title column; SearchDescriptions matches the description and returns a snippet.
func (t *Tx) SearchTitles(q string, limit int) ([]SearchHit, error) {
	return t.search(q, "title", `''`, limit)
}

func (t *Tx) SearchDescriptions(q string, limit int) ([]SearchHit, error) {
	return t.search(q, "description_md", `snippet(tasks_fts, 1, '[', ']', '…', 12)`, limit)
}

func (t *Tx) search(q, col, snippet string, limit int) ([]SearchHit, error) {
	fq := ftsQuery(q)
	out := []SearchHit{}
	if fq == "" {
		return out, nil
	}
	rows, err := t.tx.Query(`SELECT t.id, t.project_id, p.key || '-' || printf('%02d', t.number), t.title, s.key, s.name, s.color, `+snippet+
		` FROM tasks_fts JOIN tasks t ON t.id = tasks_fts.rowid JOIN statuses s ON s.id = t.status_id JOIN projects p ON p.id = t.project_id
		WHERE tasks_fts MATCH ? AND t.deleted_at IS NULL AND p.deleted_at IS NULL ORDER BY rank LIMIT ?`, col+` : (`+fq+`)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.Code, &h.Title, &h.Status, &h.StatusName, &h.StatusColor, &h.Snippet); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---- notes & activity ----

func (t *Tx) InsertNote(n Note) (Note, error) {
	res, err := t.tx.Exec(`INSERT INTO notes(task_id, author, body_md) VALUES (?, ?, ?)`, n.TaskID, n.Author, n.BodyMD)
	if err != nil {
		return n, err
	}
	n.ID, _ = res.LastInsertId()
	err = t.tx.QueryRow(`SELECT created_at FROM notes WHERE id = ?`, n.ID).Scan(&n.CreatedAt)
	return n, err
}

// ListNotes returns notes of one task, or of all tasks when taskID is 0.
func (t *Tx) ListNotes(taskID int64) ([]Note, error) {
	q, args := `SELECT id, task_id, author, body_md, created_at FROM notes`, []any{}
	if taskID != 0 {
		q += ` WHERE task_id = ?`
		args = append(args, taskID)
	}
	rows, err := t.tx.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.TaskID, &n.Author, &n.BodyMD, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (t *Tx) InsertActivity(a Activity) error {
	_, err := t.tx.Exec(`INSERT INTO activity(task_id, project_id, actor, action, diff_json) VALUES (?, ?, ?, ?, ?)`,
		a.TaskID, a.ProjectID, a.Actor, a.Action, a.DiffJSON)
	return err
}

// ListActivity returns the newest `limit` entries of one task (taskID 0: all tasks; limit <= 0: no limit),
// with the code and title of the task they belong to.
func (t *Tx) ListActivity(taskID int64, limit int) ([]Activity, error) {
	q, args := `SELECT a.id, a.task_id, a.project_id, a.actor, a.action, a.diff_json, a.at,
		coalesce(p.key || '-' || printf('%02d', t.number), ''), coalesce(t.title, '')
		FROM activity a LEFT JOIN tasks t ON t.id = a.task_id LEFT JOIN projects p ON p.id = t.project_id`, []any{}
	if taskID != 0 {
		q += ` WHERE a.task_id = ?`
		args = append(args, taskID)
	}
	q += ` ORDER BY a.id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := t.tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Activity{}
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.ID, &a.TaskID, &a.ProjectID, &a.Actor, &a.Action, &a.DiffJSON, &a.At, &a.TaskCode, &a.TaskTitle); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AllTasks returns every task including the trash, optionally of one project, for the JSON export.
func (t *Tx) AllTasks(projectID int64) ([]Task, error) {
	q, args := `SELECT `+taskCols+taskFrom, []any{}
	if projectID != 0 {
		q += ` WHERE t.project_id = ?`
		args = append(args, projectID)
	}
	rows, err := t.tx.Query(q+` ORDER BY t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		x, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
