package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Store) ReconcileCanonicalCommit(ctx context.Context, b CanonicalBatch) (bool, error) {
	if err := validateBatch(b); err != nil {
		return false, err
	}
	db, ok := s.db.(beginner)
	if !ok {
		return false, errors.New("canonical reconciliation requires transaction-capable DB")
	}
	var tx pgx.Tx
	var err error
	if configured, ok := s.db.(interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	}); ok {
		tx, err = configured.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	} else {
		tx, err = db.Begin(ctx)
		if err == nil {
			_, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`)
		}
	}
	if err != nil {
		if tx != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			_ = tx.Rollback(cleanup)
			cancel()
		}
		return false, err
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	var previous, last int64
	var blockHash string
	var recordCount, outboxCount int
	err = tx.QueryRow(ctx, `SELECT previous_height,last_complete_height,block_hash,record_count,outbox_count FROM indexer.indexer_ingest_commits WHERE chain_id=$1 AND stream_id=$2 AND batch_id=$3`, b.ChainID, b.StreamID, b.BatchID).Scan(&previous, &last, &blockHash, &recordCount, &outboxCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	final := b.Blocks[len(b.Blocks)-1]
	if previous != b.PreviousHeight || last != final.Height || blockHash != final.Hash || recordCount != len(b.Records) {
		return false, ErrIntegrity
	}
	for _, block := range b.Blocks {
		var same bool
		err = tx.QueryRow(ctx, `SELECT block_hash=$4 AND raw_count=$5 AND fetch_complete AND required_decode_complete FROM indexer.indexer_block_completeness WHERE chain_id=$1 AND stream_id=$2 AND height=$3`, b.ChainID, b.StreamID, block.Height, block.Hash, block.RawCount).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return false, ErrIntegrity
		}
		if err != nil {
			return false, err
		}
	}
	var actual []OutboxIntent
	for _, record := range b.Records {
		topics := record.Topics
		if topics == nil {
			topics = []string{}
		}
		encodedTopics, _ := json.Marshal(topics)
		data := record.Data
		if data == nil {
			data = []byte{}
		}
		var same bool
		err = tx.QueryRow(ctx, `SELECT height=$4 AND transaction_index=$5 AND block_hash=$6 AND emitter=$7 AND topics=$8::jsonb AND data=$9 FROM indexer.indexer_raw_logs WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3`, b.ChainID, record.TransactionHash, int64(record.LogIndex), record.Height, int64(record.TransactionIndex), record.BlockHash, record.Emitter, encodedTopics, data).Scan(&same)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && !same {
			return false, ErrIntegrity
		}
		if err != nil {
			return false, err
		}
		rows, queryErr := tx.Query(ctx, `SELECT transaction_hash,log_index,event_kind,payload FROM indexer.indexer_ingest_outbox WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3`, b.ChainID, record.TransactionHash, int64(record.LogIndex))
		if queryErr != nil {
			return false, queryErr
		}
		for rows.Next() {
			var intent OutboxIntent
			if err = rows.Scan(&intent.TransactionHash, &intent.LogIndex, &intent.Kind, &intent.Payload); err != nil {
				rows.Close()
				return false, err
			}
			actual = append(actual, intent)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return false, err
		}
	}
	if (len(actual) != outboxCount || b.Outbox != nil && !sameIntents(b.Outbox, actual)) && !reconciledWithDeferredInventory(b.Outbox, actual, outboxCount) {
		return false, ErrIntegrity
	}
	var rawHeight, typedHeight int64
	if err = tx.QueryRow(ctx, `SELECT raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=$1 AND stream_id=$2`, b.ChainID, b.StreamID).Scan(&rawHeight, &typedHeight); err != nil {
		return false, err
	}
	if rawHeight < last || typedHeight < last {
		return false, ErrIntegrity
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
