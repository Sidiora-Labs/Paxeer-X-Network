package evmrpc_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	seqnode "github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

var updateSequencerReceiptVectors = flag.Bool("update", false, "rewrite platform/sdk/conformance/fixtures/sequencer-receipt-v1.json")

const (
	seqVecName         = "sequencer-receipt-v1"
	seqVecFixture      = "../platform/sdk/conformance/fixtures/sequencer-receipt-v1.json"
	seqVecGenerator    = "rpc/sequencer_receipt_vectors_test.go"
	seqVecCommand      = "go test -count=1 ./rpc/ -run TestSequencerReceiptVectors -update"
	seqVecChainID      = 125
	seqVecModuleID     = seqnode.KernelReservedModules + 1
	seqVecFirstHeight  = 1000
	seqVecLastHeight   = 1999
	seqVecBaseFee      = 1_000_000_000
	seqVecTipCap       = 1_000_000_000
	seqVecFeeCap       = 3_000_000_000
	seqVecTimeBase     = 1_750_000_000_000
	seqVecBlockMs      = 400
	seqVecInsufficient = "LXP_INSUFFICIENT_BALANCE"
)

var (
	seqVecSenderSecret = common.FromHex("0x8f2a55949038a9610f50fb23b5883af3b4ecb3c3bb792cbcefbd1542c692be63")
	seqVecReceiptSeed  = bytes.Repeat([]byte{0x5e}, ed25519.SeedSize)
	seqVecKeyID        = [32]byte(bytes.Repeat([]byte{0x6b}, 32))
	seqVecRogueKeyID   = [32]byte(bytes.Repeat([]byte{0x6c}, 32))
	seqVecRecipient    = common.HexToAddress("0x8ba1f109551bd432803012645ac136ddd64dba72")
	seqVecToken        = common.HexToAddress("0x5fbdb2315678afecb367f032d93f642f64180aa3")
	seqVecSpender      = common.HexToAddress("0x70997970c51812dc3a010c7d01b50e0d17dc79c8")
	seqVecBridgeTo     = common.HexToAddress("0x3c44cdddb6a900fa2b585dd299e03d12fa4293bc")
	seqVecLedger       = common.HexToAddress("0x000000000000000000000000000000000000101A")
	seqVecBridge       = common.HexToAddress("0x0000000000000000000000000000000000001016")
	seqVecOnePAX       = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	seqVecStartBalance = new(big.Int).Mul(big.NewInt(100), seqVecOnePAX)

	errSeqVecRung     = errors.New("R1: answer is not receipted or committed with receipt bytes")
	errSeqVecKeyID    = errors.New("R3: keyId is not in the pinned ReceiptKeys")
	errSeqVecHeight   = errors.New("R3: batch_id height is outside the pinned key range")
	errSeqVecActivity = errors.New("R4: activity_id is not keccak256 of the raw transaction")
	errSeqVecSeqHash  = errors.New("R6: seqHash does not chain prevSeqHash, seq and activity_id")
)

type seqVecPin struct {
	KeyID       common.Hash   `json:"keyId"`
	PublicKey   hexutil.Bytes `json:"publicKey"`
	FirstHeight uint64        `json:"firstHeight"`
	LastHeight  uint64        `json:"lastHeight"`
}

type seqVecExpected struct {
	Rung      string `json:"rung,omitempty"`
	RefusedAt string `json:"refusedAt,omitempty"`
	Check     string `json:"check,omitempty"`
	Verifier  string `json:"verifier"`
}

type seqVecCase struct {
	ID             string          `json:"id"`
	Vector         string          `json:"vector"`
	Description    string          `json:"description"`
	RawTransaction hexutil.Bytes   `json:"rawTransaction"`
	ReceiptJSON    json.RawMessage `json:"receiptJson"`
	SignedReceipt  hexutil.Bytes   `json:"signedReceipt"`
	ReceiptDigest  common.Hash     `json:"receiptDigest"`
	PrevSeqHash    common.Hash     `json:"prevSeqHash"`
	PinnedKey      seqVecPin       `json:"pinnedKey"`
	LXPCode        string          `json:"lxpCode,omitempty"`
	Expected       seqVecExpected  `json:"expected"`
}

type seqVecProvenance struct {
	Generator string `json:"generator"`
	Command   string `json:"command"`
}

type seqVecFile struct {
	Name       string           `json:"name"`
	Provenance seqVecProvenance `json:"provenance"`
	ChainID    uint64           `json:"chainId"`
	ModuleID   uint16           `json:"moduleId"`
	FeeDenom   string           `json:"feeDenom"`
	Cases      []seqVecCase     `json:"cases"`
}

// seqVecSource is the sequencer send record the vectors are served from.
type seqVecSource struct {
	mu    sync.Mutex
	sends map[common.Hash]*evmrpc.SequencerSend
}

func (s *seqVecSource) SequencerSend(txHash common.Hash) (*evmrpc.SequencerSend, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sends[txHash], nil
}

func (s *seqVecSource) put(txHash common.Hash, send *evmrpc.SequencerSend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sends[txHash] = send
}

// seqVecTx is one EIP-1559 transaction of the vectors and its execution result.
type seqVecTx struct {
	nonce      uint64
	to         common.Address
	value      *big.Int
	data       []byte
	gasLimit   uint64
	gasUsed    uint64
	status     uint32
	returnData []byte
	logs       []*evmtypes.Log
	vmError    string
}

// seqVecSlot is the sequence position and open block of one receipt.
type seqVecSlot struct {
	seq    uint64
	prev   [32]byte
	height int64
	index  int
}

// seqVecSigned is one receipt as the node signer produced it.
type seqVecSigned struct {
	raw      []byte
	activity common.Hash
	unsigned []byte
	signed   []byte
	digest   [32]byte
	seqHash  [32]byte
	send     *evmrpc.SequencerSend
}

// seqVecBuilder signs the vector transactions and their receipts and tracks the sender balance
// and the cumulative gas of each open block along the accepted chain.
type seqVecBuilder struct {
	t          *testing.T
	sender     common.Address
	ethSigner  ethtypes.Signer
	balance    *big.Int
	cumulative map[int64]uint64
}

func newSeqVecBuilder(t *testing.T) *seqVecBuilder {
	t.Helper()
	key, err := crypto.ToECDSA(seqVecSenderSecret)
	require.NoError(t, err)
	return &seqVecBuilder{
		t:          t,
		sender:     crypto.PubkeyToAddress(key.PublicKey),
		ethSigner:  ethtypes.NewLondonSigner(big.NewInt(seqVecChainID)),
		balance:    new(big.Int).Set(seqVecStartBalance),
		cumulative: map[int64]uint64{},
	}
}

func (b *seqVecBuilder) signTx(tx seqVecTx) *ethtypes.Transaction {
	b.t.Helper()
	key, err := crypto.ToECDSA(seqVecSenderSecret)
	require.NoError(b.t, err)
	to := tx.to
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   big.NewInt(seqVecChainID),
		Nonce:     tx.nonce,
		GasTipCap: big.NewInt(seqVecTipCap),
		GasFeeCap: big.NewInt(seqVecFeeCap),
		Gas:       tx.gasLimit,
		To:        &to,
		Value:     tx.value,
		Data:      tx.data,
	}), b.ethSigner, key)
	require.NoError(b.t, err)
	sender, err := ethtypes.Sender(b.ethSigner, signed)
	require.NoError(b.t, err)
	require.Equal(b.t, b.sender, sender)
	return signed
}

// receipt executes nothing: it encodes the given execution result of signedTx with BuildReceipt,
// signs it with key and checks the signed bytes against the unsigned encoding. advance moves the
// sender balance and the block's cumulative gas forward, as an accepted receipt does.
func (b *seqVecBuilder) receipt(key *seqnode.ReceiptKey, keyID [32]byte, signedTx *ethtypes.Transaction, tx seqVecTx, slot seqVecSlot, advance bool) seqVecSigned {
	b.t.Helper()
	raw, err := signedTx.MarshalBinary()
	require.NoError(b.t, err)
	activity := signedTx.Hash()
	require.Equal(b.t, crypto.Keccak256Hash(raw), activity)
	tip, err := signedTx.EffectiveGasTip(big.NewInt(seqVecBaseFee))
	require.NoError(b.t, err)
	price := new(big.Int).Add(tip, big.NewInt(seqVecBaseFee))
	require.True(b.t, price.IsUint64())
	fee := new(big.Int).Mul(new(big.Int).SetUint64(tx.gasUsed), price)
	spent := new(big.Int).Set(fee)
	transferred := new(big.Int)
	if tx.status == uint32(ethtypes.ReceiptStatusSuccessful) {
		transferred.Set(tx.value)
		spent.Add(spent, tx.value)
	}
	fromAfter := new(big.Int).Sub(b.balance, spent)
	cumulative := b.cumulative[slot.height] + tx.gasUsed
	seqHash := seqReceiptNextHash9005(slot.prev, slot.seq, activity)
	in := &seqnode.ReceiptInput{
		Accepted: &seqnode.Accepted{
			Seq:         slot.seq,
			PrevSeqHash: slot.prev,
			SeqHash:     seqHash,
			ActivityID:  activity,
			Height:      slot.height,
			TxIndex:     slot.index,
			TimestampMs: seqVecTimeBase + slot.height*seqVecBlockMs,
		},
		EVM: &evmtypes.Receipt{
			Status:            tx.status,
			GasUsed:           tx.gasUsed,
			CumulativeGasUsed: cumulative,
			EffectiveGasPrice: price.Uint64(),
			To:                tx.to.Hex(),
			Logs:              tx.logs,
			VmError:           tx.vmError,
		},
		Operation:         signedTx.Type(),
		ReturnData:        tx.returnData,
		FeeDenom:          evmkeeper.BaseDenom,
		FeeCharged:        fee,
		Amount:            tx.value,
		From:              b.sender,
		FromBalanceBefore: new(big.Int).Set(b.balance),
		FromBalanceAfter:  fromAfter,
		FromSequence:      signedTx.Nonce(),
		To:                tx.to,
		ToBalanceBefore:   new(big.Int),
		ToBalanceAfter:    transferred,
		ModuleID:          seqVecModuleID,
	}
	unsigned, err := seqnode.BuildReceipt(in, keyID)
	require.NoError(b.t, err)
	signed, err := key.SignReceipt(in)
	require.NoError(b.t, err)
	digest := codec.ReceiptDigest(unsigned)
	require.Equal(b.t, unsigned[:len(unsigned)-1], signed[:len(unsigned)-1], "the signed receipt extends the BuildReceipt encoding")
	require.Equal(b.t, []byte{1, 0, 0, 0, ed25519.SignatureSize}, signed[len(unsigned)-1:len(unsigned)+4])
	public := key.PublicKey()
	require.True(b.t, ed25519.Verify(public[:], digest[:], signed[len(unsigned)+4:]))
	require.Len(b.t, signed, len(unsigned)+4+ed25519.SignatureSize)
	if advance {
		b.balance, b.cumulative[slot.height] = fromAfter, cumulative
	}
	logData := make([][]byte, len(tx.logs))
	for i, log := range tx.logs {
		logData[i] = log.Data
	}
	return seqVecSigned{
		raw:      raw,
		activity: activity,
		unsigned: unsigned,
		signed:   signed,
		digest:   digest,
		seqHash:  seqHash,
		send: &evmrpc.SequencerSend{
			Nonce:      signedTx.Nonce(),
			Receipt:    signed,
			ReturnData: tx.returnData,
			LogData:    logData,
			VMError:    tx.vmError,
		},
	}
}

// loadSeqVecKey loads the fixed receipt seed through the node loader with pin as its only entry.
func loadSeqVecKey(t *testing.T, dir string, pin verify.SequencerAuthorization) *seqnode.ReceiptKey {
	t.Helper()
	keyFile := filepath.Join(dir, "receipt_key")
	consensusFile := filepath.Join(dir, "priv_validator_key.json")
	if _, err := os.Stat(keyFile); errors.Is(err, os.ErrNotExist) {
		require.NoError(t, os.WriteFile(keyFile, []byte(hex.EncodeToString(seqVecReceiptSeed)+"\n"), 0o600))
		pv, err := privval.GenFilePV(consensusFile, filepath.Join(dir, "priv_validator_state.json"), "")
		require.NoError(t, err)
		require.NoError(t, pv.Key.Save())
	}
	key, err := seqnode.LoadReceiptKey(keyFile, consensusFile, []verify.SequencerAuthorization{pin}, int64(pin.FirstBatchNumber))
	require.NoError(t, err)
	return key
}

func seqVecWord(v *big.Int) common.Hash { return common.BigToHash(v) }

func seqVecArgs(t *testing.T, types ...string) abi.Arguments {
	t.Helper()
	args := make(abi.Arguments, len(types))
	for i, name := range types {
		typ, err := abi.NewType(name, "", nil)
		require.NoError(t, err)
		args[i] = abi.Argument{Type: typ}
	}
	return args
}

func seqVecSelector(signature string) []byte { return crypto.Keccak256([]byte(signature))[:4] }

func seqVecLog(address common.Address, data []byte, topics ...common.Hash) *evmtypes.Log {
	hexTopics := make([]string, len(topics))
	for i, topic := range topics {
		hexTopics[i] = topic.Hex()
	}
	return &evmtypes.Log{Address: address.Hex(), Topics: hexTopics, Data: data}
}

// seqVecApprove is an ERC-20 approve of spender with its Approval log.
func seqVecApprove(t *testing.T, owner common.Address, nonce uint64) seqVecTx {
	t.Helper()
	selector := seqVecSelector("approve(address,uint256)")
	require.Equal(t, common.FromHex("0x095ea7b3"), selector)
	allowance := new(big.Int).Mul(big.NewInt(250), seqVecOnePAX)
	args, err := seqVecArgs(t, "address", "uint256").Pack(seqVecSpender, allowance)
	require.NoError(t, err)
	approval := crypto.Keccak256Hash([]byte("Approval(address,address,uint256)"))
	require.Equal(t, common.HexToHash("0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"), approval)
	return seqVecTx{
		nonce: nonce, to: seqVecToken, value: new(big.Int), data: append(selector, args...),
		gasLimit: 60_000, gasUsed: 46_220, status: uint32(ethtypes.ReceiptStatusSuccessful),
		returnData: seqVecWord(big.NewInt(1)).Bytes(),
		logs: []*evmtypes.Log{seqVecLog(seqVecToken, seqVecWord(allowance).Bytes(),
			approval, common.BytesToHash(owner.Bytes()), common.BytesToHash(seqVecSpender.Bytes()))},
	}
}

type seqVecPayExact struct {
	from, to, asset, reference common.Hash
	amount                     *big.Int
	expiresAt                  uint64
}

// seqVecPayExactCall is the layerxledger payExact calldata of address-table.md a2.
func seqVecPayExactCall(t *testing.T, pay seqVecPayExact) []byte {
	t.Helper()
	selector := seqVecSelector("payExact(bytes32,bytes32,bytes32,uint128,uint64,bytes32)")
	require.Equal(t, common.FromHex("0x78ea11db"), selector)
	args, err := seqVecArgs(t, "bytes32", "bytes32", "bytes32", "uint128", "uint64", "bytes32").
		Pack([32]byte(pay.from), [32]byte(pay.to), [32]byte(pay.asset), pay.amount, pay.expiresAt, [32]byte(pay.reference))
	require.NoError(t, err)
	return append(selector, args...)
}

func seqVecLedgerPay() seqVecPayExact {
	return seqVecPayExact{
		from:      crypto.Keccak256Hash([]byte("vector payer account")),
		to:        crypto.Keccak256Hash([]byte("vector payee account")),
		asset:     crypto.Keccak256Hash([]byte("vector ledger asset")),
		reference: crypto.Keccak256Hash([]byte("vector payment reference")),
		amount:    big.NewInt(1_000_000),
		expiresAt: seqVecTimeBase + 3_600_000,
	}
}

// seqVecLedgerPayment is a layerxledger payExact with its PaymentExact log.
func seqVecLedgerPayment(t *testing.T, nonce uint64) seqVecTx {
	t.Helper()
	pay := seqVecLedgerPay()
	data, err := seqVecArgs(t, "bytes32", "uint128").Pack([32]byte(pay.asset), pay.amount)
	require.NoError(t, err)
	topic := crypto.Keccak256Hash([]byte("PaymentExact(bytes32,bytes32,bytes32,bytes32,uint128)"))
	return seqVecTx{
		nonce: nonce, to: seqVecLedger, value: new(big.Int), data: seqVecPayExactCall(t, pay),
		gasLimit: 90_000, gasUsed: 61_340, status: uint32(ethtypes.ReceiptStatusSuccessful),
		logs: []*evmtypes.Log{seqVecLog(seqVecLedger, data, topic, pay.from, pay.to, pay.reference)},
	}
}

// seqVecLedgerRevert is a layerxledger payExact refused with LXP_INSUFFICIENT_BALANCE.
func seqVecLedgerRevert(t *testing.T, nonce uint64) seqVecTx {
	t.Helper()
	pay := seqVecLedgerPay()
	pay.amount = new(big.Int).Lsh(big.NewInt(1), 100)
	pay.reference = crypto.Keccak256Hash([]byte("vector unfunded payment reference"))
	return seqVecTx{
		nonce: nonce, to: seqVecLedger, value: new(big.Int), data: seqVecPayExactCall(t, pay),
		gasLimit: 90_000, gasUsed: 33_120, status: uint32(ethtypes.ReceiptStatusFailed),
		returnData: pcommon.LXPRevert(seqVecInsufficient, ""), vmError: "execution reverted",
	}
}

// seqVecBridgeOut is a layerxbridge bridgeOut with its BridgeOut log, from the precompile ABI.
func seqVecBridgeOut(t *testing.T, nonce uint64) seqVecTx {
	t.Helper()
	raw, err := os.ReadFile("../precompiles/layerxbridge/abi.json")
	require.NoError(t, err)
	bridge, err := abi.JSON(bytes.NewReader(raw))
	require.NoError(t, err)
	const remoteChain, bridgeNonce = uint64(1), uint64(7)
	amount := new(big.Int).Mul(big.NewInt(5), seqVecOnePAX)
	data, err := bridge.Pack("bridgeOut", remoteChain, seqVecToken, amount, seqVecBridgeTo)
	require.NoError(t, err)
	returnData, err := bridge.Methods["bridgeOut"].Outputs.Pack(bridgeNonce)
	require.NoError(t, err)
	event := bridge.Events["BridgeOut"]
	require.Equal(t, crypto.Keccak256Hash([]byte("BridgeOut(uint64,address,uint256,address,uint64)")), event.ID)
	logData, err := event.Inputs.NonIndexed().Pack(amount, seqVecBridgeTo)
	require.NoError(t, err)
	return seqVecTx{
		nonce: nonce, to: seqVecBridge, value: new(big.Int), data: data,
		gasLimit: 120_000, gasUsed: 78_512, status: uint32(ethtypes.ReceiptStatusSuccessful),
		returnData: returnData,
		logs: []*evmtypes.Log{seqVecLog(seqVecBridge, logData, event.ID,
			seqVecWord(new(big.Int).SetUint64(remoteChain)), common.BytesToHash(seqVecToken.Bytes()),
			seqVecWord(new(big.Int).SetUint64(bridgeNonce)))},
	}
}

func seqVecTransfer(nonce uint64, paxUnits int64) seqVecTx {
	return seqVecTx{
		nonce: nonce, to: seqVecRecipient, value: new(big.Int).Mul(big.NewInt(paxUnits), seqVecOnePAX),
		gasLimit: 21_000, gasUsed: 21_000, status: uint32(ethtypes.ReceiptStatusSuccessful),
	}
}

// seqVecServe serves signed through pax_sequencerReceipt and returns the JSON answer.
func seqVecServe(t *testing.T, port int, source *seqVecSource, signed seqVecSigned) json.RawMessage {
	t.Helper()
	source.put(signed.activity, signed.send)
	raw, rpcErr := callSeqReceipt9005(t, port, evmrpc.SequencerReceiptMethod, signed.activity)
	require.Nil(t, rpcErr)
	var result *evmrpc.SequencerReceiptResult
	require.NoError(t, json.Unmarshal(raw, &result))
	require.NotNil(t, result)
	require.Equal(t, evmrpc.RungReceipted, result.Rung)
	require.NoError(t, result.Verify())
	require.Equal(t, signed.signed, []byte(result.Receipt))
	return raw
}

// seqVecEdit re-renders a JSON answer after edit changes it, as a tampering transport would.
func seqVecEdit(t *testing.T, raw json.RawMessage, edit func(*evmrpc.SequencerReceiptResult)) json.RawMessage {
	t.Helper()
	var result *evmrpc.SequencerReceiptResult
	require.NoError(t, json.Unmarshal(raw, &result))
	edit(result)
	out, err := json.Marshal(result)
	require.NoError(t, err)
	return out
}

// seqVecClientCheck runs client checks R1-R6 of clients.md (c) and returns the first check that
// refuses with its error, or "" when the answer holds the receipted rung.
func seqVecClientCheck(raw []byte, answer json.RawMessage, prev common.Hash, pin seqVecPin) (string, error) {
	var result *evmrpc.SequencerReceiptResult
	if err := json.Unmarshal(answer, &result); err != nil {
		return "R1", err
	}
	if result == nil || (result.Rung != evmrpc.RungReceipted && result.Rung != evmrpc.RungCommitted) ||
		result.SequencerReceiptView == nil || len(result.Receipt) == 0 {
		return "R1", errSeqVecRung
	}
	decoded, err := codec.DecodeReceipt(result.Receipt)
	if err != nil {
		return "R2", err
	}
	if common.Hash(decoded.ContextHash) != pin.KeyID {
		return "R3", errSeqVecKeyID
	}
	height := binary.BigEndian.Uint64(decoded.BatchID[24:])
	if !bytes.Equal(decoded.BatchID[:24], make([]byte, 24)) || height < pin.FirstHeight || height > pin.LastHeight {
		return "R3", errSeqVecHeight
	}
	if _, err := verify.ReceiptSignature(result.Receipt, [32]byte(pin.PublicKey)); err != nil {
		return "R2", err
	}
	activity := crypto.Keccak256Hash(raw)
	if common.Hash(decoded.ActivityID) != activity || result.TxHash != activity {
		return "R4", errSeqVecActivity
	}
	if err := result.Verify(); err != nil {
		return "R5", err
	}
	if common.Hash(decoded.PreviousStateRoot) != prev ||
		decoded.ResultingStateRoot != seqReceiptNextHash9005(prev, decoded.GlobalSequence, decoded.ActivityID) {
		return "R6", errSeqVecSeqHash
	}
	return "", nil
}

type seqVecRefusal struct {
	refusedAt string
	err       error
	verifier  string
}

func seqVecCaseOf(id, vector, description string, signed seqVecSigned, raw []byte, answer json.RawMessage, prev [32]byte, pin seqVecPin) seqVecCase {
	return seqVecCase{
		ID:             id,
		Vector:         vector,
		Description:    description,
		RawTransaction: raw,
		ReceiptJSON:    answer,
		SignedReceipt:  signed.signed,
		ReceiptDigest:  signed.digest,
		PrevSeqHash:    prev,
		PinnedKey:      pin,
	}
}

func TestSequencerReceiptVectors(t *testing.T) {
	dir := t.TempDir()
	public := [32]byte(ed25519.NewKeyFromSeed(seqVecReceiptSeed).Public().(ed25519.PublicKey))
	pin := seqVecPin{KeyID: seqVecKeyID, PublicKey: public[:], FirstHeight: seqVecFirstHeight, LastHeight: seqVecLastHeight}
	primary := loadSeqVecKey(t, dir, verify.SequencerAuthorization{SequencerID: seqVecKeyID, PublicKey: public, FirstBatchNumber: seqVecFirstHeight, LastBatchNumber: seqVecLastHeight})
	rogue := loadSeqVecKey(t, dir, verify.SequencerAuthorization{SequencerID: seqVecRogueKeyID, PublicKey: public, FirstBatchNumber: seqVecFirstHeight, LastBatchNumber: seqVecLastHeight})
	extended := loadSeqVecKey(t, dir, verify.SequencerAuthorization{SequencerID: seqVecKeyID, PublicKey: public, FirstBatchNumber: seqVecLastHeight + 1, LastBatchNumber: 2 * seqVecLastHeight})

	chain := newSeqReceiptChain9005(t)
	source := &seqVecSource{sends: map[common.Hash]*evmrpc.SequencerSend{}}
	port := startSeqReceiptServer9005(t, app.Setup(t, false, false, false), chain.ctxProvider,
		evmrpc.SequencerReceiptOption{Source: source, Keeper: &chain.keeper})

	b := newSeqVecBuilder(t)
	var cases []seqVecCase
	expected := map[string]seqVecRefusal{}
	prev := [32]byte{}
	accept := func(id, vector, description string, tx seqVecTx, height int64, index int) (seqVecSigned, json.RawMessage, [32]byte) {
		signedTx := b.signTx(tx)
		slot := seqVecSlot{seq: uint64(len(cases) + 1), prev: prev, height: height, index: index}
		signed := b.receipt(primary, seqVecKeyID, signedTx, tx, slot, true)
		answer := seqVecServe(t, port, source, signed)
		c := seqVecCaseOf(id, vector, description, signed, signed.raw, answer, prev, pin)
		c.Expected = seqVecExpected{Rung: string(evmrpc.RungReceipted), Verifier: "accepts"}
		cases = append(cases, c)
		before := prev
		prev = signed.seqHash
		return signed, answer, before
	}

	transfer, transferJSON, transferPrev := accept("V1-pax-transfer", "V1", "PAX transfer to an externally owned account", seqVecTransfer(0, 1), seqVecFirstHeight, 1)
	approve, approveJSON, approvePrev := accept("V1-erc20-approve", "V1", "ERC-20 approve with one Approval log", seqVecApprove(t, b.sender, 1), seqVecFirstHeight, 2)
	accept("V1-layerxledger-pay-exact", "V1", "layerxledger payExact at 0x101A with one PaymentExact log", seqVecLedgerPayment(t, 2), seqVecFirstHeight+1, 1)
	accept("V1-layerxbridge-bridge-out", "V1", "layerxbridge bridgeOut at 0x1016 with one BridgeOut log", seqVecBridgeOut(t, 3), seqVecFirstHeight+1, 2)
	accept("V2-layerxledger-revert", "V2", "layerxledger payExact reverted with Error(string) LXP_INSUFFICIENT_BALANCE", seqVecLedgerRevert(t, 4), seqVecFirstHeight+2, 1)
	cases[len(cases)-1].LXPCode = seqVecInsufficient

	refuse := func(id, description string, c seqVecCase, refusal seqVecRefusal) {
		c.ID, c.Vector, c.Description = id, "V3", description
		c.Expected = seqVecExpected{RefusedAt: refusal.refusedAt, Check: refusal.err.Error(), Verifier: refusal.verifier}
		expected[id] = refusal
		cases = append(cases, c)
	}
	next := seqVecSlot{seq: uint64(len(cases) + 1), prev: prev, height: seqVecFirstHeight + 3, index: 1}
	nextCase := func(key *seqnode.ReceiptKey, keyID [32]byte, tx seqVecTx, slot seqVecSlot) seqVecCase {
		signedTx := b.signTx(tx)
		signed := b.receipt(key, keyID, signedTx, tx, slot, false)
		return seqVecCaseOf("", "", "", signed, signed.raw, seqVecServe(t, port, source, signed), next.prev, pin)
	}

	refuse("V3-key-not-pinned", "receipt signed under a keyId outside the pinned ReceiptKeys",
		nextCase(rogue, seqVecRogueKeyID, seqVecTransfer(5, 2), next),
		seqVecRefusal{refusedAt: "R3", err: errSeqVecKeyID, verifier: "SequencerSignature"})

	outOfRange := next
	outOfRange.height = seqVecLastHeight + 1
	refuse("V3-height-outside-key-range", "receipt at a height above the pinned key's lastHeight",
		nextCase(extended, seqVecKeyID, seqVecTransfer(5, 3), outOfRange),
		seqVecRefusal{refusedAt: "R3", err: errSeqVecHeight, verifier: "SequencerSignature"})

	sentTx := b.signTx(seqVecTransfer(5, 4))
	sentRaw, err := sentTx.MarshalBinary()
	require.NoError(t, err)
	substituted := nextCase(primary, seqVecKeyID, seqVecTransfer(5, 5), next)
	require.NotEqual(t, crypto.Keccak256(sentRaw), crypto.Keccak256(substituted.RawTransaction))
	substituted.RawTransaction = sentRaw
	refuse("V3-activity-id-not-raw-hash", "receipt whose activity_id is not keccak256 of the raw transaction the client signed",
		substituted, seqVecRefusal{refusedAt: "R4", err: errSeqVecActivity, verifier: "accepts"})

	forked := next
	forked.prev = approvePrev
	refuse("V3-seq-hash-mismatch", "receipt chained from a prevSeqHash other than the client's predecessor",
		nextCase(primary, seqVecKeyID, seqVecTransfer(5, 6), forked),
		seqVecRefusal{refusedAt: "R6", err: errSeqVecSeqHash, verifier: "accepts"})

	tamperedLog := seqVecCaseOf("", "", "", approve, approve.raw, seqVecEdit(t, approveJSON, func(r *evmrpc.SequencerReceiptResult) {
		r.Logs[0].Data = append(bytes.Clone(r.Logs[0].Data[:len(r.Logs[0].Data)-1]), r.Logs[0].Data[len(r.Logs[0].Data)-1]^0x01)
	}), approvePrev, pin)
	refuse("V3-tampered-log", "Approval log data altered in the JSON answer while the signed receipt is intact",
		tamperedLog, seqVecRefusal{refusedAt: "R5", err: evmrpc.ErrSequencerReceiptMismatch, verifier: "accepts"})

	nonCanonical := bytes.Clone(transfer.signed)
	flagAt := len(nonCanonical) - 1 - 4 - ed25519.SignatureSize
	require.Equal(t, byte(1), nonCanonical[flagAt])
	nonCanonical[flagAt] = 2
	mutated := seqVecCaseOf("", "", "", transfer, transfer.raw, seqVecEdit(t, transferJSON, func(r *evmrpc.SequencerReceiptResult) {
		r.Receipt = nonCanonical
	}), transferPrev, pin)
	mutated.SignedReceipt = nonCanonical
	refuse("V3-non-canonical-bytes", "signature presence flag 2, which no canonical receipt encodes",
		mutated, seqVecRefusal{refusedAt: "R2", err: codec.ErrNonCanonical, verifier: "Decode"})

	seqVecAssertCases(t, cases, expected)

	file := seqVecFile{
		Name:       seqVecName,
		Provenance: seqVecProvenance{Generator: seqVecGenerator, Command: seqVecCommand},
		ChainID:    seqVecChainID,
		ModuleID:   seqVecModuleID,
		FeeDenom:   evmkeeper.BaseDenom,
		Cases:      cases,
	}
	rendered, err := json.MarshalIndent(file, "", "  ")
	require.NoError(t, err)
	rendered = append(rendered, '\n')
	if *updateSequencerReceiptVectors {
		require.NoError(t, os.WriteFile(seqVecFixture, rendered, 0o644)) //nolint:gosec // a published conformance fixture
	}
	stored, err := os.ReadFile(seqVecFixture)
	require.NoError(t, err, "run %s to write the fixture", seqVecCommand)
	require.Equal(t, string(stored), string(rendered), "the fixture differs from the node signer output; run %s", seqVecCommand)
}

// seqVecAssertCases checks every V1 and V2 case through R1-R6 and its decoded fields, and every V3
// case refusing at exactly its named check.
func seqVecAssertCases(t *testing.T, cases []seqVecCase, refusals map[string]seqVecRefusal) {
	t.Helper()
	require.Len(t, cases, 11)
	for _, c := range cases {
		check, err := seqVecClientCheck(c.RawTransaction, c.ReceiptJSON, c.PrevSeqHash, c.PinnedKey)
		if c.Vector == "V3" {
			refusal, ok := refusals[c.ID]
			require.True(t, ok, c.ID)
			require.Equal(t, refusal.refusedAt, check, "%s refuses at %s: %v", c.ID, refusal.refusedAt, err)
			require.ErrorIs(t, err, refusal.err, c.ID)
			continue
		}
		require.Equal(t, "", check, "%s: %v", c.ID, err)
		require.NoError(t, err, c.ID)
		seqVecAssertFields(t, c)
	}
}

func seqVecAssertFields(t *testing.T, c seqVecCase) {
	t.Helper()
	var result *evmrpc.SequencerReceiptResult
	require.NoError(t, json.Unmarshal(c.ReceiptJSON, &result))
	verified, err := verify.ReceiptSignature(c.SignedReceipt, [32]byte(c.PinnedKey.PublicKey))
	require.NoError(t, err, c.ID)
	require.Equal(t, c.ReceiptDigest, common.Hash(verified.Digest), c.ID)
	decoded := verified.Receipt
	require.Equal(t, []byte(c.SignedReceipt), []byte(result.Receipt), c.ID)
	require.Equal(t, uint16(2), decoded.ProtocolVersion)
	require.Equal(t, seqVecModuleID, decoded.ModuleID)
	require.Equal(t, uint32(seqnode.ReceiptLayoutVersion), decoded.ModuleVersion)
	require.Equal(t, uint8(ethtypes.DynamicFeeTxType), decoded.Operation)
	require.Equal(t, sha256.Sum256([]byte(evmkeeper.BaseDenom)), decoded.Asset)
	require.Equal(t, evmkeeper.BaseDenom, result.FeeDenom)
	require.Equal(t, c.PinnedKey.KeyID, result.KeyID)
	require.Equal(t, sha256.Sum256(result.ReturnData), decoded.ActivityRoot)
	require.Equal(t, uint64(2_000_000_000), uint64(result.EffectiveGasPrice))
	fee := new(big.Int).Mul(new(big.Int).SetUint64(uint64(result.GasUsed)), big.NewInt(2_000_000_000))
	require.Equal(t, 0, fee.Cmp(result.FeeAmount.ToInt()), c.ID)
	tx := new(ethtypes.Transaction)
	require.NoError(t, tx.UnmarshalBinary(c.RawTransaction))
	require.Equal(t, big.NewInt(seqVecChainID), tx.ChainId())
	require.Equal(t, tx.Nonce(), uint64(result.Nonce))
	require.Equal(t, *tx.To(), result.To)
	require.Equal(t, uint64(seqVecTimeBase)+uint64(result.Height)*seqVecBlockMs, uint64(result.Timestamp))
	switch c.Vector {
	case "V1":
		require.Equal(t, uint64(1), uint64(result.Status), c.ID)
		require.Equal(t, int32(0), decoded.ResultCode)
		require.Empty(t, result.VMError)
		require.Empty(t, c.LXPCode)
		topic, logged := map[string]string{
			"V1-erc20-approve":           "Approval(address,address,uint256)",
			"V1-layerxledger-pay-exact":  "PaymentExact(bytes32,bytes32,bytes32,bytes32,uint128)",
			"V1-layerxbridge-bridge-out": "BridgeOut(uint64,address,uint256,address,uint64)",
		}[c.ID]
		if !logged {
			require.Equal(t, "V1-pax-transfer", c.ID)
			require.Empty(t, result.Logs)
			require.Equal(t, 0, tx.Value().Cmp(seqVecOnePAX))
			break
		}
		require.Len(t, result.Logs, 1, c.ID)
		require.Equal(t, crypto.Keccak256Hash([]byte(topic)), result.Logs[0].Topics[0], c.ID)
		require.Equal(t, *tx.To(), result.Logs[0].Address, c.ID)
	case "V2":
		require.Equal(t, uint64(0), uint64(result.Status), c.ID)
		require.Equal(t, int32(1), decoded.ResultCode)
		require.Equal(t, "execution reverted", result.VMError)
		require.Empty(t, result.Logs)
		reason, err := abi.UnpackRevert(result.ReturnData)
		require.NoError(t, err)
		code, message, found := strings.Cut(reason, ": ")
		require.True(t, found, reason)
		require.Equal(t, c.LXPCode, code)
		require.Equal(t, "insufficient balance", message)
		require.Equal(t, pcommon.LXPRevert(code, ""), []byte(result.ReturnData), "the revert maps through the error code registry")
	default:
		require.Fail(t, fmt.Sprintf("unexpected vector %s", c.Vector))
	}
}
