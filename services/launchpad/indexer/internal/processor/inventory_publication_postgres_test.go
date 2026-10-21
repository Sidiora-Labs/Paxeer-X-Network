package processor

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest/chainfixture"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/runtimedocs"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store/canonicaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pnl's inventory consumer identities (core/pnl-tracker delivery document).
const (
	pnlGeneration        = "g1"
	pnlInventoryVersion  = "inventory-v1"
	pnlConsumerID        = "pnl"
	pnlDeliverySchema    = "pnl"
	inventoryTestOwner   = "indexer-test"
	inventoryLeaseHolder = "pnl-1"
)

type inventoryHarness struct {
	pool   *pgxpool.Pool
	proc   *Processor
	height *atomic.Uint64
}

// newInventoryHarness wires the production qualified ingestion path exactly as
// app.New does for a delivery document with generation g1 and
// inventoryProjectionVersion inventory-v1: the durable store and stager approve
// accounting.inventory.v1 next to the market, creation and replay subjects, and
// the pnl schema (migrations 160000/160001 plus pnl's delivery tables) holds
// the baselines pnl issues and the ordered partitions pnl consumes into.
func newInventoryHarness(t *testing.T, ctx context.Context) inventoryHarness {
	t.Helper()
	return newInventoryHarnessWithWait(t, ctx, 0)
}

// newInventoryHarnessWithWait sets Deps.InventoryBaselineWaitBlocks (zero:
// the production bound).
func newInventoryHarnessWithWait(t *testing.T, ctx context.Context, wait int64) inventoryHarness {
	t.Helper()
	_, pool := canonicaltest.New(t)
	applyProductionSchema(t, ctx, pool)
	ddl := []string{`CREATE SCHEMA IF NOT EXISTS "pnl"`}
	for _, id := range []string{"160000", "160001"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(), "spec", "production-system", "integration", "migrations", id+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		ddl = append(ddl, string(raw))
	}
	for _, build := range []func(string) (string, error){delivery.SchemaDDL, delivery.PartitionSchemaDDL} {
		sql, err := build(pnlDeliverySchema)
		if err != nil {
			t.Fatal(err)
		}
		ddl = append(ddl, sql)
	}
	for _, statement := range ddl {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("pnl schema: %v", err)
		}
	}
	url := chainfixture.New(t).Serve(t)
	options, _, err := runtimedocs.ReceiptOptions(realReceiptInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	client, closeClient, err := receipt.NewBoundedRPCClient(ctx, url, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeClient)
	authority, err := receipt.NewRPCQualification(ctx, client, options)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := runtimedocs.CommittedOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	committed.Endpoints[runtimedocs.PrimaryEndpointID] = url
	rpcSource, err := source.NewCommittedSource(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rpcSource.Close() })
	guard, err := queue.NewEpochGuard("recovery", 1, "railway-prod")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.NewLifecycleStore(pool, guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	policy := delivery.Policy{
		Subjects:   map[string]bool{publisher.CanonicalCreationSubject: true, accounting.MarketSubject: true, accounting.InventorySubject: true, ReplaySubjectV2: true},
		EventTypes: map[string]bool{publisher.CanonicalCreationEventType: true, accounting.MarketSubject: true, accounting.InventorySubject: true, ReplaySubjectV2: true},
	}
	durable, err := delivery.NewStoreWithEpochGuard(pool, "indexer", policy, guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	partitions, err := delivery.NewPartitionStore(pool, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	stager, err := publisher.NewDurableStager(durable, options.Manifest.DeploymentVersion, map[string]string{publisher.CanonicalCreationEventType: publisher.CanonicalCreationSubject, accounting.MarketSubject: accounting.MarketSubject, accounting.InventorySubject: accounting.InventorySubject})
	if err != nil {
		t.Fatal(err)
	}
	height := &atomic.Uint64{}
	height.Store(realAnchor.Height)
	live := &source.LiveQualifiedSource{RPC: rpcSource, Authority: authority, DurableHeight: height.Load, PriorHash: func(c context.Context, h uint64) (common.Hash, error) {
		if h == realAnchor.Height {
			return realAnchor.Hash, nil
		}
		var hash string
		err := pool.QueryRow(c, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2`, realStream, int64(h)).Scan(&hash)
		return common.HexToHash(hash), err
	}}
	p := New(Deps{Store: st, Decoder: decode.NewDecoder(), EventEmitterAddress: deployment.Current().Roles["EventEmitter"].Address, ChainID: 125, QualifiedBatchSource: live, CanonicalStreamID: realStream, CanonicalOwner: inventoryTestOwner, AccountingPartitions: partitions, Durable: stager, AccountingProjectionVersion: "accounting-market-v1", InventoryGeneration: pnlGeneration, InventoryProjectionVersion: pnlInventoryVersion, InventoryBaselineWaitBlocks: wait, LegacyProjection: true, Reserves: authority})
	return inventoryHarness{pool: pool, proc: p, height: height}
}

func (h inventoryHarness) batch(t *testing.T, ctx context.Context, from, to int64) {
	t.Helper()
	if err := h.proc.ProcessBatch(ctx, from, to); err != nil {
		t.Fatalf("batch %d..%d: %v", from, to, err)
	}
	h.height.Store(uint64(to))
}

type recordedFill struct {
	fill   accounting.FillV1
	digest string
}

func (h inventoryHarness) fills(t *testing.T, ctx context.Context) []recordedFill {
	t.Helper()
	rows, err := h.pool.Query(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 ORDER BY (payload->'position'->>'height')::bigint,(payload->'position'->>'transactionIndex')::bigint,(payload->'position'->>'logIndex')::bigint`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []recordedFill{}
	for rows.Next() {
		var raw []byte
		var digest string
		if err = rows.Scan(&raw, &digest); err != nil {
			t.Fatal(err)
		}
		fill, err := accounting.DecodeFillV1(raw)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, recordedFill{fill: fill, digest: digest})
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

type outboxFrame struct {
	subject string
	wire    []byte
}

func (h inventoryHarness) frames(t *testing.T, ctx context.Context, subject string) []outboxFrame {
	t.Helper()
	rows, err := h.pool.Query(ctx, `SELECT subject,envelope FROM indexer.delivery_outbox WHERE subject=$1 ORDER BY created_at,event_id`, subject)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []outboxFrame{}
	for rows.Next() {
		var f outboxFrame
		if err = rows.Scan(&f.subject, &f.wire); err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestInventoryEnrichmentWithoutIssuedBaselinePublishesMarketOnlyPostgres runs
// the real market with inventory enrichment enabled and no pnl baseline: the
// canonical commit must not halt, every fill keeps its market frame for charts
// and stats, and no accounting.inventory.v1 frame is produced.
func TestInventoryEnrichmentWithoutIssuedBaselinePublishesMarketOnlyPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newInventoryHarness(t, ctx)
	for _, r := range [][2]int64{{33963716, 33963716}, {33963717, 33963733}, {33963734, 33963749}} {
		h.batch(t, ctx, r[0], r[1])
	}
	fills := h.fills(t, ctx)
	if len(fills) != 2 {
		t.Fatalf("fills %d", len(fills))
	}
	if market := h.frames(t, ctx, accounting.MarketSubject); len(market) != 2 {
		t.Fatalf("market frames %d, want 2", len(market))
	}
	if inventory := h.frames(t, ctx, accounting.InventorySubject); len(inventory) != 0 {
		t.Fatalf("inventory frames without an issued baseline: %d", len(inventory))
	}
	// Both fills are held within the production bound, not yet decided.
	if decisions := h.inventoryDecisions(t, ctx); len(decisions) != 0 {
		t.Fatalf("held fills decided within the bound: %v", decisions)
	}
}

// The real market's creator trades its own token: a router buy at 33963733
// and a router sell at 33963749, both paid to and received by the sender.
const realMarketTrader = "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154"

// pnlMovement and pnlBaselineAttestation mirror the JSON pnl's
// BaselineAuthority.Capture hashes (core/pnl-tracker internal/production/l16
// movement and baselineAttestation), so the digests read by the indexer are
// the ones pnl issues.
type pnlMovement struct {
	TransactionHash             string
	LogIndex                    uint32
	Height                      int64
	TransactionIndex            uint32
	BlockHash, From, To, Amount string
}

type pnlBaselineAttestation struct {
	ConsumerID, Generation, Partition, Wallet, Token   string
	CreationHeight                                     int64
	CreationBlockHash, Pool, SourceStream, SourceOwner string
	SourceEpoch                                        int64
	MovementDigest, ObservedBalanceRaw, CodeSHA256     string
}

type issuedBaseline struct {
	digest string
	att    pnlBaselineAttestation
}

// pnlIssuedRow is the row pnl's BaselineAuthority.Capture actually issued for
// the real market's trader on the indexer's creation stage (core/pnl-tracker
// internal/production/l16 testdata/indexer_real_market/traded.json,
// pnl.inventory_l16_baselines), bound to the source lease (indexer-test, 1).
type pnlIssuedRow struct {
	Digest      string `json:"baseline_digest"`
	Partition   string `json:"partition"`
	SourceOwner string `json:"source_owner"`
	SourceEpoch int64  `json:"source_epoch"`
	Attestation string `json:"attestation"`
	raw         []byte
	att         pnlBaselineAttestation
}

func loadPnlIssuedRow(t *testing.T) pnlIssuedRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repositoryRoot(), "core", "pnl-tracker", "internal", "production", "l16", "testdata", "indexer_real_market", "traded.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tables struct {
		Baselines []pnlIssuedRow `json:"pnl.inventory_l16_baselines"`
	}
	if err = json.Unmarshal(data, &tables); err != nil || len(tables.Baselines) != 1 || !strings.HasPrefix(tables.Baselines[0].Attestation, `\x`) {
		t.Fatalf("pnl-issued baseline row (%v)", err)
	}
	row := tables.Baselines[0]
	if row.raw, err = hex.DecodeString(row.Attestation[2:]); err != nil {
		t.Fatal(err)
	}
	if delivery.Hash(row.raw) != row.Digest {
		t.Fatalf("pnl-issued row digest %s does not hash its attestation", row.Digest)
	}
	if err = json.Unmarshal(row.raw, &row.att); err != nil {
		t.Fatal(err)
	}
	if row.att.Partition != row.Partition || row.att.SourceOwner != row.SourceOwner || row.att.SourceEpoch != row.SourceEpoch || len(row.att.CodeSHA256) != 64 {
		t.Fatalf("pnl-issued row columns differ from its attestation %+v", row.att)
	}
	return row
}

// issueBaseline writes the fresh zero-balance creation baseline row pnl's
// BaselineAuthority.Capture issues for wallet/token, bound to the indexer's
// current source lease (owner, fence) and the canonical creation facts, with
// the movement digest over the indexed creation-height transfers. CodeSHA256,
// the one RPC observation in the attestation (sha256 of eth_getCode at the
// creation block hash), is taken from the row pnl really issued; for that
// row's lease (indexer-test, 1) the rebuilt attestation must be byte-identical
// to pnl's, so the mirror cannot drift from what pnl issues.
func (h inventoryHarness) issueBaseline(t *testing.T, ctx context.Context, wallet, token string) issuedBaseline {
	t.Helper()
	issued := loadPnlIssuedRow(t)
	var att pnlBaselineAttestation
	if err := h.pool.QueryRow(ctx, `SELECT height,block_hash,pool FROM indexer.indexer_canonical_creations_v1 WHERE token=$1`, token).Scan(&att.CreationHeight, &att.CreationBlockHash, &att.Pool); err != nil {
		t.Fatalf("canonical creation: %v", err)
	}
	if err := h.pool.QueryRow(ctx, `SELECT owner_instance,fence_token FROM indexer.indexer_ingest_leases WHERE chain_id=125 AND stream_id=$1`, realStream).Scan(&att.SourceOwner, &att.SourceEpoch); err != nil {
		t.Fatalf("source lease: %v", err)
	}
	rows, err := h.pool.Query(ctx, `SELECT transaction_hash,log_index,height,transaction_index,block_hash,from_address,to_address,amount_raw FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND height=$2 ORDER BY height,transaction_index,log_index`, token, att.CreationHeight)
	if err != nil {
		t.Fatal(err)
	}
	movements := []pnlMovement{}
	for rows.Next() {
		var m pnlMovement
		var logIndex, txIndex int64
		if err = rows.Scan(&m.TransactionHash, &logIndex, &m.Height, &txIndex, &m.BlockHash, &m.From, &m.To, &m.Amount); err != nil {
			t.Fatal(err)
		}
		m.LogIndex, m.TransactionIndex = uint32(logIndex), uint32(txIndex)
		if m.From == wallet || m.To == wallet {
			t.Fatalf("pnl would refuse the baseline: %s moved at creation", wallet)
		}
		movements = append(movements, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil || len(movements) == 0 {
		t.Fatalf("creation movements unavailable (%v)", err)
	}
	raw, err := json.Marshal(movements)
	if err != nil {
		t.Fatal(err)
	}
	att.ConsumerID, att.Generation, att.Partition, att.Wallet, att.Token = pnlConsumerID, pnlGeneration, "125:"+wallet+":"+token, wallet, token
	att.SourceStream, att.MovementDigest, att.ObservedBalanceRaw = realStream, delivery.Hash(raw), "0"
	if att.Partition != issued.Partition {
		t.Fatalf("no pnl-issued code observation for %s", att.Partition)
	}
	att.CodeSHA256 = issued.att.CodeSHA256
	attestation, err := json.Marshal(att)
	if err != nil {
		t.Fatal(err)
	}
	digest := delivery.Hash(attestation)
	if att.SourceOwner == issued.SourceOwner && att.SourceEpoch == issued.SourceEpoch && (!bytes.Equal(attestation, issued.raw) || digest != issued.Digest) {
		t.Fatalf("indexer mirror %s differs from the baseline pnl issued %s", digest, issued.Digest)
	}
	if _, err = h.pool.Exec(ctx, `INSERT INTO pnl.inventory_l16_baselines(baseline_digest,consumer_id,projection_generation,partition,wallet,token,creation_height,creation_block_hash,source_stream,source_owner,source_epoch,movement_digest,observed_balance_raw,attestation) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'0',$13)`, digest, att.ConsumerID, att.Generation, att.Partition, wallet, token, att.CreationHeight, att.CreationBlockHash, att.SourceStream, att.SourceOwner, att.SourceEpoch, att.MovementDigest, attestation); err != nil {
		t.Fatal(err)
	}
	return issuedBaseline{digest: digest, att: att}
}

// consumeAsPnl applies every staged accounting.inventory.v1 frame the way
// pnl's AccountingConsumer.PrepareAndConsume does, on pnl's own ordered
// partition store: envelope decode under pnl's inventory-only policy, the
// publication's facet/projection/partition binding, the original fill read
// back from indexer_accounting_fills_v1, the same-owner leg rules, the
// published row pnl looks up by effect identity, the fresh-same-owner evidence
// pnl recomputes from the issued baseline and the canonical movements, and the
// partition bootstrap at sequence 1 followed by gap-free advancement. Each
// frame is verified against the baseline it names, pnl's rule across indexer
// fence changes: the issued row whose digest reproduces the frame's evidence,
// bound to a source epoch not beyond the current fence. The partition is
// bootstrapped from the baseline of the current source lease, as pnl's
// PreparePartition captures it. It returns the consumed publications in
// delivery order.
func (h inventoryHarness) consumeAsPnl(t *testing.T, ctx context.Context, baselines ...issuedBaseline) []accounting.PublicationV1 {
	t.Helper()
	if len(baselines) == 0 {
		t.Fatal("issued baselines required")
	}
	baseline := baselines[0]
	var leaseOwner string
	var leaseFence int64
	if err := h.pool.QueryRow(ctx, `SELECT owner_instance,fence_token FROM indexer.indexer_ingest_leases WHERE chain_id=125 AND stream_id=$1`, realStream).Scan(&leaseOwner, &leaseFence); err != nil {
		t.Fatal(err)
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.InventorySubject: true}, EventTypes: map[string]bool{accounting.InventorySubject: true}}
	partitions, err := delivery.NewPartitionStore(h.pool, pnlDeliverySchema)
	if err != nil {
		t.Fatal(err)
	}
	leases := map[string]delivery.PartitionLease{}
	consumed := []accounting.PublicationV1{}
	for _, frame := range h.frames(t, ctx, accounting.InventorySubject) {
		envelope, err := delivery.Decode(frame.wire, policy, frame.subject)
		if err != nil {
			t.Fatalf("pnl envelope decode: %v", err)
		}
		if envelope.EventType != accounting.InventorySubject || envelope.ChainID != 125 || envelope.SchemaVersion != 1 {
			t.Fatalf("envelope identity %s/%d/%d", envelope.EventType, envelope.ChainID, envelope.SchemaVersion)
		}
		publication, err := accounting.DecodePublicationV1(envelope.Payload)
		if err != nil {
			t.Fatalf("pnl publication decode: %v", err)
		}
		if publication.Facet != "inventory" || publication.Enrichment == nil || publication.Enrichment.Attribution == nil || len(publication.Positions) != 1 {
			t.Fatalf("not an inventory enrichment: %+v", publication)
		}
		position := publication.Positions[0]
		if position.ProjectionVersion != pnlInventoryVersion || position.PartitionKind != "inventory" || position.PartitionKey != baseline.att.Partition || position.SourceEventID != publication.SourceFillID {
			t.Fatalf("partition binding %+v", position)
		}
		if publication.Enrichment.ProjectionVersion != pnlInventoryVersion || publication.Enrichment.EnrichmentVersion != "fresh-same-owner-v1" {
			t.Fatalf("enrichment identity %+v", publication.Enrichment)
		}
		var raw []byte
		var stored string
		if err = h.pool.QueryRow(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE source_fill_id=$1`, publication.SourceFillID).Scan(&raw, &stored); err != nil {
			t.Fatalf("source fill: %v", err)
		}
		fill, err := accounting.DecodeFillV1(raw)
		if err != nil {
			t.Fatal(err)
		}
		digest, err := accounting.FillDigest(fill)
		if err != nil || string(digest) != stored || digest != publication.SourceFactDigest || position.SourceOrder != fill.Position {
			t.Fatalf("source fill binding digest=%s stored=%s publication=%s (%v)", digest, stored, publication.SourceFactDigest, err)
		}
		a := publication.Enrichment.Attribution
		if a.Quality == "unavailable" || a.Payer == nil || a.Recipient == nil || *a.Payer != baseline.att.Wallet || *a.Recipient != baseline.att.Wallet || a.TransactionSender != baseline.att.Wallet || a.TransactionSender != fill.Attribution.TransactionSender || a.Trader != nil && *a.Trader != baseline.att.Wallet {
			t.Fatalf("same-owner leg attribution %+v", a)
		}
		if fill.Token.Address != baseline.att.Token || fill.PoolAddress != baseline.att.Pool {
			t.Fatalf("fill outside the baseline partition")
		}
		var publishedRaw []byte
		if err = h.pool.QueryRow(ctx, `SELECT payload FROM indexer.indexer_accounting_publications_v1 WHERE payload->>'facet'='inventory' AND payload->'positions'->0->>'effectIdentity'=$1`, position.EffectIdentity).Scan(&publishedRaw); err != nil {
			t.Fatalf("published inventory row by effect identity: %v", err)
		}
		published, err := accounting.DecodePublicationV1(publishedRaw)
		if err != nil || published.PublicationID != publication.PublicationID || published.Positions[0] != position {
			t.Fatalf("published row differs from the frame (%v)", err)
		}
		height, err := fillHeight(fill)
		if err != nil {
			t.Fatal(err)
		}
		moves, err := h.pool.Query(ctx, `SELECT payload FROM indexer.indexer_token_transfers_v1 WHERE token=$1 AND height BETWEEN $2 AND $3 AND (from_address=$4 OR to_address=$4) ORDER BY height,transaction_index,log_index`, baseline.att.Token, baseline.att.CreationHeight, height, baseline.att.Wallet)
		if err != nil {
			t.Fatal(err)
		}
		payloads := []json.RawMessage{}
		for moves.Next() {
			var m []byte
			if err = moves.Scan(&m); err != nil {
				t.Fatal(err)
			}
			payloads = append(payloads, append(json.RawMessage(nil), m...))
		}
		moves.Close()
		movementBytes, err := json.Marshal(payloads)
		if err != nil {
			t.Fatal(err)
		}
		named := false
		for _, candidate := range baselines {
			if candidate.att.Partition != baseline.att.Partition || candidate.att.SourceEpoch > leaseFence {
				continue
			}
			if publication.Enrichment.EvidenceDigest == accounting.Digest(accounting.HashTuple("fresh-inventory-evidence-v1", candidate.digest, candidate.att.MovementDigest, string(digest), string(movementBytes))) {
				named = true
			}
		}
		if !named {
			t.Fatalf("evidence digest %s names no issued baseline", publication.Enrichment.EvidenceDigest)
		}
		lease, ok := leases[position.PartitionKey]
		if !ok {
			lease, err = partitions.Acquire(ctx, delivery.ConsumerKey{ConsumerID: pnlConsumerID, ProjectionGeneration: pnlGeneration, PartitionKind: "inventory", PartitionKey: position.PartitionKey}, inventoryLeaseHolder, 30*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var current string
			if err = h.pool.QueryRow(ctx, `SELECT baseline_digest FROM pnl.inventory_l16_baselines WHERE consumer_id=$1 AND projection_generation=$2 AND partition=$3 AND source_stream=$4 AND source_owner=$5 AND source_epoch=$6`, pnlConsumerID, pnlGeneration, position.PartitionKey, realStream, leaseOwner, leaseFence).Scan(&current); err != nil {
				t.Fatalf("baseline of the current source lease: %v", err)
			}
			bootstrap := delivery.BootstrapInput{Lease: lease, CheckpointDigest: accounting.Digest(current), NextSequence: "1", PreviousEffectIdentity: ""}
			tx, err := h.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = partitions.BootstrapTx(ctx, tx, bootstrap, func(ctx context.Context, tx pgx.Tx, in delivery.BootstrapInput) error {
				var digest string
				if err := tx.QueryRow(ctx, `SELECT baseline_digest FROM pnl.inventory_l16_baselines WHERE consumer_id=$1 AND projection_generation=$2 AND partition=$3 AND source_stream=$4 AND source_owner=$5 AND source_epoch=$6`, pnlConsumerID, pnlGeneration, in.Lease.Key.PartitionKey, realStream, leaseOwner, leaseFence).Scan(&digest); err != nil {
					return err
				}
				if digest != string(in.CheckpointDigest) {
					return delivery.ErrIntegrity
				}
				return nil
			})
			if err == nil {
				err = tx.Commit(ctx)
			}
			if err != nil {
				_ = tx.Rollback(ctx)
				t.Fatalf("pnl partition bootstrap: %v", err)
			}
			leases[position.PartitionKey] = lease
		}
		tx, err := h.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		guarded, err := partitions.CheckAndLockTx(ctx, tx, lease, position)
		if err == nil && guarded.DuplicateCommitted {
			err = errors.New("first delivery reported as duplicate")
		}
		if err == nil {
			err = partitions.AdvanceTx(ctx, tx, guarded)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("pnl ordered partition rejects sequence %s: %v", position.Sequence, err)
		}
		consumed = append(consumed, publication)
	}
	return consumed
}

func fillHeight(fill accounting.FillV1) (int64, error) {
	var height int64
	err := json.Unmarshal([]byte(fill.Position.Height), &height)
	return height, err
}

// TestInventoryEnrichmentPublishesPnlContractFramesPostgres: with pnl's
// creation baseline issued for the trader before its first fill, the real
// market's buy and sell are published on accounting.inventory.v1 as sequences
// 1 and 2 of partition 125:wallet:token under inventory-v1, and every frame
// passes pnl's consumer checks; the market frames charts and stats consume
// are unchanged.
func TestInventoryEnrichmentPublishesPnlContractFramesPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newInventoryHarness(t, ctx)
	h.batch(t, ctx, 33963716, 33963716)
	baseline := h.issueBaseline(t, ctx, realMarketTrader, realMarketToken)
	h.batch(t, ctx, 33963717, 33963733)
	h.batch(t, ctx, 33963734, 33963749)

	fills := h.fills(t, ctx)
	if len(fills) != 2 || fills[0].fill.Side != "buy" || fills[1].fill.Side != "sell" {
		t.Fatalf("real market fills %+v", fills)
	}
	if market := h.frames(t, ctx, accounting.MarketSubject); len(market) != 2 {
		t.Fatalf("market frames %d, want 2", len(market))
	}
	consumed := h.consumeAsPnl(t, ctx, baseline)
	if len(consumed) != 2 {
		t.Fatalf("inventory frames %d, want 2", len(consumed))
	}
	first, second := consumed[0].Positions[0], consumed[1].Positions[0]
	if first.Sequence != "1" || first.PreviousEffectIdentity != "" || second.Sequence != "2" || second.PreviousEffectIdentity != first.EffectIdentity {
		t.Fatalf("partition chain %+v -> %+v", first, second)
	}
	if consumed[0].SourceFillID != fills[0].fill.SourceFillID || consumed[1].SourceFillID != fills[1].fill.SourceFillID {
		t.Fatal("inventory frames out of source order")
	}
	var next int64
	if err := h.pool.QueryRow(ctx, `SELECT next_sequence FROM pnl.delivery_partition_offsets WHERE consumer_id=$1 AND projection_generation=$2 AND partition_kind='inventory' AND partition_key=$3`, pnlConsumerID, pnlGeneration, baseline.att.Partition).Scan(&next); err != nil || next != 3 {
		t.Fatalf("pnl offset next_sequence=%d (%v)", next, err)
	}
	var marketPositions, inventoryPositions int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE projection_version='accounting-market-v1' AND partition_kind='market'), count(*) FILTER (WHERE projection_version=$1 AND partition_kind='inventory') FROM indexer.delivery_partition_positions`, pnlInventoryVersion).Scan(&marketPositions, &inventoryPositions); err != nil || marketPositions != 2 || inventoryPositions != 2 {
		t.Fatalf("producer positions market=%d inventory=%d (%v)", marketPositions, inventoryPositions, err)
	}
}

// committedBatch rebuilds, from the committed rows, the canonical batch whose
// last complete height is last (as a retried commit presents it).
func (h inventoryHarness) committedBatch(t *testing.T, ctx context.Context, last int64) store.CanonicalBatch {
	t.Helper()
	b := store.CanonicalBatch{ChainID: 125, StreamID: realStream}
	if err := h.pool.QueryRow(ctx, `SELECT c.batch_id,c.previous_height,l.owner_instance,l.fence_token FROM indexer.indexer_ingest_commits c,indexer.indexer_ingest_leases l WHERE c.chain_id=125 AND c.stream_id=$1 AND c.last_complete_height=$2 AND l.chain_id=125 AND l.stream_id=$1`, realStream, last).Scan(&b.BatchID, &b.PreviousHeight, &b.Owner, &b.Fence); err != nil {
		t.Fatal(err)
	}
	blocks, err := h.pool.Query(ctx, `SELECT height,block_hash,raw_count FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height BETWEEN $2 AND $3 ORDER BY height`, realStream, b.PreviousHeight+1, last)
	if err != nil {
		t.Fatal(err)
	}
	for blocks.Next() {
		block := store.BlockCompleteness{FetchComplete: true, RequiredDecodeComplete: true}
		if err = blocks.Scan(&block.Height, &block.Hash, &block.RawCount); err != nil {
			t.Fatal(err)
		}
		b.Blocks = append(b.Blocks, block)
	}
	blocks.Close()
	records, err := h.pool.Query(ctx, `SELECT transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data FROM indexer.indexer_raw_logs WHERE chain_id=125 AND height BETWEEN $1 AND $2 ORDER BY height,transaction_index,log_index`, b.PreviousHeight+1, last)
	if err != nil {
		t.Fatal(err)
	}
	for records.Next() {
		var r store.RawRecord
		var logIndex, txIndex int64
		var topics []byte
		if err = records.Scan(&r.TransactionHash, &logIndex, &r.Height, &txIndex, &r.BlockHash, &r.Emitter, &topics, &r.Data); err != nil {
			t.Fatal(err)
		}
		r.LogIndex, r.TransactionIndex = uint32(logIndex), uint32(txIndex)
		if err = json.Unmarshal(topics, &r.Topics); err != nil {
			t.Fatal(err)
		}
		b.Records = append(b.Records, r)
	}
	records.Close()
	return b
}

// inventoryDecisions lists the market-only decisions recorded for
// accounting.inventory.v1, as "transactionHash:logIndex requirement".
func (h inventoryHarness) inventoryDecisions(t *testing.T, ctx context.Context) []string {
	t.Helper()
	rows, err := h.pool.Query(ctx, `SELECT d.transaction_hash||':'||d.log_index||' '||d.requirement,d.raw_digest,r.height,r.transaction_index,r.block_hash,r.emitter,r.topics,r.data FROM indexer.indexer_capability_degradation d JOIN indexer.indexer_raw_logs r USING(chain_id,transaction_hash,log_index) WHERE d.chain_id=125 AND d.stream_id=$1 AND d.requirement LIKE 'accounting.inventory.v1:%' ORDER BY r.height,r.transaction_index,r.log_index`, realStream)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var decision, digest string
		var raw store.RawRecord
		var txIndex int64
		var topics []byte
		if err = rows.Scan(&decision, &digest, &raw.Height, &txIndex, &raw.BlockHash, &raw.Emitter, &topics, &raw.Data); err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(strings.SplitN(decision, " ", 2)[0], ":", 2)
		index, err := strconv.ParseUint(parts[1], 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		raw.TransactionHash, raw.LogIndex, raw.TransactionIndex = parts[0], uint32(index), uint32(txIndex)
		if err = json.Unmarshal(topics, &raw.Topics); err != nil {
			t.Fatal(err)
		}
		if want, err := store.RawDigest(raw); err != nil || want != digest {
			t.Fatalf("decision %s raw digest %s, want %s (%v)", decision, digest, want, err)
		}
		out = append(out, decision)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestInventoryEnrichmentDefersFirstFillUntilBaselinePostgres: the creator's
// buy (33963733) is committed before pnl issued its baseline. The buy keeps
// its market frame and is held, not skipped; once the baseline exists the next
// canonical commit (33963734..33963749) publishes the held buy and the sell as
// sequences 1 and 2 of the partition from its creation baseline, so the
// partition never starts mid-history and every frame passes pnl's checks. The
// earlier batch still reconciles with the deferred facet of its fill.
func TestInventoryEnrichmentDefersFirstFillUntilBaselinePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newInventoryHarness(t, ctx)
	h.batch(t, ctx, 33963716, 33963716)
	h.batch(t, ctx, 33963717, 33963733)
	if inventory := h.frames(t, ctx, accounting.InventorySubject); len(inventory) != 0 {
		t.Fatalf("inventory frame without a baseline: %d", len(inventory))
	}
	if decisions := h.inventoryDecisions(t, ctx); len(decisions) != 0 {
		t.Fatalf("held buy decided within the bound: %v", decisions)
	}
	baseline := h.issueBaseline(t, ctx, realMarketTrader, realMarketToken)
	h.batch(t, ctx, 33963734, 33963749)

	fills := h.fills(t, ctx)
	if len(fills) != 2 || fills[0].fill.Side != "buy" || fills[1].fill.Side != "sell" {
		t.Fatalf("real market fills %+v", fills)
	}
	if market := h.frames(t, ctx, accounting.MarketSubject); len(market) != 2 {
		t.Fatalf("market frames %d, want 2", len(market))
	}
	consumed := h.consumeAsPnl(t, ctx, baseline)
	if len(consumed) != 2 || consumed[0].SourceFillID != fills[0].fill.SourceFillID || consumed[1].SourceFillID != fills[1].fill.SourceFillID {
		t.Fatalf("inventory frames %d out of source order", len(consumed))
	}
	first, second := consumed[0].Positions[0], consumed[1].Positions[0]
	if first.Sequence != "1" || first.PreviousEffectIdentity != "" || second.Sequence != "2" || second.PreviousEffectIdentity != first.EffectIdentity {
		t.Fatalf("partition chain %+v -> %+v", first, second)
	}
	var deferred int
	if err := h.pool.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_ingest_outbox WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2 AND event_kind=$3`, fills[0].fill.Position.TransactionHash, fills[0].fill.Position.LogIndex, accounting.InventorySubject).Scan(&deferred); err != nil || deferred != 1 {
		t.Fatalf("deferred buy facet keyed to its own raw log: %d (%v)", deferred, err)
	}
	if decisions := h.inventoryDecisions(t, ctx); len(decisions) != 0 {
		t.Fatalf("released pair recorded market-only: %v", decisions)
	}
	// The already committed buy batch still reconciles: its records now also
	// carry the deferred inventory facet a later commit staged.
	if committed, err := h.proc.deps.Store.ReconcileCanonicalCommit(ctx, h.committedBatch(t, ctx, 33963733)); err != nil || !committed {
		t.Fatalf("committed batch with a deferred inventory facet does not reconcile: %v %v", committed, err)
	}
}

// TestInventoryDeferralBoundMarksPairMarketOnlyPostgres: with a 16 block wait
// bound and no baseline ever issued, the held buy (33963733) is not decided by
// its own commit; its last evaluation is the commit covering 33963749, where
// it is decided market-only with an auditable reason, and the sell is decided
// market-only because its earlier fill is: the partition is never opened
// mid-history. Market frames are unaffected.
func TestInventoryDeferralBoundMarksPairMarketOnlyPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newInventoryHarnessWithWait(t, ctx, 16)
	h.batch(t, ctx, 33963716, 33963716)
	h.batch(t, ctx, 33963717, 33963733)
	if decisions := h.inventoryDecisions(t, ctx); len(decisions) != 0 {
		t.Fatalf("buy decided before its bound: %v", decisions)
	}
	h.batch(t, ctx, 33963734, 33963749)
	fills := h.fills(t, ctx)
	if len(fills) != 2 {
		t.Fatalf("fills %d", len(fills))
	}
	want := []string{
		fills[0].fill.Position.TransactionHash + ":" + fills[0].fill.Position.LogIndex + " accounting.inventory.v1:baseline-unissued-timeout",
		fills[1].fill.Position.TransactionHash + ":" + fills[1].fill.Position.LogIndex + " accounting.inventory.v1:earlier-fill-market-only",
	}
	if got := h.inventoryDecisions(t, ctx); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("market-only decisions %v, want %v", got, want)
	}
	if market := h.frames(t, ctx, accounting.MarketSubject); len(market) != 2 {
		t.Fatalf("market frames %d, want 2", len(market))
	}
	if inventory := h.frames(t, ctx, accounting.InventorySubject); len(inventory) != 0 {
		t.Fatalf("partition opened after the bound: %d inventory frames", len(inventory))
	}
}

// TestInventoryFramesNameTheirBaselineAcrossFenceChangePostgres: the buy is
// published under source lease (indexer-test, 1) against pnl's baseline for
// that lease; the indexer's lease then expires and is re-acquired at fence 2.
// The sell is published against the baseline pnl rebinds to fence 2. Each
// frame's evidence names the baseline it was built against, and pnl's
// verification (the named past-lease row, epoch not beyond the current fence)
// accepts both frames after the fence change.
func TestInventoryFramesNameTheirBaselineAcrossFenceChangePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	h := newInventoryHarness(t, ctx)
	h.batch(t, ctx, 33963716, 33963716)
	first := h.issueBaseline(t, ctx, realMarketTrader, realMarketToken)
	h.batch(t, ctx, 33963717, 33963733)
	if _, err := h.pool.Exec(ctx, `UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp() WHERE chain_id=125 AND stream_id=$1`, realStream); err != nil {
		t.Fatal(err)
	}
	if err := h.proc.ProcessBatch(ctx, 33963734, 33963740); err == nil {
		t.Fatal("expired source lease renewed")
	}
	h.batch(t, ctx, 33963734, 33963740)
	second := h.issueBaseline(t, ctx, realMarketTrader, realMarketToken)
	if first.att.SourceEpoch != 1 || second.att.SourceEpoch != 2 || first.digest == second.digest {
		t.Fatalf("baselines bound to fences %d and %d", first.att.SourceEpoch, second.att.SourceEpoch)
	}
	h.batch(t, ctx, 33963741, 33963749)
	consumed := h.consumeAsPnl(t, ctx, first, second)
	if len(consumed) != 2 || consumed[0].Positions[0].Sequence != "1" || consumed[1].Positions[0].Sequence != "2" {
		t.Fatalf("inventory frames %d across the fence change", len(consumed))
	}
	fills := h.fills(t, ctx)
	for i, baseline := range []issuedBaseline{first, second} {
		if !evidenceNames(t, ctx, h, consumed[i], fills[i], baseline) {
			t.Fatalf("frame %d is not bound to the fence %d baseline", i+1, baseline.att.SourceEpoch)
		}
	}
}

// evidenceNames reports whether publication's evidence was built against
// baseline.
func evidenceNames(t *testing.T, ctx context.Context, h inventoryHarness, publication accounting.PublicationV1, fill recordedFill, baseline issuedBaseline) bool {
	t.Helper()
	height, err := fillHeight(fill.fill)
	if err != nil {
		t.Fatal(err)
	}
	moves, err := h.pool.Query(ctx, `SELECT payload FROM indexer.indexer_token_transfers_v1 WHERE token=$1 AND height BETWEEN $2 AND $3 AND (from_address=$4 OR to_address=$4) ORDER BY height,transaction_index,log_index`, baseline.att.Token, baseline.att.CreationHeight, height, baseline.att.Wallet)
	if err != nil {
		t.Fatal(err)
	}
	payloads := []json.RawMessage{}
	for moves.Next() {
		var m []byte
		if err = moves.Scan(&m); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, append(json.RawMessage(nil), m...))
	}
	moves.Close()
	movementBytes, err := json.Marshal(payloads)
	if err != nil {
		t.Fatal(err)
	}
	return publication.Enrichment.EvidenceDigest == accounting.Digest(accounting.HashTuple("fresh-inventory-evidence-v1", baseline.digest, baseline.att.MovementDigest, fill.digest, string(movementBytes)))
}
