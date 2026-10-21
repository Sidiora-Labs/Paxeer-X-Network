package streams

// This persistence primitive is deliberately separate from legacy HTTP auth.
// Callers must supply a current creator/moderation decision obtained from trusted
// identity and policy services, never from user-provided JSON.
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrConflict    = errors.New("stream conflict")
	ErrDenied      = errors.New("stream authorization unavailable or denied")
	ErrQuota       = errors.New("stream admission quota")
	ErrUnavailable = errors.New("stream provider unavailable")
	ErrInvalid     = errors.New("invalid stream request")
	// ErrNotProvisioned: the deployment has no broadcast provider, so the
	// request cannot be served here (409 BROADCAST_NOT_PROVISIONED).
	ErrNotProvisioned = errors.New("livestream broadcasting not provisioned")
	// ErrState: the stream's lifecycle state does not admit the request, e.g. a
	// viewer session or heartbeat on a stream that is not live (409
	// STATE_CONFLICT).
	ErrState = errors.New("stream state does not admit the request")
	// ErrViewerToken: the viewer-session token is invalid, expired or bound to
	// a superseded playback policy; the viewer opens a new session (401
	// AUTH_EXPIRED).
	ErrViewerToken = errors.New("viewer session token invalid")
	// ErrMarketCapUnavailable: the stats market-cap projection could not be
	// read; a dependency failure (503 DEPENDENCY_UNAVAILABLE).
	ErrMarketCapUnavailable = fmt.Errorf("market cap projection unavailable: %w", ErrUnavailable)
)
var addressPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var identityPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var schemaPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)

type LifecycleStore struct {
	Budget                   *BudgetPolicy
	pool                     *pgxpool.Pool
	schema                   string
	walletLimit, globalLimit int
}

func NewLifecycleStore(pool *pgxpool.Pool, schema string, walletLimit, globalLimit int) (*LifecycleStore, error) {
	if pool == nil || !schemaPattern.MatchString(schema) || walletLimit < 1 || walletLimit > 100 || globalLimit < 1 || globalLimit > 100000 {
		return nil, ErrInvalid
	}
	return &LifecycleStore{pool: pool, schema: schema, walletLimit: walletLimit, globalLimit: globalLimit}, nil
}

// SchemaSQL is a migration proposal. Runtime must not invoke it; the serial
// integrator owns migration registration and privileged DDL execution.
func (s *LifecycleStore) SchemaSQL() string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.stream_operations(
 id text PRIMARY KEY,wallet text NOT NULL,pool text NOT NULL,title text NOT NULL,idem_key text NOT NULL,request_hash text NOT NULL,
 state text NOT NULL CHECK(state IN ('REQUESTED','PROVISIONING','READY','LIVE','ENDING','ENDED','FAILED','SUSPENDED')),
 intent text NOT NULL DEFAULT 'create',provider_id text UNIQUE,revision bigint NOT NULL DEFAULT 1,
 provider_time timestamptz,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),retry_at timestamptz,
 last_reason text NOT NULL DEFAULT '',terminal_at timestamptz,UNIQUE(wallet,idem_key));
 CREATE TABLE IF NOT EXISTS %s.stream_inbox(event_id text PRIMARY KEY,request_hash text NOT NULL,provider_id text NOT NULL,
 observed_at timestamptz NOT NULL,received_at timestamptz NOT NULL DEFAULT clock_timestamp(),outcome text NOT NULL);
 CREATE TABLE IF NOT EXISTS %s.stream_audit(id bigserial PRIMARY KEY,operation_id text NOT NULL,actor text NOT NULL,reason text NOT NULL,
 revision bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());
 CREATE TABLE IF NOT EXISTS %s.stream_outbox(id bigserial PRIMARY KEY,operation_id text NOT NULL REFERENCES %s.stream_operations(id),revision bigint NOT NULL,kind text NOT NULL CHECK(kind IN ('provision','start','end','suspend','provider_state')),payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),UNIQUE(operation_id,revision,kind));`, s.schema, s.schema, s.schema, s.schema, s.schema)
}

type CreatorDecision struct {
	Wallet, Pool string
	Allowed      bool
	ExpiresAt    time.Time
}
type CreateIntent struct{ Wallet, Pool, Title, IdempotencyKey string }
type Operation struct {
	ID, Wallet, Pool, Title, State, Intent, ProviderID string
	Revision                                           int64
	ProviderTime, RetryAt                              *time.Time
}

func readOperation(row pgx.Row) (Operation, error) {
	var o Operation
	err := row.Scan(&o.ID, &o.Wallet, &o.Pool, &o.Title, &o.State, &o.Intent, &o.ProviderID, &o.Revision, &o.ProviderTime, &o.RetryAt)
	return o, err
}

const opColumns = `id,wallet,pool,title,state,intent,COALESCE(provider_id,''),revision,provider_time,retry_at`

func randomID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *LifecycleStore) Create(ctx context.Context, r CreateIntent, d CreatorDecision, providerReady bool) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Operation{}, err
	}
	defer rollbackBounded(tx)
	o, err := s.CreateTx(ctx, tx, r, d, providerReady)
	if err != nil {
		return Operation{}, err
	}
	return o, tx.Commit(ctx)
}

func (s *LifecycleStore) CreateTx(ctx context.Context, tx pgx.Tx, r CreateIntent, d CreatorDecision, providerReady bool) (Operation, error) {
	if tx == nil {
		return Operation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !addressPattern.MatchString(r.Wallet) || !addressPattern.MatchString(r.Pool) || !utf8.ValidString(r.Title) || len([]rune(r.Title)) < 1 || len([]rune(r.Title)) > 120 || strings.TrimSpace(r.Title) == "" || (!identityPattern.MatchString(r.IdempotencyKey) || strings.HasPrefix(r.IdempotencyKey, "expired_")) {
		return Operation{}, ErrInvalid
	}
	r.Wallet = strings.ToLower(r.Wallet)
	r.Pool = strings.ToLower(r.Pool)
	if !d.Allowed || !strings.EqualFold(d.Wallet, r.Wallet) || !strings.EqualFold(d.Pool, r.Pool) || !d.ExpiresAt.After(time.Now()) {
		return Operation{}, ErrDenied
	}
	var e error
	// A schema-specific global admission lock serializes reservation and quota checks.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", s.schema+":admission"); e != nil {
		return Operation{}, e
	}
	// Authorization must remain fresh after waiting for admission.
	if !d.ExpiresAt.After(time.Now()) {
		return Operation{}, ErrDenied
	}
	var oldHash string
	e = tx.QueryRow(ctx, fmt.Sprintf("SELECT request_hash FROM %s.stream_operations WHERE wallet=$1 AND idem_key=$2", s.schema), r.Wallet, r.IdempotencyKey).Scan(&oldHash)
	hash := digest(r)
	if e == nil {
		if oldHash != hash {
			return Operation{}, ErrConflict
		}
		o, e := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE wallet=$1 AND idem_key=$2", s.schema), r.Wallet, r.IdempotencyKey))
		if e != nil {
			return o, e
		}
		return o, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return Operation{}, e
	}
	if !providerReady {
		return Operation{}, ErrUnavailable
	}
	var walletCount, total int
	e = tx.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FILTER(WHERE wallet=$1),count(*) FROM %s.stream_operations WHERE state NOT IN ('ENDED','FAILED')", s.schema), r.Wallet).Scan(&walletCount, &total)
	if e != nil {
		return Operation{}, e
	}
	if s.Budget != nil {
		if e = s.Budget.admit(ctx, tx, s.schema, total); e != nil {
			return Operation{}, e
		}
	}
	if walletCount >= s.walletLimit || total >= s.globalLimit {
		return Operation{}, ErrQuota
	}
	id, e := randomID()
	if e != nil {
		return Operation{}, e
	}
	o, e := readOperation(tx.QueryRow(ctx, fmt.Sprintf("INSERT INTO %s.stream_operations(id,wallet,pool,title,idem_key,request_hash,state) VALUES($1,$2,$3,$4,$5,$6,'REQUESTED') RETURNING "+opColumns, s.schema), id, r.Wallet, r.Pool, r.Title, r.IdempotencyKey, hash))
	if e != nil {
		return o, e
	}
	_, e = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision) VALUES($1,$2,'create_requested',1)", s.schema), id, r.Wallet)
	if e != nil {
		return Operation{}, e
	}
	if e = s.enqueue(ctx, tx, o, "provision"); e != nil {
		return Operation{}, e
	}
	return o, nil
}
func (s *LifecycleStore) Get(ctx context.Context, id string) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return readOperation(s.pool.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1", s.schema), id))
}

// Transition is a compare-and-set guard. A user start is an intent, never LIVE.
type ActionDecision struct {
	Wallet, OperationID, Action string
	Revision                    int64
	Allowed                     bool
	ExpiresAt                   time.Time
}

func (s *LifecycleStore) Transition(ctx context.Context, id, actor, action string, revision int64, decision ActionDecision) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Operation{}, err
	}
	defer rollbackBounded(tx)
	o, err := s.TransitionTx(ctx, tx, id, actor, action, revision, decision)
	if err != nil {
		return Operation{}, err
	}
	return o, tx.Commit(ctx)
}

func (s *LifecycleStore) TransitionTx(ctx context.Context, tx pgx.Tx, id, actor, action string, revision int64, decision ActionDecision) (Operation, error) {
	if tx == nil {
		return Operation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !decision.Allowed || decision.OperationID != id || decision.Action != action || decision.Revision != revision || !strings.EqualFold(decision.Wallet, actor) || !decision.ExpiresAt.After(time.Now()) {
		return Operation{}, ErrDenied
	}
	var e error
	o, e := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", s.schema), id))
	if e != nil {
		return o, e
	}
	if !decision.ExpiresAt.After(time.Now()) || !strings.EqualFold(actor, o.Wallet) {
		return Operation{}, ErrDenied
	}
	if o.Revision != revision {
		return Operation{}, ErrConflict
	}
	next, intent := o.State, o.Intent
	switch action {
	case "start":
		if o.State != "READY" {
			return Operation{}, ErrConflict
		}
		intent = "start"
	case "end":
		if o.State == "ENDED" || o.State == "FAILED" {
			return o, nil
		}
		next = "ENDING"
		intent = "end"
	default:
		return Operation{}, ErrInvalid
	}
	o, e = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state=$2,intent=$3,revision=revision+1 WHERE id=$1 RETURNING "+opColumns, s.schema), id, next, intent))
	if e != nil {
		return o, e
	}
	_, e = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision) VALUES($1,$2,$3,$4)", s.schema), id, actor, action, o.Revision)
	if e != nil {
		return Operation{}, e
	}
	if e = s.enqueue(ctx, tx, o, action); e != nil {
		return Operation{}, e
	}
	return o, nil
}

// BindProvider adopts a correlated provider stream after external creation. A
// duplicate same binding is harmless; a second provider identity is an incident.
// This never stores ingest credentials. Encrypted secret storage is a prerequisite.
func (s *LifecycleStore) BindProvider(ctx context.Context, id, provider string) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !identityPattern.MatchString(provider) {
		return Operation{}, ErrInvalid
	}
	o, e := readOperation(s.pool.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET provider_id=$2,state='READY',revision=revision+1 WHERE id=$1 AND provider_id IS NULL AND state IN ('REQUESTED','PROVISIONING') RETURNING "+opColumns, s.schema), id, provider))
	if errors.Is(e, pgx.ErrNoRows) {
		o, e = s.Get(ctx, id)
		if e == nil && o.ProviderID == provider {
			return o, nil
		}
		return Operation{}, ErrConflict
	}
	return o, e
}

type ProviderEvent struct {
	EventID, ProviderID, State string
	ObservedAt                 time.Time
}

func (s *LifecycleStore) Observe(ctx context.Context, event ProviderEvent) (string, error) {
	event.ObservedAt = event.ObservedAt.UTC()
	if event.ObservedAt.Year() < 1970 || event.ObservedAt.Year() > 9999 {
		return "", ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !identityPattern.MatchString(event.EventID) || !identityPattern.MatchString(event.ProviderID) || (event.State != "LIVE" && event.State != "READY" && event.State != "ENDED") || event.ObservedAt.IsZero() || event.ObservedAt.After(time.Now().Add(5*time.Minute)) {
		return "", ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer rollbackBounded(tx)
	hash := digest(event)
	var prior string
	e = tx.QueryRow(ctx, fmt.Sprintf("INSERT INTO %s.stream_inbox(event_id,request_hash,provider_id,observed_at,outcome) VALUES($1,$2,$3,$4,'pending') ON CONFLICT DO NOTHING RETURNING request_hash", s.schema), event.EventID, hash, event.ProviderID, event.ObservedAt).Scan(&prior)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, fmt.Sprintf("SELECT request_hash FROM %s.stream_inbox WHERE event_id=$1", s.schema), event.EventID).Scan(&prior)
		if e != nil {
			return "", e
		}
		if prior != hash {
			return "", ErrConflict
		}
		return "duplicate", tx.Commit(ctx)
	}
	if e != nil {
		return "", e
	}
	o, e := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE provider_id=$1 FOR UPDATE", s.schema), event.ProviderID))
	outcome := "applied"
	if errors.Is(e, pgx.ErrNoRows) {
		outcome = "quarantined_unknown"
	} else if e != nil {
		return "", e
	} else if o.State == "ENDED" || o.State == "FAILED" || (o.State == "SUSPENDED" && event.State != "ENDED") || (o.State == "ENDING" && event.State != "ENDED") || (o.ProviderTime != nil && !event.ObservedAt.After(*o.ProviderTime)) {
		outcome = "ignored_stale_or_terminal"
	} else {
		o, e = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state=$2,provider_time=$3,revision=revision+1,retry_at=NULL,terminal_at=CASE WHEN $2='ENDED' THEN clock_timestamp() ELSE terminal_at END WHERE id=$1 RETURNING "+opColumns, s.schema), o.ID, event.State, event.ObservedAt))
		if e != nil {
			return "", e
		}
		if _, e = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'provider_observer',$2,$3)", s.schema), o.ID, "provider_"+strings.ToLower(event.State), o.Revision); e != nil {
			return "", e
		}
		if e = s.enqueue(ctx, tx, o, "provider_state"); e != nil {
			return "", e
		}
	}
	_, e = tx.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_inbox SET outcome=$2 WHERE event_id=$1", s.schema), event.EventID, outcome)
	if e != nil {
		return "", e
	}
	return outcome, tx.Commit(ctx)
}

// TerminationFailed retains ENDING and bounded retry time, rather than certifying
// provider termination. No vendor body/credential is persisted in the reason.
func (s *LifecycleStore) TerminationFailed(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tag, e := s.pool.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET retry_at=clock_timestamp()+interval '30 seconds',last_reason='provider_termination_unavailable' WHERE id=$1 AND state IN ('ENDING','SUSPENDED')", s.schema), id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func rollbackBounded(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

// BeginProvision returns a persisted correlation identity before a network call.
// Once PROVISIONING, callers must reconcile by this identity; never repeat create
// blindly after a timeout or a crash. Vendor correlation support is a release gate.
func (s *LifecycleStore) BeginProvision(ctx context.Context, id string) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	o, e := readOperation(s.pool.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state='PROVISIONING',revision=revision+1 WHERE id=$1 AND state='REQUESTED' RETURNING "+opColumns, s.schema), id))
	if errors.Is(e, pgx.ErrNoRows) {
		return Operation{}, ErrConflict
	}
	return o, e
}

type SuspensionDecision struct {
	OperationID   string
	Revision      int64
	Actor, Reason string
	Allowed       bool
	ExpiresAt     time.Time
}

func (s *LifecycleStore) Suspend(ctx context.Context, id string, d SuspensionDecision) (Operation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !d.Allowed || d.OperationID != id || !identityPattern.MatchString(d.Actor) || !identityPattern.MatchString(d.Reason) || !d.ExpiresAt.After(time.Now()) {
		return Operation{}, ErrDenied
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Operation{}, e
	}
	defer rollbackBounded(tx)
	o, e := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", s.schema), id))
	if e != nil {
		return o, e
	}
	if !d.ExpiresAt.After(time.Now()) {
		return Operation{}, ErrDenied
	}
	if o.Revision != d.Revision || o.State == "ENDED" || o.State == "FAILED" {
		return Operation{}, ErrConflict
	}
	o, e = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state='SUSPENDED',intent='end',revision=revision+1 WHERE id=$1 RETURNING "+opColumns, s.schema), id))
	if e != nil {
		return o, e
	}
	_, e = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision) VALUES($1,$2,$3,$4)", s.schema), id, d.Actor, d.Reason, o.Revision)
	if e != nil {
		return Operation{}, e
	}
	if e = s.enqueue(ctx, tx, o, "suspend"); e != nil {
		return Operation{}, e
	}
	return o, tx.Commit(ctx)
}

func (o Operation) PlaybackAllowed() bool { return o.State == "LIVE" }

// ReconcileTermination runs only after a durable intent. HTTP success is merely
// control-plane acknowledgement: provider evidence is still required for ENDED.
func (s *LifecycleStore) ReconcileTermination(ctx context.Context, id string, provider interface {
	TerminateStream(context.Context, string) error
}) error {
	o, e := s.Get(ctx, id)
	if e != nil {
		return e
	}
	if o.State != "ENDING" && o.State != "SUSPENDED" {
		return ErrConflict
	}
	if provider == nil || o.ProviderID == "" {
		if e = s.TerminationFailed(ctx, id); e != nil {
			return e
		}
		return ErrUnavailable
	}
	if e = provider.TerminateStream(ctx, o.ProviderID); e != nil {
		var missing *livepeer.ProviderError
		if errors.As(e, &missing) && missing.Status == 404 {
			return nil
		}
		retryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if persistErr := s.TerminationFailed(retryCtx, id); persistErr != nil {
			return persistErr
		}
		return ErrUnavailable
	}
	return nil
}

func (s *LifecycleStore) enqueue(ctx context.Context, tx pgx.Tx, o Operation, kind string) error {
	payload, err := json.Marshal(map[string]string{"operationId": o.ID, "streamId": o.ID, "state": o.State, "revision": fmt.Sprint(o.Revision)})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_outbox(operation_id,revision,kind,payload) VALUES($1,$2,$3,$4::jsonb)", s.schema), o.ID, o.Revision, kind, string(payload))
	return err
}

func (s *LifecycleStore) List(ctx context.Context, pool string, liveOnly bool) ([]Operation, error) {
	if pool != "" && !addressPattern.MatchString(pool) {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE ($1='' OR pool=$1) AND (NOT $2 OR state='LIVE') ORDER BY created_at DESC,id DESC LIMIT 100", s.schema), strings.ToLower(pool), liveOnly)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Operation{}
	for rows.Next() {
		o, err := readOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func (s *LifecycleStore) SuspendAndRevoke(ctx context.Context, id string, d SuspensionDecision, provider interface {
	DeleteStream(context.Context, string) error
}) (Operation, error) {
	o, err := s.Suspend(ctx, id, d)
	if err != nil {
		return Operation{}, err
	}
	if provider == nil || o.ProviderID == "" {
		if err = s.TerminationFailed(ctx, id); err != nil {
			return o, err
		}
		return o, ErrUnavailable
	}
	if err = provider.DeleteStream(ctx, o.ProviderID); err != nil {
		retryCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if persistErr := s.TerminationFailed(retryCtx, id); persistErr != nil {
			return o, persistErr
		}
		return o, ErrUnavailable
	}
	return o, nil
}

func (s *LifecycleStore) ExpireTerminalKeys(ctx context.Context) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(bounded)
	if err != nil {
		return 0, err
	}
	defer rollbackBounded(tx)
	rows, err := tx.Query(bounded, fmt.Sprintf("SELECT id,revision FROM %s.stream_operations WHERE state IN('ENDED','FAILED') AND terminal_at<=clock_timestamp()-interval '24 hours' AND idem_key<>('expired_'||id) AND EXISTS(SELECT 1 FROM %s.stream_audit a WHERE a.operation_id=stream_operations.id) ORDER BY terminal_at,id FOR UPDATE SKIP LOCKED LIMIT 100", s.schema, s.schema))
	if err != nil {
		return 0, err
	}
	type expired struct {
		id       string
		revision int64
	}
	selected := []expired{}
	for rows.Next() {
		var o expired
		if err = rows.Scan(&o.id, &o.revision); err != nil {
			rows.Close()
			return 0, err
		}
		selected = append(selected, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, o := range selected {
		if _, err = tx.Exec(bounded, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'retention_worker','terminal_idempotency_key_expired',$2)", s.schema), o.id, o.revision); err != nil {
			return 0, err
		}
		if _, err = tx.Exec(bounded, fmt.Sprintf("UPDATE %s.stream_operations SET idem_key='expired_'||id WHERE id=$1", s.schema), o.id); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(bounded); err != nil {
		return 0, err
	}
	return int64(len(selected)), nil
}
