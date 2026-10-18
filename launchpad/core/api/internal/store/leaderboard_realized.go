package store

import (
	"context"
	"math/big"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// traderFill is one committed fill seen from the wallet whose inventory it
// moves: the recipient of a buy, the payer of a sell.
type traderFill struct {
	Wallet, Pool, Side   string
	TokenRaw, PaidRaw    string
	USDLRaw              string
	Timestamp            int64
	AttributionAvailable bool
}

// traderRealizedRows loads, for every wallet/pool pair with a sell inside
// (from, windowEnd], that pair's whole fill history up to windowEnd in
// canonical chain order. Earlier buys are needed for the cost basis of the
// in-window disposals.
func traderRealizedRows(ctx context.Context, tx pgx.Tx, commits, projectionVersion string, from, windowEnd int64) ([]traderFill, error) {
	rows, err := tx.Query(ctx, `WITH f AS (SELECT CASE WHEN fill->>'side'='buy' THEN COALESCE(fill->'attribution'->>'recipient',fill->'attribution'->>'trader',fill->'attribution'->>'transactionSender') ELSE COALESCE(fill->'attribution'->>'payer',fill->'attribution'->>'trader',fill->'attribution'->>'transactionSender') END AS wallet,pool,fill->>'side' AS side,fill->>'tokenAmountRaw' AS token,fill->>'paidInputRaw' AS paid,fill->>'usdlAmountRaw' AS usdl,(fill->>'blockTimestampSeconds')::bigint AS ts,(fill->'position'->>'height')::numeric AS h,(fill->'position'->>'transactionIndex')::numeric AS ti,(fill->'position'->>'logIndex')::numeric AS li,fill->'attribution'->>'quality' <> 'unavailable' AS available FROM `+commits+` WHERE projection_version=$1 AND (fill->>'blockTimestampSeconds')::bigint <= $3) SELECT wallet,pool,side,token,paid,usdl,ts,available FROM f WHERE (wallet,pool) IN (SELECT wallet,pool FROM f WHERE side='sell' AND ts > $2) ORDER BY wallet,pool,h,ti,li`, projectionVersion, from, windowEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []traderFill{}
	for rows.Next() {
		var f traderFill
		if err = rows.Scan(&f.Wallet, &f.Pool, &f.Side, &f.TokenRaw, &f.PaidRaw, &f.USDLRaw, &f.Timestamp, &f.AttributionAvailable); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// RealizedTraderPnL ranks wallets by realised PnL on disposals inside
// (from, windowEnd]. fills must be grouped by wallet and pool and ordered by
// chain position inside each group.
//
// Per wallet and pool the known inventory K and its capital basis C follow the
// committed fills: a buy adds tokenAmountRaw to K and paidInputRaw (USDL paid
// including the fee) to C. A sell of q tokens for usdlAmountRaw proceeds P
// disposes basis c = floor(C*q/K) (c = C when q = K, proportional_floor_v1)
// and realises P - c. A sell larger than K moves tokens whose cost basis the
// fills do not contain (transferred in); its profit cannot be known, so a
// wallet with such a disposal inside the window is left off the ranking and
// counted in the returned unknown-basis total instead.
func RealizedTraderPnL(fills []traderFill, from, windowEnd int64) ([]LeaderboardEntry, int, error) {
	type state struct{ known, cost *big.Int }
	type wallet struct {
		realized              *big.Int
		disposals             int
		unknownBasis, partial bool
	}
	positions := map[string]*state{}
	wallets := map[string]*wallet{}
	order := []string{}
	for _, f := range fills {
		address, err := Address(f.Wallet)
		if err != nil || (f.Side != "buy" && f.Side != "sell") {
			return nil, 0, ErrDataIntegrity
		}
		token, okToken := new(big.Int).SetString(f.TokenRaw, 10)
		paid, okPaid := new(big.Int).SetString(f.PaidRaw, 10)
		usdl, okUSDL := new(big.Int).SetString(f.USDLRaw, 10)
		if !okToken || !okPaid || !okUSDL || token.Sign() < 0 || paid.Sign() < 0 || usdl.Sign() < 0 {
			return nil, 0, ErrDataIntegrity
		}
		key := address + "|" + f.Pool
		p := positions[key]
		if p == nil {
			p = &state{new(big.Int), new(big.Int)}
			positions[key] = p
		}
		if f.Side == "buy" {
			p.known.Add(p.known, token)
			p.cost.Add(p.cost, paid)
			continue
		}
		inWindow := f.Timestamp > from && f.Timestamp <= windowEnd
		w := wallets[address]
		if inWindow && w == nil {
			w = &wallet{realized: new(big.Int)}
			wallets[address] = w
			order = append(order, address)
		}
		if token.Cmp(p.known) > 0 {
			// Disposal beyond fill-known inventory: basis unknown.
			if inWindow {
				w.unknownBasis = true
			}
			p.known.SetInt64(0)
			p.cost.SetInt64(0)
			continue
		}
		basis := new(big.Int).Set(p.cost)
		if token.Cmp(p.known) != 0 {
			basis.Mul(p.cost, token)
			basis.Quo(basis, p.known)
		}
		p.known.Sub(p.known, token)
		p.cost.Sub(p.cost, basis)
		if inWindow {
			w.realized.Add(w.realized, new(big.Int).Sub(usdl, basis))
			w.disposals++
			if !f.AttributionAvailable {
				w.partial = true
			}
		}
	}
	entries := []LeaderboardEntry{}
	excluded := 0
	for _, address := range order {
		w := wallets[address]
		if w.unknownBasis {
			excluded++
			continue
		}
		if w.disposals == 0 {
			continue
		}
		entry := LeaderboardEntry{Wallet: address, MetricRaw: new(big.Int).Abs(w.realized).String(), MetricSigned: w.realized.String(), Quality: "complete", signed: w.realized}
		if w.partial {
			entry.Quality = "partial"
		}
		if len(entry.MetricRaw) > 156 {
			return nil, 0, ErrDataIntegrity
		}
		entries = append(entries, entry)
	}
	return entries, excluded, nil
}

func realizedDigestSuffix(excluded int) string {
	return "|unknown-basis:" + strconv.Itoa(excluded)
}
