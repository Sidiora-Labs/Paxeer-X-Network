// Package profile defines Paxeer's committed-finality and denomination boundaries.
package profile

import (
	"errors"
	"math/big"
	"strings"
)

const ChainID int64 = 125
const USDLAddress = "0x85FcD13735F4309833A503EE804ea32395851479"

type Profile struct {
	Name               string
	ChainID            int64
	NativeAsset        string
	NativeDecimals     uint8
	FinalityModel      string
	Centralized        bool
	EndpointSetVersion string
	DeploymentVersion  string
}

func Paxeer(endpoints, deployment string) Profile {
	return Profile{"paxeer-x-network", 125, "PAX", 18, "deterministic-committed", true, endpoints, deployment}
}
func (p Profile) Validate(id *big.Int) error {
	if id == nil || !id.IsInt64() || id.Int64() != 125 || p.ChainID != 125 || p.Name != "paxeer-x-network" || p.NativeAsset != "PAX" || p.NativeDecimals != 18 || p.FinalityModel != "deterministic-committed" || !p.Centralized || p.EndpointSetVersion == "" || p.DeploymentVersion == "" {
		return errors.New("incompatible chain profile")
	}
	return nil
}
func ParseRaw(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 78 {
		return nil, errors.New("invalid uint256 length")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, errors.New("raw amount must be decimal integer")
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.BitLen() > 256 {
		return nil, errors.New("uint256 overflow")
	}
	return n, nil
}
func ParseUnits(s string, decimals uint8) (*big.Int, error) {
	if decimals > 77 || len(s) > 157 {
		return nil, errors.New("invalid amount scale or length")
	}
	p := strings.Split(s, ".")
	if len(p) > 2 || p[0] == "" {
		return nil, errors.New("invalid amount")
	}
	f := ""
	if len(p) == 2 {
		f = p[1]
		if f == "" {
			return nil, errors.New("empty fraction")
		}
	}
	if len(f) > int(decimals) {
		return nil, errors.New("excess precision")
	}
	return ParseRaw(p[0] + f + strings.Repeat("0", int(decimals)-len(f)))
}
func FormatUnits(raw string, decimals uint8) (string, error) {
	n, e := ParseRaw(raw)
	if e != nil {
		return "", e
	}
	if decimals > 77 {
		return "", errors.New("invalid scale")
	}
	s := n.String()
	if decimals == 0 {
		return s, nil
	}
	if len(s) <= int(decimals) {
		s = strings.Repeat("0", int(decimals)+1-len(s)) + s
	}
	i := len(s) - int(decimals)
	f := strings.TrimRight(s[i:], "0")
	if f == "" {
		return s[:i], nil
	}
	return s[:i] + "." + f, nil
}
func VerifyUSDLDecimals(actual uint8) error {
	if actual != 6 {
		return errors.New("USDL decimal mismatch")
	}
	return nil
}
