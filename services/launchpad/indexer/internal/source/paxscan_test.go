package source

import (
	"context"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest"
)

// blockscoutSchema creates the minimal subset of Blockscout tables the paxscan
// source reads, matching the column shapes its SQL expects (bytea hashes/topics).
const blockscoutSchema = `
CREATE TABLE logs (
  address_hash     bytea,
  first_topic      bytea,
  second_topic     bytea,
  third_topic      bytea,
  fourth_topic     bytea,
  data             bytea,
  transaction_hash bytea,
  block_number     bigint,
  index            integer
);
CREATE TABLE blocks (number bigint, timestamp timestamptz, consensus boolean);
CREATE TABLE transactions (hash bytea, from_address_hash bytea);
`

func TestPaxscanFetchLogsAndHead(t *testing.T) {
	ctx := context.Background()
	dsn, pool := internaltest.NewPostgres(t)
	if _, err := pool.Exec(ctx, blockscoutSchema); err != nil {
		t.Fatalf("create blockscout schema: %v", err)
	}

	emitter := common.HexToAddress("0x00000000000000000000000000000000000000ee")
	swap := swapTopic(t)
	other := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	swapTx := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a1")
	otherTx := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000b2")
	fromAddr := common.HexToAddress("0x00000000000000000000000000000000000000f1")
	from2 := common.HexToAddress("0x00000000000000000000000000000000000000f2")

	// A Swap log (needs tx-from) and an unrelated-topic log (does not).
	if _, err := pool.Exec(ctx,
		`INSERT INTO logs (address_hash, first_topic, data, transaction_hash, block_number, index)
		 VALUES ($1,$2,$3,$4,$5,$6),($1,$7,$3,$8,$5,$9)`,
		emitter.Bytes(), swap.Bytes(), []byte{0xde, 0xad}, swapTx.Bytes(), int64(16), int32(0),
		other.Bytes(), otherTx.Bytes(), int32(1),
	); err != nil {
		t.Fatalf("insert logs: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO blocks (number, timestamp, consensus) VALUES ($1, to_timestamp($2), true)`,
		int64(16), int64(1700000000)); err != nil {
		t.Fatalf("insert block: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO transactions (hash, from_address_hash) VALUES ($1,$2),($3,$4)`,
		swapTx.Bytes(), fromAddr.Bytes(), otherTx.Bytes(), from2.Bytes()); err != nil {
		t.Fatalf("insert transactions: %v", err)
	}

	src, err := NewPaxscan(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPaxscan: %v", err)
	}
	defer src.Close()
	if src.Name() != "paxscan" {
		t.Errorf("Name = %q", src.Name())
	}

	head, err := src.Head(ctx)
	if err != nil || head != 16 {
		t.Fatalf("Head = %d, err %v", head, err)
	}

	res, err := src.FetchLogs(ctx, FetchOptions{
		MonitoredAddresses: []string{emitter.Hex()}, FromBlock: 0, ToBlock: 100,
	})
	if err != nil {
		t.Fatalf("FetchLogs: %v", err)
	}
	if len(res.Logs) != 2 {
		t.Fatalf("logs = %d, want 2", len(res.Logs))
	}
	if res.Logs[0].Index != 0 || res.Logs[1].Index != 1 {
		t.Errorf("logs not ordered: %+v", res.Logs)
	}
	if res.BlockTimestamps[16] != 1700000000 {
		t.Errorf("timestamp = %d", res.BlockTimestamps[16])
	}
	// Only the Swap tx-from is resolved (the other-topic log is excluded).
	if len(res.TxFromMap) != 1 {
		t.Fatalf("TxFromMap = %v, want 1 (swap only)", res.TxFromMap)
	}
	wantKey := common.BytesToHash(swapTx.Bytes()).Hex()
	if got := res.TxFromMap[wantKey]; got != fromAddr.Hex() {
		t.Errorf("tx-from = %q, want %q", got, fromAddr.Hex())
	}
}

func TestPaxscanFetchLogsEmpty(t *testing.T) {
	ctx := context.Background()
	dsn, pool := internaltest.NewPostgres(t)
	if _, err := pool.Exec(ctx, blockscoutSchema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	src, err := NewPaxscan(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPaxscan: %v", err)
	}
	defer src.Close()

	res, err := src.FetchLogs(ctx, FetchOptions{
		MonitoredAddresses: []string{"0x00000000000000000000000000000000000000ee"}, FromBlock: 0, ToBlock: 10,
	})
	if err != nil || len(res.Logs) != 0 {
		t.Fatalf("empty FetchLogs = %+v, err %v", res, err)
	}

	// No consensus blocks -> Head errors.
	if _, err := src.Head(ctx); err == nil {
		t.Error("Head on empty blocks should error")
	}
}

func TestNewPaxscanBadDSN(t *testing.T) {
	t.Parallel()
	if _, err := NewPaxscan(context.Background(), "://nonsense"); err == nil {
		t.Error("NewPaxscan with bad dsn should error")
	}
}
