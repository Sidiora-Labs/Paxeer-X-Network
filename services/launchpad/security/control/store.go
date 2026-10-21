// Package control implements private control-plane primitives. It neither signs
// wallet transactions nor exposes HTTP routes. Identity is resolved afresh for
// each mutation by the trusted D11 boundary; callers cannot supply roles.
package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Fault string

func (e Fault) Error() string { return string(e) }

const (
	Invalid         Fault = "invalid_input"
	Denied          Fault = "scope_denied"
	Conflict        Fault = "revision_conflict"
	Missing         Fault = "not_found"
	Unavailable     Fault = "authority_unavailable"
	FundingRequired Fault = "funding_required"
	BudgetExceeded  Fault = "budget_exceeded"
)

type Principal struct {
	ID, Role                string
	CredentialID, SessionID string
	Scopes                  []string
}

// Identity must validate active session/service identity, scopes and revocation.
// It is not a user-controlled principal decoder. No signing keys enter this API.
type Identity interface {
	Resolve(context.Context, string) (Principal, error)
	Active(context.Context, Principal) bool
}
type Store struct {
	pool     *pgxpool.Pool
	schema   string
	identity Identity
	verifier EvidenceVerifier
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,159}$`)

func key(s string) bool { return keyPattern.MatchString(s) }
func text(s string, n int) bool {
	return utf8.ValidString(s) && len(s) > 0 && len(s) <= n && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}
func id() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func New(pool *pgxpool.Pool, schema string, identity Identity, verifier EvidenceVerifier) (*Store, error) {
	if pool == nil || !identifier.MatchString(schema) || identity == nil {
		return nil, Invalid
	}
	return &Store{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), identity: identity, verifier: verifier}, nil
}

// Install is migration-only. Runtime DB roles must not own this schema, table or
// trigger function. Production registration is reserved to the serial integrator.
func (s *Store) Install(ctx context.Context) error {
	q := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s;
 CREATE TABLE IF NOT EXISTS %s.objects(kind text NOT NULL,id text NOT NULL,body jsonb NOT NULL,PRIMARY KEY(kind,id));
 CREATE TABLE IF NOT EXISTS %s.audit(seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,at timestamptz NOT NULL DEFAULT clock_timestamp(),actor text NOT NULL,role text NOT NULL,operation text NOT NULL,resource text NOT NULL,reason text NOT NULL,result text NOT NULL);
 CREATE FUNCTION %s.immutable_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit is append only'; END $$;
 CREATE TRIGGER immutable_audit BEFORE UPDATE OR DELETE OR TRUNCATE ON %s.audit FOR EACH STATEMENT EXECUTE FUNCTION %s.immutable_audit();`, s.schema, s.schema, s.schema, s.schema, s.schema, s.schema)
	_, e := s.pool.Exec(ctx, q)
	return e
}
func permission(p Principal, op, scope string) bool {
	allowed := map[string]map[string]bool{
		"infrastructure": {"action": true, "caps": true, "incident": true, "infrastructure_read": true},
		"finance":        {"inventory": true, "bounty": true, "settlement": true, "finance_read": true},
		"moderator":      {"moderation_read": true}, "support": {"support_read": true},
		"user": {"automation": true},
	}
	if !key(p.ID) || !allowed[p.Role][op] {
		return false
	}
	for _, v := range p.Scopes {
		if v == scope {
			return true
		}
	}
	return false
}

type transaction struct {
	pgx.Tx
	store *Store
	now   time.Time
	actor Principal
}

func (t *transaction) get(kind, k string, v any) error {
	var b []byte
	e := t.QueryRow(contextFor(t), "SELECT body FROM "+t.store.schema+".objects WHERE kind=$1 AND id=$2", kind, k).Scan(&b)
	if errors.Is(e, pgx.ErrNoRows) {
		return Missing
	}
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}

// tx contexts are held explicitly so all DB operations share the caller deadline.
type contextualTx struct {
	pgx.Tx
	ctx context.Context
}

func contextFor(t *transaction) context.Context { return t.Tx.(*contextualTx).ctx }
func (t *transaction) put(kind, k string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if len(b) > 65536 {
		return Invalid
	}
	_, e = t.Exec(contextFor(t), "INSERT INTO "+t.store.schema+".objects(kind,id,body) VALUES($1,$2,$3) ON CONFLICT(kind,id) DO UPDATE SET body=EXCLUDED.body", kind, k, b)
	return e
}
func (t *transaction) audit(op, scope, reason, result string) error {
	_, e := t.Exec(contextFor(t), "INSERT INTO "+t.store.schema+".audit(actor,role,operation,resource,reason,result) VALUES($1,$2,$3,$4,$5,$6)", t.actor.ID, t.actor.Role, op, scope, reason, result)
	return e
}

// A single advisory lock serializes low-volume control mutations across processes.
// It deliberately favors clear invariants; this is not a DAU throughput claim.
// Domain failures roll back their writes but commit an append-only denial record.
func (s *Store) run(ctx context.Context, credential, op, scope, reason string, fn func(*transaction) error) error {
	if !key(scope) || !text(reason, 1000) || len(credential) == 0 || len(credential) > 4096 {
		return Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, e := s.identity.Resolve(ctx, credential)
	if e != nil {
		return Denied
	}
	if !key(p.ID) || !text(p.Role, 40) {
		return Denied
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer func() { c, x := context.WithTimeout(context.Background(), time.Second); defer x(); _ = tx.Rollback(c) }()
	_, e = tx.Exec(ctx, "SET LOCAL lock_timeout='2s'; SET LOCAL statement_timeout='4s'")
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", s.schema)
	if e != nil {
		return e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	// Revalidate after contended lock acquisition, before any authority use.
	p, e = s.identity.Resolve(ctx, credential)
	if e != nil || !key(p.ID) || !text(p.Role, 40) {
		return Denied
	}
	t := &transaction{Tx: &contextualTx{Tx: tx, ctx: ctx}, store: s, now: now, actor: p}
	// Quarantine is checked under the same lock as every privileged mutation.
	var revoked bool
	var marker map[string]any
	if z := t.get("quarantine", p.ID, &marker); z == nil {
		revoked = true
	} else if z != Missing {
		return z
	}
	if !permission(p, op, scope) || revoked {
		if e = t.audit(op, scope, reason, string(Denied)); e != nil {
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
		return Denied
	}
	if _, e = tx.Exec(ctx, "SAVEPOINT mutation"); e != nil {
		return e
	}
	e = fn(t)
	// Read-only verification adapters may block. Recheck current identity after
	// they return; revoked/changed actors cannot commit the pending mutation.
	if e == nil {
		current, authErr := s.identity.Resolve(ctx, credential)
		if authErr != nil || current.ID != t.actor.ID || !permission(current, op, scope) || !s.identity.Active(ctx, t.actor) {
			e = Denied
		}
	}
	result := "ok"
	if e != nil {
		var fault Fault
		if !errors.As(e, &fault) {
			return e
		}
		if _, x := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT mutation"); x != nil {
			return x
		}
		result = string(fault)
	}
	if x := t.audit(op, scope, reason, result); x != nil {
		return x
	}
	if x := tx.Commit(ctx); x != nil {
		return x
	}
	return e
}
func (t *transaction) approvalActive(a Action) bool {
	if a.ApprovedBy == "" || a.ApprovedBy == a.RequestedBy || a.Approver.ID != a.ApprovedBy || !permission(a.Approver, "action", a.Scope) || !t.store.identity.Active(contextFor(t), a.Approver) {
		return false
	}
	var marker json.RawMessage
	return t.get("quarantine", a.ApprovedBy, &marker) == Missing
}

func (s *Store) Read(ctx context.Context, credential, scope, reason, kind, k string, out any) error {
	if !key(k) {
		return Invalid
	}
	output := reflect.ValueOf(out)
	if !output.IsValid() || output.Kind() != reflect.Pointer || output.IsNil() {
		return Invalid
	}
	accepted := reflect.New(output.Elem().Type())
	op := map[string]string{"action": "infrastructure_read", "caps": "infrastructure_read", "incident": "infrastructure_read", "inventory": "finance_read", "bounty": "finance_read", "settlement": "finance_read"}[kind]
	if op == "" {
		return Denied
	}
	err := s.run(ctx, credential, op, scope, reason, func(t *transaction) error {
		if (kind == "inventory" || kind == "caps") && scope != "platform" {
			return Denied
		}
		var raw json.RawMessage
		if e := t.get(kind, k, &raw); e != nil {
			return e
		}
		if kind != "inventory" && kind != "caps" {
			var resource struct{ Scope string }
			if e := json.Unmarshal(raw, &resource); e != nil {
				return e
			}
			if resource.Scope != scope {
				return Denied
			}
		}
		return json.Unmarshal(raw, accepted.Interface())
	})
	if err != nil {
		return err
	}
	output.Elem().Set(accepted.Elem())
	return nil
}
