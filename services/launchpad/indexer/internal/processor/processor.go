// Package processor is the indexer spine: it polls the chain head, fetches logs
// for each block range via the configured source, decodes launchpad events,
// writes the typed tables idempotently, and fans the events out as signed
// webhooks AFTER the DB write (so a downstream consumer never sees an event
// whose row failed to persist). Parity with the TS block-processor.ts.
package processor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
)

// NFTResolver resolves a pool's NFT id via the PoolRegistry (MarketCreated
// enrichment). A nil resolver, or a resolver error, yields a null nft_id —
// matching the TS fail-open behaviour.
type NFTResolver interface {
	NftIDByPool(ctx context.Context, pool common.Address) (*big.Int, error)
}

// Deps wires a Processor. All fields except NFT are required.
type QualifiedBatchSource interface {
	FetchQualifiedBatch(context.Context, uint64, uint64) (store.CanonicalBatch, []*receipt.QualifiedReceipt, error)
}

// ReserveReader reads a pool's reserves pinned to a block identity.
type ReserveReader interface {
	PoolReserves(context.Context, common.Address, common.Hash) (receipt.PoolReserves, error)
}

type Deps struct {
	Quarantine                  *QuarantinePolicy
	HistoricalReplay            *ReplayRunner
	HistoricalRequest           *store.ReplayRequest
	QualifiedBatchSource        QualifiedBatchSource
	CanonicalStreamID           string
	CanonicalOwner              string
	AccountingPartitions        *delivery.PartitionStore
	Durable                     *publisher.DurableStager
	AccountingProjectionVersion string
	InventoryGeneration         string
	InventoryProjectionVersion  string
	// InventoryBaselineWaitBlocks bounds the inventory deferral (inventory.go);
	// zero selects the production InventoryBaselineWaitBlocks.
	InventoryBaselineWaitBlocks int64
	Source                      source.LogSource
	Head                        source.HeadSource
	Store                       *store.Store
	Publisher                   *publisher.Publisher
	Decoder                     *decode.Decoder
	NFT                         NFTResolver
	EventEmitterAddress         string
	// LegacyProjection also writes the typed read tables (indexer.pools,
	// swaps, pool_state_snapshots, fee_*, airdrop_claims, ...) from qualified
	// canonical batches, inside the same canonical transaction. Production
	// readers across core-api, stats, ranking, pnl, charts, metadata, social
	// and user resolve markets and trades through these tables.
	LegacyProjection bool
	// Reserves reads pool reserves pinned to a swap's block for the legacy
	// pool_state_snapshots projection (required with LegacyProjection).
	Reserves     ReserveReader
	ChainID      int32
	StartBlock   int64
	BatchSize    int
	Concurrency  int
	PollInterval time.Duration
	Logger       *slog.Logger
}

// Processor indexes launchpad events into Postgres + webhooks.
type Processor struct {
	readyFence       *atomic.Int64
	canonicalFence   int64
	canonicalRenewed time.Time
	deps             Deps
	monitored        []string
	nftCache         map[string]*int64
}

// New builds a Processor.
func New(d Deps) *Processor {
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	if d.BatchSize <= 0 {
		d.BatchSize = 50
	}
	if d.PollInterval <= 0 {
		d.PollInterval = 100 * time.Millisecond
	}
	return &Processor{
		readyFence: new(atomic.Int64),
		deps:       d,
		monitored:  []string{strings.ToLower(d.EventEmitterAddress)},
		nftCache:   map[string]*int64{},
	}
}

// Run starts the live block-processing loop, blocking until ctx is cancelled.
func (p *Processor) Run(ctx context.Context) (result error) {
	defer func() {
		if p.deps.QualifiedBatchSource != nil && p.canonicalFence != 0 {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			result = errors.Join(result, p.deps.Store.ReleaseLease(cleanup, p.deps.ChainID, p.deps.CanonicalStreamID, p.deps.CanonicalOwner, p.canonicalFence))
			p.canonicalFence = 0
			p.readyFence.Store(0)
		}
	}()
	last := p.deps.StartBlock
	if p.deps.QualifiedBatchSource != nil {
		if p.deps.CanonicalStreamID == "" {
			return fmt.Errorf("processor: canonical stream required")
		}
		checkpoint, err := p.deps.Store.GetCanonicalWatermark(ctx, p.deps.ChainID, p.deps.CanonicalStreamID)
		if err != nil {
			return fmt.Errorf("processor: canonical watermark: %w", err)
		}
		last = checkpoint
	} else {
		cursor, err := p.deps.Store.GetCursor(ctx, p.deps.ChainID)
		if err != nil {
			return fmt.Errorf("processor: load cursor: %w", err)
		}
		if cursor != nil {
			last = *cursor
		}
	}
	logSource := "qualified-rpc"
	if p.deps.QualifiedBatchSource == nil {
		logSource = p.deps.Source.Name()
	}
	p.deps.Logger.Info("starting block processor", slog.Int64("lastProcessed", last), slog.String("logSource", logSource), slog.String("headSource", p.deps.Head.Name()))

	size := p.deps.BatchSize
	for {
		if p.deps.QualifiedBatchSource != nil && p.canonicalFence != 0 && time.Since(p.canonicalRenewed) >= 10*time.Second {
			if err := p.deps.Store.RenewLease(ctx, p.deps.ChainID, p.deps.CanonicalStreamID, p.deps.CanonicalOwner, p.canonicalFence, 30*time.Second); err != nil {
				p.canonicalFence = 0
				p.readyFence.Store(0)
				return err
			}
			p.canonicalRenewed = time.Now()
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		head, err := p.deps.Head.Head(ctx)
		if err != nil {
			p.deps.Logger.Error("get head failed, retrying", slog.String("err", err.Error()))
			if !sleep(ctx, p.deps.PollInterval) {
				return ctx.Err()
			}
			continue
		}
		from := last + 1
		if from > head {
			if !sleep(ctx, p.deps.PollInterval) {
				return ctx.Err()
			}
			continue
		}
		to := from + int64(size) - 1
		if to > head {
			to = head
		}
		if err := p.ProcessBatch(ctx, from, to); err != nil {
			p.deps.Logger.Error("block processing error, retrying", slog.Int64("from", from), slog.Int64("to", to), slog.String("err", err.Error()))
			// A range whose qualification cannot finish inside the bounded
			// read window (dense protocol activity) is retried smaller, so one
			// heavy range never stalls ingestion; it regrows after success.
			size = max(1, size/2)
			if !sleep(ctx, 5*time.Second) {
				return ctx.Err()
			}
			continue
		}
		size = min(p.deps.BatchSize, size*2)
		last = to
	}
}

// ProcessBatch fetches, decodes, persists, and fans out the events for the
// inclusive [from,to] block range, then advances the cursor. Exported so tests
// drive one batch directly against real Postgres + a real RPC/SQL source.
func (p *Processor) ProcessBatch(ctx context.Context, from, to int64) error {
	if p.deps.QualifiedBatchSource != nil {
		if from < 0 || to < from {
			return fmt.Errorf("invalid canonical source range")
		}
		if p.deps.CanonicalStreamID == "" || p.deps.CanonicalOwner == "" {
			return fmt.Errorf("canonical stream and process owner required")
		}
		if p.canonicalFence == 0 {
			fence, err := p.deps.Store.AcquireLease(ctx, p.deps.ChainID, p.deps.CanonicalStreamID, p.deps.CanonicalOwner, 30*time.Second)
			if err != nil {
				return err
			}
			p.canonicalFence = fence
			p.readyFence.Store(fence)
		} else if err := p.deps.Store.RenewLease(ctx, p.deps.ChainID, p.deps.CanonicalStreamID, p.deps.CanonicalOwner, p.canonicalFence, 30*time.Second); err != nil {
			p.canonicalFence = 0
			p.readyFence.Store(0)
			return err
		}
		checkpoint, err := p.deps.Store.GetCanonicalWatermark(ctx, p.deps.ChainID, p.deps.CanonicalStreamID)
		if err != nil {
			return err
		}
		if checkpoint != from-1 && checkpoint < to {
			return store.ErrWatermark
		}
		readCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		batch, receipts, err := p.deps.QualifiedBatchSource.FetchQualifiedBatch(readCtx, uint64(from), uint64(to))
		cancel()
		if err != nil {
			if p.deps.Quarantine != nil && p.deps.Source != nil {
				original, fetchErr := p.deps.Source.FetchLogs(ctx, source.FetchOptions{MonitoredAddresses: p.monitored, FromBlock: from, ToBlock: to, Concurrency: p.deps.Concurrency})
				if fetchErr != nil {
					return fmt.Errorf("qualified source failed: %w; original input fetch: %v", err, fetchErr)
				}
				if retainErr := p.RetainSourcePoison(ctx, original); retainErr != nil {
					return retainErr
				}
			}
			return err
		}
		if batch.ChainID != p.deps.ChainID || len(batch.Blocks) == 0 || batch.Blocks[0].Height != from || batch.Blocks[len(batch.Blocks)-1].Height != to {
			return fmt.Errorf("qualified source range mismatch")
		}
		if err = p.deps.Store.RenewLease(ctx, p.deps.ChainID, p.deps.CanonicalStreamID, p.deps.CanonicalOwner, p.canonicalFence, 30*time.Second); err != nil {
			p.canonicalFence = 0
			p.readyFence.Store(0)
			return err
		}
		p.canonicalRenewed = time.Now()
		batch.StreamID = p.deps.CanonicalStreamID
		batch.Owner = p.deps.CanonicalOwner
		batch.Fence = p.canonicalFence
		batch.PreviousHeight = from - 1
		if batch.BatchID == "" {
			batch.BatchID = fmt.Sprintf("%d-%d-%s", from, to, strings.TrimPrefix(batch.Blocks[len(batch.Blocks)-1].Hash, "0x"))
		}
		if checkpoint >= to {
			if err = receipt.ValidateBindings(ctx, batch, receipts); err != nil {
				return err
			}
			committed, err := p.deps.Store.ReconcileCanonicalCommit(ctx, batch)
			if err != nil {
				return err
			}
			if !committed {
				return store.ErrWatermark
			}
			return nil
		}
		return p.ProcessQualifiedCanonicalBatch(ctx, batch, receipts)
	}
	events, err := p.collect(ctx, from, to)
	if err != nil {
		return err
	}
	if err := p.deps.Store.UpsertCursor(ctx, p.deps.ChainID, to); err != nil {
		return fmt.Errorf("upsert cursor: %w", err)
	}
	// Fan out AFTER the DB commit + cursor advance (i9 ordering).
	if p.deps.Publisher != nil {
		p.deps.Publisher.PublishBatch(ctx, events)
	}
	return nil
}

// collect fetches the range, decodes + persists every matched event, and
// returns the webhook envelopes (no cursor advance, no fan-out). Shared by the
// live ProcessBatch and the backfill processor (which discards the envelopes).
func (p *Processor) collect(ctx context.Context, from, to int64) ([]publisher.WebhookEvent, error) {
	res, err := p.deps.Source.FetchLogs(ctx, source.FetchOptions{
		MonitoredAddresses: p.monitored,
		FromBlock:          from,
		ToBlock:            to,
		Concurrency:        p.deps.Concurrency,
	})
	if err != nil {
		return nil, fmt.Errorf("fetch logs [%d..%d]: %w", from, to, err)
	}

	if p.deps.Quarantine != nil {
		if err = p.RetainSourcePoison(ctx, res); err != nil {
			return nil, err
		}
	}
	var events []publisher.WebhookEvent
	for i := range res.Logs {
		ev, derr := p.deps.Decoder.Decode(res.Logs[i])
		if derr != nil {
			p.deps.Logger.Warn("decode log failed", slog.String("err", derr.Error()))
			continue
		}
		if ev == nil {
			continue
		}
		blockTs, ok := res.BlockTimestamps[ev.BlockNumber]
		if !ok || blockTs == 0 {
			p.deps.Logger.Warn("skipping event — no valid block timestamp",
				slog.Uint64("blockNumber", ev.BlockNumber), slog.String("txHash", ev.TxHash))
			continue
		}
		wev, herr := p.processEvent(ctx, ev, blockTs, res.TxFromMap)
		if herr != nil {
			return nil, fmt.Errorf("process %s @ %d: %w", ev.EventName, ev.BlockNumber, herr)
		}
		if wev != nil {
			events = append(events, *wev)
		}
	}
	return events, nil
}

// processEvent persists one decoded event to its typed table and returns the
// webhook envelope to fan out (nil for events with no downstream contract).
func (p *Processor) processEvent(ctx context.Context, ev *decode.Event, blockTs int64, txFrom map[string]string) (*publisher.WebhookEvent, error) {
	blockNumber := int64(ev.BlockNumber)
	logIndex := int32(ev.LogIndex)
	id := fmt.Sprintf("%s-%d", ev.TxHash, ev.LogIndex)
	traceID := fmt.Sprintf("%s:%d", ev.TxHash, ev.LogIndex)

	// serializedArgs is the webhook arg map (decode already humanized bigints
	// to strings). Enrichments below mutate this copy, not the decoder output.
	args := cloneArgs(ev.Args)

	wev := func() *publisher.WebhookEvent {
		return &publisher.WebhookEvent{
			EventName:      ev.EventName,
			BlockNumber:    blockNumber,
			BlockTimestamp: blockTs,
			TxHash:         ev.TxHash,
			LogIndex:       int(ev.LogIndex),
			Args:           args,
			TraceID:        traceID,
		}
	}

	switch ev.EventName {
	case "MarketCreated":
		if err := p.handleMarketCreated(ctx, args, ev.TxHash, blockNumber, blockTs); err != nil {
			return nil, err
		}
		return wev(), nil

	case "Swap":
		eventSender := asString(args["sender"])
		actualUser := eventSender
		poolAddr, err := p.handleSwap(ctx, args, id, eventSender, actualUser, blockNumber, blockTs, logIndex)
		if err != nil {
			return nil, err
		}
		args["poolAddress"] = poolAddr
		args["actors"] = unknownLegacyActors(eventSender)
		args["blockTimestamp"] = blockTs
		return wev(), nil

	case "PoolStateUpdated":
		poolID := asString(args["poolId"])
		if err := p.handlePoolStateUpdated(ctx, args, poolID, ev.TxHash, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		poolAddr := ""
		if pool, err := p.deps.Store.GetPoolByPoolID(ctx, poolID); err != nil {
			return nil, err
		} else if pool != nil {
			poolAddr = pool.PoolAddress
		}
		args["poolAddress"] = poolAddr
		return wev(), nil

	case "FeeRecorded":
		if err := p.handleFeeRecorded(ctx, args, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return wev(), nil

	case "FeeDistributed":
		if err := p.handleFeeDistributed(ctx, args, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return wev(), nil

	case "FeeStrategyChanged":
		if err := p.handleFeeStrategyChanged(ctx, args, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return wev(), nil

	case "AirdropClaimed":
		if err := p.handleAirdropClaimed(ctx, args, ev.EventName, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return nil, nil

	case "FeeFlow":
		if asString(args["kind"]) != feeFlowKindAirdropClaimed {
			return nil, nil
		}
		if err := p.handleAirdropClaimed(ctx, args, ev.EventName, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return nil, nil

	case "OpticalExecuted":
		if err := p.handleOpticalExecuted(ctx, args, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return wev(), nil

	case "TokenForTokenSwap":
		actualUser := asString(args["sender"])
		if err := p.handleTokenForTokenSwap(ctx, args, id, actualUser, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		args["actors"] = unknownLegacyActors(actualUser)
		args["blockTimestamp"] = blockTs
		return wev(), nil

	case "ConfigUpdated":
		if err := p.handleConfigUpdated(ctx, args, id, blockNumber, blockTs, logIndex); err != nil {
			return nil, err
		}
		return wev(), nil

	default:
		return nil, nil
	}
}

func (p *Processor) handleMarketCreated(ctx context.Context, args map[string]any, txHash string, blockNumber, blockTs int64) error {
	pool := asString(args["pool"])
	nftID := p.resolveNftID(ctx, pool)
	return p.deps.Store.InsertPool(ctx, sqlcdb.InsertPoolParams{
		PoolAddress:  pool,
		TokenAddress: asString(args["token"]),
		Creator:      asString(args["creator"]),
		Optical:      asString(args["optical"]),
		PoolID:       asString(args["poolId"]),
		NftID:        nftID,
		CreatedAt:    blockTs,
		CreatedBlock: blockNumber,
		TxHash:       txHash,
	})
}

func (p *Processor) handleSwap(ctx context.Context, args map[string]any, id, eventSender, actualUser string, blockNumber, blockTs int64, logIndex int32) (string, error) {
	poolID := asString(args["poolId"])
	poolAddr := ""
	if pool, err := p.deps.Store.GetPoolByPoolID(ctx, poolID); err != nil {
		return "", err
	} else if pool != nil {
		poolAddr = pool.PoolAddress
	}
	router := eventSender
	return poolAddr, p.deps.Store.InsertSwap(ctx, sqlcdb.InsertSwapParams{
		ID:             id,
		PoolID:         poolID,
		PoolAddress:    poolAddr,
		Sender:         actualUser,
		Router:         &router,
		IsBuy:          asBool(args["isBuy"]),
		AmountIn:       asString(args["amountIn"]),
		AmountOut:      asString(args["amountOut"]),
		Fee:            asString(args["fee"]),
		Price:          asString(args["price"]),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         strings.SplitN(id, "-", 2)[0],
		LogIndex:       logIndex,
	})
}

func (p *Processor) handlePoolStateUpdated(ctx context.Context, args map[string]any, poolID, txHash string, blockNumber, blockTs int64, logIndex int32) error {
	return p.deps.Store.InsertPoolStateSnapshot(ctx, sqlcdb.InsertPoolStateSnapshotParams{
		ID:             fmt.Sprintf("%s-%d-%d", poolID, blockNumber, logIndex),
		PoolID:         poolID,
		VirtualReserve: asString(args["virtualReserve"]),
		RealReserve:    asString(args["realReserve"]),
		TokenReserve:   asString(args["tokenReserve"]),
		Price:          asString(args["price"]),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHash,
		LogIndex:       logIndex,
	})
}

func (p *Processor) handleFeeRecorded(ctx context.Context, args map[string]any, id string, blockNumber, blockTs int64, logIndex int32) error {
	return p.deps.Store.InsertFeeEvent(ctx, sqlcdb.InsertFeeEventParams{
		ID:             id,
		PoolID:         asString(args["poolId"]),
		FeeAmount:      asString(args["feeAmount"]),
		ProtocolCut:    asString(args["protocolCut"]),
		PoolCut:        asString(args["poolCut"]),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHashFromID(id),
		LogIndex:       logIndex,
	})
}

func (p *Processor) handleFeeDistributed(ctx context.Context, args map[string]any, id string, blockNumber, blockTs int64, logIndex int32) error {
	nftID, err := asInt64(args["nftId"])
	if err != nil {
		return fmt.Errorf("nftId: %w", err)
	}
	strategy, err := asInt64(args["strategy"])
	if err != nil {
		return fmt.Errorf("strategy: %w", err)
	}
	return p.deps.Store.InsertFeeDistribution(ctx, sqlcdb.InsertFeeDistributionParams{
		ID:             id,
		PoolID:         asString(args["poolId"]),
		NftID:          nftID,
		Strategy:       int32(strategy),
		Amount:         asString(args["amount"]),
		Recipient:      asString(args["recipient"]),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHashFromID(id),
		LogIndex:       logIndex,
	})
}

func (p *Processor) handleFeeStrategyChanged(ctx context.Context, args map[string]any, id string, blockNumber, blockTs int64, logIndex int32) error {
	nftID, err := asInt64(args["nftId"])
	if err != nil {
		return fmt.Errorf("nftId: %w", err)
	}
	oldStrat, err := asInt64(args["oldStrategy"])
	if err != nil {
		return fmt.Errorf("oldStrategy: %w", err)
	}
	newStrat, err := asInt64(args["newStrategy"])
	if err != nil {
		return fmt.Errorf("newStrategy: %w", err)
	}
	return p.deps.Store.InsertFeeStrategyChange(ctx, sqlcdb.InsertFeeStrategyChangeParams{
		ID:             id,
		PoolID:         asString(args["poolId"]),
		NftID:          nftID,
		OldStrategy:    int32(oldStrat),
		NewStrategy:    int32(newStrat),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHashFromID(id),
		LogIndex:       logIndex,
	})
}

// feeFlowKindAirdropClaimed is the IEventEmitter.FeeFlow kind code mirroring a
// FeeAccumulator AirdropClaimed settlement (0=Recorded 1=Claimed 2=Burned
// 3=AirdropTriggered 4=AirdropClaimed 5=LpRewardsSent ...).
const feeFlowKindAirdropClaimed = "4"

// handleAirdropClaimed persists one holder airdrop claim. Three emitted shapes
// reach it: FeeAccumulator AirdropClaimed(pool, holder, amount, epoch), the
// EventEmitter FeeFlow(kind=4, pool, party, amount, ..., epoch) mirror, and
// FeesRouter AirdropClaimed(nftId, holder, amount). The pool identity is
// resolved from the registered pools table by address or by NFT id; a claim
// that cannot be attributed is still recorded (holder, amount, evidence) with a
// null pool so no settlement evidence is dropped and nothing is invented.
func (p *Processor) handleAirdropClaimed(ctx context.Context, args map[string]any, eventName, id string, blockNumber, blockTs int64, logIndex int32) error {
	holder := asString(args["holder"])
	if holder == "" {
		holder = asString(args["party"])
	}
	if holder == "" {
		return fmt.Errorf("airdrop claim without holder")
	}
	amount := asString(args["amount"])
	if amount == "" {
		return fmt.Errorf("airdrop claim without amount")
	}
	var pool *sqlcdb.IndexerPool
	var nftID *int64
	if address := asString(args["pool"]); address != "" {
		found, err := p.deps.Store.GetPoolByAddress(ctx, address)
		if err != nil {
			return err
		}
		pool = found
		if pool == nil {
			p.deps.Logger.Warn("airdrop claim for unregistered pool", slog.String("pool", address), slog.String("event", eventName))
		}
	} else if raw, present := args["nftId"]; present {
		n, err := asInt64(raw)
		if err != nil {
			return fmt.Errorf("nftId: %w", err)
		}
		nftID = &n
		found, err := p.deps.Store.GetPoolByNftID(ctx, n)
		if err != nil {
			return err
		}
		pool = found
		if pool == nil {
			p.deps.Logger.Warn("airdrop claim for unregistered nft", slog.Int64("nftId", n), slog.String("event", eventName))
		}
	} else {
		return fmt.Errorf("airdrop claim without pool or nftId")
	}
	params := sqlcdb.InsertAirdropClaimParams{
		ID:             id,
		NftID:          nftID,
		Holder:         holder,
		Amount:         amount,
		EventName:      eventName,
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHashFromID(id),
		LogIndex:       logIndex,
	}
	if epoch := asString(args["epoch"]); epoch != "" {
		params.Epoch = &epoch
	}
	if pool != nil {
		poolID, poolAddress := pool.PoolID, pool.PoolAddress
		params.PoolID = &poolID
		params.PoolAddress = &poolAddress
		if params.NftID == nil {
			params.NftID = pool.NftID
		}
	} else if address := asString(args["pool"]); address != "" {
		params.PoolAddress = &address
	}
	return p.deps.Store.InsertAirdropClaim(ctx, params)
}

func (p *Processor) handleOpticalExecuted(ctx context.Context, args map[string]any, id string, blockNumber, blockTs int64, logIndex int32) error {
	return p.deps.Store.InsertOpticalExecution(ctx, sqlcdb.InsertOpticalExecutionParams{
		ID:             id,
		PoolID:         asString(args["poolId"]),
		Optical:        asString(args["optical"]),
		HookName:       asString(args["hookName"]),
		Data:           asString(args["data"]),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHashFromID(id),
		LogIndex:       logIndex,
	})
}

func (p *Processor) handleTokenForTokenSwap(ctx context.Context, args map[string]any, id, actualUser string, blockNumber, blockTs int64, logIndex int32) error {
	return p.deps.Store.InsertTokenForTokenSwap(ctx, sqlcdb.InsertTokenForTokenSwapParams{
		ID:               id,
		Sender:           actualUser,
		TokenIn:          asString(args["tokenIn"]),
		TokenOut:         asString(args["tokenOut"]),
		PoolIn:           asString(args["poolIn"]),
		PoolOut:          asString(args["poolOut"]),
		AmountIn:         asString(args["amountIn"]),
		IntermediateUsdl: asString(args["intermediateUsdl"]),
		AmountOut:        asString(args["amountOut"]),
		FeeIn:            asString(args["feeIn"]),
		FeeOut:           asString(args["feeOut"]),
		BlockNumber:      blockNumber,
		BlockTimestamp:   blockTs,
		TxHash:           txHashFromID(id),
		LogIndex:         logIndex,
	})
}

func (p *Processor) handleConfigUpdated(ctx context.Context, args map[string]any, id string, blockNumber, blockTs int64, logIndex int32) error {
	return p.deps.Store.InsertConfigUpdate(ctx, sqlcdb.InsertConfigUpdateParams{
		ID:             id,
		Key:            asString(args["key"]),
		OldValue:       asString(args["oldValue"]),
		NewValue:       asString(args["newValue"]),
		BlockNumber:    blockNumber,
		BlockTimestamp: blockTs,
		TxHash:         txHashFromID(id),
		LogIndex:       logIndex,
	})
}

// resolveNftID looks up (and caches) the pool's NFT id, returning nil on a
// missing resolver or any lookup error (fail-open parity with the TS handler).
func (p *Processor) resolveNftID(ctx context.Context, pool string) *int64 {
	key := strings.ToLower(pool)
	if v, ok := p.nftCache[key]; ok {
		return v
	}
	if p.deps.NFT == nil {
		p.nftCache[key] = nil
		return nil
	}
	bi, err := p.deps.NFT.NftIDByPool(ctx, common.HexToAddress(pool))
	if err != nil || bi == nil {
		p.deps.Logger.Warn("failed to read nftId for pool", slog.String("pool", pool))
		p.nftCache[key] = nil
		return nil
	}
	v := bi.Int64()
	p.nftCache[key] = &v
	return &v
}

func txHashFromID(id string) string { return strings.SplitN(id, "-", 2)[0] }

// ── arg coercion helpers ───────────────────────────────────────────

func cloneArgs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+3)
	for k, v := range in {
		out[k] = v
	}
	return out
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

func asBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case *big.Int:
		return x.Sign() != 0
	default:
		return false
	}
}

func asInt64(v any) (int64, error) {
	switch x := v.(type) {
	case string:
		return strconv.ParseInt(x, 10, 64)
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case uint8:
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		return int64(x), nil
	case *big.Int:
		return x.Int64(), nil
	default:
		return 0, fmt.Errorf("cannot coerce %T to int64", v)
	}
}

// sleep waits d or returns false if ctx is cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (p *Processor) Ready(ctx context.Context) error {
	if p == nil || p.readyFence == nil || p.deps.Store == nil || p.deps.QualifiedBatchSource == nil {
		return errors.New("qualified ingestion unavailable")
	}
	return p.deps.Store.IngestReady(ctx, p.deps.ChainID, p.deps.CanonicalStreamID, p.deps.CanonicalOwner, p.readyFence.Load())
}

func unknownLegacyActors(caller string) *decode.ActorContext {
	actors := &decode.ActorContext{AttributionConfidence: "unknown", AttributionMethod: "unavailable", EvidenceIdentity: []decode.RawIdentity{}, Revision: "actor-v1"}
	if common.IsHexAddress(caller) && common.HexToAddress(caller) != (common.Address{}) {
		normalized := strings.ToLower(common.HexToAddress(caller).Hex())
		actors.EventCaller = &normalized
	}
	return actors
}
