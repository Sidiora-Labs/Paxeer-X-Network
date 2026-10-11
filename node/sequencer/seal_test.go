package sequencer_test

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/messages"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	clienttx "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/tx"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/module"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/tx/signing"
	authsigning "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/signing"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/storage/common/keys"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const (
	// sealCosmosGas is the gas limit of each bank send.
	sealCosmosGas = uint64(200_000)
	// sealCosmosFee is the uhpx fee of each bank send, deducted through the deferred balances.
	sealCosmosFee = int64(10_000_000_000)
	// sealTransferGas is the gas of a plain EVM transfer.
	sealTransferGas = uint64(21_000)
	// sealFeeTokenGasPrice is 500 gwei.
	sealFeeTokenGasPrice = int64(500_000_000_000)
	// sealFeeTokenConverted is 21000 gas at 500 gwei at 3.114 Sidiora per Paxeer coin.
	sealFeeTokenConverted = int64(32_697)
	// sealFeeTokenFunded is the Sidiora the payer holds.
	sealFeeTokenFunded = int64(1_000_000)
	// sealUpgradeName names the plan the upgrade test schedules; no handler of the application knows it.
	sealUpgradeName = "sequencer-seal-upgrade"
)

var sealBlockTime = time.Unix(1_800_000_000, 0).UTC()

type sealChain struct {
	w     *app.TestWrapper
	accts []utils.TestAcct
}

// newSealChain brings up an application on the synchronous block path with the default
// consensus block limits and a funded sender.
func newSealChain(t *testing.T) *sealChain {
	app.EnableOCC = false
	t.Cleanup(func() { app.EnableOCC = true })
	accts := utils.NewTestAccounts(2)
	w := app.NewTestWrapper(t, sealBlockTime, accts[0].PublicKey, true, func(ba *baseapp.BaseApp) {
		ba.SetOccEnabled(false)
		ba.SetConcurrencyWorkers(1)
	})
	c := &sealChain{w: w, accts: accts}
	defaults := tmtypes.DefaultBlockParams()
	c.setBlockParams(t, defaults.MaxGas, defaults.MaxGasWanted)
	w.FundAcc(accts[0].AccountAddress, utils.Funds(1_000_000_000_000_000))
	return c
}

func (c *sealChain) setBlockParams(t *testing.T, maxGas, maxGasWanted int64) {
	space, ok := c.w.App.ParamsKeeper.GetSubspace(baseapp.Paramspace)
	require.True(t, ok)
	defaults := tmtypes.DefaultBlockParams()
	space.Set(c.w.Ctx, baseapp.ParamStoreKeyBlockParams, &tmproto.BlockParams{
		MaxBytes:      defaults.MaxBytes,
		MaxGas:        maxGas,
		MinTxsInBlock: defaults.MinTxsInBlock,
		MaxGasWanted:  maxGasWanted,
	})
}

func (c *sealChain) header(height int64) tmproto.Header {
	return tmproto.Header{
		Height:  height,
		ChainID: c.w.Ctx.ChainID(),
		Time:    sealBlockTime.Add(time.Duration(height-1) * time.Second),
	}
}

// evmTransfers signs count EVM transfers from fresh funded signers.
func (c *sealChain) evmTransfers(count int) ([][]byte, [][32]byte) {
	tc := &utils.TestContext{Ctx: c.w.Ctx, TestAccounts: c.accts, TestApp: c.w.App}
	msgs := messages.EVMTransferNonConflicting(tc, count)
	ids := make([][32]byte, 0, count)
	for _, m := range msgs {
		etx, _ := m.Msg.(*evmtypes.MsgEVMTransaction).AsTransaction()
		ids = append(ids, etx.Hash())
	}
	return utils.ToTxBytes(tc, msgs), ids
}

// bankSends signs count bank sends of 1..count uhpx from the first account to the second.
func (c *sealChain) bankSends(t *testing.T, count int) ([][]byte, [][32]byte) {
	tc := app.MakeEncodingConfig().TxConfig
	from := c.accts[0]
	acct := c.w.App.AccountKeeper.GetAccount(c.w.Ctx, from.AccountAddress)
	seq := acct.GetSequence()
	txs := make([][]byte, 0, count)
	ids := make([][32]byte, 0, count)
	for i := 0; i < count; i++ {
		builder := tc.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(banktypes.NewMsgSend(from.AccountAddress, c.accts[1].AccountAddress, utils.Funds(int64(i+1)))))
		builder.SetGasLimit(sealCosmosGas)
		builder.SetFeeAmount(utils.Funds(sealCosmosFee))
		require.NoError(t, builder.SetSignatures(signing.SignatureV2{
			PubKey:   from.PrivateKey.PubKey(),
			Data:     &signing.SingleSignatureData{SignMode: tc.SignModeHandler().DefaultMode()},
			Sequence: seq,
		}))
		sig, err := clienttx.SignWithPrivKey(tc.SignModeHandler().DefaultMode(), authsigning.SignerData{
			ChainID:       c.w.Ctx.ChainID(),
			AccountNumber: acct.GetAccountNumber(),
			Sequence:      seq,
		}, builder, from.PrivateKey, tc, seq)
		require.NoError(t, err)
		require.NoError(t, builder.SetSignatures(sig))
		bz, err := tc.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		txs = append(txs, bz)
		ids = append(ids, sha256.Sum256(bz))
		seq++
	}
	return txs, ids
}

type processedBlock struct {
	ctx     sdk.Context
	events  []abci.Event
	results []*abci.ExecTxResult
	end     abci.ResponseEndBlock
}

// processBlock runs ProcessBlock over txs on a cache branch of ms.
func (c *sealChain) processBlock(t *testing.T, ms sdk.MultiStore, header tmproto.Header, txs [][]byte) processedBlock {
	ctx := sdk.NewContext(ms.CacheMultiStore(), header, false)
	ctx = ctx.WithConsensusParams(c.w.App.GetConsensusParams(ctx))
	events, results, end, err := c.w.App.ProcessBlock(ctx, txs, &app.BlockProcessRequest{
		Height: header.Height,
		Time:   header.Time,
	}, abci.CommitInfo{}, false, nil)
	require.NoError(t, err)
	require.Len(t, results, len(txs))
	return processedBlock{ctx: ctx, events: events, results: results, end: end}
}

// sealBlock opens a layer at header, applies txs in order and seals it.
func sealBlock(t *testing.T, layers *sequencer.Layers, header tmproto.Header, txs [][]byte) (*sequencer.SealedLayer, []sequencer.SequencedTx) {
	open, err := layers.Open(header, nil, nil)
	require.NoError(t, err)
	require.Equal(t, header.Height, open.Height())
	applied := make([]sequencer.SequencedTx, 0, len(txs))
	for i, tx := range txs {
		st, err := open.Apply(tx)
		require.NoError(t, err)
		require.Equal(t, i, st.TxIndex)
		require.Equal(t, uint32(0), st.Result.Code, st.Result.Log)
		applied = append(applied, st)
	}
	sealed, err := sequencer.Seal(layers, header.Height)
	require.NoError(t, err)
	require.Same(t, sealed, layers.Sealed())
	return sealed, applied
}

// requireSameState compares every mounted module store of the sealed layer with the ProcessBlock run.
func requireSameState(t *testing.T, a *app.App, want sdk.Context, got sdk.Context) {
	compared := 0
	for _, name := range keys.MemIAVLStoreKeys {
		key := a.GetKey(name)
		if key == nil {
			continue
		}
		utils.CompareStores(t, key, want.KVStore(key), got.KVStore(key), name)
		compared++
	}
	require.NotZero(t, compared)
}

// requireSameBlock compares results, events and the end-block response with the ProcessBlock run.
func requireSameBlock(t *testing.T, a *app.App, want processedBlock, got *sequencer.SealedLayer) {
	require.Equal(t, want.results, got.Results)
	require.Equal(t, want.events, got.Events)
	require.Equal(t, want.end, got.EndBlock)
	requireSameState(t, a, want.ctx, got.Context())
}

func seqHash(prev [32]byte, seq uint64, activity [32]byte) [32]byte {
	preimage := append([]byte("PAXEER/v1/sequence\x00"), prev[:]...)
	preimage = binary.BigEndian.AppendUint64(preimage, seq)
	preimage = append(preimage, activity[:]...)
	return sha256.Sum256(preimage)
}

func txRoot(ids [][32]byte) [32]byte {
	var concat []byte
	for _, id := range ids {
		concat = append(concat, id[:]...)
	}
	return sha256.Sum256(concat)
}

func TestSealRunsEndOfBlockInProcessBlockOrder(t *testing.T) {
	c := newSealChain(t)
	evmTxs, _ := c.evmTransfers(8)
	bankTxs, _ := c.bankSends(t, 4)
	txs := append(append([][]byte{}, evmTxs[:4]...), bankTxs[:2]...)
	txs = append(txs, evmTxs[4:]...)
	txs = append(txs, bankTxs[2:]...)

	header := c.header(1)
	want := c.processBlock(t, c.w.Ctx.MultiStore(), header, txs)

	layers := sequencer.NewLayers(c.w.App, c.w.Ctx.MultiStore(), 0, [32]byte{})
	sealed, _ := sealBlock(t, layers, header, txs)

	require.Equal(t, int64(1), sealed.Height)
	require.Equal(t, txs, sealed.Txs)
	require.Equal(t, uint64(1), sealed.Batch.FromSeq)
	require.Equal(t, uint64(12), sealed.Batch.ToSeq)
	requireSameBlock(t, c.w.App, want, sealed)

	// The block below is untouched until the sealed layer is committed.
	recipient := c.accts[1].AccountAddress
	require.Equal(t, sdk.NewInt(10), c.w.App.BankKeeper.GetBalance(sealed.Context(), recipient, "uhpx").Amount.Sub(c.w.App.BankKeeper.GetBalance(c.w.Ctx, recipient, "uhpx").Amount))

	// Sealing twice, or a height that is not open, is refused.
	_, err := sequencer.Seal(layers, 1)
	require.ErrorIs(t, err, sequencer.ErrNoOpenLayer)
}

func TestSealClosesBlockBeforeGasLimit(t *testing.T) {
	c := newSealChain(t)
	c.setBlockParams(t, 3*int64(sealTransferGas), tmtypes.DefaultBlockParams().MaxGasWanted)
	txs, _ := c.evmTransfers(4)

	header := c.header(1)
	want := c.processBlock(t, c.w.Ctx.MultiStore(), header, txs[:3])

	layers := sequencer.NewLayers(c.w.App, c.w.Ctx.MultiStore(), 0, [32]byte{})
	open, err := layers.Open(header, nil, nil)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		st, err := open.Apply(txs[i])
		require.NoError(t, err)
		require.Equal(t, uint32(0), st.Result.Code, st.Result.Log)
	}
	_, err = open.Apply(txs[3])
	require.ErrorIs(t, err, sequencer.ErrBlockFull)
	require.ErrorContains(t, err, "gas wanted 84000 of 50000000, gas 84000 of 63000")

	sealed, err := sequencer.Seal(layers, 1)
	require.NoError(t, err)
	require.Equal(t, txs[:3], sealed.Txs)
	require.Equal(t, uint64(3), sealed.Batch.ToSeq)
	requireSameBlock(t, c.w.App, want, sealed)
}

func TestSealStopsBeforeUpgradeHeight(t *testing.T) {
	c := newSealChain(t)
	txs, _ := c.evmTransfers(2)
	require.NoError(t, c.w.App.UpgradeKeeper.ScheduleUpgrade(c.w.Ctx, upgradetypes.Plan{Name: sealUpgradeName, Height: 2}))

	// The block below the upgrade height seals like ProcessBlock.
	header := c.header(1)
	want := c.processBlock(t, c.w.Ctx.MultiStore(), header, txs)
	layers := sequencer.NewLayers(c.w.App, c.w.Ctx.MultiStore(), 0, [32]byte{})
	sealed, _ := sealBlock(t, layers, header, txs)
	requireSameBlock(t, c.w.App, want, sealed)

	// The upgrade height is never opened by the sequencer.
	_, err := layers.Open(c.header(2), nil, nil)
	require.ErrorIs(t, err, sequencer.ErrUpgradeHeight)
	require.EqualError(t, err, `sequencer: an upgrade plan executes at this height: plan "sequencer-seal-upgrade" at height 2`)
	_, err = sequencer.Seal(layers, 2)
	require.ErrorIs(t, err, sequencer.ErrNoOpenLayer)

	// A plan due at the first height is refused too, and the block path applies it.
	ms := c.w.Ctx.MultiStore().CacheMultiStore()
	ctx := c.w.Ctx.WithMultiStore(ms)
	require.NoError(t, c.w.App.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{Name: sealUpgradeName, Height: 1}))
	atPlan := sequencer.NewLayers(c.w.App, ms, 0, [32]byte{})
	_, err = atPlan.Open(c.header(1), nil, nil)
	require.EqualError(t, err, `sequencer: an upgrade plan executes at this height: plan "sequencer-seal-upgrade" at height 1`)

	c.w.App.UpgradeKeeper.SetUpgradeHandler(sealUpgradeName, func(ctx sdk.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
		return fromVM, nil
	})
	blockCtx := sdk.NewContext(ms, c.header(1), false)
	blockCtx = blockCtx.WithConsensusParams(c.w.App.GetConsensusParams(blockCtx))
	_, _, _, err = c.w.App.ProcessBlock(blockCtx, nil, &app.BlockProcessRequest{Height: 1, Time: c.header(1).Time}, abci.CommitInfo{}, false, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), c.w.App.UpgradeKeeper.GetDoneHeight(ctx, sealUpgradeName))
	_, found := c.w.App.UpgradeKeeper.GetUpgradePlan(ctx)
	require.False(t, found)

	afterPlan, _ := sealBlock(t, atPlan, c.header(2), nil)
	require.Equal(t, uint64(1), afterPlan.Batch.FromSeq)
	require.Equal(t, uint64(0), afterPlan.Batch.ToSeq)
}

func TestSealRoutesCollectedFeeTokens(t *testing.T) {
	c := newSealChain(t)
	ctx := c.w.Ctx
	keeper := &c.w.App.EvmKeeper
	denom := bridgetypes.SidioraDenom()

	params := keeper.GetParams(ctx)
	params.FeeTokenEnabled = true
	params.MaxFeeTokenRateAge = evmtypes.DefaultMaxFeeTokenRateAge
	params.AllowedFeeDenoms = []evmtypes.AllowedFeeDenom{{
		Denom:            denom,
		Rate:             sdk.NewDec(evmtypes.InitialSidioraBaseUnitsPerPax),
		RateUpdateHeight: 1,
	}}
	keeper.SetParams(ctx, params)

	payer := utils.NewSigner()
	keeper.SetAddressMapping(ctx, payer.AccountAddress, payer.EvmAddress)
	funding := sdk.NewCoins(sdk.NewCoin(denom, sdk.NewInt(sealFeeTokenFunded)), sdk.NewCoin("uhpx", sdk.NewInt(1_000_000_000)))
	require.NoError(t, c.w.App.BankKeeper.MintCoins(ctx, "mint", funding))
	require.NoError(t, c.w.App.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", payer.AccountAddress, funding))
	require.NoError(t, keeper.SetAccountFeeDenom(ctx, payer.EvmAddress, denom))

	recipient := common.HexToAddress("0x00000000000000000000000000000000000000de")
	txConfig := app.MakeEncodingConfig().TxConfig
	nonce := keeper.GetNonce(ctx, payer.EvmAddress)
	txs := make([][]byte, 0, 3)
	for i := uint64(0); i < 3; i++ {
		signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    nonce + i,
			GasPrice: big.NewInt(sealFeeTokenGasPrice),
			Gas:      sealTransferGas,
			To:       &recipient,
			Value:    big.NewInt(0),
		}), ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), payer.EvmPrivateKey)
		require.NoError(t, err)
		txData, err := ethtx.NewTxDataFromTx(signed)
		require.NoError(t, err)
		msg, err := evmtypes.NewMsgEVMTransaction(txData)
		require.NoError(t, err)
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		builder.SetGasLimit(sealTransferGas)
		bz, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		txs = append(txs, bz)
	}

	collector := c.w.App.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	holding := c.w.App.AccountKeeper.GetModuleAddress(evmtypes.FeeTokenHoldingAccount)
	require.NotNil(t, holding)
	holdingBefore := c.w.App.BankKeeper.GetBalance(ctx, holding, denom).Amount

	header := c.header(1)
	want := c.processBlock(t, ctx.MultiStore(), header, txs)
	layers := sequencer.NewLayers(c.w.App, ctx.MultiStore(), 0, [32]byte{})
	sealed, _ := sealBlock(t, layers, header, txs)
	requireSameBlock(t, c.w.App, want, sealed)

	got := sealed.Context()
	charged := sdk.NewInt(3 * sealFeeTokenConverted)
	require.Equal(t, holdingBefore.Add(charged), c.w.App.BankKeeper.GetBalance(got, holding, denom).Amount)
	require.True(t, c.w.App.BankKeeper.GetBalance(got, collector, denom).Amount.IsZero())
	require.Equal(t, sdk.NewInt(sealFeeTokenFunded).Sub(charged), c.w.App.BankKeeper.GetBalance(got, payer.AccountAddress, denom).Amount)
}

func TestSealAppliesDeferredBalances(t *testing.T) {
	c := newSealChain(t)
	ctx := c.w.Ctx
	txs, _ := c.bankSends(t, 20)

	collector := c.w.App.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)
	sender := c.accts[0].AccountAddress
	recipient := c.accts[1].AccountAddress
	collectorBefore := c.w.App.BankKeeper.GetBalance(ctx, collector, "uhpx").Amount
	senderBefore := c.w.App.BankKeeper.GetBalance(ctx, sender, "uhpx").Amount
	recipientBefore := c.w.App.BankKeeper.GetBalance(ctx, recipient, "uhpx").Amount

	header := c.header(1)
	want := c.processBlock(t, ctx.MultiStore(), header, txs)
	layers := sequencer.NewLayers(c.w.App, ctx.MultiStore(), 0, [32]byte{})
	sealed, _ := sealBlock(t, layers, header, txs)
	requireSameBlock(t, c.w.App, want, sealed)

	got := sealed.Context()
	fees := sdk.NewInt(20 * sealCosmosFee)
	sent := sdk.NewInt(210)
	require.Equal(t, collectorBefore.Add(fees), c.w.App.BankKeeper.GetBalance(got, collector, "uhpx").Amount)
	require.Equal(t, senderBefore.Sub(fees).Sub(sent), c.w.App.BankKeeper.GetBalance(got, sender, "uhpx").Amount)
	require.Equal(t, recipientBefore.Add(sent), c.w.App.BankKeeper.GetBalance(got, recipient, "uhpx").Amount)
	pending := 0
	c.w.App.BankKeeper.IterateDeferredBalances(got, func(sdk.AccAddress, sdk.Coin) bool {
		pending++
		return false
	})
	require.Zero(t, pending)
}

func TestSealRecordsSeqHashChain(t *testing.T) {
	c := newSealChain(t)
	evmTxs, evmIDs := c.evmTransfers(4)
	bankTxs, bankIDs := c.bankSends(t, 2)
	firstTxs := append(append([][]byte{}, evmTxs[:3]...), bankTxs...)
	firstIDs := append(append([][32]byte{}, evmIDs[:3]...), bankIDs...)

	prior := sha256.Sum256([]byte("prior sequence head"))
	const priorSeq = uint64(41)

	header := c.header(1)
	want := c.processBlock(t, c.w.Ctx.MultiStore(), header, firstTxs)
	layers := sequencer.NewLayers(c.w.App, c.w.Ctx.MultiStore(), priorSeq, prior)
	sealed, applied := sealBlock(t, layers, header, firstTxs)
	requireSameBlock(t, c.w.App, want, sealed)

	head := prior
	for i, st := range applied {
		seq := priorSeq + 1 + uint64(i)
		require.Equal(t, seq, st.Seq)
		require.Equal(t, firstIDs[i], st.ActivityID)
		head = seqHash(head, seq, firstIDs[i])
		require.Equal(t, head, st.SeqHash)
	}
	require.Equal(t, sequencer.SequenceBatchFields{
		Height:      1,
		FromSeq:     42,
		ToSeq:       46,
		PrevSeqHash: prior,
		LastSeqHash: head,
		TxRoot:      txRoot(firstIDs),
	}, sealed.Batch)
	lastSeq, lastSeqHash := layers.LastSeq()
	require.Equal(t, uint64(46), lastSeq)
	require.Equal(t, head, lastSeqHash)

	// The next block opens over the sealed layer and continues the chain.
	_, err := layers.Open(c.header(3), nil, nil)
	require.ErrorIs(t, err, sequencer.ErrHeightMismatch)
	second, err := layers.Open(c.header(2), nil, nil)
	require.NoError(t, err)
	_, err = layers.Open(c.header(2), nil, nil)
	require.ErrorIs(t, err, sequencer.ErrLayerOpen)
	st, err := second.Apply(evmTxs[3])
	require.NoError(t, err)
	require.Equal(t, uint32(0), st.Result.Code, st.Result.Log)
	require.Equal(t, uint64(47), st.Seq)
	require.Equal(t, seqHash(head, 47, evmIDs[3]), st.SeqHash)
	_, err = sequencer.Seal(layers, 2)
	require.ErrorIs(t, err, sequencer.ErrSealedPending)
	require.ErrorIs(t, layers.Committed(2), sequencer.ErrHeightMismatch)
	require.NoError(t, layers.Committed(1))
	require.ErrorIs(t, layers.Committed(1), sequencer.ErrNoSealedLayer)

	sealedTwo, err := sequencer.Seal(layers, 2)
	require.NoError(t, err)
	require.Equal(t, sequencer.SequenceBatchFields{
		Height:      2,
		FromSeq:     47,
		ToSeq:       47,
		PrevSeqHash: head,
		LastSeqHash: st.SeqHash,
		TxRoot:      txRoot([][32]byte{evmIDs[3]}),
	}, sealedTwo.Batch)

	// An empty block keeps the head: toSeq = fromSeq - 1 and lastSeqHash = prevSeqHash.
	require.NoError(t, layers.Committed(2))
	empty, _ := sealBlock(t, layers, c.header(3), nil)
	require.Equal(t, sequencer.SequenceBatchFields{
		Height:      3,
		FromSeq:     48,
		ToSeq:       47,
		PrevSeqHash: st.SeqHash,
		LastSeqHash: st.SeqHash,
		TxRoot:      sha256.Sum256(nil),
	}, empty.Batch)
}
