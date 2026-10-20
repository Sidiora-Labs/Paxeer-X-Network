package portal

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/records"
)

// ErrNotFound reports a page key the gateway does not know; the server
// answers 404 inside the layout.
var ErrNotFound = errors.New("portal: not found")

// Row bounds of the pages.
const (
	fillRows        = 100
	historyRows     = 100
	leaderboardRows = 100
	snapshotWindow  = 7 * 24 * time.Hour
	// snapshotRows is the count of hourly snapshots inside snapshotWindow.
	snapshotRows   = int(snapshotWindow / time.Hour)
	maxGatewayBody = 16 << 20
)

// The decimal scales of the gateway values: perps USD amounts, prices and
// rates are 18-decimal fixed point, USDL token amounts 6-decimal.
const (
	usdDecimals  = 18
	usdlDecimals = 6
)

// leaderboardWindows are the windows of the leaderboard page. The all window
// is GetLeaderboard; the GraphQL leaderboard answers that window only, so the
// 30d and 7d windows read the period of /v2/leaderboard.
var leaderboardWindows = []string{"all", "30d", "7d"}

// The account and leaderboard operations, GetMarketSnapshots, GetGlobalStats
// and GetProtocolSnapshots are the application's own documents
// (Hyperpax:src/hooks/perps/useSubgraph.ts); the funding history and indexer
// status are named queries of roots the application reads without one.
const (
	getPositions = `query GetPositions($userAddress: String!) {
    positions(userAddress: $userAddress, status: "open") {
      positionId userAddress marketId isLong sizeUsd leverage entryPrice
      collateralToken collateralAmount collateralUsd status openedAt openTxHash
      market { symbol }
    }
  }`
	getAllOrdersV2 = `query GetAllOrdersV2($userAddress: String!, $status: String, $orderType: Int, $limit: Int, $offset: Int) {
    orders(userAddress: $userAddress, status: $status, orderType: $orderType, limit: $limit, offset: $offset) {
      orderId userAddress marketId orderType orderTypeName isLong triggerPrice sizeUsd status
      positionId executionPrice failureReason placedAt resolvedAt placedTxHash resolvedTxHash
    }
  }`
	getTradeSettlements = `query GetTradeSettlements($userAddress: String, $marketId: Int, $positionId: String, $tradeType: Int, $limit: Int, $offset: Int) {
    tradeSettlements(userAddress: $userAddress, marketId: $marketId, positionId: $positionId, tradeType: $tradeType, limit: $limit, offset: $offset) {
      id positionId userAddress marketId tradeType tradeTypeName sizeUsd executionPrice
      grossPnl totalFeesUsd borrowingFeeUsd fundingPaidUsd netPayoutTokens
      blockNumber txHash blockTimestamp
    }
  }`
	getFundingPayments = `query GetFundingPayments($userAddress: String, $marketId: Int, $positionId: String, $limit: Int, $offset: Int) {
    fundingPayments(userAddress: $userAddress, marketId: $marketId, positionId: $positionId, limit: $limit, offset: $offset) {
      id positionId userAddress marketId fundingPaymentUsd newCollateralUsd
      newCollateralAmount blockNumber txHash blockTimestamp
    }
  }`
	getLiquidations = `query GetLiquidations($userAddress: String!, $limit: Int, $offset: Int) {
    liquidations(userAddress: $userAddress, limit: $limit, offset: $offset) {
      id positionId marketId price penalty keeper txHash blockTimestamp
    }
  }`
	getVaultEvents = `query GetVaultEvents($userAddress: String!, $limit: Int, $offset: Int) {
    vaultEvents(userAddress: $userAddress, limit: $limit, offset: $offset) {
      id eventType tokenAddress amount blockNumber txHash blockTimestamp
    }
  }`
	getLeaderboard = `query GetLeaderboard($limit: Int, $offset: Int) {
    leaderboard(limit: $limit, offset: $offset) {
      rank userAddress totalRealizedPnl totalTrades winRate avgHoldTime
      openPositions totalVolume
    }
  }`
	getMarketSnapshots = `query GetMarketSnapshots($marketId: Int!, $limit: Int, $offset: Int) {
    marketSnapshots(marketId: $marketId, limit: $limit, offset: $offset) {
      id marketId longOi shortOi markPrice indexPrice fundingRatePerSecond
      fundingRate24h volume24hUsd onchainTimestamp blockNumber blockTimestamp
    }
  }`
	getFundingRates = `query PortalFundingRates($marketId: Int!, $limit: Int) {
    fundingRates(marketId: $marketId, limit: $limit) {
      id marketId ratePerSecond rate24h blockNumber txHash blockTimestamp
    }
  }`
	getGlobalStats = `query GetGlobalStats {
    globalStats {
      totalMarkets totalPositions openPositions totalTrades totalLiquidations totalVolume totalUsers
    }
  }`
	getProtocolSnapshots = `query GetProtocolSnapshots($limit: Int, $offset: Int) {
    protocolSnapshots(limit: $limit, offset: $offset) {
      id totalPositions totalOpenPositions totalMarkets tvlUsd insuranceTotalUsd
      onchainTimestamp blockNumber blockTimestamp
    }
  }`
	getIndexerStatus = `query PortalIndexerStatus {
    indexerStatus { lastIndexedBlock chainHead blocksScanned eventsProcessed isSynced }
  }`
)

//go:embed templates/pages.html
var templateFiles embed.FS

// Pages renders the portal pages from the gateway REST and GraphQL at
// gatewayURL, linking transactions under explorerTxBase.
type Pages struct {
	gateway  string
	explorer string
	client   *http.Client
	now      func() time.Time
	tmpl     *template.Template
}

// NewPages checks the gateway URL, the explorer transaction base and the
// client the pages read through, and parses the page templates.
func NewPages(gatewayURL, explorerTxBase string, client *http.Client) (*Pages, error) {
	if client == nil {
		return nil, errors.New("portal_http_client_required")
	}
	for _, raw := range []string{gatewayURL, explorerTxBase} {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, errors.New("portal_page_url_needs_http_or_https")
		}
	}
	p := &Pages{
		gateway:  strings.TrimRight(gatewayURL, "/"),
		explorer: strings.TrimRight(explorerTxBase, "/") + "/",
		client:   client,
		now:      time.Now,
	}
	tmpl, err := template.New("pages").Funcs(template.FuncMap{
		"usd":     func(raw string) string { return fixed(raw, usdDecimals) },
		"usdl":    func(raw string) string { return fixed(raw, usdlDecimals) },
		"num":     num,
		"optnum":  optnum,
		"optint":  optint,
		"optbool": optbool,
		"ms":      func(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) },
		"when":    when,
		"side":    side,
		"lower":   strings.ToLower,
		"tx":      p.txURL,
	}).ParseFS(templateFiles, "templates/pages.html")
	if err != nil {
		return nil, fmt.Errorf("portal templates: %w", err)
	}
	p.tmpl = tmpl
	return p, nil
}

type marketsPage struct {
	Title   string
	Markets []records.MarketRecord
	Totals  records.Totals
}

// Markets is the home page: the markets table of /api/markets and the
// platform totals of /api/totals.
func (p *Pages) Markets(ctx context.Context, w io.Writer) error {
	var markets struct {
		Markets []records.MarketRecord `json:"markets"`
	}
	if err := p.get(ctx, "/api/markets", nil, &markets); err != nil {
		return err
	}
	var totals struct {
		Totals records.Totals `json:"totals"`
	}
	if err := p.get(ctx, "/api/totals", nil, &totals); err != nil {
		return err
	}
	return p.render(w, "markets", marketsPage{Title: "Markets", Markets: markets.Markets, Totals: totals.Totals})
}

type fundingRate struct {
	ID             int64     `json:"id"`
	RatePerSecond  string    `json:"ratePerSecond"`
	Rate24h        string    `json:"rate24h"`
	BlockNumber    int64     `json:"blockNumber"`
	TxHash         string    `json:"txHash"`
	BlockTimestamp time.Time `json:"blockTimestamp"`
}

type marketSnapshot struct {
	MarkPrice      string    `json:"markPrice"`
	IndexPrice     string    `json:"indexPrice"`
	Volume24hUsd   string    `json:"volume24hUsd"`
	BlockNumber    int64     `json:"blockNumber"`
	BlockTimestamp time.Time `json:"blockTimestamp"`
}

type marketPage struct {
	Title     string
	Market    records.MarketRecord
	Fills     []records.PerpTrade
	Funding   []fundingRate
	Snapshots []marketSnapshot
}

// Market is the page of symbol: the record of /api/markets/<symbol>, the last
// fillRows perpetual fills of /api/<symbol>/perp_trades, the funding history of
// fundingRates and the hourly marketSnapshots of the last snapshotWindow.
// Chart candles stay on the Chainflow feed, so no candle route is read.
func (p *Pages) Market(ctx context.Context, w io.Writer, symbol string) error {
	upper := strings.ToUpper(symbol)
	var record struct {
		Market records.MarketRecord `json:"market"`
	}
	if err := p.get(ctx, "/api/markets/"+url.PathEscape(upper), nil, &record); err != nil {
		return err
	}
	marketID, ok := records.MarketIDs[upper]
	if !ok {
		return ErrNotFound
	}
	var fills records.Trades[records.PerpTrade]
	if err := p.get(ctx, "/api/"+url.PathEscape(strings.ToLower(symbol))+"/perp_trades",
		url.Values{"limit": {strconv.Itoa(fillRows)}}, &fills); err != nil {
		return err
	}
	var funding struct {
		FundingRates []fundingRate `json:"fundingRates"`
	}
	if err := p.query(ctx, getFundingRates, "PortalFundingRates",
		map[string]any{"marketId": marketID, "limit": historyRows}, &funding); err != nil {
		return err
	}
	var snapshots struct {
		MarketSnapshots []marketSnapshot `json:"marketSnapshots"`
	}
	if err := p.query(ctx, getMarketSnapshots, "GetMarketSnapshots",
		map[string]any{"marketId": marketID, "limit": snapshotRows}, &snapshots); err != nil {
		return err
	}
	since := p.now().Add(-snapshotWindow)
	recent := []marketSnapshot{}
	for _, s := range snapshots.MarketSnapshots {
		if s.BlockTimestamp.After(since) {
			recent = append(recent, s)
		}
	}
	return p.render(w, "market", marketPage{Title: record.Market.Symbol, Market: record.Market, Fills: fills.Trades,
		Funding: funding.FundingRates, Snapshots: recent})
}

type position struct {
	PositionID       string     `json:"positionId"`
	IsLong           bool       `json:"isLong"`
	SizeUsd          string     `json:"sizeUsd"`
	Leverage         string     `json:"leverage"`
	EntryPrice       string     `json:"entryPrice"`
	CollateralAmount string     `json:"collateralAmount"`
	CollateralUsd    string     `json:"collateralUsd"`
	OpenedAt         *time.Time `json:"openedAt"`
	OpenTxHash       *string    `json:"openTxHash"`
	Market           *struct {
		Symbol string `json:"symbol"`
	} `json:"market"`
}

type order struct {
	OrderID       string     `json:"orderId"`
	MarketID      int64      `json:"marketId"`
	OrderTypeName *string    `json:"orderTypeName"`
	IsLong        bool       `json:"isLong"`
	TriggerPrice  string     `json:"triggerPrice"`
	SizeUsd       string     `json:"sizeUsd"`
	Status        string     `json:"status"`
	PlacedAt      *time.Time `json:"placedAt"`
	PlacedTxHash  *string    `json:"placedTxHash"`
}

type settlement struct {
	ID             int64     `json:"id"`
	PositionID     string    `json:"positionId"`
	MarketID       int64     `json:"marketId"`
	TradeTypeName  *string   `json:"tradeTypeName"`
	SizeUsd        string    `json:"sizeUsd"`
	ExecutionPrice string    `json:"executionPrice"`
	GrossPnl       string    `json:"grossPnl"`
	TotalFeesUsd   string    `json:"totalFeesUsd"`
	FundingPaidUsd string    `json:"fundingPaidUsd"`
	BlockNumber    int64     `json:"blockNumber"`
	TxHash         string    `json:"txHash"`
	BlockTimestamp time.Time `json:"blockTimestamp"`
}

type fundingPayment struct {
	ID                int64     `json:"id"`
	PositionID        string    `json:"positionId"`
	MarketID          int64     `json:"marketId"`
	FundingPaymentUsd string    `json:"fundingPaymentUsd"`
	NewCollateralUsd  string    `json:"newCollateralUsd"`
	BlockNumber       int64     `json:"blockNumber"`
	TxHash            string    `json:"txHash"`
	BlockTimestamp    time.Time `json:"blockTimestamp"`
}

type liquidation struct {
	ID             int64     `json:"id"`
	PositionID     string    `json:"positionId"`
	MarketID       int64     `json:"marketId"`
	Price          string    `json:"price"`
	Penalty        string    `json:"penalty"`
	Keeper         string    `json:"keeper"`
	TxHash         string    `json:"txHash"`
	BlockTimestamp time.Time `json:"blockTimestamp"`
}

type vaultEvent struct {
	ID             int64     `json:"id"`
	EventType      string    `json:"eventType"`
	TokenAddress   string    `json:"tokenAddress"`
	Amount         string    `json:"amount"`
	BlockNumber    int64     `json:"blockNumber"`
	TxHash         string    `json:"txHash"`
	BlockTimestamp time.Time `json:"blockTimestamp"`
}

type accountPage struct {
	Title        string
	Address      string
	Positions    []position
	Orders       []order
	Fills        []settlement
	Funding      []fundingPayment
	Liquidations []liquidation
	Collateral   []vaultEvent
}

// Account is the page of the lower-case address: open positions, active
// orders, fills, funding payments, liquidations and collateral events, each
// row linking its transaction to the explorer. An address without activity
// renders the empty sections.
func (p *Pages) Account(ctx context.Context, w io.Writer, address string) error {
	page := accountPage{Title: "Account " + address, Address: address}
	var positions struct {
		Positions []position `json:"positions"`
	}
	if err := p.query(ctx, getPositions, "GetPositions", map[string]any{"userAddress": address}, &positions); err != nil {
		return err
	}
	var orders struct {
		Orders []order `json:"orders"`
	}
	if err := p.query(ctx, getAllOrdersV2, "GetAllOrdersV2",
		map[string]any{"userAddress": address, "status": "active", "limit": historyRows}, &orders); err != nil {
		return err
	}
	var fills struct {
		TradeSettlements []settlement `json:"tradeSettlements"`
	}
	if err := p.query(ctx, getTradeSettlements, "GetTradeSettlements",
		map[string]any{"userAddress": address, "limit": historyRows}, &fills); err != nil {
		return err
	}
	var funding struct {
		FundingPayments []fundingPayment `json:"fundingPayments"`
	}
	if err := p.query(ctx, getFundingPayments, "GetFundingPayments",
		map[string]any{"userAddress": address, "limit": historyRows}, &funding); err != nil {
		return err
	}
	var liquidations struct {
		Liquidations []liquidation `json:"liquidations"`
	}
	if err := p.query(ctx, getLiquidations, "GetLiquidations",
		map[string]any{"userAddress": address, "limit": historyRows}, &liquidations); err != nil {
		return err
	}
	var collateral struct {
		VaultEvents []vaultEvent `json:"vaultEvents"`
	}
	if err := p.query(ctx, getVaultEvents, "GetVaultEvents",
		map[string]any{"userAddress": address, "limit": historyRows}, &collateral); err != nil {
		return err
	}
	page.Positions, page.Orders, page.Fills = positions.Positions, orders.Orders, fills.TradeSettlements
	page.Funding, page.Liquidations, page.Collateral = funding.FundingPayments, liquidations.Liquidations, collateral.VaultEvents
	return p.render(w, "account", page)
}

// leaderRow is one ranked account of a window.
type leaderRow struct {
	Rank    int64
	Address string
	Pnl     string
	Volume  string
	Trades  int64
	WinRate float64
}

type leaderWindow struct {
	Name string
	Rows []leaderRow
}

type leaderboardPage struct {
	Title   string
	Windows []leaderWindow
}

// Leaderboard renders the windows all, 30d and 7d.
func (p *Pages) Leaderboard(ctx context.Context, w io.Writer) error {
	page := leaderboardPage{Title: "Leaderboard"}
	for _, name := range leaderboardWindows {
		rows := []leaderRow{}
		if name == "all" {
			var all struct {
				Leaderboard []struct {
					Rank             int64   `json:"rank"`
					UserAddress      string  `json:"userAddress"`
					TotalRealizedPnl string  `json:"totalRealizedPnl"`
					TotalTrades      int64   `json:"totalTrades"`
					WinRate          float64 `json:"winRate"`
					TotalVolume      string  `json:"totalVolume"`
				} `json:"leaderboard"`
			}
			if err := p.query(ctx, getLeaderboard, "GetLeaderboard", map[string]any{"limit": leaderboardRows}, &all); err != nil {
				return err
			}
			for _, e := range all.Leaderboard {
				rows = append(rows, leaderRow{Rank: e.Rank, Address: e.UserAddress, Pnl: e.TotalRealizedPnl,
					Volume: e.TotalVolume, Trades: e.TotalTrades, WinRate: e.WinRate})
			}
		} else {
			var windowed struct {
				Leaderboard []struct {
					Rank    int64   `json:"rank"`
					Address string  `json:"address"`
					Pnl     string  `json:"pnl"`
					Volume  string  `json:"volume"`
					Trades  int64   `json:"trades"`
					WinRate float64 `json:"win_rate"`
				} `json:"leaderboard"`
			}
			if err := p.get(ctx, "/v2/leaderboard",
				url.Values{"period": {name}, "limit": {strconv.Itoa(leaderboardRows)}}, &windowed); err != nil {
				return err
			}
			for _, e := range windowed.Leaderboard {
				rows = append(rows, leaderRow{Rank: e.Rank, Address: e.Address, Pnl: e.Pnl, Volume: e.Volume,
					Trades: e.Trades, WinRate: e.WinRate})
			}
		}
		page.Windows = append(page.Windows, leaderWindow{Name: name, Rows: rows})
	}
	return p.render(w, "leaderboard", page)
}

type globalStats struct {
	TotalMarkets      int64  `json:"totalMarkets"`
	TotalPositions    int64  `json:"totalPositions"`
	OpenPositions     int64  `json:"openPositions"`
	TotalTrades       int64  `json:"totalTrades"`
	TotalLiquidations int64  `json:"totalLiquidations"`
	TotalVolume       string `json:"totalVolume"`
	TotalUsers        int64  `json:"totalUsers"`
}

type protocolSnapshot struct {
	TotalPositions     string    `json:"totalPositions"`
	TotalOpenPositions string    `json:"totalOpenPositions"`
	TotalMarkets       string    `json:"totalMarkets"`
	TvlUsd             string    `json:"tvlUsd"`
	InsuranceTotalUsd  string    `json:"insuranceTotalUsd"`
	BlockNumber        int64     `json:"blockNumber"`
	BlockTimestamp     time.Time `json:"blockTimestamp"`
}

type indexerStatus struct {
	LastIndexedBlock int64  `json:"lastIndexedBlock"`
	ChainHead        *int64 `json:"chainHead"`
	BlocksScanned    *int64 `json:"blocksScanned"`
	EventsProcessed  *int64 `json:"eventsProcessed"`
	IsSynced         *bool  `json:"isSynced"`
}

type statsPage struct {
	Title     string
	Global    *globalStats
	Snapshots []protocolSnapshot
	Indexer   *indexerStatus
}

// Stats renders globalStats, the latest snapshotRows hourly protocolSnapshots
// and indexerStatus.
func (p *Pages) Stats(ctx context.Context, w io.Writer) error {
	var global struct {
		GlobalStats *globalStats `json:"globalStats"`
	}
	if err := p.query(ctx, getGlobalStats, "GetGlobalStats", nil, &global); err != nil {
		return err
	}
	var snapshots struct {
		ProtocolSnapshots []protocolSnapshot `json:"protocolSnapshots"`
	}
	if err := p.query(ctx, getProtocolSnapshots, "GetProtocolSnapshots", map[string]any{"limit": snapshotRows}, &snapshots); err != nil {
		return err
	}
	var indexer struct {
		IndexerStatus *indexerStatus `json:"indexerStatus"`
	}
	if err := p.query(ctx, getIndexerStatus, "PortalIndexerStatus", nil, &indexer); err != nil {
		return err
	}
	return p.render(w, "stats", statsPage{Title: "Venue statistics", Global: global.GlobalStats,
		Snapshots: snapshots.ProtocolSnapshots, Indexer: indexer.IndexerStatus})
}

// NotFound renders message inside the layout.
func (p *Pages) NotFound(w io.Writer, message string) error {
	return p.render(w, "notfound", struct{ Title string }{Title: message})
}

// render executes page into a buffer first, so a template failure writes
// nothing to w.
func (p *Pages) render(w io.Writer, page string, data any) error {
	var buf bytes.Buffer
	if err := p.tmpl.ExecuteTemplate(&buf, page, data); err != nil {
		return fmt.Errorf("render %s: %w", page, err)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// get reads the gateway REST route path into out; a 404 is ErrNotFound.
func (p *Pages) get(ctx context.Context, path string, query url.Values, out any) error {
	target := p.gateway + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	return p.do(req, out)
}

// query runs one GraphQL operation on the gateway and decodes its data into
// out; an answer carrying errors fails the page.
func (p *Pages) query(ctx context.Context, document, operation string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": document, "operationName": operation, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.gateway+"/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := p.do(req, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("graphql %s: %s", operation, resp.Errors[0].Message)
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("graphql %s data: %w", operation, err)
	}
	return nil
}

func (p *Pages) do(req *http.Request, out any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayBody+1))
	if err != nil {
		return err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("gateway %s answered %d", req.URL.Path, resp.StatusCode)
	case len(raw) > maxGatewayBody:
		return fmt.Errorf("gateway %s answer exceeds %d bytes", req.URL.Path, maxGatewayBody)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("gateway %s answer: %w", req.URL.Path, err)
	}
	return nil
}

// txURL is the explorer page of transaction hash.
func (p *Pages) txURL(hash string) string { return p.explorer + url.PathEscape(hash) }

// fixed renders the signed integer string raw scaled down by 10^decimals
// without trailing zeros; a value that is not an integer renders as given.
func fixed(raw string, decimals int) string {
	v, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return raw
	}
	neg := v.Sign() < 0
	v.Abs(v)
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	whole, frac := new(big.Int).QuoRem(v, unit, new(big.Int))
	out := whole.String()
	if frac.Sign() != 0 {
		out += "." + strings.TrimRight(fmt.Sprintf("%0*s", decimals, frac.String()), "0")
	}
	if neg {
		out = "-" + out
	}
	return out
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func optnum(v *float64) string {
	if v == nil {
		return "-"
	}
	return num(*v)
}

func optint(v *int64) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatInt(*v, 10)
}

func optbool(v *bool) string {
	switch {
	case v == nil:
		return "unknown"
	case *v:
		return "yes"
	default:
		return "no"
	}
}

// when renders a time, or a nil time as "-", in UTC RFC 3339.
func when(v any) string {
	switch t := v.(type) {
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	case *time.Time:
		if t != nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return "-"
}

func side(long bool) string {
	if long {
		return "long"
	}
	return "short"
}
