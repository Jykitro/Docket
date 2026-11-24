package selfhost

import (
	"crypto/rand"
	"encoding/json"
	"net/http"

	"docket/internal/auth"

	"github.com/go-webauthn/webauthn/webauthn"
)

// Registration reopens the account's current credential list when finishing on another replica.
type storedFlow struct {
	Origin  string
	Session *webauthn.SessionData
	Owner   auth.User
	Binding string
	Name    string
}

func (s *passkeyRequests) beginShared(w http.ResponseWriter, r *http.Request, f passkeyRequest, options any) {
	ctx := r.Context()
	data, err := json.Marshal(storedFlow{f.site.Config.RPOrigins[0], f.session, f.owner.User, f.binding, f.name})
	if err != nil {
		internalError(w, err)
		return
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		internalError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(720403252)"); err != nil {
		internalError(w, err)
		return
	}
	c := passkeyCookie(f.site)
	var old string
	if cookie, e := r.Cookie(c.Name); e == nil {
		old = auth.TokenHash(cookie.Value)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM vercel_flows WHERE expires<=clock_timestamp() OR key=$1", old); err != nil {
		internalError(w, err)
		return
	}
	c.Value = rand.Text()
	result, err := tx.ExecContext(ctx, "INSERT INTO vercel_flows(key,expires,flow) SELECT $1,clock_timestamp()+interval '5 minutes',$2::json WHERE (SELECT count(*) FROM vercel_flows)<1024", auth.TokenHash(c.Value), string(data))
	if err != nil {
		internalError(w, err)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		internalError(w, err)
		return
	}
	if n == 0 {
		writeError(w, 429, "rate_limited", "too many pending passkey requests")
		return
	}
	if err = tx.Commit(); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, c)
	writeJSON(w, 200, options)
}
func (s *passkeyRequests) takeShared(r *http.Request, site *webauthn.WebAuthn, key string) (passkeyRequest, bool) {
	var raw []byte
	ctx := r.Context()
	err := s.db.QueryRowContext(ctx, "DELETE FROM vercel_flows WHERE key=$1 AND expires>clock_timestamp() RETURNING flow", auth.TokenHash(key)).Scan(&raw)
	var f storedFlow
	if err != nil || json.Unmarshal(raw, &f) != nil || f.Session == nil || f.Origin != site.Config.RPOrigins[0] {
		return passkeyRequest{}, false
	}
	flow := passkeyRequest{site: site, session: f.Session, binding: f.Binding, name: f.Name}
	if f.Owner.ID != 0 {
		flow.owner, err = s.auth.RegistrationUser(ctx, f.Owner, site.Config.RPID)
		if err != nil {
			return passkeyRequest{}, false
		}
	}
	return flow, true
}
