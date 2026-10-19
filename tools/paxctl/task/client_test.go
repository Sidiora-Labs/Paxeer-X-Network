package task

import "testing"

func TestNodeFromPod(t *testing.T) {
	tests := []struct {
		pod  string
		want string
	}{
		{"sei-rpc-0", "sei-rpc"},
		{"sei-rpc-11", "sei-rpc"},
		{"a-0", "a"},
		{"no-ordinal", "no-ordinal"},
		{"plain", "plain"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.pod, func(t *testing.T) {
			if got := nodeFromPod(tc.pod); got != tc.want {
				t.Errorf("nodeFromPod(%q) = %q, want %q", tc.pod, got, tc.want)
			}
		})
	}
}
