package notifications

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
func ecKey(value string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, ErrInvalid
	}
	raw, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	if e != nil {
		return nil, e
	}
	key, ok := raw.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, ErrInvalid
	}
	return key, nil
}
func ecJWT(key *ecdsa.PrivateKey, header, claims any) (string, error) {
	h, e := json.Marshal(header)
	if e != nil {
		return "", e
	}
	c, e := json.Marshal(claims)
	if e != nil {
		return "", e
	}
	message := b64(h) + "." + b64(c)
	digest := sha256.Sum256([]byte(message))
	rr, ss, e := ecdsa.Sign(rand.Reader, key, digest[:])
	if e != nil {
		return "", e
	}
	sig := make([]byte, 64)
	rr.FillBytes(sig[:32])
	ss.FillBytes(sig[32:])
	return message + "." + b64(sig), nil
}
func providerClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{Proxy: nil, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 4 * time.Second, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(addr)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, e
		}
		for _, ip := range ips {
			if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsUnspecified() || ip.IP.IsLinkLocalUnicast() || !ip.IP.IsGlobalUnicast() {
				return nil, ErrForbidden
			}
		}
		if len(ips) == 0 {
			return nil, ErrUnavailable
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}}}
}
func sendRequest(client *http.Client, req *http.Request) (int, http.Header, []byte, error) {
	response, e := client.Do(req)
	if e != nil {
		return 0, nil, nil, e
	}
	defer response.Body.Close()
	body, e := io.ReadAll(io.LimitReader(response.Body, 65537))
	if len(body) > 65536 {
		return 0, nil, nil, ErrInvalid
	}
	return response.StatusCode, response.Header, body, e
}
func jsonRequest(ctx context.Context, method, endpoint string, body any) (*http.Request, error) {
	b, e := json.Marshal(body)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(b))
	if e == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, e
}
func validateLink(p Payload) error {
	u, e := url.Parse(p.AuthenticatedDeepLink)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || !strings.HasPrefix(u.Path, "/notifications/") {
		return ErrInvalid
	}
	return nil
}

type socialAuthority struct {
	rpc   accountRPC
	store discovery.Store
}

func (s socialAuthority) Graph(ctx context.Context, account string) (discovery.GraphSnapshot, error) {
	var v discovery.GraphSnapshot
	e := s.rpc.call(ctx, "GET", "/internal/v2/social/graph/"+url.PathEscape(account), nil, &v)
	if e != nil {
		return v, e
	}
	if v.AccountID != account || v.Revision == 0 || v.ObservedAt.IsZero() || v.ObservedAt.After(time.Now()) || time.Since(v.ObservedAt) > 60*time.Second {
		return v, ErrUnavailable
	}
	return v, nil
}
func (s socialAuthority) ResolveVisibility(ctx context.Context, id, account string) (discovery.ModerationDecision, error) {
	var v discovery.ModerationDecision
	ref, e := s.store.Source(ctx, id)
	if e != nil {
		return v, e
	}
	e = s.rpc.call(ctx, "GET", "/internal/v2/social/visibility/"+url.PathEscape(ref.Kind)+"/"+url.PathEscape(id)+"?accountId="+url.QueryEscape(account), nil, &v)
	if e != nil {
		return v, e
	}
	if v.ResourceID != id || v.Revision == 0 || v.ObservedAt.IsZero() || v.ObservedAt.After(time.Now()) || time.Since(v.ObservedAt) > 60*time.Second {
		return v, ErrUnavailable
	}
	return v, nil
}
func (s socialAuthority) ResolvePublic(ctx context.Context, kind, id string) (discovery.PublicResource, error) {
	var v discovery.PublicResource
	e := s.rpc.call(ctx, "GET", "/internal/v2/social/public/"+url.PathEscape(kind)+"/"+url.PathEscape(id), nil, &v)
	if e != nil {
		return v, e
	}
	if v.Ref.ID != id || v.Ref.Kind != kind || v.Ref.Validate() != nil || !v.Ref.Public || !v.Ref.Authorized {
		return v, ErrUnavailable
	}
	return v, nil
}
func (s socialAuthority) ReviewCallout(ctx context.Context, actor, pool, body string) (discovery.ModerationDecision, error) {
	var v discovery.ModerationDecision
	in := struct{ Actor, Pool, Body string }{actor, pool, body}
	e := s.rpc.call(ctx, "POST", "/internal/v2/social/callouts:review", in, &v)
	return v, e
}
func (s socialAuthority) Publish(ctx context.Context, kind, id string, raw json.RawMessage) error {
	if kind != "callout-target-ref-v2" {
		return ErrInvalid
	}
	return s.rpc.call(ctx, "POST", "/internal/v2/social/callout-authority", json.RawMessage(raw), nil)
}

type sourceAuthority struct {
	pools    discovery.SourceAuthority
	accounts *AccountClient
	social   socialAuthority
}

func (s sourceAuthority) CanonicalPool(ctx context.Context, pool string) (bool, error) {
	if s.pools == nil {
		return false, ErrUnavailable
	}
	return s.pools.CanonicalPool(ctx, pool)
}
func (s sourceAuthority) VerifiedAccount(ctx context.Context, actor string) (bool, error) {
	if s.accounts == nil {
		return false, ErrUnavailable
	}
	return s.accounts.VerifiedAccount(ctx, actor)
}
func (s sourceAuthority) ResolvePublic(ctx context.Context, kind, id string) (discovery.PublicResource, error) {
	if kind == "pool" || kind == "milestone" {
		if s.pools == nil {
			return discovery.PublicResource{}, ErrUnavailable
		}
		return s.pools.ResolvePublic(ctx, kind, id)
	}
	return s.social.ResolvePublic(ctx, kind, id)
}
func (s sourceAuthority) ReviewCallout(ctx context.Context, actor, pool, body string) (discovery.ModerationDecision, error) {
	return s.social.ReviewCallout(ctx, actor, pool, body)
}

type eventPublisher struct {
	triggers TriggerConsumer
	social   socialAuthority
}

func (p eventPublisher) Publish(ctx context.Context, kind, id string, raw json.RawMessage) error {
	switch kind {
	case "threshold-crossing-v2":
		return p.triggers.Publish(ctx, kind, id, raw)
	case "callout-target-ref-v2":
		return p.social.Publish(ctx, kind, id, raw)
	default:
		return ErrInvalid
	}
}
