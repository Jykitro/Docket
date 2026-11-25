package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"docket/internal/store"
)

const (
	maxHTMLBytes            = 2 << 20
	maxProfileRevisionBytes = 64 << 20
	maxProfileRevisions     = 4096
)

type PublishHTMLInput struct {
	Task            string `json:"task,omitempty" jsonschema:"owning task code or numeric ID; use either task or page_id"`
	PageID          int64  `json:"page_id,omitempty" jsonschema:"owning wiki page ID from read_page"`
	Title           string `json:"title" jsonschema:"document name, unique within this task or wiki page"`
	HTML            string `json:"html" jsonschema:"complete self-contained HTML with inline CSS and JavaScript, up to 2 MiB"`
	ExpectedVersion int    `json:"expected_version,omitempty" jsonschema:"0 for new, current version for an update"`
}

func (s *Service) PublishHTML(ctx context.Context, actor string, in PublishHTMLInput) (store.HTMLDocument, error) {
	return s.publishHTML(ctx, actor, in, maxProfileRevisionBytes, maxProfileRevisions)
}

func (s *Service) publishHTML(ctx context.Context, actor string, in PublishHTMLInput, maxBytes int64, maxRevisions int) (d store.HTMLDocument, err error) {
	in.Title = strings.TrimSpace(in.Title)
	if (in.Task == "") == (in.PageID == 0) || in.PageID < 0 {
		return d, invalid("provide exactly one task or page_id")
	}
	if in.Title == "" || utf8.RuneCountInString(in.Title) > 120 || strings.ContainsAny(in.Title, "\r\n") {
		return d, invalid("document title must be one line, 1–120 characters")
	}
	if strings.TrimSpace(in.HTML) == "" || len(in.HTML) > maxHTMLBytes {
		return d, invalid("HTML must be between 1 byte and 2 MiB")
	}
	if in.ExpectedVersion < 0 {
		return d, invalid("expected_version must be 0 or a positive revision")
	}
	err = s.write(ctx, actor, func(o *op) error {
		var taskID, projectID int64
		if in.Task != "" {
			t, err := liveTask(o.tx, in.Task)
			if err != nil {
				return err
			}
			taskID, projectID = t.ID, t.ProjectID
			d.TaskID = &taskID
		} else {
			p, err := getPage(o.tx, in.PageID)
			if err != nil {
				return err
			}
			projectID = p.ProjectID
			d.PageID = &in.PageID
		}
		if err := requireLiveProject(o.tx, projectID); err != nil {
			return err
		}
		key := titleKey(in.Title)
		old, err := o.tx.DocumentByTitle(taskID, in.PageID, key)
		switch {
		case err == nil:
			d = old
		case errors.Is(err, store.ErrNotFound):
			d.Title = in.Title
		default:
			return err
		}
		if d.Version != in.ExpectedVersion {
			// A retry after a committed write may have lost its response.
			if d.Version == in.ExpectedVersion+1 && d.Author == actor {
				current, err := o.tx.Document(d.ID, d.Version)
				if err != nil {
					return err
				}
				if current.HTML == in.HTML {
					return nil
				}
			}
			return conflict("document version is %d; read it before publishing", d.Version)
		}
		used, count, err := o.tx.RevisionUsage(maxRevisions)
		if err != nil {
			return err
		}
		if count >= maxRevisions || used+int64(len(in.HTML)) > maxBytes {
			return &Error{Code: "quota_exceeded", Message: fmt.Sprintf("profile HTML history limit reached (%d bytes or %d revisions); delete documents to free space", maxBytes, maxRevisions)}
		}
		d.Version++
		d.Author = actor
		d.CreatedAt = nowTS()
		d.HTML = in.HTML
		if err = o.tx.SaveDocument(&d, key); err != nil {
			return err
		}
		d.HTML = ""
		return o.log("html_published", taskID, projectID, diff{"document": {nil, d.Title}, "version": {nil, d.Version}})
	})
	return
}

func (s *Service) ReadHTML(ctx context.Context, id int64, version int) (d store.HTMLDocument, err error) {
	if id < 1 || version < 0 {
		return d, invalid("provide a document ID and a non-negative version")
	}
	err = s.read(ctx, func(tx *store.Tx) error { d, err = tx.Document(id, version); return err })
	return
}

func (s *Service) DeleteHTML(ctx context.Context, id int64, version int) error {
	return s.write(ctx, ActorDenis, func(o *op) error {
		d, err := o.tx.Document(id, 0)
		if err != nil {
			return err
		}
		if d.Version != version {
			return conflict("document changed; refresh before deleting")
		}
		var pid, tid int64
		if d.TaskID != nil {
			t, err := liveTask(o.tx, fmt.Sprint(*d.TaskID))
			if err != nil {
				return err
			}
			tid, pid = t.ID, t.ProjectID
		} else {
			p, err := getPage(o.tx, *d.PageID)
			if err != nil {
				return err
			}
			pid = p.ProjectID
		}
		o.skipStorageCheck = true
		if err = o.tx.DeleteDocument(id); err != nil {
			return err
		}
		return o.log("html_deleted", tid, pid, diff{"document": {d.Title, nil}})
	})
}
