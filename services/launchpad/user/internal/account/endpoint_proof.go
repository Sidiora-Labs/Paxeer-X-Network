package account

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"time"
)

// EndpointChallenge is signed by an explicitly configured trusted provider
// after endpoint ownership validation. The signature is not a wallet session.
type EndpointChallenge struct {
	AccountID   string    `json:"accountId"`
	TokenDigest string    `json:"tokenDigest"`
	Nonce       string    `json:"nonce"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

func SignedEndpointVerifier(provider ed25519.PublicKey) EndpointProofVerifier {
	return func(ctx context.Context, id uuid.UUID, token, proof string, now time.Time) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if len(provider) != ed25519.PublicKeySize || len(proof) > 4096 {
			return ErrDenied
		}
		parts := strings.Split(proof, ".")
		if len(parts) != 2 {
			return ErrDenied
		}
		body, e := base64.RawURLEncoding.Strict().DecodeString(parts[0])
		if e != nil {
			return ErrDenied
		}
		sig, e := base64.RawURLEncoding.Strict().DecodeString(parts[1])
		if e != nil || !ed25519.Verify(provider, body, sig) {
			return ErrDenied
		}
		var v EndpointChallenge
		if e = json.Unmarshal(body, &v); e != nil {
			return ErrDenied
		}
		h := sha256.Sum256([]byte(token))
		if v.AccountID != id.String() || v.TokenDigest != hex.EncodeToString(h[:]) || len(v.Nonce) < 16 || !v.ExpiresAt.After(now) || v.ExpiresAt.After(now.Add(5*time.Minute)) {
			return ErrDenied
		}
		return nil
	}
}

func CompactEndpointProofVerifier(provider ed25519.PublicKey) EndpointProofVerifier {
	key := append(ed25519.PublicKey(nil), provider...)
	return func(ctx context.Context, id uuid.UUID, token, proof string, now time.Time) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if len(key) != ed25519.PublicKeySize || len(proof) > 256 {
			return ErrDenied
		}
		wire, e := base64.RawURLEncoding.Strict().DecodeString(proof)
		if e != nil || len(wire) != 137 || wire[0] != 1 || !ed25519.Verify(key, wire[:73], wire[73:]) {
			return ErrDenied
		}
		if !bytes.Equal(wire[1:17], id[:]) {
			return ErrDenied
		}
		digest := sha256.Sum256([]byte(token))
		if !bytes.Equal(wire[17:49], digest[:]) {
			return ErrDenied
		}
		expiry := int64(binary.BigEndian.Uint64(wire[65:73]))
		if expiry <= now.Unix() || expiry > now.Add(5*time.Minute).Unix() {
			return ErrDenied
		}
		nonce := wire[49:65]
		nonzero := false
		for _, b := range nonce {
			nonzero = nonzero || b != 0
		}
		if !nonzero {
			return ErrDenied
		}
		return nil
	}
}

type DeviceBinding struct {
	AccountID, SessionID, InstallationID uuid.UUID
	Platform, Provider, Token, Proof     string
}
type DeviceProofVerifier func(context.Context, DeviceBinding, time.Time) error

func CompactDeviceProofVerifier(provider ed25519.PublicKey) DeviceProofVerifier {
	key := append(ed25519.PublicKey(nil), provider...)
	return func(ctx context.Context, b DeviceBinding, now time.Time) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if len(key) != ed25519.PublicKeySize || len(b.Proof) > 256 || b.AccountID == uuid.Nil || b.SessionID == uuid.Nil || b.InstallationID == uuid.Nil {
			return ErrDenied
		}
		raw, e := base64.RawURLEncoding.Strict().DecodeString(b.Proof)
		if e != nil || len(raw) != 171 || raw[0] != 2 || !ed25519.Verify(key, raw[:107], raw[107:]) {
			return ErrDenied
		}
		digest := sha256.Sum256([]byte(b.Token))
		if !bytes.Equal(raw[1:17], b.AccountID[:]) || !bytes.Equal(raw[17:33], b.SessionID[:]) || !bytes.Equal(raw[33:49], b.InstallationID[:]) || !bytes.Equal(raw[49:81], digest[:]) {
			return ErrDenied
		}
		nonce := raw[81:97]
		nonzero := false
		for _, v := range nonce {
			nonzero = nonzero || v != 0
		}
		expiry := int64(binary.BigEndian.Uint64(raw[97:105]))
		if !nonzero || expiry <= now.Unix() || expiry > now.Add(5*time.Minute).Unix() {
			return ErrDenied
		}
		platforms := map[string]byte{"web": 1, "ios": 2, "android": 3}
		providers := map[string]byte{"webpush": 1, "apns": 2, "fcm": 3}
		if platforms[b.Platform] == 0 || providers[b.Provider] == 0 || raw[105] != platforms[b.Platform] || raw[106] != providers[b.Provider] {
			return ErrDenied
		}
		return nil
	}
}
