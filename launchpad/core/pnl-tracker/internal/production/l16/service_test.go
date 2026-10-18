package l16

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNormalizeExternalAcquisitionAndSplitBeneficiary(t *testing.T) {
	leg := testLeg(1)
	wallet, token := leg.Wallet, leg.Token
	other := "0x" + strings.Repeat("6", 40)
	m := movement{TransactionHash: leg.Identity.Transaction, LogIndex: 1, Height: 100, BlockHash: "0x" + strings.Repeat("b", 64), From: other, To: wallet, Amount: "5"}
	record, include, err := normalizeMovement(wallet, token, m, nil, time.Unix(1700000000, 0), strings.Repeat("c", 64))
	if err != nil || !include {
		t.Fatal(record, include, err)
	}
	state, err := Apply(Empty(), record.Leg)
	if err != nil || state.Unknown != "5" || state.Known != "0" || state.Cost != "0" {
		t.Fatal(state, err)
	}
	credit := "20"
	state, allocation, err := Dispose(state, "5", &credit)
	if err != nil || allocation.UnknownProceeds != "20" || state.CompleteRealized() != nil {
		t.Fatal(state, allocation, err)
	}
	settlement := receiptSettlement{fill: accounting.FillV1{SourceFillID: "domain-arithmetic", Side: "buy", USDLAmountRaw: "100", FeeRaw: "1", Settlement: accounting.Asset{Address: other, Decimals: 6}}, tokenMovement: m, payer: other, recipient: wallet, digest: strings.Repeat("d", 64)}
	record, include, err = normalizeMovement(wallet, token, m, &settlement, time.Unix(1700000000, 0), strings.Repeat("c", 64))
	if err != nil || !include || record.Leg.ActualUSDL != nil || record.Payer != other || record.Recipient != wallet {
		t.Fatal(record, include, err)
	}
	if _, include, err = normalizeMovement(other, token, movement{TransactionHash: m.TransactionHash, LogIndex: m.LogIndex, Height: m.Height, BlockHash: m.BlockHash, From: zeroAddress, To: wallet, Amount: m.Amount}, nil, time.Unix(1700000000, 0), strings.Repeat("c", 64)); err != nil || include {
		t.Fatal("payer credited without a token movement", include, err)
	}
}

func TestExactReaderPreservesNullZeroStaleAndResidual(t *testing.T) {
	wallet := "0x" + strings.Repeat("5", 40)
	token := "0x" + strings.Repeat("3", 40)
	hash := "0x" + strings.Repeat("b", 64)
	state := Empty()
	state.Known = "3000000000000000000"
	state.Cost = "1"
	watermark := l17.Watermark{ChainID: "125", Height: "100", BlockHash: hash, ProjectionRevision: "2", ObservedAt: "2026-10-12T00:00:00.000Z", Integrity: "healthy"}
	p, err := ProjectPosition(wallet, token, state, watermark, nil, "unpriced")
	if err != nil || p.UnrealizedPnlRaw != nil {
		t.Fatal(p, err)
	}
	chain, currency, usdl, event := "125", "USDL", "0x85FcD13735F4309833A503EE804ea32395851479", "event1"
	decimals, quoteDecimals := 18, 6
	price := &l17.Price{Numerator: "0", Denominator: "1", BaseAsset: l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &token, Decimals: &decimals}, QuoteAsset: l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &usdl, CurrencyCode: &currency, Decimals: &quoteDecimals}, Source: l17.PriceSource{Kind: "post_swap_spot", AmountBasis: "effective_reserves", SourceEventID: &event, Position: &l17.ChainPosition{ChainID: "125", Height: "100", TransactionIndex: "0", LogIndex: "0", TransactionHash: hash, BlockHash: hash}, ObservedAt: watermark.ObservedAt}}
	p, err = ProjectPosition(wallet, token, state, watermark, price, "priced")
	if err != nil || p.UnrealizedPnlRaw == nil || *p.UnrealizedPnlRaw != "-1" {
		t.Fatal(p, err)
	}
	p, err = ProjectPosition(wallet, token, state, watermark, price, "stale")
	if err != nil || p.UnrealizedPnlRaw != nil || p.MarkPrice == nil {
		t.Fatal(p, err)
	}
	state.Unknown = "1"
	p, err = ProjectPosition(wallet, token, state, watermark, price, "priced")
	if err != nil || p.RemainingCostUsdlRaw != nil || p.UnrealizedPnlRaw != nil {
		t.Fatal(p, err)
	}
}

func TestPostgresReplayActivationArchiveOutboxAtomicity(t *testing.T) {
	dsn := os.Getenv("L16_TEST_DSN")
	if dsn == "" {
		t.Fatal("L16_TEST_DSN required for real PostgreSQL qualification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := fmt.Sprintf("l16_replay_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	ddl, err := delivery.SchemaDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SET LOCAL search_path TO "+quoted); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, RebuildDDL); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	outbox, err := delivery.NewStore(pool, schema, delivery.Policy{Subjects: map[string]bool{RebuildSubject: true}, EventTypes: map[string]bool{RebuildSubject: true}})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{authority: &BaselineAuthority{pool: pool, schema: quoted, generation: "replay_v2"}, outbox: outbox}
	leg := testLeg(1)
	state, err := Apply(Empty(), leg)
	if err != nil {
		t.Fatal(err)
	}
	decimals := 18
	record := EconomicRecord{TimedLeg: TimedLeg{Leg: leg, CommittedAt: time.Date(2026, 10, 11, 23, 59, 59, 0, time.UTC)}, SourceDigest: strings.Repeat("a", 64), State: state, BlockHash: "0x" + strings.Repeat("b", 64), TokenDecimals: &decimals}
	cp, err := RebuildDay(ctx, Empty(), []TimedLeg{record.TimedLeg}, record.CommittedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	recordBytes, err := json.Marshal([]EconomicRecord{record})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := RebuildSnapshot{LastIdentity: cp.LastIdentity, InputDigest: delivery.Hash(recordBytes), Generation: "replay_v2", Partition: "125:" + leg.Wallet + ":" + leg.Token, Algorithm: Algorithm, ReleaseDigest: strings.Repeat("c", 64), Height: 100, BlockHash: "0x" + strings.Repeat("b", 64), State: state, Checkpoints: []Checkpoint{cp}, ObservedThrough: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}
	for i := 0; i < 100; i++ {
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = service.persistReplayTx(ctx, tx, snapshot, []EconomicRecord{record}); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"inventory_l16_archive_v2", "inventory_l16_snapshots_v2", "inventory_l16_active_v2", "inventory_l16_checkpoints_v2", "delivery_outbox"} {
		var count int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+quoted+"."+table).Scan(&count); err != nil || count != 1 {
			t.Fatal(table, count, err)
		}
	}
	observed, err := service.Active(ctx, leg.Wallet, leg.Token)
	if err != nil || observed.State != state {
		t.Fatal(observed, err)
	}
	history, err := service.HistoricalDay(ctx, leg.Wallet, leg.Token, record.CommittedAt)
	if err != nil || history.State != state {
		t.Fatal(history, err)
	}
	if _, err = service.HistoricalDay(ctx, leg.Wallet, leg.Token, snapshot.ObservedThrough); err == nil {
		t.Fatal("partial current day used as complete history")
	}
	changed := snapshot
	changed.Height = 101
	changed.State.Cost = "200"
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.persistReplayTx(ctx, tx, changed, []EconomicRecord{record}); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	observed, err = service.Active(ctx, leg.Wallet, leg.Token)
	if err != nil || observed.Height != 100 || observed.State.Cost != "100" {
		t.Fatal(observed, err)
	}
	record.State.Cost = "999"
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.persistReplayTx(ctx, tx, snapshot, []EconomicRecord{record}); err == nil {
		tx.Rollback(ctx)
		t.Fatal("immutable archive changed")
	}
	tx.Rollback(ctx)
	var payload []byte
	if err = pool.QueryRow(ctx, "SELECT payload FROM "+quoted+".inventory_l16_snapshots_v2").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var persisted RebuildSnapshot
	if err = json.Unmarshal(payload, &persisted); err != nil || persisted.State.Cost != "100" {
		t.Fatal(persisted, err)
	}
	record.State = state
	recovered := snapshot
	recovered.SourceOwner = "restarted-owner"
	for i, persist := range []func(context.Context, pgx.Tx, RebuildSnapshot, []EconomicRecord) error{service.persistReplayTx, service.livePersistReplayTx} {
		recovered.SourceEpoch = int64(i + 2)
		tx, err = pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = persist(ctx, tx, recovered, []EconomicRecord{record}); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		observed, err = service.Active(ctx, leg.Wallet, leg.Token)
		if err != nil || observed.State != state || observed.SourceEpoch != recovered.SourceEpoch {
			t.Fatal(observed, err)
		}
		for _, mutate := range []func(*RebuildSnapshot){
			func(s *RebuildSnapshot) { s.MovementDigest = strings.Repeat("e", 64) },
			func(s *RebuildSnapshot) { s.InputDigest = strings.Repeat("f", 64) },
			func(s *RebuildSnapshot) { s.CreationHeight++ },
		} {
			conflicting := recovered
			mutate(&conflicting)
			tx, err = pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = persist(ctx, tx, conflicting, []EconomicRecord{record}); err == nil {
				tx.Rollback(ctx)
				t.Fatal("same-height economic proof changed")
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	for table, want := range map[string]int{"inventory_l16_archive_v2": 1, "inventory_l16_snapshots_v2": 3, "inventory_l16_active_v2": 1, "delivery_outbox": 3} {
		var count int
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+quoted+"."+table).Scan(&count); err != nil || count != want {
			t.Fatal(table, count, err)
		}
	}
	t.Logf("same-height recovery state=%+v epoch=%d archive=1 snapshots=3 active=1 durable_outbox=3", observed.State, observed.SourceEpoch)
	futureLeg := testLeg(2)
	futureLeg.Height = 101
	futureLeg.Identity.Transaction = "0x" + strings.Repeat("9", 64)
	futureLeg.Kind, futureLeg.ActualUSDL = "incoming", nil
	futureLeg.Predecessor = snapshot.LastIdentity
	futureState, err := Apply(state, futureLeg)
	if err != nil {
		t.Fatal(err)
	}
	future := EconomicRecord{TimedLeg: TimedLeg{Leg: futureLeg, CommittedAt: snapshot.ObservedThrough.Add(time.Second)}, BlockHash: "0x" + strings.Repeat("d", 64), TokenDecimals: &decimals, SourceDigest: strings.Repeat("e", 64), State: futureState}
	futureID, err := futureLeg.Identity.Key()
	if err != nil {
		t.Fatal(err)
	}
	futureRaw, err := json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, "INSERT INTO "+quoted+".inventory_l16_archive_v2 VALUES($1,$2,$3,$4,$5)", snapshot.Generation, snapshot.Partition, futureID, delivery.Hash(futureRaw), futureRaw); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := service.readReplayArchiveTx(ctx, tx, leg.Wallet, snapshot)
	if err != nil || len(selected) != 1 || selected[0].State != state {
		tx.Rollback(ctx)
		t.Fatal(selected, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	t.Logf("snapshot-height=%d archive-contains-height=101 selected-state=%+v selected-records=%d", snapshot.Height, selected[0].State, len(selected))
}

func TestDenseUTCCheckpointPagesPreserveCanonicalFold(t *testing.T) {
	day := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	legs := make([]TimedLeg, 10001)
	last := ""
	for i := range legs {
		leg := testLeg(uint32(i + 1))
		leg.Quantity = "1"
		cost := "1"
		leg.ActualUSDL = &cost
		leg.Predecessor = last
		var err error
		last, err = leg.Identity.Key()
		if err != nil {
			t.Fatal(err)
		}
		legs[i] = TimedLeg{Leg: leg, CommittedAt: day.Add(time.Second)}
	}
	cp, err := RebuildDayPages(context.Background(), Checkpoint{}, legs, day)
	if err != nil || cp.State.Known != "10001" || cp.State.Cost != "10001" || cp.LastIdentity != last || !cp.CompleteHistory {
		t.Fatal(cp, err)
	}
	if _, err = RebuildDay(context.Background(), Empty(), legs, day, true); err == nil {
		t.Fatal("frozen single-page boundary accepted dense day")
	}
	boundary := testLeg(10002)
	boundary.Predecessor = last
	cost := "1"
	boundary.Quantity = "1"
	boundary.ActualUSDL = &cost
	next, err := RebuildDayPages(context.Background(), cp, []TimedLeg{{Leg: boundary, CommittedAt: day.AddDate(0, 0, 1)}}, day.AddDate(0, 0, 1))
	if err != nil || next.State.Known != "10002" || cp.State.Known != "10001" {
		t.Fatal(cp, next, err)
	}
}

func TestIntervalRealizedSeparatesUTCUnknownBasisAndFeeAssets(t *testing.T) {
	day := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	buy := testLeg(1)
	cost := "100"
	buy.Quantity = "10"
	buy.ActualUSDL = &cost
	state, err := Apply(Empty(), buy)
	if err != nil {
		t.Fatal(err)
	}
	incoming := testLeg(2)
	incoming.Kind = "incoming"
	incoming.Quantity = "10"
	incoming.ActualUSDL = nil
	state, err = Apply(state, incoming)
	if err != nil {
		t.Fatal(err)
	}
	sale := testLeg(3)
	sale.Kind = "sell"
	sale.Quantity = "10"
	proceeds := "150"
	sale.ActualUSDL = &proceeds
	sale.Payer = sale.Wallet
	sale.Recipient = sale.Wallet
	state, allocation, err := Dispose(state, sale.Quantity, sale.ActualUSDL)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := intervalRecords([]EconomicRecord{{TimedLeg: TimedLeg{Leg: buy, CommittedAt: day.Add(-time.Second)}, FeeAsset: &accounting.Asset{Address: buy.Token, Decimals: 18}, FeeRaw: "1"}, {TimedLeg: TimedLeg{Leg: sale, CommittedAt: day}, Allocation: &allocation, State: state, FeeAsset: &accounting.Asset{Address: sale.Token, Decimals: 18}, FeeRaw: "1"}}, 24*time.Hour)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	got := rows[1]
	if !got.Start.Equal(day) || got.GrossProceedsUsdlRaw != "150" || got.KnownAllocatedBasisUsdlRaw != "50" || got.KnownNetGainUsdlRaw != "25" || got.UnknownProceedsUsdlRaw != "75" || got.AccountingComplete || got.RealizedPnlUsdlRaw != nil || got.TradeCount != 1 || got.SaleCount != 1 || len(got.Fees) != 1 || got.Fees[0].AmountRaw != "1" {
		t.Fatal(got)
	}
}

func TestPagedCheckpointPreservesIncompleteHistoryAndRejectsForgedFrontier(t *testing.T) {
	day := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	leg := testLeg(1)
	partial, err := RebuildDayPages(context.Background(), Checkpoint{State: Empty(), CompleteHistory: false}, []TimedLeg{{Leg: leg, CommittedAt: day}}, day)
	if err != nil || partial.CompleteHistory {
		t.Fatal(partial, err)
	}
	next, err := RebuildDayPages(context.Background(), partial, nil, day.AddDate(0, 0, 1))
	if err != nil || next.CompleteHistory || next.State != partial.State {
		t.Fatal(next, err)
	}
	for _, mutate := range []func(*Checkpoint){
		func(cp *Checkpoint) { cp.Partition = "125:bad:bad" },
		func(cp *Checkpoint) { cp.LastIdentity = "invented" },
		func(cp *Checkpoint) { cp.LastHeight = 0 },
		func(cp *Checkpoint) { cp.LastLogIndex++ },
	} {
		forged := partial
		mutate(&forged)
		if _, err = RebuildDayPages(context.Background(), forged, nil, day.AddDate(0, 0, 1)); err == nil {
			t.Fatal("forged frontier accepted", forged)
		}
	}
}

func TestIntervalReplayDeduplicatesGasAndRejectsDisagreement(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	first := testLeg(1)
	first.Sender = first.Wallet
	second := testLeg(2)
	second.Sender = second.Wallet
	second.Token = "0x" + strings.Repeat("7", 40)
	a := EconomicRecord{TimedLeg: TimedLeg{Leg: first, CommittedAt: at}, BlockHash: "0x" + strings.Repeat("b", 64), GasPAXRaw: "21000"}
	b := EconomicRecord{TimedLeg: TimedLeg{Leg: second, CommittedAt: at}, BlockHash: a.BlockHash, GasPAXRaw: a.GasPAXRaw}
	rows, err := intervalRecords([]EconomicRecord{b, a, a}, time.Hour)
	if err != nil || len(rows) != 1 || rows[0].GasPaxRaw != "21000" || rows[0].TradeCount != 2 {
		t.Fatal(rows, err)
	}
	for _, mutate := range []func(*EconomicRecord){
		func(r *EconomicRecord) { r.GasPAXRaw = "21001" },
		func(r *EconomicRecord) { r.BlockHash = "0x" + strings.Repeat("c", 64) },
		func(r *EconomicRecord) { r.CommittedAt = r.CommittedAt.Add(time.Hour) },
	} {
		changed := b
		mutate(&changed)
		if _, err = intervalRecords([]EconomicRecord{a, changed}, time.Hour); err == nil {
			t.Fatal("receipt disagreement accepted", changed)
		}
	}
	changed := a
	changed.Leg.Quantity = "11"
	if _, err = intervalRecords([]EconomicRecord{a, changed}, time.Hour); err == nil {
		t.Fatal("conflicting duplicate accepted")
	}
	if _, err = intervalRecords([]EconomicRecord{a}, 0); err == nil {
		t.Fatal("zero interval accepted")
	}
}

func TestEconomicArchiveReplayRejectsForgedDisposalAllocation(t *testing.T) {
	prior, err := Acquire(Empty(), "10", str("100"))
	if err != nil {
		t.Fatal(err)
	}
	prior, err = Acquire(prior, "10", nil)
	if err != nil {
		t.Fatal(err)
	}
	leg := testLeg(3)
	leg.Kind, leg.Payer, leg.Recipient = "sell", leg.Wallet, leg.Wallet
	leg.ActualUSDL = str("150")
	state, allocation, err := Dispose(prior, leg.Quantity, leg.ActualUSDL)
	if err != nil {
		t.Fatal(err)
	}
	record := EconomicRecord{TimedLeg: TimedLeg{Leg: leg}, State: state, Allocation: &allocation}
	observed, err := replayEconomicRecord(prior, record)
	if err != nil || observed != state {
		t.Fatal(observed, err)
	}
	for _, mutate := range []func(*Disposal){
		func(a *Disposal) { a.Cost = "0" },
		func(a *Disposal) { a.Known = "10"; a.Unknown = "0" },
		func(a *Disposal) { a.KnownProceeds = "150"; a.UnknownProceeds = "0" },
	} {
		forged := allocation
		mutate(&forged)
		record.Allocation = &forged
		if _, err = replayEconomicRecord(prior, record); err == nil {
			t.Fatal("forged allocation accepted", forged)
		}
	}
}
