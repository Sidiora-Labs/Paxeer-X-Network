package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
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

type RequestLedger interface {
	Ledger
	ForRequest(requestID string) Ledger
}

func AccountKey(account string) string {
	return strings.ToLower(common.HexToAddress(account).Hex())
}

type ledgerSpend struct {
	Request string `json:"request"`
	Asset   string `json:"asset"`
	Amount  string `json:"amount"`
	At      int64  `json:"at"`
}

type ledgerRequest struct {
	Request string `json:"request"`
	At      int64  `json:"at"`
}

type ledgerRecord struct {
	Spends   []ledgerSpend   `json:"spends"`
	Requests []ledgerRequest `json:"requests"`
}

type SpendLedger struct {
	store *store.Store
	clock func() (time.Time, error)
	seq   atomic.Uint64
}

func NewSpendLedger(st *store.Store, clock func() time.Time) (*SpendLedger, error) {
	if clock == nil {
		return NewSpendLedgerClock(st, nil)
	}
	return NewSpendLedgerClock(st, func() (time.Time, error) { return clock(), nil })
}

func NewSpendLedgerClock(st *store.Store, clock func() (time.Time, error)) (*SpendLedger, error) {
	if st == nil {
		return nil, errors.New("spend ledger: store is required")
	}
	return &SpendLedger{store: st, clock: clock}, nil
}

func (l *SpendLedger) Now() (time.Time, error) {
	if l.clock == nil {
		return time.Time{}, ErrNoClock
	}
	return l.clock()
}

func (l *SpendLedger) ForRequest(requestID string) Ledger {
	return requestView{ledger: l, request: requestID}
}

func (l *SpendLedger) anonymous() requestView {
	return requestView{ledger: l, request: fmt.Sprintf("local/%d/%d", time.Now().UnixNano(), l.seq.Add(1))}
}

func (l *SpendLedger) Spent(account, asset string, since time.Time) (*big.Int, error) {
	return requestView{ledger: l}.Spent(account, asset, since)
}

func (l *SpendLedger) Requests(account string, since time.Time) (int, error) {
	return requestView{ledger: l}.Requests(account, since)
}

func (l *SpendLedger) RecordSpend(account, asset string, amount *big.Int, at time.Time) error {
	return l.anonymous().RecordSpend(account, asset, amount, at)
}

func (l *SpendLedger) RecordRequest(account string, at time.Time) error {
	return l.anonymous().RecordRequest(account, at)
}

func (l *SpendLedger) Apply(account, requestID string, spends []Spend, at time.Time) error {
	if requestID == "" {
		return errors.New("spend ledger: request id is required")
	}
	view := requestView{ledger: l, request: requestID}
	if err := view.RecordRequest(account, at); err != nil {
		return err
	}
	for _, spend := range spends {
		if spend.Amount == nil || spend.Asset == "" || spend.Amount.Sign() < 0 {
			return errors.New("spend ledger: spend is missing its asset or amount")
		}
		if err := view.RecordSpend(account, spend.Asset, spend.Amount, at); err != nil {
			return err
		}
	}
	return nil
}

func (l *SpendLedger) load(account string) (ledgerRecord, error) {
	var rec ledgerRecord
	err := l.store.WithRecord(store.RecordLedger, account, func(plain []byte) error { return json.Unmarshal(plain, &rec) })
	if errors.Is(err, store.ErrNotFound) {
		return ledgerRecord{}, nil
	}
	return rec, err
}

func (l *SpendLedger) update(account string, fn func(*ledgerRecord) error) error {
	return l.store.UpdateRecord(store.RecordLedger, account, func(prev []byte) ([]byte, error) {
		var rec ledgerRecord
		if prev != nil {
			if err := json.Unmarshal(prev, &rec); err != nil {
				return nil, fmt.Errorf("spend ledger: account %s: undecodable record", account)
			}
		}
		if err := fn(&rec); err != nil {
			return nil, err
		}
		return json.Marshal(rec)
	})
}

type requestView struct {
	ledger  *SpendLedger
	request string
}

func (v requestView) Now() (time.Time, error) { return v.ledger.Now() }

func (v requestView) Spent(account, asset string, since time.Time) (*big.Int, error) {
	rec, err := v.ledger.load(account)
	if err != nil {
		return nil, err
	}
	total := new(big.Int)
	for _, entry := range rec.Spends {
		if entry.Asset != asset || !time.Unix(0, entry.At).After(since) || (v.request != "" && entry.Request == v.request) {
			continue
		}
		amount, ok := new(big.Int).SetString(entry.Amount, 10)
		if !ok {
			return nil, fmt.Errorf("spend ledger: account %s: malformed amount", account)
		}
		total.Add(total, amount)
	}
	return total, nil
}

func (v requestView) Requests(account string, since time.Time) (int, error) {
	rec, err := v.ledger.load(account)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range rec.Requests {
		if time.Unix(0, entry.At).After(since) && (v.request == "" || entry.Request != v.request) {
			count++
		}
	}
	return count, nil
}

func (v requestView) RecordSpend(account, asset string, amount *big.Int, at time.Time) error {
	if amount == nil || amount.Sign() < 0 {
		return errors.New("spend ledger: amount must be non-negative")
	}
	return v.ledger.update(account, func(rec *ledgerRecord) error {
		cutoff := at.Add(-SpendWindow).UnixNano()
		kept := rec.Spends[:0]
		found := false
		for _, entry := range rec.Spends {
			if entry.At <= cutoff {
				continue
			}
			if entry.Request == v.request && entry.Asset == asset {
				found = true
				prev, ok := new(big.Int).SetString(entry.Amount, 10)
				if !ok || prev.Cmp(amount) < 0 {
					entry.Amount = amount.String()
				}
			}
			kept = append(kept, entry)
		}
		if !found {
			kept = append(kept, ledgerSpend{Request: v.request, Asset: asset, Amount: amount.String(), At: at.UnixNano()})
		}
		rec.Spends = kept
		return nil
	})
}

func (v requestView) RecordRequest(account string, at time.Time) error {
	return v.ledger.update(account, func(rec *ledgerRecord) error {
		cutoff := at.Add(-RateWindow).UnixNano()
		kept := rec.Requests[:0]
		found := false
		for _, entry := range rec.Requests {
			if entry.At <= cutoff {
				continue
			}
			found = found || entry.Request == v.request
			kept = append(kept, entry)
		}
		if !found {
			kept = append(kept, ledgerRequest{Request: v.request, At: at.UnixNano()})
		}
		rec.Requests = kept
		return nil
	})
}
