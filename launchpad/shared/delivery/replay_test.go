package delivery

import (
	"testing"
)

func TestMeasuredRunwayFailsUnavailableInputs(t *testing.T) {
	if v, err := DiskRunwaySeconds(100, 1000, 100, 50, 20); err != nil || v != 30 {
		t.Fatal(v, err)
	}
	for _, rates := range [][2]int64{{0, 0}, {20, 20}, {10, 20}, {20, -1}} {
		if _, err := DiskRunwaySeconds(100, 1000, 100, rates[0], rates[1]); err == nil {
			t.Fatal("unavailable growth accepted", rates)
		}
	}
}
