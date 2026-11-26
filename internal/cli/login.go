package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// login signs in through the browser (OAuth with a loopback redirect) or saves a workspace token from stdin.
func (a *app) login(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	withToken := fs.Bool("with-token", false, "")
	withCode := fs.Bool("code", false, "")
	err := fs.Parse(args)
	if err == nil && fs.NArg() > 0 { // the address may come before the options
		a.url = fs.Arg(0)
		err = fs.Parse(fs.Args()[1:])
		if err == nil && fs.NArg() > 0 {
			err = errors.New("login takes one board address")
		}
	}
	if err != nil {
		return usageError("login: %v; usage: plandillo-cli login [URL] [--with-token] [--code]", err)
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	base, err := a.target(cfg)
	if err != nil {
		return err
	}
	if u, _ := url.Parse(base); u.Scheme == "http" && !loopback(u.Hostname()) {
		fmt.Fprintf(a.stderr, "plandillo-cli: warning: %s is not HTTPS, so the token travels unencrypted\n", base)
	}
	if cfg.URL != base {
		cfg = &config{URL: base}
	}

	if *withToken {
		b, err := io.ReadAll(io.LimitReader(a.stdin, 4096))
		if err != nil {
			return err
		}
		tok := strings.TrimSpace(string(b))
		if tok == "" {
			return usageError("login --with-token reads the workspace token from stdin, for example: plandillo-cli login --with-token < token.txt")
		}
		n, err := a.check(ctx, base, "/mcp", staticToken(tok), nil)
		if err != nil {
			return err
		}
		cfg.Token, cfg.OAuth = tok, nil
		if err := a.saveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(a.stderr, "Signed in to %s with a workspace token: %d tools.\n", base, n)
		return nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	codes := make(chan callback, 1)
	srv := &http.Server{Handler: callbackHandler(codes), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	redirect := "http://" + ln.Addr().String() + "/callback"
	fetch := func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
		select { // drop a callback left from an earlier page reload
		case <-codes:
		default:
		}
		fmt.Fprintf(a.stderr, "Open this address to sign in to %s:\n  %s\n", base, args.URL)
		openBrowser(args.URL)
		select {
		case cb := <-codes:
			return cb.res, cb.err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Minute):
			return nil, errors.New("no answer from the browser in 10 minutes")
		}
	}

	paths := []string{"/mcp"}
	if *withCode {
		paths = append(paths, "/mcp/code") // OAuth tokens are bound to one endpoint
	}
	grants := map[string]*oauthGrant{}
	n := 0
	for _, p := range paths {
		var grant *oauthGrant
		h, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
			DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs: []string{redirect}, ClientName: "Plandillo CLI", TokenEndpointAuthMethod: "none",
				GrantTypes: []string{"authorization_code", "refresh_token"}, ResponseTypes: []string{"code"},
			}},
			AuthorizationCodeFetcher: fetch,
			RequestRefreshToken:      true,
			NewTokenSource: func(ctx context.Context, oc *oauth2.Config, t *oauth2.Token) (oauth2.TokenSource, error) {
				grant = &oauthGrant{ClientID: oc.ClientID, ClientSecret: oc.ClientSecret, TokenURL: oc.Endpoint.TokenURL, AuthStyle: oc.Endpoint.AuthStyle, Token: t}
				return oc.TokenSource(ctx, t), nil
			},
		})
		if err != nil {
			return err
		}
		count, err := a.check(ctx, base, p, nil, h)
		if err != nil {
			return err
		}
		if grant == nil {
			return fmt.Errorf("%s%s answered without asking to sign in", base, p)
		}
		grants[p] = grant
		if p == "/mcp" {
			n = count
		}
	}
	cfg.Token = ""
	if cfg.OAuth == nil {
		cfg.OAuth = map[string]*oauthGrant{}
	}
	for p, g := range grants {
		cfg.OAuth[p] = g
	}
	if err := a.saveConfig(cfg); err != nil {
		return err
	}
	fmt.Fprintf(a.stderr, "Signed in to %s: %d tools.\n", base, n)
	return nil
}

// check connects with the given credentials and returns the number of tools the board offers.
func (a *app) check(ctx context.Context, base, path string, ts oauth2.TokenSource, h auth.OAuthHandler) (int, error) {
	c, err := a.dial(ctx, base, path, ts, h)
	if err == nil {
		defer c.Close()
		var list []any
		for _, err = range c.Tools(ctx, nil) {
			if err != nil {
				break
			}
			list = append(list, nil)
		}
		if err == nil {
			return len(list), nil
		}
	}
	if h != nil && c.probe.err == nil {
		return 0, &exitError{exitAuth, fmt.Sprintf("unauthorized: sign-in to %s failed: %v", base, err)}
	}
	return 0, c.fail(err)
}

type callback struct {
	res *auth.AuthorizationResult
	err error
}

// callbackHandler receives the browser redirect; the SDK checks state and iss.
func callbackHandler(codes chan<- callback) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var cb callback
		switch {
		case q.Get("error") != "":
			cb.err = fmt.Errorf("the board refused: %s %s", q.Get("error"), q.Get("error_description"))
		case q.Get("code") == "":
			cb.err = errors.New("the browser returned no authorization code")
		default:
			cb.res = &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}
		}
		if cb.err != nil {
			http.Error(w, cb.err.Error(), http.StatusBadRequest)
		} else {
			fmt.Fprintln(w, "Plandillo CLI is signed in. You can close this tab.")
		}
		select {
		case codes <- cb:
		default:
		}
	})
}

func loopback(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// openBrowser is best effort: the address is printed as well, for a headless machine.
func openBrowser(u string) {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if cmd.Start() == nil {
		go cmd.Wait()
	}
}
