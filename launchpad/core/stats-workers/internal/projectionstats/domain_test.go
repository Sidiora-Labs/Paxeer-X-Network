package projectionstats

import (
	"errors"
	"strings"
	"testing"
)

const usdl = "0x2222222222222222222222222222222222222222"
const token = "0x1111111111111111111111111111111111111111"
const poolAddress = "0x3333333333333333333333333333333333333333"

func ptr(s string) *string { return &s }
func fixture(side string, log string) Trade {
	t := Trade{PoolAddress: poolAddress, Sender: "0x4444444444444444444444444444444444444444", Side: side,
		Position: Position{"125", "80", "2", log, "0x" + strings.Repeat("a", 64), "0x" + strings.Repeat("b", 64)},
		FeeRaw:   "27000", FeeAssetAddress: usdl, FeeAssetDecimals: 6, FeeDisposition: "distributed_usdl", PaidInputRaw: ptr("10000000"), PricedInputRaw: ptr("9000000"), AMMInputAfterFeeRaw: ptr("8973000"),
		TokenAmountRaw: "2000000", USDLAmountRaw: "10000000", InputDerivationQuality: "exact", RecipientAttribution: "unavailable", RecipientEvidenceRefs: []string{}, EmittingContract: poolAddress, CodeEpoch: "fixture-source-v1"}
	if side == "sell" {
		t.TokenAmountRaw = "1000000"
		t.USDLAmountRaw = "456789"
		t.PaidInputRaw = ptr("1000000")
		t.PricedInputRaw = ptr("900000")
		t.AMMInputAfterFeeRaw = ptr("897300")
		t.FeeRaw = "2700"
		t.FeeAssetAddress = token
		t.FeeAssetDecimals = 18
		t.FeeDisposition = "retained_token_reserve"
	}
	t.EventID = t.Position.Identity()
	return t
}
func TestExactAccounting(t *testing.T) {
	for _, tc := range []struct{ side, volume, reserve string }{{"buy", "10000000", "9973000"}, {"sell", "456789", "1000000"}} {
		a, e := Account(fixture(tc.side, "7"), PoolAssets{usdl, token, 18}, 30)
		if e != nil || a.TradedUSDLRaw != tc.volume || a.InputReserveIncrementRaw != tc.reserve {
			t.Fatalf("%s: %+v %v", tc.side, a, e)
		}
	}
	b := fixture("buy", "1")
	b.PricedInputRaw = ptr("999")
	b.FeeRaw = "2"
	b.AMMInputAfterFeeRaw = ptr("997")
	if _, e := Account(b, PoolAssets{usdl, token, 18}, 30); e != nil {
		t.Fatal(e)
	}
	b.FeeRaw = "3"
	if _, e := Account(b, PoolAssets{usdl, token, 18}, 30); !errors.Is(e, ErrInvalid) {
		t.Fatal("accepted incorrect floor fee")
	}
}
func TestUnknownAndUnits(t *testing.T) {
	tr := fixture("sell", "1")
	tr.PricedInputRaw = nil
	tr.AMMInputAfterFeeRaw = nil
	tr.InputDerivationQuality = "receipt_incomplete"
	if _, e := Account(tr, PoolAssets{usdl, token, 18}, 30); e != nil {
		t.Fatal(e)
	}
	tr.FeeAssetAddress = usdl
	if _, e := Account(tr, PoolAssets{usdl, token, 18}, 30); e == nil {
		t.Fatal("cross asset fee accepted")
	}
	tr = fixture("buy", "1")
	tr.Recipient = ptr(tr.Sender)
	if _, e := Account(tr, PoolAssets{usdl, token, 18}, 30); e == nil {
		t.Fatal("unproven recipient accepted")
	}
}
func TestBoundsTimeAndOrder(t *testing.T) {
	for _, s := range []string{"01", "-1", "1e3", "", strings.Repeat("9", 79), "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		if _, e := Uint(s); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if _, e := Uint("115792089237316195423570985008687907853269984665640564039457584007913129639935"); e != nil {
		t.Fatal(e)
	}
	a, _ := Minute(119)
	b, _ := Minute(120)
	if a != 60 || b != 120 {
		t.Fatal(a, b)
	}
	if !InWindow(0, 3599, 3600) || InWindow(0, 3600, 3600) || InWindow(4000, 3600, 3600) {
		t.Fatal("window boundary")
	}
	if ValidateTime(121, 119, 1) == nil {
		t.Fatal("future accepted")
	}
	p := fixture("buy", "9").Position
	q := p
	q.TransactionIndex = "3"
	q.LogIndex = "1"
	if p.Compare(q) >= 0 {
		t.Fatal("position order")
	}
	p.Height = "9007199254740993"
	q.Height = "9007199254740992"
	if p.Compare(q) <= 0 {
		t.Fatal("precision loss")
	}
}
func TestCursorBinding(t *testing.T) {
	f := Filter{poolAddress, "buy", "v1"}
	c, e := EncodeCursor(f, fixture("buy", "7").Position, "80")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeCursor(f, c); e != nil {
		t.Fatal(e)
	}
	f.Side = "sell"
	if _, e = DecodeCursor(f, c); e == nil {
		t.Fatal("cursor accepted for different filter")
	}
}

func TestOptionalRateStillChecksExactInputAndUnits(t *testing.T) {
	trade := fixture("sell", "7")
	amounts, err := accountAmounts(trade, PoolAssets{usdl, token, 18}, nil, true)
	if err != nil || amounts.TradedUSDLRaw != "456789" || amounts.InputReserveIncrementRaw != "1000000" {
		t.Fatal(amounts, err)
	}
	trade.AMMInputAfterFeeRaw = ptr("897301")
	if _, err = accountAmounts(trade, PoolAssets{usdl, token, 18}, nil, true); err == nil {
		t.Fatal("unknown rate bypassed net arithmetic")
	}
	trade = fixture("sell", "7")
	trade.FeeAssetAddress = usdl
	if _, err = accountAmounts(trade, PoolAssets{usdl, token, 18}, nil, true); err == nil {
		t.Fatal("unknown rate bypassed asset units")
	}
	trade = fixture("buy", "7")
	trade.PricedInputRaw = nil
	trade.AMMInputAfterFeeRaw = nil
	trade.InputDerivationQuality = "receipt_incomplete"
	if _, err = accountAmounts(trade, PoolAssets{usdl, token, 18}, nil, true); err != nil {
		t.Fatal("actual raw amounts require no guessed rate", err)
	}
}
