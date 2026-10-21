package notifications

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"time"
)

type APNS struct {
	Credentials                           *Credentials
	Authority                             TargetAuthority
	KeyRef, KeyIDRef, TeamIDRef, TopicRef string
	Sandbox                               bool
	client                                *http.Client
}

func NewAPNS(c *Credentials, a TargetAuthority, key, keyID, team, topic string, sandbox bool) (*APNS, error) {
	if c == nil || a == nil {
		return nil, ErrInvalid
	}
	values := []string{key, keyID, team, topic}
	for _, ref := range values {
		if _, e := c.Get(ref); e != nil {
			return nil, e
		}
	}
	raw, _ := c.Get(key)
	if _, e := ecKey(raw); e != nil {
		return nil, e
	}
	return &APNS{c, a, key, keyID, team, topic, sandbox, providerClient()}, nil
}
func (*APNS) Provider() string     { return "apns" }
func (*APNS) MaxPayloadBytes() int { return 4096 }
func (a *APNS) Send(ctx context.Context, target PushTarget, p Payload) (SendResult, error) {
	unknown := SendResult{State: "unknown", Reason: "provider_uncertain"}
	if target.Provider != "apns" || target.Status != "active" || ValidatePayload(p, 4096) != nil || validateLink(p) != nil {
		return unknown, ErrInvalid
	}
	endpoint, e := a.Authority.ResolveEndpoint(ctx, target)
	if e != nil {
		return unknown, e
	}
	if !regexp.MustCompile(`^[a-fA-F0-9]{64,200}$`).MatchString(endpoint.Token) {
		return unknown, ErrInvalid
	}
	raw, e := a.Credentials.Get(a.KeyRef)
	if e != nil {
		return unknown, e
	}
	key, e := ecKey(raw)
	if e != nil {
		return unknown, e
	}
	kid, e := a.Credentials.Get(a.KeyIDRef)
	if e != nil {
		return unknown, e
	}
	team, e := a.Credentials.Get(a.TeamIDRef)
	if e != nil {
		return unknown, e
	}
	topic, e := a.Credentials.Get(a.TopicRef)
	if e != nil {
		return unknown, e
	}
	jwt, e := ecJWT(key, map[string]string{"alg": "ES256", "kid": kid}, map[string]any{"iss": team, "iat": time.Now().Unix()})
	if e != nil {
		return unknown, e
	}
	body := map[string]any{"aps": map[string]any{"alert": map[string]string{"body": p.Summary}, "sound": "default"}, "deepLink": p.AuthenticatedDeepLink, "notificationId": p.IdempotencyKey}
	encoded, e := json.Marshal(body)
	if e != nil || len(encoded) > 4096 {
		return unknown, ErrInvalid
	}
	host := "https://api.push.apple.com"
	if a.Sandbox {
		host = "https://api.sandbox.push.apple.com"
	}
	req, e := jsonRequest(ctx, "POST", host+"/3/device/"+endpoint.Token, body)
	if e != nil {
		return unknown, e
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", topic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "5")
	req.Header.Set("apns-expiration", "0")
	status, headers, b, e := sendRequest(a.client, req)
	if e != nil {
		return unknown, e
	}
	return apnsResult(status, headers, b)
}

func apnsResult(status int, headers http.Header, b []byte) (SendResult, error) {
	unknown := SendResult{State: "unknown", Reason: "provider_uncertain"}
	if status == 200 {
		receipt := headers.Get("apns-id")
		if receipt == "" {
			return unknown, ErrUnavailable
		}
		return SendResult{State: "accepted", Receipt: receipt}, nil
	}
	var rejection struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(b, &rejection)
	if (status == 410 && rejection.Reason == "Unregistered") || (status == 400 && (rejection.Reason == "BadDeviceToken" || rejection.Reason == "DeviceTokenNotForTopic")) {
		return SendResult{State: "rejected", Reason: rejection.Reason, DisableTarget: true}, nil
	}
	if status == 429 || status >= 500 {
		return SendResult{State: "retry", Reason: "provider_unavailable"}, nil
	}
	return SendResult{State: "rejected", Reason: "provider_rejected"}, nil
}
