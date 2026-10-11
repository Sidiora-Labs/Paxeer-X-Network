package sequencer_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"strconv"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	epochtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/epoch/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	clienttx "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client/tx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/tx/signing"
	authsigning "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/signing"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
)

const (
	testChainID  = "pax-test"
	gasPriceWei  = 500_000_000_000
	transferGas  = 21000
	startSeq     = uint64(41)
	fundingUhpx  = 1_000_000_000_000
	cosmosFee    = 30_000
	weiPerUhpx   = 1_000_000_000_000
	onePaxInWei  = 1_000_000_000_000_000_000
	openHeight   = int64(2)
	committedTip = int64(1)
)

var startSeqHash = sha256.Sum256([]byte("executor test previous seq hash"))

type harness struct {
	t        *testing.T
	app      *app.App
	exec     sequencer.Executor
	proposer []byte
	genesis  time.Time
}

// newHarness builds the real App as node/app_test.go builds it, funds the
// given accounts (EVM-mapped), commits height 1 and builds a Sequential
// executor over the committed state continuing the chain at startSeq.
func newHarness(t *testing.T, funded ...utils.TestAcct) *harness {
	t.Helper()
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := app.NewTestWrapper(t, tm, valPub, false)
	a := tw.App
	for _, acct := range funded {
		a.EvmKeeper.SetAddressMapping(tw.Ctx, acct.AccountAddress, acct.EvmAddress)
		tw.FundAcc(acct.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(fundingUhpx))))
	}
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: testChainID, Height: committedTip},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	require.Equal(t, committedTip, a.LastBlockHeight())

	proposer := valPub.Address().Bytes()
	exec, err := sequencer.NewExecutor(a, sequencer.Options{
		ChainID:         testChainID,
		ProposerAddress: proposer,
		LastSeq:         startSeq,
		LastSeqHash:     startSeqHash,
	})
	require.NoError(t, err)
	require.Equal(t, sequencer.ModeSequential, exec.Mode())
	return &harness{t: t, app: a, exec: exec, proposer: proposer, genesis: tm}
}

func (h *harness) openAt(ts time.Time) int64 {
	h.t.Helper()
	ms := ts.UnixMilli()
	require.NoError(h.t, h.exec.Open(openHeight, ms))
	return ms
}

func (h *harness) open() int64 {
	return h.openAt(h.genesis.Add(time.Second))
}

func (h *harness) openCtx() sdk.Context {
	h.t.Helper()
	ctx, err := h.exec.Snapshot()
	require.NoError(h.t, err)
	return ctx
}

func (h *harness) committedCtx() sdk.Context {
	return sdk.NewContext(h.app.CommitMultiStore().CacheMultiStore(), tmproto.Header{ChainID: testChainID, Height: committedTip}, false)
}

func (h *harness) weiBalance(ctx sdk.Context, addr common.Address) *big.Int {
	return h.app.EvmKeeper.GetBalance(ctx, h.app.EvmKeeper.GetPaxAddressOrDefault(ctx, addr))
}

func signEVM(t *testing.T, from utils.TestAcct, chainID int64, nonce uint64, to common.Address, value *big.Int) (*ethtypes.Transaction, []byte) {
	t.Helper()
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(gasPriceWei),
		Gas:      transferGas,
		To:       &to,
		Value:    value,
	}), ethtypes.LatestSignerForChainID(big.NewInt(chainID)), from.EvmPrivateKey)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(transferGas)
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return signed, bz
}

func transfer(t *testing.T, from utils.TestAcct, nonce uint64, to common.Address, value *big.Int) (*ethtypes.Transaction, []byte) {
	return signEVM(t, from, config.DefaultChainID, nonce, to, value)
}

func signCosmos(t *testing.T, ctx sdk.Context, h *harness, from utils.TestAcct, gas uint64, msg sdk.Msg) []byte {
	t.Helper()
	acct := h.app.AccountKeeper.GetAccount(ctx, from.AccountAddress)
	require.NotNil(t, acct)
	tc := app.MakeEncodingConfig().TxConfig
	builder := tc.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(gas)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(cosmosFee))))
	mode := tc.SignModeHandler().DefaultMode()
	require.NoError(t, builder.SetSignatures(signing.SignatureV2{
		PubKey:   from.PrivateKey.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: mode},
		Sequence: acct.GetSequence(),
	}))
	sig, err := clienttx.SignWithPrivKey(mode, authsigning.SignerData{
		ChainID:       testChainID,
		AccountNumber: acct.GetAccountNumber(),
		Sequence:      acct.GetSequence(),
	}, builder, from.PrivateKey, tc, acct.GetSequence())
	require.NoError(t, err)
	require.NoError(t, builder.SetSignatures(sig))
	bz, err := tc.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz
}

func expectedSeqHash(prev [32]byte, seq uint64, activity [32]byte) [32]byte {
	buf := append([]byte("PAXEER/v1/sequence\x00"), prev[:]...)
	buf = binary.BigEndian.AppendUint64(buf, seq)
	buf = append(buf, activity[:]...)
	return sha256.Sum256(buf)
}

func requireWei(t *testing.T, want, got *big.Int) {
	t.Helper()
	require.Zerof(t, want.Cmp(got), "want %s wei, got %s", want, got)
}

func requireRefused(t *testing.T, err error, want *sdkerrors.Error) {
	t.Helper()
	require.ErrorIs(t, err, want)
	codespace, code, _ := sdkerrors.ABCIInfo(err, false)
	require.Equal(t, want.Codespace(), codespace)
	require.Equal(t, want.ABCICode(), code)
}

// deliverOnBranch runs the block path's DeliverTx for txBytes on a throwaway
// branch of the open layer and returns the response and the branch context.
func (h *harness) deliverOnBranch(txBytes []byte, txIndex int) (abci.ResponseDeliverTx, sdk.Context) {
	h.t.Helper()
	ctx := h.openCtx().WithTxIndex(txIndex)
	tx, err := h.app.GetTxConfig().TxDecoder()(txBytes)
	if err != nil {
		tx = nil
	}
	sum := sha256.Sum256(txBytes)
	return h.app.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: txBytes}, tx, sum), ctx
}

func TestExecutorAcceptWritesBranchOnSuccess(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	ts := h.open()

	before := h.weiBalance(h.openCtx(), sender.EvmAddress)
	requireWei(t, new(big.Int).Mul(big.NewInt(fundingUhpx), big.NewInt(weiPerUhpx)), before)
	value := big.NewInt(onePaxInWei)

	signed, bz := transfer(t, sender, 0, recipient, value)
	first, err := h.exec.Accept(bz)
	require.NoError(t, err)
	activity := signed.Hash()
	require.Equal(t, startSeq+1, first.Seq)
	require.Equal(t, startSeqHash, first.PrevSeqHash)
	require.Equal(t, [32]byte(activity), first.ActivityID)
	require.Equal(t, expectedSeqHash(startSeqHash, startSeq+1, activity), first.SeqHash)
	require.Equal(t, openHeight, first.Height)
	require.Equal(t, 1, first.TxIndex)
	require.Equal(t, ts, first.TimestampMs)
	require.Equal(t, abci.CodeTypeOK, first.Result.Code)
	require.NotNil(t, first.Result.EvmTxInfo)
	require.Equal(t, activity.Hex(), first.Result.EvmTxInfo.TxHash)
	require.Equal(t, uint64(0), first.Result.EvmTxInfo.Nonce)
	require.Equal(t, sender.EvmAddress.Hex(), first.Result.EvmTxInfo.SenderAddress)

	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	afterFirst := new(big.Int).Sub(new(big.Int).Sub(before, value), fee)
	ctx := h.openCtx()
	require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(ctx, sender.EvmAddress))
	requireWei(t, afterFirst, h.weiBalance(ctx, sender.EvmAddress))
	requireWei(t, value, h.weiBalance(ctx, recipient))

	// A refused transaction leaves no trace on the open layer and takes no seq.
	_, err = h.exec.Accept(bz)
	requireRefused(t, err, sdkerrors.ErrWrongSequence)
	ctx = h.openCtx()
	require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(ctx, sender.EvmAddress))
	requireWei(t, afterFirst, h.weiBalance(ctx, sender.EvmAddress))
	requireWei(t, value, h.weiBalance(ctx, recipient))

	signed2, bz2 := transfer(t, sender, 1, recipient, value)
	second, err := h.exec.Accept(bz2)
	require.NoError(t, err)
	require.Equal(t, startSeq+2, second.Seq)
	require.Equal(t, first.SeqHash, second.PrevSeqHash)
	require.Equal(t, expectedSeqHash(first.SeqHash, startSeq+2, signed2.Hash()), second.SeqHash)
	require.Equal(t, 2, second.TxIndex)
	requireWei(t, new(big.Int).Mul(value, big.NewInt(2)), h.weiBalance(h.openCtx(), recipient))

	// Execution never reaches the committed state.
	committed := h.committedCtx()
	require.Equal(t, uint64(0), h.app.EvmKeeper.GetNonce(committed, sender.EvmAddress))
	requireWei(t, before, h.weiBalance(committed, sender.EvmAddress))
	require.Equal(t, 0, h.weiBalance(committed, recipient).Sign())

	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, openHeight, batch.Height)
	require.Equal(t, ts, batch.TimestampMs)
	require.Equal(t, [][]byte{bz, bz2}, batch.Txs)
	require.Equal(t, []*abci.ExecTxResult{first.Result, second.Result}, batch.Results)
	require.Len(t, batch.EVMMsgs, 2)
	require.NotNil(t, batch.EVMMsgs[0])
	require.NotNil(t, batch.EVMMsgs[1])
	require.Equal(t, startSeq+1, batch.FromSeq)
	require.Equal(t, startSeq+2, batch.LastSeq)
	require.Equal(t, startSeqHash, batch.PrevSeqHash)
	require.Equal(t, second.SeqHash, batch.LastSeqHash)
	require.Equal(t, uint64(2), h.app.EvmKeeper.GetNonce(batch.Ctx, sender.EvmAddress))

	_, err = h.exec.Accept(bz2)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	_, err = h.exec.Snapshot()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	_, err = h.exec.Seal(openHeight)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
}

func TestExecutorBeginBlockOncePerOpen(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)

	before := h.app.EpochKeeper.GetEpoch(h.committedCtx())
	require.Greater(t, int64(before.EpochDuration), int64(0))
	openTime := before.CurrentEpochStartTime.Add(10 * before.EpochDuration)
	if openTime.Before(h.genesis) {
		openTime = h.genesis.Add(time.Second)
	}
	// The open time is past several epoch boundaries, so every extra BeginBlock
	// would advance the epoch again.
	require.Greater(t, int64(openTime.Sub(before.CurrentEpochStartTime)), int64(2*before.EpochDuration))

	require.ErrorIs(t, h.exec.Open(openHeight+1, openTime.UnixMilli()), sequencer.ErrOpenHeight)
	require.ErrorIs(t, h.exec.Open(committedTip, openTime.UnixMilli()), sequencer.ErrOpenHeight)
	require.ErrorIs(t, h.exec.Open(openHeight, 0), sequencer.ErrOpenTimestamp)
	require.ErrorIs(t, h.exec.Open(openHeight, -1), sequencer.ErrOpenTimestamp)
	_, err := h.exec.Snapshot()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)

	ts := h.openAt(openTime)
	require.ErrorIs(t, h.exec.Open(openHeight, ts), sequencer.ErrLayerOpen)

	wantEpoch := epochtypes.Epoch{
		GenesisTime:           before.GenesisTime,
		EpochDuration:         before.EpochDuration,
		CurrentEpoch:          before.CurrentEpoch + 1,
		CurrentEpochStartTime: before.CurrentEpochStartTime.Add(before.EpochDuration),
		CurrentEpochHeight:    openHeight,
	}
	requireEpoch := func(ctx sdk.Context) {
		got := h.app.EpochKeeper.GetEpoch(ctx)
		require.Equal(t, wantEpoch.CurrentEpoch, got.CurrentEpoch)
		require.Equal(t, wantEpoch.CurrentEpochHeight, got.CurrentEpochHeight)
		require.Equal(t, wantEpoch.EpochDuration, got.EpochDuration)
		require.True(t, wantEpoch.CurrentEpochStartTime.Equal(got.CurrentEpochStartTime))
		require.True(t, wantEpoch.GenesisTime.Equal(got.GenesisTime))
	}
	requireEpoch(h.openCtx())

	for nonce := uint64(0); nonce < 3; nonce++ {
		_, bz := transfer(t, sender, nonce, recipient, big.NewInt(1))
		acc, err := h.exec.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, openHeight, acc.Height)
		require.Equal(t, ts, acc.TimestampMs)
		requireEpoch(h.openCtx())
	}

	// The header the transactions ran under is the open block's, with the
	// open time in place of a wall-clock time.
	ctx := h.openCtx()
	require.Equal(t, openHeight, ctx.BlockHeight())
	require.True(t, time.UnixMilli(ts).UTC().Equal(ctx.BlockTime()))
	require.Equal(t, h.proposer, ctx.BlockHeader().ProposerAddress)

	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	requireEpoch(batch.Ctx)
	newEpochEvents := 0
	for _, ev := range batch.BeginBlockEvents {
		if ev.Type != epochtypes.EventTypeNewEpoch {
			continue
		}
		newEpochEvents++
		for _, attr := range ev.Attributes {
			if string(attr.Key) == epochtypes.AttributeEpochNumber {
				require.Equal(t, strconv.FormatUint(wantEpoch.CurrentEpoch, 10), string(attr.Value))
			}
		}
	}
	require.Equal(t, 1, newEpochEvents)

	committed := h.app.EpochKeeper.GetEpoch(h.committedCtx())
	require.Equal(t, before.CurrentEpoch, committed.CurrentEpoch)
}

func TestExecutorRunsDeliverModeAnte(t *testing.T) {
	sender := utils.NewSigner()
	unfunded := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	h.open()

	ctx := h.openCtx()
	require.False(t, ctx.IsCheckTx())
	require.False(t, ctx.IsReCheckTx())

	// Deliver mode requires the exact next nonce; check mode would hold a
	// future nonce as pending.
	_, gap := transfer(t, sender, 1, recipient, big.NewInt(1))
	_, err := h.exec.Accept(gap)
	requireRefused(t, err, sdkerrors.ErrWrongSequence)
	require.Equal(t, uint64(0), h.app.EvmKeeper.GetNonce(h.openCtx(), sender.EvmAddress))

	// The deliver path bumps the nonce of a correct-nonce transaction whose
	// fee charge fails (deliver-only callback); Accept drops that branch.
	_, broke := transfer(t, unfunded, 0, recipient, big.NewInt(1))
	res, branchCtx := h.deliverOnBranch(broke, 1)
	require.Equal(t, sdkerrors.ErrInsufficientFunds.ABCICode(), res.Code)
	require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(branchCtx, unfunded.EvmAddress))
	_, err = h.exec.Accept(broke)
	requireRefused(t, err, sdkerrors.ErrInsufficientFunds)
	require.Equal(t, uint64(0), h.app.EvmKeeper.GetNonce(h.openCtx(), unfunded.EvmAddress))

	// The exact next nonce passes and the fee is charged at the gas price.
	before := h.weiBalance(h.openCtx(), sender.EvmAddress)
	_, ok := transfer(t, sender, 0, recipient, big.NewInt(0))
	acc, err := h.exec.Accept(ok)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	requireWei(t, new(big.Int).Sub(before, fee), h.weiBalance(h.openCtx(), sender.EvmAddress))
	require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(h.openCtx(), sender.EvmAddress))

	// The same future-nonce transaction is now the exact next one.
	acc, err = h.exec.Accept(gap)
	require.NoError(t, err)
	require.Equal(t, startSeq+2, acc.Seq)
}

func TestExecutorRecoversExecutionPanic(t *testing.T) {
	payer := utils.NewSigner()
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, payer, sender)
	h.open()

	// A multi-send to many fresh accounts fits the ante gas but runs out of
	// gas inside message execution, which panics in the gas meter.
	const outputs = 200
	outs := make([]banktypes.Output, outputs)
	for i := range outs {
		outs[i] = banktypes.NewOutput(utils.NewSigner().AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(1))))
	}
	multi := banktypes.NewMsgMultiSend(
		[]banktypes.Input{banktypes.NewInput(payer.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(outputs))))},
		outs,
	)
	ctx := h.openCtx()
	balanceBefore := h.app.BankKeeper.GetBalance(ctx, payer.AccountAddress, "uhpx")
	seqBefore := h.app.AccountKeeper.GetAccount(ctx, payer.AccountAddress).GetSequence()
	bz := signCosmos(t, ctx, h, payer, 300_000, multi)

	// The block path recovers the panic and keeps the ante writes (fee and
	// account sequence) in its branch.
	res, branchCtx := h.deliverOnBranch(bz, 1)
	require.Equal(t, sdkerrors.ErrOutOfGas.ABCICode(), res.Code)
	require.Equal(t, balanceBefore.Amount.SubRaw(cosmosFee).String(), h.app.BankKeeper.GetBalance(branchCtx, payer.AccountAddress, "uhpx").Amount.String())
	require.Equal(t, seqBefore+1, h.app.AccountKeeper.GetAccount(branchCtx, payer.AccountAddress).GetSequence())

	acc, err := h.exec.Accept(bz)
	require.Nil(t, acc)
	requireRefused(t, err, sdkerrors.ErrOutOfGas)

	ctx = h.openCtx()
	require.Equal(t, balanceBefore.String(), h.app.BankKeeper.GetBalance(ctx, payer.AccountAddress, "uhpx").String())
	require.Equal(t, seqBefore, h.app.AccountKeeper.GetAccount(ctx, payer.AccountAddress).GetSequence())
	require.Nil(t, h.app.AccountKeeper.GetAccount(ctx, sdk.MustAccAddressFromBech32(outs[0].Address)))

	// The seq counter did not move: the next transaction takes startSeq+1 at index 1.
	signed, next := transfer(t, sender, 0, recipient, big.NewInt(1))
	acc, err = h.exec.Accept(next)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, 1, acc.TxIndex)
	require.Equal(t, startSeqHash, acc.PrevSeqHash)
	require.Equal(t, expectedSeqHash(startSeqHash, startSeq+1, signed.Hash()), acc.SeqHash)

	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, [][]byte{next}, batch.Txs)
}

func TestExecutorContextFromOpenBlock(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	ts := h.open()

	ctx := h.openCtx()
	require.Equal(t, testChainID, ctx.ChainID())
	require.Equal(t, openHeight, ctx.BlockHeight())
	require.True(t, time.UnixMilli(ts).UTC().Equal(ctx.BlockTime()))
	require.Equal(t, h.proposer, ctx.BlockHeader().ProposerAddress)
	require.False(t, ctx.IsCheckTx())
	require.Equal(t, h.app.GetConsensusParams(ctx), ctx.ConsensusParams())

	checkBefore := h.weiBalance(h.app.GetCheckCtx(), sender.EvmAddress)
	value := big.NewInt(onePaxInWei)
	for nonce := uint64(0); nonce < 2; nonce++ {
		_, bz := transfer(t, sender, nonce, recipient, value)
		acc, err := h.exec.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, startSeq+1+nonce, acc.Seq)
	}

	// The open layer carries both transactions; the check state is untouched.
	ctx = h.openCtx()
	require.Equal(t, uint64(2), h.app.EvmKeeper.GetNonce(ctx, sender.EvmAddress))
	requireWei(t, new(big.Int).Mul(value, big.NewInt(2)), h.weiBalance(ctx, recipient))
	checkCtx := h.app.GetCheckCtx()
	require.Equal(t, uint64(0), h.app.EvmKeeper.GetNonce(checkCtx, sender.EvmAddress))
	requireWei(t, checkBefore, h.weiBalance(checkCtx, sender.EvmAddress))
	require.Equal(t, 0, h.weiBalance(checkCtx, recipient).Sign())

	// A read branch of the open layer never leaks back into it.
	branch := h.openCtx()
	h.app.EvmKeeper.SetNonce(branch, sender.EvmAddress, 99)
	require.Equal(t, uint64(2), h.app.EvmKeeper.GetNonce(h.openCtx(), sender.EvmAddress))
}

func TestExecutorSequentialOrder(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	h.open()

	const count = 50
	prev := startSeqHash
	want := make([][]byte, 0, count)
	total := new(big.Int)
	for i := 0; i < count; i++ {
		nonce := uint64(i)
		value := big.NewInt(int64(i + 1))
		signed, bz := transfer(t, sender, nonce, recipient, value)
		if i == 10 {
			// A transaction ahead of its turn is refused and does not disturb the order.
			_, ahead := transfer(t, sender, nonce+1, recipient, value)
			_, err := h.exec.Accept(ahead)
			requireRefused(t, err, sdkerrors.ErrWrongSequence)
		}
		acc, err := h.exec.Accept(bz)
		require.NoError(t, err)
		require.Equal(t, startSeq+1+nonce, acc.Seq)
		require.Equal(t, i+1, acc.TxIndex)
		require.Equal(t, prev, acc.PrevSeqHash)
		require.Equal(t, expectedSeqHash(prev, acc.Seq, signed.Hash()), acc.SeqHash)
		require.Equal(t, nonce, acc.Result.EvmTxInfo.Nonce)
		prev = acc.SeqHash
		want = append(want, bz)
		total.Add(total, value)
		requireWei(t, total, h.weiBalance(h.openCtx(), recipient))
	}
	require.Equal(t, uint64(count), h.app.EvmKeeper.GetNonce(h.openCtx(), sender.EvmAddress))

	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, want, batch.Txs)
	require.Len(t, batch.Results, count)
	require.Equal(t, startSeq+1, batch.FromSeq)
	require.Equal(t, startSeq+count, batch.LastSeq)
	require.Equal(t, prev, batch.LastSeqHash)
	require.NoError(t, h.exec.Open(openHeight, h.genesis.Add(2*time.Second).UnixMilli()))
	_, err = h.exec.Seal(openHeight + 1)
	require.ErrorIs(t, err, sequencer.ErrSealHeight)
}

func TestExecutorAdmissionIsAnteChain(t *testing.T) {
	sender := utils.NewSigner()
	unfunded := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	h.open()

	_, wrongChain := signEVM(t, sender, config.DefaultChainID+1, 0, recipient, big.NewInt(1))
	_, gap := transfer(t, sender, 5, recipient, big.NewInt(1))
	_, broke := transfer(t, unfunded, 0, recipient, big.NewInt(1))
	garbage := []byte{0x0a, 0xff, 0xff, 0x01}

	cases := []struct {
		name string
		tx   []byte
		want *sdkerrors.Error
	}{
		{"wrong chain id", wrongChain, sdkerrors.ErrInvalidChainID},
		{"nonce gap", gap, sdkerrors.ErrWrongSequence},
		{"unfunded sender", broke, sdkerrors.ErrInsufficientFunds},
		{"undecodable bytes", garbage, sdkerrors.ErrTxDecode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The refusal is the deliver path's own answer for the same bytes.
			res, _ := h.deliverOnBranch(tc.tx, 1)
			require.Equal(t, tc.want.Codespace(), res.Codespace)
			require.Equal(t, tc.want.ABCICode(), res.Code)
			acc, err := h.exec.Accept(tc.tx)
			require.Nil(t, acc)
			requireRefused(t, err, tc.want)
		})
	}

	_, empty := h.exec.Accept(nil)
	requireRefused(t, empty, sdkerrors.ErrTxDecode)

	// Nothing refused took a seq.
	_, ok := transfer(t, sender, 0, recipient, big.NewInt(1))
	acc, err := h.exec.Accept(ok)
	require.NoError(t, err)
	require.Equal(t, startSeq+1, acc.Seq)
	require.Equal(t, 1, acc.TxIndex)

	_, err = sequencer.NewExecutor(h.app, sequencer.Options{Mode: sequencer.Mode(7), ChainID: testChainID, ProposerAddress: h.proposer})
	require.EqualError(t, err, "sequencer: unknown executor mode 7")
}

func TestExecutorSnapshotIsPointInTime(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	h.open()

	funded := new(big.Int).Mul(big.NewInt(fundingUhpx), big.NewInt(weiPerUhpx))
	value := big.NewInt(onePaxInWei)
	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	afterFirst := new(big.Int).Sub(funded, new(big.Int).Add(value, fee))
	afterSecond := new(big.Int).Sub(afterFirst, new(big.Int).Add(value, fee))

	_, first := transfer(t, sender, 0, recipient, value)
	_, err := h.exec.Accept(first)
	require.NoError(t, err)
	snap := h.openCtx()
	// A second snapshot with no Accept in between reads the same state.
	again := h.openCtx()

	_, second := transfer(t, sender, 1, recipient, value)
	_, err = h.exec.Accept(second)
	require.NoError(t, err)

	for _, ctx := range []sdk.Context{snap, again} {
		requireWei(t, afterFirst, h.weiBalance(ctx, sender.EvmAddress))
		requireWei(t, value, h.weiBalance(ctx, recipient))
		require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(ctx, sender.EvmAddress))
		require.Equal(t, openHeight, ctx.BlockHeight())
	}
	open := h.openCtx()
	requireWei(t, afterSecond, h.weiBalance(open, sender.EvmAddress))
	requireWei(t, new(big.Int).Mul(value, big.NewInt(2)), h.weiBalance(open, recipient))
	require.Equal(t, uint64(2), h.app.EvmKeeper.GetNonce(open, sender.EvmAddress))

	// A write to a snapshot reaches neither the open layer nor later snapshots.
	h.app.EvmKeeper.SetNonce(snap, sender.EvmAddress, 99)
	require.Equal(t, uint64(1), h.app.EvmKeeper.GetNonce(again, sender.EvmAddress))
	require.Equal(t, uint64(2), h.app.EvmKeeper.GetNonce(h.openCtx(), sender.EvmAddress))

	// Seal flattens every branch into L_open in order.
	batch, err := h.exec.Seal(openHeight)
	require.NoError(t, err)
	require.Equal(t, [][]byte{first, second}, batch.Txs)
	requireWei(t, afterSecond, h.weiBalance(batch.Ctx, sender.EvmAddress))
	requireWei(t, new(big.Int).Mul(value, big.NewInt(2)), h.weiBalance(batch.Ctx, recipient))
	require.Equal(t, uint64(2), h.app.EvmKeeper.GetNonce(batch.Ctx, sender.EvmAddress))
	_, err = h.exec.Snapshot()
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	requireWei(t, funded, h.weiBalance(h.committedCtx(), sender.EvmAddress))
}

func TestExecutorSnapshotDoesNotBlockAccept(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	h := newHarness(t, sender)
	h.open()

	funded := new(big.Int).Mul(big.NewInt(fundingUhpx), big.NewInt(weiPerUhpx))
	value := big.NewInt(onePaxInWei)
	fee := new(big.Int).Mul(big.NewInt(transferGas), big.NewInt(gasPriceWei))
	const accepts = 5
	txs := make([][]byte, accepts)
	for nonce := range txs {
		_, txs[nonce] = transfer(t, sender, uint64(nonce), recipient, value)
	}

	snap := h.openCtx()
	stop := make(chan struct{})
	type reads struct {
		count int
		bad   *big.Int
	}
	readerDone := make(chan reads, 1)
	go func() {
		// The reader holds and reads its snapshot for as long as Accepts run.
		var r reads
		for {
			select {
			case <-stop:
				readerDone <- r
				return
			default:
			}
			if got := h.weiBalance(snap, sender.EvmAddress); got.Cmp(funded) != 0 {
				r.bad = got
			}
			r.count++
		}
	}()

	acceptDone := make(chan error, 1)
	go func() {
		for _, bz := range txs {
			if _, err := h.exec.Accept(bz); err != nil {
				acceptDone <- err
				return
			}
		}
		acceptDone <- nil
	}()
	select {
	case err := <-acceptDone:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		require.FailNow(t, "Accept blocked while a snapshot was held")
	}
	close(stop)
	r := <-readerDone
	require.Nil(t, r.bad)
	require.Positive(t, r.count)

	requireWei(t, funded, h.weiBalance(snap, sender.EvmAddress))
	require.Equal(t, 0, h.weiBalance(snap, recipient).Sign())
	spent := new(big.Int).Mul(new(big.Int).Add(value, fee), big.NewInt(accepts))
	open := h.openCtx()
	requireWei(t, new(big.Int).Sub(funded, spent), h.weiBalance(open, sender.EvmAddress))
	requireWei(t, new(big.Int).Mul(value, big.NewInt(accepts)), h.weiBalance(open, recipient))
	require.Equal(t, uint64(accepts), h.app.EvmKeeper.GetNonce(open, sender.EvmAddress))
}
