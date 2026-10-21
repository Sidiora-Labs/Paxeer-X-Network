package source

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Blockscout SQL (parity with paxscan-source.ts). The two-step lookup-by-number
// avoids the blocks-table JOIN that degenerates into millions of random btree
// probes on large pages.
const (
	paxscanFetchLogsSQL = `
		SELECT l.address_hash, l.first_topic, l.second_topic, l.third_topic,
		       l.fourth_topic, l.data, l.transaction_hash, l.block_number, l.index
		FROM logs l
		WHERE l.address_hash = ANY($1::bytea[])
		  AND l.block_number BETWEEN $2 AND $3
		  AND l.first_topic IS NOT NULL
		ORDER BY l.block_number ASC, l.index ASC`

	paxscanBlockTsSQL = `
		SELECT number, EXTRACT(EPOCH FROM timestamp)::bigint AS ts
		FROM blocks
		WHERE number = ANY($1::bigint[]) AND consensus = TRUE`

	paxscanTxFromSQL = `
		SELECT hash, from_address_hash
		FROM transactions
		WHERE hash = ANY($1::bytea[])`

	paxscanHeadSQL = `SELECT MAX(number)::bigint AS head FROM blocks WHERE consensus = TRUE`

	paxscanLookupBatch = 1000
)

// PaxscanSource reads decoded logs from a Blockscout-compatible Postgres DB.
type PaxscanSource struct {
	pool      *pgxpool.Pool
	lifecycle *sourceLifecycle
}

// NewPaxscan opens a dedicated pool to the Blockscout DB with the proven session
// tuning (work_mem 512MB, 5-min statement_timeout).
func NewPaxscan(ctx context.Context, dsn string) (*PaxscanSource, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("paxscan: parse dsn: %w", err)
	}
	cfg.MaxConns = 5
	cfg.MaxConnIdleTime = 30 * time.Second
	cfg.ConnConfig.ConnectTimeout = 15 * time.Second
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if _, err := conn.Exec(ctx, "SET work_mem = '512MB'"); err != nil {
			return err
		}
		_, err := conn.Exec(ctx, "SET statement_timeout = '300000'")
		return err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("paxscan: new pool: %w", err)
	}
	return &PaxscanSource{pool: pool, lifecycle: newSourceLifecycle()}, nil
}

// Name implements LogSource / HeadSource.
func (s *PaxscanSource) Name() string { return "paxscan" }

// Close releases the pool.
func (s *PaxscanSource) Close() error {
	if s.lifecycle != nil {
		if err := s.lifecycle.close(); err != nil {
			return err
		}
	}
	s.pool.Close()
	return nil
}

// Head implements HeadSource via MAX(number) over consensus blocks.
func (s *PaxscanSource) Head(ctx context.Context) (int64, error) {
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return 0, err
		}
		defer done()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var head *int64
	if err := s.pool.QueryRow(ctx, paxscanHeadSQL).Scan(&head); err != nil {
		return 0, fmt.Errorf("paxscan head: %w", err)
	}
	if head == nil {
		return 0, fmt.Errorf("paxscan: no consensus blocks found (is the DB empty?)")
	}
	return *head, nil
}

// FetchLogs implements LogSource via the three-step lookup.
func (s *PaxscanSource) FetchLogs(ctx context.Context, opts FetchOptions) (FetchResult, error) {
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.beginRange(ctx, opts, opts.Historical, "legacy-explorer")
		if err != nil {
			return FetchResult{}, err
		}
		defer done()
	}
	deadline := 2 * time.Second
	if opts.Historical {
		deadline = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	addrBytes := make([][]byte, 0, len(opts.MonitoredAddresses))
	for _, a := range opts.MonitoredAddresses {
		addrBytes = append(addrBytes, common.HexToAddress(a).Bytes())
	}

	res := FetchResult{BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}

	rows, err := s.pool.Query(ctx, paxscanFetchLogsSQL, addrBytes, opts.FromBlock, opts.ToBlock)
	if err != nil {
		return FetchResult{}, fmt.Errorf("paxscan fetch logs: %w", err)
	}
	type logRow struct {
		log    ethtypes.Log
		txHash []byte
		topic0 string
	}
	var collected []logRow
	uniqueBlocks := map[uint64]struct{}{}
	txNeeding := map[string][]byte{} // lowercased 0xhex -> raw bytes
	for rows.Next() {
		var addr, t1, t2, t3, t4, data, txHash []byte
		var blockNumber int64
		var logIndex int32
		if err := rows.Scan(&addr, &t1, &t2, &t3, &t4, &data, &txHash, &blockNumber, &logIndex); err != nil {
			rows.Close()
			return FetchResult{}, fmt.Errorf("paxscan scan log: %w", err)
		}
		topics := make([]common.Hash, 0, 4)
		for _, t := range [][]byte{t1, t2, t3, t4} {
			if len(t) > 0 {
				topics = append(topics, common.BytesToHash(t))
			}
		}
		l := ethtypes.Log{
			Address:     common.BytesToAddress(addr),
			Topics:      topics,
			Data:        data,
			BlockNumber: uint64(blockNumber),
			TxHash:      common.BytesToHash(txHash),
			Index:       uint(logIndex),
		}
		topic0 := ""
		if len(topics) > 0 {
			topic0 = topics[0].Hex()
		}
		collected = append(collected, logRow{log: l, txHash: txHash, topic0: topic0})
		uniqueBlocks[l.BlockNumber] = struct{}{}
		if needsTxFrom(topic0) {
			txNeeding[strings.ToLower(l.TxHash.Hex())] = txHash
		}
	}
	if err := rows.Err(); err != nil {
		return FetchResult{}, fmt.Errorf("paxscan rows: %w", err)
	}
	rows.Close()

	if len(collected) == 0 {
		return res, nil
	}

	res.Logs = make([]ethtypes.Log, len(collected))
	for i, c := range collected {
		res.Logs[i] = c.log
	}

	// Step 2: batch block timestamps.
	blocks := make([]int64, 0, len(uniqueBlocks))
	for b := range uniqueBlocks {
		blocks = append(blocks, int64(b))
	}
	for i := 0; i < len(blocks); i += paxscanLookupBatch {
		end := i + paxscanLookupBatch
		if end > len(blocks) {
			end = len(blocks)
		}
		tsRows, err := s.pool.Query(ctx, paxscanBlockTsSQL, blocks[i:end])
		if err != nil {
			return FetchResult{}, fmt.Errorf("paxscan block ts: %w", err)
		}
		for tsRows.Next() {
			var number, ts int64
			if err := tsRows.Scan(&number, &ts); err != nil {
				tsRows.Close()
				return FetchResult{}, fmt.Errorf("paxscan scan ts: %w", err)
			}
			res.BlockTimestamps[uint64(number)] = ts
		}
		if err := tsRows.Err(); err != nil {
			return FetchResult{}, err
		}
		tsRows.Close()
	}

	// Step 3: batch tx-from for Swap-like events only.
	txBytes := make([][]byte, 0, len(txNeeding))
	for _, b := range txNeeding {
		txBytes = append(txBytes, b)
	}
	for i := 0; i < len(txBytes); i += paxscanLookupBatch {
		end := i + paxscanLookupBatch
		if end > len(txBytes) {
			end = len(txBytes)
		}
		txRows, err := s.pool.Query(ctx, paxscanTxFromSQL, txBytes[i:end])
		if err != nil {
			return FetchResult{}, fmt.Errorf("paxscan tx from: %w", err)
		}
		for txRows.Next() {
			var hash, from []byte
			if err := txRows.Scan(&hash, &from); err != nil {
				txRows.Close()
				return FetchResult{}, fmt.Errorf("paxscan scan tx: %w", err)
			}
			res.TxFromMap[strings.ToLower(common.BytesToHash(hash).Hex())] = strings.ToLower(common.BytesToAddress(from).Hex())
		}
		if err := txRows.Err(); err != nil {
			return FetchResult{}, err
		}
		txRows.Close()
	}

	return res, nil
}
