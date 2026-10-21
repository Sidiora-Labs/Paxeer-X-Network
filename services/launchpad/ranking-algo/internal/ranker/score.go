// Package ranker computes the pool rankings (trending, breakout, top-volume,
// movers, unusual, new-pools) and publishes them to Redis sorted sets, porting
// @market-microservices/ranking-algo (rankers/trending.ts, rankers/new-pools.ts,
// rankers/helpers.ts).
//
// The numeric SCORES here are heuristic ranking weights, not money. They are
// computed in float64 to (a) match the TS implementation byte-for-byte and (b)
// match Redis ZSET semantics, whose scores are IEEE-754 doubles. No token/price/
// fee/PnL value is ever persisted from these computations, so invariant i1
// (no float for money) is preserved: money columns are read as text from
// Postgres and only coerced to float64 to derive a ranking heuristic.
package ranker

import (
	"math"
	"strconv"
)

// Scored is one ranked entry: a pool address and its heuristic score.
type Scored struct {
	Address string
	Score   float64
}

// PoolStat is the subset of stats.pool_stats columns the rankers read. Money/
// price columns are kept as their raw text representation (invariant i1).
type PoolStat struct {
	PoolAddress      string
	Volume24h        string
	Volume1h         string
	Volume5m         string
	MarketCap        string
	PriceChange24h   string
	BuyCount24h      int
	SellCount24h     int
	UniqueTraders24h int
	HolderCount      int
}

// jsParseFloat keeps its private historical name while rejecting prefixes,
// exponents, whitespace and nonfinite values under the v2 decimal policy.
// A rejected input yields NaN, exactly what JS parseFloat returns for an
// unparseable string, so the failure value is falsy in the `parseFloat(s) ||
// fallback` idioms below and can never be mistaken for a parsed zero.
func jsParseFloat(s string) (float64, bool) {
	if len(s) > 512 || !decimalGrammar.MatchString(s) {
		return math.NaN(), false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return math.NaN(), false
	}
	return f, true
}

// parseFloatOr replicates the JS idiom `parseFloat(s) || fallback`: the fallback
// is returned when the parse yields NaN OR zero (both falsy in JS).
func parseFloatOr(s string, fallback float64) float64 {
	f, ok := jsParseFloat(s)
	if !ok || f == 0 {
		return fallback
	}
	return f
}

// absParseFloatOrZero replicates the JS idiom `Math.abs(parseFloat(s)) || 0`:
// NaN and zero both collapse to 0; otherwise the absolute value is returned.
func absParseFloatOrZero(s string) float64 {
	f, ok := jsParseFloat(s)
	if !ok {
		return 0
	}
	a := math.Abs(f)
	if a == 0 {
		return 0
	}
	return a
}

// ScoreTrending computes the trending heuristic for a pool (rankers/trending.ts
// computeTrending). Every pre-filtered pool is ranked.
func ScoreTrending(p PoolStat) float64 {
	vol24h := parseFloatOr(p.Volume24h, 1)
	vol1h := parseFloatOr(p.Volume1h, 0)
	volumeVelocity := vol1h / (vol24h / 24)
	traderScore := math.Min(float64(p.UniqueTraders24h)/50, 1)
	priceChange := absParseFloatOrZero(p.PriceChange24h) / 100
	tradeFreq := float64(p.BuyCount24h+p.SellCount24h) / 24

	return math.Min(volumeVelocity, 10)/10*0.4 +
		traderScore*0.25 +
		math.Min(priceChange, 1)*0.2 +
		math.Min(tradeFreq/10, 1)*0.15
}

// ScoreBreakout computes the breakout heuristic (computeBreakout). The bool is
// false when the entry should be dropped (score <= 0, parity with `if (score>0)`).
func ScoreBreakout(p PoolStat) (float64, bool) {
	vol1h := parseFloatOr(p.Volume1h, 0)
	vol24hAvgHourly := parseFloatOr(p.Volume24h, 1) / 24
	volumeSpike := vol1h / vol24hAvgHourly
	priceChange := absParseFloatOrZero(p.PriceChange24h)
	holderCount := p.HolderCount
	if holderCount == 0 {
		holderCount = 1
	}

	score := (priceChange / 100) * math.Log2(1+volumeSpike) * math.Log2(float64(holderCount))
	return score, score > 0
}

// ScoreMovers computes the movers heuristic (computeMovers) for a pool that is
// NOT already in the trending top-50. The bool is false when the entry should be
// dropped (mcap <= 0, or score <= 0).
func ScoreMovers(p PoolStat) (float64, bool) {
	mcap := parseFloatOr(p.MarketCap, 0)
	vol1h := parseFloatOr(p.Volume1h, 0)
	vol24hAvg := parseFloatOr(p.Volume24h, 1) / 24
	holderCount := p.HolderCount

	if mcap <= 0 {
		return 0, false
	}

	volumeAccel := vol1h / vol24hAvg
	holderSignal := math.Log2(1 + float64(holderCount))
	score := volumeAccel * holderSignal / math.Log10(1+mcap)
	return score, score > 0
}

// ScoreUnusual computes the unusual-activity heuristic (computeUnusual). The bool
// is false when the entry should be dropped (score <= 0).
func ScoreUnusual(p PoolStat) (float64, bool) {
	vol5m := parseFloatOr(p.Volume5m, 0)
	vol1h := parseFloatOr(p.Volume1h, 1)
	vol24h := parseFloatOr(p.Volume24h, 1)

	shortTermSpike := vol5m / (vol1h / 12)
	hourlyVsDaily := vol1h / (vol24h / 24)

	totalTrades := p.BuyCount24h + p.SellCount24h
	if totalTrades == 0 {
		totalTrades = 1
	}
	buySellRatio := math.Abs(float64(p.BuyCount24h-p.SellCount24h)) / float64(totalTrades)

	traderConcentration := 0.0
	if p.UniqueTraders24h > 0 {
		traderConcentration = float64(totalTrades) / float64(p.UniqueTraders24h)
	}

	score := math.Min(shortTermSpike, 20)/20*0.35 +
		math.Min(hourlyVsDaily, 10)/10*0.25 +
		buySellRatio*0.2 +
		math.Min(traderConcentration/10, 1)*0.2
	return score, score > 0
}
