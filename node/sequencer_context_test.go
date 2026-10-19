package app_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	rpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const (
	seqCtxChainID     = "pax-test"
	seqCtxGasPrice    = 500_000_000_000
	seqCtxTransferGas = 21000
	seqCtxFundingUhpx = 1_000_000_000_000
	seqCtxWeiPerUhpx  = 1_000_000_000_000
	seqCtxCommitted   = int64(1)
	seqCtxOpen        = int64(2)
)

// newSequencerContextApp builds the real App as node/app_test.go builds it, funds each
// account's cosmos address (mapping its EVM address when mapped is set), commits height 1 and
// returns the app with a Sequential executor over the committed state.
func newSequencerContextApp(t *testing.T, mapped bool, funded ...utils.TestAcct) (*app.App, sequencer.Executor, time.Time) {
	t.Helper()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := app.NewTestWrapper(t, tm, valPub, false)
	a := tw.App
	for _, acct := range funded {
		if mapped {
			a.EvmKeeper.SetAddressMapping(tw.Ctx, acct.AccountAddress, acct.EvmAddress)
		}
		tw.FundAcc(acct.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(seqCtxFundingUhpx))))
	}
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: seqCtxChainID, Height: seqCtxCommitted},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, seqCtxCommitted, a.LastBlockHeight())

	exec, err := sequencer.NewExecutor(a, sequencer.Options{
		ChainID:         seqCtxChainID,
		ProposerAddress: valPub.Address().Bytes(),
	})
	require.NoError(t, err)
	return a, exec, tm
}

func seqCtxTransfer(t *testing.T, from utils.TestAcct, nonce uint64, to common.Address, value *big.Int) []byte {
	t.Helper()
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(seqCtxGasPrice),
		Gas:      seqCtxTransferGas,
		To:       &to,
		Value:    value,
	}), ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), from.EvmPrivateKey)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(seqCtxTransferGas)
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz
}

func seqCtxWei(a *app.App, ctx sdk.Context, addr common.Address) *big.Int {
	return a.EvmKeeper.GetBalance(ctx, a.EvmKeeper.GetPaxAddressOrDefault(ctx, addr))
}

func requireSeqCtxWei(t *testing.T, want, got *big.Int) {
	t.Helper()
	require.Zerof(t, want.Cmp(got), "want %s wei, got %s", want, got)
}

func TestSequencerContextPendingSnapshot(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	a, exec, genesis := newSequencerContextApp(t, true, sender)
	value := big.NewInt(1_000_000_000_000_000_000)
	funded := new(big.Int).Mul(big.NewInt(seqCtxFundingUhpx), big.NewInt(seqCtxWeiPerUhpx))
	fee := new(big.Int).Mul(big.NewInt(seqCtxTransferGas), big.NewInt(seqCtxGasPrice))

	// Full-node mode: pending is refused and the latest getter is unchanged.
	_, err := a.PendingSnapshot()
	require.ErrorIs(t, err, app.ErrPendingServedBySequencer)
	require.PanicsWithError(t, app.ErrPendingServedBySequencer.Error(), func() {
		a.RPCContextProvider(app.PendingCtxHeight)
	})
	latest := a.RPCContextProvider(rpc.LatestCtxHeight)
	require.Equal(t, seqCtxCommitted, latest.BlockHeight())
	require.Equal(t, a.GetCheckCtx().MultiStore(), latest.MultiStore())
	require.Equal(t, rpc.LatestCtxHeight-1, app.PendingCtxHeight)

	// Sequencer mode with no open block: the executor's refusal is returned as is.
	a.SetOpenBlock(exec)
	_, err = a.PendingSnapshot()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)

	require.NoError(t, exec.Open(seqCtxOpen, genesis.Add(time.Second).UnixMilli()))
	first, err := exec.Accept(seqCtxTransfer(t, sender, 0, recipient, value))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, first.Result.Code)

	afterFirst := new(big.Int).Sub(new(big.Int).Sub(funded, value), fee)
	snap := a.RPCContextProvider(app.PendingCtxHeight)
	require.Equal(t, seqCtxOpen, snap.BlockHeight())
	require.True(t, snap.IsEVM())
	require.True(t, snap.IsTracing())
	require.False(t, snap.IsCheckTx())
	require.Equal(t, app.LatestUpgrade, snap.ClosestUpgradeName())
	require.NotNil(t, snap.ConsensusParams())
	require.Equal(t, uint64(1), a.EvmKeeper.GetNonce(snap, sender.EvmAddress))
	requireSeqCtxWei(t, afterFirst, seqCtxWei(a, snap, sender.EvmAddress))
	requireSeqCtxWei(t, value, seqCtxWei(a, snap, recipient))

	// The committed reads are untouched by the open block.
	latest = a.RPCContextProvider(rpc.LatestCtxHeight)
	require.Equal(t, uint64(0), a.EvmKeeper.GetNonce(latest, sender.EvmAddress))
	requireSeqCtxWei(t, funded, seqCtxWei(a, latest, sender.EvmAddress))
	require.Equal(t, 0, seqCtxWei(a, latest, recipient).Sign())

	// Writes to the snapshot never reach L_open.
	a.EvmKeeper.SetNonce(snap, sender.EvmAddress, 99)
	snap.EventManager().EmitEvent(sdk.NewEvent("snapshot-write"))
	require.Equal(t, uint64(99), a.EvmKeeper.GetNonce(snap, sender.EvmAddress))
	fresh, err := a.PendingSnapshot()
	require.NoError(t, err)
	require.Equal(t, uint64(1), a.EvmKeeper.GetNonce(fresh, sender.EvmAddress))
	require.Empty(t, fresh.EventManager().Events())

	// A held snapshot does not block Accept: the lock is released once the branch is taken.
	nextTx := seqCtxTransfer(t, sender, 1, recipient, value)
	type acceptResult struct {
		res *sequencer.Accepted
		err error
	}
	accepted := make(chan acceptResult, 1)
	go func() {
		res, acceptErr := exec.Accept(nextTx)
		accepted <- acceptResult{res: res, err: acceptErr}
	}()
	require.Equal(t, uint64(99), a.EvmKeeper.GetNonce(snap, sender.EvmAddress))
	select {
	case got := <-accepted:
		require.NoError(t, got.err)
		require.Equal(t, abci.CodeTypeOK, got.res.Result.Code)
		require.Equal(t, first.Seq+1, got.res.Seq)
	case <-time.After(30 * time.Second):
		t.Fatal("Accept blocked while a pending snapshot was held")
	}
	second := a.RPCContextProvider(app.PendingCtxHeight)
	require.Equal(t, uint64(2), a.EvmKeeper.GetNonce(second, sender.EvmAddress))
	requireSeqCtxWei(t, new(big.Int).Mul(value, big.NewInt(2)), seqCtxWei(a, second, recipient))
	requireSeqCtxWei(t, new(big.Int).Sub(afterFirst, new(big.Int).Add(value, fee)), seqCtxWei(a, second, sender.EvmAddress))
}

func TestSequencerContextAnteReadsOpenBlock(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	a, exec, genesis := newSequencerContextApp(t, false, sender)
	value := big.NewInt(1_000_000_000_000_000_000)

	// Full-node mode keeps the check context as the ante read context.
	check := a.GetCheckCtx()
	ctx := a.LatestCtx()
	require.Equal(t, check.MultiStore(), ctx.MultiStore())
	require.Equal(t, check.BlockHeight(), ctx.BlockHeight())
	require.True(t, ctx.IsCheckTx())
	_, associated := a.EvmKeeper.GetPaxAddress(ctx, sender.EvmAddress)
	require.False(t, associated)

	// Sequencer mode between blocks reads the check context as well.
	a.SetOpenBlock(exec)
	require.Equal(t, check.MultiStore(), a.LatestCtx().MultiStore())

	require.NoError(t, exec.Open(seqCtxOpen, genesis.Add(time.Second).UnixMilli()))
	first, err := exec.Accept(seqCtxTransfer(t, sender, 0, recipient, value))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, first.Result.Code)

	// The ante read context is L_open: it sees the association and nonce written by the
	// receipted transaction, which the check context does not.
	ctx = a.LatestCtx()
	require.Equal(t, seqCtxOpen, ctx.BlockHeight())
	require.False(t, ctx.IsCheckTx())
	paxAddr, associated := a.EvmKeeper.GetPaxAddress(ctx, sender.EvmAddress)
	require.True(t, associated)
	require.Equal(t, sender.AccountAddress, paxAddr)
	require.Equal(t, uint64(1), a.EvmKeeper.GetNonce(ctx, sender.EvmAddress))

	check = a.GetCheckCtx()
	_, associated = a.EvmKeeper.GetPaxAddress(check, sender.EvmAddress)
	require.False(t, associated)
	require.Equal(t, uint64(0), a.EvmKeeper.GetNonce(check, sender.EvmAddress))

	// Reads through the ante context never write L_open.
	a.EvmKeeper.SetNonce(ctx, sender.EvmAddress, 7)
	require.Equal(t, uint64(1), a.EvmKeeper.GetNonce(a.LatestCtx(), sender.EvmAddress))

	// The signature and nonce checks of the next transaction run against L_open.
	second, err := exec.Accept(seqCtxTransfer(t, sender, 1, recipient, value))
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, second.Result.Code)
	require.Equal(t, first.Seq+1, second.Seq)
	ctx = a.LatestCtx()
	require.Equal(t, uint64(2), a.EvmKeeper.GetNonce(ctx, sender.EvmAddress))
	requireSeqCtxWei(t, new(big.Int).Mul(value, big.NewInt(2)), seqCtxWei(a, ctx, recipient))
}
