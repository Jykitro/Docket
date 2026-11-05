package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Project configuration: statuses, labels, custom fields, saved views; plus settings and the MCP call log.

type Status struct {
	ID          int64   `json:"id"`
	ProjectID   int64   `json:"project_id"`
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Category    string  `json:"category"`
	ShowOnBoard bool    `json:"show_on_board"`
	Position    float64 `json:"position"`
	TaskCount   int     `json:"task_count"`
}

type Label struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"project_id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	TaskCount int    `json:"task_count"`
}

type Field struct {
	ID         int64    `json:"id"`
	ProjectID  int64    `json:"project_id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Options    []string `json:"options"`
	ShowOnCard bool     `json:"show_on_card"`
	Position   float64  `json:"position"`
}

type View struct {
	ID         int64           `json:"id"`
	ProjectID  *int64          `json:"project_id"`
	Name       string          `json:"name"`
	Layout     string          `json:"layout"`
	FilterJSON json.RawMessage `json:"filter"`
	GroupBy    string          `json:"group_by"`
	Sort       string          `json:"sort"`
	Pinned     bool            `json:"pinned"`
	Position   float64         `json:"position"`
}

type MCPCall struct {
	ID          int64   `json:"id"`
	At          string  `json:"at"`
	Tool        string  `json:"tool"`
	ArgsSummary string  `json:"args_summary"`
	DurationMS  int64   `json:"duration_ms"`
	Status      string  `json:"status"`
	ErrorCode   *string `json:"error_code"`
	Actor       string  `json:"actor"`
}

func notFoundIf(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (t *Tx) nextPos(table string, where string, args ...any) (float64, error) {
	var max float64
	err := t.tx.QueryRow(`SELECT coalesce(max(position), 0) FROM `+table+` WHERE `+where, args...).Scan(&max)
	return max + 1024, err
}

// ---- statuses ----

const statusCols = `s.id, s.project_id, s.key, s.name, s.color, s.category, s.show_on_board, s.position,
	(SELECT count(*) FROM tasks t WHERE t.status_id = s.id AND t.deleted_at IS NULL)`

func scanStatus(r interface{ Scan(...any) error }) (Status, error) {
	var s Status
	err := r.Scan(&s.ID, &s.ProjectID, &s.Key, &s.Name, &s.Color, &s.Category, &s.ShowOnBoard, &s.Position, &s.TaskCount)
	return s, notFoundIf(err)
}

func (t *Tx) ListStatuses(projectID int64) ([]Status, error) {
	rows, err := t.tx.Query(`SELECT `+statusCols+` FROM statuses s WHERE s.project_id = ? ORDER BY s.position, s.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Status{}
	for rows.Next() {
		s, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (t *Tx) GetStatus(id int64) (Status, error) {
	return scanStatus(t.tx.QueryRow(`SELECT `+statusCols+` FROM statuses s WHERE s.id = ?`, id))
}

func (t *Tx) GetStatusByKey(projectID int64, key string) (Status, error) {
	return scanStatus(t.tx.QueryRow(`SELECT `+statusCols+` FROM statuses s WHERE s.project_id = ? AND s.key = ?`, projectID, key))
}

func (t *Tx) InsertStatus(s Status) (Status, error) {
	if s.Position == 0 {
		var err error
		if s.Position, err = t.nextPos("statuses", "project_id = ?", s.ProjectID); err != nil {
			return s, err
		}
	}
	res, err := t.tx.Exec(`INSERT INTO statuses(project_id, key, name, color, category, show_on_board, position) VALUES (?,?,?,?,?,?,?)`,
		s.ProjectID, s.Key, s.Name, s.Color, s.Category, s.ShowOnBoard, s.Position)
	if err != nil {
		return s, err
	}
	id, _ := res.LastInsertId()
	return t.GetStatus(id)
}

func (t *Tx) UpdateStatus(s Status) error {
	_, err := t.tx.Exec(`UPDATE statuses SET name = ?, color = ?, category = ?, show_on_board = ?, position = ? WHERE id = ?`,
		s.Name, s.Color, s.Category, s.ShowOnBoard, s.Position, s.ID)
	return err
}

// SetDoneForStatus stamps or clears done_at of every task in a status after its category changed.
func (t *Tx) SetDoneForStatus(statusID int64, done bool) error {
	q := `UPDATE tasks SET done_at = NULL WHERE status_id = ?`
	if done {
		q = `UPDATE tasks SET done_at = coalesce(done_at, strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE status_id = ?`
	}
	_, err := t.tx.Exec(q, statusID)
	return err
}

func (t *Tx) DeleteStatus(id int64) error {
	_, err := t.tx.Exec(`DELETE FROM statuses WHERE id = ?`, id)
	return err
}

// ---- labels ----

const labelCols = `l.id, l.project_id, l.name, l.color,
	(SELECT count(*) FROM tasks t WHERE t.project_id = l.project_id AND t.area = l.name AND t.deleted_at IS NULL)`

func scanLabel(r interface{ Scan(...any) error }) (Label, error) {
	var l Label
	err := r.Scan(&l.ID, &l.ProjectID, &l.Name, &l.Color, &l.TaskCount)
	return l, notFoundIf(err)
}

func (t *Tx) ListLabels(projectID int64) ([]Label, error) {
	rows, err := t.tx.Query(`SELECT `+labelCols+` FROM labels l WHERE l.project_id = ? ORDER BY l.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Label{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (t *Tx) GetLabel(id int64) (Label, error) {
	return scanLabel(t.tx.QueryRow(`SELECT `+labelCols+` FROM labels l WHERE l.id = ?`, id))
}

func (t *Tx) GetLabelByName(projectID int64, name string) (Label, error) {
	return scanLabel(t.tx.QueryRow(`SELECT `+labelCols+` FROM labels l WHERE l.project_id = ? AND l.name = ?`, projectID, name))
}

func (t *Tx) CountLabels(projectID int64) (n int, err error) {
	err = t.tx.QueryRow(`SELECT count(*) FROM labels WHERE project_id = ?`, projectID).Scan(&n)
	return
}

func (t *Tx) InsertLabel(l Label) (Label, error) {
	res, err := t.tx.Exec(`INSERT INTO labels(project_id, name, color) VALUES (?,?,?)`, l.ProjectID, l.Name, l.Color)
	if err != nil {
		return l, err
	}
	id, _ := res.LastInsertId()
	return t.GetLabel(id)
}

// UpdateLabel renames/recolors a label; tasks follow a rename because task.area is the label name.
func (t *Tx) UpdateLabel(old, l Label) error {
	if _, err := t.tx.Exec(`UPDATE labels SET name = ?, color = ? WHERE id = ?`, l.Name, l.Color, l.ID); err != nil {
		return err
	}
	if old.Name != l.Name {
		_, err := t.tx.Exec(`UPDATE tasks SET area = ? WHERE project_id = ? AND area = ?`, l.Name, l.ProjectID, old.Name)
		return err
	}
	return nil
}

// DeleteLabel removes the label and clears the area of its tasks.
func (t *Tx) DeleteLabel(l Label) error {
	if _, err := t.tx.Exec(`UPDATE tasks SET area = '' WHERE project_id = ? AND area = ?`, l.ProjectID, l.Name); err != nil {
		return err
	}
	_, err := t.tx.Exec(`DELETE FROM labels WHERE id = ?`, l.ID)
	return err
}

// ---- custom fields ----

const fieldCols = `id, project_id, name, type, options, show_on_card, position`

func scanField(r interface{ Scan(...any) error }) (Field, error) {
	var f Field
	var opts string
	err := r.Scan(&f.ID, &f.ProjectID, &f.Name, &f.Type, &opts, &f.ShowOnCard, &f.Position)
	if err != nil {
		return f, notFoundIf(err)
	}
	if err = json.Unmarshal([]byte(opts), &f.Options); f.Options == nil {
		f.Options = []string{}
	}
	return f, err
}

func optsJSON(o []string) string {
	if len(o) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(o)
	return string(b)
}

func (t *Tx) ListFields(projectID int64) ([]Field, error) {
	rows, err := t.tx.Query(`SELECT `+fieldCols+` FROM custom_fields WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Field{}
	for rows.Next() {
		f, err := scanField(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (t *Tx) GetField(id int64) (Field, error) {
	return scanField(t.tx.QueryRow(`SELECT `+fieldCols+` FROM custom_fields WHERE id = ?`, id))
}

func (t *Tx) InsertField(f Field) (Field, error) {
	pos, err := t.nextPos("custom_fields", "project_id = ?", f.ProjectID)
	if err != nil {
		return f, err
	}
	res, err := t.tx.Exec(`INSERT INTO custom_fields(project_id, name, type, options, show_on_card, position) VALUES (?,?,?,?,?,?)`,
		f.ProjectID, f.Name, f.Type, optsJSON(f.Options), f.ShowOnCard, pos)
	if err != nil {
		return f, err
	}
	id, _ := res.LastInsertId()
	return t.GetField(id)
}

func (t *Tx) UpdateField(f Field) error {
	_, err := t.tx.Exec(`UPDATE custom_fields SET name = ?, options = ?, show_on_card = ?, position = ? WHERE id = ?`,
		f.Name, optsJSON(f.Options), f.ShowOnCard, f.Position, f.ID)
	return err
}

// DeleteField removes the field and its values from every task of the project.
func (t *Tx) DeleteField(f Field) error {
	if _, err := t.tx.Exec(`UPDATE tasks SET custom = json_remove(custom, ?) WHERE project_id = ?`, fmt.Sprintf(`$."%d"`, f.ID), f.ProjectID); err != nil {
		return err
	}
	_, err := t.tx.Exec(`DELETE FROM custom_fields WHERE id = ?`, f.ID)
	return err
}

// ---- saved views ----

const viewCols = `id, project_id, name, layout, filter_json, group_by, sort, pinned, position`

func scanView(r interface{ Scan(...any) error }) (View, error) {
	var v View
	var f string
	err := r.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Layout, &f, &v.GroupBy, &v.Sort, &v.Pinned, &v.Position)
	v.FilterJSON = json.RawMessage(f)
	return v, notFoundIf(err)
}

// ListViews returns the views of one project plus the global ones; projectID 0 returns only global views.
func (t *Tx) ListViews(projectID int64) ([]View, error) {
	rows, err := t.tx.Query(`SELECT `+viewCols+` FROM saved_views WHERE project_id IS NULL OR project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []View{}
	for rows.Next() {
		v, err := scanView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *Tx) GetView(id int64) (View, error) {
	return scanView(t.tx.QueryRow(`SELECT `+viewCols+` FROM saved_views WHERE id = ?`, id))
}

func (t *Tx) InsertView(v View) (View, error) {
	pos, err := t.nextPos("saved_views", "1 = 1")
	if err != nil {
		return v, err
	}
	res, err := t.tx.Exec(`INSERT INTO saved_views(project_id, name, layout, filter_json, group_by, sort, pinned, position) VALUES (?,?,?,?,?,?,?,?)`,
		v.ProjectID, v.Name, v.Layout, string(v.FilterJSON), v.GroupBy, v.Sort, v.Pinned, pos)
	if err != nil {
		return v, err
	}
	id, _ := res.LastInsertId()
	return t.GetView(id)
}

func (t *Tx) UpdateView(v View) error {
	_, err := t.tx.Exec(`UPDATE saved_views SET name = ?, layout = ?, filter_json = ?, group_by = ?, sort = ?, pinned = ?, position = ? WHERE id = ?`,
		v.Name, v.Layout, string(v.FilterJSON), v.GroupBy, v.Sort, v.Pinned, v.Position, v.ID)
	return err
}

func (t *Tx) DeleteView(id int64) error {
	_, err := t.tx.Exec(`DELETE FROM saved_views WHERE id = ?`, id)
	return err
}

// ---- settings ----

func (t *Tx) Setting(key string) (string, bool, error) {
	var v string
	err := t.tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (t *Tx) SetSetting(key, value string) error {
	_, err := t.tx.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// ---- MCP call log ----

func (t *Tx) InsertCall(c MCPCall) error {
	_, err := t.tx.Exec(`INSERT INTO mcp_calls(at, tool, args_summary, duration_ms, status, error_code, actor) VALUES (?,?,?,?,?,?,?)`,
		c.At, c.Tool, c.ArgsSummary, c.DurationMS, c.Status, c.ErrorCode, c.Actor)
	return err
}

// ListCalls returns the newest calls; filter is "all", "write" (tools listed in writeTools) or "error".
func (t *Tx) ListCalls(filter string, writeTools []string, limit int) ([]MCPCall, error) {
	q, args := `SELECT id, at, tool, args_summary, duration_ms, status, error_code, actor FROM mcp_calls`, []any{}
	switch filter {
	case "error":
		q += ` WHERE status = 'error'`
	case "write":
		q += ` WHERE tool IN (SELECT value FROM json_each(?))`
		b, _ := json.Marshal(writeTools)
		args = append(args, string(b))
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := t.tx.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MCPCall{}
	for rows.Next() {
		var c MCPCall
		if err := rows.Scan(&c.ID, &c.At, &c.Tool, &c.ArgsSummary, &c.DurationMS, &c.Status, &c.ErrorCode, &c.Actor); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *Tx) DeleteCalls() error {
	_, err := t.tx.Exec(`DELETE FROM mcp_calls`)
	return err
}

// PurgeCallsBefore drops log rows older than the cutoff timestamp.
func (t *Tx) PurgeCallsBefore(cutoff string) (int64, error) {
	res, err := t.tx.Exec(`DELETE FROM mcp_calls WHERE at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

type CallStats struct {
	LastAt     string
	CallsSince int
	MedianMS   int64
	PerTool    map[string]int
}

// CallStats summarises the log: calls since `since` with their median duration, and per-tool counts since `weekAgo`.
func (t *Tx) CallStats(since, weekAgo string) (CallStats, error) {
	st := CallStats{PerTool: map[string]int{}}
	if err := t.tx.QueryRow(`SELECT coalesce(max(at), '') FROM mcp_calls`).Scan(&st.LastAt); err != nil {
		return st, err
	}
	rows, err := t.tx.Query(`SELECT duration_ms FROM mcp_calls WHERE at >= ? ORDER BY duration_ms`, since)
	if err != nil {
		return st, err
	}
	var ds []int64
	for rows.Next() {
		var d int64
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return st, err
		}
		ds = append(ds, d)
	}
	rows.Close()
	st.CallsSince = len(ds)
	if len(ds) > 0 {
		st.MedianMS = ds[len(ds)/2]
	}
	rows, err = t.tx.Query(`SELECT tool, count(*) FROM mcp_calls WHERE at >= ? GROUP BY tool`, weekAgo)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var tool string
		var n int
		if err := rows.Scan(&tool, &n); err != nil {
			return st, err
		}
		st.PerTool[tool] = n
	}
	return st, rows.Err()
}
