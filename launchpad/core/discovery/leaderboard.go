package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"math/big"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

type PnLInterval struct {
	Wallet, AccountID, ProfileID, SnapshotID, ValuationRevision, AccountingWatermark, Methodology string
	Start, End                                                                                    time.Time
	GrossProceeds, Fees, CapitalBasis, NetPnL                                                     string
	TradeCount                                                                                    uint64
	KnownBasis, Reconciled, VerifiedLink, AttributionOptIn                                        bool
	QualityFlags                                                                                  []string
}
type IntervalAuthority interface {
	Intervals(context.Context, time.Time, time.Time) ([]PnLInterval, error)
	CurrentAttribution(context.Context, string, string) (bool, string, error)
}
type LeaderboardEntryV2 struct {
	PnLInterval
	ReturnNumerator, ReturnDenominator string
	Rank                               int
}
type LeaderboardV2 struct {
	SchemaVersion                             int
	Methodology                               string
	Start, End                                time.Time
	Entries                                   []LeaderboardEntryV2
	ExcludedUnknownBasis, ExcludedUnqualified int
}

var signedDecimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func signed(s string) (*big.Rat, error) {
	if len(s) > 160 || !signedDecimal.MatchString(s) {
		return nil, ErrInvalid
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, ErrInvalid
	}
	return v, nil
}
func RankIntervals(ctx context.Context, a IntervalAuthority, start, end time.Time) (LeaderboardV2, error) {
	out := LeaderboardV2{SchemaVersion: 2, Methodology: "owner-net-over-known-capital-v1", Start: start, End: end, Entries: []LeaderboardEntryV2{}}
	if a == nil {
		return out, ErrUnavailable
	}
	if start.IsZero() || !end.After(start) || start.Location() != time.UTC || end.Location() != time.UTC {
		return out, ErrInvalid
	}
	rows, e := a.Intervals(ctx, start, end)
	if e != nil {
		return out, e
	}
	if len(rows) > 10000 {
		return out, ErrInvalid
	}
	seen := map[string]bool{}
	for _, v := range rows {
		if v.Start != start || v.End != end || !address.MatchString(v.Wallet) || v.SnapshotID == "" || v.ValuationRevision == "" || v.AccountingWatermark == "" || v.Methodology == "" {
			return out, ErrInvalid
		}
		if seen[v.Wallet] {
			return out, ErrConflict
		}
		seen[v.Wallet] = true
		if !v.KnownBasis {
			out.ExcludedUnknownBasis++
			continue
		}
		if !v.Reconciled || !v.VerifiedLink || !v.AttributionOptIn {
			out.ExcludedUnqualified++
			continue
		}
		opted, profile, e := a.CurrentAttribution(ctx, v.AccountID, v.Wallet)
		if e != nil {
			return out, e
		}
		if !opted || profile == "" || profile != v.ProfileID {
			out.ExcludedUnqualified++
			continue
		}
		basis, e := exact(v.CapitalBasis)
		if e != nil || basis.Sign() == 0 {
			out.ExcludedUnqualified++
			continue
		}
		if _, e = exact(v.GrossProceeds); e != nil {
			return out, e
		}
		if _, e = exact(v.Fees); e != nil {
			return out, e
		}
		net, e := signed(v.NetPnL)
		if e != nil {
			return out, e
		}
		ratio := new(big.Rat).Quo(net, basis)
		out.Entries = append(out.Entries, LeaderboardEntryV2{PnLInterval: v, ReturnNumerator: ratio.Num().String(), ReturnDenominator: ratio.Denom().String()})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		av, _ := new(big.Rat).SetString(a.ReturnNumerator + "/" + a.ReturnDenominator)
		bv, _ := new(big.Rat).SetString(b.ReturnNumerator + "/" + b.ReturnDenominator)
		if av.Cmp(bv) == 0 {
			return a.Wallet < b.Wallet
		}
		return av.Cmp(bv) > 0
	})
	for i := range out.Entries {
		out.Entries[i].Rank = i + 1
	}
	return out, nil
}

type SquadPerformance struct {
	Methodology                                                string
	EligibleMembers, ExcludedUnknownBasis, ExcludedUnqualified int
	NetPnL                                                     string
}

func AggregateSquad(entries []PnLInterval) (SquadPerformance, error) {
	out := SquadPerformance{Methodology: "sum-qualified-opted-in-member-net-v1", NetPnL: "0"}
	total := new(big.Rat)
	precision := 0
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.AccountID] {
			return out, ErrConflict
		}
		seen[e.AccountID] = true
		if !e.KnownBasis {
			out.ExcludedUnknownBasis++
			continue
		}
		if !e.Reconciled || !e.VerifiedLink || !e.AttributionOptIn {
			out.ExcludedUnqualified++
			continue
		}
		v, err := signed(e.NetPnL)
		if err != nil {
			return out, err
		}
		if dot := strings.IndexByte(e.NetPnL, '.'); dot >= 0 {
			precision = max(precision, len(e.NetPnL)-dot-1)
		}
		total.Add(total, v)
		out.EligibleMembers++
	}
	out.NetPnL = total.FloatString(precision)
	return out, nil
}

func (s Store) SquadPerformance(ctx context.Context, a IntervalAuthority, squadID string, start, end time.Time) (SquadPerformance, error) {
	if a == nil {
		return SquadPerformance{}, ErrUnavailable
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT document FROM narrative_discovery.members WHERE squad_id=$1`, squadID)
	if e != nil {
		return SquadPerformance{}, e
	}
	members := map[string]bool{}
	for rows.Next() {
		var b []byte
		var m Membership
		if e = rows.Scan(&b); e == nil {
			e = json.Unmarshal(b, &m)
		}
		if e != nil {
			rows.Close()
			return SquadPerformance{}, e
		}
		if m.CanPublish() {
			members[m.AccountID] = true
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return SquadPerformance{}, e
	}
	entries, e := a.Intervals(ctx, start, end)
	if e != nil {
		return SquadPerformance{}, e
	}
	selected := []PnLInterval{}
	for _, v := range entries {
		if !members[v.AccountID] {
			continue
		}
		if v.Start != start || v.End != end || v.SnapshotID == "" || v.ValuationRevision == "" || v.AccountingWatermark == "" {
			return SquadPerformance{}, ErrInvalid
		}
		opted, profile, err := a.CurrentAttribution(ctx, v.AccountID, v.Wallet)
		if err != nil {
			return SquadPerformance{}, err
		}
		v.AttributionOptIn = v.AttributionOptIn && opted && profile != "" && profile == v.ProfileID
		selected = append(selected, v)
	}
	return AggregateSquad(selected)
}

type WalletAttribution struct {
	AccountID, ProfileID string
	Verified, OptedIn    bool
}
type WalletAttributionAuthority interface {
	WalletAttribution(context.Context, string) (WalletAttribution, error)
	CurrentAttribution(context.Context, string, string) (bool, string, error)
}
type SQLIntervalAuthority struct {
	DB                *sql.DB
	Attribution       WalletAttributionAuthority
	QuoteTokenAddress string
}

func (a SQLIntervalAuthority) CurrentAttribution(ctx context.Context, account, wallet string) (bool, string, error) {
	if a.Attribution == nil {
		return false, "", ErrUnavailable
	}
	return a.Attribution.CurrentAttribution(ctx, account, wallet)
}
func rawInteger(value string) (*big.Int, error) {
	if !regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`).MatchString(value) {
		return nil, ErrInvalid
	}
	out, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return nil, ErrInvalid
	}
	return out, nil
}
func usdl(value *big.Int) string {
	return new(big.Rat).SetFrac(value, big.NewInt(1000000)).FloatString(6)
}
func (a SQLIntervalAuthority) Intervals(ctx context.Context, start, end time.Time) ([]PnLInterval, error) {
	if a.DB == nil || a.Attribution == nil {
		return nil, ErrUnavailable
	}
	if start.IsZero() || !end.After(start) || start.Location() != time.UTC || end.Location() != time.UTC || start != start.Truncate(time.Hour) || end != end.Truncate(time.Hour) || end.Sub(start) > 1000*time.Hour || !address.MatchString(a.QuoteTokenAddress) {
		return nil, ErrInvalid
	}
	rows, e := a.DB.QueryContext(ctx, `WITH admitted AS(SELECT DISTINCT ON(wallet) snapshot_id,wallet,input_digest,watermark FROM pnl.inventory_l16_wallet_ranges_v3 WHERE start_at<=$1 AND end_at>=$2 ORDER BY wallet,generation DESC) SELECT r.snapshot_id::text,r.wallet,r.input_digest,r.watermark,COALESCE(jsonb_agg(i.payload ORDER BY i.bucket) FILTER(WHERE i.bucket IS NOT NULL),'[]'::jsonb) FROM admitted r LEFT JOIN pnl.inventory_l16_wallet_intervals_v3 i ON i.snapshot_id=r.snapshot_id AND i.interval='1h' AND i.bucket>=$1 AND i.bucket<$2 GROUP BY r.snapshot_id,r.wallet,r.input_digest,r.watermark ORDER BY r.wallet LIMIT 10001`, start, end)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PnLInterval{}
	for rows.Next() {
		var v PnLInterval
		var watermark, raw []byte
		if e = rows.Scan(&v.SnapshotID, &v.Wallet, &v.ValuationRevision, &watermark, &raw); e != nil {
			return nil, e
		}
		v.AccountingWatermark = string(watermark)
		v.Start = start
		v.End = end
		v.Methodology = "canonical_interval_realized_v1"
		v.KnownBasis = true
		v.Reconciled = true
		gross, basis, net, fees := new(big.Int), new(big.Int), new(big.Int), new(big.Int)
		var buckets []struct {
			Start, End time.Time
			Gross      string  `json:"grossProceedsUsdlRaw"`
			Basis      string  `json:"knownAllocatedBasisUsdlRaw"`
			Net        *string `json:"realizedPnlUsdlRaw"`
			Count      uint64  `json:"tradeCount"`
			Complete   bool    `json:"accountingComplete"`
			Fees       []struct {
				Asset struct {
					Kind     string `json:"kind"`
					ChainID  uint64 `json:"chainId"`
					Address  string `json:"address"`
					Decimals int    `json:"decimals"`
				} `json:"asset"`
				Amount string `json:"amountRaw"`
			} `json:"fees"`
		}
		if e = json.Unmarshal(raw, &buckets); e != nil {
			return nil, e
		}
		last := start
		for _, b := range buckets {
			if b.Start.Before(start) || b.End.After(end) || !b.End.Equal(b.Start.Add(time.Hour)) || b.Start.Before(last) {
				return nil, ErrInvalid
			}
			last = b.End
			for _, part := range []struct {
				Value string
				Into  *big.Int
			}{{b.Gross, gross}, {b.Basis, basis}} {
				n, err := rawInteger(part.Value)
				if err != nil || n.Sign() < 0 {
					return nil, ErrInvalid
				}
				part.Into.Add(part.Into, n)
			}
			if b.Net == nil {
				v.KnownBasis = false
			} else {
				n, err := rawInteger(*b.Net)
				if err != nil {
					return nil, err
				}
				net.Add(net, n)
			}
			if !b.Complete {
				v.Reconciled = false
			}
			if ^uint64(0)-v.TradeCount < b.Count {
				return nil, ErrInvalid
			}
			v.TradeCount += b.Count
			for _, fee := range b.Fees {
				n, err := rawInteger(fee.Amount)
				if err != nil || n.Sign() < 0 {
					return nil, ErrInvalid
				}
				if n.Sign() == 0 {
					continue
				}
				if fee.Asset.ChainID != 125 || !strings.EqualFold(fee.Asset.Address, a.QuoteTokenAddress) || fee.Asset.Decimals != 6 {
					v.Reconciled = false
					v.QualityFlags = append(v.QualityFlags, "fee_assets_unvalued")
					continue
				}
				fees.Add(fees, n)
			}
		}
		identity, err := a.Attribution.WalletAttribution(ctx, v.Wallet)
		if err != nil {
			return nil, err
		}
		v.AccountID = identity.AccountID
		v.ProfileID = identity.ProfileID
		v.VerifiedLink = identity.Verified
		v.AttributionOptIn = identity.OptedIn
		v.GrossProceeds = usdl(gross)
		v.CapitalBasis = usdl(basis)
		v.NetPnL = usdl(net)
		v.Fees = usdl(fees)
		if !v.KnownBasis {
			v.NetPnL = ""
			v.QualityFlags = append(v.QualityFlags, "unknown_basis")
		}
		if slices.Contains(v.QualityFlags, "fee_assets_unvalued") {
			v.Fees = ""
		}
		out = append(out, v)
		if len(out) > 10000 {
			return nil, ErrInvalid
		}
	}
	return out, rows.Err()
}
