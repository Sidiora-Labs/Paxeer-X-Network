package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
)

var (
	ErrFenced    = errors.New("ingestion lease fenced or expired")
	ErrWatermark = errors.New("unexpected ingestion watermark")
	ErrIntegrity = errors.New("immutable ingestion payload conflict")
)
var hashPattern = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

type RawRecord struct {
	TransactionHash, BlockHash, Emitter string
	Height                              int64
	TransactionIndex, LogIndex          uint32
	Topics                              []string
	Data                                []byte
}
type BlockCompleteness struct {
	Height                                int64
	Hash                                  string
	RawCount                              int
	FetchComplete, RequiredDecodeComplete bool
}
type OutboxIntent struct {
	TransactionHash string
	LogIndex        uint32
	Kind            string
	Payload         json.RawMessage
}
type CanonicalBatch struct {
	ChainID                  int32
	StreamID, Owner, BatchID string
	Fence, PreviousHeight    int64
	Blocks                   []BlockCompleteness
	Records                  []RawRecord
	Outbox                   []OutboxIntent
}
type CanonicalTx struct {
	Tx    pgx.Tx
	Store *Store
}

type beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

// CommitCanonical commits immutable provenance, existing typed projections,
// completeness, durable delivery intent and both watermarks in one transaction.
// project must use the supplied transaction-bound Store and must not publish.
func (s *Store) CommitCanonical(ctx context.Context, b CanonicalBatch, project func(context.Context, *Store) error) error {
	return s.commitCanonical(ctx, b, func(ctx context.Context, canonical CanonicalTx) ([]OutboxIntent, error) {
		if project != nil {
			if err := project(ctx, canonical.Store); err != nil {
				return nil, err
			}
		}
		return b.Outbox, nil
	})
}

// CommitProjectedCanonical derives durable intents from the typed projection in
// this same transaction. Supplied Outbox, when nonnil, must exactly match the
// derived intent set; an explicit empty set cannot suppress required delivery.
func (s *Store) CommitProjectedCanonical(ctx context.Context, b CanonicalBatch, project func(context.Context, *Store) ([]OutboxIntent, error)) error {
	if project == nil {
		return errors.New("canonical projection is required")
	}
	return s.commitCanonical(ctx, b, func(ctx context.Context, canonical CanonicalTx) ([]OutboxIntent, error) {
		return project(ctx, canonical.Store)
	})
}
func (s *Store) CommitProjectedCanonicalTx(ctx context.Context, b CanonicalBatch, project func(context.Context, CanonicalTx) ([]OutboxIntent, error)) error {
	if project == nil {
		return errors.New("canonical projection is required")
	}
	return s.commitCanonical(ctx, b, project)
}
func (s *Store) commitCanonical(ctx context.Context, b CanonicalBatch, project func(context.Context, CanonicalTx) ([]OutboxIntent, error)) error {
	if err := validateBatch(b); err != nil {
		return err
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("canonical ingest requires transaction-capable DB")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	var fence int64
	err = tx.QueryRow(ctx, `SELECT fence_token FROM indexer.indexer_ingest_leases WHERE chain_id=$1 AND stream_id=$2 AND owner_instance=$3 AND expires_at>clock_timestamp() FOR UPDATE`, b.ChainID, b.StreamID, b.Owner).Scan(&fence)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && fence != b.Fence {
		return ErrFenced
	}
	if err != nil {
		return err
	}
	var raw, typed int64
	err = tx.QueryRow(ctx, `SELECT raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=$1 AND stream_id=$2 FOR UPDATE`, b.ChainID, b.StreamID).Scan(&raw, &typed)
	if err != nil {
		return fmt.Errorf("watermark: %w", err)
	}
	if raw != b.PreviousHeight || typed != b.PreviousHeight {
		return ErrWatermark
	}
	for _, r := range b.Records {
		topicsList := r.Topics
		if topicsList == nil {
			topicsList = []string{}
		}
		topics, _ := json.Marshal(topicsList)
		if r.Data == nil {
			r.Data = []byte{}
		}
		tag, e := tx.Exec(ctx, `INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, b.ChainID, r.TransactionHash, int64(r.LogIndex), r.Height, int64(r.TransactionIndex), r.BlockHash, r.Emitter, topics, r.Data)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			var same bool
			e = tx.QueryRow(ctx, `SELECT height=$4 AND transaction_index=$5 AND block_hash=$6 AND emitter=$7 AND topics=$8::jsonb AND data=$9 FROM indexer.indexer_raw_logs WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3`, b.ChainID, r.TransactionHash, int64(r.LogIndex), r.Height, int64(r.TransactionIndex), r.BlockHash, r.Emitter, topics, r.Data).Scan(&same)
			if e != nil {
				return e
			}
			if !same {
				return ErrIntegrity
			}
		}
	}
	for _, block := range b.Blocks {
		_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete) VALUES($1,$2,$3,$4,$5,true,true)`, b.ChainID, b.StreamID, block.Height, block.Hash, block.RawCount)
		if err != nil {
			return err
		}
	}
	intents, err := project(ctx, CanonicalTx{Tx: tx, Store: NewFromDBTX(tx)})
	if err != nil {
		return err
	}
	if s.lifecycle {
		if err = checkPoisonTx(ctx, tx, b.ChainID, b.StreamID, b.PreviousHeight+1, b.Blocks[len(b.Blocks)-1].Height); err != nil {
			return err
		}
	}
	if b.Outbox != nil && !sameIntents(b.Outbox, intents) {
		return errors.New("outbox intent does not match typed projection")
	}
	b.Outbox = intents
	if err = validateBatch(b); err != nil {
		return err
	}

	for _, o := range b.Outbox {
		tag, e := tx.Exec(ctx, `INSERT INTO indexer.indexer_ingest_outbox(chain_id,transaction_hash,log_index,event_kind,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, b.ChainID, o.TransactionHash, int64(o.LogIndex), o.Kind, o.Payload)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			var same bool
			e = tx.QueryRow(ctx, `SELECT payload=$5::jsonb FROM indexer.indexer_ingest_outbox WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3 AND event_kind=$4`, b.ChainID, o.TransactionHash, int64(o.LogIndex), o.Kind, o.Payload).Scan(&same)
			if e != nil {
				return e
			}
			if !same {
				return ErrIntegrity
			}
		}
	}
	last := b.Blocks[len(b.Blocks)-1]
	// Recheck actual database time at the final write; row lock excludes takeover.
	tag, err := tx.Exec(ctx, `UPDATE indexer.indexer_ingest_watermarks SET raw_complete_height=$4,typed_complete_height=$4 WHERE chain_id=$1 AND stream_id=$2 AND raw_complete_height=$3 AND typed_complete_height=$3 AND EXISTS(SELECT 1 FROM indexer.indexer_ingest_leases WHERE chain_id=$1 AND stream_id=$2 AND fence_token=$5 AND owner_instance=$6 AND expires_at>clock_timestamp())`, b.ChainID, b.StreamID, b.PreviousHeight, last.Height, b.Fence, b.Owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_ingest_commits(chain_id,stream_id,batch_id,previous_height,last_complete_height,block_hash,record_count,outbox_count) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, b.ChainID, b.StreamID, b.BatchID, b.PreviousHeight, last.Height, last.Hash, len(b.Records), len(b.Outbox))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func validateBatch(b CanonicalBatch) error {
	if b.ChainID <= 0 || b.Fence <= 0 || b.PreviousHeight < 0 || b.PreviousHeight > math.MaxInt64-int64(len(b.Blocks)) || len(b.StreamID) == 0 || len(b.StreamID) > 128 || len(b.Owner) == 0 || len(b.Owner) > 128 || len(b.BatchID) == 0 || len(b.BatchID) > 128 || len(b.Blocks) == 0 || len(b.Blocks) > 1000 || len(b.Records) > 100000 || len(b.Outbox) > 100000 {
		return errors.New("invalid canonical batch bounds")
	}
	totalBytes := 0
	blocks := map[int64]string{}
	for _, bl := range b.Blocks {
		blocks[bl.Height] = bl.Hash
	}
	counts := map[int64]int{}
	identities := map[string]bool{}
	for _, r := range b.Records {
		totalBytes += len(r.Data) + len(r.Topics)*66 + 200
		if totalBytes > 32*1024*1024 {
			return errors.New("canonical batch exceeds byte bound")
		}
		if !hashPattern.MatchString(r.TransactionHash) || !hashPattern.MatchString(r.BlockHash) || !addressPattern.MatchString(r.Emitter) || len(r.Topics) > 4 || len(r.Data) > 1048576 {
			return errors.New("invalid raw record")
		}
		for _, t := range r.Topics {
			if !hashPattern.MatchString(t) {
				return errors.New("invalid topic")
			}
		}
		key := fmt.Sprintf("%s:%d", r.TransactionHash, r.LogIndex)
		if identities[key] {
			return errors.New("duplicate batch identity")
		}
		identities[key] = true
		counts[r.Height]++
		if blocks[r.Height] != r.BlockHash {
			return errors.New("raw record outside complete blocks")
		}
	}
	for i, bl := range b.Blocks {
		if bl.Height != b.PreviousHeight+int64(i)+1 || !hashPattern.MatchString(bl.Hash) || !bl.FetchComplete || !bl.RequiredDecodeComplete || bl.RawCount != counts[bl.Height] {
			return errors.New("incomplete or noncontiguous blocks")
		}
	}
	for _, o := range b.Outbox {
		totalBytes += len(o.Payload)
		if totalBytes > 32*1024*1024 {
			return errors.New("canonical batch exceeds byte bound")
		}
		if !identities[fmt.Sprintf("%s:%d", o.TransactionHash, o.LogIndex)] || len(o.Kind) == 0 || len(o.Kind) > 128 || len(o.Payload) > 1048576 || !json.Valid(o.Payload) {
			return errors.New("invalid outbox intent")
		}
	}
	return nil
}

// AcquireLease increments fencing tokens only after expiry or explicit release.
func (s *Store) AcquireLease(ctx context.Context, chain int32, stream, owner string, ttl time.Duration) (int64, error) {
	if chain <= 0 || len(stream) == 0 || len(stream) > 128 || len(owner) == 0 || len(owner) > 128 || ttl < time.Millisecond || ttl > 30*time.Second {
		return 0, errors.New("invalid lease bounds")
	}
	var fence int64
	err := s.db.QueryRow(ctx, `INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES($1,$2,$3,1,clock_timestamp()+$4::bigint*interval '1 millisecond') ON CONFLICT(chain_id,stream_id) DO UPDATE SET owner_instance=EXCLUDED.owner_instance,fence_token=indexer_ingest_leases.fence_token+1,expires_at=clock_timestamp()+$4::bigint*interval '1 millisecond',last_heartbeat_at=clock_timestamp() WHERE indexer_ingest_leases.expires_at<=clock_timestamp() RETURNING fence_token`, chain, stream, owner, ttl.Milliseconds()).Scan(&fence)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrFenced
	}
	return fence, err
}
func (s *Store) ReleaseLease(ctx context.Context, chain int32, stream, owner string, fence int64) error {
	tag, err := s.db.Exec(ctx, `UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp() WHERE chain_id=$1 AND stream_id=$2 AND owner_instance=$3 AND fence_token=$4`, chain, stream, owner, fence)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	return nil
}

// RenewLease cannot revive an expired owner or overwrite a replacement fence.
func (s *Store) RenewLease(ctx context.Context, chain int32, stream, owner string, fence int64, ttl time.Duration) error {
	if ttl < time.Millisecond || ttl > 30*time.Second {
		return errors.New("invalid lease duration")
	}
	tag, err := s.db.Exec(ctx, `UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp()+$5::bigint*interval '1 millisecond',last_heartbeat_at=clock_timestamp() WHERE chain_id=$1 AND stream_id=$2 AND owner_instance=$3 AND fence_token=$4 AND expires_at>clock_timestamp()`, chain, stream, owner, fence, ttl.Milliseconds())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	return nil
}

// InsertCommittedOutboxIntent stages, inside the canonical transaction of
// batch b, a derived accounting.inventory.v1 intent of a raw log committed by
// an earlier canonical batch: the inventory facet of a fill whose publication
// was deferred until pnl issued the wallet's baseline (processor inventory.go).
// The raw log must already be canonical below b; a recorded intent with the
// same identity must be byte-identical.
func (c CanonicalTx) InsertCommittedOutboxIntent(ctx context.Context, b CanonicalBatch, o OutboxIntent) error {
	if c.Tx == nil || o.Kind != accounting.InventorySubject || !hashPattern.MatchString(o.TransactionHash) || len(o.Payload) == 0 || len(o.Payload) > 1048576 || !json.Valid(o.Payload) {
		return errors.New("committed outbox intent requires a bounded inventory intent")
	}
	var height int64
	err := c.Tx.QueryRow(ctx, `SELECT height FROM indexer.indexer_raw_logs WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3`, b.ChainID, o.TransactionHash, int64(o.LogIndex)).Scan(&height)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && height > b.PreviousHeight {
		return ErrIntegrity
	}
	if err != nil {
		return err
	}
	tag, err := c.Tx.Exec(ctx, `INSERT INTO indexer.indexer_ingest_outbox(chain_id,transaction_hash,log_index,event_kind,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, b.ChainID, o.TransactionHash, int64(o.LogIndex), o.Kind, o.Payload)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var same bool
	if err = c.Tx.QueryRow(ctx, `SELECT payload=$5::jsonb FROM indexer.indexer_ingest_outbox WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3 AND event_kind=$4`, b.ChainID, o.TransactionHash, int64(o.LogIndex), o.Kind, o.Payload).Scan(&same); err != nil {
		return err
	}
	if !same {
		return ErrIntegrity
	}
	return nil
}

// reconciledWithDeferredInventory accepts a committed batch whose records
// carry, besides the outboxCount intents the batch committed, only
// accounting.inventory.v1 intents a later canonical batch staged through
// InsertCommittedOutboxIntent. Every other derived intent must match exactly.
func reconciledWithDeferredInventory(expected []OutboxIntent, actual []OutboxIntent, outboxCount int) bool {
	other := 0
	for _, o := range actual {
		if o.Kind != accounting.InventorySubject {
			other++
		}
	}
	if outboxCount < other || outboxCount > len(actual) {
		return false
	}
	if expected == nil {
		return true
	}
	if len(expected) != outboxCount {
		return false
	}
	key := func(o OutboxIntent) string { return fmt.Sprintf("%s:%d:%s", o.TransactionHash, o.LogIndex, o.Kind) }
	committed := map[string]bool{}
	for _, o := range expected {
		committed[key(o)] = true
	}
	kept := make([]OutboxIntent, 0, len(expected))
	for _, o := range actual {
		if committed[key(o)] {
			kept = append(kept, o)
		} else if o.Kind != accounting.InventorySubject {
			return false
		}
	}
	return sameIntents(expected, kept)
}

func sameIntents(expected, actual []OutboxIntent) bool {
	if len(expected) != len(actual) {
		return false
	}
	key := func(o OutboxIntent) string { return fmt.Sprintf("%s:%d:%s", o.TransactionHash, o.LogIndex, o.Kind) }
	items := map[string]json.RawMessage{}
	for _, o := range expected {
		if _, ok := items[key(o)]; ok {
			return false
		}
		items[key(o)] = o.Payload
	}
	for _, o := range actual {
		p, ok := items[key(o)]
		if !ok {
			return false
		}
		var a, b any
		da := json.NewDecoder(bytes.NewReader(p))
		da.UseNumber()
		db := json.NewDecoder(bytes.NewReader(o.Payload))
		db.UseNumber()
		if da.Decode(&a) != nil || db.Decode(&b) != nil {
			return false
		}
		// UseNumber keeps huge exact raw quantity strings/numbers distinguishable.
		canonicalA, _ := json.Marshal(a)
		canonicalB, _ := json.Marshal(b)
		if string(canonicalA) != string(canonicalB) {
			return false
		}
		delete(items, key(o))
	}
	return len(items) == 0
}

func (c CanonicalTx) InsertAccountingFill(ctx context.Context, fill accounting.FillV1) (accounting.Digest, error) {
	if c.Tx == nil {
		return "", errors.New("canonical accounting transaction required")
	}
	digest, err := accounting.FillDigest(fill)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(fill)
	if err != nil || len(payload) > accounting.MaxWireBytes {
		return "", errors.New("accounting fill encoding exceeds bound")
	}
	height, err := strconv.ParseInt(fill.Position.Height, 10, 64)
	if err != nil {
		return "", err
	}
	transactionIndex, err := strconv.ParseUint(fill.Position.TransactionIndex, 10, 32)
	if err != nil {
		return "", err
	}
	logIndex, err := strconv.ParseUint(fill.Position.LogIndex, 10, 32)
	if err != nil {
		return "", err
	}
	var bound bool
	err = c.Tx.QueryRow(ctx, `SELECT height=$4 AND transaction_index=$5 AND block_hash=$6 AND emitter=$7 FROM indexer.indexer_raw_logs WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3`, 125, fill.Position.TransactionHash, int64(logIndex), height, int64(transactionIndex), fill.Position.BlockHash, fill.ActualEmitter).Scan(&bound)
	if err != nil {
		return "", err
	}
	if !bound {
		return "", ErrIntegrity
	}
	tag, err := c.Tx.Exec(ctx, `INSERT INTO indexer.indexer_accounting_fills(source_fill_id,source_fact_digest,chain_id,transaction_hash,log_index,pool_address,payload) VALUES($1,$2,125,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, fill.SourceFillID, string(digest), fill.Position.TransactionHash, int64(logIndex), fill.PoolAddress, payload)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		var same bool
		err = c.Tx.QueryRow(ctx, `SELECT source_fact_digest=$2 AND chain_id=125 AND transaction_hash=$3 AND log_index=$4 AND pool_address=$5 AND payload=$6::jsonb FROM indexer.indexer_accounting_fills WHERE source_fill_id=$1`, fill.SourceFillID, string(digest), fill.Position.TransactionHash, int64(logIndex), fill.PoolAddress, payload).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrIntegrity
		}
		if err != nil {
			return "", err
		}
		if !same {
			return "", ErrIntegrity
		}
	}
	return digest, nil
}

func (s *Store) GetCanonicalWatermark(ctx context.Context, chain int32, stream string) (int64, error) {
	if chain != 125 || stream == "" || len(stream) > 128 {
		return 0, ErrWatermark
	}
	var raw, typed int64
	err := s.db.QueryRow(ctx, `SELECT raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=$1 AND stream_id=$2`, chain, stream).Scan(&raw, &typed)
	if err != nil {
		return 0, err
	}
	if raw < 0 || raw != typed {
		return 0, ErrWatermark
	}
	return typed, nil
}

type canonicalEpochDB struct {
	*pgxpool.Pool
	assert func(context.Context, pgx.Tx) error
}
type canonicalEpochTx struct {
	pgx.Tx
	assert func(context.Context, pgx.Tx) error
}

func NewCanonicalStore(pool *pgxpool.Pool, assert func(context.Context, pgx.Tx) error) (*Store, error) {
	if pool == nil || assert == nil {
		return nil, errors.New("canonical store requires pool and recovery epoch authority")
	}
	return NewFromDBTX(&canonicalEpochDB{Pool: pool, assert: assert}), nil
}
func (s *Store) CanonicalEpochGuarded() bool {
	if s == nil {
		return false
	}
	db, ok := s.db.(*canonicalEpochDB)
	return ok && db != nil && db.Pool != nil && db.assert != nil
}
func (d *canonicalEpochDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return d.BeginTx(ctx, pgx.TxOptions{})
}
func (d *canonicalEpochDB) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	if d == nil || d.Pool == nil || d.assert == nil {
		return nil, errors.New("canonical recovery epoch authority required")
	}
	tx, err := d.Pool.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	if err = d.assert(ctx, tx); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
		return nil, err
	}
	return &canonicalEpochTx{Tx: tx, assert: d.assert}, nil
}
func (t *canonicalEpochTx) Commit(ctx context.Context) error {
	if t == nil || t.Tx == nil || t.assert == nil {
		return errors.New("canonical recovery epoch authority required")
	}
	if err := t.assert(ctx, t.Tx); err != nil {
		return err
	}
	return t.Tx.Commit(ctx)
}

func NewLifecycleStore(pool *pgxpool.Pool, assert func(context.Context, pgx.Tx) error) (*Store, error) {
	s, err := NewCanonicalStore(pool, assert)
	if err != nil {
		return nil, err
	}
	s.lifecycle = true
	return s, nil
}
func (s *Store) LifecycleEnabled() bool { return s != nil && s.lifecycle && s.CanonicalEpochGuarded() }

func (s *Store) IngestReady(ctx context.Context, chain int32, stream, owner string, fence int64) error {
	if s == nil || s.db == nil || chain != 125 || stream == "" || owner == "" || fence <= 0 {
		return ErrFenced
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("readiness requires actual transaction authority")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var ready bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_ingest_leases l JOIN indexer.indexer_ingest_watermarks w USING(chain_id,stream_id) WHERE l.chain_id=$1 AND l.stream_id=$2 AND l.owner_instance=$3 AND l.fence_token=$4 AND l.expires_at>clock_timestamp() AND w.raw_complete_height=w.typed_complete_height)`, chain, stream, owner, fence).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return ErrFenced
	}
	return tx.Commit(ctx)
}
