package l18

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

const AttributionWindow = 7 * 24 * time.Hour

type Click struct {
	Campaign   string `json:"campaign"`
	Version    string `json:"version"`
	Referrer   string `json:"referrer"`
	IssuedAt   int64  `json:"issuedAt"`
	Nonce      string `json:"nonce"`
	KeyVersion string `json:"keyVersion,omitempty"`
}
type Binding struct {
	Campaign      string `json:"campaign"`
	Beneficiary   string `json:"beneficiary"`
	PolicyVersion string `json:"policyVersion"`
	Referrer      string `json:"referrer"`
	Attribution   string `json:"attribution"`
	IssuedAt      int64  `json:"issuedAt"`
	ConsentAt     int64  `json:"consentAt"`
	Nonce         string `json:"nonce"`
}
type Attribution struct {
	ledger            *Ledger
	key               []byte
	now               func() time.Time
	campaign, version string
	keys              map[string][]byte
	activeKey         string
}

func NewAttribution(l *Ledger, key []byte, now func() time.Time, campaign, version string) (*Attribution, error) {
	if l == nil || len(key) < 32 || now == nil || !identifier.MatchString(campaign) || !identifier.MatchString(version) {
		return nil, ErrInvalid
	}
	return &Attribution{ledger: l, key: append([]byte(nil), key...), now: now, campaign: campaign, version: version}, nil
}

func NewRotatingAttribution(l *Ledger, keys map[string][]byte, active string, now func() time.Time, campaign, version string) (*Attribution, error) {
	if !identifier.MatchString(active) {
		return nil, ErrInvalid
	}
	a, e := NewAttribution(l, keys[active], now, campaign, version)
	if e != nil {
		return nil, e
	}
	a.keys = map[string][]byte{}
	a.activeKey = active
	for version, key := range keys {
		if !identifier.MatchString(version) || len(key) < 32 {
			return nil, ErrInvalid
		}
		a.keys[version] = append([]byte(nil), key...)
	}
	return a, nil
}

// IssueClick is called only after a known short code resolves to this campaign
// and its referrer through the service's database; it stores no IP address.
func (a *Attribution) IssueClick(referrer, nonce string) (string, error) {
	w, e := wallet(referrer)
	if e != nil || !identifier.MatchString(nonce) {
		return "", ErrInvalid
	}
	c := Click{Campaign: a.campaign, Version: a.version, Referrer: w, IssuedAt: a.now().Unix(), Nonce: nonce, KeyVersion: a.activeKey}
	b, _ := json.Marshal(c)
	key := a.key
	if a.activeKey != "" {
		key = a.keys[a.activeKey]
	}
	m := hmac.New(sha256.New, key)
	m.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}
func (a *Attribution) decode(token string) (Click, error) {
	if len(token) > 1024 {
		return Click{}, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return Click{}, ErrInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return Click{}, ErrInvalid
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return Click{}, ErrInvalid
	}
	var c Click
	if json.Unmarshal(b, &c) != nil {
		return Click{}, ErrInvalid
	}
	key := a.key
	if a.keys != nil {
		var ok bool
		key, ok = a.keys[c.KeyVersion]
		if !ok {
			return Click{}, ErrUnauthorized
		}
	} else if c.KeyVersion != "" {
		return Click{}, ErrUnauthorized
	}
	m := hmac.New(sha256.New, key)
	m.Write(b)
	if !hmac.Equal(sig, m.Sum(nil)) {
		return Click{}, ErrUnauthorized
	}
	if json.Unmarshal(b, &c) != nil || c.Campaign != a.campaign || c.Version != a.version || !identifier.MatchString(c.Nonce) {
		return Click{}, ErrInvalid
	}
	w, e := wallet(c.Referrer)
	if e != nil || w != c.Referrer {
		return Click{}, ErrInvalid
	}
	return c, nil
}

func (a *Attribution) Binding(ctx context.Context, actor string) (Binding, error) {
	who, e := wallet(actor)
	if e != nil {
		return Binding{}, e
	}
	tx, e := a.ledger.begin(ctx)
	if e != nil {
		return Binding{}, e
	}
	defer tx.Rollback(ctx)
	var out Binding
	e = tx.QueryRow(ctx, `SELECT campaign,beneficiary,policy_version,referrer,attribution,issued_at,consent_at,token_nonce FROM l18_bindings WHERE campaign=$1 AND beneficiary=$2`, a.campaign, who).Scan(&out.Campaign, &out.Beneficiary, &out.PolicyVersion, &out.Referrer, &out.Attribution, &out.IssuedAt, &out.ConsentAt, &out.Nonce)
	if e != nil {
		return Binding{}, e
	}
	return out, tx.Commit(ctx)
}

// Bind's actor comes from verified account authentication. No client wallet or
// referrer field may stand in for it. First binding is immutable per campaign.
func (a *Attribution) Bind(ctx context.Context, request, actor string, consent bool, tokens []string) (Binding, error) {
	if !consent {
		return Binding{}, ErrUnauthorized
	}
	who, e := wallet(actor)
	if e != nil || !identifier.MatchString(request) || len(tokens) > 16 {
		return Binding{}, ErrInvalid
	}
	now := a.now().Unix()
	clicks := []Click{}
	for _, token := range tokens {
		c, e := a.decode(token)
		if e != nil {
			return Binding{}, e
		}
		if c.IssuedAt > now {
			return Binding{}, ErrInvalid
		}
		if c.Referrer == who {
			return Binding{}, ErrUnauthorized
		}
		if now-c.IssuedAt <= int64(AttributionWindow/time.Second) {
			clicks = append(clicks, c)
		}
	}
	sort.Slice(clicks, func(i, j int) bool {
		if clicks[i].IssuedAt == clicks[j].IssuedAt {
			return clicks[i].Nonce < clicks[j].Nonce
		}
		return clicks[i].IssuedAt < clicks[j].IssuedAt
	})
	out := Binding{Campaign: a.campaign, Beneficiary: who, PolicyVersion: a.version, Attribution: "organic", ConsentAt: now}
	if len(clicks) > 0 {
		c := clicks[0]
		out.Referrer = c.Referrer
		out.IssuedAt = c.IssuedAt
		out.Nonce = c.Nonce
		out.Attribution = "referred"
	}
	tx, e := a.ledger.begin(ctx)
	if e != nil {
		return Binding{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "referral-account:"+a.campaign+":"+who); e != nil {
		return Binding{}, e
	}
	// Bind request payload excludes execution clock so exact retries remain valid.
	payload := struct {
		Actor   string
		Consent bool
		Tokens  []string
	}{who, consent, tokens}
	dup, e := inbox(ctx, tx, "bind:"+request, payload)
	if e != nil {
		return Binding{}, e
	}
	if !dup {
		tag, e := tx.Exec(ctx, `INSERT INTO l18_bindings(campaign,beneficiary,policy_version,referrer,attribution,issued_at,consent_at,token_nonce) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, out.Campaign, out.Beneficiary, out.PolicyVersion, out.Referrer, out.Attribution, out.IssuedAt, out.ConsentAt, out.Nonce)
		if e != nil {
			return Binding{}, e
		}
		if tag.RowsAffected() == 1 {
			if e = a.recordConsent(ctx, tx, who, "bind:"+request, true); e != nil {
				return Binding{}, e
			}
			if e = emit(ctx, tx, "bind:"+request, "referral.bound", out); e != nil {
				return Binding{}, e
			}
		}
	}
	e = tx.QueryRow(ctx, `SELECT campaign,beneficiary,policy_version,referrer,attribution,issued_at,consent_at,token_nonce FROM l18_bindings WHERE campaign=$1 AND beneficiary=$2`, a.campaign, who).Scan(&out.Campaign, &out.Beneficiary, &out.PolicyVersion, &out.Referrer, &out.Attribution, &out.IssuedAt, &out.ConsentAt, &out.Nonce)
	if e != nil {
		return Binding{}, e
	}
	return out, tx.Commit(ctx)
}

// ReferralAwardIdentity is campaign+policy+referred-wallet unique, so replaying
// fills or generating additional clicks cannot mint multiple conversions.
func ReferralAwardIdentity(campaign, version, referred string) (string, error) {
	w, e := wallet(referred)
	if e != nil || !identifier.MatchString(campaign) || !identifier.MatchString(version) {
		return "", ErrInvalid
	}
	return "referral:" + fingerprint([]string{campaign, version, w}), nil
}

func (a *Attribution) Matches(l *Ledger, campaign, version string) bool {
	return a != nil && a.ledger == l && a.campaign == campaign && a.version == version
}
