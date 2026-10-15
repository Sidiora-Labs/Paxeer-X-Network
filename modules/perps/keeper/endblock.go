package keeper

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Event type and attributes of the error event a failed EndBlock step emits
// on the block's event manager.
const (
	eventTypeEndBlockError = "perps_endblock_error"
	attributeStep          = "step"
	attributeMarketID      = "market_id"
	attributeError         = "error"
)

// Names of the EndBlock steps of contracts/perps.md section 23, and of the
// pass-level stages that frame them in error events.
const (
	stepPrices         = "prices"
	stepPremiumSamples = "premium_samples"
	stepFundingIndex   = "funding_index"
	stepTriggers       = "triggers"
	stepExpiries       = "expiries"
	stepSweep          = "sweep"
	stepLiquidation    = "liquidation"
	stepDeficits       = "deficits"
	stepBackstopCloses = "backstop_closes"

	stageSetup   = "setup"
	stageReceipt = "receipt"
)

// endBlockStep is one bounded step of the pass.
type endBlockStep struct {
	name string
	run  func(k *Keeper, pass *endBlockPass)
}

// endBlockSteps are the nine steps of contracts/perps.md section 23 in the
// order EndBlock runs them.
var endBlockSteps = [...]endBlockStep{
	{stepPrices, (*Keeper).endBlockPrices},
	{stepPremiumSamples, (*Keeper).endBlockPremiumSamples},
	{stepFundingIndex, (*Keeper).endBlockFundingIndex},
	{stepTriggers, (*Keeper).endBlockTriggers},
	{stepExpiries, (*Keeper).endBlockExpiries},
	{stepSweep, (*Keeper).endBlockSweep},
	{stepLiquidation, (*Keeper).endBlockLiquidation},
	{stepDeficits, (*Keeper).endBlockDeficits},
	{stepBackstopCloses, (*Keeper).endBlockBackstopCloses},
}

// endBlockPass is the state of one EndBlock pass: ctx caches the block
// context until the receipt is written, events is the block's event manager,
// markets are the enabled markets in ascending id, skipped the markets a
// failed step removed from the rest of the block, records the logs of the
// pass and deficits the accounts the sweep and liquidation left with a
// deficit, with the market that recorded it.
type endBlockPass struct {
	ctx      sdk.Context
	events   *sdk.EventManager
	params   types.Params
	markets  []types.Market
	skipped  map[uint64]bool
	records  []types.EventRecord
	deficits []endBlockDeficit
}

type endBlockDeficit struct {
	account  common.Address
	marketID uint64
}

// EndBlock runs the nine bounded steps of contracts/perps.md section 23 in
// order and writes the logs of the pass as one synthetic receipt with
// WriteEndBlockReceipt. It moves no token: every step books store entries
// over the USDL balance of the perps module EVM address. Each market step
// runs in its own cached context, as does every global step, every account of
// the sweep and every deficit; a failure drops the writes of that unit, is
// emitted as a perps_endblock_error event and removes a failed market from
// the rest of the block. The pass is written only together with its receipt.
// It returns the records of the pass.
func (k *Keeper) EndBlock(ctx sdk.Context) []types.EventRecord {
	passCtx, writePass := ctx.CacheContext()
	pass := &endBlockPass{ctx: passCtx, events: ctx.EventManager(), skipped: make(map[uint64]bool)}
	params, err := k.GetParams(passCtx)
	if err != nil {
		k.endBlockFailure(pass, stageSetup, nil, err)
		return nil
	}
	pass.params = params
	if err := k.IterateMarkets(passCtx, func(market types.Market) bool {
		if market.Enabled {
			pass.markets = append(pass.markets, market)
		}
		return false
	}); err != nil {
		k.endBlockFailure(pass, stageSetup, nil, err)
		return nil
	}
	for _, step := range endBlockSteps {
		step.run(k, pass)
	}
	if err := k.WriteEndBlockReceipt(passCtx, pass.records); err != nil {
		k.endBlockFailure(pass, stageReceipt, nil, err)
		return nil
	}
	writePass()
	return pass.records
}

// endBlockFailure emits the error event of a failed step.
func (k *Keeper) endBlockFailure(pass *endBlockPass, step string, marketID *uint64, err error) {
	attributes := []sdk.Attribute{sdk.NewAttribute(attributeStep, step)}
	if marketID != nil {
		attributes = append(attributes, sdk.NewAttribute(attributeMarketID, strconv.FormatUint(*marketID, 10)))
	}
	attributes = append(attributes, sdk.NewAttribute(attributeError, err.Error()))
	pass.events.EmitEvent(sdk.NewEvent(eventTypeEndBlockError, attributes...))
}

// isolated runs one unit of a step in a cached context, keeping its writes
// and records only when it succeeds, and reports whether it did.
func (k *Keeper) isolated(pass *endBlockPass, step string, marketID *uint64,
	run func(ctx sdk.Context) ([]types.EventRecord, error)) bool {
	cacheCtx, write := pass.ctx.CacheContext()
	records, err := run(cacheCtx)
	if err != nil {
		k.endBlockFailure(pass, step, marketID, err)
		return false
	}
	write()
	pass.records = append(pass.records, records...)
	return true
}

// eachMarket runs a market step for every enabled market not yet skipped, in
// ascending id; a market whose step fails is skipped for the rest of the
// block.
func (k *Keeper) eachMarket(pass *endBlockPass, step string,
	run func(ctx sdk.Context, market types.Market) ([]types.EventRecord, error)) {
	for _, market := range pass.markets {
		if pass.skipped[market.ID] {
			continue
		}
		if !k.isolated(pass, step, &market.ID, func(ctx sdk.Context) ([]types.EventRecord, error) {
			return run(ctx, market)
		}) {
			pass.skipped[market.ID] = true
		}
	}
}

// endBlockPrices is step 1: the pricefeed price, market state and mark of
// every enabled market, MarkPriceUpdated when the mark changes.
func (k *Keeper) endBlockPrices(pass *endBlockPass) {
	k.eachMarket(pass, stepPrices, func(ctx sdk.Context, market types.Market) ([]types.EventRecord, error) {
		record, changed, err := k.updateMarketPrice(ctx, market.ID, pass.params.MarkEmaSeconds)
		if err != nil || !changed {
			return nil, err
		}
		return []types.EventRecord{record}, nil
	})
}

// endBlockPremiumSamples is step 2: a premium sample for each market whose
// last sample is Params.PremiumIntervalSeconds of block time old.
func (k *Keeper) endBlockPremiumSamples(pass *endBlockPass) {
	k.eachMarket(pass, stepPremiumSamples, func(ctx sdk.Context, market types.Market) ([]types.EventRecord, error) {
		_, err := k.SamplePremium(ctx, market)
		return nil, err
	})
}

// endBlockFundingIndex is step 3: on a new whole hour of block time each
// market's hourly rate and cumulative funding index, visiting no position.
func (k *Keeper) endBlockFundingIndex(pass *endBlockPass) {
	k.eachMarket(pass, stepFundingIndex, k.SettleFunding)
}

// endBlockTriggers is step 4: at most Params.TriggersPerBlock triggers over
// all markets, markets in ascending id and each in the key order of its
// trigger index, the rest carried to the next block.
func (k *Keeper) endBlockTriggers(pass *endBlockPass) {
	budget := pass.params.TriggersPerBlock
	k.eachMarket(pass, stepTriggers, func(ctx sdk.Context, market types.Market) ([]types.EventRecord, error) {
		if budget == 0 {
			return nil, nil
		}
		state, err := k.GetMarketState(ctx, market.ID)
		if err != nil {
			return nil, err
		}
		evaluated, records, err := k.EvaluateTriggers(ctx, market, state.MarkPrice, budget)
		if err != nil {
			return nil, err
		}
		if evaluated > budget {
			return nil, fmt.Errorf("%w: market %d evaluated %d triggers above the budget %d",
				types.ErrInvalidParams, market.ID, evaluated, budget)
		}
		budget -= evaluated
		return records, nil
	})
}

// endBlockExpiries is step 5: at most Params.ExpiriesPerBlock resting orders
// whose deadline is below block time, in Expiry key order.
func (k *Keeper) endBlockExpiries(pass *endBlockPass) {
	k.isolated(pass, stepExpiries, nil, k.ExpireOrders)
}

// endBlockSweep is step 6: the next Params.SweepPositionsPerBlock positions
// in position-id order from the SweepCursor, wrapping at the highest id, are
// settled at mark through Settle of their account, which realises PnL and
// funding and re-buckets the account. An account holding several swept
// positions settles once; a position of a market skipped for the block waits
// for the next sweep cycle. An account left with a deficit is handed to step
// 8.
func (k *Keeper) endBlockSweep(pass *endBlockPass) {
	positions, next, err := k.sweepWindow(pass.ctx, pass.params.SweepPositionsPerBlock)
	if err != nil {
		k.endBlockFailure(pass, stepSweep, nil, err)
		return
	}
	settled := make(map[common.Address]bool, len(positions))
	for _, position := range positions {
		if pass.skipped[position.MarketID] || settled[position.Account] {
			continue
		}
		settled[position.Account] = true
		account, marketID := position.Account, position.MarketID
		k.isolated(pass, stepSweep, &marketID, func(ctx sdk.Context) ([]types.EventRecord, error) {
			records, err := k.Settle(ctx, account)
			if err != nil {
				return nil, err
			}
			stored, err := k.GetAccount(ctx, account)
			if err != nil {
				return nil, err
			}
			if !stored.DeficitUsd.IsZero() {
				pass.deficits = append(pass.deficits, endBlockDeficit{account: account, marketID: marketID})
			}
			return records, nil
		})
	}
	word := next.Bytes32()
	k.store(pass.ctx).Set(types.SweepCursorKey, word[:])
}

// sweepWindow reads at most limit positions in id order from the stored
// SweepCursor, continuing from the lowest id once the highest is passed, and
// returns them with the cursor of the next block: the id after the last
// position read, 0 after the highest possible id, the stored cursor when no
// position exists.
func (k *Keeper) sweepWindow(ctx sdk.Context, limit uint64) ([]types.Position, uint256.Int, error) {
	store := k.store(ctx)
	var cursor uint256.Int
	if bz := store.Get(types.SweepCursorKey); bz != nil {
		if len(bz) != 32 {
			return nil, uint256.Int{}, fmt.Errorf("%w: sweep cursor holds %d bytes", types.ErrInvalidParams, len(bz))
		}
		cursor.SetBytes32(bz)
	}
	cursorKey := types.PositionKey(cursor)
	ranges := [][2][]byte{
		{cursorKey, sdk.PrefixEndBytes(types.PositionPrefix)},
		{types.PositionPrefix, cursorKey},
	}
	var positions []types.Position
	for _, bounds := range ranges {
		if err := k.sweepRange(ctx, bounds[0], bounds[1], limit, &positions); err != nil {
			return nil, uint256.Int{}, err
		}
	}
	if len(positions) == 0 {
		return nil, cursor, nil
	}
	var next uint256.Int
	last := positions[len(positions)-1].ID
	next.AddOverflow(&last, uint256.NewInt(1))
	return positions, next, nil
}

// sweepRange appends the positions of keys in [start, end) to positions until
// it holds limit.
func (k *Keeper) sweepRange(ctx sdk.Context, start, end []byte, limit uint64, positions *[]types.Position) error {
	iterator := k.store(ctx).Iterator(start, end)
	defer iterator.Close()
	for ; iterator.Valid() && uint64(len(*positions)) < limit; iterator.Next() {
		key := iterator.Key()
		if len(key) != len(types.PositionPrefix)+32 {
			return fmt.Errorf("%w: position key %x", types.ErrInvalidParams, key)
		}
		var position types.Position
		if err := position.Unmarshal(iterator.Value()); err != nil {
			return fmt.Errorf("%w: corrupt position %x: %v", types.ErrInvalidParams, key, err)
		}
		*positions = append(*positions, position)
	}
	return nil
}

// endBlockLiquidation is step 7: LiquidationScan walks the MarginRatioIndex
// from bucket 0 upward within its per-block bounds. Each account whose
// liquidation failed is reported; each liquidated account is handed to step
// 8 for its deficit.
func (k *Keeper) endBlockLiquidation(pass *endBlockPass) {
	k.isolated(pass, stepLiquidation, nil, func(ctx sdk.Context) ([]types.EventRecord, error) {
		result, err := k.LiquidationScan(ctx)
		if err != nil {
			return nil, err
		}
		for _, failure := range result.Failures {
			k.endBlockFailure(pass, stepLiquidation, nil, failure)
		}
		for _, record := range result.Events {
			if deficit, ok := liquidatedAccount(record); ok {
				pass.deficits = append(pass.deficits, deficit)
			}
		}
		return result.Events, nil
	})
}

// liquidatedAccount reads the account and market of a Liquidation record,
// whose arguments are positionId, user, marketId, liquidationPrice, penalty
// and keeper.
func liquidatedAccount(record types.EventRecord) (endBlockDeficit, bool) {
	if record.Name != types.EventLiquidation || len(record.Args) < 3 {
		return endBlockDeficit{}, false
	}
	account, ok := record.Args[1].(common.Address)
	if !ok {
		return endBlockDeficit{}, false
	}
	marketID, ok := record.Args[2].(*big.Int)
	if !ok || !marketID.IsUint64() {
		return endBlockDeficit{}, false
	}
	return endBlockDeficit{account: account, marketID: marketID.Uint64()}, true
}

// endBlockDeficits is step 8: the deficit the insurance fund carries from an
// earlier block, then every deficit the sweep and liquidation recorded, is
// drawn from the insurance fund by CoverDeficit, which emits InsuranceDraw,
// and for what remains VaultDeficit and ADL within its per-block bound.
func (k *Keeper) endBlockDeficits(pass *endBlockPass) {
	candidates, err := k.carriedDeficit(pass.ctx)
	if err != nil {
		k.endBlockFailure(pass, stepDeficits, nil, err)
	}
	candidates = append(candidates, pass.deficits...)
	seen := make(map[common.Address]bool, len(candidates))
	for _, candidate := range candidates {
		if seen[candidate.account] || pass.skipped[candidate.marketID] {
			continue
		}
		seen[candidate.account] = true
		k.isolated(pass, stepDeficits, &candidate.marketID, func(ctx sdk.Context) ([]types.EventRecord, error) {
			account, err := k.GetAccount(ctx, candidate.account)
			if err != nil {
				return nil, err
			}
			if account.DeficitUsd.IsZero() {
				return nil, nil
			}
			return k.CoverDeficit(ctx, candidate.account, candidate.marketID)
		})
	}
}

// carriedDeficit is the deficit of the insurance fund left by an earlier
// block, under the market of the fund's first backstop position; a fund
// without a position has nothing ADL can deleverage.
func (k *Keeper) carriedDeficit(ctx sdk.Context) ([]endBlockDeficit, error) {
	insurance := k.InsuranceAccountAddress()
	fund, err := k.GetAccount(ctx, insurance)
	if errors.Is(err, types.ErrNoAccount) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if fund.DeficitUsd.IsZero() {
		return nil, nil
	}
	positions, err := k.liquidationAccountPositions(ctx, insurance)
	if err != nil || len(positions) == 0 {
		return nil, err
	}
	return []endBlockDeficit{{account: insurance, marketID: positions[0].MarketID}}, nil
}

// endBlockBackstopCloses is step 9: the insurance fund's backstop positions
// close against the book under the slice rule; each position whose close
// failed is reported.
func (k *Keeper) endBlockBackstopCloses(pass *endBlockPass) {
	k.isolated(pass, stepBackstopCloses, nil, func(ctx sdk.Context) ([]types.EventRecord, error) {
		result, err := k.CloseBackstopPositions(ctx)
		if err != nil {
			return nil, err
		}
		for _, failure := range result.Failures {
			k.endBlockFailure(pass, stepBackstopCloses, nil, failure)
		}
		return result.Events, nil
	})
}
