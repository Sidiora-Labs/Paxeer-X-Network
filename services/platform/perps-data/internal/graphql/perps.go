package graphql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

// Paging bounds of contracts/data-services.md section 13.
const (
	defaultLimit = 50
	maxLimit     = 500
	maxOffset    = 100000
)

// headCursor is the perps.cursors row the indexer advances.
const headCursor = "head"

// BigDecimal is a canonical decimal integer string of raw event units.
type BigDecimal string

// ImplementsGraphQLType binds BigDecimal to the BigDecimal scalar.
func (BigDecimal) ImplementsGraphQLType(name string) bool { return name == "BigDecimal" }

// UnmarshalGraphQL accepts a canonical signed decimal integer string.
func (b *BigDecimal) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("BigDecimal: want a string, got %T", input)
	}
	if err := bigint.ValidateRawInt256(s); err != nil {
		return fmt.Errorf("BigDecimal: %w", err)
	}
	*b = BigDecimal(s)
	return nil
}

// DateTime is a UTC instant serialized as RFC 3339.
type DateTime struct{ time.Time }

// ImplementsGraphQLType binds DateTime to the DateTime scalar.
func (DateTime) ImplementsGraphQLType(name string) bool { return name == "DateTime" }

// UnmarshalGraphQL accepts an RFC 3339 string.
func (d *DateTime) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("DateTime: want a string, got %T", input)
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("DateTime: %w", err)
	}
	d.Time = t.UTC()
	return nil
}

// Scan reads a timestamptz column.
func (d *DateTime) Scan(src any) error {
	t, ok := src.(time.Time)
	if !ok {
		return fmt.Errorf("DateTime: cannot scan %T", src)
	}
	d.Time = t.UTC()
	return nil
}

type Position struct {
	PositionID       BigDecimal
	UserAddress      string
	MarketID         int32
	IsLong           bool
	SizeUsd          BigDecimal
	Leverage         BigDecimal
	EntryPrice       BigDecimal
	CollateralToken  *string
	CollateralAmount BigDecimal
	CollateralUsd    BigDecimal
	Status           string
	RealizedPnl      *BigDecimal
	ExitPrice        *BigDecimal
	OpenedAt         *DateTime
	ClosedAt         *DateTime
	OpenBlock        *int32
	CloseBlock       *int32
	OpenTxHash       *string
	CloseTxHash      *string
	markets          *marketLoader
}

// Market resolves through the loader shared by every position of the answer,
// so the markets of a position list cost one query.
func (p *Position) Market(ctx context.Context) (*Market, error) {
	return p.markets.get(ctx, p.MarketID)
}

type Market struct {
	MarketID    int32
	Name        string
	Symbol      string
	MaxLeverage BigDecimal
	Enabled     bool
	CreatedAt   *DateTime
	states      *marketStateLoader
}

// LatestPrice resolves through the loader shared by every market of the
// answer, one query per request.
func (m *Market) LatestPrice(ctx context.Context) (*LatestPrice, error) {
	s, err := m.states.get(ctx, m.MarketID)
	if err != nil || s == nil {
		return nil, err
	}
	return s.price, nil
}

func (m *Market) PoolState(ctx context.Context) (*PoolState, error) {
	s, err := m.states.get(ctx, m.MarketID)
	if err != nil || s == nil || s.price == nil {
		return nil, err
	}
	return s.price.poolState(), nil
}

func (m *Market) FundingRate(ctx context.Context) (*FundingRate, error) {
	s, err := m.states.get(ctx, m.MarketID)
	if err != nil || s == nil {
		return nil, err
	}
	return s.funding, nil
}

type Trade struct {
	ExecutionPrice *BigDecimal
	ID             int32
	PositionID     BigDecimal
	UserAddress    *string
	MarketID       *int32
	TradeType      string
	IsLong         *bool
	SizeUsd        BigDecimal
	Price          BigDecimal
	RealizedPnl    *BigDecimal
	FeeUsd         *BigDecimal
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type Order struct {
	RemainingSize  *BigDecimal
	BlockTimestamp *DateTime
	OrderID        BigDecimal
	UserAddress    string
	MarketID       int32
	OrderType      int32
	OrderTypeName  *string
	IsLong         bool
	TriggerPrice   BigDecimal
	SizeUsd        BigDecimal
	Status         string
	PositionID     *BigDecimal
	ExecutionPrice *BigDecimal
	FailureReason  *string
	PlacedAt       *DateTime
	ResolvedAt     *DateTime
	PlacedBlock    *int32
	ResolvedBlock  *int32
	PlacedTxHash   *string
	ResolvedTxHash *string
}

type Liquidation struct {
	SizeUsd        *BigDecimal
	ExecutionPrice *BigDecimal
	ID             int32
	PositionID     BigDecimal
	UserAddress    string
	MarketID       int32
	Price          BigDecimal
	Penalty        BigDecimal
	Keeper         string
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type PriceUpdate struct {
	UpdatedAt        *DateTime
	Timestamp        *int32
	ID               int32
	MarketID         int32
	Price            BigDecimal
	OnchainTimestamp int32
	BlockNumber      int32
	TxHash           string
	BlockTimestamp   DateTime
}

type LatestPrice struct {
	MarketID         int32
	Price            BigDecimal
	OnchainTimestamp int32
	BlockNumber      int32
	UpdatedAt        *DateTime
}

// poolState is the PoolState of the latest mark row: the order book venue
// has no reserves, and the oracle price is its index price.
func (l *LatestPrice) poolState() *PoolState {
	block := l.BlockNumber
	price := l.Price
	return &PoolState{MarketID: l.MarketID, BaseReserve: "0", QuoteReserve: "0", OraclePrice: &price, UpdatedAt: l.UpdatedAt, BlockNumber: &block}
}

type FundingRate struct {
	ID             int32
	MarketID       int32
	RatePerSecond  BigDecimal
	Rate24h        BigDecimal
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type UserVault struct {
	MarginMode   *int32
	UserAddress  string
	VaultAddress string
	CreatedAt    *DateTime
	BlockNumber  *int32
	TxHash       *string
}

type VaultEvent struct {
	ID             int32
	EventType      string
	UserAddress    *string
	TokenAddress   string
	Amount         BigDecimal
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp DateTime
}

type CollateralToken struct {
	TokenAddress string
	Decimals     int32
	IsActive     bool
	AddedAt      *DateTime
}

type PoolState struct {
	MarketID     int32
	BaseReserve  BigDecimal
	QuoteReserve BigDecimal
	OraclePrice  *BigDecimal
	UpdatedAt    *DateTime
	BlockNumber  *int32
}

type FeeConfig struct {
	TakerFeeBps       int32
	MakerFeeBps       int32
	LiquidationFeeBps int32
	InsuranceFeeBps   int32
	UpdatedAt         *DateTime
}

type ProtocolEvent struct {
	ID             int32
	EventName      string
	EventData      string
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp DateTime
}

type IndexerStatus struct {
	LastIndexedBlock int32
	ChainHead        *int32
	BlocksScanned    *int32
	EventsProcessed  *int32
	IsSynced         *bool
}

type KeeperCycle struct {
	ID                   int32
	OnchainTimestamp     int32
	MarketsUpdated       int32
	OrdersExecuted       int32
	LiquidationsExecuted int32
	OrdersFailed         int32
	LiquidationsFailed   int32
	BlockNumber          int32
	TxHash               string
	BlockTimestamp       DateTime
}

type AccountLedgerEntry struct {
	ID             int32
	EntryID        BigDecimal
	UserAddress    string
	EntryType      int32
	TokenAddress   string
	Amount         BigDecimal
	PositionID     BigDecimal
	IsDebit        bool
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type Delegate struct {
	ID              int32
	UserAddress     string
	DelegateAddress string
	CanTrade        bool
	CanWithdraw     bool
	CanModifyMargin bool
	Expiry          BigDecimal
	IsActive        bool
	BlockNumber     int32
	TxHash          string
	BlockTimestamp  DateTime
}

type TradingAccountEvent struct {
	ID             int32
	EventType      string
	UserAddress    string
	PositionID     *BigDecimal
	TokenAddress   *string
	Amount         *BigDecimal
	ExtraData      *string
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type Fee struct {
	ID             int32
	PositionID     BigDecimal
	UserAddress    string
	MarketID       int32
	FeeType        int32
	FeeTypeName    *string
	FeeUsd         BigDecimal
	FeeTokens      BigDecimal
	TokenAddress   string
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp DateTime
}

type TradeSettlement struct {
	ID              int32
	PositionID      BigDecimal
	UserAddress     string
	MarketID        int32
	TradeType       int32
	TradeTypeName   *string
	SizeUsd         BigDecimal
	ExecutionPrice  BigDecimal
	GrossPnl        BigDecimal
	TotalFeesUsd    BigDecimal
	BorrowingFeeUsd BigDecimal
	FundingPaidUsd  BigDecimal
	NetPayoutTokens BigDecimal
	BlockNumber     int32
	TxHash          string
	BlockTimestamp  DateTime
}

type OiSnapshot struct {
	ID             int32
	MarketID       int32
	LongOi         BigDecimal
	ShortOi        BigDecimal
	DeltaUsd       BigDecimal
	IsIncrease     bool
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type MarkPriceRecord struct {
	ID             int32
	MarketID       int32
	MarkPrice      BigDecimal
	IndexPrice     BigDecimal
	BaseReserve    BigDecimal
	QuoteReserve   BigDecimal
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type VaultBalanceRecord struct {
	ID             int32
	TokenAddress   string
	VaultType      int32
	VaultTypeName  *string
	NewBalance     BigDecimal
	Delta          BigDecimal
	IsIncrease     bool
	BlockNumber    int32
	TxHash         string
	BlockTimestamp DateTime
}

type FundingPayment struct {
	ID                  int32
	PositionID          BigDecimal
	UserAddress         string
	MarketID            int32
	FundingPaymentUsd   BigDecimal
	NewCollateralUsd    BigDecimal
	NewCollateralAmount BigDecimal
	BlockNumber         int32
	TxHash              string
	BlockTimestamp      DateTime
}

type MarketSnapshotRecord struct {
	ID                   int32
	MarketID             int32
	LongOi               *BigDecimal
	ShortOi              *BigDecimal
	MarkPrice            BigDecimal
	IndexPrice           *BigDecimal
	FundingRatePerSecond *BigDecimal
	FundingRate24h       *BigDecimal
	Volume24hUsd         BigDecimal
	OnchainTimestamp     int32
	BlockNumber          int32
	BlockTimestamp       DateTime
}

type ProtocolSnapshotRecord struct {
	ID                 int32
	TotalPositions     BigDecimal
	TotalOpenPositions BigDecimal
	TotalMarkets       BigDecimal
	TvlUsd             BigDecimal
	InsuranceTotalUsd  BigDecimal
	OnchainTimestamp   int32
	BlockNumber        int32
	BlockTimestamp     DateTime
}

type UserStats struct {
	Address             *string
	TotalPnl            *BigDecimal
	TotalVolume         *BigDecimal
	TotalFeesPaid       *BigDecimal
	WinCount            *int32
	LossCount           *int32
	LiquidationCount    *int32
	UserAddress         string
	TotalPositions      int32
	OpenPositions       int32
	ClosedPositions     int32
	LiquidatedPositions int32
	TotalTrades         int32
	TotalRealizedPnl    BigDecimal
	TotalOrders         int32
	ActiveOrders        int32
}

type MarketStats struct {
	MarketID          int32
	Symbol            *string
	TotalPositions    int32
	OpenPositions     int32
	TotalTrades       int32
	TotalLiquidations int32
	TotalVolume       BigDecimal
	LatestPrice       *BigDecimal
	LatestFundingRate *BigDecimal
}

type GlobalStats struct {
	TotalMarkets      int32
	TotalPositions    int32
	OpenPositions     int32
	TotalTrades       int32
	TotalLiquidations int32
	TotalVolume       BigDecimal
	TotalUsers        int32
	IndexerBlock      int32
}

type OrderlyTicker struct {
	Symbol                string
	PaxeerSymbol          string
	MarkPrice             *float64
	IndexPrice            *float64
	SumUnitaryFunding     *float64
	LastFundingRateHourly *string
	LastFundingRate       *float64
	NextFundingTime       *float64
	OpenInterest          *float64
	H24Open               *float64
	H24Close              *float64
	H24High               *float64
	H24Low                *float64
	H24Amount             *float64
	H24Volume             *float64
}

type OrderlyVolumeStats struct {
	PerpVolumeYtd        *float64
	PerpVolumeLtd        *float64
	PerpVolumeToday      *float64
	PerpVolumeLast1Day   *float64
	PerpVolumeLast7Days  *float64
	PerpVolumeLast30Days *float64
}

type OrderlyPriceChange struct {
	Symbol       string
	PaxeerSymbol string
	LastPrice    *float64
	Change5m     *float64
	Change30m    *float64
	Change1h     *float64
	Change4h     *float64
	Change24h    *float64
	Change3d     *float64
	Change7d     *float64
	Change30d    *float64
}

type OrderlyOpenInterest struct {
	Symbol       string
	PaxeerSymbol string
	LongOi       *float64
	ShortOi      *float64
}

type OrderlyFundingRate struct {
	Symbol                   string
	PaxeerSymbol             string
	LastFundingRateHourly    *string
	LastFundingRate          *float64
	LastFundingRateTimestamp *float64
	NextFundingTime          *float64
	SumUnitaryFunding        *float64
}

type EnrichedGlobalStats struct {
	Onchain *GlobalStats
	Orderly *OrderlyVolumeStats
}

type EnrichedMarketStats struct {
	Onchain      *MarketStats
	Ticker       *OrderlyTicker
	FundingRate  *OrderlyFundingRate
	OpenInterest *OrderlyOpenInterest
	PriceChange  *OrderlyPriceChange
}

type LeaderboardEntry struct {
	Rank             *int32
	UserAddress      *string
	TotalRealizedPnl *BigDecimal
	TotalTrades      *int32
	WinRate          *float64
	AvgHoldTime      *float64
	OpenPositions    *int32
	TotalVolume      *BigDecimal
}

// Enumerations of contracts/perps.md section 2.
var (
	tradeTypeNames = []string{"open", "close", "liquidation", "reduce", "increase", "adl", "backstop"}
	orderTypeNames = []string{"limit", "stop_limit", "take_profit", "stop_loss", "market", "stop_market"}
)

// nameCase renders the SQL CASE naming column by the index of names.
func nameCase(column string, names []string) string {
	var b strings.Builder
	b.WriteString("CASE " + column)
	for i, n := range names {
		fmt.Fprintf(&b, " WHEN %d THEN '%s'", i, n)
	}
	b.WriteString(" END")
	return b.String()
}

// feeTypeNameSQL names the emitted feeType values: TRADING 0, MAKER_REBATE 4.
const feeTypeNameSQL = "CASE f.fee_type WHEN 0 THEN 'trading' WHEN 4 THEN 'maker_rebate' END"

// hexSQL renders a bytea column as lower-case 0x hex.
func hexSQL(column string) string { return "'0x' || encode(" + column + ", 'hex')" }

const newest = "block DESC, tx_index DESC, log_index DESC, sub DESC"

// paging clamps limit to [1, 500] with default 50 and offset to [0, 100000].
func paging(limit, offset *int32) (int64, int64) {
	l, o := int64(defaultLimit), int64(0)
	if limit != nil {
		l = min(max(int64(*limit), 1), maxLimit)
	}
	if offset != nil {
		o = min(max(int64(*offset), 0), maxOffset)
	}
	return l, o
}

// addressArg parses an optional address argument into its 20 bytes; hex
// case does not matter.
func addressArg(name string, s *string) (any, error) {
	if s == nil {
		return nil, nil
	}
	if !common.IsHexAddress(*s) {
		return nil, fmt.Errorf("invalid %s %q", name, *s)
	}
	return common.HexToAddress(*s).Bytes(), nil
}

// idArg parses an optional uint256 id argument.
func idArg(name string, s *string) (any, error) {
	if s == nil {
		return nil, nil
	}
	if err := bigint.ValidateRaw(*s); err != nil {
		return nil, fmt.Errorf("invalid %s %q", name, *s)
	}
	return *s, nil
}

func intArg(v *int32) any {
	if v == nil {
		return nil
	}
	return int64(*v)
}

func textArg(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// collect runs query and scans every row into a new T.
func collect[T any](ctx context.Context, db *sql.DB, root, query string, args []any, dest func(*T) []any) ([]*T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", root, err)
	}
	defer rows.Close()
	out := []*T{}
	for rows.Next() {
		v := new(T)
		if err := rows.Scan(dest(v)...); err != nil {
			return nil, fmt.Errorf("%s: %w", root, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", root, err)
	}
	return out, nil
}

// first runs query and answers its first row, or nil when it has none.
func first[T any](ctx context.Context, db *sql.DB, root, query string, args []any, dest func(*T) []any) (*T, error) {
	list, err := collect(ctx, db, root, query, args, dest)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

var positionSQL = `SELECT p.position_id::text, ` + hexSQL("p.account") + `, p.market_id, p.is_long, p.size_usd::text, p.leverage::text,
    p.entry_price::text, ` + hexSQL("p.collateral_token") + `, p.collateral_amount::text, p.collateral_usd::text, p.status,
    p.realized_pnl::text, c.exit_price::text, o.block_time, c.block_time, p.opened_block, p.closed_block,
    ` + hexSQL("o.tx_hash") + `, ` + hexSQL("c.tx_hash") + `
FROM perps.positions_current p
LEFT JOIN LATERAL (SELECT e.block_time, e.tx_hash FROM perps.position_events e
    WHERE e.position_id = p.position_id AND e.kind = 'opened' AND e.block = p.opened_block
    ORDER BY e.tx_index DESC, e.log_index DESC, e.sub DESC LIMIT 1) o ON true
LEFT JOIN LATERAL (SELECT e.block_time, e.tx_hash, e.exit_price FROM perps.position_events e
    WHERE p.closed_block IS NOT NULL AND e.position_id = p.position_id AND e.kind = 'closed' AND e.is_full_close
        AND e.block = p.closed_block
    ORDER BY e.tx_index DESC, e.log_index DESC, e.sub DESC LIMIT 1) c ON true
`

func positionDest(p *Position) []any {
	return []any{&p.PositionID, &p.UserAddress, &p.MarketID, &p.IsLong, &p.SizeUsd, &p.Leverage, &p.EntryPrice,
		&p.CollateralToken, &p.CollateralAmount, &p.CollateralUsd, &p.Status, &p.RealizedPnl, &p.ExitPrice,
		&p.OpenedAt, &p.ClosedAt, &p.OpenBlock, &p.CloseBlock, &p.OpenTxHash, &p.CloseTxHash}
}

// withMarkets attaches one market loader to every position of list.
func (r *Resolver) withMarkets(list []*Position) []*Position {
	ids := make([]int32, len(list))
	for i, p := range list {
		ids[i] = p.MarketID
	}
	l := &marketLoader{r: r, ids: ids}
	for _, p := range list {
		p.markets = l
	}
	return list
}

func (r *Resolver) Position(ctx context.Context, args struct{ PositionID string }) (*Position, error) {
	id, err := idArg("positionId", &args.PositionID)
	if err != nil {
		return nil, err
	}
	p, err := first(ctx, r.DB, "position", positionSQL+"WHERE p.position_id = $1::numeric", []any{id}, positionDest)
	if err != nil || p == nil {
		return nil, err
	}
	return r.withMarkets([]*Position{p})[0], nil
}

func (r *Resolver) Positions(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	Status      *string
	Limit       *int32
	Offset      *int32
}) ([]*Position, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	list, err := collect(ctx, r.DB, "positions", positionSQL+`WHERE ($1::bytea IS NULL OR p.account = $1::bytea)
    AND ($2::numeric IS NULL OR p.market_id = $2::numeric) AND ($3::text IS NULL OR p.status = $3::text)
ORDER BY p.opened_block DESC, p.position_id DESC LIMIT $4 OFFSET $5`,
		[]any{user, intArg(args.MarketID), textArg(args.Status), limit, offset}, positionDest)
	if err != nil {
		return nil, err
	}
	return r.withMarkets(list), nil
}

// tradeTypeSQL names a trade type, or answers the emitted value of a type
// outside the enumeration.
var tradeTypeSQL = "COALESCE(" + nameCase("t.trade_type", tradeTypeNames) + ", t.trade_type::text)"

func (r *Resolver) Trades(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	PositionID  *string
	TradeType   *string
	Limit       *int32
	Offset      *int32
}) ([]*Trade, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	position, err := idArg("positionId", args.PositionID)
	if err != nil {
		return nil, err
	}
	var tradeType any
	if args.TradeType != nil {
		i := indexOf(tradeTypeNames, *args.TradeType)
		if i < 0 {
			return []*Trade{}, nil
		}
		tradeType = int64(i)
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "trades", `SELECT t.execution_price::text, t.row_id, t.position_id::text, `+hexSQL("t.account")+`,
    t.market_id, `+tradeTypeSQL+`, p.is_long, t.size_usd::text, t.execution_price::text, t.gross_pnl::text,
    t.total_fees_usd::text, t.block, `+hexSQL("t.tx_hash")+`, t.block_time
FROM perps.trade_settlements t LEFT JOIN perps.positions_current p ON p.position_id = t.position_id
WHERE ($1::bytea IS NULL OR t.account = $1::bytea) AND ($2::numeric IS NULL OR t.market_id = $2::numeric)
    AND ($3::numeric IS NULL OR t.position_id = $3::numeric) AND ($4::smallint IS NULL OR t.trade_type = $4::smallint)
ORDER BY t.block DESC, t.tx_index DESC, t.log_index DESC, t.sub DESC LIMIT $5 OFFSET $6`,
		[]any{user, intArg(args.MarketID), position, tradeType, limit, offset},
		func(t *Trade) []any {
			return []any{&t.ExecutionPrice, &t.ID, &t.PositionID, &t.UserAddress, &t.MarketID, &t.TradeType, &t.IsLong,
				&t.SizeUsd, &t.Price, &t.RealizedPnl, &t.FeeUsd, &t.BlockNumber, &t.TxHash, &t.BlockTimestamp}
		})
}

func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// orderSQL reads perps.orders with the placing event and the event that
// closed the order: a cancel, expiry or execution, or the last fill naming
// the order in its closing block. failureReason is always null.
var orderSQL = `SELECT o.remaining_size_usd::text, COALESCE(rs.block_time, pl.block_time), o.order_id::text,
    ` + hexSQL("o.account") + `, o.market_id, o.order_type, ` + nameCase("o.order_type", orderTypeNames) + `, o.is_long,
    o.trigger_price::text, o.size_usd::text, o.status, o.position_id::text, o.execution_price::text, NULL::text,
    pl.block_time, rs.block_time, o.placed_block, o.closed_block, ` + hexSQL("pl.tx_hash") + `, ` + hexSQL("rs.tx_hash") + `
FROM perps.orders o
LEFT JOIN LATERAL (SELECT e.block_time, e.tx_hash FROM perps.order_events e
    WHERE e.order_id = o.order_id AND e.kind = 'placed' AND e.block = o.placed_block
    ORDER BY e.tx_index, e.log_index, e.sub LIMIT 1) pl ON true
LEFT JOIN LATERAL (SELECT u.block_time, u.tx_hash FROM (
        SELECT e.block_time, e.tx_hash, e.tx_index, e.log_index, e.sub FROM perps.order_events e
        WHERE e.order_id = o.order_id AND e.block = o.closed_block AND e.kind IN ('cancelled', 'expired', 'executed')
        UNION ALL
        SELECT f.block_time, f.tx_hash, f.tx_index, f.log_index, f.sub FROM perps.fills f
        WHERE f.block = o.closed_block AND (f.maker_order_id = o.order_id OR f.taker_order_id = o.order_id)
    ) u ORDER BY u.tx_index DESC, u.log_index DESC, u.sub DESC LIMIT 1) rs ON o.closed_block IS NOT NULL
`

func orderDest(o *Order) []any {
	return []any{&o.RemainingSize, &o.BlockTimestamp, &o.OrderID, &o.UserAddress, &o.MarketID, &o.OrderType, &o.OrderTypeName,
		&o.IsLong, &o.TriggerPrice, &o.SizeUsd, &o.Status, &o.PositionID, &o.ExecutionPrice, &o.FailureReason,
		&o.PlacedAt, &o.ResolvedAt, &o.PlacedBlock, &o.ResolvedBlock, &o.PlacedTxHash, &o.ResolvedTxHash}
}

func (r *Resolver) Order(ctx context.Context, args struct{ OrderID string }) (*Order, error) {
	id, err := idArg("orderId", &args.OrderID)
	if err != nil {
		return nil, err
	}
	return first(ctx, r.DB, "order", orderSQL+"WHERE o.order_id = $1::numeric", []any{id}, orderDest)
}

func (r *Resolver) Orders(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	Status      *string
	OrderType   *int32
	Limit       *int32
	Offset      *int32
}) ([]*Order, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "orders", orderSQL+`WHERE ($1::bytea IS NULL OR o.account = $1::bytea)
    AND ($2::numeric IS NULL OR o.market_id = $2::numeric) AND ($3::text IS NULL OR o.status = $3::text)
    AND ($4::integer IS NULL OR o.order_type = $4::integer)
ORDER BY o.placed_block DESC, o.order_id DESC LIMIT $5 OFFSET $6`,
		[]any{user, intArg(args.MarketID), textArg(args.Status), intArg(args.OrderType), limit, offset}, orderDest)
}

func (r *Resolver) Liquidations(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	Limit       *int32
	Offset      *int32
}) ([]*Liquidation, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "liquidations", `SELECT c.closed_size_usd::text, c.exit_price::text, l.row_id, l.position_id::text,
    `+hexSQL("l.account")+`, l.market_id, l.liquidation_price::text, l.penalty::text, `+hexSQL("l.keeper")+`, l.block,
    `+hexSQL("l.tx_hash")+`, l.block_time
FROM perps.liquidations l
LEFT JOIN LATERAL (SELECT e.closed_size_usd, e.exit_price FROM perps.position_events e
    WHERE e.kind = 'closed' AND e.position_id = l.position_id AND e.tx_hash = l.tx_hash
    ORDER BY e.log_index DESC, e.sub DESC LIMIT 1) c ON true
WHERE ($1::bytea IS NULL OR l.account = $1::bytea) AND ($2::numeric IS NULL OR l.market_id = $2::numeric)
ORDER BY l.block DESC, l.tx_index DESC, l.log_index DESC, l.sub DESC LIMIT $3 OFFSET $4`,
		[]any{user, intArg(args.MarketID), limit, offset},
		func(l *Liquidation) []any {
			return []any{&l.SizeUsd, &l.ExecutionPrice, &l.ID, &l.PositionID, &l.UserAddress, &l.MarketID, &l.Price,
				&l.Penalty, &l.Keeper, &l.BlockNumber, &l.TxHash, &l.BlockTimestamp}
		})
}

const marketSQL = `SELECT market_id, name, symbol, max_leverage::text, enabled, NULL::timestamptz FROM perps.markets `

func marketDest(m *Market) []any {
	return []any{&m.MarketID, &m.Name, &m.Symbol, &m.MaxLeverage, &m.Enabled, &m.CreatedAt}
}

// withStates attaches one market state loader to every market of list.
func (r *Resolver) withStates(list []*Market) []*Market {
	ids := make([]int32, len(list))
	for i, m := range list {
		ids[i] = m.MarketID
	}
	l := &marketStateLoader{r: r, ids: ids}
	for _, m := range list {
		m.states = l
	}
	return list
}

func (r *Resolver) Market(ctx context.Context, args struct{ MarketID int32 }) (*Market, error) {
	m, err := first(ctx, r.DB, "market", marketSQL+"WHERE market_id = $1::numeric", []any{int64(args.MarketID)}, marketDest)
	if err != nil || m == nil {
		return nil, err
	}
	return r.withStates([]*Market{m})[0], nil
}

func (r *Resolver) Markets(ctx context.Context) ([]*Market, error) {
	list, err := collect(ctx, r.DB, "markets", marketSQL+"ORDER BY market_id", nil, marketDest)
	if err != nil {
		return nil, err
	}
	return r.withStates(list), nil
}

// marketLoader loads the markets of one answer's positions in one query.
type marketLoader struct {
	r    *Resolver
	ids  []int32
	once sync.Once
	byID map[int32]*Market
	err  error
}

func (l *marketLoader) get(ctx context.Context, id int32) (*Market, error) {
	l.once.Do(func() {
		ids := distinct(l.ids)
		var list []*Market
		list, l.err = collect(ctx, l.r.DB, "position market", marketSQL+"WHERE market_id = ANY($1::numeric[]) ORDER BY market_id",
			[]any{pq.Array(ids)}, marketDest)
		if l.err != nil {
			return
		}
		l.byID = make(map[int32]*Market, len(list))
		for _, m := range l.r.withStates(list) {
			l.byID[m.MarketID] = m
		}
	})
	if l.err != nil {
		return nil, l.err
	}
	return l.byID[id], nil
}

func distinct(ids []int32) []int64 {
	seen := map[int32]bool{}
	out := []int64{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, int64(id))
		}
	}
	return out
}

// marketState is the latest mark row and the latest funding rate of a market.
type marketState struct {
	price   *LatestPrice
	funding *FundingRate
}

// marketStateLoader loads the states of one answer's markets in one query.
type marketStateLoader struct {
	r    *Resolver
	ids  []int32
	once sync.Once
	byID map[int32]*marketState
	err  error
}

var marketStateSQL = `SELECT m.id, mp.index_price::text, floor(extract(epoch FROM mp.block_time))::bigint, mp.block, mp.block_time,
    fr.row_id, fr.new_rate_per_second::text, fr.funding_rate24h::text, fr.block, ` + hexSQL("fr.tx_hash") + `, fr.block_time
FROM unnest($1::bigint[]) AS m(id)
LEFT JOIN LATERAL (SELECT index_price, block, block_time FROM perps.mark_prices
    WHERE market_id = m.id::numeric ORDER BY ` + newest + ` LIMIT 1) mp ON true
LEFT JOIN LATERAL (SELECT row_id, new_rate_per_second, funding_rate24h, block, tx_hash, block_time FROM perps.funding_rates
    WHERE market_id = m.id::numeric ORDER BY ` + newest + ` LIMIT 1) fr ON true`

func (l *marketStateLoader) get(ctx context.Context, id int32) (*marketState, error) {
	l.once.Do(func() { l.byID, l.err = l.load(ctx) })
	if l.err != nil {
		return nil, l.err
	}
	return l.byID[id], nil
}

func (l *marketStateLoader) load(ctx context.Context) (map[int32]*marketState, error) {
	rows, err := l.r.DB.QueryContext(ctx, marketStateSQL, pq.Array(distinct(l.ids)))
	if err != nil {
		return nil, fmt.Errorf("market state: %w", err)
	}
	defer rows.Close()
	out := map[int32]*marketState{}
	for rows.Next() {
		var (
			id                    int32
			price                 sql.NullString
			epoch, block          sql.NullInt32
			at                    sql.NullTime
			rowID, frBlock        sql.NullInt32
			rate, rate24h, txHash sql.NullString
			frAt                  sql.NullTime
		)
		if err := rows.Scan(&id, &price, &epoch, &block, &at, &rowID, &rate, &rate24h, &frBlock, &txHash, &frAt); err != nil {
			return nil, fmt.Errorf("market state: %w", err)
		}
		s := &marketState{}
		if price.Valid {
			updated := DateTime{at.Time.UTC()}
			s.price = &LatestPrice{MarketID: id, Price: BigDecimal(price.String), OnchainTimestamp: epoch.Int32,
				BlockNumber: block.Int32, UpdatedAt: &updated}
		}
		if rowID.Valid {
			s.funding = &FundingRate{ID: rowID.Int32, MarketID: id, RatePerSecond: BigDecimal(rate.String),
				Rate24h: BigDecimal(rate24h.String), BlockNumber: frBlock.Int32, TxHash: txHash.String,
				BlockTimestamp: DateTime{frAt.Time.UTC()}}
		}
		out[id] = s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("market state: %w", err)
	}
	return out, nil
}

// latestMarkSQL answers the latest perps.mark_prices row of every market.
const latestMarkSQL = `SELECT DISTINCT ON (market_id) market_id, index_price::text, floor(extract(epoch FROM block_time))::bigint,
    block, block_time FROM perps.mark_prices `

func latestPriceDest(l *LatestPrice) []any {
	return []any{&l.MarketID, &l.Price, &l.OnchainTimestamp, &l.BlockNumber, &l.UpdatedAt}
}

func (r *Resolver) LatestPrices(ctx context.Context) ([]*LatestPrice, error) {
	return collect(ctx, r.DB, "latestPrices", latestMarkSQL+"ORDER BY market_id, "+newest, nil, latestPriceDest)
}

func (r *Resolver) PriceHistory(ctx context.Context, args struct {
	MarketID int32
	Limit    *int32
	Offset   *int32
}) ([]*PriceUpdate, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "priceHistory", `SELECT block_time, floor(timestamp_ms / 1000)::bigint, row_id, market_id,
    index_price::text, floor(timestamp_ms / 1000)::bigint, block, `+hexSQL("block_hash")+`, block_time
FROM perps.index_prices
WHERE kind = 'aggregate' AND market_id = $1::numeric AND index_price IS NOT NULL AND timestamp_ms IS NOT NULL
ORDER BY `+newest+` LIMIT $2 OFFSET $3`,
		[]any{int64(args.MarketID), limit, offset},
		func(p *PriceUpdate) []any {
			return []any{&p.UpdatedAt, &p.Timestamp, &p.ID, &p.MarketID, &p.Price, &p.OnchainTimestamp, &p.BlockNumber,
				&p.TxHash, &p.BlockTimestamp}
		})
}

func (r *Resolver) FundingRates(ctx context.Context, args struct {
	MarketID int32
	Limit    *int32
	Offset   *int32
}) ([]*FundingRate, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "fundingRates", `SELECT row_id, market_id, new_rate_per_second::text, funding_rate24h::text, block,
    `+hexSQL("tx_hash")+`, block_time
FROM perps.funding_rates WHERE market_id = $1::numeric ORDER BY `+newest+` LIMIT $2 OFFSET $3`,
		[]any{int64(args.MarketID), limit, offset},
		func(f *FundingRate) []any {
			return []any{&f.ID, &f.MarketID, &f.RatePerSecond, &f.Rate24h, &f.BlockNumber, &f.TxHash, &f.BlockTimestamp}
		})
}

func (r *Resolver) UserVault(ctx context.Context, args struct{ UserAddress string }) (*UserVault, error) {
	user, err := addressArg("userAddress", &args.UserAddress)
	if err != nil {
		return nil, err
	}
	return first(ctx, r.DB, "userVault", `SELECT m.margin_mode, `+hexSQL(`v."user"`)+`, `+hexSQL("v.vault")+`, v.block_time, v.block,
    `+hexSQL("v.tx_hash")+`
FROM perps.vaults v LEFT JOIN perps.account_margin m ON m.account = v."user" AND m.token = $2
WHERE v."user" = $1`,
		[]any{user, store.USDL.Bytes()},
		func(v *UserVault) []any {
			return []any{&v.MarginMode, &v.UserAddress, &v.VaultAddress, &v.CreatedAt, &v.BlockNumber, &v.TxHash}
		})
}

func (r *Resolver) VaultEvents(ctx context.Context, args struct {
	UserAddress *string
	EventType   *string
	Limit       *int32
	Offset      *int32
}) ([]*VaultEvent, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "vaultEvents", `SELECT row_id, kind, `+hexSQL("account")+`, `+hexSQL("$1::bytea")+`, amount::text, block,
    `+hexSQL("tx_hash")+`, log_index, block_time
FROM perps.collateral_events
WHERE ($2::bytea IS NULL OR account = $2::bytea) AND ($3::text IS NULL OR kind = $3::text)
ORDER BY `+newest+` LIMIT $4 OFFSET $5`,
		[]any{store.USDL.Bytes(), user, textArg(args.EventType), limit, offset},
		func(v *VaultEvent) []any {
			return []any{&v.ID, &v.EventType, &v.UserAddress, &v.TokenAddress, &v.Amount, &v.BlockNumber, &v.TxHash,
				&v.LogIndex, &v.BlockTimestamp}
		})
}

// CollateralTokens answers the one collateral token, USDL with 6 decimals,
// added at the activation block.
func (r *Resolver) CollateralTokens(ctx context.Context) ([]*CollateralToken, error) {
	return collect(ctx, r.DB, "collateralTokens", `SELECT `+hexSQL("$1::bytea")+`, 6, true,
    (SELECT block_time FROM perps.protocol_events WHERE receipt_kind = 'activation' ORDER BY block, tx_index, log_index, sub LIMIT 1)`,
		[]any{store.USDL.Bytes()},
		func(c *CollateralToken) []any { return []any{&c.TokenAddress, &c.Decimals, &c.IsActive, &c.AddedAt} })
}

func (r *Resolver) PoolStates(ctx context.Context) ([]*PoolState, error) {
	list, err := r.LatestPrices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*PoolState, len(list))
	for i, l := range list {
		out[i] = l.poolState()
	}
	return out, nil
}

func (r *Resolver) PoolState(ctx context.Context, args struct{ MarketID int32 }) (*PoolState, error) {
	l, err := first(ctx, r.DB, "poolState", latestMarkSQL+"WHERE market_id = $1::numeric ORDER BY market_id, "+newest,
		[]any{int64(args.MarketID)}, latestPriceDest)
	if err != nil || l == nil {
		return nil, err
	}
	return l.poolState(), nil
}

// FeeConfig answers each fee of perps.fee_config in basis points, the ppm
// value / 100 rounded down.
func (r *Resolver) FeeConfig(ctx context.Context) (*FeeConfig, error) {
	return first(ctx, r.DB, "feeConfig", `SELECT floor(f.taker_fee_ppm / 100)::bigint, floor(f.maker_fee_ppm / 100)::bigint,
    floor(f.liquidation_fee_ppm / 100)::bigint, floor(f.insurance_share_ppm / 100)::bigint, b.time
FROM perps.fee_config f LEFT JOIN perps.blocks b ON b.number = f.refreshed_block WHERE f.id = 1`, nil,
		func(f *FeeConfig) []any {
			return []any{&f.TakerFeeBps, &f.MakerFeeBps, &f.LiquidationFeeBps, &f.InsuranceFeeBps, &f.UpdatedAt}
		})
}

func (r *Resolver) ProtocolEvents(ctx context.Context, args struct {
	EventName *string
	Limit     *int32
	Offset    *int32
}) ([]*ProtocolEvent, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "protocolEvents", `SELECT row_id, event_name, args::text, block, `+hexSQL("tx_hash")+`, log_index, block_time
FROM perps.protocol_events WHERE ($1::text IS NULL OR event_name = $1::text) ORDER BY `+newest+` LIMIT $2 OFFSET $3`,
		[]any{textArg(args.EventName), limit, offset},
		func(e *ProtocolEvent) []any {
			return []any{&e.ID, &e.EventName, &e.EventData, &e.BlockNumber, &e.TxHash, &e.LogIndex, &e.BlockTimestamp}
		})
}

const userStatsSQL = `SELECT s.account IS NOT NULL, COALESCE(s.total_pnl, 0)::text, COALESCE(s.volume_usd, 0)::text,
    COALESCE(s.fees_usd, 0)::text, COALESCE(s.win_count, 0), COALESCE(s.loss_count, 0), COALESCE(s.liquidations, 0),
    p.total, p.open, p.closed, p.liquidated, COALESCE(s.trades_count, 0), COALESCE(s.realized_pnl, 0)::text, o.total, o.active
FROM (SELECT $1::bytea AS account) a
LEFT JOIN perps.user_stats s ON s.account = a.account
CROSS JOIN LATERAL (SELECT count(*) AS total, count(*) FILTER (WHERE status = 'open') AS open,
    count(*) FILTER (WHERE status = 'closed') AS closed, count(*) FILTER (WHERE status = 'liquidated') AS liquidated
    FROM perps.positions_current WHERE account = a.account) p
CROSS JOIN LATERAL (SELECT count(*) AS total, count(*) FILTER (WHERE status = 'active') AS active
    FROM perps.orders WHERE account = a.account) o`

// UserStats takes exactly one of userAddress and address and answers null for
// an account with no statistics, positions or orders.
func (r *Resolver) UserStats(ctx context.Context, args struct {
	UserAddress *string
	Address     *string
}) (*UserStats, error) {
	if (args.UserAddress == nil) == (args.Address == nil) {
		return nil, errors.New("exactly one of userAddress or address is required")
	}
	raw := args.UserAddress
	if raw == nil {
		raw = args.Address
	}
	if !common.IsHexAddress(*raw) {
		return nil, errors.New("invalid address")
	}
	user := common.HexToAddress(*raw).Bytes()
	var (
		s                          UserStats
		found                      bool
		pnl, volume, fees          BigDecimal
		wins, losses, liquidations int32
	)
	err := r.DB.QueryRowContext(ctx, userStatsSQL, user).Scan(&found, &pnl, &volume, &fees, &wins, &losses, &liquidations,
		&s.TotalPositions, &s.OpenPositions, &s.ClosedPositions, &s.LiquidatedPositions, &s.TotalTrades, &s.TotalRealizedPnl,
		&s.TotalOrders, &s.ActiveOrders)
	if err != nil {
		return nil, fmt.Errorf("userStats: %w", err)
	}
	if !found && s.TotalPositions == 0 && s.TotalOrders == 0 {
		return nil, nil
	}
	address := strings.ToLower(common.BytesToAddress(user).Hex())
	s.Address, s.UserAddress = &address, address
	s.TotalPnl, s.TotalVolume, s.TotalFeesPaid = &pnl, &volume, &fees
	s.WinCount, s.LossCount, s.LiquidationCount = &wins, &losses, &liquidations
	return &s, nil
}

func marketStatsDest(m *MarketStats) []any {
	return []any{&m.MarketID, &m.Symbol, &m.TotalPositions, &m.OpenPositions, &m.TotalTrades, &m.TotalLiquidations,
		&m.TotalVolume, &m.LatestPrice, &m.LatestFundingRate}
}

// MarketStats answers the counts of a market, its fill count from
// market_stats_rolling, its total fill volume, its last fill price and its
// latest funding rate per second.
func (r *Resolver) MarketStats(ctx context.Context, args struct{ MarketID int32 }) (*MarketStats, error) {
	return first(ctx, r.DB, "marketStats", `SELECT m.market_id, m.symbol,
    (SELECT count(*) FROM perps.positions_current p WHERE p.market_id = m.market_id),
    (SELECT count(*) FROM perps.positions_current p WHERE p.market_id = m.market_id AND p.status = 'open'),
    COALESCE(s.trades_count, 0),
    (SELECT count(*) FROM perps.liquidations l WHERE l.market_id = m.market_id),
    (SELECT COALESCE(sum(f.size_usd), 0)::text FROM perps.fills f WHERE f.market_id = m.market_id),
    s.last_price::text,
    (SELECT fr.new_rate_per_second::text FROM perps.funding_rates fr WHERE fr.market_id = m.market_id
        ORDER BY fr.block DESC, fr.tx_index DESC, fr.log_index DESC, fr.sub DESC LIMIT 1)
FROM perps.markets m LEFT JOIN perps.market_stats_rolling s ON s.market_id = m.market_id
WHERE m.market_id = $1::numeric`, []any{int64(args.MarketID)}, marketStatsDest)
}

const globalStatsSQL = `SELECT (SELECT count(*) FROM perps.markets), (SELECT count(*) FROM perps.positions_current),
    (SELECT count(*) FROM perps.positions_current WHERE status = 'open'),
    (SELECT COALESCE(sum(trades_count), 0) FROM perps.market_stats_rolling),
    (SELECT count(*) FROM perps.liquidations), (SELECT COALESCE(sum(size_usd), 0)::text FROM perps.fills),
    (SELECT count(DISTINCT account) FROM perps.positions_current),
    COALESCE((SELECT height FROM perps.cursors WHERE name = $1), 0)`

func (r *Resolver) GlobalStats(ctx context.Context) (*GlobalStats, error) {
	return first(ctx, r.DB, "globalStats", globalStatsSQL, []any{headCursor},
		func(g *GlobalStats) []any {
			return []any{&g.TotalMarkets, &g.TotalPositions, &g.OpenPositions, &g.TotalTrades, &g.TotalLiquidations,
				&g.TotalVolume, &g.TotalUsers, &g.IndexerBlock}
		})
}

// IndexerStatus answers the head cursor with the stored block and event
// counts; the chain head and sync state are not known to the database.
func (r *Resolver) IndexerStatus(ctx context.Context) (*IndexerStatus, error) {
	return first(ctx, r.DB, "indexerStatus", `SELECT c.height, NULL::integer, (SELECT count(*) FROM perps.blocks),
    (SELECT count(*) FROM perps.protocol_events), NULL::boolean
FROM perps.cursors c WHERE c.name = $1`, []any{headCursor},
		func(s *IndexerStatus) []any {
			return []any{&s.LastIndexedBlock, &s.ChainHead, &s.BlocksScanned, &s.EventsProcessed, &s.IsSynced}
		})
}

// KeeperCycles answers perps.keeper_cycles with id the block and txHash the
// perps_endblock receipt hash of the block.
func (r *Resolver) KeeperCycles(ctx context.Context, args struct {
	Limit  *int32
	Offset *int32
}) ([]*KeeperCycle, error) {
	limit, offset := paging(args.Limit, args.Offset)
	list, err := collect(ctx, r.DB, "keeperCycles", `SELECT k.block, floor(extract(epoch FROM k.block_time))::bigint, k.markets_updated,
    k.orders_executed, k.liquidations_executed, k.orders_failed, k.liquidations_failed, k.block,
    COALESCE((SELECT `+hexSQL("e.tx_hash")+` FROM perps.protocol_events e WHERE e.block = k.block AND e.receipt_kind = 'perps_endblock'
        ORDER BY e.tx_index, e.log_index, e.sub LIMIT 1), ''), k.block_time
FROM perps.keeper_cycles k ORDER BY k.block DESC LIMIT $1 OFFSET $2`, []any{limit, offset},
		func(k *KeeperCycle) []any {
			return []any{&k.ID, &k.OnchainTimestamp, &k.MarketsUpdated, &k.OrdersExecuted, &k.LiquidationsExecuted,
				&k.OrdersFailed, &k.LiquidationsFailed, &k.BlockNumber, &k.TxHash, &k.BlockTimestamp}
		})
	if err != nil {
		return nil, err
	}
	for _, k := range list {
		if k.TxHash == "" {
			return nil, fmt.Errorf("keeperCycles: block %d has no perps_endblock receipt in perps.protocol_events", k.BlockNumber)
		}
	}
	return list, nil
}

func (r *Resolver) AccountLedger(ctx context.Context, args struct {
	UserAddress *string
	PositionID  *string
	Limit       *int32
	Offset      *int32
}) ([]*AccountLedgerEntry, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	position, err := idArg("positionId", args.PositionID)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "accountLedger", `SELECT row_id, row_id::text, `+hexSQL("account")+`, entry_type, `+hexSQL("token")+`,
    amount::text, position_id::text, is_debit, block, `+hexSQL("tx_hash")+`, block_time
FROM perps.account_ledger
WHERE ($1::bytea IS NULL OR account = $1::bytea) AND ($2::numeric IS NULL OR position_id = $2::numeric)
ORDER BY `+newest+`, entry_type LIMIT $3 OFFSET $4`,
		[]any{user, position, limit, offset},
		func(e *AccountLedgerEntry) []any {
			return []any{&e.ID, &e.EntryID, &e.UserAddress, &e.EntryType, &e.TokenAddress, &e.Amount, &e.PositionID,
				&e.IsDebit, &e.BlockNumber, &e.TxHash, &e.BlockTimestamp}
		})
}

// Delegates answers an empty list: this release emits no delegate events.
func (r *Resolver) Delegates(args struct{ UserAddress string }) ([]*Delegate, error) {
	if _, err := addressArg("userAddress", &args.UserAddress); err != nil {
		return nil, err
	}
	return []*Delegate{}, nil
}

// TradingAccountEvents merges the collateral and position events of an
// account; extraData holds the remaining argument columns, numbers as decimal
// strings and addresses as 0x hex.
func (r *Resolver) TradingAccountEvents(ctx context.Context, args struct {
	UserAddress *string
	EventType   *string
	Limit       *int32
	Offset      *int32
}) ([]*TradingAccountEvent, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "tradingAccountEvents", `SELECT row_id, kind, `+hexSQL("account")+`, position_id, token, amount, extra,
    block, `+hexSQL("tx_hash")+`, block_time
FROM (
    SELECT row_id, kind, account, NULL::text AS position_id, `+hexSQL("$1::bytea")+` AS token, amount::text AS amount,
        jsonb_strip_nulls(jsonb_build_object('account', `+hexSQL("account_arg")+`, 'new_balance', new_balance::text,
            'from_account', `+hexSQL("from_account")+`, 'to_account', `+hexSQL("to_account")+`))::text AS extra,
        block, tx_index, log_index, sub, tx_hash, block_time
    FROM perps.collateral_events
    UNION ALL
    SELECT row_id, kind, account, position_id::text, `+hexSQL("collateral_token")+`, NULL::text,
        jsonb_strip_nulls(jsonb_build_object('user', `+hexSQL(`"user"`)+`, 'market_id', market_id::text, 'is_long', is_long,
            'size_usd', size_usd::text, 'leverage', leverage::text, 'entry_price', entry_price::text,
            'collateral_amount', collateral_amount::text, 'new_size_usd', new_size_usd::text,
            'new_collateral_usd', new_collateral_usd::text, 'new_collateral_amount', new_collateral_amount::text,
            'old_entry_price', old_entry_price::text, 'new_entry_price', new_entry_price::text,
            'closed_size_usd', closed_size_usd::text, 'exit_price', exit_price::text, 'realized_pnl', realized_pnl::text,
            'is_full_close', is_full_close, 'take_profit_price', take_profit_price::text,
            'stop_loss_price', stop_loss_price::text))::text,
        block, tx_index, log_index, sub, tx_hash, block_time
    FROM perps.position_events
) u
WHERE ($2::bytea IS NULL OR account = $2::bytea) AND ($3::text IS NULL OR kind = $3::text)
ORDER BY `+newest+` LIMIT $4 OFFSET $5`,
		[]any{store.USDL.Bytes(), user, textArg(args.EventType), limit, offset},
		func(e *TradingAccountEvent) []any {
			return []any{&e.ID, &e.EventType, &e.UserAddress, &e.PositionID, &e.TokenAddress, &e.Amount, &e.ExtraData,
				&e.BlockNumber, &e.TxHash, &e.BlockTimestamp}
		})
}

func (r *Resolver) Fees(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	PositionID  *string
	FeeType     *int32
	Limit       *int32
	Offset      *int32
}) ([]*Fee, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	position, err := idArg("positionId", args.PositionID)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "fees", `SELECT f.row_id, f.position_id::text, `+hexSQL("f.account")+`, f.market_id, f.fee_type,
    `+feeTypeNameSQL+`, f.fee_usd::text, f.fee_tokens::text, `+hexSQL("f.token")+`, f.block, `+hexSQL("f.tx_hash")+`,
    f.log_index, f.block_time
FROM perps.fees f
WHERE ($1::bytea IS NULL OR f.account = $1::bytea) AND ($2::numeric IS NULL OR f.market_id = $2::numeric)
    AND ($3::numeric IS NULL OR f.position_id = $3::numeric) AND ($4::integer IS NULL OR f.fee_type = $4::integer)
ORDER BY f.block DESC, f.tx_index DESC, f.log_index DESC, f.sub DESC LIMIT $5 OFFSET $6`,
		[]any{user, intArg(args.MarketID), position, intArg(args.FeeType), limit, offset},
		func(f *Fee) []any {
			return []any{&f.ID, &f.PositionID, &f.UserAddress, &f.MarketID, &f.FeeType, &f.FeeTypeName, &f.FeeUsd,
				&f.FeeTokens, &f.TokenAddress, &f.BlockNumber, &f.TxHash, &f.LogIndex, &f.BlockTimestamp}
		})
}

// TradeSettlements answers perps.trade_settlements; the venue charges no
// borrowing fee, so borrowingFeeUsd is 0.
func (r *Resolver) TradeSettlements(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	PositionID  *string
	TradeType   *int32
	Limit       *int32
	Offset      *int32
}) ([]*TradeSettlement, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	position, err := idArg("positionId", args.PositionID)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "tradeSettlements", `SELECT t.row_id, t.position_id::text, `+hexSQL("t.account")+`, t.market_id,
    t.trade_type, `+nameCase("t.trade_type", tradeTypeNames)+`, t.size_usd::text, t.execution_price::text, t.gross_pnl::text,
    t.total_fees_usd::text, '0', t.funding_paid_usd::text, t.net_payout_tokens::text, t.block, `+hexSQL("t.tx_hash")+`, t.block_time
FROM perps.trade_settlements t
WHERE ($1::bytea IS NULL OR t.account = $1::bytea) AND ($2::numeric IS NULL OR t.market_id = $2::numeric)
    AND ($3::numeric IS NULL OR t.position_id = $3::numeric) AND ($4::integer IS NULL OR t.trade_type = $4::integer)
ORDER BY t.block DESC, t.tx_index DESC, t.log_index DESC, t.sub DESC LIMIT $5 OFFSET $6`,
		[]any{user, intArg(args.MarketID), position, intArg(args.TradeType), limit, offset},
		func(t *TradeSettlement) []any {
			return []any{&t.ID, &t.PositionID, &t.UserAddress, &t.MarketID, &t.TradeType, &t.TradeTypeName, &t.SizeUsd,
				&t.ExecutionPrice, &t.GrossPnl, &t.TotalFeesUsd, &t.BorrowingFeeUsd, &t.FundingPaidUsd, &t.NetPayoutTokens,
				&t.BlockNumber, &t.TxHash, &t.BlockTimestamp}
		})
}

func (r *Resolver) OiSnapshots(ctx context.Context, args struct {
	MarketID int32
	Limit    *int32
	Offset   *int32
}) ([]*OiSnapshot, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "oiSnapshots", `SELECT row_id, market_id, long_oi::text, short_oi::text, delta_usd::text, is_increase,
    block, `+hexSQL("tx_hash")+`, block_time
FROM perps.open_interest WHERE market_id = $1::numeric ORDER BY `+newest+` LIMIT $2 OFFSET $3`,
		[]any{int64(args.MarketID), limit, offset},
		func(o *OiSnapshot) []any {
			return []any{&o.ID, &o.MarketID, &o.LongOi, &o.ShortOi, &o.DeltaUsd, &o.IsIncrease, &o.BlockNumber, &o.TxHash,
				&o.BlockTimestamp}
		})
}

func (r *Resolver) MarkPriceHistory(ctx context.Context, args struct {
	MarketID int32
	Limit    *int32
	Offset   *int32
}) ([]*MarkPriceRecord, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "markPriceHistory", `SELECT row_id, market_id, mark_price::text, index_price::text, '0', '0', block,
    `+hexSQL("tx_hash")+`, block_time
FROM perps.mark_prices WHERE market_id = $1::numeric ORDER BY `+newest+` LIMIT $2 OFFSET $3`,
		[]any{int64(args.MarketID), limit, offset},
		func(m *MarkPriceRecord) []any {
			return []any{&m.ID, &m.MarketID, &m.MarkPrice, &m.IndexPrice, &m.BaseReserve, &m.QuoteReserve, &m.BlockNumber,
				&m.TxHash, &m.BlockTimestamp}
		})
}

// VaultBalanceHistory merges the collateral balances of deposits and
// withdrawals (vaultType 0) and the insurance balances of draws (vaultType 1),
// both in USDL.
func (r *Resolver) VaultBalanceHistory(ctx context.Context, args struct {
	TokenAddress *string
	VaultType    *int32
	Limit        *int32
	Offset       *int32
}) ([]*VaultBalanceRecord, error) {
	token, err := addressArg("tokenAddress", args.TokenAddress)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "vaultBalanceHistory", `SELECT row_id, `+hexSQL("$1::bytea")+`, vault_type, vault_type_name,
    new_balance::text, amount::text, is_increase, block, `+hexSQL("tx_hash")+`, block_time
FROM (
    SELECT row_id, 0 AS vault_type, 'collateral' AS vault_type_name, new_balance, amount, kind = 'deposited' AS is_increase,
        block, tx_index, log_index, sub, tx_hash, block_time
    FROM perps.collateral_events WHERE new_balance IS NOT NULL
    UNION ALL
    SELECT row_id, 1, 'insurance', new_balance, amount, false, block, tx_index, log_index, sub, tx_hash, block_time
    FROM perps.insurance_events WHERE kind = 'draw' AND new_balance IS NOT NULL AND amount IS NOT NULL
) u
WHERE ($2::bytea IS NULL OR $2::bytea = $1::bytea) AND ($3::integer IS NULL OR vault_type = $3::integer)
ORDER BY `+newest+`, vault_type LIMIT $4 OFFSET $5`,
		[]any{store.USDL.Bytes(), token, intArg(args.VaultType), limit, offset},
		func(v *VaultBalanceRecord) []any {
			return []any{&v.ID, &v.TokenAddress, &v.VaultType, &v.VaultTypeName, &v.NewBalance, &v.Delta, &v.IsIncrease,
				&v.BlockNumber, &v.TxHash, &v.BlockTimestamp}
		})
}

func (r *Resolver) FundingPayments(ctx context.Context, args struct {
	UserAddress *string
	MarketID    *int32
	PositionID  *string
	Limit       *int32
	Offset      *int32
}) ([]*FundingPayment, error) {
	user, err := addressArg("userAddress", args.UserAddress)
	if err != nil {
		return nil, err
	}
	position, err := idArg("positionId", args.PositionID)
	if err != nil {
		return nil, err
	}
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "fundingPayments", `SELECT row_id, position_id::text, `+hexSQL("account")+`, market_id,
    funding_payment_usd::text, new_collateral_usd::text, new_collateral_amount::text, block, `+hexSQL("tx_hash")+`, block_time
FROM perps.funding_payments
WHERE ($1::bytea IS NULL OR account = $1::bytea) AND ($2::numeric IS NULL OR market_id = $2::numeric)
    AND ($3::numeric IS NULL OR position_id = $3::numeric)
ORDER BY `+newest+` LIMIT $4 OFFSET $5`,
		[]any{user, intArg(args.MarketID), position, limit, offset},
		func(f *FundingPayment) []any {
			return []any{&f.ID, &f.PositionID, &f.UserAddress, &f.MarketID, &f.FundingPaymentUsd, &f.NewCollateralUsd,
				&f.NewCollateralAmount, &f.BlockNumber, &f.TxHash, &f.BlockTimestamp}
		})
}

// hourIDSQL numbers an hourly snapshot by its hour since the Unix epoch, the
// snapshot tables having no row_id.
func hourIDSQL(column string) string {
	return "floor(extract(epoch FROM " + column + ") / 3600)::bigint"
}

// crossingBlockSQL is the first block at or after the snapshot hour, the block
// whose time crossed it.
const crossingBlockSQL = `COALESCE((SELECT b.number FROM perps.blocks b WHERE b.time >= s.hour ORDER BY b.number LIMIT 1), 0)`

// MarketSnapshots answers perps.market_snapshots with the open interest and
// 24 hour funding rate in force before the hour and the fill volume of the
// 24 hours before it; open interest, index price and funding rates are null
// when no fact preceded the hour.
func (r *Resolver) MarketSnapshots(ctx context.Context, args struct {
	MarketID int32
	Limit    *int32
	Offset   *int32
}) ([]*MarketSnapshotRecord, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "marketSnapshots", `SELECT `+hourIDSQL("s.hour")+`, s.market_id, oi.long_oi::text,
    oi.short_oi::text, s.mark_price::text, s.index_price::text, s.funding_rate::text,
    fr.funding_rate24h::text, COALESCE(v.vol, 0)::text, floor(extract(epoch FROM s.hour))::bigint,
    `+crossingBlockSQL+`, s.hour
FROM perps.market_snapshots s
LEFT JOIN LATERAL (SELECT long_oi, short_oi FROM perps.open_interest WHERE market_id = s.market_id AND block_time < s.hour
    ORDER BY `+newest+` LIMIT 1) oi ON true
LEFT JOIN LATERAL (SELECT funding_rate24h FROM perps.funding_rates WHERE market_id = s.market_id AND block_time < s.hour
    ORDER BY `+newest+` LIMIT 1) fr ON true
LEFT JOIN LATERAL (SELECT sum(size_usd) AS vol FROM perps.fills WHERE market_id = s.market_id
    AND block_time >= s.hour - interval '24 hours' AND block_time < s.hour) v ON true
WHERE s.market_id = $1::numeric ORDER BY s.hour DESC LIMIT $2 OFFSET $3`,
		[]any{int64(args.MarketID), limit, offset},
		func(m *MarketSnapshotRecord) []any {
			return []any{&m.ID, &m.MarketID, &m.LongOi, &m.ShortOi, &m.MarkPrice, &m.IndexPrice, &m.FundingRatePerSecond,
				&m.FundingRate24h, &m.Volume24hUsd, &m.OnchainTimestamp, &m.BlockNumber, &m.BlockTimestamp}
		})
}

// ProtocolSnapshots answers perps.protocol_snapshots with the positions opened
// before the hour, those of them not fully closed before it, and the markets.
func (r *Resolver) ProtocolSnapshots(ctx context.Context, args struct {
	Limit  *int32
	Offset *int32
}) ([]*ProtocolSnapshotRecord, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "protocolSnapshots", `SELECT `+hourIDSQL("s.hour")+`, c.opened::text, GREATEST(c.opened - c.closed, 0)::text,
    (SELECT count(*) FROM perps.markets)::text, s.tvl_usd::text, s.insurance_total_usd::text,
    floor(extract(epoch FROM s.hour))::bigint, `+crossingBlockSQL+`, s.hour
FROM perps.protocol_snapshots s
CROSS JOIN LATERAL (SELECT count(*) FILTER (WHERE kind = 'opened') AS opened,
    count(*) FILTER (WHERE kind = 'closed' AND is_full_close) AS closed
    FROM perps.position_events WHERE block_time < s.hour) c
ORDER BY s.hour DESC LIMIT $1 OFFSET $2`,
		[]any{limit, offset},
		func(p *ProtocolSnapshotRecord) []any {
			return []any{&p.ID, &p.TotalPositions, &p.TotalOpenPositions, &p.TotalMarkets, &p.TvlUsd, &p.InsuranceTotalUsd,
				&p.OnchainTimestamp, &p.BlockNumber, &p.BlockTimestamp}
		})
}

// EnrichedGlobalStats carries the on-chain values only.
func (r *Resolver) EnrichedGlobalStats(ctx context.Context) (*EnrichedGlobalStats, error) {
	g, err := r.GlobalStats(ctx)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, errors.New("enrichedGlobalStats: no global statistics row")
	}
	return &EnrichedGlobalStats{Onchain: g}, nil
}

// EnrichedMarketStats carries the on-chain values only; a market without a
// perps.markets row is an error.
func (r *Resolver) EnrichedMarketStats(ctx context.Context, args struct{ MarketID int32 }) (*EnrichedMarketStats, error) {
	m, err := r.MarketStats(ctx, args)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("enrichedMarketStats: unknown market %d", args.MarketID)
	}
	return &EnrichedMarketStats{Onchain: m}, nil
}

// orderlySymbol is the Orderly symbol of a market (Hyperpax config
// toOrderlySymbol).
const orderlySymbolSQL = `'PERP_' || upper(m.symbol) || '_USDC', m.symbol`

// latestSQL is the latest row of table for market m.
func latestSQL(table, columns string) string {
	return "(SELECT " + columns + " FROM perps." + table + " x WHERE x.market_id = m.market_id ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1)"
}

// OrderlyTickers answers every market in the Orderly ticker shape, in USD:
// the latest mark and index price, the hourly rate of the latest
// FundingSettled exactly as a decimal string and as a float, the open interest of
// market_stats_rolling and the 24 hour window of fills.
func (r *Resolver) OrderlyTickers(ctx context.Context) ([]*OrderlyTicker, error) {
	return collect(ctx, r.DB, "orderlyTickers", `SELECT `+orderlySymbolSQL+`, mp.mark_price / 1e18, mp.index_price / 1e18, NULL::float8,
    trim_scale(`+latestSQL("funding_settlements", "funding_rate")+` * 0.000000000000000001)::text, `+latestSQL("funding_settlements", "funding_rate")+` / 1e18,
    NULL::float8, (s.open_interest_long + s.open_interest_short) / 1e18, h.open / 1e18, h.close / 1e18, h.high / 1e18,
    h.low / 1e18, h.amount / 1e18, h.volume
FROM perps.markets m
LEFT JOIN perps.market_stats_rolling s ON s.market_id = m.market_id
LEFT JOIN LATERAL (SELECT x.mark_price, x.index_price FROM perps.mark_prices x WHERE x.market_id = m.market_id
    ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1) mp ON true
LEFT JOIN LATERAL (SELECT (array_agg(price ORDER BY block, tx_index, log_index, sub))[1] AS open,
    (array_agg(price ORDER BY `+newest+`))[1] AS close, max(price) AS high, min(price) AS low, sum(size_usd) AS amount,
    sum(size_usd / NULLIF(price, 0)) AS volume
    FROM perps.fills WHERE market_id = m.market_id AND block_time >= $1) h ON true
ORDER BY m.market_id`, []any{time.Now().Add(-24 * time.Hour).UTC()},
		func(t *OrderlyTicker) []any {
			return []any{&t.Symbol, &t.PaxeerSymbol, &t.MarkPrice, &t.IndexPrice, &t.SumUnitaryFunding, &t.LastFundingRateHourly,
				&t.LastFundingRate, &t.NextFundingTime, &t.OpenInterest, &t.H24Open, &t.H24Close, &t.H24High, &t.H24Low,
				&t.H24Amount, &t.H24Volume}
		})
}

// OrderlyFundingRates answers every market with a funding rate: the hourly
// rate of its latest FundingSettled exactly as a decimal string and as a
// float, with its time in milliseconds.
func (r *Resolver) OrderlyFundingRates(ctx context.Context) ([]*OrderlyFundingRate, error) {
	return collect(ctx, r.DB, "orderlyFundingRates", `SELECT `+orderlySymbolSQL+`, trim_scale(fs.funding_rate * 0.000000000000000001)::text,
    fs.funding_rate / 1e18, floor(extract(epoch FROM fs.block_time) * 1000),
    NULL::float8, NULL::float8
FROM perps.markets m
JOIN LATERAL (SELECT 1 FROM perps.funding_rates x WHERE x.market_id = m.market_id
    ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1) fr ON true
LEFT JOIN LATERAL (SELECT x.funding_rate, x.block_time FROM perps.funding_settlements x WHERE x.market_id = m.market_id
    ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1) fs ON true
ORDER BY m.market_id`, nil,
		func(f *OrderlyFundingRate) []any {
			return []any{&f.Symbol, &f.PaxeerSymbol, &f.LastFundingRateHourly, &f.LastFundingRate,
				&f.LastFundingRateTimestamp, &f.NextFundingTime, &f.SumUnitaryFunding}
		})
}

// priceChangeWindows are the Orderly price change windows.
var priceChangeWindows = []string{"5 minutes", "30 minutes", "1 hour", "4 hours", "24 hours", "3 days", "7 days", "30 days"}

func (r *Resolver) OrderlyPriceChanges(ctx context.Context) ([]*OrderlyPriceChange, error) {
	columns := make([]string, len(priceChangeWindows))
	for i, w := range priceChangeWindows {
		columns[i] = `(SELECT (l.index_price - x.index_price) / NULLIF(x.index_price, 0) FROM perps.mark_prices x
        WHERE x.market_id = m.market_id AND x.block_time <= $1::timestamptz - interval '` + w + `'
        ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1)`
	}
	return collect(ctx, r.DB, "orderlyPriceChanges", `SELECT `+orderlySymbolSQL+`, l.index_price / 1e18, `+strings.Join(columns, ", ")+`
FROM perps.markets m
JOIN LATERAL (SELECT x.index_price FROM perps.mark_prices x WHERE x.market_id = m.market_id
    ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1) l ON true
ORDER BY m.market_id`, []any{time.Now().UTC()},
		func(p *OrderlyPriceChange) []any {
			return []any{&p.Symbol, &p.PaxeerSymbol, &p.LastPrice, &p.Change5m, &p.Change30m, &p.Change1h, &p.Change4h,
				&p.Change24h, &p.Change3d, &p.Change7d, &p.Change30d}
		})
}

func (r *Resolver) OrderlyOpenInterests(ctx context.Context) ([]*OrderlyOpenInterest, error) {
	return collect(ctx, r.DB, "orderlyOpenInterests", `SELECT `+orderlySymbolSQL+`, oi.long_oi / 1e18, oi.short_oi / 1e18
FROM perps.markets m
JOIN LATERAL (SELECT x.long_oi, x.short_oi FROM perps.open_interest x WHERE x.market_id = m.market_id
    ORDER BY x.block DESC, x.tx_index DESC, x.log_index DESC, x.sub DESC LIMIT 1) oi ON true
ORDER BY m.market_id`, nil,
		func(o *OrderlyOpenInterest) []any { return []any{&o.Symbol, &o.PaxeerSymbol, &o.LongOi, &o.ShortOi} })
}

// Leaderboard answers the all-time window of perps.leaderboard by rank.
func (r *Resolver) Leaderboard(ctx context.Context, args struct {
	Limit  *int32
	Offset *int32
}) ([]*LeaderboardEntry, error) {
	limit, offset := paging(args.Limit, args.Offset)
	return collect(ctx, r.DB, "leaderboard", `SELECT rank, `+hexSQL("account")+`, realized_pnl::text, trades_count, win_rate,
    avg_hold_time_seconds, open_positions, volume_usd::text
FROM perps.leaderboard WHERE "window" = 'all' ORDER BY rank LIMIT $1 OFFSET $2`, []any{limit, offset},
		func(e *LeaderboardEntry) []any {
			return []any{&e.Rank, &e.UserAddress, &e.TotalRealizedPnl, &e.TotalTrades, &e.WinRate, &e.AvgHoldTime,
				&e.OpenPositions, &e.TotalVolume}
		})
}
