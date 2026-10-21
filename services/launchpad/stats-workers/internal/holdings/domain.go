// Package holdings implements Transfer-authoritative token accounting. Swap
// analytics deliberately have no mutation entry point here.
package holdings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
)

const Zero = "0x0000000000000000000000000000000000000000"

var ErrIntegrity = errors.New("holdings: integrity incident; reconciliation required")
var ErrConflict = errors.New("holdings: event identity conflict")
var ErrUnknownToken = errors.New("holdings: undiscovered token")
var ErrOrder = errors.New("holdings: nonmonotonic position")
var hexAddress = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var hexHash = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
var digits = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)
var maxUint = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

func UInt(s string) (*big.Int, error) {
	if !digits.MatchString(s) {
		return nil, fmt.Errorf("invalid uint256 %q", s)
	}
	n, _ := new(big.Int).SetString(s, 10)
	if n.Cmp(maxUint) > 0 {
		return nil, errors.New("uint256 overflow")
	}
	return n, nil
}
func Address(s string) (string, error) {
	if !hexAddress.MatchString(s) {
		return "", errors.New("invalid address")
	}
	return strings.ToLower(s), nil
}

// Transfer is an internal adapter, not a replacement shared envelope. All chain
// integer fields preserve the frozen IF_CHAINPOSITION decimal-string semantics.
type Transfer struct {
	ChainID          string `json:"chainId"`
	Token            string `json:"token"`
	TransactionHash  string `json:"transactionHash"`
	BlockHash        string `json:"blockHash"`
	Height           string `json:"height"`
	TransactionIndex string `json:"transactionIndex"`
	LogIndex         string `json:"logIndex"`
	From             string `json:"from"`
	To               string `json:"to"`
	Amount           string `json:"amount"`
}

func (e Transfer) Normalize() (Transfer, error) {
	if e.ChainID != "125" {
		return e, errors.New("unsupported chain")
	}
	var err error
	for _, p := range []*string{&e.Token, &e.From, &e.To} {
		*p, err = Address(*p)
		if err != nil {
			return e, err
		}
	}
	if e.Token == Zero || (e.From == Zero && e.To == Zero) {
		return e, errors.New("invalid token or zero-to-zero transfer")
	}
	for _, p := range []*string{&e.TransactionHash, &e.BlockHash} {
		if !hexHash.MatchString(*p) || strings.Trim((*p)[2:], "0") == "" {
			return e, errors.New("invalid hash")
		}
		*p = strings.ToLower(*p)
	}
	for _, s := range []string{e.Height, e.TransactionIndex, e.LogIndex, e.Amount} {
		if _, err = UInt(s); err != nil {
			return e, err
		}
	}
	return e, nil
}
func (e Transfer) Identity() string {
	return e.ChainID + ":" + e.Token + ":" + e.TransactionHash + ":" + e.LogIndex
}
func (e Transfer) Digest() string {
	b, _ := json.Marshal(e)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func comparePosition(a, b Transfer) int {
	for _, v := range [][2]string{{a.Height, b.Height}, {a.TransactionIndex, b.TransactionIndex}, {a.LogIndex, b.LogIndex}} {
		x, _ := UInt(v[0])
		y, _ := UInt(v[1])
		if c := x.Cmp(y); c != 0 {
			return c
		}
	}
	return 0
}

// Mutation checks all debits and bounds before returning any state change.
func Mutation(e Transfer, supply, from, to string) (newSupply, newFrom, newTo string, err error) {
	e, err = e.Normalize()
	if err != nil {
		return
	}
	s, er := UInt(supply)
	if er != nil {
		err = er
		return
	}
	f, er := UInt(from)
	if er != nil {
		err = er
		return
	}
	t, er := UInt(to)
	if er != nil {
		err = er
		return
	}
	q, _ := UInt(e.Amount)
	if e.From != Zero && f.Cmp(q) < 0 {
		err = ErrIntegrity
		return
	}
	if e.From == e.To {
		return s.String(), f.String(), t.String(), nil
	}
	if e.From == Zero {
		s.Add(s, q)
	} else {
		f.Sub(f, q)
	}
	if e.To == Zero {
		if s.Cmp(q) < 0 {
			err = ErrIntegrity
			return
		}
		s.Sub(s, q)
	} else {
		t.Add(t, q)
	}
	if s.Cmp(maxUint) > 0 || t.Cmp(maxUint) > 0 {
		err = ErrIntegrity
		return
	}
	return s.String(), f.String(), t.String(), nil
}

type Ratio struct {
	BasisPoints *string `json:"basisPoints"`
	Reason      string  `json:"reason,omitempty"`
}

func BasisPoints(balance, supply string) Ratio {
	b, e := UInt(balance)
	if e != nil {
		return Ratio{Reason: "invalid_balance"}
	}
	s, e := UInt(supply)
	if e != nil {
		return Ratio{Reason: "unknown_supply"}
	}
	if s.Sign() == 0 {
		return Ratio{Reason: "zero_supply"}
	}
	if b.Cmp(s) > 0 {
		return Ratio{Reason: "balance_exceeds_supply"}
	}
	v := new(big.Int).Quo(new(big.Int).Mul(b, big.NewInt(10000)), s).String()
	return Ratio{BasisPoints: &v}
}

type Balance struct {
	Address  string `json:"address"`
	Raw      string `json:"balance"`
	Category string `json:"category"`
}

// Sort validates every input and uses numeric ordering plus normalized address.
func Sort(rows []Balance) error {
	for i := range rows {
		a, e := Address(rows[i].Address)
		if e != nil {
			return e
		}
		rows[i].Address = a
		if _, e = UInt(rows[i].Raw); e != nil {
			return e
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, _ := UInt(rows[i].Raw)
		b, _ := UInt(rows[j].Raw)
		if c := a.Cmp(b); c != 0 {
			return c > 0
		}
		return rows[i].Address < rows[j].Address
	})
	return nil
}
