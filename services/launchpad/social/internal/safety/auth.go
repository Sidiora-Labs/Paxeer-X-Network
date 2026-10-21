// Package safety owns moderation authority. Principals must be supplied by
// verified D11 middleware, never decoded from request actor headers or bodies.
package safety

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"regexp"
	"strings"
	"time"
)

var (
	ErrForbidden   = errors.New("moderation forbidden")
	ErrUnavailable = errors.New("moderation unavailable")
	ErrConflict    = errors.New("stale revision or invalid transition")
	ErrInvalid     = errors.New("invalid moderation input")
)
var walletRE = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

type Principal struct {
	Wallet    string    `json:"wallet"`
	SessionID string    `json:"sessionId"`
	Role      string    `json:"role"`
	Pools     []string  `json:"pools"`
	Cases     []string  `json:"cases"`
	Expires   time.Time `json:"expires"`
	StepUp    time.Time `json:"stepUp"`
}
type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	p.Pools = append([]string(nil), p.Pools...)
	p.Cases = append([]string(nil), p.Cases...)
	return context.WithValue(ctx, principalKey{}, p)
}
func Actor(ctx context.Context, now time.Time) (Principal, error) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	if !ok || !walletRE.MatchString(p.Wallet) || p.SessionID == "" || !now.Before(p.Expires) {
		return Principal{}, ErrForbidden
	}
	return p, nil
}

// VerifySession verifies a private-fixture compatible detached Ed25519 session.
// D11 integration must also enforce audience, revocation and browser CSRF; this
// verifier is not an alternate production login protocol.
func VerifySession(payload, signature string, key ed25519.PublicKey, now time.Time) (Principal, error) {
	if len(payload) > 8192 || len(signature) > 128 || len(key) != ed25519.PublicKeySize {
		return Principal{}, ErrForbidden
	}
	b, e := base64.RawURLEncoding.DecodeString(payload)
	if e != nil {
		return Principal{}, ErrForbidden
	}
	sig, e := base64.RawURLEncoding.DecodeString(signature)
	if e != nil || !ed25519.Verify(key, b, sig) {
		return Principal{}, ErrForbidden
	}
	var p Principal
	if json.Unmarshal(b, &p) != nil || len(p.Pools) > 100 || len(p.Cases) > 100 {
		return Principal{}, ErrForbidden
	}
	_, e = Actor(WithPrincipal(context.Background(), p), now)
	return p, e
}
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func Authorized(p Principal, pool string, mutate bool, now time.Time) bool {
	if !walletRE.MatchString(p.Wallet) || p.SessionID == "" || !now.Before(p.Expires) {
		return false
	}
	if p.Role == "global_administrator" {
		return !mutate || (!p.StepUp.After(now) && now.Sub(p.StepUp) <= 5*time.Minute)
	}
	if pool == "" {
		return !mutate && p.Role == "reviewer"
	}
	return contains(p.Pools, pool) && (p.Role == "room_moderator" || (!mutate && p.Role == "reviewer"))
}
func Bounded(s string, n int) bool { return strings.TrimSpace(s) != "" && len(s) <= n }

func (s *Store) ResolvePrincipal(ctx context.Context, wallet, account, session string, expires time.Time) (Principal, error) {
	if !s.available() || !walletRE.MatchString(wallet) || account == "" || session == "" || !expires.After(s.now()) {
		return Principal{}, ErrForbidden
	}
	p := Principal{Wallet: wallet, SessionID: session, Role: "participant", Expires: expires}
	var grantExpiry time.Time
	var stepUp *time.Time
	e := s.DB.QueryRow(ctx, `SELECT role,expires_at,step_up_at FROM safety_moderator_sessions WHERE session_id=$1 AND account_id=$2 AND wallet=$3 AND expires_at>$4`, session, account, wallet, s.now()).Scan(&p.Role, &grantExpiry, &stepUp)
	if e == pgx.ErrNoRows {
		return p, nil
	}
	if e != nil {
		return Principal{}, ErrUnavailable
	}
	if grantExpiry.Before(p.Expires) {
		p.Expires = grantExpiry
	}
	if stepUp != nil {
		p.StepUp = *stepUp
	}
	rows, e := s.DB.Query(ctx, `SELECT pool_id,case_id::text FROM safety_moderator_scopes WHERE session_id=$1 ORDER BY pool_id,case_id LIMIT 200`, session)
	if e != nil {
		return Principal{}, ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var pool string
		var caseID *string
		if e = rows.Scan(&pool, &caseID); e != nil {
			return Principal{}, ErrUnavailable
		}
		if pool != "" {
			p.Pools = append(p.Pools, pool)
		}
		if caseID != nil {
			p.Cases = append(p.Cases, *caseID)
		}
	}
	if rows.Err() != nil {
		return Principal{}, ErrUnavailable
	}
	return p, nil
}
