package analytics

import (
	"math"
	"testing"
)

func TestPressureExact(t *testing.T) {
	p, e := CalculatePressure(3, 1, "3000001", "1000000", true)
	if e != nil || *p.BuyCountShare != 7500 || *p.BuyNotionalShare != 7500 || p.Direction != "bullish" || len(p.Flags) != 2 {
		t.Fatalf("%+v %v", p, e)
	}
	p, e = CalculatePressure(0, 0, "0", "0", false)
	if e != nil || p.BuyCountShare != nil || p.Direction != "unavailable" {
		t.Fatal(p, e)
	}
	p, e = CalculatePressure(math.MaxInt64, math.MaxInt64, "1", "1", false)
	if e != nil || *p.BuyCountShare != 5000 {
		t.Fatal(p, e)
	}
	for _, raw := range []string{"-1", "1.0", "01", "0x1", "999999999999999999999999999999999999999999999999999999999999999999999999999999"} {
		if _, e := CalculatePressure(1, 1, raw, "1", false); e == nil {
			t.Fatal(raw)
		}
	}
}
func TestPressureThresholds(t *testing.T) {
	for _, c := range []struct {
		b, s int64
		want string
	}{{55, 45, "bullish"}, {45, 55, "bearish"}, {54999, 45001, "neutral"}, {45001, 54999, "neutral"}} {
		p, e := CalculatePressure(c.b, c.s, "0", "0", false)
		if e != nil || p.Direction != c.want {
			t.Fatal(p, e)
		}
	}
}

func TestPressureAggregatesPreserveIntegerUnits(t *testing.T) {
	maximum := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	double := "231584178474632390847141970017375815706539969331281128078915168015826259279870"
	p, err := CalculatePressure(2, 1, double, maximum, false)
	if err != nil || p.BuyCountShare == nil || *p.BuyCountShare != 6666 || p.BuyNotionalShare == nil || *p.BuyNotionalShare != 6666 || p.BuyUSDLRaw != double || p.SellUSDLRaw != maximum {
		t.Fatal(p, err)
	}
	for _, c := range []struct {
		count int64
		raw   string
	}{{0, "1"}, {1, double}, {1, "1e6"}, {1, " 1"}, {1, "+1"}, {-1, "0"}} {
		if _, err := CalculatePressure(c.count, 1, c.raw, "1", false); err == nil {
			t.Fatal("invalid aggregate accepted", c)
		}
	}
}
