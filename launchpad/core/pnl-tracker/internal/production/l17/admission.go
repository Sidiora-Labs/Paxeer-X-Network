package l17

import (
	"context"
	"errors"
	"sync"
)

var ErrBusy = errors.New("portfolio read budget exhausted: retryable unavailable")

// Admission rejects saturated reads without an unbounded waiter queue. The map
// contains only active wallets, hence at most global-limit entries.
type Admission struct {
	mu                       sync.Mutex
	global, perWallet, total int
	wallets                  map[string]int
}

func NewAdmission(global, perWallet int) (*Admission, error) {
	if global < 1 || global > 4096 || perWallet < 1 || perWallet > global {
		return nil, ErrInvalid
	}
	return &Admission{global: global, perWallet: perWallet, wallets: map[string]int{}}, nil
}

var defaultAdmission, _ = NewAdmission(32, 2)

func (a *Admission) Acquire(ctx context.Context, wallet string) (func(), error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	w, e := Wallet(wallet)
	if e != nil {
		return nil, e
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if a.total >= a.global || a.wallets[w] >= a.perWallet {
		return nil, ErrBusy
	}
	a.total++
	a.wallets[w]++
	var once sync.Once
	return func() {
		once.Do(func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.total--
			a.wallets[w]--
			if a.wallets[w] == 0 {
				delete(a.wallets, w)
			}
		})
	}, nil
}
