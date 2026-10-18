package l16

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
)

// These domain values exercise mapping and PostgreSQL atomicity, not receipt or
// movement authority. No test implementation of FreshBaselineAuthority exists.
func inventoryWireFixture(t *testing.T, log uint32, side string) (accounting.FillV1, accounting.PublicationV1) {
	t.Helper()
	wallet := "0x" + strings.Repeat("5", 40)
	fill := accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: "100", TransactionIndex: "0", LogIndex: strconv.FormatUint(uint64(log), 10), TransactionHash: "0x" + strings.Repeat("a", 64), BlockHash: "0x" + strings.Repeat("b", 64)}, ActualEmitter: "0x" + strings.Repeat("1", 40), PoolAddress: "0x" + strings.Repeat("2", 40), Token: accounting.Asset{Address: "0x" + strings.Repeat("3", 40), Decimals: 18}, Settlement: accounting.Asset{Address: "0x" + strings.Repeat("4", 40), Decimals: 6}, Side: side, TokenAmountRaw: "10", USDLAmountRaw: "100", PaidInputRaw: "100", FeeRaw: "0", InputDerivationQuality: "receipt_incomplete", FeeDisposition: "distributed_usdl", Attribution: accounting.Attribution{TransactionSender: wallet, Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Execution: accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Provenance: accounting.Provenance{DeploymentID: "domain-fixture", ABIRevision: "v1", EmitterCodeEpoch: "epoch1", PoolCodeEpoch: "epoch1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}, BlockTimestampSeconds: "1700000000"}
	if side == "sell" {
		fill.TokenAmountRaw = "5"
		fill.PaidInputRaw = "5"
		fill.USDLAmountRaw = "75"
		fill.FeeDisposition = "retained_token_reserve"
	}
	fill.SourceFillID = accounting.SourceFillIdentity(fill.Position, fill.Provenance.DeploymentID)
	digest, err := accounting.FillDigest(fill)
	if err != nil {
		t.Fatal(err)
	}
	attribution := accounting.Attribution{TransactionSender: wallet, Payer: &wallet, Recipient: &wallet, Trader: &wallet, Quality: "exact_receipt", Evidence: []accounting.EvidenceRef{{Kind: "domain-fixture", ArtifactID: "not-a-receipt-claim", ArtifactSHA256: strings.Repeat("e", 64)}}}
	enrichment := accounting.EnrichmentV1{SourceFillID: fill.SourceFillID, SourceFactDigest: digest, Facet: "inventory", EnrichmentVersion: "attribution-v1", ProjectionVersion: "inventory-v1", EvidenceDigest: accounting.Digest(strings.Repeat("f", 64)), Attribution: &attribution}
	body, err := json.Marshal(enrichment)
	if err != nil {
		t.Fatal(err)
	}
	partition := "125:" + wallet + ":" + fill.Token.Address
	position := accounting.PartitionPosition{ProjectionVersion: "inventory-v1", PartitionKind: "inventory", PartitionKey: partition, Sequence: "1", SourceEventID: fill.SourceFillID, EffectIdentity: accounting.EffectIdentity(fill.SourceFillID, "inventory", "inventory-v1", enrichment.EvidenceDigest, "inventory", partition), PayloadDigest: accounting.Digest(delivery.Hash(body)), SourceOrder: fill.Position}
	publication, err := accounting.NewEnrichmentPublication(enrichment, []accounting.PartitionPosition{position})
	if err != nil {
		t.Fatal(err)
	}
	return fill, publication
}

func TestInventoryLegMapsActualSettlementAndRejectsUnavailableOwnership(t *testing.T) {
	for _, side := range []string{"buy", "sell"} {
		fill, p := inventoryWireFixture(t, 1, side)
		leg, err := InventoryLeg(fill, p)
		if err != nil || leg.Kind != side || *leg.ActualUSDL != fill.USDLAmountRaw || leg.Identity.Emitter != fill.ActualEmitter || leg.Quantity != fill.TokenAmountRaw {
			t.Fatal(leg, err)
		}
		altered := fill
		altered.USDLAmountRaw = "101"
		if _, err = InventoryLeg(altered, p); err == nil {
			t.Fatal("different immutable source accepted")
		}
	}
	fill, p := inventoryWireFixture(t, 1, "buy")
	other := "0x" + strings.Repeat("6", 40)
	p.Enrichment.Attribution.Trader = &other
	// Rebind every immutable identity so this is an actual unsupported ownership
	// case, rather than merely a hash tampering test.
	raw, _ := json.Marshal(p.Enrichment)
	p.Positions[0].PayloadDigest = accounting.Digest(delivery.Hash(raw))
	if _, err := InventoryLeg(fill, p); !errors.Is(err, ErrCostAllocationUnavailable) {
		t.Fatal(err)
	}
	market := accounting.PublicationV1{Facet: "market"}
	if _, err := InventoryLeg(fill, market); err == nil {
		t.Fatal("market cast into inventory")
	}
}

func TestInventoryConsumerRequiresRealBaselineIssuer(t *testing.T) {
	consumer := &AccountingConsumer{}
	if err := consumer.Consume(context.Background(), accounting.InventorySubject, nil, nil); !errors.Is(err, ErrBaselineUnavailable) {
		t.Fatal(err)
	}
	if err := consumer.Bootstrap(context.Background(), delivery.BootstrapInput{}); !errors.Is(err, ErrBaselineUnavailable) {
		t.Fatal(err)
	}
}

func TestPostgresOwnedGenerationMappingAtomicity(t *testing.T) {
	dsn := os.Getenv("L16_TEST_DSN")
	if dsn == "" {
		t.Fatal("L16_TEST_DSN required for real PostgreSQL qualification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	schema := fmt.Sprintf("l16_owned_%d", time.Now().UnixNano())
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		if _, err := conn.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	if _, err = conn.Exec(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, DDL+OwnedDDL); err != nil {
		t.Fatal(err)
	}
	fill, buy := inventoryWireFixture(t, 1, "buy")
	buyLeg, err := InventoryLeg(fill, buy)
	if err != nil {
		t.Fatal(err)
	}
	guard := func(p accounting.PartitionPosition, generation string) delivery.GuardedPosition {
		return delivery.GuardedPosition{Lease: delivery.PartitionLease{Key: delivery.ConsumerKey{ConsumerID: "pnl", ProjectionGeneration: generation, PartitionKind: "inventory", PartitionKey: p.PartitionKey}, Owner: "worker", Epoch: 1}, Position: p}
	}
	apply := func(generation string, p accounting.PartitionPosition, leg Leg, commit bool) (Inventory, bool, error) {
		tx, err := conn.Begin(ctx)
		if err != nil {
			return Inventory{}, false, err
		}
		defer inventoryRollback(tx)
		state, inserted, err := ApplyOwnedLegTx(ctx, tx, guard(p, generation), leg, generation)
		if err == nil && commit {
			err = tx.Commit(ctx)
		}
		return state, inserted, err
	}
	for _, generation := range []string{"g1", "g2"} {
		for i := 0; i < 100; i++ {
			state, inserted, err := apply(generation, buy.Positions[0], buyLeg, true)
			if err != nil || inserted != (i == 0) || state.Known != "10" || state.Cost != "100" {
				t.Fatal(generation, i, state, inserted, err)
			}
		}
	}
	sellFill, sell := inventoryWireFixture(t, 2, "sell")
	sell.Positions[0].Sequence = "2"
	sell.Positions[0].PreviousEffectIdentity = buy.Positions[0].EffectIdentity
	sellLeg, err := InventoryLeg(sellFill, sell)
	if err != nil {
		t.Fatal(err)
	}
	state, _, err := apply("g1", sell.Positions[0], sellLeg, false)
	if err != nil || state.Known != "5" || state.Cost != "50" || state.Realized != "25" {
		t.Fatal(state, err)
	}
	var count int
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_effects").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if _, err = conn.Exec(ctx, "UPDATE inventory_l16_effects SET mapping_digest=$1 WHERE projection_generation='g1'", strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = apply("g1", sell.Positions[0], sellLeg, true); !errors.Is(err, delivery.ErrIntegrity) {
		t.Fatal("corrupt predecessor mapping accepted", err)
	}
	if _, _, err = apply("g2", sell.Positions[0], sellLeg, true); err != nil {
		t.Fatal(err)
	}
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_inbox").Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM inventory_l16_outbox").Scan(&count); err != nil || count != 3 {
		t.Fatal(count, err)
	}
}

func TestPortfolioBatchUsesComputedInventoryAndUnavailableMarks(t *testing.T) {
	fill, publication := inventoryWireFixture(t, 1, "buy")
	leg, err := InventoryLeg(fill, publication)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := Apply(Empty(), leg)
	if err != nil {
		t.Fatal(err)
	}
	row := portfolioRow{LogicalPartition: publication.Positions[0].PartitionKey, Revision: 1, Inventory: inventory, Position: publication.Positions[0], Fill: fill}
	update, watermark, occurred, err := portfolioBatch("g1", leg.Wallet, publication.Positions[0].EffectIdentity, []portfolioRow{row})
	if err != nil {
		t.Fatal(err)
	}
	if !update.CompleteBatch || update.SnapshotId != update.Summary.SnapshotId || string(update.SummaryRevision) != "1" || len(update.Changes) != 1 || update.Changes[0].Position.KnownRemainingCostUsdlRaw != "100" || update.Changes[0].Position.BalanceRaw != "10" {
		t.Fatal(update)
	}
	if update.Summary.CompleteValueUsdlRaw != nil || update.Changes[0].Position.MarkPrice != nil || update.Changes[0].Position.UnrealizedPnlRaw != nil || update.Summary.UnpricedAssetCount != 1 || update.Summary.PricingQuality != "unpriced" {
		t.Fatal("invented marked value", update)
	}
	payload, err := json.Marshal(update)
	if err != nil {
		t.Fatal(err)
	}
	watermarkRaw, err := json.Marshal(watermark)
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(map[string]any{"eventId": publication.Positions[0].EffectIdentity + "_" + leg.Wallet, "topic": "private.portfolio." + leg.Wallet, "sequence": "1", "revision": "1", "occurredAt": occurred, "watermark": json.RawMessage(watermarkRaw), "payload": json.RawMessage(payload), "coalesced": false, "sourceSequenceFirst": nil, "sourceSequenceLast": nil})
	if err != nil {
		t.Fatal(err)
	}
	if err = interfaces.ValidateJSON("PortfolioEvent", event); err != nil {
		t.Fatal(err)
	}
	duplicate := row
	if _, _, _, err = portfolioBatch("g1", leg.Wallet, publication.Positions[0].EffectIdentity, []portfolioRow{row, duplicate}); err == nil {
		t.Fatal("duplicate position accepted")
	}
	tooMany := make([]portfolioRow, 101)
	if _, _, _, err = portfolioBatch("g1", leg.Wallet, publication.Positions[0].EffectIdentity, tooMany); err == nil {
		t.Fatal("partial coherent full-portfolio claim accepted")
	}
}
