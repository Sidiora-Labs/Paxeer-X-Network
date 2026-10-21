package holdings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"regexp"
)

// SchemaSQL is a proposed serial migration. Runtime constructors never execute
// DDL. The explicit schema argument permits isolated replay and fixture stores.
func SchemaSQL(schema string) (string, error) {
	if !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schema) {
		return "", errors.New("invalid schema")
	}
	q := pgx.Identifier{schema}.Sanitize()
	return fmt.Sprintf(`
CREATE SCHEMA IF NOT EXISTS %[1]s;
CREATE TABLE %[1]s.tokens(token text PRIMARY KEY, decimals smallint NOT NULL CHECK(decimals BETWEEN 0 AND 255), creation_height numeric(78,0) NOT NULL CHECK(creation_height>=0), coverage text NOT NULL CHECK(coverage IN ('complete','incomplete','quarantined')), supply numeric(78,0) NOT NULL DEFAULT 0 CHECK(supply>=0 AND supply<=115792089237316195423570985008687907853269984665640564039457584007913129639935), expected_initial_supply numeric(78,0) CHECK(expected_initial_supply>=0 AND expected_initial_supply<=115792089237316195423570985008687907853269984665640564039457584007913129639935), creation_transaction_hash text, revision bigint NOT NULL DEFAULT 0, first_event jsonb, last_event jsonb);
CREATE TABLE %[1]s.transfers(identity text PRIMARY KEY,token text NOT NULL REFERENCES %[1]s.tokens(token),digest text NOT NULL,event jsonb NOT NULL,revision bigint NOT NULL);
CREATE TABLE %[1]s.balances(token text NOT NULL REFERENCES %[1]s.tokens(token),address text NOT NULL,balance numeric(78,0) NOT NULL CHECK(balance>0 AND balance<=115792089237316195423570985008687907853269984665640564039457584007913129639935),category text NOT NULL DEFAULT 'unknown' CHECK(category IN ('unknown','external_wallet','pool_reserve','burn','treasury','router')),classification_version bigint NOT NULL DEFAULT 0,PRIMARY KEY(token,address));
CREATE INDEX ON %[1]s.balances(token,balance DESC,address ASC);
CREATE INDEX ON %[1]s.balances(token,category,balance DESC,address ASC);
CREATE TABLE %[1]s.incidents(identity text PRIMARY KEY,token text NOT NULL,event jsonb NOT NULL,reason text NOT NULL);
CREATE TABLE %[1]s.legacy_rows(pool text NOT NULL,address text NOT NULL,raw text NOT NULL,normalized_address text,numeric_balance numeric(78,0),reason text NOT NULL,PRIMARY KEY(pool,address));
CREATE TABLE %[1]s.classifications(token text NOT NULL REFERENCES %[1]s.tokens,address text NOT NULL,category text NOT NULL CHECK(category IN ('unknown','external_wallet','pool_reserve','burn','treasury','router')),version bigint NOT NULL CHECK(version>0),evidence text NOT NULL,PRIMARY KEY(token,address));
CREATE TABLE %[1]s.aggregates(token text NOT NULL REFERENCES %[1]s.tokens,category text NOT NULL,band smallint NOT NULL CHECK(band BETWEEN 0 AND 9),population bigint NOT NULL CHECK(population>=0),raw numeric(78,0) NOT NULL CHECK(raw>=0),PRIMARY KEY(token,category,band));
CREATE TABLE %[1]s.provenance(identity text PRIMARY KEY REFERENCES %[1]s.transfers,payload jsonb NOT NULL);
CREATE TABLE %[1]s.snapshots(snapshot_id uuid PRIMARY KEY,token text NOT NULL,pool text NOT NULL,metadata jsonb NOT NULL,expires_at timestamptz NOT NULL);
CREATE INDEX ON %[1]s.snapshots(expires_at);
CREATE TABLE %[1]s.snapshot_balances(snapshot_id uuid NOT NULL REFERENCES %[1]s.snapshots ON DELETE CASCADE,address text NOT NULL,balance numeric(78,0) NOT NULL CHECK(balance>0),category text NOT NULL,PRIMARY KEY(snapshot_id,address));
CREATE INDEX ON %[1]s.snapshot_balances(snapshot_id,balance DESC,address ASC);
CREATE TABLE %[1]s.checkpoints(token text NOT NULL,projection_version text NOT NULL,height numeric(78,0) NOT NULL,revision bigint NOT NULL,coverage_start numeric(78,0) NOT NULL,digest text NOT NULL,position jsonb NOT NULL,PRIMARY KEY(token,projection_version,height));
`, q), nil
}

type Repository struct {
	pool     *pgxpool.Pool
	schema   string
	observer *Observer
}

func NewRepository(pool *pgxpool.Pool, schema string) (*Repository, error) {
	if pool == nil {
		return nil, errors.New("holdings: nil PostgreSQL pool")
	}
	if _, err := SchemaSQL(schema); err != nil {
		return nil, err
	}
	observer, err := NewObserver(nil)
	if err != nil {
		return nil, err
	}
	return &Repository{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), observer: observer}, nil
}

// Discover records verified token metadata and the available creation range.
// Existing discoveries cannot silently overwrite supply or coverage.
func (r *Repository) Discover(ctx context.Context, token string, decimals uint8, creationHeight string, genesisAvailable bool) error {
	token, err := Address(token)
	if err != nil || token == Zero {
		return errors.New("invalid token")
	}
	if _, err = UInt(creationHeight); err != nil {
		return err
	}
	coverage := "incomplete"
	if genesisAvailable {
		coverage = "complete"
	}
	tag, err := r.pool.Exec(ctx, "INSERT INTO "+r.schema+".tokens(token,decimals,creation_height,coverage) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", token, int(decimals), creationHeight, coverage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var d int
		var h string
		err = r.pool.QueryRow(ctx, "SELECT decimals,creation_height::text FROM "+r.schema+".tokens WHERE token=$1", token).Scan(&d, &h)
		if err == nil && (d != int(decimals) || h != creationHeight) {
			return ErrConflict
		}
	}
	return err
}

type ApplyResult struct {
	Duplicate bool
	Revision  int64
	Coverage  string
	Incident  bool
}

func (r *Repository) Apply(ctx context.Context, input Transfer) (result ApplyResult, err error) {
	defer func() {
		if err != nil && !(result.Incident && errors.Is(err, ErrIntegrity)) {
			r.observeFailure(ctx, input, err)
		}
	}()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	result, err = r.ApplyTx(ctx, tx, input)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	r.observer.Committed(ctx, input, result)
	if result.Incident {
		return result, ErrIntegrity
	}
	return result, nil
}
func (r *Repository) ApplyTx(ctx context.Context, tx pgx.Tx, input Transfer) (result ApplyResult, err error) {
	if tx == nil {
		return result, errors.New("holdings: transaction required")
	}
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer savepoint.Rollback(context.WithoutCancel(ctx))
	result, err = r.applyTx(ctx, savepoint, input)
	if err != nil {
		return result, err
	}
	return result, savepoint.Commit(ctx)
}
func (r *Repository) applyTx(ctx context.Context, tx pgx.Tx, input Transfer) (result ApplyResult, err error) {
	if tx == nil {
		return result, errors.New("holdings: transaction required")
	}
	e, err := input.Normalize()
	if err != nil {
		return result, err
	}

	var supply, creation string
	var previous []byte
	err = tx.QueryRow(ctx, "SELECT supply::text,creation_height::text,revision,coverage,last_event FROM "+r.schema+".tokens WHERE token=$1 FOR UPDATE", e.Token).Scan(&supply, &creation, &result.Revision, &result.Coverage, &previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrUnknownToken
	}
	if err != nil {
		return result, err
	}
	var digest string
	var eventRevision int64
	err = tx.QueryRow(ctx, "SELECT digest,revision FROM "+r.schema+".transfers WHERE identity=$1", e.Identity()).Scan(&digest, &eventRevision)
	if err == nil {
		if digest != e.Digest() {
			return result, ErrConflict
		}
		result.Duplicate = true
		result.Revision = eventRevision
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if result.Coverage == "quarantined" {
		result.Incident = true
		return result, nil
	}
	height, _ := UInt(e.Height)
	start, _ := UInt(creation)
	if height.Cmp(start) < 0 {
		return result, ErrOrder
	}
	if len(previous) > 0 {
		var p Transfer
		if err = json.Unmarshal(previous, &p); err != nil {
			return result, err
		}
		if comparePosition(e, p) <= 0 {
			return result, ErrOrder
		}
		if e.Height == p.Height && e.BlockHash != p.BlockHash {
			return result, ErrConflict
		}
	}
	read := func(address string) (string, error) {
		if address == Zero {
			return "0", nil
		}
		var v string
		er := tx.QueryRow(ctx, "SELECT balance::text FROM "+r.schema+".balances WHERE token=$1 AND address=$2", e.Token, address).Scan(&v)
		if errors.Is(er, pgx.ErrNoRows) {
			return "0", nil
		}
		return v, er
	}
	from, err := read(e.From)
	if err != nil {
		return result, err
	}
	to, err := read(e.To)
	if err != nil {
		return result, err
	}
	ns, nf, nt, err := Mutation(e, supply, from, to)
	raw, _ := json.Marshal(e)
	if errors.Is(err, ErrIntegrity) {
		_, er := tx.Exec(ctx, "INSERT INTO "+r.schema+".incidents(identity,token,event,reason) VALUES($1,$2,$3,'underflow_or_overflow') ON CONFLICT DO NOTHING", e.Identity(), e.Token, raw)
		if er != nil {
			return result, er
		}
		_, er = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET coverage='quarantined' WHERE token=$1", e.Token)
		if er != nil {
			return result, er
		}
		result.Coverage = "quarantined"
		result.Incident = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	write := func(address, value string) error {
		if address == Zero {
			return nil
		}
		if err := r.balanceDeltaTx(ctx, tx, e.Token, address, value); err != nil {
			return err
		}
		if value == "0" {
			_, er := tx.Exec(ctx, "DELETE FROM "+r.schema+".balances WHERE token=$1 AND address=$2", e.Token, address)
			return er
		}
		_, er := tx.Exec(ctx, "INSERT INTO "+r.schema+".balances(token,address,balance,category,classification_version) VALUES($1,$2,$3,COALESCE((SELECT category FROM "+r.schema+".classifications WHERE token=$1 AND address=$2),'unknown'),COALESCE((SELECT version FROM "+r.schema+".classifications WHERE token=$1 AND address=$2),0)) ON CONFLICT(token,address) DO UPDATE SET balance=excluded.balance", e.Token, address, value)
		return er
	}
	if e.From != e.To {
		if err = write(e.From, nf); err != nil {
			return result, err
		}
		if err = write(e.To, nt); err != nil {
			return result, err
		}
	}
	result.Revision++
	if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".transfers(identity,token,digest,event,revision) VALUES($1,$2,$3,$4,$5)", e.Identity(), e.Token, e.Digest(), raw, result.Revision); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET supply=$2,revision=$3,first_event=COALESCE(first_event,$4),last_event=$4 WHERE token=$1", e.Token, ns, result.Revision, raw); err != nil {
		return result, err
	}
	if err = r.conserveTx(ctx, tx, e.Token, ns); err != nil {
		return result, err
	}
	provenance, _ := RecipientProvenance(e)
	encoded, _ := json.Marshal(provenance)
	if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".provenance(identity,payload) VALUES($1,$2)", e.Identity(), encoded); err != nil {
		return result, err
	}
	return result, nil
}

type Snapshot struct {
	Token         string    `json:"token"`
	Decimals      int       `json:"decimals"`
	Supply        string    `json:"supply"`
	Coverage      string    `json:"coverage"`
	Revision      int64     `json:"revision"`
	Position      *Transfer `json:"position"`
	StartPosition *Transfer `json:"fromPosition"`
	Balances      []Balance `json:"balances"`
}

// Snapshot is bounded and consistent. This primitive intentionally does not
// advertise offset pagination as a stable multi-request snapshot protocol.
func (r *Repository) Snapshot(ctx context.Context, token string, limit int) (s Snapshot, err error) {
	if limit < 1 || limit > 100 {
		return s, errors.New("limit must be 1..100")
	}
	token, err = Address(token)
	if err != nil {
		return s, err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return s, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	s.Token = token
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT decimals,supply::text,coverage,revision,last_event FROM "+r.schema+".tokens WHERE token=$1", token).Scan(&s.Decimals, &s.Supply, &s.Coverage, &s.Revision, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, ErrUnknownToken
	}
	if err != nil {
		return s, err
	}
	if len(raw) > 0 {
		s.Position = &Transfer{}
		if err = json.Unmarshal(raw, s.Position); err != nil {
			return s, err
		}
	}
	rows, err := tx.Query(ctx, "SELECT address,balance::text,category FROM "+r.schema+".balances WHERE token=$1 ORDER BY balance DESC,address ASC LIMIT $2", token, limit)
	if err != nil {
		return s, err
	}
	s.Balances = []Balance{}
	for rows.Next() {
		var b Balance
		if err = rows.Scan(&b.Address, &b.Raw, &b.Category); err != nil {
			rows.Close()
			return s, err
		}
		s.Balances = append(s.Balances, b)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return s, err
	}
	return s, tx.Commit(ctx)
}
