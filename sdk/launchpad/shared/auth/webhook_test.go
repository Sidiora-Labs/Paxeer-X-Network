package auth

import (
	"errors"
	"testing"
	"time"
)

func TestSignWebhookGolden(t *testing.T) {
	t.Parallel()
	// Golden HMAC values produced by Node crypto (the TS webhook-auth scheme).
	if got := SignWebhook("topsecret", "1718000000", `{"a":1,"b":"x"}`); got != "sha256=83cdfa9f7514418dd780e60715727b6005d1c630f679f0e7a6652f947db9672a" {
		t.Errorf("SignWebhook = %s", got)
	}
	if got := SignWebhook("k", "1700000000", ""); got != "sha256=5c2999a43333a6877f49c8003f235c4f6de40f85b1b7414b70bd470a8db53d97" {
		t.Errorf("SignWebhook(empty body) = %s", got)
	}
}

func TestVerifyWebhookRoundTrip(t *testing.T) {
	t.Parallel()
	secret, ts, body := "topsecret", "1718000000", `{"a":1,"b":"x"}`
	sig := SignWebhook(secret, ts, body)
	now := time.Unix(1718000010, 0) // 10s after ts, within window
	if err := VerifyWebhook(secret, ts, body, sig, now, DefaultReplayWindow); err != nil {
		t.Fatalf("VerifyWebhook valid sig: %v", err)
	}
}

func TestVerifyWebhookFailures(t *testing.T) {
	t.Parallel()
	secret, ts, body := "topsecret", "1718000000", `{"a":1}`
	sig := SignWebhook(secret, ts, body)
	now := time.Unix(1718000000, 0)

	cases := []struct {
		name             string
		secret, ts, body string
		sig              string
		now              time.Time
		want             error
	}{
		{"missing sig", secret, ts, body, "", now, ErrMissingSignature},
		{"missing ts", secret, "", body, sig, now, ErrMissingSignature},
		{"bad scheme", secret, ts, body, "md5=deadbeef", now, ErrUnsupportedScheme},
		{"bad timestamp", secret, "notanum", body, "sha256=" + sig[len(SignaturePrefix):], now, ErrInvalidTimestamp},
		{"outside window", secret, ts, body, sig, time.Unix(1718999999, 0), ErrReplayWindow},
		{"wrong secret", "other", ts, body, sig, now, ErrSignatureMismatch},
		{"tampered body", secret, ts, `{"a":2}`, sig, now, ErrSignatureMismatch},
		{"non-hex sig", secret, ts, body, "sha256=zzzz", now, ErrSignatureMismatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := VerifyWebhook(c.secret, c.ts, c.body, c.sig, c.now, DefaultReplayWindow)
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestVerifyWebhookDefaultsWindow(t *testing.T) {
	t.Parallel()
	secret, ts, body := "s", "1700000000", "payload"
	sig := SignWebhook(secret, ts, body)
	// window=0 should default to 300s; 100s skew passes.
	if err := VerifyWebhook(secret, ts, body, sig, time.Unix(1700000100, 0), 0); err != nil {
		t.Fatalf("default window verify: %v", err)
	}
}
