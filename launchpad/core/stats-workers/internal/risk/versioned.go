package risk

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
)

const ModelVersion = "risk-v1"

// V1Result reports a partial sum only; Score is unavailable unless all inputs
// are measured. This descriptive heuristic makes no contract-safety claim.
type V1Result struct {
	ModelVersion    string               `json:"model_version"`
	Score           *int                 `json:"score"`
	PartialScore    int                  `json:"partial_score"`
	Coverage        string               `json:"coverage"`
	Unavailable     []string             `json:"unavailable"`
	QualityFlags    []string             `json:"quality_flags"`
	Factors         []string             `json:"factors"`
	CalculatedAt    int64                `json:"calculated_at"`
	InputsWatermark *analytics.Watermark `json:"inputs_watermark"`
}

// CalculateV1 retains frozen coefficients and strict concentration boundaries.
// Empty strings are unknown, not measured zero. Negative counts mean unknown.
func CalculateV1(in Input, now int64, watermark *analytics.Watermark) V1Result {
	if watermark != nil {
		copy := *watermark
		watermark = &copy
	}
	r := V1Result{ModelVersion: ModelVersion, Coverage: "complete", Unavailable: []string{}, QualityFlags: []string{}, Factors: []string{}, CalculatedAt: now, InputsWatermark: watermark}
	add := func(n int, factor string) {
		r.PartialScore += n
		if factor != "" {
			r.Factors = append(r.Factors, factor)
		}
	}
	unknown := func(s string) {
		r.Unavailable = append(r.Unavailable, s)
		r.QualityFlags = append(r.QualityFlags, s+"_unavailable")
	}
	if in.CreatedAt <= 0 || now < in.CreatedAt {
		unknown("age")
	} else {
		age := now - in.CreatedAt
		switch {
		case age < 3600:
			add(25, "very_new")
		case age < 86400:
			add(15, "new")
		case age < 604800:
			add(5, "")
		}
	}
	parse := func(s, name string) (int, bool) {
		if s == "" || len(s) > 5 {
			unknown(name)
			return 0, false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				unknown(name)
				return 0, false
			}
		}
		n, e := strconv.Atoi(s)
		if e != nil || n > 10000 {
			unknown(name)
			return 0, false
		}
		return n, true
	}
	if n, ok := parse(in.Top10Concentration, "top10_concentration"); ok {
		switch {
		case n > 8000:
			add(25, "extreme_concentration")
		case n > 5000:
			add(15, "high_concentration")
		case n > 3000:
			add(5, "")
		}
	}
	if n, ok := parse(in.CreatorHoldingsPct, "creator_holdings"); ok {
		switch {
		case n > 5000:
			add(20, "creator_heavy")
		case n > 2000:
			add(10, "creator_significant")
		}
	}
	if in.UniqueTraders24h < 0 {
		unknown("activity")
	} else if in.UniqueTraders24h < 3 {
		add(10, "low_activity")
	}
	if in.HolderCount < 0 {
		unknown("holders")
	} else if in.HolderCount < 5 {
		add(10, "few_holders")
	}
	if watermark == nil || watermark.Validate() != nil || watermark.Integrity != "healthy" {
		unknown("inputs_watermark")
	}
	if len(r.Unavailable) > 0 {
		r.Coverage = "incomplete"
	} else {
		n := r.PartialScore
		r.Score = &n
	}
	return r
}

// CalculateV1WithSupply rejects concentration provenance when supply is absent,
// malformed or zero. CalculateV1 alone expects already validated basis points.
func CalculateV1WithSupply(in Input, supplyRaw string, now int64, watermark *analytics.Watermark) V1Result {
	valid := len(supplyRaw) > 0 && len(supplyRaw) <= 78
	for _, c := range supplyRaw {
		if c < '0' || c > '9' {
			valid = false
		}
	}
	if len(supplyRaw) > 1 && supplyRaw[0] == '0' {
		valid = false
	}
	supply, ok := new(big.Int).SetString(supplyRaw, 10)
	valid = valid && ok && supply.Sign() > 0 && supply.BitLen() <= 256
	if !valid {
		in.Top10Concentration = ""
		in.CreatorHoldingsPct = ""
	}
	r := CalculateV1(in, now, watermark)
	if !valid {
		r.Unavailable = append(r.Unavailable, "supply")
		r.QualityFlags = append(r.QualityFlags, "supply_unavailable")
		r.Coverage = "incomplete"
		r.Score = nil
	}
	return r
}

//go:embed model-v1.json
var modelV1Metadata []byte

type ProjectionModelV1 struct{}

func (ProjectionModelV1) Version() string { return ModelVersion }
func (ProjectionModelV1) Validate() error {
	digest := sha256.Sum256(modelV1Metadata)
	if hex.EncodeToString(digest[:]) != "605f60cbebe5bc598079f8406b4865eaf9dcbb3025d63bfdb08ee8a07d9abefb" {
		return errors.New("risk-v1 metadata differs from frozen model")
	}
	return nil
}
func (ProjectionModelV1) Calculate(in analytics.RiskInputs, now int64, wm analytics.Watermark) (json.RawMessage, error) {
	if err := (ProjectionModelV1{}).Validate(); err != nil {
		return nil, err
	}
	result := CalculateV1WithSupply(Input{CreatedAt: in.CreatedAt, Top10Concentration: in.Top10Concentration, CreatorHoldingsPct: in.CreatorHoldingsPct, UniqueTraders24h: in.UniqueTraders24h, HolderCount: in.HolderCount}, in.SupplyRaw, now, &wm)
	return json.Marshal(result)
}
func ModelMetadata() json.RawMessage { return append(json.RawMessage(nil), modelV1Metadata...) }
