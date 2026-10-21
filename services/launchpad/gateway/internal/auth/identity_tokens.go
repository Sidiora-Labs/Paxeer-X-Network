package auth

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"
)

// SigningKey stores references and bounded verification metadata, never private key bytes.
type SigningKey struct {
	ID, SecretReference string
	ActiveAt, RetireAt  time.Time
}
type SecretResolver interface {
	ResolveIdentityKey(context.Context, string) ([]byte, error)
}
type cachedIdentityKey struct {
	secret []byte
	until  time.Time
}
type IdentityTokens struct {
	issuer, audience, active string
	keys                     map[string]SigningKey
	resolver                 SecretResolver
	now                      func() time.Time
	mu                       sync.Mutex
	cache                    map[string]cachedIdentityKey
	observe                  func(string)
}
type IdentityClaims struct {
	Issuer    string `json:"iss"`
	Audience  string `json:"aud"`
	AccountID string `json:"sub"`
	AccessID  string `json:"jti"`
	SessionID string `json:"sid"`
	Epoch     int64  `json:"ae"`
	IssuedAt  int64  `json:"iat"`
	NotBefore int64  `json:"nbf"`
	ExpiresAt int64  `json:"exp"`
}

func NewIdentityTokens(issuer, audience, active string, keys []SigningKey, resolver SecretResolver, now func() time.Time) (*IdentityTokens, error) {
	if issuer == "" || audience == "" || active == "" || resolver == nil || len(keys) > 8 {
		return nil, invalid()
	}
	if now == nil {
		now = time.Now
	}
	m := map[string]SigningKey{}
	for _, k := range keys {
		if k.ID == "" || len(k.ID) > 64 || k.SecretReference == "" || !k.RetireAt.After(k.ActiveAt) {
			return nil, invalid()
		}
		if _, ok := m[k.ID]; ok {
			return nil, invalid()
		}
		m[k.ID] = k
	}
	activeKey, ok := m[active]
	if !ok {
		return nil, invalid()
	}
	for id, k := range m {
		if id != active && k.RetireAt.After(activeKey.ActiveAt.Add(15*time.Minute)) {
			return nil, invalid()
		}
	}
	return &IdentityTokens{issuer: issuer, audience: audience, active: active, keys: m, resolver: resolver, now: now, cache: map[string]cachedIdentityKey{}}, nil
}
func (t *IdentityTokens) key(ctx context.Context, id string) ([]byte, error) {
	k, ok := t.keys[id]
	now := t.now()
	if !ok {
		t.observeCategory("unknown_kid")
		return nil, unauthorized()
	}
	if now.Before(k.ActiveAt) {
		t.observeCategory("key_not_active")
		return nil, unauthorized()
	}
	if !now.Before(k.RetireAt) {
		t.observeCategory("key_retired")
		return nil, unauthorized()
	}
	t.mu.Lock()
	dependencyFailed := false
	defer func() {
		t.mu.Unlock()
		if dependencyFailed {
			t.observeCategory("key_dependency_unavailable")
		}
	}()
	c, ok := t.cache[id]
	if ok && now.Before(c.until) {
		return c.secret, nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	secret, e := t.resolver.ResolveIdentityKey(resolveCtx, k.SecretReference)
	if e != nil || len(secret) < 32 {
		dependencyFailed = true
		return nil, unavailable()
	}
	until := now.Add(time.Minute)
	if until.After(k.RetireAt) {
		until = k.RetireAt
	}
	secret = append([]byte(nil), secret...)
	t.cache[id] = cachedIdentityKey{secret, until}
	return secret, nil
}
func (t *IdentityTokens) Mint(ctx context.Context, p Principal) (string, error) {
	if !validUUID(p.accountID) || !validUUID(p.sessionID) || p.epoch < 0 {
		return "", unauthorized()
	}
	now := t.now()
	exp := now.Add(15 * time.Minute)
	if !exp.Before(t.keys[t.active].RetireAt) && !exp.Equal(t.keys[t.active].RetireAt) {
		return "", unavailable()
	}
	accessID := p.accessID
	if accessID == "" {
		var err error
		accessID, err = randomSecret()
		if err != nil {
			return "", err
		}
	}
	c := IdentityClaims{Issuer: t.issuer, Audience: t.audience, AccountID: p.accountID, AccessID: accessID, SessionID: p.sessionID, Epoch: p.epoch, IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: exp.Unix()}
	return t.signClaims(ctx, t.active, c)
}
func (t *IdentityTokens) signClaims(ctx context.Context, id string, c IdentityClaims) (string, error) {
	key, e := t.key(ctx, id)
	if e != nil {
		return "", e
	}
	h, _ := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		KeyID     string `json:"kid"`
	}{"HS256", "JWT", id})
	payload, _ := json.Marshal(c)
	input := b64.EncodeToString(h) + "." + b64.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(input))
	token := input + "." + b64.EncodeToString(mac.Sum(nil))
	if len(token) > 2048 {
		return "", unavailable()
	}
	return token, nil
}
func (t *IdentityTokens) Verify(ctx context.Context, token string) (claims IdentityClaims, failure error) {
	defer func() {
		if failure != nil {
			category := "claims_rejected"
			var public *PublicError
			if errors.As(failure, &public) && public.Status == 503 {
				category = "key_dependency_unavailable"
			}
			t.observeCategory(category)
		}
	}()
	var c IdentityClaims
	if len(token) > 2048 {
		return c, unauthorized()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return c, unauthorized()
	}
	raw, e := b64.DecodeString(parts[0])
	if e != nil {
		return c, unauthorized()
	}
	var h struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
		KeyID     string `json:"kid"`
	}
	if strictJSON(raw, &h) != nil || h.Algorithm != "HS256" || h.Type != "JWT" {
		return c, unauthorized()
	}
	key, e := t.key(ctx, h.KeyID)
	if e != nil {
		return c, e
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, e := b64.DecodeString(parts[2])
	if e != nil || subtle.ConstantTimeCompare(mac.Sum(nil), sig) != 1 {
		return c, unauthorized()
	}
	raw, e = b64.DecodeString(parts[1])
	if e != nil || strictJSON(raw, &c) != nil {
		return c, unauthorized()
	}
	now := t.now().Unix()
	k := t.keys[h.KeyID]
	accessRaw, accessErr := hex.DecodeString(c.AccessID)
	if accessErr != nil || len(accessRaw) != 32 || strings.ToLower(c.AccessID) != c.AccessID {
		return IdentityClaims{}, unauthorized()
	}
	if c.Issuer != t.issuer || c.Audience != t.audience || !validUUID(c.AccountID) || !validUUID(c.SessionID) || c.Epoch < 0 || c.IssuedAt <= 0 || c.IssuedAt > now+30 || c.NotBefore > now+30 || c.NotBefore < c.IssuedAt || c.ExpiresAt <= now || c.ExpiresAt <= c.IssuedAt || c.ExpiresAt-c.IssuedAt > 900 || c.IssuedAt < k.ActiveAt.Unix() || c.ExpiresAt > k.RetireAt.Unix() {
		return IdentityClaims{}, unauthorized()
	}
	return c, nil
}
func strictJSON(raw []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return invalid()
	}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return e
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return invalid()
		}
		seen[key] = true
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return e
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid()
		}
	}
	if _, e = d.Token(); e != nil {
		return e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return invalid()
	}
	typ := reflect.TypeOf(v)
	if typ.Kind() != reflect.Pointer || typ.Elem().Kind() != reflect.Struct {
		return invalid()
	}
	typ = typ.Elem()
	allowed := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag != "" && tag != "-" {
			allowed[tag] = true
		}
	}
	for k := range seen {
		if !allowed[k] {
			return invalid()
		}
	}
	for k := range allowed {
		if !seen[k] {
			return invalid()
		}
	}
	d = json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// AuthenticateJWT pins cryptographic claims and then rechecks durable session/epoch authority.
func (s *Identity) AuthenticateJWT(ctx context.Context, tokens *IdentityTokens, token string) (Principal, error) {
	if tokens == nil {
		return Principal{}, unavailable()
	}
	c, e := tokens.Verify(ctx, token)
	if e != nil {
		return Principal{}, e
	}
	var p Principal
	e = s.pool.QueryRow(ctx, `SELECT a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE a.account_id=$1 AND s.session_id=$2 AND s.auth_epoch=$3 AND a.auth_epoch=$3 AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND s.access_hash=$4`, c.AccountID, c.SessionID, c.Epoch, digest(c.AccessID)).Scan(&p.accountID, &p.sessionID, &p.wallet, &p.epoch, &p.reauthenticatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return Principal{}, unauthorized()
	}
	if e != nil {
		return Principal{}, unavailable()
	}
	p.accessID = c.AccessID
	return p, nil
}

type EnvironmentIdentitySecrets struct{}

func (EnvironmentIdentitySecrets) ResolveIdentityKey(ctx context.Context, reference string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(reference) == 0 || len(reference) > 128 {
		return nil, unavailable()
	}
	for index, c := range reference {
		if !((c >= 'A' && c <= 'Z') || c == '_' || (index > 0 && c >= '0' && c <= '9')) {
			return nil, unavailable()
		}
	}
	secret, ok := os.LookupEnv(reference)
	if !ok || len(secret) < 32 {
		return nil, unavailable()
	}
	return []byte(secret), nil
}

func (t *IdentityTokens) SetObserver(observe func(string)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.observe = observe
}
func (t *IdentityTokens) observeCategory(category string) {
	t.mu.Lock()
	observe := t.observe
	t.mu.Unlock()
	if observe != nil {
		observe(category)
	}
}

type IdentityKeyMetadata struct {
	KeyID     string `json:"kid"`
	Algorithm string `json:"alg"`
	ActiveAt  string `json:"activeAt"`
	RetireAt  string `json:"retireAt"`
	Signing   bool   `json:"signing"`
}

func (t *IdentityTokens) KeyMetadata() []IdentityKeyMetadata {
	items := make([]IdentityKeyMetadata, 0, len(t.keys))
	for _, key := range t.keys {
		items = append(items, IdentityKeyMetadata{key.ID, "HS256", wireTime(key.ActiveAt), wireTime(key.RetireAt), key.ID == t.active})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].KeyID < items[j].KeyID })
	return items
}
