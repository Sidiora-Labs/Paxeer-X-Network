package state_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/eventbus"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/mempool"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/proxy"
	sm "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state/indexer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/state/indexer/sink/kv"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/store"
	tmtime "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/time"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

// sealedSource is a SequencerSource over pending blocks sealed ahead of the
// proposal, active from the activation height.
type sealedSource struct {
	activation int64
	sealed     map[int64]sm.SealedBlock
	sealCalls  []int64
}

func (s *sealedSource) Sequenced(height int64) bool { return height >= s.activation }

func (s *sealedSource) Seal(_ context.Context, height int64) (sm.SealedBlock, error) {
	s.sealCalls = append(s.sealCalls, height)
	b, ok := s.sealed[height]
	if !ok {
		return sm.SealedBlock{}, fmt.Errorf("no pending block at height %d", height)
	}
	return b, nil
}

// priorityApp admits every transaction at a high priority and counts rechecks.
type priorityApp struct {
	testApp
	rechecks atomic.Int32
}

func (app *priorityApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	if req.Type == abci.CheckTxTypeV2Recheck {
		app.rechecks.Add(1)
	}
	return &abci.ResponseCheckTxV2{ResponseCheckTx: &abci.ResponseCheckTx{Priority: 1_000_000, GasWanted: 1}}
}

type sequencerFixture struct {
	state     sm.State
	blockExec *sm.BlockExecutor
	mempool   *mempool.TxMempool
	eventBus  *eventbus.EventBus
	app       *priorityApp
	source    *sealedSource
}

func newSequencerFixture(t *testing.T, height int, activation int64) *sequencerFixture {
	t.Helper()
	ctx := t.Context()

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, _ := makeState(t, 1, height)
	app := &priorityApp{}
	proxyApp := proxy.New(app, proxy.NopMetrics())
	mp := makeTxMempool(t, proxyApp)
	blockExec := sm.NewBlockExecutor(
		sm.NewStore(stateDB),
		proxyApp,
		mp,
		sm.EmptyEvidencePool{},
		store.NewBlockStore(dbm.NewMemDB()),
		eventBus,
		sm.NopMetrics(),
		types.DefaultConsensusPolicy(),
	)
	source := &sealedSource{activation: activation, sealed: map[int64]sm.SealedBlock{}}
	blockExec.SetSequencerSource(source)
	return &sequencerFixture{state: state, blockExec: blockExec, mempool: mp, eventBus: eventBus, app: app, source: source}
}

func (f *sequencerFixture) proposer() []byte {
	return f.state.Validators.GetProposer().Address
}

func (f *sequencerFixture) propose(t *testing.T, height int64) (*types.Block, error) {
	t.Helper()
	return f.blockExec.CreateProposalBlock(t.Context(), height, f.state, &types.Commit{}, f.proposer())
}

func (f *sequencerFixture) addToMempool(t *testing.T, txs ...types.Tx) {
	t.Helper()
	for _, tx := range txs {
		res, err := f.mempool.CheckTx(t.Context(), tx)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code)
	}
}

// sequenceBatch encodes the batch fields carried at tx 0 of a sequenced block.
func sequenceBatch(height, fromSeq, toSeq uint64) types.Tx {
	b := []byte("PAXEER/v1/sequence-batch\x00")
	b = binary.BigEndian.AppendUint64(b, height)
	b = binary.BigEndian.AppendUint64(b, fromSeq)
	return binary.BigEndian.AppendUint64(b, toSeq)
}

// receipted returns the executed transactions for seq fromSeq..toSeq.
func receipted(fromSeq, toSeq uint64) types.Txs {
	txs := make(types.Txs, 0, toSeq-fromSeq+1)
	for seq := fromSeq; seq <= toSeq; seq++ {
		txs = append(txs, types.Tx(fmt.Appendf(nil, "receipted-tx-seq-%d", seq)))
	}
	return txs
}

func applyProposal(t *testing.T, f *sequencerFixture, block *types.Block) sm.State {
	t.Helper()
	parts, err := block.MakePartSet(testPartSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: parts.Header()}
	state, err := f.blockExec.ApplyBlock(t.Context(), f.state, blockID, block, nil)
	require.NoError(t, err)
	return state
}

func TestSequencerProposalUsesSealedSlice(t *testing.T) {
	const height = 1001
	f := newSequencerFixture(t, height, height)
	mempoolTx := types.Tx("mempool-only-tx")
	f.addToMempool(t, mempoolTx)

	batch := sequenceBatch(height, 31, 42)
	slice := receipted(31, 42)
	require.Len(t, slice, 12)
	f.source.sealed[height] = sm.SealedBlock{Batch: batch, Txs: slice, OpenTime: time.UnixMilli(1700000000123)}

	block, err := f.propose(t, height)
	require.NoError(t, err)

	require.Equal(t, []int64{height}, f.source.sealCalls)
	require.Equal(t, int64(height), block.Height)
	require.Len(t, block.Txs, 13)
	require.Equal(t, batch, block.Txs[0])
	for i, tx := range slice {
		require.Equal(t, tx, block.Txs[i+1], "tx %d", i+1)
	}
	require.Equal(t, types.Txs(slice).Hash(), types.Txs(block.Txs[1:]).Hash())
	// ReapTxs was not called: the mempool transaction is neither proposed nor reaped.
	require.Equal(t, 1, f.mempool.Size())
	require.NotContains(t, block.Txs, mempoolTx)
}

func TestSequencerProposalHeaderTimeIsOpenTime(t *testing.T) {
	const height = 1001
	f := newSequencerFixture(t, height, height)
	open := time.UnixMilli(1700000000123)
	f.source.sealed[height] = sm.SealedBlock{Batch: sequenceBatch(height, 1, 0), OpenTime: open}

	block, err := f.propose(t, height)
	require.NoError(t, err)

	require.Equal(t, int64(1700000000123), block.Time.UnixMilli())
	require.True(t, block.Time.Equal(open))
	require.Equal(t, time.UTC, block.Time.Location())
	// The empty sealed block still carries its batch at tx 0.
	require.Equal(t, types.Txs{sequenceBatch(height, 1, 0)}, block.Txs)
}

func TestSequencerProposalIgnoresMempoolOrder(t *testing.T) {
	const height = 1001
	f := newSequencerFixture(t, height, height)
	highPriority := types.Tx("high-priority-mempool-tx")
	f.addToMempool(t, highPriority)
	reaped, _ := f.mempool.ReapTxs(mempool.ReapLimits{MaxBytes: utils.Some(int64(1 << 20))}, false)
	require.Equal(t, types.Txs{highPriority}, reaped)

	batch := sequenceBatch(height, 7, 8)
	slice := receipted(7, 8)
	f.source.sealed[height] = sm.SealedBlock{Batch: batch, Txs: slice, OpenTime: time.UnixMilli(1700000000000)}

	block, err := f.propose(t, height)
	require.NoError(t, err)

	require.Equal(t, types.Txs{batch, slice[0], slice[1]}, block.Txs)
}

func TestSequencerProposalLeavesMempoolUpdate(t *testing.T) {
	f := newSequencerFixture(t, 1, 1)
	f.state.ConsensusParams.ABCI.RecheckTx = true
	forwarded := receipted(1, 1)[0]
	gossiped := types.Tx("gossiped-tx-not-yet-receipted")
	f.addToMempool(t, forwarded, gossiped)
	require.Equal(t, 2, f.mempool.Size())

	f.source.sealed[1] = sm.SealedBlock{
		Batch:    sequenceBatch(1, 1, 1),
		Txs:      types.Txs{forwarded},
		OpenTime: f.state.LastBlockTime.Add(time.Second),
	}
	block, err := f.propose(t, 1)
	require.NoError(t, err)

	state := applyProposal(t, f, block)

	require.Equal(t, int64(1), state.LastBlockHeight)
	// Update removed the committed transaction and rechecked the one left.
	require.Equal(t, 1, f.mempool.Size())
	require.Equal(t, int32(1), f.app.rechecks.Load())
	left, _ := f.mempool.ReapTxs(mempool.ReapLimits{MaxBytes: utils.Some(int64(1 << 20))}, false)
	require.Equal(t, types.Txs{gossiped}, left)
}

func TestSequencerProposalCommittedTxIndexed(t *testing.T) {
	f := newSequencerFixture(t, 1, 1)
	sink := kv.NewEventSink(dbm.NewMemDB())
	service := indexer.NewService(indexer.ServiceArgs{Sinks: []indexer.EventSink{sink}, EventBus: f.eventBus})
	require.NoError(t, service.Start(t.Context()))
	t.Cleanup(service.Wait)

	batch := sequenceBatch(1, 1, 2)
	slice := receipted(1, 2)
	f.source.sealed[1] = sm.SealedBlock{Batch: batch, Txs: slice, OpenTime: f.state.LastBlockTime.Add(time.Second)}
	block, err := f.propose(t, 1)
	require.NoError(t, err)

	// Receipted and proposed but not committed: not found.
	for _, tx := range slice {
		res, err := sink.GetTxByHash(tx.Hash().Bytes())
		require.NoError(t, err)
		require.Nil(t, res)
	}

	applyProposal(t, f, block)

	for i, tx := range slice {
		var res *abci.TxResultV2
		require.Eventually(t, func() bool {
			res, err = sink.GetTxByHash(tx.Hash().Bytes())
			return err == nil && res != nil
		}, 5*time.Second, 10*time.Millisecond)
		require.Equal(t, int64(1), res.Height)
		require.Equal(t, uint32(i+1), res.Index)
		require.Equal(t, tx, types.Tx(res.Tx))
	}
	res, err := sink.GetTxByHash(batch.Hash().Bytes())
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, uint32(0), res.Index)
}

func TestSequencerProposalFromSeal(t *testing.T) {
	f := newSequencerFixture(t, 1, 1)
	batch := sequenceBatch(1, 1, 12)
	slice := receipted(1, 12)
	f.source.sealed[1] = sm.SealedBlock{Batch: batch, Txs: slice, OpenTime: f.state.LastBlockTime.Add(time.Second)}

	block, err := f.propose(t, 1)
	require.NoError(t, err)

	require.Equal(t, append(types.Txs{batch}, slice...), types.Txs(block.Txs))
	require.Equal(t, f.proposer(), []byte(block.ProposerAddress))
	require.Equal(t, f.state.ChainID, block.ChainID)
	require.Equal(t, f.state.AppHash, []byte(block.AppHash))
	require.NoError(t, f.blockExec.ValidateBlock(t.Context(), f.state, block))
}

func TestSequencerProposalHeaderTime(t *testing.T) {
	const height = 1001
	f := newSequencerFixture(t, height, height)
	sp := f.state.ConsensusParams.Synchrony.SynchronyParamsOrDefaults()
	require.Equal(t, 505*time.Millisecond, sp.Precision)
	require.Equal(t, 12*time.Second, sp.MessageDelay)

	timely := func(open, recv time.Time) bool {
		f.source.sealed[height] = sm.SealedBlock{Batch: sequenceBatch(height, 1, 0), OpenTime: open}
		block, err := f.propose(t, height)
		require.NoError(t, err)
		require.True(t, block.Time.Equal(open))
		parts, err := block.MakePartSet(testPartSize)
		require.NoError(t, err)
		blockID := types.BlockID{Hash: block.Hash(), PartSetHeader: parts.Header()}
		proposal := types.NewProposal(height, 0, -1, blockID, block.Time, block.GetTxHashes(), block.Header, block.LastCommit, block.Evidence, f.proposer())
		return proposal.IsTimely(recv, sp, 0)
	}

	now := tmtime.Now()
	require.True(t, timely(now.Add(-2*time.Second), now))
	require.False(t, timely(now.Add(20*time.Second), now))
}

func TestSequencerProposalBelowActivationReaps(t *testing.T) {
	const activation = 1000
	f := newSequencerFixture(t, activation-1, activation)
	mempoolTx := types.Tx("mempool-tx-before-activation")
	f.addToMempool(t, mempoolTx)

	block, err := f.propose(t, activation-1)
	require.NoError(t, err)
	require.Equal(t, types.Txs{mempoolTx}, block.Txs)
	require.Empty(t, f.source.sealCalls)

	batch := sequenceBatch(activation, 1, 1)
	slice := receipted(1, 1)
	f.source.sealed[activation] = sm.SealedBlock{Batch: batch, Txs: slice, OpenTime: time.UnixMilli(1700000000000)}
	block, err = f.propose(t, activation)
	require.NoError(t, err)
	require.Equal(t, types.Txs{batch, slice[0]}, block.Txs)
	require.Equal(t, []int64{activation}, f.source.sealCalls)
}

func TestSequencerProposalRefusesIncompleteSeal(t *testing.T) {
	const height = 1001
	open := time.UnixMilli(1700000000000)
	maxDataBytes := types.MaxDataBytes(types.DefaultBlockParams().MaxBytes, 0, 1)

	for _, tc := range []struct {
		name   string
		sealed *sm.SealedBlock
		want   error
	}{
		{name: "no batch", sealed: &sm.SealedBlock{Txs: receipted(1, 1), OpenTime: open}, want: sm.ErrSequenceBatchMissing},
		{name: "no open time", sealed: &sm.SealedBlock{Batch: sequenceBatch(height, 1, 1), Txs: receipted(1, 1)}, want: sm.ErrOpenTimeMissing},
		{
			name: "over the data bytes",
			sealed: &sm.SealedBlock{
				Batch:    sequenceBatch(height, 1, 1),
				Txs:      types.Txs{make(types.Tx, maxDataBytes)},
				OpenTime: open,
			},
			want: sm.ErrSealedBlockTooLarge,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSequencerFixture(t, height, height)
			f.source.sealed[height] = *tc.sealed
			block, err := f.propose(t, height)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, block)
		})
	}

	t.Run("seal fails", func(t *testing.T) {
		f := newSequencerFixture(t, height, height)
		block, err := f.propose(t, height)
		require.EqualError(t, err, "seal height 1001: no pending block at height 1001")
		require.Nil(t, block)
		require.Equal(t, []int64{height}, f.source.sealCalls)
	})

}
