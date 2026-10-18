// Package l18 implements exact fee/reward primitives. It does not grant payout
// authority, sign transactions, or treat referral analytics as token balances.
package l18

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
)

var (
	ErrInvalid      = errors.New("l18: invalid input")
	ErrUnavailable  = errors.New("l18: authority unavailable")
	ErrConflict     = errors.New("l18: identity replay changed content")
	ErrUnauthorized = errors.New("l18: unauthorized")
)

const RuleVersion = "l18-v1"
const ManifestDigest = "a35ff7c6f532d2f92e4ca59e4ef2cf0a105d551ae9ac3eba68b95f79a355df4a"
const ChainID = 125
const USDL = "0x85fcd13735f4309833a503ee804ea32395851479"
const FeesRouter = "0x05681da6f611c2dffd3e92611f6de2d247647927"
const Accumulator = "0x7b14808b72f61664cdb79d25e6d1587655571a66"
const NFT = "0xe8dcc844a8006dbf64f5bb851bcad90292a6409b"

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)
var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var hash = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var canonicalEvent = regexp.MustCompile(`^125:0x[0-9a-f]{64}:(0|[1-9][0-9]{0,9})$`)
var identifier = regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`)

func amount(s string) (*big.Int, error) {
	if !decimal.MatchString(s) {
		return nil, ErrInvalid
	}
	n, _ := new(big.Int).SetString(s, 10)
	if n.BitLen() > 256 {
		return nil, ErrInvalid
	}
	return n, nil
}
func wallet(s string) (string, error) {
	s = strings.ToLower(s)
	if !address.MatchString(s) || s == "0x0000000000000000000000000000000000000000" {
		return "", ErrInvalid
	}
	return s, nil
}

// Cashback floors integer raw units; this is eligibility, never an available balance.
func Cashback(netRaw string, bps uint16, capRaw string) (string, error) {
	n, e := amount(netRaw)
	if e != nil || bps > 10000 {
		return "", ErrInvalid
	}
	cap, e := amount(capRaw)
	if e != nil {
		return "", e
	}
	n.Mul(n, new(big.Int).SetUint64(uint64(bps)))
	n.Div(n, big.NewInt(10000))
	if n.Cmp(cap) > 0 {
		n = cap
	}
	return n.String(), nil
}

// EpochShare matches FeeAccumulator's checkpoint arithmetic including final
// claimant residue. Inputs must come from the same canonical pinned block.
func EpochShare(funded, remaining, supply, claimedSupply, balance string, alreadyClaimed bool) (string, error) {
	vals := []string{funded, remaining, supply, claimedSupply, balance}
	ns := make([]*big.Int, len(vals))
	for i, v := range vals {
		n, e := amount(v)
		if e != nil {
			return "", e
		}
		ns[i] = n
	}
	f, r, s, c, b := ns[0], ns[1], ns[2], ns[3], ns[4]
	if s.Sign() == 0 || r.Cmp(f) > 0 || c.Cmp(s) > 0 || b.Cmp(s) > 0 {
		return "", ErrInvalid
	}
	if alreadyClaimed {
		return "0", nil
	}
	if new(big.Int).Add(c, b).Cmp(s) > 0 {
		return "", ErrInvalid
	}
	if b.Sign() == 0 {
		return "0", nil
	}
	// Solidity 0.8 multiplication overflow reverts before division.
	product := new(big.Int).Mul(f, b)
	if product.BitLen() > 256 {
		return "", ErrInvalid
	}
	out := product.Div(product, s)
	if new(big.Int).Add(c, b).Cmp(s) == 0 {
		out.Set(r)
	}
	if out.Cmp(r) > 0 {
		return "", ErrInvalid
	}
	return out.String(), nil
}

// Capability reports missing authority explicitly and never contains calldata.
type Capability struct {
	Available         bool   `json:"available"`
	Reason            string `json:"reason,omitempty"`
	Strategy          string `json:"strategy"`
	DeploymentVersion string `json:"deploymentVersion"`
}

func StrategyCapability(strategy uint8, snapshotSupported bool) Capability {
	names := []string{"CLAIM", "BURN", "AIRDROP", "LP_REWARDS"}
	c := Capability{DeploymentVersion: ManifestDigest}
	if int(strategy) >= len(names) {
		c.Strategy = "UNSUPPORTED"
		c.Reason = "unknown strategy"
		return c
	}
	c.Strategy = names[strategy]
	if strategy == 2 && !snapshotSupported {
		c.Reason = "checkpoint snapshot authority unavailable"
		return c
	}
	c.Available = true
	return c
}
func SharingCapability() Capability {
	return Capability{Reason: "no admitted deployed split settlement mechanism", DeploymentVersion: ManifestDigest}
}
func RewardSettlementCapability() Capability {
	return Capability{Reason: "no admitted funded reward payout adapter", DeploymentVersion: ManifestDigest}
}
