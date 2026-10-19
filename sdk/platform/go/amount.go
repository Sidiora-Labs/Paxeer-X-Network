package layerx

import (
	"encoding/json"
	"math/big"
	"math/bits"
)

type Uint128 struct {
	high uint64
	low  uint64
}

func NewUint128(high uint64, low uint64) Uint128 {
	return Uint128{high: high, low: low}
}

func ParseUint128(value string) (Uint128, error) {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return Uint128{}, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return Uint128{}, newSDKError(ErrorInvalidArgument, RetryNever)
		}
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok || parsed.Sign() < 0 || parsed.BitLen() > 128 {
		return Uint128{}, newSDKError(ErrorInvalidArgument, RetryNever)
	}
	bytes := parsed.FillBytes(make([]byte, 16))
	return Uint128{
		high: uint64(bytes[0])<<56 | uint64(bytes[1])<<48 | uint64(bytes[2])<<40 | uint64(bytes[3])<<32 |
			uint64(bytes[4])<<24 | uint64(bytes[5])<<16 | uint64(bytes[6])<<8 | uint64(bytes[7]),
		low: uint64(bytes[8])<<56 | uint64(bytes[9])<<48 | uint64(bytes[10])<<40 | uint64(bytes[11])<<32 |
			uint64(bytes[12])<<24 | uint64(bytes[13])<<16 | uint64(bytes[14])<<8 | uint64(bytes[15]),
	}, nil
}

func (value Uint128) High() uint64 { return value.high }
func (value Uint128) Low() uint64  { return value.low }

func (value Uint128) String() string {
	bytes := make([]byte, 16)
	putUint64(bytes[:8], value.high)
	putUint64(bytes[8:], value.low)
	return new(big.Int).SetBytes(bytes).String()
}

func (value Uint128) Add(other Uint128) (Uint128, bool) {
	low, carry := bits.Add64(value.low, other.low, 0)
	high, overflow := bits.Add64(value.high, other.high, carry)
	return Uint128{high: high, low: low}, overflow == 0
}

func (value Uint128) Sub(other Uint128) (Uint128, bool) {
	low, borrow := bits.Sub64(value.low, other.low, 0)
	high, underflow := bits.Sub64(value.high, other.high, borrow)
	return Uint128{high: high, low: low}, underflow == 0
}

func (value Uint128) Equal(other Uint128) bool {
	return value.high == other.high && value.low == other.low
}

func (value Uint128) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value *Uint128) UnmarshalJSON(encoded []byte) error {
	if value == nil || len(encoded) < 2 || encoded[0] != '"' {
		return newSDKError(ErrorInvalidArgument, RetryNever)
	}
	var decimal string
	if err := json.Unmarshal(encoded, &decimal); err != nil {
		return newSDKError(ErrorInvalidArgument, RetryNever)
	}
	parsed, err := ParseUint128(decimal)
	if err != nil {
		return err
	}
	*value = parsed
	return nil
}
