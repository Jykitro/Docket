package service

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"docket/internal/events"
	"docket/internal/store"
)

var ctx = context.Background()

func newSvc(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, events.NewBus(), time.UTC)
}

func mustProject(t *testing.T, s *Service, slug string) store.Project {
	t.Helper()
	p, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: slug, Slug: slug})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustTasks(t *testing.T, s *Service, actor string, items ...NewTask) []store.Task {
	t.Helper()
	ts, err := s.CreateTasks(ctx, actor, items)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func id(n int64) string { return strconv.FormatInt(n, 10) }

func columnIDs(t *testing.T, s *Service, projectID int64, status string) []int64 {
	t.Helper()
	ts, err := s.ListTasks(ctx, store.TaskFilter{ProjectID: projectID, Status: status})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, x := range ts {
		ids = append(ids, x.ID)
	}
	return ids
}

func TestFractionalOrderingWithRebalance(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	var items []NewTask
	for _, title := range []string{"a", "b", "c"} {
		items = append(items, NewTask{Project: "p", Title: title, Status: "todo"})
	}
	ts := mustTasks(t, s, ActorDenis, items...)
	a, b, c := ts[0].ID, ts[1].ID, ts[2].ID
	if ts[0].Position != 1024 || ts[2].Position != 3072 {
		t.Fatalf("append positions = %v, %v; want 1024, 3072", ts[0].Position, ts[2].Position)
	}

	// c to the top, then b after c.
	if _, err := s.MoveTask(ctx, ActorDenis, id(c), MoveInput{Status: "todo", BeforeID: a}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveTask(ctx, ActorDenis, id(b), MoveInput{Status: "todo", AfterID: c}); err != nil {
		t.Fatal(err)
	}
	if got := columnIDs(t, s, p.ID, "todo"); !equal(got, []int64{c, b, a}) {
		t.Fatalf("order = %v, want %v", got, []int64{c, b, a})
	}

	// Insert many tasks right after c: the gap halves each time until a rebalance is needed.
	extra := mustTasks(t, s, ActorDenis, NewTask{Project: "p", Title: "x", Status: "todo"})[0].ID
	for i := 0; i < 40; i++ {
		target := b
		if i%2 == 1 {
			target = extra
		}
		if _, err := s.MoveTask(ctx, ActorDenis, id(target), MoveInput{Status: "todo", AfterID: c}); err != nil {
			t.Fatal(err)
		}
	}
	got := columnIDs(t, s, p.ID, "todo")
	if !equal(got, []int64{c, extra, b, a}) {
		t.Fatalf("order after many moves = %v, want %v", got, []int64{c, extra, b, a})
	}
	all, _ := s.ListTasks(ctx, store.TaskFilter{ProjectID: p.ID, Status: "todo"})
	for i := 1; i < len(all); i++ {
		if all[i].Position-all[i-1].Position < 1e-6 {
			t.Fatalf("positions not rebalanced: %v then %v", all[i-1].Position, all[i].Position)
		}
	}

	// Moving into another column with a neighbour.
	if _, err := s.MoveTask(ctx, ActorDenis, id(a), MoveInput{Status: "doing"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MoveTask(ctx, ActorDenis, id(c), MoveInput{Status: "doing", BeforeID: a}); err != nil {
		t.Fatal(err)
	}
	if got := columnIDs(t, s, p.ID, "doing"); !equal(got, []int64{c, a}) {
		t.Fatalf("doing = %v, want %v", got, []int64{c, a})
	}
	if _, err := s.MoveTask(ctx, ActorDenis, id(b), MoveInput{Status: "todo", BeforeID: a}); err == nil {
		t.Fatal("before_id from another column must fail")
	}
}

func TestDoneAtFollowsStatusCategory(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	x := mustTasks(t, s, ActorDenis, NewTask{Project: "p", Title: "t", Status: "todo"})[0]
	if x.DoneAt != nil {
		t.Fatal("new task has done_at")
	}
	x, _ = s.MoveTask(ctx, ActorDenis, id(x.ID), MoveInput{Status: "done"})
	if x.DoneAt == nil {
		t.Fatal("done task has no done_at")
	}
	x, _ = s.MoveTask(ctx, ActorDenis, id(x.ID), MoveInput{Status: "doing"})
	if x.DoneAt != nil {
		t.Fatal("leaving done must clear done_at")
	}
	y := mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "born done", Status: "done"})[0]
	if y.DoneAt == nil {
		t.Fatal("task created as done has no done_at")
	}

	// A custom status of the done category stamps done_at too; changing its category re-stamps its tasks.
	shipped, err := s.CreateStatus(ctx, ActorDenis, p.ID, StatusInput{Name: "Shipped", Category: "done", Color: "dn"})
	if err != nil {
		t.Fatal(err)
	}
	z := mustTasks(t, s, ActorDenis, NewTask{Project: "p", Title: "z", Status: shipped.Key})[0]
	if z.DoneAt == nil || z.StatusCategory != "done" {
		t.Fatalf("custom done status: done_at=%v category=%s", z.DoneAt, z.StatusCategory)
	}
	if _, err := s.UpdateStatus(ctx, ActorDenis, shipped.ID, StatusPatch{Category: ptr("doing")}); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetTask(ctx, id(z.ID), 1); d.DoneAt != nil {
		t.Fatal("changing the status category away from done must clear done_at")
	}
}

func TestStatusDeleteNeedsTransfer(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	x := mustTasks(t, s, ActorDenis, NewTask{Project: "p", Title: "t", Status: "doing"})[0]
	sts, _ := s.ListStatuses(ctx, p.ID)
	var doingID, todoID int64
	for _, st := range sts {
		switch st.Key {
		case "doing":
			doingID = st.ID
		case "todo":
			todoID = st.ID
		}
	}

	err := s.DeleteStatus(ctx, ActorDenis, doingID, 0)
	var se *Error
	if !asErr(err, &se) || se.Code != "status_in_use" {
		t.Fatalf("deleting a status with tasks: %v, want status_in_use", err)
	}
	if err := s.DeleteStatus(ctx, ActorDenis, doingID, doingID); err == nil {
		t.Fatal("moving tasks into the status being deleted must fail")
	}
	if err := s.DeleteStatus(ctx, ActorDenis, doingID, todoID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetTask(ctx, id(x.ID), 1); d.Status != "todo" {
		t.Fatalf("task moved to %q, want todo", d.Status)
	}
	// An empty status goes without a transfer, but the last one stays.
	for _, key := range []string{"backlog", "done"} {
		sts, _ = s.ListStatuses(ctx, p.ID)
		for _, x := range sts {
			if x.Key == key {
				if err := s.DeleteStatus(ctx, ActorDenis, x.ID, 0); err != nil {
					t.Fatalf("deleting empty %s: %v", key, err)
				}
			}
		}
	}
	sts, _ = s.ListStatuses(ctx, p.ID)
	if len(sts) != 1 {
		t.Fatalf("%d statuses left, want 1", len(sts))
	}
	if err := s.DeleteStatus(ctx, ActorDenis, sts[0].ID, 0); err == nil {
		t.Fatal("the last status of a project must not be deletable")
	}
}

func TestTaskNumbersAreNeverReused(t *testing.T) {
	s := newSvc(t)
	p, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: "Tracker", Slug: "tracker", Key: "trk"})
	if err != nil || p.Key != "TRK" {
		t.Fatalf("key: %q %v", p.Key, err)
	}
	ts := mustTasks(t, s, ActorClaude,
		NewTask{Project: "TRK", Title: "a"}, NewTask{Project: "TRK", Title: "b"}, NewTask{Project: "TRK", Title: "c"})
	if ts[0].Code != "TRK-01" || ts[2].Code != "TRK-03" {
		t.Fatalf("codes = %s .. %s", ts[0].Code, ts[2].Code)
	}
	if _, err := s.DeleteTasks(ctx, ActorDenis, []string{"trk-03"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.PurgeTrash(ctx); n != 1 {
		t.Fatalf("purged %d", n)
	}
	next := mustTasks(t, s, ActorClaude, NewTask{Project: "tracker", Title: "d"})[0]
	if next.Code != "TRK-04" {
		t.Fatalf("number after delete+purge = %s, want TRK-04", next.Code)
	}

	// Renaming the key recomputes every code at once; the old key stops resolving.
	if _, err := s.UpdateProject(ctx, ActorDenis, p.ID, ProjectPatch{Key: ptr("HT")}); err != nil {
		t.Fatal(err)
	}
	if d, err := s.GetTask(ctx, "HT-04", 1); err != nil || d.Code != "HT-04" {
		t.Fatalf("new key: %v %+v", err, d.Code)
	}
	if _, err := s.GetTask(ctx, "TRK-04", 1); err == nil {
		t.Fatal("old key still resolves")
	}
	if _, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: "Other", Key: "HT"}); err == nil {
		t.Fatal("duplicate key accepted")
	}
	if _, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: "Other", Key: "1X"}); err == nil {
		t.Fatal("key starting with a digit accepted")
	}
}

func TestSoftDeleteHidden(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	ts := mustTasks(t, s, ActorClaude,
		NewTask{Project: "p", Title: "epic", Ref: "e", PlannedFor: "2026-11-04", Status: "todo"},
		NewTask{Project: "p", Title: "sub", ParentRef: "e", PlannedFor: "2026-11-04", Status: "todo"},
		NewTask{Project: "p", Title: "keep", PlannedFor: "2026-11-04", Status: "todo"})
	n, err := s.DeleteTasks(ctx, ActorClaude, []string{id(ts[0].ID)})
	if err != nil || n != 2 {
		t.Fatalf("trashed %d, %v; want 2 (epic + subtask)", n, err)
	}
	live, _ := s.ListTasks(ctx, store.TaskFilter{ProjectID: p.ID, IncludeDone: true})
	today, _ := s.TodayView(ctx, "2026-11-04")
	hits, _ := s.Search(ctx, "epic", "all", 10)
	if len(live) != 1 || len(today.Tasks) != 1 || hits.Counts["tasks"] != 0 {
		t.Fatalf("trashed tasks leak: list=%d today=%d search=%v", len(live), len(today.Tasks), hits.Counts)
	}
	if ps, _ := s.ListProjects(ctx, false); ps[0].Counts["todo"] != 1 || ps[0].Open != 1 {
		t.Fatalf("counts include trash: %v open=%d", ps[0].Counts, ps[0].Open)
	}
	trash, _ := s.Trash(ctx)
	if len(trash) != 2 {
		t.Fatalf("trash has %d, want 2", len(trash))
	}
	if _, err := s.UpdateTask(ctx, ActorDenis, id(ts[0].ID), Changes{Title: ptr("x")}); err == nil {
		t.Fatal("editing a trashed task must fail")
	}

	if _, err := s.RestoreTask(ctx, ActorDenis, id(ts[0].ID)); err != nil {
		t.Fatal(err)
	}
	if live, _ := s.ListTasks(ctx, store.TaskFilter{ProjectID: p.ID}); len(live) != 3 {
		t.Fatalf("restore brought back %d tasks, want 3", len(live))
	}
	s.DeleteTasks(ctx, ActorDenis, []string{id(ts[2].ID)})
	if n, err := s.PurgeTrash(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d, %v", n, err)
	}
}

func TestCreateTasksParentRefOneTransaction(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	ts := mustTasks(t, s, ActorClaude,
		NewTask{Project: "p", Title: "epic", Ref: "e"},
		NewTask{Project: "p", Title: "s1", ParentRef: "e"},
		NewTask{Project: "p", Title: "s2", ParentRef: "e"})
	if ts[1].ParentID == nil || *ts[1].ParentID != ts[0].ID || *ts[2].ParentID != ts[0].ID {
		t.Fatal("subtasks not linked to epic")
	}
	if ts[1].ParentCode != ts[0].Code {
		t.Fatalf("parent_code = %q, want %q", ts[1].ParentCode, ts[0].Code)
	}
	d, _ := s.GetTask(ctx, ts[0].Code, 10)
	if len(d.Subtasks) != 2 || d.SubtaskCount != 2 {
		t.Fatalf("epic has %d subtasks", len(d.Subtasks))
	}

	// A bad item rolls back the whole batch.
	_, err := s.CreateTasks(ctx, ActorClaude, []NewTask{
		{Project: "p", Title: "ok", Ref: "e2"},
		{Project: "p", Title: "bad", ParentRef: "missing"},
	})
	if err == nil {
		t.Fatal("unknown parent_ref must fail")
	}
	if all, _ := s.ListTasks(ctx, store.TaskFilter{ProjectID: p.ID}); len(all) != 3 {
		t.Fatalf("partial batch committed: %d tasks", len(all))
	}

	// Nesting, status and project rules.
	mustProject(t, s, "q")
	for name, it := range map[string]NewTask{
		"two levels":     {Project: "p", Title: "deep", ParentID: ts[1].ID},
		"other project":  {Project: "q", Title: "x", ParentID: ts[0].ID},
		"empty title":    {Project: "p", Title: "  "},
		"bad date":       {Project: "p", Title: "x", PlannedFor: "04.11.2026"},
		"bad project":    {Project: "nope", Title: "x"},
		"unknown status": {Project: "p", Title: "x", Status: "waiting"},
		"unknown field":  {Project: "p", Title: "x", Custom: map[string]any{"Budget": 5}},
	} {
		if _, err := s.CreateTasks(ctx, ActorClaude, []NewTask{it}); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestExternalIDIdempotent(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	first := mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "v1", ExternalID: "ht-01", Priority: "P0"})[0]
	again := mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "v2", ExternalID: "ht-01", Priority: "P0"})[0]
	if again.ID != first.ID || again.Title != "v2" || again.Code != first.Code {
		t.Fatalf("external_id created a duplicate or did not update: %+v", again)
	}
	if all, _ := s.ListTasks(ctx, store.TaskFilter{ProjectID: p.ID}); len(all) != 1 {
		t.Fatalf("%d tasks, want 1", len(all))
	}
}

func TestSeedIdempotent(t *testing.T) {
	s := newSvc(t)
	path := filepath.Join(t.TempDir(), "seed.json")
	os.WriteFile(path, []byte(`{"projects":[{"slug":"tracker","name":"Demo Tracker","key":"TRK","icon":"target","color":"#2D5CC9","description":"d"}],
		"tasks":[{"external_id":"ht-01","project":"tracker","title":"One","area":"UI","priority":"P0",
		"status":"todo","planned_for":"2026-11-05","description_md":""},
		{"external_id":"ht-02","project":"tracker","title":"Two","area":"Страницы","priority":"P1","status":"backlog"}]}`), 0o644)
	if ok, err := s.Seed(ctx, path); !ok || err != nil {
		t.Fatalf("seed: %v %v", ok, err)
	}
	if ok, err := s.Seed(ctx, path); ok || err != nil {
		t.Fatalf("second seed ran: %v %v", ok, err)
	}
	ts, _ := s.ListTasks(ctx, store.TaskFilter{})
	if len(ts) != 2 || ts[0].CreatedBy != ActorClaude {
		t.Fatalf("seed result: %+v", ts)
	}
	codes := map[string]bool{ts[0].Code: true, ts[1].Code: true}
	if !codes["TRK-01"] || !codes["TRK-02"] {
		t.Fatalf("codes = %v, want TRK-01 and TRK-02 in file order", codes)
	}
	p, _ := s.ResolveProject(ctx, "TRK")
	if ls, _ := s.ListLabels(ctx, p.ID); len(ls) != 2 {
		t.Fatalf("labels from areas = %+v", ls)
	}
}

func TestCarryOver(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "p")
	ts := mustTasks(t, s, ActorDenis,
		NewTask{Project: "p", Title: "open", PlannedFor: "2026-11-04", Status: "todo"},
		NewTask{Project: "p", Title: "done", PlannedFor: "2026-11-04", Status: "done"},
		NewTask{Project: "p", Title: "other day", PlannedFor: "2026-11-03", Status: "todo"})
	n, err := s.CarryOver(ctx, ActorDenis, "2026-11-04", "2026-11-05")
	if err != nil || n != 1 {
		t.Fatalf("moved %d, %v; want 1", n, err)
	}
	next, _ := s.TodayView(ctx, "2026-11-05")
	if len(next.Tasks) != 1 || next.Tasks[0].ID != ts[0].ID {
		t.Fatalf("2026-11-05 has %+v", next.Tasks)
	}
	if len(next.Overdue) != 1 || next.Overdue[0].ID != ts[2].ID {
		t.Fatalf("overdue = %+v, want the open task from 11-03", next.Overdue)
	}
	d, _ := s.GetTask(ctx, ts[0].Code, 1)
	if d.Activity[0].Action != "carried_over" {
		t.Fatalf("last activity = %q", d.Activity[0].Action)
	}
}

func TestActivityActor(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "p")
	x := mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "t"})[0]
	s.UpdateTask(ctx, ActorDenis, x.Code, Changes{Title: ptr("t2"), Area: ptr("UI")})
	s.MoveTask(ctx, ActorDenis, x.Code, MoveInput{Status: "doing"})
	s.AddNote(ctx, ActorClaude, x.Code, "hi")
	s.DeleteTasks(ctx, ActorClaude, []string{x.Code})
	s.RestoreTask(ctx, ActorDenis, x.Code)

	d, err := s.GetTask(ctx, x.Code, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ action, actor string }{
		{"restored", "denis"}, {"deleted", "claude"}, {"note_added", "claude"},
		{"moved", "denis"}, {"updated", "denis"}, {"created", "claude"},
	}
	if len(d.Activity) != len(want) {
		t.Fatalf("activity = %+v", d.Activity)
	}
	for i, w := range want {
		if a := d.Activity[i]; a.Action != w.action || a.Actor != w.actor {
			t.Errorf("activity[%d] = %s/%s, want %s/%s", i, a.Action, a.Actor, w.action, w.actor)
		}
	}
	if d.Activity[4].DiffJSON != `{"area":["","UI"],"title":["t","t2"]}` {
		t.Errorf("update diff = %s", d.Activity[4].DiffJSON)
	}
	if d.Activity[3].DiffJSON != `{"status":["Бэклог","В работе"]}` {
		t.Errorf("move diff = %s, want status names", d.Activity[3].DiffJSON)
	}
	if d.Notes[0].Author != "claude" {
		t.Errorf("note author = %s", d.Notes[0].Author)
	}
}

func TestEventsPublishedAfterCommit(t *testing.T) {
	s := newSvc(t)
	ch, cancel := s.bus.Subscribe()
	defer cancel()
	mustProject(t, s, "p")
	if _, err := s.CreateTasks(ctx, ActorClaude, []NewTask{{Project: "p", Title: ""}}); err == nil {
		t.Fatal("empty title accepted")
	}
	mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "ok"})
	var got []string
	for len(ch) > 0 {
		e := <-ch
		got = append(got, e.Type+"/"+e.Actor)
	}
	if !equal(got, []string{"project_changed/denis", "created/claude"}) {
		t.Fatalf("events = %v", got)
	}
}

func TestLabelsFromAreas(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	x := mustTasks(t, s, ActorClaude,
		NewTask{Project: "p", Title: "a", Area: "UI"},
		NewTask{Project: "p", Title: "b", Area: "Платежи"},
		NewTask{Project: "p", Title: "c", Area: "Платежи"})
	ls, _ := s.ListLabels(ctx, p.ID)
	if len(ls) != 2 || ls[0].Color != "#8B6CFF" || ls[1].Name != "Платежи" || ls[1].TaskCount != 2 {
		t.Fatalf("labels = %+v", ls)
	}
	if ls[1].Color != labelColors[1] {
		t.Fatalf("second label colour = %s, want the next slot %s", ls[1].Color, labelColors[1])
	}
	// Renaming a label renames the area of its tasks; deleting clears it.
	if _, err := s.UpdateLabel(ctx, ActorDenis, ls[1].ID, LabelPatch{Name: ptr("Billing")}); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetTask(ctx, x[1].Code, 1); d.Area != "Billing" {
		t.Fatalf("area after rename = %q", d.Area)
	}
	if err := s.DeleteLabel(ctx, ActorDenis, ls[1].ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetTask(ctx, x[2].Code, 1); d.Area != "" {
		t.Fatalf("area after delete = %q", d.Area)
	}
}

func TestCustomFields(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "p")
	budget, err := s.CreateField(ctx, ActorDenis, p.ID, FieldInput{Name: "Budget", Type: "number", ShowOnCard: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateField(ctx, ActorDenis, p.ID, FieldInput{Name: "Stage", Type: "select"}); err == nil {
		t.Fatal("select field without options accepted")
	}
	if _, err := s.CreateField(ctx, ActorDenis, p.ID, FieldInput{Name: "Stage", Type: "select", Options: []string{"alpha", "beta"}}); err != nil {
		t.Fatal(err)
	}
	x := mustTasks(t, s, ActorClaude, NewTask{Project: "p", Title: "t", Custom: map[string]any{"Budget": 1500.0, "Stage": "beta"}})[0]
	if x.Custom[id(budget.ID)] != 1500.0 {
		t.Fatalf("custom = %v", x.Custom)
	}
	if _, err := s.UpdateTask(ctx, ActorDenis, x.Code, Changes{Custom: map[string]any{"Stage": "gamma"}}); err == nil {
		t.Fatal("value outside select options accepted")
	}
	if _, err := s.UpdateTask(ctx, ActorDenis, x.Code, Changes{Custom: map[string]any{"Budget": "abc"}}); err == nil {
		t.Fatal("non-numeric value accepted for a number field")
	}
	if err := s.DeleteField(ctx, ActorDenis, budget.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetTask(ctx, x.Code, 1); len(d.Custom) != 1 {
		t.Fatalf("deleting a field must drop its values: %v", d.Custom)
	}
}

func TestSearchGroups(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "p")
	ts := mustTasks(t, s, ActorClaude,
		NewTask{Project: "p", Title: "Платёжный экран"},
		NewTask{Project: "p", Title: "Другое", DescriptionMD: "нужно проверить платёжный шлюз"})
	r, err := s.Search(ctx, "платёж", "all", 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Counts["tasks"] != 1 || r.Counts["descriptions"] != 1 || !strings.Contains(r.Groups.Descriptions[0].Snippet, "[") {
		t.Fatalf("search = %+v", r)
	}
	if byCode, _ := s.Search(ctx, ts[1].Code, "tasks", 10); byCode.Counts["tasks"] != 1 || byCode.Groups.Tasks[0].ID != ts[1].ID {
		t.Fatalf("lookup by code = %+v", byCode)
	}
	if only, _ := s.Search(ctx, "p", "projects", 10); only.Counts["projects"] != 1 || only.Counts["tasks"] != 0 {
		t.Fatalf("projects scope = %+v", only)
	}
}

func TestMCPSettingsAndCallLog(t *testing.T) {
	s := newSvc(t)
	tok, err := s.EnsureMCPSettings(ctx)
	if err != nil || !strings.HasPrefix(tok, "dkt_") || len(tok) != 36 {
		t.Fatalf("generated token %q, %v", tok, err)
	}
	if again, _ := s.EnsureMCPSettings(ctx); again != "" {
		t.Fatal("a second start generated another token")
	}
	pol, _ := s.MCPPolicy(ctx)
	if pol.Mode != ModeReadWrite || !pol.Disabled["delete_tasks"] || len(pol.Disabled) != 1 {
		t.Fatalf("default policy = %+v", pol)
	}
	st, err := s.UpdateMCPSettings(ctx, MCPSettingsPatch{AccessMode: ptr("ro"), Tools: map[string]bool{"delete_tasks": true, "search": false}})
	if err != nil || st.AccessMode != "ro" {
		t.Fatalf("update settings: %v %+v", err, st)
	}
	if pol, _ = s.MCPPolicy(ctx); pol.Disabled["delete_tasks"] || !pol.Disabled["search"] {
		t.Fatalf("policy after update = %+v", pol)
	}
	if _, err := s.UpdateMCPSettings(ctx, MCPSettingsPatch{Tools: map[string]bool{"nope": true}}); err == nil {
		t.Fatal("unknown tool accepted")
	}

	// Rotation replaces the token, but not while DOCKET_MCP_TOKEN pins it.
	rot, err := s.RotateMCPToken(ctx)
	if got, _ := s.MCPToken(ctx); err != nil || got != rot.Token || got == tok {
		t.Fatalf("rotate: %v token=%q old=%q", err, got, tok)
	}
	s.SetEnvToken("from-env")
	if got, _ := s.MCPToken(ctx); got != "from-env" {
		t.Fatalf("env token ignored: %q", got)
	}
	if _, err := s.RotateMCPToken(ctx); err == nil {
		t.Fatal("rotation allowed while the token comes from the environment")
	}

	// The log keeps 7 days and shortens long arguments.
	if err := s.RecordMCPCall(ctx, "get_board", strings.Repeat("x", 300), 12*time.Millisecond, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMCPCall(ctx, "get_task", `{"id":"TRK-99"}`, 3*time.Millisecond, "not_found"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-8 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	if err := s.st.WithTx(ctx, func(tx *store.Tx) error {
		return tx.InsertCall(store.MCPCall{At: old, Tool: "search", ArgsSummary: "{}", DurationMS: 1, Status: "ok", Actor: "claude"})
	}); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.ListMCPCalls(ctx, "all", 50); len(all) != 3 {
		t.Fatalf("log has %d lines, want 3", len(all))
	}
	if n, err := s.PurgeOldMCPCalls(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d, %v; want the 8-day-old line only", n, err)
	}
	all, _ := s.ListMCPCalls(ctx, "all", 50)
	if len(all) != 2 || len([]rune(all[1].ArgsSummary)) > 120 {
		t.Fatalf("after purge: %+v", all)
	}
	if errs, _ := s.ListMCPCalls(ctx, "error", 50); len(errs) != 1 || *errs[0].ErrorCode != "not_found" {
		t.Fatalf("error filter = %+v", errs)
	}
	if w, _ := s.ListMCPCalls(ctx, "write", 50); len(w) != 0 {
		t.Fatalf("write filter = %+v, only reads were made", w)
	}
	stats, _ := s.MCPStats(ctx)
	if stats.CallsToday != 2 || stats.TokenLastUsedAt == "" {
		t.Fatalf("stats = %+v", stats)
	}
}

func ptr[T any](v T) *T { return &v }

func asErr(err error, target **Error) bool {
	e, ok := err.(*Error)
	if ok {
		*target = e
	}
	return ok
}

func equal[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestProjectTrash(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "doomed")
	x := mustTasks(t, s, ActorClaude, NewTask{Project: "doomed", Title: "inside", PlannedFor: "2026-11-05", DescriptionMD: "needle"})[0]
	keep := mustProject(t, s, "keep")

	if _, err := s.DeleteProject(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	ps, _ := s.ListProjects(ctx, true)
	today, _ := s.TodayView(ctx, "2026-11-05")
	hits, _ := s.Search(ctx, "needle", "all", 10)
	if len(ps) != 1 || ps[0].ID != keep.ID || len(today.Tasks) != 0 || hits.Counts["descriptions"] != 0 {
		t.Fatalf("a trashed project leaks: projects=%d today=%d search=%v", len(ps), len(today.Tasks), hits.Counts)
	}
	if _, err := s.CreateTasks(ctx, ActorClaude, []NewTask{{Project: "doomed", Title: "x"}}); err == nil {
		t.Fatal("a task was created in a trashed project")
	}
	if gone, _ := s.ListDeletedProjects(ctx); len(gone) != 1 || gone[0].ID != p.ID {
		t.Fatalf("trash projects = %+v", gone)
	}

	if _, err := s.RestoreProject(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	if ps, _ := s.ListProjects(ctx, true); len(ps) != 2 {
		t.Fatalf("restore: %d projects", len(ps))
	}
	if d, err := s.GetTask(ctx, x.Code, 1); err != nil || d.Title != "inside" {
		t.Fatalf("task after restore: %v", err)
	}

	// Emptying the trash removes the project for good and frees its slug and key.
	s.DeleteProject(ctx, ActorDenis, p.ID)
	if n, err := s.PurgeTrash(ctx); err != nil || n != 1 {
		t.Fatalf("purged %d tasks, %v; want 1", n, err)
	}
	if _, err := s.GetProject(ctx, p.ID); err == nil {
		t.Fatal("purged project still exists")
	}
	if _, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: "doomed", Slug: "doomed", Key: p.Key}); err != nil {
		t.Fatalf("slug and key not freed: %v", err)
	}
}

func TestRussianNamesGetLatinKeys(t *testing.T) {
	s := newSvc(t)
	p, err := s.CreateProject(ctx, ActorDenis, ProjectInput{Name: "Учебный терминал"})
	if err != nil || p.Slug != "uchebnyy-terminal" || p.Key != "UCH" {
		t.Fatalf("project from a Russian name: slug=%q key=%q %v", p.Slug, p.Key, err)
	}
	st, err := s.CreateStatus(ctx, ActorDenis, p.ID, StatusInput{Name: "Ревью"})
	if err != nil || st.Key != "revyu" || st.Name != "Ревью" {
		t.Fatalf("status from a Russian name: %+v %v", st, err)
	}
	if st2, _ := s.CreateStatus(ctx, ActorDenis, p.ID, StatusInput{Name: "Ревью"}); st2.Key != "revyu-2" {
		t.Fatalf("duplicate status key = %q, want revyu-2", st2.Key)
	}
}
