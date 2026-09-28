package policy

import (
	"errors"
	"math/big"
	"sync"
	"time"
)

const (
	RateWindow  = time.Minute
	SpendWindow = 24 * time.Hour
)

var ErrNoClock = errors.New("ledger has no clock")

type Ledger interface {
	Now() (time.Time, error)
	Spent(account, asset string, since time.Time) (*big.Int, error)
	Requests(account string, since time.Time) (int, error)
	RecordSpend(account, asset string, amount *big.Int, at time.Time) error
	RecordRequest(account string, at time.Time) error
}

type spendKey struct {
	account string
	asset   string
}

type spendEntry struct {
	at     time.Time
	amount *big.Int
}

type MemoryLedger struct {
	Clock func() time.Time

	mu       sync.Mutex
	spends   map[spendKey][]spendEntry
	requests map[string][]time.Time
}

func NewMemoryLedger(clock func() time.Time) *MemoryLedger {
	return &MemoryLedger{
		Clock:    clock,
		spends:   map[spendKey][]spendEntry{},
		requests: map[string][]time.Time{},
	}
}

func (l *MemoryLedger) Now() (time.Time, error) {
	if l.Clock == nil {
		return time.Time{}, ErrNoClock
	}
	return l.Clock(), nil
}

func (l *MemoryLedger) Spent(account, asset string, since time.Time) (*big.Int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	total := new(big.Int)
	for _, entry := range l.spends[spendKey{account, asset}] {
		if entry.at.After(since) {
			total.Add(total, entry.amount)
		}
	}
	return total, nil
}

func (l *MemoryLedger) Requests(account string, since time.Time) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, at := range l.requests[account] {
		if at.After(since) {
			count++
		}
	}
	return count, nil
}

func (l *MemoryLedger) RecordSpend(account, asset string, amount *big.Int, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.spends == nil {
		l.spends = map[spendKey][]spendEntry{}
	}
	key := spendKey{account, asset}
	cutoff := at.Add(-SpendWindow)
	kept := l.spends[key][:0]
	for _, entry := range l.spends[key] {
		if entry.at.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	l.spends[key] = append(kept, spendEntry{at: at, amount: new(big.Int).Set(amount)})
	return nil
}

func (l *MemoryLedger) RecordRequest(account string, at time.Time) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.requests == nil {
		l.requests = map[string][]time.Time{}
	}
	cutoff := at.Add(-RateWindow)
	kept := l.requests[account][:0]
	for _, entry := range l.requests[account] {
		if entry.After(cutoff) {
			kept = append(kept, entry)
		}
	}
	l.requests[account] = append(kept, at)
	return nil
}
