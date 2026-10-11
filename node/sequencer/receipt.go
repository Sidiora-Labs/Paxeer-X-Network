package sequencer

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	receiptProtocolVersion uint16 = 2
	receiptStructureTag    uint16 = 0x5201
	// ReceiptLayoutVersion is the module_version of the sequencer receipt layout.
	ReceiptLayoutVersion uint32 = 1
	// CosmosOperation is the operation byte of a receipt for a Cosmos transaction.
	CosmosOperation uint8 = 0xFF
	// KernelReservedModules is LXP_MODULE_RESERVED_COUNT; the receipt module id
	// is chosen above it.
	KernelReservedModules uint16 = 11

	effectKindState  uint8 = 1
	effectKindEvent  uint8 = 3
	maxSummaryBody         = 191
	maxLogBody             = 181
	maxFeeDenomBytes       = 64
	maxLogTopics           = 4
)

var (
	// ErrReceiptInput refuses receipt fields the canonical envelope cannot carry.
	ErrReceiptInput = errors.New("sequencer: invalid receipt input")
	// ErrReceiptInvariant reports an accepted transaction whose receipt could
	// not be built from its execution; the transaction is on the open layer.
	ErrReceiptInvariant = errors.New("sequencer: accepted transaction has no receipt")
)

var sponsoredTopic = crypto.Keccak256Hash([]byte("Sponsored(address,address,uint256,uint256)"))

// ReceiptInput is everything one signed receipt carries about an accepted
// transaction.
type ReceiptInput struct {
	Accepted *Accepted
	// EVM supplies status, gas fields, contract address, vmError and logs. For
	// a Cosmos transaction only Status, GasUsed and CumulativeGasUsed are set.
	EVM               *evmtypes.Receipt
	Operation         uint8
	ReturnData        []byte
	FeeDenom          string
	FeeCharged        *big.Int
	Sponsor           common.Address
	Amount            *big.Int
	From              common.Address
	FromBalanceBefore *big.Int
	FromBalanceAfter  *big.Int
	FromSequence      uint64
	To                common.Address
	ToBalanceBefore   *big.Int
	ToBalanceAfter    *big.Int
	AuthorizationHash [32]byte
	ModuleID          uint16
}

func inputErr(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrReceiptInput, fmt.Sprintf(format, args...))
}

func appendArr32(b []byte, v []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, 32)
	return append(b, v...)
}

func appendAddr32(b []byte, a common.Address) []byte {
	var word [32]byte
	copy(word[12:], a[:])
	return appendArr32(b, word[:])
}

func appendU128(b []byte, name string, v *big.Int) ([]byte, error) {
	if v == nil || v.Sign() < 0 || v.BitLen() > 128 {
		return nil, inputErr("%s is not an unsigned 128-bit value", name)
	}
	var out [16]byte
	v.FillBytes(out[:])
	return append(b, out[:]...), nil
}

func appendEffect(b []byte, moduleID, ordinal uint16, kind uint8, body []byte) []byte {
	b = binary.BigEndian.AppendUint16(b, moduleID)
	b = binary.BigEndian.AppendUint16(b, ordinal)
	b = binary.BigEndian.AppendUint16(b, 0)
	b = append(b, kind, 0)
	b = appendArr32(b, make([]byte, 32))
	b = binary.BigEndian.AppendUint32(b, uint32(len(body))) //nolint:gosec // bodies are bounded by maxSummaryBody and maxLogBody
	return append(b, body...)
}

// contractAddress is the created contract of a creation, else zero, as the
// standard receipt reports it.
func contractAddress(r *evmtypes.Receipt) (common.Address, error) {
	if r.To != "" || r.ContractAddress == "" {
		return common.Address{}, nil
	}
	if !common.IsHexAddress(r.ContractAddress) {
		return common.Address{}, inputErr("contract address %q", r.ContractAddress)
	}
	return common.HexToAddress(r.ContractAddress), nil
}

// SummaryEffect is the body of effect 0: gasUsed, cumulativeGasUsed,
// effectiveGasPrice, txIndex, contractAddress, sponsor, sha256(vmError),
// returnData length, log count and the fee denom.
func SummaryEffect(in *ReceiptInput) ([]byte, error) {
	if in == nil || in.Accepted == nil || in.EVM == nil {
		return nil, inputErr("missing accepted transaction or execution receipt")
	}
	if in.Accepted.TxIndex < 0 || uint64(in.Accepted.TxIndex) > math.MaxUint32 {
		return nil, inputErr("tx index %d", in.Accepted.TxIndex)
	}
	if len(in.FeeDenom) == 0 || len(in.FeeDenom) > maxFeeDenomBytes {
		return nil, inputErr("fee denom is %d bytes, want 1..%d", len(in.FeeDenom), maxFeeDenomBytes)
	}
	if uint64(len(in.ReturnData)) > math.MaxUint32 || uint64(len(in.EVM.Logs)) > math.MaxUint32 {
		return nil, inputErr("return data or log count exceeds 32 bits")
	}
	created, err := contractAddress(in.EVM)
	if err != nil {
		return nil, err
	}
	vmErr := sha256.Sum256([]byte(in.EVM.VmError))
	b := make([]byte, 0, maxSummaryBody)
	b = binary.BigEndian.AppendUint64(b, in.EVM.GasUsed)
	b = binary.BigEndian.AppendUint64(b, in.EVM.CumulativeGasUsed)
	b = binary.BigEndian.AppendUint64(b, in.EVM.EffectiveGasPrice)
	b = binary.BigEndian.AppendUint32(b, uint32(in.Accepted.TxIndex))
	b = append(b, created[:]...)
	b = append(b, in.Sponsor[:]...)
	b = append(b, vmErr[:]...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(in.ReturnData)))
	b = binary.BigEndian.AppendUint32(b, uint32(len(in.EVM.Logs)))
	b = append(b, uint8(len(in.FeeDenom)))
	return append(b, in.FeeDenom...), nil
}

func parseLog(log *evmtypes.Log) (common.Address, []common.Hash, error) {
	if log == nil {
		return common.Address{}, nil, inputErr("nil log")
	}
	if !common.IsHexAddress(log.Address) {
		return common.Address{}, nil, inputErr("log address %q", log.Address)
	}
	if len(log.Topics) > maxLogTopics {
		return common.Address{}, nil, inputErr("log has %d topics, at most %d", len(log.Topics), maxLogTopics)
	}
	topics := make([]common.Hash, len(log.Topics))
	for i, t := range log.Topics {
		raw, err := hexutil.Decode(t)
		if err != nil || len(raw) != common.HashLength {
			return common.Address{}, nil, inputErr("log topic %d %q", i, t)
		}
		topics[i] = common.BytesToHash(raw)
	}
	return common.HexToAddress(log.Address), topics, nil
}

// LogEffect is the body of a log effect: address, topic count, topics and
// sha256(data).
func LogEffect(log *evmtypes.Log) ([]byte, error) {
	addr, topics, err := parseLog(log)
	if err != nil {
		return nil, err
	}
	data := sha256.Sum256(log.Data)
	b := make([]byte, 0, maxLogBody)
	b = append(b, addr[:]...)
	b = append(b, uint8(len(topics)))
	for _, t := range topics {
		b = append(b, t[:]...)
	}
	return append(b, data[:]...), nil
}

// encodeReceipt is the canonical envelope up to and excluding the signature flag.
func encodeReceipt(in *ReceiptInput, contextHash [32]byte) ([]byte, error) {
	summary, err := SummaryEffect(in)
	if err != nil {
		return nil, err
	}
	a := in.Accepted
	if a.ActivityID == ([32]byte{}) {
		return nil, inputErr("zero activity id")
	}
	if a.Seq == 0 {
		return nil, inputErr("sequence 0")
	}
	if a.SeqHash != nextSeqHash(a.PrevSeqHash, a.Seq, a.ActivityID) {
		return nil, inputErr("seqHash does not chain prevSeqHash, seq and activity id")
	}
	if a.Height <= 0 || a.TimestampMs <= 0 {
		return nil, inputErr("height %d, timestamp %d", a.Height, a.TimestampMs)
	}
	if in.ModuleID <= KernelReservedModules {
		return nil, inputErr("module id %d is reserved", in.ModuleID)
	}
	var resultCode uint32
	switch in.EVM.Status {
	case uint32(ethtypes.ReceiptStatusSuccessful):
		resultCode = 0
	case uint32(ethtypes.ReceiptStatusFailed):
		resultCode = 1
	default:
		return nil, inputErr("status %d", in.EVM.Status)
	}
	if uint64(len(in.EVM.Logs)) >= math.MaxUint16 {
		return nil, inputErr("%d logs exceed the u16 ordinal", len(in.EVM.Logs))
	}
	activityRoot := sha256.Sum256(in.ReturnData)
	asset := sha256.Sum256([]byte(in.FeeDenom))
	var batchID [32]byte
	binary.BigEndian.PutUint64(batchID[24:], uint64(a.Height))

	b := make([]byte, 0, 1024)
	b = binary.BigEndian.AppendUint16(b, receiptProtocolVersion)
	b = binary.BigEndian.AppendUint16(b, receiptStructureTag)
	b = binary.BigEndian.AppendUint16(b, receiptProtocolVersion)
	b = appendArr32(b, a.ActivityID[:])
	b = binary.BigEndian.AppendUint64(b, a.Seq)
	b = appendArr32(b, a.PrevSeqHash[:])
	b = appendArr32(b, a.SeqHash[:])
	b = appendArr32(b, activityRoot[:])
	b = binary.BigEndian.AppendUint32(b, resultCode)
	b = binary.BigEndian.AppendUint32(b, uint32(len(in.EVM.Logs)+1))
	b = appendEffect(b, in.ModuleID, 0, effectKindState, summary)
	for i, log := range in.EVM.Logs {
		body, err := LogEffect(log)
		if err != nil {
			return nil, err
		}
		b = appendEffect(b, in.ModuleID, uint16(i+1), effectKindEvent, body)
	}
	if b, err = appendU128(b, "fee_charged", in.FeeCharged); err != nil {
		return nil, err
	}
	b = appendArr32(b, batchID[:])
	b = binary.BigEndian.AppendUint16(b, in.ModuleID)
	b = binary.BigEndian.AppendUint32(b, ReceiptLayoutVersion)
	b = binary.BigEndian.AppendUint32(b, 0)
	b = append(b, in.Operation)
	b = appendArr32(b, asset[:])
	if b, err = appendU128(b, "amount", in.Amount); err != nil {
		return nil, err
	}
	b = appendAddr32(b, in.From)
	if b, err = appendU128(b, "from_balance_before", in.FromBalanceBefore); err != nil {
		return nil, err
	}
	if b, err = appendU128(b, "from_balance_after", in.FromBalanceAfter); err != nil {
		return nil, err
	}
	b = binary.BigEndian.AppendUint64(b, in.FromSequence)
	b = appendAddr32(b, in.To)
	if b, err = appendU128(b, "to_balance_before", in.ToBalanceBefore); err != nil {
		return nil, err
	}
	if b, err = appendU128(b, "to_balance_after", in.ToBalanceAfter); err != nil {
		return nil, err
	}
	b = appendArr32(b, make([]byte, 32))
	b = appendArr32(b, in.AuthorizationHash[:])
	b = appendArr32(b, contextHash[:])
	return binary.BigEndian.AppendUint64(b, uint64(a.TimestampMs)), nil
}

// BuildReceipt encodes the unsigned ProtocolReceipt v2 (signature flag 0)
// with contextHash as context_hash.
func BuildReceipt(in *ReceiptInput, contextHash [32]byte) ([]byte, error) {
	b, err := encodeReceipt(in, contextHash)
	if err != nil {
		return nil, err
	}
	return append(b, 0), nil
}

// Receipter accepts transactions on an executor and returns each accepted
// transaction's signed receipt.
type Receipter struct {
	mu         sync.Mutex
	exec       Executor
	host       Host
	evm        *evmkeeper.Keeper
	key        *ReceiptKey
	moduleID   uint16
	height     int64
	cumulative uint64
}

// NewReceipter signs receipts for transactions accepted on exec. It must be
// the only caller of exec.Accept so cumulative gas follows the open block.
func NewReceipter(exec Executor, host Host, evm *evmkeeper.Keeper, key *ReceiptKey, moduleID uint16) (*Receipter, error) {
	if exec == nil || host == nil || evm == nil || key == nil {
		return nil, errors.New("sequencer: receipter needs an executor, host, EVM keeper and receipt key")
	}
	if moduleID <= KernelReservedModules {
		return nil, inputErr("module id %d is reserved", moduleID)
	}
	return &Receipter{exec: exec, host: host, evm: evm, key: key, moduleID: moduleID}, nil
}

// Accept executes txBytes on the open layer and signs its receipt. A
// refusal leaves the open layer unchanged. An error wrapping
// ErrReceiptInvariant comes with the Accepted outcome: the transaction is on
// the open layer but has no receipt.
func (r *Receipter) Accept(txBytes []byte) (*Accepted, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pre, err := r.exec.Snapshot()
	if err != nil {
		return nil, nil, err
	}
	if _, err := r.key.KeyID(pre.BlockHeight()); err != nil {
		return nil, nil, err
	}
	if len(txBytes) == 0 {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, "empty transaction")
	}
	tx, err := r.host.GetTxConfig().TxDecoder()(txBytes)
	if err != nil {
		return nil, nil, sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
	}
	var etx *ethtypes.Transaction
	if msg := r.host.GetEVMMsg(tx); msg != nil {
		etx, _ = msg.AsTransaction()
	}
	in := &ReceiptInput{ModuleID: r.moduleID}
	if etx != nil {
		err = r.beforeEVM(pre, etx, in)
	} else {
		err = beforeCosmos(tx, in)
	}
	if err != nil {
		return nil, nil, err
	}
	acc, err := r.exec.Accept(txBytes)
	if err != nil {
		return nil, nil, err
	}
	in.Accepted = acc
	if acc.Height != r.height {
		r.height, r.cumulative = acc.Height, 0
	}
	if err := r.after(etx, in); err != nil {
		return acc, nil, fmt.Errorf("%w: %v", ErrReceiptInvariant, err)
	}
	signed, err := r.key.SignReceipt(in)
	if err != nil {
		return acc, nil, fmt.Errorf("%w: %v", ErrReceiptInvariant, err)
	}
	return acc, signed, nil
}

func (r *Receipter) balance(ctx sdk.Context, addr common.Address) *big.Int {
	return r.evm.GetBalance(ctx, r.evm.GetPaxAddressOrDefault(ctx, addr))
}

func (r *Receipter) beforeEVM(ctx sdk.Context, etx *ethtypes.Transaction, in *ReceiptInput) error {
	from, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(etx.ChainId()), etx)
	if err != nil {
		return sdkerrors.Wrap(sdkerrors.ErrUnauthorized, err.Error())
	}
	if etx.Value().BitLen() > 128 {
		return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "value exceeds 128 bits")
	}
	to := crypto.CreateAddress(from, etx.Nonce())
	if etx.To() != nil {
		to = *etx.To()
	}
	if etx.Type() == ethtypes.SetCodeTxType {
		enc, err := rlp.EncodeToBytes(etx.SetCodeAuthorizations())
		if err != nil {
			return sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		}
		in.AuthorizationHash = crypto.Keccak256Hash(enc)
	}
	in.Operation = etx.Type()
	in.Amount = etx.Value()
	in.From, in.To = from, to
	in.FromSequence = etx.Nonce()
	in.FromBalanceBefore = r.balance(ctx, from)
	in.ToBalanceBefore = r.balance(ctx, to)
	return nil
}

// beforeCosmos takes the fee of a Cosmos transaction; it carries no EVM
// sender, recipient, value or balances.
func beforeCosmos(tx sdk.Tx, in *ReceiptInput) error {
	in.Operation = CosmosOperation
	in.Amount, in.FeeCharged = new(big.Int), new(big.Int)
	in.FromBalanceBefore, in.FromBalanceAfter = new(big.Int), new(big.Int)
	in.ToBalanceBefore, in.ToBalanceAfter = new(big.Int), new(big.Int)
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		switch fee := feeTx.GetFee(); len(fee) {
		case 0:
		case 1:
			in.FeeDenom, in.FeeCharged = fee[0].Denom, fee[0].Amount.BigInt()
		default:
			return sdkerrors.Wrap(sdkerrors.ErrInvalidRequest, "a receipted transaction pays its fee in one denom")
		}
	}
	return nil
}

func (r *Receipter) after(etx *ethtypes.Transaction, in *ReceiptInput) error {
	ctx, err := r.exec.Snapshot()
	if err != nil {
		return err
	}
	acc := in.Accepted
	if etx == nil {
		if acc.Result.GasUsed < 0 {
			return fmt.Errorf("negative gas used %d", acc.Result.GasUsed)
		}
		if in.FeeDenom == "" {
			in.FeeDenom = r.evm.GetBaseDenom(ctx)
		}
		in.EVM = &evmtypes.Receipt{
			Status:            uint32(ethtypes.ReceiptStatusSuccessful),
			GasUsed:           uint64(acc.Result.GasUsed),
			CumulativeGasUsed: r.cumulative,
		}
		return nil
	}
	rcpt, err := r.evm.GetTransientReceipt(ctx, etx.Hash(), uint64(acc.TxIndex)) //nolint:gosec // TxIndex is positive
	if err != nil {
		return fmt.Errorf("execution receipt: %w", err)
	}
	if rcpt.From != in.From.Hex() {
		return fmt.Errorf("execution sender %s, recovered %s", rcpt.From, in.From.Hex())
	}
	if r.cumulative > math.MaxUint64-rcpt.GasUsed {
		return errors.New("cumulative gas overflows")
	}
	r.cumulative += rcpt.GasUsed
	rcpt.CumulativeGasUsed = r.cumulative
	in.EVM = rcpt
	if in.ReturnData, err = evmReturnData(acc.Result.Data); err != nil {
		return err
	}
	gasFee := new(big.Int).Mul(new(big.Int).SetUint64(rcpt.GasUsed), new(big.Int).SetUint64(rcpt.EffectiveGasPrice))
	charge, err := r.evm.GetAnteFeeTokenCharge(ctx, etx.Hash())
	if err != nil {
		return err
	}
	if charge == nil {
		in.FeeDenom, in.FeeCharged = r.evm.GetBaseDenom(ctx), gasFee
	} else {
		converted, err := evmkeeper.ConvertFeeToDenom(sdk.NewIntFromBigInt(gasFee), charge.Rate, true)
		if err != nil {
			return err
		}
		in.FeeDenom, in.FeeCharged = charge.Denom, converted.BigInt()
	}
	if in.Sponsor, err = sponsor(rcpt.Logs); err != nil {
		return err
	}
	in.FromBalanceAfter = r.balance(ctx, in.From)
	in.ToBalanceAfter = r.balance(ctx, in.To)
	return nil
}

// evmReturnData unwraps the EVM return data from the deliver result, which
// carries it on success and on revert.
func evmReturnData(data []byte) ([]byte, error) {
	var msgData sdk.TxMsgData
	if err := msgData.Unmarshal(data); err != nil {
		return nil, fmt.Errorf("deliver result data: %w", err)
	}
	if len(msgData.Data) != 1 {
		return nil, fmt.Errorf("deliver result holds %d message responses, want 1", len(msgData.Data))
	}
	var resp evmtypes.MsgEVMTransactionResponse
	if err := resp.Unmarshal(msgData.Data[0].Data); err != nil {
		return nil, fmt.Errorf("EVM response: %w", err)
	}
	return resp.ReturnData, nil
}

// sponsor is quote.sponsor of the first Sponsored event, else zero.
func sponsor(logs []*evmtypes.Log) (common.Address, error) {
	for _, log := range logs {
		_, topics, err := parseLog(log)
		if err != nil {
			return common.Address{}, err
		}
		if len(topics) == 3 && topics[0] == sponsoredTopic && len(log.Data) == 64 {
			return common.BytesToAddress(topics[1][12:]), nil
		}
	}
	return common.Address{}, nil
}
