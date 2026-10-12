package sequencer_test

import (
	"context"
	"crypto/sha256"
	"math"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer/wal"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const (
	// sourceLastSeq is the stored lastSeq the consensus source continues.
	sourceLastSeq = uint64(30)
	// sourceTxs is the number of receipted transactions of the sealed block.
	sourceTxs = 12
)

var sourcePrevSeqHash = sha256.Sum256([]byte("consensus source previous seq hash"))

type sourceHarness struct {
	*harness
	r   *sequencer.Receipter
	src *sequencer.ConsensusSource
}

// newSourceHarness builds the real App on the synchronous block path, funds the given accounts
// (EVM-mapped), runs setup on the genesis context, writes the sequencer genesis with the test
// receipt key pinned and the stored chain at sourceLastSeq, commits height 1, sets the
// activation height and builds the executor, receipter and consensus source over them.
func newSourceHarness(t *testing.T, activation int64, setup func(ctx sdk.Context, a *app.App), funded ...utils.TestAcct) *sourceHarness {
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
	if setup != nil {
		setup(tw.Ctx, a)
	}
	require.NoError(t, a.SequencerKeeper.InitGenesis(tw.Ctx, seqtypes.GenesisState{
		Leaders: [][32]byte{[32]byte(label("consensus source leader"))},
		ReceiptKeys: []seqtypes.ReceiptKey{{
			KeyId:       testKeyID,
			PublicKey:   publicOf(testKeySeed),
			FirstHeight: 1,
			LastHeight:  math.MaxUint64,
		}},
		LastSeq:     sourceLastSeq,
		LastSeqHash: sourcePrevSeqHash,
	}))
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: testChainID, Height: committedTip},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	require.NoError(t, a.SetActivationHeight(activation))

	proposer := valPub.Address().Bytes()
	exec, err := sequencer.NewExecutor(a, sequencer.Options{
		ChainID:         testChainID,
		ProposerAddress: proposer,
		LastSeq:         sourceLastSeq,
		LastSeqHash:     sourcePrevSeqHash,
	})
	require.NoError(t, err)
	key, err := loadKey(t, testKeySeed, []verify.SequencerAuthorization{{
		SequencerID: testKeyID, PublicKey: publicOf(testKeySeed), FirstBatchNumber: 1, LastBatchNumber: 1000,
	}}, openHeight)
	require.NoError(t, err)
	r, err := sequencer.NewReceipter(exec, a, &a.EvmKeeper, key, receiptModuleID)
	require.NoError(t, err)
	src, err := sequencer.NewConsensusSource(a, exec, key)
	require.NoError(t, err)
	return &sourceHarness{
		harness: &harness{t: t, app: a, exec: exec, proposer: proposer, genesis: tm},
		r:       r,
		src:     src,
	}
}

func TestConsensusSourceTxZeroBatch(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newSourceHarness(t, openHeight, nil, sender)
	h.open()

	walBytes := make([][]byte, 0, sourceTxs)
	ids := make([][32]byte, 0, sourceTxs)
	head := sourcePrevSeqHash
	for nonce := range uint64(sourceTxs) {
		signed, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		acc, receipt, err := h.r.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, sourceLastSeq+1+nonce, acc.Seq)
		require.Equal(t, uint32(0), acc.Result.Code, acc.Result.Log)
		entry, err := wal.EncodeEntry(&wal.Accept{Seq: acc.Seq, Height: uint64(acc.Height), Tx: bz, Receipt: receipt})
		require.NoError(t, err)
		walBytes = append(walBytes, entry)
		ids = append(ids, [32]byte(signed.Hash()))
		head = expectedSeqHash(head, acc.Seq, ids[nonce])
	}

	block, err := h.src.Seal(context.Background(), openHeight)
	require.NoError(t, err)

	msg, err := app.DecodeSequenceBatch(block.Batch)
	require.NoError(t, err)
	root := resolutionTxRoot(ids)
	require.Equal(t, seqtypes.MsgSequenceBatch{
		Height:      uint64(openHeight),
		FromSeq:     31,
		ToSeq:       42,
		PrevSeqHash: sourcePrevSeqHash[:],
		LastSeqHash: head[:],
		TxRoot:      root[:],
		KeyId:       testKeyID[:],
		Sig:         msg.Sig,
	}, msg)
	require.NoError(t, msg.VerifySignature(testChainID, publicOf(testKeySeed)))
	require.ErrorIs(t, msg.VerifySignature("other-chain", publicOf(testKeySeed)), seqtypes.ErrBadSignature)

	require.Len(t, block.Txs, sourceTxs)
	for i, raw := range walBytes {
		entry, err := wal.DecodeEntry(raw)
		require.NoError(t, err)
		accept, ok := entry.(*wal.Accept)
		require.True(t, ok)
		require.Equal(t, sourceLastSeq+1+uint64(i), accept.Seq)
		require.Equal(t, tmtypes.Tx(accept.Tx), block.Txs[i])
	}

	sealed := h.src.Sealed()
	require.NotNil(t, sealed)
	require.Equal(t, append([][]byte{block.Batch}, block.Txs.ToSliceOfBytes()...), sealed.Txs)
	require.Equal(t, uint64(42), sealed.Batch.ToSeq)
	requireStoredSequence(t, h.app, sealed.Context(), 42, head)

	_, err = h.src.Seal(context.Background(), openHeight)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
}

func TestConsensusSourceOpenTime(t *testing.T) {
	h := newSourceHarness(t, openHeight, nil)
	openTime := h.genesis.Add(1500 * time.Millisecond)
	ms := h.openAt(openTime)

	block, err := h.src.Seal(context.Background(), openHeight)
	require.NoError(t, err)
	want := time.UnixMilli(ms).UTC()
	require.Equal(t, want, block.OpenTime)
	require.Empty(t, block.Txs)
	require.Equal(t, want, h.src.Sealed().Context().BlockHeader().Time)

	msg, err := app.DecodeSequenceBatch(block.Batch)
	require.NoError(t, err)
	require.True(t, msg.IsEmpty())
	require.Equal(t, uint64(31), msg.FromSeq)
	require.Equal(t, uint64(30), msg.ToSeq)
	require.Equal(t, seqtypes.EmptyTxRoot[:], msg.TxRoot)
	require.NoError(t, msg.VerifySignature(testChainID, publicOf(testKeySeed)))
}

func TestConsensusSourceRoutesFees(t *testing.T) {
	denom := bridgetypes.SidioraDenom()
	payer := utils.NewSigner()
	setup := func(ctx sdk.Context, a *app.App) {
		keeper := &a.EvmKeeper
		params := keeper.GetParams(ctx)
		params.FeeTokenEnabled = true
		params.MaxFeeTokenRateAge = evmtypes.DefaultMaxFeeTokenRateAge
		params.AllowedFeeDenoms = []evmtypes.AllowedFeeDenom{{
			Denom:            denom,
			Rate:             sdk.NewDec(evmtypes.InitialSidioraBaseUnitsPerPax),
			RateUpdateHeight: committedTip,
		}}
		keeper.SetParams(ctx, params)
		funding := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(sealFeeTokenFunded)))
		require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", funding))
		require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", payer.AccountAddress, funding))
		require.NoError(t, keeper.SetAccountFeeDenom(ctx, payer.EvmAddress, denom))
	}
	h := newSourceHarness(t, openHeight, setup, payer)
	collector := h.app.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	holding := h.app.AccountKeeper.GetModuleAddress(evmtypes.FeeTokenHoldingAccount)
	require.NotNil(t, holding)
	holdingBefore := h.app.BankKeeper.GetBalance(h.committedCtx(), holding, denom).Amount

	h.open()
	recipient := common.HexToAddress("0x00000000000000000000000000000000000000de")
	for nonce := range uint64(3) {
		_, bz := evmTx(t, payer, receiptLegacyTx(nonce, &recipient, big.NewInt(0), sealTransferGas, nil))
		acc, err := h.exec.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, uint32(0), acc.Result.Code, acc.Result.Log)
	}
	require.Equal(t, holdingBefore, h.app.BankKeeper.GetBalance(h.openCtx(), holding, denom).Amount)

	_, err := h.src.Seal(context.Background(), openHeight)
	require.NoError(t, err)
	got := h.src.Sealed().Context()
	charged := sdk.NewInt(3 * sealFeeTokenConverted)
	require.Equal(t, holdingBefore.Add(charged), h.app.BankKeeper.GetBalance(got, holding, denom).Amount)
	require.True(t, h.app.BankKeeper.GetBalance(got, collector, denom).Amount.IsZero())
	require.Equal(t, sdk.NewInt(sealFeeTokenFunded).Sub(charged), h.app.BankKeeper.GetBalance(got, payer.AccountAddress, denom).Amount)
}

func TestConsensusSourceBelowActivation(t *testing.T) {
	const activation = int64(5)
	h := newSourceHarness(t, activation, nil)
	for height := int64(1); height < activation; height++ {
		require.False(t, h.src.Sequenced(height), "height %d", height)
	}
	require.True(t, h.src.Sequenced(activation))
	require.True(t, h.src.Sequenced(activation+1))

	h.open()
	_, err := h.src.Seal(context.Background(), openHeight)
	require.ErrorIs(t, err, sequencer.ErrNotSequenced)
	require.EqualError(t, err, "sequencer: height is below the activation height: height 2, activation height 5")
	require.Nil(t, h.src.Sealed())
	_, err = h.exec.Snapshot()
	require.NoError(t, err, "a refused seal leaves the open layer")

	require.NoError(t, h.app.SetActivationHeight(0))
	for _, height := range []int64{1, openHeight, activation, math.MaxInt64} {
		require.False(t, h.src.Sequenced(height), "height %d", height)
	}
}
