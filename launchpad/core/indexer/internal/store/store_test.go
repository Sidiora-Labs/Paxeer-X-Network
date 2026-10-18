package store_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
)

func ptrStr(s string) *string { return &s }

func TestCursor(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	c, err := st.GetCursor(ctx, 125)
	if err != nil {
		t.Fatalf("GetCursor empty: %v", err)
	}
	if c != nil {
		t.Fatalf("GetCursor on empty = %v, want nil", *c)
	}

	if err := st.UpsertCursor(ctx, 125, 100); err != nil {
		t.Fatalf("UpsertCursor insert: %v", err)
	}
	c, err = st.GetCursor(ctx, 125)
	if err != nil || c == nil || *c != 100 {
		t.Fatalf("GetCursor after insert = %v, err %v", c, err)
	}

	if err := st.UpsertCursor(ctx, 125, 250); err != nil {
		t.Fatalf("UpsertCursor update: %v", err)
	}
	c, _ = st.GetCursor(ctx, 125)
	if c == nil || *c != 250 {
		t.Fatalf("GetCursor after update = %v, want 250", c)
	}

	// NewFromDBTX builds an equivalent store from any DBTX (the pool here).
	st2 := store.NewFromDBTX(pool)
	if c2, err := st2.GetCursor(ctx, 125); err != nil || c2 == nil || *c2 != 250 {
		t.Fatalf("NewFromDBTX GetCursor = %v, err %v", c2, err)
	}

	// A cancelled context surfaces a non-ErrNoRows error through the
	// not-found-aware wrappers (exercises their error branches).
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := st.GetCursor(cctx, 125); err == nil {
		t.Error("GetCursor with cancelled ctx should error")
	}
	if _, err := st.GetPoolByPoolID(cctx, "0xpid"); err == nil {
		t.Error("GetPoolByPoolID with cancelled ctx should error")
	}
	if _, err := st.ActiveBackfillJob(cctx); err == nil {
		t.Error("ActiveBackfillJob with cancelled ctx should error")
	}
}

func TestPoolInsertGetCountIdempotent(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	if got, _ := st.GetPoolByPoolID(ctx, "0xdead"); got != nil {
		t.Fatalf("unknown pool = %v, want nil", got)
	}

	nft := int64(7)
	p := sqlcdb.InsertPoolParams{
		PoolAddress:  "0xpool",
		TokenAddress: "0xtoken",
		Creator:      "0xcreator",
		Optical:      "0xoptical",
		PoolID:       "0xpid",
		NftID:        &nft,
		CreatedAt:    1700000000,
		CreatedBlock: 10,
		TxHash:       "0xtx",
	}
	if err := st.InsertPool(ctx, p); err != nil {
		t.Fatalf("InsertPool: %v", err)
	}
	// Idempotent re-insert (ON CONFLICT DO NOTHING).
	if err := st.InsertPool(ctx, p); err != nil {
		t.Fatalf("InsertPool idempotent: %v", err)
	}

	got, err := st.GetPoolByPoolID(ctx, "0xpid")
	if err != nil || got == nil {
		t.Fatalf("GetPoolByPoolID = %v, err %v", got, err)
	}
	if got.PoolAddress != "0xpool" || got.NftID == nil || *got.NftID != 7 {
		t.Errorf("pool row wrong: %+v", got)
	}

	n, err := st.PoolCount(ctx)
	if err != nil || n != 1 {
		t.Fatalf("PoolCount = %d, err %v (want 1 after idempotent re-insert)", n, err)
	}
}

func TestSwapInsertCountIdempotent(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	s := sqlcdb.InsertSwapParams{
		ID: "0xtx-0", PoolID: "0xpid", PoolAddress: "0xpool", Sender: "0xuser",
		Router: ptrStr("0xrouter"), IsBuy: true, AmountIn: "100", AmountOut: "95",
		Fee: "5", Price: "2", BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 0,
	}
	if err := st.InsertSwap(ctx, s); err != nil {
		t.Fatalf("InsertSwap: %v", err)
	}
	if err := st.InsertSwap(ctx, s); err != nil {
		t.Fatalf("InsertSwap idempotent: %v", err)
	}
	n, err := st.SwapCount(ctx)
	if err != nil || n != 1 {
		t.Fatalf("SwapCount = %d, err %v (want 1)", n, err)
	}
}

func TestInsertAllEventTypes(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	// Each insert is run twice to prove ON CONFLICT idempotency.
	twice := func(name string, fn func() error) {
		t.Helper()
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := fn(); err != nil {
			t.Fatalf("%s idempotent: %v", name, err)
		}
	}

	twice("snapshot", func() error {
		return st.InsertPoolStateSnapshot(ctx, sqlcdb.InsertPoolStateSnapshotParams{
			ID: "0xpid-10-0", PoolID: "0xpid", VirtualReserve: "1000", RealReserve: "900",
			TokenReserve: "800", Price: "7", BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 0,
		})
	})
	twice("fee event", func() error {
		return st.InsertFeeEvent(ctx, sqlcdb.InsertFeeEventParams{
			ID: "0xtx-1", PoolID: "0xpid", FeeAmount: "10", ProtocolCut: "6", PoolCut: "4",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 1,
		})
	})
	twice("fee distribution", func() error {
		return st.InsertFeeDistribution(ctx, sqlcdb.InsertFeeDistributionParams{
			ID: "0xtx-2", PoolID: "0xpid", NftID: 7, Strategy: 3, Amount: "500", Recipient: "0xrec",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 2,
		})
	})
	twice("fee strategy change", func() error {
		return st.InsertFeeStrategyChange(ctx, sqlcdb.InsertFeeStrategyChangeParams{
			ID: "0xtx-3", PoolID: "0xpid", NftID: 7, OldStrategy: 1, NewStrategy: 2,
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 3,
		})
	})
	twice("airdrop claim", func() error {
		poolID, poolAddress, nft, epoch := "0xpid", "0xpool", int64(7), "2"
		return st.InsertAirdropClaim(ctx, sqlcdb.InsertAirdropClaimParams{
			ID: "0xtx-7", PoolID: &poolID, PoolAddress: &poolAddress, NftID: &nft, Holder: "0xholder", Amount: "125", Epoch: &epoch, EventName: "AirdropClaimed",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 7,
		})
	})
	twice("airdrop claim unattributed", func() error {
		return st.InsertAirdropClaim(ctx, sqlcdb.InsertAirdropClaimParams{
			ID: "0xtx-8", Holder: "0xholder", Amount: "1", EventName: "FeeFlow",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 8,
		})
	})
	twice("optical", func() error {
		return st.InsertOpticalExecution(ctx, sqlcdb.InsertOpticalExecutionParams{
			ID: "0xtx-4", PoolID: "0xpid", Optical: "0xopt", HookName: "hook", Data: "0xdeadbeef",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 4,
		})
	})
	twice("token-for-token", func() error {
		return st.InsertTokenForTokenSwap(ctx, sqlcdb.InsertTokenForTokenSwapParams{
			ID: "0xtx-5", Sender: "0xuser", TokenIn: "0xin", TokenOut: "0xout", PoolIn: "0xpin", PoolOut: "0xpout",
			AmountIn: "100", IntermediateUsdl: "50", AmountOut: "98", FeeIn: "1", FeeOut: "1",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 5,
		})
	})
	twice("config update", func() error {
		return st.InsertConfigUpdate(ctx, sqlcdb.InsertConfigUpdateParams{
			ID: "0xtx-6", Key: "0xkey", OldValue: "10", NewValue: "20",
			BlockNumber: 10, BlockTimestamp: 1700000000, TxHash: "0xtx", LogIndex: 6,
		})
	})
}

func TestBackfillJobLifecycle(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	if j, err := st.ActiveBackfillJob(ctx); err != nil || j != nil {
		t.Fatalf("ActiveBackfillJob empty = %v, err %v", j, err)
	}

	if err := st.InsertBackfillJob(ctx, sqlcdb.InsertBackfillJobParams{
		ID: "job-1", ChainID: 125, FromBlock: 0, ToBlock: 100, LastProcessedBlock: -1, TotalBlocks: 101, Status: "running",
	}); err != nil {
		t.Fatalf("InsertBackfillJob: %v", err)
	}

	j, err := st.ActiveBackfillJob(ctx)
	if err != nil || j == nil || j.ID != "job-1" {
		t.Fatalf("ActiveBackfillJob = %v, err %v", j, err)
	}

	if err := st.UpdateBackfillProgress(ctx, "job-1", 50); err != nil {
		t.Fatalf("UpdateBackfillProgress: %v", err)
	}
	j, _ = st.ActiveBackfillJob(ctx)
	if j == nil || j.LastProcessedBlock != 50 {
		t.Fatalf("after progress = %v, want lastProcessed 50", j)
	}

	if err := st.CompleteBackfillJob(ctx, "job-1"); err != nil {
		t.Fatalf("CompleteBackfillJob: %v", err)
	}
	if j, err := st.ActiveBackfillJob(ctx); err != nil || j != nil {
		t.Fatalf("ActiveBackfillJob after complete = %v, err %v (want nil)", j, err)
	}

	// A second job that fails.
	if err := st.InsertBackfillJob(ctx, sqlcdb.InsertBackfillJobParams{
		ID: "job-2", ChainID: 125, FromBlock: 101, ToBlock: 200, LastProcessedBlock: 100, TotalBlocks: 100, Status: "running",
	}); err != nil {
		t.Fatalf("InsertBackfillJob 2: %v", err)
	}
	if err := st.FailBackfillJob(ctx, "job-2", "boom"); err != nil {
		t.Fatalf("FailBackfillJob: %v", err)
	}
	if j, err := st.ActiveBackfillJob(ctx); err != nil || j != nil {
		t.Fatalf("ActiveBackfillJob after fail = %v, err %v (want nil)", j, err)
	}
}
