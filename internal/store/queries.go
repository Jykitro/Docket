package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrNotFound = errors.New("not found")

type Project struct {
	ID          int64          `json:"id"`
	Slug        string         `json:"slug"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	IconURL     string         `json:"icon_url,omitempty"`
	IconPNG     []byte         `json:"icon_png,omitempty"` // Populated only for JSON export.
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
	HasDescription bool   `json:"has_description,omitempty"`
	ProjectSlug    string `json:"-"`
	readBytes      int
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
	IssueKind      string         `json:"issue_kind,omitempty"`
	AttentionAfter *string        `json:"attention_after,omitempty"`
	ReportCount    int            `json:"report_count,omitempty"`
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
	ID          int64  `json:"id"`
	TaskID      *int64 `json:"task_id"`
	ProjectID   *int64 `json:"project_id"`
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	DiffJSON    string `json:"diff_json"`
	DiffOmitted bool   `json:"diff_omitted,omitempty"`
	At          string `json:"at"`
	TaskCode    string `json:"task_code"`
	TaskTitle   string `json:"task_title"`
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

const projectCols = `id, slug, key, name, icon, color, description, position, archived_at, deleted_at, created_at, updated_at, icon_hash`

func (t *Tx) scanProject(r interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var hash string
	err := r.Scan(&p.ID, &p.Slug, &p.Key, &p.Name, &p.Icon, &p.Color, &p.Description, &p.Position, &p.ArchivedAt, &p.DeletedAt, &p.CreatedAt, &p.UpdatedAt, &hash)
	if hash != "" {
		prefix := "/api"
		if t.scopedURL {
			prefix = fmt.Sprintf("/api/workspaces/%d", t.tx.WorkspaceID())
		}
		p.IconURL = fmt.Sprintf("%s/projects/%d/icon?v=%s", prefix, p.ID, hash)
	}
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return p, err
}

func (t *Tx) CountLiveProjects() (count int, err error) {
	err = t.tx.QueryRow("SELECT count(*) FROM projects WHERE workspace_id=:workspace AND deleted_at IS NULL").Scan(&count)
	return
}

func (t *Tx) ProjectPage(includeArchived, deleted, compact bool, limit, offset int) ([]Project, error) {
	cols := projectCols
	if compact {
		cols = strings.Replace(cols, "description,", "'' as description,", 1)
	}
	where, order := "deleted_at IS NULL", "position,id"
	if deleted {
		where, order = "deleted_at IS NOT NULL", "deleted_at DESC,id"
	} else if !includeArchived {
		where += " AND archived_at IS NULL"
	}
	page := " FROM projects WHERE workspace_id=:workspace AND " + where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	rows, err := t.tx.Query("SELECT "+cols+page, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	byID := map[int64]int{}
	bytes := 0
	for rows.Next() {
		p, err := t.scanProject(rows)
		if err != nil {
			return nil, err
		}
		bytes += len(p.Name) + len(p.Slug) + len(p.Description)
		if bytes > maxReadBytes && len(out) > 0 {
			return nil, ErrPageTooLarge
		}
		byID[p.ID] = len(out)
		if !deleted {
			p.Counts = map[string]int{}
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if deleted || len(out) == 0 {
		return out, nil
	}
	counts, err := t.tx.Query(`SELECT t.project_id,s.key,s.category,count(*) FROM tasks t JOIN statuses s ON s.id=t.status_id AND t.workspace_id=:workspace WHERE t.deleted_at IS NULL AND t.project_id IN (SELECT id`+page+") GROUP BY 1,2,3", limit, offset)
	if err != nil {
		return nil, err
	}
	defer counts.Close()
	for counts.Next() {
		var id int64
		var key, cat string
		var n int
		if err := counts.Scan(&id, &key, &cat, &n); err != nil {
			return nil, err
		}
		p := &out[byID[id]]
		p.Counts[key] = n
		if cat != "done" {
			p.Open += n
		}
	}
	return out, counts.Err()
}

// PurgeProject physically removes a project with its tasks, notes, history and configuration.
func (t *Tx) PurgeProject(id int64) (int64, error) {
	for _, q := range []string{
		`UPDATE pages SET parent_id = NULL WHERE workspace_id=:workspace AND project_id = ?`,
		`DELETE FROM pages WHERE workspace_id=:workspace AND project_id = ?`,
		`UPDATE tasks SET parent_id = NULL WHERE workspace_id=:workspace AND project_id = ?`,
		`DELETE FROM notes WHERE workspace_id=:workspace AND task_id IN (SELECT id FROM tasks WHERE workspace_id=:workspace AND project_id = ?)`,
		`DELETE FROM activity WHERE workspace_id=:workspace AND task_id IN (SELECT id FROM tasks WHERE workspace_id=:workspace AND project_id = ?)`,
		`DELETE FROM activity WHERE workspace_id=:workspace AND project_id = ?`,
	} {
		if _, err := t.tx.Exec(q, id); err != nil {
			return 0, err
		}
	}
	res, err := t.tx.Exec(`DELETE FROM tasks WHERE workspace_id=:workspace AND project_id = ?`, id)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	for _, q := range []string{
		`DELETE FROM statuses WHERE workspace_id=:workspace AND project_id = ?`, `DELETE FROM labels WHERE workspace_id=:workspace AND project_id = ?`,
		`DELETE FROM custom_fields WHERE workspace_id=:workspace AND project_id = ?`, `DELETE FROM saved_views WHERE workspace_id=:workspace AND project_id = ?`,
		`DELETE FROM projects WHERE workspace_id=:workspace AND id = ?`,
	} {
		if _, err := t.tx.Exec(q, id); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func (t *Tx) ProjectIcon(id int64) ([]byte, error) {
	var data []byte
	err := t.tx.QueryRow(`SELECT icon_png FROM projects WHERE workspace_id=:workspace AND id = ? AND icon_png IS NOT NULL`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

func (t *Tx) SetProjectIcon(id int64, data []byte, hash string) error {
	_, err := t.tx.Exec(`UPDATE projects SET icon_png = ?, icon_hash = ? WHERE workspace_id=:workspace AND id = ?`, data, hash, id)
	return err
}

func (t *Tx) GetProject(id int64) (Project, error) {
	return t.scanProject(t.tx.QueryRow(`SELECT `+projectCols+` FROM projects WHERE workspace_id=:workspace AND id = ?`, id))
}

func (t *Tx) GetProjectBySlug(slug string) (Project, error) {
	return t.scanProject(t.tx.QueryRow(`SELECT `+projectCols+` FROM projects WHERE workspace_id=:workspace AND slug = ?`, slug))
}

func (t *Tx) GetProjectByKey(key string) (Project, error) {
	return t.scanProject(t.tx.QueryRow(`SELECT `+projectCols+` FROM projects WHERE workspace_id=:workspace AND key = ?`, key))
}

func (t *Tx) InsertProject(p Project) (Project, error) {
	var max float64
	if err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM projects WHERE workspace_id=:workspace`).Scan(&max); err != nil {
		return p, err
	}
	var id int64
	err := t.tx.QueryRow(`INSERT INTO projects(workspace_id, slug, key, name, icon, color, description, position) VALUES (:workspace, ?,?,?,?,?,?,?) RETURNING id`,
		p.Slug, p.Key, p.Name, p.Icon, p.Color, p.Description, max+1024).Scan(&id)
	if err != nil {
		return p, err
	}
	return t.GetProject(id)
}

func (t *Tx) UpdateProject(p Project) error {
	_, err := t.tx.Exec(`UPDATE projects SET key = ?, name = ?, icon = ?, color = ?, description = ?, position = ?, archived_at = ?, deleted_at = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE workspace_id=:workspace AND id = ?`,
		p.Key, p.Name, p.Icon, p.Color, p.Description, p.Position, p.ArchivedAt, p.DeletedAt, p.ID)
	return err
}

// NextNumber hands out the next task number of a project; numbers are never reused.
func (t *Tx) NextNumber(projectID int64) (int, error) {
	var n int
	err := t.tx.QueryRow(`UPDATE projects SET task_seq = task_seq + 1 WHERE workspace_id=:workspace AND id = ? RETURNING task_seq`, projectID).Scan(&n)
	return n, err
}

// ---- tasks ----

const taskFrom = ` FROM tasks t JOIN statuses s ON s.id = t.status_id AND t.workspace_id=:workspace JOIN projects p ON p.id = t.project_id`

const taskCols = `t.id, t.project_id, t.number, p.key || '-' || printf('%02d', t.number), t.parent_id, t.title, t.description_md,
	t.status_id, s.key, s.name, s.category, t.priority, t.area, t.planned_for, t.position, t.custom, t.external_id, t.created_by,
	t.created_at, t.updated_at, t.done_at, t.deleted_at, t.issue_kind, t.attention_after,
 CASE WHEN t.issue_kind = '' THEN 0 ELSE (SELECT count(*) FROM issue_reports ir WHERE ir.workspace_id=:workspace AND ir.issue_id = t.id) END,
	(SELECT count(*) FROM tasks c WHERE c.workspace_id=:workspace AND c.parent_id = t.id AND c.deleted_at IS NULL),
	(SELECT count(*) FROM tasks c JOIN statuses cs ON cs.id = c.status_id WHERE c.workspace_id=:workspace AND c.parent_id = t.id AND c.deleted_at IS NULL AND cs.category = 'done'),
	coalesce((SELECT p.key || '-' || printf('%02d', pt.number) FROM tasks pt WHERE pt.workspace_id=:workspace AND pt.id = t.parent_id), '')`

func scanTask(r interface{ Scan(...any) error }) (Task, error) {
	var x Task
	var custom string
	err := r.Scan(&x.ID, &x.ProjectID, &x.Number, &x.Code, &x.ParentID, &x.Title, &x.DescriptionMD,
		&x.StatusID, &x.Status, &x.StatusName, &x.StatusCategory, &x.Priority, &x.Area, &x.PlannedFor, &x.Position, &custom, &x.ExternalID, &x.CreatedBy,
		&x.CreatedAt, &x.UpdatedAt, &x.DoneAt, &x.DeletedAt, &x.IssueKind, &x.AttentionAfter, &x.ReportCount, &x.SubtaskCount, &x.SubtaskDone, &x.ParentCode)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	if err == nil {
		x.readBytes = len(custom) + len(x.DescriptionMD) + len(x.Title) + len(x.Area) + len(x.StatusName) + len(x.Status) + len(x.ParentCode) + len(x.Code) + len(x.CreatedBy)
		if x.ExternalID != nil {
			x.readBytes += len(*x.ExternalID)
		}
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
	Limit, Offset int
	Summary       bool // REST cards retain custom fields and counts, with a description presence flag.
	Compact       bool // MCP board and day rows need only identity, title and display fields.
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
	if t.tx.Postgres() {
		if q := postgresFTSQuery(f.Q); q != "" {
			add("to_tsvector('simple', t.title || ' ' || t.description_md) @@ to_tsquery('simple', ?)", q)
		} else if strings.TrimSpace(f.Q) != "" {
			where = append(where, "FALSE")
		}
	} else if q := ftsQuery(f.Q); q != "" {
		add("t.id IN (SELECT rowid FROM tasks_fts WHERE tasks_fts MATCH ?)", q)
	}
	cols := taskCols
	scan := scanTask
	if f.Compact {
		cols = `t.id,t.project_id,p.slug,p.key || '-' || printf('%02d', t.number), t.title,s.key,t.priority,t.area,t.planned_for,octet_length(t.description_md) > 0,
        coalesce((SELECT p.key || '-' || printf('%02d', pt.number) FROM tasks pt WHERE pt.workspace_id=:workspace AND pt.id=t.parent_id),'')`
		scan = func(r scanner) (x Task, err error) {
			err = r.Scan(&x.ID, &x.ProjectID, &x.ProjectSlug, &x.Code, &x.Title, &x.Status, &x.Priority, &x.Area, &x.PlannedFor, &x.HasDescription, &x.ParentCode)
			x.readBytes = len(x.Title) + len(x.Area) + len(x.Status) + len(x.ProjectSlug) + len(x.ParentCode) + len(x.Code)
			return
		}
		if f.PlannedFor != "" {
			order = "p.position,p.id,s.position,t.position"
		}
	} else if f.Summary {
		cols = strings.Replace(taskCols, "t.description_md,", "CASE WHEN octet_length(t.description_md) > 0 THEN '1' ELSE '' END,", 1)
		scan = func(r scanner) (x Task, err error) {
			x, err = scanTask(r)
			x.HasDescription = x.DescriptionMD != ""
			x.DescriptionMD = ""
			return
		}
	}
	filtered := taskFrom + " WHERE " + strings.Join(where, " AND ")
	ordered := " ORDER BY " + order + ",t.id"
	query := "SELECT " + cols + filtered + ordered
	if f.Limit > 0 {
		window := ordered + " LIMIT ? OFFSET ?"
		if f.Summary && !f.Compact {
			// Select the requested IDs before reading custom fields and counting children.
			// This also bounds the work for rows skipped on later board pages.
			query = "WITH cards AS MATERIALIZED (SELECT t.id" + filtered + window + ") SELECT " +
				cols + taskFrom + " JOIN cards ON cards.id=t.id" + ordered
		} else {
			query = "SELECT " + cols + filtered + window
		}
		args = append(args, f.Limit, f.Offset)
	}
	rows, err := t.tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	bytes := 0
	for rows.Next() {
		x, err := scan(rows)
		if err != nil {
			return nil, err
		}
		bytes += x.readBytes
		if f.Limit > 0 && bytes > maxReadBytes && len(out) > 0 {
			return nil, ErrPageTooLarge
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (t *Tx) InsertTask(x Task) (Task, error) {
	var id int64
	err := t.tx.QueryRow(`INSERT INTO tasks(workspace_id, project_id, number, parent_id, title, description_md, status_id, priority, area,
		planned_for, position, custom, external_id, created_by, done_at) VALUES (:workspace, ?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
		x.ProjectID, x.Number, x.ParentID, x.Title, x.DescriptionMD, x.StatusID, x.Priority, x.Area,
		x.PlannedFor, x.Position, customJSON(x.Custom), x.ExternalID, x.CreatedBy, x.DoneAt).Scan(&id)
	if err != nil {
		return x, err
	}
	return t.GetTask(id)
}

// UpdateTask writes every mutable column of x.
func (t *Tx) UpdateTask(x Task) error {
	_, err := t.tx.Exec(`UPDATE tasks SET project_id = ?, number = ?, parent_id = ?, title = ?, description_md = ?, status_id = ?,
		priority = ?, area = ?, planned_for = ?, position = ?, custom = ?, done_at = ?, deleted_at = ?,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE workspace_id=:workspace AND id = ?`,
		x.ProjectID, x.Number, x.ParentID, x.Title, x.DescriptionMD, x.StatusID, x.Priority, x.Area,
		x.PlannedFor, x.Position, customJSON(x.Custom), x.DoneAt, x.DeletedAt, x.ID)
	return err
}

type Pos struct {
	ID       int64
	Position float64
}

// ColumnEnd returns the highest position among live tasks of one board column, and false for an empty column.
// It reads one entry of idx_tasks_board instead of the whole column, so adding a task stays fast in a long column.
func (t *Tx) ColumnEnd(projectID, statusID int64) (float64, bool, error) {
	var end sql.NullFloat64
	err := t.tx.QueryRow(`SELECT max(position) FROM tasks WHERE workspace_id=:workspace AND project_id = ? AND status_id = ? AND deleted_at IS NULL`,
		projectID, statusID).Scan(&end)
	return end.Float64, end.Valid, err
}

// Column returns live tasks of one board column in order.
func (t *Tx) Column(projectID, statusID int64) ([]Pos, error) {
	return t.positions(`SELECT id, position FROM tasks WHERE workspace_id=:workspace AND project_id = ? AND status_id = ? AND deleted_at IS NULL
		ORDER BY position, id`, projectID, statusID)
}

func (t *Tx) positions(query string, args ...any) ([]Pos, error) {
	rows, err := t.tx.Query(query, args...)
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
	_, err := t.tx.Exec(`UPDATE tasks SET position = ? WHERE workspace_id=:workspace AND id = ?`, pos, id)
	return err
}

func (t *Tx) CountTasksInStatus(statusID int64) (n int, err error) {
	err = t.tx.QueryRow(`SELECT count(*) FROM tasks WHERE workspace_id=:workspace AND status_id = ?`, statusID).Scan(&n)
	return
}

// MoveStatusTasks re-homes every task (trash included) of one status; positions go to the end of the target column.
func (t *Tx) MoveStatusTasks(from, to int64, toDone bool) error {
	var max float64
	if err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM tasks WHERE workspace_id=:workspace AND status_id = ?`, to).Scan(&max); err != nil {
		return err
	}
	done := `NULL`
	if toDone {
		done = `coalesce(done_at, strftime('%Y-%m-%dT%H:%M:%fZ','now'))`
	}
	_, err := t.tx.Exec(`UPDATE tasks SET status_id = ?, position = position + ?, done_at = `+done+` WHERE workspace_id=:workspace AND status_id = ?`, to, max, from)
	return err
}

type TrashEntry struct {
	ID        int64  `json:"id"`
	DeletedAt string `json:"deleted_at"`
}

// TrashMatches checks the same visible sets returned by the two trash listings in the purge transaction.
func (t *Tx) TrashMatches(tasks, projects []TrashEntry) (bool, error) {
	for _, set := range []struct {
		query string
		seen  []TrashEntry
	}{
		{`SELECT t.id,t.deleted_at FROM tasks t JOIN projects p ON p.id=t.project_id WHERE t.workspace_id=:workspace AND t.deleted_at IS NOT NULL AND p.deleted_at IS NULL`, tasks},
		{`SELECT id,deleted_at FROM projects WHERE workspace_id=:workspace AND deleted_at IS NOT NULL`, projects},
	} {
		remaining := make(map[TrashEntry]bool, len(set.seen))
		for _, entry := range set.seen {
			if remaining[entry] {
				return false, nil
			}
			remaining[entry] = true
		}
		rows, err := t.tx.Query(set.query)
		if err != nil {
			return false, err
		}
		for rows.Next() {
			var entry TrashEntry
			if err := rows.Scan(&entry.ID, &entry.DeletedAt); err != nil {
				rows.Close()
				return false, err
			}
			if !remaining[entry] {
				rows.Close()
				return false, nil
			}
			delete(remaining, entry)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return false, err
		}
		if len(remaining) > 0 {
			return false, nil
		}
	}
	return true, nil
}

// Purge physically deletes everything in the trash and returns how many tasks were removed.
func (t *Tx) Purge() (int64, error) {
	const trashed = `SELECT id FROM tasks WHERE workspace_id=:workspace AND deleted_at IS NOT NULL`
	for _, q := range []string{
		`UPDATE tasks SET parent_id = NULL WHERE workspace_id=:workspace AND deleted_at IS NULL AND parent_id IN (` + trashed + `)`,
		`DELETE FROM notes WHERE workspace_id=:workspace AND task_id IN (` + trashed + `)`,
		`DELETE FROM activity WHERE workspace_id=:workspace AND task_id IN (` + trashed + `)`,
	} {
		if _, err := t.tx.Exec(q); err != nil {
			return 0, err
		}
	}
	res, err := t.tx.Exec(`DELETE FROM tasks WHERE workspace_id=:workspace AND deleted_at IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// SearchTitles matches the title column; SearchDescriptions matches the description and returns a snippet.
func (t *Tx) SearchTitles(q string, limit int) ([]SearchHit, error) {
	return t.search(q, "title", `''`, limit)
}

// SearchNumber finds live tasks by number in every project, for a search of digits only: the number itself first
// ("08" is 8), then the numbers that start with those digits ("1" goes on to 10-19, 100…).
func (t *Tx) SearchNumber(digits string, limit int) ([]SearchHit, error) {
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n < 1 {
		return []SearchHit{}, nil
	}
	rows, err := t.tx.Query(`SELECT t.id, t.project_id, p.key || '-' || printf('%02d', t.number), t.title, s.key, s.name, s.color`+taskFrom+`
  WHERE (t.number = ? OR CAST(t.number AS TEXT) LIKE ?) AND t.deleted_at IS NULL AND p.deleted_at IS NULL
  ORDER BY CASE WHEN t.number = ? THEN 0 ELSE 1 END, t.number, p.key LIMIT ?`, n, strconv.FormatInt(n, 10)+"%", n, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.ProjectID, &h.Code, &h.Title, &h.Status, &h.StatusName, &h.StatusColor); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (t *Tx) SearchDescriptions(q string, limit int) ([]SearchHit, error) {
	return t.search(q, "description_md", `snippet(tasks_fts, 1, '[', ']', '…', 12)`, limit)
}

func (t *Tx) search(q, col, snippet string, limit int) ([]SearchHit, error) {
	fq := ftsQuery(q)
	if t.tx.Postgres() {
		fq = postgresFTSQuery(q)
	}
	out := []SearchHit{}
	if fq == "" {
		return out, nil
	}
	query := `SELECT t.id, t.project_id, p.key || '-' || printf('%02d', t.number), t.title, s.key, s.name, s.color, ` + snippet +
		` FROM tasks_fts JOIN tasks t ON t.id = tasks_fts.rowid JOIN statuses s ON s.id = t.status_id AND t.workspace_id=:workspace JOIN projects p ON p.id = t.project_id
  WHERE tasks_fts MATCH ? AND t.deleted_at IS NULL AND p.deleted_at IS NULL ORDER BY rank LIMIT ?`
	args := []any{col + ` : (` + fq + `)`, limit}
	if t.tx.Postgres() {
		snippet = `''`
		if col == "description_md" {
			snippet = `ts_headline('simple', t.description_md, query, 'StartSel=[, StopSel=], MaxWords=12, MinWords=4')`
		}
		query = `SELECT t.id, t.project_id, p.key || '-' || printf('%02d', t.number), t.title, s.key, s.name, s.color, ` + snippet +
			taskFrom + `, to_tsquery('simple', ?) query WHERE to_tsvector('simple', t.` + col + `) @@ query
   AND t.deleted_at IS NULL AND p.deleted_at IS NULL ORDER BY ts_rank(to_tsvector('simple', t.` + col + `), query) DESC, t.id LIMIT ?`
		args = []any{fq, limit}
	}
	rows, err := t.tx.Query(query, args...)
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
	err := t.tx.QueryRow(`INSERT INTO notes(workspace_id, task_id, author, body_md) VALUES (:workspace, ?, ?, ?) RETURNING id`, n.TaskID, n.Author, n.BodyMD).Scan(&n.ID)
	if err != nil {
		return n, err
	}
	err = t.tx.QueryRow(`SELECT created_at FROM notes WHERE workspace_id=:workspace AND id = ?`, n.ID).Scan(&n.CreatedAt)
	return n, err
}

const maxReadBytes = 4 << 20

var ErrPageTooLarge = errors.New("page exceeds 4 MiB; request a smaller limit")

func (t *Tx) ListNotes(taskID int64, limit, offset int) ([]Note, error) {
	rows, err := t.tx.Query(`SELECT id,task_id,author,body_md,created_at FROM notes WHERE workspace_id=:workspace AND task_id=? ORDER BY id LIMIT ? OFFSET ?`, taskID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	bytes := 0
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		bytes += len(n.BodyMD)
		if bytes > maxReadBytes && len(out) > 0 {
			return nil, ErrPageTooLarge
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (t *Tx) InsertActivity(a Activity) error {
	_, err := t.tx.Exec(`INSERT INTO activity(workspace_id, task_id, project_id, actor, action, diff_json) VALUES (:workspace, ?, ?, ?, ?, ?)`,
		a.TaskID, a.ProjectID, a.Actor, a.Action, a.DiffJSON)
	return err
}

// ListActivity returns the newest `limit` entries of one task (taskID 0: all tasks; limit <= 0: no limit),
// with the code and title of the task they belong to.
func (t *Tx) ListActivity(taskID int64, limit int) ([]Activity, error) {
	// Keep normal diffs exact. Large history entries remain in the export; the UI only needs action metadata.
	cols := strings.Replace(activitySelect, "a.diff_json,", "CASE WHEN octet_length(a.diff_json)<=65536 THEN a.diff_json ELSE NULL END,", 1)
	cols = strings.Replace(cols, "coalesce(t.title, '')", "substr(coalesce(t.title, ''),1,512)", 1)
	q, args := cols+" WHERE a.workspace_id=:workspace", []any{}
	if taskID != 0 {
		q += ` AND a.task_id = ?`
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
		a, err := scanActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
