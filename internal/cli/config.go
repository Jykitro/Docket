package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

const defaultURL = "http://localhost:7777"

// config is the saved sign-in. Credentials are sent only to URL, never to another board.
type config struct {
	URL   string                 `json:"url"`
	Token string                 `json:"token,omitempty"` // workspace token from login --with-token
	OAuth map[string]*oauthGrant `json:"oauth,omitempty"` // by endpoint path: OAuth tokens are bound to /mcp or /mcp/code
}

type oauthGrant struct {
	ClientID     string           `json:"client_id"`
	ClientSecret string           `json:"client_secret,omitempty"`
	TokenURL     string           `json:"token_url"`
	AuthStyle    oauth2.AuthStyle `json:"auth_style"`
	Token        *oauth2.Token    `json:"token"`
}

func (a *app) configPath() (string, error) {
	if p := a.getenv("PLANDILLO_CLI_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("no config directory: %w; set PLANDILLO_CLI_CONFIG", err)
	}
	return filepath.Join(dir, "plandillo", "cli.json"), nil
}

func (a *app) loadConfig() (*config, error) {
	path, err := a.configPath()
	if err != nil {
		return nil, err
	}
	cfg := &config{}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err == nil {
		err = json.Unmarshal(b, cfg)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}

// saveConfig replaces the file atomically, so an interrupted write never loses a rotated refresh token.
func (a *app) saveConfig(cfg *config) error {
	path, err := a.configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".cli-*.json") // created with mode 0600
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return fmt.Errorf("save %s: %w", path, err)
	}
	return nil
}

// target picks the board: --url, PLANDILLO_URL, the saved sign-in, then the local default.
func (a *app) target(cfg *config) (string, error) {
	for _, raw := range []string{a.url, a.getenv("PLANDILLO_URL"), cfg.URL} {
		if raw != "" {
			return normalizeURL(raw)
		}
	}
	return defaultURL, nil
}

// normalizeURL accepts the board address or its MCP URL (…/mcp, …/mcp/code) and returns the board address.
func normalizeURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return "", usageError("the board address must look like https://plans.example.com or http://localhost:7777, got %q", raw)
	}
	p := strings.TrimRight(u.Path, "/")
	p = strings.TrimSuffix(p, "/mcp/code")
	p = strings.TrimSuffix(p, "/mcp")
	u.Path, u.RawPath, u.RawQuery, u.Fragment = p, "", "", ""
	return u.String(), nil
}
