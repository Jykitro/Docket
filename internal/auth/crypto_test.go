package auth

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	pbkdf2Iter = 1000 // speed
	os.Exit(m.Run())
}

func TestHashRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "correct horse") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(h, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestHashSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Fatal("hashes of same password are identical")
	}
}

func TestCheckPasswordRejectsGarbage(t *testing.T) {
	for _, h := range []string{"nonsense", "", "pbkdf2-sha256$x$y$z", "pbkdf2-sha256$0$AA$AA", "pbkdf2-sha256$1000$!!$!!"} {
		if CheckPassword(h, "x") {
			t.Fatalf("garbage %q accepted", h)
		}
	}
}

func TestNewTokenHash(t *testing.T) {
	tok, hash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if TokenHash(tok) != hash {
		t.Fatal("hash mismatch")
	}
	tok2, _, _ := newToken()
	if tok == tok2 {
		t.Fatal("tokens equal")
	}
}

func TestCheckPasswordIterationCap(t *testing.T) {
	h, _ := HashPassword("pw")
	parts := strings.Split(h, "$")
	parts[1] = "10000001"
	start := time.Now()
	if CheckPassword(strings.Join(parts, "$"), "pw") {
		t.Fatal("hash above the iteration cap accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("cap checked after hashing")
	}
}
