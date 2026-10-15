package rest

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/pnl"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
)

// SpotNode reads account state from the node at a height; *ethclient.Client
// dialled on a PERPS_RPC_URLS entry satisfies it.
type SpotNode interface {
	BlockNumber(ctx context.Context) (uint64, error)
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
	CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// SpotUsers serves the spot user routes and the spot leaderboards of
// contracts/data-services.md section 25 from schema perps and the node.
type SpotUsers struct {
	db   *sql.DB
	node SpotNode
}

// NewSpotUsers returns the spot user routes reading db and node.
func NewSpotUsers(db *sql.DB, node SpotNode) (*SpotUsers, error) {
	if db == nil {
		return nil, errors.New("rest: nil database")
	}
	if node == nil {
		return nil, errors.New("rest: nil node")
	}
	return &SpotUsers{db: db, node: node}, nil
}

// spotRoute answers one user route for the address of the path.
type spotRoute func(ctx context.Context, user common.Address, q map[string][]string) (any, error)

// spotError is a refusal answered with its HTTP status and message.
type spotError struct {
	status int
	msg    string
}

func (e *spotError) Error() string { return e.msg }

// spotWad is 10^18, the fixed-point scale of spot prices and cost basis.
var spotWad = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// balanceOfSelector is balanceOf(address).
var balanceOfSelector = []byte{0x70, 0xa0, 0x82, 0x31}

var spotLeaderboards = []string{"pnl", "volume", "win-rate"}

// Register mounts the routes at the paths the spot client writes.
func (s *SpotUsers) Register(mux *http.ServeMux) {
	for path, route := range map[string]spotRoute{
		"holdings":     s.holdings,
		"positions":    s.positions,
		"orders":       s.orders,
		"trades":       s.trades,
		"trades/stats": s.tradesStats,
		"closed-legs":  s.closedLegs,
		"pnl/summary":  s.pnlSummary,
		"pnl":          s.pnlSummary,
		"identity":     s.identity,
	} {
		mux.HandleFunc("GET /v1/users/{address}/"+path, s.serveUser(route))
	}
	mux.HandleFunc("GET /v1/leaderboards/{kind}", s.leaderboard)
}

func (s *SpotUsers) serveUser(route spotRoute) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw := r.PathValue("address")
		if len(raw) != 2+2*common.AddressLength || !common.IsHexAddress(raw) {
			writeError(w, http.StatusBadRequest, "invalid address")
			return
		}
		body, err := route(r.Context(), common.HexToAddress(raw), r.URL.Query())
		spotWrite(w, body, err)
	}
}

func spotWrite(w http.ResponseWriter, body any, err error) {
	var refusal *spotError
	switch {
	case errors.As(err, &refusal):
		writeError(w, refusal.status, refusal.msg)
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, body)
	}
}

func spotHex(a common.Address) string { return strings.ToLower(a.Hex()) }

func spotISO(at time.Time) string { return at.UTC().Format(isoMillis) }

func spotOptISO(at sql.NullTime) *string {
	if !at.Valid {
		return nil
	}
	v := spotISO(at.Time)
	return &v
}

func spotOptHash(raw []byte) *string {
	if raw == nil {
		return nil
	}
	v := common.BytesToHash(raw).Hex()
	return &v
}

func spotOptString(raw sql.NullString) *string {
	if !raw.Valid {
		return nil
	}
	return &raw.String
}

// spotInt parses a canonical decimal integer; "" is 0.
func spotInt(s string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("rest: bad integer %q", s)
	}
	return v, nil
}

// spotToken is the registry metadata of one spot token.
type spotToken struct {
	symbol   *string
	decimals *int
	stable   bool
}

const spotRegistrySQL = `SELECT token, decimals, is_stablecoin FROM perps.spot_token_registry
WHERE kind = 'token_registered' ORDER BY block, tx_index, log_index, sub`

// tokens reads the registered spot tokens: the non-stable tokens take their
// symbol by registration rank (spotTokenRank), the stablecoin at the USDL
// address is USDL.
func (s *SpotUsers) tokens(ctx context.Context) (map[common.Address]spotToken, error) {
	rows, err := s.db.QueryContext(ctx, spotRegistrySQL)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	symbols := make(map[int]string, len(spotTokenRank))
	for sym, rank := range spotTokenRank {
		symbols[rank] = sym
	}
	out := map[common.Address]spotToken{}
	rank := 0
	for rows.Next() {
		var (
			raw    []byte
			dec    sql.NullInt64
			stable sql.NullBool
		)
		if err := rows.Scan(&raw, &dec, &stable); err != nil {
			return nil, err
		}
		token := common.BytesToAddress(raw)
		meta := spotToken{stable: stable.Bool}
		if dec.Valid {
			d := int(dec.Int64)
			meta.decimals = &d
		}
		switch {
		case meta.stable && token == store.USDL:
			sym := "USDL"
			meta.symbol = &sym
		case !meta.stable:
			if sym, ok := symbols[rank]; ok {
				meta.symbol = &sym
			}
			rank++
		}
		if _, seen := out[token]; !seen {
			out[token] = meta
		}
	}
	return out, rows.Err()
}

// head returns the latest height of the node.
func (s *SpotUsers) head(ctx context.Context) (*big.Int, error) {
	n, err := s.node.BlockNumber(ctx)
	if err != nil {
		return nil, &spotError{http.StatusBadGateway, "node unavailable"}
	}
	return new(big.Int).SetUint64(n), nil
}

// balanceOf reads balanceOf(account) of token at height.
func (s *SpotUsers) balanceOf(ctx context.Context, token, account common.Address, height *big.Int) (*big.Int, error) {
	data := append(slices.Clone(balanceOfSelector), common.LeftPadBytes(account.Bytes(), 32)...)
	out, err := s.node.CallContract(ctx, ethereum.CallMsg{To: &token, Data: data}, height)
	if err != nil {
		return nil, &spotError{http.StatusBadGateway, "node unavailable"}
	}
	if len(out) != 32 {
		return nil, &spotError{http.StatusBadGateway, fmt.Sprintf("balanceOf %s returned %d bytes", spotHex(token), len(out))}
	}
	return new(big.Int).SetBytes(out), nil
}

// spotTradesCTE lists the spot trades of account $1 (every account when $1 is
// NULL) as token in and out: market orders, simple swaps, native swaps with the
// native side as the PAX pointer (the first registered non-stable token) and
// executed limit, stop-limit and stop-loss orders at their execution price. A
// buy order escrows the stable amount, a sell order the token amount.
// ponytail: order fills are valued at the gross execution price; the order
// rails emit no fee event, so the net amount is not in the facts.
const spotTradesCTE = `WITH stable AS (
	SELECT token FROM perps.spot_token_registry WHERE kind = 'token_registered' AND is_stablecoin
), pax AS (
	SELECT token FROM perps.spot_token_registry WHERE kind = 'token_registered' AND NOT is_stablecoin
	ORDER BY block, tx_index, log_index, sub LIMIT 1
), raw AS (
	SELECT s.kind AS source, s.block, s.tx_index, s.log_index, s.sub, s.tx_hash, s.block_time, s.emitter, s."user" AS account,
		CASE WHEN s.is_buy THEN s.stablecoin ELSE s.token END AS token_in,
		CASE WHEN s.is_buy THEN s.token ELSE s.stablecoin END AS token_out,
		(CASE WHEN s.is_buy THEN s.stablecoin_amount ELSE s.token_amount END)::numeric AS amount_in,
		(CASE WHEN s.is_buy THEN s.token_amount ELSE s.stablecoin_amount END)::numeric AS amount_out,
		NULL::numeric AS price
	FROM perps.spot_swaps s
	WHERE s.kind = 'market_order' AND ($1::bytea IS NULL OR s."user" = $1)
	UNION ALL
	SELECT s.kind, s.block, s.tx_index, s.log_index, s.sub, s.tx_hash, s.block_time, s.emitter, s."user",
		s.token_in, s.token_out, s.amount_in::numeric, s.amount_out::numeric, s.execution_price::numeric
	FROM perps.spot_swaps s
	WHERE s.kind = 'simple_swap' AND ($1::bytea IS NULL OR s."user" = $1)
	UNION ALL
	SELECT s.kind, s.block, s.tx_index, s.log_index, s.sub, s.tx_hash, s.block_time, s.emitter, s."user",
		CASE WHEN s.native_is_input THEN p.token ELSE s.token_other END,
		CASE WHEN s.native_is_input THEN s.token_other ELSE p.token END,
		(CASE WHEN s.native_is_input THEN s.native_amount ELSE s.token_amount END)::numeric,
		(CASE WHEN s.native_is_input THEN s.token_amount ELSE s.native_amount END)::numeric,
		NULL::numeric
	FROM perps.spot_swaps s CROSS JOIN pax p
	WHERE s.kind = 'native_swap' AND ($1::bytea IS NULL OR s."user" = $1)
	UNION ALL
	SELECT e.kind, e.block, e.tx_index, e.log_index, e.sub, e.tx_hash, e.block_time, e.emitter, o.account,
		CASE WHEN o.side = 'buy' THEN o.stablecoin ELSE o.token END,
		CASE WHEN o.side = 'buy' THEN o.token ELSE o.stablecoin END,
		o.amount::numeric,
		trunc(CASE WHEN o.side = 'buy' THEN o.amount * 1000000000000000000 / e.execution_price
			ELSE o.amount * e.execution_price / 1000000000000000000 END),
		e.execution_price::numeric
	FROM perps.spot_order_events e
	JOIN perps.spot_orders o ON o.order_id = e.order_id AND e.kind = o.kind || '_executed'
	WHERE e.execution_price > 0 AND ($1::bytea IS NULL OR o.account = $1)
), trades AS (
	SELECT r.*, r.token_in IN (SELECT token FROM stable) AS in_stable, r.token_out IN (SELECT token FROM stable) AS out_stable
	FROM raw r
)
`

// spotLegsSQL reads the stablecoin legs of the trades in chain order: the
// trades with exactly one stable side and a non-zero token side.
const spotLegsSQL = spotTradesCTE + `SELECT account, CASE WHEN in_stable THEN token_out ELSE token_in END, in_stable,
	trunc(CASE WHEN in_stable THEN amount_in ELSE amount_out END)::text,
	trunc(CASE WHEN in_stable THEN amount_out ELSE amount_in END)::text,
	tx_hash, block_time
FROM trades
WHERE in_stable <> out_stable AND CASE WHEN in_stable THEN amount_out ELSE amount_in END >= 1
ORDER BY block, tx_index, log_index, sub`

// spotBook is the average-cost fold of one (account, token).
type spotBook struct {
	token       common.Address
	pos         pnl.Position
	first, last time.Time
	opened      *time.Time
}

// spotClosed is one closed leg: a sell folded against the average cost.
type spotClosed struct {
	id                    int
	token                 common.Address
	qty, entry, exit, pnl string
	pct                   *string
	opened                *time.Time
	closed                time.Time
	tx                    common.Hash
	full                  bool
}

// spotTrader is the fold of every leg of one account.
type spotTrader struct {
	books        []*spotBook
	closed       []spotClosed
	legs         int
	volume       *big.Int
	wins, losses int
}

// fold folds the legs of user, or of every account when user is nil, through
// pnl.Fold. A leg the fold refuses answers 422 with the refusal.
func (s *SpotUsers) fold(ctx context.Context, user *common.Address) (map[common.Address]*spotTrader, error) {
	var arg any
	if user != nil {
		arg = user.Bytes()
	}
	rows, err := s.db.QueryContext(ctx, spotLegsSQL, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	traders := map[common.Address]*spotTrader{}
	for rows.Next() {
		var (
			account, token, tx []byte
			buy                bool
			usdl, tok          string
			at                 time.Time
		)
		if err := rows.Scan(&account, &token, &buy, &usdl, &tok, &tx, &at); err != nil {
			return nil, err
		}
		a := common.BytesToAddress(account)
		tr := traders[a]
		if tr == nil {
			tr = &spotTrader{volume: new(big.Int)}
			traders[a] = tr
		}
		if err := tr.apply(common.BytesToAddress(token), buy, usdl, tok, common.BytesToHash(tx), at); err != nil {
			return nil, err
		}
	}
	return traders, rows.Err()
}

func (tr *spotTrader) apply(token common.Address, buy bool, usdl, tok string, tx common.Hash, at time.Time) error {
	var b *spotBook
	for _, x := range tr.books {
		if x.token == token {
			b = x
			break
		}
	}
	if b == nil {
		b = &spotBook{token: token, first: at}
		tr.books = append(tr.books, b)
	}
	before := b.pos
	after, err := pnl.Fold(before, pnl.Trade{IsBuy: buy, UsdlAmount: usdl, TokenAmount: tok, Ts: at.Unix()})
	if err != nil {
		return &spotError{http.StatusUnprocessableEntity, err.Error()}
	}
	usdlInt, err := spotInt(usdl)
	if err != nil {
		return err
	}
	tr.volume.Add(tr.volume, usdlInt)
	tr.legs++
	b.pos, b.last = after, at
	if buy {
		if b.opened == nil {
			opened := at
			b.opened = &opened
		}
		return nil
	}
	priorRealized, err := spotInt(before.RealizedPnlUsdl)
	if err != nil {
		return err
	}
	realized, err := spotInt(after.RealizedPnlUsdl)
	if err != nil {
		return err
	}
	tokInt, err := spotInt(tok)
	if err != nil {
		return err
	}
	delta := new(big.Int).Sub(realized, priorRealized)
	cost := new(big.Int).Sub(usdlInt, delta)
	leg := spotClosed{
		id:     len(tr.closed) + 1,
		token:  token,
		qty:    tok,
		entry:  before.AvgCostBasis,
		exit:   new(big.Int).Quo(new(big.Int).Mul(usdlInt, spotWad), tokInt).String(),
		pnl:    delta.String(),
		opened: b.opened,
		closed: at,
		tx:     tx,
		full:   after.CurrentHoldings == "0",
	}
	if cost.Sign() > 0 {
		pct := new(big.Rat).SetFrac(new(big.Int).Mul(delta, big.NewInt(100)), cost).FloatString(4)
		leg.pct = &pct
	}
	switch delta.Sign() {
	case 1:
		tr.wins++
	case -1:
		tr.losses++
	}
	tr.closed = append(tr.closed, leg)
	if leg.full {
		b.opened = nil
	}
	return nil
}

// realized sums the realized profit of every book in stablecoin raw.
func (tr *spotTrader) realized() (*big.Int, error) {
	sum := new(big.Int)
	for _, b := range tr.books {
		v, err := spotInt(b.pos.RealizedPnlUsdl)
		if err != nil {
			return nil, err
		}
		sum.Add(sum, v)
	}
	return sum, nil
}

func (tr *spotTrader) lastTrade() *time.Time {
	var last *time.Time
	for _, b := range tr.books {
		if last == nil || b.last.After(*last) {
			at := b.last
			last = &at
		}
	}
	return last
}

type spotNativeJSON struct {
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
	Balance  string `json:"balance"`
}

type spotHoldingJSON struct {
	Token       string  `json:"token"`
	Balance     string  `json:"balance"`
	BlockNumber *string `json:"blockNumber"`
	Symbol      *string `json:"symbol"`
	Name        *string `json:"name"`
	Decimals    *int    `json:"decimals"`
	HolderCount *int    `json:"holderCount"`
	Type        string  `json:"type"`
}

type spotHoldingsBody struct {
	Address string            `json:"address"`
	Native  spotNativeJSON    `json:"native"`
	ERC20   []spotHoldingJSON `json:"erc20"`
}

// holdings reads the native balance and the balanceOf of every token of
// perps.spot_token_seen at the latest height and lists the non-zero ones.
func (s *SpotUsers) holdings(ctx context.Context, user common.Address, _ map[string][]string) (any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token FROM perps.spot_token_seen ORDER BY first_block, token`)
	if err != nil {
		return nil, err
	}
	var seen []common.Address
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		seen = append(seen, common.BytesToAddress(raw))
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	height, err := s.head(ctx)
	if err != nil {
		return nil, err
	}
	native, err := s.node.BalanceAt(ctx, user, height)
	if err != nil {
		return nil, &spotError{http.StatusBadGateway, "node unavailable"}
	}
	block := height.String()
	body := spotHoldingsBody{
		Address: spotHex(user),
		Native:  spotNativeJSON{Symbol: "PAX", Decimals: 18, Balance: native.String()},
		ERC20:   []spotHoldingJSON{},
	}
	for _, token := range seen {
		balance, err := s.balanceOf(ctx, token, user, height)
		if err != nil {
			return nil, err
		}
		if balance.Sign() == 0 {
			continue
		}
		meta := tokens[token]
		body.ERC20 = append(body.ERC20, spotHoldingJSON{
			Token: spotHex(token), Balance: balance.String(), BlockNumber: &block,
			Symbol: meta.symbol, Decimals: meta.decimals, Type: "ERC-20",
		})
	}
	return body, nil
}

type spotDerivedJSON struct {
	Qty            string  `json:"qty"`
	AvgEntryPrice  string  `json:"avgEntryPrice"`
	AvgExitPrice   *string `json:"avgExitPrice"`
	TotalBought    string  `json:"totalBought"`
	TotalSold      string  `json:"totalSold"`
	TotalCostBasis string  `json:"totalCostBasis"`
	TotalProceeds  string  `json:"totalProceeds"`
	RealizedPnl    string  `json:"realizedPnl"`
	RealizedPnlUsd string  `json:"realizedPnlUsd"`
	TradeCount     int     `json:"tradeCount"`
	FirstTradeAt   *string `json:"firstTradeAt"`
	LastTradeAt    *string `json:"lastTradeAt"`
}

type spotQtyJSON struct {
	Qty string `json:"qty"`
}

type spotPositionJSON struct {
	Token            string          `json:"token"`
	Symbol           *string         `json:"symbol"`
	Name             *string         `json:"name"`
	Decimals         *int            `json:"decimals"`
	IsStablecoin     bool            `json:"isStablecoin"`
	Derived          spotDerivedJSON `json:"derived"`
	Paxscan          spotQtyJSON     `json:"paxscan"`
	ExternalActivity bool            `json:"externalActivity"`
	ExternalQtyDelta string          `json:"externalQtyDelta"`
}

type spotPositionsBody struct {
	Address   string             `json:"address"`
	Positions []spotPositionJSON `json:"positions"`
}

// positions answers the fold of every token the user traded beside its node
// balance at the latest height; externalQtyDelta is node minus derived.
func (s *SpotUsers) positions(ctx context.Context, user common.Address, _ map[string][]string) (any, error) {
	traders, err := s.fold(ctx, &user)
	if err != nil {
		return nil, err
	}
	body := spotPositionsBody{Address: spotHex(user), Positions: []spotPositionJSON{}}
	tr := traders[user]
	if tr == nil {
		return body, nil
	}
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	height, err := s.head(ctx)
	if err != nil {
		return nil, err
	}
	for _, b := range tr.books {
		node, err := s.balanceOf(ctx, b.token, user, height)
		if err != nil {
			return nil, err
		}
		qty, err := spotInt(b.pos.CurrentHoldings)
		if err != nil {
			return nil, err
		}
		sold, err := spotInt(b.pos.TotalTokensSold)
		if err != nil {
			return nil, err
		}
		var avgExit *string
		if sold.Sign() > 0 {
			received, err := spotInt(b.pos.TotalUsdlReceived)
			if err != nil {
				return nil, err
			}
			v := new(big.Int).Quo(new(big.Int).Mul(received, spotWad), sold).String()
			avgExit = &v
		}
		delta := new(big.Int).Sub(node, qty)
		meta := tokens[b.token]
		first, last := spotISO(b.first), spotISO(b.last)
		body.Positions = append(body.Positions, spotPositionJSON{
			Token: spotHex(b.token), Symbol: meta.symbol, Decimals: meta.decimals, IsStablecoin: meta.stable,
			Derived: spotDerivedJSON{
				Qty: b.pos.CurrentHoldings, AvgEntryPrice: b.pos.AvgCostBasis, AvgExitPrice: avgExit,
				TotalBought: b.pos.TotalTokensBought, TotalSold: b.pos.TotalTokensSold,
				TotalCostBasis: b.pos.TotalUsdlSpent, TotalProceeds: b.pos.TotalUsdlReceived,
				RealizedPnl: b.pos.RealizedPnlUsdl, RealizedPnlUsd: b.pos.RealizedPnlUsdl,
				TradeCount: b.pos.TradeCount, FirstTradeAt: &first, LastTradeAt: &last,
			},
			Paxscan:          spotQtyJSON{Qty: node.String()},
			ExternalActivity: delta.Sign() != 0,
			ExternalQtyDelta: delta.String(),
		})
	}
	return body, nil
}

// spotOrdersSQL reads the spot orders of $1, all or only the open and
// activated ones ($2), newest placement first, with the times and hashes of
// their placing, executing and cancelling or expiring events.
const spotOrdersSQL = `SELECT trunc(o.order_id)::text, o.kind, o.status, o.token, o.stablecoin, trunc(o.amount)::text,
	trunc(o.target_price)::text, trunc(o.stop_price)::text, trunc(o.limit_price)::text, trunc(o.trigger_price)::text,
	trunc(o.execution_price)::text, o.side = 'buy', p.block_time, p.tx_hash, x.block_time, x.tx_hash, c.block_time
FROM perps.spot_orders o
LEFT JOIN LATERAL (SELECT e.block_time, e.tx_hash FROM perps.spot_order_events e
	WHERE e.order_id = o.order_id AND e.kind = o.kind || '_created'
	ORDER BY e.block, e.tx_index, e.log_index, e.sub LIMIT 1) p ON true
LEFT JOIN LATERAL (SELECT e.block_time, e.tx_hash FROM perps.spot_order_events e
	WHERE e.order_id = o.order_id AND e.kind = o.kind || '_executed'
	ORDER BY e.block, e.tx_index, e.log_index, e.sub LIMIT 1) x ON true
LEFT JOIN LATERAL (SELECT e.block_time FROM perps.spot_order_events e
	WHERE e.order_id = o.order_id AND e.kind IN (o.kind || '_cancelled', o.kind || '_expired')
	ORDER BY e.block, e.tx_index, e.log_index, e.sub LIMIT 1) c ON true
WHERE o.account = $1 AND (NOT $2::boolean OR o.status IN ('open', 'activated'))
ORDER BY o.placed_block DESC, o.order_id DESC
LIMIT $3`

type spotOrderJSON struct {
	ID             string  `json:"id"`
	OrderID        string  `json:"orderId"`
	Type           string  `json:"type"`
	Status         string  `json:"status"`
	Token          *string `json:"token"`
	Stablecoin     *string `json:"stablecoin"`
	Amount         *string `json:"amount"`
	TargetPrice    *string `json:"targetPrice"`
	StopPrice      *string `json:"stopPrice"`
	LimitPrice     *string `json:"limitPrice"`
	TriggerPrice   *string `json:"triggerPrice"`
	ExecutionPrice *string `json:"executionPrice"`
	IsBuy          *bool   `json:"isBuy"`
	PlacedAt       *string `json:"placedAt"`
	ExecutedAt     *string `json:"executedAt"`
	CancelledAt    *string `json:"cancelledAt"`
	PlacedTxHash   *string `json:"placedTxHash"`
	ExecutedTxHash *string `json:"executedTxHash"`
	Symbol         *string `json:"symbol"`
	Decimals       *int    `json:"decimals"`
}

type spotOrdersBody struct {
	Address string          `json:"address"`
	Count   int             `json:"count"`
	Items   []spotOrderJSON `json:"items"`
}

// orders answers the spot orders of the user from perps.spot_orders; status is
// open or all (the default).
func (s *SpotUsers) orders(ctx context.Context, user common.Address, q map[string][]string) (any, error) {
	var openOnly bool
	switch spotQuery(q, "status") {
	case "", "all":
	case "open":
		openOnly = true
	default:
		return nil, &spotError{http.StatusBadRequest, "invalid status"}
	}
	limit, err := parseLimit(q)
	if err != nil {
		return nil, &spotError{http.StatusBadRequest, "invalid limit"}
	}
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, spotOrdersSQL, user.Bytes(), openOnly, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	body := spotOrdersBody{Address: spotHex(user), Items: []spotOrderJSON{}}
	for rows.Next() {
		var (
			o                                        spotOrderJSON
			token, stablecoin, placedTx, executedTx  []byte
			amount, target, stop, limitP, trig, exec sql.NullString
			buy                                      bool
			placedAt, executedAt, cancelledAt        sql.NullTime
		)
		if err := rows.Scan(&o.OrderID, &o.Type, &o.Status, &token, &stablecoin, &amount, &target, &stop, &limitP, &trig,
			&exec, &buy, &placedAt, &placedTx, &executedAt, &executedTx, &cancelledAt); err != nil {
			return nil, err
		}
		tokenAddr, stableAddr := common.BytesToAddress(token), common.BytesToAddress(stablecoin)
		tokenHex, stableHex := spotHex(tokenAddr), spotHex(stableAddr)
		meta := tokens[tokenAddr]
		o.ID, o.Token, o.Stablecoin = o.OrderID, &tokenHex, &stableHex
		o.Amount, o.TargetPrice, o.StopPrice = spotOptString(amount), spotOptString(target), spotOptString(stop)
		o.LimitPrice, o.TriggerPrice, o.ExecutionPrice = spotOptString(limitP), spotOptString(trig), spotOptString(exec)
		o.IsBuy = &buy
		o.PlacedAt, o.ExecutedAt, o.CancelledAt = spotOptISO(placedAt), spotOptISO(executedAt), spotOptISO(cancelledAt)
		o.PlacedTxHash, o.ExecutedTxHash = spotOptHash(placedTx), spotOptHash(executedTx)
		o.Symbol, o.Decimals = meta.symbol, meta.decimals
		body.Items = append(body.Items, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	body.Count = len(body.Items)
	return body, nil
}

func spotQuery(q map[string][]string, name string) string {
	if v := q[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// spotUserTradesSQL pages the trades of $1 newest first below the cursor
// (block, log_index) < ($2, $3), with the tiered fee and the price impact of
// the same transaction, user and output token. volume is the stable side.
const spotUserTradesSQL = spotTradesCTE + `SELECT t.source, t.block, t.log_index, t.tx_hash, t.block_time, t.emitter, t.account,
	t.token_in, t.token_out, trunc(t.amount_in)::text, trunc(t.amount_out)::text,
	trunc(COALESCE(t.price, CASE WHEN t.in_stable AND t.amount_out > 0 THEN t.amount_in * 1000000000000000000 / t.amount_out
		WHEN t.amount_in > 0 THEN t.amount_out * 1000000000000000000 / t.amount_in ELSE 0 END))::text,
	trunc(CASE WHEN t.in_stable THEN t.amount_in WHEN t.out_stable THEN t.amount_out ELSE 0 END)::text,
	trunc(fee.fee_amount)::text, fee.fee_bps::bigint, trunc(imp.deducted_amount)::text, imp.impact_bps::bigint
FROM trades t
LEFT JOIN LATERAL (SELECT f.fee_amount, f.fee_bps FROM perps.spot_fee_events f
	WHERE f.kind = 'tiered_fee' AND f.tx_hash = t.tx_hash AND f."user" = t.account AND f.token_out = t.token_out
	ORDER BY f.block, f.tx_index, f.log_index, f.sub LIMIT 1) fee ON true
LEFT JOIN LATERAL (SELECT f.deducted_amount, f.impact_bps FROM perps.spot_fee_events f
	WHERE f.kind = 'price_impact' AND f.tx_hash = t.tx_hash AND f."user" = t.account AND f.token_out = t.token_out
	ORDER BY f.block, f.tx_index, f.log_index, f.sub LIMIT 1) imp ON true
WHERE $2::bigint IS NULL OR (t.block, t.log_index) < ($2::bigint, $3::integer)
ORDER BY t.block DESC, t.log_index DESC, t.tx_index DESC, t.sub DESC
LIMIT $4`

type spotTradeItemJSON struct {
	ID                string  `json:"id"`
	TxHash            string  `json:"txHash"`
	LogIndex          int     `json:"logIndex"`
	BlockNumber       string  `json:"blockNumber"`
	BlockTimestamp    string  `json:"blockTimestamp"`
	User              string  `json:"user"`
	TokenIn           string  `json:"tokenIn"`
	TokenOut          string  `json:"tokenOut"`
	AmountIn          string  `json:"amountIn"`
	AmountOut         string  `json:"amountOut"`
	ExecutionPrice    string  `json:"executionPrice"`
	VolumeUsd         string  `json:"volumeUsd"`
	Source            string  `json:"source"`
	Contract          string  `json:"contract"`
	CreatedAt         string  `json:"createdAt"`
	FeeAmount         *string `json:"feeAmount"`
	FeeBps            *int64  `json:"feeBps"`
	Hops              *int    `json:"hops"`
	PriceImpactAmount *string `json:"priceImpactAmount"`
	PriceImpactBps    *int64  `json:"priceImpactBps"`
	RouteAdapterID    *string `json:"routeAdapterId"`
	TwapPrice         *string `json:"twapPrice"`
}

type spotTradeCursor struct {
	AfterBlock string `json:"afterBlock"`
	AfterIndex int    `json:"afterIndex"`
}

type spotTradesBody struct {
	Address    string              `json:"address"`
	Count      int                 `json:"count"`
	Limit      int                 `json:"limit"`
	Items      []spotTradeItemJSON `json:"items"`
	NextCursor *spotTradeCursor    `json:"nextCursor"`
}

// trades pages the spot trades of the user newest first by afterBlock and
// afterIndex, which go together.
func (s *SpotUsers) trades(ctx context.Context, user common.Address, q map[string][]string) (any, error) {
	limit, err := parseLimit(q)
	if err != nil {
		return nil, &spotError{http.StatusBadRequest, "invalid limit"}
	}
	rawBlock, rawIndex := spotQuery(q, "afterBlock"), spotQuery(q, "afterIndex")
	var afterBlock, afterIndex any
	switch {
	case rawBlock == "" && rawIndex == "":
	case rawBlock == "" || rawIndex == "":
		return nil, &spotError{http.StatusBadRequest, "afterBlock and afterIndex go together"}
	default:
		b, err := strconv.ParseInt(rawBlock, 10, 64)
		if err != nil || b < 0 {
			return nil, &spotError{http.StatusBadRequest, "invalid afterBlock"}
		}
		i, err := strconv.ParseInt(rawIndex, 10, 32)
		if err != nil || i < 0 {
			return nil, &spotError{http.StatusBadRequest, "invalid afterIndex"}
		}
		afterBlock, afterIndex = b, i
	}
	rows, err := s.db.QueryContext(ctx, spotUserTradesSQL, user.Bytes(), afterBlock, afterIndex, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	body := spotTradesBody{Address: spotHex(user), Limit: limit, Items: []spotTradeItemJSON{}}
	for rows.Next() {
		var (
			t                                   spotTradeItemJSON
			block                               int64
			txHash, emitter, account, tin, tout []byte
			at                                  time.Time
			feeAmount, impactAmount             sql.NullString
			feeBps, impactBps                   sql.NullInt64
		)
		if err := rows.Scan(&t.Source, &block, &t.LogIndex, &txHash, &at, &emitter, &account, &tin, &tout, &t.AmountIn,
			&t.AmountOut, &t.ExecutionPrice, &t.VolumeUsd, &feeAmount, &feeBps, &impactAmount, &impactBps); err != nil {
			return nil, err
		}
		if len(body.Items) == limit {
			last := body.Items[limit-1]
			body.NextCursor = &spotTradeCursor{AfterBlock: last.BlockNumber, AfterIndex: last.LogIndex}
			break
		}
		t.ID, t.TxHash, t.BlockNumber = tradeID(block, t.LogIndex), common.BytesToHash(txHash).Hex(), strconv.FormatInt(block, 10)
		t.BlockTimestamp, t.CreatedAt = spotISO(at), spotISO(at)
		t.User, t.Contract = spotHex(common.BytesToAddress(account)), spotHex(common.BytesToAddress(emitter))
		t.TokenIn, t.TokenOut = spotHex(common.BytesToAddress(tin)), spotHex(common.BytesToAddress(tout))
		t.FeeAmount, t.PriceImpactAmount = spotOptString(feeAmount), spotOptString(impactAmount)
		if feeBps.Valid {
			t.FeeBps = &feeBps.Int64
		}
		if impactBps.Valid {
			t.PriceImpactBps = &impactBps.Int64
		}
		body.Items = append(body.Items, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	body.Count = len(body.Items)
	return body, nil
}

const spotTradesStatsSQL = spotTradesCTE + `SELECT source, emitter, count(*) FROM trades GROUP BY source, emitter`

type spotTradesStatsBody struct {
	Address     string           `json:"address"`
	TotalTrades int64            `json:"totalTrades"`
	BySource    map[string]int64 `json:"bySource"`
	ByContract  map[string]int64 `json:"byContract"`
}

// tradesStats counts the trades of the user by source and by contract.
func (s *SpotUsers) tradesStats(ctx context.Context, user common.Address, _ map[string][]string) (any, error) {
	rows, err := s.db.QueryContext(ctx, spotTradesStatsSQL, user.Bytes())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	body := spotTradesStatsBody{Address: spotHex(user), BySource: map[string]int64{}, ByContract: map[string]int64{}}
	for rows.Next() {
		var (
			source  string
			emitter []byte
			n       int64
		)
		if err := rows.Scan(&source, &emitter, &n); err != nil {
			return nil, err
		}
		body.TotalTrades += n
		body.BySource[source] += n
		body.ByContract[spotHex(common.BytesToAddress(emitter))] += n
	}
	return body, rows.Err()
}

type spotClosedLegJSON struct {
	ID             int     `json:"id"`
	Token          string  `json:"token"`
	Qty            string  `json:"qty"`
	EntryPrice     string  `json:"entryPrice"`
	ExitPrice      string  `json:"exitPrice"`
	Pnl            string  `json:"pnl"`
	PnlUsd         string  `json:"pnlUsd"`
	PnlPct         *string `json:"pnlPct"`
	HoldingSeconds *int64  `json:"holdingSeconds"`
	OpenedAt       *string `json:"openedAt"`
	ClosedAt       string  `json:"closedAt"`
	CloseTxHash    string  `json:"closeTxHash"`
	FullyClosed    bool    `json:"fullyClosed"`
	Symbol         *string `json:"symbol"`
	Name           *string `json:"name"`
	Decimals       *int    `json:"decimals"`
}

type spotLegCursor struct {
	AfterID int `json:"afterId"`
}

type spotClosedLegsBody struct {
	Address    string              `json:"address"`
	Count      int                 `json:"count"`
	Limit      int                 `json:"limit"`
	Items      []spotClosedLegJSON `json:"items"`
	NextCursor *spotLegCursor      `json:"nextCursor"`
}

// closedLegs pages the closed legs of the user newest first below afterId. A
// leg id is the chronological ordinal of the sell among the user's sells.
func (s *SpotUsers) closedLegs(ctx context.Context, user common.Address, q map[string][]string) (any, error) {
	limit, err := parseLimit(q)
	if err != nil {
		return nil, &spotError{http.StatusBadRequest, "invalid limit"}
	}
	afterID := 0
	if raw := spotQuery(q, "afterId"); raw != "" {
		if afterID, err = strconv.Atoi(raw); err != nil || afterID < 1 {
			return nil, &spotError{http.StatusBadRequest, "invalid afterId"}
		}
	}
	traders, err := s.fold(ctx, &user)
	if err != nil {
		return nil, err
	}
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	body := spotClosedLegsBody{Address: spotHex(user), Limit: limit, Items: []spotClosedLegJSON{}}
	var closed []spotClosed
	if tr := traders[user]; tr != nil {
		closed = tr.closed
	}
	for i := len(closed) - 1; i >= 0; i-- {
		leg := closed[i]
		if afterID != 0 && leg.id >= afterID {
			continue
		}
		if len(body.Items) == limit {
			body.NextCursor = &spotLegCursor{AfterID: body.Items[limit-1].ID}
			break
		}
		meta := tokens[leg.token]
		item := spotClosedLegJSON{
			ID: leg.id, Token: spotHex(leg.token), Qty: leg.qty, EntryPrice: leg.entry, ExitPrice: leg.exit,
			Pnl: leg.pnl, PnlUsd: leg.pnl, PnlPct: leg.pct, ClosedAt: spotISO(leg.closed), CloseTxHash: leg.tx.Hex(),
			FullyClosed: leg.full, Symbol: meta.symbol, Decimals: meta.decimals,
		}
		if leg.opened != nil {
			opened := spotISO(*leg.opened)
			held := int64(leg.closed.Sub(*leg.opened) / time.Second)
			item.OpenedAt, item.HoldingSeconds = &opened, &held
		}
		body.Items = append(body.Items, item)
	}
	body.Count = len(body.Items)
	return body, nil
}

const spotIndexPriceSQL = `SELECT trunc(index_price)::text FROM perps.index_prices
WHERE kind = 'aggregate' AND market_id = $1 AND index_price IS NOT NULL
ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT 1`

// mark returns the price of one raw token unit in stablecoin raw units at
// 10^18 scale from the latest index price of the token's pricefeed market,
// false when the token has no market or no price.
func (s *SpotUsers) mark(ctx context.Context, meta spotToken) (string, bool, error) {
	if meta.symbol == nil || meta.decimals == nil {
		return "", false, nil
	}
	market, ok := marketIDs[*meta.symbol]
	if !ok {
		return "", false, nil
	}
	var raw string
	err := s.db.QueryRowContext(ctx, spotIndexPriceSQL, market).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	index, err := spotInt(raw)
	if err != nil {
		return "", false, err
	}
	stableScale, err := spotInt(spotScale)
	if err != nil {
		return "", false, err
	}
	tokenScale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(*meta.decimals)), nil)
	return new(big.Int).Quo(new(big.Int).Mul(index, stableScale), tokenScale).String(), true, nil
}

type spotPnlBody struct {
	Address        string  `json:"address"`
	RealizedUsd    string  `json:"realizedUsd"`
	UnrealizedUsd  *string `json:"unrealizedUsd,omitempty"`
	TotalUsd       *string `json:"totalUsd,omitempty"`
	TradeCount     int     `json:"tradeCount"`
	ClosedLegCount int     `json:"closedLegCount"`
}

// pnlSummary answers the realized profit of the fold and, when every open
// position has a mark, the unrealized and total profit at the latest index
// prices (pnl.TotalPnl).
func (s *SpotUsers) pnlSummary(ctx context.Context, user common.Address, _ map[string][]string) (any, error) {
	traders, err := s.fold(ctx, &user)
	if err != nil {
		return nil, err
	}
	body := spotPnlBody{Address: spotHex(user), RealizedUsd: "0"}
	tr := traders[user]
	if tr == nil {
		zero := "0"
		body.UnrealizedUsd, body.TotalUsd = &zero, &zero
		return body, nil
	}
	tokens, err := s.tokens(ctx)
	if err != nil {
		return nil, err
	}
	realized, err := tr.realized()
	if err != nil {
		return nil, err
	}
	unrealized, marked := new(big.Int), true
	for _, b := range tr.books {
		if b.pos.CurrentHoldings == "0" {
			continue
		}
		price, ok, err := s.mark(ctx, tokens[b.token])
		if err != nil {
			return nil, err
		}
		if !ok {
			marked = false
			break
		}
		raw, err := pnl.TotalPnl(b.pos.RealizedPnlUsdl, b.pos.AvgCostBasis, b.pos.CurrentHoldings, price)
		if err != nil {
			return nil, err
		}
		total, err := spotInt(raw)
		if err != nil {
			return nil, err
		}
		own, err := spotInt(b.pos.RealizedPnlUsdl)
		if err != nil {
			return nil, err
		}
		unrealized.Add(unrealized, total.Sub(total, own))
	}
	body.RealizedUsd, body.TradeCount, body.ClosedLegCount = realized.String(), tr.legs, len(tr.closed)
	if marked {
		u, t := unrealized.String(), new(big.Int).Add(realized, unrealized).String()
		body.UnrealizedUsd, body.TotalUsd = &u, &t
	}
	return body, nil
}

const spotIdentitySQL = spotTradesCTE + `SELECT min(block), count(DISTINCT tx_hash), count(*) FILTER (WHERE block_time >= $2),
	trunc(COALESCE(sum(CASE WHEN in_stable THEN amount_in WHEN out_stable THEN amount_out ELSE 0 END)
		FILTER (WHERE block_time >= $2), 0))::text
FROM trades`

type spotPaxscanJSON struct {
	FirstSeenBlock *string `json:"firstSeenBlock"`
	TxCount        int64   `json:"txCount"`
}

type spotTrading24hJSON struct {
	Trades    int64  `json:"trades"`
	VolumeUsd string `json:"volumeUsd"`
}

type spotSnapshotJSON struct {
	RealizedUsd string  `json:"realizedUsd"`
	At          *string `json:"at"`
}

type spotIdentityBody struct {
	Address        string             `json:"address"`
	Paxscan        spotPaxscanJSON    `json:"paxscan"`
	Trading24h     spotTrading24hJSON `json:"trading24h"`
	LatestSnapshot spotSnapshotJSON   `json:"latestSnapshot"`
}

// identity answers the first block and transaction count of the user's spot
// trades, the trades and stable volume of the last 24 hours and the realized
// profit as of the latest trade.
func (s *SpotUsers) identity(ctx context.Context, user common.Address, _ map[string][]string) (any, error) {
	var (
		first sql.NullInt64
		body  = spotIdentityBody{Address: spotHex(user)}
	)
	err := s.db.QueryRowContext(ctx, spotIdentitySQL, user.Bytes(), time.Now().Add(-window24h)).Scan(&first,
		&body.Paxscan.TxCount, &body.Trading24h.Trades, &body.Trading24h.VolumeUsd)
	if err != nil {
		return nil, err
	}
	if first.Valid {
		v := strconv.FormatInt(first.Int64, 10)
		body.Paxscan.FirstSeenBlock = &v
	}
	traders, err := s.fold(ctx, &user)
	if err != nil {
		return nil, err
	}
	body.LatestSnapshot.RealizedUsd = "0"
	if tr := traders[user]; tr != nil {
		realized, err := tr.realized()
		if err != nil {
			return nil, err
		}
		body.LatestSnapshot.RealizedUsd = realized.String()
		if last := tr.lastTrade(); last != nil {
			at := spotISO(*last)
			body.LatestSnapshot.At = &at
		}
	}
	return body, nil
}

type spotRankJSON struct {
	Rank    int    `json:"rank"`
	Address string `json:"address"`
	Value   string `json:"value"`
}

type spotLeaderboardBody struct {
	Kind  string         `json:"kind"`
	Items []spotRankJSON `json:"items"`
}

// leaderboard ranks every spot trader by realized profit (pnl), stable volume
// (volume) or the percentage of profitable closed legs among the decided ones
// (win-rate), highest first, ties by address.
func (s *SpotUsers) leaderboard(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if !slices.Contains(spotLeaderboards, kind) {
		writeError(w, http.StatusNotFound, "unknown leaderboard "+kind)
		return
	}
	limit, err := parseLimit(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid limit")
		return
	}
	body, err := s.rank(r.Context(), kind, limit)
	spotWrite(w, body, err)
}

func (s *SpotUsers) rank(ctx context.Context, kind string, limit int) (spotLeaderboardBody, error) {
	body := spotLeaderboardBody{Kind: kind, Items: []spotRankJSON{}}
	traders, err := s.fold(ctx, nil)
	if err != nil {
		return body, err
	}
	type entry struct {
		account common.Address
		value   *big.Rat
		text    string
	}
	entries := make([]entry, 0, len(traders))
	for account, tr := range traders {
		e := entry{account: account}
		switch kind {
		case "pnl":
			realized, err := tr.realized()
			if err != nil {
				return body, err
			}
			e.value, e.text = new(big.Rat).SetInt(realized), realized.String()
		case "volume":
			e.value, e.text = new(big.Rat).SetInt(tr.volume), tr.volume.String()
		default:
			decided := tr.wins + tr.losses
			if decided == 0 {
				continue
			}
			e.value = big.NewRat(int64(tr.wins)*100, int64(decided))
			e.text = e.value.FloatString(4)
		}
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b entry) int {
		if c := b.value.Cmp(a.value); c != 0 {
			return c
		}
		return bytes.Compare(a.account.Bytes(), b.account.Bytes())
	})
	for i, e := range entries[:min(len(entries), limit)] {
		body.Items = append(body.Items, spotRankJSON{Rank: i + 1, Address: spotHex(e.account), Value: e.text})
	}
	return body, nil
}
