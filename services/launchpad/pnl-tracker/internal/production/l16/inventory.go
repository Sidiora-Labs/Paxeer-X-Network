// Package l16 implements proportional_floor_v1. Inputs are verified economic
// legs, not raw Swap/Transfer notifications or user estimates.
package l16

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
)

const Algorithm = "proportional_floor_v1"

var canonical = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)
var signed = regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`)

func Uint256(s string) (*big.Int, error) {
	v, e := aggregate(s, false)
	if e == nil && v.BitLen() > 256 {
		return nil, errors.New("uint256 overflow")
	}
	return v, e
}
func aggregate(s string, negative bool) (*big.Int, error) {
	re := canonical
	if negative {
		re = signed
	}
	n := len(s)
	if len(s) > 0 && s[0] == '-' {
		n--
	}
	if n > 156 || !re.MatchString(s) {
		return nil, fmt.Errorf("noncanonical amount %q", s)
	}
	v, _ := new(big.Int).SetString(s, 10)
	return v, nil
}
func add(a, b *big.Int) *big.Int      { return new(big.Int).Add(a, b) }
func sub(a, b *big.Int) *big.Int      { return new(big.Int).Sub(a, b) }
func ratio(a, b, c *big.Int) *big.Int { return new(big.Int).Quo(new(big.Int).Mul(a, b), c) }

type Inventory struct {
	Known              string `json:"knownQuantityRaw"`
	Unknown            string `json:"unknownQuantityRaw"`
	Cost               string `json:"knownRemainingCostUsdlRaw"`
	Realized           string `json:"knownRealizedSubtotalUsdlRaw"`
	UnknownProceeds    string `json:"unknownProceedsUsdlRaw"`
	IncompleteRealized bool   `json:"incompleteRealized"`
	Version            uint64 `json:"inventoryVersion"`
}

func Empty() Inventory {
	return Inventory{Known: "0", Unknown: "0", Cost: "0", Realized: "0", UnknownProceeds: "0", Version: 2}
}
func (p Inventory) values() ([]*big.Int, error) {
	vs := []string{p.Known, p.Unknown, p.Cost, p.Realized, p.UnknownProceeds}
	out := make([]*big.Int, 5)
	for i, s := range vs {
		v, e := aggregate(s, i == 3)
		if e != nil {
			return nil, e
		}
		out[i] = v
	}
	if out[0].BitLen() > 256 || out[1].BitLen() > 256 || add(out[0], out[1]).BitLen() > 256 {
		return nil, errors.New("inventory quantity overflow")
	}
	if p.Version != 2 || out[4].Sign() > 0 && !p.IncompleteRealized || out[0].Sign() == 0 && out[2].Sign() != 0 {
		return nil, errors.New("invalid inventory invariant")
	}
	return out, nil
}
func (p Inventory) CompleteRealized() *string {
	if _, e := p.values(); e != nil {
		return nil
	}
	if p.IncompleteRealized {
		return nil
	}
	s := p.Realized
	return &s
}

// Acquire records actual USDL payer debit as cost, including buy fee exactly once.
// Unknown acquisitions (external transfer/mint or unresolved consideration) have no cost.
func Acquire(p Inventory, quantity string, actualDebit *string) (Inventory, error) {
	v, e := p.values()
	if e != nil {
		return p, e
	}
	q, e := Uint256(quantity)
	if e != nil || q.Sign() == 0 {
		return p, errors.New("invalid acquisition quantity")
	}
	if actualDebit == nil {
		v[1].Add(v[1], q)
	} else {
		c, e := Uint256(*actualDebit)
		if e != nil {
			return p, e
		}
		v[0].Add(v[0], q)
		v[2].Add(v[2], c)
	}
	p.Known = v[0].String()
	p.Unknown = v[1].String()
	p.Cost = v[2].String()
	if _, e = p.values(); e != nil {
		return Inventory{}, e
	}
	return p, nil
}

type Disposal struct{ Known, Unknown, Cost, KnownProceeds, UnknownProceeds string }

// Dispose removes gross token debit. Nil proceeds denotes transfer/burn, not sale.
func Dispose(p Inventory, quantity string, proceeds *string) (Inventory, Disposal, error) {
	var d Disposal
	v, e := p.values()
	if e != nil {
		return p, d, e
	}
	q, e := Uint256(quantity)
	if e != nil || q.Sign() == 0 {
		return p, d, errors.New("invalid disposal quantity")
	}
	total := add(v[0], v[1])
	if q.Cmp(total) > 0 {
		return p, d, errors.New("oversell attribution anomaly")
	}
	k := ratio(q, v[0], total)
	u := sub(q, k)
	c := new(big.Int)
	if v[0].Sign() > 0 {
		c = ratio(v[2], k, v[0])
	}
	pk, pu := new(big.Int), new(big.Int)
	if proceeds != nil {
		amount, e := Uint256(*proceeds)
		if e != nil {
			return p, d, e
		}
		pk = ratio(amount, k, q)
		pu = sub(amount, pk)
		v[3].Add(v[3], sub(pk, c))
		v[4].Add(v[4], pu)
		p.IncompleteRealized = p.IncompleteRealized || u.Sign() > 0
	}
	p.Known = sub(v[0], k).String()
	p.Unknown = sub(v[1], u).String()
	p.Cost = sub(v[2], c).String()
	p.Realized = v[3].String()
	p.UnknownProceeds = v[4].String()
	d = Disposal{k.String(), u.String(), c.String(), pk.String(), pu.String()}
	if _, e = p.values(); e != nil {
		return Inventory{}, Disposal{}, e
	}
	return p, d, nil
}

// Transfer requires a policy decision made using fresh proofs and explicit consent.
// The policy/proof reference must be retained in the immutable economic leg.
func Transfer(source, destination Inventory, q string, carry bool, proofReference string) (Inventory, Inventory, Disposal, error) {
	destinationOriginal := destination
	if carry && proofReference == "" {
		return source, destination, Disposal{}, errors.New("linked carry requires proof/consent provenance")
	}
	s, d, e := Dispose(source, q, nil)
	if e != nil {
		return source, destination, d, e
	}
	if !carry {
		next, e := Acquire(destination, q, nil)
		if e != nil {
			return source, destination, Disposal{}, e
		}
		return s, next, d, nil
	}
	v, e := destination.values()
	if e != nil {
		return source, destination, d, e
	}
	for i, x := range []string{d.Known, d.Unknown, d.Cost} {
		n, _ := aggregate(x, false)
		v[i].Add(v[i], n)
	}
	destination.Known = v[0].String()
	destination.Unknown = v[1].String()
	destination.Cost = v[2].String()
	if _, e = destination.values(); e != nil {
		return source, destinationOriginal, Disposal{}, e
	}
	return s, destination, d, nil
}

// Average returns exact normalized USDL per whole token; missing metadata stays nil.
func Average(p Inventory, decimals *uint8) (*big.Rat, error) {
	v, e := p.values()
	if e != nil {
		return nil, e
	}
	if decimals == nil || v[0].Sign() == 0 {
		return nil, nil
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(*decimals)), nil)
	return new(big.Rat).SetFrac(new(big.Int).Mul(v[2], scale), new(big.Int).Mul(v[0], big.NewInt(1000000))), nil
}

// Mark requires fresh provenance; zero is a valid observed price. Unknown holdings
// and stale/unavailable marks cannot produce complete unrealized profit.
func Unrealized(p Inventory, markValue *string, fresh bool) (*string, error) {
	v, e := p.values()
	if e != nil {
		return nil, e
	}
	if markValue == nil || !fresh || v[1].Sign() > 0 {
		return nil, nil
	}
	m, e := aggregate(*markValue, false)
	if e != nil {
		return nil, e
	}
	s := sub(m, v[2]).String()
	return &s, nil
}
