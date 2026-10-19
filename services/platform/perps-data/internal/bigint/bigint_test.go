package bigint

import (
	"errors"
	"testing"
)

func TestBigint(t *testing.T) {
	t.Run("add_sub", func(t *testing.T) {
		got, err := Add("123456789012345678901234567890", "987654321098765432109876543210")
		if err != nil || got != "1111111110111111111011111111100" {
			t.Fatalf("Add = %q, %v", got, err)
		}
		if got, err := Add("0", "0"); err != nil || got != "0" {
			t.Fatalf("Add zero = %q, %v", got, err)
		}
		if got, err := Sub("10", "3"); err != nil || got != "7" {
			t.Fatalf("Sub = %q, %v", got, err)
		}
		if got, err := Sub("3", "10"); err != nil || got != "0" {
			t.Fatalf("Sub clamp = %q, %v", got, err)
		}
		if got, err := Sub(Uint256Max, Uint256Max); err != nil || got != "0" {
			t.Fatalf("Sub max = %q, %v", got, err)
		}
	})

	t.Run("muldiv_floor", func(t *testing.T) {
		cases := []struct{ a, b, d, want string }{
			{"10", "3", "4", "7"},
			{"7", "1", "2", "3"},
			{"1", "1", "2", "0"},
			{"1000000000000000000", "1000000000000000", "1000000000000000000", "1000000000000000"},
			{Uint256Max, Uint256Max, Uint256Max, Uint256Max},
		}
		for _, c := range cases {
			if got, err := MulDiv(c.a, c.b, c.d); err != nil || got != c.want {
				t.Fatalf("MulDiv(%s,%s,%s) = %q, %v; want %s", c.a, c.b, c.d, got, err, c.want)
			}
		}
		if _, err := MulDiv("10", "3", "0"); !errors.Is(err, ErrDivideByZero) {
			t.Fatalf("MulDiv by zero err = %v", err)
		}
	})

	t.Run("refuses_fraction", func(t *testing.T) {
		for _, raw := range []string{"1.5", "1e18", "-1", "+1", "", " 1", "1 ", "01", "0x10", "١"} {
			if err := ValidateRaw(raw); !errors.Is(err, ErrInvalidRaw) {
				t.Fatalf("ValidateRaw(%q) = %v, want ErrInvalidRaw", raw, err)
			}
		}
		for _, raw := range []string{"0", "1", Uint256Max} {
			if err := ValidateRaw(raw); err != nil {
				t.Fatalf("ValidateRaw(%q) = %v", raw, err)
			}
		}
		if _, err := Add("1.5", "1"); !errors.Is(err, ErrInvalidRaw) {
			t.Fatalf("Add fraction err = %v", err)
		}
		if _, err := Sub("1", "-1"); !errors.Is(err, ErrInvalidRaw) {
			t.Fatalf("Sub negative err = %v", err)
		}
		if _, err := MulDiv("1", "1", "1e3"); !errors.Is(err, ErrInvalidRaw) {
			t.Fatalf("MulDiv exponent err = %v", err)
		}
	})

	t.Run("refuses_overflow", func(t *testing.T) {
		if _, err := Add(Uint256Max, "1"); !errors.Is(err, ErrOverflow) {
			t.Fatalf("Add overflow err = %v", err)
		}
		if _, err := MulDiv(Uint256Max, "2", "1"); !errors.Is(err, ErrOverflow) {
			t.Fatalf("MulDiv overflow err = %v", err)
		}
		above := "115792089237316195423570985008687907853269984665640564039457584007913129639936"
		if err := ValidateRaw(above); !errors.Is(err, ErrOverflow) {
			t.Fatalf("ValidateRaw above max = %v", err)
		}
		if err := ValidateRaw("1" + Uint256Max); !errors.Is(err, ErrOverflow) {
			t.Fatalf("ValidateRaw too long = %v", err)
		}
		if _, err := Add(above, "0"); !errors.Is(err, ErrOverflow) {
			t.Fatalf("Add input above max = %v", err)
		}
	})

	t.Run("signed_range", func(t *testing.T) {
		for _, raw := range []string{Int256Min, Int256Max, "0", "-1", "1", "-123456"} {
			if err := ValidateRawInt256(raw); err != nil {
				t.Fatalf("ValidateRawInt256(%q) = %v", raw, err)
			}
		}
		overflow := []string{
			"-57896044618658097711785492504343953926634992332820282019728792003956564819969",
			"57896044618658097711785492504343953926634992332820282019728792003956564819968",
			Uint256Max,
			"-" + Uint256Max,
		}
		for _, raw := range overflow {
			if err := ValidateRawInt256(raw); !errors.Is(err, ErrOverflow) {
				t.Fatalf("ValidateRawInt256(%q) = %v, want ErrOverflow", raw, err)
			}
		}
		for _, raw := range []string{"", "-", "-0", "--1", "+1", "-01", "-1.5", "1e3", " -1"} {
			if err := ValidateRawInt256(raw); !errors.Is(err, ErrInvalidRaw) {
				t.Fatalf("ValidateRawInt256(%q) = %v, want ErrInvalidRaw", raw, err)
			}
		}
	})
}
