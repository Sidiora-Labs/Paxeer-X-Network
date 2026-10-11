package evmrpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

const (
	pendingChainID     = "pax-test"
	pendingGasPriceWei = 500_000_000_000
	pendingTransferGas = 21000
	pendingFundingUhpx = 1_000_000_000_000
	pendingWeiPerUhpx  = 1_000_000_000_000
	pendingOnePaxWei   = 1_000_000_000_000_000_000
)

// pendingSequencer is a real App committed to a height with a Sequential
// executor that has the next height open, and a WatermarkManager wired as the
// sequencer wires it.
type pendingSequencer struct {
	app       *app.App
	exec      sequencer.Executor
	committed int64
	wm        *evmrpc.WatermarkManager
}

func newPendingSequencer(t *testing.T, committed int64, funded ...utils.TestAcct) *pendingSequencer {
	t.Helper()
	require.GreaterOrEqual(t, committed, int64(1))
	genesis := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	tw := app.NewTestWrapper(t, genesis, valPub, false)
	a := tw.App
	for _, acct := range funded {
		a.EvmKeeper.SetAddressMapping(tw.Ctx, acct.AccountAddress, acct.EvmAddress)
		tw.FundAcc(acct.AccountAddress, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(pendingFundingUhpx))))
	}
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: pendingChainID, Height: 1},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	for height := int64(2); height <= committed; height++ {
		require.Equal(t, height, a.CommitMultiStore().Commit(true).Version)
	}
	require.Equal(t, committed, a.LastBlockHeight())

	exec, err := sequencer.NewExecutor(a, sequencer.Options{
		ChainID:         pendingChainID,
		ProposerAddress: valPub.Address().Bytes(),
	})
	require.NoError(t, err)
	require.NoError(t, exec.Open(committed+1, genesis.Add(time.Second).UnixMilli()))

	s := &pendingSequencer{app: a, exec: exec, committed: committed}
	s.wm = evmrpc.NewWatermarkManager(&MockClient{latestOverride: committed}, s.ctxProvider, nil, nil)
	s.wm.SetPendingSnapshot(exec)
	return s
}

// ctxProvider serves the committed state for latest and the open block for
// pending, as the node's RPC context provider does on the sequencer.
func (s *pendingSequencer) ctxProvider(height int64) sdk.Context {
	if height == evmrpc.PendingCtxHeight {
		ctx, err := s.exec.OpenContext()
		if err != nil {
			panic(err)
		}
		return ctx
	}
	return sdk.NewContext(s.app.CommitMultiStore().CacheMultiStore(), tmproto.Header{ChainID: pendingChainID, Height: s.committed}, false)
}

func (s *pendingSequencer) weiBalance(ctx sdk.Context, addr common.Address) *big.Int {
	return s.app.EvmKeeper.GetBalance(ctx, s.app.EvmKeeper.GetPaxAddressOrDefault(ctx, addr))
}

func (s *pendingSequencer) pendingBalance(t *testing.T, addr common.Address) *big.Int {
	t.Helper()
	height, err := s.wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber))
	require.NoError(t, err)
	require.Equal(t, evmrpc.PendingCtxHeight, height)
	return s.weiBalance(s.ctxProvider(height), addr)
}

func pendingTransfer(t *testing.T, from utils.TestAcct, nonce uint64, to common.Address, value *big.Int) []byte {
	t.Helper()
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(pendingGasPriceWei),
		Gas:      pendingTransferGas,
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
	builder.SetGasLimit(pendingTransferGas)
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz
}

func requireWeiEqual(t *testing.T, want, got *big.Int) {
	t.Helper()
	require.Zerof(t, want.Cmp(got), "want %s wei, got %s", want, got)
}

func TestPendingStateResolvesOpenHeight(t *testing.T) {
	const committed = int64(1000)
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, committed, sender)
	ctx := context.Background()
	pending := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)

	height, err := evmrpc.GetBlockNumberByNrOrHash(ctx, nil, s.wm, pending)
	require.NoError(t, err)
	require.NotNil(t, height)
	require.Equal(t, committed+1, *height)

	resolved, err := s.wm.ResolveHeight(ctx, pending)
	require.NoError(t, err)
	require.Equal(t, evmrpc.PendingCtxHeight, resolved)
	require.Equal(t, committed+1, s.ctxProvider(resolved).BlockHeight())

	// Committed resolution is unchanged.
	for _, tag := range []rpc.BlockNumber{rpc.LatestBlockNumber, rpc.SafeBlockNumber, rpc.FinalizedBlockNumber} {
		resolved, err := s.wm.ResolveHeight(ctx, rpc.BlockNumberOrHashWithNumber(tag))
		require.NoError(t, err)
		require.Equal(t, committed, resolved, "tag %v", tag)
	}

	// A receipted transfer is visible through pending before commit; latest
	// still reads the committed state.
	funded := new(big.Int).Mul(big.NewInt(pendingFundingUhpx), big.NewInt(pendingWeiPerUhpx))
	value := big.NewInt(pendingOnePaxWei)
	accepted, err := s.exec.Accept(pendingTransfer(t, sender, 0, recipient, value))
	require.NoError(t, err)
	require.Equal(t, committed+1, accepted.Height)
	fee := new(big.Int).Mul(big.NewInt(pendingTransferGas), big.NewInt(pendingGasPriceWei))
	requireWeiEqual(t, new(big.Int).Sub(new(big.Int).Sub(funded, value), fee), s.pendingBalance(t, sender.EvmAddress))
	requireWeiEqual(t, value, s.pendingBalance(t, recipient))
	latest, err := s.wm.ResolveHeight(ctx, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
	require.NoError(t, err)
	requireWeiEqual(t, funded, s.weiBalance(s.ctxProvider(latest), sender.EvmAddress))
	require.Zero(t, s.weiBalance(s.ctxProvider(latest), recipient).Sign())

	// Once the open block is sealed there is no pending block to read.
	_, err = s.exec.Seal(committed + 1)
	require.NoError(t, err)
	_, err = s.wm.ResolveHeight(ctx, pending)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	_, err = evmrpc.GetBlockNumberByNrOrHash(ctx, nil, s.wm, pending)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
}

func TestPendingStateRefusedOffSequencer(t *testing.T) {
	const address = "0x1234567890123456789023456789012345678901"
	body := fmt.Sprintf(`{"jsonrpc":"2.0","method":"eth_getBalance","params":[%q,"pending"],"id":"pending"}`, address)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	var resObj struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&resObj))
	require.Nil(t, resObj.Result)
	require.NotNil(t, resObj.Error)
	require.Equal(t, -32000, resObj.Error.Code)
	require.Equal(t, "pending served by sequencer", resObj.Error.Message)

	// The same node still answers latest.
	latestBody := strings.Replace(body, `"pending"`, `"latest"`, 1)
	req, err = http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(latestBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	resObj.Result, resObj.Error = nil, nil
	require.NoError(t, json.NewDecoder(res.Body).Decode(&resObj))
	require.Nil(t, resObj.Error)
	require.JSONEq(t, `"0x38d7ea4c68000"`, string(resObj.Result))

	// Every pending resolution path refuses without an open block.
	ctx := context.Background()
	pending := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	wm := evmrpc.NewWatermarkManager(&MockClient{}, func(int64) sdk.Context { return Ctx }, nil, nil)
	_, err = wm.ResolveHeight(ctx, pending)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	height, err := evmrpc.GetBlockNumberByNrOrHash(ctx, nil, wm, pending)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	require.Nil(t, height)
	_, _, err = evmrpc.ComputeBlockBounds(MockHeight103, 1, 0, filters.FilterCriteria{ToBlock: big.NewInt(rpc.PendingBlockNumber.Int64())})
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	_, _, err = evmrpc.ComputeBlockBounds(MockHeight103, 1, 0, filters.FilterCriteria{FromBlock: big.NewInt(rpc.PendingBlockNumber.Int64())})
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	latest, err := wm.ResolveHeight(ctx, rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
	require.NoError(t, err)
	require.Equal(t, Ctx.BlockHeight(), latest)
}

func TestPendingStateSnapshotDoesNotBlockAccept(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, 1, sender)
	value := big.NewInt(pendingOnePaxWei)
	fee := new(big.Int).Mul(big.NewInt(pendingTransferGas), big.NewInt(pendingGasPriceWei))
	funded := new(big.Int).Mul(big.NewInt(pendingFundingUhpx), big.NewInt(pendingWeiPerUhpx))

	type balances struct {
		sender, recipient *big.Int
	}
	type readResult struct {
		start, end balances
		err        error
	}
	snapshotTaken := make(chan struct{})
	release := make(chan struct{})
	readDone := make(chan readResult, 1)
	go func() {
		height, err := s.wm.ResolveHeight(context.Background(), rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber))
		if err != nil {
			readDone <- readResult{err: err}
			close(snapshotTaken)
			return
		}
		snap := s.ctxProvider(height)
		start := balances{s.weiBalance(snap, sender.EvmAddress), s.weiBalance(snap, recipient)}
		close(snapshotTaken)
		// The read keeps running on its snapshot for at least 50 ms and until
		// the transactions accepted meanwhile have their receipts.
		time.Sleep(50 * time.Millisecond)
		<-release
		readDone <- readResult{start: start, end: balances{s.weiBalance(snap, sender.EvmAddress), s.weiBalance(snap, recipient)}}
	}()
	<-snapshotTaken

	txs := [][]byte{
		pendingTransfer(t, sender, 0, recipient, value),
		pendingTransfer(t, sender, 1, recipient, value),
	}
	accepted := make(chan []*sequencer.Accepted, 1)
	acceptErr := make(chan error, 1)
	go func() {
		var out []*sequencer.Accepted
		for _, tx := range txs {
			acc, err := s.exec.Accept(tx)
			if err != nil {
				acceptErr <- err
				return
			}
			out = append(out, acc)
		}
		accepted <- out
	}()
	var receipts []*sequencer.Accepted
	select {
	case receipts = <-accepted:
	case err := <-acceptErr:
		require.NoError(t, err)
	case <-time.After(30 * time.Second):
		require.FailNow(t, "Accept blocked while a pending read held its snapshot")
	}
	select {
	case res := <-readDone:
		require.FailNow(t, "pending read returned before release", "%+v", res)
	default:
	}
	require.Len(t, receipts, 2)
	require.Equal(t, uint64(1), receipts[0].Seq)
	require.Equal(t, uint64(2), receipts[1].Seq)
	require.Equal(t, abci.CodeTypeOK, receipts[0].Result.Code)
	require.Equal(t, abci.CodeTypeOK, receipts[1].Result.Code)

	close(release)
	res := <-readDone
	require.NoError(t, res.err)
	requireWeiEqual(t, funded, res.start.sender)
	require.Zero(t, res.start.recipient.Sign())
	requireWeiEqual(t, res.start.sender, res.end.sender)
	requireWeiEqual(t, res.start.recipient, res.end.recipient)

	// A pending read started after the receipts sees both transfers.
	two := big.NewInt(2)
	spent := new(big.Int).Mul(new(big.Int).Add(value, fee), two)
	requireWeiEqual(t, new(big.Int).Sub(funded, spent), s.pendingBalance(t, sender.EvmAddress))
	requireWeiEqual(t, new(big.Int).Mul(value, two), s.pendingBalance(t, recipient))
}
