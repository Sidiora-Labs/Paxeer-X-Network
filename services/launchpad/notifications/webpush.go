package notifications

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WebPush struct {
	Credentials        *Credentials
	Authority          TargetAuthority
	KeyRef, SubjectRef string
	client             *http.Client
}

func NewWebPush(c *Credentials, a TargetAuthority, key, subject string) (*WebPush, error) {
	if c == nil || a == nil {
		return nil, ErrInvalid
	}
	raw, e := c.Get(key)
	if e != nil {
		return nil, e
	}
	if _, e = ecKey(raw); e != nil {
		return nil, e
	}
	sub, e := c.Get(subject)
	if e != nil {
		return nil, e
	}
	if !strings.HasPrefix(sub, "mailto:") && !strings.HasPrefix(sub, "https://") {
		return nil, ErrInvalid
	}
	return &WebPush{c, a, key, subject, providerClient()}, nil
}
func (*WebPush) Provider() string     { return "webpush" }
func (*WebPush) MaxPayloadBytes() int { return 3993 }
func subscription(u string) error {
	v, e := url.Parse(u)
	if e != nil || v.Scheme != "https" || v.User != nil || v.Fragment != "" || v.Port() != "" {
		return ErrInvalid
	}
	switch v.Hostname() {
	case "fcm.googleapis.com", "updates.push.services.mozilla.com", "web.push.apple.com":
	default:
		return ErrForbidden
	}
	if v.Path == "" || v.Path == "/" {
		return ErrInvalid
	}
	return nil
}
func extract(salt, ikm []byte) []byte {
	h := hmac.New(sha256.New, salt)
	h.Write(ikm)
	return h.Sum(nil)
}
func expand(prk, info []byte, n int) []byte {
	h := hmac.New(sha256.New, prk)
	h.Write(info)
	h.Write([]byte{1})
	return h.Sum(nil)[:n]
}
func encryptPush(endpoint Endpoint, plain []byte) ([]byte, error) {
	ua, e := base64.RawURLEncoding.DecodeString(endpoint.PublicKey)
	if e != nil || len(ua) != 65 {
		return nil, ErrInvalid
	}
	auth, e := base64.RawURLEncoding.DecodeString(endpoint.Auth)
	if e != nil || len(auth) != 16 {
		return nil, ErrInvalid
	}
	public, e := ecdh.P256().NewPublicKey(ua)
	if e != nil {
		return nil, e
	}
	private, e := ecdh.P256().GenerateKey(rand.Reader)
	if e != nil {
		return nil, e
	}
	shared, e := private.ECDH(public)
	if e != nil {
		return nil, e
	}
	server := private.PublicKey().Bytes()
	info := append([]byte("WebPush: info\x00"), ua...)
	info = append(info, server...)
	ikm := expand(extract(auth, shared), info, 32)
	salt := make([]byte, 16)
	if _, e = rand.Read(salt); e != nil {
		return nil, e
	}
	prk := extract(salt, ikm)
	cek := expand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := expand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	block, e := aes.NewCipher(cek)
	if e != nil {
		return nil, e
	}
	gcm, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	record := append(append([]byte{}, plain...), 2)
	sealed := gcm.Seal(nil, nonce, record, nil)
	header := append([]byte{}, salt...)
	size := make([]byte, 4)
	binary.BigEndian.PutUint32(size, 4096)
	header = append(header, size...)
	header = append(header, byte(len(server)))
	header = append(header, server...)
	return append(header, sealed...), nil
}
func (w *WebPush) Send(ctx context.Context, target PushTarget, p Payload) (SendResult, error) {
	unknown := SendResult{State: "unknown", Reason: "provider_uncertain"}
	if target.Provider != "webpush" || target.Status != "active" || ValidatePayload(p, w.MaxPayloadBytes()) != nil || validateLink(p) != nil {
		return unknown, ErrInvalid
	}
	endpoint, e := w.Authority.ResolveEndpoint(ctx, target)
	if e != nil {
		return unknown, e
	}
	if endpoint.Permission != "granted" || subscription(endpoint.URL) != nil {
		return unknown, ErrForbidden
	}
	raw, e := w.Credentials.Get(w.KeyRef)
	if e != nil {
		return unknown, e
	}
	key, e := ecKey(raw)
	if e != nil {
		return unknown, e
	}
	subject, e := w.Credentials.Get(w.SubjectRef)
	if e != nil {
		return unknown, e
	}
	u, _ := url.Parse(endpoint.URL)
	jwt, e := ecJWT(key, map[string]string{"typ": "JWT", "alg": "ES256"}, map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": time.Now().Add(5 * time.Minute).Unix(), "sub": subject})
	if e != nil {
		return unknown, e
	}
	plain, e := json.Marshal(p)
	if e != nil {
		return unknown, e
	}
	body, e := encryptPush(endpoint, plain)
	if e != nil || len(body) > 4096 {
		return unknown, ErrInvalid
	}
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint.URL, bytes.NewReader(body))
	if e != nil {
		return unknown, e
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "0")
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+b64(elliptic.Marshal(key.Curve, key.X, key.Y)))
	req.Header.Set("Topic", p.IdempotencyKey[:min(32, len(p.IdempotencyKey))])
	status, headers, _, e := sendRequest(w.client, req)
	if e != nil {
		return unknown, e
	}
	return webPushResult(status, headers)
}

func webPushResult(status int, headers http.Header) (SendResult, error) {
	if status == 201 || status == 202 {
		receipt := headers.Get("Location")
		if receipt == "" {
			return SendResult{State: "unknown", Reason: "provider_receipt_unavailable"}, nil
		}
		return SendResult{State: "accepted", Receipt: receipt}, nil
	}
	if status == 404 || status == 410 {
		return SendResult{State: "rejected", Reason: "subscription_gone", DisableTarget: true}, nil
	}
	if status == 429 || status >= 500 {
		return SendResult{State: "retry", Reason: "provider_unavailable"}, nil
	}
	return SendResult{State: "rejected", Reason: "provider_rejected"}, nil
}
