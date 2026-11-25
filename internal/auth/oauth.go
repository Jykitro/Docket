package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"
)

var ErrOAuthScope = errors.New("invalid OAuth scope")

const OAuthAccessTTL = 15 * time.Minute

type OAuthRequest struct {
	ClientID, ClientName, Resource, RedirectURI, Scope, Challenge string
}

type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (d *DB) AuthorizeOAuth(ctx context.Context, owner, space int64, req OAuthRequest) (string, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := d.now()
	if _, err = tx.Exec("DELETE FROM oauth_grants WHERE expires<=?", now.Unix()); err != nil {
		return "", err
	}
	code := rand.Text() + rand.Text()
	// Capture the live workspace credential generation. Recovery, blocking and token
	// rotation invalidate the grant even if the workspace is later reactivated.
	res, err := tx.Exec(`INSERT INTO oauth_grants (id,workspace_id,credential_epoch,client_id,client_name,resource,redirect_uri,scope,challenge,code_hash,expires)
 SELECT ?,w.id,COALESCE(w.token_digest,''),?,?,?,?,?,?,?,? FROM workspaces w JOIN users u ON u.id=w.owner_user_id
 WHERE w.id=? AND u.id=? AND u.is_active=1 AND w.archived_at IS NULL`, rand.Text(), req.ClientID, req.ClientName, req.Resource, req.RedirectURI, req.Scope, req.Challenge, TokenHash(code), now.Add(5*time.Minute).Unix(), space, owner)
	if err != nil {
		return "", err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return "", errors.Join(ErrNotFound, err)
	}
	return code, tx.Commit()
}

const liveOAuthWorkspace = `EXISTS (SELECT 1 FROM workspaces w JOIN users u ON u.id=w.owner_user_id
 WHERE w.id=oauth_grants.workspace_id AND w.archived_at IS NULL AND u.is_active=1 AND COALESCE(w.token_digest,'')=oauth_grants.credential_epoch)`

// ExchangeOAuth consumes a code (with its S256 proof) or rotates a refresh token in
// one transaction. The conditional update serializes exchanges on every replica.
func (d *DB) ExchangeOAuth(ctx context.Context, issuer string, refresh bool, token string, req OAuthRequest) (OAuthTokens, error) {
	var out OAuthTokens
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	now := d.now()
	out = OAuthTokens{AccessToken: "plo_" + rand.Text() + rand.Text(), RefreshToken: "plr_" + rand.Text() + rand.Text(), TokenType: "Bearer", ExpiresIn: int(OAuthAccessTTL.Seconds())}
	set := "access_hash=?,refresh_hash=?,access_until=?"
	args := []any{TokenHash(out.AccessToken), TokenHash(out.RefreshToken), now.Add(OAuthAccessTTL).Unix()}
	condition := "refresh_hash=?"
	hash := TokenHash(token)
	if !refresh {
		set += ",code_hash=NULL,expires=?"
		args = append(args, now.Add(30*24*time.Hour).Unix())
		condition = "code_hash=? AND redirect_uri=? AND challenge=?"
	}
	args = append(args, hash)
	if !refresh {
		args = append(args, req.RedirectURI, req.Challenge)
	}
	args = append(args, req.ClientID, now.Unix(), issuer+"/mcp", issuer+"/mcp/code")
	condition += " AND client_id=? AND expires>? AND resource IN (?,?) AND " + liveOAuthWorkspace
	if req.Resource != "" {
		condition += " AND resource=?"
		args = append(args, req.Resource)
	}
	var grant string
	var expires int64
	err = tx.QueryRow("UPDATE oauth_grants SET "+set+" WHERE "+condition+" RETURNING id,scope,expires", args...).Scan(&grant, &out.Scope, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		if refresh {
			if _, err = tx.Exec("DELETE FROM oauth_grants WHERE client_id=? AND id IN (SELECT grant_id FROM oauth_used_refresh WHERE token_hash=?)", req.ClientID, hash); err != nil {
				return OAuthTokens{}, err
			}
			if err = tx.Commit(); err != nil {
				return OAuthTokens{}, err
			}
		}
		return OAuthTokens{}, ErrNotFound
	}
	if err != nil {
		return OAuthTokens{}, err
	}
	out.ExpiresIn = min(out.ExpiresIn, int(expires-now.Unix()))
	if refresh {
		if req.Scope != "" {
			approved := strings.Fields(out.Scope)
			requested := strings.Fields(req.Scope)
			if !slices.Contains(requested, "mcp:read") {
				return OAuthTokens{}, ErrOAuthScope
			}
			for _, scope := range requested {
				if !slices.Contains(approved, scope) {
					return OAuthTokens{}, ErrOAuthScope
				}
			}
			out.Scope = strings.Join(requested, " ")
			if _, err = tx.Exec("UPDATE oauth_grants SET scope=? WHERE id=?", out.Scope, grant); err != nil {
				return OAuthTokens{}, err
			}
		}
		if _, err = tx.Exec("INSERT INTO oauth_used_refresh (token_hash,grant_id) VALUES (?,?)", hash, grant); err != nil {
			return OAuthTokens{}, err
		}
	}
	return out, tx.Commit()
}

type OAuthAccess struct {
	Owner, Workspace      int64
	ID, ClientName, Scope string
	Until                 int64
}

func (d *DB) OAuthAccess(ctx context.Context, token, resource string) (a OAuthAccess, err error) {
	now := d.now().Unix()
	err = d.db.QueryRowContext(ctx, d.db.SQL(`SELECT w.owner_user_id,workspace_id,oauth_grants.id,client_name,scope,CASE WHEN expires<access_until THEN expires ELSE access_until END FROM oauth_grants
 JOIN workspaces w ON w.id=workspace_id WHERE access_hash=? AND resource=? AND access_until>? AND expires>? AND `+liveOAuthWorkspace), TokenHash(token), resource, now, now).Scan(&a.Owner, &a.Workspace, &a.ID, &a.ClientName, &a.Scope, &a.Until)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return
}

type OAuthConnection struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

func (d *DB) OAuthConnections(ctx context.Context, owner, workspace int64) ([]OAuthConnection, error) {
	rows, err := d.db.QueryContext(ctx, d.db.SQL(`SELECT g.id,g.client_name,g.scope FROM oauth_grants g JOIN workspaces w ON w.id=g.workspace_id WHERE w.owner_user_id=? AND w.id=? AND g.access_hash IS NOT NULL AND g.expires>? AND g.credential_epoch=COALESCE(w.token_digest,'') ORDER BY g.client_name,g.id`), owner, workspace, d.now().Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OAuthConnection{}
	for rows.Next() {
		var c OAuthConnection
		if err := rows.Scan(&c.ID, &c.Name, &c.Scope); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (d *DB) RevokeOAuth(ctx context.Context, owner int64, id string) error {
	_, err := d.db.ExecContext(ctx, d.db.SQL("DELETE FROM oauth_grants WHERE id=? AND workspace_id IN (SELECT id FROM workspaces WHERE owner_user_id=?)"), id, owner)
	return err
}
