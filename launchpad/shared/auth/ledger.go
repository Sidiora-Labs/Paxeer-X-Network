package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"net"
	"strings"
	"time"
)

var ErrReplayMismatch = errors.New("operation replay mismatch")

// LedgerDDL is staged migration input. Runtime roles must lack UPDATE/DELETE on
// audit rows; delivery checkpoints belong in a separate integration-owned table.
const LedgerDDL = `CREATE TABLE operation_ledger(actor text NOT NULL,action text NOT NULL,nonce text NOT NULL,envelope_digest text NOT NULL,body_digest text NOT NULL,request_id uuid NOT NULL,response bytea NOT NULL,response_digest text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),retain_until timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',PRIMARY KEY(actor,action,nonce),CHECK(retain_until>=created_at+interval '24 hours'));
CREATE TABLE security_audit_outbox(id bigserial PRIMARY KEY,request_id uuid NOT NULL,actor_pseudonym text NOT NULL,action text NOT NULL,result text NOT NULL,policy_version text NOT NULL,request_digest text NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(request_id,action));`

type Ledger struct {
	Pool        *pgxpool.Pool
	Environment string
	AuditKey    []byte
}
type OperationRequest struct {
	Envelope                                              OperationEnvelope
	Signature                                             string
	Body                                                  []byte
	Actor, Action, Method, Path, RequestID, PolicyVersion string
}
type Mutation func(context.Context, pgx.Tx, []byte) ([]byte, error)

// ExecuteValidated requires caller authorization in verify; a successful signature
// alone does not establish role/ownership. Callback, nonce and audit commit once.
func (l Ledger) ExecuteValidated(ctx context.Context, req OperationRequest, verify func(context.Context, string, string, string) error, mutate Mutation) ([]byte, bool, error) {
	if l.Pool == nil {
		return nil, false, fmt.Errorf("ledger configuration")
	}
	tx, err := l.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, &VerificationError{Category: "upstream", Retryable: true}
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	response, duplicate, err := l.ExecuteValidatedTx(ctx, tx, req, verify, mutate)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, &VerificationError{Category: "upstream", Retryable: true}
	}
	return response, duplicate, nil
}

func (l Ledger) ExecuteValidatedTx(ctx context.Context, tx pgx.Tx, req OperationRequest, verify func(context.Context, string, string, string) error, mutate Mutation) (response []byte, duplicate bool, err error) {
	defer func() {
		var database *pgconn.PgError
		var network net.Error
		if errors.As(err, &database) || errors.As(err, &network) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, pgx.ErrTxClosed) {
			response, duplicate, err = nil, false, &VerificationError{Category: "upstream", Retryable: true}
		}
	}()
	if tx == nil || len(l.AuditKey) < 32 || verify == nil || mutate == nil || !validIdentityID(req.RequestID) || !tokenPattern.MatchString(req.PolicyVersion) {
		return nil, false, fmt.Errorf("ledger configuration")
	}
	req.Body = append([]byte(nil), req.Body...)
	statement, err := req.Envelope.Statement()
	if err != nil {
		return nil, false, err
	}
	if err = verify(ctx, req.Actor, statement, req.Signature); err != nil {
		return nil, false, err
	}

	hash := sha256.Sum256([]byte(statement))
	digest := hex.EncodeToString(hash[:])
	lockKey := strings.Join([]string{req.Envelope.Actor, req.Action, req.Envelope.Nonce}, "|")
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lockKey); err != nil {
		return nil, false, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return nil, false, err
	}
	if err = req.Envelope.Validate(now, l.Environment, req.Actor, req.Action, req.Method, req.Path, req.Body); err != nil {
		return nil, false, err
	}
	var priorDigest, priorBodyDigest, priorResponseDigest string
	err = tx.QueryRow(ctx, "SELECT envelope_digest,body_digest,response,response_digest FROM operation_ledger WHERE actor=$1 AND action=$2 AND nonce=$3", req.Envelope.Actor, req.Action, req.Envelope.Nonce).Scan(&priorDigest, &priorBodyDigest, &response, &priorResponseDigest)
	if err == nil {
		if priorDigest != digest {
			return nil, false, ErrReplayMismatch
		}
		responseHash := sha256.Sum256(response)
		if priorBodyDigest != req.Envelope.BodyHash || len(response) > 1<<20 || hex.EncodeToString(responseHash[:]) != priorResponseDigest {
			return nil, false, &VerificationError{Category: "replay_integrity", Retryable: true}
		}
		return append([]byte(nil), response...), true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	response, err = mutate(ctx, tx, append([]byte(nil), req.Body...))
	if err != nil {
		return nil, false, err
	}
	if len(response) > 1<<20 {
		return nil, false, fmt.Errorf("response too large")
	}
	responseHash := sha256.Sum256(response)
	_, err = tx.Exec(ctx, "INSERT INTO operation_ledger(actor,action,nonce,envelope_digest,body_digest,request_id,response,response_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", req.Envelope.Actor, req.Action, req.Envelope.Nonce, digest, req.Envelope.BodyHash, req.RequestID, response, hex.EncodeToString(responseHash[:]))
	if err != nil {
		return nil, false, err
	}
	err = AppendAudit(ctx, tx, l.AuditKey, AuditEvent{RequestID: req.RequestID, Actor: req.Envelope.Actor, Action: req.Action, Result: "accepted", PolicyVersion: req.PolicyVersion, RequestDigest: digest})
	if err != nil {
		return nil, false, err
	}
	return response, false, nil
}

type AuditEvent struct{ RequestID, Actor, Action, Result, PolicyVersion, RequestDigest string }

func AppendAudit(ctx context.Context, tx pgx.Tx, key []byte, event AuditEvent) error {
	if tx == nil || len(key) < 32 || !tokenPattern.MatchString(event.Action) || !tokenPattern.MatchString(event.Result) || !tokenPattern.MatchString(event.PolicyVersion) || !validIdentityID(event.RequestID) || len(event.RequestDigest) != 64 {
		return fmt.Errorf("invalid audit")
	}
	decoded, decodeErr := hex.DecodeString(event.RequestDigest)
	if decodeErr != nil || len(decoded) != 32 || strings.ToLower(event.RequestDigest) != event.RequestDigest {
		return fmt.Errorf("invalid audit digest")
	}
	actor, err := NormalizeWallet(event.Actor)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(actor))
	_, err = tx.Exec(ctx, "INSERT INTO security_audit_outbox(request_id,actor_pseudonym,action,result,policy_version,request_digest) VALUES($1,$2,$3,$4,$5,$6)", event.RequestID, hex.EncodeToString(mac.Sum(nil)), event.Action, event.Result, event.PolicyVersion, event.RequestDigest)
	return err
}

func RevokeIdentityAccountTx(ctx context.Context, tx pgx.Tx, accountID, action string) error {
	if tx == nil || !validIdentityID(accountID) || !tokenPattern.MatchString(action) {
		return ErrScopeDenied
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT auth_epoch FROM identity_accounts WHERE account_id=$1 FOR UPDATE`, accountID).Scan(&epoch); err != nil {
		return err
	}
	if epoch == 9223372036854775807 {
		return fmt.Errorf("auth epoch overflow")
	}
	if _, err := tx.Exec(ctx, `UPDATE identity_accounts SET auth_epoch=auth_epoch+1 WHERE account_id=$1`, accountID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE account_id=$1 AND revoked_at IS NULL`, accountID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO identity_outbox(event_id,account_id,session_id,action,created_at) SELECT $1,$2,session_id,$3,clock_timestamp() FROM identity_sessions WHERE account_id=$2 ORDER BY issued_at DESC LIMIT 1`, uuid.NewString(), accountID, action)
	return err
}
func validIdentityID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}
func ConsumeIdentityReauthTx(ctx context.Context, tx pgx.Tx, accountID, sessionID, action, proof string) error {
	if tx == nil || !validIdentityID(accountID) || !validIdentityID(sessionID) || !tokenPattern.MatchString(action) || len(proof) != 64 {
		return ErrScopeDenied
	}
	raw, err := hex.DecodeString(proof)
	if err != nil || len(raw) != 32 {
		return ErrScopeDenied
	}
	hash := sha256.Sum256([]byte(proof))
	tag, err := tx.Exec(ctx, `UPDATE identity_reauth SET consumed_at=clock_timestamp() WHERE proof_hash=$1 AND account_id=$2 AND session_id=$3 AND action=$4 AND consumed_at IS NULL AND expires_at>clock_timestamp()`, hash[:], accountID, sessionID, action)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrScopeDenied
	}
	return nil
}
