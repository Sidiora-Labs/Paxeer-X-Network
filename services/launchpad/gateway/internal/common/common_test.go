package common

import "testing"

func TestNormalizeAddr(t *testing.T) {
	if got := NormalizeAddr("  0xABCdef  "); got != "0xabcdef" {
		t.Errorf("NormalizeAddr = %q", got)
	}
}

func TestIsAddr(t *testing.T) {
	valid := "0x" + "ab12" + "00000000000000000000000000000000000000"[:36]
	if !IsAddr("0x1234567890abcdef1234567890ABCDEF12345678") {
		t.Error("mixed-case 40-hex should normalize to valid")
	}
	_ = valid
	for _, bad := range []string{"", "0x123", "1234567890abcdef1234567890abcdef12345678", "0xzz34567890abcdef1234567890abcdef12345678"} {
		if IsAddr(bad) {
			t.Errorf("IsAddr(%q) = true, want false", bad)
		}
	}
}
