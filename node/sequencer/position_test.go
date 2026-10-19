package sequencer_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"math/big"
	"os"
	"reflect"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	xwebprecompile "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	wasmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/keeper"
	wasmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/wasm/x/wasm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const positionCosmosGas = uint64(200_000)

var (
	xwebAddress   = common.HexToAddress(xwebtypes.PrecompileAddress)
	unknownMethod = []byte{0xde, 0xad, 0xbe, 0xef}
	minorXWebPlan = `{"upgradeType":"minor"}`
)

// newPrecompileHarness builds the receipt harness on an App that serves the
// custom precompile set, the way a node serves it.
func newPrecompileHarness(t *testing.T, setup func(ctx sdk.Context, a *app.App), funded ...utils.TestAcct) *receiptHarness {
	t.Helper()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := app.NewTestWrapper(t, tm, valPub, true)
	a := tw.App
	for _, acct := range funded {
		a.EvmKeeper.SetAddressMapping(tw.Ctx, acct.AccountAddress, acct.EvmAddress)
		tw.FundAcc(acct.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(fundingUhpx))))
	}
	if setup != nil {
		setup(tw.Ctx, a)
	}
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: testChainID, Height: committedTip},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	proposer := valPub.Address().Bytes()
	exec, err := sequencer.NewExecutor(a, sequencer.Options{ChainID: testChainID, ProposerAddress: proposer})
	require.NoError(t, err)
	h := &harness{t: t, app: a, exec: exec, proposer: proposer, genesis: tm}
	openMs := h.open()
	require.NoError(t, sequencer.RefreshPrecompiles(a, exec))
	key, err := loadKey(t, testKeySeed, []verify.SequencerAuthorization{{
		SequencerID: testKeyID, PublicKey: publicOf(testKeySeed), FirstBatchNumber: 1, LastBatchNumber: 1000,
	}}, openHeight)
	require.NoError(t, err)
	r, err := sequencer.NewReceipter(h.exec, h.app, &h.app.EvmKeeper, key, receiptModuleID)
	require.NoError(t, err)
	return &receiptHarness{harness: h, key: key, r: r, openMs: openMs}
}

// sealedBlock is the block a sealed batch becomes: the signed
// MsgSequenceBatch at index 0 and the sealed transactions behind it.
func sealedBlock(t *testing.T, batch *sequencer.Batch, ids [][32]byte) [][]byte {
	t.Helper()
	_, batchTx := resolutionBatch(t, batch, resolutionTxRoot(ids))
	return append([][]byte{batchTx}, batch.Txs...)
}

// finalizeSealed runs the block path over block at the batch's height and
// time on a and commits it.
func finalizeSealed(t *testing.T, a *app.App, batch *sequencer.Batch, block [][]byte) []*abci.ExecTxResult {
	t.Helper()
	resp, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Txs:    block,
		Header: &tmproto.Header{ChainID: testChainID, Height: batch.Height, Time: time.UnixMilli(batch.TimestampMs).UTC()},
	})
	require.NoError(t, err)
	require.Len(t, resp.TxResults, len(block))
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, batch.Height, a.LastBlockHeight())
	return resp.TxResults
}

func committedAt(a *app.App, height int64) sdk.Context {
	return sdk.NewContext(a.CommitMultiStore().CacheMultiStore(), tmproto.Header{ChainID: testChainID, Height: height}, false)
}

func uhpx(a *app.App, ctx sdk.Context, addr sdk.AccAddress) int64 {
	return a.BankKeeper.GetBalance(ctx, addr, "uhpx").Amount.Int64()
}

func xwebInfo() precompiles.PrecompileInfo {
	return precompiles.GetPrecompileInfo(xwebprecompile.PrecompileName)
}

// unknownMethodError is the error the xweb precompile fails with for a
// selector its ABI does not carry.
func unknownMethodError(t *testing.T) error {
	t.Helper()
	info := xwebInfo()
	_, err := info.ABI.MethodById(unknownMethod)
	require.Error(t, err)
	return err
}

func cw20Setup(t *testing.T, creator utils.TestAcct, contract *sdk.AccAddress) func(ctx sdk.Context, a *app.App) {
	return func(ctx sdk.Context, a *app.App) {
		code, err := os.ReadFile("../../contracts/wasm/cw20_base.wasm")
		require.NoError(t, err)
		keeper := wasmkeeper.NewDefaultPermissionKeeper(a.WasmKeeper)
		codeID, err := keeper.Create(ctx, creator.AccountAddress, code, nil)
		require.NoError(t, err)
		init := fmt.Sprintf(`{"name":"position","symbol":"POS","decimals":6,"initial_balances":[{"address":"%s","amount":"1000000"}]}`, creator.AccountAddress)
		addr, _, err := keeper.Instantiate(ctx, codeID, creator.AccountAddress, creator.AccountAddress, []byte(init), "position", nil)
		require.NoError(t, err)
		*contract = addr
	}
}

func cw20Balance(t *testing.T, a *app.App, ctx sdk.Context, contract, holder sdk.AccAddress) string {
	t.Helper()
	out, err := a.WasmKeeper.QuerySmart(ctx, contract, []byte(fmt.Sprintf(`{"balance":{"address":"%s"}}`, holder)))
	require.NoError(t, err)
	return string(out)
}

func TestPositionWasmCounterMatchesBlock(t *testing.T) {
	sender, recipient := utils.NewSigner(), utils.NewSigner()
	var contract sdk.AccAddress
	rh := newReceiptHarness(t, 0, cw20Setup(t, sender, &contract), sender)
	require.NotEmpty(t, contract)

	ids := make([][32]byte, 0, 5)
	for i := 1; i <= 4; i++ {
		send := signCosmos(t, rh.openCtx(), rh.harness, sender, positionCosmosGas,
			banktypes.NewMsgSend(sender.AccountAddress, recipient.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(1)))))
		acc, rec, _ := rh.accept(t, send)
		require.Equal(t, i, acc.TxIndex)
		require.Equal(t, int32(0), rec.ResultCode)
		ids = append(ids, acc.ActivityID)
	}
	execute := signCosmos(t, rh.openCtx(), rh.harness, sender, positionCosmosGas, &wasmtypes.MsgExecuteContract{
		Sender:   sender.AccountAddress.String(),
		Contract: contract.String(),
		Msg:      []byte(fmt.Sprintf(`{"transfer":{"recipient":"%s","amount":"100"}}`, recipient.AccountAddress)),
	})
	acc, rec, _ := rh.accept(t, execute)
	require.Equal(t, 5, acc.TxIndex)
	require.Equal(t, uint64(5), acc.Seq)
	require.Equal(t, uint32(0), acc.Result.Code, acc.Result.Log)
	require.Equal(t, int32(0), rec.ResultCode)
	ids = append(ids, acc.ActivityID)

	// The position the sealed block gives the execute is the one the sequencer ran it at.
	positioned, err := sequencer.SetTxPosition(rh.openCtx(), acc.TxIndex)
	require.NoError(t, err)
	require.Equal(t, 5, positioned.TxIndex())
	_, counted := wasmtypes.TXCounter(positioned)
	require.False(t, counted)
	for _, bad := range []int{0, -1, math.MaxUint32 + 1} {
		_, err := sequencer.SetTxPosition(rh.openCtx(), bad)
		require.ErrorIs(t, err, sequencer.ErrTxPosition, "index %d", bad)
	}

	batch, err := rh.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Len(t, batch.Results, 5)

	// A validator at the same committed state executes the sealed block.
	var twinContract sdk.AccAddress
	twin := newReceiptHarness(t, rh.openMs, cw20Setup(t, sender, &twinContract), sender)
	require.Equal(t, contract, twinContract)
	results := finalizeSealed(t, twin.app, batch, sealedBlock(t, batch, ids))
	for i, want := range batch.Results {
		require.Equal(t, want, results[i+1], "tx %d", i+1)
	}

	require.Equal(t, `{"balance":"100"}`, cw20Balance(t, rh.app, batch.Ctx, contract, recipient.AccountAddress))
	committed := committedAt(twin.app, openHeight).WithBlockTime(time.UnixMilli(batch.TimestampMs).UTC())
	require.Equal(t, `{"balance":"100"}`, cw20Balance(t, twin.app, committed, contract, recipient.AccountAddress))
	require.Equal(t, `{"balance":"999900"}`, cw20Balance(t, twin.app, committed, contract, sender.AccountAddress))
}

func TestPositionCosmosTxUsesAccept(t *testing.T) {
	sender, recipient := utils.NewSigner(), utils.NewSigner()
	h := newHarness(t, sender)
	h.open()
	send := signCosmos(t, h.openCtx(), h, sender, positionCosmosGas,
		banktypes.NewMsgSend(sender.AccountAddress, recipient.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(7)))))

	acc, err := h.exec.Accept(send)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, 1, acc.TxIndex)
	require.Equal(t, openHeight, acc.Height)
	require.Equal(t, sha256.Sum256(send), acc.ActivityID)
	require.Equal(t, uint32(0), acc.Result.Code, acc.Result.Log)
	require.Nil(t, acc.Result.EvmTxInfo)
	require.Equal(t, int64(positionCosmosGas), acc.Result.GasWanted)

	// It ran in deliver mode on the open layer: the ante chain took the fee and
	// the sequence, and the send moved the coins.
	open := h.openCtx()
	require.False(t, open.IsCheckTx())
	require.Equal(t, openHeight, open.BlockHeight())
	require.Equal(t, uint64(1), h.app.AccountKeeper.GetAccount(open, sender.AccountAddress).GetSequence())
	require.Equal(t, int64(fundingUhpx-cosmosFee-7), uhpx(h.app, open, sender.AccountAddress))
	require.Equal(t, int64(7), uhpx(h.app, open, recipient.AccountAddress))

	// CheckTx never saw it: the check state and the committed state are untouched.
	for _, ctx := range []sdk.Context{h.app.GetCheckCtx(), h.committedCtx()} {
		require.Equal(t, uint64(0), h.app.AccountKeeper.GetAccount(ctx, sender.AccountAddress).GetSequence())
		require.Equal(t, int64(fundingUhpx), uhpx(h.app, ctx, sender.AccountAddress))
		require.Zero(t, uhpx(h.app, ctx, recipient.AccountAddress))
	}
}

func TestPositionDecoderMatchesBlock(t *testing.T) {
	evmSender, cosmosSender, recipient := utils.NewSigner(), utils.NewSigner(), utils.NewSigner()
	h := newHarness(t, evmSender, cosmosSender)
	h.open()
	var evmNonce uint64
	for i := 1; i <= 20; i++ {
		var bz []byte
		if i%2 == 1 {
			_, bz = transfer(t, evmSender, evmNonce, recipient.EvmAddress, big.NewInt(1))
			evmNonce++
		} else {
			bz = signCosmos(t, h.openCtx(), h, cosmosSender, positionCosmosGas,
				banktypes.NewMsgSend(cosmosSender.AccountAddress, recipient.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(1)))))
		}
		acc, err := h.exec.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, i, acc.TxIndex)
		require.Equal(t, uint32(0), acc.Result.Code, acc.Result.Log)
	}
	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Len(t, batch.Txs, 20)

	// The block decodes tx 0 apart; the sealed transactions take indexes 1..20.
	blockTxs := h.app.DecodeTransactionsConcurrently(batch.Ctx, batch.Txs)
	require.Len(t, blockTxs, 20)
	decode := h.app.GetTxConfig().TxDecoder()
	encode := h.app.GetTxConfig().TxEncoder()
	for i, bz := range batch.Txs {
		accepted, err := decode(bz)
		require.NoError(t, err)
		block := blockTxs[i]
		require.NotNil(t, block, "tx %d", i+1)
		require.Equal(t, reflect.TypeOf(block), reflect.TypeOf(accepted), "tx %d", i+1)
		require.Len(t, accepted.GetMsgs(), len(block.GetMsgs()))
		for j, msg := range accepted.GetMsgs() {
			require.Equal(t, sdk.MsgTypeURL(block.GetMsgs()[j]), sdk.MsgTypeURL(msg), "tx %d msg %d", i+1, j)
		}
		acceptedBz, err := encode(accepted)
		require.NoError(t, err)
		blockBz, err := encode(block)
		require.NoError(t, err)
		require.Equal(t, blockBz, acceptedBz, "tx %d", i+1)

		blockMsg := h.app.GetEVMMsg(block)
		if i%2 == 1 {
			require.Nil(t, batch.EVMMsgs[i], "tx %d", i+1)
			require.Nil(t, blockMsg, "tx %d", i+1)
			continue
		}
		require.NotNil(t, batch.EVMMsgs[i], "tx %d", i+1)
		require.NotNil(t, blockMsg, "tx %d", i+1)
		seqData, err := batch.EVMMsgs[i].Marshal()
		require.NoError(t, err)
		blockData, err := blockMsg.Marshal()
		require.NoError(t, err)
		require.Equal(t, blockData, seqData, "tx %d", i+1)
		require.NotNil(t, batch.EVMMsgs[i].Derived, "tx %d", i+1)
		require.Equal(t, blockMsg.Derived, batch.EVMMsgs[i].Derived, "tx %d", i+1)
		require.Equal(t, evmSender.EvmAddress, batch.EVMMsgs[i].Derived.SenderEVMAddr, "tx %d", i+1)
	}
}

func TestPositionPrecompileRevertBytes(t *testing.T) {
	sender := utils.NewSigner()
	rh := newPrecompileHarness(t, nil, sender)
	require.Contains(t, rh.app.EvmKeeper.CustomPrecompiles(rh.openCtx()), xwebAddress)

	etx, bz := evmTx(t, sender, receiptLegacyTx(0, &xwebAddress, big.NewInt(0), callGas, unknownMethod))
	acc, rec, _ := rh.accept(t, bz)
	revert := pcommon.RevertReason(unknownMethodError(t))
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, revert[:4])
	require.Equal(t, revert, evmReturnData(t, acc.Result))
	require.Equal(t, int32(1), rec.ResultCode)
	require.Equal(t, [32]byte(etx.Hash()), rec.ActivityID)
	require.Equal(t, sha256.Sum256(revert), rec.ActivityRoot)

	batch, err := rh.exec.Seal(openHeight)
	require.NoError(t, err)
	twin := newPrecompileHarness(t, nil, sender)
	results := finalizeSealed(t, twin.app, batch, sealedBlock(t, batch, [][32]byte{acc.ActivityID}))
	require.Equal(t, batch.Results[0], results[1])
	require.Equal(t, revert, evmReturnData(t, results[1]))
}

func TestPositionPrecompileSetRefreshedOnOpen(t *testing.T) {
	sender := utils.NewSigner()
	rewind := func(ctx sdk.Context, a *app.App) {
		store := ctx.KVStore(a.GetKey(xwebtypes.StoreKey))
		iter := store.Iterator(nil, nil)
		var stale [][]byte
		for ; iter.Valid(); iter.Next() {
			stale = append(stale, iter.Key())
		}
		require.NoError(t, iter.Close())
		require.NotEmpty(t, stale)
		for _, key := range stale {
			store.Delete(key)
		}
		ctx.KVStore(a.GetKey(upgradetypes.StoreKey)).Delete(append([]byte{upgradetypes.VersionMapByte}, xwebtypes.ModuleName...))
		require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(ctx, upgradetypes.Plan{
			Name: precompiles.XWebUpgrade, Height: openHeight + 1, Info: minorXWebPlan,
		}))
	}
	rh := newPrecompileHarness(t, rewind, sender)
	require.True(t, rh.app.UpgradeKeeper.HasHandler(precompiles.XWebUpgrade))
	info := xwebInfo()
	feeInput, err := info.ABI.Pack(xwebprecompile.FeeMethod)
	require.NoError(t, err)

	// Height 2 opens before the xweb upgrade: the precompile is not served.
	open := rh.openCtx()
	require.Zero(t, rh.app.UpgradeKeeper.GetDoneHeight(open, precompiles.XWebUpgrade))
	require.NotContains(t, rh.app.UpgradeKeeper.GetModuleVersionMap(open), xwebtypes.ModuleName)
	require.NotContains(t, rh.app.EvmKeeper.CustomPrecompiles(open), xwebAddress)
	_, bz := evmTx(t, sender, receiptLegacyTx(0, &xwebAddress, big.NewInt(0), callGas, feeInput))
	before, err := rh.exec.Accept(bz)
	require.NoError(t, err)
	require.Equal(t, uint32(0), before.Result.Code, before.Result.Log)
	require.Empty(t, evmReturnData(t, before.Result))

	batch, err := rh.exec.Seal(openHeight)
	require.NoError(t, err)
	_, err = rh.exec.Snapshot()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	require.ErrorIs(t, sequencer.RefreshPrecompiles(rh.app, rh.exec), sequencer.ErrLayerNotOpen)
	results := finalizeSealed(t, rh.app, batch, sealedBlock(t, batch, [][32]byte{before.ActivityID}))
	require.Equal(t, batch.Results[0], results[1])
	require.NotContains(t, rh.app.EvmKeeper.CustomPrecompiles(committedAt(rh.app, openHeight)), xwebAddress)

	// Height 3 is the upgrade height: once it opens the block serves xweb.
	require.NoError(t, rh.exec.Open(openHeight+1, rh.openMs+1000))
	require.NoError(t, sequencer.RefreshPrecompiles(rh.app, rh.exec))
	open = rh.openCtx()
	require.Equal(t, openHeight+1, rh.app.UpgradeKeeper.GetDoneHeight(open, precompiles.XWebUpgrade))
	require.Contains(t, rh.app.EvmKeeper.CustomPrecompiles(open), xwebAddress)
	_, bz = evmTx(t, sender, receiptLegacyTx(1, &xwebAddress, big.NewInt(0), callGas, feeInput))
	after, err := rh.exec.Accept(bz)
	require.NoError(t, err)
	require.Equal(t, 1, after.TxIndex)
	require.Equal(t, uint32(0), after.Result.Code, after.Result.Log)
	values, err := info.ABI.Unpack(xwebprecompile.FeeMethod, evmReturnData(t, after.Result))
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.Equal(t, xwebtypes.DefaultParams(xwebtypes.DefaultAuthority()).Fee.Mul(state.SdkUhpxToSweiMultiplier).BigInt(), values[0])
}

func TestPositionPrecompileErrorRecorded(t *testing.T) {
	sender := utils.NewSigner()
	rh := newPrecompileHarness(t, nil, sender)
	etx, bz := evmTx(t, sender, dynamicTx(0, &xwebAddress, big.NewInt(0), callGas, unknownMethod))
	acc, _, _ := rh.accept(t, bz)
	wantVMError := "execution reverted|" + unknownMethodError(t).Error()
	transient := rh.transient(t, etx, acc)
	require.Equal(t, wantVMError, transient.VmError)
	require.Equal(t, uint32(0), transient.Status)
	require.Equal(t, uint32(acc.TxIndex), transient.TransactionIndex)

	batch, err := rh.exec.Seal(openHeight)
	require.NoError(t, err)
	twin := newPrecompileHarness(t, nil, sender)
	results := finalizeSealed(t, twin.app, batch, sealedBlock(t, batch, [][32]byte{acc.ActivityID}))
	require.Equal(t, batch.Results[0].GasUsed, results[1].GasUsed)
	require.Equal(t, batch.Results[0], results[1])

	committed, err := twin.app.EvmKeeper.GetReceiptWithRetry(committedAt(twin.app, openHeight), etx.Hash(), 5)
	require.NoError(t, err)
	require.Equal(t, transient.VmError, committed.VmError)
	require.Equal(t, transient.GasUsed, committed.GasUsed)
	require.Equal(t, transient.Status, committed.Status)
	require.Equal(t, transient.TransactionIndex, committed.TransactionIndex)
	require.Equal(t, transient.EffectiveGasPrice, committed.EffectiveGasPrice)
	require.Equal(t, uint64(acc.Result.GasUsed), committed.GasUsed)
}
