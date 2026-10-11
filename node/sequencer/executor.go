package sequencer

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// Mode selects how Accept executes transactions on the open layer.
type Mode uint8

// ModeSequential runs each transaction alone on its own branch of the open
// layer. It is the zero value and so the default mode.
const ModeSequential Mode = 0

var (
	ErrLayerOpen     = errors.New("sequencer: a block is already open")
	ErrLayerNotOpen  = errors.New("sequencer: no block is open")
	ErrOpenHeight    = errors.New("sequencer: open height is not the next height")
	ErrOpenTimestamp = errors.New("sequencer: open timestamp must be positive")
	ErrSealHeight    = errors.New("sequencer: seal height is not the open height")
	ErrSeqExhausted  = errors.New("sequencer: sequence number space exhausted")
	ErrBeginBlock    = errors.New("sequencer: begin block failed")
)

var seqHashDomain = []byte("PAXEER/v1/sequence\x00")

// Executor executes transactions at receipt on the open speculative block.
type Executor interface {
	// Mode reports the execution mode the executor was built with.
	Mode() Mode
	// Open builds the open layer over the last committed state for height at
	// the block-open time and runs BeginBlock once for that height.
	Open(height int64, timestampMs int64) error
	// Accept executes one transaction in deliver mode on the open layer. On
	// success it writes the transaction's branch and assigns the next seq; on
	// failure nothing is written and no seq is taken.
	Accept(txBytes []byte) (*Accepted, error)
	// Seal closes the open layer for height and returns its accepted
	// transactions in seq order with the last seq and seqHash.
	Seal(height int64) (*Batch, error)
	// OpenContext returns a branch of the open layer for reads.
	OpenContext() (sdk.Context, error)
}

// Host is the application surface the executor runs on; *node.App satisfies it.
type Host interface {
	BeginBlock(ctx sdk.Context, height int64, votes []abci.VoteInfo, byzantineValidators []abci.Misbehavior, checkHeight bool) abci.ResponseBeginBlock
	DeliverTx(ctx sdk.Context, req abci.RequestDeliverTxV2, tx sdk.Tx, checksum [32]byte) abci.ResponseDeliverTx
	CacheTxContext(ctx sdk.Context, checksum [32]byte) (sdk.Context, sdk.CacheMultiStore)
	CommitMultiStore() sdk.CommitMultiStore
	LastBlockHeight() int64
	GetConsensusParams(ctx sdk.Context) *tmproto.ConsensusParams
	GetTxConfig() client.TxConfig
	GetEVMMsg(tx sdk.Tx) *evmtypes.MsgEVMTransaction
}

// Options configures an executor.
type Options struct {
	Mode            Mode
	ChainID         string
	ProposerAddress []byte
	// LastSeq and LastSeqHash continue the stored sequence chain.
	LastSeq     uint64
	LastSeqHash [32]byte
}

// Accepted is the execution outcome of one accepted transaction.
type Accepted struct {
	Seq         uint64
	PrevSeqHash [32]byte
	SeqHash     [32]byte
	ActivityID  [32]byte
	// Height is the open height, which is also the batch id.
	Height int64
	// TxIndex is the position in the block; index 0 holds MsgSequenceBatch.
	TxIndex     int
	TimestampMs int64
	Result      *abci.ExecTxResult
}

// Batch is the sealed content of one open layer.
type Batch struct {
	Height      int64
	TimestampMs int64
	// Txs, Results and EVMMsgs hold the accepted transactions 1..n in seq
	// order; EVMMsgs is nil where the transaction is not an EVM transaction.
	Txs              [][]byte
	Results          []*abci.ExecTxResult
	EVMMsgs          []*evmtypes.MsgEVMTransaction
	FromSeq          uint64
	LastSeq          uint64
	PrevSeqHash      [32]byte
	LastSeqHash      [32]byte
	BeginBlockEvents []abci.Event
	// Ctx is the open layer the end-of-block steps run on.
	Ctx sdk.Context
	// Store is the open layer's branch over the committed state.
	Store sdk.CacheMultiStore
}

type openLayer struct {
	height           int64
	timestampMs      int64
	ctx              sdk.Context
	store            sdk.CacheMultiStore
	beginBlockEvents []abci.Event
	fromSeq          uint64
	prevSeqHash      [32]byte
	txs              [][]byte
	results          []*abci.ExecTxResult
	evmMsgs          []*evmtypes.MsgEVMTransaction
}

type sequential struct {
	mu          sync.Mutex
	host        Host
	chainID     string
	proposer    []byte
	lastSeq     uint64
	lastSeqHash [32]byte
	open        *openLayer
}

// NewExecutor builds the executor for opts.Mode.
func NewExecutor(host Host, opts Options) (Executor, error) {
	if host == nil {
		return nil, errors.New("sequencer: nil host")
	}
	if opts.ChainID == "" {
		return nil, errors.New("sequencer: empty chain id")
	}
	if len(opts.ProposerAddress) != crypto.AddressSize {
		return nil, fmt.Errorf("sequencer: proposer address is %d bytes, want %d", len(opts.ProposerAddress), crypto.AddressSize)
	}
	switch opts.Mode {
	case ModeSequential:
		return &sequential{
			host:        host,
			chainID:     opts.ChainID,
			proposer:    append([]byte(nil), opts.ProposerAddress...),
			lastSeq:     opts.LastSeq,
			lastSeqHash: opts.LastSeqHash,
		}, nil
	default:
		return nil, fmt.Errorf("sequencer: unknown executor mode %d", opts.Mode)
	}
}

func (s *sequential) Mode() Mode { return ModeSequential }

func (s *sequential) Open(height int64, timestampMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open != nil {
		return ErrLayerOpen
	}
	if next := s.host.LastBlockHeight() + 1; height != next {
		return fmt.Errorf("%w: got %d, want %d", ErrOpenHeight, height, next)
	}
	if timestampMs <= 0 {
		return fmt.Errorf("%w: got %d", ErrOpenTimestamp, timestampMs)
	}
	header := tmproto.Header{
		ChainID:         s.chainID,
		Height:          height,
		Time:            time.UnixMilli(timestampMs).UTC(),
		ProposerAddress: append([]byte(nil), s.proposer...),
	}
	store := s.host.CommitMultiStore().CacheMultiStore()
	ctx := sdk.NewContext(store, header, false)
	ctx = ctx.WithConsensusParams(s.host.GetConsensusParams(ctx))
	events, err := s.beginBlock(ctx, height)
	if err != nil {
		return err
	}
	s.open = &openLayer{
		height:           height,
		timestampMs:      timestampMs,
		ctx:              ctx,
		store:            store,
		beginBlockEvents: events,
		fromSeq:          s.lastSeq + 1,
		prevSeqHash:      s.lastSeqHash,
	}
	return nil
}

// beginBlock runs BeginBlock once on the open layer. A panic (such as an
// upgrade-needed halt) refuses the open and the layer is discarded.
func (s *sequential) beginBlock(ctx sdk.Context, height int64) (events []abci.Event, err error) {
	defer func() {
		if r := recover(); r != nil {
			events, err = nil, fmt.Errorf("%w at height %d: %v", ErrBeginBlock, height, r)
		}
	}()
	return s.host.BeginBlock(ctx, height, nil, nil, false).Events, nil
}

func (s *sequential) Seal(height int64) (*Batch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return nil, ErrLayerNotOpen
	}
	if height != s.open.height {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrSealHeight, height, s.open.height)
	}
	l := s.open
	s.open = nil
	return &Batch{
		Height:           l.height,
		TimestampMs:      l.timestampMs,
		Txs:              l.txs,
		Results:          l.results,
		EVMMsgs:          l.evmMsgs,
		FromSeq:          l.fromSeq,
		LastSeq:          s.lastSeq,
		PrevSeqHash:      l.prevSeqHash,
		LastSeqHash:      s.lastSeqHash,
		BeginBlockEvents: l.beginBlockEvents,
		Ctx:              l.ctx,
		Store:            l.store,
	}, nil
}

func (s *sequential) OpenContext() (sdk.Context, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.open == nil {
		return sdk.Context{}, ErrLayerNotOpen
	}
	return s.open.ctx.WithMultiStore(s.open.store.CacheMultiStore()), nil
}

// nextSeqHash is sha256("PAXEER/v1/sequence\0" || prevSeqHash || u64be(seq) || activity_id).
func nextSeqHash(prev [32]byte, seq uint64, activityID [32]byte) [32]byte {
	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], seq)
	h := sha256.New()
	h.Write(seqHashDomain)
	h.Write(prev[:])
	h.Write(seqBytes[:])
	h.Write(activityID[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}
