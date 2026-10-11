package sequencer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

// ModeMicroBatch groups the transactions received within a window and runs
// each group over the OCC scheduler on the open layer, applying the results in
// arrival order so every receipt equals the Sequential one for the same order.
const ModeMicroBatch Mode = 1

var (
	// ErrMicroBatchMode refuses options whose mode is not ModeMicroBatch.
	ErrMicroBatchMode = errors.New("sequencer: micro-batch executor needs ModeMicroBatch")
	// ErrMicroBatchWindow refuses a window that is not positive.
	ErrMicroBatchWindow = errors.New("sequencer: micro-batch window must be positive")
	// ErrMicroBatchMaxCount refuses a maximum group count that is not positive.
	ErrMicroBatchMaxCount = errors.New("sequencer: micro-batch maximum count must be positive")
)

// BatchHost is the application surface MicroBatch runs on: the Sequential
// host plus the OCC batch entry point. *node.App satisfies it.
type BatchHost interface {
	Host
	DeliverTxBatch(ctx sdk.Context, req sdk.DeliverTxBatchRequest) sdk.DeliverTxBatchResponse
}

// MicroBatchOptions bounds one group. Both values are set at activation
// rehearsal; there is no default.
type MicroBatchOptions struct {
	// Window is how long the first transaction of a group waits for others.
	Window time.Duration
	// MaxCount closes a group early once it holds this many transactions.
	MaxCount int
}

type microBatch struct {
	*sequential
	batchHost BatchHost
	window    time.Duration
	maxCount  int

	// gmu guards pending and last.
	gmu     sync.Mutex
	pending *microGroup
	// last is closed once the most recently formed group has executed.
	last chan struct{}
}

type microGroup struct {
	txs [][]byte
	// full is closed when the group reaches the maximum count.
	full chan struct{}
	// prev is the done channel of the group formed before this one.
	prev chan struct{}
	done chan struct{}
	out  []microOutcome
}

type microOutcome struct {
	acc *Accepted
	err error
}

type microEntry struct {
	pos      int
	bytes    []byte
	tx       sdk.Tx
	checksum [32]byte
	// refusal is set when the bytes are refused before execution.
	refusal error
}

// NewMicroBatch builds the MicroBatch executor. Open, Seal and Snapshot are the
// Sequential ones; only Accept groups transactions.
func NewMicroBatch(host BatchHost, opts Options, mb MicroBatchOptions) (Executor, error) {
	if host == nil {
		return nil, errors.New("sequencer: nil host")
	}
	if opts.Mode != ModeMicroBatch {
		return nil, fmt.Errorf("%w: got mode %d", ErrMicroBatchMode, opts.Mode)
	}
	if mb.Window <= 0 {
		return nil, fmt.Errorf("%w: got %s", ErrMicroBatchWindow, mb.Window)
	}
	if mb.MaxCount <= 0 {
		return nil, fmt.Errorf("%w: got %d", ErrMicroBatchMaxCount, mb.MaxCount)
	}
	opts.Mode = ModeSequential
	base, err := NewExecutor(host, opts)
	if err != nil {
		return nil, err
	}
	seq, ok := base.(*sequential)
	if !ok {
		return nil, fmt.Errorf("sequencer: sequential executor has type %T", base)
	}
	last := make(chan struct{})
	close(last)
	return &microBatch{
		sequential: seq,
		batchHost:  host,
		window:     mb.Window,
		maxCount:   mb.MaxCount,
		last:       last,
	}, nil
}

func (m *microBatch) Mode() Mode { return ModeMicroBatch }

// Accept joins txBytes to the forming group in arrival order. The first
// transaction of a group leads it: it waits for the window or the maximum
// count, then executes the group after the group formed before it.
func (m *microBatch) Accept(txBytes []byte) (*Accepted, error) {
	g, i, leader := m.join(txBytes)
	if leader {
		timer := time.NewTimer(m.window)
		select {
		case <-timer.C:
		case <-g.full:
		}
		timer.Stop()
		m.gmu.Lock()
		if m.pending == g {
			m.pending = nil
		}
		m.gmu.Unlock()
		<-g.prev
		g.out = m.execute(g.txs)
		close(g.done)
	} else {
		<-g.done
	}
	return g.out[i].acc, g.out[i].err
}

func (m *microBatch) join(txBytes []byte) (*microGroup, int, bool) {
	m.gmu.Lock()
	defer m.gmu.Unlock()
	g := m.pending
	leader := g == nil
	if leader {
		g = &microGroup{full: make(chan struct{}), prev: m.last, done: make(chan struct{})}
		m.pending = g
		m.last = g.done
	}
	g.txs = append(g.txs, txBytes)
	if len(g.txs) == m.maxCount {
		m.pending = nil
		close(g.full)
	}
	return g, len(g.txs) - 1, leader
}

// execute runs one group in arrival order with Sequential's outcome for each
// transaction. A batch whose results are all receipted is written whole. When
// the first refusal sits at k, the prefix 0..k-1 ran on the same state as in
// Sequential and is rerun alone so the refused transaction's writes (and any
// later writes resting on them) are dropped; the refusal is returned and the
// rest runs over the state with the prefix applied.
func (m *microBatch) execute(txs [][]byte) []microOutcome {
	s := m.sequential
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]microOutcome, len(txs))
	if s.open == nil {
		for i := range out {
			out[i].err = ErrLayerNotOpen
		}
		return out
	}
	queue := make([]microEntry, len(txs))
	for i, bz := range txs {
		queue[i] = s.microDecode(i, bz)
	}
	limit := 0
	for len(queue) > 0 {
		if s.lastSeq == math.MaxUint64 {
			for _, e := range queue {
				out[e.pos].err = ErrSeqExhausted
			}
			break
		}
		if queue[0].refusal != nil {
			out[queue[0].pos].err = queue[0].refusal
			queue, limit = queue[1:], 0
			continue
		}
		if limit == 0 {
			limit = microRunnable(queue, math.MaxUint64-s.lastSeq)
		}
		results, branch, err := m.deliverBatch(queue[:limit])
		if err != nil {
			if limit > 1 {
				limit = 1
				continue
			}
			out[queue[0].pos].err = err
			queue, limit = queue[1:], 0
			continue
		}
		k := 0
		for k < limit && receipted(results[k]) {
			k++
		}
		switch k {
		case limit:
			branch.Write()
			for j := 0; j < limit; j++ {
				out[queue[j].pos].acc = s.microRecord(queue[j], results[j])
			}
			queue, limit = queue[limit:], 0
		case 0:
			out[queue[0].pos].err = sdkerrors.ABCIError(results[0].Codespace, results[0].Code, results[0].Log)
			queue, limit = queue[1:], 0
		default:
			limit = k
		}
	}
	return out
}

func (s *sequential) microDecode(pos int, bz []byte) microEntry {
	e := microEntry{pos: pos, bytes: bz}
	if len(bz) == 0 {
		e.refusal = sdkerrors.Wrap(sdkerrors.ErrTxDecode, "empty transaction")
		return e
	}
	tx, err := s.host.GetTxConfig().TxDecoder()(bz)
	if err != nil {
		e.refusal = sdkerrors.Wrap(sdkerrors.ErrTxDecode, err.Error())
		return e
	}
	e.tx = tx
	e.checksum = sha256.Sum256(bz)
	return e
}

// microRunnable counts the leading decoded entries, at most room.
func microRunnable(queue []microEntry, room uint64) int {
	n := 0
	for n < len(queue) && uint64(n) < room && queue[n].refusal == nil {
		n++
	}
	return n
}

// deliverBatch runs entries over DeliverTxBatch on a fresh branch of the open
// layer, at the block positions they take if all are accepted. A panic raised
// by the scheduler is recovered into ErrPanic and the branch is discarded.
func (m *microBatch) deliverBatch(entries []microEntry) (res []abci.ResponseDeliverTx, branch sdk.CacheMultiStore, err error) {
	defer func() {
		if r := recover(); r != nil {
			res, branch = nil, nil
			err = sdkerrors.Wrapf(sdkerrors.ErrPanic, "recovered: %v", r)
		}
	}()
	open := m.open
	branch = open.ctx.MultiStore().CacheMultiStore()
	base := len(open.txs) + 1
	req := sdk.DeliverTxBatchRequest{TxEntries: make([]*sdk.DeliverTxEntry, len(entries))}
	for j, e := range entries {
		req.TxEntries[j] = &sdk.DeliverTxEntry{
			Request:       abci.RequestDeliverTxV2{Tx: e.bytes},
			SdkTx:         e.tx,
			Checksum:      e.checksum,
			AbsoluteIndex: base + j,
		}
	}
	resp := m.batchHost.DeliverTxBatch(open.ctx.WithMultiStore(branch), req)
	if len(resp.Results) != len(entries) {
		return nil, nil, sdkerrors.Wrapf(sdkerrors.ErrLogic, "micro-batch returned %d results for %d transactions", len(resp.Results), len(entries))
	}
	res = make([]abci.ResponseDeliverTx, len(entries))
	for j, r := range resp.Results {
		if r == nil {
			return nil, nil, sdkerrors.Wrapf(sdkerrors.ErrLogic, "micro-batch returned no result at %d", j)
		}
		res[j] = r.Response
	}
	return res, branch, nil
}

// microRecord appends one receipted transaction to the open layer and takes the
// next seq, exactly as Sequential's Accept does.
func (s *sequential) microRecord(e microEntry, res abci.ResponseDeliverTx) *Accepted {
	activityID := e.checksum
	evmMsg := s.host.GetEVMMsg(e.tx)
	if evmMsg != nil {
		if etx, _ := evmMsg.AsTransaction(); etx != nil {
			activityID = etx.Hash()
		}
	}
	txIndex := len(s.open.txs) + 1
	seq := s.lastSeq + 1
	prev := s.lastSeqHash
	seqHash := nextSeqHash(prev, seq, activityID)
	result := &abci.ExecTxResult{
		Code:      res.Code,
		Data:      res.Data,
		Log:       res.Log,
		Info:      res.Info,
		GasWanted: res.GasWanted,
		GasUsed:   res.GasUsed,
		Events:    res.Events,
		Codespace: res.Codespace,
		EvmTxInfo: res.EvmTxInfo,
	}
	s.open.txs = append(s.open.txs, e.bytes)
	s.open.results = append(s.open.results, result)
	s.open.evmMsgs = append(s.open.evmMsgs, evmMsg)
	s.lastSeq = seq
	s.lastSeqHash = seqHash
	return &Accepted{
		Seq:         seq,
		PrevSeqHash: prev,
		SeqHash:     seqHash,
		ActivityID:  activityID,
		Height:      s.open.height,
		TxIndex:     txIndex,
		TimestampMs: s.open.timestampMs,
		Result:      result,
	}
}
