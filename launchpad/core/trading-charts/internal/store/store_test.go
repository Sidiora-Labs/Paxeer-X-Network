package store_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

const (
	testPool = "0xpool00000000000000000000000000000000aaaa"
	testTF   = "1m"
)

func sampleCandle(start, seq int64, closePrice string) store.CandleRow {
	return store.CandleRow{
		PoolAddress:     testPool,
		Timeframe:       testTF,
		CandleStart:     start,
		Open:            "1000000000000000000",
		High:            "2000000000000000000",
		Low:             "500000000000000000",
		Close:           closePrice,
		VolumeUsdl:      "100",
		VolumeToken:     "200",
		BuyVolumeUsdl:   "60",
		SellVolumeUsdl:  "40",
		TradeCount:      3,
		UniqueTraders:   2,
		LargeTradeCount: 1,
		LastTradeTs:     start + 30,
		SequenceNum:     seq,
		McapOpen:        "10",
		McapHigh:        "20",
		McapLow:         "5",
		McapClose:       "15",
	}
}

func TestCandleInsertGetUpdate(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	// Missing candle returns (nil, nil).
	if got, err := st.GetCandle(ctx, testPool, testTF, 60); err != nil || got != nil {
		t.Fatalf("GetCandle empty = %v, err %v (want nil,nil)", got, err)
	}

	c := sampleCandle(60, 0, "1500000000000000000")
	if err := st.InsertCandle(ctx, c); err != nil {
		t.Fatalf("InsertCandle: %v", err)
	}
	// Idempotent re-insert (ON CONFLICT DO NOTHING) must not error.
	if err := st.InsertCandle(ctx, c); err != nil {
		t.Fatalf("InsertCandle idempotent: %v", err)
	}

	got, err := st.GetCandle(ctx, testPool, testTF, 60)
	if err != nil || got == nil {
		t.Fatalf("GetCandle after insert = %v, err %v", got, err)
	}
	if got.Close != "1500000000000000000" || got.TradeCount != 3 || got.UniqueTraders != 2 {
		t.Errorf("candle row wrong: %+v", got)
	}

	// Update mutates high/low/close/volumes etc.
	c.High = "3000000000000000000"
	c.Close = "2500000000000000000"
	c.TradeCount = 9
	if err := st.UpdateCandle(ctx, c); err != nil {
		t.Fatalf("UpdateCandle: %v", err)
	}
	got, _ = st.GetCandle(ctx, testPool, testTF, 60)
	if got.High != "3000000000000000000" || got.Close != "2500000000000000000" || got.TradeCount != 9 {
		t.Errorf("after update wrong: %+v", got)
	}
}

func TestCursorLifecycle(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	if c, err := st.GetCursor(ctx, testPool, testTF); err != nil || c != nil {
		t.Fatalf("GetCursor empty = %v, err %v", c, err)
	}

	if err := st.UpsertCursor(ctx, store.CursorRow{
		PoolAddress: testPool, Timeframe: testTF,
		LastClose: "100", LastCandleStart: 60, LastSequenceNum: 0,
	}); err != nil {
		t.Fatalf("UpsertCursor insert: %v", err)
	}

	c, err := st.GetCursor(ctx, testPool, testTF)
	if err != nil || c == nil || c.LastClose != "100" || c.LastCandleStart != 60 {
		t.Fatalf("GetCursor after insert = %+v, err %v", c, err)
	}

	// Upsert (conflict) updates the existing row.
	if err := st.UpsertCursor(ctx, store.CursorRow{
		PoolAddress: testPool, Timeframe: testTF,
		LastClose: "250", LastCandleStart: 120, LastSequenceNum: 1,
	}); err != nil {
		t.Fatalf("UpsertCursor update: %v", err)
	}
	c, _ = st.GetCursor(ctx, testPool, testTF)
	if c.LastClose != "250" || c.LastCandleStart != 120 || c.LastSequenceNum != 1 {
		t.Errorf("after upsert update wrong: %+v", c)
	}

	// UpdateCursorClose only when candleStart <= last_candle_start.
	if err := st.UpdateCursorClose(ctx, testPool, testTF, "999", 120); err != nil {
		t.Fatalf("UpdateCursorClose: %v", err)
	}
	c, _ = st.GetCursor(ctx, testPool, testTF)
	if c.LastClose != "999" {
		t.Errorf("UpdateCursorClose did not apply: %+v", c)
	}

	all, err := st.AllCursors(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("AllCursors = %d rows, err %v (want 1)", len(all), err)
	}
}

func TestHistoryRangeAndCountback(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	// Five sequential candles, seq + candle_start ascending.
	for i := int64(0); i < 5; i++ {
		c := sampleCandle(60+i*60, i, "1000000000000000000")
		if err := st.InsertCandle(ctx, c); err != nil {
			t.Fatalf("InsertCandle %d: %v", i, err)
		}
	}

	// Range query returns the inclusive window, ascending by sequence_num.
	rng, err := st.HistoryRange(ctx, testPool, testTF, 120, 240)
	if err != nil {
		t.Fatalf("HistoryRange: %v", err)
	}
	if len(rng) != 3 {
		t.Fatalf("HistoryRange len = %d, want 3 (120,180,240)", len(rng))
	}
	if rng[0].CandleStart != 120 || rng[2].CandleStart != 240 {
		t.Errorf("HistoryRange not ascending: %d..%d", rng[0].CandleStart, rng[2].CandleStart)
	}

	// Countback returns the last N up to `to`, reversed to ascending order.
	cb, err := st.HistoryCountback(ctx, testPool, testTF, 300, 2)
	if err != nil {
		t.Fatalf("HistoryCountback: %v", err)
	}
	if len(cb) != 2 {
		t.Fatalf("HistoryCountback len = %d, want 2", len(cb))
	}
	if cb[0].CandleStart != 240 || cb[1].CandleStart != 300 {
		t.Errorf("HistoryCountback order wrong: %d, %d (want 240,300)", cb[0].CandleStart, cb[1].CandleStart)
	}

	// Empty window yields an empty slice, no error.
	empty, err := st.HistoryRange(ctx, testPool, testTF, 100000, 200000)
	if err != nil || len(empty) != 0 {
		t.Fatalf("HistoryRange empty = %d, err %v", len(empty), err)
	}
}

func TestStoreErrorBranches(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	st := store.New(pool)

	cctx, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := st.GetCandle(cctx, testPool, testTF, 60); err == nil {
		t.Error("GetCandle cancelled ctx should error")
	}
	if _, err := st.GetCursor(cctx, testPool, testTF); err == nil {
		t.Error("GetCursor cancelled ctx should error")
	}
	if err := st.InsertCandle(cctx, sampleCandle(60, 0, "1")); err == nil {
		t.Error("InsertCandle cancelled ctx should error")
	}
	if err := st.UpdateCandle(cctx, sampleCandle(60, 0, "1")); err == nil {
		t.Error("UpdateCandle cancelled ctx should error")
	}
	if err := st.UpsertCursor(cctx, store.CursorRow{PoolAddress: testPool, Timeframe: testTF, LastClose: "1"}); err == nil {
		t.Error("UpsertCursor cancelled ctx should error")
	}
	if err := st.UpdateCursorClose(cctx, testPool, testTF, "1", 60); err == nil {
		t.Error("UpdateCursorClose cancelled ctx should error")
	}
	if _, err := st.AllCursors(cctx); err == nil {
		t.Error("AllCursors cancelled ctx should error")
	}
	if _, err := st.HistoryRange(cctx, testPool, testTF, 0, 100); err == nil {
		t.Error("HistoryRange cancelled ctx should error")
	}
	if _, err := st.HistoryCountback(cctx, testPool, testTF, 100, 5); err == nil {
		t.Error("HistoryCountback cancelled ctx should error")
	}
}
