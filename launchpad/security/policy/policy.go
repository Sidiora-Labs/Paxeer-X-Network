// Package policy contains fail-closed authority and request admission primitives.
package policy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

var ErrDenied = errors.New("policy: denied")

type Identity struct {
	Actor, Audience, Kind string
	Issued, Expires       time.Time
	MFA                   bool
	Scopes                map[string]bool
}

// Verifier must authenticate D11 session/workload credentials, never actor headers.
type Verifier interface {
	Verify(context.Context, *http.Request) (Identity, error)
}
type Decision struct {
	Actor, Scope, RequestID, Version string
	Allowed                          bool
}
type Journal interface {
	Record(context.Context, Decision) error
}

func Authorize(v Verifier, j Journal, audience, scope, version string, next http.Handler) http.Handler {
	return authorizeCaptured(v, j, audience, scope, version, next, nil)
}
func MeasuredAuthorize(v Verifier, j Journal, audience, scope, version string, next http.Handler, capture *MiddlewareCapture) http.Handler {
	if capture == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "capture_unavailable", 503) })
	}
	return authorizeCaptured(v, j, audience, scope, version, next, capture)
}
func authorizeCaptured(v Verifier, j Journal, audience, scope, version string, next http.Handler, capture *MiddlewareCapture) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v == nil || j == nil || audience == "" || scope == "" || version == "" {
			http.Error(w, "unavailable", 503)
			return
		}
		started := time.Now()
		id, err := v.Verify(r.Context(), r)
		if err != nil {
			requestID := r.Header.Get("X-Request-ID")
			if len(requestID) > 128 {
				requestID = ""
			}
			if j.Record(r.Context(), Decision{Scope: scope, RequestID: requestID, Version: version, Allowed: false}) != nil {
				if capture != nil {
					_ = capture.Record(r, "authorization", started, false, true)
				}
				http.Error(w, "unavailable", 503)
				return
			}
			if capture != nil && capture.Record(r, "authorization", started, false, false) != nil {
				http.Error(w, "capture_unavailable", 503)
				return
			}
			http.Error(w, "unauthorized", 401)
			return
		}
		now := time.Now()
		allowed := id.Actor != "" && id.Audience == audience && id.Kind == "operator" && id.MFA && !id.Issued.After(now) && id.Expires.After(now) && id.Expires.Sub(id.Issued) <= 15*time.Minute && id.Scopes[scope]
		d := Decision{id.Actor, scope, r.Header.Get("X-Request-ID"), version, allowed}
		if len(d.RequestID) > 128 {
			d.RequestID = ""
		}
		if j.Record(r.Context(), d) != nil {
			if capture != nil {
				_ = capture.Record(r, "authorization", started, false, true)
			}
			http.Error(w, "unavailable", 503)
			return
		}
		if capture != nil && capture.Record(r, "authorization", started, allowed, false) != nil {
			http.Error(w, "capture_unavailable", 503)
			return
		}
		if !allowed {
			http.Error(w, "forbidden", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type Route struct {
	Method, Path, Audience, Scope, Class string
	BodyLimit                            int64
	Public                               bool
}

func validRouteMethod(method string) bool {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return true
	}
	return false
}
func validRouteClass(class string) bool {
	_, ok := frozenBudget(class)
	return ok || class == "operator"
}
func ValidateRoutes(routes []Route) error {
	if len(routes) == 0 || len(routes) > 10000 {
		return ErrDenied
	}
	seen := map[string]bool{}
	for _, r := range routes {
		k := r.Method + " " + r.Path
		if seen[k] || !validRouteMethod(r.Method) || !strings.HasPrefix(r.Path, "/") || strings.ContainsAny(r.Path, "?\r\n\x00") || !validRouteClass(r.Class) || r.BodyLimit < 0 || r.BodyLimit > 8<<20 || r.Public && (r.Method != "GET" && r.Method != "HEAD" || r.Class != "public-read") || (!r.Public && (r.Audience == "" || r.Scope == "")) {
			return ErrDenied
		}
		seen[k] = true
	}
	return nil
}

// DecodeMutation bounds bytes before any caller mutation, rejects duplicate fields,
// excessive nesting/arrays, unknown fields and trailing JSON.
func DecodeMutation(r *http.Request, dst any, limit int64) error {
	if r == nil || r.Body == nil || limit <= 0 || limit > 8<<20 {
		return ErrDenied
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if e != nil {
		return e
	}
	if int64(len(b)) > limit {
		return &http.MaxBytesError{Limit: limit}
	}
	if !validUnicode(b) {
		return ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if e = walk(d, 0); e != nil {
		return e
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrDenied
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return decodeDestination(d, dst)
}
func walk(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrDenied
	}
	t, e := d.Token()
	if e != nil {
		return e
	}
	c, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if c == '{' {
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return ErrDenied
			}
			seen[s] = true
			if len(seen) > 1024 {
				return ErrDenied
			}
			if e = walk(d, depth+1); e != nil {
				return e
			}
		}
	} else if c == '[' {
		n := 0
		for d.More() {
			n++
			if n > 1000 {
				return ErrDenied
			}
			if e = walk(d, depth+1); e != nil {
				return e
			}
		}
	} else {
		return ErrDenied
	}
	_, e = d.Token()
	return e
}

// PublicURL is enforced for initial URLs and each redirect. DialPublic repeats
// resolution at connect and dials only the vetted literal IP, closing DNS rebinding.
func PublicURL(u *url.URL, hosts map[string]bool) error {
	if u == nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || !hosts[strings.ToLower(u.Hostname())] || (u.Port() != "" && u.Port() != "443") || (net.ParseIP(u.Hostname()) != nil && !publicIP(net.ParseIP(u.Hostname()))) {
		return ErrDenied
	}
	return nil
}
func publicIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !net.ParseIP("100.64.0.0").Equal(ip) && !inCIDR(ip, "100.64.0.0/10") && !inCIDR(ip, "198.18.0.0/15") && !inCIDR(ip, "192.0.0.0/24") && !inCIDR(ip, "64:ff9b::/96") && !inCIDR(ip, "64:ff9b:1::/48") && !inCIDR(ip, "2002::/16") && !inCIDR(ip, "2001::/32")
}
func inCIDR(ip net.IP, s string) bool { _, n, _ := net.ParseCIDR(s); return n.Contains(ip) }
func DialPublic(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil || port != "443" || network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, ErrDenied
	}
	ips, e := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if e != nil || len(ips) == 0 {
		return nil, ErrDenied
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, ErrDenied
		}
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
}
func PublicClient(hosts map[string]bool) *http.Client {
	copyHosts := map[string]bool{}
	for h, v := range hosts {
		copyHosts[strings.ToLower(h)] = v
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: validatedTransport{hosts: copyHosts, base: &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, n, a string) (net.Conn, error) {
		h, _, e := net.SplitHostPort(a)
		if e != nil || !copyHosts[strings.ToLower(h)] {
			return nil, ErrDenied
		}
		return DialPublic(ctx, n, a)
	}}}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return ErrDenied
		}
		return PublicURL(r.URL, copyHosts)
	}}
}

func decodeDestination(d *json.Decoder, dst any) error {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return ErrDenied
	}
	candidate := reflect.New(v.Elem().Type())
	snapshot, err := json.Marshal(dst)
	if err != nil {
		return ErrDenied
	}
	if err = json.Unmarshal(snapshot, candidate.Interface()); err != nil {
		return ErrDenied
	}
	if err := d.Decode(candidate.Interface()); err != nil {
		return err
	}
	v.Elem().Set(candidate.Elem())
	return nil
}

type Budget struct{ PerMinute, Burst int64 }

var Defaults = map[string]Budget{"public-read": {120, 30}, "account-read": {600, 100}, "quote": {120, 20}, "account-write": {60, 10}}

func frozenBudget(class string) (Budget, bool) {
	switch class {
	case "public-read":
		return Budget{120, 30}, true
	case "account-read":
		return Budget{600, 100}, true
	case "quote":
		return Budget{120, 20}, true
	case "account-write":
		return Budget{60, 10}, true
	default:
		return Budget{}, false
	}
}

// Take is an atomic Redis-time token bucket. No local mutation fallback exists.
func Take(ctx context.Context, r *redis.Client, namespace, class, principal string, cost int64) (bool, time.Duration, error) {
	b, ok := frozenBudget(class)
	if !ok || r == nil || len(namespace) == 0 || len(namespace) > 100 || len(principal) == 0 || len(principal) > 512 || cost < 1 || cost > b.Burst {
		return false, 0, ErrDenied
	}
	key := fmt.Sprintf("%s:budget:%s:%x", namespace, class, sha256.Sum256([]byte(principal)))
	v, e := r.Eval(ctx, `local t=redis.call('TIME'); local now=t[1]*1000+math.floor(t[2]/1000);local cap=tonumber(ARGV[1]);local rate=tonumber(ARGV[2])/60000;local cost=tonumber(ARGV[3]);local s=redis.call('HMGET',KEYS[1],'tokens','at');local at=tonumber(s[2]) or now; local tokens=math.min(cap,(tonumber(s[1]) or cap)+math.max(0,now-at)*rate);local allowed=0;if tokens>=cost then tokens=tokens-cost;allowed=1 end;redis.call('HSET',KEYS[1],'tokens',tokens,'at',math.max(at,now));redis.call('PEXPIRE',KEYS[1],120000);return {allowed,math.ceil(math.max(0,cost-tokens)/rate)}`, []string{key}, b.Burst, b.PerMinute, cost).Int64Slice()
	if e != nil {
		return false, 0, e
	}
	if len(v) != 2 || (v[0] != 0 && v[0] != 1) || v[1] < 0 || v[1] > 60000 {
		return false, 0, ErrDenied
	}
	return v[0] == 1, time.Duration(v[1]) * time.Millisecond, nil
}

type Key struct {
	ID, Version, Audience string
	Activate, Retire      time.Time
	Value                 []byte
	Revoked               bool
}
type KeyRing struct {
	mu   sync.RWMutex
	keys []Key
}

func (k *KeyRing) Replace(keys []Key) error {
	if len(keys) == 0 || len(keys) > 2 {
		return ErrDenied
	}
	seen := map[string]bool{}
	copyKeys := make([]Key, len(keys))
	for i, v := range keys {
		if len(v.Value) < 32 || len(v.Value) > 4096 || v.ID == "" || v.Version == "" || v.Audience == "" || !v.Retire.After(v.Activate) || seen[v.Version] || i > 0 && (v.ID != keys[0].ID || v.Audience != keys[0].Audience || v.Activate.Equal(keys[0].Activate)) {
			return ErrDenied
		}
		seen[v.Version] = true
		v.Value = append([]byte(nil), v.Value...)
		copyKeys[i] = v
	}
	if len(keys) == 2 {
		a, b := keys[0], keys[1]
		start := a.Activate
		if b.Activate.After(start) {
			start = b.Activate
		}
		end := a.Retire
		if b.Retire.Before(end) {
			end = b.Retire
		}
		if end.Sub(start) > 15*time.Minute {
			return ErrDenied
		}
	}
	k.mu.Lock()
	k.keys = copyKeys
	k.mu.Unlock()
	return nil
}
func (k *KeyRing) Sign(aud string, body []byte, now time.Time) (string, string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	var best *Key
	for i := range k.keys {
		v := &k.keys[i]
		if v.Audience == aud && !v.Revoked && !now.Before(v.Activate) && now.Before(v.Retire) && (best == nil || v.Activate.After(best.Activate)) {
			best = v
		}
	}
	if best == nil {
		return "", "", ErrDenied
	}
	m := hmac.New(sha256.New, best.Value)
	m.Write(body)
	return best.Version, hex.EncodeToString(m.Sum(nil)), nil
}
func (k *KeyRing) Verify(aud, version, sig string, body []byte, now time.Time) bool {
	raw, e := hex.DecodeString(sig)
	if e != nil || len(raw) != 32 {
		return false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	for _, v := range k.keys {
		if v.Version == version && v.Audience == aud && !v.Revoked && !now.Before(v.Activate) && now.Before(v.Retire) {
			m := hmac.New(sha256.New, v.Value)
			m.Write(body)
			return hmac.Equal(raw, m.Sum(nil))
		}
	}
	return false
}

// SafeHeaders intentionally emits no request values: only stable presence booleans.
func SafeHeaders(h http.Header) map[string]bool {
	out := map[string]bool{}
	for _, n := range []string{"Authorization", "Cookie", "X-API-Key", "X-Signature", "X-Stream-Key"} {
		out[n] = len(h.Values(n)) > 0
	}
	return out
}

type validatedTransport struct {
	hosts map[string]bool
	base  http.RoundTripper
}

func (t validatedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if PublicURL(r.URL, t.hosts) != nil {
		return nil, ErrDenied
	}
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, ErrDenied
	}
	return response, nil
}

func validUnicode(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			return false
		}
		if b[i] != 'u' {
			continue
		}
		if i+4 >= len(b) {
			return false
		}
		v, e := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if e != nil {
			return false
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return false
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
				return false
			}
			v, e = strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
			if e != nil || v < 0xDC00 || v > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
