package risk

import (
	"reflect"
	"testing"
)

// base is an arbitrary fixed creation time; each case derives now = base+ageSec
// so the age tier is fully controlled and deterministic.
const base int64 = 1_000_000

func TestCalculate(t *testing.T) {
	tests := []struct {
		name        string
		ageSec      int64 // now - createdAt, in seconds
		top10       string
		creator     string
		traders     int
		holders     int
		wantScore   int
		wantFactors []string
	}{
		{
			name: "zero risk - old pool, no concentration, active",
			// exactly 168h -> not < 168 -> no age points
			ageSec: 604800, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		// ── Age tiers ──────────────────────────────────────────────
		{
			name:   "age < 1h -> very_new",
			ageSec: 1800, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 25, wantFactors: []string{"very_new"},
		},
		{
			name:   "age == 1h boundary -> new (not very_new)",
			ageSec: 3600, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 15, wantFactors: []string{"new"},
		},
		{
			name:   "age < 24h -> new",
			ageSec: 3600 * 5, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 15, wantFactors: []string{"new"},
		},
		{
			name:   "age == 24h boundary -> +5 no factor",
			ageSec: 86400, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 5, wantFactors: []string{},
		},
		{
			name:   "age < 168h -> +5 no factor",
			ageSec: 3600 * 100, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 5, wantFactors: []string{},
		},
		{
			name:   "age == 168h boundary -> no age points",
			ageSec: 604800, top10: "0", creator: "0", traders: 10, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		// ── Top-10 concentration tiers (old pool to isolate) ───────
		{
			name:   "top10 > 8000 -> extreme_concentration",
			ageSec: 604800, top10: "8001", creator: "0", traders: 10, holders: 100,
			wantScore: 25, wantFactors: []string{"extreme_concentration"},
		},
		{
			name:   "top10 == 8000 boundary -> high_concentration",
			ageSec: 604800, top10: "8000", creator: "0", traders: 10, holders: 100,
			wantScore: 15, wantFactors: []string{"high_concentration"},
		},
		{
			name:   "top10 > 5000 -> high_concentration",
			ageSec: 604800, top10: "5001", creator: "0", traders: 10, holders: 100,
			wantScore: 15, wantFactors: []string{"high_concentration"},
		},
		{
			name:   "top10 == 5000 boundary -> +5 no factor",
			ageSec: 604800, top10: "5000", creator: "0", traders: 10, holders: 100,
			wantScore: 5, wantFactors: []string{},
		},
		{
			name:   "top10 == 3000 boundary -> no points",
			ageSec: 604800, top10: "3000", creator: "0", traders: 10, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		{
			name:   "top10 > 3000 -> +5 no factor",
			ageSec: 604800, top10: "3001", creator: "0", traders: 10, holders: 100,
			wantScore: 5, wantFactors: []string{},
		},
		// ── Creator holdings tiers ─────────────────────────────────
		{
			name:   "creator > 5000 -> creator_heavy",
			ageSec: 604800, top10: "0", creator: "5001", traders: 10, holders: 100,
			wantScore: 20, wantFactors: []string{"creator_heavy"},
		},
		{
			name:   "creator == 5000 boundary -> creator_significant",
			ageSec: 604800, top10: "0", creator: "5000", traders: 10, holders: 100,
			wantScore: 10, wantFactors: []string{"creator_significant"},
		},
		{
			name:   "creator > 2000 -> creator_significant",
			ageSec: 604800, top10: "0", creator: "2001", traders: 10, holders: 100,
			wantScore: 10, wantFactors: []string{"creator_significant"},
		},
		{
			name:   "creator == 2000 boundary -> no points",
			ageSec: 604800, top10: "0", creator: "2000", traders: 10, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		// ── Trading activity ───────────────────────────────────────
		{
			name:   "traders < 3 -> low_activity",
			ageSec: 604800, top10: "0", creator: "0", traders: 2, holders: 100,
			wantScore: 10, wantFactors: []string{"low_activity"},
		},
		{
			name:   "traders == 3 boundary -> no points",
			ageSec: 604800, top10: "0", creator: "0", traders: 3, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		// ── Holder count ───────────────────────────────────────────
		{
			name:   "holders < 5 -> few_holders",
			ageSec: 604800, top10: "0", creator: "0", traders: 10, holders: 4,
			wantScore: 10, wantFactors: []string{"few_holders"},
		},
		{
			name:   "holders == 5 boundary -> no points",
			ageSec: 604800, top10: "0", creator: "0", traders: 10, holders: 5,
			wantScore: 0, wantFactors: []string{},
		},
		// ── Degenerate concentration inputs ────────────────────────
		{
			name:   "empty concentration strings -> Number('')==0 -> no points",
			ageSec: 604800, top10: "", creator: "", traders: 10, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		{
			name:   "non-numeric concentration -> NaN -> all comparisons false",
			ageSec: 604800, top10: "abc", creator: "xyz", traders: 10, holders: 100,
			wantScore: 0, wantFactors: []string{},
		},
		// ── Combined / ordering ────────────────────────────────────
		{
			name:   "mixed mid-tier factors in declaration order",
			ageSec: 3600 * 5, top10: "6000", creator: "3000", traders: 1, holders: 1,
			wantScore:   60,
			wantFactors: []string{"new", "high_concentration", "creator_significant", "low_activity", "few_holders"},
		},
		{
			name:   "maximum risk (90) - all top-tier factors in order",
			ageSec: 0, top10: "9999", creator: "9999", traders: 0, holders: 0,
			wantScore:   90,
			wantFactors: []string{"very_new", "extreme_concentration", "creator_heavy", "low_activity", "few_holders"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{
				CreatedAt:          base,
				Top10Concentration: tc.top10,
				CreatorHoldingsPct: tc.creator,
				UniqueTraders24h:   tc.traders,
				HolderCount:        tc.holders,
			}
			got := Calculate(in, base+tc.ageSec)

			if got.Score != tc.wantScore {
				t.Errorf("score = %d, want %d", got.Score, tc.wantScore)
			}
			if !reflect.DeepEqual(got.Factors, tc.wantFactors) {
				t.Errorf("factors = %#v, want %#v", got.Factors, tc.wantFactors)
			}
		})
	}
}

// TestCalculateFactorsNeverNil guards the JSON-parity invariant: Factors must be
// a non-nil empty slice (marshals to "[]", not "null") when no factor fires.
func TestCalculateFactorsNeverNil(t *testing.T) {
	got := Calculate(Input{
		CreatedAt:          base,
		Top10Concentration: "0",
		CreatorHoldingsPct: "0",
		UniqueTraders24h:   100,
		HolderCount:        100,
	}, base+604800)
	if got.Factors == nil {
		t.Fatal("Factors is nil; must be an initialized empty slice for JSON '[]' parity")
	}
	if len(got.Factors) != 0 {
		t.Fatalf("Factors = %#v, want empty", got.Factors)
	}
}

// TestCalculateScoreNeverExceeds100 documents that the additive maximum is 90
// (categories are mutually exclusive), so the defensive >100 clamp is never the
// binding constraint, yet the score still stays within [0,100].
func TestCalculateScoreNeverExceeds100(t *testing.T) {
	got := Calculate(Input{
		CreatedAt:          base,
		Top10Concentration: "999999",
		CreatorHoldingsPct: "999999",
		UniqueTraders24h:   0,
		HolderCount:        0,
	}, base) // ageHours = 0
	if got.Score < 0 || got.Score > 100 {
		t.Fatalf("score %d out of [0,100]", got.Score)
	}
	if got.Score != 90 {
		t.Fatalf("score = %d, want 90 (additive maximum)", got.Score)
	}
}
