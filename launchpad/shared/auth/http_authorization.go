package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AssertionTransport struct {
	AssertionHeader string
	SignatureHeader string
}

type ServiceAssertionVerifier struct {
	Transport                        AssertionTransport
	Issuer, Audience, PeerCertSHA256 string
	Key                              ed25519.PublicKey
	Observe                          func(string)
}

func decodeAuthorizationJSON(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > 8192 || rejectDuplicateKeys(raw) != nil {
		return ErrScopeDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return ErrScopeDenied
	}
	return nil
}

func authorizationHeader(r *http.Request, name string) (string, error) {
	if r == nil || name == "" || strings.ContainsAny(name, " \r\n\t:") {
		return "", ErrScopeDenied
	}
	values := r.Header.Values(name)
	if len(values) != 1 || values[0] == "" || len(values[0]) > 12000 {
		return "", ErrScopeDenied
	}
	return values[0], nil
}

func (v ServiceAssertionVerifier) Authenticate(r *http.Request, body []byte) (p Principal, err error) {
	category := "assertion_invalid"
	defer func() {
		if v.Observe != nil {
			v.Observe(category)
		}
	}()
	if r == nil || r.URL == nil || r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || len(body) > 1<<20 || strings.EqualFold(v.Transport.AssertionHeader, v.Transport.SignatureHeader) {
		return p, ErrScopeDenied
	}
	encoded, err := authorizationHeader(r, v.Transport.AssertionHeader)
	if err != nil {
		return p, err
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return p, ErrScopeDenied
	}
	var assertion ServiceAssertion
	if decodeAuthorizationJSON(raw, &assertion) != nil {
		return p, ErrScopeDenied
	}
	encodedSignature, err := authorizationHeader(r, v.Transport.SignatureHeader)
	if err != nil {
		return p, err
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(encodedSignature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return p, ErrScopeDenied
	}
	if assertion.Issuer != v.Issuer {
		category = "assertion_issuer"
		return p, ErrScopeDenied
	}
	if assertion.Audience != v.Audience {
		category = "assertion_audience"
		return p, ErrScopeDenied
	}
	if assertion.ExpiresAt <= time.Now().Unix() {
		category = "assertion_expired"
		return p, ErrScopeDenied
	}
	p, err = VerifyServiceAssertion(assertion, signature, v.Key, r.TLS, v.PeerCertSHA256, v.Issuer, v.Audience, RequestDigest(r.Method, r.URL.RequestURI(), body), time.Now())
	if err != nil {
		return Principal{}, &VerificationError{Category: category}
	}
	if !validIdentityID(p.SessionID) || !validIdentityID(p.TraceID) || p.TraceID != r.Header.Get("X-Request-ID") || len(r.Header.Values("X-Request-ID")) != 1 {
		return Principal{}, ErrScopeDenied
	}
	wallet, walletErr := NormalizeWallet(p.Wallet)
	if walletErr != nil || wallet != p.Wallet {
		return Principal{}, ErrScopeDenied
	}
	seen := map[string]bool{}
	for _, role := range p.Roles {
		if !tokenPattern.MatchString(role) || seen[role] {
			return Principal{}, ErrScopeDenied
		}
		seen[role] = true
	}
	category = "assertion_accepted"
	return p, nil
}

type OperationTransport struct {
	EnvelopeHeader  string
	SignatureHeader string
}

type OperationVerifier interface {
	Verify(context.Context, string, string, string) error
}

type ActionRoute struct {
	Action, Method, Template string
}

type OperationAuthorizer struct {
	Ledger    Ledger
	Verifier  OperationVerifier
	Policies  PolicyRegistry
	Transport OperationTransport
	Routes    map[string]ActionRoute
	Observe   func(string)
}

func operationRouteMatches(template, requestPath string) bool {
	if template == "" || strings.ContainsAny(requestPath, "?#%") {
		return false
	}
	a, b := strings.Split(template, "/"), strings.Split(requestPath, "/")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.HasPrefix(a[i], "{") {
			end := strings.IndexByte(a[i], '}')
			if end <= 1 || b[i] == "" || !strings.HasSuffix(b[i], a[i][end+1:]) || len(b[i]) <= len(a[i][end+1:]) {
				return false
			}
		} else if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (a OperationAuthorizer) AuthorizeRequest(ctx context.Context, r *http.Request, identity IdentityPrincipal, action, target string, body []byte) (req OperationRequest, err error) {
	category := "operation_invalid"
	defer func() {
		if a.Observe != nil {
			a.Observe(category)
		}
	}()
	if err = ctx.Err(); err != nil {
		return req, err
	}
	if r == nil || r.URL == nil || a.Verifier == nil || a.Ledger.Environment == "" || strings.EqualFold(a.Transport.EnvelopeHeader, a.Transport.SignatureHeader) {
		return req, ErrScopeDenied
	}
	if !validIdentityID(identity.AccountID) || !validIdentityID(identity.SessionID) || identity.AuthEpoch < 0 || identity.ProofAt.IsZero() || identity.ProofAt.After(time.Now().Add(30*time.Second)) || !identity.AccessExpiresAt.After(time.Now()) {
		return req, ErrScopeDenied
	}
	wallet, err := NormalizeWallet(identity.Wallet)
	if err != nil || wallet != identity.Wallet {
		return req, ErrScopeDenied
	}
	route, exists := a.Routes[action]
	policy, policyExists := a.Policies.Actions[action]
	if !exists || !policyExists || route.Action != action || route.Method != r.Method || route.Template != policy.RouteTemplate || !operationRouteMatches(route.Template, r.URL.Path) || r.URL.RawQuery != "" || r.URL.RawPath != "" {
		category = "action_mismatch"
		return req, ErrScopeDenied
	}
	p := Principal{Wallet: wallet, SessionID: identity.SessionID, Roles: []string{"owner"}}
	if err = a.Policies.Authorize(p, action, target); err != nil {
		category = "scope_denied"
		return req, err
	}
	encoded, err := authorizationHeader(r, a.Transport.EnvelopeHeader)
	if err != nil {
		return req, err
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || decodeAuthorizationJSON(raw, &req.Envelope) != nil {
		return req, ErrScopeDenied
	}
	req.Signature, err = authorizationHeader(r, a.Transport.SignatureHeader)
	if err != nil {
		return req, err
	}
	digest, digestErr := BodyDigest(body)
	if req.Envelope.Version == 2 {
		digest, digestErr = BodyDigestV2(body)
	}
	if digestErr != nil || req.Envelope.BodyHash != digest {
		category = "body_mismatch"
		return req, ErrScopeDenied
	}
	if req.Envelope.Action != action {
		category = "action_mismatch"
		return req, ErrScopeDenied
	}
	if err = req.Envelope.Validate(time.Now(), a.Ledger.Environment, wallet, action, r.Method, r.URL.Path, body); err != nil {
		return req, ErrScopeDenied
	}
	id := r.Header.Get("X-Request-ID")
	if !validIdentityID(id) {
		return req, ErrScopeDenied
	}
	req.Body = append([]byte(nil), body...)
	req.Actor, req.Action, req.Method, req.Path, req.RequestID, req.PolicyVersion = wallet, action, r.Method, r.URL.Path, id, a.Policies.Version
	category = "operation_authorized"
	return req, nil
}

func (a OperationAuthorizer) ExecuteTx(ctx context.Context, tx pgx.Tx, req OperationRequest, mutate Mutation) ([]byte, bool, error) {
	if a.Verifier == nil || a.Policies.Version == "" || req.PolicyVersion != a.Policies.Version {
		return nil, false, ErrScopeDenied
	}
	result, duplicate, err := a.Ledger.ExecuteValidatedTx(ctx, tx, req, a.Verifier.Verify, mutate)
	if a.Observe != nil {
		if errors.Is(err, ErrReplayMismatch) {
			a.Observe("replay_mismatch")
		} else if err == nil && duplicate {
			a.Observe("duplicate_accepted")
		} else if err != nil {
			var failure *VerificationError
			if errors.As(err, &failure) && failure.Category == "replay_integrity" {
				a.Observe("replay_integrity")
			} else if errors.As(err, &failure) && failure.Retryable {
				a.Observe("operation_dependency_unavailable")
			} else {
				a.Observe("operation_rejected")
			}
		}
	}
	return result, duplicate, err
}

type PostgresIdentityResolver struct {
	Pool          *pgxpool.Pool
	GuardFunction string
}

func (v PostgresIdentityResolver) Resolve(ctx context.Context, p Principal) (IdentityPrincipal, error) {
	var result IdentityPrincipal
	if v.GuardFunction != "" {
		if v.Pool == nil {
			return result, &VerificationError{Category: "upstream", Retryable: true}
		}
		tx, err := v.Pool.Begin(ctx)
		if err != nil {
			return result, &VerificationError{Category: "upstream", Retryable: true}
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		result, err = v.ResolveTx(ctx, tx, p)
		if err != nil {
			return IdentityPrincipal{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return IdentityPrincipal{}, &VerificationError{Category: "upstream", Retryable: true}
		}
		return result, nil
	}
	if v.Pool == nil || !validIdentityID(p.SessionID) {
		return result, &VerificationError{Category: "upstream", Retryable: true}
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet {
		return result, ErrScopeDenied
	}
	err = v.Pool.QueryRow(ctx, `SELECT a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at,s.access_expires_at FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE s.session_id=$1 AND s.wallet=$2 AND s.auth_epoch=a.auth_epoch AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND w.chain_id=125`, p.SessionID, wallet).Scan(&result.AccountID, &result.SessionID, &result.Wallet, &result.AuthEpoch, &result.ProofAt, &result.AccessExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	if err != nil {
		return IdentityPrincipal{}, &VerificationError{Category: "upstream", Retryable: true}
	}
	if result.Validate(time.Now()) != nil {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	return result, nil
}

func (v PostgresIdentityResolver) ResolveTx(ctx context.Context, tx pgx.Tx, p Principal) (IdentityPrincipal, error) {
	var result IdentityPrincipal
	if tx == nil || !validIdentityID(p.SessionID) {
		return result, ErrScopeDenied
	}
	wallet, err := NormalizeWallet(p.Wallet)
	if err != nil || wallet != p.Wallet {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	if v.GuardFunction != "" {
		if v.GuardFunction != "users.identity_guard_v2" {
			return IdentityPrincipal{}, ErrScopeDenied
		}
		err = tx.QueryRow(ctx, `SELECT account_id::text,session_id::text,wallet,auth_epoch,proof_at,access_expires_at FROM "users"."identity_guard_v2"($1::uuid,$2::text)`, p.SessionID, wallet).Scan(&result.AccountID, &result.SessionID, &result.Wallet, &result.AuthEpoch, &result.ProofAt, &result.AccessExpiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return IdentityPrincipal{}, ErrScopeDenied
		}
		if err != nil {
			return IdentityPrincipal{}, &VerificationError{Category: "upstream", Retryable: true}
		}
		if result.SessionID != p.SessionID || result.Wallet != wallet || result.Validate(time.Now()) != nil {
			return IdentityPrincipal{}, ErrScopeDenied
		}
		return result, nil
	}
	err = tx.QueryRow(ctx, `SELECT a.account_id::text,s.session_id::text,s.wallet,s.auth_epoch,s.reauthenticated_at,s.access_expires_at FROM identity_sessions s JOIN identity_accounts a USING(account_id) JOIN identity_wallet_links w ON w.account_id=a.account_id AND w.wallet=s.wallet AND w.state='verified' WHERE s.session_id=$1 AND s.wallet=$2 AND s.auth_epoch=a.auth_epoch AND s.revoked_at IS NULL AND s.access_expires_at>clock_timestamp() AND w.chain_id=125 FOR SHARE OF a,s,w`, p.SessionID, wallet).Scan(&result.AccountID, &result.SessionID, &result.Wallet, &result.AuthEpoch, &result.ProofAt, &result.AccessExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	if err != nil {
		return IdentityPrincipal{}, &VerificationError{Category: "upstream", Retryable: true}
	}
	if result.Validate(time.Now()) != nil {
		return IdentityPrincipal{}, ErrScopeDenied
	}
	return result, nil
}

type OperationOwnerResolver func(context.Context, IdentityPrincipal, *http.Request) (string, error)

func (a OperationAuthorizer) Handler(action string, assertions ServiceAssertionVerifier, identities PostgresIdentityResolver, owner OperationOwnerResolver, mutation Mutation) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		deny := func(err error) {
			status, code, retryable := PublicCode(err)
			id := r.Header.Get("X-Request-ID")
			if !validIdentityID(id) {
				id = uuid.NewString()
			}
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "Authorization request could not be completed", "requestId": id, "retryable": retryable, "retryAfterMs": nil, "fieldErrors": []any{}, "details": nil})
		}
		if r.Body == nil || owner == nil || mutation == nil || a.Ledger.Pool == nil || identities.Pool != a.Ledger.Pool {
			deny(&VerificationError{Category: "upstream", Retryable: true})
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || len(body) > 1<<20 {
			deny(ErrScopeDenied)
			return
		}
		principal, err := assertions.Authenticate(r, body)
		if err != nil {
			deny(&VerificationError{Category: "assertion"})
			return
		}
		identity, err := identities.Resolve(r.Context(), principal)
		if err != nil {
			deny(err)
			return
		}
		target, err := owner(r.Context(), identity, r)
		if err != nil {
			deny(err)
			return
		}
		if err = a.Policies.Authorize(principal, action, target); err != nil {
			id := r.Header.Get("X-Request-ID")
			if validIdentityID(id) {
				if auditErr := a.Ledger.RecordDenial(r.Context(), AuditEvent{RequestID: id, Actor: identity.Wallet, Action: action, Result: "denied", PolicyVersion: a.Policies.Version, RequestDigest: RequestDigest(r.Method, r.URL.RequestURI(), body)}); auditErr != nil {
					deny(&VerificationError{Category: "audit", Retryable: true})
					return
				}
			}
			deny(err)
			return
		}
		req, err := a.AuthorizeRequest(r.Context(), r, identity, action, target, body)
		if err != nil {
			id := r.Header.Get("X-Request-ID")
			if validIdentityID(id) {
				if auditErr := a.Ledger.RecordDenial(r.Context(), AuditEvent{RequestID: id, Actor: identity.Wallet, Action: action, Result: "denied", PolicyVersion: a.Policies.Version, RequestDigest: RequestDigest(r.Method, r.URL.RequestURI(), body)}); auditErr != nil {
					deny(&VerificationError{Category: "audit", Retryable: true})
					return
				}
			}
			deny(err)
			return
		}
		tx, err := a.Ledger.Pool.Begin(r.Context())
		if err != nil {
			deny(&VerificationError{Category: "upstream", Retryable: true})
			return
		}
		defer tx.Rollback(context.WithoutCancel(r.Context()))
		current, err := identities.ResolveTx(r.Context(), tx, principal)
		if err != nil || current.AccountID != identity.AccountID || current.AuthEpoch != identity.AuthEpoch || current.Wallet != identity.Wallet || current.SessionID != identity.SessionID {
			if err == nil {
				err = ErrScopeDenied
			}
			deny(err)
			return
		}
		response, duplicate, err := a.ExecuteTx(r.Context(), tx, req, func(ctx context.Context, tx pgx.Tx, validated []byte) ([]byte, error) {
			result, err := mutation(ctx, tx, validated)
			if err == nil && !json.Valid(result) {
				return nil, fmt.Errorf("invalid mutation response")
			}
			return result, err
		})
		if err != nil {
			_ = tx.Rollback(context.WithoutCancel(r.Context()))
			var failure *VerificationError
			if errors.Is(err, ErrScopeDenied) || (errors.As(err, &failure) && !failure.Retryable) {
				if auditErr := a.Ledger.RecordDenial(r.Context(), AuditEvent{RequestID: req.RequestID, Actor: req.Actor, Action: req.Action, Result: "denied", PolicyVersion: req.PolicyVersion, RequestDigest: RequestDigest(req.Method, req.Path, req.Body)}); auditErr != nil {
					deny(&VerificationError{Category: "audit", Retryable: true})
					return
				}
			}
			deny(err)
			return
		}
		if duplicate {
			if err = tx.QueryRow(r.Context(), `SELECT request_id::text FROM operation_ledger WHERE actor=$1 AND action=$2 AND nonce=$3`, req.Actor, req.Action, req.Envelope.Nonce).Scan(&req.RequestID); err != nil {
				deny(&VerificationError{Category: "upstream", Retryable: true})
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			deny(&VerificationError{Category: "upstream", Retryable: true})
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			RequestID   string          `json:"requestId"`
			Data        json.RawMessage `json:"data"`
			Watermark   any             `json:"watermark"`
			Revision    any             `json:"revision"`
			Quality     string          `json:"quality"`
			NextCursor  any             `json:"nextCursor"`
			GeneratedAt string          `json:"generatedAt"`
		}{req.RequestID, response, nil, nil, "complete", nil, time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	})
}
