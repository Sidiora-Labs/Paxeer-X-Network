package broker

import (
	"crypto/rand"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"time"
)

var ErrDraining = errors.New("gateway draining")

type DrainNotice struct{ Deadline time.Time }
type Lifecycle struct {
	mu              sync.Mutex
	draining        bool
	next            uint64
	clients         map[uint64]chan DrainNotice
	admissionAt     time.Time
	admissionTokens float64
	queueFrames     atomic.Int64
	queueBytes      atomic.Int64
	coalesced       atomic.Uint64
	evictions       atomic.Uint64
	staleRevisions  atomic.Uint64
	sequenceGaps    atomic.Uint64
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{clients: make(map[uint64]chan DrainNotice), admissionTokens: 4000}
}
func (l *Lifecycle) Ready() bool { l.mu.Lock(); defer l.mu.Unlock(); return !l.draining }
func (l *Lifecycle) Join() (<-chan DrainNotice, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.draining {
		return nil, nil, ErrDraining
	}
	now := time.Now()
	if !l.admissionAt.IsZero() {
		l.admissionTokens += now.Sub(l.admissionAt).Seconds() * 2000
		if l.admissionTokens > 4000 {
			l.admissionTokens = 4000
		}
	}
	l.admissionAt = now
	if l.admissionTokens < 1 {
		return nil, nil, ErrStreamUnavailable
	}
	l.admissionTokens--
	l.next++
	id := l.next
	ch := make(chan DrainNotice, 1)
	l.clients[id] = ch
	return ch, func() { l.mu.Lock(); delete(l.clients, id); l.mu.Unlock() }, nil
}
func (l *Lifecycle) Drain() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.draining {
		return
	}
	l.draining = true
	notice := DrainNotice{Deadline: time.Now().Add(30 * time.Second)}
	for _, ch := range l.clients {
		ch <- notice
	}
}
func (l *Lifecycle) Connections() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.clients) }

type QueueMetrics struct {
	Frames                int64
	Bytes                 int64
	Coalesced             uint64
	SlowConsumerEvictions uint64
	StaleRevisionRejects  uint64
	SequenceGapRejects    uint64
}

func (l *Lifecycle) QueueMetrics() QueueMetrics {
	return QueueMetrics{Frames: l.queueFrames.Load(), Bytes: l.queueBytes.Load(), Coalesced: l.coalesced.Load(), SlowConsumerEvictions: l.evictions.Load(), StaleRevisionRejects: l.staleRevisions.Load(), SequenceGapRejects: l.sequenceGaps.Load()}
}
func ReconnectDelay() (int64, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(30001))
	if err != nil {
		return 0, err
	}
	return n.Int64(), nil
}

type ActionLimit struct {
	start time.Time
	count int
}

func (l *ActionLimit) Allow(now time.Time) bool {
	if l.start.IsZero() || now.Sub(l.start) >= time.Second {
		l.start = now
		l.count = 0
	}
	if l.count >= 10 {
		return false
	}
	l.count++
	return true
}
