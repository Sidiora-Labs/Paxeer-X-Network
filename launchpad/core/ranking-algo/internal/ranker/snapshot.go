package ranker

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

// AlgorithmVersion v3: features are derived from canonical fills
// (ranking-metrics-v3) and "new" ranks every visible, unpaused canonical pool
// by creation order without trading-activity gates.
const AlgorithmVersion = "discovery-v3"
const FeatureSchemaVersion = "ranking-input-v1"

// ActivityModelVersion is the only activity model BuildSnapshot accepts.
const ActivityModelVersion = "activity-v1"

var decimalGrammar = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)
var addressGrammar = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

// Decimal validates the entire accounting string before deriving any heuristic.
// 512 characters bound parsing work; no accounting value is rounded or rewritten.
func Decimal(raw string, signed bool) (*big.Rat, error) {
	if len(raw) > 512 || !decimalGrammar.MatchString(raw) {
		return nil, fmt.Errorf("input-invalid: decimal")
	}
	r, ok := new(big.Rat).SetString(raw)
	if !ok || (!signed && r.Sign() < 0) {
		return nil, fmt.Errorf("input-invalid: sign")
	}
	return r, nil
}

// BoundedRatio computes in rational arithmetic first, avoiding overflow for raw
// amounts larger than float64. Zero denominators are explicitly ineligible.
func BoundedRatio(numerator, denominator string, cap float64) (float64, error) {
	n, e := Decimal(numerator, false)
	if e != nil {
		return 0, e
	}
	d, e := Decimal(denominator, false)
	if e != nil {
		return 0, e
	}
	if d.Sign() == 0 || cap <= 0 || math.IsNaN(cap) || math.IsInf(cap, 0) {
		return 0, fmt.Errorf("input-invalid: minimum-volume")
	}
	ratio := new(big.Rat).Quo(n, d)
	bound := new(big.Rat).SetFloat64(cap)
	if ratio.Cmp(bound) > 0 {
		return cap, nil
	}
	f, _ := ratio.Float64()
	return f, nil
}
func ValidatePool(p PoolStat) error {
	for _, v := range []string{p.Volume24h, p.Volume1h, p.Volume5m, p.MarketCap} {
		if _, e := Decimal(v, false); e != nil {
			return e
		}
	}
	if _, e := Decimal(p.PriceChange24h, true); e != nil {
		return e
	}
	if p.BuyCount24h < 0 || p.SellCount24h < 0 || p.UniqueTraders24h < 0 || p.HolderCount < 0 {
		return fmt.Errorf("input-invalid: negative-count")
	}
	return nil
}

type CategoryPolicy struct {
	Category          string  `json:"category"`
	Version           string  `json:"version"`
	MinimumAgeSeconds int64   `json:"minimumAgeSeconds"`
	MinimumDepth      string  `json:"minimumDepth"`
	MinimumTraders    int     `json:"minimumTraders"`
	FreshnessSeconds  int64   `json:"freshnessSeconds"`
	MaxScore          float64 `json:"maxScore"`
	Description       string  `json:"description"`
}

// Policies are returned by value, so callers cannot mutate the release policy.
func Policy(category string) (CategoryPolicy, error) {
	switch category {
	case "trending", "breakout", "volume", "movers", "unusual", "new":
	default:
		return CategoryPolicy{}, fmt.Errorf("input-invalid: category")
	}
	freshness, traders := int64(90), 1
	if category == "new" {
		freshness, traders = 30, 0
	}
	return CategoryPolicy{category, AlgorithmVersion + "/" + category, 0, "0", traders, freshness, 1e12, "Heuristic discovery score; not a safety rating or investment advice."}, nil
}

type ActivityFeatures struct {
	RawVolume      string   `json:"rawVolume"`
	AdjustedVolume string   `json:"adjustedVolume"`
	Flags          []string `json:"suspiciousActivityFlags"`
	Confidence     string   `json:"confidence"`
	ModelVersion   string   `json:"modelVersion"`
	WindowStart    int64    `json:"windowStart"`
	WindowEnd      int64    `json:"windowEnd"`
}

// AdjustActivity preserves chain facts. Only evidenced self volume is removed;
// shared funding is a risk signal and never proof of common ownership.
func AdjustActivity(raw, self string, available, sharedFunding bool, start, end int64) (ActivityFeatures, error) {
	a := ActivityFeatures{RawVolume: raw, ModelVersion: ActivityModelVersion, WindowStart: start, WindowEnd: end, Flags: []string{}, Confidence: "complete"}
	r, e := Decimal(raw, false)
	if e != nil {
		return a, e
	}
	s, e := Decimal(self, false)
	if e != nil {
		return a, e
	}
	if start < 0 || end <= start || s.Cmp(r) > 0 {
		return a, fmt.Errorf("input-invalid: activity-window-or-volume")
	}
	if !available {
		a.Confidence = "degraded"
		a.Flags = append(a.Flags, "features-unavailable")
		a.AdjustedVolume = "0"
		return a, nil
	}
	if sharedFunding {
		a.Flags = append(a.Flags, "shared-funding-risk")
	}
	if s.Sign() > 0 {
		a.Flags = append(a.Flags, "evidenced-self-volume")
	}
	scale := 0
	for _, v := range []string{raw, self} {
		if i := strings.IndexByte(v, '.'); i >= 0 && len(v)-i-1 > scale {
			scale = len(v) - i - 1
		}
	}
	a.AdjustedVolume = new(big.Rat).Sub(r, s).FloatString(scale)
	return a, nil
}

type NullableMetrics struct {
	Version        string   `json:"version"`
	MarketCap      *string  `json:"marketCap"`
	Depth          *string  `json:"depth"`
	PriceChange24h *string  `json:"priceChange24h"`
	HolderCount    *int     `json:"holderCount"`
	Paused         *bool    `json:"paused"`
	AdjustedVolume *string  `json:"adjustedDiscoveryVolume"`
	Unavailable    []string `json:"unavailable"`
}
type Candidate struct {
	NullableMetrics      *NullableMetrics `json:"nullableMetrics,omitempty"`
	Pool                 PoolStat         `json:"pool"`
	SourceRevision       string           `json:"sourceRevision"`
	SourceObservedAt     int64            `json:"sourceObservedAt"`
	ModerationObservedAt int64            `json:"moderationObservedAt"`
	Depth                string           `json:"depth"`
	Paused               bool             `json:"paused"`
	Suppressed           bool             `json:"suppressed"`
	SupplyVerified       bool             `json:"supplyVerified"`
	Activity             ActivityFeatures `json:"activity"`
	CreationHeight       uint64           `json:"creationHeight"`
	TransactionIndex     uint32           `json:"transactionIndex"`
	LogIndex             uint32           `json:"logIndex"`
}
type InputSnapshot struct {
	RunID                string      `json:"runId"`
	AlgorithmVersion     string      `json:"algorithmVersion"`
	FeatureSchemaVersion string      `json:"featureSchemaVersion"`
	CutoffHeight         uint64      `json:"cutoffHeight"`
	CutoffTimeUTC        int64       `json:"cutoffTimeUTC"`
	Complete             bool        `json:"complete"`
	Candidates           []Candidate `json:"candidates"`
	Provenance           *Provenance `json:"provenance,omitempty"`
}
type Exclusion struct {
	Address string `json:"address"`
	Reason  string `json:"reason"`
}

// Snapshot's slices are private. Accessors return copies; persisted JSON is a
// copy of the canonical artifact, not an alias to the caller's input.
type Snapshot struct {
	input                     InputSnapshot
	category                  string
	entries                   []Scored
	excluded                  []Exclusion
	inputDigest, outputDigest string
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s Snapshot) Entries() []Scored       { return append([]Scored{}, s.entries...) }
func (s Snapshot) Exclusions() []Exclusion { return append([]Exclusion{}, s.excluded...) }
func (s Snapshot) InputDigest() string     { return s.inputDigest }
func (s Snapshot) OutputDigest() string    { return s.outputDigest }
func (s Snapshot) Category() string        { return s.category }
func (s Snapshot) RunID() string           { return s.input.RunID }
func (s Snapshot) Stale(now time.Time) bool {
	p, _ := Policy(s.category)
	return now.Unix() < s.input.CutoffTimeUTC || s.input.CutoffTimeUTC <= 0 || now.Unix()-s.input.CutoffTimeUTC > p.FreshnessSeconds
}
func (s Snapshot) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Input        InputSnapshot `json:"input"`
		Category     string        `json:"category"`
		Entries      []Scored      `json:"entries"`
		Excluded     []Exclusion   `json:"excluded"`
		InputDigest  string        `json:"inputDigest"`
		OutputDigest string        `json:"outputDigest"`
	}{s.input, s.category, s.Entries(), s.Exclusions(), s.inputDigest, s.outputDigest})
}
func order(entries []Scored) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Score == entries[j].Score {
			return entries[i].Address < entries[j].Address
		}
		return entries[i].Score > entries[j].Score
	})
}
func BuildSnapshot(in InputSnapshot, category string, trending *Snapshot) (Snapshot, error) {
	_, e := Policy(category)
	if e != nil {
		return Snapshot{}, e
	}
	if !in.Complete || in.RunID == "" || len(in.RunID) > 64 || in.CutoffTimeUTC <= 0 || in.CutoffHeight == 0 || in.AlgorithmVersion != AlgorithmVersion || in.FeatureSchemaVersion != FeatureSchemaVersion || len(in.Candidates) > 1000000 {
		return Snapshot{}, fmt.Errorf("input-invalid: incomplete-or-version")
	}
	// Bound untrusted metadata before allocating a deep copy.
	for _, c := range in.Candidates {
		if len(c.Pool.PoolAddress) != 42 || len(c.Activity.ModelVersion) > 64 || len(c.Activity.Confidence) > 32 || len(c.SourceRevision) > 64 || len(c.Activity.Flags) > 8 || len(c.Depth) > 512 || len(c.Activity.RawVolume) > 512 || len(c.Activity.AdjustedVolume) > 512 {
			return Snapshot{}, fmt.Errorf("input-invalid: metadata-bounds")
		}
		for _, flag := range c.Activity.Flags {
			if len(flag) > 64 {
				return Snapshot{}, fmt.Errorf("input-invalid: metadata-bounds")
			}
		}
		for _, v := range []string{c.Pool.Volume24h, c.Pool.Volume1h, c.Pool.Volume5m, c.Pool.MarketCap, c.Pool.PriceChange24h} {
			if len(v) > 512 {
				return Snapshot{}, fmt.Errorf("input-invalid: amount-bounds")
			}
		}
	}
	// Deep copy and canonicalize the universe independently of query order.
	b, e := json.Marshal(in)
	if e != nil {
		return Snapshot{}, e
	}
	var frozen InputSnapshot
	if e = json.Unmarshal(b, &frozen); e != nil {
		return Snapshot{}, e
	}
	for i := range frozen.Candidates {
		frozen.Candidates[i].Pool.PoolAddress = strings.ToLower(frozen.Candidates[i].Pool.PoolAddress)
	}
	sort.Slice(frozen.Candidates, func(i, j int) bool {
		return frozen.Candidates[i].Pool.PoolAddress < frozen.Candidates[j].Pool.PoolAddress
	})
	return buildFrozenSnapshot(frozen, category, trending)
}
func buildFrozenSnapshot(frozen InputSnapshot, category string, trending *Snapshot) (Snapshot, error) {
	p, e := Policy(category)
	if e != nil {
		return Snapshot{}, e
	}
	in := frozen
	s := Snapshot{input: frozen, category: category, entries: []Scored{}, excluded: []Exclusion{}}
	s.inputDigest = digest(frozen)
	excludedTrending := map[string]bool{}
	if category == "movers" {
		if trending == nil || trending.category != "trending" || trending.inputDigest != s.inputDigest {
			return Snapshot{}, fmt.Errorf("dependency-unavailable: trending")
		}
		for i, v := range trending.entries {
			if i == 50 {
				break
			}
			excludedTrending[v.Address] = true
		}
	}
	seen := map[string]bool{}
	for _, c := range frozen.Candidates {
		addr := c.Pool.PoolAddress
		if !addressGrammar.MatchString(addr) || seen[addr] {
			return Snapshot{}, fmt.Errorf("input-invalid: address-or-duplicate")
		}
		seen[addr] = true
		reason := ""
		score := float64(0)
		pool := c.Pool
		switch {
		case ValidatePool(pool) != nil:
			reason = "input-invalid"
		case c.SourceRevision == "":
			reason = "missing-source-revision"
		case c.SourceObservedAt <= 0 || c.SourceObservedAt > in.CutoffTimeUTC || in.CutoffTimeUTC-c.SourceObservedAt > p.FreshnessSeconds:
			reason = "source-stale"
		case c.ModerationObservedAt <= 0 || c.ModerationObservedAt > in.CutoffTimeUTC || in.CutoffTimeUTC-c.ModerationObservedAt > 60:
			reason = "moderation-stale"
		case category == "new" && (c.CreationHeight == 0 || c.CreationHeight > in.CutoffHeight):
			reason = "creation-provenance-invalid"
		case c.Suppressed:
			reason = "suppressed"
		case c.Paused:
			reason = "paused"
		case category == "new":
			// "new" orders canonical creations; a pool is listed from its
			// creation block, before its first trade.
		case c.Pool.UniqueTraders24h < p.MinimumTraders:
			reason = "minimum-traders"
		}
		if reason == "" && category == "new" {
			s.entries = append(s.entries, Scored{addr, 0})
			continue
		}
		if reason == "" {
			depth, err := Decimal(c.Depth, false)
			if err != nil || depth.Sign() == 0 {
				reason = "minimum-depth"
			}
		}
		if reason == "" && (c.Activity.Confidence != "complete" || c.Activity.ModelVersion != ActivityModelVersion || c.Activity.WindowEnd != in.CutoffTimeUTC || c.Activity.WindowStart < 0 || c.Activity.WindowStart >= c.Activity.WindowEnd) {
			reason = "activity-unavailable"
		}
		if reason == "" {
			raw, err := Decimal(c.Activity.RawVolume, false)
			adj, ae := Decimal(c.Activity.AdjustedVolume, false)
			v, _ := Decimal(pool.Volume24h, false)
			if err != nil || ae != nil || raw.Cmp(v) != 0 || adj.Cmp(raw) > 0 {
				reason = "input-invalid"
			} else if adj.Sign() == 0 {
				reason = "minimum-volume"
			} else {
				factor := new(big.Rat).Quo(adj, raw)
				pool.Volume24h = c.Activity.AdjustedVolume
				for _, target := range []*string{&pool.Volume1h, &pool.Volume5m} {
					r, _ := Decimal(*target, false)
					*target = new(big.Rat).Mul(r, factor).FloatString(18)
				}
				// Evidenced self volume is also evidenced self trades: a wash
				// loop inflates the buy/sell counts exactly as it inflates
				// volume, and those counts feed the trade-activity terms
				// (trending trades/240, unusual imbalance and trades per
				// trader). Scale them by the same adjusted/raw factor, rounding
				// down so self activity can never round a count upward.
				for _, count := range []*int{&pool.BuyCount24h, &pool.SellCount24h} {
					scaled := new(big.Rat).Mul(new(big.Rat).SetInt64(int64(*count)), factor)
					*count = int(new(big.Int).Quo(scaled.Num(), scaled.Denom()).Int64())
				}
			}
		}
		if reason == "" && excludedTrending[addr] {
			reason = "trending-top-50"
		}
		if reason == "" {
			if category == "movers" && !c.SupplyVerified {
				reason = "supply-unverified"
			} else {
				var scoreErr error
				score, scoreErr = finiteScore(category, pool)
				if scoreErr != nil {
					reason = "input-invalid"
				}

				if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
					reason = "input-invalid"
				} else {
					score = math.Min(score, p.MaxScore)
				}
			}
		}
		if reason != "" {
			s.excluded = append(s.excluded, Exclusion{addr, reason})
		} else {
			s.entries = append(s.entries, Scored{addr, score})
		}
	}
	order(s.entries)
	if category == "new" {
		byAddress := map[string]Candidate{}
		for _, c := range frozen.Candidates {
			byAddress[c.Pool.PoolAddress] = c
		}
		sort.Slice(s.entries, func(i, j int) bool {
			a, b := byAddress[s.entries[i].Address], byAddress[s.entries[j].Address]
			if a.CreationHeight != b.CreationHeight {
				return a.CreationHeight > b.CreationHeight
			}
			if a.TransactionIndex != b.TransactionIndex {
				return a.TransactionIndex > b.TransactionIndex
			}
			if a.LogIndex != b.LogIndex {
				return a.LogIndex > b.LogIndex
			}
			return a.Pool.PoolAddress < b.Pool.PoolAddress
		})
	}
	s.outputDigest = digest(s.entries)
	return s, nil
}
