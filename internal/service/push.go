package service

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"time"

	"plandillo/internal/store"
)

// PushSubscription is one device's Web Push subscription with its notification choices.
// Morning is the local reminder time "HH:MM" ("" turns it off); TZ is the device's IANA zone.
type PushSubscription struct {
	Endpoint  string `json:"endpoint"`
	P256dh    string `json:"p256dh"`
	Auth      string `json:"auth"`
	AgentDone bool   `json:"agent_done"`
	Morning   string `json:"morning"`
	TZ        string `json:"tz"`
	Lang      string `json:"lang"`
	Origin    string `json:"-"` // this installation's address, the VAPID contact; set by the HTTP layer
}

// PushStore keeps the subscriptions of this workspace's owner (internal/push). Nil when push is off (PLANDILLO_PUSH).
type PushStore interface {
	PublicKey() string
	Get(ctx context.Context, endpoint string) (PushSubscription, bool, error)
	Save(ctx context.Context, sub PushSubscription) error
	Remove(ctx context.Context, endpoint string) error
}

// SetPush enables push notifications for this workspace.
func (s *Service) SetPush(p PushStore) { s.push = p }

// ObserveAgentDone calls fn after each commit in which an agent moved a task into a done-category status
// or created it there. The owner's own changes never call it.
func (s *Service) ObserveAgentDone(fn func(store.Task)) { s.agentDone = fn }

var hhmm = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func pushOff() error {
	return &Error{Code: "push_disabled", Message: "push notifications are off on this server (PLANDILLO_PUSH=on enables them)"}
}

// PushState is what the settings page needs: whether push works here, the key to subscribe with, and this device's choices.
type PushState struct {
	Enabled      bool              `json:"enabled"`
	PublicKey    string            `json:"public_key,omitempty"`
	Subscription *PushSubscription `json:"subscription,omitempty"`
}

func (s *Service) PushState(ctx context.Context, endpoint string) (PushState, error) {
	if s.push == nil {
		return PushState{}, nil
	}
	st := PushState{Enabled: true, PublicKey: s.push.PublicKey()}
	if endpoint != "" {
		sub, ok, err := s.push.Get(ctx, endpoint)
		if err != nil {
			return st, err
		}
		if ok {
			st.Subscription = &sub
		}
	}
	return st, nil
}

func (s *Service) SavePush(ctx context.Context, sub PushSubscription) error {
	if s.push == nil {
		return pushOff()
	}
	if u, err := url.Parse(sub.Endpoint); err != nil || u.Scheme != "https" || !PushHost(u.Hostname()) {
		return invalid("endpoint must be an https address of a browser push service (Google, Apple, Mozilla or Microsoft)")
	}
	if sub.P256dh == "" || sub.Auth == "" || len(sub.P256dh) > 200 || len(sub.Auth) > 100 {
		return invalid("p256dh and auth are the subscription keys from the browser")
	}
	if sub.Morning != "" && !hhmm.MatchString(sub.Morning) {
		return invalid("morning must be a time like 08:30, or empty to turn the reminder off")
	}
	if _, err := time.LoadLocation(sub.TZ); err != nil || sub.TZ == "" {
		return invalid("tz must be an IANA time zone such as Europe/Kyiv")
	}
	if sub.Lang != "ru" && sub.Lang != "en" && sub.Lang != "uk" {
		return invalid("lang must be ru, en or uk")
	}
	return s.push.Save(ctx, sub)
}

func (s *Service) RemovePush(ctx context.Context, endpoint string) error {
	if s.push == nil {
		return pushOff()
	}
	return s.push.Remove(ctx, endpoint)
}

// PushHost reports whether host belongs to a browser push service. Subscriptions name the address the server
// posts to, so anything else is refused: the server never sends requests to an address a client picked.
func PushHost(host string) bool {
	switch {
	case host == "fcm.googleapis.com", host == "updates.push.services.mozilla.com":
		return true
	case strings.HasSuffix(host, ".push.apple.com"), strings.HasSuffix(host, ".notify.windows.com"):
		return true
	}
	return false
}
