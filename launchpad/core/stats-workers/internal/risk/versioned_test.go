package risk

import (
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
)

var fixtureWatermark = &analytics.Watermark{ChainID: "125", Height: "1", BlockHash: "0x" + strings.Repeat("a", 64), ProjectionRevision: "1", ObservedAt: "2026-10-09T00:00:00.000Z", Integrity: "healthy"}

func TestV1Boundaries(t *testing.T) {
	for _, c := range []struct {
		age    int64
		score  int
		factor string
	}{{3599, 25, "very_new"}, {3600, 15, "new"}, {86399, 15, "new"}, {86400, 5, ""}, {604799, 5, ""}, {604800, 0, ""}} {
		r := CalculateV1(Input{1, "0", "0", 3, 5}, 1+c.age, fixtureWatermark)
		if r.Score == nil || *r.Score != c.score {
			t.Fatal(r)
		}
		if strings.Join(r.Factors, ",") != c.factor {
			t.Fatal(r)
		}
	}
	for _, c := range []struct {
		bp     string
		score  int
		factor string
	}{{"3000", 0, ""}, {"3001", 5, ""}, {"5000", 5, ""}, {"5001", 15, "high_concentration"}, {"8000", 15, "high_concentration"}, {"8001", 25, "extreme_concentration"}} {
		r := CalculateV1(Input{1, c.bp, "0", 3, 5}, 604801, fixtureWatermark)
		if strings.Join(r.Factors, ",") != c.factor {
			t.Fatal(r)
		}
		if r.Score == nil || *r.Score != c.score {
			t.Fatal(r)
		}
	}
}
func TestV1UnknownNotZero(t *testing.T) {
	for _, s := range []string{"", "abc", "-1", "10001", "1.0"} {
		r := CalculateV1(Input{2, s, "0", 3, 5}, 1, nil)
		if r.Score != nil || r.Coverage != "incomplete" || len(r.Unavailable) != 3 {
			t.Fatal(r)
		}
	}
}
func TestV1MissingSupply(t *testing.T) {
	in := Input{1, "9000", "9000", 3, 5}
	for _, s := range []string{"", "0", "abc", "-1", "01"} {
		r := CalculateV1WithSupply(in, s, 604801, fixtureWatermark)
		if r.Score != nil || r.PartialScore != 0 || len(r.Unavailable) != 3 {
			t.Fatal(r)
		}
	}
	r := CalculateV1WithSupply(in, "100000000000000000000000000000000000000", 604801, fixtureWatermark)
	if r.Score == nil || *r.Score != 45 {
		t.Fatal(r)
	}
}

func TestV1FrozenFactorOrderAndExplicitQuality(t *testing.T) {
	result := CalculateV1WithSupply(Input{CreatedAt: 1, Top10Concentration: "8001", CreatorHoldingsPct: "5001", UniqueTraders24h: 2, HolderCount: 4}, "100", 3600, fixtureWatermark)
	if result.Score == nil || *result.Score != 90 || result.PartialScore != 90 || strings.Join(result.Factors, ",") != "very_new,extreme_concentration,creator_heavy,low_activity,few_holders" || len(result.QualityFlags) != 0 {
		t.Fatal(result)
	}
	for _, c := range []struct {
		top, supply string
		created     int64
		want        string
	}{{"abc", "100", 1, "top10_concentration_unavailable"}, {"8001", "", 1, "supply_unavailable"}, {"0", "100", 3601, "age_unavailable"}} {
		result := CalculateV1WithSupply(Input{CreatedAt: c.created, Top10Concentration: c.top, CreatorHoldingsPct: "0", UniqueTraders24h: 3, HolderCount: 5}, c.supply, 3600, fixtureWatermark)
		if result.Score != nil || result.Coverage != "incomplete" || !strings.Contains(strings.Join(result.QualityFlags, ","), c.want) {
			t.Fatal(c, result)
		}
	}
	for _, supply := range []string{"0x10", "1e6", " 100", "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		result := CalculateV1WithSupply(Input{1, "8001", "5001", 3, 5}, supply, 604801, fixtureWatermark)
		if result.Score != nil || result.PartialScore != 0 || strings.Join(result.QualityFlags, ",") != "top10_concentration_unavailable,creator_holdings_unavailable,supply_unavailable" {
			t.Fatal(supply, result)
		}
	}
	if err := (ProjectionModelV1{}).Validate(); err != nil {
		t.Fatal(err)
	}
	metadata := ModelMetadata()
	metadata[0] = '!'
	if err := (ProjectionModelV1{}).Validate(); err != nil {
		t.Fatal("caller altered admitted model bytes", err)
	}
}
