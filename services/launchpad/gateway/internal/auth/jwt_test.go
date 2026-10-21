package auth

import (
	"testing"
	"time"
)

func TestSigner_MintVerify(t *testing.T) {
	s := newSigner("test-secret", time.Hour, nil)
	tok, exp := s.mint("0xabc")
	if exp.Before(time.Now()) {
		t.Fatalf("expiry %v is in the past", exp)
	}
	wallet, err := s.verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if wallet != "0xabc" {
		t.Errorf("wallet = %q, want 0xabc", wallet)
	}
}

func TestSigner_Verify_Rejects(t *testing.T) {
	s := newSigner("test-secret", time.Hour, nil)
	tok, _ := s.mint("0xabc")

	tests := map[string]string{
		"empty":        "",
		"two parts":    "a.b",
		"garbage":      "x.y.z",
		"tampered sig": tok[:len(tok)-2] + "AA",
		"wrong secret": mustMint(t, "other-secret"),
	}
	for name, bad := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := s.verify(bad); err == nil {
				t.Fatalf("verify(%q) = nil err, want rejection", bad)
			}
		})
	}
}

func TestSigner_Verify_Expired(t *testing.T) {
	base := time.Now()
	clock := func() time.Time { return base }
	s := newSigner("test-secret", time.Minute, clock)
	tok, _ := s.mint("0xabc")

	// Advance the clock past expiry.
	s.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, err := s.verify(tok); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

func mustMint(t *testing.T, secret string) string {
	t.Helper()
	tok, _ := newSigner(secret, time.Hour, nil).mint("0xabc")
	return tok
}
