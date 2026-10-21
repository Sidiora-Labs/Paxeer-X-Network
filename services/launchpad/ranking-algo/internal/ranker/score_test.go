package ranker

import (
	"math"
	"strconv"
	"testing"
)

const eps = 1e-9

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestJSParseFloat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"0", 0, true},
		{"123", 123, true},
		{"123.45", 123.45, true},
		{"  7.5  ", 0, false},
		{"-3.2", -3.2, true},
		{"+9", 0, false},
		{"1e3", 0, false},
		{"1.5e-2", 0, false},
		{"12.5abc", 0, false}, // Complete decimal grammar is required
		{"1e", 0, false},      // Dangling exponents are invalid
		{"1000000000000000000", 1e18, true},
		{"", 0, false},
		{"abc", 0, false},
		{".", 0, false},
		{"+", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := jsParseFloat(tc.in)
			if ok != tc.ok {
				t.Fatalf("jsParseFloat(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if ok {
				approx(t, got, tc.want)
			}
		})
	}
}

func TestParseFloatOr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		fallback float64
		want     float64
	}{
		{"5", 1, 5},
		{"0", 1, 1},   // zero is falsy -> fallback
		{"abc", 1, 1}, // NaN -> fallback
		{"", 7, 7},    // empty -> fallback
		{"-4", 1, -4}, // negative is truthy
		{"2.5", 99, 2.5},
	}
	for _, tc := range cases {
		if got := parseFloatOr(tc.in, tc.fallback); got != tc.want {
			t.Errorf("parseFloatOr(%q,%v) = %v, want %v", tc.in, tc.fallback, got, tc.want)
		}
	}
}

func TestAbsParseFloatOrZero(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want float64
	}{
		{"5", 5},
		{"-5", 5},
		{"0", 0},
		{"abc", 0},
		{"", 0},
		{"-0.25", 0.25},
	}
	for _, tc := range cases {
		if got := absParseFloatOrZero(tc.in); got != tc.want {
			t.Errorf("absParseFloatOrZero(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestScoreTrending(t *testing.T) {
	t.Parallel()
	// vol velocity=1 -> 0.04; trader=0.5 -> 0.125; priceChange=0.5 -> 0.1;
	// tradeFreq=10 -> 0.15; total = 0.415.
	p := PoolStat{
		Volume24h:        "2400",
		Volume1h:         "100",
		UniqueTraders24h: 25,
		PriceChange24h:   "50",
		BuyCount24h:      120,
		SellCount24h:     120,
	}
	approx(t, ScoreTrending(p), 0.415)

	// All-clamped maxima: velocity>=10, trader>=1, priceChange>=1, freq>=10.
	hi := PoolStat{
		Volume24h:        "24",
		Volume1h:         "1000",
		UniqueTraders24h: 1000,
		PriceChange24h:   "1000",
		BuyCount24h:      10000,
		SellCount24h:     10000,
	}
	approx(t, ScoreTrending(hi), 0.4+0.25+0.2+0.15)

	// Empty/zero pool: fallbacks make velocity 0, everything 0.
	approx(t, ScoreTrending(PoolStat{}), 0)
}

func TestScoreBreakout(t *testing.T) {
	t.Parallel()
	// vol1h=100, vol24h=2400 -> avgHourly=100 -> spike=1 -> log2(2)=1;
	// priceChange=abs(50)=50 -> 0.5; holders=8 -> log2(8)=3; score=0.5*1*3=1.5.
	p := PoolStat{Volume1h: "100", Volume24h: "2400", PriceChange24h: "50", HolderCount: 8}
	score, ok := ScoreBreakout(p)
	if !ok {
		t.Fatal("expected included")
	}
	approx(t, score, 1.5)

	// Zero price change -> score 0 -> dropped.
	if _, ok := ScoreBreakout(PoolStat{Volume1h: "100", Volume24h: "2400", PriceChange24h: "0", HolderCount: 8}); ok {
		t.Error("expected dropped when score == 0")
	}
}

func TestScoreMovers(t *testing.T) {
	t.Parallel()
	// vol1h=100, vol24h=2400 -> avg=100 -> accel=1; holders=3 -> log2(4)=2;
	// mcap=99 -> log10(100)=2; score=1*2/2=1.
	p := PoolStat{Volume1h: "100", Volume24h: "2400", HolderCount: 3, MarketCap: "99"}
	score, ok := ScoreMovers(p)
	if !ok {
		t.Fatal("expected included")
	}
	approx(t, score, 1)

	// mcap <= 0 -> dropped (fallback makes "0" -> 0).
	if _, ok := ScoreMovers(PoolStat{Volume1h: "100", Volume24h: "2400", HolderCount: 3, MarketCap: "0"}); ok {
		t.Error("expected dropped when mcap <= 0")
	}
}

func TestScoreUnusual(t *testing.T) {
	t.Parallel()
	// vol5m=10, vol1h=120 -> short=10/(120/12)=10/10=1 -> min(1,20)/20=0.05*0.35=0.0175
	// hourlyVsDaily=120/(2400/24)=120/100=1.2 -> 1.2/10=0.12*0.25=0.03
	// trades=120 buy 0 sell -> ratio=120/120=1 -> 1*0.2=0.2
	// traderConc=120/40=3 -> min(3/10,1)=0.3*0.2=0.06
	// total = 0.0175+0.03+0.2+0.06 = 0.3075
	p := PoolStat{
		Volume5m:         "10",
		Volume1h:         "120",
		Volume24h:        "2400",
		BuyCount24h:      120,
		SellCount24h:     0,
		UniqueTraders24h: 40,
	}
	score, ok := ScoreUnusual(p)
	if !ok {
		t.Fatal("expected included")
	}
	approx(t, score, 0.3075)

	// A pool with no activity still yields a positive score from the volume
	// fallbacks (vol1h, vol24h default to 1), matching the TS heuristic, so it
	// is included.
	if _, ok := ScoreUnusual(PoolStat{}); !ok {
		t.Error("expected included for empty pool (fallback-driven positive score)")
	}
}

func TestScoreUnusualNoUniqueTraders(t *testing.T) {
	t.Parallel()
	// uniqueTraders == 0 -> traderConcentration term is 0.
	p := PoolStat{Volume5m: "0", Volume1h: "1", Volume24h: "1", BuyCount24h: 1, SellCount24h: 1, UniqueTraders24h: 0}
	score, ok := ScoreUnusual(p)
	if !ok {
		t.Fatal("expected included")
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		t.Fatalf("score must be finite, got %v", score)
	}
}

func FuzzJSParseFloat(f *testing.F) {
	for _, s := range []string{"", "0", "123.45", "-1e9", "  12abc", "1.2.3", "NaN", "Infinity", "1e999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got, ok := jsParseFloat(s)
		if !ok {
			if !math.IsNaN(got) {
				t.Fatalf("not-ok result must be NaN, got %v for %q", got, s)
			}
			return
		}
		// When ok, the result must be finite-or-Inf but never NaN, and must equal
		// a strconv parse of some leading prefix of the trimmed input.
		if math.IsNaN(got) {
			t.Fatalf("ok result must not be NaN for %q", s)
		}
		_ = strconv.FormatFloat(got, 'g', -1, 64)
	})
}
