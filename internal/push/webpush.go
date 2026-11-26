package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

var b64 = base64.RawURLEncoding

// encrypt is the aes128gcm content coding of RFC 8188 with the Web Push keys of RFC 8291, one record.
// The sender key and salt are arguments so the RFC's test vector can check it; send passes fresh ones.
func encrypt(plaintext []byte, p256dh, authSecret string, sender *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaRaw, err := b64.DecodeString(p256dh)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	ua, err := ecdh.P256().NewPublicKey(uaRaw)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	secret, err := b64.DecodeString(authSecret)
	if err != nil || len(secret) != 16 {
		return nil, errors.New("auth: want 16 bytes")
	}
	shared, err := sender.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPub := sender.PublicKey().Bytes()
	ikm, err := hkdf.Key(sha256.New, shared, secret, "WebPush: info\x00"+string(uaRaw)+string(asPub), 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	var head bytes.Buffer
	head.Write(salt)
	binary.Write(&head, binary.BigEndian, uint32(4096)) // record size
	head.WriteByte(byte(len(asPub)))
	head.Write(asPub)
	return gcm.Seal(head.Bytes(), nonce, append(plaintext, 2), nil), nil // 0x02: the last (only) record, no padding
}

// vapid is the Authorization header value (RFC 8292) for one push service origin.
func vapid(key *ecdsa.PrivateKey, endpoint, contact string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(12 * time.Hour).Unix(), "sub": contact})
	unsigned := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64) // ES256 is r||s, 32 bytes each
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return "", err
	}
	return "vapid t=" + unsigned + "." + b64.EncodeToString(sig) + ", k=" + b64.EncodeToString(pub), nil
}

// errGone means the browser dropped the subscription: the caller deletes it.
var errGone = errors.New("subscription expired")

func (p *Push) send(ctx context.Context, sub target, msg message) error {
	body, _ := json.Marshal(msg)
	sender, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	payload, err := encrypt(body, sub.p256dh, sub.auth, sender, salt)
	if err != nil {
		return err
	}
	auth, err := vapid(p.key, sub.endpoint, sub.origin, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", sub.endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "86400") // a phone that is off gets it within a day, then never
	req.Header.Set("Urgency", "normal")
	res, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == 404 || res.StatusCode == 410:
		return errGone
	case res.StatusCode >= 300:
		return fmt.Errorf("push service answered %s", res.Status)
	}
	return nil
}
