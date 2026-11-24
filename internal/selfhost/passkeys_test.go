package selfhost

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasskeyTrustedOrigins(t *testing.T) {
	hosts := []string{"localhost", "127.0.0.1", "planner.example.com"}
	sites, err := PasskeySites(hosts, ":7777", "")
	if err != nil || len(sites) != 2 || sites["http://localhost:7777"] == nil || sites["https://planner.example.com"] == nil {
		t.Fatalf("unexpected sites: %v", err)
	}
	for _, origin := range []string{"http://planner.example.com", "https://unlisted.example.com", "https://planner.example.com/path", "https://127.0.0.1", "https://login@planner.example.com"} {
		if _, err := PasskeySites(hosts, ":7777", origin); err == nil {
			t.Errorf("accepted %s", origin)
		}
	}
	sites, err = PasskeySites(hosts, ":7777", "https://planner.example.com:8443")
	if err != nil || len(sites) != 1 {
		t.Fatalf("canonical site: %v", err)
	}
	h := handler{Options: Options{Passkeys: sites}}
	r := httptest.NewRequest("GET", "http://planner.example.com:8443/", nil)
	if h.passkeySite(r) != nil {
		t.Fatal("accepted public HTTP")
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if h.passkeySite(r) == nil {
		t.Fatal("public proxy origin unavailable")
	}
	r.Host = "planner.example.com:8444"
	if h.passkeySite(r) != nil {
		t.Fatal("accepted unconfigured port")
	}
}

func TestPasskeyChallengeSingleUse(t *testing.T) {
	sites, err := PasskeySites([]string{"localhost"}, ":7777", "")
	if err != nil {
		t.Fatal(err)
	}
	site := sites["http://localhost:7777"]
	var flows passkeyRequests
	begin := func() string {
		r := httptest.NewRequest("POST", "http://localhost:7777/", nil)
		w := httptest.NewRecorder()
		flows.begin(w, r, passkeyRequest{site: site}, nil)
		return w.Result().Cookies()[0].Value
	}
	take := func(token string) bool {
		r := httptest.NewRequest("POST", "http://localhost:7777/", nil)
		c := passkeyCookie(site)
		c.Value = token
		r.AddCookie(c)
		_, ok := flows.take(httptest.NewRecorder(), r, site)
		return ok
	}
	token := begin()
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if take(token) {
				successes.Add(1)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("challenge must be consumed exactly once")
	}
	token = begin()
	old := flows.pending[token]
	old.expires = time.Now().Add(-time.Second)
	flows.pending[token] = old
	if take(token) {
		t.Fatal("expired challenge accepted")
	}
}

func TestPasskeyEnrollmentRequiresSessionAndPassword(t *testing.T) {
	s := newServer(t)
	sites, err := PasskeySites(hosts, ":80", "")
	if err != nil {
		t.Fatal(err)
	}
	s.h = Handler(Options{Auth: s.db, WS: s.ws, AllowedHosts: hosts, Passkeys: sites, Web: http.NotFoundHandler()})
	owner, _ := s.addUser("owner")
	origin := map[string]string{"Origin": "http://localhost"}
	const begin = "/api/auth/passkeys/register/begin"
	valid := `{"name":"Laptop","password":"password-123"}`
	if rec := s.do("POST", begin, valid, nil, origin); rec.Code != 401 {
		t.Fatalf("anonymous enrollment: %d", rec.Code)
	}
	if rec := s.do("POST", begin, `{"name":"Laptop","password":"wrong"}`, owner, origin); rec.Code != 401 {
		t.Fatalf("unconfirmed enrollment: %d", rec.Code)
	}
	if rec := s.do("POST", begin, valid, owner, nil); rec.Code != 403 {
		t.Fatalf("missing origin: %d", rec.Code)
	}
	rec := s.do("POST", begin, valid, owner, origin)
	if rec.Code != 200 {
		t.Fatalf("confirmed enrollment: %d", rec.Code)
	}
	var creation struct {
		PublicKey struct {
			AuthenticatorSelection struct{ UserVerification, ResidentKey string }
		}
	}
	if err = json.Unmarshal(rec.Body.Bytes(), &creation); err != nil || creation.PublicKey.AuthenticatorSelection.UserVerification != "required" || creation.PublicKey.AuthenticatorSelection.ResidentKey != "required" {
		t.Fatal("enrollment must require resident keys and user verification")
	}
	rec = s.do("POST", "/api/auth/passkeys/login/begin", "{}", nil, origin)
	var login struct {
		PublicKey struct{ UserVerification string }
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &login) != nil || login.PublicKey.UserVerification != "required" {
		t.Fatal("login must require user verification")
	}
	if rec = s.do("POST", "/api/auth/passkeys/login/finish", "{}", nil, origin); rec.Code != 401 || sessionCookie(rec) != nil {
		t.Fatal("issued a session without verification")
	}
}
