package processor

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// projectQualifiedLegacy writes the typed read tables from a qualified
// canonical batch through the transaction-bound store. It applies exactly the
// projection of the legacy log path (EventEmitter logs decoded by the protocol
// registry, in chain order) to records that were already qualified, so the
// tables only ever hold source-qualified facts. Block timestamps and senders
// come from the qualified receipts; NFT ids come from the qualified factory
// MarketCreated facts of the same batch, so no chain read happens inside the
// canonical transaction.
func (p *Processor) projectQualifiedLegacy(ctx context.Context, st *store.Store, b store.CanonicalBatch, receipts []*receipt.QualifiedReceipt, reserves []reserveSnapshot) error {
	emitter := strings.ToLower(p.deps.EventEmitterAddress)
	if st == nil || p.deps.Decoder == nil || !common.IsHexAddress(emitter) {
		return errors.New("legacy projection dependencies required")
	}
	timestamps := make(map[string]int64, len(receipts))
	senders := make(map[string]string, len(receipts))
	for _, q := range receipts {
		txHash, sender, timestamp, err := q.SourceContext(ctx)
		if err != nil {
			return err
		}
		if timestamp <= 0 {
			return store.ErrIntegrity
		}
		timestamps[txHash] = timestamp
		senders[txHash] = sender
	}
	facts, err := qualifiedCreationFacts(ctx, receipts)
	if err != nil {
		return err
	}
	q := *p
	q.deps.Store = st
	q.nftCache = make(map[string]*int64, len(facts))
	for _, f := range facts {
		id, err := strconv.ParseInt(f.NFTID, 10, 64)
		if err != nil {
			return fmt.Errorf("creation nft id: %w", err)
		}
		q.nftCache[strings.ToLower(f.Pool)] = &id
	}
	for _, r := range b.Records {
		if r.Emitter != emitter {
			continue
		}
		timestamp, ok := timestamps[r.TransactionHash]
		if !ok {
			return store.ErrIntegrity
		}
		topics := make([]common.Hash, len(r.Topics))
		for i, t := range r.Topics {
			topics[i] = common.HexToHash(t)
		}
		ev, err := p.deps.Decoder.Decode(ethtypes.Log{Address: common.HexToAddress(r.Emitter), Topics: topics, Data: r.Data, BlockNumber: uint64(r.Height), TxHash: common.HexToHash(r.TransactionHash), TxIndex: uint(r.TransactionIndex), BlockHash: common.HexToHash(r.BlockHash), Index: uint(r.LogIndex)})
		if err != nil {
			return fmt.Errorf("legacy projection decode: %w", err)
		}
		if ev == nil {
			continue
		}
		if _, err = q.processEvent(ctx, ev, timestamp, senders); err != nil {
			return fmt.Errorf("legacy projection %s @ %d: %w", ev.EventName, ev.BlockNumber, err)
		}
	}
	for _, r := range reserves {
		timestamp, ok := timestamps[r.TransactionHash]
		if !ok {
			return store.ErrIntegrity
		}
		if err = st.InsertPoolStateSnapshot(ctx, sqlcdb.InsertPoolStateSnapshotParams{
			ID:             fmt.Sprintf("%s-%d-%d", r.PoolID, r.Height, r.LogIndex),
			PoolID:         r.PoolID,
			VirtualReserve: r.Reserves.VirtualUSDL.String(),
			RealReserve:    r.Reserves.RealUSDL.String(),
			TokenReserve:   r.Reserves.Token.String(),
			Price:          r.Reserves.Price.String(),
			BlockNumber:    r.Height,
			BlockTimestamp: timestamp,
			TxHash:         r.TransactionHash,
			LogIndex:       int32(r.LogIndex),
		}); err != nil {
			return fmt.Errorf("legacy projection reserves: %w", err)
		}
	}
	return nil
}

// reserveSnapshot is a pool's pinned post-block reserve state, attributed to
// the block's last qualified swap on that pool.
type reserveSnapshot struct {
	PoolID, TransactionHash string
	Height                  int64
	LogIndex                uint32
	Reserves                receipt.PoolReserves
}

// readSwapReserves reads, outside the canonical transaction, the reserves of
// every pool with a qualified swap, pinned to the swap's block hash (EIP-1898).
// SidioraPool emits PoolStateUpdated only from syncReserves(), so a swap does
// not announce reserves. One read per (pool, block): the state after that
// block, attributed to its last swap on the pool.
func (p *Processor) readSwapReserves(ctx context.Context, fills []accounting.FillV1) ([]reserveSnapshot, error) {
	if len(fills) == 0 {
		return nil, nil
	}
	if p.deps.Reserves == nil {
		return nil, errors.New("legacy projection reserve reader required")
	}
	type key struct{ pool, block string }
	last := map[key]accounting.FillV1{}
	order := []key{}
	for _, f := range fills {
		k := key{f.PoolAddress, f.Position.BlockHash}
		prior, seen := last[k]
		if !seen {
			order = append(order, k)
			last[k] = f
			continue
		}
		cmp, err := accounting.CompareSourcePosition(prior.Position, f.Position)
		if err != nil {
			return nil, err
		}
		if cmp < 0 {
			last[k] = f
		}
	}
	out := make([]reserveSnapshot, 0, len(order))
	for _, k := range order {
		f := last[k]
		if !common.IsHexAddress(k.pool) || len(k.block) != 66 {
			return nil, store.ErrIntegrity
		}
		pool := common.HexToAddress(k.pool)
		reserves, err := p.deps.Reserves.PoolReserves(ctx, pool, common.HexToHash(k.block))
		if err != nil {
			return nil, fmt.Errorf("pinned pool reserves %s: %w", k.pool, err)
		}
		height, err := strconv.ParseInt(f.Position.Height, 10, 64)
		if err != nil {
			return nil, err
		}
		logIndex, err := strconv.ParseUint(f.Position.LogIndex, 10, 32)
		if err != nil {
			return nil, err
		}
		out = append(out, reserveSnapshot{PoolID: poolIDFor(pool), TransactionHash: f.Position.TransactionHash, Height: height, LogIndex: uint32(logIndex), Reserves: reserves})
	}
	return out, nil
}

// poolIDFor is the bytes32 pool id the protocol events carry for a pool: the
// address left-padded to 32 bytes, lowercase hex.
func poolIDFor(pool common.Address) string {
	return "0x" + hex.EncodeToString(common.LeftPadBytes(pool.Bytes(), 32))
}
