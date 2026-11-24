package selfhost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"docket/internal/events"
	"docket/internal/service"
	"docket/internal/store"
)

func TestTrustedProxyChain(t *testing.T) {
	h := handler{Options: Options{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")}}}
	for _, tc := range []struct{ peer, header, client string }{
		{"192.0.2.8:81", "198.51.100.9", "192.0.2.8"},
		{"10.0.0.1:81", "192.0.2.8, 10.0.0.2", "192.0.2.8"},
		{"10.0.0.1:81", "203.0.113.9, 192.0.2.8, 10.0.0.2", "192.0.2.8"},
		{"10.0.0.1:81", "192.0.2.8, unknown", "10.0.0.1"},
		{"10.0.0.1:81", "", "10.0.0.1"},
		{"10.0.0.1:81", "::ffff:192.0.2.8", "192.0.2.8"},
	} {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = tc.peer
		req.Header.Set("X-Forwarded-For", tc.header)
		if got := h.clientIP(req); got != tc.client {
			t.Errorf("%s via %s: %s, want %s", tc.header, tc.peer, got, tc.client)
		}
	}
}

func TestSignupBudgetsBehindProxy(t *testing.T) {
	s := newServer(t)
	s.h = Handler(Options{Auth: s.db, WS: s.ws, Web: http.NotFoundHandler(), AllowedHosts: hosts, TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}})
	client := map[string]string{"X-Forwarded-For": "192.0.2.8"}
	for range 20 {
		if rec := s.do("POST", "/api/auth/register", "{}", nil, client); rec.Code != 403 {
			t.Fatalf("initial signup status %d", rec.Code)
		}
	}
	if rec := s.do("POST", "/api/auth/register", "{}", nil, client); rec.Code != 429 {
		t.Fatalf("signup limit status %d", rec.Code)
	}
	if rec := s.do("POST", "/api/auth/register", "{}", nil, map[string]string{"X-Forwarded-For": "192.0.2.9"}); rec.Code != 403 {
		t.Fatalf("other client signup status %d", rec.Code)
	}
	if rec := s.do("POST", "/api/auth/recover", "{}", nil, client); rec.Code != 401 {
		t.Fatalf("separate recovery status %d", rec.Code)
	}
}

func TestConfirmedRoutesShareOwnerBudget(t *testing.T) {
	s := newServer(t)
	c, _ := s.addUser("owner")
	for i, route := range []struct{ method, path, body string }{
		{"POST", "/api/auth/recovery-code", `{"password":"wrong"}`},
		{"PUT", "/api/auth/password", `{"old":"wrong","new":"other-password"}`},
		{"DELETE", "/api/auth/account", `{"password":"wrong"}`},
		{"DELETE", "/api/auth/passkeys/unknown", `{"password":"wrong"}`},
		{"POST", "/api/auth/recovery-code", `{"password":"wrong"}`},
		{"POST", "/api/auth/recovery-code", `{"password":"password-123"}`},
	} {
		want := 401
		if i == 5 {
			want = 429
		}
		if rec := s.do(route.method, route.path, route.body, c, nil); rec.Code != want {
			t.Fatalf("%s status %d, want %d", route.path, rec.Code, want)
		}
	}
}

func TestWorkspaceIdentityHeader(t *testing.T) {
	s := newServer(t)
	_, first := s.addUser("first")
	c, second := s.addUser("second")
	hdr := map[string]string{"X-Docket-User": strconv.FormatInt(first.ID, 10)}
	for _, route := range []struct{ method, path string }{{"GET", "/api/projects"}, {"POST", "/api/auth/recovery-code"}, {"POST", "/api/auth/logout"}} {
		rec := s.do(route.method, route.path, `{"password":"password-123"}`, c, hdr)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), "session_changed") {
			t.Fatalf("%s returned %d", route.path, rec.Code)
		}
	}
	hdr["X-Docket-User"] = strconv.FormatInt(second.ID, 10)
	if rec := s.do("GET", "/api/projects", "", c, hdr); rec.Code != 200 {
		t.Fatalf("matching user status %d", rec.Code)
	}
}

func TestMCPTokenIndexDoesNotReadUnrelatedWorkspaces(t *testing.T) {
	s := newServer(t)
	_, a := s.addUser("first")
	_, b := s.addUser("second")
	token := bearerFor(t, b)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- a.st.WithTx(context.Background(), func(*store.Tx) error { close(entered); <-release; return nil })
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for _, tc := range []struct {
		bearer string
		want   int
	}{{"Bearer invalid", 401}, {token, 200}} {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(mcpListProjects))
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		req = req.WithContext(ctx)
		req.Host = "localhost"
		req.Header.Set("Content-Type", "application/json")
		for k, v := range withAuth(mcpHdr, tc.bearer) {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, req)
		late := ctx.Err()
		cancel()
		if late != nil || rec.Code != tc.want {
			t.Fatalf("unrelated workspace delayed lookup: status %d, deadline %v", rec.Code, late)
		}
	}
}

func TestLegacyAndMixedOwnersRequireReconciliation(t *testing.T) {
	ctx := context.Background()
	s := newServer(t)
	_, ws := s.addUser("owner")
	users, err := s.db.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	owner := users[0]
	mismatch := owner
	mismatch.WorkspaceKey = "independent-catalog"
	if _, err = s.ws.Open(ctx, mismatch); err == nil {
		t.Fatal("same ID and name from another catalog accepted")
	}
	if err = ws.st.WithTx(ctx, func(tx *store.Tx) error { return tx.SetSetting("selfhost.generation", "") }); err != nil {
		t.Fatal(err)
	}
	s.ws.Close()
	if err = s.ws.VerifyOwners(ctx, users, s.db); err == nil {
		t.Fatal("populated legacy storage was silently adopted")
	}
	legacy := owner
	legacy.WorkspaceKey = ""
	if _, err = s.ws.Open(ctx, legacy); err == nil {
		t.Fatal("unbound catalog accepted")
	}
}

func TestDeletionReplayNeedsItsWorkspaceConfirmation(t *testing.T) {
	ctx := context.Background()
	s := newServer(t)
	s.addUser("admin")
	_, owner := s.addUser("owner")
	if err := s.db.DeleteUser(owner.ID, "not-in-this-workspace"); err != nil {
		t.Fatal(err)
	}
	users, err := s.db.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	s.ws.Close()
	if err = s.ws.VerifyOwners(ctx, users, s.db); err == nil {
		t.Fatal("incomplete deletion confirmation accepted")
	}
	ids, err := s.ws.persistedIDs(ctx, s.db)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == owner.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("workspace removed during failed restore")
	}
	pending, err := s.db.PendingDeletions(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending intent lost: %v", err)
	}
}

func TestOwnerRotationEndsActiveBearer(t *testing.T) {
	entered := make(chan struct{})
	s := newServerWith(t, func(context.Context, *service.Service, *events.Bus) http.Handler {
		return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() })
	})
	_, ws := s.addUser("owner")
	old := bearerFor(t, ws)
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		req := httptest.NewRequest("POST", "/mcp", nil).WithContext(ctx)
		req.Host = "localhost"
		req.Header.Set("Authorization", old)
		s.h.ServeHTTP(httptest.NewRecorder(), req)
		close(done)
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("bearer did not reach workspace")
	}
	if _, err := ws.Svc.RotateMCPToken(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("rotation left old bearer active")
	}
	if rec := s.do("POST", "/mcp", "{}", nil, withAuth(mcpHdr, old)); rec.Code != 401 {
		t.Fatal("rotated bearer accepted")
	}
}
