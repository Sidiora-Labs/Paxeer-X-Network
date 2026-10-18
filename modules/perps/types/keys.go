package types

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

const (
	// StoreKey is the one KV store of the module.
	StoreKey = ModuleName

	// FeesAccountName is the store account that receives every trading fee
	// and pays every maker rebate.
	FeesAccountName = "perps_fees"

	// InsuranceAccountName is the store account of the insurance fund.
	InsuranceAccountName = "perps_insurance"
)

var (
	ParamsKey                   = []byte{0x01}
	MarketPrefix                = []byte{0x02}
	MarketStatePrefix           = []byte{0x03}
	AccountPrefix               = []byte{0x04}
	OrderPrefix                 = []byte{0x05}
	BookPrefix                  = []byte{0x06}
	CloidPrefix                 = []byte{0x07}
	AccountOrderPrefix          = []byte{0x08}
	PositionPrefix              = []byte{0x09}
	AccountMarketPositionPrefix = []byte{0x0A}
	TriggerPrefix               = []byte{0x0B}
	ExpiryPrefix                = []byte{0x0C}
	VolumePrefix                = []byte{0x0D}
	VenueVolumePrefix           = []byte{0x0E}
	LiquidationCooldownPrefix   = []byte{0x0F}
	SequencePrefix              = []byte{0x10}
	AttachedTpSlPrefix          = []byte{0x11}
	MarginRatioIndexPrefix      = []byte{0x12}
	AdlRankIndexPrefix          = []byte{0x13}
	SweepCursorKey              = []byte{0x14}
	VenueBooksKey               = []byte{0x15}
)

// Sequence names of the SequencePrefix entries.
const (
	SequenceOrderID    byte = 0x01
	SequencePositionID byte = 0x02
	SequencePlacement  byte = 0x03
)

func join(prefix []byte, parts ...[]byte) []byte {
	out := append([]byte(nil), prefix...)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func uint64Bytes(value uint64) []byte { return binary.BigEndian.AppendUint64(nil, value) }

func uint256Bytes(value uint256.Int) []byte {
	word := value.Bytes32()
	return word[:]
}

// invertedBytes is 2^256-1 minus value, so ascending iteration visits the
// highest value first.
func invertedBytes(value uint256.Int) []byte {
	var inverted uint256.Int
	inverted.Not(&value)
	return uint256Bytes(inverted)
}

// BookPriceKey is the 32-byte price key of a book entry: the price itself for
// asks and 2^256-1 minus the price for bids, so ascending iteration yields the
// best price first on both sides.
func BookPriceKey(isBid bool, price uint256.Int) []byte {
	if isBid {
		return invertedBytes(price)
	}
	return uint256Bytes(price)
}

func MarketKey(marketID uint64) []byte      { return join(MarketPrefix, uint64Bytes(marketID)) }
func MarketStateKey(marketID uint64) []byte { return join(MarketStatePrefix, uint64Bytes(marketID)) }
func AccountKey(account common.Address) []byte {
	return join(AccountPrefix, account.Bytes())
}
func OrderKey(orderID uint256.Int) []byte { return join(OrderPrefix, uint256Bytes(orderID)) }

// BookSidePrefix is the prefix of one side of a market's book.
func BookSidePrefix(marketID uint64, side byte) []byte {
	return join(BookPrefix, uint64Bytes(marketID), []byte{side})
}

// BookKey orders one side of a book by price key, then placement sequence.
func BookKey(marketID uint64, side byte, priceKey []byte, sequence uint64) []byte {
	return join(BookSidePrefix(marketID, side), priceKey, uint64Bytes(sequence))
}

func CloidKey(account common.Address, marketID uint64, cloid [16]byte) []byte {
	return join(CloidPrefix, account.Bytes(), uint64Bytes(marketID), cloid[:])
}

// AccountOrderPrefixFor is the prefix of one account's order index.
func AccountOrderPrefixFor(account common.Address) []byte {
	return join(AccountOrderPrefix, account.Bytes())
}

func AccountOrderKey(account common.Address, orderID uint256.Int) []byte {
	return join(AccountOrderPrefixFor(account), uint256Bytes(orderID))
}

func PositionKey(positionID uint256.Int) []byte {
	return join(PositionPrefix, uint256Bytes(positionID))
}

func AccountMarketPositionKey(account common.Address, marketID uint64) []byte {
	return join(AccountMarketPositionPrefix, account.Bytes(), uint64Bytes(marketID))
}

// TriggerDirectionPrefix is the prefix of one trigger direction of a market.
func TriggerDirectionPrefix(marketID uint64, direction byte) []byte {
	return join(TriggerPrefix, uint64Bytes(marketID), []byte{direction})
}

func TriggerKey(marketID uint64, direction byte, priceKey []byte, sequence uint64) []byte {
	return join(TriggerDirectionPrefix(marketID, direction), priceKey, uint64Bytes(sequence))
}

func ExpiryKey(deadline uint64, orderID uint256.Int) []byte {
	return join(ExpiryPrefix, uint64Bytes(deadline), uint256Bytes(orderID))
}

func VolumeKey(account common.Address, day uint64) []byte {
	return join(VolumePrefix, account.Bytes(), uint64Bytes(day))
}

func VenueVolumeKey(day uint64) []byte { return join(VenueVolumePrefix, uint64Bytes(day)) }

func LiquidationCooldownKey(positionID uint256.Int) []byte {
	return join(LiquidationCooldownPrefix, uint256Bytes(positionID))
}

// LiquidationCooldownValueLength is the length of a prefix 0x0F value: the
// block time of the last slice (8 bytes) and the slice basis in base units
// (32 bytes).
const LiquidationCooldownValueLength = 8 + 32

func LiquidationCooldownValue(lastSliceTime uint64, sliceBasis uint256.Int) []byte {
	return join(uint64Bytes(lastSliceTime), uint256Bytes(sliceBasis))
}

func SequenceKey(name byte) []byte { return join(SequencePrefix, []byte{name}) }

func AttachedTpSlKey(positionID uint256.Int) []byte {
	return join(AttachedTpSlPrefix, uint256Bytes(positionID))
}

func MarginRatioIndexKey(bucket uint16, account common.Address) []byte {
	return join(MarginRatioIndexPrefix, binary.BigEndian.AppendUint16(nil, bucket), account.Bytes())
}

// AdlRankSidePrefix is the prefix of one side of a market's ADL ranking.
func AdlRankSidePrefix(marketID uint64, side byte) []byte {
	return join(AdlRankIndexPrefix, uint64Bytes(marketID), []byte{side})
}

// AdlRankIndexKey stores 2^256-1 minus the rank score, so ascending iteration
// yields descending rank, ties in ascending position id.
func AdlRankIndexKey(marketID uint64, side byte, rankScore, positionID uint256.Int) []byte {
	return join(AdlRankSidePrefix(marketID, side), invertedBytes(rankScore), uint256Bytes(positionID))
}
