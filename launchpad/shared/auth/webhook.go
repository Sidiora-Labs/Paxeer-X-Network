// Package auth provides wallet signature verification (EIP-191) and webhook
// HMAC signing/verification, ported from the TS shared auth + webhook-auth.ts.
// The webhook scheme is byte-compatible with the TS implementation (invariant
// i3) so a Go indexer can sign payloads that TS consumers verify and vice-versa.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// SignaturePrefix is the scheme tag on the X-Sidiora-Signature header.
const SignaturePrefix = "sha256="

// DefaultReplayWindow matches the TS default (300s).
const DefaultReplayWindow = 300 * time.Second

// Webhook verification errors.
var (
	ErrMissingSignature  = errors.New("auth: missing signature or timestamp")
	ErrUnsupportedScheme = errors.New("auth: unsupported signature scheme")
	ErrInvalidTimestamp  = errors.New("auth: invalid timestamp")
	ErrReplayWindow      = errors.New("auth: timestamp outside replay window")
	ErrSignatureMismatch = errors.New("auth: signature mismatch")
)

// SignWebhook returns the X-Sidiora-Signature value for a payload:
// "sha256=" + hex(HMAC_SHA256(secret, timestamp + "." + body)). The HMAC input
// concatenation is byte-identical to webhook-auth.ts (timestamp, then ".", then
// the raw body).
func SignWebhook(secret, timestamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write([]byte(body))
	return SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

// VerifyWebhook validates a received signature against the body. It checks the
// scheme prefix, the replay window (|now - timestamp| <= window), and a
// constant-time HMAC comparison — mirroring webhook-auth.ts exactly. A zero
// window uses DefaultReplayWindow.
func VerifyWebhook(secret, timestamp, body, signature string, now time.Time, window time.Duration) error {
	if signature == "" || timestamp == "" {
		return ErrMissingSignature
	}
	if len(signature) < len(SignaturePrefix) || signature[:len(SignaturePrefix)] != SignaturePrefix {
		return ErrUnsupportedScheme
	}
	if window <= 0 {
		window = DefaultReplayWindow
	}

	tsNum, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidTimestamp, timestamp)
	}
	skew := now.Unix() - tsNum
	if skew < 0 {
		skew = -skew
	}
	if skew > int64(window.Seconds()) {
		return ErrReplayWindow
	}

	provided, err := hex.DecodeString(signature[len(SignaturePrefix):])
	if err != nil {
		return ErrSignatureMismatch
	}
	expected, err := hex.DecodeString(SignWebhook(secret, timestamp, body)[len(SignaturePrefix):])
	if err != nil {
		return ErrSignatureMismatch
	}
	if !hmac.Equal(expected, provided) {
		return ErrSignatureMismatch
	}
	return nil
}
