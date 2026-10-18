package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"regexp"
	"sort"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
)

var ErrStaleOwner = errors.New("stale_pool_owner")
var ErrIntegrity = errors.New("conflicting_canonical_fill")
var schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// ExactSchemaDDL is a migration proposal, not an automatic runtime migration.
func ExactSchemaDDL(schema string) (string, error) {
	if !schemaName.MatchString(schema) {
		return "", errors.New("invalid_schema")
	}
	return fmt.Sprintf(`
CREATE SCHEMA IF NOT EXISTS %[1]s;
CREATE TABLE %[1]s.chart_pool_fences(chain_id text NOT NULL,pool text NOT NULL,epoch bigint NOT NULL CHECK(epoch>0),frozen boolean NOT NULL DEFAULT false,PRIMARY KEY(chain_id,pool));
CREATE TABLE %[1]s.chart_inbox(chain_id text NOT NULL,emitter text NOT NULL,code_epoch text NOT NULL,tx_hash text NOT NULL,log_index numeric(20,0) NOT NULL,pool text NOT NULL,digest bytea NOT NULL,fill jsonb NOT NULL,algorithm text NOT NULL,PRIMARY KEY(chain_id,emitter,code_epoch,tx_hash,log_index));
CREATE TABLE %[1]s.chart_exact(chain_id text NOT NULL,pool text NOT NULL,interval text NOT NULL,start bigint NOT NULL,snapshot jsonb NOT NULL,PRIMARY KEY(chain_id,pool,interval,start));
CREATE TABLE %[1]s.chart_traders(chain_id text NOT NULL,pool text NOT NULL,interval text NOT NULL,start bigint NOT NULL,actor text NOT NULL,PRIMARY KEY(chain_id,pool,interval,start,actor));
CREATE TABLE %[1]s.chart_outbox(id bigserial PRIMARY KEY,chain_id text NOT NULL,pool text NOT NULL,interval text NOT NULL,start bigint NOT NULL,revision numeric(20,0) NOT NULL,payload jsonb NOT NULL,published_at timestamptz,UNIQUE(chain_id,pool,interval,start,revision));
CREATE INDEX chart_outbox_pending ON %[1]s.chart_outbox(id) WHERE published_at IS NULL;
`, schema), nil
}

type ExactStore struct {
	Pool   *pgxpool.Pool
	Schema string
}

func NewExact(pool *pgxpool.Pool, schema string) (*ExactStore, error) {
	if pool == nil || !schemaName.MatchString(schema) {
		return nil, errors.New("invalid_exact_store")
	}
	return &ExactStore{pool, schema}, nil
}

type Claim struct {
	ChainID, Pool, Emitter, CodeEpoch, TxHash, LogIndex string
	Epoch                                               int64
	Digest, Fill                                        []byte
}

// Transaction holds a row lock for the pool across inbox, all folds and outbox.
// A higher epoch advances the durable fence; an old owner can never write after it.
func (s *ExactStore) Transaction(ctx context.Context, c Claim, fold func(pgx.Tx, string) error) error {
	for attempt := 0; attempt < 3; attempt++ {
		err := s.transaction(ctx, c, fold)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || (pgerr.Code != "40001" && pgerr.Code != "40P01") || attempt == 2 {
			return err
		}
		timer := time.NewTimer(time.Duration(5*(attempt+1)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("transaction_retry_exhausted")
}
func (s *ExactStore) transaction(ctx context.Context, c Claim, fold func(pgx.Tx, string) error) error {
	if c.Epoch <= 0 || len(c.Digest) != 32 || len(c.Pool) == 0 || len(c.Pool) > 160 || len(c.Emitter) > 160 || len(c.CodeEpoch) > 64 || len(c.TxHash) == 0 || len(c.TxHash) > 160 || len(c.Fill) > 65536 {
		return errors.New("invalid_claim")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := s.Schema
	// Serialize canonical identity before discovering all affected pools. Then lock
	// pool rows in lexical order, including the original pool on a conflict.
	identity, _ := json.Marshal([]string{c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex})
	_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(identity))
	if e != nil {
		return e
	}
	originalPool := ""
	e = tx.QueryRow(ctx, `SELECT pool FROM `+q+`.chart_inbox WHERE chain_id=$1 AND emitter=$2 AND code_epoch=$3 AND tx_hash=$4 AND log_index=$5`, c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex).Scan(&originalPool)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	pools := []string{c.Pool}
	if originalPool != "" && originalPool != c.Pool {
		pools = append(pools, originalPool)
	}
	sort.Strings(pools)
	for _, pool := range pools {
		_, e = tx.Exec(ctx, `INSERT INTO `+q+`.chart_pool_fences(chain_id,pool,epoch) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, c.ChainID, pool, c.Epoch)
		if e != nil {
			return e
		}
		var locked string
		e = tx.QueryRow(ctx, `SELECT pool FROM `+q+`.chart_pool_fences WHERE chain_id=$1 AND pool=$2 FOR UPDATE`, c.ChainID, pool).Scan(&locked)
		if e != nil {
			return e
		}
	}
	var epoch int64
	var frozen bool
	e = tx.QueryRow(ctx, `SELECT epoch,frozen FROM `+q+`.chart_pool_fences WHERE chain_id=$1 AND pool=$2 FOR UPDATE`, c.ChainID, c.Pool).Scan(&epoch, &frozen)
	if e != nil {
		return e
	}
	if frozen && (originalPool == "" || originalPool == c.Pool) {
		return ErrIntegrity
	}
	if c.Epoch < epoch && (originalPool == "" || originalPool == c.Pool) {
		return ErrStaleOwner
	}
	if c.Epoch > epoch {
		_, e = tx.Exec(ctx, `UPDATE `+q+`.chart_pool_fences SET epoch=$3 WHERE chain_id=$1 AND pool=$2`, c.ChainID, c.Pool, c.Epoch)
		if e != nil {
			return e
		}
	}
	ct, e := tx.Exec(ctx, `INSERT INTO `+q+`.chart_inbox(chain_id,emitter,code_epoch,tx_hash,log_index,pool,digest,fill,algorithm) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'exact-v1') ON CONFLICT DO NOTHING`, c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex, c.Pool, c.Digest, c.Fill)
	if e != nil {
		return e
	}
	if ct.RowsAffected() == 0 {
		var equal bool
		e = tx.QueryRow(ctx, `SELECT digest=$6 AND pool=$7 AND fill=$8::jsonb FROM `+q+`.chart_inbox WHERE chain_id=$1 AND emitter=$2 AND code_epoch=$3 AND tx_hash=$4 AND log_index=$5`, c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex, c.Digest, c.Pool, c.Fill).Scan(&equal)
		if e != nil {
			return e
		}
		if !equal {
			_, e = tx.Exec(ctx, `UPDATE `+q+`.chart_pool_fences SET frozen=true WHERE chain_id=$1 AND (pool=$2 OR pool=$3)`, c.ChainID, c.Pool, originalPool)
			if e != nil {
				return e
			}
			if e = tx.Commit(ctx); e != nil {
				return e
			}
			return ErrIntegrity
		}
		return tx.Commit(ctx)
	}
	if e = fold(tx, q); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s *ExactStore) TransactionTx(ctx context.Context, tx pgx.Tx, c Claim, fold func(pgx.Tx, string) error) error {
	if tx == nil || fold == nil || s == nil {
		return errors.New("transaction and fold required")
	}
	if c.Epoch <= 0 || len(c.Digest) != 32 || len(c.Pool) == 0 || len(c.Pool) > 160 || len(c.Emitter) > 160 || len(c.CodeEpoch) > 64 || len(c.TxHash) == 0 || len(c.TxHash) > 160 || len(c.Fill) > 65536 {
		return errors.New("invalid_claim")
	}
	var e error
	q := s.Schema
	// Serialize canonical identity before discovering all affected pools. Then lock
	// pool rows in lexical order, including the original pool on a conflict.
	identity, _ := json.Marshal([]string{c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex})
	_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(identity))
	if e != nil {
		return e
	}
	originalPool := ""
	e = tx.QueryRow(ctx, `SELECT pool FROM `+q+`.chart_inbox WHERE chain_id=$1 AND emitter=$2 AND code_epoch=$3 AND tx_hash=$4 AND log_index=$5`, c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex).Scan(&originalPool)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	pools := []string{c.Pool}
	if originalPool != "" && originalPool != c.Pool {
		pools = append(pools, originalPool)
	}
	sort.Strings(pools)
	for _, pool := range pools {
		_, e = tx.Exec(ctx, `INSERT INTO `+q+`.chart_pool_fences(chain_id,pool,epoch) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, c.ChainID, pool, c.Epoch)
		if e != nil {
			return e
		}
		var locked string
		e = tx.QueryRow(ctx, `SELECT pool FROM `+q+`.chart_pool_fences WHERE chain_id=$1 AND pool=$2 FOR UPDATE`, c.ChainID, pool).Scan(&locked)
		if e != nil {
			return e
		}
	}
	var epoch int64
	var frozen bool
	e = tx.QueryRow(ctx, `SELECT epoch,frozen FROM `+q+`.chart_pool_fences WHERE chain_id=$1 AND pool=$2 FOR UPDATE`, c.ChainID, c.Pool).Scan(&epoch, &frozen)
	if e != nil {
		return e
	}
	if frozen && (originalPool == "" || originalPool == c.Pool) {
		return &delivery.IntegrityFailure{Class: delivery.IdentityConflict, AffectedPools: []string{c.Pool}, EvidenceDigest: accounting.Digest(delivery.Hash(c.Fill)), Cause: ErrIntegrity}
	}
	if c.Epoch < epoch && (originalPool == "" || originalPool == c.Pool) {
		return ErrStaleOwner
	}
	if c.Epoch > epoch {
		_, e = tx.Exec(ctx, `UPDATE `+q+`.chart_pool_fences SET epoch=$3 WHERE chain_id=$1 AND pool=$2`, c.ChainID, c.Pool, c.Epoch)
		if e != nil {
			return e
		}
	}
	ct, e := tx.Exec(ctx, `INSERT INTO `+q+`.chart_inbox(chain_id,emitter,code_epoch,tx_hash,log_index,pool,digest,fill,algorithm) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'exact-v1') ON CONFLICT DO NOTHING`, c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex, c.Pool, c.Digest, c.Fill)
	if e != nil {
		return e
	}
	if ct.RowsAffected() == 0 {
		var equal bool
		e = tx.QueryRow(ctx, `SELECT digest=$6 AND pool=$7 AND fill=$8::jsonb FROM `+q+`.chart_inbox WHERE chain_id=$1 AND emitter=$2 AND code_epoch=$3 AND tx_hash=$4 AND log_index=$5`, c.ChainID, c.Emitter, c.CodeEpoch, c.TxHash, c.LogIndex, c.Digest, c.Pool, c.Fill).Scan(&equal)
		if e != nil {
			return e
		}
		if !equal {
			return &delivery.IntegrityFailure{Class: delivery.IdentityConflict, AffectedPools: []string{c.Pool, originalPool}, EvidenceDigest: accounting.Digest(delivery.Hash(c.Fill)), Cause: ErrIntegrity}
		}
		return nil
	}
	if e = fold(tx, q); e != nil {
		return e
	}
	return nil
}

func (s *ExactStore) FreezePoolsTx(ctx context.Context, tx pgx.Tx, pools []string, evidence accounting.Digest) error {
	if s == nil || tx == nil || len(pools) == 0 || len(pools) > 128 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(string(evidence)) {
		return errors.New("invalid freeze input")
	}
	ordered := append([]string(nil), pools...)
	sort.Strings(ordered)
	previous := ""
	for _, pool := range ordered {
		if !regexp.MustCompile(`^0x[0-9a-f]{40}$`).MatchString(pool) {
			return errors.New("invalid freeze pool")
		}
		if pool == previous {
			continue
		}
		previous = pool
		if _, err := tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_pool_fences(chain_id,pool,epoch,frozen) VALUES('125',$1,1,true) ON CONFLICT(chain_id,pool) DO UPDATE SET frozen=true`, pool); err != nil {
			return err
		}
	}
	return nil
}

func (s *ExactStore) PublishNextCommitted(ctx context.Context, publisher *natsdriver.Publisher, subject string) (bool, error) {
	if s == nil || publisher == nil || subject != "candle.updated.v2" {
		return false, errors.New("invalid_candle_relay")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		tx.Rollback(cleanup)
	}()
	var id int64
	var chain, pool, interval, revision string
	var start int64
	var payload []byte
	err = tx.QueryRow(ctx, `SELECT id,chain_id,pool,interval,start,revision::text,payload FROM `+s.Schema+`.chart_outbox WHERE published_at IS NULL ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &chain, &pool, &interval, &start, &revision, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	identity := "candle_" + accounting.HashTuple("chart-outbox-v2", chain, pool, interval, strconv.FormatInt(start, 10), revision)
	if _, err = publisher.Publish(ctx, subject, identity, payload); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `UPDATE `+s.Schema+`.chart_outbox SET published_at=clock_timestamp() WHERE id=$1 AND published_at IS NULL`, id)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, ErrIntegrity
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
