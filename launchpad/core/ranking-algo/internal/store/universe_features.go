package store

import (
	"errors"
	"math/big"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
)

// MetricsVersion names the derivation of the per-candidate market features
// below. Every value is computed from committed canonical fills (stats
// accounting_market_commits_v2) at or below the frozen cutoff height; nothing
// is read from a cache, a price provider or a legacy stats table.
const MetricsVersion = "ranking-metrics-v3"

// Gaps that the canonical fill projection cannot close. They stay listed on
// every candidate so a reader can tell a derived value from an observed one.
const (
	// Pause state lives in pool storage, not in fills; ranking does not treat
	// an unknown pause flag as paused.
	gapPause = "canonical-pause-unavailable"
	// Holder count is the number of wallets with a positive net fill position;
	// plain wallet-to-wallet transfers are not part of the fill projection.
	gapHolderTransfers = "holder-transfers-unobserved"
)

// universeRow is one canonical pool with its fill aggregates, exactly as the
// universe query returns them (raw integer strings, no rounding).
type universeRow struct {
	pool                          string
	creationHeight                uint64
	transactionIndex, logIndex    uint32
	created                       int64
	day, hour, five               string
	buys, sells, actors           int
	self                          string
	boughtTokens, soldTokens      string
	reserveIn, reserveOut         string
	holders                       int
	lastUSDL, lastToken           *string
	lastDecimals                  *int
	referenceUSDL, referenceToken *string
	referenceDecimals             *int
}

var errFeatureIntegrity = errors.New("ranking feature integrity")

func rawInteger(raw string) (*big.Int, error) {
	if len(raw) > 156 {
		return nil, errFeatureIntegrity
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok || n.String() != raw {
		return nil, errFeatureIntegrity
	}
	return n, nil
}

// executionPrice is the USDL price of one whole token implied by a fill:
// (usdlRaw / 10^6) / (tokenRaw / 10^decimals). A fill without both legs or with
// an out-of-range decimals value carries no price.
func executionPrice(usdl, token *string, decimals *int) (*big.Rat, error) {
	if usdl == nil || token == nil || decimals == nil {
		return nil, nil
	}
	if *decimals < 0 || *decimals > 77 {
		return nil, errFeatureIntegrity
	}
	u, err := rawInteger(*usdl)
	if err != nil {
		return nil, err
	}
	t, err := rawInteger(*token)
	if err != nil {
		return nil, err
	}
	if u.Sign() < 0 || t.Sign() < 0 {
		return nil, errFeatureIntegrity
	}
	if u.Sign() == 0 || t.Sign() == 0 {
		return nil, nil
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(*decimals)), nil)
	num := new(big.Int).Mul(u, scale)
	den := new(big.Int).Mul(t, big.NewInt(1000000))
	return new(big.Rat).SetFrac(num, den), nil
}

// candidate turns a universe row into a scored-ready ranking candidate.
//
//   - price: execution price of the latest fill at or below the cutoff;
//   - priceChange24h: percent change from the latest fill at or before the
//     window start (or, for pools first traded inside the window, the first
//     fill in the window) to the latest fill;
//   - circulating supply: tokens bought from the pool minus tokens sold back,
//     over the pool's whole fill history (complete since creation because the
//     creation and every later block are inside the coherent indexed range);
//   - marketCap: price x circulating supply;
//   - depth: USDL held by the pool from trading, buy input net of the
//     distributed fee minus sell output;
//   - activity: raw 24h volume minus evidenced round-trip self volume (a
//     wallet's matched buy and sell volume inside the window).
func (row universeRow) candidate(cutoff int64) (ranker.Candidate, error) {
	c := ranker.Candidate{CreationHeight: row.creationHeight, TransactionIndex: row.transactionIndex, LogIndex: row.logIndex}
	c.Pool.PoolAddress = row.pool
	c.Pool.BuyCount24h, c.Pool.SellCount24h, c.Pool.UniqueTraders24h = row.buys, row.sells, row.actors
	var err error
	if c.Pool.Volume24h, err = rawUSDLDecimal(row.day); err != nil {
		return c, err
	}
	if c.Pool.Volume1h, err = rawUSDLDecimal(row.hour); err != nil {
		return c, err
	}
	if c.Pool.Volume5m, err = rawUSDLDecimal(row.five); err != nil {
		return c, err
	}
	self, err := rawUSDLDecimal(row.self)
	if err != nil {
		return c, err
	}
	bought, err := rawInteger(row.boughtTokens)
	if err != nil {
		return c, err
	}
	sold, err := rawInteger(row.soldTokens)
	if err != nil {
		return c, err
	}
	reserveIn, err := rawInteger(row.reserveIn)
	if err != nil {
		return c, err
	}
	reserveOut, err := rawInteger(row.reserveOut)
	if err != nil {
		return c, err
	}
	if bought.Sign() < 0 || sold.Sign() < 0 || reserveIn.Sign() < 0 || reserveOut.Sign() < 0 || row.holders < 0 || row.buys < 0 || row.sells < 0 || row.actors < 0 {
		return c, errFeatureIntegrity
	}
	circulating := new(big.Int).Sub(bought, sold)
	reserve := new(big.Int).Sub(reserveIn, reserveOut)
	// A pool can never pay out more USDL or take back more tokens than its
	// fills put in; a negative balance means the history is not the pool's.
	if circulating.Sign() < 0 || reserve.Sign() < 0 {
		return c, errFeatureIntegrity
	}
	last, err := executionPrice(row.lastUSDL, row.lastToken, row.lastDecimals)
	if err != nil {
		return c, err
	}
	reference, err := executionPrice(row.referenceUSDL, row.referenceToken, row.referenceDecimals)
	if err != nil {
		return c, err
	}
	metrics := &ranker.NullableMetrics{Version: MetricsVersion, Unavailable: []string{gapPause, gapHolderTransfers}}
	c.Pool.MarketCap, c.Pool.PriceChange24h = "0", "0"
	if last != nil {
		if row.lastDecimals == nil {
			return c, errFeatureIntegrity
		}
		supply := new(big.Rat).SetFrac(circulating, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(*row.lastDecimals)), nil))
		c.Pool.MarketCap = new(big.Rat).Mul(last, supply).FloatString(6)
		cap := c.Pool.MarketCap
		metrics.MarketCap = &cap
		c.SupplyVerified = true
		if reference != nil {
			change := new(big.Rat).Sub(last, reference)
			change.Quo(change, reference)
			change.Mul(change, big.NewRat(100, 1))
			c.Pool.PriceChange24h = change.FloatString(6)
			value := c.Pool.PriceChange24h
			metrics.PriceChange24h = &value
		}
	}
	c.Depth = new(big.Rat).SetFrac(reserve, big.NewInt(1000000)).FloatString(6)
	depth := c.Depth
	metrics.Depth = &depth
	c.Pool.HolderCount = row.holders
	holders := row.holders
	metrics.HolderCount = &holders
	c.Activity, err = ranker.AdjustActivity(c.Pool.Volume24h, self, true, false, cutoff-86400, cutoff)
	if err != nil {
		return c, err
	}
	adjusted := c.Activity.AdjustedVolume
	metrics.AdjustedVolume = &adjusted
	c.NullableMetrics = metrics
	return c, nil
}

// universeQuerySQL selects every canonical pool created at or below the cutoff
// height with its fill aggregates. $1 projection version, $2 cutoff height,
// $3 cutoff time (unix seconds). Fills above the cutoff height or after the
// cutoff time never contribute.
func universeQuerySQL(commits, creations string) string {
	return `WITH facts AS (SELECT c.pool,c.fill->>'side' side,(c.fill->>'usdlAmountRaw')::numeric usdl,(c.fill->>'tokenAmountRaw')::numeric token,(c.fill->>'paidInputRaw')::numeric paid,(c.fill->>'feeRaw')::numeric fee,(c.fill->'token'->>'decimals')::int decimals,(c.fill->>'blockTimestampSeconds')::bigint ts,(c.fill->'position'->>'height')::numeric h,(c.fill->'position'->>'transactionIndex')::numeric ti,(c.fill->'position'->>'logIndex')::numeric li,c.fill->'attribution'->>'transactionSender' sender,COALESCE(c.fill->'attribution'->>'trader',c.fill->'attribution'->>'transactionSender') trader,CASE WHEN c.fill->>'side'='buy' THEN COALESCE(c.fill->'attribution'->>'recipient',c.fill->'attribution'->>'trader',c.fill->'attribution'->>'transactionSender') ELSE COALESCE(c.fill->'attribution'->>'payer',c.fill->'attribution'->>'trader',c.fill->'attribution'->>'transactionSender') END holder FROM ` + commits + ` c WHERE c.projection_version=$1 AND (c.fill->'position'->>'height')::numeric<=$2 AND (c.fill->>'blockTimestampSeconds')::bigint<=$3),
windows AS (SELECT pool,SUM(usdl)::text AS day,COALESCE(SUM(usdl) FILTER(WHERE ts>$3-3600),0)::text AS hour,COALESCE(SUM(usdl) FILTER(WHERE ts>$3-300),0)::text AS five,COUNT(*) FILTER(WHERE side='buy') buys,COUNT(*) FILTER(WHERE side='sell') sells,COUNT(DISTINCT sender) actors FROM facts WHERE ts>$3-86400 GROUP BY pool),
wallets AS (SELECT pool,trader,COALESCE(SUM(usdl) FILTER(WHERE side='buy'),0) b,COALESCE(SUM(usdl) FILTER(WHERE side='sell'),0) s FROM facts WHERE ts>$3-86400 GROUP BY pool,trader),
selfv AS (SELECT pool,SUM(2*LEAST(b,s))::text AS self FROM wallets GROUP BY pool),
totals AS (SELECT pool,COALESCE(SUM(token) FILTER(WHERE side='buy'),0)::text bought,COALESCE(SUM(token) FILTER(WHERE side='sell'),0)::text sold,COALESCE(SUM(paid-fee) FILTER(WHERE side='buy'),0)::text reserve_in,COALESCE(SUM(usdl) FILTER(WHERE side='sell'),0)::text reserve_out FROM facts GROUP BY pool),
positions AS (SELECT pool,holder,SUM(CASE WHEN side='buy' THEN token ELSE -token END) net FROM facts GROUP BY pool,holder),
holders AS (SELECT pool,COUNT(*) FILTER(WHERE net>0) holders FROM positions GROUP BY pool),
last AS (SELECT DISTINCT ON(pool) pool,usdl::text usdl,token::text token,decimals FROM facts ORDER BY pool,h DESC,ti DESC,li DESC),
reference AS (SELECT DISTINCT ON(pool) pool,usdl::text usdl,token::text token,decimals FROM facts ORDER BY pool,(ts<=$3-86400) DESC,CASE WHEN ts<=$3-86400 THEN -h ELSE h END,CASE WHEN ts<=$3-86400 THEN -ti ELSE ti END,CASE WHEN ts<=$3-86400 THEN -li ELSE li END)
SELECT p.pool,p.height,p.transaction_index,p.log_index,p.block_timestamp,COALESCE(w.day,'0'),COALESCE(w.hour,'0'),COALESCE(w.five,'0'),COALESCE(w.buys,0),COALESCE(w.sells,0),COALESCE(w.actors,0),COALESCE(sv.self,'0'),COALESCE(t.bought,'0'),COALESCE(t.sold,'0'),COALESCE(t.reserve_in,'0'),COALESCE(t.reserve_out,'0'),COALESCE(hd.holders,0),l.usdl,l.token,l.decimals,rf.usdl,rf.token,rf.decimals FROM ` + creations + ` p LEFT JOIN windows w ON w.pool=p.pool LEFT JOIN selfv sv ON sv.pool=p.pool LEFT JOIN totals t ON t.pool=p.pool LEFT JOIN holders hd ON hd.pool=p.pool LEFT JOIN last l ON l.pool=p.pool LEFT JOIN reference rf ON rf.pool=p.pool WHERE p.height<=$2 ORDER BY p.pool LIMIT 1000001`
}
