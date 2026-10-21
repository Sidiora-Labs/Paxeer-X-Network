package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

type localIdentitySecrets struct{ fail bool }

func (s *localIdentitySecrets) ResolveIdentityKey(_ context.Context, ref string) ([]byte, error) {
	if s.fail {
		return nil, unavailable()
	}
	return []byte(strings.Repeat(ref, 32)), nil
}
func TestIdentityJWTClaimsAndRotation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	clock := func() time.Time { return now }
	ctx := context.Background()
	provider := &localIdentitySecrets{}
	keys := []SigningKey{{"A", "a", now.Add(-time.Hour), now.Add(15 * time.Minute)}, {"B", "b", now, now.Add(time.Hour)}}
	tokens, e := NewIdentityTokens("kindle", "gateway", "B", keys, provider, clock)
	if e != nil {
		t.Fatal(e)
	}
	p := Principal{accountID: uuid.NewString(), sessionID: uuid.NewString()}
	b, e := tokens.Mint(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	c, e := tokens.Verify(ctx, b)
	if e != nil || c.AccountID != p.accountID {
		t.Fatal(e)
	}
	a, e := tokens.signClaims(ctx, "A", c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tokens.Verify(ctx, a); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*IdentityClaims){func(c *IdentityClaims) { c.Audience = "evil" }, func(c *IdentityClaims) { c.ExpiresAt = now.Unix() - 1 }, func(c *IdentityClaims) { c.NotBefore = now.Unix() + 31 }, func(c *IdentityClaims) { c.AccountID = "" }, func(c *IdentityClaims) { c.IssuedAt = 1; c.ExpiresAt = 9223372036854775807 }} {
		bad := c
		mutate(&bad)
		v, e := tokens.signClaims(ctx, "B", bad)
		if e != nil {
			t.Fatal(e)
		}
		_, e = tokens.Verify(ctx, v)
		requireCode(t, e, "AUTH_EXPIRED")
	}
	for _, nullEpoch := range []bool{false, true} {
		raw, e := b64.DecodeString(strings.Split(b, ".")[1])
		if e != nil {
			t.Fatal(e)
		}
		var fields map[string]any
		if e = json.Unmarshal(raw, &fields); e != nil {
			t.Fatal(e)
		}
		if nullEpoch {
			fields["ae"] = nil
		} else {
			delete(fields, "ae")
		}
		payload, e := json.Marshal(fields)
		if e != nil {
			t.Fatal(e)
		}
		input := strings.Split(b, ".")[0] + "." + b64.EncodeToString(payload)
		key, e := provider.ResolveIdentityKey(ctx, "b")
		if e != nil {
			t.Fatal(e)
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(input))
		_, e = tokens.Verify(ctx, input+"."+b64.EncodeToString(mac.Sum(nil)))
		requireCode(t, e, "AUTH_EXPIRED")
	}
	parts := strings.Split(b, ".")
	parts[0] = b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT","kid":"unknown"}`))
	_, e = tokens.Verify(ctx, strings.Join(parts, "."))
	requireCode(t, e, "AUTH_EXPIRED")
	parts[0] = b64.EncodeToString([]byte(`{"alg":"none","typ":"JWT","kid":"B"}`))
	_, e = tokens.Verify(ctx, strings.Join(parts, "."))
	requireCode(t, e, "AUTH_EXPIRED")
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":1} ?`, `{"a":1}{"a":2}`, `{"a":1,"A":2}`} {
		var v struct {
			A int `json:"a"`
		}
		if strictJSON([]byte(raw), &v) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	now = now.Add(15 * time.Minute)
	_, e = tokens.Verify(ctx, a)
	requireCode(t, e, "AUTH_EXPIRED")
	provider.fail = true
	_, e = tokens.Mint(ctx, p)
	requireCode(t, e, "DEPENDENCY_UNAVAILABLE")
	keys[0].RetireAt = now.Add(time.Hour)
	_, e = NewIdentityTokens("kindle", "gateway", "B", keys, provider, clock)
	requireCode(t, e, "FIELD_INVALID")
}

func TestIdentityManagedKeysAdversarialClaimsRotationAndLoss(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	clock := func() time.Time { return now }
	ctx := context.Background()
	t.Setenv("KINDLE_ADVERSARIAL_KEY_A", strings.Repeat("a", 48))
	t.Setenv("KINDLE_ADVERSARIAL_KEY_B", strings.Repeat("b", 48))
	keys := []SigningKey{{"A", "KINDLE_ADVERSARIAL_KEY_A", now.Add(-time.Hour), now.Add(15 * time.Minute)}, {"B", "KINDLE_ADVERSARIAL_KEY_B", now, now.Add(time.Hour)}}
	tokens, err := NewIdentityTokens("https://kindle.test", "owner", "B", keys, EnvironmentIdentitySecrets{}, clock)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{accountID: uuid.NewString(), sessionID: uuid.NewString(), epoch: 3}
	tokenB, err := tokens.Mint(ctx, principal)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.Verify(ctx, tokenB)
	if err != nil {
		t.Fatal(err)
	}
	tokenA, err := tokens.signClaims(ctx, "A", claims)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{tokenA, tokenB} {
		if _, err = tokens.Verify(ctx, token); err != nil {
			t.Fatal("rotation overlap rejected live token", err)
		}
	}
	for _, mutation := range []func(*IdentityClaims){
		func(c *IdentityClaims) { c.Audience = "foreign" },
		func(c *IdentityClaims) { c.Issuer = "https://foreign.test" },
		func(c *IdentityClaims) { c.ExpiresAt = now.Unix() - 1 },
		func(c *IdentityClaims) { c.NotBefore = now.Unix() + 31 },
		func(c *IdentityClaims) { c.SessionID = "" },
		func(c *IdentityClaims) { c.AccountID = "" },
		func(c *IdentityClaims) { c.Epoch = -1 },
	} {
		bad := claims
		mutation(&bad)
		token, err := tokens.signClaims(ctx, "B", bad)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tokens.Verify(ctx, token)
		requireCode(t, err, "AUTH_EXPIRED")
	}
	withinTolerance := claims
	withinTolerance.NotBefore = now.Unix() + 30
	tolerated, err := tokens.signClaims(ctx, "B", withinTolerance)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tokens.Verify(ctx, tolerated); err != nil {
		t.Fatal("declared nbf tolerance rejected", err)
	}
	parts := strings.Split(tokenB, ".")
	parts[0] = b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT","kid":"unknown"}`))
	_, err = tokens.Verify(ctx, strings.Join(parts, "."))
	requireCode(t, err, "AUTH_EXPIRED")
	t.Setenv("KINDLE_ADVERSARIAL_KEY_B", "")
	if _, err = tokens.Verify(ctx, tokenB); err != nil {
		t.Fatal("bounded previously verified key cache lost during dependency failure", err)
	}
	now = now.Add(time.Minute + time.Second)
	_, err = tokens.Verify(ctx, tokenB)
	requireCode(t, err, "DEPENDENCY_UNAVAILABLE")
	t.Setenv("KINDLE_ADVERSARIAL_KEY_B", strings.Repeat("b", 48))
	if _, err = tokens.Verify(ctx, tokenB); err != nil {
		t.Fatal("restored key provider failed", err)
	}
	now = time.Unix(claims.ExpiresAt, 0)
	_, err = tokens.Verify(ctx, tokenA)
	requireCode(t, err, "AUTH_EXPIRED")
	if _, err = tokens.Mint(ctx, principal); err != nil {
		t.Fatal("active B key failed after A retirement", err)
	}
	for _, key := range tokens.KeyMetadata() {
		if strings.Contains(key.KeyID, "KINDLE_") || key.Algorithm != "HS256" {
			t.Fatal("secret reference leaked in published key metadata")
		}
	}
}
