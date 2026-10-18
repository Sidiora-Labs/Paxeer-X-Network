package l16

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

// BaselineAheadWindowBlocks bounds which canonical creations receive
// ahead-of-fill baselines: those within this many blocks of the indexer's
// complete watermark (about 50 hours at chain 125's ~0.18 s blocks). Open pnl
// inventory partitions are rebound to every new source epoch regardless.
const BaselineAheadWindowBlocks = 1_000_000

// BaselineAheadFillWindowBlocks mirrors core/indexer's inventory deferral
// bound, processor.InventoryBaselineWaitBlocks = 300
// (core/indexer/internal/processor/inventory.go): the indexer holds a
// same-owner fill's accounting.inventory.v1 facet while its height is in
// [from-300, to] of the committing batch, i.e. above watermark-300, and then
// records the pair market-only for good (requirement
// accounting.inventory.v1:<code>-timeout). Fills inside this window that have
// no inventory publication are the most urgent baseline candidates.
const BaselineAheadFillWindowBlocks = 300

// baselineAheadRefusalCap bounds the in-process memory of pairs whose
// creation facts permanently refuse a fresh baseline.
const baselineAheadRefusalCap = 4096

// Candidate priorities, most urgent first (baselineCandidate.priority).
const (
	baselinePriorityPendingFill = iota
	baselinePriorityOpenPartition
	baselinePriorityCreation
	baselinePriorityEnrollee
)

// BaselineAhead issues fresh zero creation baselines bound to the indexer's
// current source lease (owner, fence), so the indexer (core/indexer processor
// inventory.go planInventory / creation.go qualifiedInventoryEnrichment) can
// publish a wallet's accounting.inventory.v1 partition from sequence 1. The
// indexer only reads a baseline bound to the lease of the batch it commits.
// It holds a wallet's unpublished same-owner fills for up to
// BaselineAheadFillWindowBlocks canonical blocks waiting for that baseline
// (and an open partition's next fill likewise after a fence change); past the
// bound the pair is recorded market-only permanently. Every tick therefore
// binds, in this order:
//
//   - wallets whose same-owner fill inside BaselineAheadFillWindowBlocks of
//     the watermark has no inventory publication yet (held by the indexer;
//     the earliest such fill first, as it times out first). They are exempt
//     from the "has not moved the token" filter: their fill moved it, and
//     Capture only refuses movement at the creation height;
//   - open pnl inventory partitions (rebound after each indexer fence change,
//     otherwise their next fill times out market-only and the partition
//     stalls),
//   - the creator of every canonical creation inside BaselineAheadWindowBlocks
//     that has not moved the token yet, and wallets already issued a baseline
//     for such a token (rebinding),
//   - actively enrolled live-portfolio wallets for such tokens they have not
//     moved yet.
//
// A tick handles at most one batch of pairs. Each Capture is the full
// BaselineAuthority proof (factory creation path, complete indexed movements
// cross-checked against RPC logs, code and zero balance at the creation block)
// inside the recovery epoch guard and the source lease fence; a pair already
// bound to the current lease is never captured again, and a refusal is
// remembered so it is not retried every tick. Capture runs on the primary node
// first and on the archive node when the primary cannot serve the
// creation-block state (pruned history).
type BaselineAhead struct {
	primary, archive *BaselineAuthority
	batch            int
	refused          map[string]struct{}
	order            []string
}

func NewBaselineAhead(primary, archive *BaselineAuthority, batch int) (*BaselineAhead, error) {
	if primary == nil || archive == nil || primary == archive || batch <= 0 || batch > 256 {
		return nil, errors.New("baseline ahead: primary and archive authorities and a bounded batch required")
	}
	if primary.pool != archive.pool || primary.schema != archive.schema || primary.sourceStream != archive.sourceStream || primary.consumerID != archive.consumerID || primary.generation != archive.generation || primary.owner != archive.owner || primary.projectionVersion != archive.projectionVersion || primary.factory != archive.factory || primary.epochGuard == nil || archive.epochGuard == nil {
		return nil, errors.New("baseline ahead: authorities must share one bound identity")
	}
	return &BaselineAhead{primary: primary, archive: archive, batch: batch, refused: map[string]struct{}{}}, nil
}

type baselineCandidate struct {
	wallet, token string
	priority      int
}

// candidates lists the pairs that still lack a baseline bound to the current
// source lease, most urgent first. It returns no pairs while the indexer holds
// no live lease (nothing could be bound).
func (b *BaselineAhead) candidates(ctx context.Context) ([]baselineCandidate, error) {
	a := b.primary
	refused := make([]string, 0, len(b.order))
	refused = append(refused, b.order...)
	rows, err := a.pool.Query(ctx, baselineCandidatesQuery(a), a.sourceStream, a.consumerID, a.generation, int64(BaselineAheadWindowBlocks), refused, b.batch, int64(BaselineAheadFillWindowBlocks))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]baselineCandidate, 0, b.batch)
	for rows.Next() {
		var c baselineCandidate
		if err = rows.Scan(&c.wallet, &c.token, &c.priority); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// baselineCandidatesQuery reads only the reviewed pnl_runtime grants: the
// indexer lease, watermark, canonical creation, raw log, accounting fill,
// accounting publication and token transfer views, and pnl's own tables.
// Pending fills are found through the raw log canonical-order index (height)
// joined to the fills' unique (chain_id, transaction_hash, log_index) key.
func baselineCandidatesQuery(a *BaselineAuthority) string {
	return `WITH lease AS (
 SELECT owner_instance,fence_token FROM indexer.indexer_ingest_leases_v1 WHERE chain_id=125 AND stream_id=$1 AND expires_at>clock_timestamp()
), head AS (
 SELECT LEAST(raw_complete_height,typed_complete_height) AS h FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1
), recent AS (
 SELECT c.token,c.creator,c.height FROM indexer.indexer_canonical_creations_v1 c,head WHERE c.height<=head.h AND c.height>head.h-$4
), pending AS MATERIALIZED (
 SELECT f.payload->'attribution'->>'payer' AS wallet,f.payload->'token'->>'address' AS token,min(r.height) AS height
  FROM head,indexer.indexer_raw_logs_v1 r JOIN indexer.indexer_accounting_fills_v1 f ON f.chain_id=r.chain_id AND f.transaction_hash=r.transaction_hash AND f.log_index=r.log_index
  WHERE r.chain_id=125 AND r.height>head.h-$7
   AND f.payload->'attribution'->>'payer'=f.payload->'attribution'->>'recipient'
   AND NOT EXISTS(SELECT 1 FROM indexer.indexer_accounting_publications_v1 p WHERE p.payload->>'facet'='inventory' AND p.payload->>'sourceFillId'=f.source_fill_id)
  GROUP BY 1,2
), candidates AS (
 SELECT p.wallet,p.token,` + strconv.Itoa(baselinePriorityPendingFill) + ` AS priority,p.height AS urgent,p.height,true AS exempt FROM pending p
 UNION ALL SELECT split_part(o.partition_key,':',2),split_part(o.partition_key,':',3),` + strconv.Itoa(baselinePriorityOpenPartition) + `,NULL::bigint,0::bigint,true
  FROM ` + a.table("delivery_partition_offsets") + ` o WHERE o.consumer_id=$2 AND o.projection_generation=$3 AND o.partition_kind='inventory' AND o.partition_key ~ '^125:0x[0-9a-f]{40}:0x[0-9a-f]{40}$'
 UNION ALL SELECT r.creator,r.token,` + strconv.Itoa(baselinePriorityCreation) + `,NULL::bigint,r.height,false FROM recent r
 UNION ALL SELECT DISTINCT b.wallet,b.token,` + strconv.Itoa(baselinePriorityCreation) + `,NULL::bigint,r.height,true FROM ` + a.table("inventory_l16_baselines") + ` b JOIN recent r ON r.token=b.token WHERE b.consumer_id=$2 AND b.projection_generation=$3 AND b.source_stream=$1
 UNION ALL SELECT o.wallet,r.token,` + strconv.Itoa(baselinePriorityEnrollee) + `,NULL::bigint,r.height,false FROM ` + a.table("inventory_l16_live_owners_v1") + ` o CROSS JOIN recent r WHERE o.cancelled_at IS NULL AND o.expires_at>clock_timestamp()
)
SELECT c.wallet,c.token,min(c.priority) FROM candidates c,lease
WHERE c.wallet ~ '^0x[0-9a-f]{40}$' AND c.token ~ '^0x[0-9a-f]{40}$'
 AND NOT ('125:'||c.wallet||':'||c.token = ANY($5::text[]))
 AND NOT EXISTS(SELECT 1 FROM ` + a.table("inventory_l16_baselines") + ` x WHERE x.consumer_id=$2 AND x.projection_generation=$3 AND x.partition='125:'||c.wallet||':'||c.token AND x.source_stream=$1 AND x.source_owner=lease.owner_instance AND x.source_epoch=lease.fence_token)
 AND (c.exempt OR NOT EXISTS(SELECT 1 FROM indexer.indexer_token_transfers_v1 t WHERE t.chain_id=125 AND t.token=c.token AND (t.from_address=c.wallet OR t.to_address=c.wallet)))
GROUP BY c.wallet,c.token
ORDER BY min(c.priority),min(c.urgent) NULLS LAST,max(c.height) DESC,c.wallet,c.token
LIMIT $6`
}

func (b *BaselineAhead) refuse(key string) {
	if _, ok := b.refused[key]; ok {
		return
	}
	if len(b.order) >= baselineAheadRefusalCap {
		delete(b.refused, b.order[0])
		b.order = b.order[1:]
	}
	b.refused[key] = struct{}{}
	b.order = append(b.order, key)
}

// RunOnce binds up to one batch of pending pairs to the current source lease
// and returns how many baselines it issued. A pair whose creation facts refuse
// a fresh baseline is remembered; a lease change, an incomplete canonical
// range or an unavailable node leaves the pair for the next tick.
func (b *BaselineAhead) RunOnce(ctx context.Context) (int, error) {
	pending, err := b.candidates(ctx)
	if err != nil {
		return 0, err
	}
	issued := 0
	var failures []error
	for _, c := range pending {
		if ctx.Err() != nil {
			return issued, ctx.Err()
		}
		key := "125:" + c.wallet + ":" + c.token
		_, err = b.primary.Capture(ctx, c.wallet, c.token)
		if err != nil && !errors.Is(err, ErrBaselineUnavailable) && !errors.Is(err, delivery.ErrLease) && !errors.Is(err, delivery.ErrGap) && !errors.Is(err, pgx.ErrNoRows) && ctx.Err() == nil {
			_, err = b.archive.Capture(ctx, c.wallet, c.token)
		}
		switch {
		case err == nil:
			issued++
		case errors.Is(err, ErrBaselineUnavailable) || errors.Is(err, pgx.ErrNoRows):
			b.refuse(key)
		case errors.Is(err, delivery.ErrLease) || errors.Is(err, delivery.ErrGap):
			// The indexer fence moved or the creation block is not yet
			// canonically complete: retried against the next lease.
		case errors.Is(err, delivery.ErrIntegrity):
			b.refuse(key)
			failures = append(failures, errors.New(key+": "+err.Error()))
		default:
			failures = append(failures, errors.New(key+": "+err.Error()))
		}
	}
	if len(failures) > 0 {
		msgs := make([]string, 0, len(failures))
		for _, f := range failures {
			msgs = append(msgs, f.Error())
		}
		return issued, errors.New("baseline ahead: " + strings.Join(msgs, "; "))
	}
	return issued, nil
}

// Drain runs RunOnce until a tick issues nothing (bounded by ctx), so a new
// source epoch rebinds every open partition promptly.
func (b *BaselineAhead) Drain(ctx context.Context) error {
	for {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
		n, err := b.RunOnce(bounded)
		cancel()
		if err != nil || n == 0 {
			return err
		}
	}
}
