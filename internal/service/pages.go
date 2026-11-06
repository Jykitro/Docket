package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"docket/internal/store"
)

const (
	maxPageTitle = 120
	maxPageBody  = 204800
)

type PageInput struct {
	Title, BodyMD string
	ParentID      *int64
}

type PagePatch struct {
	Title, BodyMD     *string
	ParentID          *int64 // 0 = top level
	ExpectedUpdatedAt string
}

// titleKey is the uniqueness key of a page title; SQLite lower() folds only ASCII, so it is computed here.
func titleKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func checkPageTitle(title string) error {
	if n := utf8.RuneCountInString(title); n < 1 || n > maxPageTitle {
		return invalid("page title must be 1-%d characters, got %d", maxPageTitle, n)
	}
	if strings.ContainsAny(title, "[]\r\n") {
		return invalid("page title must not contain [, ] or a line break: it would break [[page]] links")
	}
	return nil
}

func checkPageBody(body string) error {
	if len(body) > maxPageBody {
		return invalid("page body is %d bytes; the limit is %d", len(body), maxPageBody)
	}
	return nil
}

// checkPageParent requires a parent of the same project that is not the page itself or its descendant.
func checkPageParent(tx *store.Tx, projectID, selfID, parentID int64) error {
	for cur := parentID; ; {
		if cur == selfID {
			return invalid("a page cannot be moved under itself or its own subpage")
		}
		p, err := tx.GetPage(cur)
		if err != nil {
			return invalid("parent page %d not found", cur)
		}
		if p.ProjectID != projectID {
			return invalid("parent page %d belongs to another project", cur)
		}
		if p.ParentID == nil {
			return nil
		}
		cur = *p.ParentID
	}
}

func checkPageTitleFree(tx *store.Tx, projectID, selfID int64, title string) error {
	other, err := tx.PageByTitleKey(projectID, titleKey(title))
	if err == nil && other.ID != selfID {
		return invalid("a page titled %q already exists in this project (titles are compared ignoring case)", other.Title)
	}
	if err != nil && err != store.ErrNotFound {
		return err
	}
	return nil
}

// ListPages returns the project's pages flat, without bodies.
func (s *Service) ListPages(ctx context.Context, projectRef string) (out []store.Page, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		p, err := resolveProject(tx, projectRef)
		if err != nil {
			return err
		}
		if out, err = tx.ListPages(p.ID); err != nil {
			return err
		}
		for i := range out {
			out[i].BodyMD = ""
		}
		return nil
	})
	return out, err
}

func (s *Service) CreatePage(ctx context.Context, actor, projectRef string, in PageInput) (pg store.Page, err error) {
	title := strings.TrimSpace(in.Title)
	if err := checkPageTitle(title); err != nil {
		return pg, err
	}
	if err := checkPageBody(in.BodyMD); err != nil {
		return pg, err
	}
	err = s.write(ctx, actor, func(o *op) error {
		p, err := resolveProject(o.tx, projectRef)
		if err != nil {
			return err
		}
		if in.ParentID != nil {
			if err := checkPageParent(o.tx, p.ID, 0, *in.ParentID); err != nil {
				return err
			}
		}
		if err := checkPageTitleFree(o.tx, p.ID, 0, title); err != nil {
			return err
		}
		pos, err := o.tx.NextPagePosition(p.ID, in.ParentID)
		if err != nil {
			return err
		}
		now := nowTS()
		pg, err = o.tx.InsertPage(store.Page{ProjectID: p.ID, ParentID: in.ParentID, Title: title, BodyMD: in.BodyMD,
			Position: pos, CreatedAt: now, UpdatedAt: now}, titleKey(title))
		o.emit("page_changed", 0, p.ID)
		return err
	})
	return pg, err
}

func (s *Service) UpdatePage(ctx context.Context, actor string, id int64, patch PagePatch) (pg store.Page, err error) {
	if patch.Title != nil {
		t := strings.TrimSpace(*patch.Title)
		patch.Title = &t
		if err := checkPageTitle(t); err != nil {
			return pg, err
		}
	}
	if patch.BodyMD != nil {
		if err := checkPageBody(*patch.BodyMD); err != nil {
			return pg, err
		}
	}
	err = s.write(ctx, actor, func(o *op) error {
		if pg, err = o.tx.GetPage(id); err != nil {
			return notFound("page %d not found", id)
		}
		if patch.ExpectedUpdatedAt != "" && patch.ExpectedUpdatedAt != pg.UpdatedAt {
			return conflict("page changed since %s; reload it and merge", patch.ExpectedUpdatedAt)
		}
		if patch.Title != nil {
			if err := checkPageTitleFree(o.tx, pg.ProjectID, id, *patch.Title); err != nil {
				return err
			}
			pg.Title = *patch.Title
		}
		if patch.BodyMD != nil {
			pg.BodyMD = *patch.BodyMD
		}
		if patch.ParentID != nil {
			var parent *int64
			if *patch.ParentID != 0 {
				parent = patch.ParentID
				if err := checkPageParent(o.tx, pg.ProjectID, id, *parent); err != nil {
					return err
				}
			}
			if !samePage(pg.ParentID, parent) {
				pg.ParentID = parent
				if pg.Position, err = o.tx.NextPagePosition(pg.ProjectID, parent); err != nil {
					return err
				}
			}
		}
		pg.UpdatedAt = nowTS()
		if err := o.tx.UpdatePage(pg, titleKey(pg.Title)); err != nil {
			return err
		}
		o.emit("page_changed", 0, pg.ProjectID)
		return nil
	})
	return pg, err
}

func samePage(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// DeletePage removes a page; its subpages move up to its parent.
func (s *Service) DeletePage(ctx context.Context, actor string, id int64) error {
	return s.write(ctx, actor, func(o *op) error {
		pg, err := o.tx.GetPage(id)
		if err != nil {
			return notFound("page %d not found", id)
		}
		if err := o.tx.ReparentPages(id, pg.ParentID); err != nil {
			return err
		}
		if err := o.tx.DeletePage(id); err != nil {
			return err
		}
		o.emit("page_changed", 0, pg.ProjectID)
		return nil
	})
}
