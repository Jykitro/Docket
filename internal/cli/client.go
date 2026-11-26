package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"plandillo/internal/toolcatalog"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// conn is one MCP session and what is needed to explain its failure with an exit code.
type conn struct {
	*mcp.ClientSession
	base  string
	probe *probe
	src   *savingSource // nil unless a saved browser sign-in is used
}

// probe remembers how the last HTTP exchange failed, because the MCP client reports it only as text.
type probe struct {
	next   http.RoundTripper
	status int
	err    error
}

func (p *probe) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := p.next.RoundTrip(r)
	if err != nil {
		p.err = err
		return nil, err
	}
	if resp.StatusCode >= 400 {
		p.status = resp.StatusCode
	}
	return resp, nil
}

// savingSource refreshes a browser sign-in and saves each new token: the server rotates refresh tokens.
type savingSource struct {
	mu    sync.Mutex
	src   oauth2.TokenSource
	grant *oauthGrant
	save  func() error
	warn  func(error)
	err   error
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.src.Token()
	if err != nil {
		s.err = err
		return nil, err
	}
	if t.AccessToken != s.grant.Token.AccessToken {
		s.grant.Token = t
		if err := s.save(); err != nil {
			s.warn(err)
		}
	}
	return t, nil
}

// open connects to path (/mcp or /mcp/code) with PLANDILLO_TOKEN or the saved sign-in for this board.
func (a *app) open(ctx context.Context, path string) (*conn, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	base, err := a.target(cfg)
	if err != nil {
		return nil, err
	}
	var ts oauth2.TokenSource
	var src *savingSource
	if tok := a.getenv("PLANDILLO_TOKEN"); tok != "" {
		ts = staticToken(tok)
	} else if cfg.URL == base { // never send a saved credential to another board
		if cfg.Token != "" {
			ts = staticToken(cfg.Token)
		} else if g := cfg.OAuth[path]; g != nil && g.Token != nil {
			oc := &oauth2.Config{ClientID: g.ClientID, ClientSecret: g.ClientSecret, Endpoint: oauth2.Endpoint{TokenURL: g.TokenURL, AuthStyle: g.AuthStyle}}
			src = &savingSource{src: oc.TokenSource(context.WithoutCancel(ctx), g.Token), grant: g,
				save: func() error { return a.saveConfig(cfg) },
				warn: func(err error) {
					fmt.Fprintf(a.stderr, "plandillo-cli: warning: the refreshed sign-in was not saved, run login again later: %v\n", err)
				}}
			ts = src
		}
	}
	c, err := a.dial(ctx, base, path, ts, nil)
	c.src = src
	if err != nil {
		return nil, c.fail(err)
	}
	return c, nil
}

func staticToken(tok string) oauth2.TokenSource {
	return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: strings.TrimSpace(tok), TokenType: "Bearer"})
}

// dial opens a session; it returns the conn even on failure so the caller can classify the error.
func (a *app) dial(ctx context.Context, base, path string, ts oauth2.TokenSource, h auth.OAuthHandler) (*conn, error) {
	c := &conn{base: base, probe: &probe{next: http.DefaultTransport}}
	hc := &http.Client{Transport: c.probe, Timeout: 2 * time.Minute}
	if ts != nil {
		hc.Transport = &oauth2.Transport{Source: ts, Base: c.probe}
	}
	endpoint := base + path
	if a.agent != "" {
		endpoint += "?agent=" + url.QueryEscape(a.agent)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "plandillo-cli", Version: Version}, nil)
	s, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: hc, OAuthHandler: h,
		DisableStandaloneSSE: true, MaxRetries: -1}, nil) // Plandillo's MCP is stateless: no server-sent stream
	if err != nil {
		return c, err
	}
	c.ClientSession = s
	return c, nil
}

func (c *conn) fail(err error) error {
	login := "plandillo-cli login " + c.base
	switch {
	case c.src != nil && c.src.err != nil:
		return &exitError{exitAuth, fmt.Sprintf("unauthorized: the sign-in to %s expired (%v); run %s", c.base, c.src.err, login)}
	case c.probe.status == http.StatusUnauthorized || c.probe.status == http.StatusForbidden:
		return &exitError{exitAuth, fmt.Sprintf("unauthorized: %s refused the credentials (HTTP %d); run %s or set PLANDILLO_TOKEN", c.base, c.probe.status, login)}
	case c.probe.err != nil:
		return &exitError{exitNetwork, fmt.Sprintf("unavailable: cannot reach %s: %v", c.base, c.probe.err)}
	case errors.Is(err, context.Canceled):
		return &exitError{exitTool, "canceled"}
	}
	return err
}

func (c *conn) toolList(ctx context.Context) ([]*mcp.Tool, error) {
	var out []*mcp.Tool
	for t, err := range c.Tools(ctx, nil) {
		if err != nil {
			return nil, c.fail(err)
		}
		out = append(out, t)
	}
	return out, nil
}

// callTool runs one tool and prints its result.
func (a *app) callTool(ctx context.Context, c *conn, name string, args map[string]any) error {
	res, err := c.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return c.fail(err)
	}
	var text []string
	for _, part := range res.Content {
		if t, ok := part.(*mcp.TextContent); ok {
			text = append(text, t.Text)
		}
	}
	return a.print(strings.Join(text, "\n"), res.IsError)
}

func (a *app) call(ctx context.Context, name string, args []string) error {
	if strings.HasPrefix(name, "-") {
		return usageError("put global options before the command; unknown option %s", name)
	}
	c, err := a.open(ctx, "/mcp")
	if err != nil {
		return err
	}
	defer c.Close()
	list, err := c.toolList(ctx)
	if err != nil {
		return err
	}
	var tool *mcp.Tool
	names := make([]string, 0, len(list))
	for _, t := range list {
		names = append(names, t.Name)
		if t.Name == name {
			tool = t
		}
	}
	if tool == nil && access(name) != "?" { // the board hides write tools in read-only mode and switched-off tools
		return &exitError{exitAuth, fmt.Sprintf("forbidden: %s is switched off on this board or needs write access", name)}
	}
	if tool == nil {
		return usageError("unknown command %q; commands: login, logout, status, tools, code, and the tools %s", name, strings.Join(names, ", "))
	}
	in, err := buildArgs(args, schemaOf(tool), a.stdin)
	if err != nil {
		return err
	}
	return a.callTool(ctx, c, name, in)
}

func (a *app) code(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return usageError("code takes one argument: JavaScript, @file.js or - for stdin")
	}
	js, err := readArg(args[0], a.stdin)
	if err != nil {
		return err
	}
	c, err := a.open(ctx, "/mcp/code")
	if err != nil {
		return err
	}
	defer c.Close()
	return a.callTool(ctx, c, "code", map[string]any{"code": string(js)})
}

func (a *app) tools(ctx context.Context, args []string) error {
	asJSON := len(args) == 1 && args[0] == "--json"
	if len(args) > 0 && !asJSON {
		return usageError("tools takes only --json")
	}
	c, err := a.open(ctx, "/mcp")
	if err != nil {
		return err
	}
	defer c.Close()
	list, err := c.toolList(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		type entry struct {
			Name        string `json:"name"`
			Access      string `json:"access"`
			Description string `json:"description"`
			InputSchema any    `json:"input_schema"`
		}
		out := make([]entry, 0, len(list))
		for _, t := range list {
			out = append(out, entry{t.Name, access(t.Name), t.Description, t.InputSchema})
		}
		b, err := json.Marshal(out)
		if err != nil {
			return err
		}
		return a.print(string(b), false)
	}
	for _, t := range list {
		fmt.Fprintf(a.stdout, "%-15s %-5s %s\n", t.Name, access(t.Name), t.Description)
	}
	return nil
}

// access is read or write from the shared catalogue; a tool newer than this CLI shows "?".
func access(name string) string {
	for _, t := range toolcatalog.MCPTools {
		if t.Name == name {
			return t.Group
		}
	}
	return "?"
}

func (a *app) status(ctx context.Context) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	base, err := a.target(cfg)
	if err != nil {
		return err
	}
	signIn := "none"
	switch {
	case a.getenv("PLANDILLO_TOKEN") != "":
		signIn = "PLANDILLO_TOKEN"
	case cfg.URL != base:
	case cfg.Token != "":
		signIn = "workspace token"
	case cfg.OAuth["/mcp"] != nil:
		signIn = "browser"
		if cfg.OAuth["/mcp/code"] != nil {
			signIn += " (with Code Mode)"
		}
	}
	fmt.Fprintf(a.stdout, "board:   %s\nsign-in: %s\nskills:  %s\n", base, signIn, a.skillsLine())
	c, err := a.open(ctx, "/mcp")
	if err != nil {
		return err
	}
	defer c.Close()
	list, err := c.toolList(ctx)
	if err != nil {
		return err
	}
	mode, names := "read-only", make([]string, 0, len(list))
	for _, t := range list {
		names = append(names, t.Name)
		if access(t.Name) == "write" {
			mode = "read-write"
		}
	}
	sort.Strings(names)
	fmt.Fprintf(a.stdout, "access:  %s\ntools:   %d (%s)\n", mode, len(list), strings.Join(names, ", "))
	return nil
}

func (a *app) logout() error {
	path, err := a.configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintln(a.stderr, "Signed out: removed "+path)
	return nil
}
