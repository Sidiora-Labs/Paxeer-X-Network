package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type IngestStorageSample struct {
	ObservedAt                                                time.Time `json:"observedAt"`
	Database, Role, ServerVersion, WALInsertLSN               string
	DatabaseBytes, RawHeapBytes, RawIndexBytes, PendingOutbox int64
	OldestOutboxAgeMS                                         float64
	RawPartitioned, DateRangePartitionKey                     bool
	RawPartitionKeyDefinition                                 string
	IndexDefinitions                                          []string
}

func (s *Store) MeasureIngestStorage(ctx context.Context, schema string) (IngestStorageSample, error) {
	var sample IngestStorageSample
	if !replayToken.MatchString(schema) {
		return sample, errors.New("explicit delivery schema required")
	}
	err := s.db.QueryRow(ctx, `SELECT clock_timestamp(),current_database(),current_user,current_setting('server_version'),pg_current_wal_insert_lsn()::text,pg_database_size(current_database()),CASE WHEN (SELECT relkind FROM pg_class WHERE oid='indexer.indexer_raw_logs'::regclass)='p' THEN (SELECT COALESCE(sum(pg_relation_size(relid)),0) FROM pg_partition_tree('indexer.indexer_raw_logs'::regclass) WHERE isleaf) ELSE pg_relation_size('indexer.indexer_raw_logs') END,CASE WHEN (SELECT relkind FROM pg_class WHERE oid='indexer.indexer_raw_logs'::regclass)='p' THEN (SELECT COALESCE(sum(pg_indexes_size(relid)),0) FROM pg_partition_tree('indexer.indexer_raw_logs'::regclass) WHERE isleaf) ELSE pg_indexes_size('indexer.indexer_raw_logs') END,(SELECT relkind='p' FROM pg_class WHERE oid='indexer.indexer_raw_logs'::regclass),COALESCE(pg_get_partkeydef('indexer.indexer_raw_logs'::regclass),''),EXISTS(SELECT 1 FROM pg_partitioned_table p JOIN pg_attribute a ON a.attrelid=p.partrelid AND a.attnum=ANY(p.partattrs) WHERE p.partrelid='indexer.indexer_raw_logs'::regclass AND p.partstrat='r' AND p.partnatts=1 AND a.atttypid='date'::regtype)`).Scan(&sample.ObservedAt, &sample.Database, &sample.Role, &sample.ServerVersion, &sample.WALInsertLSN, &sample.DatabaseBytes, &sample.RawHeapBytes, &sample.RawIndexBytes, &sample.RawPartitioned, &sample.RawPartitionKeyDefinition, &sample.DateRangePartitionKey)
	if err != nil {
		return sample, err
	}
	err = s.db.QueryRow(ctx, "SELECT count(*),COALESCE(EXTRACT(epoch FROM clock_timestamp()-min(created_at))*1000,0) FROM "+pgx.Identifier{schema, "delivery_outbox"}.Sanitize()+" WHERE state='pending'").Scan(&sample.PendingOutbox, &sample.OldestOutboxAgeMS)
	if err != nil {
		return sample, err
	}
	rows, err := s.db.Query(ctx, `SELECT pg_get_indexdef(indexrelid) FROM pg_index WHERE indrelid IN('indexer.indexer_raw_logs'::regclass,'indexer.indexer_accounting_fills'::regclass,'indexer.indexer_quarantine'::regclass) ORDER BY indexrelid::regclass::text`)
	if err != nil {
		return sample, err
	}
	defer rows.Close()
	sample.IndexDefinitions = []string{}
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			return sample, err
		}
		sample.IndexDefinitions = append(sample.IndexDefinitions, value)
	}
	return sample, rows.Err()
}
func WALBytesBetween(first, last string) (uint64, error) {
	parse := func(value string) (uint64, error) {
		var high, low string
		for i, ch := range value {
			if ch == '/' {
				high = value[:i]
				low = value[i+1:]
				break
			}
		}
		if high == "" || low == "" {
			return 0, errors.New("actual PostgreSQL LSN required")
		}
		a, err := strconv.ParseUint(high, 16, 32)
		if err != nil {
			return 0, err
		}
		b, err := strconv.ParseUint(low, 16, 32)
		if err != nil {
			return 0, err
		}
		return a<<32 | b, nil
	}
	a, err := parse(first)
	if err != nil {
		return 0, err
	}
	b, err := parse(last)
	if err != nil {
		return 0, err
	}
	if b < a {
		return 0, errors.New("WAL timeline moved backwards; restore boundary must be measured separately")
	}
	return b - a, nil
}

type CanonicalRangeDigest struct {
	SourceStream                                           string
	FromHeight, ToHeight, RawCount, TypedCount, BlockCount int64
	RawSHA256, TypedSHA256, BlockSHA256, ProjectionSHA256  string
	ProjectionCount, AccountingFillCount                   int64
}

func digestPart(h hash.Hash, value []byte) { fmt.Fprintf(h, "%d:", len(value)); h.Write(value) }
func canonicalPayload(body []byte) ([]byte, error) {
	var value any
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (s *Store) MeasureCanonicalRange(ctx context.Context, stream string, from, to int64) (CanonicalRangeDigest, error) {
	result := CanonicalRangeDigest{SourceStream: stream, FromHeight: from, ToHeight: to}
	if !replayToken.MatchString(stream) || from < 0 || to < from || to-from >= 1000000 {
		return result, errors.New("bounded actual canonical range required")
	}
	db, ok := s.db.(interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	})
	if !ok {
		return result, errors.New("canonical measurement requires snapshot transaction")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	blocks, err := tx.Query(ctx, `SELECT height,block_hash,raw_count,fetch_complete,required_decode_complete FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height BETWEEN $2 AND $3 ORDER BY height`, stream, from, to)
	if err != nil {
		return result, err
	}
	blockHash := sha256.New()
	expectedRaw := int64(0)
	for blocks.Next() {
		var b BlockCompleteness
		if err = blocks.Scan(&b.Height, &b.Hash, &b.RawCount, &b.FetchComplete, &b.RequiredDecodeComplete); err != nil {
			blocks.Close()
			return result, err
		}
		if b.Height != from+result.BlockCount || !b.FetchComplete || !b.RequiredDecodeComplete {
			blocks.Close()
			return result, ErrIntegrity
		}
		body, _ := json.Marshal(b)
		digestPart(blockHash, body)
		expectedRaw += int64(b.RawCount)
		result.BlockCount++
	}
	blocks.Close()
	if err = blocks.Err(); err != nil {
		return result, err
	}
	if result.BlockCount != to-from+1 {
		return result, ErrWatermark
	}
	rawRows, err := tx.Query(ctx, `SELECT transaction_hash,block_hash,emitter,height,transaction_index,log_index,topics,data FROM indexer.indexer_raw_logs WHERE chain_id=125 AND height BETWEEN $1 AND $2 ORDER BY height,transaction_index,log_index,transaction_hash`, from, to)
	if err != nil {
		return result, err
	}
	rawHash := sha256.New()
	for rawRows.Next() {
		var raw RawRecord
		var topics []byte
		if err = rawRows.Scan(&raw.TransactionHash, &raw.BlockHash, &raw.Emitter, &raw.Height, &raw.TransactionIndex, &raw.LogIndex, &topics, &raw.Data); err != nil {
			rawRows.Close()
			return result, err
		}
		if err = json.Unmarshal(topics, &raw.Topics); err != nil {
			rawRows.Close()
			return result, err
		}
		digest, err := RawDigest(raw)
		if err != nil {
			rawRows.Close()
			return result, err
		}
		digestPart(rawHash, []byte(digest))
		result.RawCount++
	}
	rawRows.Close()
	if err = rawRows.Err(); err != nil {
		return result, err
	}
	if result.RawCount != expectedRaw {
		return result, ErrIntegrity
	}
	typedRows, err := tx.Query(ctx, `SELECT r.transaction_hash,r.log_index,o.event_kind,o.payload FROM indexer.indexer_raw_logs r JOIN indexer.indexer_ingest_outbox o USING(chain_id,transaction_hash,log_index) WHERE r.chain_id=125 AND r.height BETWEEN $1 AND $2 AND o.event_kind<>'MarketCreated' ORDER BY r.height,r.transaction_index,r.log_index,r.transaction_hash,o.event_kind`, from, to)
	if err != nil {
		return result, err
	}
	typedHash := sha256.New()
	for typedRows.Next() {
		var txHash, kind string
		var index uint32
		var body []byte
		if err = typedRows.Scan(&txHash, &index, &kind, &body); err != nil {
			typedRows.Close()
			return result, err
		}
		body, err = canonicalPayload(body)
		if err != nil {
			typedRows.Close()
			return result, err
		}
		digestPart(typedHash, []byte(fmt.Sprintf("%s:%d:%s", txHash, index, kind)))
		digestPart(typedHash, body)
		result.TypedCount++
	}
	typedRows.Close()
	if err = typedRows.Err(); err != nil {
		return result, err
	}
	projectionRows, err := tx.Query(ctx, `SELECT kind,transaction_hash,log_index,payload FROM (SELECT 'accounting-fill'::text AS kind,f.transaction_hash,f.log_index,f.payload,r.height,r.transaction_index FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs r USING(chain_id,transaction_hash,log_index) WHERE r.chain_id=125 AND r.height BETWEEN $1 AND $2 UNION ALL SELECT 'legacy-swap'::text,s.tx_hash,s.log_index::bigint,to_jsonb(s),r.height,r.transaction_index FROM indexer.swaps s JOIN indexer.indexer_raw_logs r ON r.chain_id=125 AND r.transaction_hash=s.tx_hash AND r.log_index=s.log_index WHERE r.height BETWEEN $1 AND $2) facts ORDER BY height,transaction_index,log_index,transaction_hash,kind`, from, to)
	if err != nil {
		return result, err
	}
	projectionHash := sha256.New()
	for projectionRows.Next() {
		var kind, identity string
		var index int64
		var payload []byte
		if err = projectionRows.Scan(&kind, &identity, &index, &payload); err != nil {
			projectionRows.Close()
			return result, err
		}
		payload, err = canonicalPayload(payload)
		if err != nil {
			projectionRows.Close()
			return result, err
		}
		digestPart(projectionHash, []byte(fmt.Sprintf("%s:%s:%d", kind, identity, index)))
		digestPart(projectionHash, payload)
		result.ProjectionCount++
		if kind == "accounting-fill" {
			result.AccountingFillCount++
		}
	}
	projectionRows.Close()
	if err = projectionRows.Err(); err != nil {
		return result, err
	}
	result.ProjectionSHA256 = hex.EncodeToString(projectionHash.Sum(nil))
	result.BlockSHA256 = hex.EncodeToString(blockHash.Sum(nil))
	result.RawSHA256 = hex.EncodeToString(rawHash.Sum(nil))
	result.TypedSHA256 = hex.EncodeToString(typedHash.Sum(nil))
	return result, tx.Commit(ctx)
}

type IngestQueryPlan struct {
	Name, SQL string
	Arguments []any
	Plan      json.RawMessage
}

func (s *Store) MeasureIngestPlans(ctx context.Context, stream string, raw RawRecord, pool string) ([]IngestQueryPlan, error) {
	if !replayToken.MatchString(stream) {
		return nil, errors.New("actual source stream required")
	}
	if _, err := RawDigest(raw); err != nil {
		return nil, err
	}
	if !addressPattern.MatchString(pool) {
		return nil, errors.New("actual hot pool address required")
	}
	plans := []IngestQueryPlan{
		{Name: "canonical-height", SQL: `SELECT transaction_hash,log_index FROM indexer.indexer_raw_logs WHERE chain_id=125 AND height>=$1 ORDER BY height,transaction_index,log_index LIMIT 5000`, Arguments: []any{raw.Height}},
		{Name: "receipt-identity", SQL: `SELECT * FROM indexer.indexer_raw_logs WHERE chain_id=125 AND transaction_hash=$1 ORDER BY log_index`, Arguments: []any{raw.TransactionHash}},
		{Name: "emitter-height", SQL: `SELECT * FROM indexer.indexer_raw_logs WHERE chain_id=125 AND emitter=$1 AND height>=$2 ORDER BY height,transaction_index,log_index LIMIT 5000`, Arguments: []any{raw.Emitter, raw.Height}},
		{Name: "required-quarantine", SQL: `SELECT transaction_hash,log_index FROM indexer.indexer_quarantine WHERE stream_id=$1 AND resolution='unresolved' AND required ORDER BY height LIMIT 5000`, Arguments: []any{stream}},
		{Name: "hot-pool-trades", SQL: `SELECT f.source_fill_id,r.height,r.transaction_index,r.log_index FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs r USING(chain_id,transaction_hash,log_index) WHERE f.pool_address=$1 AND r.height>=$2 ORDER BY r.height,r.transaction_index,r.log_index LIMIT 5000`, Arguments: []any{pool, raw.Height}},
	}
	for i := range plans {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := s.db.QueryRow(bounded, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+plans[i].SQL, plans[i].Arguments...).Scan(&plans[i].Plan)
		cancel()
		if err != nil {
			return plans[:i], err
		}
	}
	return plans, nil
}

func (s *Store) CompareReconstructionSegment(ctx context.Context, r ReplayRequest, segment ReplaySegment) (int64, int64, error) {
	if err := ValidateReplaySegment(segment); err != nil {
		return 0, 0, err
	}
	if segment.SourceStream != r.SourceStream || segment.FromHeight < r.FromHeight || segment.ToHeight > r.ToHeight {
		return 0, 0, ErrIntegrity
	}
	db, ok := s.db.(interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	})
	if !ok {
		return 0, 0, errors.New("reconstruction comparison requires snapshot")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var rawCount, typedCount int64
	for _, raw := range segment.Records {
		digest, err := RawDigest(raw)
		if err != nil {
			return rawCount, typedCount, err
		}
		var topics []byte
		var data []byte
		var actual RawRecord
		err = tx.QueryRow(ctx, `SELECT transaction_hash,block_hash,emitter,height,transaction_index,log_index,topics,data FROM indexer.indexer_raw_logs WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2`, raw.TransactionHash, int64(raw.LogIndex)).Scan(&actual.TransactionHash, &actual.BlockHash, &actual.Emitter, &actual.Height, &actual.TransactionIndex, &actual.LogIndex, &topics, &data)
		if err != nil {
			return rawCount, typedCount, err
		}
		actual.Data = data
		if err = json.Unmarshal(topics, &actual.Topics); err != nil {
			return rawCount, typedCount, err
		}
		observed, err := RawDigest(actual)
		if err != nil || observed != digest {
			return rawCount, typedCount, ErrIntegrity
		}
		rawCount++
	}
	for _, event := range segment.Events {
		var body []byte
		err = tx.QueryRow(ctx, `SELECT payload FROM indexer.indexer_replay_effects WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND raw_digest=$4 AND event_kind=$5 AND height=$6`, r.Target, r.Generation, r.ProjectionRevision, event.RawDigest, event.Kind, event.Raw.Height).Scan(&body)
		if err != nil {
			return rawCount, typedCount, err
		}
		actual, err := canonicalPayload(body)
		if err != nil {
			return rawCount, typedCount, err
		}
		expected, err := canonicalPayload(event.Payload)
		if err != nil {
			return rawCount, typedCount, err
		}
		if !bytes.Equal(actual, expected) {
			return rawCount, typedCount, ErrIntegrity
		}
		typedCount++
	}
	var actualRaw, actualTyped int64
	err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM indexer.indexer_raw_logs WHERE chain_id=125 AND height BETWEEN $4 AND $5),(SELECT count(*) FROM indexer.indexer_replay_effects WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND height BETWEEN $4 AND $5)`, r.Target, r.Generation, r.ProjectionRevision, segment.FromHeight, segment.ToHeight).Scan(&actualRaw, &actualTyped)
	if err != nil {
		return rawCount, typedCount, err
	}
	if actualRaw != rawCount || actualTyped != typedCount {
		return rawCount, typedCount, ErrIntegrity
	}
	return rawCount, typedCount, tx.Commit(ctx)
}
