package service

import (
	"strings"
	"testing"
)

func TestHTMLDocumentsOnTasksAndPages(t *testing.T) {
	s := newSvc(t)
	p := mustProject(t, s, "qa")
	other := mustProject(t, s, "other")
	task := mustTasks(t, s, "codex", NewTask{Project: "qa", Title: "Task"})[0]
	page, _, err := s.WritePage(ctx, "claude", "qa", WritePageInput{Title: "Wiki", BodyMD: "existing markdown"})
	if err != nil {
		t.Fatal(err)
	}
	in := PublishHTMLInput{Task: task.Code, Title: "Report", HTML: "<!doctype html><button>One</button>"}
	first, err := s.PublishHTML(ctx, "codex", in)
	if err != nil || first.HTML != "" || first.Version != 1 {
		t.Fatal(first, err)
	}
	if retry, err := s.PublishHTML(ctx, "codex", in); err != nil || retry.ID != first.ID || retry.Version != 1 {
		t.Fatal(retry, err)
	}
	in.HTML = "<button>Two</button>"
	if _, err := s.PublishHTML(ctx, "claude", in); ErrorCode(err) != "conflict" {
		t.Fatal(err)
	}
	in.ExpectedVersion = 1
	second, err := s.PublishHTML(ctx, "claude", in)
	if err != nil || second.Version != 2 || second.ID != first.ID {
		t.Fatal(second, err)
	}
	in.Task = ""
	in.PageID = page.ID
	in.ExpectedVersion = 0
	wiki, err := s.PublishHTML(ctx, "codex", in)
	if err != nil || wiki.ID == first.ID {
		t.Fatal(wiki, err)
	}
	td, err := s.GetTask(ctx, task.Code, 20)
	if err != nil || len(td.Documents) != 1 || td.Documents[0].Version != 2 {
		t.Fatal(td.Documents, err)
	}
	pd, err := s.GetPageByTitle(ctx, "qa", "Wiki")
	if err != nil || pd.BodyMD != "existing markdown" || len(pd.Documents) != 1 {
		t.Fatal(pd, err)
	}
	v1, err := s.ReadHTML(ctx, first.ID, 1)
	if err != nil || v1.Author != "codex" || !strings.Contains(v1.HTML, "One") {
		t.Fatal(v1, err)
	}
	for _, tc := range []struct {
		id    int64
		count int
	}{{0, 3}, {p.ID, 3}, {other.ID, 0}} {
		export, err := s.Export(ctx, tc.id)
		if err != nil || len(export.Documents) != tc.count {
			t.Fatal(export.Documents, err)
		}
	}
	if err = s.DeleteHTML(ctx, first.ID, 1); ErrorCode(err) != "conflict" {
		t.Fatal(err)
	}
	if _, err = s.DeleteTasks(ctx, ActorDenis, []string{task.Code}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadHTML(ctx, first.ID, 0); ErrorCode(err) != "not_found" {
		t.Fatal(err)
	}
	if _, err = s.PurgeTrash(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.DeletePage(ctx, ActorDenis, page.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadHTML(ctx, wiki.ID, 0); ErrorCode(err) != "not_found" {
		t.Fatal(err)
	}
}
