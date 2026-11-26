// Package push sends Web Push notifications (PLANDILLO_PUSH=on): "an agent finished a task" and the morning
// day-plan reminder. Subscriptions belong to accounts in the catalog; sending goes only to browser push services.
package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"plandillo/internal/database"
	"plandillo/internal/service"
	"plandillo/internal/store"
)

type Push struct {
	db     *database.DB
	ctx    context.Context
	key    *ecdsa.PrivateKey
	pub    string // the VAPID public key the browser subscribes with
	client *http.Client
	now    func() time.Time
}

// Open loads the installation's VAPID key, creating it on first use.
func Open(ctx context.Context, db *database.DB) (*Push, error) {
	p := &Push{db: db, ctx: ctx, now: time.Now, client: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, // never follow a push service elsewhere
	}}
	var stored string
	err := db.QueryRow("SELECT private_key FROM push_vapid WHERE id=1").Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return nil, err
		}
		if _, err = db.Exec("INSERT INTO push_vapid(id,private_key) VALUES (1,?) ON CONFLICT(id) DO NOTHING", b64.EncodeToString(der)); err != nil {
			return nil, err
		}
		return Open(ctx, db) // another instance may have won the insert: use the stored key
	}
	if err != nil {
		return nil, err
	}
	der, err := b64.DecodeString(stored)
	if err != nil {
		return nil, fmt.Errorf("push_vapid: %w", err)
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if p.key, _ = k.(*ecdsa.PrivateKey); err != nil || p.key == nil {
		return nil, fmt.Errorf("push_vapid: not an ECDSA key (%v)", err)
	}
	pub, err := p.key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	p.pub = b64.EncodeToString(pub)
	return p, nil
}

// For is the subscription store of one workspace's owner, for service.SetPush.
func (p *Push) For(owner, space int64) service.PushStore { return account{p, owner, space} }

type account struct {
	p            *Push
	owner, space int64
}

func (a account) PublicKey() string { return a.p.pub }

func (a account) Get(ctx context.Context, endpoint string) (service.PushSubscription, bool, error) {
	s := service.PushSubscription{Endpoint: endpoint}
	err := a.p.db.ReadQueryRowContext(ctx, "SELECT p256dh,auth,agent_done,morning,tz,lang FROM push_subscriptions WHERE endpoint=? AND user_id=?", endpoint, a.owner).
		Scan(&s.P256dh, &s.Auth, &s.AgentDone, &s.Morning, &s.TZ, &s.Lang)
	if errors.Is(err, sql.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, err
}

// Save subscribes the device for this account (a device another account subscribed before moves over).
func (a account) Save(_ context.Context, s service.PushSubscription) error {
	agent := 0 // an integer column in both dialects
	if s.AgentDone {
		agent = 1
	}
	_, err := a.p.db.Exec(`INSERT INTO push_subscriptions(endpoint,user_id,workspace_id,p256dh,auth,agent_done,morning,tz,lang,origin,created_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(endpoint) DO UPDATE SET user_id=excluded.user_id, workspace_id=excluded.workspace_id,
 p256dh=excluded.p256dh, auth=excluded.auth, agent_done=excluded.agent_done, morning=excluded.morning, tz=excluded.tz,
 lang=excluded.lang, origin=excluded.origin`,
		s.Endpoint, a.owner, a.space, s.P256dh, s.Auth, agent, s.Morning, s.TZ, s.Lang, s.Origin, a.p.now().Unix())
	return err
}

func (a account) Remove(_ context.Context, endpoint string) error {
	_, err := a.p.db.Exec("DELETE FROM push_subscriptions WHERE endpoint=? AND user_id=?", endpoint, a.owner)
	return err
}

type target struct{ endpoint, p256dh, auth, origin, lang string }

type message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	Tag   string `json:"tag"`
}

var texts = map[string][4]string{ // agent finished, today's plan, tasks for today: {n}, nothing planned
	"ru": {"Агент завершил задачу", "План на сегодня", "Задач на сегодня: %d", "На сегодня задач нет. Спланируйте день"},
	"en": {"An agent finished a task", "Today's plan", "Tasks for today: %d", "Nothing planned for today. Plan your day"},
	"uk": {"Агент завершив завдання", "План на сьогодні", "Завдань на сьогодні: %d", "На сьогодні завдань немає. Сплануйте день"},
}

func (p *Push) deliver(t target, m message) {
	switch err := p.send(p.ctx, t, m); {
	case errors.Is(err, errGone):
		p.db.Exec("DELETE FROM push_subscriptions WHERE endpoint=?", t.endpoint)
	case err != nil:
		host := ""
		if u, perr := url.Parse(t.endpoint); perr == nil {
			host = u.Host // the rest of the endpoint identifies the device: keep it out of the log
		}
		slog.Warn("push not delivered", "host", host, "err", err)
	}
}

// AgentDone notifies the owner's devices that asked for it; for service.ObserveAgentDone. It returns at once.
func (p *Push) AgentDone(owner, space int64, t store.Task) {
	go func() {
		var slug string
		if err := p.db.QueryRow("SELECT slug FROM workspaces WHERE id=?", space).Scan(&slug); err != nil {
			slog.Warn("push: workspace", "err", err)
			return
		}
		rows, err := p.db.Query("SELECT endpoint,p256dh,auth,origin,lang FROM push_subscriptions WHERE user_id=? AND agent_done=1", owner)
		if err != nil {
			slog.Warn("push: subscriptions", "err", err)
			return
		}
		var all []target
		for rows.Next() {
			var x target
			if rows.Scan(&x.endpoint, &x.p256dh, &x.auth, &x.origin, &x.lang) == nil {
				all = append(all, x)
			}
		}
		rows.Close()
		for _, x := range all {
			p.deliver(x, message{Title: texts[x.lang][0], Body: t.Code + " · " + t.Title, URL: "/" + slug + "/t/" + t.Code, Tag: "task-" + t.Code})
		}
	}()
}

// Today counts the open tasks planned for date in a workspace and names its slug (selfhost.Workspaces.TodayOpen).
type Today func(ctx context.Context, owner, space int64, date string) (slug string, open int, err error)

// Run sends the morning reminders until ctx ends, checking once a minute.
func (p *Push) Run(ctx context.Context, today Today) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		p.morning(ctx, today)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// morning sends each due reminder once a day. shortcut: a server that was down at reminder time sends it up to
// 3 hours late and then skips that day; a missed-reminder queue would be the upgrade.
func (p *Push) morning(ctx context.Context, today Today) {
	type due struct {
		target
		owner, space      int64
		morning, tz, last string
	}
	rows, err := p.db.Query("SELECT endpoint,p256dh,auth,origin,lang,user_id,workspace_id,morning,tz,last_morning FROM push_subscriptions WHERE morning<>''")
	if err != nil {
		slog.Warn("push: reminders", "err", err)
		return
	}
	var all []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.endpoint, &d.p256dh, &d.auth, &d.origin, &d.lang, &d.owner, &d.space, &d.morning, &d.tz, &d.last) == nil {
			all = append(all, d)
		}
	}
	rows.Close()
	for _, d := range all {
		loc, err := time.LoadLocation(d.tz)
		if err != nil {
			continue
		}
		now := p.now().In(loc)
		date := now.Format("2006-01-02")
		at, err := time.ParseInLocation("2006-01-02 15:04", date+" "+d.morning, loc)
		if err != nil || d.last == date || now.Before(at) || now.Sub(at) > 3*time.Hour {
			continue
		}
		// Claim the day first: two instances or a slow send never remind twice.
		res, err := p.db.Exec("UPDATE push_subscriptions SET last_morning=? WHERE endpoint=? AND last_morning<>?", date, d.endpoint, date)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n != 1 {
			continue
		}
		slug, open, err := today(ctx, d.owner, d.space, date)
		if err != nil {
			slog.Warn("push: today", "err", err)
			continue
		}
		body := fmt.Sprintf(texts[d.lang][2], open)
		if open == 0 {
			body = texts[d.lang][3]
		}
		p.deliver(d.target, message{Title: texts[d.lang][1], Body: body, URL: "/" + slug, Tag: "morning"})
	}
}
