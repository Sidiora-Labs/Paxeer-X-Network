package devreads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlmath"
	inventory "github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l16"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
)

// maxReplayTrades bounds the request-time replay of one wallet's trades.
const maxReplayTrades = 50000

// intervalRelease identifies the development interval method (the series'
// releaseDigest): the dev fold replayed with pnlmath remaining-cost basis.
var intervalRelease = digest("pnl-development-interval-realized-v1:pnlmath.Fold")

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type replayTrade struct {
	id, pool, token string
	isBuy           bool
	usdl, amount    string
	block, ts       int64
}

// loadTrades reads the wallet's folded trades in fold order (block, then log
// index from the txHash-logIndex id).
func loadTrades(ctx context.Context, pool *pgxpool.Pool, wallet string) ([]replayTrade, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.id, t.pool_address, lower(COALESCE(NULLIF(t.token_address, ''), ip.token_address, '')),
		       t.is_buy, t.usdl_amount, t.token_amount, t.block_number, t.block_timestamp
		FROM pnl.user_trades t
		LEFT JOIN indexer.pools ip ON ip.pool_address = t.pool_address
		WHERE t.user_address = $1
		ORDER BY t.block_number,
		         CASE WHEN t.id ~ '-[0-9]{1,9}$' THEN substring(t.id from '-([0-9]{1,9})$')::bigint ELSE 0 END,
		         t.id
		LIMIT $2`, wallet, maxReplayTrades+1)
	if err != nil {
		return nil, fmt.Errorf("devreads: trades: %w", err)
	}
	defer rows.Close()
	out := []replayTrade{}
	for rows.Next() {
		var t replayTrade
		if err := rows.Scan(&t.id, &t.pool, &t.token, &t.isBuy, &t.usdl, &t.amount, &t.block, &t.ts); err != nil {
			return nil, fmt.Errorf("devreads: scan trade: %w", err)
		}
		out = append(out, t)
		if len(out) > maxReplayTrades {
			return nil, l17.ErrBusy
		}
	}
	return out, rows.Err()
}

// replay re-folds trades through pnlmath.Fold (the same math the dev consumer
// used), exposing per-trade realized deltas and the running inventory.
type replay struct {
	positions map[string]pnlmath.Position
	tokens    map[string]string
	broken    map[string]bool
}

func newReplay() *replay {
	return &replay{positions: map[string]pnlmath.Position{}, tokens: map[string]string{}, broken: map[string]bool{}}
}

// apply folds t. For a sale it returns the realized gain and the basis it
// consumed; known is false when the pool's basis can no longer be replayed.
func (rp *replay) apply(t replayTrade) (gain, basis *big.Int, known bool) {
	if t.token != "" {
		rp.tokens[t.pool] = t.token
	}
	if rp.broken[t.pool] {
		return nil, nil, false
	}
	prior := rp.positions[t.pool]
	next, err := pnlmath.Fold(prior, pnlmath.Trade{IsBuy: t.isBuy, UsdlAmount: t.usdl, TokenAmount: t.amount, Ts: t.ts})
	if err != nil {
		rp.broken[t.pool] = true
		delete(rp.positions, t.pool)
		return nil, nil, false
	}
	rp.positions[t.pool] = next
	if t.isBuy {
		return new(big.Int), new(big.Int), true
	}
	before, _ := new(big.Int).SetString(orZero(prior.RealizedPnlUsdl), 10)
	after, _ := new(big.Int).SetString(orZero(next.RealizedPnlUsdl), 10)
	usdl, _ := new(big.Int).SetString(t.usdl, 10)
	gain = new(big.Int).Sub(after, before)
	return gain, new(big.Int).Sub(usdl, gain), true
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// states returns the current per-token inventory of replayable pools.
func (rp *replay) states() map[string]*tokenState {
	out := map[string]*tokenState{}
	for pool, p := range rp.positions {
		token := rp.tokens[pool]
		if _, e := l17.Wallet(token); e != nil {
			continue
		}
		spent, _ := new(big.Int).SetString(orZero(p.TotalUsdlSpent), 10)
		received, _ := new(big.Int).SetString(orZero(p.TotalUsdlReceived), 10)
		realized, _ := new(big.Int).SetString(orZero(p.RealizedPnlUsdl), 10)
		holdings, _ := new(big.Int).SetString(orZero(p.CurrentHoldings), 10)
		s := out[token]
		if s == nil {
			s = &tokenState{known: new(big.Int), cost: new(big.Int), realized: new(big.Int)}
			out[token] = s
		}
		s.known.Add(s.known, holdings)
		s.cost.Add(s.cost, new(big.Int).Sub(spent, new(big.Int).Sub(received, realized)))
		s.realized.Add(s.realized, realized)
	}
	return out
}

// intervalHistory serves ConnectedL16IntervalSeries (/v3/me/pnl/history) with
// production's query rules: RFC 3339 UTC bounds aligned to the 1h/1d step,
// limit 1..1000 (default 50) covering the whole range.
func (h *handlers) intervalHistory(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.principal(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	q, err := strictQuery(r, "from", "to", "interval", "limit")
	if err != nil {
		h.fail(w, err)
		return
	}
	from, e1 := time.Parse(time.RFC3339, q.Get("from"))
	to, e2 := time.Parse(time.RFC3339, q.Get("to"))
	if e1 != nil || e2 != nil {
		h.fail(w, l17.ErrInvalid)
		return
	}
	_, fromOffset := from.Zone()
	_, toOffset := to.Zone()
	interval := q.Get("interval")
	if interval == "" {
		interval = "1d"
	}
	limit := 50
	if q.Has("limit") {
		if limit, err = strconv.Atoi(q.Get("limit")); err != nil {
			h.fail(w, l17.ErrInvalid)
			return
		}
	}
	step, ok := map[string]time.Duration{"1h": time.Hour, "1d": 24 * time.Hour}[interval]
	from, to = from.UTC(), to.UTC()
	if !ok || fromOffset != 0 || toOffset != 0 || from.IsZero() || !from.Before(to) || !from.Equal(from.Truncate(step)) || !to.Equal(to.Truncate(step)) || limit < 1 || limit > 1000 || to.Sub(from) > time.Duration(limit)*step {
		h.fail(w, l17.ErrInvalid)
		return
	}
	snap, err := h.Reader.Snapshot(r.Context(), wallet)
	if err != nil {
		h.fail(w, err)
		return
	}
	trades, err := loadTrades(r.Context(), h.Reader.Pool, wallet)
	if err != nil {
		h.fail(w, err)
		return
	}
	series := inventory.IntervalSeries{Version: 3, Algorithm: l17.Algorithm, Method: "canonical_interval_realized_v1", SnapshotID: snap.ID, Wallet: wallet, ReleaseDigest: intervalRelease, Watermark: snap.Watermark, Entries: []inventory.IntervalPnL{}}
	type bucket struct {
		gross, basis, net, unknown *big.Int
		trades, sales              int
	}
	buckets := map[int64]*bucket{}
	input := sha256.New()
	rp := newReplay()
	for _, t := range trades {
		fmt.Fprintf(input, "%s|%s|%t|%s|%s|%d|%d\n", t.id, t.pool, t.isBuy, t.usdl, t.amount, t.block, t.ts)
		gain, basis, known := rp.apply(t)
		at := time.Unix(t.ts, 0).UTC()
		if at.Before(from) || !at.Before(to) {
			continue
		}
		key := at.Truncate(step).Unix()
		b := buckets[key]
		if b == nil {
			b = &bucket{gross: new(big.Int), basis: new(big.Int), net: new(big.Int), unknown: new(big.Int)}
			buckets[key] = b
		}
		b.trades++
		if t.isBuy {
			continue
		}
		b.sales++
		usdl, _ := new(big.Int).SetString(t.usdl, 10)
		if !known {
			b.unknown.Add(b.unknown, usdl)
			continue
		}
		b.gross.Add(b.gross, usdl)
		b.basis.Add(b.basis, basis)
		b.net.Add(b.net, gain)
	}
	series.InputDigest = hex.EncodeToString(input.Sum(nil))
	for start := from; start.Before(to) && len(series.Entries) < limit; start = start.Add(step) {
		zero := "0"
		item := inventory.IntervalPnL{Start: start, End: start.Add(step), GrossProceedsUsdlRaw: "0", KnownAllocatedBasisUsdlRaw: "0", KnownNetGainUsdlRaw: "0", UnknownProceedsUsdlRaw: "0", RealizedPnlUsdlRaw: &zero, Fees: []inventory.IntervalFee{}, GasPaxRaw: "0", AccountingComplete: true}
		if b := buckets[start.Unix()]; b != nil {
			item.TradeCount, item.SaleCount = b.trades, b.sales
			item.GrossProceedsUsdlRaw, item.KnownAllocatedBasisUsdlRaw, item.KnownNetGainUsdlRaw, item.UnknownProceedsUsdlRaw = b.gross.String(), b.basis.String(), b.net.String(), b.unknown.String()
			net := b.net.String()
			item.RealizedPnlUsdlRaw = &net
			if b.unknown.Sign() != 0 {
				item.RealizedPnlUsdlRaw = nil
			}
			// The dev fold records swap legs only: the fee asset and the gas
			// paid are not captured, so traded buckets are not complete.
			item.Fees = nil
			item.AccountingComplete = false
		}
		series.Entries = append(series.Entries, item)
	}
	h.write(w, "ConnectedL16IntervalSeries", series)
}

type historyEntry struct {
	Time      string        `json:"time"`
	Watermark l17.Watermark `json:"watermark"`
	Priced    string        `json:"pricedSubtotalUsdlRaw"`
	Complete  *string       `json:"completeValueUsdlRaw"`
	Realized  string        `json:"knownRealizedSubtotalUsdlRaw"`
	Basis     string        `json:"basisQuality"`
	Unpriced  int           `json:"unpricedAssetCount"`
}

// valueHistory serves PnlHistoryResponse (/v2/me/portfolio/history): one entry
// per bucket in which the wallet traded, holding the portfolio summary right
// after its last trade in that bucket, aggregated by l17.Aggregate. Entries are
// computed by the current projection, so each carries its watermark.
func (h *handlers) valueHistory(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.principal(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	q, err := strictQuery(r, "from", "to", "interval", "limit")
	if err != nil {
		h.fail(w, err)
		return
	}
	from, e1 := time.Parse(observedLayout, q.Get("from"))
	to, e2 := time.Parse(observedLayout, q.Get("to"))
	step, ok := map[string]time.Duration{"1h": time.Hour, "1d": 24 * time.Hour}[q.Get("interval")]
	limit := 50
	if q.Get("limit") != "" {
		if limit, err = strconv.Atoi(q.Get("limit")); err != nil {
			h.fail(w, l17.ErrInvalid)
			return
		}
	}
	if e1 != nil || e2 != nil || !ok || from.IsZero() || !from.Before(to) || limit < 1 || limit > 1000 || to.Sub(from) > time.Duration(limit)*step {
		h.fail(w, l17.ErrInvalid)
		return
	}
	snap, err := h.Reader.Snapshot(r.Context(), wallet)
	if err != nil {
		h.fail(w, err)
		return
	}
	trades, err := loadTrades(r.Context(), h.Reader.Pool, wallet)
	if err != nil {
		h.fail(w, err)
		return
	}
	type point struct {
		at     time.Time
		states map[string]*tokenState
		broken bool
	}
	points := map[int64]*point{}
	rp := newReplay()
	for _, t := range trades {
		rp.apply(t)
		at := time.Unix(t.ts, 0).UTC()
		if at.Before(from) || at.After(to) {
			continue
		}
		key := from.Add(at.Sub(from) / step * step).Unix()
		points[key] = &point{at: at, states: rp.states(), broken: len(rp.broken) > 0}
	}
	keys := make([]int64, 0, len(points))
	for k := range points {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	items := []historyEntry{}
	for _, k := range keys {
		if len(items) >= limit {
			break
		}
		p := points[k]
		tokens := make([]string, 0, len(p.states))
		for token := range p.states {
			tokens = append(tokens, token)
		}
		sort.Strings(tokens)
		positions := make([]l17.Position, 0, len(tokens))
		for _, token := range tokens {
			s := p.states[token]
			state := inventory.Empty()
			state.Known, state.Cost, state.Realized = s.known.String(), s.cost.String(), s.realized.String()
			position, err := inventory.ProjectPosition(wallet, token, state, snap.Watermark, nil, "unpriced")
			if err != nil {
				h.fail(w, err)
				return
			}
			positions = append(positions, position)
		}
		summary, err := l17.Aggregate(wallet, snap.ID, snap.Watermark, positions)
		if err != nil {
			h.fail(w, err)
			return
		}
		if p.broken {
			summary.BasisQuality = "partial"
			if len(positions) == 0 {
				summary.BasisQuality = "unknown"
			}
			summary.CompleteValueUsdlRaw = nil
		}
		items = append(items, historyEntry{Time: p.at.Format(observedLayout), Watermark: snap.Watermark, Priced: summary.PricedSubtotalUsdlRaw, Complete: summary.CompleteValueUsdlRaw, Realized: summary.KnownRealizedSubtotalUsdlRaw, Basis: summary.BasisQuality, Unpriced: summary.UnpricedAssetCount})
	}
	revision := snap.Revision
	watermark := snap.Watermark
	h.write(w, "PnlHistoryResponse", struct {
		RequestID string `json:"requestId"`
		Data      struct {
			Items []historyEntry `json:"items"`
		} `json:"data"`
		Watermark     *l17.Watermark       `json:"watermark"`
		Revision      *string              `json:"revision"`
		Quality       string               `json:"quality"`
		NextCursor    *string              `json:"nextCursor"`
		GeneratedAt   string               `json:"generatedAt"`
		Return        l17.HistoricalReturn `json:"historicalReturn"`
		MoneyWeighted l17.HistoricalReturn `json:"moneyWeightedReturn"`
	}{
		RequestID: requestID(),
		Data: struct {
			Items []historyEntry `json:"items"`
		}{items},
		Watermark:     &watermark,
		Revision:      &revision,
		Quality:       "partial",
		GeneratedAt:   h.Now().UTC().Format(observedLayout),
		Return:        l17.HistoricalReturn{Method: "unavailable", Reason: "incomplete_cash_flow_or_valuation_history"},
		MoneyWeighted: l17.HistoricalReturn{Method: "unavailable", Reason: "approved_money_weighted_algorithm_unavailable"},
	})
}
