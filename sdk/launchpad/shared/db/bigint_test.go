package db

import (
	"testing"
	"time"
)

// checker returns a closure that asserts no error and yields the value, so it
// can wrap a (string, error) call directly: ok(BigintMax("5","3")).
func checker(t *testing.T) func(string, error) string {
	return func(got string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return got
	}
}

func TestBigintMaxMin(t *testing.T) {
	t.Parallel()
	ok := checker(t)
	if got := ok(BigintMax("5", "3")); got != "5" {
		t.Errorf("BigintMax(5,3) = %s", got)
	}
	if got := ok(BigintMax("3", "5")); got != "5" {
		t.Errorf("BigintMax(3,5) = %s", got)
	}
	if got := ok(BigintMin("5", "3")); got != "3" {
		t.Errorf("BigintMin(5,3) = %s", got)
	}
	// Equal returns first arg (parity with >= / <=).
	if got := ok(BigintMax("7", "7")); got != "7" {
		t.Errorf("BigintMax(7,7) = %s", got)
	}
	// uint256-scale values.
	big1 := "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	if got := ok(BigintMax(big1, "1")); got != big1 {
		t.Errorf("BigintMax with uint256 max failed: %s", got)
	}
}

func TestBigintAddSub(t *testing.T) {
	t.Parallel()
	ok := checker(t)
	if got := ok(BigintAdd("1000000000000000000", "2000000000000000000")); got != "3000000000000000000" {
		t.Errorf("BigintAdd = %s", got)
	}
	// Golden vs TS bigintSub.
	if got := ok(BigintSub("5", "3")); got != "2" {
		t.Errorf("BigintSub(5,3) = %s, want 2", got)
	}
	if got := ok(BigintSub("3", "5")); got != "0" {
		t.Errorf("BigintSub(3,5) = %s, want 0 (clamped)", got)
	}
}

func TestBigintMulDiv(t *testing.T) {
	t.Parallel()
	ok := checker(t)
	// Golden vs TS: (1.5e18 * 1e15) / 1e18 = 1.5e15.
	if got := ok(BigintMulDiv("1500000000000000000", "1000000000000000", "1000000000000000000")); got != "1500000000000000" {
		t.Errorf("BigintMulDiv = %s, want 1500000000000000", got)
	}
	if _, err := BigintMulDiv("1", "1", "0"); err == nil {
		t.Error("divide by zero should error")
	}
}

func TestComputeMarketCap(t *testing.T) {
	t.Parallel()
	ok := checker(t)
	cases := map[string]string{
		"1000000000000000000": "1000000000000000", // golden vs TS
		"0":                   "0",
		"":                    "0",
		"2500000000000000000": "2500000000000000", // golden vs TS
	}
	for in, want := range cases {
		if got := ok(ComputeMarketCap(in)); got != want {
			t.Errorf("ComputeMarketCap(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestBigintErrors(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"", "abc", "1.5", "0x1"} {
		if _, err := BigintAdd(bad, "1"); err == nil {
			t.Errorf("BigintAdd(%q,1) expected error", bad)
		}
		if _, err := BigintAdd("1", bad); err == nil {
			t.Errorf("BigintAdd(1,%q) expected error", bad)
		}
	}
	if _, err := BigintMax("x", "1"); err == nil {
		t.Error("BigintMax bad a should error")
	}
	if _, err := BigintMax("1", "x"); err == nil {
		t.Error("BigintMax bad b should error")
	}
	if _, err := BigintMin("x", "1"); err == nil {
		t.Error("BigintMin bad a should error")
	}
	if _, err := BigintMin("1", "x"); err == nil {
		t.Error("BigintMin bad b should error")
	}
	if _, err := BigintSub("x", "1"); err == nil {
		t.Error("BigintSub bad a should error")
	}
	if _, err := BigintSub("1", "x"); err == nil {
		t.Error("BigintSub bad b should error")
	}
	if _, err := BigintMulDiv("x", "1", "1"); err == nil {
		t.Error("BigintMulDiv bad a should error")
	}
	if _, err := BigintMulDiv("1", "x", "1"); err == nil {
		t.Error("BigintMulDiv bad b should error")
	}
	if _, err := BigintMulDiv("1", "1", "x"); err == nil {
		t.Error("BigintMulDiv bad divisor should error")
	}
}

func TestNowSeconds(t *testing.T) {
	t.Parallel()
	got := NowSeconds()
	if delta := time.Now().Unix() - got; delta < 0 || delta > 2 {
		t.Errorf("NowSeconds() = %d, too far from now", got)
	}
}

func TestRawUint256Boundaries(t *testing.T) {
	for _, s := range []string{"0", "9", "100", Uint256Max} {
		if err := ValidateRawUint256(s); err != nil {
			t.Errorf("valid %q: %v", s, err)
		}
	}
	for _, s := range []string{"", "-1", "+1", "01", "00", "1e18", "1.0", " 1", "1 ", "0x10", "١", Uint256Max[:76] + "936", string(make([]byte, 1000000))} {
		if ValidateRawUint256(s) == nil {
			t.Errorf("accepted invalid length %d", len(s))
		}
	}
}
