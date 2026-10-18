package notifications

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type FCM struct {
	Credentials       *Credentials
	Authority         TargetAuthority
	ServiceAccountRef string
	client            *http.Client
}
type serviceAccount struct {
	Type     string `json:"type"`
	Project  string `json:"project_id"`
	Email    string `json:"client_email"`
	Key      string `json:"private_key"`
	TokenURI string `json:"token_uri"`
}

func fcmAccount(raw string) (serviceAccount, *rsa.PrivateKey, error) {
	var a serviceAccount
	if e := json.Unmarshal([]byte(raw), &a); e != nil {
		return a, nil, e
	}
	if a.Type != "service_account" || a.Project == "" || strings.ContainsAny(a.Project, "/ ?#") || a.Email == "" || a.TokenURI != "https://oauth2.googleapis.com/token" {
		return a, nil, ErrInvalid
	}
	block, _ := pem.Decode([]byte(a.Key))
	if block == nil {
		return a, nil, ErrInvalid
	}
	parsed, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		return a, nil, e
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 {
		return a, nil, ErrInvalid
	}
	return a, key, nil
}
func NewFCM(c *Credentials, a TargetAuthority, ref string) (*FCM, error) {
	if c == nil || a == nil {
		return nil, ErrInvalid
	}
	raw, e := c.Get(ref)
	if e != nil {
		return nil, e
	}
	if _, _, e = fcmAccount(raw); e != nil {
		return nil, e
	}
	return &FCM{c, a, ref, providerClient()}, nil
}
func (*FCM) Provider() string     { return "fcm" }
func (*FCM) MaxPayloadBytes() int { return 4096 }
func (f *FCM) Send(ctx context.Context, target PushTarget, p Payload) (SendResult, error) {
	unknown := SendResult{State: "unknown", Reason: "provider_uncertain"}
	if target.Provider != "fcm" || target.Status != "active" || ValidatePayload(p, 4096) != nil || validateLink(p) != nil {
		return unknown, ErrInvalid
	}
	raw, e := f.Credentials.Get(f.ServiceAccountRef)
	if e != nil {
		return unknown, e
	}
	a, key, e := fcmAccount(raw)
	if e != nil {
		return unknown, e
	}
	endpoint, e := f.Authority.ResolveEndpoint(ctx, target)
	if e != nil {
		return unknown, e
	}
	if endpoint.Token == "" || len(endpoint.Token) > 4096 {
		return unknown, ErrInvalid
	}
	now := time.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": a.Email, "scope": "https://www.googleapis.com/auth/firebase.messaging", "aud": a.TokenURI, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix()})
	input := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(input))
	sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if e != nil {
		return unknown, e
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {input + "." + b64(sig)}}
	req, e := http.NewRequestWithContext(ctx, "POST", a.TokenURI, strings.NewReader(form.Encode()))
	if e != nil {
		return unknown, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, _, body, e := sendRequest(f.client, req)
	if e != nil {
		return unknown, e
	}
	if status != 200 {
		return SendResult{State: "retry", Reason: "oauth_unavailable"}, nil
	}
	var oauth struct {
		Token string `json:"access_token"`
	}
	if e = json.Unmarshal(body, &oauth); e != nil || oauth.Token == "" {
		return unknown, ErrUnavailable
	}
	message := map[string]any{"message": map[string]any{"token": endpoint.Token, "notification": map[string]string{"body": p.Summary}, "data": map[string]string{"deepLink": p.AuthenticatedDeepLink, "notificationId": p.IdempotencyKey}, "android": map[string]string{"ttl": "0s"}}}
	encoded, _ := json.Marshal(message)
	if len(encoded) > 4096 {
		return unknown, ErrInvalid
	}
	req, e = jsonRequest(ctx, "POST", "https://fcm.googleapis.com/v1/projects/"+a.Project+"/messages:send", message)
	if e != nil {
		return unknown, e
	}
	req.Header.Set("Authorization", "Bearer "+oauth.Token)
	status, _, body, e = sendRequest(f.client, req)
	if e != nil {
		return unknown, e
	}
	return fcmResult(status, body)
}

func fcmResult(status int, body []byte) (SendResult, error) {
	unknown := SendResult{State: "unknown", Reason: "provider_uncertain"}
	if status == 200 {
		var accepted struct {
			Name string `json:"name"`
		}
		if e := json.Unmarshal(body, &accepted); e != nil || accepted.Name == "" {
			return unknown, ErrUnavailable
		}
		return SendResult{State: "accepted", Receipt: accepted.Name}, nil
	}
	var failure struct {
		Error struct {
			Details []struct {
				Type string `json:"@type"`
				Code string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &failure)
	for _, detail := range failure.Error.Details {
		if status == 404 && detail.Type == "type.googleapis.com/google.firebase.fcm.v1.FcmError" && detail.Code == "UNREGISTERED" {
			return SendResult{State: "rejected", Reason: "UNREGISTERED", DisableTarget: true}, nil
		}
	}
	if status == 429 || status >= 500 {
		return SendResult{State: "retry", Reason: "provider_unavailable"}, nil
	}
	return SendResult{State: "rejected", Reason: "provider_rejected"}, nil
}
