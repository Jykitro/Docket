package selfhost

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"plandillo/internal/auth"
)

func TestRegisteredRedirect(t *testing.T) {
	allowed := []string{"http://127.0.0.1:10001/callback?x=1", "https://client.example/callback"}
	for _, uri := range []string{allowed[0], allowed[1], "http://127.0.0.1:25001/callback?x=1"} {
		if !registeredRedirect(allowed, uri) {
			t.Errorf("rejected registered callback %s", uri)
		}
	}
	for _, uri := range []string{"https://client.example:8443/callback", "http://localhost:10001/callback?x=1", "http://127.0.0.1:25001/callback", "http://127.0.0.1:25001/other?x=1", "http://127.0.0.1:25001/callback?x=1#fragment"} {
		if registeredRedirect(allowed, uri) {
			t.Errorf("accepted unregistered callback %s", uri)
		}
	}
}

func TestBrowserOAuthOverMCP(t *testing.T) {
	s := sharedServer(t)
	alice, space := s.addUser("alice")
	bob, other := s.addUser("bob")
	for _, fixture := range []struct {
		cookie *http.Cookie
		id     int64
		name   string
	}{{alice, space.ID, "Alice board"}, {bob, other.ID, "Bob board"}} {
		if r := s.do("POST", fmt.Sprintf("/api/workspaces/%d/projects", fixture.id), fmt.Sprintf(`{"name":%q,"key":"AB"}`, fixture.name), fixture.cookie, nil); r.Code != 201 {
			t.Fatal("fixture project", r.Code)
		}
	}
	const origin = "http://localhost"
	const redirect = "http://127.0.0.1:44444/callback"
	const verifier = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	sum := sha256.Sum256([]byte(verifier))
	digest := base64.RawURLEncoding.EncodeToString(sum[:])
	reg := s.do("POST", "/oauth/register", `{"client_name":"Test client","redirect_uris":["http://127.0.0.1:44444/callback"],"token_endpoint_auth_method":"none"}`, nil, nil)
	var client struct {
		ID string `json:"client_id"`
	}
	if reg.Code != 201 || json.Unmarshal(reg.Body.Bytes(), &client) != nil || client.ID == "" {
		t.Fatal("registration failed")
	}
	grant := func(endpoint string, write bool) string {
		t.Helper()
		values := url.Values{"client_id": {client.ID}, "code_challenge_method": {"S256"}, "code_challenge": {digest}, "redirect_uri": {redirect}, "resource": {origin + endpoint}, "response_type": {"code"}, "state": {"client-state"}}
		path := "/api/auth/oauth/decision?" + values.Encode()
		body := fmt.Sprintf(`{"allow":true,"workspace":%d,"write":%t}`, space.ID, write)
		for _, tc := range []struct {
			cookie       *http.Cookie
			origin, body string
			status       int
		}{
			{nil, origin, body, 401}, {alice, "https://elsewhere.example", body, 403}, {alice, origin, fmt.Sprintf(`{"allow":true,"workspace":%d}`, other.ID), 404},
		} {
			res := s.do("POST", path, tc.body, tc.cookie, map[string]string{"Origin": tc.origin})
			if res.Code != tc.status {
				t.Fatalf("consent validation: %d, want %d", res.Code, tc.status)
			}
		}
		decision := func(body string) *url.URL {
			res := s.do("POST", path, body, alice, map[string]string{"Origin": origin})
			var got struct{ Redirect string }
			if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &got) != nil {
				t.Fatalf("decision HTTP %d", res.Code)
			}
			u, err := url.Parse(got.Redirect)
			if err != nil || u.Scheme+"://"+u.Host+u.Path != redirect || u.Query().Get("state") != "client-state" || u.Query().Get("iss") != origin {
				t.Fatal("incorrect callback")
			}
			return u
		}
		if u := decision(`{"allow":false}`); u.Query().Get("error") != "access_denied" || u.Query().Has("code") {
			t.Fatal("deny returned a code")
		}
		return decision(body).Query().Get("code")
	}
	exchange := func(form url.Values, status int) auth.OAuthTokens {
		t.Helper()
		form.Set("client_id", client.ID)
		r := s.do("POST", "/oauth/token", form.Encode(), nil, map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if r.Code != status {
			t.Fatalf("exchange HTTP %d, want %d", r.Code, status)
		}
		var out auth.OAuthTokens
		if status == 200 && (json.Unmarshal(r.Body.Bytes(), &out) != nil || out.AccessToken == "" || out.RefreshToken == "") {
			t.Fatal("tokens missing")
		}
		return out
	}
	mcp := func(endpoint, token, rpc string, status int) string {
		t.Helper()
		r := s.do("POST", endpoint, rpc, nil, map[string]string{"Accept": "application/json, text/event-stream", "Authorization": "Bearer " + token})
		if r.Code != status {
			t.Fatalf("MCP %s HTTP %d, want %d", endpoint, r.Code, status)
		}
		if status == 401 && !strings.Contains(r.Header().Get("WWW-Authenticate"), "/.well-known/oauth-protected-resource"+endpoint) {
			t.Fatal("missing discovery challenge")
		}
		return r.Body.String()
	}
	const list = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	for _, endpoint := range []string{"/mcp", "/mcp/code"} {
		mcp(endpoint, "", list, 401)
		r := s.do("GET", "/.well-known/oauth-protected-resource"+endpoint, "", nil, nil)
		if !strings.Contains(r.Body.String(), origin+endpoint) {
			t.Fatal("wrong discovery resource")
		}
		code := grant(endpoint, false)
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {strings.Repeat("x", 50)}}
		exchange(form, 400)
		form.Set("code_verifier", verifier)
		form.Set("redirect_uri", redirect+"x")
		exchange(form, 400)
		form.Set("redirect_uri", redirect)
		tok := exchange(form, 200)
		exchange(form, 400)
		tools := mcp(endpoint, tok.AccessToken, list, 200)
		if strings.Contains(tools, "create_tasks(") || strings.Contains(tools, `"name":"create_tasks"`) {
			t.Fatal("read-only grant exposed writes")
		}
		if !strings.Contains(tools, "list_projects") {
			t.Fatal("missing read tools")
		}
		read, write := mcpListProjects, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_project","arguments":{"name":"Test","key":"TE"}}}`
		if endpoint == "/mcp/code" {
			read = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code","arguments":{"code":"return await codemode.list_projects({})"}}}`
			write = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code","arguments":{"code":"return await codemode.create_project({name:'Test',key:'TE'})"}}}`
		}
		if result := mcp(endpoint, tok.AccessToken, read, 200); !strings.Contains(result, "Alice board") || strings.Contains(result, "Bob board") {
			t.Fatalf("workspace isolation on %s", endpoint)
		}
		if result := mcp(endpoint, tok.AccessToken, write, 200); !strings.Contains(result, "forbidden") {
			t.Fatalf("read-only write on %s: %.300s", endpoint, result)
		}
		calls, err := space.Svc.ListMCPCalls(t.Context(), "all", 1)
		if err != nil || len(calls) != 1 || calls[0].Actor != "test-client" {
			t.Fatal("missing OAuth client attribution", err)
		}
		otherEndpoint := "/mcp"
		if endpoint == otherEndpoint {
			otherEndpoint = "/mcp/code"
		}
		mcp(otherEndpoint, tok.AccessToken, list, 401)
		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}}
		refresh.Set("scope", "mcp:read mcp:write")
		exchange(refresh, 400)
		refresh.Set("scope", "mcp:read")
		next := exchange(refresh, 200)
		mcp(endpoint, tok.AccessToken, list, 401)
		mcp(endpoint, next.AccessToken, list, 200)
		exchange(refresh, 400)
		mcp(endpoint, next.AccessToken, list, 401)
	}
	tok := exchange(url.Values{"grant_type": {"authorization_code"}, "code": {grant("/mcp", true)}, "redirect_uri": {redirect}, "code_verifier": {verifier}}, 200)
	if !strings.Contains(mcp("/mcp", tok.AccessToken, list, 200), `"name":"create_tasks"`) {
		t.Fatal("approved writes missing")
	}
	rows, err := s.db.OAuthConnections(t.Context(), space.OwnerID, space.ID)
	if err != nil || len(rows) != 1 {
		t.Fatal("missing connected application")
	}
	if r := s.do("DELETE", "/api/auth/oauth/connections/"+rows[0].ID, "", alice, nil); r.Code != 204 {
		t.Fatal("revocation failed")
	}
	mcp("/mcp", tok.AccessToken, list, 401)
	exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}}, 400)
}
