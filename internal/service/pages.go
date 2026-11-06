package service

import (
	"context"
	"errors"
	"regexp"
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
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

func getPage(tx *store.Tx, id int64) (store.Page, error) {
	p, err := tx.GetPage(id)
	if errors.Is(err, store.ErrNotFound) {
		return p, notFound("page %d not found", id)
	}
	return p, err
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
		cur, err := getPage(o.tx, id)
		if err != nil {
			return err
		}
		oldKey := titleKey(cur.Title)
		if patch.ExpectedUpdatedAt != "" && patch.ExpectedUpdatedAt != cur.UpdatedAt {
			return conflict("page changed since %s; reload it and merge", patch.ExpectedUpdatedAt)
		}
		if patch.Title != nil {
			if err := checkPageTitleFree(o.tx, cur.ProjectID, id, *patch.Title); err != nil {
				return err
			}
			cur.Title = *patch.Title
			if newKey := titleKey(cur.Title); newKey != oldKey {
				if err := rewriteLinks(o.tx, cur, oldKey, cur.Title); err != nil {
					return err
				}
			}
		}
		if patch.BodyMD != nil {
			cur.BodyMD = *patch.BodyMD
		}
		if patch.ParentID != nil {
			var parent *int64
			if *patch.ParentID != 0 {
				parent = patch.ParentID
				if err := checkPageParent(o.tx, cur.ProjectID, id, *parent); err != nil {
					return err
				}
			}
			if !samePage(cur.ParentID, parent) {
				cur.ParentID = parent
				if cur.Position, err = o.tx.NextPagePosition(cur.ProjectID, parent); err != nil {
					return err
				}
			}
		}
		cur.UpdatedAt = nowTS()
		if err := o.tx.UpdatePage(cur, titleKey(cur.Title)); err != nil {
			return err
		}
		o.emit("page_changed", 0, cur.ProjectID)
		pg = cur
		return nil
	})
	if err != nil {
		return store.Page{}, err
	}
	return pg, nil
}

// rewriteLinks turns [[old]] into [[newTitle]] in the other pages of self's project (same transaction).
func rewriteLinks(tx *store.Tx, self store.Page, oldKey, newTitle string) error {
	pages, err := tx.ListPages(self.ProjectID)
	if err != nil {
		return err
	}
	now := nowTS()
	for _, p := range pages {
		if p.ID == self.ID {
			continue
		}
		body := wikiLinkRe.ReplaceAllStringFunc(p.BodyMD, func(m string) string {
			if titleKey(m[2:len(m)-2]) == oldKey {
				return "[[" + newTitle + "]]"
			}
			return m
		})
		if body == p.BodyMD {
			continue
		}
		p.BodyMD, p.UpdatedAt = body, now
		if err := tx.UpdatePage(p, titleKey(p.Title)); err != nil {
			return err
		}
	}
	return nil
}

func samePage(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

// DeletePage removes a page; its subpages move up to its parent.
func (s *Service) DeletePage(ctx context.Context, actor string, id int64) error {
	return s.write(ctx, actor, func(o *op) error {
		pg, err := getPage(o.tx, id)
		if err != nil {
			return err
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

var (
	wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)
	taskCodeRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,4}-\d+\b`)
)

type PageRef struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type TaskRef struct {
	Code  string `json:"code"`
	Title string `json:"title"`
}

type PageDetail struct {
	store.Page
	Backlinks []PageRef `json:"backlinks"`
	Links     struct {
		Pages []PageRef `json:"pages"`
		Tasks []TaskRef `json:"tasks"`
	} `json:"links"`
}

type TaskPage struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project"`
}

// parseLinks returns the distinct [[page]] titles (as titleKey) and task codes of a body.
// shortcut: links inside fenced code are counted too; upgrade when it matters.
func parseLinks(body string) (titles []string, codes []string) {
	seen := map[string]bool{}
	for _, m := range wikiLinkRe.FindAllStringSubmatch(body, -1) {
		if k := titleKey(m[1]); k != "" && !seen[k] {
			seen[k] = true
			titles = append(titles, k)
		}
	}
	for _, c := range taskCodeRe.FindAllString(body, -1) {
		if !seen[c] {
			seen[c] = true
			codes = append(codes, c)
		}
	}
	return titles, codes
}

func pageDetail(tx *store.Tx, pg store.Page) (PageDetail, error) {
	d := PageDetail{Page: pg, Backlinks: []PageRef{}}
	d.Links.Pages, d.Links.Tasks = []PageRef{}, []TaskRef{}
	all, err := tx.ListPages(pg.ProjectID)
	if err != nil {
		return d, err
	}
	byKey := map[string]store.Page{}
	self := titleKey(pg.Title)
	for _, p := range all {
		byKey[titleKey(p.Title)] = p
		if p.ID == pg.ID {
			continue
		}
		if titles, _ := parseLinks(p.BodyMD); contains(titles, self) {
			d.Backlinks = append(d.Backlinks, PageRef{p.ID, p.Title})
		}
	}
	titles, codes := parseLinks(pg.BodyMD)
	for _, k := range titles {
		if p, ok := byKey[k]; ok {
			d.Links.Pages = append(d.Links.Pages, PageRef{p.ID, p.Title})
		}
	}
	for _, c := range codes {
		t, err := resolveTask(tx, c)
		if err != nil {
			var se *Error
			if errors.As(err, &se) && (se.Code == "not_found" || se.Code == "validation") {
				continue
			}
			return d, err
		}
		if t.DeletedAt == nil {
			d.Links.Tasks = append(d.Links.Tasks, TaskRef{t.Code, t.Title})
		}
	}
	return d, nil
}

func (s *Service) GetPage(ctx context.Context, id int64) (d PageDetail, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		pg, err := getPage(tx, id)
		if err != nil {
			return err
		}
		d, err = pageDetail(tx, pg)
		return err
	})
	return d, err
}

func (s *Service) GetPageByTitle(ctx context.Context, projectRef, title string) (d PageDetail, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		p, err := resolveProject(tx, projectRef)
		if err != nil {
			return err
		}
		pg, err := tx.PageByTitleKey(p.ID, titleKey(title))
		if errors.Is(err, store.ErrNotFound) {
			return notFound("no page titled %q in this project", title)
		}
		if err != nil {
			return err
		}
		d, err = pageDetail(tx, pg)
		return err
	})
	return d, err
}

// TaskPages lists the pages (any project) whose body mentions the task's code.
func (s *Service) TaskPages(ctx context.Context, taskRef string) (out []TaskPage, err error) {
	out = []TaskPage{}
	err = s.read(ctx, func(tx *store.Tx) error {
		t, err := resolveTask(tx, taskRef)
		if err != nil {
			return err
		}
		if t.DeletedAt != nil {
			return notFound("task %s not found", t.Code)
		}
		cand, err := tx.PagesContaining(t.Code)
		if err != nil {
			return err
		}
		for _, pg := range cand {
			if !contains(taskCodeRe.FindAllString(pg.BodyMD, -1), t.Code) {
				continue
			}
			p, err := tx.GetProject(pg.ProjectID)
			if err != nil {
				return err
			}
			if p.DeletedAt == nil {
				out = append(out, TaskPage{pg.ID, pg.Title, p.Slug})
			}
		}
		return nil
	})
	return out, err
}
