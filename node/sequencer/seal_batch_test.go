package sequencer_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"math"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	seqkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/stretchr/testify/require"
)

// newSealBatchHarness builds the real App as node/app_test.go builds it on the synchronous block
// path, funds the given accounts (EVM-mapped), pins the test receipt key, commits height 1,
// activates the sequenced block at openHeight and builds a Sequential executor that continues
// the empty stored sequence.
func newSealBatchHarness(t *testing.T, funded ...utils.TestAcct) *harness {
	t.Helper()
	app.EnableOCC = false
	t.Cleanup(func() { app.EnableOCC = true })
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := app.NewTestWrapper(t, tm, valPub, false, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(false)
		ba.SetConcurrencyWorkers(1)
	})
	a := tw.App
	for _, acct := range funded {
		a.EvmKeeper.SetAddressMapping(tw.Ctx, acct.AccountAddress, acct.EvmAddress)
		tw.FundAcc(acct.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(fundingUhpx))))
	}
	public := publicOf(testKeySeed)
	require.NoError(t, a.SequencerKeeper.SetReceiptKeys(tw.Ctx, seqkeeper.ReceiptKeys{{
		KeyId:       testKeyID[:],
		PublicKey:   public[:],
		FirstHeight: 1,
		LastHeight:  math.MaxUint64,
	}}))
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: testChainID, Height: committedTip},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	require.NoError(t, a.SetActivationHeight(openHeight))
	require.True(t, a.Sequenced(openHeight))

	proposer := valPub.Address().Bytes()
	exec, err := sequencer.NewExecutor(a, sequencer.Options{ChainID: testChainID, ProposerAddress: proposer})
	require.NoError(t, err)
	return &harness{t: t, app: a, exec: exec, proposer: proposer, genesis: tm}
}

// signSealBatch builds the MsgSequenceBatch of f under keyID signed by the ed25519 key of seed.
func signSealBatch(t *testing.T, f sequencer.SequenceBatchFields, seed []byte, keyID [32]byte) seqtypes.MsgSequenceBatch {
	t.Helper()
	msg := seqtypes.MsgSequenceBatch{
		Height:      f.Height,
		FromSeq:     f.FromSeq,
		ToSeq:       f.ToSeq,
		PrevSeqHash: append([]byte(nil), f.PrevSeqHash[:]...),
		LastSeqHash: append([]byte(nil), f.LastSeqHash[:]...),
		TxRoot:      append([]byte(nil), f.TxRoot[:]...),
		KeyId:       append([]byte(nil), keyID[:]...),
	}
	digest, err := msg.SignBytes(testChainID)
	require.NoError(t, err)
	msg.Sig = ed25519.Sign(ed25519.NewKeyFromSeed(seed), digest[:])
	return msg
}

// processSequencedBlock runs ProcessBlock over block at openHeight on a branch of the committed
// state, under the header the executor opens at ms.
func (h *harness) processSequencedBlock(t *testing.T, ms int64, block [][]byte) processedBlock {
	t.Helper()
	header := tmproto.Header{
		ChainID:         testChainID,
		Height:          openHeight,
		Time:            time.UnixMilli(ms).UTC(),
		ProposerAddress: h.proposer,
	}
	ctx := sdk.NewContext(h.app.CommitMultiStore().CacheMultiStore(), header, false)
	ctx = ctx.WithConsensusParams(h.app.GetConsensusParams(ctx))
	events, results, end, err := h.app.ProcessBlock(ctx, block, &app.BlockProcessRequest{
		Height: openHeight,
		Time:   header.Time,
	}, abci.CommitInfo{}, false, nil)
	require.NoError(t, err)
	require.Len(t, results, len(block))
	require.Equal(t, abci.ExecTxResult{}, *results[0])
	return processedBlock{ctx: ctx, events: events, results: results, end: end}
}

// requireStoredSequence checks the sequencer store of ctx holds lastSeq and lastSeqHash.
func requireStoredSequence(t *testing.T, a *app.App, ctx sdk.Context, lastSeq uint64, lastSeqHash [32]byte) {
	t.Helper()
	gotSeq, err := a.SequencerKeeper.LastSeq(ctx)
	require.NoError(t, err)
	require.Equal(t, lastSeq, gotSeq)
	gotHash, err := a.SequencerKeeper.LastSeqHash(ctx)
	require.NoError(t, err)
	require.Equal(t, lastSeqHash, gotHash)
}

func TestSealBatchMatchesProcessBlock(t *testing.T) {
	sender, payer := utils.NewSigner(), utils.NewSigner()
	recipient := utils.NewSigner()
	h := newSealBatchHarness(t, sender, payer)

	txs := make([][]byte, 0, 4)
	ids := make([][32]byte, 0, 4)
	for nonce := range uint64(3) {
		signed, bz := transfer(t, sender, nonce, recipient.EvmAddress, big.NewInt(onePaxInWei))
		txs = append(txs, bz)
		ids = append(ids, [32]byte(signed.Hash()))
	}
	send := signCosmos(t, h.committedCtx(), h, payer, 200_000,
		banktypes.NewMsgSend(payer.AccountAddress, recipient.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(7)))))
	txs = append(txs, send)
	ids = append(ids, sha256.Sum256(send))

	var head [32]byte
	for i, id := range ids {
		head = expectedSeqHash(head, uint64(i)+1, id)
	}
	want := sequencer.SequenceBatchFields{
		Height:      uint64(openHeight),
		FromSeq:     1,
		ToSeq:       4,
		LastSeqHash: head,
		TxRoot:      resolutionTxRoot(ids),
	}
	msg := signSealBatch(t, want, testKeySeed, testKeyID)
	tx0, err := app.EncodeSequenceBatch(msg)
	require.NoError(t, err)
	block := append([][]byte{tx0}, txs...)

	ms := h.genesis.Add(time.Second).UnixMilli()
	reference := h.processSequencedBlock(t, ms, block)
	requireStoredSequence(t, h.app, reference.ctx, 4, head)

	h.openAt(time.UnixMilli(ms))
	for i, tx := range txs {
		acc, err := h.exec.Accept(tx)
		require.NoError(t, err)
		require.Equal(t, uint64(i)+1, acc.Seq)
		require.Equal(t, i+1, acc.TxIndex)
		require.Equal(t, ids[i], acc.ActivityID)
		require.Equal(t, uint32(0), acc.Result.Code, acc.Result.Log)
	}
	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, want, sequencer.BatchFields(batch))
	requireStoredSequence(t, h.app, batch.Ctx, 0, [32]byte{})

	sealed, gotTx0, err := sequencer.SealBatch(h.app, batch, msg)
	require.NoError(t, err)
	require.Equal(t, tx0, gotTx0)
	decoded, err := app.DecodeSequenceBatch(gotTx0)
	require.NoError(t, err)
	require.Equal(t, msg, decoded)
	require.Equal(t, openHeight, sealed.Height)
	require.Equal(t, block, sealed.Txs)
	require.Equal(t, want, sealed.Batch)
	requireSameBlock(t, h.app, reference, sealed)
	requireStoredSequence(t, h.app, sealed.Context(), 4, head)
	requireStoredSequence(t, h.app, batch.Ctx, 4, head)
}

func TestSealBatchRefusesMismatchedFields(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newSealBatchHarness(t, sender)
	h.open()
	ids := make([][32]byte, 0, 2)
	for nonce := range uint64(2) {
		signed, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		_, err := h.exec.Accept(bz)
		require.NoError(t, err)
		ids = append(ids, [32]byte(signed.Hash()))
	}
	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	fields := sequencer.BatchFields(batch)
	require.Equal(t, uint64(openHeight), fields.Height)
	require.Equal(t, uint64(1), fields.FromSeq)
	require.Equal(t, uint64(2), fields.ToSeq)
	require.Equal(t, [32]byte{}, fields.PrevSeqHash)
	require.Equal(t, batch.LastSeqHash, fields.LastSeqHash)
	require.Equal(t, resolutionTxRoot(ids), fields.TxRoot)

	flip := func(h [32]byte) [32]byte { h[0] ^= 0xff; return h }
	cases := []struct {
		name   string
		mutate func(f *sequencer.SequenceBatchFields)
		want   string
	}{
		{"height", func(f *sequencer.SequenceBatchFields) { f.Height = 3 }, "height 3, sealed 2"},
		{"fromSeq", func(f *sequencer.SequenceBatchFields) { f.FromSeq, f.ToSeq = 2, 3 }, "fromSeq 2, sealed 1"},
		{"toSeq", func(f *sequencer.SequenceBatchFields) { f.ToSeq = 1 }, "toSeq 1, sealed 2"},
		{"prevSeqHash", func(f *sequencer.SequenceBatchFields) { f.PrevSeqHash = flip(f.PrevSeqHash) }, "prevSeqHash FF00"},
		{"lastSeqHash", func(f *sequencer.SequenceBatchFields) { f.LastSeqHash = flip(f.LastSeqHash) }, "lastSeqHash"},
		{"txRoot", func(f *sequencer.SequenceBatchFields) { f.TxRoot = flip(f.TxRoot) }, "txRoot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := fields
			tc.mutate(&mutated)
			sealed, tx0, err := sequencer.SealBatch(h.app, batch, signSealBatch(t, mutated, testKeySeed, testKeyID))
			require.ErrorIs(t, err, sequencer.ErrBatchFieldsMismatch)
			require.ErrorContains(t, err, "sequencer: sequence batch fields differ from the sealed batch: "+tc.want)
			require.Nil(t, sealed)
			require.Nil(t, tx0)
			requireStoredSequence(t, h.app, batch.Ctx, 0, [32]byte{})
		})
	}

	// Matching fields under a key that is not pinned, or a signature by another key, are refused
	// by the keeper and leave the batch store unwritten.
	otherID := [32]byte(label("sealed batch unpinned key id"))
	_, _, err = sequencer.SealBatch(h.app, batch, signSealBatch(t, fields, testKeySeed, otherID))
	require.ErrorIs(t, err, seqkeeper.ErrUnknownReceiptKey)
	requireStoredSequence(t, h.app, batch.Ctx, 0, [32]byte{})
	_, _, err = sequencer.SealBatch(h.app, batch, signSealBatch(t, fields, label("sealed batch other key"), testKeyID))
	require.ErrorIs(t, err, seqtypes.ErrBadSignature)
	requireStoredSequence(t, h.app, batch.Ctx, 0, [32]byte{})

	// A batch whose seq range disagrees with its transactions is refused before tx 0 is applied.
	short := *batch
	short.Txs, short.Results, short.EVMMsgs = batch.Txs[:1], batch.Results[:1], batch.EVMMsgs[:1]
	_, _, err = sequencer.SealBatch(h.app, &short, signSealBatch(t, fields, testKeySeed, testKeyID))
	require.ErrorIs(t, err, sequencer.ErrMalformedBatch)
	require.EqualError(t, err, "sequencer: sealed batch is malformed: 1 txs from seq 1 end at 1, last seq 2")
	requireStoredSequence(t, h.app, batch.Ctx, 0, [32]byte{})

	// The matching batch still seals after every refusal.
	sealed, _, err := sequencer.SealBatch(h.app, batch, signSealBatch(t, fields, testKeySeed, testKeyID))
	require.NoError(t, err)
	require.Equal(t, fields, sealed.Batch)
	requireStoredSequence(t, h.app, sealed.Context(), 2, fields.LastSeqHash)
}

func TestSealBatchEmptyBlock(t *testing.T) {
	h := newSealBatchHarness(t)
	ms := h.genesis.Add(time.Second).UnixMilli()
	want := sequencer.SequenceBatchFields{
		Height:  uint64(openHeight),
		FromSeq: 1,
		ToSeq:   0,
		TxRoot:  seqtypes.EmptyTxRoot,
	}
	msg := signSealBatch(t, want, testKeySeed, testKeyID)
	require.True(t, msg.IsEmpty())
	tx0, err := app.EncodeSequenceBatch(msg)
	require.NoError(t, err)
	reference := h.processSequencedBlock(t, ms, [][]byte{tx0})

	h.openAt(time.UnixMilli(ms))
	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Empty(t, batch.Txs)
	require.Equal(t, uint64(0), batch.LastSeq)
	require.Equal(t, want, sequencer.BatchFields(batch))

	sealed, gotTx0, err := sequencer.SealBatch(h.app, batch, msg)
	require.NoError(t, err)
	require.Equal(t, tx0, gotTx0)
	require.Equal(t, [][]byte{tx0}, sealed.Txs)
	require.Equal(t, want, sealed.Batch)
	requireSameBlock(t, h.app, reference, sealed)
	requireStoredSequence(t, h.app, sealed.Context(), 0, [32]byte{})

	// tx 0 is applied once per block: a second seal of the same block is refused by the keeper.
	_, _, err = sequencer.SealBatch(h.app, batch, msg)
	require.ErrorIs(t, err, seqkeeper.ErrBatchRepeated)
}
