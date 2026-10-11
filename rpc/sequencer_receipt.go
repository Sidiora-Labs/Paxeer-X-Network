package evmrpc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	seqkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// SequencerReceiptMethod is the JSON-RPC name of SequencerReceiptAPI.SequencerReceipt.
const SequencerReceiptMethod = "pax_sequencerReceipt"

// Rung is how far a sent transaction has progressed.
type Rung string

const (
	// RungSubmitted is a transaction the sequencer holds without a receipt.
	RungSubmitted Rung = "submitted"
	// RungReceipted is a transaction with a signed receipt whose batch is not committed.
	RungReceipted Rung = "receipted"
	// RungCommitted is a receipted transaction covered by the MsgSequenceBatch of a committed block.
	RungCommitted Rung = "committed"
)

const (
	sequencerReceiptProtocolVersion uint16 = 2
	sequencerReceiptLayoutVersion   uint32 = 1
	sequencerEffectKindState        uint8  = 1
	sequencerEffectKindEvent        uint8  = 3
	sequencerMaxLogTopics                  = 4
	// sequencerSummaryFixedBytes is the summary effect body before the fee denom.
	sequencerSummaryFixedBytes = 8 + 8 + 8 + 4 + common.AddressLength + common.AddressLength + 32 + 4 + 4 + 1
	sequencerMaxFeeDenomBytes  = 64
	sequencerNotServedCode     = -32000
)

var (
	// ErrSequencerReceiptInvalid refuses receipt bytes that are not a signed sequencer receipt of layout 1.
	ErrSequencerReceiptInvalid = errors.New("sequencer receipt: invalid receipt bytes")
	// ErrSequencerReceiptMismatch refuses a JSON view whose fields or data differ from its receipt bytes.
	ErrSequencerReceiptMismatch = errors.New("sequencer receipt: JSON view differs from receipt bytes")
	// ErrSequencerReceiptCommit reports committed sequencer state that contradicts a signed receipt.
	ErrSequencerReceiptCommit = errors.New("sequencer receipt: committed sequence contradicts the receipt")
	// ErrSequencerReceiptOption refuses a sequencer receipt option that is both local and forwarded,
	// or local without the sequencer keeper.
	ErrSequencerReceiptOption = errors.New("sequencer receipt: invalid option")
)

// errSequencerReceiptNotServed answers pax_sequencerReceipt on a node that is neither the
// sequencer nor the standby and has no sequencer endpoint to forward to.
type errSequencerReceiptNotServed struct{}

func (errSequencerReceiptNotServed) Error() string {
	return SequencerReceiptMethod + " served by sequencer"
}

func (errSequencerReceiptNotServed) ErrorCode() int { return sequencerNotServedCode }

var _ rpc.Error = errSequencerReceiptNotServed{}

// SequencerSend is the sequencer's record of one sent transaction.
type SequencerSend struct {
	// Nonce is the transaction nonce.
	Nonce uint64
	// Receipt is the canonical signed receipt; nil while the transaction is submitted.
	Receipt []byte
	// ReturnData, LogData and VMError are the execution outputs the receipt binds by digest:
	// sha256(ReturnData) is activity_root, sha256(LogData[i]) is in log effect i+1 and
	// sha256(VMError) is in the summary effect.
	ReturnData []byte
	LogData    [][]byte
	VMError    string
}

// SequencerReceiptSource is the send record held by the sequencer, and by the standby from the
// replicated ACCEPT records.
type SequencerReceiptSource interface {
	// SequencerSend returns the record of txHash, or nil when the hash is unknown.
	SequencerSend(txHash common.Hash) (*SequencerSend, error)
}

// SequencerReceiptOption selects how NewEVMHTTPServer serves pax_sequencerReceipt. On the
// sequencer and standby Source and Keeper are set; on every other node ForwardURL names the
// sequencer endpoint. The zero option answers -32000 "served by sequencer".
type SequencerReceiptOption struct {
	Source     SequencerReceiptSource
	Keeper     *seqkeeper.Keeper
	ForwardURL *url.URL
}

func (o SequencerReceiptOption) validate() error {
	if o.Source != nil && o.ForwardURL != nil {
		return fmt.Errorf("%w: a local source and a forward URL are both set", ErrSequencerReceiptOption)
	}
	if o.Source != nil && o.Keeper == nil {
		return fmt.Errorf("%w: a local source needs the sequencer keeper", ErrSequencerReceiptOption)
	}
	return nil
}

// SequencerReceiptLog is one log of a sequencer receipt.
type SequencerReceiptLog struct {
	Address  common.Address `json:"address"`
	Topics   []common.Hash  `json:"topics"`
	Data     hexutil.Bytes  `json:"data"`
	LogIndex hexutil.Uint   `json:"logIndex"`
}

// SequencerReceiptView is the JSON view of a signed receipt, every field decoded from Receipt.
type SequencerReceiptView struct {
	Seq               hexutil.Uint64        `json:"seq"`
	Height            hexutil.Uint64        `json:"height"`
	Index             hexutil.Uint          `json:"index"`
	Timestamp         hexutil.Uint64        `json:"timestamp"`
	Status            hexutil.Uint64        `json:"status"`
	ReturnData        hexutil.Bytes         `json:"returnData"`
	Logs              []SequencerReceiptLog `json:"logs"`
	GasUsed           hexutil.Uint64        `json:"gasUsed"`
	CumulativeGasUsed hexutil.Uint64        `json:"cumulativeGasUsed"`
	EffectiveGasPrice hexutil.Uint64        `json:"effectiveGasPrice"`
	FeeDenom          string                `json:"feeDenom"`
	FeeAmount         *hexutil.Big          `json:"feeAmount"`
	Sponsor           common.Address        `json:"sponsor"`
	From              common.Address        `json:"from"`
	To                common.Address        `json:"to"`
	ContractAddress   common.Address        `json:"contractAddress"`
	VMError           string                `json:"vmError"`
	KeyID             common.Hash           `json:"keyId"`
	Receipt           hexutil.Bytes         `json:"receipt"`
}

// SequencerReceiptResult is the answer of pax_sequencerReceipt. A submitted transaction carries
// only Rung, TxHash and Nonce; receipted and committed ones carry the view, and committed ones
// also the block whose MsgSequenceBatch covers the receipt's seq.
type SequencerReceiptResult struct {
	Rung   Rung           `json:"rung"`
	TxHash common.Hash    `json:"txHash"`
	Nonce  hexutil.Uint64 `json:"nonce"`
	*SequencerReceiptView
	BlockNumber *hexutil.Uint64 `json:"blockNumber,omitempty"`
	BlockHash   *common.Hash    `json:"blockHash,omitempty"`
}

// Verify checks that every JSON field equals the field decoded from the receipt bytes and that
// returnData, each log's data and vmError hash to the digests the bytes carry.
func (r *SequencerReceiptResult) Verify() error {
	switch r.Rung {
	case RungSubmitted:
		if r.SequencerReceiptView != nil || r.BlockNumber != nil || r.BlockHash != nil {
			return fmt.Errorf("%w: a submitted answer carries receipt fields", ErrSequencerReceiptMismatch)
		}
		return nil
	case RungReceipted:
		if r.BlockNumber != nil || r.BlockHash != nil {
			return fmt.Errorf("%w: a receipted answer carries a block", ErrSequencerReceiptMismatch)
		}
	case RungCommitted:
		if r.BlockNumber == nil || r.BlockHash == nil {
			return fmt.Errorf("%w: a committed answer has no block", ErrSequencerReceiptMismatch)
		}
		if uint64(*r.BlockNumber) < uint64(r.Height) {
			return fmt.Errorf("%w: committed block %d is below the receipt height %d", ErrSequencerReceiptMismatch, *r.BlockNumber, r.Height)
		}
	default:
		return fmt.Errorf("%w: unknown rung %q", ErrSequencerReceiptMismatch, r.Rung)
	}
	if r.SequencerReceiptView == nil {
		return fmt.Errorf("%w: %s answer has no receipt", ErrSequencerReceiptMismatch, r.Rung)
	}
	logData := make([][]byte, len(r.Logs))
	for i, log := range r.Logs {
		logData[i] = log.Data
	}
	want, decoded, err := sequencerReceiptView(&SequencerSend{
		Receipt:    r.Receipt,
		ReturnData: r.ReturnData,
		LogData:    logData,
		VMError:    r.VMError,
	})
	if err != nil {
		return err
	}
	if common.Hash(decoded.ActivityID) != r.TxHash || decoded.FromSequence != uint64(r.Nonce) {
		return fmt.Errorf("%w: txHash or nonce", ErrSequencerReceiptMismatch)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		return err
	}
	gotJSON, err := json.Marshal(r.SequencerReceiptView)
	if err != nil {
		return err
	}
	if !bytes.Equal(wantJSON, gotJSON) {
		return fmt.Errorf("%w: decoded fields differ", ErrSequencerReceiptMismatch)
	}
	return nil
}

// SequencerReceiptAPI serves pax_sequencerReceipt: from the local send record on the sequencer
// and standby, forwarded to the sequencer on every other node.
type SequencerReceiptAPI struct {
	tmClient       client.LocalClient
	ctxProvider    func(int64) sdk.Context
	option         SequencerReceiptOption
	connectionType ConnectionType
}

func newSequencerReceiptAPI(tmClient client.LocalClient, ctxProvider func(int64) sdk.Context, option SequencerReceiptOption, connectionType ConnectionType) (*SequencerReceiptAPI, error) {
	if err := option.validate(); err != nil {
		return nil, err
	}
	return &SequencerReceiptAPI{tmClient: tmClient, ctxProvider: ctxProvider, option: option, connectionType: connectionType}, nil
}

// SequencerReceipt returns the rung of txHash with its signed receipt, or nil when txHash is unknown.
func (a *SequencerReceiptAPI) SequencerReceipt(ctx context.Context, txHash common.Hash) (result *SequencerReceiptResult, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, SequencerReceiptMethod, a.connectionType, startTime, returnErr, recover())
	}()
	if a.option.Source == nil {
		return a.forward(ctx, txHash)
	}
	send, err := a.option.Source.SequencerSend(txHash)
	if err != nil || send == nil {
		return nil, err
	}
	if send.Receipt == nil {
		return &SequencerReceiptResult{Rung: RungSubmitted, TxHash: txHash, Nonce: hexutil.Uint64(send.Nonce)}, nil
	}
	view, decoded, err := sequencerReceiptView(send)
	if err != nil {
		return nil, err
	}
	if common.Hash(decoded.ActivityID) != txHash {
		return nil, fmt.Errorf("%w: receipt of %s carries activity id %x", ErrSequencerReceiptInvalid, txHash.Hex(), decoded.ActivityID)
	}
	if decoded.FromSequence != send.Nonce {
		return nil, fmt.Errorf("%w: receipt nonce %d, sent nonce %d", ErrSequencerReceiptInvalid, decoded.FromSequence, send.Nonce)
	}
	result = &SequencerReceiptResult{Rung: RungReceipted, TxHash: txHash, Nonce: hexutil.Uint64(send.Nonce), SequencerReceiptView: view}
	height, committed, err := a.coveringHeight(decoded.GlobalSequence, decoded.ResultingStateRoot, uint64(view.Height))
	if err != nil || !committed {
		return result, err
	}
	block, err := a.tmClient.Block(ctx, &height)
	if err != nil {
		return nil, err
	}
	if block == nil || block.Block == nil || block.Block.Height != height {
		return nil, fmt.Errorf("committed block %d is not available", height)
	}
	blockNumber, blockHash := hexutil.Uint64(height), common.BytesToHash(block.BlockID.Hash)
	result.Rung, result.BlockNumber, result.BlockHash = RungCommitted, &blockNumber, &blockHash
	return result, nil
}

// forward asks the sequencer and returns its answer once the answer's JSON matches its bytes.
func (a *SequencerReceiptAPI) forward(ctx context.Context, txHash common.Hash) (*SequencerReceiptResult, error) {
	if a.option.ForwardURL == nil {
		return nil, errSequencerReceiptNotServed{}
	}
	recordRedirectedRequest(ctx, SequencerReceiptMethod, string(a.connectionType))
	c, err := rpc.DialContext(ctx, a.option.ForwardURL.String())
	if err != nil {
		return nil, fmt.Errorf("rpc.DialContext(%q): %w", a.option.ForwardURL.String(), err)
	}
	defer c.Close()
	var result *SequencerReceiptResult
	if err := c.CallContext(ctx, &result, SequencerReceiptMethod, txHash); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if result.TxHash != txHash {
		return nil, fmt.Errorf("%w: sequencer answered for %s", ErrSequencerReceiptMismatch, result.TxHash.Hex())
	}
	if err := result.Verify(); err != nil {
		return nil, err
	}
	return result, nil
}

// coveringHeight finds the committed block whose MsgSequenceBatch covers seq: the lowest height
// at or above the receipt height whose committed lastSeq reaches seq. Committed lastSeq never
// decreases with height, so the search is a bisection over [receiptHeight, latest].
func (a *SequencerReceiptAPI) coveringHeight(seq uint64, seqHash [32]byte, receiptHeight uint64) (int64, bool, error) {
	latest := a.ctxProvider(LatestCtxHeight).BlockHeight()
	if receiptHeight == 0 || latest < 1 || receiptHeight > uint64(latest) {
		return 0, false, nil
	}
	first := int64(receiptHeight)
	lastSeqAt := func(height int64) (uint64, error) {
		return a.option.Keeper.LastSeq(a.ctxProvider(height))
	}
	top, err := lastSeqAt(latest)
	if err != nil || top < seq {
		return 0, false, err
	}
	var searchErr error
	offset := sort.Search(int(latest-first+1), func(i int) bool {
		if searchErr != nil {
			return true
		}
		reached, err := lastSeqAt(first + int64(i))
		if err != nil {
			searchErr = err
			return true
		}
		return reached >= seq
	})
	if searchErr != nil {
		return 0, false, searchErr
	}
	height := first + int64(offset)
	if height > 1 {
		below, err := lastSeqAt(height - 1)
		if err != nil {
			return 0, false, err
		}
		if below >= seq {
			return 0, false, fmt.Errorf("%w: seq %d committed at lastSeq %d below the receipt height %d", ErrSequencerReceiptCommit, seq, below, receiptHeight)
		}
	}
	reached, err := lastSeqAt(height)
	if err != nil {
		return 0, false, err
	}
	if reached == seq {
		committedHash, err := a.option.Keeper.LastSeqHash(a.ctxProvider(height))
		if err != nil {
			return 0, false, err
		}
		if committedHash != seqHash {
			return 0, false, fmt.Errorf("%w: seqHash of seq %d at height %d is %x, receipt %x", ErrSequencerReceiptCommit, seq, height, committedHash, seqHash)
		}
	}
	return height, true, nil
}

func invalidReceipt(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrSequencerReceiptInvalid, fmt.Sprintf(format, args...))
}

func mismatch(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrSequencerReceiptMismatch, fmt.Sprintf(format, args...))
}

// addr32 is the EVM address of an addr32 field: 12 zero bytes then 20 address bytes.
func addr32(word [32]byte, name string) (common.Address, error) {
	if !bytes.Equal(word[:32-common.AddressLength], make([]byte, 32-common.AddressLength)) {
		return common.Address{}, invalidReceipt("%s is not an addr32", name)
	}
	return common.BytesToAddress(word[32-common.AddressLength:]), nil
}

// sequencerReceiptView decodes send.Receipt and derives every JSON field from the decoded
// receipt, checking the execution outputs of send against the digests the receipt binds.
func sequencerReceiptView(send *SequencerSend) (*SequencerReceiptView, *codec.Receipt, error) {
	decoded, err := codec.DecodeReceipt(send.Receipt)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrSequencerReceiptInvalid, err)
	}
	if decoded.ProtocolVersion != sequencerReceiptProtocolVersion || decoded.ModuleVersion != sequencerReceiptLayoutVersion ||
		decoded.ParameterVersion != 0 || decoded.Supply != nil || decoded.ProgramOutcome != nil {
		return nil, nil, invalidReceipt("not a sequencer receipt of layout %d", sequencerReceiptLayoutVersion)
	}
	if decoded.SequencerSignature == nil {
		return nil, nil, invalidReceipt("unsigned")
	}
	if !bytes.Equal(decoded.BatchID[:24], make([]byte, 24)) {
		return nil, nil, invalidReceipt("batch_id is not a height")
	}
	height := binary.BigEndian.Uint64(decoded.BatchID[24:])
	var status uint64
	switch decoded.ResultCode {
	case 0:
		status = 1
	case 1:
		status = 0
	default:
		return nil, nil, invalidReceipt("result code %d", decoded.ResultCode)
	}
	if len(decoded.Effects) == 0 {
		return nil, nil, invalidReceipt("no summary effect")
	}
	summary := decoded.Effects[0]
	if summary.ModuleID != decoded.ModuleID || summary.Ordinal != 0 || summary.Kind != sequencerEffectKindState {
		return nil, nil, invalidReceipt("effect 0 is not the execution summary")
	}
	body := summary.Body
	if len(body) < sequencerSummaryFixedBytes {
		return nil, nil, invalidReceipt("summary is %d bytes", len(body))
	}
	view := &SequencerReceiptView{
		Seq:               hexutil.Uint64(decoded.GlobalSequence),
		Height:            hexutil.Uint64(height),
		Timestamp:         hexutil.Uint64(decoded.Timestamp),
		Status:            hexutil.Uint64(status),
		GasUsed:           hexutil.Uint64(binary.BigEndian.Uint64(body[0:8])),
		CumulativeGasUsed: hexutil.Uint64(binary.BigEndian.Uint64(body[8:16])),
		EffectiveGasPrice: hexutil.Uint64(binary.BigEndian.Uint64(body[16:24])),
		Index:             hexutil.Uint(binary.BigEndian.Uint32(body[24:28])),
		ContractAddress:   common.BytesToAddress(body[28:48]),
		Sponsor:           common.BytesToAddress(body[48:68]),
		KeyID:             common.Hash(decoded.ContextHash),
		Receipt:           decoded.CanonicalBytes(),
	}
	vmErrorDigest := body[68:100]
	returnDataLength := binary.BigEndian.Uint32(body[100:104])
	logCount := binary.BigEndian.Uint32(body[104:108])
	denomLength := int(body[108])
	if denomLength == 0 || denomLength > sequencerMaxFeeDenomBytes || len(body) != sequencerSummaryFixedBytes+denomLength {
		return nil, nil, invalidReceipt("fee denom length %d in a %d-byte summary", denomLength, len(body))
	}
	view.FeeDenom = string(body[sequencerSummaryFixedBytes:])
	if sha256.Sum256([]byte(view.FeeDenom)) != decoded.Asset {
		return nil, nil, invalidReceipt("asset is not the fee denom digest")
	}
	fee := decoded.FeeCharged.Bytes()
	view.FeeAmount = (*hexutil.Big)(new(big.Int).SetBytes(fee[:]))
	if view.From, err = addr32(decoded.From, "from"); err != nil {
		return nil, nil, err
	}
	if view.To, err = addr32(decoded.To, "to"); err != nil {
		return nil, nil, err
	}
	if uint64(logCount) != uint64(len(decoded.Effects)-1) {
		return nil, nil, invalidReceipt("summary counts %d logs, receipt has %d log effects", logCount, len(decoded.Effects)-1)
	}
	if uint64(len(send.ReturnData)) != uint64(returnDataLength) || sha256.Sum256(send.ReturnData) != decoded.ActivityRoot {
		return nil, nil, mismatch("returnData does not hash to activity_root")
	}
	view.ReturnData = bytes.Clone(send.ReturnData)
	vmError := sha256.Sum256([]byte(send.VMError))
	if !bytes.Equal(vmError[:], vmErrorDigest) {
		return nil, nil, mismatch("vmError does not hash to the summary digest")
	}
	view.VMError = send.VMError
	if len(send.LogData) != len(decoded.Effects)-1 {
		return nil, nil, mismatch("%d log data for %d log effects", len(send.LogData), len(decoded.Effects)-1)
	}
	view.Logs = make([]SequencerReceiptLog, len(send.LogData))
	for i, data := range send.LogData {
		effect := decoded.Effects[i+1]
		if effect.ModuleID != decoded.ModuleID || int(effect.Ordinal) != i+1 || effect.Kind != sequencerEffectKindEvent {
			return nil, nil, invalidReceipt("effect %d is not log %d", i+1, i)
		}
		logBody := effect.Body
		if len(logBody) < common.AddressLength+1 {
			return nil, nil, invalidReceipt("log %d body is %d bytes", i, len(logBody))
		}
		topicCount := int(logBody[common.AddressLength])
		if topicCount > sequencerMaxLogTopics || len(logBody) != common.AddressLength+1+topicCount*common.HashLength+32 {
			return nil, nil, invalidReceipt("log %d body is %d bytes for %d topics", i, len(logBody), topicCount)
		}
		topics := make([]common.Hash, topicCount)
		for t := range topics {
			start := common.AddressLength + 1 + t*common.HashLength
			topics[t] = common.BytesToHash(logBody[start : start+common.HashLength])
		}
		dataDigest := sha256.Sum256(data)
		if !bytes.Equal(dataDigest[:], logBody[len(logBody)-32:]) {
			return nil, nil, mismatch("log %d data does not hash to its effect", i)
		}
		view.Logs[i] = SequencerReceiptLog{
			Address:  common.BytesToAddress(logBody[:common.AddressLength]),
			Topics:   topics,
			Data:     bytes.Clone(data),
			LogIndex: hexutil.Uint(i),
		}
	}
	return view, decoded, nil
}
