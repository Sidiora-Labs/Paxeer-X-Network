package projectionstats

import (
	"context"
	"math/big"
	"testing"
)

func TestPhase2ActualReceiptBatchAndReplay(t *testing.T) {
	ctx, r := repository(t)
	authority, labels := historicalAuthority(t)
	trades := []Trade{}
	volume := new(big.Int)
	observed := int64(0)
	for _, label := range []string{"buy", "sell", "direct-swap", "rounding-buy", "multihop"} {
		for _, trade := range authority.Trades(labels[label]) {
			trades = append(trades, trade)
			n, ok := new(big.Int).SetString(trade.USDLAmountRaw, 10)
			if !ok {
				t.Fatal("actual amount")
			}
			volume.Add(volume, n)
			if ts := authority.admitted[trade.EventID].timestamp; ts > observed {
				observed = ts
			}
		}
	}
	first, err := r.ApplyAuthorizedBatch(ctx, authority, trades, "phase2-batch", "phase2-batch")
	if err != nil || len(first) != len(trades) {
		t.Fatal(first, err)
	}
	for _, result := range first {
		if !result.Accepted {
			t.Fatal(first)
		}
	}
	replay, err := r.ApplyAuthorizedBatch(ctx, authority, trades, "phase2-batch", "phase2-batch")
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range replay {
		if result.Accepted {
			t.Fatal(replay)
		}
	}
	aggregate, err := r.AggregateWindow(ctx, "phase2-batch", "999999999999999999", observed, 86400)
	if err != nil || aggregate.VenueVolumeRaw != volume.String() || aggregate.PoolFillCount != int64(len(trades)) {
		t.Fatal(aggregate, volume, err)
	}
	for _, table := range []string{"trade_facts", "inbox", "outbox"} {
		if count(t, ctx, r, table) != len(trades) {
			t.Fatal(table)
		}
	}
	var mismatches int
	err = r.pool.QueryRow(ctx, "SELECT count(*) FROM "+r.schema+".minute_buckets b WHERE (b.volume,b.buys,b.sells,b.revision) IS DISTINCT FROM (SELECT sum(t.volume),count(*) FILTER(WHERE side='buy'),count(*) FILTER(WHERE side='sell'),count(*) FROM "+r.schema+".trade_facts t WHERE t.pool=b.pool AND t.projection_version=b.projection_version AND t.block_timestamp/60*60=b.start)").Scan(&mismatches)
	if err != nil || mismatches != 0 {
		t.Fatal(mismatches, err)
	}
	if _, err = r.ApplyQualifiedBatchTx(context.Background(), nil, nil); err == nil {
		t.Fatal("missing owned transaction accepted")
	}
}
