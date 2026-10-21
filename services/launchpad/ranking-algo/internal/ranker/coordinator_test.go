package ranker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHeavyRunUsesOneFrozenUniverse(t *testing.T) {
	in := fixture(100)
	snapshots, err := ComputeRun(in, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 5 {
		t.Fatal("incomplete heavy run")
	}
	for _, snapshot := range snapshots {
		if snapshot.RunID() != in.RunID || snapshot.InputDigest() != snapshots[0].InputDigest() {
			t.Fatal("mixed run provenance")
		}
		if snapshot.Category() == "movers" {
			for _, entry := range snapshot.Entries() {
				for _, top := range snapshots[0].Entries()[:50] {
					if entry.Address == top.Address {
						t.Fatal("top trending member leaked")
					}
				}
			}
		}
	}
	independent, err := ComputeRun(in, true)
	if err != nil || len(independent) != 1 || independent[0].Category() != "new" {
		t.Fatal("new category dependency")
	}
}
func TestPublicCursorRemainsBoundToRetainedRun(t *testing.T) {
	snapshot, err := BuildSnapshot(fixture(110), "trending", nil)
	if err != nil {
		t.Fatal(err)
	}
	header := Header{RunID: snapshot.RunID(), Category: snapshot.Category(), InputDigest: snapshot.InputDigest(), OutputDigest: snapshot.OutputDigest(), AlgorithmVersion: AlgorithmVersion, CutoffTime: 1000, CompletedAt: time.Unix(1001, 0), Revision: 1, Total: 110}
	codec, err := NewCursorCodec([]byte(strings.Repeat("r", 32)))
	if err != nil {
		t.Fatal(err)
	}
	entry := snapshot.Entries()[49]
	last := RankedItem{Address: entry.Address, Score: "1", Rank: 50}
	token, err := codec.EncodePage(header, last)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := codec.ResolvePage("trending", token, time.Unix(1002, 0))
	if err != nil || ref.RunID != header.RunID || ref.Offset != 50 || ref.InputDigest != header.InputDigest {
		t.Fatal("cursor lost immutable identity")
	}
	if _, err = codec.ResolvePage("volume", token, time.Unix(1002, 0)); err != ErrCursorInvalid {
		t.Fatal("cross-category cursor accepted")
	}
	if _, err = codec.ResolvePage("trending", token, time.Unix(1900, 0)); err != ErrCursorExpired {
		t.Fatal("expired cursor accepted")
	}
	if _, err = codec.ResolvePage("trending", token[:len(token)-1]+"!", time.Unix(1002, 0)); err != ErrCursorInvalid {
		t.Fatal("unauthenticated cursor accepted")
	}
}
func TestComparisonRequiresReviewedReleaseIdentity(t *testing.T) {
	s, err := BuildSnapshot(fixture(100), "trending", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Compare(s, s, 10, "", "", ""); err == nil {
		t.Fatal("missing release identity accepted")
	}
	d := digest(s)
	r, err := Compare(s, s, 0, d, d, "")
	if err != nil || r.PromotionAllowed {
		t.Fatal("missing reviewer signoff accepted")
	}
	r, err = Compare(s, s, 0, d, d, "review-1")
	if err != nil || r.PromotionAllowed || !r.WithinReviewedThreshold || r.Top100Overlap != 100 {
		t.Fatal("identical reviewed comparison rejected")
	}
}

func TestCompletedEmptyRunAndInputImmutability(t *testing.T) {
	empty, err := ComputeRun(fixture(0), false)
	if err != nil || len(empty) != 5 {
		t.Fatal("empty complete run", err)
	}
	for _, snapshot := range empty {
		if snapshot.Entries() == nil || len(snapshot.Entries()) != 0 || snapshot.OutputDigest() == "" {
			t.Fatal("empty snapshot confused with absence")
		}
	}
	in := fixture(100)
	snapshots, err := ComputeRun(in, false)
	if err != nil {
		t.Fatal(err)
	}
	original := snapshots[0].InputDigest()
	in.Candidates[0].Pool.Volume1h = "99999"
	exposed := snapshots[0].Input()
	exposed.Candidates[0].Pool.Volume1h = "88888"
	if snapshots[0].InputDigest() != original || snapshots[0].Input().Candidates[0].Pool.Volume1h != "1" {
		t.Fatal("shared frozen universe changed through a caller")
	}
}

func TestComparisonArtifactDigestAndReplay(t *testing.T) {
	snapshot, err := BuildSnapshot(fixture(100), "trending", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	restored, err := ReadComparisonArtifact(raw, hex.EncodeToString(sum[:]))
	if err != nil || restored.InputDigest() != snapshot.InputDigest() || restored.OutputDigest() != snapshot.OutputDigest() {
		t.Fatal("retained artifact identity", err)
	}
	changed := append([]byte(nil), raw...)
	changed[len(changed)-1] = ' '
	if _, err = ReadComparisonArtifact(changed, hex.EncodeToString(sum[:])); err == nil {
		t.Fatal("modified artifact accepted")
	}
	replayed, err := BuildSnapshot(restored.Input(), "trending", nil)
	if err != nil || replayed.OutputDigest() != restored.OutputDigest() {
		t.Fatal("retained input replay", err)
	}
}

func TestPersistedPageRejectsCachePoisoning(t *testing.T) {
	snapshot, err := BuildSnapshot(fixture(2), "volume", nil)
	if err != nil {
		t.Fatal(err)
	}
	h := Header{RunID: snapshot.RunID(), Category: "volume", InputDigest: snapshot.InputDigest(), OutputDigest: snapshot.OutputDigest(), AlgorithmVersion: AlgorithmVersion, CutoffTime: 1000, CompletedAt: time.Unix(1001, 0), Revision: 1, Total: 2}
	items := []RankedItem{{snapshot.Entries()[0].Address, "2400", 1}, {snapshot.Entries()[1].Address, "2400", 2}}
	for _, scenario := range []string{"duplicate", "nonfinite", "overflow", "rank", "reverse-tie", "beyond-total"} {
		p := StoredPage{Header: h, Items: append([]RankedItem(nil), items...)}
		switch scenario {
		case "duplicate":
			p.Items[1].Address = p.Items[0].Address
		case "nonfinite":
			p.Items[0].Score = "NaN"
		case "overflow":
			p.Items[0].Score = "1000000000001"
		case "rank":
			p.Items[1].Rank = 3
		case "reverse-tie":
			p.Items[0].Address, p.Items[1].Address = p.Items[1].Address, p.Items[0].Address
		case "beyond-total":
			p.Header.Total = 1
		}
		if p.Validate() == nil {
			t.Fatalf("%s accepted", scenario)
		}
	}
}
