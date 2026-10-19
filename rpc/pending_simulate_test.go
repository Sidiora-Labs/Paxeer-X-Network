package evmrpc_test

import (
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/legacyabci"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer/wal"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/fixtures/example/contracts/simplestorage"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	pendingSimulateDeployGas = 1_000_000
	pendingSimulateSetGas    = 100_000
	pendingSimulateStored    = 20
	// pendingSimulateSetFloorGas is the intrinsic gas plus the cold SSTORE of a
	// zero slot to a non-zero value that set(20) pays on a fresh contract.
	pendingSimulateSetFloorGas = 21_000 + 22_100
)

var (
	pendingSimulateTag = rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	pendingSimulateLat = rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
)

// pendingSimulateNode is the RPC surface of one sequencer or standby node: the
// state, simulation and trace backends over its context provider and its
// watermarks, as the node wires them.
type pendingSimulateNode struct {
	seq     *pendingSequencer
	state   *evmrpc.StateAPI
	sim     *evmrpc.SimulationAPI
	backend *evmrpc.Backend
	block   *evmrpc.BlockAPI
}

func pendingSimulateAPIs(s *pendingSequencer, wm *evmrpc.WatermarkManager) pendingSimulateNode {
	// Committed heights carry the consensus params as the node's RPC context
	// provider sets them; the pending snapshot carries its own.
	ctxProvider := func(height int64) sdk.Context {
		ctx := s.ctxProvider(height)
		if height == evmrpc.PendingCtxHeight {
			return ctx
		}
		return ctx.WithConsensusParams(s.app.GetConsensusParams(ctx))
	}
	tmClient := &MockClient{latestOverride: s.committed}
	txConfig := app.MakeEncodingConfig().TxConfig
	txConfigProvider := func(int64) client.TxConfig { return txConfig }
	cfg := &evmrpc.SimulateConfig{GasCap: 10_000_000, EVMTimeout: 5 * time.Second}
	return pendingSimulateNode{
		seq:   s,
		state: evmrpc.NewStateAPI(tmClient, &s.app.EvmKeeper, ctxProvider, evmrpc.ConnectionTypeHTTP, wm),
		sim: evmrpc.NewSimulationAPI(ctxProvider, &s.app.EvmKeeper, legacyabci.BeginBlockKeepers{}, txConfigProvider, tmClient, cfg,
			s.app.BaseApp, s.app.TracerAnteHandler, evmrpc.ConnectionTypeHTTP, evmrpc.NewBlockCache(3000), &sync.Mutex{}, wm),
		backend: evmrpc.NewBackend(ctxProvider, &s.app.EvmKeeper, legacyabci.BeginBlockKeepers{}, txConfigProvider, tmClient, cfg,
			s.app.BaseApp, s.app.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, wm),
		block: evmrpc.NewBlockAPI(tmClient, &s.app.EvmKeeper, ctxProvider, txConfigProvider, evmrpc.ConnectionTypeHTTP, wm, nil, &sync.Mutex{}),
	}
}

// pendingSimulateTx signs a legacy transaction and wraps it as the Cosmos tx
// bytes the executor accepts; a nil to creates a contract.
func pendingSimulateTx(t *testing.T, from utils.TestAcct, nonce uint64, to *common.Address, value *big.Int, data []byte, gas uint64) []byte {
	t.Helper()
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(pendingGasPriceWei),
		Gas:      gas,
		To:       to,
		Value:    value,
		Data:     data,
	}), ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), from.EvmPrivateKey)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(gas)
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz
}

func pendingSimulateAccept(t *testing.T, exec sequencer.Executor, tx []byte) *sequencer.Accepted {
	t.Helper()
	acc, err := exec.Accept(tx)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code, acc.Result.Log)
	return acc
}

// pendingSimulateStorage returns the SimpleStorage creation code and the
// calldata of set(20) and get().
func pendingSimulateStorage(t *testing.T) (creation, set, get []byte) {
	t.Helper()
	code, err := os.ReadFile("../tests/fixtures/example/contracts/simplestorage/SimpleStorage.bin")
	require.NoError(t, err)
	creation, err = hex.DecodeString(string(code))
	require.NoError(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.NoError(t, err)
	set, err = abi.Pack("set", big.NewInt(pendingSimulateStored))
	require.NoError(t, err)
	get, err = abi.Pack("get")
	require.NoError(t, err)
	return creation, set, get
}

func pendingSimulateWord(v int64) hexutil.Bytes {
	return common.BigToHash(big.NewInt(v)).Bytes()
}

func pendingSimulateArgs(from, to common.Address, value *big.Int, input []byte) export.TransactionArgs {
	in := hexutil.Bytes(input)
	return export.TransactionArgs{From: &from, To: &to, Value: (*hexutil.Big)(value), Input: &in}
}

func TestPendingSimulateBalanceMatchesCommit(t *testing.T) {
	const committed = int64(10)
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, committed, sender)
	n := pendingSimulateAPIs(s, s.wm)
	ctx := context.Background()

	funded := new(big.Int).Mul(big.NewInt(pendingFundingUhpx), big.NewInt(pendingWeiPerUhpx))
	value := big.NewInt(pendingOnePaxWei)
	fee := new(big.Int).Mul(big.NewInt(pendingTransferGas), big.NewInt(pendingGasPriceWei))
	pendingSimulateAccept(t, s.exec, pendingTransfer(t, sender, 0, recipient, value))
	wantSender := new(big.Int).Sub(new(big.Int).Sub(funded, value), fee)

	pendingSender, err := n.state.GetBalance(ctx, sender.EvmAddress, pendingSimulateTag)
	require.NoError(t, err)
	requireWeiEqual(t, wantSender, pendingSender.ToInt())
	pendingRecipient, err := n.state.GetBalance(ctx, recipient, pendingSimulateTag)
	require.NoError(t, err)
	requireWeiEqual(t, value, pendingRecipient.ToInt())
	latestSender, err := n.state.GetBalance(ctx, sender.EvmAddress, pendingSimulateLat)
	require.NoError(t, err)
	requireWeiEqual(t, funded, latestSender.ToInt())
	latestRecipient, err := n.state.GetBalance(ctx, recipient, pendingSimulateLat)
	require.NoError(t, err)
	require.Zero(t, latestRecipient.ToInt().Sign())

	// Seal and commit the open block; latest then answers what pending did.
	batch, err := s.exec.Seal(committed + 1)
	require.NoError(t, err)
	require.Len(t, batch.Txs, 1)
	_, err = n.state.GetBalance(ctx, sender.EvmAddress, pendingSimulateTag)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
	batch.Store.Write()
	require.Equal(t, committed+1, s.app.CommitMultiStore().Commit(true).Version)
	s.committed = committed + 1
	after := pendingSimulateAPIs(s, evmrpc.NewWatermarkManager(&MockClient{latestOverride: s.committed}, s.ctxProvider, nil, nil))
	committedSender, err := after.state.GetBalance(ctx, sender.EvmAddress, pendingSimulateLat)
	require.NoError(t, err)
	requireWeiEqual(t, pendingSender.ToInt(), committedSender.ToInt())
	committedRecipient, err := after.state.GetBalance(ctx, recipient, pendingSimulateLat)
	require.NoError(t, err)
	requireWeiEqual(t, pendingRecipient.ToInt(), committedRecipient.ToInt())
}

func TestPendingSimulateEstimateDefaultsPending(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	third := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, 1, sender)
	n := pendingSimulateAPIs(s, s.wm)
	ctx := context.Background()

	// The recipient is funded only in the open block.
	pendingSimulateAccept(t, s.exec, pendingTransfer(t, sender, 0, recipient, big.NewInt(pendingOnePaxWei)))
	args := pendingSimulateArgs(recipient, third, big.NewInt(pendingOnePaxWei/2), nil)

	estimate, err := n.sim.EstimateGas(ctx, args, nil, nil)
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(pendingTransferGas), estimate)
	explicit, err := n.sim.EstimateGas(ctx, args, &pendingSimulateTag, nil)
	require.NoError(t, err)
	require.Equal(t, estimate, explicit)
	_, err = n.sim.EstimateGas(ctx, args, &pendingSimulateLat, nil)
	require.ErrorContains(t, err, "insufficient funds")
}

func TestPendingSimulateAccessListOnOpenBlock(t *testing.T) {
	sender := utils.NewSigner()
	s := newPendingSequencer(t, 1, sender)
	n := pendingSimulateAPIs(s, s.wm)
	ctx := context.Background()
	creation, set, get := pendingSimulateStorage(t)

	pendingSimulateAccept(t, s.exec, pendingSimulateTx(t, sender, 0, nil, big.NewInt(0), creation, pendingSimulateDeployGas))
	contract := crypto.CreateAddress(sender.EvmAddress, 0)
	args := pendingSimulateArgs(sender.EvmAddress, contract, big.NewInt(0), set)

	// Default tag is pending: the contract exists and set(20) writes its slot.
	open, err := n.sim.CreateAccessList(ctx, args, nil)
	require.NoError(t, err)
	require.Empty(t, open.Error)
	require.NotNil(t, open.Accesslist)
	require.Equal(t, ethtypes.AccessList{{Address: contract, StorageKeys: []common.Hash{{}}}}, *open.Accesslist)
	require.GreaterOrEqual(t, uint64(open.GasUsed), uint64(pendingSimulateSetFloorGas))
	explicit, err := n.sim.CreateAccessList(ctx, args, &pendingSimulateTag)
	require.NoError(t, err)
	require.Equal(t, open.GasUsed, explicit.GasUsed)

	// At latest the address has no code, so the call is a plain transfer.
	latest, err := n.sim.CreateAccessList(ctx, args, &pendingSimulateLat)
	require.NoError(t, err)
	require.Empty(t, latest.Error)
	require.Less(t, uint64(latest.GasUsed), uint64(pendingSimulateSetFloorGas))

	// eth_call reads the same snapshot: get() sees the receipted set(20) only
	// through pending.
	getArgs := pendingSimulateArgs(sender.EvmAddress, contract, big.NewInt(0), get)
	before, err := n.sim.Call(ctx, getArgs, &pendingSimulateTag, nil, nil)
	require.NoError(t, err)
	require.Equal(t, pendingSimulateWord(0), before)
	pendingSimulateAccept(t, s.exec, pendingSimulateTx(t, sender, 1, &contract, big.NewInt(0), set, pendingSimulateSetGas))
	after, err := n.sim.Call(ctx, getArgs, &pendingSimulateTag, nil, nil)
	require.NoError(t, err)
	require.Equal(t, pendingSimulateWord(pendingSimulateStored), after)
	atLatest, err := n.sim.Call(ctx, getArgs, &pendingSimulateLat, nil, nil)
	require.NoError(t, err)
	require.Empty(t, atLatest)
}

func TestPendingSimulateRefusedOffSequencer(t *testing.T) {
	sender := utils.NewSigner()
	s := newPendingSequencer(t, 1, sender)
	// The same state behind watermarks without an open block: any other node.
	other := pendingSimulateAPIs(s, evmrpc.NewWatermarkManager(&MockClient{latestOverride: s.committed}, s.ctxProvider, nil, nil))
	ctx := context.Background()
	args := pendingSimulateArgs(sender.EvmAddress, utils.NewSigner().EvmAddress, big.NewInt(1), nil)

	_, err := other.sim.Call(ctx, args, &pendingSimulateTag, nil, nil)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	_, err = other.sim.EstimateGas(ctx, args, &pendingSimulateTag, nil)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	_, err = other.sim.CreateAccessList(ctx, args, nil)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	_, err = other.state.GetBalance(ctx, sender.EvmAddress, pendingSimulateTag)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	_, err = other.state.GetCode(ctx, sender.EvmAddress, pendingSimulateTag)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	_, err = other.state.GetStorageAt(ctx, sender.EvmAddress, "0x0", pendingSimulateTag)
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)

	// Over JSON-RPC the refusal is -32000, and the estimate default stays latest.
	Ctx = Ctx.WithBlockHeight(1)
	defer func() { Ctx = Ctx.WithBlockHeight(8) }()
	to := utils.NewSigner().EvmAddress.Hex()
	callArgs := map[string]any{"to": to, "value": "0x0"}
	for _, method := range []string{"call", "estimateGas"} {
		resObj := sendRequestGood(t, method, callArgs, "pending")
		require.Nil(t, resObj["result"], method)
		errObj, ok := resObj["error"].(map[string]any)
		require.True(t, ok, method)
		require.Equal(t, float64(evmrpc.ErrCodeEVMNotSupported), errObj["code"], method)
		require.Equal(t, "pending served by sequencer", errObj["message"], method)
	}
	resObj := sendRequestGood(t, "createAccessList", callArgs)
	errObj, ok := resObj["error"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "pending served by sequencer", errObj["message"])
	resObj = sendRequestGood(t, "estimateGas", callArgs)
	require.Nil(t, resObj["error"], "estimate refused: %v", resObj["error"])
	require.Equal(t, "0x5208", resObj["result"])
}

func TestPendingSimulateStorageAndCode(t *testing.T) {
	sender := utils.NewSigner()
	s := newPendingSequencer(t, 1, sender)
	n := pendingSimulateAPIs(s, s.wm)
	ctx := context.Background()
	creation, set, _ := pendingSimulateStorage(t)
	contract := crypto.CreateAddress(sender.EvmAddress, 0)

	pendingSimulateAccept(t, s.exec, pendingSimulateTx(t, sender, 0, nil, big.NewInt(0), creation, pendingSimulateDeployGas))
	pendingSimulateAccept(t, s.exec, pendingSimulateTx(t, sender, 1, &contract, big.NewInt(0), set, pendingSimulateSetGas))

	code, err := n.state.GetCode(ctx, contract, pendingSimulateTag)
	require.NoError(t, err)
	require.NotEmpty(t, code)
	snap, err := s.exec.Snapshot()
	require.NoError(t, err)
	require.Equal(t, hexutil.Bytes(s.app.EvmKeeper.GetCode(snap, contract)), code)
	latestCode, err := n.state.GetCode(ctx, contract, pendingSimulateLat)
	require.NoError(t, err)
	require.Empty(t, latestCode)

	slot, err := n.state.GetStorageAt(ctx, contract, "0x0", pendingSimulateTag)
	require.NoError(t, err)
	require.Equal(t, pendingSimulateWord(pendingSimulateStored), slot)
	latestSlot, err := n.state.GetStorageAt(ctx, contract, "0x0", pendingSimulateLat)
	require.NoError(t, err)
	require.Equal(t, pendingSimulateWord(0), latestSlot)
}

func TestPendingSimulateProofRefused(t *testing.T) {
	sender := utils.NewSigner()
	s := newPendingSequencer(t, 1, sender)
	ctx := context.Background()
	sequencerNode := pendingSimulateAPIs(s, s.wm)
	otherNode := pendingSimulateAPIs(s, evmrpc.NewWatermarkManager(&MockClient{latestOverride: s.committed}, s.ctxProvider, nil, nil))
	for _, n := range []pendingSimulateNode{sequencerNode, otherNode} {
		res, err := n.state.GetProof(ctx, sender.EvmAddress, []string{"0x0"}, pendingSimulateTag)
		require.ErrorIs(t, err, evmrpc.ErrPendingProofRefused)
		require.Nil(t, res)
	}
	require.Equal(t, "pending state has no committed root", evmrpc.ErrPendingProofRefused.Error())
	var coded interface{ ErrorCode() int }
	require.ErrorAs(t, evmrpc.ErrPendingProofRefused, &coded)
	require.Equal(t, evmrpc.ErrCodeEVMNotSupported, coded.ErrorCode())
}

func TestPendingSimulateTraceRefused(t *testing.T) {
	sender := utils.NewSigner()
	s := newPendingSequencer(t, 1, sender)
	n := pendingSimulateAPIs(s, s.wm)
	ctx := context.Background()
	pendingSimulateAccept(t, s.exec, pendingTransfer(t, sender, 0, utils.NewSigner().EvmAddress, big.NewInt(pendingOnePaxWei)))

	const refused = "tracing on the pending block is not supported"
	_, err := n.backend.ConvertBlockNumber(ctx, rpc.PendingBlockNumber)
	require.EqualError(t, err, refused)
	block, metadata, err := n.backend.BlockByNumber(ctx, rpc.PendingBlockNumber)
	require.EqualError(t, err, refused)
	require.Nil(t, block)
	require.Nil(t, metadata)
}

// pendingSimulateReplayer drives a node's real executor from WAL records, as
// the standby wires it; the receipt bytes are u64be seq ‖ seqHash ‖ u64be open
// time ‖ ExecTxResult protobuf.
type pendingSimulateReplayer struct {
	exec sequencer.Executor
}

func (r pendingSimulateReplayer) Apply(e wal.Entry) error {
	switch v := e.(type) {
	case *wal.Open:
		return r.exec.Open(int64(v.Height), int64(v.OpenTimeMs)) //nolint:gosec // test heights and times fit int64
	case *wal.Seal:
		_, err := r.exec.Seal(int64(v.Height)) //nolint:gosec // test heights fit int64
		return err
	default:
		return nil
	}
}

func (r pendingSimulateReplayer) Execute(a *wal.Accept) ([]byte, error) {
	acc, err := r.exec.Accept(a.Tx)
	if err != nil {
		return nil, err
	}
	return pendingSimulateReceipt(acc)
}

func pendingSimulateReceipt(acc *sequencer.Accepted) ([]byte, error) {
	res, err := acc.Result.Marshal()
	if err != nil {
		return nil, err
	}
	b := binary.BigEndian.AppendUint64(nil, acc.Seq)
	b = append(b, acc.SeqHash[:]...)
	b = binary.BigEndian.AppendUint64(b, uint64(acc.TimestampMs)) //nolint:gosec // positive open time
	return append(b, res...), nil
}

func TestPendingSimulateStandbyEqualsPrimary(t *testing.T) {
	const committed = int64(1)
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	primary := newPendingSequencer(t, committed, sender)
	standby := newPendingSequencer(t, committed, sender)
	// Both nodes start without an open block; the standby opens only from the
	// primary's WAL, a minute ahead so both genesis times lie before it.
	_, err := primary.exec.Seal(committed + 1)
	require.NoError(t, err)
	_, err = standby.exec.Seal(committed + 1)
	require.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	primaryWAL, err := wal.OpenWAL(filepath.Join(t.TempDir(), "primary-wal"), wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(primaryWAL.Close)
	addr := l.Addr().String()
	repl, err := sequencer.NewReplicator(sequencer.ReplicatorConfig{
		WAL: primaryWAL,
		Dial: func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
		ReplicationTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(repl.Close)
	primaryKeyDir, standbyKeyDir := t.TempDir(), t.TempDir()
	primaryPV, err := privval.GenFilePV(filepath.Join(primaryKeyDir, "key.json"), filepath.Join(primaryKeyDir, "state.json"), "")
	require.NoError(t, err)
	standbyPV, err := privval.GenFilePV(filepath.Join(standbyKeyDir, "key.json"), filepath.Join(standbyKeyDir, "state.json"), "")
	require.NoError(t, err)
	primaryReceipt, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, standbyReceipt, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	primaryPub, err := primaryPV.GetPubKey(context.Background())
	require.NoError(t, err)
	standbyWAL, err := wal.OpenWAL(filepath.Join(t.TempDir(), "standby-wal"), wal.DefaultConfig())
	require.NoError(t, err)
	t.Cleanup(standbyWAL.Close)
	sb, err := sequencer.NewStandby(context.Background(), sequencer.StandbyConfig{
		WAL:                 standbyWAL,
		Executor:            pendingSimulateReplayer{exec: standby.exec},
		ConsensusKey:        standbyPV,
		ReceiptKey:          standbyReceipt,
		PrimaryConsensusKey: primaryPub,
		PrimaryReceiptKey:   primaryReceipt,
		CommittedHeight:     uint64(committed),
	})
	require.NoError(t, err)
	go func() { _ = sb.Serve(l) }()

	openMs := time.Now().Add(time.Minute).UnixMilli()
	require.NoError(t, primary.exec.Open(committed+1, openMs))
	entries := []wal.Entry{&wal.Open{Height: uint64(committed + 1), OpenTimeMs: uint64(openMs), BaseFee: uint256.NewInt(0)}}
	creation, set, get := pendingSimulateStorage(t)
	contract := crypto.CreateAddress(sender.EvmAddress, 0)
	txs := [][]byte{
		pendingSimulateTx(t, sender, 0, nil, big.NewInt(0), creation, pendingSimulateDeployGas),
		pendingSimulateTx(t, sender, 1, &contract, big.NewInt(0), set, pendingSimulateSetGas),
		pendingTransfer(t, sender, 2, recipient, big.NewInt(pendingOnePaxWei)),
	}
	for _, tx := range txs {
		acc := pendingSimulateAccept(t, primary.exec, tx)
		receipt, err := pendingSimulateReceipt(acc)
		require.NoError(t, err)
		entries = append(entries, &wal.Accept{Seq: acc.Seq, Height: uint64(acc.Height), Tx: tx, Receipt: receipt})
	}
	ack, err := repl.Release(context.Background(), entries...)
	require.NoError(t, err)
	require.Equal(t, sequencer.Ack{Status: sequencer.AckOK, LastSeq: uint64(len(txs))}, ack)
	require.Equal(t, uint64(len(txs)), sb.LastSeq())
	require.NoError(t, sb.Err())

	ctx := context.Background()
	p := pendingSimulateAPIs(primary, primary.wm)
	q := pendingSimulateAPIs(standby, standby.wm)
	for _, a := range []common.Address{sender.EvmAddress, recipient, contract} {
		pb, err := p.state.GetBalance(ctx, a, pendingSimulateTag)
		require.NoError(t, err)
		sbBal, err := q.state.GetBalance(ctx, a, pendingSimulateTag)
		require.NoError(t, err)
		requireWeiEqual(t, pb.ToInt(), sbBal.ToInt())
	}
	recipientBal, err := q.state.GetBalance(ctx, recipient, pendingSimulateTag)
	require.NoError(t, err)
	requireWeiEqual(t, big.NewInt(pendingOnePaxWei), recipientBal.ToInt())

	pc, err := p.state.GetCode(ctx, contract, pendingSimulateTag)
	require.NoError(t, err)
	sc, err := q.state.GetCode(ctx, contract, pendingSimulateTag)
	require.NoError(t, err)
	require.NotEmpty(t, sc)
	require.Equal(t, pc, sc)
	ps, err := p.state.GetStorageAt(ctx, contract, "0x0", pendingSimulateTag)
	require.NoError(t, err)
	ss, err := q.state.GetStorageAt(ctx, contract, "0x0", pendingSimulateTag)
	require.NoError(t, err)
	require.Equal(t, pendingSimulateWord(pendingSimulateStored), ss)
	require.Equal(t, ps, ss)

	getArgs := pendingSimulateArgs(sender.EvmAddress, contract, big.NewInt(0), get)
	pr, err := p.sim.Call(ctx, getArgs, &pendingSimulateTag, nil, nil)
	require.NoError(t, err)
	sr, err := q.sim.Call(ctx, getArgs, &pendingSimulateTag, nil, nil)
	require.NoError(t, err)
	require.Equal(t, pendingSimulateWord(pendingSimulateStored), sr)
	require.Equal(t, pr, sr)
	transferArgs := pendingSimulateArgs(recipient, sender.EvmAddress, big.NewInt(pendingOnePaxWei/2), nil)
	pe, err := p.sim.EstimateGas(ctx, transferArgs, nil, nil)
	require.NoError(t, err)
	se, err := q.sim.EstimateGas(ctx, transferArgs, nil, nil)
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(pendingTransferGas), se)
	require.Equal(t, pe, se)
}

// pendingSimulateInitCode wraps runtime in creation code that copies it to
// memory and returns it.
func pendingSimulateInitCode(runtime []byte) []byte {
	return append([]byte{
		0x60, byte(len(runtime)), // PUSH1 len
		0x80,       // DUP1
		0x60, 0x0b, // PUSH1 11, the runtime offset
		0x60, 0x00, // PUSH1 0
		0x39,       // CODECOPY
		0x60, 0x00, // PUSH1 0
		0xf3, // RETURN
	}, runtime...)
}

func TestPendingSimulateBlockhashParent(t *testing.T) {
	const committed = int64(10)
	sender := utils.NewSigner()
	s := newPendingSequencer(t, committed, sender)
	n := pendingSimulateAPIs(s, s.wm)
	ctx := context.Background()

	// Returns blockhash(block.number - 1).
	reader := []byte{
		0x60, 0x01, // PUSH1 1
		0x43,       // NUMBER
		0x03,       // SUB
		0x40,       // BLOCKHASH
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 32
		0x60, 0x00, // PUSH1 0
		0xf3, // RETURN
	}
	// Reverts when blockhash(block.number - 1) is zero, else stores 1 at slot 0.
	brancher := []byte{
		0x60, 0x01, // PUSH1 1
		0x43,       // NUMBER
		0x03,       // SUB
		0x40,       // BLOCKHASH
		0x60, 0x0c, // PUSH1 12, the JUMPDEST
		0x57,       // JUMPI
		0x60, 0x00, // PUSH1 0
		0x80,       // DUP1
		0xfd,       // REVERT
		0x5b,       // JUMPDEST
		0x60, 0x01, // PUSH1 1
		0x60, 0x00, // PUSH1 0
		0x55, // SSTORE
		0x00, // STOP
	}
	pendingSimulateAccept(t, s.exec, pendingSimulateTx(t, sender, 0, nil, big.NewInt(0), pendingSimulateInitCode(reader), pendingSimulateDeployGas))
	pendingSimulateAccept(t, s.exec, pendingSimulateTx(t, sender, 1, nil, big.NewInt(0), pendingSimulateInitCode(brancher), pendingSimulateDeployGas))
	readerAddr := crypto.CreateAddress(sender.EvmAddress, 0)
	brancherAddr := crypto.CreateAddress(sender.EvmAddress, 1)

	block, err := n.block.GetBlockByNumber(ctx, rpc.BlockNumber(committed), false)
	require.NoError(t, err)
	parentHash, ok := block["hash"].(common.Hash)
	require.True(t, ok, "hash is %T", block["hash"])
	require.NotEqual(t, common.Hash{}, parentHash)

	got, err := n.sim.Call(ctx, pendingSimulateArgs(sender.EvmAddress, readerAddr, big.NewInt(0), nil), &pendingSimulateTag, nil, nil)
	require.NoError(t, err)
	require.Equal(t, hexutil.Bytes(parentHash.Bytes()), got)

	estimate, err := n.sim.EstimateGas(ctx, pendingSimulateArgs(sender.EvmAddress, brancherAddr, big.NewInt(0), nil), &pendingSimulateTag, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, uint64(estimate), uint64(pendingSimulateSetFloorGas))
}
