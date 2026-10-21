package processor

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
)

// InventoryBaselineWaitBlocks is the production bound, in canonical blocks,
// for which a same-owner fill's accounting.inventory.v1 facet is held waiting
// for pnl's creation baseline (about 54 s at chain 125's ~0.18 s blocks: pnl's
// BaselineAhead polls every 250 ms and one Capture is ~8 RPC round trips).
const InventoryBaselineWaitBlocks = 300

// inventoryWindowCap bounds the unpublished fills one canonical commit
// re-evaluates. Fills beyond it in source order wait for the next commit; a
// fill that leaves the window undecided is never published (fail closed), and
// every later fill of its wallet/token pair is then recorded market-only.
const inventoryWindowCap = 4096

// Inventory deferral rule (accounting.inventory.v1, fresh-same-owner-v1).
//
// pnl bootstraps a wallet/token inventory partition at sequence 1 from its
// zero creation baseline and replays every same-owner leg, so the partition
// may only start at the wallet's first same-owner fill and must stay gap-free.
// pnl issues baselines asynchronously (BaselineAhead), so a wallet's first
// fill can be committed before its baseline exists. Instead of skipping such a
// fill (which would leave the pair market-only forever), the indexer DEFERS
// the pair's inventory publication:
//
//   - The market facet, the canonical facts and every other pair are never
//     held: they are published in the fill's own canonical commit.
//   - Every canonical commit covering heights [from, to] re-evaluates, in
//     source order, each same-owner fill with height in [from-W, to] (W =
//     InventoryBaselineWaitBlocks) that has neither an inventory publication
//     nor an inventory degradation record.
//   - A fill is published iff its enrichment qualifies against pnl's baseline
//     bound to THIS commit's source lease (owner, fence) and every earlier
//     same-owner fill of the pair is already in the partition or planned
//     earlier in this commit. A fill from an earlier commit is staged keyed to
//     its own raw log identity (store.InsertCommittedOutboxIntent).
//   - A fill refused for a pending reason (baseline not issued yet, movement
//     range not complete) holds the pair: later fills of the pair are not
//     evaluated in this commit. The commit whose range contains height+W is
//     the fill's last evaluation; still refused there, it is decided
//     market-only with requirement accounting.inventory.v1:<code>-timeout.
//   - A permanent refusal (routed or non-same-owner fill, unresolved wallet
//     movement, bounded replay exceeded) is decided market-only at once.
//   - A fill whose earlier same-owner fill was decided market-only (or left
//     its window undecided) is decided market-only with
//     accounting.inventory.v1:earlier-fill-market-only: the partition is never
//     started or resumed mid-history.
//
// Decisions are recorded in indexer.indexer_capability_degradation (append
// only; requirement accounting.inventory.v1:<code>, raw_digest of the fill's
// raw log), which is the auditable reason a pair stayed market-only. The bound
// is in canonical blocks, not wall-clock time: each decision is a function of
// the committed history and the baselines visible to the deciding commit, and
// it is taken inside that commit's fenced transaction, so single-writer and
// fencing semantics are those of the canonical commit itself. While the
// indexer catches up, W blocks pass faster than pnl may issue baselines; such
// pairs stay market-only.
type inventoryPlan struct {
	fill       accounting.FillV1
	enrichment accounting.EnrichmentV1
	key        string
	// deferred marks a fill committed by an earlier canonical batch.
	deferred bool
}

type inventoryCandidate struct {
	fill accounting.FillV1
	raw  store.RawRecord
}

func planInventory(ctx context.Context, c store.CanonicalTx, b store.CanonicalBatch, generation, version string, wait int64) ([]inventoryPlan, error) {
	if wait <= 0 || len(b.Blocks) == 0 {
		return nil, errors.New("inventory wait bound and canonical blocks required")
	}
	from, to := b.PreviousHeight+1, b.Blocks[len(b.Blocks)-1].Height
	low := from - wait
	if low < 0 {
		low = 0
	}
	candidates, err := inventoryCandidates(ctx, c, b.StreamID, low, to)
	if err != nil {
		return nil, err
	}
	plans := []inventoryPlan{}
	planned := map[string]map[string]bool{}
	const pending, terminal = 1, 2
	blocked := map[string]int{}
	for _, candidate := range candidates {
		fill := candidate.fill
		a := fill.Attribution
		if a.Payer == nil || a.Recipient == nil || *a.Payer != *a.Recipient {
			// No same-owner wallet: the fill belongs to no inventory partition.
			continue
		}
		key := "125:" + *a.Payer + ":" + fill.Token.Address
		final := candidate.raw.Height+wait <= to
		switch blocked[key] {
		case terminal:
			if err = degradeInventory(ctx, c, b, candidate.raw, "earlier-fill-market-only"); err != nil {
				return nil, err
			}
			continue
		case pending:
			// An earlier fill of the pair is held and not final, so neither
			// is this one.
			continue
		}
		enrichment, prior, err := qualifiedInventoryEnrichment(ctx, c, b, fill, generation, version)
		contiguous := false
		if err == nil {
			contiguous, err = inventoryPartitionContiguous(ctx, c.Tx, version, key, fill, prior, planned[key])
		}
		var refusal *inventoryRefusal
		if errors.As(err, &refusal) {
			if refusal.pending && !final {
				blocked[key] = pending
				continue
			}
			code := refusal.code
			if refusal.pending {
				code += "-timeout"
			}
			if err = degradeInventory(ctx, c, b, candidate.raw, code); err != nil {
				return nil, err
			}
			blocked[key] = terminal
			continue
		}
		if err != nil {
			return nil, err
		}
		if !contiguous {
			if err = degradeInventory(ctx, c, b, candidate.raw, "earlier-fill-market-only"); err != nil {
				return nil, err
			}
			blocked[key] = terminal
			continue
		}
		if planned[key] == nil {
			planned[key] = map[string]bool{}
		}
		planned[key][fill.SourceFillID] = true
		plans = append(plans, inventoryPlan{fill: fill, enrichment: enrichment, key: key, deferred: candidate.raw.Height < from})
	}
	return plans, nil
}

// inventoryCandidates lists, in source order, the accounting fills at heights
// [low, to] that have neither an accounting.inventory.v1 publication nor an
// inventory degradation decision.
func inventoryCandidates(ctx context.Context, c store.CanonicalTx, stream string, low, to int64) ([]inventoryCandidate, error) {
	rows, err := c.Tx.Query(ctx, `SELECT f.payload,r.transaction_hash,r.log_index,r.height,r.transaction_index,r.block_hash,r.emitter,r.topics,r.data
FROM indexer.indexer_raw_logs r JOIN indexer.indexer_accounting_fills f ON f.chain_id=r.chain_id AND f.transaction_hash=r.transaction_hash AND f.log_index=r.log_index
WHERE r.chain_id=125 AND r.height BETWEEN $1 AND $2
 AND NOT EXISTS(SELECT 1 FROM indexer.indexer_ingest_outbox o WHERE o.chain_id=125 AND o.transaction_hash=r.transaction_hash AND o.log_index=r.log_index AND o.event_kind=$3)
 AND NOT EXISTS(SELECT 1 FROM indexer.indexer_capability_degradation d WHERE d.chain_id=125 AND d.stream_id=$4 AND d.transaction_hash=r.transaction_hash AND d.log_index=r.log_index AND starts_with(d.requirement,$3||':'))
ORDER BY r.height,r.transaction_index,r.log_index LIMIT $5`, low, to, accounting.InventorySubject, stream, inventoryWindowCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []inventoryCandidate{}
	for rows.Next() {
		var payload, topics []byte
		var candidate inventoryCandidate
		var logIndex, txIndex int64
		if err = rows.Scan(&payload, &candidate.raw.TransactionHash, &logIndex, &candidate.raw.Height, &txIndex, &candidate.raw.BlockHash, &candidate.raw.Emitter, &topics, &candidate.raw.Data); err != nil {
			return nil, err
		}
		if logIndex < 0 || logIndex > 1<<32-1 || txIndex < 0 || txIndex > 1<<32-1 {
			return nil, store.ErrIntegrity
		}
		candidate.raw.LogIndex, candidate.raw.TransactionIndex = uint32(logIndex), uint32(txIndex)
		if err = json.Unmarshal(topics, &candidate.raw.Topics); err != nil {
			return nil, store.ErrIntegrity
		}
		if candidate.fill, err = accounting.DecodeFillV1(payload); err != nil {
			return nil, err
		}
		if candidate.fill.Position.TransactionHash != candidate.raw.TransactionHash || candidate.fill.Position.LogIndex != strconv.FormatInt(logIndex, 10) || candidate.fill.Position.Height != strconv.FormatInt(candidate.raw.Height, 10) || candidate.fill.Position.BlockHash != candidate.raw.BlockHash {
			return nil, store.ErrIntegrity
		}
		out = append(out, candidate)
	}
	return out, rows.Err()
}

// degradeInventory records that the fill at raw is decided market-only for
// accounting.inventory.v1, with the reason code.
func degradeInventory(ctx context.Context, c store.CanonicalTx, b store.CanonicalBatch, raw store.RawRecord, code string) error {
	digest, err := store.RawDigest(raw)
	if err != nil {
		return err
	}
	_, err = c.Tx.Exec(ctx, `INSERT INTO indexer.indexer_capability_degradation(chain_id,stream_id,transaction_hash,log_index,requirement,raw_digest) VALUES(125,$1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, b.StreamID, raw.TransactionHash, int64(raw.LogIndex), accounting.InventorySubject+":"+code, digest)
	return err
}
