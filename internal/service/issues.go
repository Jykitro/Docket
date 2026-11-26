package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"plandillo/internal/store"
)

type ReportIssueInput struct {
	Project       string `json:"project" jsonschema:"project key or slug of the issue"`
	Title         string `json:"title" jsonschema:"short symptom; the same open issue title in this project is reused"`
	Observation   string `json:"observation" jsonschema:"what happened, where, and how it was noticed"`
	SourceTask    string `json:"source_task,omitempty" jsonschema:"task ID where you noticed the issue"`
	ExistingIssue string `json:"existing_issue,omitempty" jsonschema:"append to this open issue even if the title differs"`
	Kind          string `json:"kind,omitempty" jsonschema:"problem (default), unverified, decision or blocker"`
	Priority      string `json:"priority,omitempty" jsonschema:"priority of a new issue, P0, P1 or P2"`
}

type IssueReceipt struct {
	ID       string `json:"id"`
	Created  bool   `json:"created"`
	Recorded bool   `json:"observation_added"`
}

func (s *Service) ReportIssue(ctx context.Context, actor string, in ReportIssueInput) (result IssueReceipt, err error) {
	in.Title, in.Observation = strings.TrimSpace(in.Title), strings.TrimSpace(in.Observation)
	if in.Title == "" || len(in.Title) > 1024 || in.Observation == "" || len(in.Observation) > 16384 {
		return result, invalid("supply an issue title (1–1024 bytes) and observation (1–16384 bytes)")
	}
	if in.Kind == "" {
		in.Kind = "problem"
	}
	kinds := []string{"unverified", "problem", "decision", "blocker"}
	if err := oneOf("kind", in.Kind, kinds); err != nil {
		return result, err
	}
	if in.Priority != "" {
		if err := oneOf("priority", in.Priority, priorities); err != nil {
			return result, err
		}
	}
	err = s.write(ctx, actor, func(o *op) error {
		project, err := resolveProject(o.tx, in.Project)
		if err != nil {
			return err
		}
		var source store.Task
		if in.SourceTask != "" {
			source, err = liveTask(o.tx, in.SourceTask)
			if err != nil {
				return err
			}
		}
		var issue store.Task
		if in.ExistingIssue != "" {
			issue, err = liveTask(o.tx, in.ExistingIssue)
			if err != nil {
				return err
			}
			if issue.ProjectID != project.ID || issue.StatusCategory == "done" || (issue.IssueKind == "" && issue.Area != "issue") {
				return invalid("existing_issue must be an open issue in this project")
			}
		} else {
			id, err := o.tx.OpenIssueID(project.ID, in.Title)
			if err != nil {
				return err
			}
			if id != 0 {
				issue, err = o.tx.GetTask(id)
				if err != nil {
					return err
				}
			}
		}
		if issue.ID == 0 {
			tasks, err := createTasks(o, []NewTask{{ProjectID: project.ID, Title: in.Title, Area: "issue", Priority: in.Priority, DescriptionMD: in.Observation}})
			if err != nil {
				return err
			}
			issue = tasks[0]
			if issue.StatusCategory == "done" {
				return invalid("issues need an open default status")
			}
			result.Created = true
		}
		result.ID = issue.Code
		key := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", source.ID, actor, in.Kind, in.Observation)))
		result.Recorded, err = o.tx.RecordIssueReport(issue.ID, fmt.Sprintf("%x", key))
		if err != nil || !result.Recorded {
			return err
		}
		kind := in.Kind
		if slices.Index(kinds, issue.IssueKind) > slices.Index(kinds, kind) {
			kind = issue.IssueKind
		}
		after := issue.AttentionAfter
		if in.Kind == "blocker" || in.Kind == "decision" {
			after = nil
		}
		if err = o.tx.SetIssueAttention(issue.ID, kind, after); err != nil {
			return err
		}
		note := in.Observation
		if source.ID != 0 && source.ID != issue.ID {
			note += fmt.Sprintf("\n\n[%s](/t/%d)", source.Code, source.ID)
			if err = issueNote(o, source, fmt.Sprintf("[%s](/t/%d) — %s", issue.Code, issue.ID, issue.Title)); err != nil {
				return err
			}
		}
		return issueNote(o, issue, note)
	})
	return result, err
}

func issueNote(o *op, t store.Task, body string) error {
	if _, err := o.tx.InsertNote(store.Note{TaskID: t.ID, Author: o.actor, BodyMD: body}); err != nil {
		return err
	}
	return o.log("note_added", t.ID, t.ProjectID, nil)
}

func (s *Service) Attention(ctx context.Context) (out []store.AttentionItem, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { out, err = tx.ListAttention(s.Today()); return err })
	return
}

type TriageInput struct {
	Action string `json:"action"`
	Until  string `json:"until"`
	Reason string `json:"reason"`
}

func (s *Service) TriageIssue(ctx context.Context, ref string, in TriageInput) error {
	return s.write(ctx, ActorDenis, func(o *op) error {
		t, err := liveTask(o.tx, ref)
		if err != nil {
			return err
		}
		if (t.IssueKind == "" && t.Area != "issue") || t.StatusCategory == "done" {
			return invalid("an open issue is required")
		}
		if in.Action == "snooze" {
			if err = checkDate("until", in.Until); err != nil {
				return err
			}
			if in.Until <= s.Today() {
				return invalid("choose a future date")
			}
			if err = o.tx.SetIssueAttention(t.ID, t.IssueKind, &in.Until); err != nil {
				return err
			}
			return o.log("updated", t.ID, t.ProjectID, diff{"attention_after": {t.AttentionAfter, in.Until}})
		}
		category := "doing"
		switch in.Action {
		case "close":
			category = "done"
			if strings.TrimSpace(in.Reason) == "" {
				return invalid("a reason is required to close an issue")
			}
		case "take":
		default:
			return invalid("action must be take, snooze or close")
		}
		sts, err := o.tx.ListStatuses(t.ProjectID)
		if err != nil {
			return err
		}
		for _, st := range sts {
			if st.Category != category {
				continue
			}
			d, err := apply(o, &t, Changes{Status: &st.Key})
			if err != nil {
				return err
			}
			if err = o.tx.SetIssueAttention(t.ID, t.IssueKind, nil); err != nil {
				return err
			}
			if err = o.log("updated", t.ID, t.ProjectID, d); err != nil {
				return err
			}
			if in.Action == "close" {
				return issueNote(o, t, strings.TrimSpace(in.Reason))
			}
			return nil
		}
		return invalid("project has no status in category %q", category)
	})
}
