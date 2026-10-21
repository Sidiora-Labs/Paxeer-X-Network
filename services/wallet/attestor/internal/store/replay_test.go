package store

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestTokenRequestRecordsAreScopedToTheRequest(t *testing.T) {
	s := openStore(t, t.TempDir(), randomBytes(t, 32))
	now := time.Unix(1_900_000_000, 0)
	s.now = func() time.Time { return now }
	token := sha256.Sum256([]byte("token"))
	first := sha256.Sum256([]byte("first request"))
	second := sha256.Sum256([]byte("second request"))
	expiry := now.Add(time.Hour)
	for _, step := range []struct {
		name    string
		request [32]byte
		fresh   bool
	}{
		{"first request", first, true},
		{"second request", second, true},
		{"first request again", first, false},
		{"second request again", second, false},
	} {
		fresh, err := s.UseTokenRequest(token, step.request, expiry)
		if err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if fresh != step.fresh {
			t.Fatalf("%s: fresh = %v, want %v", step.name, fresh, step.fresh)
		}
	}
	other := sha256.Sum256([]byte("other token"))
	if fresh, err := s.UseTokenRequest(other, first, expiry); err != nil || !fresh {
		t.Fatalf("the first request under another token: fresh %v err %v", fresh, err)
	}
	if _, err := s.UseTokenRequest([32]byte{}, first, expiry); err == nil {
		t.Fatal("an empty token digest was recorded")
	}
	if _, err := s.UseTokenRequest(token, [32]byte{}, expiry); err == nil {
		t.Fatal("an empty request digest was recorded")
	}
	now = expiry.Add(time.Second)
	if fresh, err := s.UseTokenRequest(token, first, now.Add(time.Hour)); err != nil || !fresh {
		t.Fatalf("the first request once its record expired: fresh %v err %v", fresh, err)
	}
}
