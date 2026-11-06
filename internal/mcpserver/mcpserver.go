// Package mcpserver exposes the service as MCP tools over Streamable HTTP. Every call acts
// as claude; answers are compact JSON with IDs like TRK-07 to save the assistant's context.
// A middleware enforces the access mode and tool switches and writes every call to the log.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"docket/internal/service"
	"docket/internal/store"
	"docket/skills"
)

const actor = service.ActorClaude

// Handler returns the /mcp handler. Host checks and auth are done by the caller's middleware.
func Handler(svc *service.Service, loc *time.Location) http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "docket", Version: "1.0.0"}, &mcp.ServerOptions{Instructions: skills.Instructions()})
	t := &tools{svc: svc, loc: loc}

	add(s, "list_projects", t.listProjects)
	add(s, "get_board", t.getBoard)
	add(s, "get_today", t.getToday)
	add(s, "get_task", t.getTask)
	add(s, "search", t.search)
	add(s, "create_project", t.createProject)
	add(s, "create_tasks", t.createTasks)
	add(s, "update_task", t.updateTask)
	add(s, "move_tasks", t.moveTasks)
	add(s, "add_note", t.addNote)
	add(s, "list_pages", t.listPages)
	add(s, "read_page", t.readPage)
	add(s, "write_page", t.writePage)
	add(s, "append_to_page", t.appendToPage)
	add(s, "carry_over", t.carryOver)
	add(s, "delete_tasks", t.deleteTasks)
	s.AddReceivingMiddleware(policy(svc))

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// Our own middleware checks Host against DOCKET_ALLOWED_HOSTS (incl. docket.localhost).
		DisableLocalhostProtection: true,
	})
}

func description(name string) string {
	for _, t := range service.MCPTools {
		if t.Name == name {
			return t.Description
		}
	}
	panic("mcpserver: tool " + name + " is not in service.MCPTools")
}

// add registers a tool whose result is sent as one compact JSON text block (no structuredContent duplicate).
// Errors are reported as "code: message" so the call log and the assistant see a stable code.
func add[In any](s *mcp.Server, name string, fn func(context.Context, In) (any, error)) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description(name)},
		func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			v, err := fn(ctx, in)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %s", service.ErrorCode(err), err)
			}
			b, err := json.Marshal(v)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
		})
}

// policy hides switched-off tools from tools/list, refuses calls to them and to write tools in read-only
// mode, and logs every tools/call (also the refused and the malformed ones).
func policy(svc *service.Service) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			switch method {
			case "tools/list":
				res, err := next(ctx, method, req)
				if lr, ok := res.(*mcp.ListToolsResult); ok && err == nil {
					pol, perr := svc.MCPPolicy(ctx)
					if perr != nil {
						return nil, perr
					}
					keep := lr.Tools[:0]
					for _, t := range lr.Tools {
						if allowed(pol, t.Name) == "" {
							keep = append(keep, t)
						}
					}
					lr.Tools = keep
				}
				return res, err
			case "tools/call":
				return call(ctx, svc, method, req, next)
			}
			return next(ctx, method, req)
		}
	}
}

// allowed returns "" when the tool may run, else the error code that refuses it.
func allowed(pol service.MCPPolicy, tool string) string {
	switch {
	case pol.Disabled[tool]:
		return "tool_disabled"
	case pol.Mode == service.ModeReadOnly && service.IsWriteTool(tool):
		return "forbidden"
	}
	return ""
}

func call(ctx context.Context, svc *service.Service, method string, req mcp.Request, next mcp.MethodHandler) (mcp.Result, error) {
	start := time.Now()
	var name, args string
	if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
		name, args = p.Name, summarize(p.Arguments)
	}
	var res mcp.Result
	var err error
	code := ""
	pol, perr := svc.MCPPolicy(ctx)
	switch {
	case perr != nil:
		err, code = perr, "internal"
	case allowed(pol, name) != "":
		code = allowed(pol, name)
		msg := "tool " + name + " is switched off in Docket (MCP settings)"
		if code == "forbidden" {
			msg = "Docket MCP is in read-only mode: " + name + " writes data"
		}
		res = &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: code + ": " + msg}}}
	default:
		res, err = next(ctx, method, req)
		code = resultCode(res, err)
	}
	if rerr := svc.RecordMCPCall(context.WithoutCancel(ctx), name, args, time.Since(start), code); rerr != nil {
		slog.Error("mcp call log failed", "tool", name, "err", rerr)
	}
	return res, err
}

var knownCodes = map[string]bool{"not_found": true, "invalid_argument": true, "forbidden": true, "tool_disabled": true,
	"status_in_use": true, "internal": true}

// resultCode returns "" for a successful call, else the error code taken from the "code: message" text.
func resultCode(res mcp.Result, err error) string {
	if err != nil {
		return "invalid_argument" // protocol-level failure, e.g. arguments that do not match the schema
	}
	cr, ok := res.(*mcp.CallToolResult)
	if !ok || !cr.IsError {
		return ""
	}
	for _, c := range cr.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if code, _, found := strings.Cut(tc.Text, ":"); found && knownCodes[code] {
				return code
			}
		}
	}
	return "internal"
}

// summarize renders the call arguments as short JSON for the log, without long markdown bodies.
func summarize(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	var strip func(any) any
	strip = func(x any) any {
		switch t := x.(type) {
		case map[string]any:
			for k, e := range t {
				if k == "description_md" || k == "body_md" {
					t[k] = "…"
				} else {
					t[k] = strip(e)
				}
			}
		case []any:
			for i, e := range t {
				t[i] = strip(e)
			}
		}
		return x
	}
	b, _ := json.Marshal(strip(v))
	return string(b)
}

type tools struct {
	svc *service.Service
	loc *time.Location
}

// ---- compact shapes ----

type ctask struct {
	ID             string `json:"id"`
	Project        string `json:"project,omitempty"`
	Title          string `json:"title"`
	Status         string `json:"status,omitempty"`
	Priority       string `json:"priority"`
	Area           string `json:"area,omitempty"`
	PlannedFor     string `json:"planned_for,omitempty"`
	Parent         string `json:"parent,omitempty"`
	HasDescription bool   `json:"has_description,omitempty"`
}

func compact(t store.Task) ctask {
	c := ctask{ID: t.Code, Title: t.Title, Status: t.Status, Priority: t.Priority, Area: t.Area, Parent: t.ParentCode,
		HasDescription: t.DescriptionMD != ""}
	if t.PlannedFor != nil {
		c.PlannedFor = *t.PlannedFor
	}
	return c
}

// short renders a stored UTC timestamp as "2026-11-04 10:23" in the local zone.
func (t *tools) short(ts string) string {
	tm, err := time.Parse("2006-01-02T15:04:05.000Z", ts)
	if err != nil {
		return ts
	}
	return tm.In(t.loc).Format("2006-01-02 15:04")
}

func (t *tools) slugs(ctx context.Context) (map[int64]string, error) {
	ps, err := t.svc.ListProjects(ctx, true)
	m := map[int64]string{}
	for _, p := range ps {
		m[p.ID] = p.Slug
	}
	return m, err
}

// ---- tools ----

type listProjectsIn struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"also list archived projects"`
}

func (t *tools) listProjects(ctx context.Context, in listProjectsIn) (any, error) {
	ps, err := t.svc.ListProjects(ctx, in.IncludeArchived)
	if err != nil {
		return nil, err
	}
	type proj struct {
		Key      string         `json:"key"`
		Slug     string         `json:"slug"`
		Name     string         `json:"name"`
		Icon     string         `json:"icon,omitempty"`
		Archived bool           `json:"archived,omitempty"`
		Open     int            `json:"open"`
		Counts   map[string]int `json:"counts"`
	}
	out := []proj{}
	for _, p := range ps {
		out = append(out, proj{p.Key, p.Slug, p.Name, p.Icon, p.ArchivedAt != nil, p.Open, p.Counts})
	}
	return out, nil
}

type createProjectIn struct {
	Name        string `json:"name" jsonschema:"project name"`
	Key         string `json:"key,omitempty" jsonschema:"2-5 characters, A-Z first, then A-Z or 0-9; task IDs look like KEY-07; derived from the name when omitted"`
	Slug        string `json:"slug,omitempty" jsonschema:"short url id, a-z 0-9 and '-'"`
	Icon        string `json:"icon,omitempty" jsonschema:"icon name: target, clover, key, stack, candle, bars, board, plug, flag, tag, doc, cal, link, spark, home, cart or globe"`
	Color       string `json:"color,omitempty" jsonschema:"hex color like #2D5CC9"`
	Description string `json:"description,omitempty" jsonschema:"one line about the project"`
}

func (t *tools) createProject(ctx context.Context, in createProjectIn) (any, error) {
	p, err := t.svc.CreateProject(ctx, actor, service.ProjectInput{Name: in.Name, Key: in.Key, Slug: in.Slug, Icon: in.Icon,
		Color: in.Color, Description: in.Description})
	if err != nil {
		return nil, err
	}
	return map[string]any{"key": p.Key, "slug": p.Slug, "name": p.Name, "icon": p.Icon, "color": p.Color}, nil
}

type boardIn struct {
	Project     string `json:"project,omitempty" jsonschema:"project key (TRK), slug or id; all projects when omitted"`
	Status      string `json:"status,omitempty" jsonschema:"status key such as backlog, todo, doing, done or a custom one"`
	PlannedFor  string `json:"planned_for,omitempty" jsonschema:"only tasks planned for this date, YYYY-MM-DD"`
	Area        string `json:"area,omitempty" jsonschema:"only tasks with this area label"`
	IncludeDone bool   `json:"include_done,omitempty" jsonschema:"include tasks in done-category statuses"`
}

func (t *tools) getBoard(ctx context.Context, in boardIn) (any, error) {
	f := store.TaskFilter{Status: in.Status, PlannedFor: in.PlannedFor, Area: in.Area, IncludeDone: in.IncludeDone}
	if in.Project != "" {
		p, err := t.svc.ResolveProject(ctx, in.Project)
		if err != nil {
			return nil, err
		}
		f.ProjectID = p.ID
	}
	ts, err := t.svc.ListTasks(ctx, f)
	if err != nil {
		return nil, err
	}
	slugs, err := t.slugs(ctx)
	if err != nil {
		return nil, err
	}
	board := map[string][]ctask{}
	for _, x := range ts {
		c := compact(x)
		c.Status = ""
		if f.ProjectID == 0 {
			c.Project = slugs[x.ProjectID]
		}
		board[x.Status] = append(board[x.Status], c)
	}
	return board, nil
}

type todayIn struct {
	Date string `json:"date,omitempty" jsonschema:"YYYY-MM-DD; defaults to today"`
}

func (t *tools) getToday(ctx context.Context, in todayIn) (any, error) {
	r, err := t.svc.TodayView(ctx, in.Date)
	if err != nil {
		return nil, err
	}
	slugs, err := t.slugs(ctx)
	if err != nil {
		return nil, err
	}
	type group struct {
		Project string  `json:"project"`
		Tasks   []ctask `json:"tasks"`
	}
	byProject := map[string][]ctask{}
	for _, x := range r.Tasks {
		c := compact(x)
		c.PlannedFor = ""
		byProject[slugs[x.ProjectID]] = append(byProject[slugs[x.ProjectID]], c)
	}
	groups := []group{}
	for _, p := range r.Projects { // keep sidebar order
		if ts, ok := byProject[p.Slug]; ok {
			groups = append(groups, group{p.Slug, ts})
			delete(byProject, p.Slug)
		}
	}
	rest := make([]string, 0, len(byProject))
	for slug := range byProject { // archived projects
		rest = append(rest, slug)
	}
	sort.Strings(rest)
	for _, slug := range rest {
		groups = append(groups, group{slug, byProject[slug]})
	}
	overdue := []ctask{}
	for _, x := range r.Overdue {
		c := compact(x)
		c.Project = slugs[x.ProjectID]
		overdue = append(overdue, c)
	}
	return map[string]any{"date": r.Date, "projects": groups, "overdue": overdue}, nil
}

type taskIn struct {
	ID string `json:"id" jsonschema:"task ID such as TRK-07"`
}

func (t *tools) getTask(ctx context.Context, in taskIn) (any, error) {
	d, err := t.svc.GetTask(ctx, in.ID, 20)
	if err != nil {
		return nil, err
	}
	slugs, err := t.slugs(ctx)
	if err != nil {
		return nil, err
	}
	fields, err := t.svc.ListFields(ctx, d.ProjectID)
	if err != nil {
		return nil, err
	}
	type sub struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	type note struct {
		Author string `json:"author"`
		At     string `json:"at"`
		Body   string `json:"body"`
	}
	type act struct {
		At      string `json:"at"`
		Actor   string `json:"actor"`
		Action  string `json:"action"`
		Changes string `json:"changes,omitempty"`
	}
	out := struct {
		ctask
		Description string         `json:"description_md,omitempty"`
		Custom      map[string]any `json:"custom,omitempty"`
		CreatedBy   string         `json:"created_by"`
		Created     string         `json:"created"`
		Done        string         `json:"done,omitempty"`
		Trashed     bool           `json:"trashed,omitempty"`
		Subtasks    []sub          `json:"subtasks,omitempty"`
		Notes       []note         `json:"notes,omitempty"`
		History     []act          `json:"history,omitempty"`
	}{ctask: compact(d.Task), Description: d.DescriptionMD, CreatedBy: d.CreatedBy, Created: t.short(d.CreatedAt),
		Trashed: d.DeletedAt != nil}
	out.HasDescription = false
	out.Project = slugs[d.ProjectID]
	for _, f := range fields {
		if v, ok := d.Custom[fmt.Sprint(f.ID)]; ok {
			if out.Custom == nil {
				out.Custom = map[string]any{}
			}
			out.Custom[f.Name] = v
		}
	}
	if d.DoneAt != nil {
		out.Done = t.short(*d.DoneAt)
	}
	for _, s := range d.Subtasks {
		out.Subtasks = append(out.Subtasks, sub{s.Code, s.Title, s.Status})
	}
	for _, n := range d.Notes {
		out.Notes = append(out.Notes, note{n.Author, t.short(n.CreatedAt), n.BodyMD})
	}
	for _, a := range d.Activity {
		out.History = append(out.History, act{t.short(a.At), a.Actor, a.Action, changes(a.DiffJSON)})
	}
	return out, nil
}

// changes renders {"status":["К работе","В работе"]} as "status: К работе → В работе".
func changes(diffJSON string) string {
	var d map[string][2]any
	if json.Unmarshal([]byte(diffJSON), &d) != nil || len(d) == 0 {
		return ""
	}
	keys := make([]string, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "description_md" {
			parts = append(parts, "description edited")
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %v → %v", k, show(d[k][0]), show(d[k][1])))
	}
	return strings.Join(parts, "; ")
}

func show(v any) string {
	if v == nil {
		return "∅"
	}
	s := fmt.Sprint(v)
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

type newTaskIn struct {
	Project       string         `json:"project" jsonschema:"project key (TRK), slug or id"`
	Title         string         `json:"title"`
	DescriptionMD string         `json:"description_md,omitempty" jsonschema:"markdown; links and - [ ] checkboxes are rendered"`
	Status        string         `json:"status,omitempty" jsonschema:"status key: backlog (default), todo, doing, done or a custom one of the project"`
	Priority      string         `json:"priority,omitempty" jsonschema:"P0 (highest), P1 (default) or P2"`
	Area          string         `json:"area,omitempty" jsonschema:"free label such as UI, API, Лендинг; an unknown one is created"`
	PlannedFor    string         `json:"planned_for,omitempty" jsonschema:"YYYY-MM-DD"`
	Custom        map[string]any `json:"custom,omitempty" jsonschema:"custom field values by field name"`
	Parent        string         `json:"parent,omitempty" jsonschema:"existing epic ID such as TRK-07 (same project, one level deep)"`
	Ref           string         `json:"ref,omitempty" jsonschema:"local name of this item so later items can use it as parent_ref"`
	ParentRef     string         `json:"parent_ref,omitempty" jsonschema:"ref of an earlier item in this call that becomes the parent"`
	ExternalID    string         `json:"external_id,omitempty" jsonschema:"stable id for idempotent import; repeating it updates the task"`
}

type createTasksIn struct {
	Tasks []newTaskIn `json:"tasks" jsonschema:"1 to 100 tasks"`
}

func (t *tools) createTasks(ctx context.Context, in createTasksIn) (any, error) {
	items := make([]service.NewTask, len(in.Tasks))
	for i, x := range in.Tasks {
		items[i] = service.NewTask{Project: x.Project, Title: x.Title, DescriptionMD: x.DescriptionMD, Status: x.Status,
			Priority: x.Priority, Area: x.Area, PlannedFor: x.PlannedFor, Custom: x.Custom, Parent: x.Parent,
			Ref: x.Ref, ParentRef: x.ParentRef, ExternalID: x.ExternalID}
	}
	ts, err := t.svc.CreateTasks(ctx, actor, items)
	if err != nil {
		return nil, err
	}
	type created struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	out := make([]created, len(ts))
	for i, x := range ts {
		out[i] = created{x.Code, x.Title}
	}
	return out, nil
}

type updateTaskIn struct {
	ID            string         `json:"id" jsonschema:"task ID such as TRK-07"`
	Title         *string        `json:"title,omitempty"`
	DescriptionMD *string        `json:"description_md,omitempty" jsonschema:"markdown, replaces the whole description"`
	Priority      *string        `json:"priority,omitempty" jsonschema:"P0, P1 or P2"`
	Area          *string        `json:"area,omitempty"`
	PlannedFor    *string        `json:"planned_for,omitempty" jsonschema:"YYYY-MM-DD, or empty string to clear"`
	Custom        map[string]any `json:"custom,omitempty" jsonschema:"custom field values by field name; null clears one"`
	Parent        *string        `json:"parent,omitempty" jsonschema:"epic ID such as TRK-07, or empty string to detach"`
}

func (t *tools) updateTask(ctx context.Context, in updateTaskIn) (any, error) {
	x, err := t.svc.UpdateTask(ctx, actor, in.ID, service.Changes{Title: in.Title, DescriptionMD: in.DescriptionMD,
		Priority: in.Priority, Area: in.Area, PlannedFor: in.PlannedFor, Custom: in.Custom, ParentRef: in.Parent})
	if err != nil {
		return nil, err
	}
	return compact(x), nil
}

type moveTasksIn struct {
	IDs        []string `json:"ids" jsonschema:"task IDs such as TRK-07"`
	Status     *string  `json:"status,omitempty" jsonschema:"status key of the project"`
	PlannedFor *string  `json:"planned_for,omitempty" jsonschema:"YYYY-MM-DD, or empty string to clear"`
	Project    *string  `json:"project,omitempty" jsonschema:"target project key, slug or id; subtasks follow their epic and tasks get new IDs"`
}

func (t *tools) moveTasks(ctx context.Context, in moveTasksIn) (any, error) {
	if in.Status == nil && in.PlannedFor == nil && in.Project == nil {
		return nil, fmt.Errorf("give at least one of status, planned_for, project")
	}
	n, err := t.svc.MoveTasks(ctx, actor, in.IDs, service.BulkMove{Status: in.Status, PlannedFor: in.PlannedFor, Project: in.Project})
	if err != nil {
		return nil, err
	}
	return map[string]int{"updated": n}, nil
}

type addNoteIn struct {
	TaskID string `json:"task_id" jsonschema:"task ID such as TRK-07"`
	BodyMD string `json:"body_md" jsonschema:"markdown"`
}

func (t *tools) addNote(ctx context.Context, in addNoteIn) (any, error) {
	n, err := t.svc.AddNote(ctx, actor, in.TaskID, in.BodyMD)
	if err != nil {
		return nil, err
	}
	return map[string]any{"task": in.TaskID, "at": t.short(n.CreatedAt)}, nil
}

type listPagesIn struct {
	Project string `json:"project" jsonschema:"project key, slug or id"`
}

// pageTitles maps page IDs of a project to titles, to show parents by title.
func (t *tools) pageTitles(ctx context.Context, project string) (map[int64]string, []store.Page, error) {
	pages, err := t.svc.ListPages(ctx, project)
	if err != nil {
		return nil, nil, err
	}
	titles := make(map[int64]string, len(pages))
	for _, p := range pages {
		titles[p.ID] = p.Title
	}
	return titles, pages, nil
}

func parentTitle(titles map[int64]string, p store.Page) string {
	if p.ParentID == nil {
		return ""
	}
	return titles[*p.ParentID]
}

func (t *tools) listPages(ctx context.Context, in listPagesIn) (any, error) {
	titles, pages, err := t.pageTitles(ctx, in.Project)
	if err != nil {
		return nil, err
	}
	type row struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Parent    string `json:"parent"`
		UpdatedAt string `json:"updated_at"`
	}
	out := make([]row, len(pages))
	for i, p := range pages {
		out[i] = row{p.ID, p.Title, parentTitle(titles, p), p.UpdatedAt}
	}
	return out, nil
}

type readPageIn struct {
	Project string `json:"project" jsonschema:"project key, slug or id"`
	Title   string `json:"title" jsonschema:"page title (case-insensitive)"`
}

func (t *tools) readPage(ctx context.Context, in readPageIn) (any, error) {
	d, err := t.svc.GetPageByTitle(ctx, in.Project, in.Title)
	if err != nil {
		return nil, err
	}
	titles, _, err := t.pageTitles(ctx, in.Project)
	if err != nil {
		return nil, err
	}
	backlinks, tasks := []string{}, []string{}
	for _, b := range d.Backlinks {
		backlinks = append(backlinks, b.Title)
	}
	for _, k := range d.Links.Tasks {
		tasks = append(tasks, k.Code)
	}
	return map[string]any{"id": d.ID, "title": d.Title, "parent": parentTitle(titles, d.Page), "body_md": d.BodyMD,
		"updated_at": d.UpdatedAt, "backlinks": backlinks, "tasks": tasks}, nil
}

type writePageIn struct {
	Project           string `json:"project" jsonschema:"project key, slug or id"`
	Title             string `json:"title" jsonschema:"page title; an existing page with this title (any case) is replaced"`
	BodyMD            string `json:"body_md" jsonschema:"markdown, replaces the whole body; link pages as [[Title]]"`
	Parent            string `json:"parent,omitempty" jsonschema:"title of the parent page; empty keeps the current parent (top level for a new page)"`
	ExpectedUpdatedAt string `json:"expected_updated_at,omitempty" jsonschema:"updated_at from read_page; the write fails with conflict if the page changed since"`
}

func (t *tools) writePage(ctx context.Context, in writePageIn) (any, error) {
	p, created, err := t.svc.WritePage(ctx, actor, in.Project, service.WritePageInput{Title: in.Title, BodyMD: in.BodyMD,
		Parent: in.Parent, ExpectedUpdatedAt: in.ExpectedUpdatedAt})
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": p.ID, "title": p.Title, "created": created, "updated_at": p.UpdatedAt}, nil
}

type appendToPageIn struct {
	Project string `json:"project" jsonschema:"project key, slug or id"`
	Title   string `json:"title" jsonschema:"title of an existing page"`
	Text    string `json:"text" jsonschema:"markdown added after a blank line at the end"`
}

func (t *tools) appendToPage(ctx context.Context, in appendToPageIn) (any, error) {
	p, err := t.svc.AppendToPage(ctx, actor, in.Project, in.Title, in.Text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": p.ID, "title": p.Title, "updated_at": p.UpdatedAt}, nil
}

type idsIn struct {
	IDs []string `json:"ids" jsonschema:"task IDs such as TRK-07"`
}

func (t *tools) deleteTasks(ctx context.Context, in idsIn) (any, error) {
	n, err := t.svc.DeleteTasks(ctx, actor, in.IDs)
	if err != nil {
		return nil, err
	}
	return map[string]int{"trashed": n}, nil
}

type searchIn struct {
	Q     string `json:"q" jsonschema:"words to find, or a task ID such as TRK-07"`
	Limit int    `json:"limit,omitempty" jsonschema:"max results, default 20"`
}

func (t *tools) search(ctx context.Context, in searchIn) (any, error) {
	r, err := t.svc.Search(ctx, in.Q, "tasks", in.Limit)
	if err != nil {
		return nil, err
	}
	type hit struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Status  string `json:"status"`
		Snippet string `json:"snippet,omitempty"`
	}
	out := []hit{}
	for _, h := range append(r.Groups.Tasks, r.Groups.Descriptions...) {
		out = append(out, hit{h.Code, h.Title, h.Status, h.Snippet})
	}
	return out, nil
}

type carryIn struct {
	From string `json:"from" jsonschema:"YYYY-MM-DD"`
	To   string `json:"to" jsonschema:"YYYY-MM-DD"`
}

func (t *tools) carryOver(ctx context.Context, in carryIn) (any, error) {
	n, err := t.svc.CarryOver(ctx, actor, in.From, in.To)
	if err != nil {
		return nil, err
	}
	return map[string]int{"moved": n}, nil
}
