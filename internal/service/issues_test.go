package service

import "testing"

func TestConcurrentAgentsReportOneIssue(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "qa")
	start := make(chan struct{})
	errors := make(chan error, 2)
	for actor, title := range map[string]string{"codex": "Нет ответа", "claude": "НЕТ ОТВЕТА"} {
		go func() {
			<-start
			_, err := s.ReportIssue(ctx, actor, ReportIssueInput{Project: "qa", Title: title, Observation: "503"})
			errors <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.Attention(ctx)
	if err != nil || len(rows) != 1 || rows[0].ReportCount != 2 {
		t.Fatalf("concurrent reports: %+v, %v", rows, err)
	}
}

func TestIssueReportAndTriage(t *testing.T) {
	s := newSvc(t)
	mustProject(t, s, "qa")
	seed := mustTasks(t, s, ActorDenis, NewTask{Project: "qa", Title: "Source"}, NewTask{Project: "qa", Title: "Legacy", Area: "issue"})
	in := ReportIssueInput{Project: "qa", Title: "Legacy", SourceTask: seed[0].Code, Observation: "Unexpected total", Kind: "blocker"}
	result, err := s.ReportIssue(ctx, "codex", in)
	if err != nil || result.Created || !result.Recorded || result.ID != seed[1].Code {
		t.Fatalf("report: %+v %v", result, err)
	}
	if again, err := s.ReportIssue(ctx, "codex", in); err != nil || again.Recorded {
		t.Fatalf("retry %+v %v", again, err)
	}
	if again, err := s.ReportIssue(ctx, "claude", in); err != nil || !again.Recorded {
		t.Fatalf("second agent %+v %v", again, err)
	}
	rows, err := s.Attention(ctx)
	if err != nil || len(rows) != 1 || rows[0].ReportCount != 2 || rows[0].IssueKind != "blocker" {
		t.Fatalf("attention %+v %v", rows, err)
	}
	if d, err := s.GetTask(ctx, seed[0].Code, 20); err != nil || len(d.Notes) != 2 {
		t.Fatalf("source notes %+v %v", d.Notes, err)
	}
	if err = s.TriageIssue(ctx, result.ID, TriageInput{Action: "snooze", Until: "2099-12-01"}); err != nil {
		t.Fatal(err)
	}
	if rows, err = s.Attention(ctx); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	in.Observation = "Blocked again"
	if _, err = s.ReportIssue(ctx, "codex", in); err != nil {
		t.Fatal(err)
	}
	if rows, err = s.Attention(ctx); err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if err = s.TriageIssue(ctx, result.ID, TriageInput{Action: "take"}); err != nil {
		t.Fatal(err)
	}
	if err = s.TriageIssue(ctx, result.ID, TriageInput{Action: "close", Reason: "Fixed and checked"}); err != nil {
		t.Fatal(err)
	}
	if rows, err = s.Attention(ctx); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	in.Title = "New finding"
	r, err := s.ReportIssue(ctx, "codex", in)
	if err != nil || !r.Created {
		t.Fatal(r, err)
	}
}
