package util

import "testing"

// Golden values produced by running the actual TS shared algorithms
// (hashToInt64 / formatPrice / formatVolume) on Node v24 — parity fixtures.
func TestHashToInt64Parity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"a", 97},
		{"indexer:swap", 2760782684373639982},
		{"0x322170E27d0c5Bd252337791fadED31dc4E85cA6", 5464066984825564289},
		{"pool:0xabc", 3059060046206012},
		{"héllo", 103094734}, // single UTF-16 code unit for é
		{"🚀", 1773027},       // surrogate pair: two UTF-16 code units
	}
	for _, c := range cases {
		if got := HashToInt64(c.in); got != c.want {
			t.Errorf("HashToInt64(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestHashToInt64Deterministic(t *testing.T) {
	t.Parallel()
	const k = "pool:0xdeadbeef"
	first := HashToInt64(k)
	second := HashToInt64(k)
	if first != second {
		t.Fatalf("HashToInt64 not deterministic: %d != %d", first, second)
	}
	if got := HashToInt64(k); got < 0 {
		t.Fatalf("HashToInt64(%q) = %d, must be non-negative (fits pg int8)", k, got)
	}
}

func TestFormatPriceParity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"0", "0.00000000"},
		{"1", "0.00000000"},
		{"1000000000000000000", "1.00000000"},
		{"1500000000000000000", "1.50000000"},
		{"123456789012345678901234567890", "123456789012.34567890"},
		{"999999999999999999", "0.99999999"},
	}
	for _, c := range cases {
		got, err := FormatPrice(c.in)
		if err != nil {
			t.Errorf("FormatPrice(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("FormatPrice(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatVolumeParity(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"0", "0.00"},
		{"1", "0.00"},
		{"1000000000000000000", "1.00"},
		{"1500000000000000000", "1.50"},
		{"123456789012345678901234567890", "123456789012.34"},
		{"999999999999999999", "0.99"},
	}
	for _, c := range cases {
		got, err := FormatVolume(c.in)
		if err != nil {
			t.Errorf("FormatVolume(%q) error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("FormatVolume(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatErrors(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "abc", "1.5", "0x10", "-1", "  "} {
		if _, err := FormatPrice(in); err == nil {
			t.Errorf("FormatPrice(%q) expected error, got nil", in)
		}
	}
}

func TestFormatTrimsWhitespace(t *testing.T) {
	t.Parallel()
	got, err := FormatPrice("  1000000000000000000  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "1.00000000" {
		t.Fatalf("got %q, want %q", got, "1.00000000")
	}
}
