package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"docket/internal/store"
)

const (
	maxPageTitle = 120
	maxPageBody  = 204800
)

type PageInput struct {
	Title, BodyMD string
	BodyHTML      *string
	ParentID      *int64
}

type PagePatch struct {
	Title, BodyMD, BodyHTML *string
	ParentID                *int64 // 0 = top level
	ExpectedUpdatedAt       string
}

type PageLayout struct {
	Pinned   *bool `json:"pinned"`
	BeforeID int64 `json:"before_id"`
	AfterID  int64 `json:"after_id"`
}

// Page layout changes leave the content revision valid for open editors.
func (s *Service) SetPageLayout(ctx context.Context, actor string, id int64, in PageLayout) error {
	move := in.BeforeID != 0 || in.AfterID != 0
	if (in.Pinned == nil && !move) || (in.Pinned != nil && move) || (in.BeforeID != 0 && in.AfterID != 0) {
		return invalid("provide exactly one of pinned, before_id or after_id")
	}
	return s.write(ctx, actor, func(o *op) error {
		page, err := getPage(o.tx, id)
		if err != nil {
			return err
		}
		if in.Pinned != nil {
			if err := o.tx.SetPagePinned(id, *in.Pinned); err != nil {
				return err
			}
		} else {
			siblings, err := o.tx.PageSiblings(page)
			if err != nil {
				return err
			}
			pos, err := relativePosition(siblings, id, in.BeforeID, in.AfterID, o.tx.SetPagePosition, "target must be another page under the same parent")
			if err != nil {
				return err
			}
			if err := o.tx.SetPagePosition(id, pos); err != nil {
				return err
			}
		}
		o.emit("page_changed", 0, page.ProjectID)
		return nil
	})
}

// A content revision must advance even when two writes share a millisecond.
func nextPageRevision(previous string) string {
	now := nowTS()
	if now > previous {
		return now
	}
	t, err := time.Parse(time.RFC3339Nano, previous)
	if err != nil {
		return now
	}
	return t.Add(time.Millisecond).UTC().Format("2006-01-02T15:04:05.000Z")
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
		if errors.Is(err, store.ErrNotFound) {
			return invalid("parent page %d not found", cur)
		}
		if err != nil {
			return err
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
func (s *Service) ListPages(ctx context.Context, projectRef string) ([]store.Page, error) {
	return s.WikiPageHeaders(ctx, projectRef, ReadPage{})
}
func (s *Service) WikiPageHeaders(ctx context.Context, projectRef string, page ReadPage) (out []store.Page, err error) {
	page, err = page.normalized()
	if err != nil {
		return
	}
	err = s.read(ctx, func(tx *store.Tx) error {
		p, err := resolveProject(tx, projectRef)
		if err != nil {
			return err
		}
		out, err = tx.PageHeaders(p.ID, page.Limit, page.Offset)
		return err
	})
	return out, err
}

func (s *Service) CreatePage(ctx context.Context, actor, projectRef string, in PageInput) (pg store.Page, err error) {
	if in.ParentID != nil && *in.ParentID == 0 { // 0 = top level, as in UpdatePage
		in.ParentID = nil
	}
	title := strings.TrimSpace(in.Title)
	if err := checkPageTitle(title); err != nil {
		return pg, err
	}
	if in.BodyHTML != nil {
		if in.BodyMD != "" {
			return pg, invalid("provide one of body_md or body_html")
		}
		if len(*in.BodyHTML) > maxHTMLBytes {
			return pg, invalid("HTML exceeds 2 MiB")
		}
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
		pg, err = o.tx.InsertPage(store.Page{ProjectID: p.ID, ParentID: in.ParentID, Title: title, BodyMD: in.BodyMD, BodyHTML: in.BodyHTML,
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
		}
		if patch.BodyHTML != nil {
			if patch.BodyMD != nil {
				return invalid("provide one of body_md or body_html")
			}
			if cur.BodyHTML == nil {
				return invalid("this Markdown page requires body_md")
			}
			if len(*patch.BodyHTML) > maxHTMLBytes {
				return invalid("HTML exceeds 2 MiB")
			}
			cur.BodyHTML = patch.BodyHTML
		}
		if patch.BodyMD != nil {
			if cur.BodyHTML != nil {
				return invalid("this HTML page requires body_html")
			}
			cur.BodyMD = *patch.BodyMD
		}
		if patch.Title != nil && titleKey(cur.Title) != oldKey {
			if err := rewriteLinks(o.tx, cur, oldKey, cur.Title); err != nil {
				return err
			}
			cur.BodyMD = replaceLinks(cur.BodyMD, oldKey, cur.Title)
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
		cur.UpdatedAt = nextPageRevision(cur.UpdatedAt)
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

type WritePageInput struct {
	Title, BodyMD     string
	BodyHTML          *string
	Parent            string // parent page title; "" = unchanged on update, top level on create
	ExpectedUpdatedAt string
}

// findPage looks up a page by title and, when parentTitle is set, the parent's ID, in one read.
// shortcut: lookup and write are separate transactions; a racing creator of the same title gets a
// validation error, a racing editor a conflict (AppendToPage pins updated_at). Fine for one assistant.
func (s *Service) findPage(ctx context.Context, projectRef, title, parentTitle string) (pg *store.Page, parentID *int64, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		p, err := resolveProject(tx, projectRef)
		if err != nil {
			return err
		}
		found, err := tx.PageByTitleKey(p.ID, titleKey(title))
		if err == nil {
			pg = &found
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if parentTitle == "" {
			return nil
		}
		par, err := tx.PageByTitleKey(p.ID, titleKey(parentTitle))
		if errors.Is(err, store.ErrNotFound) {
			return invalid("parent page %q not found in this project; create it first or leave parent empty", parentTitle)
		}
		parentID = &par.ID
		return err
	})
	return pg, parentID, err
}

// WritePage creates the page titled in.Title or replaces its body (upsert by title, ignoring case; the stored title is kept).
// A non-empty ExpectedUpdatedAt for a title that does not exist is a conflict: the page was deleted or renamed meanwhile.
func (s *Service) WritePage(ctx context.Context, actor, projectRef string, in WritePageInput) (store.Page, bool, error) {
	pg, parentID, err := s.findPage(ctx, projectRef, in.Title, in.Parent)
	if err != nil {
		return store.Page{}, false, err
	}
	if pg == nil && in.ExpectedUpdatedAt != "" {
		return store.Page{}, false, conflict("page %q no longer exists; it was deleted or renamed since you read it", in.Title)
	}
	if pg == nil {
		created, err := s.CreatePage(ctx, actor, projectRef, PageInput{Title: in.Title, BodyMD: in.BodyMD, BodyHTML: in.BodyHTML, ParentID: parentID})
		return created, true, err
	}
	// The title stays as stored: a different casing in the call must not rename the owner's page.
	patch := PagePatch{BodyHTML: in.BodyHTML, ParentID: parentID, ExpectedUpdatedAt: in.ExpectedUpdatedAt}
	if in.BodyHTML == nil {
		patch.BodyMD = &in.BodyMD
	} else if in.BodyMD != "" {
		return store.Page{}, false, invalid("provide one of body_md or body_html")
	}
	updated, err := s.UpdatePage(ctx, actor, pg.ID, patch)
	return updated, false, err
}

// AppendToPage adds text after a blank line at the end of the page titled title.
func (s *Service) AppendToPage(ctx context.Context, actor, projectRef, title, text string) (store.Page, error) {
	if strings.TrimSpace(text) == "" {
		return store.Page{}, invalid("text to append is empty")
	}
	pg, _, err := s.findPage(ctx, projectRef, title, "")
	if err != nil {
		return store.Page{}, err
	}
	if pg == nil {
		return store.Page{}, notFound("no page titled %q in this project", title)
	}
	if pg.BodyHTML != nil {
		return store.Page{}, invalid("append_to_page requires Markdown; use write_page with body_html to update HTML")
	}
	body := text
	if base := strings.TrimRight(pg.BodyMD, "\n"); base != "" {
		body = base + "\n\n" + text
	}
	return s.UpdatePage(ctx, actor, pg.ID, PagePatch{BodyMD: &body, ExpectedUpdatedAt: pg.UpdatedAt})
}

// Keep interactive source opaque to the wiki link index and automatic renames.
// Track other fences too, so Markdown examples that quote a live block stay literal.
func mapWikiProse(body string, prose func(string) string, keepHTML bool) string {
	if !strings.Contains(body, "html-preview") {
		return prose(body)
	}
	var out strings.Builder
	var fence string
	var live bool
	var start, offset int
	for line := range strings.SplitAfterSeq(body, "\n") {
		begin := offset
		offset += len(line)
		trim := strings.TrimLeft(line, " ")
		if len(line)-len(trim) > 3 || len(trim) < 3 || (trim[0] != '~' && trim[0] != '`') {
			continue
		}
		n := 1
		for n < len(trim) && trim[n] == trim[0] {
			n++
		}
		if n < 3 {
			continue
		}
		info := strings.TrimSpace(trim[n:])
		if fence == "" {
			if trim[0] == '`' && strings.ContainsRune(info, '`') {
				continue
			}
			fence = trim[:n]
			live = info == "html-preview"
			if live {
				out.WriteString(prose(body[start:begin]))
				start = begin
			}
		} else if strings.HasPrefix(trim[:n], fence) && info == "" {
			if live {
				if keepHTML {
					out.WriteString(body[start:offset])
				}
				start = offset
			}
			fence = ""
			live = false
		}
	}
	if live {
		if keepHTML {
			out.WriteString(body[start:])
		}
	} else {
		out.WriteString(prose(body[start:]))
	}
	return out.String()
}

func linkText(body string) string {
	return mapWikiProse(body, func(s string) string { return s }, false)
}

func replaceLinks(body, oldKey, newTitle string) string {
	return mapWikiProse(body, func(text string) string {
		return wikiLinkRe.ReplaceAllStringFunc(text, func(m string) string {
			if titleKey(m[2:len(m)-2]) == oldKey {
				return "[[" + newTitle + "]]"
			}
			return m
		})
	}, true)
}

// rewriteLinks turns [[old]] into [[newTitle]] in the other pages of self's project (same transaction);
// self's own body is rewritten by the caller.
func rewriteLinks(tx *store.Tx, self store.Page, oldKey, newTitle string) error {
	var lastID int64
	for {
		pages, err := tx.MarkdownBatch(self.ProjectID, lastID)
		if err != nil {
			return err
		}
		if len(pages) == 0 {
			return nil
		}
		lastID = pages[len(pages)-1].ID
		for _, p := range pages {
			if p.ID == self.ID {
				continue
			}
			body := replaceLinks(p.BodyMD, oldKey, newTitle)
			if body == p.BodyMD {
				continue
			}
			p.BodyMD, p.UpdatedAt = body, nextPageRevision(p.UpdatedAt)
			if err := tx.UpdatePage(p, titleKey(p.Title)); err != nil {
				return err
			}
		}
	}
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
	Documents []store.HTMLDocument `json:"documents,omitempty"`
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
	body = linkText(body)
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

func pageDetail(tx *store.Tx, pg store.Page, documentOffset int) (PageDetail, error) {
	d := PageDetail{Page: pg, Backlinks: []PageRef{}}
	d.Links.Pages, d.Links.Tasks = []PageRef{}, []TaskRef{}
	if pg.ParentID != nil {
		title, err := tx.PageTitle(*pg.ParentID)
		if err != nil {
			return d, err
		}
		d.ParentTitle = title
	}
	titles, codes := parseLinks(pg.BodyMD)
	targets := make(map[string]PageRef, len(titles))
	for _, key := range titles {
		targets[key] = PageRef{}
	}
	self := titleKey(pg.Title)
	offset := documentOffset
	err := tx.WalkPageLinks(pg.ProjectID, func(id int64, title, body string) error {
		if k := titleKey(title); len(targets) > 0 {
			if _, needed := targets[k]; needed {
				targets[k] = PageRef{id, title}
			}
		}
		if id != pg.ID && len(d.Backlinks) < TaskPageSize && strings.Contains(body, "[[") {
			if links, _ := parseLinks(body); contains(links, self) {
				if offset > 0 {
					offset--
				} else {
					d.Backlinks = append(d.Backlinks, PageRef{id, title})
				}
			}
		}
		return nil
	})
	if err != nil {
		return d, err
	}
	for _, key := range titles {
		if ref := targets[key]; ref.ID != 0 {
			d.Links.Pages = append(d.Links.Pages, ref)
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
		if t.DeletedAt == nil && t.Code == c { // only the canonical form (TRK-07) is a link, as in TaskPages
			d.Links.Tasks = append(d.Links.Tasks, TaskRef{t.Code, t.Title})
		}
	}
	d.Documents, err = tx.Documents(0, pg.ID, TaskPageSize, documentOffset)
	return d, err
}

func (s *Service) GetPage(ctx context.Context, id int64) (PageDetail, error) {
	return s.PageDocuments(ctx, id, 0)
}
func (s *Service) PageDocuments(ctx context.Context, id int64, documentOffset int) (d PageDetail, err error) {
	if documentOffset < 0 {
		return d, invalid("document_offset must be non-negative")
	}
	err = s.read(ctx, func(tx *store.Tx) error {
		pg, err := getPage(tx, id)
		if err != nil {
			return err
		}
		d, err = pageDetail(tx, pg, documentOffset)
		return err
	})
	return d, err
}

func (s *Service) GetPageByTitle(ctx context.Context, projectRef, title string) (PageDetail, error) {
	return s.PageDocumentsByTitle(ctx, projectRef, title, 0)
}
func (s *Service) PageDocumentsByTitle(ctx context.Context, projectRef, title string, documentOffset int) (d PageDetail, err error) {
	if documentOffset < 0 {
		return d, invalid("document_offset must be non-negative")
	}
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
		d, err = pageDetail(tx, pg, documentOffset)
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
			if !contains(taskCodeRe.FindAllString(linkText(pg.BodyMD), -1), t.Code) {
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
