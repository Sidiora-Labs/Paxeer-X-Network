package l16

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrBaselineUnavailable = errors.New("fresh complete movement baseline unavailable")
var ErrAttributionUnavailable = errors.New("plain same-owner attribution unavailable")
var ErrCostAllocationUnavailable = errors.New("inventory cost allocation unavailable")

// The issuer must prove a fresh zero balance, complete actual mint/transfer
// history and exact settlement-transfer suppression through the fill frontier.
// Unsupported gifts, multihop or unresolved movements must return an error.
type FreshBaselineAuthority interface {
	VerifyFreshSameOwnerTx(context.Context, pgx.Tx, string, accounting.FillV1, accounting.PartitionPosition) error
	VerifyBootstrapTx(context.Context, pgx.Tx, delivery.BootstrapInput) error
}

type AccountingConsumer struct {
	pool                                             *pgxpool.Pool
	schema                                           string
	consumerID, generation, owner, projectionVersion string
	deliveries                                       *delivery.Store
	partitions                                       *delivery.PartitionStore
	authority                                        FreshBaselineAuthority
	epochGuard                                       func(context.Context, pgx.Tx) error
	streamWriter                                     *delivery.StreamWriter
	portfolioRepository                              *l17.Repository
	portfolioOutbox                                  *delivery.Store
	portfolioSubject                                 string
}

func NewAccountingConsumer(pool *pgxpool.Pool, schema, consumerID, generation, owner, projectionVersion string, authority FreshBaselineAuthority) (*AccountingConsumer, error) {
	if pool == nil || !regexpToken(schema) || !regexpToken(consumerID) || !regexpToken(generation) || !regexpToken(owner) || projectionVersion == "" || len(projectionVersion) > 128 {
		return nil, errors.New("invalid inventory consumer configuration")
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.InventorySubject: true}, EventTypes: map[string]bool{accounting.InventorySubject: true}}
	st, err := delivery.NewStore(pool, schema, policy)
	if err != nil {
		return nil, err
	}
	partitions, err := delivery.NewPartitionStore(pool, schema)
	if err != nil {
		return nil, err
	}
	return &AccountingConsumer{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), consumerID: consumerID, generation: generation, owner: owner, projectionVersion: projectionVersion, deliveries: st, partitions: partitions, authority: authority}, nil
}

func NewAccountingConsumerWithEpochGuard(pool *pgxpool.Pool, schema, consumerID, generation, owner, projectionVersion string, authority FreshBaselineAuthority, guard func(context.Context, pgx.Tx) error) (*AccountingConsumer, error) {
	if guard == nil {
		return nil, errors.New("inventory recovery epoch guard required")
	}
	c, err := NewAccountingConsumer(pool, schema, consumerID, generation, owner, projectionVersion, authority)
	if err != nil {
		return nil, err
	}
	c.deliveries, err = delivery.NewStoreWithEpochGuard(pool, schema, delivery.Policy{Subjects: map[string]bool{accounting.InventorySubject: true}, EventTypes: map[string]bool{accounting.InventorySubject: true}}, guard)
	if err != nil {
		return nil, err
	}
	c.epochGuard = guard
	return c, nil
}

func NewAccountingConsumerWithRuntime(pool *pgxpool.Pool, schema, consumerID, generation, owner, projectionVersion string, authority FreshBaselineAuthority, guard func(context.Context, pgx.Tx) error, writer delivery.StreamWriter) (*AccountingConsumer, error) {
	if writer.Schema != "api_stream" {
		return nil, errors.New("qualified portfolio stream schema required")
	}
	c, err := NewAccountingConsumerWithEpochGuard(pool, schema, consumerID, generation, owner, projectionVersion, authority, guard)
	if err != nil {
		return nil, err
	}
	c.streamWriter = &writer
	return c, nil
}

func regexpToken(v string) bool {
	if len(v) == 0 || len(v) > 128 {
		return false
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (c *AccountingConsumer) Bootstrap(ctx context.Context, in delivery.BootstrapInput) error {
	if c.authority == nil {
		return ErrBaselineUnavailable
	}
	if in.Lease.Key.ConsumerID != c.consumerID || in.Lease.Key.ProjectionGeneration != c.generation || in.Lease.Key.PartitionKind != "inventory" {
		return delivery.ErrIntegrity
	}
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer inventoryRollback(tx)
	if c.epochGuard != nil {
		if err = c.epochGuard(ctx, tx); err != nil {
			return err
		}
	}
	if err = c.partitions.BootstrapTx(ctx, tx, in, c.authority.VerifyBootstrapTx); err != nil {
		return err
	}
	if c.epochGuard != nil {
		if err = c.epochGuard(ctx, tx); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func inventoryRollback(tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

func ReadSourceFillTx(ctx context.Context, tx pgx.Tx, sourceFillID string, expected accounting.Digest) (accounting.FillV1, error) {
	var fill accounting.FillV1
	if tx == nil || !partitionHash(sourceFillID) || !partitionHash(string(expected)) {
		return fill, delivery.ErrIntegrity
	}
	var raw []byte
	var digest string
	err := tx.QueryRow(ctx, `SELECT payload,source_fact_digest FROM indexer.indexer_accounting_fills_v1 WHERE source_fill_id=$1`, sourceFillID).Scan(&raw, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return fill, delivery.ErrGap
	}
	if err != nil {
		return fill, err
	}
	fill, err = accounting.DecodeFillV1(raw)
	if err != nil {
		return fill, err
	}
	actual, err := accounting.FillDigest(fill)
	if err != nil || fill.SourceFillID != sourceFillID || actual != expected || digest != string(expected) {
		return fill, delivery.ErrIntegrity
	}
	return fill, nil
}

func partitionHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func InventoryLeg(fill accounting.FillV1, p accounting.PublicationV1) (Leg, error) {
	if accounting.ValidateFillV1(fill) != nil || accounting.ValidatePublicationV1(p) != nil || p.Facet != "inventory" || p.Enrichment == nil || p.Enrichment.Attribution == nil {
		return Leg{}, delivery.ErrIntegrity
	}
	digest, err := accounting.FillDigest(fill)
	if err != nil || fill.SourceFillID != p.SourceFillID || digest != p.SourceFactDigest || p.Positions[0].SourceOrder != fill.Position {
		return Leg{}, delivery.ErrIntegrity
	}
	a := p.Enrichment.Attribution
	if a.Quality == "unavailable" || a.Payer == nil || a.Recipient == nil || *a.Payer != *a.Recipient || a.TransactionSender != fill.Attribution.TransactionSender {
		return Leg{}, ErrAttributionUnavailable
	}
	wallet := *a.Payer
	if a.Trader != nil && *a.Trader != wallet || a.TransactionSender != wallet {
		return Leg{}, ErrCostAllocationUnavailable
	}
	if p.Positions[0].PartitionKey != "125:"+wallet+":"+fill.Token.Address {
		return Leg{}, delivery.ErrIntegrity
	}
	for _, original := range []*string{fill.Attribution.Payer, fill.Attribution.Recipient, fill.Attribution.Trader} {
		if original != nil && *original != wallet {
			return Leg{}, ErrCostAllocationUnavailable
		}
	}
	height, err := strconv.ParseUint(fill.Position.Height, 10, 63)
	if err != nil {
		return Leg{}, err
	}
	txIndex, err := strconv.ParseUint(fill.Position.TransactionIndex, 10, 32)
	if err != nil {
		return Leg{}, err
	}
	logIndex, err := strconv.ParseUint(fill.Position.LogIndex, 10, 32)
	if err != nil {
		return Leg{}, err
	}
	amount := fill.USDLAmountRaw
	leg := Leg{Identity: Identity{Chain: "125", Emitter: fill.ActualEmitter, Transaction: fill.Position.TransactionHash, Log: uint32(logIndex)}, Height: height, TransactionIndex: uint32(txIndex), Wallet: wallet, Token: fill.Token.Address, Sender: a.TransactionSender, Payer: wallet, Recipient: wallet, Kind: fill.Side, Quantity: fill.TokenAmountRaw, ActualUSDL: &amount, SettlementReference: string(p.Enrichment.EvidenceDigest)}
	if a.RouterCaller != nil {
		leg.RouterCaller = *a.RouterCaller
	}
	return leg, leg.Validate()
}

func (c *AccountingConsumer) Consume(ctx context.Context, subject string, wire []byte, message delivery.Delivery) error {
	if c.authority == nil {
		return ErrBaselineUnavailable
	}
	if subject != accounting.InventorySubject {
		return delivery.ErrIntegrity
	}
	wire = append([]byte(nil), wire...)
	// Lease acquisition uses the injected pool before the outer transaction owns a
	// connection; acquiring it inside Guard can exhaust a single-connection pool.
	var lease delivery.PartitionLease
	policy := delivery.Policy{Subjects: map[string]bool{accounting.InventorySubject: true}, EventTypes: map[string]bool{accounting.InventorySubject: true}}
	if outer, decodeErr := delivery.Decode(wire, policy, subject); decodeErr == nil {
		if publication, innerErr := accounting.DecodePublicationV1(outer.Payload); innerErr == nil && publication.Facet == "inventory" && publication.Enrichment != nil && publication.Positions[0].ProjectionVersion == c.projectionVersion {
			key := delivery.ConsumerKey{ConsumerID: c.consumerID, ProjectionGeneration: c.generation, PartitionKind: "inventory", PartitionKey: publication.Positions[0].PartitionKey}
			var acquireErr error
			lease, acquireErr = c.partitions.Acquire(ctx, key, c.owner, 30*time.Second)
			if acquireErr != nil {
				return acquireErr
			}
		}
	}
	var selected accounting.PublicationV1
	var fill accounting.FillV1
	var leg Leg
	var baselineProof qualifiedBaselineUse
	return c.deliveries.ConsumeOrdered(ctx, c.consumerID, c.generation, subject, wire, message, delivery.ConsumeHooks{
		Guard: func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope) (delivery.GuardedPosition, error) {
			var zero delivery.GuardedPosition
			var err error
			selected, err = accounting.DecodePublicationV1(envelope.Payload)
			if err != nil {
				return zero, &delivery.IntegrityFailure{Class: delivery.InnerSchema, Cause: err}
			}
			if selected.Facet != "inventory" || selected.Enrichment == nil || selected.Positions[0].ProjectionVersion != c.projectionVersion {
				return zero, &delivery.IntegrityFailure{Class: delivery.PartitionBinding, Cause: delivery.ErrIntegrity}
			}
			position := selected.Positions[0]
			if lease.Key.PartitionKey != position.PartitionKey {
				return zero, delivery.ErrIntegrity
			}
			guarded, err := c.partitions.CheckAndLockTx(ctx, tx, lease, position)
			if err != nil {
				return guarded, err
			}
			if _, err = tx.Exec(ctx, "SET LOCAL search_path TO "+c.schema+", pg_catalog"); err != nil {
				return guarded, err
			}
			fill, err = ReadSourceFillTx(ctx, tx, selected.SourceFillID, selected.SourceFactDigest)
			if err != nil {
				return guarded, err
			}
			leg, err = InventoryLeg(fill, selected)
			if err != nil {
				return guarded, err
			}
			baselineProof = qualifiedBaselineUse{}
			if concrete, ok := c.authority.(*BaselineAuthority); ok {
				err = concrete.qualifyFreshTx(ctx, tx, c.generation, fill, position, &baselineProof)
			} else {
				err = c.authority.VerifyFreshSameOwnerTx(ctx, tx, c.generation, fill, position)
			}
			if err != nil {
				return guarded, err
			}
			return guarded, nil
		},
		Effect: func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope, g delivery.GuardedPosition) error {
			if c.streamWriter != nil {
				if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "portfolio:"+c.generation+":"+leg.Wallet); err != nil {
					return err
				}
			}
			if concrete, ok := c.authority.(*BaselineAuthority); ok {
				if err := concrete.persistFreshUseTx(ctx, tx, baselineProof); err != nil {
					return err
				}
			}
			_, inserted, err := ApplyOwnedLegTx(ctx, tx, g, leg, c.generation)
			if err != nil {
				return err
			}
			if !inserted {
				return delivery.ErrIntegrity
			}
			if c.streamWriter != nil {
				return c.appendPortfolioTx(ctx, tx, g, leg, fill)
			}
			return nil
		},
		Advance: func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope, g delivery.GuardedPosition) error {
			return c.partitions.AdvanceTx(ctx, tx, g)
		},
	})
}

// DecodeInventoryState keeps the generation-specific persisted read boundary
// independent from legacy inventory rows.
func DecodeInventoryState(raw []byte) (Inventory, error) {
	var state Inventory
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	_, err := state.values()
	return state, err
}

func (c *AccountingConsumer) PreparePartition(ctx context.Context, partition string) error {
	authority, ok := c.authority.(*BaselineAuthority)
	if !ok || authority == nil {
		return ErrBaselineUnavailable
	}
	in, err := authority.PreparePartition(ctx, partition)
	if err != nil {
		return err
	}
	return c.Bootstrap(ctx, in)
}

func (c *AccountingConsumer) PrepareAndConsume(ctx context.Context, subject string, wire []byte, message delivery.Delivery) error {
	if c.authority == nil {
		return ErrBaselineUnavailable
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.InventorySubject: true}, EventTypes: map[string]bool{accounting.InventorySubject: true}}
	envelope, err := delivery.Decode(wire, policy, subject)
	if err != nil {
		return c.Consume(ctx, subject, wire, message)
	}
	publication, err := accounting.DecodePublicationV1(envelope.Payload)
	if err != nil || publication.Facet != "inventory" {
		return c.Consume(ctx, subject, wire, message)
	}
	partition := publication.Positions[0].PartitionKey
	var exists bool
	err = c.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+c.schema+".delivery_partition_offsets WHERE consumer_id=$1 AND projection_generation=$2 AND partition_kind='inventory' AND partition_key=$3)", c.consumerID, c.generation, partition).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		if err = c.PreparePartition(ctx, partition); err != nil {
			return err
		}
	}
	return c.Consume(ctx, subject, wire, message)
}

func (c *AccountingConsumer) Run(ctx context.Context, transport *natsdriver.Consumer) error {
	if transport == nil {
		return errors.New("actual inventory transport required")
	}
	return transport.Run(ctx, func(ctx context.Context, message *natsdriver.Message) error {
		return c.PrepareAndConsume(ctx, message.Subject, message.Wire, message)
	})
}

func (c *AccountingConsumer) WithFullPortfolio(repository l17.Repository, outbox *delivery.Store, subject string) (*AccountingConsumer, error) {
	if c == nil || c.streamWriter == nil || repository.Pool != c.pool || outbox == nil || subject != "portfolio.snapshot.v2" {
		return nil, errors.New("full portfolio runtime binding required")
	}
	copy := *c
	copy.portfolioRepository = &repository
	copy.portfolioOutbox = outbox
	copy.portfolioSubject = subject
	return &copy, nil
}
