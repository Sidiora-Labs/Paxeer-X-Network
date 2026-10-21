package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

var ErrInvalidFill = errors.New("invalid_fill")
var ErrUnsupportedResolution = errors.New("unsupported_resolution")

// RawUint256 accepts canonical ABI integers, with no floating point conversion.
func RawUint256(s string) (*big.Int, error) {
	if len(s) == 0 || len(s) > 78 || (len(s) > 1 && s[0] == '0') {
		return nil, ErrInvalidFill
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return nil, ErrInvalidFill
		}
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.BitLen() > 256 {
		return nil, ErrInvalidFill
	}
	return n, nil
}

type Rational struct {
	Numerator   string `json:"numerator"`
	Denominator string `json:"denominator"`
}

func rational(n, d *big.Int) Rational {
	r := new(big.Rat).SetFrac(n, d)
	return Rational{r.Num().String(), r.Denom().String()}
}
func (r Rational) value() (*big.Rat, error) {
	n, ok := new(big.Int).SetString(r.Numerator, 10)
	if !ok || n.Sign() < 0 {
		return nil, ErrInvalidFill
	}
	d, ok := new(big.Int).SetString(r.Denominator, 10)
	if !ok || d.Sign() <= 0 {
		return nil, ErrInvalidFill
	}
	return new(big.Rat).SetFrac(n, d), nil
}
func (r Rational) Compare(s Rational) (int, error) {
	a, e := r.value()
	if e != nil {
		return 0, e
	}
	b, e := s.value()
	if e != nil {
		return 0, e
	}
	return a.Cmp(b), nil
}

// Render uses round-half-even and bounds presentation scale separately from ABI metadata.
func (r Rational) Render(scale int) (string, error) {
	if scale < 0 || scale > 255 {
		return "", ErrInvalidFill
	}
	a, e := r.value()
	if e != nil {
		return "", e
	}
	n := new(big.Int).Mul(a.Num(), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil))
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, a.Denom(), rem)
	cmp := new(big.Int).Lsh(rem, 1).Cmp(a.Denom())
	if cmp > 0 || (cmp == 0 && q.Bit(0) == 1) {
		q.Add(q, big.NewInt(1))
	}
	s := q.String()
	if scale == 0 {
		return s, nil
	}
	for len(s) <= scale {
		s = "0" + s
	}
	return s[:len(s)-scale] + "." + s[len(s)-scale:], nil
}

type ChainOrder struct {
	Height           uint64 `json:"height"`
	TransactionIndex uint64 `json:"transactionIndex"`
	LogIndex         uint64 `json:"logIndex"`
}

func (a ChainOrder) Compare(b ChainOrder) int {
	aa := []uint64{a.Height, a.TransactionIndex, a.LogIndex}
	bb := []uint64{b.Height, b.TransactionIndex, b.LogIndex}
	for i := range aa {
		if aa[i] < bb[i] {
			return -1
		}
		if aa[i] > bb[i] {
			return 1
		}
	}
	return 0
}

// Fill mirrors frozen Trade monetary fields. Timestamp and decimals must be supplied
// by the authenticated block and asset metadata adapters, never inferred from height.
type Fill struct {
	ChainID                string     `json:"chainId"`
	EmittingContract       string     `json:"emittingContract"`
	CodeEpoch              string     `json:"codeEpoch"`
	TransactionHash        string     `json:"transactionHash"`
	PoolAddress            string     `json:"poolAddress"`
	Sender                 string     `json:"sender"`
	Position               ChainOrder `json:"position"`
	BlockTimestamp         int64      `json:"blockTimestamp"`
	TimestampAuthenticated bool       `json:"timestampAuthenticated"`
	Side                   string     `json:"side"`
	TokenAmountRaw         string     `json:"tokenAmountRaw"`
	UsdlAmountRaw          string     `json:"usdlAmountRaw"`
	PaidInputRaw           string     `json:"paidInputRaw"`
	PricedInputRaw         *string    `json:"pricedInputRaw"`
	AmmInputAfterFeeRaw    *string    `json:"ammInputAfterFeeRaw"`
	FeeRaw                 string     `json:"feeRaw"`
	FeeDisposition         string     `json:"feeDisposition"`
	InputDerivationQuality string     `json:"inputDerivationQuality"`
	TokenDecimals          int        `json:"tokenDecimals"`
	UsdlDecimals           int        `json:"usdlDecimals"`
}

func (f Fill) Price() (Rational, error) {
	if f.ChainID != "125" || !f.TimestampAuthenticated || f.BlockTimestamp < 0 || f.TokenDecimals < 0 || f.TokenDecimals > 255 || f.UsdlDecimals != 6 || (f.Side != "buy" && f.Side != "sell") {
		return Rational{}, ErrInvalidFill
	}
	token, e := RawUint256(f.TokenAmountRaw)
	if e != nil || token.Sign() == 0 {
		return Rational{}, ErrInvalidFill
	}
	usdl, e := RawUint256(f.UsdlAmountRaw)
	if e != nil || usdl.Sign() == 0 {
		return Rational{}, ErrInvalidFill
	}
	paid, e := RawUint256(f.PaidInputRaw)
	if e != nil {
		return Rational{}, e
	}
	fee, e := RawUint256(f.FeeRaw)
	if e != nil || fee.Cmp(paid) > 0 {
		return Rational{}, ErrInvalidFill
	}
	input := token
	disposition := "retained_token_reserve"
	if f.Side == "buy" {
		input = usdl
		disposition = "distributed_usdl"
	}
	if paid.Cmp(input) != 0 || f.FeeDisposition != disposition {
		return Rational{}, ErrInvalidFill
	}
	for _, s := range []*string{f.PricedInputRaw, f.AmmInputAfterFeeRaw} {
		if s != nil {
			if _, e := RawUint256(*s); e != nil {
				return Rational{}, e
			}
		}
	}
	if f.InputDerivationQuality == "exact" {
		if f.PricedInputRaw == nil || f.AmmInputAfterFeeRaw == nil {
			return Rational{}, ErrInvalidFill
		}
		p, _ := RawUint256(*f.PricedInputRaw)
		a, _ := RawUint256(*f.AmmInputAfterFeeRaw)
		if new(big.Int).Add(a, fee).Cmp(p) != 0 {
			return Rational{}, ErrInvalidFill
		}
	} else if f.InputDerivationQuality != "legacy_unavailable" && f.InputDerivationQuality != "receipt_incomplete" && f.InputDerivationQuality != "abi_unqualified" {
		return Rational{}, ErrInvalidFill
	}
	return rational(new(big.Int).Mul(usdl, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(f.TokenDecimals)), nil)), new(big.Int).Mul(token, big.NewInt(1000000))), nil
}
func (f Fill) FeeExclusivePrice() (*Rational, error) {
	if _, e := f.Price(); e != nil {
		return nil, e
	}
	if f.InputDerivationQuality != "exact" {
		return nil, nil
	}
	a, _ := RawUint256(*f.AmmInputAfterFeeRaw)
	token, _ := RawUint256(f.TokenAmountRaw)
	usdl, _ := RawUint256(f.UsdlAmountRaw)
	if f.Side == "buy" {
		usdl = a
	} else {
		token = a
	}
	if token.Sign() == 0 {
		return nil, ErrInvalidFill
	}
	r := rational(new(big.Int).Mul(usdl, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(f.TokenDecimals)), nil)), new(big.Int).Mul(token, big.NewInt(1000000)))
	return &r, nil
}

var ExactIntervals = [...]string{"1m", "5m", "15m", "1h", "4h", "1d", "1w"}

func BucketStart(timestamp int64, interval string) (int64, error) {
	if timestamp < 0 {
		return 0, ErrInvalidFill
	}
	durations := map[string]int64{"1m": 60, "5m": 300, "15m": 900, "1h": 3600, "4h": 14400, "1d": 86400}
	if d, ok := durations[interval]; ok {
		return timestamp / d * d, nil
	}
	if interval == "1w" {
		t := time.Unix(timestamp, 0).UTC()
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return day.AddDate(0, 0, -(int(day.Weekday())+6)%7).Unix(), nil
	}
	return 0, ErrUnsupportedResolution
}

type ExactCandle struct {
	PoolAddress       string     `json:"poolAddress"`
	Interval          string     `json:"interval"`
	Start             int64      `json:"start"`
	Open              Rational   `json:"open"`
	High              Rational   `json:"high"`
	Low               Rational   `json:"low"`
	Close             Rational   `json:"close"`
	FirstPosition     ChainOrder `json:"firstPosition"`
	LastPosition      ChainOrder `json:"lastPosition"`
	VolumeUsdlRaw     string     `json:"volumeUsdlRaw"`
	VolumeTokenRaw    string     `json:"volumeTokenRaw"`
	BuyVolumeUsdlRaw  string     `json:"buyVolumeUsdlRaw"`
	SellVolumeUsdlRaw string     `json:"sellVolumeUsdlRaw"`
	Trades            uint64     `json:"trades"`
	Revision          uint64     `json:"revision"`
	UniqueTraders     uint64     `json:"uniqueTraderCount"`
}

func FoldExact(c *ExactCandle, f Fill, interval string, newTrader bool) (ExactCandle, error) {
	p, e := f.Price()
	if e != nil {
		return ExactCandle{}, e
	}
	start, e := BucketStart(f.BlockTimestamp, interval)
	if e != nil {
		return ExactCandle{}, e
	}
	var out ExactCandle
	if c == nil {
		out = ExactCandle{PoolAddress: f.PoolAddress, Interval: interval, Start: start, Open: p, High: p, Low: p, Close: p, FirstPosition: f.Position, LastPosition: f.Position, VolumeUsdlRaw: "0", VolumeTokenRaw: "0", BuyVolumeUsdlRaw: "0", SellVolumeUsdlRaw: "0"}
	} else {
		out = *c
		// Aggregate sums can exceed uint256, but must remain bounded canonical
		// nonnegative integers. Corrupt storage is an error, never a panic.
		for _, amount := range []string{out.VolumeUsdlRaw, out.VolumeTokenRaw, out.BuyVolumeUsdlRaw, out.SellVolumeUsdlRaw} {
			if len(amount) == 0 || len(amount) > 1024 || (len(amount) > 1 && amount[0] == '0') {
				return out, ErrInvalidFill
			}
			for _, digit := range amount {
				if digit < '0' || digit > '9' {
					return out, ErrInvalidFill
				}
			}
		}

		if out.PoolAddress != f.PoolAddress || out.Interval != interval || out.Start != start {
			return out, ErrInvalidFill
		}
		if f.Position.Compare(out.FirstPosition) < 0 {
			out.FirstPosition = f.Position
			out.Open = p
		}
		if f.Position.Compare(out.LastPosition) > 0 {
			out.LastPosition = f.Position
			out.Close = p
		}
		cmp, e := p.Compare(out.High)
		if e != nil {
			return out, e
		}
		if cmp > 0 {
			out.High = p
		}
		cmp, e = p.Compare(out.Low)
		if e != nil {
			return out, e
		}
		if cmp < 0 {
			out.Low = p
		}
	}
	if out.Trades == ^uint64(0) || out.Revision == ^uint64(0) || (newTrader && out.UniqueTraders == ^uint64(0)) {
		return out, fmt.Errorf("counter_overflow")
	}
	add := func(a, b string) string {
		x, _ := new(big.Int).SetString(a, 10)
		y, _ := new(big.Int).SetString(b, 10)
		return x.Add(x, y).String()
	}
	out.VolumeUsdlRaw = add(out.VolumeUsdlRaw, f.UsdlAmountRaw)
	out.VolumeTokenRaw = add(out.VolumeTokenRaw, f.TokenAmountRaw)
	if f.Side == "buy" {
		out.BuyVolumeUsdlRaw = add(out.BuyVolumeUsdlRaw, f.UsdlAmountRaw)
	} else {
		out.SellVolumeUsdlRaw = add(out.SellVolumeUsdlRaw, f.UsdlAmountRaw)
	}
	out.Trades++
	out.Revision++
	if newTrader {
		out.UniqueTraders++
	}
	return out, nil
}
func decodeExact(b []byte) (*ExactCandle, error) {
	if len(b) == 0 {
		return nil, nil
	}
	var c ExactCandle
	e := json.Unmarshal(b, &c)
	return &c, e
}
