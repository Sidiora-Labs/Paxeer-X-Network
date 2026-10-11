package common

import (
	"encoding/binary"
	"math"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
)

func MethodGas(selector []byte, inputLen int) uint64 {
	base, perByte, known := methodGas(selector)
	if !known {
		return pcommon.UnknownMethodCallGas
	}
	if inputLen < 0 {
		return math.MaxUint64
	}
	length := uint64(inputLen)
	if length > (math.MaxUint64-base)/perByte {
		return math.MaxUint64
	}
	return base + perByte*length
}

func methodGas(selector []byte) (base, perByte uint64, known bool) {
	if len(selector) != 4 {
		return 0, 0, false
	}
	switch binary.BigEndian.Uint32(selector) {
	case 0x454ad3a1, 0x3df597e3, 0x3f779aef,
		0x81097a95,
		0xe087a6ba, 0x1b1fba48, 0xa8f32b0e,
		0xd61c3c34, 0xe6d341cd,
		0x99812615, 0xbf22c457, 0x5eebea20, 0xf52981f4,
		0x894e9a0d, 0x01ec24b8, 0x26f02502:
		return 1000, 3, true
	case 0x8397e9f8:
		return 6000, 3, true
	case 0xdc9c2875:
		return 30000, 30, true
	case 0x849ba0a3:
		return 25000, 30, true
	case 0x40e58ee5, 0x08e6f770:
		return 20000, 30, true
	case 0x93e5b08b:
		return 50000, 30, true
	case 0x8df82800, 0x405abb41:
		return 60000, 30, true
	case 0x0aebeb4e:
		return 80000, 30, true
	case 0x5fa55ca7, 0x71ded4b3:
		return 46000, 3, true
	case 0xac580b5c:
		return 4000, 3, true
	default:
		return 0, 0, false
	}
}
