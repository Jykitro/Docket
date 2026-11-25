package auth

import (
	"errors"
	"testing"
	"time"
)

func TestOAuthExpiryAndRotation(t *testing.T) {
	d := openTest(t)
	u, err := mkUser(d, "owner", "longenough1", false)
	if err != nil {
		t.Fatal(err)
	}
	var ws int64
	if err := d.db.QueryRow("SELECT id FROM workspaces WHERE owner_user_id=?", u.ID).Scan(&ws); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	d.now = func() time.Time { return now }
	ctx := t.Context()
	const origin = "https://planner.example"
	req := OAuthRequest{ClientID: "native", Resource: origin + "/mcp", Scope: "mcp:read mcp:write", RedirectURI: "http://localhost:4567/callback", Challenge: "fixture"}
	code := func() string {
		t.Helper()
		value, err := d.AuthorizeOAuth(ctx, u.ID, ws, req)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	redeem := func(value string) OAuthTokens {
		t.Helper()
		tokens, err := d.ExchangeOAuth(ctx, origin, false, value, req)
		if err != nil {
			t.Fatal(err)
		}
		return tokens
	}
	expired := code()
	now = now.Add(5 * time.Minute)
	if _, err := d.ExchangeOAuth(ctx, origin, false, expired, req); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired code accepted", err)
	}
	start := now
	tok := redeem(code())
	now = now.Add(OAuthAccessTTL)
	if _, err := d.OAuthAccess(ctx, tok.AccessToken, req.Resource); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired access accepted", err)
	}
	now = start.Add(30*24*time.Hour - time.Minute)
	narrow := OAuthRequest{ClientID: req.ClientID, Scope: "mcp:read"}
	next, err := d.ExchangeOAuth(ctx, origin, true, tok.RefreshToken, narrow)
	if err != nil || next.Scope != "mcp:read" || next.ExpiresIn != 60 {
		t.Fatal("refresh lifetime or scope incorrect", err)
	}
	a, err := d.OAuthAccess(ctx, next.AccessToken, req.Resource)
	if err != nil || a.Until != now.Add(time.Minute).Unix() {
		t.Fatal("access outlives grant", err)
	}
	now = now.Add(time.Minute)
	if _, err := d.ExchangeOAuth(ctx, origin, true, next.RefreshToken, narrow); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired refresh accepted", err)
	}
	tok = redeem(code())
	if _, err := d.db.ExecContext(ctx, d.db.SQL("UPDATE workspaces SET token_digest=? WHERE id=?"), TokenHash("rotated"), ws); err != nil {
		t.Fatal(err)
	}
	if _, err := d.OAuthAccess(ctx, tok.AccessToken, req.Resource); !errors.Is(err, ErrNotFound) {
		t.Fatal("rotation left access active", err)
	}
	if _, err := d.ExchangeOAuth(ctx, origin, true, tok.RefreshToken, narrow); !errors.Is(err, ErrNotFound) {
		t.Fatal("rotation left refresh active", err)
	}
}
