package sequencer

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
)

var (
	// ErrNonceTooLow carries the text CheckTx and the RPC layer already match on (rpc/utils.go).
	ErrNonceTooLow = core.ErrNonceTooLow
	// ErrSameNonce refuses a (sender, nonce) that is already receipted or held; nothing is superseded.
	ErrSameNonce = errors.New("same nonce already receipted or held")
	// ErrSenderQueueFull refuses a future nonce beyond the per-sender bound.
	ErrSenderQueueFull = errors.New("sender future-nonce queue full")
	// ErrQueueFull refuses a future nonce beyond the total bound.
	ErrQueueFull = errors.New("future-nonce queue full")
)

// NextNonceReader reads the next executable nonce of a sender from the open state,
// through the keeper path the ante uses (App.EvmNonce).
type NextNonceReader interface {
	EvmNonce(common.Address) uint64
}

// AcceptFunc executes one transaction at receipt; nil means it was receipted.
type AcceptFunc func(sender common.Address, nonce uint64, tx []byte) error

// NonceQueueConfig holds the bounds and lifetime set at activation rehearsal.
type NonceQueueConfig struct {
	PerSender int
	Total     int
	Lifetime  time.Duration
}

// HeldTx is a future-nonce transaction waiting at rung submitted.
type HeldTx struct {
	Sender  common.Address
	Nonce   uint64
	Tx      []byte
	Arrived time.Time
}

type senderNonce struct {
	sender common.Address
	nonce  uint64
}

// NonceQueue holds future nonces per sender and hands them to Accept in nonce order
// once the gap fills. Accept calls are serialised in arrival order, so across senders
// the receipt order is the arrival order and one sender's transactions never reorder.
// Accept must not call back into the queue.
type NonceQueue struct {
	cfg    NonceQueueConfig
	nonces NextNonceReader
	accept AcceptFunc

	mu        sync.Mutex
	held      map[common.Address]map[uint64]HeldTx
	total     int
	receipted map[senderNonce]struct{}
}

// NewNonceQueue builds a queue over the open-state nonce reader and the Accept path.
func NewNonceQueue(cfg NonceQueueConfig, nonces NextNonceReader, accept AcceptFunc) (*NonceQueue, error) {
	switch {
	case nonces == nil:
		return nil, errors.New("nonce queue: nil nonce reader")
	case accept == nil:
		return nil, errors.New("nonce queue: nil accept")
	case cfg.PerSender <= 0:
		return nil, fmt.Errorf("nonce queue: per-sender bound %d must be positive", cfg.PerSender)
	case cfg.Total < cfg.PerSender:
		return nil, fmt.Errorf("nonce queue: total bound %d below per-sender bound %d", cfg.Total, cfg.PerSender)
	case cfg.Lifetime <= 0:
		return nil, fmt.Errorf("nonce queue: lifetime %s must be positive", cfg.Lifetime)
	}
	return &NonceQueue{
		cfg:       cfg,
		nonces:    nonces,
		accept:    accept,
		held:      make(map[common.Address]map[uint64]HeldTx),
		receipted: make(map[senderNonce]struct{}),
	}, nil
}

// Hold admits tx at (sender, nonce). A nonce equal to next is accepted at once and the
// held nonces it unblocks drain behind it in the same receipt slot (held = false).
// A nonce above next waits at rung submitted (held = true). A nonce below next, or a
// (sender, nonce) already receipted or held, is refused. An Accept refusal is returned as is.
func (q *NonceQueue) Hold(sender common.Address, nonce uint64, tx []byte) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if _, ok := q.receipted[senderNonce{sender, nonce}]; ok {
		return false, fmt.Errorf("%w: sender %s nonce %d receipted", ErrSameNonce, sender, nonce)
	}
	if _, ok := q.held[sender][nonce]; ok {
		return false, fmt.Errorf("%w: sender %s nonce %d held", ErrSameNonce, sender, nonce)
	}
	next := q.nonces.EvmNonce(sender)
	switch {
	case nonce < next:
		return false, fmt.Errorf("%w: address %s, tx: %d state: %d", ErrNonceTooLow, sender, nonce, next)
	case nonce == next:
		if err := q.accept(sender, nonce, tx); err != nil {
			return false, err
		}
		q.receipted[senderNonce{sender, nonce}] = struct{}{}
		q.drain(sender)
		return false, nil
	}
	pending := q.held[sender]
	if len(pending) >= q.cfg.PerSender {
		return false, fmt.Errorf("%w: sender %s holds %d", ErrSenderQueueFull, sender, len(pending))
	}
	if q.total >= q.cfg.Total {
		return false, fmt.Errorf("%w: %d held", ErrQueueFull, q.total)
	}
	if pending == nil {
		pending = make(map[uint64]HeldTx)
		q.held[sender] = pending
	}
	pending[nonce] = HeldTx{Sender: sender, Nonce: nonce, Tx: tx, Arrived: time.Now()}
	q.total++
	return true, nil
}

// Drain hands the sender's held transactions to Accept in nonce order while the next
// nonce on the open state matches a held one. A held transaction Accept refuses is
// dropped with no receipt, and the drain stops at the gap it leaves.
func (q *NonceQueue) Drain(sender common.Address) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.drain(sender)
}

func (q *NonceQueue) drain(sender common.Address) {
	pending := q.held[sender]
	for len(pending) > 0 {
		next := q.nonces.EvmNonce(sender)
		h, ok := pending[next]
		if !ok {
			break
		}
		q.remove(sender, next)
		if q.accept(sender, next, h.Tx) == nil {
			q.receipted[senderNonce{sender, next}] = struct{}{}
		}
	}
}

func (q *NonceQueue) remove(sender common.Address, nonce uint64) {
	pending := q.held[sender]
	delete(pending, nonce)
	q.total--
	if len(pending) == 0 {
		delete(q.held, sender)
	}
}

// Expire drops, with no receipt, every held transaction that arrived more than the
// lifetime before now or whose nonce the open state has already passed, and returns them.
func (q *NonceQueue) Expire(now time.Time) []HeldTx {
	q.mu.Lock()
	defer q.mu.Unlock()

	minArrival := now.Add(-q.cfg.Lifetime)
	var dropped []HeldTx
	for sender, pending := range q.held {
		next := q.nonces.EvmNonce(sender)
		for nonce, h := range pending {
			if h.Arrived.Before(minArrival) || nonce < next {
				dropped = append(dropped, h)
				q.remove(sender, nonce)
			}
		}
	}
	return dropped
}

// Sealed forgets the receipted (sender, nonce) pairs once the open layer is sealed;
// they are then below next on the committed state and refused as too low.
func (q *NonceQueue) Sealed() {
	q.mu.Lock()
	defer q.mu.Unlock()
	clear(q.receipted)
}

// Held reports whether (sender, nonce) waits at rung submitted.
func (q *NonceQueue) Held(sender common.Address, nonce uint64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.held[sender][nonce]
	return ok
}

// Len is the number of held transactions across all senders.
func (q *NonceQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.total
}
