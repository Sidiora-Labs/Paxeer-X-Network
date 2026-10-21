package engine

import (
	"math/big"
	"testing"
	"time"
)

func TestMeasurementIndependentReferenceMatchesActualFold(t *testing.T) {
	reference, err := NewMeasurementReference(100)
	if err != nil {
		t.Fatal(err)
	}
	bars := map[string]QualifiedCandle{}
	actors := map[string]map[string]bool{}
	for _, index := range []int{3, 1, 2, 0} {
		source := seriesSource(index, 1700000040, "")
		source.TokenAmountRaw = "9007199254740993"
		source.USDLAmountRaw = new(big.Int).Add(big.NewInt(9007199254740993), big.NewInt(int64(index))).String()
		source.PaidInputRaw = source.USDLAmountRaw
		if err = reference.Add(source); err != nil {
			t.Fatal(err)
		}
		for _, interval := range ExactIntervals {
			var previous *QualifiedCandle
			if value, ok := bars[interval]; ok {
				copy := value
				previous = &copy
			}
			if actors[interval] == nil {
				actors[interval] = map[string]bool{}
			}
			next, err := FoldQualified(previous, source, interval, false)
			if err != nil {
				t.Fatal(err)
			}
			bars[interval] = next
		}
	}
	actual := []MeasurementBar{}
	for _, bar := range bars {
		state, err := DescribeSeries(bar, SeriesCheckpoint{Height: 1000, Timestamp: 1700010000}, time.Unix(1700010001, 0))
		if err != nil {
			t.Fatal(err)
		}
		mapped, err := MeasurementActual(state)
		if err != nil {
			t.Fatal(err)
		}
		actual = append(actual, mapped)
	}
	expected, err := MeasurementDigest(reference.Bars())
	if err != nil {
		t.Fatal(err)
	}
	measured, err := MeasurementDigest(actual)
	if err != nil || expected != measured {
		t.Fatal("independent exact price/order/count/volume reference differs", expected, measured, err)
	}
	actual[0].VolumeUsdlRaw = new(big.Int).Add(big.NewInt(1), new(big.Int).SetUint64(36028797018963978)).String()
	changed, err := MeasurementDigest(actual)
	if err != nil || changed == expected {
		t.Fatal("one-unit amount discrepancy collapsed", changed, err)
	}
}
func TestMeasurementRejectsPoisonAndFabricatedActivity(t *testing.T) {
	reference, err := NewMeasurementReference(7)
	if err != nil {
		t.Fatal(err)
	}
	source := seriesSource(0, 1700000040, "")
	bad := source
	bad.TokenAmountRaw = "1e6"
	if err = reference.Add(bad); err == nil {
		t.Fatal("signed/exponent malformed amount accepted")
	}
	if err = reference.Add(source); err != nil {
		t.Fatal(err)
	}
	other := seriesSource(1, 1700000100, "")
	if err = reference.Add(other); err == nil {
		t.Fatal("bounded reference admitted excess bars")
	}
	bar, err := FoldQualified(nil, source, "1m", false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DescribeSeries(bar, SeriesCheckpoint{}, time.Unix(1700010000, 0))
	if err != nil {
		t.Fatal(err)
	}
	state.Synthetic = true
	if _, err = MeasurementActual(state); err == nil {
		t.Fatal("synthetic carry counted as accepted trading activity")
	}
	if _, err = MeasurementDigest([]MeasurementBar{{Pool: source.PoolAddress, Interval: "1m", Start: 1700000040}, {Pool: source.PoolAddress, Interval: "1m", Start: 1700000040}}); err == nil {
		t.Fatal("duplicate persisted measurement bar accepted")
	}
}
func TestMeasurementIndependentCalendarBoundaries(t *testing.T) {
	for _, row := range []struct {
		timestamp int64
		interval  string
		expected  int64
	}{{59, "1m", 0}, {60, "1m", 60}, {3599, "1h", 0}, {3600, "1h", 3600}} {
		actual, err := measurementBucket(row.timestamp, row.interval)
		if err != nil || actual != row.expected {
			t.Fatal(row, actual, err)
		}
	}
	sunday := time.Date(2026, 10, 11, 23, 59, 59, 0, time.UTC)
	monday := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	actual, err := measurementBucket(sunday.Unix(), "1w")
	if err != nil || actual != time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC).Unix() {
		t.Fatal(actual, err)
	}
	actual, err = measurementBucket(monday.Unix(), "1w")
	if err != nil || actual != monday.Unix() {
		t.Fatal(actual, err)
	}
	if _, err = measurementBucket(monday.Unix(), "1M"); err == nil {
		t.Fatal("unsupported monthly interval guessed")
	}
}
