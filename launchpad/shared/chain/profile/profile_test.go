package profile

import (
	"math/big"
	"testing"
)

func TestIdentityAndAmounts(t *testing.T) {
	p := Paxeer("v1", "d1")
	if p.Validate(big.NewInt(125)) != nil || p.Validate(big.NewInt(1)) == nil {
		t.Fatal("identity")
	}
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for _, s := range []string{"0", "1", "1000000", max.String()} {
		n, e := ParseRaw(s)
		if e != nil || n.String() != s {
			t.Fatal(s, e)
		}
	}
	for _, s := range []string{"-1", "1e6", "1.0", "+1", new(big.Int).Add(max, big.NewInt(1)).String()} {
		if _, e := ParseRaw(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, d := range []uint8{6, 18} {
		v, e := ParseUnits("1", d)
		if e != nil {
			t.Fatal(e)
		}
		s, e := FormatUnits(v.String(), d)
		if e != nil || s != "1" {
			t.Fatal(s, e)
		}
	}
	if _, e := ParseUnits("0.0000001", 6); e == nil {
		t.Fatal("precision")
	}
	formatted, err := FormatUnits(max.String(), 18)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseUnits(formatted, 18)
	if err != nil || parsed.Cmp(max) != 0 {
		t.Fatal("uint256 scaled roundtrip", formatted, err)
	}
	if VerifyUSDLDecimals(18) == nil {
		t.Fatal("metadata")
	}
}
