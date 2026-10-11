package sequencer

import (
	"errors"
	"fmt"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// ErrSealPanicked reports a panic in the end-of-block sequence; the open layer is discarded.
var ErrSealPanicked = errors.New("sequencer: seal panicked")

// Seal runs MidBlock, WriteDeferredBalances, EndBlock and RouteCollectedFeeTokens on L_open in
// the order ProcessBlock runs them, then makes L_open the sealed layer L_N and advances the
// seqHash chain to its last seq. On any failure L_open is discarded and the chain is unchanged.
func Seal(l *Layers, height int64) (*SealedLayer, error) {
	o := l.open
	if o == nil {
		return nil, ErrNoOpenLayer
	}
	if o.height != height {
		return nil, fmt.Errorf("%w: seal height %d, open height %d", ErrHeightMismatch, height, o.height)
	}
	if l.sealed != nil {
		return nil, fmt.Errorf("%w: sealed height %d", ErrSealedPending, l.sealed.Height)
	}
	l.open = nil

	events, endBlock, err := endOfBlock(o)
	if err != nil {
		return nil, err
	}
	sealed := &SealedLayer{
		Height:   o.height,
		Txs:      o.txs,
		Results:  o.results,
		Events:   events,
		EndBlock: endBlock,
		Batch:    o.batchFields(),
		ctx:      o.ctx,
		ms:       o.ms,
	}
	l.sealed = sealed
	l.lastSeq = sealed.Batch.ToSeq
	l.lastSeqHash = sealed.Batch.LastSeqHash
	return sealed, nil
}

func endOfBlock(o *OpenLayer) (events []abci.Event, endBlock abci.ResponseEndBlock, err error) {
	defer func() {
		if r := recover(); r != nil {
			events = nil
			endBlock = abci.ResponseEndBlock{}
			err = fmt.Errorf("%w: %v", ErrSealPanicked, r)
		}
	}()
	a := o.app
	ctx := o.ctx

	events = append(events, o.beginEvents...)
	events = append(events, a.MidBlock(ctx, o.height)...)

	if a.GigaExecutorEnabled {
		ctx.GigaMultiStore().WriteGiga()
	}

	a.EvmKeeper.SetTxResults(o.results)
	a.EvmKeeper.SetMsgs(o.evmMsgs)

	events = append(events, a.BankKeeper.WriteDeferredBalances(ctx)...)

	var evmTotalGasUsed int64
	for _, result := range o.results {
		if result.EvmTxInfo != nil {
			evmTotalGasUsed += result.GasUsed
		}
	}

	endBlock = a.EndBlock(ctx, o.height, evmTotalGasUsed)

	feeCtx := ctx.WithEventManager(sdk.NewEventManager())
	if err := a.EvmKeeper.RouteCollectedFeeTokens(feeCtx); err != nil {
		return nil, abci.ResponseEndBlock{}, fmt.Errorf("route collected fee tokens: %w", err)
	}
	endBlock.Events = append(endBlock.Events, sdk.MarkEventsToIndex(feeCtx.EventManager().ABCIEvents(), a.IndexEvents)...)

	events = append(events, endBlock.Events...)
	return events, endBlock, nil
}
