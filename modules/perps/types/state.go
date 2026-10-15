package types

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// Sides of a book entry, a position and an ADL rank entry, numbered as
// lx_perps_side: a bid is a buy and a long, an ask is a sell and a short.
const (
	SideBuy  byte = 1
	SideSell byte = 2
)

// Directions of the trigger index: a rising trigger fires at mark >= trigger
// and is keyed by its price, a falling trigger fires at mark <= trigger and is
// keyed by 2^256-1 minus its price, so each direction iterates in crossing
// order.
const (
	TriggerRising  byte = 1
	TriggerFalling byte = 2
)

// SideOf is the side byte of a long (buy) or short (sell).
func SideOf(isLong bool) byte {
	if isLong {
		return SideBuy
	}
	return SideSell
}

// OrderType numbers the order types of contracts/perps.md section 2.
type OrderType uint8

const (
	OrderTypeLimit OrderType = iota
	OrderTypeStopLimit
	OrderTypeTakeProfit
	OrderTypeStopLoss
	OrderTypeMarket
	OrderTypeStopMarket
)

// TimeInForce numbers the time-in-force values of lx_perps_time_in_force.
type TimeInForce uint8

const (
	TimeInForceGTC TimeInForce = iota
	TimeInForceIOC
	TimeInForceFOK
	TimeInForcePostOnly
)

// OrderStatus numbers the order statuses of contracts/perps.md section 2.
type OrderStatus uint8

const (
	OrderStatusUntriggered OrderStatus = iota
	OrderStatusResting
	OrderStatusFilled
	OrderStatusCancelled
	OrderStatusExpired
)

// Every 256-bit field of the records below is a uint256.Int; a field
// documented as signed holds an int256 in two's complement, as the EVM does.
// Prices and USD amounts are 18-decimal fixed point, base sizes 18-decimal
// base units, collateral USDL units of 6 decimals, times Unix seconds of block
// time unless named in milliseconds.

// Order is the binary value of prefix 0x05 and replaces lx_perps_order. A
// resting order rests at LimitPrice; an untriggered order waits in the trigger
// index at TriggerPrice. TakeProfitPrice and StopLossPrice are the TP/SL pair
// attached to the position when the order fills, 0 for none.
type Order struct {
	ID              uint256.Int    `json:"id"`
	Account         common.Address `json:"account"`
	MarketID        uint64         `json:"market_id"`
	IsLong          bool           `json:"is_long"`
	Type            OrderType      `json:"type"`
	TimeInForce     TimeInForce    `json:"time_in_force"`
	Status          OrderStatus    `json:"status"`
	ReduceOnly      bool           `json:"reduce_only"`
	LimitPrice      uint256.Int    `json:"limit_price"`
	TriggerPrice    uint256.Int    `json:"trigger_price"`
	AcceptablePrice uint256.Int    `json:"acceptable_price"`
	SizeUsd         uint256.Int    `json:"size_usd"`
	SizeBase        uint256.Int    `json:"size_base"`
	FilledBase      uint256.Int    `json:"filled_base"`
	Leverage        uint256.Int    `json:"leverage"`
	TakeProfitPrice uint256.Int    `json:"take_profit_price"`
	StopLossPrice   uint256.Int    `json:"stop_loss_price"`
	Deadline        uint64         `json:"deadline"`
	Cloid           [16]byte       `json:"cloid"`
	Sequence        uint64         `json:"sequence"`
}

// Position is the binary value of prefix 0x09 and replaces lx_perps_position.
// CostBasisUsd is SizeBase x ReferencePrice / 1e18 and FundingBasis (signed)
// is SizeBase x the market's cumulative funding index at the last settlement;
// EntryPrice is the displayed entry price and RankScore the ADL rank score of
// the last settlement.
type Position struct {
	ID             uint256.Int    `json:"id"`
	Account        common.Address `json:"account"`
	MarketID       uint64         `json:"market_id"`
	IsLong         bool           `json:"is_long"`
	SizeBase       uint256.Int    `json:"size_base"`
	ReferencePrice uint256.Int    `json:"reference_price"`
	CostBasisUsd   uint256.Int    `json:"cost_basis_usd"`
	FundingBasis   uint256.Int    `json:"funding_basis"`
	EntryPrice     uint256.Int    `json:"entry_price"`
	Leverage       uint256.Int    `json:"leverage"`
	RankScore      uint256.Int    `json:"rank_score"`
	OpenedAt       uint64         `json:"opened_at"`
}

// Account is the binary value of prefix 0x04: Collateral in USDL units and the
// unrecovered deficit in USD.
type Account struct {
	Address           common.Address `json:"address"`
	Collateral        uint256.Int    `json:"collateral"`
	DeficitUsd        uint256.Int    `json:"deficit_usd"`
	OpenOrderCount    uint64         `json:"open_order_count"`
	PositionCount     uint64         `json:"position_count"`
	MarginRatioBucket uint16         `json:"margin_ratio_bucket"`
	Created           bool           `json:"created"`
}

// MarketState is the binary value of prefix 0x03 and replaces
// lx_perps_funding_state. PremiumEma (EMA of mid minus oracle), FundingIndex
// (cumulative funding per unit of base), HourlyRate and PremiumSum are signed;
// OracleSum is the sum of the index prices at the premium samples of the
// hour; LastIndexHour is floor(block time / 3600) of the last index step and the
// open interest is in base units.
type MarketState struct {
	MarketID          uint64      `json:"market_id"`
	IndexPrice        uint256.Int `json:"index_price"`
	PricefeedMark     uint256.Int `json:"pricefeed_mark"`
	PriceTimestampMs  uint64      `json:"price_timestamp_ms"`
	Fresh             bool        `json:"fresh"`
	MarkPrice         uint256.Int `json:"mark_price"`
	PremiumEma        uint256.Int `json:"premium_ema"`
	LastTradePrice    uint256.Int `json:"last_trade_price"`
	FundingIndex      uint256.Int `json:"funding_index"`
	HourlyRate        uint256.Int `json:"hourly_rate"`
	PremiumSum        uint256.Int `json:"premium_sum"`
	OracleSum         uint256.Int `json:"oracle_sum"`
	PremiumCount      uint64      `json:"premium_count"`
	LastSampleTime    uint64      `json:"last_sample_time"`
	LastIndexHour     uint64      `json:"last_index_hour"`
	LongOpenInterest  uint256.Int `json:"long_open_interest"`
	ShortOpenInterest uint256.Int `json:"short_open_interest"`
}

// VenueBooks is the binary value of prefix 0x15: the rounding residue held in
// the venue's USDL balance and the sum of account deficits, both in USD.
type VenueBooks struct {
	ResidueUsd    uint256.Int `json:"residue_usd"`
	DeficitSumUsd uint256.Int `json:"deficit_sum_usd"`
}

type recordWriter []byte

func (w *recordWriter) word(value uint256.Int) {
	bytes := value.Bytes32()
	*w = append(*w, bytes[:]...)
}
func (w *recordWriter) u64(value uint64)         { *w = binary.BigEndian.AppendUint64(*w, value) }
func (w *recordWriter) u16(value uint16)         { *w = binary.BigEndian.AppendUint16(*w, value) }
func (w *recordWriter) u8(value uint8)           { *w = append(*w, value) }
func (w *recordWriter) raw(value []byte)         { *w = append(*w, value...) }
func (w *recordWriter) address(a common.Address) { w.raw(a.Bytes()) }
func (w *recordWriter) flag(value bool) {
	if value {
		w.u8(1)
		return
	}
	w.u8(0)
}

// recordReader decodes a fixed layout; the first short read or non-canonical
// flag sets err and every later read returns zero values.
type recordReader struct {
	name string
	buf  []byte
	err  error
}

func (r *recordReader) take(n int) []byte {
	if r.err != nil {
		return make([]byte, n)
	}
	if len(r.buf) < n {
		r.err = fmt.Errorf("perps: %s record truncated", r.name)
		return make([]byte, n)
	}
	out := r.buf[:n]
	r.buf = r.buf[n:]
	return out
}

func (r *recordReader) word() uint256.Int {
	var value uint256.Int
	value.SetBytes32(r.take(32))
	return value
}
func (r *recordReader) u64() uint64 { return binary.BigEndian.Uint64(r.take(8)) }
func (r *recordReader) u16() uint16 { return binary.BigEndian.Uint16(r.take(2)) }
func (r *recordReader) u8() uint8   { return r.take(1)[0] }
func (r *recordReader) address() common.Address {
	return common.BytesToAddress(r.take(common.AddressLength))
}
func (r *recordReader) flag() bool {
	value := r.u8()
	if value > 1 && r.err == nil {
		r.err = fmt.Errorf("perps: %s record flag %d is not 0 or 1", r.name, value)
	}
	return value == 1
}

func (r *recordReader) finish() error {
	if r.err == nil && len(r.buf) != 0 {
		r.err = fmt.Errorf("perps: %s record has %d trailing bytes", r.name, len(r.buf))
	}
	return r.err
}

// Marshal is the fixed binary encoding of the order, fields in declaration
// order, integers big-endian, 256-bit fields as 32 bytes.
func (o Order) Marshal() []byte {
	var w recordWriter
	w.word(o.ID)
	w.address(o.Account)
	w.u64(o.MarketID)
	w.flag(o.IsLong)
	w.u8(uint8(o.Type))
	w.u8(uint8(o.TimeInForce))
	w.u8(uint8(o.Status))
	w.flag(o.ReduceOnly)
	for _, value := range []uint256.Int{
		o.LimitPrice, o.TriggerPrice, o.AcceptablePrice, o.SizeUsd, o.SizeBase,
		o.FilledBase, o.Leverage, o.TakeProfitPrice, o.StopLossPrice,
	} {
		w.word(value)
	}
	w.u64(o.Deadline)
	w.raw(o.Cloid[:])
	w.u64(o.Sequence)
	return w
}

// Unmarshal decodes the encoding of Marshal and refuses any other length or a
// flag byte other than 0 and 1.
func (o *Order) Unmarshal(bz []byte) error {
	r := recordReader{name: "order", buf: bz}
	var out Order
	out.ID = r.word()
	out.Account = r.address()
	out.MarketID = r.u64()
	out.IsLong = r.flag()
	out.Type = OrderType(r.u8())
	out.TimeInForce = TimeInForce(r.u8())
	out.Status = OrderStatus(r.u8())
	out.ReduceOnly = r.flag()
	for _, field := range []*uint256.Int{
		&out.LimitPrice, &out.TriggerPrice, &out.AcceptablePrice, &out.SizeUsd, &out.SizeBase,
		&out.FilledBase, &out.Leverage, &out.TakeProfitPrice, &out.StopLossPrice,
	} {
		*field = r.word()
	}
	out.Deadline = r.u64()
	copy(out.Cloid[:], r.take(len(out.Cloid)))
	out.Sequence = r.u64()
	if err := r.finish(); err != nil {
		return err
	}
	*o = out
	return nil
}

// Marshal is the fixed binary encoding of the position.
func (p Position) Marshal() []byte {
	var w recordWriter
	w.word(p.ID)
	w.address(p.Account)
	w.u64(p.MarketID)
	w.flag(p.IsLong)
	for _, value := range []uint256.Int{
		p.SizeBase, p.ReferencePrice, p.CostBasisUsd, p.FundingBasis, p.EntryPrice, p.Leverage, p.RankScore,
	} {
		w.word(value)
	}
	w.u64(p.OpenedAt)
	return w
}

// Unmarshal decodes the encoding of Marshal.
func (p *Position) Unmarshal(bz []byte) error {
	r := recordReader{name: "position", buf: bz}
	var out Position
	out.ID = r.word()
	out.Account = r.address()
	out.MarketID = r.u64()
	out.IsLong = r.flag()
	for _, field := range []*uint256.Int{
		&out.SizeBase, &out.ReferencePrice, &out.CostBasisUsd, &out.FundingBasis, &out.EntryPrice,
		&out.Leverage, &out.RankScore,
	} {
		*field = r.word()
	}
	out.OpenedAt = r.u64()
	if err := r.finish(); err != nil {
		return err
	}
	*p = out
	return nil
}

// Marshal is the fixed binary encoding of the account.
func (a Account) Marshal() []byte {
	var w recordWriter
	w.address(a.Address)
	w.word(a.Collateral)
	w.word(a.DeficitUsd)
	w.u64(a.OpenOrderCount)
	w.u64(a.PositionCount)
	w.u16(a.MarginRatioBucket)
	w.flag(a.Created)
	return w
}

// Unmarshal decodes the encoding of Marshal.
func (a *Account) Unmarshal(bz []byte) error {
	r := recordReader{name: "account", buf: bz}
	var out Account
	out.Address = r.address()
	out.Collateral = r.word()
	out.DeficitUsd = r.word()
	out.OpenOrderCount = r.u64()
	out.PositionCount = r.u64()
	out.MarginRatioBucket = r.u16()
	out.Created = r.flag()
	if err := r.finish(); err != nil {
		return err
	}
	*a = out
	return nil
}

// Marshal is the fixed binary encoding of the market state.
func (m MarketState) Marshal() []byte {
	var w recordWriter
	w.u64(m.MarketID)
	w.word(m.IndexPrice)
	w.word(m.PricefeedMark)
	w.u64(m.PriceTimestampMs)
	w.flag(m.Fresh)
	for _, value := range []uint256.Int{
		m.MarkPrice, m.PremiumEma, m.LastTradePrice, m.FundingIndex, m.HourlyRate, m.PremiumSum, m.OracleSum,
	} {
		w.word(value)
	}
	w.u64(m.PremiumCount)
	w.u64(m.LastSampleTime)
	w.u64(m.LastIndexHour)
	w.word(m.LongOpenInterest)
	w.word(m.ShortOpenInterest)
	return w
}

// Unmarshal decodes the encoding of Marshal.
func (m *MarketState) Unmarshal(bz []byte) error {
	r := recordReader{name: "market state", buf: bz}
	var out MarketState
	out.MarketID = r.u64()
	out.IndexPrice = r.word()
	out.PricefeedMark = r.word()
	out.PriceTimestampMs = r.u64()
	out.Fresh = r.flag()
	for _, field := range []*uint256.Int{
		&out.MarkPrice, &out.PremiumEma, &out.LastTradePrice, &out.FundingIndex, &out.HourlyRate, &out.PremiumSum, &out.OracleSum,
	} {
		*field = r.word()
	}
	out.PremiumCount = r.u64()
	out.LastSampleTime = r.u64()
	out.LastIndexHour = r.u64()
	out.LongOpenInterest = r.word()
	out.ShortOpenInterest = r.word()
	if err := r.finish(); err != nil {
		return err
	}
	*m = out
	return nil
}

// Marshal is the fixed binary encoding of the venue books.
func (v VenueBooks) Marshal() []byte {
	var w recordWriter
	w.word(v.ResidueUsd)
	w.word(v.DeficitSumUsd)
	return w
}

// Unmarshal decodes the encoding of Marshal.
func (v *VenueBooks) Unmarshal(bz []byte) error {
	r := recordReader{name: "venue books", buf: bz}
	var out VenueBooks
	out.ResidueUsd = r.word()
	out.DeficitSumUsd = r.word()
	if err := r.finish(); err != nil {
		return err
	}
	*v = out
	return nil
}
