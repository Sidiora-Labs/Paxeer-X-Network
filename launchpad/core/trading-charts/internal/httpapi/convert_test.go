package httpapi

import (
	"encoding/json"
	"testing"
)

func TestAsString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   interface{}
		want string
	}{
		{nil, ""},
		{"abc", "abc"},
		{float64(7), "7"},
		{json.Number("123456789012345678"), "123456789012345678"},
		{true, "true"},
	}
	for _, tc := range cases {
		if got := asString(tc.in); got != tc.want {
			t.Errorf("asString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAsBool(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   interface{}
		want bool
	}{
		{nil, false},
		{true, true},
		{false, false},
		{"true", true},
		{"nope", false},
		{float64(1), false},
	}
	for _, tc := range cases {
		if got := asBool(tc.in); got != tc.want {
			t.Errorf("asBool(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
