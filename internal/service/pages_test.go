package service

import (
	"strings"
	"testing"
	"time"
)

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	e, ok := err.(*Error)
	if !ok || e.Code != code {
		t.Fatalf("want %s error, got %v", code, err)
	}
}

func TestPageTitleUniqueIgnoringCase(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	if _, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Настройки"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "настройки"})
	wantCode(t, err, "validation")
}

func TestPageTitleRejectsBrackets(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	for _, title := range []string{"a]]b", "a[b", "a\nb", "", strings.Repeat("я", 121)} {
		_, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: title})
		wantCode(t, err, "validation")
	}
	if _, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: strings.Repeat("я", 120)}); err != nil {
		t.Fatal(err)
	}
}

func TestPageBodyTooLarge(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	_, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "x", BodyMD: strings.Repeat("a", 204801)})
	wantCode(t, err, "validation")
	if _, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "x", BodyMD: strings.Repeat("a", 204800)}); err != nil {
		t.Fatal(err)
	}
}

func TestPageParentRules(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	mustProject(t, s, "b")
	other, err := s.CreatePage(ctx, ActorDenis, "b", PageInput{Title: "other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "x", ParentID: &other.ID})
	wantCode(t, err, "validation")

	a, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "A"})
	b, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "B", ParentID: &a.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdatePage(ctx, ActorDenis, a.ID, PagePatch{ParentID: &b.ID})
	wantCode(t, err, "validation")
	_, err = s.UpdatePage(ctx, ActorDenis, a.ID, PagePatch{ParentID: &a.ID})
	wantCode(t, err, "validation")

	zero := int64(0)
	got, err := s.UpdatePage(ctx, ActorDenis, b.ID, PagePatch{ParentID: &zero})
	if err != nil || got.ParentID != nil {
		t.Fatalf("detach: %v %+v", err, got)
	}
}

func TestUpdatePageConflict(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	p, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "A"})
	_, err := s.UpdatePage(ctx, ActorDenis, p.ID, PagePatch{BodyMD: ptr("x"), ExpectedUpdatedAt: "2000-01-01T00:00:00.000Z"})
	wantCode(t, err, "conflict")
	time.Sleep(2 * time.Millisecond)
	got, err := s.UpdatePage(ctx, ActorDenis, p.ID, PagePatch{BodyMD: ptr("x"), ExpectedUpdatedAt: p.UpdatedAt})
	if err != nil || got.UpdatedAt == p.UpdatedAt || got.BodyMD != "x" {
		t.Fatalf("update: %v %+v", err, got)
	}
}

func TestListPagesBlanksBodiesAndDelete(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	p, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "A", BodyMD: "text"})
	l, err := s.ListPages(ctx, "a")
	if err != nil || len(l) != 1 || l[0].BodyMD != "" {
		t.Fatalf("list: %v %+v", err, l)
	}
	if err := s.DeletePage(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListPages(ctx, "a"); len(l) != 0 {
		t.Fatalf("not deleted: %+v", l)
	}
}

func TestPurgeProjectWithPages(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "a")
	a, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "A"})
	if _, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "B", ParentID: &a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteProject(ctx, ActorDenis, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurgeTrash(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestParseLinks(t *testing.T) {
	for _, c := range []struct {
		body          string
		titles, codes []string
	}{
		{"[[Настройки]] и [[настройки]]", []string{"настройки"}, nil},
		{"TRK-07 TRK-070", nil, []string{"TRK-07", "TRK-070"}},
		{"xTRK-07", nil, nil},
		{"[[]] and [[a\nb]]", nil, nil},
	} {
		titles, codes := parseLinks(c.body)
		if strings.Join(titles, "|") != strings.Join(c.titles, "|") || strings.Join(codes, "|") != strings.Join(c.codes, "|") {
			t.Errorf("%q: got %q %q, want %q %q", c.body, titles, codes, c.titles, c.codes)
		}
	}
}

func TestBacklinksCaseInsensitive(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	a, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Настройки", BodyMD: "see [[Настройки]]"})
	b, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "B", BodyMD: "go to [[настройки]]"})
	d, err := s.GetPage(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Backlinks) != 1 || d.Backlinks[0].ID != b.ID || d.Backlinks[0].Title != "B" {
		t.Fatalf("backlinks: %+v", d.Backlinks)
	}
	if len(d.Links.Pages) != 1 || d.Links.Pages[0].ID != a.ID {
		t.Fatalf("links: %+v", d.Links)
	}
	_, err = s.GetPage(ctx, 9999)
	wantCode(t, err, "not_found")
	byTitle, err := s.GetPageByTitle(ctx, "a", "НАСТРОЙКИ")
	if err != nil || byTitle.ID != a.ID {
		t.Fatalf("by title: %v %+v", err, byTitle)
	}
	_, err = s.GetPageByTitle(ctx, "a", "нет такой")
	wantCode(t, err, "not_found")
}

func TestTaskCodeBoundary(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	task := mustTasks(t, s, ActorDenis, NewTask{Project: "a", Title: "t"})[0]
	pg, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "P", BodyMD: "only " + task.Code + "0 here"})
	got, err := s.TaskPages(ctx, task.Code)
	if err != nil || len(got) != 0 {
		t.Fatalf("want none: %v %+v", err, got)
	}
	if _, err := s.UpdatePage(ctx, ActorDenis, pg.ID, PagePatch{BodyMD: ptr("see " + task.Code + ".")}); err != nil {
		t.Fatal(err)
	}
	got, err = s.TaskPages(ctx, task.Code)
	if err != nil || len(got) != 1 || got[0].ID != pg.ID || got[0].Title != "P" || got[0].Project != "a" {
		t.Fatalf("want P: %v %+v", err, got)
	}
}

func TestLinksResolveOnlyExisting(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	ts := mustTasks(t, s, ActorDenis, NewTask{Project: "a", Title: "alive"}, NewTask{Project: "a", Title: "gone"})
	if _, err := s.DeleteTasks(ctx, ActorDenis, []string{ts[1].Code}); err != nil {
		t.Fatal(err)
	}
	pg, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "P", BodyMD: "[[Nope]] " + ts[0].Code + " " + ts[1].Code + " ZZZ-99"})
	d, err := s.GetPage(ctx, pg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Links.Pages) != 0 {
		t.Fatalf("pages: %+v", d.Links.Pages)
	}
	if len(d.Links.Tasks) != 1 || d.Links.Tasks[0].Code != ts[0].Code || d.Links.Tasks[0].Title != "alive" {
		t.Fatalf("tasks: %+v", d.Links.Tasks)
	}
}

func TestRenameRewritesLinks(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	mustProject(t, s, "b")
	a, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Старое", BodyMD: "self [[Старое]]"})
	b, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "B", BodyMD: "x [[старое]] y [[СТАРОЕ]] z [[Другое]]"})
	other, _ := s.CreatePage(ctx, ActorDenis, "b", PageInput{Title: "O", BodyMD: "[[Старое]]"})
	time.Sleep(2 * time.Millisecond)
	if _, err := s.UpdatePage(ctx, ActorDenis, a.ID, PagePatch{Title: ptr("Новое")}); err != nil {
		t.Fatal(err)
	}
	gb, _ := s.GetPage(ctx, b.ID)
	if gb.BodyMD != "x [[Новое]] y [[Новое]] z [[Другое]]" || gb.UpdatedAt == b.UpdatedAt {
		t.Fatalf("sibling: %+v", gb.Page)
	}
	if ga, _ := s.GetPage(ctx, a.ID); ga.BodyMD != "self [[Новое]]" {
		t.Fatalf("renamed page's own links must follow the rename: %q", ga.BodyMD)
	}
	if go2, _ := s.GetPage(ctx, other.ID); go2.BodyMD != "[[Старое]]" {
		t.Fatalf("other project touched: %q", go2.BodyMD)
	}
}

func TestRenameRewritesOwnLinksInPatchedBody(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	a, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Старое", BodyMD: "old"})
	got, err := s.UpdatePage(ctx, ActorDenis, a.ID, PagePatch{Title: ptr("Новое"), BodyMD: ptr("me [[старое]]")})
	if err != nil || got.BodyMD != "me [[Новое]]" {
		t.Fatalf("%v %q", err, got.BodyMD)
	}
}

func TestRenameUniqueness(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	a, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Alpha"})
	if _, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Бета"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdatePage(ctx, ActorDenis, a.ID, PagePatch{Title: ptr("бЕТА")})
	wantCode(t, err, "validation")
	if got.ID != 0 {
		t.Fatalf("error must return zero page: %+v", got)
	}
	if got, err = s.UpdatePage(ctx, ActorDenis, a.ID, PagePatch{Title: ptr("ALPHA")}); err != nil || got.Title != "ALPHA" {
		t.Fatalf("case variant of own title: %v %+v", err, got)
	}
}

func TestDeletePageReparentsChildren(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	root, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "root"})
	mid, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "mid", ParentID: &root.ID})
	kid, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "kid", ParentID: &mid.ID})
	if err := s.DeletePage(ctx, ActorDenis, mid.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetPage(ctx, kid.ID); d.ParentID == nil || *d.ParentID != root.ID {
		t.Fatalf("kid parent: %v", d.ParentID)
	}
	if err := s.DeletePage(ctx, ActorDenis, root.ID); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.GetPage(ctx, kid.ID); d.ParentID != nil {
		t.Fatalf("kid must be top level, got %d", *d.ParentID)
	}
	wantCode(t, s.DeletePage(ctx, ActorDenis, 9999), "not_found")
}

func TestWritePageUpserts(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	first, created, err := s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Архитектура", BodyMD: "v1"})
	if err != nil || !created || first.BodyMD != "v1" || first.ParentID != nil {
		t.Fatalf("create: %v %v %+v", err, created, first)
	}
	time.Sleep(2 * time.Millisecond)
	second, created, err := s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "архитектура", BodyMD: "v2"})
	if err != nil || created || second.ID != first.ID || second.BodyMD != "v2" || second.UpdatedAt == first.UpdatedAt {
		t.Fatalf("replace: %v %v %+v", err, created, second)
	}
	if l, _ := s.ListPages(ctx, "a"); len(l) != 1 {
		t.Fatalf("want one page, got %+v", l)
	}

	// parent by title (case-insensitive): on create, kept on update when Parent is empty, moved when set
	kid, created, err := s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Kid", Parent: "АРХИТЕКТУРА"})
	if err != nil || !created || kid.ParentID == nil || *kid.ParentID != first.ID {
		t.Fatalf("parent on create: %v %v %+v", err, created, kid)
	}
	kid, _, err = s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Kid", BodyMD: "x"})
	if err != nil || kid.ParentID == nil || *kid.ParentID != first.ID {
		t.Fatalf("empty parent must keep it: %v %+v", err, kid)
	}
	other, _, _ := s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Other"})
	kid, _, err = s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Kid", Parent: "other"})
	if err != nil || kid.ParentID == nil || *kid.ParentID != other.ID {
		t.Fatalf("move: %v %+v", err, kid)
	}

	_, _, err = s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Kid", Parent: "нет такой"})
	wantCode(t, err, "validation")
	if !strings.Contains(err.Error(), "нет такой") {
		t.Fatalf("message must name the parent: %v", err)
	}
	_, _, err = s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "New", Parent: "нет такой"})
	wantCode(t, err, "validation")
	_, _, err = s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "Other", Parent: "Kid"}) // cycle
	wantCode(t, err, "validation")
	_, _, err = s.WritePage(ctx, ActorDenis, "a", WritePageInput{Title: "a]]b"})
	wantCode(t, err, "validation")
}

func TestWritePageExpectedUpdatedAt(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	p, _, _ := s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "A", BodyMD: "1"})
	_, _, err := s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "A", BodyMD: "2", ExpectedUpdatedAt: "2000-01-01T00:00:00.000Z"})
	wantCode(t, err, "conflict")
	if d, _ := s.GetPage(ctx, p.ID); d.BodyMD != "1" {
		t.Fatalf("stale write must not apply: %q", d.BodyMD)
	}
	time.Sleep(2 * time.Millisecond)
	got, _, err := s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "A", BodyMD: "2", ExpectedUpdatedAt: p.UpdatedAt})
	if err != nil || got.BodyMD != "2" {
		t.Fatalf("fresh write: %v %+v", err, got)
	}
}

func TestWritePageActorClaude(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	if _, created, err := s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "A", BodyMD: "x"}); err != nil || !created {
		t.Fatalf("%v %v", err, created)
	}
	if _, err := s.AppendToPage(ctx, ActorClaude, "a", "A", "y"); err != nil {
		t.Fatal(err)
	}
}

func TestAppendToPage(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "Log", BodyMD: "first\n"})
	got, err := s.AppendToPage(ctx, ActorClaude, "a", "log", "second")
	if err != nil || got.BodyMD != "first\n\nsecond" {
		t.Fatalf("append: %v %q", err, got.BodyMD)
	}
	s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "Empty"})
	if got, err = s.AppendToPage(ctx, ActorClaude, "a", "Empty", "only"); err != nil || got.BodyMD != "only" {
		t.Fatalf("append to empty: %v %q", err, got.BodyMD)
	}
	_, err = s.AppendToPage(ctx, ActorClaude, "a", "Nope", "x")
	wantCode(t, err, "not_found")
	_, err = s.AppendToPage(ctx, ActorClaude, "a", "Log", " \n ")
	wantCode(t, err, "validation")
	_, err = s.AppendToPage(ctx, ActorClaude, "a", "Log", strings.Repeat("a", 204800))
	wantCode(t, err, "validation")
}

func TestIsWriteToolPages(t *testing.T) {
	for name, want := range map[string]bool{"write_page": true, "append_to_page": true, "list_pages": false, "read_page": false, "delete_page": false} {
		if IsWriteTool(name) != want {
			t.Errorf("IsWriteTool(%q) != %v", name, want)
		}
	}
}

func TestCreatePageParentZeroIsTopLevel(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	got, err := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "A", ParentID: ptr(int64(0))})
	if err != nil || got.ParentID != nil {
		t.Fatalf("parent 0: %v %+v", err, got)
	}
}

func TestWritePageMissingWithExpectedIsConflict(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	_, created, err := s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "Gone", BodyMD: "x", ExpectedUpdatedAt: "2000-01-01T00:00:00.000Z"})
	wantCode(t, err, "conflict")
	if created {
		t.Fatal("must not report created")
	}
	if l, _ := s.ListPages(ctx, "a"); len(l) != 0 {
		t.Fatalf("a deleted page must not come back: %+v", l)
	}
}

func TestWritePageKeepsStoredTitleCasing(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "Настройки", BodyMD: "1"})
	got, created, err := s.WritePage(ctx, ActorClaude, "a", WritePageInput{Title: "настройки", BodyMD: "2"})
	if err != nil || created || got.Title != "Настройки" || got.BodyMD != "2" {
		t.Fatalf("%v %v %+v", err, created, got)
	}
}

func TestLinksTaskCodesCanonicalOnly(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	task := mustTasks(t, s, ActorDenis, NewTask{Project: "a", Title: "t"})[0]
	prefix := task.Code[:strings.Index(task.Code, "-")]
	pg, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "P", BodyMD: prefix + "-1 " + prefix + "-001"})
	d, err := s.GetPage(ctx, pg.ID)
	if err != nil || len(d.Links.Tasks) != 0 {
		t.Fatalf("non-canonical codes are not links: %v %+v", err, d.Links.Tasks)
	}
	if got, err := s.TaskPages(ctx, task.Code); err != nil || len(got) != 0 {
		t.Fatalf("task pages: %v %+v", err, got)
	}
}

func TestTaskPagesEdges(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "a")
	pb := mustProject(t, s, "b")
	ts := mustTasks(t, s, ActorDenis, NewTask{Project: "a", Title: "t"}, NewTask{Project: "a", Title: "gone"})
	code := ts[0].Code
	_, err := s.TaskPages(ctx, "ZZ-99")
	wantCode(t, err, "not_found")
	if _, err := s.DeleteTasks(ctx, ActorDenis, []string{ts[1].Code}); err != nil {
		t.Fatal(err)
	}
	_, err = s.TaskPages(ctx, ts[1].Code)
	wantCode(t, err, "not_found")

	s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "PA", BodyMD: code})
	s.CreatePage(ctx, ActorDenis, "b", PageInput{Title: "PB", BodyMD: "also " + code})
	got, err := s.TaskPages(ctx, code)
	if err != nil || len(got) != 2 || got[0].Project != "a" || got[1].Project != "b" {
		t.Fatalf("two projects: %v %+v", err, got)
	}
	if _, err := s.DeleteProject(ctx, ActorDenis, pb.ID); err != nil {
		t.Fatal(err)
	}
	if got, err = s.TaskPages(ctx, code); err != nil || len(got) != 1 || got[0].Title != "PA" {
		t.Fatalf("soft-deleted project must be skipped: %v %+v", err, got)
	}
}

func TestExportIncludesPages(t *testing.T) {
	s := newSvc(t)
	pa := mustProject(t, s, "a")
	mustProject(t, s, "b")
	root, _ := s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Root", BodyMD: "root body"})
	s.CreatePage(ctx, ActorDenis, "a", PageInput{Title: "Kid", BodyMD: "kid body", ParentID: &root.ID})
	s.CreatePage(ctx, ActorDenis, "b", PageInput{Title: "Other", BodyMD: "other body"})
	e, err := s.Export(ctx, 0)
	if err != nil || len(e.Pages) != 3 {
		t.Fatalf("all: %v %+v", err, e.Pages)
	}
	for _, p := range e.Pages {
		if !strings.HasSuffix(p.BodyMD, "body") {
			t.Fatalf("body must be exported: %+v", p)
		}
	}
	e, err = s.Export(ctx, pa.ID)
	if err != nil || len(e.Pages) != 2 {
		t.Fatalf("one project: %v %+v", err, e.Pages)
	}
	if e, _ := s.Export(ctx, mustProject(t, s, "c").ID); e.Pages == nil {
		t.Fatal("pages must be [] not null")
	}
}
