package policy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WorkloadVerifier struct {
	Pool                                    *pgxpool.Pool
	Schema                                  string
	Issuer, Audience, PeerCertificateSHA256 string
	PublicKey                               ed25519.PublicKey
}

func (v WorkloadVerifier) Verify(ctx context.Context, r *http.Request) (Identity, error) {
	var zero Identity
	if r == nil || r.URL == nil || r.TLS == nil || r.TLS.Version < tls.VersionTLS13 || v.Pool == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(v.Schema) || v.Issuer == "" || v.Audience == "" || len(v.PublicKey) != ed25519.PublicKeySize || len(v.PeerCertificateSHA256) != 64 {
		return zero, ErrDenied
	}
	if len(r.Header.Values("X-Service-Assertion")) != 1 || len(r.Header.Values("X-Service-Signature")) != 1 {
		return zero, ErrDenied
	}
	raw := r.Header.Get("X-Service-Assertion")
	if len(raw) > 12000 {
		return zero, ErrDenied
	}
	wire, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return zero, ErrDenied
	}
	var assertion auth.ServiceAssertion
	decoder := json.NewDecoder(bytes.NewReader(wire))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&assertion) != nil {
		return zero, ErrDenied
	}
	if decoder.Decode(new(any)) != io.EOF {
		return zero, ErrDenied
	}
	canonical, marshalErr := json.Marshal(assertion)
	if marshalErr != nil || !bytes.Equal(canonical, wire) || !validUnicode(wire) || len(r.Header.Values("X-Request-ID")) != 1 || r.Header.Get("X-Request-ID") != assertion.Principal.TraceID {
		return zero, ErrDenied
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(r.Header.Get("X-Service-Signature"))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return zero, ErrDenied
	}
	var body []byte
	if r.Body != nil {
		body, err = io.ReadAll(io.LimitReader(r.Body, 65537))
		if err != nil || len(body) > 65536 {
			return zero, ErrDenied
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	principal, err := auth.VerifyServiceAssertion(assertion, signature, v.PublicKey, r.TLS, v.PeerCertificateSHA256, v.Issuer, v.Audience, auth.RequestDigest(r.Method, r.URL.RequestURI(), body), time.Now())
	if err != nil {
		return zero, ErrDenied
	}
	id := Identity{Actor: principal.Wallet, Audience: v.Audience, Kind: "user", Issued: time.Unix(assertion.IssuedAt, 0), Expires: time.Unix(assertion.ExpiresAt, 0), Scopes: map[string]bool{}}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var actor, audience string
	var issued, expires, mfa time.Time
	var scopes []string
	err = v.Pool.QueryRow(ctx, "SELECT actor,audience,issued_at,expires_at,mfa_expires_at,scopes FROM "+pgx.Identifier{v.Schema, "operator_sessions"}.Sanitize()+" WHERE session_id=$1 AND wallet=$2 AND NOT revoked AND expires_at>clock_timestamp()", principal.SessionID, principal.Wallet).Scan(&actor, &audience, &issued, &expires, &mfa, &scopes)
	if errors.Is(err, pgx.ErrNoRows) {
		return id, nil
	}
	if err != nil {
		return zero, err
	}
	if actor == "" || len(actor) > 160 || audience != v.Audience || expires.Sub(issued) > 15*time.Minute || len(scopes) > 32 {
		return zero, ErrDenied
	}
	id.Actor = actor
	id.Kind = "operator"
	id.Issued = issued
	id.Expires = expires
	id.MFA = mfa.After(time.Now()) && mfa.Sub(time.Now()) <= 5*time.Minute
	for _, scope := range scopes {
		if !regexp.MustCompile(`^[a-z][a-z0-9.-]{0,63}$`).MatchString(scope) {
			return zero, ErrDenied
		}
		id.Scopes[scope] = true
	}
	return id, nil
}

type PostgresJournal struct {
	Pool   *pgxpool.Pool
	Schema string
}

func (j PostgresJournal) Record(ctx context.Context, d Decision) error {
	if j.Pool == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(j.Schema) || d.Scope == "" || d.Version == "" || len(d.Actor) > 160 || len(d.RequestID) > 128 {
		return ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := j.Pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{j.Schema, "authorization_decisions"}.Sanitize()+"(actor,scope,request_id,policy_version,allowed) VALUES($1,$2,$3,$4,$5)", d.Actor, d.Scope, d.RequestID, d.Version, d.Allowed)
	return err
}
func SecurityDDL(schema string) (string, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schema) {
		return "", ErrDenied
	}
	s := pgx.Identifier{schema}.Sanitize()
	return `CREATE SCHEMA IF NOT EXISTS ` + s + `;
CREATE TABLE IF NOT EXISTS ` + s + `.operator_sessions(session_id text PRIMARY KEY,wallet text NOT NULL,actor text NOT NULL,audience text NOT NULL,issued_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,mfa_expires_at timestamptz NOT NULL,scopes text[] NOT NULL,revoked boolean NOT NULL DEFAULT false,CHECK(expires_at>issued_at AND expires_at-issued_at<=interval '15 minutes'));
CREATE TABLE IF NOT EXISTS ` + s + `.deadletter_redrives(job_id text PRIMARY KEY,consumer_id text NOT NULL,projection_generation text NOT NULL,envelope_hash text NOT NULL,reason text NOT NULL,actor text NOT NULL,state text NOT NULL DEFAULT 'pending',created_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE IF NOT EXISTS ` + s + `.authorization_decisions(seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,at timestamptz NOT NULL DEFAULT clock_timestamp(),actor text NOT NULL,scope text NOT NULL,request_id text NOT NULL,policy_version text NOT NULL,allowed boolean NOT NULL);
CREATE TABLE IF NOT EXISTS ` + s + `.audit_outbox(seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,at timestamptz NOT NULL DEFAULT clock_timestamp(),record jsonb NOT NULL);
CREATE OR REPLACE FUNCTION ` + s + `.reject_audit_change() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'append-only security journal'; END $$;
DROP TRIGGER IF EXISTS immutable_authorization ON ` + s + `.authorization_decisions;
CREATE TRIGGER immutable_authorization BEFORE UPDATE OR DELETE OR TRUNCATE ON ` + s + `.authorization_decisions FOR EACH STATEMENT EXECUTE FUNCTION ` + s + `.reject_audit_change();
DROP TRIGGER IF EXISTS immutable_audit ON ` + s + `.audit_outbox;
CREATE TRIGGER immutable_audit BEFORE UPDATE OR DELETE OR TRUNCATE ON ` + s + `.audit_outbox FOR EACH STATEMENT EXECUTE FUNCTION ` + s + `.reject_audit_change();`, nil
}
