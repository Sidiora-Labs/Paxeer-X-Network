package common

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeAddr(t *testing.T) {
	cases := map[string]string{
		"  0xABCdef0000000000000000000000000000000001  ": "0xabcdef0000000000000000000000000000000001",
		"0X00":  "0x00",
		"":      "",
		"MiXeD": "mixed",
	}
	for in, want := range cases {
		if got := NormalizeAddr(in); got != want {
			t.Errorf("NormalizeAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsAddr(t *testing.T) {
	valid := "0x" + strings.Repeat("a", 40)
	if !IsAddr(valid) {
		t.Errorf("IsAddr(%q) = false, want true", valid)
	}
	for _, bad := range []string{"", "0x123", "not-an-address", "0x" + strings.Repeat("g", 40), "0x" + strings.Repeat("A", 40)} {
		if IsAddr(bad) {
			t.Errorf("IsAddr(%q) = true, want false", bad)
		}
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"<script>alert(1)</script>hi": "alert(1)hi",
		"  spaced  ":                  "spaced",
		"line\x00with\x07ctrl":        "linewithctrl",
		"<b>bold</b>":                 "bold",
		"plain":                       "plain",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalConversationIDOrderInvariant(t *testing.T) {
	a := "0x" + strings.Repeat("1", 40)
	b := "0x" + strings.Repeat("2", 40)
	if CanonicalConversationID(a, b) != CanonicalConversationID(b, a) {
		t.Error("canonical conversation id must be order-independent")
	}
	want := "dm:" + a + ":" + b
	if got := CanonicalConversationID(b, a); got != want {
		t.Errorf("CanonicalConversationID = %q, want %q", got, want)
	}
}

func TestSortedPair(t *testing.T) {
	a := "0x" + strings.Repeat("a", 40)
	b := "0x" + strings.Repeat("b", 40)
	x, y := SortedPair(b, a)
	if x != a || y != b {
		t.Errorf("SortedPair = (%q,%q), want (%q,%q)", x, y, a, b)
	}
}

func TestGenerateIDShapeAndOrdering(t *testing.T) {
	t0 := time.UnixMilli(1_000_000_000_000)
	t1 := time.UnixMilli(1_000_000_001_000)
	id0 := GenerateID(t0)
	id1 := GenerateID(t1)

	if !strings.Contains(id0, "-") {
		t.Errorf("id %q missing separator", id0)
	}
	parts := strings.SplitN(id0, "-", 2)
	if len(parts[1]) != 8 {
		t.Errorf("suffix len = %d, want 8", len(parts[1]))
	}
	// Later timestamp must sort lexicographically after the earlier one (keyset
	// pagination depends on this).
	if !(id0 < id1) {
		t.Errorf("expected id0 (%q) < id1 (%q)", id0, id1)
	}
	// Uniqueness for the same instant (random suffix).
	if GenerateID(t0) == GenerateID(t0) {
		t.Error("two ids at the same instant collided")
	}
}
