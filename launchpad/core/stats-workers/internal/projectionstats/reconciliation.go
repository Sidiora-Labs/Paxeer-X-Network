package projectionstats

import (
	"fmt"
	"math/big"
	"sort"
)

type FeeDisagreement struct {
	ChainID       string `json:"chainId"`
	AssetAddress  string `json:"assetAddress"`
	AssetDecimals int    `json:"assetDecimals"`
	Disposition   string `json:"disposition"`
	ExpectedRaw   string `json:"expectedRaw"`
	ObservedRaw   string `json:"observedRaw"`
	RawUnit       string `json:"rawUnit"`
	FromHeight    string `json:"fromHeight"`
	ThroughHeight string `json:"throughHeight"`
	Revision      string `json:"revision"`
}

type FeeReconciliation struct {
	Watermark     Position          `json:"watermark"`
	Disagreements []FeeDisagreement `json:"disagreements"`
}

func ReconcileTradingFees(expected, observed []AssetFee, fromHeight string, watermark Position, revision string) (FeeReconciliation, error) {
	out := FeeReconciliation{Watermark: watermark, Disagreements: []FeeDisagreement{}}
	from, err := Uint(fromHeight)
	if err != nil || watermark.Validate() != nil {
		return out, ErrInvalid
	}
	through, _ := Uint(watermark.Height)
	rev, err := Uint(revision)
	if err != nil || rev.Sign() == 0 || from.Cmp(through) > 0 || len(expected) > 4096 || len(observed) > 4096 {
		return out, ErrInvalid
	}
	type group struct {
		asset       string
		decimals    int
		disposition string
	}
	keys := map[group]bool{}
	fold := func(fees []AssetFee) (map[group]*big.Int, error) {
		totals := map[group]*big.Int{}
		for _, fee := range fees {
			if !address.MatchString(fee.Asset) || fee.Decimals < 0 || fee.Decimals > 255 || (fee.Disposition != "distributed_usdl" && fee.Disposition != "retained_token_reserve") || len(fee.Raw) > 156 || !digits.MatchString(fee.Raw) {
				return nil, ErrInvalid
			}
			key := group{fee.Asset, fee.Decimals, fee.Disposition}
			raw, _ := new(big.Int).SetString(fee.Raw, 10)
			if totals[key] == nil {
				totals[key] = new(big.Int)
			}
			totals[key].Add(totals[key], raw)
			keys[key] = true
		}
		return totals, nil
	}
	want, err := fold(expected)
	if err != nil {
		return out, err
	}
	got, err := fold(observed)
	if err != nil {
		return out, err
	}
	ordered := make([]group, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.asset != b.asset {
			return a.asset < b.asset
		}
		if a.decimals != b.decimals {
			return a.decimals < b.decimals
		}
		return a.disposition < b.disposition
	})
	for _, key := range ordered {
		if want[key] == nil {
			want[key] = new(big.Int)
		}
		if got[key] == nil {
			got[key] = new(big.Int)
		}
		if want[key].Cmp(got[key]) == 0 {
			continue
		}
		out.Disagreements = append(out.Disagreements, FeeDisagreement{"125", key.asset, key.decimals, key.disposition, want[key].String(), got[key].String(), fmt.Sprintf("%s base units (%d decimals)", key.asset, key.decimals), fromHeight, watermark.Height, revision})
	}
	return out, nil
}
