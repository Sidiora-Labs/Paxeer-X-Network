// Package risk is the pure pool risk-rating heuristic, ported 1:1 from the TS
// stats/src/services/risk-rating.ts (calculateRiskRating). It is deliberately
// dependency-free and side-effect-free so it can be unit-tested exhaustively and
// reused by the holder enrichment path. Concentration/holdings inputs are
// basis-point strings; the score is clamped to [0,100] and factors are returned
// in TS declaration order (the JSON array order callers persist verbatim).
package risk

import (
	"math"
	"strconv"
)

// Input mirrors the TS RiskInput.
type Input struct {
	CreatedAt          int64
	Top10Concentration string
	CreatorHoldingsPct string
	UniqueTraders24h   int
	HolderCount        int
}

// Result mirrors the TS RiskResult: a 0-100 score plus ordered factor tags.
type Result struct {
	Score   int
	Factors []string
}

// jsNumber mirrors JavaScript Number(string): a leading/trailing-trimmed numeric
// parse, where an empty string is 0 and any non-numeric input is NaN. Only the
// threshold comparisons below consume it, and every threshold is positive, so a
// NaN (or 0) input fails all of them identically — matching the TS branches.
func jsNumber(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

// Calculate computes the risk score and factors for a pool. nowUnix is the
// current unix time in seconds (injected for determinism; the TS uses
// Date.now()/1000). The factor push order is identical to the TS source.
func Calculate(in Input, nowUnix int64) Result {
	factors := []string{}
	score := 0

	// 1. Age risk: younger = riskier.
	ageHours := float64(nowUnix-in.CreatedAt) / 3600
	switch {
	case ageHours < 1:
		score += 25
		factors = append(factors, "very_new")
	case ageHours < 24:
		score += 15
		factors = append(factors, "new")
	case ageHours < 168:
		score += 5
	}

	// 2. Holder concentration (top-10, basis points).
	top10 := jsNumber(in.Top10Concentration)
	switch {
	case top10 > 8000:
		score += 25
		factors = append(factors, "extreme_concentration")
	case top10 > 5000:
		score += 15
		factors = append(factors, "high_concentration")
	case top10 > 3000:
		score += 5
	}

	// 3. Creator holdings (basis points).
	creatorPct := jsNumber(in.CreatorHoldingsPct)
	switch {
	case creatorPct > 5000:
		score += 20
		factors = append(factors, "creator_heavy")
	case creatorPct > 2000:
		score += 10
		factors = append(factors, "creator_significant")
	}

	// 4. Trading activity.
	if in.UniqueTraders24h < 3 {
		score += 10
		factors = append(factors, "low_activity")
	}

	// 5. Holder count.
	if in.HolderCount < 5 {
		score += 10
		factors = append(factors, "few_holders")
	}

	if score > 100 {
		score = 100
	}
	return Result{Score: score, Factors: factors}
}
