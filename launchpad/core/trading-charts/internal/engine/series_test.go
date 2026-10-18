package engine

import (
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func seriesSource(index int, timestamp int64, actor string) accounting.FillV1 {
	f := accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: fmt.Sprint(100 + index), TransactionIndex: "0", LogIndex: "1", TransactionHash: fmt.Sprintf("0x%064x", index+1), BlockHash: fmt.Sprintf("0x%064x", index+1000)}, ActualEmitter: "0x" + strings.Repeat("1", 40), PoolAddress: "0x" + strings.Repeat("2", 40), Token: accounting.Asset{Address: "0x" + strings.Repeat("3", 40), Decimals: 6}, Settlement: accounting.Asset{Address: "0x" + strings.Repeat("4", 40), Decimals: 6}, Side: "buy", TokenAmountRaw: "1000000", USDLAmountRaw: fmt.Sprint(1000000 + index), PaidInputRaw: fmt.Sprint(1000000 + index), FeeRaw: "100", FeeDisposition: "distributed_usdl", InputDerivationQuality: "receipt_incomplete", BlockTimestampSeconds: fmt.Sprint(timestamp), Attribution: accounting.Attribution{TransactionSender: "0x" + strings.Repeat("5", 40), Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Execution: accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Provenance: accounting.Provenance{DeploymentID: "arithmetic-vector", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch-1", PoolCodeEpoch: "epoch-1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}}
	if actor != "" {
		f.Attribution.Trader = &actor
		f.Attribution.Quality = "exact_receipt"
		f.Attribution.Evidence = []accounting.EvidenceRef{{Kind: "receipt", ArtifactSHA256: strings.Repeat("c", 64), ArtifactID: "arithmetic-vector", LogIndex: nil}}
	}
	f.SourceFillID = accounting.SourceFillIdentity(f.Position, f.Provenance.DeploymentID)
	return f
}
func TestSeriesSparseCarryAndLateReplacement(t *testing.T) {
	actual := []SeriesCandle{}
	for i, stamp := range []int64{0, 240} {
		source := seriesSource(i, stamp, "")
		bar, err := FoldQualified(nil, source, "1m", false)
		if err != nil {
			t.Fatal(err)
		}
		state, err := DescribeSeries(bar, SeriesCheckpoint{Height: 200, Timestamp: 600}, time.Unix(601, 0))
		if err != nil {
			t.Fatal(err)
		}
		state.DisplayRevision = uint64(i + 1)
		actual = append(actual, state)
	}
	dense, err := DenseView(actual, 0, 300, "1m", nil, 2)
	if err != nil || len(dense) != 5 {
		t.Fatal(len(dense), err)
	}
	synthetic := 0
	for _, bar := range dense {
		if bar.Synthetic {
			synthetic++
			if bar.Trades != 0 || bar.VolumeUsdlRaw != "0" || bar.VolumeTokenRaw != "0" || bar.BuyVolumeUsdlRaw != "0" || bar.SellVolumeUsdlRaw != "0" || bar.Open != actual[0].Close || bar.High != bar.Close || bar.Low != bar.Close {
				t.Fatal(bar)
			}
		}
	}
	if synthetic != 3 || len(actual) != 2 {
		t.Fatal(synthetic)
	}
	source := seriesSource(2, 120, "")
	bar, err := FoldQualified(nil, source, "1m", false)
	if err != nil {
		t.Fatal(err)
	}
	late, err := DescribeSeries(bar, SeriesCheckpoint{Height: 200, Timestamp: 600}, time.Unix(601, 0))
	if err != nil {
		t.Fatal(err)
	}
	late.DisplayRevision = 3
	actual = append(actual, late)
	replaced, err := DenseView(actual, 0, 300, "1m", nil, 3)
	if err != nil || replaced[2].Synthetic || replaced[2].DisplayRevision <= dense[2].DisplayRevision {
		t.Fatal(replaced, err)
	}
	if _, err = DenseView(nil, 0, 120, "1m", nil, 1); err != ErrNoData {
		t.Fatal(err)
	}
	incomplete, err := DescribeSeries(bar, SeriesCheckpoint{Height: 200, Timestamp: 600, HasGaps: true}, time.Unix(601, 0))
	if err != nil || incomplete.CompletenessStatus != "backfill_incomplete" {
		t.Fatal(incomplete, err)
	}
	elapsed, err := DescribeSeries(bar, SeriesCheckpoint{Height: 0, Timestamp: 0}, time.Unix(601, 0))
	if err != nil || elapsed.CompletenessStatus != "interval_elapsed" {
		t.Fatal(elapsed, err)
	}
}
func TestSeriesRollupsMatchShuffledDirectFold(t *testing.T) {
	actorA := "0x" + strings.Repeat("a", 40)
	actorB := "0x" + strings.Repeat("b", 40)
	sources := []accounting.FillV1{}
	for i := 0; i < 100; i++ {
		actor := actorA
		if i%2 == 0 {
			actor = actorB
		}
		if i%7 == 0 {
			actor = ""
		}
		sources = append(sources, seriesSource(i, 1700000000+int64(i*61), actor))
	}
	rand.New(rand.NewSource(125)).Shuffle(len(sources), func(i, j int) { sources[i], sources[j] = sources[j], sources[i] })
	for _, interval := range ExactIntervals[1:] {
		direct := map[int64]QualifiedCandle{}
		children := map[int64]QualifiedCandle{}
		actors := map[int64]map[string]bool{}
		minuteActors := map[int64]map[string]bool{}
		for _, source := range sources {
			fill, err := QualifiedFill(source)
			if err != nil {
				t.Fatal(err)
			}
			start, _ := BucketStart(fill.BlockTimestamp, interval)
			minute, _ := BucketStart(fill.BlockTimestamp, "1m")
			if actors[start] == nil {
				actors[start] = map[string]bool{}
			}
			if minuteActors[minute] == nil {
				minuteActors[minute] = map[string]bool{}
			}
			var old *QualifiedCandle
			if prior, ok := direct[start]; ok {
				copy := prior
				old = &copy
			}
			fresh := fill.Sender != "" && !actors[start][fill.Sender]
			if fill.Sender != "" {
				actors[start][fill.Sender] = true
			}
			next, err := FoldQualified(old, source, interval, fresh)
			if err != nil {
				t.Fatal(err)
			}
			direct[start] = next
			old = nil
			if prior, ok := children[minute]; ok {
				copy := prior
				old = &copy
			}
			fresh = fill.Sender != "" && !minuteActors[minute][fill.Sender]
			if fill.Sender != "" {
				minuteActors[minute][fill.Sender] = true
			}
			next, err = FoldQualified(old, source, "1m", fresh)
			if err != nil {
				t.Fatal(err)
			}
			children[minute] = next
		}
		for start, parent := range direct {
			duration, _ := IntervalSeconds(interval)
			rows := []QualifiedCandle{}
			union := []string{}
			for minute, child := range children {
				if minute >= start && minute < start+duration {
					rows = append(rows, child)
					for actor := range minuteActors[minute] {
						union = append(union, actor)
					}
				}
			}
			rolled, _, err := RollupQualified(rows, union, interval, start)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := EconomicDigest(parent)
			b, _ := EconomicDigest(rolled)
			if a != b {
				t.Fatal(interval, parent, rolled)
			}
		}
	}
}
