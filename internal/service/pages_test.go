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
