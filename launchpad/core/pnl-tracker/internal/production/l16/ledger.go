package l16

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var hash = regexp.MustCompile(`^0x[0-9a-f]{64}$`)

type Identity struct {
	Chain, Emitter, Transaction string
	Log                         uint32
}

func (i Identity) Key() (string, error) {
	if i.Chain != "125" || !address.MatchString(i.Emitter) || !hash.MatchString(i.Transaction) {
		return "", errors.New("invalid canonical identity")
	}
	return fmt.Sprintf("%s:%s:%s:%d", i.Chain, i.Emitter, i.Transaction, i.Log), nil
}

type Leg struct {
	Identity                               Identity
	Height                                 uint64
	TransactionIndex                       uint32
	Predecessor                            string
	Wallet, Token                          string
	Sender, RouterCaller, Payer, Recipient string
	Kind                                   string // buy, sell, incoming, outgoing, mint, burn
	Quantity                               string
	ActualUSDL                             *string
	SettlementReference                    string
}

func (l Leg) Validate() error {
	if l.Height == 0 || l.Height > 1<<63-1 {
		return errors.New("invalid canonical height")
	}
	quantity, err := Uint256(l.Quantity)
	if err != nil || quantity.Sign() == 0 {
		return errors.New("invalid economic quantity")
	}
	if l.ActualUSDL != nil {
		if _, err = Uint256(*l.ActualUSDL); err != nil {
			return err
		}
	}
	if _, e := l.Identity.Key(); e != nil {
		return e
	}
	if !address.MatchString(l.Wallet) || !address.MatchString(l.Token) {
		return errors.New("invalid wallet/token")
	}
	if l.SettlementReference == "" || len(l.SettlementReference) > 256 {
		return errors.New("verified settlement reference required")
	}
	if len(l.Predecessor) > 256 {
		return errors.New("invalid predecessor")
	}
	for _, a := range []string{l.Sender, l.RouterCaller, l.Payer, l.Recipient} {
		if a != "" && !address.MatchString(a) {
			return errors.New("invalid actor")
		}
	}
	switch l.Kind {
	case "buy":
		if l.ActualUSDL == nil || l.Payer == "" || l.Recipient != l.Wallet {
			return errors.New("unattributed buy: reconciliation required")
		}
	case "sell":
		if l.ActualUSDL == nil || l.Payer != l.Wallet || l.Recipient == "" {
			return errors.New("unattributed sell: reconciliation required")
		}
	case "incoming", "outgoing", "mint", "burn":
		if l.ActualUSDL != nil {
			return errors.New("movement cannot invent consideration")
		}
	default:
		return errors.New("unsupported leg")
	}
	return nil
}
func Apply(p Inventory, l Leg) (Inventory, error) {
	if e := l.Validate(); e != nil {
		return p, e
	}
	switch l.Kind {
	case "buy":
		return Acquire(p, l.Quantity, l.ActualUSDL)
	case "incoming", "mint":
		return Acquire(p, l.Quantity, nil)
	default:
		n, _, e := Dispose(p, l.Quantity, l.ActualUSDL)
		return n, e
	}
}

// DDL is a serial integration proposal. The caller owns a transaction and sets a
// trusted search_path to the dedicated service schema before using ApplyTx.
const DDL = `CREATE TABLE inventory_l16 (partition text PRIMARY KEY, revision bigint NOT NULL CHECK(revision>0), state jsonb NOT NULL, last_identity text NOT NULL, height numeric(20,0) NOT NULL, tx_index bigint NOT NULL, log_index bigint NOT NULL);
CREATE TABLE inventory_l16_inbox (consumer_version text NOT NULL, identity text NOT NULL, partition text NOT NULL, payload_hash text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(consumer_version,identity,partition));
CREATE TABLE inventory_l16_outbox (consumer_version text NOT NULL, identity text NOT NULL, partition text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(consumer_version,identity,partition));
CREATE TABLE inventory_l16_transfers (consumer_version text NOT NULL, identity text NOT NULL, source_partition text NOT NULL, destination_partition text NOT NULL, payload_hash text NOT NULL, payload jsonb NOT NULL, PRIMARY KEY(consumer_version,identity,source_partition,destination_partition));`

// ApplyTx atomically locks a partition and writes immutable inbox, inventory and
// outbox. Caller must rollback on any error and commit before acknowledging.
// Predecessor/order gaps return an explicit error; coordinator parks and replays.
func ApplyTx(ctx context.Context, tx pgx.Tx, l Leg) (Inventory, bool, error) {
	return applyTx(ctx, tx, l, Apply)
}

func applyTx(ctx context.Context, tx pgx.Tx, l Leg, fold func(Inventory, Leg) (Inventory, error)) (Inventory, bool, error) {
	partition := strings.Join([]string{"125", l.Wallet, l.Token}, ":")
	return applyVersionedTx(ctx, tx, l, fold, Algorithm+":inventory_v2", partition)
}

func applyVersionedTx(ctx context.Context, tx pgx.Tx, l Leg, fold func(Inventory, Leg) (Inventory, error), version, statePartition string) (Inventory, bool, error) {
	if tx == nil || fold == nil {
		return Inventory{}, false, errors.New("inventory transaction and fold required")
	}
	if e := l.Validate(); e != nil {
		return Inventory{}, false, e
	}
	id, _ := l.Identity.Key()
	partition := strings.Join([]string{"125", l.Wallet, l.Token}, ":")
	payload, e := json.Marshal(l)
	if e != nil {
		return Inventory{}, false, e
	}
	digest := sha256.Sum256(payload)
	h := hex.EncodeToString(digest[:])
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, statePartition); e != nil {
		return Inventory{}, false, e
	}
	var oldHash string
	e = tx.QueryRow(ctx, `SELECT payload_hash FROM inventory_l16_inbox WHERE consumer_version=$1 AND identity=$2 AND partition=$3`, version, id, partition).Scan(&oldHash)
	if e != nil && e != pgx.ErrNoRows {
		return Inventory{}, false, e
	}
	if e == nil {
		if oldHash != h {
			return Inventory{}, false, errors.New("conflicting immutable leg")
		}
		var raw []byte
		e = tx.QueryRow(ctx, `SELECT state FROM inventory_l16 WHERE partition=$1`, statePartition).Scan(&raw)
		var p Inventory
		if e == nil {
			p, e = DecodeInventoryState(raw)
		}
		return p, false, e
	}
	p := Empty()
	var raw []byte
	var last string
	var height uint64
	var heightText string
	var ti, li uint32
	var revision int64
	e = tx.QueryRow(ctx, `SELECT state,last_identity,height::text,tx_index,log_index,revision FROM inventory_l16 WHERE partition=$1 FOR UPDATE`, statePartition).Scan(&raw, &last, &heightText, &ti, &li, &revision)
	if e != nil && e != pgx.ErrNoRows {
		return Inventory{}, false, e
	}
	if e == nil {
		height, e = strconv.ParseUint(heightText, 10, 64)
		if e != nil {
			return Inventory{}, false, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			return Inventory{}, false, e
		}
		if l.Height < height || l.Height == height && (l.TransactionIndex < ti || l.TransactionIndex == ti && l.Identity.Log <= li) {
			return Inventory{}, false, errors.New("noncanonical ordering: park partition")
		}
	}
	if l.Predecessor != last {
		return Inventory{}, false, errors.New("missing predecessor: park partition")
	}
	next, e := fold(p, l)
	if e != nil {
		return Inventory{}, false, e
	}
	body, e := json.Marshal(next)
	if e != nil {
		return Inventory{}, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO inventory_l16_inbox VALUES($1,$2,$3,$4,$5)`, version, id, partition, h, payload); e != nil {
		return Inventory{}, false, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO inventory_l16 VALUES($1,1,$2,$3,$4,$5,$6) ON CONFLICT(partition) DO UPDATE SET revision=inventory_l16.revision+1,state=excluded.state,last_identity=excluded.last_identity,height=excluded.height,tx_index=excluded.tx_index,log_index=excluded.log_index`, statePartition, body, id, fmt.Sprint(l.Height), l.TransactionIndex, l.Identity.Log); e != nil {
		return Inventory{}, false, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO inventory_l16_outbox VALUES($1,$2,$3,$4)`, version, id, partition, body)
	return next, e == nil, e
}

type TransferRequest struct {
	Source                 Leg
	Destination            string
	DestinationPredecessor string
	Carry                  bool
	ProofReference         string
}

func ApplyTransferTx(ctx context.Context, tx pgx.Tx, request TransferRequest) (Inventory, Inventory, bool, error) {
	var zero Inventory
	l := request.Source
	if err := l.Validate(); err != nil {
		return zero, zero, false, err
	}
	if l.Kind != "outgoing" || l.Recipient != request.Destination || !address.MatchString(request.Destination) || request.Destination == l.Wallet || request.Carry && (request.ProofReference == "" || len(request.ProofReference) > 256) {
		return zero, zero, false, errors.New("invalid transfer or missing verified ownership/consent reference")
	}
	sourcePartition := "125:" + l.Wallet + ":" + l.Token
	destinationPartition := "125:" + request.Destination + ":" + l.Token
	partitions := []string{sourcePartition, destinationPartition}
	sort.Strings(partitions)
	for _, partition := range partitions {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, partition); err != nil {
			return zero, zero, false, err
		}
	}
	id, _ := l.Identity.Key()
	body, err := json.Marshal(request)
	if err != nil {
		return zero, zero, false, err
	}
	digest := sha256.Sum256(body)
	digestText := hex.EncodeToString(digest[:])
	version := Algorithm + ":inventory_v2"
	var storedHash string
	err = tx.QueryRow(ctx, `SELECT payload_hash FROM inventory_l16_transfers WHERE consumer_version=$1 AND identity=$2 AND source_partition=$3 AND destination_partition=$4`, version, id, sourcePartition, destinationPartition).Scan(&storedHash)
	if err != nil && err != pgx.ErrNoRows {
		return zero, zero, false, err
	}
	if err == nil {
		if storedHash != digestText {
			return zero, zero, false, errors.New("conflicting immutable transfer")
		}
		states := make([]Inventory, 2)
		for i, partition := range []string{sourcePartition, destinationPartition} {
			var raw []byte
			if err = tx.QueryRow(ctx, `SELECT state FROM inventory_l16 WHERE partition=$1`, partition).Scan(&raw); err != nil {
				return zero, zero, false, err
			}
			if err = json.Unmarshal(raw, &states[i]); err != nil {
				return zero, zero, false, err
			}
			if _, err = states[i].values(); err != nil {
				return zero, zero, false, err
			}
		}
		return states[0], states[1], false, nil
	}
	var removed Disposal
	source, inserted, err := applyTx(ctx, tx, l, func(p Inventory, leg Leg) (Inventory, error) {
		next, disposal, err := Dispose(p, leg.Quantity, nil)
		removed = disposal
		return next, err
	})
	if err != nil {
		return zero, zero, false, err
	}
	if !inserted {
		return zero, zero, false, errors.New("source transfer already applied outside atomic wrapper")
	}
	incoming := l
	incoming.Kind = "incoming"
	incoming.Wallet = request.Destination
	incoming.Recipient = request.Destination
	incoming.Predecessor = request.DestinationPredecessor
	incoming.SettlementReference = "transfer-v2:" + digestText
	destination, inserted, err := applyTx(ctx, tx, incoming, func(p Inventory, leg Leg) (Inventory, error) {
		if !request.Carry {
			return Acquire(p, leg.Quantity, nil)
		}
		values, err := p.values()
		if err != nil {
			return p, err
		}
		for i, amount := range []string{removed.Known, removed.Unknown, removed.Cost} {
			n, err := aggregate(amount, false)
			if err != nil {
				return p, err
			}
			values[i].Add(values[i], n)
		}
		p.Known, p.Unknown, p.Cost = values[0].String(), values[1].String(), values[2].String()
		_, err = p.values()
		return p, err
	})
	if err != nil {
		return zero, zero, false, err
	}
	if !inserted {
		return zero, zero, false, errors.New("destination transfer already applied outside atomic wrapper")
	}
	_, err = tx.Exec(ctx, `INSERT INTO inventory_l16_transfers VALUES($1,$2,$3,$4,$5,$6)`, version, id, sourcePartition, destinationPartition, digestText, body)
	return source, destination, err == nil, err
}

const OwnedDDL = `CREATE TABLE inventory_l16_effects (
 projection_generation text NOT NULL, partition text NOT NULL, effect_identity text NOT NULL,
 leg_identity text NOT NULL, mapping_digest text NOT NULL CHECK(mapping_digest ~ '^[0-9a-f]{64}$'),
 position bytea NOT NULL,
 PRIMARY KEY(projection_generation,partition,effect_identity),
 UNIQUE(projection_generation,partition,leg_identity)
);`

type effectMapping struct {
	Generation string
	Partition  string
	Identity   string
	Position   accounting.PartitionPosition
}

func mappingDigest(m effectMapping) (string, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return delivery.Hash(b), nil
}

func ownedVersion(generation string) (string, error) {
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(generation) {
		return "", errors.New("invalid projection generation")
	}
	return Algorithm + ":" + generation, nil
}

func ApplyOwnedLegTx(ctx context.Context, tx pgx.Tx, g delivery.GuardedPosition, l Leg, projectionGeneration string) (Inventory, bool, error) {
	version, err := ownedVersion(projectionGeneration)
	if err != nil || tx == nil {
		return Inventory{}, false, errors.New("owned inventory transaction/generation required")
	}
	p := g.Position
	partition := "125:" + l.Wallet + ":" + l.Token
	if accounting.ValidatePartitionPosition(p) != nil || p.PartitionKind != "inventory" || p.PartitionKey != partition || g.Lease.Key.PartitionKey != partition || g.Lease.Key.PartitionKind != "inventory" || g.Lease.Key.ProjectionGeneration != projectionGeneration || g.Lease.Epoch <= 0 {
		return Inventory{}, false, delivery.ErrIntegrity
	}
	if l.Kind != "buy" && l.Kind != "sell" || l.Payer != l.Wallet || l.Recipient != l.Wallet || l.ActualUSDL == nil {
		return Inventory{}, false, ErrAttributionUnavailable
	}
	if p.SourceOrder.ChainID != l.Identity.Chain || p.SourceOrder.TransactionHash != l.Identity.Transaction || p.SourceOrder.Height != strconv.FormatUint(l.Height, 10) || p.SourceOrder.TransactionIndex != strconv.FormatUint(uint64(l.TransactionIndex), 10) || p.SourceOrder.LogIndex != strconv.FormatUint(uint64(l.Identity.Log), 10) {
		return Inventory{}, false, delivery.ErrIntegrity
	}
	// Lock and verify mapping even on redelivery; identities in the producer and
	// L16 domains are deliberately different and cannot be copied as predecessors.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, version+":"+partition); err != nil {
		return Inventory{}, false, err
	}
	predecessor := ""
	if p.PreviousEffectIdentity != "" {
		var raw []byte
		var digest string
		if err = tx.QueryRow(ctx, `SELECT leg_identity,mapping_digest,position FROM inventory_l16_effects WHERE projection_generation=$1 AND partition=$2 AND effect_identity=$3`, projectionGeneration, partition, p.PreviousEffectIdentity).Scan(&predecessor, &digest, &raw); errors.Is(err, pgx.ErrNoRows) {
			return Inventory{}, false, delivery.ErrGap
		} else if err != nil {
			return Inventory{}, false, err
		}
		var prior accounting.PartitionPosition
		if err = json.Unmarshal(raw, &prior); err != nil {
			return Inventory{}, false, delivery.ErrIntegrity
		}
		actual, hashErr := mappingDigest(effectMapping{projectionGeneration, partition, predecessor, prior})
		priorSequence, sequenceErr := strconv.ParseUint(prior.Sequence, 10, 63)
		currentSequence, currentErr := strconv.ParseUint(p.Sequence, 10, 63)
		if hashErr != nil || actual != digest || accounting.ValidatePartitionPosition(prior) != nil || prior.EffectIdentity != p.PreviousEffectIdentity || prior.PartitionKey != partition || prior.ProjectionVersion != p.ProjectionVersion || sequenceErr != nil || currentErr != nil || priorSequence+1 != currentSequence {
			return Inventory{}, false, delivery.ErrIntegrity
		}
	}
	if l.Predecessor != "" && l.Predecessor != predecessor {
		return Inventory{}, false, delivery.ErrIntegrity
	}
	l.Predecessor = predecessor
	state, inserted, err := applyVersionedTx(ctx, tx, l, Apply, version, version+":"+partition)
	if err != nil {
		return state, false, err
	}
	id, err := l.Identity.Key()
	if err != nil {
		return state, false, err
	}
	m := effectMapping{projectionGeneration, partition, id, p}
	digest, err := mappingDigest(m)
	if err != nil {
		return state, false, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return state, false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO inventory_l16_effects(projection_generation,partition,effect_identity,leg_identity,mapping_digest,position) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, projectionGeneration, partition, p.EffectIdentity, id, digest, raw)
	if err != nil {
		return state, false, err
	}
	if tag.RowsAffected() == 0 {
		var oldIdentity, oldDigest string
		var oldRaw []byte
		err = tx.QueryRow(ctx, `SELECT leg_identity,mapping_digest,position FROM inventory_l16_effects WHERE projection_generation=$1 AND partition=$2 AND effect_identity=$3`, projectionGeneration, partition, p.EffectIdentity).Scan(&oldIdentity, &oldDigest, &oldRaw)
		if err != nil {
			return state, false, err
		}
		if oldIdentity != id || oldDigest != digest || string(oldRaw) != string(raw) {
			return state, false, delivery.ErrIntegrity
		}
	}
	return state, inserted, nil
}

type portfolioRow struct {
	LogicalPartition string
	Revision         int64
	Inventory        Inventory
	Position         accounting.PartitionPosition
	Fill             accounting.FillV1
}

func portfolioBatch(generation, wallet, effectIdentity string, rows []portfolioRow) (interfaces.PortfolioUpdate, interfaces.Watermark, string, error) {
	var update interfaces.PortfolioUpdate
	var watermark interfaces.Watermark
	if !regexpToken(generation) || !address.MatchString(wallet) || !partitionHash(effectIdentity) || len(rows) == 0 || len(rows) > 100 {
		return update, watermark, "", errors.New("complete portfolio batch unavailable or exceeds100 positions")
	}
	revision := new(big.Int)
	realized := new(big.Int)
	known := new(big.Int)
	unknown := new(big.Int)
	unpriced := 0
	realizedComplete := true
	var newest *portfolioRow
	changes := make([]interfaces.PositionChange, 0, len(rows))
	seen := map[string]bool{}
	for i := range rows {
		row := &rows[i]
		parts := strings.Split(row.LogicalPartition, ":")
		if len(parts) != 3 || parts[0] != "125" || parts[1] != wallet || parts[2] != row.Fill.Token.Address || seen[row.LogicalPartition] || row.Revision <= 0 || accounting.ValidatePartitionPosition(row.Position) != nil || row.Position.PartitionKey != row.LogicalPartition || row.Position.SourceOrder != row.Fill.Position {
			return update, watermark, "", delivery.ErrIntegrity
		}
		seen[row.LogicalPartition] = true
		values, err := row.Inventory.values()
		if err != nil {
			return update, watermark, "", err
		}
		revision.Add(revision, big.NewInt(row.Revision))
		realized.Add(realized, values[3])
		known.Add(known, values[0])
		unknown.Add(unknown, values[1])
		realizedComplete = realizedComplete && !row.Inventory.IncompleteRealized
		total := new(big.Int).Add(values[0], values[1])
		if total.Sign() > 0 {
			unpriced++
		}
		timestamp, err := strconv.ParseInt(row.Fill.BlockTimestampSeconds, 10, 64)
		if err != nil {
			return update, watermark, "", err
		}
		observed := time.Unix(timestamp, 0).UTC().Format("2006-01-02T15:04:05.000Z")
		ownWatermark := interfaces.Watermark{ChainId: "125", Height: interfaces.UInt256(row.Position.SourceOrder.Height), BlockHash: row.Position.SourceOrder.BlockHash, ProjectionRevision: interfaces.PositiveUInt256(strconv.FormatInt(row.Revision, 10)), ObservedAt: observed, Integrity: "healthy"}
		coverageNumerator, coverageDenominator := values[0].String(), total.String()
		if total.Sign() == 0 {
			coverageNumerator, coverageDenominator = "1", "1"
		}
		basis := "complete"
		if values[1].Sign() > 0 {
			basis = "partial"
			if values[0].Sign() == 0 {
				basis = "unknown"
			}
		}
		realizedQuality := "complete"
		if row.Inventory.IncompleteRealized {
			realizedQuality = "partial"
		}
		var remaining *interfaces.AggregateUInt
		if values[1].Sign() == 0 {
			v := interfaces.AggregateUInt(row.Inventory.Cost)
			remaining = &v
		}
		var completeRealized *interfaces.SignedAggregateInteger
		if !row.Inventory.IncompleteRealized {
			v := interfaces.SignedAggregateInteger(row.Inventory.Realized)
			completeRealized = &v
		}
		position := interfaces.PortfolioPosition{PositionId: row.LogicalPartition, ChainId: "125", Wallet: wallet, TokenAddress: parts[2], BalanceRaw: interfaces.UInt256(total.String()), KnownQuantityRaw: interfaces.UInt256(row.Inventory.Known), UnknownQuantityRaw: interfaces.UInt256(row.Inventory.Unknown), KnownRemainingCostUsdlRaw: interfaces.AggregateUInt(row.Inventory.Cost), BasisCoverage: interfaces.Ratio{Numerator: interfaces.PriceComponent(coverageNumerator), Denominator: interfaces.PositivePriceComponent(coverageDenominator)}, RemainingCostUsdlRaw: remaining, RealizedPnlRaw: completeRealized, KnownRealizedSubtotalUsdlRaw: interfaces.SignedAggregateInteger(row.Inventory.Realized), BasisQuality: basis, RealizedBasisQuality: realizedQuality, PricingQuality: "unpriced", SourceWatermark: ownWatermark}
		changes = append(changes, interfaces.PositionChange{Operation: "upsert", PositionId: row.LogicalPartition, Revision: interfaces.PositiveUInt256(strconv.FormatInt(row.Revision, 10)), Position: &position})
		if newest == nil {
			newest = row
		} else {
			cmp, err := accounting.CompareSourcePosition(row.Position.SourceOrder, newest.Position.SourceOrder)
			if err != nil {
				return update, watermark, "", err
			}
			if cmp > 0 {
				newest = row
			}
		}
	}
	if revision.BitLen() > 256 {
		return update, watermark, "", delivery.ErrIntegrity
	}
	timestamp, err := strconv.ParseInt(newest.Fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return update, watermark, "", err
	}
	occurred := time.Unix(timestamp, 0).UTC().Format("2006-01-02T15:04:05.000Z")
	watermark = interfaces.Watermark{ChainId: "125", Height: interfaces.UInt256(newest.Position.SourceOrder.Height), BlockHash: newest.Position.SourceOrder.BlockHash, ProjectionRevision: interfaces.PositiveUInt256(revision.String()), ObservedAt: occurred, Integrity: "healthy"}
	snapshot := uuid.NewSHA1(uuid.NameSpaceOID, []byte("portfolio:"+generation+":"+wallet+":"+revision.String()+":"+effectIdentity)).String()
	basis := "complete"
	if unknown.Sign() > 0 {
		basis = "partial"
		if known.Sign() == 0 {
			basis = "unknown"
		}
	}
	pricing := "unpriced"
	var completeValue *interfaces.AggregateUInt
	if unpriced == 0 {
		pricing = "complete"
		v := interfaces.AggregateUInt("0")
		completeValue = &v
	}
	var completeRealized *interfaces.SignedAggregateInteger
	if realizedComplete {
		v := interfaces.SignedAggregateInteger(realized.String())
		completeRealized = &v
	}
	summary := interfaces.PortfolioSummary{Wallet: wallet, PricedSubtotalUsdlRaw: "0", CompleteValueUsdlRaw: completeValue, RealizedPnlRaw: completeRealized, KnownRealizedSubtotalUsdlRaw: interfaces.SignedAggregateInteger(realized.String()), BasisQuality: basis, PricingQuality: pricing, UnpricedAssetCount: interfaces.SafeInteger(unpriced), PositionCount: interfaces.SafeInteger(len(rows)), SnapshotId: snapshot, Watermark: watermark}
	update = interfaces.PortfolioUpdate{SnapshotId: snapshot, SummaryRevision: interfaces.PositiveUInt256(revision.String()), Changes: changes, Summary: summary, CompleteBatch: true}
	raw, err := json.Marshal(update)
	if err != nil {
		return update, watermark, "", err
	}
	if err = interfaces.ValidateJSON("PortfolioUpdate", raw); err != nil {
		return update, watermark, "", err
	}
	return update, watermark, occurred, nil
}

func (c *AccountingConsumer) appendPortfolioTx(ctx context.Context, tx pgx.Tx, g delivery.GuardedPosition, leg Leg, fill accounting.FillV1) error {
	version, err := ownedVersion(c.generation)
	if err != nil {
		return err
	}
	prefix := "125:" + leg.Wallet + ":"
	query := `SELECT e.partition,s.revision,s.state,e.position,f.payload,f.source_fact_digest FROM inventory_l16 s JOIN inventory_l16_effects e ON e.projection_generation=$1 AND s.partition=$2||':'||e.partition AND e.leg_identity=s.last_identity JOIN indexer.indexer_accounting_fills_v1 f ON f.source_fill_id=convert_from(e.position,'UTF8')::jsonb->>'sourceEventId' WHERE left(e.partition,length($3))=$3 ORDER BY e.partition`
	if c.portfolioRepository == nil {
		query += " LIMIT 101"
	}
	rows, err := tx.Query(ctx, query, c.generation, version, prefix)
	if err != nil {
		return err
	}
	current := make([]portfolioRow, 0)
	for rows.Next() {
		var row portfolioRow
		var stateRaw, positionRaw, fillRaw []byte
		var sourceDigest string
		if err = rows.Scan(&row.LogicalPartition, &row.Revision, &stateRaw, &positionRaw, &fillRaw, &sourceDigest); err != nil {
			rows.Close()
			return err
		}
		row.Inventory, err = DecodeInventoryState(stateRaw)
		if err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(positionRaw, &row.Position); err != nil {
			rows.Close()
			return err
		}
		row.Fill, err = accounting.DecodeFillV1(fillRaw)
		if err != nil {
			rows.Close()
			return err
		}
		actual, hashErr := accounting.FillDigest(row.Fill)
		if hashErr != nil || string(actual) != sourceDigest {
			rows.Close()
			return delivery.ErrIntegrity
		}
		current = append(current, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var update interfaces.PortfolioUpdate
	var watermark interfaces.Watermark
	var occurred string
	if c.portfolioRepository != nil {
		update, watermark, occurred, err = c.commitFullPortfolioTx(ctx, tx, g, leg, current)
	} else {
		update, watermark, occurred, err = portfolioBatch(c.generation, leg.Wallet, g.Position.EffectIdentity, current)
	}
	if err != nil {
		return err
	}
	var mappingDigest, attestationDigest string
	var epoch int64
	err = tx.QueryRow(ctx, `SELECT mapping_digest,attestation_digest,owner_epoch FROM inventory_l16_stream_proof_v1 WHERE projection_generation=$1 AND partition=$2 AND effect_identity=$3 AND owner_epoch=$4`, c.generation, g.Position.PartitionKey, g.Position.EffectIdentity, g.Lease.Epoch).Scan(&mappingDigest, &attestationDigest, &epoch)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(update)
	if err != nil {
		return err
	}
	watermarkRaw, err := json.Marshal(watermark)
	if err != nil {
		return err
	}
	id, err := leg.Identity.Key()
	if err != nil {
		return err
	}
	return c.streamWriter.AppendTx(ctx, tx, delivery.StreamUpdate{EventID: g.Position.EffectIdentity + "_" + leg.Wallet, Topic: "private.portfolio." + leg.Wallet, Revision: string(update.SummaryRevision), OccurredAt: occurred, Watermark: watermarkRaw, Payload: payload, Source: delivery.StreamSourceProof{Kind: "portfolio_commit", Wallet: leg.Wallet, Token: leg.Token, SourceID: g.Position.EffectIdentity, SourceDigest: attestationDigest, OwnerEpoch: strconv.FormatInt(epoch, 10), ProjectionVersion: c.generation, Sequence: string(update.SummaryRevision), SourceFillID: id, SourceFactDigest: mappingDigest}})
}

func (c *AccountingConsumer) commitFullPortfolioTx(ctx context.Context, tx pgx.Tx, g delivery.GuardedPosition, leg Leg, rows []portfolioRow) (interfaces.PortfolioUpdate, interfaces.Watermark, string, error) {
	var update interfaces.PortfolioUpdate
	var wireWatermark interfaces.Watermark
	if len(rows) == 0 || len(rows) > 100000 {
		return update, wireWatermark, "", ErrCostAllocationUnavailable
	}
	activeRows, err := tx.Query(ctx, `SELECT p.payload,p.snapshot_digest FROM inventory_l16_active_v2 a JOIN inventory_l16_snapshots_v2 p USING(generation,partition,snapshot_digest) WHERE a.generation=$1 AND left(a.partition,length($2))=$2`, c.generation, "125:"+leg.Wallet+":")
	if err != nil {
		return update, wireWatermark, "", err
	}
	active := map[string]RebuildSnapshot{}
	for activeRows.Next() {
		var raw []byte
		var digest string
		if err = activeRows.Scan(&raw, &digest); err != nil {
			activeRows.Close()
			return update, wireWatermark, "", err
		}
		if delivery.Hash(raw) != digest {
			activeRows.Close()
			return update, wireWatermark, "", delivery.ErrIntegrity
		}
		var replay RebuildSnapshot
		if err = json.Unmarshal(raw, &replay); err != nil {
			activeRows.Close()
			return update, wireWatermark, "", err
		}
		if _, err = replay.State.values(); err != nil {
			activeRows.Close()
			return update, wireWatermark, "", err
		}
		active[replay.Partition] = replay
	}
	err = activeRows.Err()
	activeRows.Close()
	if err != nil {
		return update, wireWatermark, "", err
	}
	for _, row := range rows {
		if replay, ok := active[row.LogicalPartition]; ok {
			height, e := strconv.ParseInt(row.Fill.Position.Height, 10, 64)
			if e != nil {
				return update, wireWatermark, "", e
			}
			if replay.Height >= height && replay.State != row.Inventory || replay.Height < height && (replay.State.Unknown != row.Inventory.Unknown || replay.State.IncompleteRealized && !row.Inventory.IncompleteRealized) {
				return update, wireWatermark, "", delivery.ErrGap
			}
			delete(active, row.LogicalPartition)
		}
	}
	if len(active) > 0 {
		return update, wireWatermark, "", delivery.ErrGap
	}
	revision := new(big.Int)
	newest := rows[0]
	for _, row := range rows {
		if row.Revision <= 0 {
			return update, wireWatermark, "", delivery.ErrIntegrity
		}
		revision.Add(revision, big.NewInt(row.Revision))
		cmp, err := accounting.CompareSourcePosition(row.Position.SourceOrder, newest.Position.SourceOrder)
		if err != nil {
			return update, wireWatermark, "", err
		}
		if cmp > 0 {
			newest = row
		}
	}
	var latest string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(revision),0)::text FROM portfolio_snapshots WHERE wallet=$1`, leg.Wallet).Scan(&latest); err != nil {
		return update, wireWatermark, "", err
	}
	prior, err := Uint256(latest)
	if err != nil {
		return update, wireWatermark, "", err
	}
	if revision.Cmp(prior) <= 0 {
		revision.Add(prior, big.NewInt(1))
	}
	if revision.BitLen() > 256 {
		return update, wireWatermark, "", delivery.ErrIntegrity
	}
	seconds, err := strconv.ParseInt(newest.Fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return update, wireWatermark, "", err
	}
	observed := time.Unix(seconds, 0).UTC()
	watermark := l17.Watermark{ChainID: "125", Height: newest.Position.SourceOrder.Height, BlockHash: newest.Position.SourceOrder.BlockHash, ProjectionRevision: revision.String(), ObservedAt: observed.Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	positions := make([]l17.Position, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		parts := strings.Split(row.LogicalPartition, ":")
		if len(parts) != 3 || parts[0] != "125" || parts[1] != leg.Wallet || parts[2] != row.Fill.Token.Address || seen[row.LogicalPartition] || accounting.ValidatePartitionPosition(row.Position) != nil || row.Position.SourceOrder != row.Fill.Position {
			return update, wireWatermark, "", delivery.ErrIntegrity
		}
		seen[row.LogicalPartition] = true
		own := watermark
		own.Height = row.Fill.Position.Height
		own.BlockHash = row.Fill.Position.BlockHash
		own.ProjectionRevision = strconv.FormatInt(row.Revision, 10)
		timestamp, e := strconv.ParseInt(row.Fill.BlockTimestampSeconds, 10, 64)
		if e != nil {
			return update, wireWatermark, "", e
		}
		own.ObservedAt = time.Unix(timestamp, 0).UTC().Format("2006-01-02T15:04:05.000Z")
		p, e := ProjectPosition(leg.Wallet, parts[2], row.Inventory, own, nil, "unpriced")
		if e != nil {
			return update, wireWatermark, "", e
		}
		positions = append(positions, p)
	}
	snapshot := uuid.NewSHA1(uuid.NameSpaceOID, []byte("portfolio:"+c.generation+":"+leg.Wallet+":"+revision.String()+":"+g.Position.EffectIdentity)).String()
	summary, err := l17.Aggregate(leg.Wallet, snapshot, watermark, positions)
	if err != nil {
		return update, wireWatermark, "", err
	}
	notice := l17.Notice{Wallet: leg.Wallet, SnapshotID: snapshot, Revision: revision.String(), Algorithm: Algorithm}
	payload, err := json.Marshal(notice)
	if err != nil {
		return update, wireWatermark, "", err
	}
	eventID := delivery.Hash([]byte("portfolio:" + c.generation + ":" + g.Position.EffectIdentity))
	envelope := delivery.Envelope{EventID: eventID, EventType: c.portfolioSubject, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: newest.Fill.Position.Height, TransactionIndex: newest.Fill.Position.TransactionIndex, LogIndex: newest.Fill.Position.LogIndex}, EmittedAt: observed, TraceID: eventID, PayloadHash: delivery.Hash(payload), Payload: payload}
	if _, err = c.portfolioRepository.CommitFullProjectionDeliveryTx(ctx, tx, c.portfolioOutbox, c.portfolioSubject, g.Position.EffectIdentity, summary, positions, time.Now().UTC().Add(24*time.Hour), envelope); err != nil {
		return update, wireWatermark, "", err
	}
	if err = c.commitPortfolioHistoryTx(ctx, tx, leg.Wallet, snapshot, g.Position.EffectIdentity); err != nil {
		return update, wireWatermark, "", err
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		return update, wireWatermark, "", err
	}
	var wireSummary interfaces.PortfolioSummary
	if err = json.Unmarshal(raw, &wireSummary); err != nil {
		return update, wireWatermark, "", err
	}
	raw, err = json.Marshal(watermark)
	if err != nil {
		return update, wireWatermark, "", err
	}
	if err = json.Unmarshal(raw, &wireWatermark); err != nil {
		return update, wireWatermark, "", err
	}
	changes := make([]interfaces.PositionChange, 0, 100)
	for _, p := range positions {
		if len(positions) > 100 && p.TokenAddress != leg.Token {
			continue
		}
		raw, e := json.Marshal(p)
		if e != nil {
			return update, wireWatermark, "", e
		}
		var wire interfaces.PortfolioPosition
		if e = json.Unmarshal(raw, &wire); e != nil {
			return update, wireWatermark, "", e
		}
		changes = append(changes, interfaces.PositionChange{Operation: "upsert", PositionId: p.PositionID, Revision: interfaces.PositiveUInt256(p.SourceWatermark.ProjectionRevision), Position: &wire})
	}
	update = interfaces.PortfolioUpdate{SnapshotId: snapshot, SummaryRevision: interfaces.PositiveUInt256(revision.String()), Summary: wireSummary, Changes: changes, CompleteBatch: len(positions) <= 100}
	raw, err = json.Marshal(update)
	if err != nil {
		return update, wireWatermark, "", err
	}
	if err = interfaces.ValidateJSON("PortfolioUpdate", raw); err != nil {
		return update, wireWatermark, "", err
	}
	return update, wireWatermark, watermark.ObservedAt, nil
}

func (c *AccountingConsumer) commitPortfolioHistoryTx(ctx context.Context, tx pgx.Tx, wallet, snapshot, sourceEvent string) error {
	version, err := ownedVersion(c.generation)
	if err != nil {
		return err
	}
	prefix := "125:" + wallet + ":"
	rows, err := tx.Query(ctx, `SELECT i.identity,i.payload,f.payload,f.source_fact_digest FROM inventory_l16_inbox i JOIN inventory_l16_effects e ON e.projection_generation=$1 AND e.partition=i.partition AND e.leg_identity=i.identity JOIN indexer.indexer_accounting_fills_v1 f ON f.source_fill_id=convert_from(e.position,'UTF8')::jsonb->>'sourceEventId' WHERE i.consumer_version=$2 AND left(i.partition,length($3))=$3 ORDER BY (i.payload->>'Height')::numeric,(i.payload->>'TransactionIndex')::bigint,(i.payload->'Identity'->>'Log')::bigint`, c.generation, version, prefix)
	if err != nil {
		return err
	}
	records := make([]l17.HistoryRecord, 0)
	var firstAt, lastAt time.Time
	var quote *l17.AssetRef
	for rows.Next() {
		var identity, digest string
		var legRaw, fillRaw []byte
		if err = rows.Scan(&identity, &legRaw, &fillRaw, &digest); err != nil {
			rows.Close()
			return err
		}
		var leg Leg
		if err = json.Unmarshal(legRaw, &leg); err != nil {
			rows.Close()
			return err
		}
		fill, e := accounting.DecodeFillV1(fillRaw)
		if e != nil {
			rows.Close()
			return e
		}
		actual, e := accounting.FillDigest(fill)
		id, idErr := leg.Identity.Key()
		if e != nil || idErr != nil || string(actual) != digest || id != identity || leg.Wallet != wallet {
			rows.Close()
			return delivery.ErrIntegrity
		}
		chain, token := fill.Position.ChainID, fill.Token.Address
		decimals := int(fill.Token.Decimals)
		timestamp, e := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
		if e != nil {
			rows.Close()
			return e
		}
		at := time.Unix(timestamp, 0).UTC()
		if firstAt.IsZero() || at.Before(firstAt) {
			firstAt = at
		}
		if at.After(lastAt) {
			lastAt = at
		}
		currency, settlement := "USDL", fill.Settlement.Address
		quoteDecimals := int(fill.Settlement.Decimals)
		asset := l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &settlement, Decimals: &quoteDecimals, CurrencyCode: &currency}
		if quote == nil {
			quote = &asset
		} else if *quote.Address != settlement || *quote.Decimals != quoteDecimals {
			rows.Close()
			return delivery.ErrIntegrity
		}
		records = append(records, l17.HistoryRecord{EventID: identity, Kind: "trade", Asset: l17.AssetRef{Kind: "erc20", ChainID: &chain, Address: &token, Decimals: &decimals}, AmountRaw: leg.Quantity, QuoteAmountUsdlRaw: leg.ActualUSDL, BasisQuality: "complete", Position: l17.ChainPosition{ChainID: fill.Position.ChainID, Height: fill.Position.Height, TransactionIndex: fill.Position.TransactionIndex, LogIndex: fill.Position.LogIndex, TransactionHash: fill.Position.TransactionHash, BlockHash: fill.Position.BlockHash}})
		if len(records) > 1000000 {
			rows.Close()
			return ErrCostAllocationUnavailable
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = c.portfolioRepository.CommitHistoryTx(ctx, tx, wallet, snapshot, sourceEvent, records); err != nil {
		return err
	}
	if quote != nil && firstAt.Before(lastAt) {
		return c.portfolioRepository.CommitHistoryMetadataTx(ctx, tx, wallet, snapshot, l17.SnapshotHistoryMetadata{SourceEvent: sourceEvent, Start: firstAt, End: lastAt, Quote: *quote, Periods: []l17.ReturnPeriod{}})
	}
	return nil
}
