package ranker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

type ComparisonReport struct {
	OldVersion              string         `json:"oldVersion"`
	NewVersion              string         `json:"newVersion"`
	InputDigest             string         `json:"inputDigest"`
	Top100Overlap           int            `json:"top100Overlap"`
	MembershipChanges       int            `json:"membershipChanges"`
	Movement                map[string]int `json:"movementHistogram"`
	ExclusionDeltas         map[string]int `json:"exclusionDeltas"`
	FormulaDigest           string         `json:"formulaDigest"`
	ImageDigest             string         `json:"imageDigest"`
	ReviewerSignoff         string         `json:"reviewerSignoff"`
	PromotionAllowed        bool           `json:"promotionAllowed"`
	WithinReviewedThreshold bool           `json:"withinReviewedThreshold"`
}

func Compare(old, next Snapshot, maxChanges int, formulaDigest, imageDigest, signoff string) (ComparisonReport, error) {
	r := ComparisonReport{OldVersion: old.input.AlgorithmVersion, NewVersion: next.input.AlgorithmVersion, InputDigest: old.inputDigest, Movement: map[string]int{}, ExclusionDeltas: map[string]int{}, FormulaDigest: formulaDigest, ImageDigest: imageDigest, ReviewerSignoff: signoff}
	a, b := old.Input(), next.Input()
	a.AlgorithmVersion = ""
	b.AlgorithmVersion = ""
	a.RunID = ""
	b.RunID = ""
	if old.category != next.category || digest(a) != digest(b) || maxChanges < 0 || maxChanges > 100 || !hashGrammar.MatchString(formulaDigest) || !hashGrammar.MatchString(imageDigest) {
		return r, fmt.Errorf("comparison requires identical frozen source and reviewed release identities")
	}
	r.InputDigest = digest(a)
	positions := map[string]int{}
	limit := len(old.entries)
	if limit > 100 {
		limit = 100
	}
	for i, e := range old.entries[:limit] {
		positions[e.Address] = i
	}
	n := len(next.entries)
	if n > 100 {
		n = 100
	}
	for i, e := range next.entries[:n] {
		if prior, ok := positions[e.Address]; ok {
			r.Top100Overlap++
			movement := prior - i
			if movement < 0 {
				movement = -movement
			}
			bucket := "0"
			if movement > 0 {
				bucket = "1-10"
			}
			if movement > 10 {
				bucket = "11-50"
			}
			if movement > 50 {
				bucket = "51-99"
			}
			r.Movement[bucket]++
		}
	}
	larger := limit
	if n > larger {
		larger = n
	}
	r.MembershipChanges = larger - r.Top100Overlap
	for _, e := range old.excluded {
		r.ExclusionDeltas[e.Reason]--
	}
	for _, e := range next.excluded {
		r.ExclusionDeltas[e.Reason]++
	}
	r.WithinReviewedThreshold = r.MembershipChanges <= maxChanges
	r.PromotionAllowed = false
	return r, nil
}
func CanonicalUniverse(candidates []Candidate) string {
	values := make([][2]string, len(candidates))
	for i, c := range candidates {
		values[i] = [2]string{strings.ToLower(c.Pool.PoolAddress), c.SourceRevision}
	}
	sort.Slice(values, func(i, j int) bool { return values[i][0] < values[j][0] })
	return digest(values)
}

type PromotionAuthority interface {
	VerifyRankingPromotion(context.Context, ComparisonReport) error
}

func (r ComparisonReport) Authorize(ctx context.Context, authority PromotionAuthority) (ComparisonReport, error) {
	r.PromotionAllowed = false
	if authority == nil || !r.WithinReviewedThreshold || len(r.ReviewerSignoff) == 0 || len(r.ReviewerSignoff) > 128 {
		return r, fmt.Errorf("reviewed promotion authority required")
	}
	if err := authority.VerifyRankingPromotion(ctx, r); err != nil {
		return r, err
	}
	r.PromotionAllowed = true
	return r, nil
}

func ReadComparisonArtifact(raw []byte, expectedSHA256 string) (Snapshot, error) {
	var artifact struct {
		Input        InputSnapshot `json:"input"`
		Category     string        `json:"category"`
		Entries      []Scored      `json:"entries"`
		Excluded     []Exclusion   `json:"excluded"`
		InputDigest  string        `json:"inputDigest"`
		OutputDigest string        `json:"outputDigest"`
	}
	if len(raw) == 0 || len(raw) > 1<<30 || !hashGrammar.MatchString(expectedSHA256) {
		return Snapshot{}, ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != expectedSHA256 {
		return Snapshot{}, ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return Snapshot{}, err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Snapshot{}, ErrUnavailable
	}
	in := artifact.Input
	policy, err := Policy(artifact.Category)
	if err != nil || !in.Complete || !uuidGrammar.MatchString(in.RunID) || len(in.AlgorithmVersion) == 0 || len(in.AlgorithmVersion) > 64 || in.FeatureSchemaVersion != FeatureSchemaVersion || in.CutoffHeight == 0 || in.CutoffTimeUTC <= 0 || len(in.Candidates) > 1000000 || len(artifact.Entries)+len(artifact.Excluded) != len(in.Candidates) || digest(in) != artifact.InputDigest || digest(artifact.Entries) != artifact.OutputDigest {
		return Snapshot{}, ErrUnavailable
	}
	universe := map[string]bool{}
	for _, candidate := range in.Candidates {
		address := candidate.Pool.PoolAddress
		if !addressGrammar.MatchString(address) || address != strings.ToLower(address) || universe[address] {
			return Snapshot{}, ErrUnavailable
		}
		universe[address] = true
	}
	seen := map[string]bool{}
	for _, entry := range artifact.Entries {
		if !universe[entry.Address] || seen[entry.Address] || math.IsNaN(entry.Score) || math.IsInf(entry.Score, 0) || entry.Score < 0 || entry.Score > policy.MaxScore {
			return Snapshot{}, ErrUnavailable
		}
		seen[entry.Address] = true
	}
	for _, excluded := range artifact.Excluded {
		if !universe[excluded.Address] || seen[excluded.Address] || len(excluded.Reason) == 0 || len(excluded.Reason) > 64 {
			return Snapshot{}, ErrUnavailable
		}
		seen[excluded.Address] = true
	}
	return Snapshot{input: in, category: artifact.Category, entries: artifact.Entries, excluded: artifact.Excluded, inputDigest: artifact.InputDigest, outputDigest: artifact.OutputDigest}, nil
}
