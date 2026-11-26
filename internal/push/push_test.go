package push

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"plandillo/internal/auth"
	"plandillo/internal/service"
	"plandillo/internal/store"
)

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291, section 5 and appendix A.
const (
	rfcUAPublic = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcAuth     = "BTBZMqHH6r4Tts7J_aSIgg"
)

func TestEncryptMatchesRFC8291(t *testing.T) {
	sender, err := ecdh.P256().NewPrivateKey(mustB64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt([]byte("When I grow up, I want to be a watermelon"), rfcUAPublic, rfcAuth, sender, mustB64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Fatalf("aes128gcm body differs from RFC 8291:\n got %s\nwant %s", b64.EncodeToString(got), want)
	}
}

// fakePush records the requests the push service would get and answers with status.
type fakePush struct {
	mu     sync.Mutex
	reqs   []*http.Request
	status int
	seen   chan struct{}
}

func (f *fakePush) RoundTrip(r *http.Request) (*http.Response, error) {
	io.Copy(io.Discard, r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.mu.Unlock()
	if f.seen != nil {
		defer func() { f.seen <- struct{}{} }()
	}
	return &http.Response{StatusCode: f.status, Status: http.StatusText(f.status), Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

func setup(t *testing.T) (*Push, *fakePush, int64) {
	t.Helper()
	ad, err := auth.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ad.Close() })
	owner, err := store.EnsureLocalUser(ad.Connection())
	if err != nil {
		t.Fatal(err)
	}
	p, err := Open(context.Background(), ad.Connection())
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(context.Background(), ad.Connection())
	if err != nil || again.pub != p.pub {
		t.Fatalf("the VAPID key must be created once and reused: %v", err)
	}
	f := &fakePush{status: 201}
	p.client = &http.Client{Transport: f}
	return p, f, owner
}

func subscribe(t *testing.T, p *Push, owner int64, s service.PushSubscription) {
	t.Helper()
	s.P256dh, s.Auth, s.TZ, s.Lang, s.Origin = rfcUAPublic, rfcAuth, "Europe/Kyiv", "en", "https://plandillo.test"
	if err := p.For(owner, 1).Save(context.Background(), s); err != nil {
		t.Fatal(err)
	}
}

func TestMorningReminderOncePerDay(t *testing.T) {
	p, f, owner := setup(t)
	subscribe(t, p, owner, service.PushSubscription{Endpoint: "https://fcm.googleapis.com/fcm/send/a", Morning: "08:30"})
	kyiv, _ := time.LoadLocation("Europe/Kyiv")
	today := func(_ context.Context, o, space int64, date string) (string, int, error) {
		if o != owner || space != 1 || date != "2026-11-27" {
			t.Errorf("today(%d, %d, %s)", o, space, date)
		}
		return "personal", 3, nil
	}
	for _, at := range []string{"08:29", "08:31", "08:45"} { // before the time, due, already sent today
		p.now = func() time.Time { tm, _ := time.ParseInLocation("2006-01-02 15:04", "2026-11-27 "+at, kyiv); return tm }
		p.morning(context.Background(), today)
	}
	if len(f.reqs) != 1 {
		t.Fatalf("%d reminders sent, want 1", len(f.reqs))
	}
	r := f.reqs[0]
	if r.URL.String() != "https://fcm.googleapis.com/fcm/send/a" || r.Header.Get("Content-Encoding") != "aes128gcm" {
		t.Fatalf("request %s %v", r.URL, r.Header)
	}
	verifyVAPID(t, p, r.Header.Get("Authorization"), "https://fcm.googleapis.com")
}

// verifyVAPID checks the JWT signature with the public key in the header, and its claims.
func verifyVAPID(t *testing.T, p *Push, header, aud string) {
	t.Helper()
	parts := strings.SplitN(strings.TrimPrefix(header, "vapid t="), ", k=", 2)
	if !strings.HasPrefix(header, "vapid t=") || len(parts) != 2 || parts[1] != p.pub {
		t.Fatalf("vapid header %q", header)
	}
	jwt := strings.Split(parts[0], ".")
	var claims map[string]any
	json.Unmarshal(mustB64(t, jwt[1]), &claims)
	if claims["aud"] != aud || claims["sub"] != "https://plandillo.test" {
		t.Fatalf("claims %v", claims)
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), mustB64(t, parts[1]))
	if err != nil {
		t.Fatal(err)
	}
	sig := mustB64(t, jwt[2])
	digest := sha256.Sum256([]byte(jwt[0] + "." + jwt[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("VAPID signature does not verify")
	}
}

func TestAgentDoneDropsAnExpiredSubscription(t *testing.T) {
	p, f, owner := setup(t)
	f.status, f.seen = 410, make(chan struct{}, 4)
	subscribe(t, p, owner, service.PushSubscription{Endpoint: "https://web.push.apple.com/x", AgentDone: true})
	subscribe(t, p, owner, service.PushSubscription{Endpoint: "https://updates.push.services.mozilla.com/y"}) // agent notices off
	p.AgentDone(owner, 1, store.Task{Code: "PER-01", Title: "Ship it"})
	select {
	case <-f.seen:
	case <-time.After(5 * time.Second):
		t.Fatal("no push sent")
	}
	time.Sleep(50 * time.Millisecond) // the delete follows the answer
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reqs) != 1 || f.reqs[0].URL.Host != "web.push.apple.com" {
		t.Fatalf("sent to %v, want only the device with agent notices on", f.reqs)
	}
	if _, ok, _ := p.For(owner, 1).Get(context.Background(), "https://web.push.apple.com/x"); ok {
		t.Fatal("a subscription the push service answered 410 for must be removed")
	}
	if _, ok, _ := p.For(owner, 1).Get(context.Background(), "https://updates.push.services.mozilla.com/y"); !ok {
		t.Fatal("the other device must keep its subscription")
	}
}
