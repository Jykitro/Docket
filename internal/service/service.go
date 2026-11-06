// Package service holds all domain rules. REST and MCP both call it, so validation,
// activity and live events behave the same for denis and claude.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"docket/internal/events"
	"docket/internal/store"
)

const (
	ActorDenis  = "denis"
	ActorClaude = "claude"
)

// Error is a client-facing error. Code: validation, not_found, conflict, status_in_use, forbidden, tool_disabled.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

func invalid(format string, a ...any) error {
	return &Error{Code: "validation", Message: fmt.Sprintf(format, a...)}
}

func notFound(format string, a ...any) error {
	return &Error{Code: "not_found", Message: fmt.Sprintf(format, a...)}
}

func conflict(format string, a ...any) error {
	return &Error{Code: "conflict", Message: fmt.Sprintf(format, a...)}
}

type Service struct {
	st       *store.Store
	bus      *events.Bus
	loc      *time.Location
	envToken string // DOCKET_MCP_TOKEN: wins over the stored token
}

func New(st *store.Store, bus *events.Bus, loc *time.Location) *Service {
	return &Service{st: st, bus: bus, loc: loc}
}

// Today returns today's date in the configured time zone.
func (s *Service) Today() string { return time.Now().In(s.loc).Format("2006-01-02") }

// op is one write transaction; events are published only after commit.
type op struct {
	tx    *store.Tx
	actor string
	evs   []events.Event
}

type diff map[string][2]any

func (o *op) emit(typ string, taskID, projectID int64) {
	o.evs = append(o.evs, events.Event{Type: typ, TaskID: taskID, ProjectID: projectID, Actor: o.actor})
}

func (o *op) log(action string, taskID, projectID int64, d diff) error {
	if d == nil {
		d = diff{}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	a := store.Activity{Actor: o.actor, Action: action, DiffJSON: string(b)}
	if taskID != 0 {
		a.TaskID = &taskID
	}
	if projectID != 0 {
		a.ProjectID = &projectID
	}
	typ := action
	if strings.HasPrefix(action, "project_") {
		typ = "project_changed"
	}
	o.emit(typ, taskID, projectID)
	return o.tx.InsertActivity(a)
}

func (s *Service) write(ctx context.Context, actor string, fn func(*op) error) error {
	if actor != ActorDenis && actor != ActorClaude {
		return invalid("unknown actor %q", actor)
	}
	o := &op{actor: actor}
	err := s.st.WithTx(ctx, func(tx *store.Tx) error { o.tx = tx; return fn(o) })
	if err != nil {
		return mapErr(err)
	}
	for _, e := range o.evs {
		s.bus.Publish(e)
	}
	return nil
}

func (s *Service) read(ctx context.Context, fn func(*store.Tx) error) error {
	return mapErr(s.st.WithTx(ctx, fn))
}

func mapErr(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return notFound("not found")
	}
	return err
}

func nowTS() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// ---- validation ----

var (
	priorities = []string{"P0", "P1", "P2"}
	slugRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	colorRe    = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	keyRe      = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,4}$`)
	codeRe     = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,4})-(\d+)$`)

	projectIcons = []string{"target", "clover", "key", "stack", "candle", "bars", "board", "plug", "flag", "tag", "doc", "cal", "link", "spark", "home", "cart", "globe"}
	// projectColors is the 10-colour project palette; new projects take the next one.
	projectColors = []string{"#2D5CC9", "#17784C", "#B8620A", "#0E7490", "#C0265F", "#6D3FD0", "#4B5362", "#A12B2B", "#2F7D1F", "#B0791E"}
	// labelColors is the area-dot cycle; the first six belong to the default areas.
	labelColors  = []string{"#8B6CFF", "#4C8DF6", "#E5A23A", "#E56B9B", "#8A93A3", "#2EB5A3"}
	defaultAreas = map[string]string{"UI": "#8B6CFF", "API": "#4C8DF6", "Лендинг": "#E5A23A", "Бренд": "#E56B9B", "Инфра": "#8A93A3", "Данные": "#2EB5A3"}
)

func oneOf(field, v string, allowed []string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return invalid("%s must be one of %s", field, strings.Join(allowed, ", "))
}

func checkDate(field, v string) error {
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return invalid("%s must be YYYY-MM-DD, got %q", field, v)
	}
	return nil
}

// ---- projects ----

func (s *Service) ListProjects(ctx context.Context, includeArchived bool) (out []store.Project, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { out, err = tx.ListProjects(includeArchived); return err })
	return out, err
}

func (s *Service) GetProject(ctx context.Context, id int64) (p store.Project, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { p, err = tx.GetProject(id); return err })
	return p, err
}

type ProjectInput struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Key         string `json:"key"`
	Icon        string `json:"icon"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

func (s *Service) CreateProject(ctx context.Context, actor string, in ProjectInput) (p store.Project, err error) {
	err = s.write(ctx, actor, func(o *op) error { p, err = createProject(o, in); return err })
	return p, err
}

var defaultStatuses = []store.Status{
	{Key: "backlog", Name: "Бэклог", Color: "bk", Category: "todo"},
	{Key: "todo", Name: "К работе", Color: "td", Category: "todo"},
	{Key: "doing", Name: "В работе", Color: "dg", Category: "doing"},
	{Key: "done", Name: "Готово", Color: "dn", Category: "done"},
}

func createProject(o *op, in ProjectInput) (store.Project, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return store.Project{}, invalid("project name is required")
	}
	all, err := o.tx.ListProjects(true)
	if err != nil {
		return store.Project{}, err
	}
	color := in.Color
	if color == "" {
		color = projectColors[len(all)%len(projectColors)]
	} else if !colorRe.MatchString(color) {
		return store.Project{}, invalid("color must look like #rrggbb")
	}
	icon := in.Icon
	if icon == "" {
		icon = "board"
	} else if err := oneOf("icon", icon, projectIcons); err != nil {
		return store.Project{}, err
	}
	slug := in.Slug
	if slug != "" {
		if !slugRe.MatchString(slug) {
			return store.Project{}, invalid("slug may contain only a-z, 0-9 and '-'")
		}
		if _, err := o.tx.GetProjectBySlug(slug); err == nil {
			return store.Project{}, invalid("slug %q is taken", slug)
		}
	} else {
		base := slugify(name)
		slug = base
		for i := 2; ; i++ {
			if _, err := o.tx.GetProjectBySlug(slug); errors.Is(err, store.ErrNotFound) {
				break
			}
			slug = fmt.Sprintf("%s-%d", base, i)
		}
	}
	key := strings.ToUpper(strings.TrimSpace(in.Key))
	if key == "" {
		key = deriveKey(o.tx, slug)
	} else if err := checkKey(o.tx, key, 0); err != nil {
		return store.Project{}, err
	}
	p, err := o.tx.InsertProject(store.Project{Slug: slug, Key: key, Name: name, Icon: icon, Color: color, Description: strings.TrimSpace(in.Description)})
	if err != nil {
		return p, err
	}
	for i, st := range defaultStatuses {
		st.ProjectID, st.ShowOnBoard, st.Position = p.ID, true, float64(i+1)*1024
		if _, err := o.tx.InsertStatus(st); err != nil {
			return p, err
		}
	}
	return p, o.log("project_created", 0, p.ID, diff{"name": {nil, p.Name}})
}

// checkKey validates a project key and its uniqueness (ignoring project selfID).
func checkKey(tx *store.Tx, key string, selfID int64) error {
	if !keyRe.MatchString(key) {
		return invalid("key must be 2-5 characters: A-Z first, then A-Z or 0-9")
	}
	if p, err := tx.GetProjectByKey(key); err == nil && p.ID != selfID {
		return invalid("key %q is taken", key)
	}
	return nil
}

// deriveKey builds a free key from the first ASCII letters of the slug (PRJ for e.g. Cyrillic names).
func deriveKey(tx *store.Tx, slug string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(slug) {
		if (r >= 'A' && r <= 'Z') || (b.Len() > 0 && r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() == 3 {
			break
		}
	}
	base := b.String()
	if len(base) < 2 {
		base = "PRJ"
	}
	key := base
	for i := 2; ; i++ {
		if _, err := tx.GetProjectByKey(key); errors.Is(err, store.ErrNotFound) {
			return key
		}
		key = base + strconv.Itoa(i)
	}
}

func slugify(name string) string {
	if s := asciiSlug(name); s != "" {
		return s
	}
	return "project" // e.g. a Cyrillic-only name
}

type ProjectPatch struct {
	Name        *string  `json:"name"`
	Key         *string  `json:"key"`
	Icon        *string  `json:"icon"`
	Color       *string  `json:"color"`
	Description *string  `json:"description"`
	Position    *float64 `json:"position"`
	Archived    *bool    `json:"archived"`
}

func (s *Service) UpdateProject(ctx context.Context, actor string, id int64, in ProjectPatch) (p store.Project, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if p, err = o.tx.GetProject(id); err != nil {
			return notFound("project %d not found", id)
		}
		old := p
		if in.Name != nil {
			if p.Name = strings.TrimSpace(*in.Name); p.Name == "" {
				return invalid("project name is required")
			}
		}
		if in.Key != nil {
			p.Key = strings.ToUpper(strings.TrimSpace(*in.Key))
			if err := checkKey(o.tx, p.Key, p.ID); err != nil {
				return err
			}
		}
		if in.Color != nil {
			if !colorRe.MatchString(*in.Color) {
				return invalid("color must look like #rrggbb")
			}
			p.Color = *in.Color
		}
		if in.Icon != nil {
			if err := oneOf("icon", *in.Icon, projectIcons); err != nil {
				return err
			}
			p.Icon = *in.Icon
		}
		if in.Description != nil {
			p.Description = strings.TrimSpace(*in.Description)
		}
		if in.Position != nil {
			p.Position = *in.Position
		}
		if in.Archived != nil {
			if !*in.Archived {
				p.ArchivedAt = nil
			} else if p.ArchivedAt == nil {
				ts := nowTS()
				p.ArchivedAt = &ts
			}
		}
		if err := o.tx.UpdateProject(p); err != nil {
			return err
		}
		d := diff{}
		cmp(d, "name", old.Name, p.Name)
		cmp(d, "key", old.Key, p.Key)
		cmp(d, "color", old.Color, p.Color)
		cmp(d, "icon", old.Icon, p.Icon)
		cmp(d, "description", old.Description, p.Description)
		cmp(d, "archived", old.ArchivedAt != nil, p.ArchivedAt != nil)
		if p, err = o.tx.GetProject(id); err != nil {
			return err
		}
		return o.log("project_updated", 0, p.ID, d)
	})
	return p, err
}

// DeleteProject moves a project to the trash: it vanishes from every list, search and the MCP tools.
func (s *Service) DeleteProject(ctx context.Context, actor string, id int64) (p store.Project, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if p, err = o.tx.GetProject(id); err != nil {
			return notFound("project %d not found", id)
		}
		if p.DeletedAt == nil {
			ts := nowTS()
			p.DeletedAt = &ts
			if err := o.tx.UpdateProject(p); err != nil {
				return err
			}
		}
		return o.log("project_deleted", 0, p.ID, diff{"name": {p.Name, nil}})
	})
	return p, err
}

// RestoreProject brings a project back from the trash.
func (s *Service) RestoreProject(ctx context.Context, actor string, id int64) (p store.Project, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if p, err = o.tx.GetProject(id); err != nil {
			return notFound("project %d not found", id)
		}
		if p.DeletedAt == nil {
			return invalid("project %q is not in the trash", p.Name)
		}
		p.DeletedAt = nil
		if err := o.tx.UpdateProject(p); err != nil {
			return err
		}
		return o.log("project_restored", 0, p.ID, diff{"name": {nil, p.Name}})
	})
	return p, err
}

func (s *Service) ListDeletedProjects(ctx context.Context) (out []store.Project, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { out, err = tx.ListDeletedProjects(); return err })
	return out, err
}

// resolveProject accepts a slug, a key (TRK) or a numeric id.
func resolveProject(tx *store.Tx, ref string) (store.Project, error) {
	ref = strings.TrimSpace(ref)
	p, err := tx.GetProjectBySlug(ref)
	if errors.Is(err, store.ErrNotFound) {
		p, err = tx.GetProjectByKey(strings.ToUpper(ref))
	}
	if errors.Is(err, store.ErrNotFound) {
		if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
			p, err = tx.GetProject(id)
		}
	}
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.DeletedAt != nil) {
		return p, invalid("unknown project %q", ref)
	}
	return p, err
}

func (s *Service) ResolveProject(ctx context.Context, ref string) (p store.Project, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { p, err = resolveProject(tx, ref); return err })
	return p, err
}

// resolveTask accepts a code (TRK-07, case-insensitive) or a numeric id.
func resolveTask(tx *store.Tx, ref string) (store.Task, error) {
	ref = strings.TrimSpace(ref)
	if m := codeRe.FindStringSubmatch(ref); m != nil {
		p, err := tx.GetProjectByKey(strings.ToUpper(m[1]))
		if err != nil {
			return store.Task{}, notFound("task %s not found", ref)
		}
		n, _ := strconv.Atoi(m[2])
		t, err := tx.GetTaskByNumber(p.ID, n)
		if err != nil {
			return t, notFound("task %s not found", ref)
		}
		return t, nil
	}
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil {
		return store.Task{}, invalid("task must be an id like TRK-07 or a number, got %q", ref)
	}
	t, err := tx.GetTask(id)
	if err != nil {
		return t, notFound("task %s not found", ref)
	}
	return t, nil
}

func (s *Service) ResolveTask(ctx context.Context, ref string) (t store.Task, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { t, err = resolveTask(tx, ref); return err })
	return t, err
}

// ---- tasks: reads ----

func (s *Service) ListTasks(ctx context.Context, f store.TaskFilter) (out []store.Task, err error) {
	if f.PlannedFor != "" {
		if err := checkDate("planned_for", f.PlannedFor); err != nil {
			return nil, err
		}
	}
	err = s.read(ctx, func(tx *store.Tx) error { out, err = tx.ListTasks(f); return err })
	return out, err
}

type TaskDetail struct {
	store.Task
	Subtasks []store.Task     `json:"subtasks"`
	Notes    []store.Note     `json:"notes"`
	Activity []store.Activity `json:"activity"`
}

// GetTask takes a code (TRK-07) or a numeric id.
func (s *Service) GetTask(ctx context.Context, ref string, activityLimit int) (d TaskDetail, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		if d.Task, err = resolveTask(tx, ref); err != nil {
			return err
		}
		if d.Subtasks, err = tx.ListTasks(store.TaskFilter{ParentID: d.ID, IncludeDone: true}); err != nil {
			return err
		}
		if d.Notes, err = tx.ListNotes(d.ID); err != nil {
			return err
		}
		d.Activity, err = tx.ListActivity(d.ID, activityLimit)
		return err
	})
	return d, err
}

type TodayResult struct {
	Date     string           `json:"date"`
	Projects []store.Project  `json:"projects"`
	Tasks    []store.Task     `json:"tasks"`
	Overdue  []store.Task     `json:"overdue"`
	Activity []store.Activity `json:"activity"`
}

// TodayView returns the tasks planned for date (done included) and open tasks planned before it.
func (s *Service) TodayView(ctx context.Context, date string) (r TodayResult, err error) {
	if date == "" {
		date = s.Today()
	}
	if err := checkDate("date", date); err != nil {
		return r, err
	}
	r.Date = date
	err = s.read(ctx, func(tx *store.Tx) error {
		if r.Projects, err = tx.ListProjects(false); err != nil {
			return err
		}
		if r.Tasks, err = tx.ListTasks(store.TaskFilter{PlannedFor: date, IncludeDone: true}); err != nil {
			return err
		}
		if r.Overdue, err = tx.ListTasks(store.TaskFilter{PlannedBefore: date}); err != nil {
			return err
		}
		r.Activity, err = tx.ListActivity(0, 15)
		return err
	})
	return r, err
}

func (s *Service) Trash(ctx context.Context) ([]store.Task, error) {
	return s.ListTasks(ctx, store.TaskFilter{Deleted: true, IncludeDone: true})
}

type Export struct {
	ExportedAt string           `json:"exported_at"`
	Projects   []store.Project  `json:"projects"`
	Statuses   []store.Status   `json:"statuses"`
	Labels     []store.Label    `json:"labels"`
	Fields     []store.Field    `json:"fields"`
	Views      []store.View     `json:"views"`
	Tasks      []store.Task     `json:"tasks"`
	Notes      []store.Note     `json:"notes"`
	Activity   []store.Activity `json:"activity"`
}

// Export dumps everything, or one project when projectID is set.
func (s *Service) Export(ctx context.Context, projectID int64) (e Export, err error) {
	e.ExportedAt = nowTS()
	err = s.read(ctx, func(tx *store.Tx) error {
		if e.Projects, err = tx.ListProjects(true); err != nil {
			return err
		}
		if projectID != 0 {
			if _, err := tx.GetProject(projectID); err != nil {
				return err
			}
			keep := e.Projects[:0]
			for _, p := range e.Projects {
				if p.ID == projectID {
					keep = append(keep, p)
				}
			}
			e.Projects = keep
		}
		e.Statuses, e.Labels, e.Fields, e.Views = []store.Status{}, []store.Label{}, []store.Field{}, []store.View{}
		for _, p := range e.Projects {
			ss, err := tx.ListStatuses(p.ID)
			if err != nil {
				return err
			}
			e.Statuses = append(e.Statuses, ss...)
			ls, err := tx.ListLabels(p.ID)
			if err != nil {
				return err
			}
			e.Labels = append(e.Labels, ls...)
			fs, err := tx.ListFields(p.ID)
			if err != nil {
				return err
			}
			e.Fields = append(e.Fields, fs...)
		}
		vs, err := tx.ListViews(projectID)
		if err != nil {
			return err
		}
		e.Views = vs
		if e.Tasks, err = tx.AllTasks(projectID); err != nil {
			return err
		}
		ids := map[int64]bool{}
		for _, t := range e.Tasks {
			ids[t.ID] = true
		}
		ns, err := tx.ListNotes(0)
		if err != nil {
			return err
		}
		e.Notes = []store.Note{}
		for _, n := range ns {
			if ids[n.TaskID] {
				e.Notes = append(e.Notes, n)
			}
		}
		as, err := tx.ListActivity(0, 0)
		if err != nil {
			return err
		}
		e.Activity = []store.Activity{}
		for _, a := range as {
			if projectID == 0 || (a.ProjectID != nil && *a.ProjectID == projectID) {
				e.Activity = append(e.Activity, a)
			}
		}
		return nil
	})
	return e, err
}

// ---- tasks: writes ----

// NewTask creates a task. Project is an id, slug or key (used when ProjectID is 0). Status is a status key.
// Ref/ParentRef link items of one batch; Parent is an existing task (code or id); ExternalID makes creation idempotent.
type NewTask struct {
	ProjectID     int64          `json:"project_id"`
	Project       string         `json:"project"`
	Title         string         `json:"title"`
	DescriptionMD string         `json:"description_md"`
	Status        string         `json:"status"`
	Priority      string         `json:"priority"`
	Area          string         `json:"area"`
	PlannedFor    string         `json:"planned_for"`
	Custom        map[string]any `json:"custom"`
	ParentID      int64          `json:"parent_id"`
	Parent        string         `json:"parent"`
	Ref           string         `json:"ref"`
	ParentRef     string         `json:"parent_ref"`
	ExternalID    string         `json:"external_id"`
}

func (s *Service) CreateTasks(ctx context.Context, actor string, items []NewTask) (out []store.Task, err error) {
	if len(items) == 0 || len(items) > 100 {
		return nil, invalid("send between 1 and 100 tasks")
	}
	err = s.write(ctx, actor, func(o *op) error { out, err = createTasks(o, items); return err })
	return out, err
}

func createTasks(o *op, items []NewTask) ([]store.Task, error) {
	refs := map[string]int64{}
	out := make([]store.Task, 0, len(items))
	for i, it := range items {
		fail := func(err error) error {
			if len(items) > 1 {
				var se *Error
				if errors.As(err, &se) {
					return &Error{Code: se.Code, Message: fmt.Sprintf("tasks[%d]: %s", i, se.Message)}
				}
			}
			return err
		}
		var p store.Project
		var err error
		if it.ProjectID != 0 {
			if p, err = o.tx.GetProject(it.ProjectID); err != nil {
				return nil, fail(invalid("unknown project %d", it.ProjectID))
			}
		} else if p, err = resolveProject(o.tx, it.Project); err != nil {
			return nil, fail(err)
		}
		parentID := it.ParentID
		switch {
		case it.ParentRef != "":
			id, ok := refs[it.ParentRef]
			if !ok {
				return nil, fail(invalid("parent_ref %q must refer to an earlier item's ref", it.ParentRef))
			}
			parentID = id
		case it.Parent != "":
			pt, err := resolveTask(o.tx, it.Parent)
			if err != nil {
				return nil, fail(err)
			}
			parentID = pt.ID
		}
		priority := it.Priority
		if priority == "" {
			priority = "P1"
		}
		statusKey := it.Status
		ch := Changes{Title: &it.Title, Priority: &priority, Area: &it.Area, PlannedFor: &it.PlannedFor,
			ParentID: &parentID, ProjectID: &p.ID}
		if statusKey != "" {
			ch.Status = &statusKey
		}
		if it.DescriptionMD != "" || it.ExternalID == "" {
			ch.DescriptionMD = &it.DescriptionMD
		}
		if len(it.Custom) > 0 {
			ch.Custom = it.Custom
		}

		var t store.Task
		if it.ExternalID != "" {
			t, err = o.tx.GetTaskByExternalID(it.ExternalID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
		}
		if t.ID != 0 { // known external_id: update in place
			d, err := apply(o, &t, ch)
			if err != nil {
				return nil, fail(err)
			}
			if len(d) > 0 {
				if err := o.log("updated", t.ID, t.ProjectID, d); err != nil {
					return nil, err
				}
			}
		} else {
			t = store.Task{CreatedBy: o.actor, ProjectID: p.ID, Custom: map[string]any{}}
			st, err := defaultStatus(o.tx, p.ID)
			if err != nil {
				return nil, fail(err)
			}
			setStatus(&t, st)
			if it.ExternalID != "" {
				t.ExternalID = &it.ExternalID
			}
			if _, err := validate(o, &t, ch); err != nil {
				return nil, fail(err)
			}
			if t.Number, err = o.tx.NextNumber(p.ID); err != nil {
				return nil, err
			}
			if t.Position, err = endOf(o.tx, t.ProjectID, t.StatusID); err != nil {
				return nil, err
			}
			if err := ensureLabel(o.tx, t.ProjectID, t.Area); err != nil {
				return nil, err
			}
			if t, err = o.tx.InsertTask(t); err != nil {
				return nil, err
			}
			if err := o.log("created", t.ID, t.ProjectID, nil); err != nil {
				return nil, err
			}
		}
		if it.Ref != "" {
			refs[it.Ref] = t.ID
		}
		out = append(out, t)
	}
	return out, nil
}

// Changes describes a task edit. nil = leave as is; PlannedFor "" clears the date; ParentID 0 detaches.
// Status is a status key of the (new) project. ParentRef is a task code/id ("" detaches). Custom is keyed by
// field name or field id; a nil value clears the field.
type Changes struct {
	Title         *string        `json:"title"`
	DescriptionMD *string        `json:"description_md"`
	Priority      *string        `json:"priority"`
	Area          *string        `json:"area"`
	PlannedFor    *string        `json:"planned_for"`
	Custom        map[string]any `json:"custom"`
	ParentID      *int64         `json:"parent_id"`
	ParentRef     *string        `json:"parent"`
	Status        *string        `json:"status"`
	ProjectID     *int64         `json:"project_id"`
}

func setStatus(t *store.Task, st store.Status) {
	t.StatusID, t.Status, t.StatusName, t.StatusCategory = st.ID, st.Key, st.Name, st.Category
}

// defaultStatus is "backlog" or, when the project removed it, its first status.
func defaultStatus(tx *store.Tx, projectID int64) (store.Status, error) {
	if st, err := tx.GetStatusByKey(projectID, "backlog"); err == nil {
		return st, nil
	}
	all, err := tx.ListStatuses(projectID)
	if err != nil {
		return store.Status{}, err
	}
	if len(all) == 0 {
		return store.Status{}, invalid("project has no statuses")
	}
	return all[0], nil
}

func statusByKey(tx *store.Tx, projectID int64, key string) (store.Status, error) {
	st, err := tx.GetStatusByKey(projectID, key)
	if errors.Is(err, store.ErrNotFound) {
		all, _ := tx.ListStatuses(projectID)
		keys := make([]string, len(all))
		for i, s := range all {
			keys[i] = s.Key
		}
		return st, invalid("unknown status %q (this project has: %s)", key, strings.Join(keys, ", "))
	}
	return st, err
}

// validate applies c to t in memory, enforcing all rules. It does not write the task.
func validate(o *op, t *store.Task, c Changes) (diff, error) {
	old := *t
	if c.Title != nil {
		if t.Title = strings.TrimSpace(*c.Title); t.Title == "" {
			return nil, invalid("title is required")
		}
	}
	if c.DescriptionMD != nil {
		t.DescriptionMD = *c.DescriptionMD
	}
	if c.Priority != nil {
		if err := oneOf("priority", *c.Priority, priorities); err != nil {
			return nil, err
		}
		t.Priority = *c.Priority
	}
	if c.Area != nil {
		if t.Area = strings.TrimSpace(*c.Area); utf8.RuneCountInString(t.Area) > 40 {
			return nil, invalid("area is longer than 40 characters")
		}
	}
	if c.PlannedFor != nil {
		if *c.PlannedFor == "" {
			t.PlannedFor = nil
		} else {
			if err := checkDate("planned_for", *c.PlannedFor); err != nil {
				return nil, err
			}
			v := *c.PlannedFor
			t.PlannedFor = &v
		}
	}
	if c.ProjectID != nil && *c.ProjectID != t.ProjectID {
		np, err := o.tx.GetProject(*c.ProjectID)
		if err != nil {
			return nil, invalid("unknown project %d", *c.ProjectID)
		}
		// Keep the status key if the new project has it, else the first status of the same category.
		st, err := o.tx.GetStatusByKey(np.ID, t.Status)
		if err != nil {
			all, lerr := o.tx.ListStatuses(np.ID)
			if lerr != nil || len(all) == 0 {
				return nil, invalid("project %d has no statuses", np.ID)
			}
			st = all[0]
			for _, s := range all {
				if s.Category == t.StatusCategory {
					st = s
					break
				}
			}
		}
		t.ProjectID, t.ParentID = np.ID, nil // a parent never spans projects
		t.Custom = map[string]any{}          // fields belong to one project
		setStatus(t, st)
	}
	if c.Status != nil {
		st, err := statusByKey(o.tx, t.ProjectID, *c.Status)
		if err != nil {
			return nil, err
		}
		setStatus(t, st)
	}
	if c.ParentRef != nil {
		if *c.ParentRef == "" {
			zero := int64(0)
			c.ParentID = &zero
		} else {
			pt, err := resolveTask(o.tx, *c.ParentRef)
			if err != nil {
				return nil, err
			}
			c.ParentID = &pt.ID
		}
	}
	if c.ParentID != nil {
		if *c.ParentID == 0 {
			t.ParentID = nil
		} else {
			if err := checkParent(o.tx, t, *c.ParentID); err != nil {
				return nil, err
			}
			v := *c.ParentID
			t.ParentID = &v
		}
	}
	if c.Custom != nil {
		if err := applyCustom(o.tx, t, c.Custom); err != nil {
			return nil, err
		}
	}
	if t.StatusCategory == "done" && t.DoneAt == nil {
		ts := nowTS()
		t.DoneAt = &ts
	} else if t.StatusCategory != "done" {
		t.DoneAt = nil
	}
	return diffTasks(old, *t), nil
}

// applyCustom merges values into t.Custom after checking them against the project's field definitions.
func applyCustom(tx *store.Tx, t *store.Task, in map[string]any) error {
	fields, err := tx.ListFields(t.ProjectID)
	if err != nil {
		return err
	}
	out := maps.Clone(t.Custom)
	if out == nil {
		out = map[string]any{}
	}
	for k, v := range in {
		var f *store.Field
		for i := range fields {
			if fields[i].Name == k || strconv.FormatInt(fields[i].ID, 10) == k {
				f = &fields[i]
				break
			}
		}
		if f == nil {
			return invalid("unknown custom field %q", k)
		}
		id := strconv.FormatInt(f.ID, 10)
		if v == nil || v == "" {
			delete(out, id)
			continue
		}
		switch f.Type {
		case "number":
			switch n := v.(type) {
			case float64, int, int64:
				out[id] = n
			case string:
				x, err := strconv.ParseFloat(n, 64)
				if err != nil {
					return invalid("field %q must be a number", f.Name)
				}
				out[id] = x
			default:
				return invalid("field %q must be a number", f.Name)
			}
		case "url":
			u, ok := v.(string)
			if !ok || !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
				return invalid("field %q must be an http(s) link", f.Name)
			}
			out[id] = u
		case "select":
			sv, ok := v.(string)
			if !ok || !contains(f.Options, sv) {
				return invalid("field %q must be one of %s", f.Name, strings.Join(f.Options, ", "))
			}
			out[id] = sv
		}
	}
	t.Custom = out
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func checkParent(tx *store.Tx, t *store.Task, parentID int64) error {
	if parentID == t.ID {
		return invalid("a task cannot be its own parent")
	}
	p, err := tx.GetTask(parentID)
	if err != nil || p.DeletedAt != nil {
		return invalid("parent task %d not found", parentID)
	}
	if p.ProjectID != t.ProjectID {
		return invalid("parent task %s belongs to another project", p.Code)
	}
	if p.ParentID != nil {
		return invalid("parent task %s is itself a subtask (only one level of nesting)", p.Code)
	}
	if t.SubtaskCount > 0 {
		return invalid("task %s has subtasks and cannot become a subtask", t.Code)
	}
	return nil
}

// ensureLabel creates the area label on first use, coloured from the next slot of the circle.
func ensureLabel(tx *store.Tx, projectID int64, area string) error {
	if area == "" {
		return nil
	}
	if _, err := tx.GetLabelByName(projectID, area); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	color, err := nextLabelColor(tx, projectID, area)
	if err != nil {
		return err
	}
	_, err = tx.InsertLabel(store.Label{ProjectID: projectID, Name: area, Color: color})
	return err
}

// apply validates c, saves t and moves its subtasks along when the project changes.
// A task whose column changed is appended to the end of the new column.
func apply(o *op, t *store.Task, c Changes) (diff, error) {
	old := *t
	d, err := validate(o, t, c)
	if err != nil {
		return nil, err
	}
	if t.ProjectID != old.ProjectID {
		if t.Number, err = o.tx.NextNumber(t.ProjectID); err != nil {
			return nil, err
		}
	}
	if t.StatusID != old.StatusID || t.ProjectID != old.ProjectID {
		if t.Position, err = endOf(o.tx, t.ProjectID, t.StatusID); err != nil {
			return nil, err
		}
	}
	if err := ensureLabel(o.tx, t.ProjectID, t.Area); err != nil {
		return nil, err
	}
	if err := o.tx.UpdateTask(*t); err != nil {
		return nil, err
	}
	if t.ProjectID != old.ProjectID {
		kids, err := o.tx.ListTasks(store.TaskFilter{ParentID: t.ID, IncludeDone: true})
		if err != nil {
			return nil, err
		}
		for _, k := range kids {
			if err := moveSubtask(o, k, t.ProjectID); err != nil {
				return nil, err
			}
		}
	}
	*t, err = o.tx.GetTask(t.ID)
	return d, err
}

// moveSubtask carries a subtask along with its epic into another project.
func moveSubtask(o *op, k store.Task, projectID int64) error {
	kold := k
	st, err := o.tx.GetStatusByKey(projectID, k.Status)
	if err != nil {
		all, lerr := o.tx.ListStatuses(projectID)
		if lerr != nil || len(all) == 0 {
			return invalid("project %d has no statuses", projectID)
		}
		st = all[0]
		for _, s := range all {
			if s.Category == k.StatusCategory {
				st = s
				break
			}
		}
	}
	k.ProjectID, k.Custom = projectID, map[string]any{}
	setStatus(&k, st)
	if k.Number, err = o.tx.NextNumber(projectID); err != nil {
		return err
	}
	if k.Position, err = endOf(o.tx, projectID, st.ID); err != nil {
		return err
	}
	if err := ensureLabel(o.tx, projectID, k.Area); err != nil {
		return err
	}
	if err := o.tx.UpdateTask(k); err != nil {
		return err
	}
	return o.log("moved", k.ID, projectID, diffTasks(kold, k))
}

func cmp(d diff, k string, a, b any) {
	if !reflect.DeepEqual(a, b) {
		d[k] = [2]any{a, b}
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func diffTasks(a, b store.Task) diff {
	d := diff{}
	cmp(d, "title", a.Title, b.Title)
	cmp(d, "description_md", a.DescriptionMD, b.DescriptionMD)
	cmp(d, "status", a.StatusName, b.StatusName)
	cmp(d, "priority", a.Priority, b.Priority)
	cmp(d, "area", a.Area, b.Area)
	cmp(d, "planned_for", deref(a.PlannedFor), deref(b.PlannedFor))
	cmp(d, "parent_id", deref(a.ParentID), deref(b.ParentID))
	cmp(d, "project_id", a.ProjectID, b.ProjectID)
	cmp(d, "custom", a.Custom, b.Custom)
	return d
}

func endOf(tx *store.Tx, projectID, statusID int64) (float64, error) {
	col, err := tx.Column(projectID, statusID)
	if err != nil || len(col) == 0 {
		return 1024, err
	}
	return col[len(col)-1].Position + 1024, nil
}

func liveTask(tx *store.Tx, ref string) (store.Task, error) {
	t, err := resolveTask(tx, ref)
	if err != nil {
		return t, err
	}
	if t.DeletedAt != nil {
		return t, notFound("task %s not found", t.Code)
	}
	return t, nil
}

// UpdateTask takes a task code (TRK-07) or numeric id.
func (s *Service) UpdateTask(ctx context.Context, actor string, ref string, c Changes) (t store.Task, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if t, err = liveTask(o.tx, ref); err != nil {
			return err
		}
		d, err := apply(o, &t, c)
		if err != nil || len(d) == 0 {
			return err
		}
		return o.log("updated", t.ID, t.ProjectID, d)
	})
	return t, err
}

type MoveInput struct {
	Status    string `json:"status"`
	BeforeID  int64  `json:"before_id"` // place directly above this task
	AfterID   int64  `json:"after_id"`  // place directly below this task
	ProjectID int64  `json:"project_id"`
}

// MoveTask is drag-n-drop: change column and/or position inside a column.
func (s *Service) MoveTask(ctx context.Context, actor string, ref string, in MoveInput) (t store.Task, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if t, err = liveTask(o.tx, ref); err != nil {
			return err
		}
		c := Changes{}
		if in.Status != "" {
			c.Status = &in.Status
		}
		if in.ProjectID != 0 {
			c.ProjectID = &in.ProjectID
		}
		d, err := apply(o, &t, c)
		if err != nil {
			return err
		}
		if in.BeforeID != 0 || in.AfterID != 0 {
			if t.Position, err = place(o.tx, t, in.BeforeID, in.AfterID); err != nil {
				return err
			}
			if err := o.tx.SetPosition(t.ID, t.Position); err != nil {
				return err
			}
		}
		return o.log("moved", t.ID, t.ProjectID, d)
	})
	return t, err
}

// place computes a fractional position for t between its new neighbours, rebalancing the
// column (step 1024) when the gap gets too small.
func place(tx *store.Tx, t store.Task, beforeID, afterID int64) (float64, error) {
	all, err := tx.Column(t.ProjectID, t.StatusID)
	if err != nil {
		return 0, err
	}
	col := all[:0]
	for _, p := range all {
		if p.ID != t.ID {
			col = append(col, p)
		}
	}
	idx := -1
	for i, p := range col {
		if beforeID != 0 && p.ID == beforeID {
			idx = i
		} else if beforeID == 0 && p.ID == afterID {
			idx = i + 1
		}
	}
	if idx < 0 {
		return 0, invalid("before_id/after_id must be a task in the target column")
	}
	for {
		switch {
		case idx == 0:
			return col[0].Position - 1024, nil
		case idx == len(col):
			return col[len(col)-1].Position + 1024, nil
		}
		a, b := col[idx-1].Position, col[idx].Position
		if b-a >= 1e-6 {
			return (a + b) / 2, nil
		}
		for i := range col {
			col[i].Position = float64(i+1) * 1024
			if err := tx.SetPosition(col[i].ID, col[i].Position); err != nil {
				return 0, err
			}
		}
	}
}

// BulkMove changes status, date and/or project of many tasks (MCP move_tasks).
type BulkMove struct {
	Status     *string // status key
	PlannedFor *string // "" clears
	Project    *string // id, slug or key
}

func (s *Service) MoveTasks(ctx context.Context, actor string, refs []string, in BulkMove) (n int, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		c := Changes{Status: in.Status, PlannedFor: in.PlannedFor}
		if in.Project != nil {
			p, err := resolveProject(o.tx, *in.Project)
			if err != nil {
				return err
			}
			c.ProjectID = &p.ID
		}
		for _, ref := range refs {
			t, err := liveTask(o.tx, ref)
			if err != nil {
				return err
			}
			d, err := apply(o, &t, c)
			if err != nil {
				return invalid("task %s: %v", ref, err)
			}
			if err := o.log("moved", t.ID, t.ProjectID, d); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// DeleteTasks moves tasks (and their subtasks) to the trash.
func (s *Service) DeleteTasks(ctx context.Context, actor string, refs []string) (n int, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		ts := nowTS()
		for _, ref := range refs {
			t, err := resolveTask(o.tx, ref)
			if err != nil {
				return err
			}
			if t.DeletedAt != nil {
				if *t.DeletedAt == ts {
					continue // trashed above as a subtask of an earlier id
				}
				return notFound("task %s not found", t.Code)
			}
			kids, err := o.tx.ListTasks(store.TaskFilter{ParentID: t.ID, IncludeDone: true})
			if err != nil {
				return err
			}
			for _, x := range append(kids, t) {
				x.DeletedAt = &ts
				if err := o.tx.UpdateTask(x); err != nil {
					return err
				}
				if err := o.log("deleted", x.ID, x.ProjectID, nil); err != nil {
					return err
				}
				n++
			}
		}
		return nil
	})
	return n, err
}

// RestoreTask takes a task out of the trash with the subtasks that were trashed together with it.
func (s *Service) RestoreTask(ctx context.Context, actor string, ref string) (t store.Task, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if t, err = resolveTask(o.tx, ref); err != nil {
			return err
		}
		if t.DeletedAt == nil {
			return invalid("task %s is not in the trash", t.Code)
		}
		when := *t.DeletedAt
		restore := func(x store.Task) error {
			x.DeletedAt = nil
			if x.ParentID != nil {
				if p, err := o.tx.GetTask(*x.ParentID); err != nil || p.DeletedAt != nil {
					x.ParentID = nil // the parent stays in the trash
				}
			}
			if err := o.tx.UpdateTask(x); err != nil {
				return err
			}
			return o.log("restored", x.ID, x.ProjectID, nil)
		}
		id := t.ID
		if err := restore(t); err != nil {
			return err
		}
		kids, err := o.tx.ListTasks(store.TaskFilter{ParentID: id, IncludeDone: true, Deleted: true})
		if err != nil {
			return err
		}
		for _, k := range kids {
			if *k.DeletedAt == when {
				if err := restore(k); err != nil {
					return err
				}
			}
		}
		t, err = o.tx.GetTask(id)
		return err
	})
	return t, err
}

// PurgeTrash physically deletes the trash. Only the owner's UI may call it.
func (s *Service) PurgeTrash(ctx context.Context) (n int64, err error) {
	err = s.write(ctx, ActorDenis, func(o *op) error {
		gone, err := o.tx.ListDeletedProjects()
		if err != nil {
			return err
		}
		for _, p := range gone {
			k, err := o.tx.PurgeProject(p.ID)
			if err != nil {
				return err
			}
			n += k
		}
		k, err := o.tx.Purge()
		n += k
		o.emit("purged", 0, 0)
		return err
	})
	return n, err
}

func (s *Service) AddNote(ctx context.Context, actor string, ref string, body string) (n store.Note, err error) {
	if strings.TrimSpace(body) == "" {
		return n, invalid("note body is required")
	}
	err = s.write(ctx, actor, func(o *op) error {
		t, err := liveTask(o.tx, ref)
		if err != nil {
			return err
		}
		if n, err = o.tx.InsertNote(store.Note{TaskID: t.ID, Author: actor, BodyMD: body}); err != nil {
			return err
		}
		return o.log("note_added", t.ID, t.ProjectID, nil)
	})
	return n, err
}

// CarryOver moves open tasks planned for `from` to `to`.
func (s *Service) CarryOver(ctx context.Context, actor, from, to string) (n int, err error) {
	if err := checkDate("from", from); err != nil {
		return 0, err
	}
	if err := checkDate("to", to); err != nil {
		return 0, err
	}
	err = s.write(ctx, actor, func(o *op) error {
		ts, err := o.tx.ListTasks(store.TaskFilter{PlannedFor: from})
		if err != nil {
			return err
		}
		for _, t := range ts {
			t.PlannedFor = &to
			if err := o.tx.UpdateTask(t); err != nil {
				return err
			}
			if err := o.log("carried_over", t.ID, t.ProjectID, diff{"planned_for": {from, to}}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// ---- seed ----

type seedFile struct {
	Projects []ProjectInput `json:"projects"`
	Tasks    []NewTask      `json:"tasks"`
}

// Seed imports path when the database has no projects yet. Returns whether it imported.
func (s *Service) Seed(ctx context.Context, path string) (bool, error) {
	ps, err := s.ListProjects(ctx, true)
	if err != nil || len(ps) > 0 {
		return false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var f seedFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return false, fmt.Errorf("seed %s: %w", path, err)
	}
	return true, s.write(ctx, ActorClaude, func(o *op) error {
		for _, p := range f.Projects {
			if _, err := o.tx.GetProjectBySlug(p.Slug); errors.Is(err, store.ErrNotFound) {
				if _, err := createProject(o, p); err != nil {
					return err
				}
			}
		}
		for i := 0; i < len(f.Tasks); i += 100 {
			if _, err := createTasks(o, f.Tasks[i:min(i+100, len(f.Tasks))]); err != nil {
				return err
			}
		}
		return nil
	})
}
