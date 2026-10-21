package store

import (
	"encoding/json"
	"errors"
	"math/big"
	"strings"
)

var ErrDataIntegrity = errors.New("DATA_INTEGRITY")
var ErrAddressInvalid = errors.New("ADDRESS_INVALID")

// Address returns the canonical storage/cache key; checksum casing is accepted.
func Address(s string) (string, error) {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return "", ErrAddressInvalid
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return "", ErrAddressInvalid
		}
	}
	s = strings.ToLower(s)
	if s == "0x"+strings.Repeat("0", 40) {
		return "", ErrAddressInvalid
	}
	return s, nil
}

// UInt256 rejects noncanonical text and values outside the actual uint256 range.
type UInt256 string

func ParseUInt256(s string) (UInt256, error) {
	if len(s) == 0 || len(s) > 78 || len(s) > 1 && s[0] == '0' {
		return "", ErrDataIntegrity
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return "", ErrDataIntegrity
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.BitLen() > 256 {
		return "", ErrDataIntegrity
	}
	return UInt256(s), nil
}
func (u UInt256) MarshalJSON() ([]byte, error) {
	if _, err := ParseUInt256(string(u)); err != nil {
		return nil, err
	}
	return json.Marshal(string(u))
}
func (u *UInt256) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) != nil {
		return ErrDataIntegrity
	}
	v, e := ParseUInt256(s)
	if e == nil {
		*u = v
	}
	return e
}

type Money struct {
	Raw          UInt256 `json:"raw"`
	Decimals     uint8   `json:"decimals"`
	AssetAddress *string `json:"asset_address,omitempty"`
	NativeAsset  string  `json:"native_asset,omitempty"`
	ChainID      string  `json:"chain_id"`
}

// PoolCreationFee is protocol USDL, distinct from any native PAX gas estimate.
func PoolCreationFee(usdl string) (Money, error) {
	a, e := Address(usdl)
	if e != nil {
		return Money{}, e
	}
	return Money{Raw: "1000000", Decimals: 6, AssetAddress: &a, ChainID: "125"}, nil
}

const PublicJSONMaxBytes = 256 << 10

func ValidatePublicJSON(body []byte) error {
	if len(body) == 0 || len(body) > PublicJSONMaxBytes || !json.Valid(body) {
		return ErrDataIntegrity
	}
	return nil
}
