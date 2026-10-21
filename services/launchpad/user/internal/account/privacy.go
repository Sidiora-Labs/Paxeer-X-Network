package account

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"strconv"
	"time"
)

// EndpointProofVerifier verifies a fresh provider challenge bound to account and token.
type EndpointProofVerifier func(context.Context, uuid.UUID, string, string, time.Time) error

func Seal(key, plain, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	n := make([]byte, g.NonceSize())
	if _, e = io.ReadFull(rand.Reader, n); e != nil {
		return nil, e
	}
	return g.Seal(n, n, plain, aad), nil
}
func Open(key, sealed, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	b, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	if len(sealed) < g.NonceSize() {
		return nil, ErrInvalid
	}
	return g.Open(nil, sealed[:g.NonceSize()], sealed[g.NonceSize():], aad)
}
func fingerprint(key []byte, token string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(token))
	return m.Sum(nil)
}
func (s Store) BindEndpoint(ctx context.Context, p Principal, token, platform, proof string, key []byte, verify EndpointProofVerifier) error {
	if p.AccountID == uuid.Nil || len(token) < 1 || len(token) > 4096 || len(proof) < 1 || len(proof) > 4096 || len(key) != 32 || (platform != "ios" && platform != "android" && platform != "web") {
		return ErrInvalid
	}
	if verify == nil {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	if e := verify(ctx, p.AccountID, token, proof, s.now()); e != nil {
		return e
	}
	sealed, e := Seal(key, []byte(token), []byte(p.AccountID.String()))
	if e != nil {
		return e
	}
	return s.mutate(ctx, p, p.AccountID, "", "endpoint.bound", func(tx pgx.Tx, a *state) error {
		proofHash := sha256.Sum256([]byte(proof))
		tag, e := tx.Exec(ctx, `INSERT INTO account_endpoint_proof VALUES($1,$2) ON CONFLICT DO NOTHING`, proofHash[:], s.now().Add(5*time.Minute))
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrDenied
		}
		_, e = tx.Exec(ctx, `INSERT INTO account_endpoint VALUES($1,$2,$3,$4,$5,$6,false) ON CONFLICT(fingerprint) DO UPDATE SET account_id=EXCLUDED.account_id,ciphertext=EXCLUDED.ciphertext,platform=EXCLUDED.platform,consent_revision=EXCLUDED.consent_revision,validated_at=EXCLUDED.validated_at,revoked=false`, fingerprint(key, token), p.AccountID, sealed, platform, a.Revision, s.now())
		return e
	})
}
func (s Store) RevokeEndpoint(ctx context.Context, p Principal, token string, key []byte) error {
	if len(key) != 32 || len(token) > 4096 {
		return ErrInvalid
	}
	return s.mutate(ctx, p, p.AccountID, "", "endpoint.revoked", func(tx pgx.Tx, a *state) error {
		_, e := tx.Exec(ctx, `UPDATE account_endpoint SET revoked=true WHERE fingerprint=$1 AND account_id=$2`, fingerprint(key, token), p.AccountID)
		return e
	})
}

type Job struct {
	ID          uuid.UUID
	AccountID   uuid.UUID
	Kind        string
	State       string
	RequestedAt time.Time
}

// RevokeSessions must revoke sessions within tx; absent integration fails closed.
type RevokeSessions func(context.Context, pgx.Tx, uuid.UUID) error

func (s Store) RequestDeletion(ctx context.Context, p Principal, expected string, revoke RevokeSessions) (Job, error) {
	j := Job{uuid.New(), p.AccountID, "deletion", "processing", s.now()}
	if e := p.Fresh(s.now()); e != nil {
		return Job{}, e
	}
	if revoke == nil {
		return Job{}, errors.New("DEPENDENCY_UNAVAILABLE")
	}
	e := s.mutate(ctx, p, p.AccountID, expected, "account.deletion.requested", s.deletionMutation(ctx, p, j, revoke))
	return j, e
}
func (s Store) deletionMutation(ctx context.Context, p Principal, j Job, revoke RevokeSessions) func(pgx.Tx, *state) error {
	return func(tx pgx.Tx, a *state) error {
		if e := revoke(ctx, tx, p.AccountID); e != nil {
			return e
		}
		a.Deleted = true
		a.Profile = Profile{PublicProfile: PublicProfile{Wallet: a.Wallet}, SchemaVersion: 1, UpdatedAt: s.now()}
		a.Preferences = DefaultPreferences()
		for _, table := range []string{"account_watch", "account_consent", "account_endpoint", "account_device_installation", "account_idempotency"} {
			if _, e := tx.Exec(ctx, `DELETE FROM `+table+` WHERE account_id=$1`, p.AccountID); e != nil {
				return e
			}
		}
		b, _ := json.Marshal(map[string]any{"requiredOwners": []string{"user", "sessions", "images", "notifications"}, "acks": []string{"user", "sessions"}, "deadline": s.now().Add(30 * 24 * time.Hour)})
		_, e := tx.Exec(ctx, `INSERT INTO account_job(job_id,account_id,kind,state,requested_at,payload) VALUES($1,$2,'deletion','processing',$3,$4)`, j.ID, p.AccountID, s.now(), b)
		if e != nil {
			return e
		}
		return emit(ctx, tx, *a, "account.deletion.job", map[string]any{"deletionId": j.ID})
	}
}

func (s Store) RequestDeletionOperation(ctx context.Context, p Principal, ledger sharedauth.Ledger, req sharedauth.OperationRequest, sessionID, key uuid.UUID, verify func(context.Context, string, string, string) error) ([]byte, bool, error) {
	if s.DB == nil || ledger.Pool != s.DB || verify == nil || key == uuid.Nil || sessionID == uuid.Nil {
		return nil, false, errors.New("DEPENDENCY_UNAVAILABLE")
	}
	if req.Action != "account.delete" || req.Method != "POST" || req.Path != "/v1/me/deletion-requests" || len(req.Body) > 16384 {
		return nil, false, ErrInvalid
	}
	id, err := uuid.Parse(req.RequestID)
	if err != nil || id == uuid.Nil {
		return nil, false, ErrInvalid
	}
	var body DeletionBody
	if err = Decode(bytes.NewReader(req.Body), &body); err != nil {
		return nil, false, err
	}
	if err = body.Validate(p); err != nil {
		return nil, false, err
	}
	if err = p.Fresh(s.now()); err != nil {
		return nil, false, err
	}
	return ledger.ExecuteValidated(ctx, req, verify, func(ctx context.Context, tx pgx.Tx, validated []byte) ([]byte, error) {
		if !bytes.Equal(validated, req.Body) {
			return nil, ErrInvalid
		}
		proofBytes, err := hex.DecodeString(body.ReauthProof)
		if err != nil || len(proofBytes) != 32 || body.ReauthProof != hex.EncodeToString(proofBytes) {
			return nil, ErrDenied
		}
		proofHash := sha256.Sum256([]byte(body.ReauthProof))
		if _, err := tx.Exec(ctx, `SELECT users.consume_account_deletion_reauth($1,$2,$3)`, proofHash[:], p.AccountID, sessionID); err != nil {
			return nil, deletionGuardError(err)
		}
		var wallet string
		if err := tx.QueryRow(ctx, `SELECT wallet FROM account_state WHERE account_id=$1 FOR UPDATE`, p.AccountID).Scan(&wallet); err != nil {
			return nil, err
		}
		if req.Actor != wallet {
			return nil, ErrDenied
		}
		digest := sha256.Sum256(validated)
		var priorDigest, priorResult []byte
		var expires time.Time
		err = tx.QueryRow(ctx, `SELECT digest,result,expires_at FROM account_idempotency WHERE account_id=$1 AND method=$2 AND path=$3 AND key=$4`, p.AccountID, req.Method, req.Path, key).Scan(&priorDigest, &priorResult, &expires)
		if err == nil && expires.After(s.now()) {
			if !bytes.Equal(priorDigest, digest[:]) {
				return nil, ErrIdempotency
			}
			return priorResult, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		j := Job{uuid.New(), p.AccountID, "deletion", "processing", s.now()}
		revoke := func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
			_, err := tx.Exec(ctx, `SELECT users.revoke_account_sessions($1)`, id)
			return deletionGuardError(err)
		}
		if err := s.mutateTx(ctx, tx, p, p.AccountID, body.ExpectedRevision, "account.deletion.requested", s.deletionMutation(ctx, p, j, revoke)); err != nil {
			return nil, err
		}
		var revision int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM account_state WHERE account_id=$1`, p.AccountID).Scan(&revision); err != nil {
			return nil, err
		}
		data := deletionProjection(j)
		envelope, err := Response(id, data, strconv.FormatInt(revision, 10), s.now())
		if err != nil {
			return nil, err
		}
		result, err := json.Marshal(envelope)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO account_idempotency VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(account_id,method,path,key) DO UPDATE SET digest=EXCLUDED.digest,result=EXCLUDED.result,expires_at=EXCLUDED.expires_at`, p.AccountID, req.Method, req.Path, key, digest[:], result, s.now().Add(24*time.Hour))
		return result, err
	})
}

func (s Store) RequestExport(ctx context.Context, p Principal) (Job, error) {
	if e := p.Fresh(s.now()); e != nil {
		return Job{}, e
	}
	j := Job{uuid.New(), p.AccountID, "export", "requested", s.now()}
	e := s.mutate(ctx, p, p.AccountID, "", "account.export.requested", func(tx pgx.Tx, a *state) error {
		_, e := tx.Exec(ctx, `INSERT INTO account_job(job_id,account_id,kind,state,requested_at,payload) VALUES($1,$2,'export','requested',$3,'{}')`, j.ID, p.AccountID, s.now())
		return e
	})
	return j, e
}

// ExportSnapshot excludes secrets, endpoint tokens, and other-service DM bodies.
func (s Store) ExportSnapshot(ctx context.Context, p Principal) (json.RawMessage, error) {
	if p.AccountID == uuid.Nil {
		return nil, ErrDenied
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	a, e := load(ctx, tx, p.AccountID)
	if e != nil {
		return nil, e
	}
	if a.Deleted {
		return nil, ErrState
	}
	rows, e := tx.Query(ctx, `SELECT pool,added_at,revision,alert_enabled FROM account_watch WHERE account_id=$1 ORDER BY added_at,pool LIMIT 500`, p.AccountID)
	if e != nil {
		return nil, e
	}
	v := []WatchEntry{}
	for rows.Next() {
		var w WatchEntry
		if e = rows.Scan(&w.Pool, &w.AddedAt, &w.Revision, &w.AlertEnabled); e != nil {
			rows.Close()
			return nil, e
		}
		v = append(v, w)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	consents := []map[string]any{}
	consentRows, e := tx.Query(ctx, `SELECT category,channel,enabled,source,policy_version,revision,revoke_revision,changed_at FROM account_consent WHERE account_id=$1 ORDER BY category,channel`, p.AccountID)
	if e != nil {
		return nil, e
	}
	for consentRows.Next() {
		var category, channel, source, policy string
		var enabled bool
		var revision, revoke int64
		var at time.Time
		if e = consentRows.Scan(&category, &channel, &enabled, &source, &policy, &revision, &revoke, &at); e != nil {
			consentRows.Close()
			return nil, e
		}
		consents = append(consents, map[string]any{"category": category, "channel": channel, "enabled": enabled, "source": source, "policyVersion": policy, "revision": revision, "revokeRevision": revoke, "changedAt": at.UTC()})
	}
	e = consentRows.Err()
	consentRows.Close()
	if e != nil {
		return nil, e
	}
	return json.Marshal(map[string]any{"profile": a.Profile, "preferences": a.Preferences, "watchlist": v, "consents": consents})
}

// ReadyExport requires encrypted object storage supplied by the integration worker.
func (s Store) ReadyExport(ctx context.Context, id uuid.UUID, ref string) (string, error) {
	if ref == "" || len(ref) > 512 {
		return "", ErrInvalid
	}
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	token := hex.EncodeToString(raw)
	h := sha256.Sum256([]byte(token))
	b, _ := json.Marshal(map[string]any{"encryptedObject": ref, "retainUntil": s.now().Add(24 * time.Hour)})
	tag, e := s.DB.Exec(ctx, `UPDATE account_job SET state='ready',expires_at=$2,download_hash=$3,payload=$4 WHERE job_id=$1 AND kind='export' AND state IN ('requested','preparing','failed')`, id, s.now().Add(15*time.Minute), h[:], b)
	if e != nil {
		return "", e
	}
	if tag.RowsAffected() != 1 {
		return "", ErrState
	}
	return token, nil
}
func (s Store) ConsumeExport(ctx context.Context, p Principal, id uuid.UUID, token string) (json.RawMessage, error) {
	if p.AccountID == uuid.Nil || len(token) > 128 {
		return nil, ErrDenied
	}
	h := sha256.Sum256([]byte(token))
	var b []byte
	e := s.DB.QueryRow(ctx, `UPDATE account_job SET consumed_at=$4 WHERE job_id=$1 AND account_id=$2 AND kind='export' AND state='ready' AND expires_at>$4 AND consumed_at IS NULL AND download_hash=$3 AND EXISTS(SELECT 1 FROM account_state a WHERE a.account_id=account_job.account_id AND NOT a.deleted) RETURNING payload`, id, p.AccountID, h[:], s.now()).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrDenied
	}
	return b, e
}
func (s Store) AckDeletion(ctx context.Context, id uuid.UUID, owner string) error {
	if owner != "user" && owner != "sessions" && owner != "images" && owner != "notifications" {
		return ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var b []byte
	e = tx.QueryRow(ctx, `SELECT payload FROM account_job WHERE job_id=$1 AND kind='deletion' AND state='processing' FOR UPDATE`, id).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrState
	}
	if e != nil {
		return e
	}
	var v struct {
		Required []string  `json:"requiredOwners"`
		Acks     []string  `json:"acks"`
		Deadline time.Time `json:"deadline"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, s := range v.Acks {
		seen[s] = true
	}
	if !seen[owner] {
		v.Acks = append(v.Acks, owner)
		seen[owner] = true
	}
	complete := len(v.Required) > 0
	for _, s := range v.Required {
		complete = complete && seen[s]
	}
	st := "processing"
	if complete {
		st = "completed"
	}
	b, _ = json.Marshal(v)
	_, e = tx.Exec(ctx, `UPDATE account_job SET state=$2,payload=$3 WHERE job_id=$1`, id, st, b)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// Backfill never merges conflicting stable account identities; changed sources quarantine.
func (s Store) Backfill(ctx context.Context, source string, id uuid.UUID, wallet string, digest []byte) error {
	if source == "" || len(source) > 256 || id == uuid.Nil || len(digest) != 32 {
		return ErrInvalid
	}
	w, e := Wallet(wallet)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "l35-source:"+source)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "l35-wallet:"+w)
	if e != nil {
		return e
	}
	var existing uuid.UUID
	e = tx.QueryRow(ctx, `SELECT account_id FROM account_state WHERE wallet=$1`, w).Scan(&existing)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	st, reason := "linked", ""
	if e == nil && existing != id {
		st, reason = "quarantined", "historical identity conflict"
	}
	var old []byte
	var oldID *uuid.UUID
	var oldState string
	e = tx.QueryRow(ctx, `SELECT source_hash,account_id,state FROM account_backfill WHERE source_key=$1 FOR UPDATE`, source).Scan(&old, &oldID, &oldState)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if oldState == "quarantined" {
		st, reason = "quarantined", "review required for quarantined source"
	}
	if e == nil && (string(old) != string(digest) || oldID == nil || *oldID != id) {
		st, reason = "quarantined", "source changed or ownership conflict"
	}
	if st == "linked" {
		pr, _ := json.Marshal(Profile{PublicProfile: PublicProfile{Wallet: w, Revision: "0"}, SchemaVersion: 1})
		pf, _ := json.Marshal(DefaultPreferences())
		_, e = tx.Exec(ctx, `INSERT INTO account_state(account_id,wallet,profile,preferences) VALUES($1,$2,$3,$4) ON CONFLICT(account_id) DO NOTHING`, id, w, pr, pf)
		if e != nil {
			return e
		}
		var actual string
		if e = tx.QueryRow(ctx, `SELECT wallet FROM account_state WHERE account_id=$1`, id).Scan(&actual); e != nil {
			return e
		}
		if actual != w {
			st, reason = "quarantined", "account wallet conflict"
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO account_backfill VALUES($1,$2,$3,$4,$5) ON CONFLICT(source_key) DO UPDATE SET state=CASE WHEN account_backfill.state='quarantined' THEN 'quarantined' ELSE EXCLUDED.state END,reason=EXCLUDED.reason`, source, digest, id, st, reason)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func deletionGuardError(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "28000" {
		return ErrDenied
	}
	return err
}
