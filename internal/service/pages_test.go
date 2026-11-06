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
	if ga, _ := s.GetPage(ctx, a.ID); ga.BodyMD != "self [[Старое]]" {
		t.Fatalf("renamed page itself must stay untouched: %q", ga.BodyMD)
	}
	if go2, _ := s.GetPage(ctx, other.ID); go2.BodyMD != "[[Старое]]" {
		t.Fatalf("other project touched: %q", go2.BodyMD)
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
