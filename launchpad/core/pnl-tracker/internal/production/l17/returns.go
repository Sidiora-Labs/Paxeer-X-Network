package l17

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"math/big"
	"time"
)

type ReturnPeriod struct {
	Start                      time.Time `json:"start"`
	End                        time.Time `json:"end"`
	OpeningEquityRaw           string    `json:"openingEquityRaw"`
	ClosingEquityBeforeFlowRaw string    `json:"closingEquityBeforeFlowRaw"`
	ExternalFlowAtEndRaw       string    `json:"externalFlowAtEndRaw"`
	Complete                   bool      `json:"complete"`
}

type HistoricalReturn struct {
	Method  string `json:"method"`
	Return  *Ratio `json:"return,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Periods int    `json:"periods"`
}

func TimeWeightedReturn(periods []ReturnPeriod) (HistoricalReturn, error) {
	out := HistoricalReturn{Method: "unavailable", Periods: len(periods)}
	if len(periods) == 0 || len(periods) > 10000 {
		return out, ErrInvalid
	}
	factor := big.NewRat(1, 1)
	var previousEnd time.Time
	var previousAfterFlow *big.Int
	for i, p := range periods {
		if p.Start.IsZero() || !p.Start.Before(p.End) || (i > 0 && !p.Start.Equal(previousEnd)) {
			return out, ErrInvalid
		}
		open, e := number(p.OpeningEquityRaw, false, 156)
		if e != nil {
			return out, e
		}
		close, e := number(p.ClosingEquityBeforeFlowRaw, false, 156)
		if e != nil {
			return out, e
		}
		flow, e := number(p.ExternalFlowAtEndRaw, true, 156)
		if e != nil {
			return out, e
		}
		if !p.Complete {
			out.Reason = "incomplete_cash_flow_or_valuation_history"
		}
		if previousAfterFlow != nil && open.Cmp(previousAfterFlow) != 0 {
			return out, ErrInvalid
		}
		after := new(big.Int).Add(close, flow)
		if after.Sign() < 0 {
			return out, ErrInvalid
		}
		if open.Sign() == 0 {
			if out.Reason == "" {
				out.Reason = "zero_opening_equity"
			}
		} else if out.Reason == "" {
			factor.Mul(factor, new(big.Rat).SetFrac(close, open))
		}
		if factor.Num().BitLen() > 65536 || factor.Denom().BitLen() > 65536 {
			return out, ErrInvalid
		}
		previousAfterFlow = after
		previousEnd = p.End
	}
	if out.Reason != "" {
		return out, nil
	}
	factor.Sub(factor, big.NewRat(1, 1))
	out.Method = "time_weighted_boundary_cash_flows_v1"
	out.Return = &Ratio{Numerator: factor.Num().String(), Denominator: factor.Denom().String()}
	return out, nil
}

func (r Repository) HistoricalReturn(ctx context.Context, wallet, verified, snapshot string, from, to time.Time) (HistoricalReturn, error) {
	unavailable := HistoricalReturn{Method: "unavailable", Reason: "incomplete_cash_flow_or_valuation_history"}
	if e := RequireOwner(wallet, verified); e != nil {
		return unavailable, e
	}
	wallet, _ = Wallet(wallet)
	if !validUUID(snapshot) {
		return unavailable, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	var raw []byte
	e := r.Pool.QueryRow(ctx, "SELECT m.payload FROM portfolio_snapshot_history_metadata m JOIN portfolio_snapshots s ON s.id=m.snapshot_id WHERE m.snapshot_id=$1 AND s.wallet=$2", snapshot, wallet).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return unavailable, nil
	}
	if e != nil {
		return unavailable, e
	}
	var m SnapshotHistoryMetadata
	if e = json.Unmarshal(raw, &m); e != nil {
		return unavailable, e
	}
	if !m.Start.Equal(from) || !m.End.Equal(to) || len(m.Periods) == 0 {
		return unavailable, nil
	}
	return TimeWeightedReturn(m.Periods)
}
