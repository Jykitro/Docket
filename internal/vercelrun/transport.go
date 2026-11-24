// Package vercelrun carries Code Mode frames between two private service bindings.
// The application executes tool calls. Only the disposable WASI guest evaluates JavaScript.
package vercelrun

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"docket/internal/codeeval"
	"docket/internal/toolcatalog"
)

const timeout = 30 * time.Second

var client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func endpoint(base, path string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid internal service URL")
	}
	// The local CLI publishes each Docker port on the host and injects that host's loopback URL.
	if os.Getenv("VERCEL_ENV") == "development" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback()) {
		if _, err := os.Stat("/.dockerenv"); err == nil && u.Port() != "" {
			u.Host = net.JoinHostPort("host.docker.internal", u.Port())
		}
	}
	return u.JoinPath(path).String(), nil
}
func post(ctx context.Context, address, key string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", address, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/octet-stream")
	return client.Do(req)
}
func digest(s string) string { v := sha256.Sum256([]byte(s)); return hex.EncodeToString(v[:]) }

type App struct {
	DB          *sql.DB
	URL, Secret string
}

func (a App) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	dest, err := endpoint(a.URL, "ready")
	if err != nil {
		return err
	}
	res, err := post(ctx, dest, a.Secret, nil)
	if err != nil {
		return errors.New("unavailable: Code Mode worker is offline")
	}
	defer res.Body.Close()
	if res.StatusCode != 204 {
		return errors.New("unavailable: Code Mode worker is not ready")
	}
	return nil
}

// A short-lived random capability identifies only this reply channel. Neither the
// worker request nor its guest receives the user's bearer token. The guest has no environment.
func (a App) admit(ctx context.Context, owner, key string) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(720403251)"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM docket_auth.vercel_jobs WHERE expires <= clock_timestamp()"); err != nil {
		return err
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, "SELECT count(*) >= 2 OR count(*) FILTER (WHERE owner=$1)>0 FROM docket_auth.vercel_jobs", owner).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return errors.New("rate_limited: Code Mode is busy, try again later")
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO docket_auth.vercel_jobs (id,owner,expires) VALUES ($1,$2,clock_timestamp()+interval '35 seconds')", digest(key), owner); err != nil {
		return err
	}
	return tx.Commit()
}

func (a App) Execute(ctx context.Context, owner, code string, call func(string, json.RawMessage) (string, error)) (string, error) {
	if len(code) > codeeval.MaxCode {
		return "", errors.New("invalid_argument: Code Mode input exceeds 256 KiB")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dest, err := endpoint(a.URL, "run")
	if err != nil {
		return "", err
	}
	key := rand.Text()
	if err = a.admit(ctx, owner, key); err != nil {
		return "", err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_, _ = a.DB.ExecContext(cleanup, "DELETE FROM docket_auth.vercel_jobs WHERE id=$1", digest(key))
	}()
	req, err := http.NewRequestWithContext(ctx, "POST", dest, strings.NewReader(code))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.Secret)
	req.Header.Set("X-Plandillo-Job", key)
	res, err := client.Do(req)
	if err != nil {
		return "", errors.New("unavailable: Code Mode worker request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("unavailable: Code Mode worker returned %d", res.StatusCode)
	}
	for seq := 1; ; seq++ {
		m, err := codeeval.Receive(res.Body)
		if err != nil {
			return "", errors.New("unavailable: incomplete Code Mode response")
		}
		switch m.Kind {
		case "done":
			if len(m.Text) > 1<<20 || len(m.Error) > 1<<16 {
				return "", errors.New("invalid Code Mode result")
			}
			if m.Error != "" {
				return "", errors.New(m.Error)
			}
			return m.Text, nil
		case "call":
			valid := false
			for _, t := range toolcatalog.MCPTools {
				if t.Name == m.Text {
					valid = true
					break
				}
			}
			if !valid || seq > codeeval.MaxCalls {
				return "", errors.New("invalid Code Mode callback")
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			text, callErr := call(m.Text, m.Args)
			reply := codeeval.Message{Kind: "reply", Text: text}
			if callErr != nil {
				reply.Error = callErr.Error()
			}
			data, err := json.Marshal(reply)
			if err != nil || len(data) > codeeval.MaxFrame {
				return "", errors.New("Code Mode reply too large")
			}
			if _, err = a.DB.ExecContext(ctx, "UPDATE docket_auth.vercel_jobs SET seq=$2,reply=$3 WHERE id=$1", digest(key), seq, data); err != nil {
				return "", err
			}
		default:
			return "", errors.New("unexpected Code Mode message")
		}
	}
}

// Reply is reachable through the worker-to-app binding. A reply can land on any replica.
func (a App) Reply(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	seq, err := strconv.Atoi(r.URL.Query().Get("seq"))
	if r.Method != "POST" || !ok || len(key) < 26 || len(key) > 128 || err != nil || seq < 1 || seq > codeeval.MaxCalls {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var n int
		var data []byte
		err = a.DB.QueryRowContext(ctx, "SELECT seq,reply FROM docket_auth.vercel_jobs WHERE id=$1 AND expires>clock_timestamp()", digest(key)).Scan(&n, &data)
		if err != nil || n > seq {
			http.NotFound(w, r)
			return
		}
		if n == seq {
			w.Header().Set("Content-Type", "application/json")
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			_, _ = w.Write(data)
			return
		}
		select {
		case <-ctx.Done():
			http.Error(w, "job expired", 408)
			return
		case <-tick.C:
		}
	}
}

func (a App) Validate() error {
	if len(a.Secret) < 32 {
		return errors.New("PLANDILLO_RUNNER_SECRET must contain at least 32 characters")
	}
	_, err := endpoint(a.URL, "run")
	return err
}
