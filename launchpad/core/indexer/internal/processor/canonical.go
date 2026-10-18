package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// ProcessCanonicalBatch requires authenticated completeness including empty
// block hashes. It projects in chain order and derives every durable delivery
// intent from the actual projected event, including exact decoded quantities.
// A supplied Outbox is an assertion: nil derives it, empty/mismatched rejects.
// No network publisher is called from this transaction.
func (p *Processor) ProcessCanonicalBatch(ctx context.Context, b store.CanonicalBatch, timestamps map[uint64]int64, txFrom map[string]string) error {
	if b.ChainID != p.deps.ChainID {
		return fmt.Errorf("canonical chain mismatch")
	}
	b.Records = append([]store.RawRecord(nil), b.Records...)
	sort.Slice(b.Records, func(i, j int) bool {
		a, z := b.Records[i], b.Records[j]
		if a.Height != z.Height {
			return a.Height < z.Height
		}
		if a.TransactionIndex != z.TransactionIndex {
			return a.TransactionIndex < z.TransactionIndex
		}
		return a.LogIndex < z.LogIndex
	})
	return p.deps.Store.CommitProjectedCanonical(ctx, b, func(ctx context.Context, st *store.Store) ([]store.OutboxIntent, error) {
		q := *p
		q.deps = p.deps
		q.deps.Store = st
		q.nftCache = make(map[string]*int64)
		intents := make([]store.OutboxIntent, 0, len(b.Records))
		for _, r := range b.Records {
			topics := make([]common.Hash, len(r.Topics))
			for i, t := range r.Topics {
				topics[i] = common.HexToHash(t)
			}
			ev, err := p.deps.Decoder.Decode(ethtypes.Log{Address: common.HexToAddress(r.Emitter), Topics: topics, Data: r.Data, BlockNumber: uint64(r.Height), TxHash: common.HexToHash(r.TransactionHash), TxIndex: uint(r.TransactionIndex), BlockHash: common.HexToHash(r.BlockHash), Index: uint(r.LogIndex)})
			if err != nil {
				return nil, fmt.Errorf("required decode: %w", err)
			}
			if ev == nil {
				continue
			}
			ts := timestamps[uint64(r.Height)]
			if ts <= 0 {
				return nil, fmt.Errorf("missing block timestamp")
			}
			event, err := q.processEvent(ctx, ev, ts, txFrom)
			if err != nil {
				return nil, err
			}
			if event != nil {
				payload, err := json.Marshal(event)
				if err != nil {
					return nil, err
				}
				intents = append(intents, store.OutboxIntent{TransactionHash: r.TransactionHash, LogIndex: r.LogIndex, Kind: event.EventName, Payload: payload})
			}
		}
		return intents, nil
	})
}

func AccountQualified(ctx context.Context, q *receipt.QualifiedReceipt) ([]accounting.FillV1, error) {
	if q == nil {
		return nil, errors.New("source-issued receipt required")
	}
	fills, err := q.Fills(ctx)
	if err != nil {
		return nil, err
	}
	for _, fill := range fills {
		if err := accounting.ValidateFillV1(fill); err != nil {
			return nil, err
		}
	}
	return fills, nil
}

func (p *Processor) ProcessQualifiedCanonicalBatch(ctx context.Context, b store.CanonicalBatch, receipts []*receipt.QualifiedReceipt) error {
	if p == nil || p.deps.Store == nil || !p.deps.Store.CanonicalEpochGuarded() || p.deps.AccountingPartitions == nil || p.deps.Durable == nil || p.deps.AccountingProjectionVersion == "" || p.deps.ChainID != 125 || b.ChainID != 125 {
		return errors.New("qualified canonical producer dependencies required")
	}
	if err := receipt.ValidateBindings(ctx, b, receipts); err != nil {
		return err
	}
	fills := make([]accounting.FillV1, 0)
	intents := make([]store.OutboxIntent, 0)
	selected := make(map[string]bool)
	for _, q := range receipts {
		qualified, err := AccountQualified(ctx, q)
		if err != nil {
			return err
		}
		for _, fill := range qualified {
			key := fill.Position.TransactionHash + ":" + fill.Position.LogIndex
			if selected[key] {
				return store.ErrIntegrity
			}
			selected[key] = true
			fills = append(fills, fill)
		}
	}
	for _, q := range receipts {
		events, err := qualifiedEventsWithActors(ctx, q)
		if err != nil {
			return err
		}
		for _, event := range events {
			if event == nil || event.BlockNumber > math.MaxInt64 || uint64(event.LogIndex) > math.MaxUint32 {
				return store.ErrIntegrity
			}
			key := event.TxHash + ":" + strconv.FormatUint(uint64(event.LogIndex), 10)
			if event.EventName == "Swap" {
				if !selected[key] {
					return errors.New("primary swap lacks qualified accounting fact")
				}
				continue
			}
			payload, err := json.Marshal(event)
			if err != nil {
				return err
			}
			intents = append(intents, store.OutboxIntent{TransactionHash: event.TxHash, LogIndex: uint32(event.LogIndex), Kind: event.EventName, Payload: payload})
		}
	}
	sort.Slice(fills, func(i, j int) bool {
		a, z := fills[i], fills[j]
		if a.PoolAddress != z.PoolAddress {
			return a.PoolAddress < z.PoolAddress
		}
		cmp, _ := accounting.CompareSourcePosition(a.Position, z.Position)
		return cmp < 0
	})
	var reserves []reserveSnapshot
	if p.deps.LegacyProjection {
		var err error
		if reserves, err = p.readSwapReserves(ctx, fills); err != nil {
			return err
		}
	}
	return p.deps.Store.CommitProjectedCanonicalTx(ctx, b, func(ctx context.Context, canonical store.CanonicalTx) ([]store.OutboxIntent, error) {
		if err := receipt.ValidateBindings(ctx, b, receipts); err != nil {
			return nil, err
		}
		if p.deps.Store.LifecycleEnabled() {
			if err := canonical.ResolveSourceQualifiedPoisonTx(ctx, b); err != nil {
				return nil, err
			}
		}
		creationIntents, err := stageQualifiedSourceFacts(ctx, canonical, b, receipts, p.deps.Durable)
		if err != nil {
			return nil, err
		}
		if p.deps.LegacyProjection {
			if err = p.projectQualifiedLegacy(ctx, canonical.Store, b, receipts, reserves); err != nil {
				return nil, err
			}
		}
		derived := append([]store.OutboxIntent(nil), intents...)
		derived = append(derived, creationIntents...)
		type plan struct {
			fill                accounting.FillV1
			enrichment          *accounting.EnrichmentV1
			facet, version, key string
			digest              accounting.Digest
			// deferred: an inventory facet of a fill committed by an
			// earlier canonical batch (inventory.go deferral rule).
			deferred bool
		}
		plans := make([]plan, 0, len(fills)*2)
		for _, fill := range fills {
			digest, err := canonical.InsertAccountingFill(ctx, fill)
			if err != nil {
				return nil, err
			}
			plans = append(plans, plan{fill: fill, facet: "market", version: p.deps.AccountingProjectionVersion, key: "125:" + fill.PoolAddress, digest: digest})
		}
		if p.deps.InventoryGeneration != "" {
			if p.deps.InventoryProjectionVersion == "" {
				return nil, errors.New("inventory projection version required")
			}
			wait := p.deps.InventoryBaselineWaitBlocks
			if wait == 0 {
				wait = InventoryBaselineWaitBlocks
			}
			inventory, err := planInventory(ctx, canonical, b, p.deps.InventoryGeneration, p.deps.InventoryProjectionVersion, wait)
			if err != nil {
				return nil, err
			}
			for _, entry := range inventory {
				enrichment := entry.enrichment
				digest, err := accounting.FacetDigest(accounting.PublicationV1{Enrichment: &enrichment})
				if err != nil {
					return nil, err
				}
				plans = append(plans, plan{fill: entry.fill, enrichment: &enrichment, facet: "inventory", version: p.deps.InventoryProjectionVersion, key: entry.key, digest: digest, deferred: entry.deferred})
			}
		}
		sort.Slice(plans, func(i, j int) bool {
			a, z := plans[i], plans[j]
			an, zn := a.version+"\x00"+a.facet+"\x00"+a.key, z.version+"\x00"+z.facet+"\x00"+z.key
			if an != zn {
				return an < zn
			}
			cmp, _ := accounting.CompareSourcePosition(a.fill.Position, z.fill.Position)
			return cmp < 0
		})
		for _, entry := range plans {
			evidence := accounting.Digest("")
			if entry.enrichment != nil {
				evidence = entry.enrichment.EvidenceDigest
			}
			effect := accounting.EffectIdentity(entry.fill.SourceFillID, entry.facet, entry.version, evidence, entry.facet, entry.key)
			position, err := p.deps.AccountingPartitions.AppendTx(ctx, canonical.Tx, delivery.AppendInput{ProjectionVersion: entry.version, PartitionKind: entry.facet, PartitionKey: entry.key, SourceEventID: entry.fill.SourceFillID, EffectIdentity: effect, PayloadDigest: entry.digest, SourceOrder: entry.fill.Position})
			if err != nil {
				return nil, err
			}
			var publication accounting.PublicationV1
			if entry.enrichment == nil {
				publication, err = accounting.NewMarketPublication(entry.fill, []accounting.PartitionPosition{position})
			} else {
				publication, err = accounting.NewEnrichmentPublication(*entry.enrichment, []accounting.PartitionPosition{position})
			}
			if err != nil {
				return nil, err
			}
			subject, err := accounting.SubjectForFacet(entry.facet)
			if err != nil {
				return nil, err
			}
			if err = p.deps.Durable.StagePublication(ctx, canonical.Tx, subject, publication); err != nil {
				return nil, err
			}
			payload, err := accounting.CanonicalPublicationV1(publication)
			if err != nil {
				return nil, err
			}
			logIndex, err := strconv.ParseUint(entry.fill.Position.LogIndex, 10, 32)
			if err != nil {
				return nil, err
			}
			intent := store.OutboxIntent{TransactionHash: entry.fill.Position.TransactionHash, LogIndex: uint32(logIndex), Kind: subject, Payload: payload}
			if entry.deferred {
				if err = canonical.InsertCommittedOutboxIntent(ctx, b, intent); err != nil {
					return nil, err
				}
				continue
			}
			derived = append(derived, intent)
		}
		return derived, nil
	})
}

func qualifiedEventsWithActors(ctx context.Context, q *receipt.QualifiedReceipt) ([]*decode.Event, error) {
	events, err := q.Events(ctx)
	if err != nil {
		return nil, err
	}
	envelopes, err := q.QualifiedEvents(ctx)
	if err != nil {
		return nil, err
	}
	txHash, origin, _, err := q.SourceContext(ctx)
	if err != nil {
		return nil, err
	}
	actors := map[string]decode.ActorContext{}
	for _, envelope := range envelopes {
		if envelope == nil || envelope.Actors == nil {
			continue
		}
		if envelope.Raw.Identity.TransactionHash != txHash {
			return nil, store.ErrIntegrity
		}
		copy := *envelope.Actors
		copy.TransactionOrigin = &origin
		actors[fmt.Sprintf("%s:%d", txHash, envelope.Raw.Identity.LogIndex)] = copy
	}
	for _, event := range events {
		if event == nil {
			return nil, store.ErrIntegrity
		}
		if actor, ok := actors[fmt.Sprintf("%s:%d", event.TxHash, event.LogIndex)]; ok {
			event.Args = cloneArgs(event.Args)
			event.Args["actors"] = &actor
		}
	}
	return events, nil
}
