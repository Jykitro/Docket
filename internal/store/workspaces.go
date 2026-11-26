package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"docket/internal/database"
)

// Space is the catalog record. Service handles are created on demand for active spaces.
type Space struct {
	ID         int64  `json:"id"`
	OwnerID    int64  `json:"owner_user_id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Icon       string `json:"icon"`
	HasIcon    bool   `json:"has_icon"`
	Generation string `json:"-"`
	ArchivedAt *int64 `json:"archived_at"`
	CreatedAt  int64  `json:"created_at"`
	UpdatedAt  int64  `json:"updated_at"`
}

var ErrSpaceInput = errors.New("invalid workspace name, icon or action")
var ErrLastSpace = errors.New("keep at least one active workspace")

const spaceColumns = "id,owner_user_id,name,slug,icon,icon_png IS NOT NULL,generation,archived_at,created_at,updated_at"

func scanSpace(row scanner) (v Space, err error) {
	err = notFoundIf(row.Scan(&v.ID, &v.OwnerID, &v.Name, &v.Slug, &v.Icon, &v.HasIcon, &v.Generation, &v.ArchivedAt, &v.CreatedAt, &v.UpdatedAt))
	return
}
func (s *Store) Spaces(ctx context.Context, owner int64) ([]Space, error) {
	// The workspace reads below run on every routed request, so they use the read pool (database.DB.BeginReadTx).
	rows, err := s.db.ReadQueryContext(ctx, "SELECT "+spaceColumns+" FROM workspaces WHERE owner_user_id=? ORDER BY archived_at IS NOT NULL,id", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Space{}
	for rows.Next() {
		v, e := scanSpace(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) Space(ctx context.Context, owner, id int64) (Space, error) {
	q := "SELECT " + spaceColumns + " FROM workspaces WHERE owner_user_id=? AND archived_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=owner_user_id AND is_active=1)"
	if id == 0 {
		return scanSpace(s.db.ReadQueryRowContext(ctx, q+" ORDER BY id LIMIT 1", owner))
	}
	return scanSpace(s.db.ReadQueryRowContext(ctx, q+" AND id=?", owner, id))
}
// SpaceRef is Space by the reference an address carries: the numeric ID or the slug ("" is the first workspace).
// Slugs are never all digits (database.Tx.SpaceSlug), so the two cannot be confused.
func (s *Store) SpaceRef(ctx context.Context, owner int64, ref string) (Space, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 || ref == "" {
		return s.Space(ctx, owner, id)
	}
	q := "SELECT " + spaceColumns + " FROM workspaces WHERE owner_user_id=? AND slug=? AND archived_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=owner_user_id AND is_active=1)"
	return scanSpace(s.db.ReadQueryRowContext(ctx, q, owner, ref))
}
func (s *Store) SpaceByToken(ctx context.Context, digest string) (Space, error) {
	return scanSpace(s.db.ReadQueryRowContext(ctx, "SELECT "+spaceColumns+" FROM workspaces WHERE token_digest=? AND archived_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE users.id=owner_user_id AND is_active=1)", digest))
}
func (s *Store) SpaceIcon(owner, id int64) ([]byte, error) {
	var png []byte
	err := s.db.ReadQueryRowContext(context.Background(), "SELECT icon_png FROM workspaces WHERE owner_user_id=? AND id=? AND icon_png IS NOT NULL", owner, id).Scan(&png)
	return png, notFoundIf(err)
}

type SpaceChange struct {
	Name   *string `json:"name"`
	Icon   *string `json:"icon"`
	PNG    *[]byte `json:"icon_png"`
	Action string  `json:"action"`
}

func (s *Store) ChangeSpace(ctx context.Context, owner, id int64, in SpaceChange) (Space, error) {
	var v Space
	tx, err := s.db.BeginOwnerTx(ctx, owner)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	// Acquire the writer before checking the last-space invariant, on both engines.
	if _, err = tx.Exec("UPDATE users SET updated_at=updated_at WHERE id=? AND is_active=1", owner); err != nil {
		return v, err
	}
	now := time.Now().Unix()
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if n := utf8.RuneCountInString(name); n == 0 || n > 80 {
			return v, ErrSpaceInput
		}
		in.Name = &name
	}
	if in.Icon != nil && (len(*in.Icon) > 32 || *in.Icon == "") {
		return v, ErrSpaceInput
	}
	if id == 0 {
		if in.Name == nil || in.Action != "" {
			return v, ErrSpaceInput
		}
		var slug string
		if slug, err = tx.SpaceSlug(owner, *in.Name); err != nil {
			return v, err
		}
		err = tx.QueryRow("INSERT INTO workspaces(owner_user_id,name,slug,generation,created_at,updated_at) VALUES (?,?,?,?,?,?) RETURNING id", owner, *in.Name, slug, rand.Text(), now, now).Scan(&id)
		if err != nil {
			return v, err
		}
	}
	v, err = scanSpace(tx.QueryRow("SELECT "+spaceColumns+" FROM workspaces WHERE id=? AND owner_user_id=?", id, owner))
	if err != nil {
		return v, err
	}
	switch in.Action {
	case "archive":
		if v.ArchivedAt == nil {
			var other bool
			if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM workspaces WHERE owner_user_id=? AND id<>? AND archived_at IS NULL)", owner, id).Scan(&other); err != nil {
				return v, err
			}
			if !other {
				return v, ErrLastSpace
			}
		}
		if _, err = tx.Exec("DELETE FROM oauth_grants WHERE workspace_id=?", id); err != nil {
			return v, err
		}
		_, err = tx.Exec("UPDATE workspaces SET archived_at=?,updated_at=?,mcp_token=NULL,token_digest=NULL WHERE id=?", now, now, id)
	case "restore":
		_, err = tx.Exec("UPDATE workspaces SET archived_at=NULL,updated_at=? WHERE id=?", now, id)
	case "delete":
		if v.ArchivedAt == nil || in.Name == nil || *in.Name != v.Name {
			return v, ErrSpaceInput
		}
		_, err = tx.Exec("DELETE FROM workspaces WHERE id=? AND owner_user_id=? AND archived_at IS NOT NULL", id, owner)
	case "":
		if v.ArchivedAt != nil {
			return v, ErrSpaceInput
		}
		_, err = tx.Exec("UPDATE workspaces SET name=coalesce(?,name),icon=coalesce(?,icon),updated_at=? WHERE id=?", in.Name, in.Icon, now, id)
		if err == nil && (in.PNG != nil || in.Icon != nil) {
			var data []byte
			if in.PNG != nil {
				data = *in.PNG
			}
			_, err = tx.Exec("UPDATE workspaces SET icon_png=? WHERE id=?", data, id)
		}
	default:
		return v, ErrSpaceInput
	}
	if err != nil {
		return v, err
	}
	if in.Action != "delete" {
		v, err = scanSpace(tx.QueryRow("SELECT "+spaceColumns+" FROM workspaces WHERE id=?", id))
		if err != nil {
			return v, err
		}
	}
	return v, tx.Commit()
}

func EnsureLocalUser(db *database.DB) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow("SELECT id FROM users WHERE username='local'").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().Unix()
		err = tx.QueryRow("INSERT INTO users(username,name,role,created_at,updated_at) VALUES ('local','','user',?,?) RETURNING id", now, now).Scan(&id)
		if err == nil {
			_, err = tx.Exec("INSERT INTO workspaces(owner_user_id,name,slug,generation,created_at,updated_at) VALUES (?,'Personal','personal',?,?,?)", id, rand.Text(), now, now)
		}
	}
	if err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
