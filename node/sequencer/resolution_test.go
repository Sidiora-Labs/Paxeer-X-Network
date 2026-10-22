package sequencer_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer/wal"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// resolutionMerged runs resolutionBlockGas and records the merge plan as done at the genesis
// height, so the block at openHeight is sequenced.
func resolutionMerged(ctx sdk.Context, a *app.App) {
	resolutionBlockGas(ctx, a)
	a.UpgradeKeeper.SetDone(ctx, app.MergeUpgrade)
}

// resolutionBatch signs the MsgSequenceBatch of a sealed batch with the test receipt key and
// encodes it as tx 0.
func resolutionBatch(t *testing.T, batch *sequencer.Batch, txRoot [32]byte) (seqtypes.MsgSequenceBatch, []byte) {
	t.Helper()
	msg := seqtypes.MsgSequenceBatch{
		Height:      uint64(batch.Height),
		FromSeq:     batch.FromSeq,
		ToSeq:       batch.LastSeq,
		PrevSeqHash: bytes.Clone(batch.PrevSeqHash[:]),
		LastSeqHash: bytes.Clone(batch.LastSeqHash[:]),
		TxRoot:      bytes.Clone(txRoot[:]),
		KeyId:       bytes.Clone(testKeyID[:]),
	}
	digest, err := msg.SignBytes(testChainID)
	require.NoError(t, err)
	msg.Sig = ed25519.Sign(ed25519.NewKeyFromSeed(testKeySeed), digest[:])
	require.NoError(t, msg.VerifySignature(testChainID, publicOf(testKeySeed)))
	bz, err := app.EncodeSequenceBatch(msg)
	require.NoError(t, err)
	return msg, bz
}

// resolutionTxRoot is sha256 over the activity ids of the sealed transactions in order.
func resolutionTxRoot(ids [][32]byte) [32]byte {
	var cat []byte
	for _, id := range ids {
		cat = append(cat, id[:]...)
	}
	return sha256.Sum256(cat)
}

// resolutionProposal runs ProcessProposal on a for txs at height and waits for the optimistic
// processing an accepted proposal starts.
func resolutionProposal(t *testing.T, a *app.App, height int64, at time.Time, txs [][]byte) abci.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	a.ClearOptimisticProcessingInfo()
	hash := sha256.Sum256(bytes.Join(append([][]byte{[]byte(strconv.FormatInt(height, 10))}, txs...), []byte{0}))
	resp, err := a.ProcessProposal(context.Background(), &abci.RequestProcessProposal{
		Txs:    txs,
		Hash:   hash[:],
		Header: &tmproto.Header{ChainID: testChainID, Height: height, Time: at},
	})
	require.NoError(t, err)
	if info := a.GetOptimisticProcessingInfo(); info.Completion != nil {
		select {
		case <-info.Completion:
		case <-time.After(time.Minute):
			t.Fatal("optimistic processing did not complete")
		}
	}
	a.ClearOptimisticProcessingInfo()
	return resp.Status
}

// resolutionBlockGas commits block gas bounds with height 1, so ProcessProposal at height 2
// admits a transfer block under its gas check.
func resolutionBlockGas(ctx sdk.Context, a *app.App) {
	cp := *app.DefaultConsensusParams
	cp.Block = &tmproto.BlockParams{MaxBytes: cp.Block.MaxBytes, MaxGas: cp.Block.MaxGas, MaxGasWanted: cp.Block.MaxGas}
	a.StoreConsensusParams(ctx, &cp)
}

// resolutionOpenNonces reads the next nonce through the ante read context.
type resolutionOpenNonces struct{ a *app.App }

func (r resolutionOpenNonces) EvmNonce(addr common.Address) uint64 {
	return r.a.EvmKeeper.GetNonce(r.a.LatestCtx(), addr)
}

func TestResolutionReceiptModuleId(t *testing.T) {
	header, err := os.ReadFile("../../include/layerx/lxp_module.h")
	require.NoError(t, err)
	ids := map[uint64]string{}
	var reserved uint64
	for _, m := range regexp.MustCompile(`LXP_MODULE_([A-Z_]+) = (\d+),`).FindAllSubmatch(header, -1) {
		v, err := strconv.ParseUint(string(m[2]), 10, 16)
		require.NoError(t, err)
		switch name := string(m[1]); name {
		case "RESERVED_COUNT":
			reserved = v
		case "MAX_NAME", "MAX_ACTIVITY_TYPES":
		default:
			ids[v] = name
		}
	}
	require.Equal(t, uint64(11), reserved)
	require.Len(t, ids, 11)
	for id := range ids {
		require.LessOrEqual(t, id, reserved)
	}
	require.Equal(t, uint16(reserved+1), app.EVMReceiptModuleID)
	require.Equal(t, sequencer.KernelReservedModules+1, app.EVMReceiptModuleID)
	_, reused := ids[uint64(app.EVMReceiptModuleID)]
	require.False(t, reused)

	sender := utils.NewSigner()
	rh := newReceiptHarness(t, 0, nil, sender)
	r, err := sequencer.NewReceipter(rh.exec, rh.app, &rh.app.EvmKeeper, rh.key, app.EVMReceiptModuleID)
	require.NoError(t, err)
	_, bz := transfer(t, sender, 0, utils.NewSigner().EvmAddress, big.NewInt(onePaxInWei))
	acc, signed, err := r.Accept(bz)
	require.NoError(t, err)
	require.Equal(t, uint64(1), acc.Seq)
	verified, err := verify.ReceiptSignature(signed, rh.key.PublicKey())
	require.NoError(t, err)
	require.Equal(t, uint16(12), verified.Receipt.ModuleID)
	require.Len(t, verified.Receipt.Effects, 1)
	for _, e := range verified.Receipt.Effects {
		require.Equal(t, uint16(12), e.ModuleID)
	}
}

func TestResolutionBatchInjection(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	rh := newReceiptHarness(t, 0, resolutionMerged, sender)
	ids := make([][32]byte, 0, 40)
	for nonce := range uint64(40) {
		signed, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		acc, rec, _ := rh.accept(t, bz)
		require.Equal(t, nonce+1, acc.Seq)
		require.Equal(t, nonce+1, rec.GlobalSequence)
		ids = append(ids, [32]byte(signed.Hash()))
	}
	batch, err := rh.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, uint64(1), batch.FromSeq)
	require.Equal(t, uint64(40), batch.LastSeq)
	require.Len(t, batch.Txs, 40)

	root := resolutionTxRoot(ids)
	msg, batchTx := resolutionBatch(t, batch, root)
	block := append([][]byte{batchTx}, batch.Txs...)

	// Tx 0 of the block is the batch over seq 1..40.
	decoded, err := app.DecodeSequenceBatch(block[0])
	require.NoError(t, err)
	require.Equal(t, msg, decoded)
	require.Equal(t, uint64(openHeight), decoded.Height)
	require.Equal(t, uint64(1), decoded.FromSeq)
	require.Equal(t, uint64(40), decoded.ToSeq)
	require.Equal(t, root[:], decoded.TxRoot)
	require.Equal(t, batch.LastSeqHash[:], decoded.LastSeqHash)

	at := time.UnixMilli(batch.TimestampMs).UTC()
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resolutionProposal(t, rh.app, openHeight, at, block))

	// A batch whose txRoot differs from the sealed sequence is refused, even when re-signed.
	other := root
	other[0] ^= 0xff
	_, tampered := resolutionBatch(t, batch, other)
	require.Equal(t, abci.ResponseProcessProposal_REJECT,
		resolutionProposal(t, rh.app, openHeight, at, append([][]byte{tampered}, batch.Txs...)))

	// So is the sealed slice out of receipt order under the original batch.
	swapped := append([][]byte{batchTx}, batch.Txs...)
	swapped[1], swapped[2] = swapped[2], swapped[1]
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resolutionProposal(t, rh.app, openHeight, at, swapped))
}

func TestResolutionNonceQueue(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	rh := newReceiptHarness(t, 0, nil, sender)
	for nonce := range uint64(7) {
		_, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		rh.accept(t, bz)
	}
	rh.app.SetOpenBlock(rh.exec)
	nonces := resolutionOpenNonces{a: rh.app}
	require.Equal(t, uint64(7), nonces.EvmNonce(sender.EvmAddress))

	type issued struct{ nonce, seq uint64 }
	var receipts []issued
	q, err := sequencer.NewNonceQueue(sequencer.NonceQueueConfig{PerSender: 4, Total: 8, Lifetime: time.Minute}, nonces,
		func(_ common.Address, nonce uint64, tx []byte) error {
			acc, signed, err := rh.r.Accept(tx)
			if err != nil {
				return err
			}
			verified, err := verify.ReceiptSignature(signed, rh.key.PublicKey())
			if err != nil {
				return err
			}
			require.Equal(t, nonce, verified.Receipt.FromSequence)
			receipts = append(receipts, issued{nonce: nonce, seq: acc.Seq})
			return nil
		})
	require.NoError(t, err)

	txs := map[uint64][]byte{}
	for _, nonce := range []uint64{7, 8, 9} {
		_, txs[nonce] = transfer(t, sender, nonce, recipient, big.NewInt(1))
	}

	held, err := q.Hold(sender.EvmAddress, 9, txs[9])
	require.NoError(t, err)
	require.True(t, held)
	require.Empty(t, receipts)

	held, err = q.Hold(sender.EvmAddress, 7, txs[7])
	require.NoError(t, err)
	require.False(t, held)
	require.Equal(t, []issued{{7, 8}}, receipts)
	require.True(t, q.Held(sender.EvmAddress, 9))

	held, err = q.Hold(sender.EvmAddress, 8, txs[8])
	require.NoError(t, err)
	require.False(t, held)
	require.Equal(t, []issued{{7, 8}, {8, 9}, {9, 10}}, receipts)
	require.Equal(t, 0, q.Len())
	require.Equal(t, uint64(10), nonces.EvmNonce(sender.EvmAddress))
}

func TestResolutionReadContext(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	rh := newReceiptHarness(t, 0, nil, sender)
	funded := new(big.Int).Mul(big.NewInt(fundingUhpx), big.NewInt(weiPerUhpx))

	// Off the sequencer the read context is the check context over the committed state.
	requireWei(t, funded, rh.weiBalance(rh.app.LatestCtx(), sender.EvmAddress))
	require.Equal(t, rh.app.GetCheckCtx().MultiStore(), rh.app.LatestCtx().MultiStore())

	value := new(big.Int).Mul(big.NewInt(5), big.NewInt(onePaxInWei))
	_, bz := transfer(t, sender, 0, recipient, value)
	acc, _, _ := rh.accept(t, bz)
	require.Equal(t, uint64(transferGas), uint64(acc.Result.GasUsed))

	rh.app.SetOpenBlock(rh.exec)
	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	want := new(big.Int).Sub(new(big.Int).Sub(funded, value), fee)
	ctx := rh.app.LatestCtx()
	requireWei(t, want, rh.weiBalance(ctx, sender.EvmAddress))
	requireWei(t, value, rh.weiBalance(ctx, recipient))
	require.Equal(t, uint64(1), rh.app.EvmKeeper.GetNonce(ctx, sender.EvmAddress))

	// The debit is receipted, not in a block: the committed state is unchanged.
	requireWei(t, funded, rh.weiBalance(rh.committedCtx(), sender.EvmAddress))
	require.Equal(t, 0, rh.weiBalance(rh.committedCtx(), recipient).Sign())
}

func TestResolutionStandbyReplay(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	l := listen(t)
	p := newPrimary(t, l.Addr().String(), replicationTimeout, sender)
	sb := newStandby(t, p, sender)
	sb.serve(t, l)

	open := p.open()
	entries := []wal.Entry{&wal.Lease{Epoch: 1, KeyID: [32]byte(label("primary")), ExpiryMs: uint64(p.openMs)}, open}
	sent := make([][]byte, 0, 25)
	ids := make([][32]byte, 0, 25)
	for nonce := range uint64(25) {
		signed, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		a := p.accept(bz)
		require.Equal(t, startSeq+nonce+1, a.Seq)
		entries = append(entries, a)
		sent = append(sent, bz)
		ids = append(ids, [32]byte(signed.Hash()))
	}
	ack, err := p.repl.Release(context.Background(), entries...)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: startSeq + 25}, ack)

	// The primary stops before proposing; the standby takes the lease at epoch 2.
	p.repl.Close()
	lease, err := sb.wal.Append(&wal.Lease{Epoch: 2, KeyID: [32]byte(label("standby")), ExpiryMs: uint64(p.openMs)})
	require.NoError(t, err)
	_, err = sb.wal.GroupSync()
	require.NoError(t, err)
	require.NoError(t, lease.Wait(context.Background()))

	primaryBatch, err := p.h.exec.Seal(openHeight)
	require.NoError(t, err)
	batch, err := sb.h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, sent, batch.Txs)
	require.Equal(t, startSeq+1, batch.FromSeq)
	require.Equal(t, startSeq+25, batch.LastSeq)
	require.Equal(t, startSeqHash, batch.PrevSeqHash)
	require.Equal(t, primaryBatch.LastSeqHash, batch.LastSeqHash)
	require.Equal(t, p.openMs, batch.TimestampMs)

	// A validator replica at the same committed state accepts the standby's proposal.
	_, batchTx := resolutionBatch(t, batch, resolutionTxRoot(ids))
	validator := newReceiptHarness(t, 0, resolutionMerged, sender)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resolutionProposal(t, validator.app, openHeight,
		time.UnixMilli(batch.TimestampMs).UTC(), append([][]byte{batchTx}, batch.Txs...)))
}

func TestResolutionActivationHeight(t *testing.T) {
	sender := utils.NewSigner()
	h := newHarness(t, sender)
	ctx := h.app.GetCheckCtx()
	require.False(t, h.app.Sequenced(ctx, 1))
	require.False(t, h.app.Sequenced(ctx, 1_000_000))

	// The block that applies the merge plan is not sequenced; every block after it is.
	h.app.UpgradeKeeper.SetDone(ctx.WithBlockHeight(999), app.MergeUpgrade)
	require.False(t, h.app.Sequenced(ctx, 998))
	require.False(t, h.app.Sequenced(ctx, 999))
	require.True(t, h.app.Sequenced(ctx, 1000))
	require.True(t, h.app.Sequenced(ctx, 1001))
}

func TestResolutionProposalSource(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	rh := newReceiptHarness(t, 0, resolutionMerged, sender)
	ids := make([][32]byte, 0, 3)
	for nonce := range uint64(3) {
		signed, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		rh.accept(t, bz)
		ids = append(ids, [32]byte(signed.Hash()))
	}
	batch, err := rh.exec.Seal(openHeight)
	require.NoError(t, err)
	_, batchTx := resolutionBatch(t, batch, resolutionTxRoot(ids))
	at := time.UnixMilli(batch.TimestampMs).UTC()

	// On a replica that has not applied the merge plan the proposal is the reaped transactions,
	// with no batch.
	unmerged := newReceiptHarness(t, 0, resolutionBlockGas, sender)
	require.False(t, unmerged.app.Sequenced(unmerged.app.GetCheckCtx(), openHeight))
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resolutionProposal(t, unmerged.app, openHeight, at, batch.Txs))

	// Once the merge plan is done it is the sealed slice behind its batch; a reaped block is refused.
	require.True(t, rh.app.Sequenced(rh.app.GetCheckCtx(), openHeight))
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resolutionProposal(t, rh.app, openHeight, at, batch.Txs))
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resolutionProposal(t, rh.app, openHeight, at, nil))
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT,
		resolutionProposal(t, rh.app, openHeight, at, append([][]byte{batchTx}, batch.Txs...)))

}
