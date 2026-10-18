package ranker

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// Synthetic arithmetic/discovery inputs, never chain receipt evidence.
func fixture(n int) InputSnapshot {
	in := InputSnapshot{RunID: "be0ebdd7-e6e2-4f9a-b856-68e7fb31bf22", AlgorithmVersion: AlgorithmVersion, FeatureSchemaVersion: FeatureSchemaVersion, CutoffHeight: 125, CutoffTimeUTC: 1000, Complete: true}
	for i := 1; i <= n; i++ {
		a, _ := AdjustActivity("2400", "0", true, false, 0, 1000)
		in.Candidates = append(in.Candidates, Candidate{Pool: PoolStat{PoolAddress: fmt.Sprintf("0x%040x", i), Volume24h: "2400", Volume1h: fmt.Sprint(i), Volume5m: "1", MarketCap: "1000", PriceChange24h: "1", UniqueTraders24h: 1, HolderCount: 1}, SourceRevision: "1", SourceObservedAt: 1000, ModerationObservedAt: 1000, Depth: "1", SupplyVerified: true, Activity: a, CreationHeight: uint64(i)})
	}
	return in
}
func TestSnapshotReplayFrozenAndMovers(t *testing.T) {
	in := fixture(100)
	tr, e := BuildSnapshot(in, "trending", nil)
	if e != nil {
		t.Fatal(e)
	}
	for i, j := 0, len(in.Candidates)-1; i < j; i, j = i+1, j-1 {
		in.Candidates[i], in.Candidates[j] = in.Candidates[j], in.Candidates[i]
	}
	replay, e := BuildSnapshot(in, "trending", nil)
	if e != nil || tr.OutputDigest() != replay.OutputDigest() || tr.InputDigest() != replay.InputDigest() {
		t.Fatalf("replay mismatch: %v", e)
	}
	movers, e := BuildSnapshot(in, "movers", &tr)
	if e != nil {
		t.Fatal(e)
	}
	if len(movers.Entries()) != 50 {
		t.Fatalf("movers count %d", len(movers.Entries()))
	}
	for _, entry := range movers.Entries() {
		if entry.Address > fmt.Sprintf("0x%040x", 50) {
			t.Fatalf("top-50 leaked: %s", entry.Address)
		}
	}
	in.Candidates[0].Pool.Volume1h = "999999"
	entries := tr.Entries()
	entries[0].Score = -1
	if tr.Entries()[0].Score < 0 {
		t.Fatal("aliased output")
	}
	if _, e = BuildSnapshot(in, "movers", &tr); e == nil {
		t.Fatal("mixed inputs accepted")
	}
	in.Complete = false
	if _, e = BuildSnapshot(in, "trending", nil); e == nil {
		t.Fatal("incomplete accepted")
	}
}
func TestFiniteDecimalAndLargeRatio(t *testing.T) {
	for _, v := range []string{"12garbage", "1e999", "NaN", "Infinity", "-1", " 1", "1e3"} {
		if _, e := Decimal(v, false); e == nil {
			t.Fatalf("accepted %q", v)
		}
	}
	huge := strings.Repeat("9", 400)
	r, e := BoundedRatio(huge, huge, 10)
	if e != nil || r != 1 {
		t.Fatalf("large ratio %v %v", r, e)
	}
	if _, e = BoundedRatio("1", "0", 10); e == nil {
		t.Fatal("zero baseline accepted")
	}
	in := fixture(1)
	in.Candidates[0].Pool.HolderCount = -1
	s, e := BuildSnapshot(in, "trending", nil)
	if e != nil || len(s.Entries()) != 0 || s.Exclusions()[0].Reason != "input-invalid" {
		t.Fatalf("negative count: %v", e)
	}
	in = fixture(1)
	in.Candidates[0].Pool.Volume24h = huge
	in.Candidates[0].Pool.Volume1h = huge
	in.Candidates[0].Activity, _ = AdjustActivity(huge, "0", true, false, 0, 1000)
	s, e = BuildSnapshot(in, "trending", nil)
	if e != nil || len(s.Entries()) != 1 {
		t.Fatalf("large raw excluded: %v %v", s.Exclusions(), e)
	}
}
func TestPolicyTieCreationFreshnessAndAbuse(t *testing.T) {
	in := fixture(2)
	in.Candidates[1].Pool.Volume1h = "1"
	s, e := BuildSnapshot(in, "trending", nil)
	if e != nil || s.Entries()[0].Address != in.Candidates[0].Pool.PoolAddress {
		t.Fatal("address tie-break")
	}
	newer, _ := BuildSnapshot(in, "new", nil)
	if newer.Entries()[0].Address != in.Candidates[1].Pool.PoolAddress {
		t.Fatal("creation order")
	}
	if s.Stale(time.Unix(1090, 0)) || !s.Stale(time.Unix(1091, 0)) || !newer.Stale(time.Unix(1031, 0)) {
		t.Fatal("stale threshold")
	}
	in.Candidates[1].Activity, _ = AdjustActivity("2400", "2400", true, true, 0, 1000)
	wash, _ := BuildSnapshot(in, "trending", nil)
	if len(wash.Entries()) != 1 || wash.Exclusions()[0].Reason != "minimum-volume" {
		t.Fatal("self volume admitted")
	}
	a, e := AdjustActivity("10.10", "0.01", true, true, 0, 1)
	if e != nil || a.RawVolume != "10.10" || a.AdjustedVolume != "10.09" || a.Confidence != "complete" {
		t.Fatalf("raw/adjusted: %+v", a)
	}
	a, _ = AdjustActivity("10", "0", false, false, 0, 1)
	if a.Confidence != "degraded" || a.AdjustedVolume != "0" {
		t.Fatal("unavailable abuse")
	}
	in = fixture(1)
	in.Candidates[0].ModerationObservedAt = 939
	mod, _ := BuildSnapshot(in, "trending", nil)
	if mod.Exclusions()[0].Reason != "moderation-stale" {
		t.Fatal("stale moderation admitted")
	}
}
func TestSignedSnapshotPagination(t *testing.T) {
	s, _ := BuildSnapshot(fixture(110), "trending", nil)
	codec, e := NewCursorCodec([]byte(strings.Repeat("k", 32)))
	if e != nil {
		t.Fatal(e)
	}
	p, e := codec.Page(s, 100, "", time.Unix(1000, 0))
	if e != nil || len(p.Items) != 100 || len(p.NextCursor) > 256 {
		t.Fatalf("first: %v", e)
	}
	q, e := codec.Page(s, 100, p.NextCursor, time.Unix(1001, 0))
	if e != nil || len(q.Items) != 10 || q.Items[0].Address == p.Items[99].Address {
		t.Fatalf("second: %v", e)
	}
	if _, e = codec.Page(s, 100, p.NextCursor, time.Unix(1900, 0)); e != ErrCursorExpired {
		t.Fatal("expiry")
	}
	other, _ := BuildSnapshot(fixture(110), "volume", nil)
	if _, e = codec.Page(other, 100, p.NextCursor, time.Unix(1000, 0)); e != ErrCursorInvalid {
		t.Fatal("category binding")
	}
	altered := p.NextCursor[:20] + "!" + p.NextCursor[21:]
	if _, e = codec.Page(s, 100, altered, time.Unix(1000, 0)); e != ErrCursorInvalid {
		t.Fatal("tamper")
	}
	for _, limit := range []int{-1, 0, 101} {
		if _, e = codec.Page(s, limit, "", time.Unix(1000, 0)); e == nil {
			t.Fatal("limit")
		}
	}
}

func TestCategoryExclusionsAndFormulaComponents(t *testing.T) {
	in := fixture(1)
	p := in.Candidates[0].Pool
	p.Volume1h = "100"
	p.PriceChange24h = "50"
	p.UniqueTraders24h = 25
	p.BuyCount24h = 120
	p.SellCount24h = 120
	score, err := finiteScore("trending", p)
	if err != nil || score < 0.414999999 || score > 0.415000001 {
		t.Fatalf("formula components %v %v", score, err)
	}
	for _, category := range []string{"trending", "breakout", "volume", "movers", "unusual", "new"} {
		in = fixture(1)
		tr, _ := BuildSnapshot(in, "trending", nil)
		var dep *Snapshot
		if category == "movers" {
			dep = &tr
		}
		s, e := BuildSnapshot(in, category, dep)
		if e != nil {
			t.Fatalf("%s: %v", category, e)
		}
		if s.OutputDigest() == "" || s.InputDigest() == "" {
			t.Fatal("missing digest")
		}
	}
	for _, scenario := range []string{"paused", "suppressed", "stale", "missing-revision", "depth", "activity", "invalid-decimal"} {
		in = fixture(1)
		c := &in.Candidates[0]
		switch scenario {
		case "paused":
			c.Paused = true
		case "suppressed":
			c.Suppressed = true
		case "stale":
			c.SourceObservedAt = 909
		case "missing-revision":
			c.SourceRevision = ""
		case "depth":
			c.Depth = "0"
		case "activity":
			c.Activity.Confidence = "degraded"
		case "invalid-decimal":
			c.Pool.Volume1h = "12garbage"
		}
		s, e := BuildSnapshot(in, "trending", nil)
		if e != nil || len(s.Entries()) != 0 || len(s.Exclusions()) != 1 {
			t.Fatalf("%s not excluded: %v", scenario, e)
		}
	}
	in = fixture(51)
	in.Candidates[0].SupplyVerified = false
	tr, _ := BuildSnapshot(in, "trending", nil)
	s, e := BuildSnapshot(in, "movers", &tr)
	if e != nil || len(s.Entries()) != 0 || s.Exclusions()[0].Reason != "supply-unverified" {
		t.Fatalf("unknown supply: %v %v", s.Exclusions(), e)
	}
	in = fixture(2)
	in.Candidates[1].Pool.PoolAddress = in.Candidates[0].Pool.PoolAddress
	if _, e = BuildSnapshot(in, "volume", nil); e == nil {
		t.Fatal("duplicate universe address")
	}
}

func TestSnapshotTimestampExtremaAndMetadataBounds(t *testing.T) {
	for _, observed := range []int64{math.MinInt64, -1, 0, 1001, math.MaxInt64} {
		for _, moderation := range []bool{false, true} {
			in := fixture(1)
			if moderation {
				in.Candidates[0].ModerationObservedAt = observed
			} else {
				in.Candidates[0].SourceObservedAt = observed
			}
			s, e := BuildSnapshot(in, "trending", nil)
			if e != nil || len(s.Entries()) != 0 || len(s.Exclusions()) != 1 {
				t.Fatalf("timestamp %d moderation %v admitted: %v", observed, moderation, e)
			}
		}
	}
	for _, field := range []string{"address", "model", "confidence"} {
		in := fixture(1)
		huge := strings.Repeat("x", 1000000)
		switch field {
		case "address":
			in.Candidates[0].Pool.PoolAddress = huge
		case "model":
			in.Candidates[0].Activity.ModelVersion = huge
		case "confidence":
			in.Candidates[0].Activity.Confidence = huge
		}
		if _, e := BuildSnapshot(in, "trending", nil); e == nil || !strings.Contains(e.Error(), "metadata-bounds") {
			t.Fatalf("%s allocation bound: %v", field, e)
		}
	}
	in := fixture(1)
	in.CutoffHeight = 0
	if _, e := BuildSnapshot(in, "trending", nil); e == nil {
		t.Fatal("zero cutoff height accepted")
	}
	for _, height := range []uint64{0, 126, math.MaxUint64} {
		in = fixture(1)
		in.Candidates[0].CreationHeight = height
		s, e := BuildSnapshot(in, "new", nil)
		if e != nil || len(s.Entries()) != 0 || s.Exclusions()[0].Reason != "creation-provenance-invalid" {
			t.Fatalf("creation height %d: %v", height, e)
		}
	}
	codec, _ := NewCursorCodec([]byte(strings.Repeat("k", 32)))
	s, _ := BuildSnapshot(fixture(1), "trending", nil)
	if _, e := codec.Page(s, 1, "", time.Unix(999, 0)); e == nil {
		t.Fatal("backward clock accepted")
	}
	// Package-local mutation constructs the unreachable external overflow case;
	// the paging primitive must still reject it without adding 900 unsafely.
	s.input.CutoffTimeUTC = math.MaxInt64
	if _, e := codec.Page(s, 1, "", time.Unix(math.MaxInt64, 0)); e == nil {
		t.Fatal("overflow cutoff accepted")
	}
	if !s.Stale(time.Unix(math.MinInt64, 0)) {
		t.Fatal("clock reversal considered fresh")
	}
}

func TestAdversarialCandidateQuarantineAllCategories(t *testing.T) {
	for _, scenario := range []string{"12garbage", "1e999", "NaN", "Infinity", "negative-volume", "negative-count", "source-stale", "moderation-stale", "negative-activity-window"} {
		t.Run(scenario, func(t *testing.T) {
			in := fixture(2)
			c := &in.Candidates[0]
			switch scenario {
			case "negative-volume":
				c.Pool.Volume24h = "-1"
			case "negative-count":
				c.Pool.BuyCount24h = -1
			case "source-stale":
				c.SourceObservedAt = in.CutoffTimeUTC - 91
			case "moderation-stale":
				c.ModerationObservedAt = in.CutoffTimeUTC - 61
			case "negative-activity-window":
				c.Activity.WindowStart = -1
			default:
				c.Pool.Volume1h = scenario
			}
			run, err := ComputeRun(in, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, snapshot := range run {
				for _, item := range snapshot.Entries() {
					if item.Address == c.Pool.PoolAddress || math.IsNaN(item.Score) || math.IsInf(item.Score, 0) {
						t.Fatalf("%s admitted invalid candidate", snapshot.Category())
					}
				}
				found := false
				for _, excluded := range snapshot.Exclusions() {
					if excluded.Address == c.Pool.PoolAddress {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s lost quarantine attribution", snapshot.Category())
				}
			}
		})
	}
}

func TestWashLoopCannotOutrankOrganicFromSelfVolume(t *testing.T) {
	in := fixture(2)
	organic, wash := &in.Candidates[0], &in.Candidates[1]
	organic.Pool.Volume1h = "100"
	organic.Pool.UniqueTraders24h = 20
	organic.Pool.BuyCount24h, organic.Pool.SellCount24h = 10, 10
	wash.Pool.Volume24h, wash.Pool.Volume1h, wash.Pool.Volume5m = "240000", "10000", "100"
	wash.Pool.UniqueTraders24h = 20
	wash.Pool.BuyCount24h, wash.Pool.SellCount24h = 1000, 1000
	var err error
	wash.Activity, err = AdjustActivity("240000", "237600", true, false, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := BuildSnapshot(in, "trending", nil)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Input().Candidates[1].Pool.Volume24h != "240000" || wash.Activity.RawVolume != "240000" || wash.Activity.AdjustedVolume != "2400" {
		t.Fatal("raw activity facts changed")
	}
	var organicScore, washScore float64
	for _, entry := range snapshot.Entries() {
		if entry.Address == organic.Pool.PoolAddress {
			organicScore = entry.Score
		}
		if entry.Address == wash.Pool.PoolAddress {
			washScore = entry.Score
		}
	}
	if washScore > organicScore {
		t.Fatalf("evidenced repeated self activity outranked equal adjusted organic volume: wash=%v organic=%v; reviewed count adjustment policy required", washScore, organicScore)
	}
}

func TestActivityNegativeWindowRejected(t *testing.T) {
	if _, err := AdjustActivity("10", "0", true, false, -1, 1000); err == nil {
		t.Fatal("negative activity window admitted")
	}
}
