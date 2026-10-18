package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

type CanonicalCandleClient struct {
	Endpoint string
	Client   *http.Client
}
type CanonicalCandlePage struct {
	Items                 []interfaces.Candle
	SnapshotID, Algorithm string
	Watermark             *interfaces.Watermark
	Complete              bool
}

func (c CanonicalCandleClient) Read(ctx context.Context, r HistoryRequest) (CanonicalCandlePage, error) {
	out := CanonicalCandlePage{Items: []interfaces.Candle{}, Complete: true}
	if err := validateHistoryRequest(r); err != nil {
		return out, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	base, err := url.Parse(c.Endpoint)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "http" && base.Scheme != "https") {
		return out, errors.New("canonical history authority unavailable")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v2/pools/" + r.Symbol + "/candles"
	q := url.Values{"interval": {r.Timeframe}, "from": {time.Unix(r.From, 0).UTC().Format("2006-01-02T15:04:05.000Z")}, "to": {time.Unix(r.To, 0).UTC().Format("2006-01-02T15:04:05.000Z")}, "limit": {"2000"}}
	seenCursors := map[string]bool{}
	for page := 0; page < 3; page++ {
		base.RawQuery = q.Encode()
		var response interfaces.CandlesResponse
		if err = boundedChartJSON(ctx, c.Client, base.String(), &response, "CandlesResponse"); err != nil {
			return out, err
		}
		if response.Watermark == nil || response.Watermark.ChainId != "125" || response.Watermark.Integrity != "healthy" || response.Revision == nil || response.Quality == "quarantined" || response.Quality == "stale" {
			return out, errors.New("canonical history not healthy")
		}
		if page == 0 {
			out.SnapshotID = response.Data.SnapshotId
			out.Algorithm = response.Data.SeriesModelVersion
			out.Watermark = response.Watermark
		} else if out.SnapshotID != response.Data.SnapshotId || out.Algorithm != response.Data.SeriesModelVersion || *out.Watermark != *response.Watermark {
			return out, errors.New("canonical history changed during pagination")
		}
		out.Complete = out.Complete && response.Quality == "complete"
		for _, bar := range response.Data.Items {
			start, e := time.Parse("2006-01-02T15:04:05.000Z", bar.Start)
			if e != nil || start.Unix() < r.From || start.Unix() >= r.To || strings.ToLower(bar.PoolAddress) != r.Symbol || bar.Interval != r.Timeframe || bar.SnapshotId != out.SnapshotID || bar.SeriesModelVersion != out.Algorithm {
				return out, errors.New("canonical candle scope mismatch")
			}
			if len(out.Items) > 0 && out.Items[len(out.Items)-1].Start >= bar.Start {
				return out, errors.New("canonical history order mismatch")
			}
			out.Items = append(out.Items, bar)
		}
		if len(out.Items) > MaxHistoryBars {
			return out, errors.New("canonical history exceeds vendor bound")
		}
		if response.NextCursor == nil {
			if r.Countback > 0 && len(out.Items) > r.Countback {
				out.Items = out.Items[len(out.Items)-r.Countback:]
			}
			return out, nil
		}
		if *response.NextCursor == "" || len(*response.NextCursor) > 4096 || seenCursors[*response.NextCursor] {
			return out, errors.New("invalid canonical continuation")
		}
		seenCursors[*response.NextCursor] = true
		q.Set("cursor", *response.NextCursor)
	}
	return out, errors.New("canonical history requires narrower range")
}

type SupplySnapshot struct {
	Token          string `json:"token"`
	Decimals       int    `json:"decimals"`
	TotalSupplyRaw string `json:"totalSupplyRaw"`
	SourceBlock    string `json:"supplySourceBlock"`
	SourceHash     string `json:"supplySourceHash"`
	Revision       string `json:"valuationRevision"`
	Alignment      string `json:"alignment"`
}

// RPCSupplySource reads a token's total supply at the block a price came
// from, bound to the node-reported block hash of that block (chainread).
type RPCSupplySource struct{ Chain *chainread.Reader }

func (s RPCSupplySource) At(ctx context.Context, token string, position interfaces.ChainPosition) (SupplySnapshot, error) {
	var result SupplySnapshot
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	if s.Chain == nil || position.ChainId != "125" || !poolAddressPattern.MatchString(token) {
		return result, errors.New("historical supply unavailable")
	}
	height, err := strconv.ParseUint(string(position.Height), 10, 63)
	if err != nil {
		return result, err
	}
	blockHash := strings.ToLower(position.BlockHash)
	address := common.HexToAddress(token)
	values, err := s.Chain.CallAt(ctx, int64(height), blockHash, []ethereum.CallMsg{{To: &address, Data: []byte{0x18, 0x16, 0x0d, 0xdd}}, {To: &address, Data: []byte{0x31, 0x3c, 0xe5, 0x67}}})
	if err != nil || len(values) != 2 || len(values[0]) != 32 || len(values[1]) != 32 {
		return result, errors.New("historical token supply read failed")
	}
	decimals := new(big.Int).SetBytes(values[1])
	if !decimals.IsInt64() || decimals.Int64() > 255 {
		return result, errors.New("unverified historical decimals")
	}
	total := new(big.Int).SetBytes(values[0]).String()
	digest := sha256.Sum256([]byte(strings.ToLower(token) + ":" + blockHash + ":" + total + ":" + decimals.String()))
	return SupplySnapshot{Token: strings.ToLower(token), Decimals: int(decimals.Int64()), TotalSupplyRaw: total, SourceBlock: strconv.FormatUint(height, 10), SourceHash: blockHash, Revision: hex.EncodeToString(digest[:]), Alignment: "canonical_block_end"}, nil
}
func exactPrice(p interfaces.Price) (*big.Rat, error) {
	if len(p.Numerator) > 512 || len(p.Denominator) > 512 {
		return nil, errors.New("oversized exact price")
	}
	n, ok := new(big.Int).SetString(string(p.Numerator), 10)
	if !ok || n.Sign() < 0 || !integerPattern.MatchString(string(p.Numerator)) {
		return nil, errors.New("invalid price numerator")
	}
	d, ok := new(big.Int).SetString(string(p.Denominator), 10)
	if !ok || d.Sign() <= 0 || !integerPattern.MatchString(string(p.Denominator)) {
		return nil, errors.New("invalid price denominator")
	}
	return new(big.Rat).SetFrac(n, d), nil
}
func totalSupplyValuation(p interfaces.Price, s SupplySnapshot) (*big.Rat, error) {
	if s.Decimals < 0 || s.Decimals > 255 || p.BaseAsset.Address == nil || p.BaseAsset.Decimals == nil || p.Source.Position == nil || !strings.EqualFold(*p.BaseAsset.Address, s.Token) || int(*p.BaseAsset.Decimals) != s.Decimals || string(p.Source.Position.Height) != s.SourceBlock || !strings.EqualFold(p.Source.Position.BlockHash, s.SourceHash) || s.Alignment != "canonical_block_end" {
		return nil, errors.New("supply provenance mismatch")
	}
	price, err := exactPrice(p)
	if err != nil {
		return nil, err
	}
	if len(s.TotalSupplyRaw) > 78 || !integerPattern.MatchString(s.TotalSupplyRaw) {
		return nil, errors.New("invalid total supply")
	}
	supply, ok := new(big.Int).SetString(s.TotalSupplyRaw, 10)
	if !ok || supply.Sign() < 0 || supply.BitLen() > 256 {
		return nil, errors.New("invalid total supply")
	}
	return price.Mul(price, new(big.Rat).SetFrac(supply, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(s.Decimals)), nil))), nil
}
func vendorRational(value *big.Rat) (float64, error) {
	v, _ := value.Float64()
	if v == 0 && value.Sign() != 0 {
		return 0, errors.New("vendor price underflow")
	}
	return displayNumber(strconv.FormatFloat(v, 'g', -1, 64))
}
func valuationHistory(ctx context.Context, page CanonicalCandlePage, supply *RPCSupplySource, mode string) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if mode != "price" && mode != "total_supply_valuation" {
		return nil, errors.New("invalid valuation mode")
	}
	if len(page.Items) > MaxHistoryBars {
		return nil, errors.New("unbounded valuation history")
	}
	times := []int64{}
	values := make([][]float64, 5)
	for i := range values {
		values[i] = []float64{}
	}
	valuations := []any{}
	available := true
	valuationValues := make([][]float64, 4)
	supplies := map[string]SupplySnapshot{}
	supplyFailures := map[string]bool{}
	for _, bar := range page.Items {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		start, err := time.Parse("2006-01-02T15:04:05.000Z", bar.Start)
		if err != nil {
			return nil, err
		}
		if start.Unix() < 0 || start.Unix() > maxHistoryTimestamp || (len(times) > 0 && times[len(times)-1] >= start.Unix()) {
			return nil, errors.New("invalid valuation candle order")
		}
		times = append(times, start.Unix())
		prices := []interfaces.Price{bar.Open, bar.High, bar.Low, bar.Close}
		provenance := []SupplySnapshot{}
		valued := make([]float64, 4)
		barAvailable := true
		for i, p := range prices {
			rational, e := exactPrice(p)
			if e != nil {
				return nil, e
			}
			v, e := vendorRational(rational)
			if e != nil {
				return nil, e
			}
			values[i] = append(values[i], v)
			if supply == nil || p.Source.Position == nil || p.BaseAsset.Address == nil {
				barAvailable = false
				continue
			}
			supplyKey := strings.ToLower(*p.BaseAsset.Address) + ":" + p.Source.Position.BlockHash
			snapshot, found := supplies[supplyKey]
			e = nil
			if !found && !supplyFailures[supplyKey] {
				snapshot, e = supply.At(ctx, *p.BaseAsset.Address, *p.Source.Position)
				if e == nil {
					supplies[supplyKey] = snapshot
				} else {
					supplyFailures[supplyKey] = true
				}
			}
			if supplyFailures[supplyKey] {
				barAvailable = false
				continue
			}
			value, e := totalSupplyValuation(p, snapshot)
			if e != nil {
				barAvailable = false
				continue
			}
			valued[i], e = vendorRational(value)
			if e != nil {
				barAvailable = false
				continue
			}
			provenance = append(provenance, snapshot)
		}
		volume, err := exactFixed(string(bar.VolumeUsdlRaw), 6)
		if err != nil {
			return nil, err
		}
		v, err := displayNumber(volume)
		if err != nil {
			return nil, err
		}
		values[4] = append(values[4], v)
		if !barAvailable {
			available = false
			valuations = append(valuations, map[string]any{"available": false, "valuationBasis": "total_supply_valuation", "reason": "historical_supply_unavailable"})
		} else {
			valuations = append(valuations, map[string]any{"available": true, "valuationBasis": "total_supply_valuation", "o": valued[0], "h": valued[1], "l": valued[2], "c": valued[3], "supply": provenance})
			for i := 0; i < 4; i++ {
				valuationValues[i] = append(valuationValues[i], valued[i])
			}
		}
	}
	body := map[string]any{"s": "ok", "t": times, "o": values[0], "h": values[1], "l": values[2], "c": values[3], "v": values[4], "mode": mode, "valuationBasis": "total_supply_valuation", "valuationAvailable": available, "valuations": valuations, "snapshotId": page.SnapshotID, "algorithmVersion": page.Algorithm, "watermark": page.Watermark}
	if mode == "total_supply_valuation" && available {
		for i, key := range []string{"o", "h", "l", "c"} {
			body[key] = valuationValues[i]
		}
	}
	if len(times) == 0 {
		body["s"] = "no_data"
	}
	if mode == "total_supply_valuation" && !available {
		body["s"] = "unavailable"
		body["priceHistory"] = map[string]any{"t": times, "o": values[0], "h": values[1], "l": values[2], "c": values[3]}
		delete(body, "o")
		delete(body, "h")
		delete(body, "l")
		delete(body, "c")
	}
	return json.Marshal(body)
}
