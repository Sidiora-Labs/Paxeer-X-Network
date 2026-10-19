package sequencer_test

import (
	"math/big"
	"sync"
	"testing"
	"time"

	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/stretchr/testify/require"
)

const (
	microbatchConflicting = 200
	microbatchRefused     = 10
	microbatchMaxCount    = 32
	microbatchWindow      = 50 * time.Millisecond
)

// microbatchCountingApp is the real App, recording the size of every batch the
// executor hands to the OCC scheduler.
type microbatchCountingApp struct {
	*app.App
	mu    sync.Mutex
	sizes []int
}

func (c *microbatchCountingApp) DeliverTxBatch(ctx sdk.Context, req sdk.DeliverTxBatchRequest) sdk.DeliverTxBatchResponse {
	c.mu.Lock()
	c.sizes = append(c.sizes, len(req.TxEntries))
	c.mu.Unlock()
	return c.App.DeliverTxBatch(ctx, req)
}

func microbatchOptions(h *harness) sequencer.Options {
	return sequencer.Options{
		Mode:            sequencer.ModeMicroBatch,
		ChainID:         testChainID,
		ProposerAddress: h.proposer,
		LastSeq:         startSeq,
		LastSeqHash:     startSeqHash,
	}
}

func TestMicroBatchEqualsSequential(t *testing.T) {
	senders := make([]utils.TestAcct, microbatchConflicting)
	for i := range senders {
		senders[i] = utils.NewSigner()
	}
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, senders...)
	counting := &microbatchCountingApp{App: h.app}
	mb, err := sequencer.NewMicroBatch(counting, microbatchOptions(h), sequencer.MicroBatchOptions{Window: microbatchWindow, MaxCount: microbatchMaxCount})
	require.NoError(t, err)
	require.Equal(t, sequencer.ModeMicroBatch, mb.Mode())
	ts := h.genesis.Add(time.Second).UnixMilli()
	require.NoError(t, mb.Open(openHeight, ts))

	// Every transfer writes the same recipient balance and fee collector, so
	// each conflicts with all others; the nonce-gap transactions are refused
	// in any order and their writes must not reach the open layer.
	value := big.NewInt(onePaxInWei)
	txs := make([][]byte, 0, microbatchConflicting+microbatchRefused)
	for _, s := range senders {
		_, bz := transfer(t, s, 0, recipient, value)
		txs = append(txs, bz)
	}
	for i := 0; i < microbatchRefused; i++ {
		_, bz := transfer(t, senders[i], 5, recipient, value)
		txs = append(txs, bz)
	}

	type outcome struct {
		acc *sequencer.Accepted
		err error
	}
	outs := make([]outcome, len(txs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range txs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outs[i].acc, outs[i].err = mb.Accept(txs[i])
		}(i)
	}
	close(start)
	wg.Wait()

	// The seq each transaction took is its arrival position among the accepted.
	arrival := make([]int, microbatchConflicting)
	for i := range arrival {
		arrival[i] = -1
	}
	for i, o := range outs {
		if i >= microbatchConflicting {
			require.Nil(t, o.acc)
			requireRefused(t, o.err, sdkerrors.ErrWrongSequence)
			continue
		}
		require.NoError(t, o.err)
		require.Greater(t, o.acc.Seq, startSeq)
		pos := int(o.acc.Seq - startSeq - 1)
		require.Less(t, pos, microbatchConflicting)
		require.Equal(t, -1, arrival[pos])
		arrival[pos] = i
		require.Equal(t, pos+1, o.acc.TxIndex)
		require.Equal(t, openHeight, o.acc.Height)
		require.Equal(t, ts, o.acc.TimestampMs)
	}
	mbBatch, err := mb.Seal(openHeight)
	require.NoError(t, err)

	counting.mu.Lock()
	sizes := append([]int(nil), counting.sizes...)
	counting.mu.Unlock()
	largest := 0
	for _, n := range sizes {
		require.Positive(t, n)
		require.LessOrEqual(t, n, microbatchMaxCount)
		largest = max(largest, n)
	}
	require.Greater(t, largest, 1, "no group ran more than one transaction")

	// Sequential over the same committed state in the same order gives the
	// same receipts, seq chain and state.
	require.Equal(t, sequencer.ModeSequential, h.exec.Mode())
	require.NoError(t, h.exec.Open(openHeight, ts))
	for _, i := range arrival {
		acc, err := h.exec.Accept(txs[i])
		require.NoError(t, err)
		require.Equal(t, outs[i].acc, acc)
	}
	for _, bz := range txs[microbatchConflicting:] {
		acc, err := h.exec.Accept(bz)
		require.Nil(t, acc)
		requireRefused(t, err, sdkerrors.ErrWrongSequence)
	}
	seqBatch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)

	require.Len(t, mbBatch.Txs, microbatchConflicting)
	require.Equal(t, seqBatch.Txs, mbBatch.Txs)
	require.Equal(t, seqBatch.Results, mbBatch.Results)
	require.Equal(t, seqBatch.FromSeq, mbBatch.FromSeq)
	require.Equal(t, startSeq+microbatchConflicting, mbBatch.LastSeq)
	require.Equal(t, seqBatch.LastSeq, mbBatch.LastSeq)
	require.Equal(t, startSeqHash, mbBatch.PrevSeqHash)
	require.Equal(t, seqBatch.LastSeqHash, mbBatch.LastSeqHash)
	require.Equal(t, outs[arrival[microbatchConflicting-1]].acc.SeqHash, mbBatch.LastSeqHash)
	require.Equal(t, seqBatch.BeginBlockEvents, mbBatch.BeginBlockEvents)

	total := new(big.Int).Mul(value, big.NewInt(microbatchConflicting))
	requireWei(t, total, h.weiBalance(mbBatch.Ctx, recipient))
	requireWei(t, total, h.weiBalance(seqBatch.Ctx, recipient))
	for _, s := range senders {
		require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(mbBatch.Ctx, s.EvmAddress))
		require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(seqBatch.Ctx, s.EvmAddress))
		requireWei(t, h.weiBalance(seqBatch.Ctx, s.EvmAddress), h.weiBalance(mbBatch.Ctx, s.EvmAddress))
	}
}

func TestMicroBatchOptionsRefused(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	counting := &microbatchCountingApp{App: h.app}
	valid := sequencer.MicroBatchOptions{Window: time.Millisecond, MaxCount: 1}

	_, err := sequencer.NewMicroBatch(nil, microbatchOptions(h), valid)
	require.EqualError(t, err, "sequencer: nil host")

	wrongMode := microbatchOptions(h)
	wrongMode.Mode = sequencer.ModeSequential
	_, err = sequencer.NewMicroBatch(counting, wrongMode, valid)
	require.ErrorIs(t, err, sequencer.ErrMicroBatchMode)

	for _, w := range []time.Duration{0, -time.Millisecond} {
		_, err = sequencer.NewMicroBatch(counting, microbatchOptions(h), sequencer.MicroBatchOptions{Window: w, MaxCount: 1})
		require.ErrorIs(t, err, sequencer.ErrMicroBatchWindow)
	}
	for _, n := range []int{0, -1} {
		_, err = sequencer.NewMicroBatch(counting, microbatchOptions(h), sequencer.MicroBatchOptions{Window: time.Millisecond, MaxCount: n})
		require.ErrorIs(t, err, sequencer.ErrMicroBatchMaxCount)
	}

	noChain := microbatchOptions(h)
	noChain.ChainID = ""
	_, err = sequencer.NewMicroBatch(counting, noChain, valid)
	require.EqualError(t, err, "sequencer: empty chain id")

	mb, err := sequencer.NewMicroBatch(counting, microbatchOptions(h), valid)
	require.NoError(t, err)
	_, bz := transfer(t, sender, 0, recipient, big.NewInt(1))
	acc, err := mb.Accept(bz)
	require.Nil(t, acc)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)

	// Refusals before execution keep Sequential's answers and take no seq.
	require.NoError(t, mb.Open(openHeight, h.genesis.Add(time.Second).UnixMilli()))
	_, err = mb.Accept(nil)
	requireRefused(t, err, sdkerrors.ErrTxDecode)
	_, err = mb.Accept([]byte{0x0a, 0xff, 0xff, 0x01})
	requireRefused(t, err, sdkerrors.ErrTxDecode)
	acc, err = mb.Accept(bz)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, 1, acc.TxIndex)
	require.Equal(t, expectedSeqHash(startSeqHash, startSeq+1, acc.ActivityID), acc.SeqHash)
}
