package sequencer

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	evmante "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/ante"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	oracletypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/oracle/types"
	pricefeedtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/antedecorators"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// batchReserveBytes keeps block data room for tx 0, the MsgSequenceBatch.
// ponytail: fixed 1 KiB over the 216 bytes of batch fields; size it from the encoded message once the batch message type lands.
const batchReserveBytes int64 = 1024

var (
	ErrNoOpenLayer       = errors.New("sequencer: no open layer")
	ErrSealedPending     = errors.New("sequencer: the sealed layer is not committed yet")
	ErrNoSealedLayer     = errors.New("sequencer: no sealed layer")
	ErrHeightMismatch    = errors.New("sequencer: height does not follow the layer below")
	ErrUpgradeHeight     = errors.New("sequencer: an upgrade plan executes at this height")
	ErrNoBlockParams     = errors.New("sequencer: consensus block params are not set")
	ErrNoBlockSpace      = errors.New("sequencer: block max bytes leave no room for transactions")
	ErrBlockFull         = errors.New("sequencer: transaction does not fit the block limits")
	ErrUndecodableTx     = errors.New("sequencer: transaction does not decode")
	ErrMaliciousGasless  = errors.New("sequencer: gasless check panicked")
	ErrBeginBlockPanic   = errors.New("sequencer: begin block panicked")
	ErrDeliverTxPanicked = errors.New("sequencer: deliver tx panicked")
)

// SequenceBatchFields are the MsgSequenceBatch fields of tx 0 derived from the seqHash chain.
type SequenceBatchFields struct {
	Height      uint64
	FromSeq     uint64
	ToSeq       uint64
	PrevSeqHash [32]byte
	LastSeqHash [32]byte
	TxRoot      [32]byte
}

// SequencedTx is one transaction applied to the open layer with its place in the seqHash chain.
type SequencedTx struct {
	Seq        uint64
	TxIndex    int
	ActivityID [32]byte
	SeqHash    [32]byte
	Result     *abci.ExecTxResult
}

// Layers holds the committed layer L_c, the sealed layer L_N and the open layer L_open,
// each a cache branch over the one below.
type Layers struct {
	app         *app.App
	committed   sdk.MultiStore
	sealed      *SealedLayer
	open        *OpenLayer
	lastSeq     uint64
	lastSeqHash [32]byte
}

// OpenLayer is L_open: the block being filled, with BeginBlock already run.
type OpenLayer struct {
	app          *app.App
	height       int64
	ctx          sdk.Context
	ms           sdk.CacheMultiStore
	checkCtx     sdk.Context
	limits       blockLimits
	beginEvents  []abci.Event
	txs          [][]byte
	results      []*abci.ExecTxResult
	evmMsgs      []*evmtypes.MsgEVMTransaction
	activityIDs  [][32]byte
	fromSeq      uint64
	prevSeqHash  [32]byte
	lastSeqHash  [32]byte
	totalGas     uint64
	totalWanted  uint64
	totalDataLen int64
}

// SealedLayer is L_N: a block whose end-of-block ran in ProcessBlock order.
type SealedLayer struct {
	Height   int64
	Txs      [][]byte
	Results  []*abci.ExecTxResult
	Events   []abci.Event
	EndBlock abci.ResponseEndBlock
	Batch    SequenceBatchFields
	ctx      sdk.Context
	ms       sdk.CacheMultiStore
}

type blockLimits struct {
	maxGas       uint64
	maxGasWanted uint64
	maxDataBytes int64
}

// NewLayers starts the layers over the committed store with the stored lastSeq and lastSeqHash.
func NewLayers(a *app.App, committed sdk.MultiStore, lastSeq uint64, lastSeqHash [32]byte) *Layers {
	return &Layers{app: a, committed: committed, lastSeq: lastSeq, lastSeqHash: lastSeqHash}
}

// LastSeq returns the last sealed seq and its seqHash.
func (l *Layers) LastSeq() (uint64, [32]byte) {
	return l.lastSeq, l.lastSeqHash
}

// Sealed returns L_N, or nil when no sealed layer awaits commit.
func (l *Layers) Sealed() *SealedLayer {
	return l.sealed
}

// Open branches L_open over L_N (or L_c when nothing is sealed) the way the deliver state is
// branched and runs BeginBlock once. It refuses a height whose upgrade plan executes, so the
// plan runs in the block path.
func (l *Layers) Open(header tmproto.Header, votes []abci.VoteInfo, byzantine []abci.Misbehavior) (*OpenLayer, error) {
	if l.open != nil {
		return nil, ErrLayerOpen
	}
	if header.Height < 1 {
		return nil, fmt.Errorf("%w: height %d", ErrHeightMismatch, header.Height)
	}
	parent := l.committed
	if l.sealed != nil {
		if header.Height != l.sealed.Height+1 {
			return nil, fmt.Errorf("%w: height %d over sealed height %d", ErrHeightMismatch, header.Height, l.sealed.Height)
		}
		parent = l.sealed.ms
	}

	ms := parent.CacheMultiStore()
	ctx := sdk.NewContext(ms, header, false)
	ctx = ctx.WithConsensusParams(l.app.GetConsensusParams(ctx))

	if plan, found := l.app.UpgradeKeeper.GetUpgradePlan(ctx); found && plan.ShouldExecute(ctx) {
		return nil, fmt.Errorf("%w: plan %q at height %d", ErrUpgradeHeight, plan.Name, plan.Height)
	}
	limits, err := l.blockLimits(ctx)
	if err != nil {
		return nil, err
	}
	checkCtx := sdk.NewContext(parent.CacheMultiStore(), header, false).WithConsensusParams(ctx.ConsensusParams())

	beginEvents, err := l.beginBlock(ctx, header.Height, votes, byzantine)
	if err != nil {
		return nil, err
	}

	l.open = &OpenLayer{
		app:         l.app,
		height:      header.Height,
		ctx:         ctx,
		ms:          ms,
		checkCtx:    checkCtx,
		limits:      limits,
		beginEvents: beginEvents,
		fromSeq:     l.lastSeq + 1,
		prevSeqHash: l.lastSeqHash,
		lastSeqHash: l.lastSeqHash,
	}
	return l.open, nil
}

// Committed drops L_N once its height is committed; L_c then carries its writes.
func (l *Layers) Committed(height int64) error {
	if l.sealed == nil {
		return ErrNoSealedLayer
	}
	if l.sealed.Height != height {
		return fmt.Errorf("%w: committed height %d, sealed height %d", ErrHeightMismatch, height, l.sealed.Height)
	}
	l.sealed = nil
	return nil
}

func (l *Layers) beginBlock(ctx sdk.Context, height int64, votes []abci.VoteInfo, byzantine []abci.Misbehavior) (events []abci.Event, err error) {
	defer func() {
		if r := recover(); r != nil {
			events = nil
			err = fmt.Errorf("%w: %v", ErrBeginBlockPanic, r)
		}
	}()
	return l.app.BeginBlock(ctx, height, votes, byzantine, false).Events, nil
}

// blockLimits reads the limits ProcessProposal enforces through checkTotalBlockGas and the
// data bytes CreateProposalBlock reaps, with the worst-case commit and evidence sizes.
func (l *Layers) blockLimits(ctx sdk.Context) (blockLimits, error) {
	cp := ctx.ConsensusParams()
	if cp == nil || cp.Block == nil {
		return blockLimits{}, ErrNoBlockParams
	}
	var evidenceBytes int64
	if cp.Evidence != nil {
		evidenceBytes = cp.Evidence.MaxBytes
	}
	maxValidators := int(l.app.StakingKeeper.MaxValidators(ctx))
	dataBytes := cp.Block.MaxBytes -
		tmtypes.MaxOverheadForBlock -
		tmtypes.MaxHeaderBytes -
		tmtypes.MaxCommitBytes(maxValidators) -
		evidenceBytes -
		batchReserveBytes
	if dataBytes <= 0 {
		return blockLimits{}, fmt.Errorf("%w: max bytes %d", ErrNoBlockSpace, cp.Block.MaxBytes)
	}
	return blockLimits{
		maxGas:       uint64(cp.Block.MaxGas),       //nolint:gosec // same reading as checkTotalBlockGas
		maxGasWanted: uint64(cp.Block.MaxGasWanted), //nolint:gosec // same reading as checkTotalBlockGas
		maxDataBytes: dataBytes,
	}, nil
}

// Height returns the height of the open block.
func (o *OpenLayer) Height() int64 {
	return o.height
}

// Apply decodes txBytes, refuses it with ErrBlockFull when it would take the block past the
// limits checkTotalBlockGas enforces or past the data bytes, executes it at the next tx index
// the way ProcessBlock does and folds it into the seqHash chain. A refused tx leaves the layer
// unchanged.
func (o *OpenLayer) Apply(txBytes []byte) (SequencedTx, error) {
	a := o.app
	typed := a.DecodeTransactionsConcurrently(o.ctx, [][]byte{txBytes})[0]
	if typed == nil {
		return SequencedTx{}, ErrUndecodableTx
	}
	wanted, estimated, err := blockGas(a, o.checkCtx, typed)
	if err != nil {
		return SequencedTx{}, err
	}
	dataLen := tmtypes.ComputeProtoSizeForTxs([]tmtypes.Tx{txBytes})
	if o.totalWanted > math.MaxUint64-wanted || o.totalGas > math.MaxUint64-estimated {
		return SequencedTx{}, fmt.Errorf("%w: gas overflows", ErrBlockFull)
	}
	totalWanted := o.totalWanted + wanted
	totalGas := o.totalGas + estimated
	totalDataLen := o.totalDataLen + dataLen
	if totalWanted > o.limits.maxGasWanted || totalGas > o.limits.maxGas || totalDataLen > o.limits.maxDataBytes {
		return SequencedTx{}, fmt.Errorf("%w: gas wanted %d of %d, gas %d of %d, data bytes %d of %d",
			ErrBlockFull, totalWanted, o.limits.maxGasWanted, totalGas, o.limits.maxGas, totalDataLen, o.limits.maxDataBytes)
	}

	index := len(o.txs)
	result, err := deliver(a, o.ctx.WithTxIndex(index), txBytes, typed)
	if err != nil {
		return SequencedTx{}, err
	}
	activity := activityID(typed, txBytes)
	seq := o.fromSeq + uint64(index) //nolint:gosec // index is a slice length
	seqHash := sequenceHash(o.lastSeqHash, seq, activity)

	o.txs = append(o.txs, txBytes)
	o.results = append(o.results, result)
	o.evmMsgs = append(o.evmMsgs, a.GetEVMMsg(typed))
	o.activityIDs = append(o.activityIDs, activity)
	o.lastSeqHash = seqHash
	o.totalWanted = totalWanted
	o.totalGas = totalGas
	o.totalDataLen = totalDataLen
	return SequencedTx{Seq: seq, TxIndex: index, ActivityID: activity, SeqHash: seqHash, Result: result}, nil
}

func deliver(a *app.App, ctx sdk.Context, txBytes []byte, typed sdk.Tx) (result *abci.ExecTxResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("%w: %v", ErrDeliverTxPanicked, r)
		}
	}()
	return a.DeliverTxWithResult(ctx, txBytes, typed), nil
}

// blockGas returns the gas wanted and the gas estimate checkTotalBlockGas counts for tx.
func blockGas(a *app.App, checkCtx sdk.Context, tx sdk.Tx) (wanted uint64, estimated uint64, err error) {
	isEVM, evmErr := evmante.IsEVMMessage(tx)
	if !(evmErr == nil && isEVM) && couldBeGasless(tx) {
		gasless, gaslessErr := antedecorators.IsTxGasless(tx, checkCtx, a.OracleKeeper, &a.EvmKeeper, a.PricefeedKeeper)
		if gaslessErr != nil && strings.Contains(gaslessErr.Error(), "panic in IsTxGasless") {
			return 0, 0, fmt.Errorf("%w: %v", ErrMaliciousGasless, gaslessErr)
		}
		if gasless {
			return 0, 0, nil
		}
	}
	if evmErr != nil {
		return 0, 0, nil
	}
	if isEVM {
		msg := evmtypes.GetEVMTransactionMessage(tx)
		if msg.IsAssociateTx() {
			return 0, 0, nil
		}
		etx, _ := msg.AsTransaction()
		if etx == nil {
			return 0, 0, ErrUndecodableTx
		}
		wanted = etx.Gas()
	} else {
		feeTx, ok := tx.(sdk.FeeTx)
		if !ok {
			return 0, 0, nil
		}
		wanted = feeTx.GetGas()
	}
	if int64(wanted) < 0 { //nolint:gosec // the overflow guard checkTotalBlockGas applies
		return 0, 0, fmt.Errorf("%w: gas wanted %d overflows int64", ErrBlockFull, wanted)
	}
	estimated = wanted
	if est := tx.GetGasEstimate(); est >= app.MinGasEVMTx && est <= wanted {
		estimated = est
	}
	return wanted, estimated, nil
}

func couldBeGasless(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		switch msg.(type) {
		case *evmtypes.MsgAssociate, *oracletypes.MsgAggregateExchangeRateVote, *pricefeedtypes.MsgSubmitPrices:
			return true
		}
	}
	return false
}

// activityID is keccak256 of the signed EVM transaction, or sha256 of the tx bytes otherwise.
func activityID(tx sdk.Tx, txBytes []byte) [32]byte {
	if msg := evmtypes.GetEVMTransactionMessage(tx); msg != nil {
		if etx, _ := msg.AsTransaction(); etx != nil {
			return etx.Hash()
		}
	}
	return sha256.Sum256(txBytes)
}

// sequenceHash is sha256("PAXEER/v1/sequence\0" ‖ prevSeqHash ‖ u64be(seq) ‖ activity_id).
func sequenceHash(prev [32]byte, seq uint64, activity [32]byte) [32]byte {
	h := sha256.New()
	h.Write(seqHashDomain)
	h.Write(prev[:])
	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], seq)
	h.Write(seqBytes[:])
	h.Write(activity[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// batchFields derives the MsgSequenceBatch fields of the open block from its seqHash chain.
func (o *OpenLayer) batchFields() SequenceBatchFields {
	root := sha256.New()
	for _, id := range o.activityIDs {
		root.Write(id[:])
	}
	var txRoot [32]byte
	copy(txRoot[:], root.Sum(nil))
	return SequenceBatchFields{
		Height:      uint64(o.height), //nolint:gosec // Open refuses heights below 1
		FromSeq:     o.fromSeq,
		ToSeq:       o.fromSeq + uint64(len(o.txs)) - 1,
		PrevSeqHash: o.prevSeqHash,
		LastSeqHash: o.lastSeqHash,
		TxRoot:      txRoot,
	}
}

// Context returns the sealed block's context over L_N.
func (s *SealedLayer) Context() sdk.Context {
	return s.ctx
}
