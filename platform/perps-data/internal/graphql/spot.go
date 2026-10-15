package graphql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
)

// Paging bounds of the list roots (contracts/data-services.md section 13).
const (
	spotDefaultLimit = 50
	spotMaxLimit     = 500
	spotMaxOffset    = 100000
)

// spotDecimal is a BigDecimal value: the decimal string of a raw integer column.
type spotDecimal string

func (spotDecimal) ImplementsGraphQLType(name string) bool { return name == "BigDecimal" }

func (d *spotDecimal) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("BigDecimal must be a string, got %T", input)
	}
	if _, ok := new(big.Rat).SetString(s); !ok {
		return fmt.Errorf("invalid BigDecimal %q", s)
	}
	*d = spotDecimal(s)
	return nil
}

func (d spotDecimal) MarshalJSON() ([]byte, error) { return json.Marshal(string(d)) }

// spotTime is a DateTime value, written as RFC 3339 in UTC.
type spotTime time.Time

func (spotTime) ImplementsGraphQLType(name string) bool { return name == "DateTime" }

func (t *spotTime) UnmarshalGraphQL(input any) error {
	s, ok := input.(string)
	if !ok {
		return fmt.Errorf("DateTime must be a string, got %T", input)
	}
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("invalid DateTime %q: %w", s, err)
	}
	*t = spotTime(v)
	return nil
}

func (t spotTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).UTC().Format(time.RFC3339Nano))
}

type spotMarket struct {
	MarketID            string
	BaseToken           string
	QuoteToken          string
	Mode                int32
	ModeName            *string
	Status              string
	CreatedAt           *spotTime
	CreatedBlock        *int32
	CreatedTxHash       *string
	LastModeChangedAt   *spotTime
	LastModeChangeBlock *int32
	LastModeChangeTx    *string
}

type spotOrder struct {
	OrderID        spotDecimal
	TraderAddress  string
	MarketID       string
	Side           int32
	SideName       *string
	OrderType      int32
	OrderTypeName  *string
	OffsetBps      int32
	Size           spotDecimal
	FilledSize     spotDecimal
	Status         string
	PlacedAt       spotTime
	PlacedBlock    int32
	PlacedTxHash   string
	ResolvedAt     *spotTime
	ResolvedBlock  *int32
	ResolvedTxHash *string

	db *sql.DB
}

type spotOrderFill struct {
	ID             int32
	OrderID        spotDecimal
	TraderAddress  string
	MarketID       string
	FillSize       spotDecimal
	FillPrice      spotDecimal
	FillScore      spotDecimal
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp spotTime
}

type spotCollateralMovement struct {
	ID             int32
	EventType      string
	UserAddress    string
	TokenAddress   string
	Amount         spotDecimal
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp spotTime
}

type spotBatchClearing struct {
	ID                int32
	MarketID          string
	ClearingOffsetBps int32
	ClearingPrice     spotDecimal
	MatchedVolume     spotDecimal
	NumBuysFilled     int32
	NumSellsFilled    int32
	BlockNumber       int32
	TxHash            string
	LogIndex          int32
	BlockTimestamp    spotTime
}

type spotEpochSettlement struct {
	EpochNumber    spotDecimal
	UsersProcessed int32
	OnchainBlock   int32
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp spotTime
}

type spotFastSettlement struct {
	ID             int32
	UserAddress    string
	TokenAddress   string
	Amount         spotDecimal
	Fee            spotDecimal
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp spotTime
}

type spotMarketModeChange struct {
	ID                  int32
	MarketID            string
	OldMode             int32
	NewMode             int32
	BatchModeUntilBlock spotDecimal
	BlockNumber         int32
	TxHash              string
	LogIndex            int32
	BlockTimestamp      spotTime
}

type spotPofqUpdate struct {
	ID             int32
	EntityAddress  string
	NewScore       spotDecimal
	NewWeight      spotDecimal
	IsVault        bool
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp spotTime
}

type spotPLV struct {
	VaultAddress       string
	IsRegistered       bool
	RegisteredAt       *spotTime
	RegisteredBlock    *int32
	RegisteredTxHash   *string
	DeregisteredAt     *spotTime
	DeregisteredBlock  *int32
	DeregisteredTxHash *string
}

type spotKeeperCycle struct {
	ID               int32
	OnchainTimestamp int32
	MarketsEvaluated int32
	BatchesCleared   int32
	EpochSettled     bool
	BlockNumber      int32
	TxHash           string
	BlockTimestamp   spotTime
}

type spotBatchClearFailure struct {
	ID             int32
	MarketID       string
	Reason         string
	BlockNumber    int32
	TxHash         string
	LogIndex       int32
	BlockTimestamp spotTime
}

type spotPageArgs struct {
	Limit  *int32
	Offset *int32
}

type spotMarketsArgs struct {
	Status *string
	spotPageArgs
}

type spotOrdersArgs struct {
	TraderAddress *string
	MarketID      *string
	Status        *string
	spotPageArgs
}

type spotOrderFillsArgs struct {
	OrderID       *string
	TraderAddress *string
	MarketID      *string
	spotPageArgs
}

type spotMarketFilterArgs struct {
	MarketID *string
	spotPageArgs
}

type spotUserTokenArgs struct {
	UserAddress  *string
	TokenAddress *string
	spotPageArgs
}

type spotCollateralArgs struct {
	UserAddress  *string
	TokenAddress *string
	EventType    *string
	spotPageArgs
}

type spotPofqArgs struct {
	EntityAddress *string
	IsVault       *bool
	spotPageArgs
}

type spotPLVArgs struct {
	IsRegistered *bool
	spotPageArgs
}

// spotPage clamps limit to [1, 500] with default 50 and offset to [0, 100000].
func spotPage(p spotPageArgs) (limit, offset int32) {
	limit = spotDefaultLimit
	if p.Limit != nil {
		limit = min(max(*p.Limit, 1), spotMaxLimit)
	}
	if p.Offset != nil {
		offset = min(max(*p.Offset, 0), spotMaxOffset)
	}
	return limit, offset
}

// spotAddressArg returns the 20 bytes of an optional address filter, nil for
// an absent filter, and ok false for a value that names no address, which
// matches no row. Hex compares case-insensitively.
func spotAddressArg(s *string) (arg any, ok bool) {
	if s == nil {
		return nil, true
	}
	if !common.IsHexAddress(*s) {
		return nil, false
	}
	return common.HexToAddress(*s).Bytes(), true
}

// spotOrderIDArg returns an optional order id filter as a decimal string, and
// ok false for a value that is not a non-negative integer, which matches no row.
func spotOrderIDArg(s *string) (arg any, ok bool) {
	if s == nil {
		return nil, true
	}
	v, valid := new(big.Int).SetString(*s, 10)
	if !valid || v.Sign() < 0 {
		return nil, false
	}
	return v.String(), true
}

func spotTextArg(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func spotHex(b []byte) string { return "0x" + hex.EncodeToString(b) }

func spotInt32(v int64, what string) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, fmt.Errorf("%s %d does not fit Int", what, v)
	}
	return int32(v), nil
}

// spotMarketsSQL lists the non-stablecoin tokens of the spot token registry in
// registration order; status is paused when the latest Paused, Unpaused or
// TokenPauseUpdated of the token is a pause.
const spotMarketsSQL = `SELECT r.token, r.block, r.tx_hash, r.block_time, COALESCE(p.paused, false)
FROM (SELECT DISTINCT ON (token) token, block, tx_index, log_index, sub, tx_hash, block_time
    FROM perps.spot_token_registry WHERE kind = 'token_registered' AND is_stablecoin = false
    ORDER BY token, block, tx_index, log_index, sub) r
LEFT JOIN LATERAL (SELECT a.kind = 'paused' OR (a.kind = 'token_pause_updated' AND a.paused) AS paused
    FROM perps.spot_admin a
    WHERE a.kind IN ('paused', 'unpaused') OR (a.kind = 'token_pause_updated' AND a.token = r.token)
    ORDER BY a.block DESC, a.tx_index DESC, a.log_index DESC, a.sub DESC LIMIT 1) p ON true
WHERE ($1::bytea IS NULL OR r.token = $1::bytea)
    AND ($2::text IS NULL OR (CASE WHEN COALESCE(p.paused, false) THEN 'paused' ELSE 'active' END) = $2::text)
ORDER BY r.block, r.tx_index, r.log_index, r.sub
LIMIT $3 OFFSET $4`

func (r *Resolver) spotMarkets(ctx context.Context, token, status any, limit, offset int32) ([]*spotMarket, error) {
	rows, err := r.DB.QueryContext(ctx, spotMarketsSQL, token, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("spot markets: %w", err)
	}
	defer rows.Close()
	quote := strings.ToLower(store.USDL.Hex())
	markets := []*spotMarket{}
	for rows.Next() {
		var (
			tok, txHash []byte
			block       int64
			at          time.Time
			paused      bool
		)
		if err := rows.Scan(&tok, &block, &txHash, &at, &paused); err != nil {
			return nil, fmt.Errorf("spot markets: %w", err)
		}
		b, err := spotInt32(block, "block")
		if err != nil {
			return nil, err
		}
		modeName, status, created, hash := "pecor", "active", spotTime(at), spotHex(txHash)
		if paused {
			status = "paused"
		}
		markets = append(markets, &spotMarket{
			MarketID: spotHex(tok), BaseToken: spotHex(tok), QuoteToken: quote, Mode: 0, ModeName: &modeName, Status: status,
			CreatedAt: &created, CreatedBlock: &b, CreatedTxHash: &hash,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spot markets: %w", err)
	}
	return markets, nil
}

// SpotMarket resolves Query.spotMarket: the registered non-stablecoin token
// marketId, or null.
func (r *Resolver) SpotMarket(ctx context.Context, args struct{ MarketID string }) (*spotMarket, error) {
	token, ok := spotAddressArg(&args.MarketID)
	if !ok {
		return nil, nil
	}
	markets, err := r.spotMarkets(ctx, token, nil, 1, 0)
	if err != nil || len(markets) == 0 {
		return nil, err
	}
	return markets[0], nil
}

// SpotMarkets resolves Query.spotMarkets.
func (r *Resolver) SpotMarkets(ctx context.Context, args spotMarketsArgs) ([]*spotMarket, error) {
	limit, offset := spotPage(args.spotPageArgs)
	return r.spotMarkets(ctx, nil, spotTextArg(args.Status), limit, offset)
}

// spotOrdersSQL reads perps.spot_orders with the created event of each order
// and, for a final status, the event that closed it.
const spotOrdersSQL = `SELECT o.order_id::text, o.account, o.token, o.side, o.kind, o.amount::text, o.status,
    c.block_time, c.block, c.tx_hash, x.block_time, x.block, x.tx_hash
FROM perps.spot_orders o
JOIN LATERAL (SELECT e.block_time, e.block, e.tx_hash FROM perps.spot_order_events e
    WHERE e.order_id = o.order_id AND e.kind = o.kind || '_created'
    ORDER BY e.block, e.tx_index, e.log_index, e.sub LIMIT 1) c ON true
LEFT JOIN LATERAL (SELECT e.block_time, e.block, e.tx_hash FROM perps.spot_order_events e
    WHERE o.status IN ('executed', 'cancelled', 'expired') AND e.order_id = o.order_id
        AND e.kind IN (o.kind || '_executed', o.kind || '_cancelled', o.kind || '_expired')
    ORDER BY e.block DESC, e.tx_index DESC, e.log_index DESC, e.sub DESC LIMIT 1) x ON true
WHERE ($1::numeric IS NULL OR o.order_id = $1::numeric)
    AND ($2::bytea IS NULL OR o.account = $2::bytea)
    AND ($3::bytea IS NULL OR o.token = $3::bytea)
    AND ($4::text IS NULL OR o.status = $4::text)
ORDER BY o.placed_block DESC, o.order_id DESC
LIMIT $5 OFFSET $6`

// spotOrderTypes maps the spot order kinds to the SpotOrder orderType values.
var spotOrderTypes = map[string]int32{"limit": 0, "stop_limit": 1, "stop_loss": 2}

func (r *Resolver) spotOrders(ctx context.Context, orderID, trader, token, status any, limit, offset int32) ([]*spotOrder, error) {
	rows, err := r.DB.QueryContext(ctx, spotOrdersSQL, orderID, trader, token, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("spot orders: %w", err)
	}
	defer rows.Close()
	orders := []*spotOrder{}
	for rows.Next() {
		var (
			id, side, kind, amount, st string
			account, tok, placedHash   []byte
			placedAt                   time.Time
			placedBlock                int64
			resolvedAt                 sql.NullTime
			resolvedBlock              sql.NullInt64
			resolvedHash               []byte
		)
		if err := rows.Scan(&id, &account, &tok, &side, &kind, &amount, &st, &placedAt, &placedBlock, &placedHash,
			&resolvedAt, &resolvedBlock, &resolvedHash); err != nil {
			return nil, fmt.Errorf("spot orders: %w", err)
		}
		orderType, ok := spotOrderTypes[kind]
		if !ok {
			return nil, fmt.Errorf("spot order %s: unknown kind %q", id, kind)
		}
		var sideValue int32
		switch side {
		case "buy":
		case "sell":
			sideValue = 1
		default:
			return nil, fmt.Errorf("spot order %s: unknown side %q", id, side)
		}
		pb, err := spotInt32(placedBlock, "block")
		if err != nil {
			return nil, err
		}
		filled := spotDecimal("0")
		if st == "executed" {
			filled = spotDecimal(amount)
		}
		o := &spotOrder{
			OrderID: spotDecimal(id), TraderAddress: spotHex(account), MarketID: spotHex(tok),
			Side: sideValue, SideName: &side, OrderType: orderType, OrderTypeName: &kind, OffsetBps: 0,
			Size: spotDecimal(amount), FilledSize: filled, Status: st,
			PlacedAt: spotTime(placedAt), PlacedBlock: pb, PlacedTxHash: spotHex(placedHash), db: r.DB,
		}
		if resolvedBlock.Valid {
			rb, err := spotInt32(resolvedBlock.Int64, "block")
			if err != nil {
				return nil, err
			}
			at, hash := spotTime(resolvedAt.Time), spotHex(resolvedHash)
			o.ResolvedAt, o.ResolvedBlock, o.ResolvedTxHash = &at, &rb, &hash
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spot orders: %w", err)
	}
	return orders, nil
}

// SpotOrder resolves Query.spotOrder; the limit, stop-limit and stop-loss
// records share one id sequence, so an id names at most one order.
func (r *Resolver) SpotOrder(ctx context.Context, args struct{ OrderID string }) (*spotOrder, error) {
	id, ok := spotOrderIDArg(&args.OrderID)
	if !ok {
		return nil, nil
	}
	orders, err := r.spotOrders(ctx, id, nil, nil, nil, 1, 0)
	if err != nil || len(orders) == 0 {
		return nil, err
	}
	return orders[0], nil
}

// SpotOrders resolves Query.spotOrders.
func (r *Resolver) SpotOrders(ctx context.Context, args spotOrdersArgs) ([]*spotOrder, error) {
	trader, ok := spotAddressArg(args.TraderAddress)
	if !ok {
		return []*spotOrder{}, nil
	}
	token, ok := spotAddressArg(args.MarketID)
	if !ok {
		return []*spotOrder{}, nil
	}
	limit, offset := spotPage(args.spotPageArgs)
	return r.spotOrders(ctx, nil, trader, token, spotTextArg(args.Status), limit, offset)
}

// Market resolves SpotOrder.market: the spot market of the order token.
func (o *spotOrder) Market(ctx context.Context) (*spotMarket, error) {
	return (&Resolver{DB: o.db}).SpotMarket(ctx, struct{ MarketID string }{o.MarketID})
}

// Fills resolves SpotOrder.fills: the execution of the order, if any.
func (o *spotOrder) Fills(ctx context.Context) ([]*spotOrderFill, error) {
	return (&Resolver{DB: o.db}).spotOrderFills(ctx, string(o.OrderID), nil, nil, spotMaxLimit, 0)
}

// spotOrderFillsSQL reads the executed spot order events with the trader and
// amount of their order.
const spotOrderFillsSQL = `SELECT e.row_id, e.order_id::text, o.account, e.token, o.amount::text, e.execution_price::text,
    e.block, e.tx_hash, e.log_index, e.block_time
FROM perps.spot_order_events e
JOIN perps.spot_orders o ON o.order_id = e.order_id AND e.kind = o.kind || '_executed'
WHERE e.kind IN ('limit_executed', 'stop_limit_executed', 'stop_loss_executed')
    AND ($1::numeric IS NULL OR e.order_id = $1::numeric)
    AND ($2::bytea IS NULL OR o.account = $2::bytea)
    AND ($3::bytea IS NULL OR e.token = $3::bytea)
ORDER BY e.block DESC, e.tx_index DESC, e.log_index DESC, e.sub DESC
LIMIT $4 OFFSET $5`

func (r *Resolver) spotOrderFills(ctx context.Context, orderID, trader, token any, limit, offset int32) ([]*spotOrderFill, error) {
	rows, err := r.DB.QueryContext(ctx, spotOrderFillsSQL, orderID, trader, token, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("spot order fills: %w", err)
	}
	defer rows.Close()
	fills := []*spotOrderFill{}
	for rows.Next() {
		var (
			rowID, block         int64
			logIndex             int32
			id, amount, price    string
			account, tok, txHash []byte
			at                   time.Time
		)
		if err := rows.Scan(&rowID, &id, &account, &tok, &amount, &price, &block, &txHash, &logIndex, &at); err != nil {
			return nil, fmt.Errorf("spot order fills: %w", err)
		}
		rid, err := spotInt32(rowID, "row id")
		if err != nil {
			return nil, err
		}
		b, err := spotInt32(block, "block")
		if err != nil {
			return nil, err
		}
		fills = append(fills, &spotOrderFill{
			ID: rid, OrderID: spotDecimal(id), TraderAddress: spotHex(account), MarketID: spotHex(tok),
			FillSize: spotDecimal(amount), FillPrice: spotDecimal(price), FillScore: "0",
			BlockNumber: b, TxHash: spotHex(txHash), LogIndex: logIndex, BlockTimestamp: spotTime(at),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spot order fills: %w", err)
	}
	return fills, nil
}

// SpotOrderFills resolves Query.spotOrderFills.
func (r *Resolver) SpotOrderFills(ctx context.Context, args spotOrderFillsArgs) ([]*spotOrderFill, error) {
	id, ok := spotOrderIDArg(args.OrderID)
	if !ok {
		return []*spotOrderFill{}, nil
	}
	trader, ok := spotAddressArg(args.TraderAddress)
	if !ok {
		return []*spotOrderFill{}, nil
	}
	token, ok := spotAddressArg(args.MarketID)
	if !ok {
		return []*spotOrderFill{}, nil
	}
	limit, offset := spotPage(args.spotPageArgs)
	return r.spotOrderFills(ctx, id, trader, token, limit, offset)
}

// spotCollateralSQL reads the deposit, withdrawal, LP withdrawal and native
// rows of perps.spot_vault_events; native rows carry the PAX token, the first
// registered non-stablecoin token.
const spotCollateralSQL = `WITH pax AS (
    SELECT token FROM perps.spot_token_registry WHERE kind = 'token_registered' AND is_stablecoin = false
    ORDER BY block, tx_index, log_index, sub LIMIT 1
), m AS (
    SELECT v.row_id, v.kind, COALESCE(v.depositor, v.recipient, v.provider) AS holder,
        CASE WHEN v.kind IN ('native_deposit', 'native_withdrawal') THEN (SELECT token FROM pax) ELSE v.token END AS token,
        v.amount, v.block, v.tx_index, v.log_index, v.sub, v.tx_hash, v.block_time
    FROM perps.spot_vault_events v
    WHERE v.kind IN ('deposit', 'withdrawal', 'lp_withdrawal', 'native_deposit', 'native_withdrawal')
)
SELECT row_id, kind, holder, token, amount::text, block, tx_hash, log_index, block_time FROM m
WHERE ($1::bytea IS NULL OR holder = $1::bytea)
    AND ($2::bytea IS NULL OR token = $2::bytea)
    AND ($3::text IS NULL OR kind = $3::text)
ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC
LIMIT $4 OFFSET $5`

// SpotCollateralMovements resolves Query.spotCollateralMovements.
func (r *Resolver) SpotCollateralMovements(ctx context.Context, args spotCollateralArgs) ([]*spotCollateralMovement, error) {
	user, ok := spotAddressArg(args.UserAddress)
	if !ok {
		return []*spotCollateralMovement{}, nil
	}
	token, ok := spotAddressArg(args.TokenAddress)
	if !ok {
		return []*spotCollateralMovement{}, nil
	}
	limit, offset := spotPage(args.spotPageArgs)
	rows, err := r.DB.QueryContext(ctx, spotCollateralSQL, user, token, spotTextArg(args.EventType), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("spot collateral movements: %w", err)
	}
	defer rows.Close()
	moves := []*spotCollateralMovement{}
	for rows.Next() {
		var (
			rowID, block        int64
			logIndex            int32
			kind, amount        string
			holder, tok, txHash []byte
			at                  time.Time
		)
		if err := rows.Scan(&rowID, &kind, &holder, &tok, &amount, &block, &txHash, &logIndex, &at); err != nil {
			return nil, fmt.Errorf("spot collateral movements: %w", err)
		}
		if holder == nil || tok == nil {
			return nil, fmt.Errorf("spot collateral movement %d: %s row has no account or token", rowID, kind)
		}
		rid, err := spotInt32(rowID, "row id")
		if err != nil {
			return nil, err
		}
		b, err := spotInt32(block, "block")
		if err != nil {
			return nil, err
		}
		moves = append(moves, &spotCollateralMovement{
			ID: rid, EventType: kind, UserAddress: spotHex(holder), TokenAddress: spotHex(tok), Amount: spotDecimal(amount),
			BlockNumber: b, TxHash: spotHex(txHash), LogIndex: logIndex, BlockTimestamp: spotTime(at),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spot collateral movements: %w", err)
	}
	return moves, nil
}

// The roots below have no source event in the spot precompile set of this
// release and answer an empty list (contracts/data-services.md section 13).

// SpotBatchClearings resolves Query.spotBatchClearings.
func (r *Resolver) SpotBatchClearings(spotMarketFilterArgs) []*spotBatchClearing {
	return []*spotBatchClearing{}
}

// SpotEpochSettlements resolves Query.spotEpochSettlements.
func (r *Resolver) SpotEpochSettlements(spotPageArgs) []*spotEpochSettlement {
	return []*spotEpochSettlement{}
}

// SpotFastSettlements resolves Query.spotFastSettlements.
func (r *Resolver) SpotFastSettlements(spotUserTokenArgs) []*spotFastSettlement {
	return []*spotFastSettlement{}
}

// SpotMarketModeChanges resolves Query.spotMarketModeChanges.
func (r *Resolver) SpotMarketModeChanges(spotMarketFilterArgs) []*spotMarketModeChange {
	return []*spotMarketModeChange{}
}

// SpotPofqUpdates resolves Query.spotPofqUpdates.
func (r *Resolver) SpotPofqUpdates(spotPofqArgs) []*spotPofqUpdate {
	return []*spotPofqUpdate{}
}

// SpotPLVs resolves Query.spotPLVs.
func (r *Resolver) SpotPLVs(spotPLVArgs) []*spotPLV {
	return []*spotPLV{}
}

// SpotKeeperCycles resolves Query.spotKeeperCycles.
func (r *Resolver) SpotKeeperCycles(spotPageArgs) []*spotKeeperCycle {
	return []*spotKeeperCycle{}
}

// SpotBatchClearFailures resolves Query.spotBatchClearFailures.
func (r *Resolver) SpotBatchClearFailures(spotMarketFilterArgs) []*spotBatchClearFailure {
	return []*spotBatchClearFailure{}
}
