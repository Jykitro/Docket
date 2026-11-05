package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"docket/internal/store"
)

// Project configuration: statuses, labels, custom fields, saved views.

var statusColors = []string{"bk", "td", "dg", "dn"}

func checkStatusColor(c string) error {
	if contains(statusColors, c) || colorRe.MatchString(c) {
		return nil
	}
	return invalid("color must be one of %s or #rrggbb", strings.Join(statusColors, ", "))
}

var translit = map[rune]string{'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh", 'з': "z", 'и': "i", 'й': "y",
	'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "c",
	'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu", 'я': "ya"}

// asciiSlug lowercases name to a-z0-9 words joined by '-', transliterating Russian letters; "" when nothing is left.
func asciiSlug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if t, ok := translit[r]; ok {
			b.WriteString(t)
			dash = false
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// nextLabelColor gives known areas their own colour and others the next slot of the circle.
func nextLabelColor(tx *store.Tx, projectID int64, name string) (string, error) {
	if c, ok := defaultAreas[name]; ok {
		return c, nil
	}
	n, err := tx.CountLabels(projectID)
	return labelColors[n%len(labelColors)], err
}

func (s *Service) projectOp(ctx context.Context, actor string, projectID int64, event string, fn func(o *op) error) error {
	return s.write(ctx, actor, func(o *op) error {
		if _, err := o.tx.GetProject(projectID); err != nil {
			return notFound("project %d not found", projectID)
		}
		if err := fn(o); err != nil {
			return err
		}
		o.emit(event, 0, projectID)
		return nil
	})
}

// ---- statuses ----

func (s *Service) ListStatuses(ctx context.Context, projectID int64) (out []store.Status, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		if _, err := tx.GetProject(projectID); err != nil {
			return notFound("project %d not found", projectID)
		}
		out, err = tx.ListStatuses(projectID)
		return err
	})
	return out, err
}

type StatusInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Category    string `json:"category"`
	ShowOnBoard *bool  `json:"show_on_board"`
}

func (s *Service) CreateStatus(ctx context.Context, actor string, projectID int64, in StatusInput) (st store.Status, err error) {
	err = s.projectOp(ctx, actor, projectID, "status_changed", func(o *op) error {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return invalid("status name is required")
		}
		if in.Color == "" {
			in.Color = "bk"
		}
		if err := checkStatusColor(in.Color); err != nil {
			return err
		}
		if in.Category == "" {
			in.Category = "todo"
		}
		if err := oneOf("category", in.Category, []string{"todo", "doing", "done"}); err != nil {
			return err
		}
		key := in.Key
		if key == "" {
			key = asciiSlug(name)
		}
		if key == "" {
			key = "status"
		}
		base := key
		for i := 2; ; i++ {
			if _, err := o.tx.GetStatusByKey(projectID, key); errors.Is(err, store.ErrNotFound) {
				break
			}
			if in.Key != "" {
				return invalid("status key %q is taken", in.Key)
			}
			key = fmt.Sprintf("%s-%d", base, i)
		}
		show := in.ShowOnBoard == nil || *in.ShowOnBoard
		st, err = o.tx.InsertStatus(store.Status{ProjectID: projectID, Key: key, Name: name, Color: in.Color, Category: in.Category, ShowOnBoard: show})
		return err
	})
	return st, err
}

type StatusPatch struct {
	Name        *string  `json:"name"`
	Color       *string  `json:"color"`
	Category    *string  `json:"category"`
	ShowOnBoard *bool    `json:"show_on_board"`
	Position    *float64 `json:"position"`
}

func (s *Service) UpdateStatus(ctx context.Context, actor string, id int64, in StatusPatch) (st store.Status, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if st, err = o.tx.GetStatus(id); err != nil {
			return notFound("status %d not found", id)
		}
		old := st
		if in.Name != nil {
			if st.Name = strings.TrimSpace(*in.Name); st.Name == "" {
				return invalid("status name is required")
			}
		}
		if in.Color != nil {
			if err := checkStatusColor(*in.Color); err != nil {
				return err
			}
			st.Color = *in.Color
		}
		if in.Category != nil {
			if err := oneOf("category", *in.Category, []string{"todo", "doing", "done"}); err != nil {
				return err
			}
			st.Category = *in.Category
		}
		if in.ShowOnBoard != nil {
			st.ShowOnBoard = *in.ShowOnBoard
		}
		if in.Position != nil {
			st.Position = *in.Position
		}
		if err := o.tx.UpdateStatus(st); err != nil {
			return err
		}
		// Tasks follow the category: entering "done" stamps done_at, leaving it clears it.
		if (old.Category == "done") != (st.Category == "done") {
			if err := o.tx.SetDoneForStatus(id, st.Category == "done"); err != nil {
				return err
			}
		}
		o.emit("status_changed", 0, st.ProjectID)
		st, err = o.tx.GetStatus(id)
		return err
	})
	return st, err
}

// DeleteStatus removes a status. A status that holds tasks (trash included) needs moveTo, the status that takes them.
func (s *Service) DeleteStatus(ctx context.Context, actor string, id, moveTo int64) error {
	return s.write(ctx, actor, func(o *op) error {
		st, err := o.tx.GetStatus(id)
		if err != nil {
			return notFound("status %d not found", id)
		}
		all, err := o.tx.ListStatuses(st.ProjectID)
		if err != nil {
			return err
		}
		if len(all) < 2 {
			return invalid("a project needs at least one status")
		}
		n, err := o.tx.CountTasksInStatus(id)
		if err != nil {
			return err
		}
		if n > 0 {
			if moveTo == 0 {
				return &Error{Code: "status_in_use", Message: fmt.Sprintf("status %q holds %d tasks: choose a status to move them to (move_to)", st.Name, n)}
			}
			to, err := o.tx.GetStatus(moveTo)
			if err != nil || to.ProjectID != st.ProjectID || to.ID == id {
				return invalid("move_to must be another status of the same project")
			}
			if err := o.tx.MoveStatusTasks(id, to.ID, to.Category == "done"); err != nil {
				return err
			}
		}
		if err := o.tx.DeleteStatus(id); err != nil {
			return err
		}
		o.emit("status_changed", 0, st.ProjectID)
		return nil
	})
}

// ReorderStatuses sets the column order; ids must be exactly the project's statuses.
func (s *Service) ReorderStatuses(ctx context.Context, actor string, projectID int64, ids []int64) (out []store.Status, err error) {
	err = s.projectOp(ctx, actor, projectID, "status_changed", func(o *op) error {
		all, err := o.tx.ListStatuses(projectID)
		if err != nil {
			return err
		}
		byID := map[int64]store.Status{}
		for _, st := range all {
			byID[st.ID] = st
		}
		if len(ids) != len(all) {
			return invalid("send every status of the project exactly once")
		}
		for i, id := range ids {
			st, ok := byID[id]
			if !ok {
				return invalid("status %d does not belong to this project", id)
			}
			delete(byID, id)
			st.Position = float64(i+1) * 1024
			if err := o.tx.UpdateStatus(st); err != nil {
				return err
			}
		}
		out, err = o.tx.ListStatuses(projectID)
		return err
	})
	return out, err
}

// ---- labels ----

func (s *Service) ListLabels(ctx context.Context, projectID int64) (out []store.Label, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		if _, err := tx.GetProject(projectID); err != nil {
			return notFound("project %d not found", projectID)
		}
		out, err = tx.ListLabels(projectID)
		return err
	})
	return out, err
}

func checkLabelName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", invalid("label name is required")
	}
	if utf8.RuneCountInString(name) > 40 {
		return "", invalid("label name is longer than 40 characters")
	}
	return name, nil
}

func (s *Service) CreateLabel(ctx context.Context, actor string, projectID int64, name, color string) (l store.Label, err error) {
	err = s.projectOp(ctx, actor, projectID, "project_changed", func(o *op) error {
		if name, err = checkLabelName(name); err != nil {
			return err
		}
		if _, err := o.tx.GetLabelByName(projectID, name); err == nil {
			return invalid("label %q already exists", name)
		}
		if color == "" {
			if color, err = nextLabelColor(o.tx, projectID, name); err != nil {
				return err
			}
		} else if !colorRe.MatchString(color) {
			return invalid("color must look like #rrggbb")
		}
		l, err = o.tx.InsertLabel(store.Label{ProjectID: projectID, Name: name, Color: color})
		return err
	})
	return l, err
}

type LabelPatch struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

func (s *Service) UpdateLabel(ctx context.Context, actor string, id int64, in LabelPatch) (l store.Label, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		old, err := o.tx.GetLabel(id)
		if err != nil {
			return notFound("label %d not found", id)
		}
		l = old
		if in.Name != nil {
			if l.Name, err = checkLabelName(*in.Name); err != nil {
				return err
			}
			if x, err := o.tx.GetLabelByName(l.ProjectID, l.Name); err == nil && x.ID != id {
				return invalid("label %q already exists", l.Name)
			}
		}
		if in.Color != nil {
			if !colorRe.MatchString(*in.Color) {
				return invalid("color must look like #rrggbb")
			}
			l.Color = *in.Color
		}
		if err := o.tx.UpdateLabel(old, l); err != nil {
			return err
		}
		o.emit("project_changed", 0, l.ProjectID)
		l, err = o.tx.GetLabel(id)
		return err
	})
	return l, err
}

// DeleteLabel removes a label; its tasks keep existing with an empty area.
func (s *Service) DeleteLabel(ctx context.Context, actor string, id int64) error {
	return s.write(ctx, actor, func(o *op) error {
		l, err := o.tx.GetLabel(id)
		if err != nil {
			return notFound("label %d not found", id)
		}
		if err := o.tx.DeleteLabel(l); err != nil {
			return err
		}
		o.emit("project_changed", 0, l.ProjectID)
		return nil
	})
}

// ---- custom fields ----

func (s *Service) ListFields(ctx context.Context, projectID int64) (out []store.Field, err error) {
	err = s.read(ctx, func(tx *store.Tx) error {
		if _, err := tx.GetProject(projectID); err != nil {
			return notFound("project %d not found", projectID)
		}
		out, err = tx.ListFields(projectID)
		return err
	})
	return out, err
}

type FieldInput struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Options    []string `json:"options"`
	ShowOnCard bool     `json:"show_on_card"`
}

func cleanOptions(in []string) []string {
	out := []string{}
	for _, o := range in {
		if o = strings.TrimSpace(o); o != "" && !contains(out, o) {
			out = append(out, o)
		}
	}
	return out
}

func (s *Service) CreateField(ctx context.Context, actor string, projectID int64, in FieldInput) (f store.Field, err error) {
	err = s.projectOp(ctx, actor, projectID, "project_changed", func(o *op) error {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return invalid("field name is required")
		}
		if err := oneOf("type", in.Type, []string{"number", "url", "select"}); err != nil {
			return err
		}
		opts := cleanOptions(in.Options)
		if in.Type == "select" && len(opts) == 0 {
			return invalid("a select field needs at least one option")
		}
		if in.Type != "select" {
			opts = nil
		}
		fs, err := o.tx.ListFields(projectID)
		if err != nil {
			return err
		}
		for _, x := range fs {
			if x.Name == name {
				return invalid("field %q already exists", name)
			}
		}
		f, err = o.tx.InsertField(store.Field{ProjectID: projectID, Name: name, Type: in.Type, Options: opts, ShowOnCard: in.ShowOnCard})
		return err
	})
	return f, err
}

type FieldPatch struct {
	Name       *string   `json:"name"`
	Options    *[]string `json:"options"`
	ShowOnCard *bool     `json:"show_on_card"`
	Position   *float64  `json:"position"`
}

func (s *Service) UpdateField(ctx context.Context, actor string, id int64, in FieldPatch) (f store.Field, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if f, err = o.tx.GetField(id); err != nil {
			return notFound("field %d not found", id)
		}
		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if name == "" {
				return invalid("field name is required")
			}
			fs, err := o.tx.ListFields(f.ProjectID)
			if err != nil {
				return err
			}
			for _, x := range fs {
				if x.Name == name && x.ID != id {
					return invalid("field %q already exists", name)
				}
			}
			f.Name = name
		}
		if in.Options != nil && f.Type == "select" {
			if f.Options = cleanOptions(*in.Options); len(f.Options) == 0 {
				return invalid("a select field needs at least one option")
			}
		}
		if in.ShowOnCard != nil {
			f.ShowOnCard = *in.ShowOnCard
		}
		if in.Position != nil {
			f.Position = *in.Position
		}
		if err := o.tx.UpdateField(f); err != nil {
			return err
		}
		o.emit("project_changed", 0, f.ProjectID)
		f, err = o.tx.GetField(id)
		return err
	})
	return f, err
}

func (s *Service) DeleteField(ctx context.Context, actor string, id int64) error {
	return s.write(ctx, actor, func(o *op) error {
		f, err := o.tx.GetField(id)
		if err != nil {
			return notFound("field %d not found", id)
		}
		if err := o.tx.DeleteField(f); err != nil {
			return err
		}
		o.emit("project_changed", 0, f.ProjectID)
		return nil
	})
}

// ---- saved views ----

// ListViews returns the global views plus the views of projectID (0: only the global ones).
func (s *Service) ListViews(ctx context.Context, projectID int64) (out []store.View, err error) {
	err = s.read(ctx, func(tx *store.Tx) error { out, err = tx.ListViews(projectID); return err })
	return out, err
}

type ViewInput struct {
	ProjectID *int64          `json:"project_id"`
	Name      string          `json:"name"`
	Layout    string          `json:"layout"`
	Filter    json.RawMessage `json:"filter"`
	GroupBy   string          `json:"group_by"`
	Sort      string          `json:"sort"`
	Pinned    *bool           `json:"pinned"`
}

// ViewPatch fields are optional; Filter is replaced as a whole.
type ViewPatch struct {
	Name     *string         `json:"name"`
	Layout   *string         `json:"layout"`
	Filter   json.RawMessage `json:"filter"`
	GroupBy  *string         `json:"group_by"`
	Sort     *string         `json:"sort"`
	Pinned   *bool           `json:"pinned"`
	Position *float64        `json:"position"`
}

func checkFilter(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, invalid("filter must be a JSON object")
	}
	return raw, nil
}

func (s *Service) CreateView(ctx context.Context, actor string, in ViewInput) (v store.View, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return invalid("view name is required")
		}
		if in.Layout == "" {
			in.Layout = "board"
		}
		if err := oneOf("layout", in.Layout, []string{"board", "list"}); err != nil {
			return err
		}
		var pid int64
		if in.ProjectID != nil {
			if _, err := o.tx.GetProject(*in.ProjectID); err != nil {
				return invalid("unknown project %d", *in.ProjectID)
			}
			pid = *in.ProjectID
		}
		filter, err := checkFilter(in.Filter)
		if err != nil {
			return err
		}
		group, sort := in.GroupBy, in.Sort
		if group == "" {
			group = "status"
		}
		if sort == "" {
			sort = "manual"
		}
		v, err = o.tx.InsertView(store.View{ProjectID: in.ProjectID, Name: name, Layout: in.Layout, FilterJSON: filter,
			GroupBy: group, Sort: sort, Pinned: in.Pinned != nil && *in.Pinned})
		o.emit("project_changed", 0, pid)
		return err
	})
	return v, err
}

func (s *Service) UpdateView(ctx context.Context, actor string, id int64, in ViewPatch) (v store.View, err error) {
	err = s.write(ctx, actor, func(o *op) error {
		if v, err = o.tx.GetView(id); err != nil {
			return notFound("view %d not found", id)
		}
		if in.Name != nil {
			if v.Name = strings.TrimSpace(*in.Name); v.Name == "" {
				return invalid("view name is required")
			}
		}
		if in.Layout != nil {
			if err := oneOf("layout", *in.Layout, []string{"board", "list"}); err != nil {
				return err
			}
			v.Layout = *in.Layout
		}
		if len(in.Filter) > 0 {
			if v.FilterJSON, err = checkFilter(in.Filter); err != nil {
				return err
			}
		}
		if in.GroupBy != nil {
			v.GroupBy = *in.GroupBy
		}
		if in.Sort != nil {
			v.Sort = *in.Sort
		}
		if in.Pinned != nil {
			v.Pinned = *in.Pinned
		}
		if in.Position != nil {
			v.Position = *in.Position
		}
		if err := o.tx.UpdateView(v); err != nil {
			return err
		}
		var pid int64
		if v.ProjectID != nil {
			pid = *v.ProjectID
		}
		o.emit("project_changed", 0, pid)
		v, err = o.tx.GetView(id)
		return err
	})
	return v, err
}

func (s *Service) DeleteView(ctx context.Context, actor string, id int64) error {
	return s.write(ctx, actor, func(o *op) error {
		v, err := o.tx.GetView(id)
		if err != nil {
			return notFound("view %d not found", id)
		}
		if err := o.tx.DeleteView(id); err != nil {
			return err
		}
		var pid int64
		if v.ProjectID != nil {
			pid = *v.ProjectID
		}
		o.emit("project_changed", 0, pid)
		return nil
	})
}
