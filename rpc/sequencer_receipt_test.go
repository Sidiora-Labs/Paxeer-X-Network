package evmrpc_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	seqkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	seqnode "github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	evmrpcconfig "github.com/Sidiora-Labs/Paxeer-X-Network/rpc/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

const (
	seqReceiptChainID9005  = "sequencer-receipt-test-1"
	seqReceiptModuleID9005 = uint16(12)
	seqReceiptFeeDenom9005 = "upax"
)

var seqReceiptSeqDomain9005 = []byte("PAXEER/v1/sequence\x00")

// seqReceiptChain9005 is a sequencer host: the signed receipts it hands out and the committed
// sequencer store whose MsgSequenceBatch history the API reads.
type seqReceiptChain9005 struct {
	t          *testing.T
	mu         sync.Mutex
	ms         storetypes.CommitMultiStore
	key        storetypes.StoreKey
	keeper     seqkeeper.Keeper
	latest     int64
	snapshots  map[int64]storetypes.CommitMultiStore
	seed       []byte
	keyID      [32]byte
	receiptKey *seqnode.ReceiptKey
	lastSeq    uint64
	seqHashes  map[uint64][32]byte
	activities map[uint64][32]byte
	committed  uint64
	sends      map[common.Hash]*evmrpc.SequencerSend
}

func newSeqReceiptChain9005(t *testing.T) *seqReceiptChain9005 {
	t.Helper()
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	key := sdk.NewKVStoreKey(seqtypes.StoreKey)
	ms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())

	seed := bytes.Repeat([]byte{0x21}, ed25519.SeedSize)
	public := [32]byte(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey))
	var keyID [32]byte
	copy(keyID[:], bytes.Repeat([]byte{0x44}, 32))
	var leader [32]byte
	copy(leader[:], bytes.Repeat([]byte{0x55}, 32))

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "receipt_key")
	require.NoError(t, os.WriteFile(keyFile, []byte(hex.EncodeToString(seed)+"\n"), 0o600))
	pv, err := privval.GenFilePV(filepath.Join(dir, "priv_validator_key.json"), filepath.Join(dir, "priv_validator_state.json"), "")
	require.NoError(t, err)
	require.NoError(t, pv.Key.Save())
	receiptKey, err := seqnode.LoadReceiptKey(keyFile, filepath.Join(dir, "priv_validator_key.json"),
		[]verify.SequencerAuthorization{{SequencerID: keyID, PublicKey: public, FirstBatchNumber: 1, LastBatchNumber: 1999}}, 7)
	require.NoError(t, err)

	c := &seqReceiptChain9005{
		t:          t,
		ms:         ms,
		key:        key,
		keeper:     seqkeeper.NewKeeper(key),
		snapshots:  map[int64]storetypes.CommitMultiStore{},
		seed:       seed,
		keyID:      keyID,
		receiptKey: receiptKey,
		seqHashes:  map[uint64][32]byte{0: {}},
		activities: map[uint64][32]byte{},
		sends:      map[common.Hash]*evmrpc.SequencerSend{},
	}
	require.NoError(t, c.keeper.InitGenesis(c.writeCtx9005(1), seqtypes.GenesisState{
		Leaders:     [][32]byte{leader},
		ReceiptKeys: []seqtypes.ReceiptKey{{KeyId: keyID, PublicKey: public, FirstHeight: 1, LastHeight: 1999}},
	}))
	for height := int64(1); height <= 6; height++ {
		c.commit9005(height)
	}
	return c
}

// commit9005 commits the block at height and keeps a copy of the committed sequencer store as
// the state at that height: the legacy root store serves only its latest version.
func (c *seqReceiptChain9005) commit9005(height int64) {
	c.t.Helper()
	require.Equal(c.t, height, c.ms.Commit(true).Version)
	db := dbm.NewMemDB()
	snapshot := store.NewCommitMultiStore(db)
	snapshot.MountStoreWithDB(c.key, sdk.StoreTypeIAVL, db)
	require.NoError(c.t, snapshot.LoadLatestVersion())
	src, dst := c.ms.GetKVStore(c.key), snapshot.GetKVStore(c.key)
	it := src.Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		dst.Set(bytes.Clone(it.Key()), bytes.Clone(it.Value()))
	}
	require.NoError(c.t, it.Close())
	snapshot.Commit(true)
	c.snapshots[height], c.latest = snapshot, height
}

func (c *seqReceiptChain9005) writeCtx9005(height int64) sdk.Context {
	return sdk.NewContext(c.ms, tmproto.Header{ChainID: seqReceiptChainID9005, Height: height}, false).
		WithChainID(seqReceiptChainID9005).WithBlockHeight(height).WithTxIndex(0)
}

// ctxProvider serves the committed sequencer store at a height, the latest one for LatestCtxHeight.
func (c *seqReceiptChain9005) ctxProvider(height int64) sdk.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if height == evmrpc.LatestCtxHeight {
		height = c.latest
	}
	snapshot, ok := c.snapshots[height]
	if !ok {
		panic(fmt.Sprintf("no committed sequencer state at height %d", height))
	}
	return sdk.NewContext(snapshot.CacheMultiStore(), tmproto.Header{ChainID: seqReceiptChainID9005, Height: height}, false).
		WithChainID(seqReceiptChainID9005).WithBlockHeight(height)
}

func (c *seqReceiptChain9005) SequencerSend(txHash common.Hash) (*evmrpc.SequencerSend, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sends[txHash], nil
}

func seqReceiptNextHash9005(prev [32]byte, seq uint64, activity [32]byte) [32]byte {
	preimage := append(bytes.Clone(seqReceiptSeqDomain9005), prev[:]...)
	preimage = binary.BigEndian.AppendUint64(preimage, seq)
	return sha256.Sum256(append(preimage, activity[:]...))
}

// seqReceiptTx9005 describes one executed transaction of a test receipt.
type seqReceiptTx9005 struct {
	nonce      uint64
	height     int64
	txIndex    int
	status     uint32
	returnData []byte
	logs       []*types.Log
	vmError    string
	create     bool
}

func seqReceiptTxHash9005(height int64, nonce uint64) common.Hash {
	return sha256.Sum256([]byte(fmt.Sprintf("sequencer receipt tx %d/%d", height, nonce)))
}

var (
	seqReceiptFrom9005     = common.HexToAddress("0x1000000000000000000000000000000000000001")
	seqReceiptTo9005       = common.HexToAddress("0x2000000000000000000000000000000000000002")
	seqReceiptSponsor9005  = common.HexToAddress("0x3000000000000000000000000000000000000003")
	seqReceiptCreated9005  = common.HexToAddress("0x4000000000000000000000000000000000000004")
	seqReceiptLogAddr9005  = common.HexToAddress("0x5000000000000000000000000000000000000005")
	seqReceiptTopicA9005   = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a1")
	seqReceiptTopicB9005   = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000b2")
	seqReceiptFeeTotal9005 = big.NewInt(4_200_000)
)

// signReceipt9005 signs the receipt of tx at seq chained from prev without recording it.
func (c *seqReceiptChain9005) signReceipt9005(tx seqReceiptTx9005, seq uint64, prev [32]byte, activity [32]byte) []byte {
	c.t.Helper()
	evm := &types.Receipt{
		Status:            tx.status,
		GasUsed:           21_000 + uint64(len(tx.logs))*375,
		CumulativeGasUsed: 63_000 + uint64(len(tx.logs))*375,
		EffectiveGasPrice: 200,
		To:                seqReceiptTo9005.Hex(),
		Logs:              tx.logs,
		VmError:           tx.vmError,
	}
	to := seqReceiptTo9005
	if tx.create {
		evm.To, evm.ContractAddress, to = "", seqReceiptCreated9005.Hex(), common.Address{}
	}
	signed, err := c.receiptKey.SignReceipt(&seqnode.ReceiptInput{
		Accepted: &seqnode.Accepted{
			Seq:         seq,
			PrevSeqHash: prev,
			SeqHash:     seqReceiptNextHash9005(prev, seq, activity),
			ActivityID:  activity,
			Height:      tx.height,
			TxIndex:     tx.txIndex,
			TimestampMs: 1_700_000_000_000 + tx.height*400,
		},
		EVM:               evm,
		Operation:         2,
		ReturnData:        tx.returnData,
		FeeDenom:          seqReceiptFeeDenom9005,
		FeeCharged:        seqReceiptFeeTotal9005,
		Sponsor:           seqReceiptSponsor9005,
		Amount:            big.NewInt(1_000),
		From:              seqReceiptFrom9005,
		FromBalanceBefore: big.NewInt(10_000_000),
		FromBalanceAfter:  big.NewInt(5_799_000),
		FromSequence:      tx.nonce,
		To:                to,
		ToBalanceBefore:   big.NewInt(0),
		ToBalanceAfter:    big.NewInt(1_000),
		ModuleID:          seqReceiptModuleID9005,
	})
	require.NoError(c.t, err)
	return signed
}

func seqReceiptLogData9005(logs []*types.Log) [][]byte {
	data := make([][]byte, len(logs))
	for i, log := range logs {
		data[i] = log.Data
	}
	return data
}

// accept9005 sequences tx as the next seq, signs its receipt and records the send.
func (c *seqReceiptChain9005) accept9005(tx seqReceiptTx9005) (common.Hash, uint64) {
	c.t.Helper()
	hash := seqReceiptTxHash9005(tx.height, tx.nonce)
	c.mu.Lock()
	seq, prev := c.lastSeq+1, c.seqHashes[c.lastSeq]
	c.mu.Unlock()
	signed := c.signReceipt9005(tx, seq, prev, hash)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastSeq = seq
	c.seqHashes[seq] = seqReceiptNextHash9005(prev, seq, hash)
	c.activities[seq] = hash
	c.sends[hash] = &evmrpc.SequencerSend{
		Nonce:      tx.nonce,
		Receipt:    signed,
		ReturnData: tx.returnData,
		LogData:    seqReceiptLogData9005(tx.logs),
		VMError:    tx.vmError,
	}
	return hash, seq
}

func (c *seqReceiptChain9005) submit9005(hash common.Hash, nonce uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sends[hash] = &evmrpc.SequencerSend{Nonce: nonce}
}

// commitBatch9005 delivers the MsgSequenceBatch of height covering every seq above the committed
// one up to toSeq at tx index 0, and commits the block.
func (c *seqReceiptChain9005) commitBatch9005(height int64, toSeq uint64) {
	c.t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	from := c.committed + 1
	prev, last := c.seqHashes[c.committed], c.seqHashes[toSeq]
	msg := seqtypes.MsgSequenceBatch{
		Height:      uint64(height),
		FromSeq:     from,
		ToSeq:       toSeq,
		PrevSeqHash: bytes.Clone(prev[:]),
		LastSeqHash: bytes.Clone(last[:]),
		TxRoot:      bytes.Clone(seqtypes.EmptyTxRoot[:]),
		KeyId:       bytes.Clone(c.keyID[:]),
	}
	if !msg.IsEmpty() {
		var ids []byte
		for seq := from; seq <= toSeq; seq++ {
			id := c.activities[seq]
			ids = append(ids, id[:]...)
		}
		root := sha256.Sum256(ids)
		msg.TxRoot = root[:]
	}
	digest, err := msg.SignBytes(seqReceiptChainID9005)
	require.NoError(c.t, err)
	msg.Sig = ed25519.Sign(ed25519.NewKeyFromSeed(c.seed), digest[:])
	require.NoError(c.t, c.keeper.DeliverSequenceBatch(c.writeCtx9005(height), msg))
	c.commit9005(height)
	c.committed = toSeq
}

func seqReceiptFreePort9005(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", TestAddr+":0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

// startSeqReceiptServer9005 starts an EVM HTTP server with the sequencer receipt option and
// returns its port.
func startSeqReceiptServer9005(t *testing.T, testApp *app.App, ctxProvider func(int64) sdk.Context, option evmrpc.SequencerReceiptOption) int {
	t.Helper()
	config := evmrpcconfig.DefaultConfig
	config.HTTPAddress = TestAddr
	config.HTTPPort = seqReceiptFreePort9005(t)
	server, err := evmrpc.NewEVMHTTPServer(config, &MockClient{}, EVMKeeper, testApp.BeginBlockKeepers, testApp.BaseApp,
		testApp.TracerAnteHandler, ctxProvider, func(int64) client.TxConfig { return TxConfig }, "", nil, option)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(server.Stop)
	return config.HTTPPort
}

type seqReceiptRPCError9005 struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// callSeqReceipt9005 posts one JSON-RPC call and returns its raw result or its error.
func callSeqReceipt9005(t *testing.T, port int, method string, param any) (json.RawMessage, *seqReceiptRPCError9005) {
	t.Helper()
	params, err := json.Marshal([]any{param})
	require.NoError(t, err)
	body := fmt.Sprintf(`{"jsonrpc":"2.0","method":%q,"params":%s,"id":1}`, method, params)
	res, err := http.Post(fmt.Sprintf("http://%s:%d", TestAddr, port), "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	var answer struct {
		Result json.RawMessage         `json:"result"`
		Error  *seqReceiptRPCError9005 `json:"error"`
	}
	require.NoError(t, json.Unmarshal(raw, &answer), string(raw))
	return answer.Result, answer.Error
}

func seqReceiptResult9005(t *testing.T, port int, hash common.Hash) (map[string]any, *evmrpc.SequencerReceiptResult, json.RawMessage) {
	t.Helper()
	raw, rpcErr := callSeqReceipt9005(t, port, evmrpc.SequencerReceiptMethod, hash)
	require.Nil(t, rpcErr)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	var result *evmrpc.SequencerReceiptResult
	require.NoError(t, json.Unmarshal(raw, &result))
	return fields, result, raw
}

func seqReceiptTwoLogs9005() []*types.Log {
	return []*types.Log{
		{Address: seqReceiptLogAddr9005.Hex(), Topics: []string{seqReceiptTopicA9005.Hex(), seqReceiptTopicB9005.Hex()}, Data: []byte("transfer")},
		{Address: seqReceiptTo9005.Hex(), Topics: []string{}, Data: nil},
	}
}

func TestSequencerReceiptRungs(t *testing.T) {
	chain := newSeqReceiptChain9005(t)
	port := startSeqReceiptServer9005(t, app.Setup(t, false, false, false), chain.ctxProvider, evmrpc.SequencerReceiptOption{Source: chain, Keeper: &chain.keeper})

	unknown, rpcErr := callSeqReceipt9005(t, port, evmrpc.SequencerReceiptMethod, common.HexToHash("0x99"))
	require.Nil(t, rpcErr)
	require.Equal(t, "null", string(unknown))

	submitted := seqReceiptTxHash9005(0, 9)
	chain.submit9005(submitted, 9)
	fields, result, _ := seqReceiptResult9005(t, port, submitted)
	require.Equal(t, map[string]any{"rung": "submitted", "txHash": submitted.Hex(), "nonce": "0x9"}, fields)
	require.NoError(t, result.Verify())

	committedHash, _ := chain.accept9005(seqReceiptTx9005{nonce: 1, height: 7, txIndex: 1, status: 1, returnData: []byte{0x01}, logs: seqReceiptTwoLogs9005()})
	chain.commitBatch9005(7, 1)
	receiptedHash, _ := chain.accept9005(seqReceiptTx9005{nonce: 2, height: 8, txIndex: 1, status: 1, returnData: []byte{0x02}})

	fields, result, _ = seqReceiptResult9005(t, port, receiptedHash)
	require.Equal(t, "receipted", fields["rung"])
	require.NotContains(t, fields, "blockHash")
	require.NotContains(t, fields, "blockNumber")
	require.Equal(t, "0x8", fields["height"])
	require.NoError(t, result.Verify())
	receiptedBytes := []byte(result.Receipt)
	verified, err := verify.ReceiptSignature(receiptedBytes, chain.receiptKey.PublicKey())
	require.NoError(t, err)
	require.Equal(t, [32]byte(receiptedHash), verified.Receipt.ActivityID)

	fields, result, _ = seqReceiptResult9005(t, port, committedHash)
	require.Equal(t, "committed", fields["rung"])
	require.Equal(t, "0x7", fields["blockNumber"])
	require.Equal(t, TestBlockHash, fields["blockHash"])
	require.NoError(t, result.Verify())

	chain.commitBatch9005(8, 2)
	fields, result, _ = seqReceiptResult9005(t, port, receiptedHash)
	require.Equal(t, "committed", fields["rung"])
	require.Equal(t, "0x8", fields["blockNumber"])
	require.Equal(t, TestBlockHash, fields["blockHash"])
	require.Equal(t, receiptedBytes, []byte(result.Receipt), "receipt bytes are identical at the receipted and committed rungs")
	require.NoError(t, result.Verify())
}

func TestStandardReceiptStaysCommittedOnly(t *testing.T) {
	chain := newSeqReceiptChain9005(t)
	port := startSeqReceiptServer9005(t, app.Setup(t, false, false, false), chain.ctxProvider, evmrpc.SequencerReceiptOption{Source: chain, Keeper: &chain.keeper})
	hash, _ := chain.accept9005(seqReceiptTx9005{nonce: 1, height: 7, txIndex: 1, status: 1, returnData: []byte{0x0a}})
	chain.submit9005(seqReceiptTxHash9005(0, 2), 2)

	fields, _, _ := seqReceiptResult9005(t, port, hash)
	require.Equal(t, "receipted", fields["rung"])
	standard := sendRequestGood(t, "getTransactionReceipt", hash.Hex())
	require.Nil(t, standard["error"])
	require.Contains(t, standard, "result")
	require.Nil(t, standard["result"], "eth_getTransactionReceipt never answers a receipted transaction")

	submittedStandard := sendRequestGood(t, "getTransactionReceipt", seqReceiptTxHash9005(0, 2).Hex())
	require.Nil(t, submittedStandard["error"])
	require.Nil(t, submittedStandard["result"], "eth_getTransactionReceipt never answers a submitted transaction")

	chain.commitBatch9005(7, 1)
	fields, _, _ = seqReceiptResult9005(t, port, hash)
	require.Equal(t, "committed", fields["rung"])
	require.Nil(t, sendRequestGood(t, "getTransactionReceipt", hash.Hex())["result"],
		"the sequencer rung never feeds eth_getTransactionReceipt; only the committed EVM receipt does")

	committed := sendRequestGood(t, "getTransactionReceipt", tx1.Hash().Hex())
	require.Nil(t, committed["error"])
	receipt, ok := committed["result"].(map[string]any)
	require.True(t, ok, "a committed transaction keeps its standard receipt")
	require.Equal(t, TestBlockHash, receipt["blockHash"])
	require.Equal(t, "0x8", receipt["blockNumber"])
	require.Equal(t, tx1.Hash().Hex(), receipt["transactionHash"])
}

func TestSequencerReceiptJSONMatchesBytes(t *testing.T) {
	chain := newSeqReceiptChain9005(t)
	port := startSeqReceiptServer9005(t, app.Setup(t, false, false, false), chain.ctxProvider, evmrpc.SequencerReceiptOption{Source: chain, Keeper: &chain.keeper})
	returnData := []byte{0xde, 0xad, 0xbe, 0xef}
	hash, seq := chain.accept9005(seqReceiptTx9005{nonce: 4, height: 7, txIndex: 3, status: 1, returnData: returnData, logs: seqReceiptTwoLogs9005()})
	revertedHash, _ := chain.accept9005(seqReceiptTx9005{nonce: 5, height: 7, txIndex: 4, status: 0, returnData: []byte("revert"), vmError: "execution reverted", create: true})

	fields, result, _ := seqReceiptResult9005(t, port, hash)
	require.NoError(t, result.Verify())
	require.Equal(t, evmrpc.RungReceipted, result.Rung)
	require.Equal(t, hash, result.TxHash)
	require.Equal(t, uint64(4), uint64(result.Nonce))
	require.Equal(t, seq, uint64(result.Seq))
	require.Equal(t, uint64(7), uint64(result.Height))
	require.Equal(t, uint(3), uint(result.Index))
	require.Equal(t, uint64(1_700_000_000_000+7*400), uint64(result.Timestamp))
	require.Equal(t, uint64(1), uint64(result.Status))
	require.Equal(t, returnData, []byte(result.ReturnData))
	require.Equal(t, uint64(21_750), uint64(result.GasUsed))
	require.Equal(t, uint64(63_750), uint64(result.CumulativeGasUsed))
	require.Equal(t, uint64(200), uint64(result.EffectiveGasPrice))
	require.Equal(t, seqReceiptFeeDenom9005, result.FeeDenom)
	require.Equal(t, "0x401640", fields["feeAmount"])
	require.Equal(t, seqReceiptSponsor9005, result.Sponsor)
	require.Equal(t, seqReceiptFrom9005, result.From)
	require.Equal(t, seqReceiptTo9005, result.To)
	require.Equal(t, common.Address{}, result.ContractAddress)
	require.Equal(t, "", result.VMError)
	require.Equal(t, common.Hash(chain.keyID), result.KeyID)
	require.Equal(t, chain.sends[hash].Receipt, []byte(result.Receipt))
	require.Equal(t, []evmrpc.SequencerReceiptLog{
		{Address: seqReceiptLogAddr9005, Topics: []common.Hash{seqReceiptTopicA9005, seqReceiptTopicB9005}, Data: []byte("transfer"), LogIndex: 0},
		{Address: seqReceiptTo9005, Topics: []common.Hash{}, Data: []byte{}, LogIndex: 1},
	}, result.Logs)
	verified, err := verify.ReceiptSignature(result.Receipt, chain.receiptKey.PublicKey())
	require.NoError(t, err)
	require.Equal(t, seq, verified.Receipt.GlobalSequence)

	_, reverted, _ := seqReceiptResult9005(t, port, revertedHash)
	require.NoError(t, reverted.Verify())
	require.Equal(t, uint64(0), uint64(reverted.Status))
	require.Equal(t, "execution reverted", reverted.VMError)
	require.Equal(t, seqReceiptCreated9005, reverted.ContractAddress)
	require.Equal(t, common.Address{}, reverted.To)
	require.Empty(t, reverted.Logs)

	tampered := func(edit func(r *evmrpc.SequencerReceiptResult)) error {
		var copyOf *evmrpc.SequencerReceiptResult
		_, _, raw := seqReceiptResult9005(t, port, hash)
		require.NoError(t, json.Unmarshal(raw, &copyOf))
		edit(copyOf)
		return copyOf.Verify()
	}
	for name, edit := range map[string]func(r *evmrpc.SequencerReceiptResult){
		"returnData": func(r *evmrpc.SequencerReceiptResult) { r.ReturnData = []byte{0xde, 0xad, 0xbe, 0xee} },
		"log data":   func(r *evmrpc.SequencerReceiptResult) { r.Logs[0].Data = []byte("transfeR") },
		"log topic":  func(r *evmrpc.SequencerReceiptResult) { r.Logs[0].Topics[1] = seqReceiptTopicA9005 },
		"log index":  func(r *evmrpc.SequencerReceiptResult) { r.Logs[1].LogIndex = 7 },
		"gasUsed":    func(r *evmrpc.SequencerReceiptResult) { r.GasUsed++ },
		"index":      func(r *evmrpc.SequencerReceiptResult) { r.Index = 0 },
		"sponsor":    func(r *evmrpc.SequencerReceiptResult) { r.Sponsor = seqReceiptFrom9005 },
		"vmError":    func(r *evmrpc.SequencerReceiptResult) { r.VMError = "out of gas" },
		"feeAmount":  func(r *evmrpc.SequencerReceiptResult) { r.FeeAmount.ToInt().Add(r.FeeAmount.ToInt(), big.NewInt(1)) },
		"nonce":      func(r *evmrpc.SequencerReceiptResult) { r.Nonce++ },
		"txHash":     func(r *evmrpc.SequencerReceiptResult) { r.TxHash = revertedHash },
		"rung block": func(r *evmrpc.SequencerReceiptResult) { n := r.Height; r.BlockNumber = &n },
	} {
		require.ErrorIs(t, tampered(edit), evmrpc.ErrSequencerReceiptMismatch, name)
	}
	require.ErrorIs(t, tampered(func(r *evmrpc.SequencerReceiptResult) { r.Receipt = r.Receipt[:len(r.Receipt)-1] }), evmrpc.ErrSequencerReceiptInvalid)
}

func TestSequencerReceiptCommittedRungMatchesBatch(t *testing.T) {
	chain := newSeqReceiptChain9005(t)
	port := startSeqReceiptServer9005(t, app.Setup(t, false, false, false), chain.ctxProvider, evmrpc.SequencerReceiptOption{Source: chain, Keeper: &chain.keeper})
	first, seqFirst := chain.accept9005(seqReceiptTx9005{nonce: 1, height: 7, txIndex: 1, status: 1, returnData: []byte{0x01}})
	second, seqSecond := chain.accept9005(seqReceiptTx9005{nonce: 2, height: 7, txIndex: 2, status: 1, returnData: []byte{0x02}})
	chain.commitBatch9005(7, seqSecond)
	chain.commitBatch9005(8, seqSecond)
	reproposed, seqReproposed := chain.accept9005(seqReceiptTx9005{nonce: 3, height: 8, txIndex: 1, status: 1, returnData: []byte{0x03}})
	open, _ := chain.accept9005(seqReceiptTx9005{nonce: 4, height: 10, txIndex: 1, status: 1, returnData: []byte{0x04}})
	chain.commitBatch9005(9, seqReproposed)

	for _, want := range []struct {
		hash   common.Hash
		seq    uint64
		height uint64
	}{{first, seqFirst, 7}, {second, seqSecond, 7}, {reproposed, seqReproposed, 9}} {
		fields, result, _ := seqReceiptResult9005(t, port, want.hash)
		require.Equal(t, "committed", fields["rung"])
		require.Equal(t, want.height, uint64(*result.BlockNumber))
		require.Equal(t, common.HexToHash(TestBlockHash), *result.BlockHash)
		require.NoError(t, result.Verify())
		atBlock, err := chain.keeper.LastSeq(chain.ctxProvider(int64(want.height)))
		require.NoError(t, err)
		require.GreaterOrEqual(t, atBlock, want.seq, "the block's MsgSequenceBatch covers the seq")
		belowBlock, err := chain.keeper.LastSeq(chain.ctxProvider(int64(want.height) - 1))
		require.NoError(t, err)
		require.Less(t, belowBlock, want.seq, "no lower block covers the seq")
	}

	fields, _, _ := seqReceiptResult9005(t, port, open)
	require.Equal(t, "receipted", fields["rung"], "a receipt above the latest block is not committed")

	forkedActivity := seqReceiptTxHash9005(7, 77)
	chain.mu.Lock()
	forkPoint := chain.seqHashes[seqFirst]
	chain.mu.Unlock()
	forked := chain.signReceipt9005(seqReceiptTx9005{nonce: 77, height: 7, txIndex: 5, status: 1}, seqSecond, forkPoint, forkedActivity)
	chain.mu.Lock()
	chain.sends[forkedActivity] = &evmrpc.SequencerSend{Nonce: 77, Receipt: forked, LogData: [][]byte{}}
	chain.mu.Unlock()
	_, rpcErr := callSeqReceipt9005(t, port, evmrpc.SequencerReceiptMethod, forkedActivity)
	require.NotNil(t, rpcErr)
	require.Contains(t, rpcErr.Message, evmrpc.ErrSequencerReceiptCommit.Error())
}

func TestSequencerReceiptForwardedFromFullNode(t *testing.T) {
	chain := newSeqReceiptChain9005(t)
	testApp := app.Setup(t, false, false, false)
	sequencerPort := startSeqReceiptServer9005(t, testApp, chain.ctxProvider, evmrpc.SequencerReceiptOption{Source: chain, Keeper: &chain.keeper})
	sequencerURL, err := url.Parse(fmt.Sprintf("http://%s:%d", TestAddr, sequencerPort))
	require.NoError(t, err)
	fullNodePort := startSeqReceiptServer9005(t, testApp, chain.ctxProvider, evmrpc.SequencerReceiptOption{ForwardURL: sequencerURL})

	committed, _ := chain.accept9005(seqReceiptTx9005{nonce: 1, height: 7, txIndex: 1, status: 1, returnData: []byte{0x01}, logs: seqReceiptTwoLogs9005()})
	chain.commitBatch9005(7, 1)
	receipted, _ := chain.accept9005(seqReceiptTx9005{nonce: 2, height: 8, txIndex: 1, status: 1, returnData: []byte{0x02}})
	submitted := seqReceiptTxHash9005(0, 3)
	chain.submit9005(submitted, 3)

	for _, hash := range []common.Hash{committed, receipted, submitted, common.HexToHash("0x77")} {
		fromSequencer, rpcErr := callSeqReceipt9005(t, sequencerPort, evmrpc.SequencerReceiptMethod, hash)
		require.Nil(t, rpcErr)
		fromFullNode, rpcErr := callSeqReceipt9005(t, fullNodePort, evmrpc.SequencerReceiptMethod, hash)
		require.Nil(t, rpcErr)
		require.JSONEq(t, string(fromSequencer), string(fromFullNode))
	}

	_, honest, raw := seqReceiptResult9005(t, sequencerPort, committed)
	require.NoError(t, honest.Verify())
	var altered map[string]any
	require.NoError(t, json.Unmarshal(raw, &altered))
	altered["returnData"] = "0x0f"
	lying := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": altered})
	}))
	t.Cleanup(lying.Close)
	lyingURL, err := url.Parse(lying.URL)
	require.NoError(t, err)
	refusingPort := startSeqReceiptServer9005(t, testApp, chain.ctxProvider, evmrpc.SequencerReceiptOption{ForwardURL: lyingURL})
	_, rpcErr := callSeqReceipt9005(t, refusingPort, evmrpc.SequencerReceiptMethod, committed)
	require.NotNil(t, rpcErr)
	require.Contains(t, rpcErr.Message, evmrpc.ErrSequencerReceiptMismatch.Error())

	_, rpcErr = callSeqReceipt9005(t, TestPort, evmrpc.SequencerReceiptMethod, committed)
	require.NotNil(t, rpcErr)
	require.Equal(t, -32000, rpcErr.Code)
	require.Equal(t, "pax_sequencerReceipt served by sequencer", rpcErr.Message)

	_, err = evmrpc.NewEVMHTTPServer(evmrpcconfig.DefaultConfig, &MockClient{}, EVMKeeper, testApp.BeginBlockKeepers, testApp.BaseApp,
		testApp.TracerAnteHandler, chain.ctxProvider, func(int64) client.TxConfig { return TxConfig }, "", nil,
		evmrpc.SequencerReceiptOption{Source: chain, Keeper: &chain.keeper, ForwardURL: sequencerURL})
	require.True(t, errors.Is(err, evmrpc.ErrSequencerReceiptOption))
	_, err = evmrpc.NewEVMHTTPServer(evmrpcconfig.DefaultConfig, &MockClient{}, EVMKeeper, testApp.BeginBlockKeepers, testApp.BaseApp,
		testApp.TracerAnteHandler, chain.ctxProvider, func(int64) client.TxConfig { return TxConfig }, "", nil,
		evmrpc.SequencerReceiptOption{Source: chain})
	require.True(t, errors.Is(err, evmrpc.ErrSequencerReceiptOption))
}
