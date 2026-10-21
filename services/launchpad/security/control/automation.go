package control

import (
	"context"
	"math/big"
	"time"
)

// Amounts are canonical uint256 decimal strings, never floating-point or mixed assets.
func amount(v string) (*big.Int, error) {
	if len(v) == 0 || len(v) > 78 || (len(v) > 1 && v[0] == '0') {
		return nil, Invalid
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return nil, Invalid
		}
	}
	n, ok := new(big.Int).SetString(v, 10)
	if !ok || n.BitLen() > 256 {
		return nil, Invalid
	}
	return n, nil
}
func add(a, b string) (string, error) {
	x, e := amount(a)
	if e != nil {
		return "", e
	}
	y, e := amount(b)
	if e != nil {
		return "", e
	}
	x.Add(x, y)
	if x.BitLen() > 256 {
		return "", Invalid
	}
	return x.String(), nil
}
func sub(a, b string) (string, error) {
	x, e := amount(a)
	if e != nil {
		return "", e
	}
	y, e := amount(b)
	if e != nil {
		return "", e
	}
	if x.Cmp(y) < 0 {
		return "", BudgetExceeded
	}
	return x.Sub(x, y).String(), nil
}

type Asset struct {
	Chain    uint64
	Address  string
	Decimals uint8
}

const USDLAddress = "0x85fcd13735f4309833a503ee804ea32395851479"
const NativePAX = "native"

func (a Asset) Valid() bool {
	return a.Chain == 125 && ((a.Address == USDLAddress && a.Decimals == 6) || (a.Address == NativePAX && a.Decimals == 18))
}
func (a Asset) Key() string {
	if a.Address == NativePAX {
		return "pax"
	}
	return "usdl"
}

type Budget struct {
	Owner           string
	Asset           Asset
	Requests, Used  uint64
	Spend, Reserved string
	Revision        uint64
	Expires         time.Time
}
type Policy struct {
	ID, Owner, Mode, Disclosure, State string
	Asset                              Asset
	Instruments                        []string
	Requests, Used                     uint64
	Spend, Reserved                    string
	QuoteAge                           time.Duration
	Expires                            time.Time
	Revision                           uint64
}
type Reservation struct {
	ID, Owner, PolicyID, Instrument, Amount, State, Mode, Disclosure string
	Created                                                          time.Time
}

func (s *Store) SetBudget(ctx context.Context, cred, owner, reason string, b Budget, expected uint64) error {
	if !key(owner) || !b.Asset.Valid() || b.Owner != owner || b.Requests == 0 || b.Requests > 1000000 {
		return Invalid
	}
	if _, e := amount(b.Spend); e != nil {
		return e
	}
	return s.run(ctx, cred, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner {
			return Denied
		}
		var old Budget
		e := t.get("budget", owner+"."+b.Asset.Key(), &old)
		if e != nil && e != Missing {
			return e
		}
		if old.Revision != expected || expected == ^uint64(0) {
			return Conflict
		}
		if !b.Expires.After(t.now) || b.Expires.After(t.now.Add(24*time.Hour)) {
			return Invalid
		}
		if b.Requests < old.Used {
			return BudgetExceeded
		}
		if _, e = sub(b.Spend, zero(old.Reserved)); e != nil {
			return e
		}
		b.Used = old.Used
		b.Reserved = zero(old.Reserved)
		b.Revision = expected + 1
		return t.put("budget", owner+"."+b.Asset.Key(), b)
	})
}
func zero(v string) string {
	if v == "" {
		return "0"
	}
	return v
}
func (s *Store) CreatePolicy(ctx context.Context, cred, owner, reason string, p Policy) error {
	if !key(p.ID) || p.Owner != owner || !p.Asset.Valid() || p.Mode != "disclosed_assistant" || !text(p.Disclosure, 256) || len(p.Instruments) < 1 || len(p.Instruments) > 20 || p.Requests < 1 || p.Requests > 1000000 || p.QuoteAge <= 0 || p.QuoteAge > time.Minute {
		return Invalid
	}
	if _, e := amount(p.Spend); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, v := range p.Instruments {
		if !key(v) || seen[v] {
			return Invalid
		}
		seen[v] = true
	}
	return s.run(ctx, cred, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner {
			return Denied
		}
		if !p.Expires.After(t.now) || p.Expires.After(t.now.Add(24*time.Hour)) {
			return Invalid
		}
		var old Policy
		if e := t.get("policy", p.ID, &old); e == nil {
			return Conflict
		} else if e != Missing {
			return e
		}
		p.State = "draft"
		p.Used = 0
		p.Reserved = "0"
		p.Revision = 1
		return t.put("policy", p.ID, p)
	})
}

// Only recommendation mode is supported. An enable request for settlement must
// never turn into an implicit signing capability. No seed/key field exists.
func (s *Store) PolicyState(ctx context.Context, cred, owner, k, reason, next string, expected uint64) error {
	if !key(k) {
		return Invalid
	}
	return s.run(ctx, cred, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner {
			return Denied
		}
		var p Policy
		if e := t.get("policy", k, &p); e != nil {
			return e
		}
		if p.Owner != owner {
			return Denied
		}
		if p.Revision != expected || expected == ^uint64(0) {
			return Conflict
		}
		if p.State == "revoked" || p.State == "stopped" {
			return Denied
		}
		switch next {
		case "enabled":
			if !p.Expires.After(t.now) {
				return Denied
			}
		case "paused", "stopped", "revoked":
		default:
			return Invalid
		}
		p.State = next
		p.Revision++
		return t.put("policy", k, p)
	})
}
func (s *Store) ReserveRecommendation(ctx context.Context, cred, owner, policyID, k, instrument, reason string, quoteAt time.Time) (Reservation, error) {
	var r Reservation
	if !key(policyID) || !key(k) || !key(instrument) {
		return r, Invalid
	}
	e := s.run(ctx, cred, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner {
			return Denied
		}
		var p Policy
		if e := t.get("policy", policyID, &p); e != nil {
			return e
		}
		if p.Owner != owner {
			return Denied
		}
		var old Reservation
		if e := t.get("reservation", owner+"."+k, &old); e == nil {
			if old.PolicyID != policyID || old.Instrument != instrument {
				return Conflict
			}
			r = old
			return nil
		} else if e != Missing {
			return e
		}
		if p.State != "enabled" || !p.Expires.After(t.now) || p.Mode != "disclosed_assistant" {
			return Denied
		}
		if quoteAt.After(t.now) || t.now.Sub(quoteAt) > p.QuoteAge {
			return Denied
		}
		found := false
		for _, v := range p.Instruments {
			if v == instrument {
				found = true
			}
		}
		if !found {
			return Denied
		}
		var b Budget
		if e := t.get("budget", owner+"."+p.Asset.Key(), &b); e == Missing {
			return Unavailable
		} else if e != nil {
			return e
		}
		if !b.Expires.After(t.now) || b.Used >= b.Requests || p.Used >= p.Requests {
			return BudgetExceeded
		}
		b.Used++
		p.Used++
		if b.Revision == ^uint64(0) || p.Revision == ^uint64(0) {
			return Conflict
		}
		b.Revision++
		p.Revision++
		r = Reservation{ID: id(), Owner: owner, PolicyID: policyID, Instrument: instrument, Amount: "0", State: "suggestion", Mode: p.Mode, Disclosure: p.Disclosure, Created: t.now}
		if e := t.put("budget", owner+"."+p.Asset.Key(), b); e != nil {
			return e
		}
		if e := t.put("policy", policyID, p); e != nil {
			return e
		}
		return t.put("reservation", owner+"."+k, r)
	})
	return r, e
}

// AuthorizeDelegatedExecution fails closed until a separately qualified contract
// delegation adapter and trading-lane bounded quote handoff have been registered.
func (s *Store) AuthorizeDelegatedExecution(context.Context, string) error { return Unavailable }
