// Package markets keeps perps.markets and perps.fee_config in step with the
// perpetuals precompile: the seed migration writes the eighteen markets of the
// activation, and the startup refresh overwrites their chain-owned fields with
// the values the precompile serves at the latest height.
package markets

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
)

// Precompile is the address of the perpetuals precompile.
var Precompile = common.HexToAddress("0x0000000000000000000000000000000000001015")

const (
	// RetryInterval is the wait between failed refresh attempts.
	RetryInterval = 60 * time.Second
	// MaxMarkets bounds the totalMarkets() count a refresh accepts.
	MaxMarkets = 1024
)

// ErrChainValue reports a precompile answer the refresh cannot store.
var ErrChainValue = errors.New("markets: invalid chain value")

// Market is one seeded row of perps.markets. MaxOpenInterest is the per-side
// cap in 18-decimal USD as a decimal string.
type Market struct {
	ID                   uint64
	Symbol               string
	Name                 string
	OracleSymbol         string
	SzDecimals           uint8
	PxDecimals           uint8
	MaxLeverage          uint64
	MaintenanceMarginBps uint64
	MaxOpenInterest      string
	Enabled              bool
}

// Seed is the content of migrations/0005_markets_seed.sql: the eighteen
// markets of contracts/perps-markets.md section 2, all with source seed.
var Seed = []Market{
	{0, "BTC", "Bitcoin", "BTC", 5, 1, 1000, 5, "500000000000000000000000000", true},
	{1, "ETH", "Ethereum", "ETH", 4, 2, 1000, 5, "500000000000000000000000000", true},
	{2, "SOL", "Solana", "SOL", 2, 3, 1000, 5, "50000000000000000000000000", true},
	{3, "AVAX", "Avalanche", "AVAX", 2, 3, 1000, 5, "50000000000000000000000000", true},
	{4, "LINK", "Chainlink", "LINK", 1, 3, 1000, 5, "20000000000000000000000000", true},
	{5, "TSLA", "Tesla", "TSLA", 3, 2, 1000, 5, "25000000000000000000000000", true},
	{6, "NVDA", "NVIDIA", "NVDA", 3, 2, 1000, 5, "25000000000000000000000000", true},
	{7, "NAS100", "Nasdaq 100", "NAS100", 4, 1, 1000, 5, "100000000000000000000000000", true},
	{8, "XAU", "Gold", "XAU", 4, 1, 1000, 5, "100000000000000000000000000", true},
	{9, "SPX500", "S&P 500", "SPX500", 4, 1, 1000, 5, "100000000000000000000000000", true},
	{10, "GOOGL", "Alphabet", "GOOGL", 3, 2, 1000, 5, "25000000000000000000000000", true},
	{11, "PAX", "Paxeer Network", "PAX", 0, 5, 1000, 5, "2000000000000000000000000", true},
	{12, "SID", "Sidiora Markets", "SID", 0, 5, 1000, 5, "2000000000000000000000000", true},
	{13, "HYPE", "Hyperliquid", "HYPE", 2, 3, 1000, 5, "50000000000000000000000000", true},
	{14, "XRP", "XRP", "XRP", 0, 4, 1000, 5, "50000000000000000000000000", true},
	{15, "ASTER", "Aster", "ASTER", 0, 5, 1000, 5, "2000000000000000000000000", true},
	{16, "TRUMP", "Trump", "TRUMP", 1, 3, 1000, 5, "2000000000000000000000000", true},
	{17, "BNB", "Binance Smart Chain", "BNB", 3, 2, 1000, 5, "50000000000000000000000000", true},
}

const (
	updateMarketSQL = `UPDATE perps.markets SET name = $2, max_leverage = $3, maintenance_margin_bps = $4, max_open_interest = $5, enabled = $6, source = 'chain', refreshed_block = $7 WHERE market_id = $1`
	insertMarketSQL = `INSERT INTO perps.markets (market_id, symbol, name, oracle_symbol, sz_decimals, px_decimals, max_leverage, maintenance_margin_bps, max_open_interest, enabled, source, refreshed_block) VALUES ($1, $2, $3, $2, NULL, NULL, $4, $5, $6, $7, 'chain', $8)`
	upsertFeesSQL   = `INSERT INTO perps.fee_config (id, taker_fee_ppm, maker_fee_ppm, liquidation_fee_ppm, insurance_share_ppm, refreshed_block) VALUES (1, $1, $2, $3, $4, $5)
ON CONFLICT (id) DO UPDATE SET taker_fee_ppm = EXCLUDED.taker_fee_ppm, maker_fee_ppm = EXCLUDED.maker_fee_ppm, liquidation_fee_ppm = EXCLUDED.liquidation_fee_ppm, insurance_share_ppm = EXCLUDED.insurance_share_ppm, refreshed_block = EXCLUDED.refreshed_block`
)

type chainMarket struct {
	id                   string
	name                 string
	symbol               string
	maxLeverage          string
	maintenanceMarginBps string
	maxOpenInterest      string
	enabled              bool
}

type fees struct {
	takerFeePpm       string
	makerFeePpm       string
	liquidationFeePpm string
	insuranceSharePpm string
}

type caller struct {
	client *ethclient.Client
	abi    abi.ABI
	at     *big.Int
}

// Refresh reads totalMarkets(), getMarket(id) for every market and getFees()
// from the precompile at the latest height, then writes them in one
// transaction: a stored market gets the chain name, max_leverage,
// maintenance_margin_bps, max_open_interest and enabled with source chain and
// refreshed_block the height; a market id not stored is inserted with the
// chain values and null decimals; perps.fee_config row 1 gets the fees. On any
// failure the stored rows are left unchanged.
func Refresh(ctx context.Context, db *sql.DB, client *ethclient.Client) error {
	c, height, err := latest(ctx, client)
	if err != nil {
		return err
	}
	out, err := c.call(ctx, "totalMarkets")
	if err != nil {
		return err
	}
	count, err := field[*big.Int](out, 0, "totalMarkets")
	if err != nil {
		return err
	}
	if !count.IsUint64() || count.Uint64() > MaxMarkets {
		return fmt.Errorf("%w: totalMarkets %s above %d", ErrChainValue, count, MaxMarkets)
	}
	list := make([]chainMarket, 0, count.Uint64())
	for id := range count.Uint64() {
		m, err := c.market(ctx, id)
		if err != nil {
			return err
		}
		list = append(list, m)
	}
	f, err := c.fees(ctx)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("markets: begin: %w", err)
	}
	if err := writeMarkets(ctx, tx, list, height); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := writeFees(ctx, tx, f, height); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("markets: commit: %w", err)
	}
	return nil
}

// FeeConfig reads getFees() from the precompile at the latest height and
// writes takerFeePpm, makerFeePpm, liquidationFeePpm and insuranceSharePpm
// into perps.fee_config row 1 with refreshed_block the height.
func FeeConfig(ctx context.Context, db *sql.DB, client *ethclient.Client) error {
	c, height, err := latest(ctx, client)
	if err != nil {
		return err
	}
	f, err := c.fees(ctx)
	if err != nil {
		return err
	}
	return writeFees(ctx, db, f, height)
}

// Retry runs Refresh until it succeeds, waiting RetryInterval after each
// failure. failed, when set, receives each refresh error and the wait before
// the next attempt. Retry returns nil on success and the context error when
// ctx ends first.
func Retry(ctx context.Context, db *sql.DB, client *ethclient.Client, failed func(err error, retryIn time.Duration)) error {
	for {
		err := Refresh(ctx, db, client)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if failed != nil {
			failed(err, RetryInterval)
		}
		timer := time.NewTimer(RetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func latest(ctx context.Context, client *ethclient.Client) (*caller, int64, error) {
	a, err := perpsabi.ABI()
	if err != nil {
		return nil, 0, fmt.Errorf("markets: perps ABI: %w", err)
	}
	height, err := client.BlockNumber(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("markets: latest height: %w", err)
	}
	if height > math.MaxInt64 {
		return nil, 0, fmt.Errorf("%w: height %d above bigint", ErrChainValue, height)
	}
	return &caller{client: client, abi: a, at: new(big.Int).SetUint64(height)}, int64(height), nil
}

func (c *caller) call(ctx context.Context, method string, args ...any) ([]any, error) {
	input, err := c.abi.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("markets: pack %s: %w", method, err)
	}
	to := Precompile
	data, err := c.client.CallContract(ctx, ethereum.CallMsg{To: &to, Data: input}, c.at)
	if err != nil {
		return nil, fmt.Errorf("markets: call %s at %s: %w", method, c.at, err)
	}
	out, err := c.abi.Unpack(method, data)
	if err != nil {
		return nil, fmt.Errorf("%w: unpack %s: %v", ErrChainValue, method, err)
	}
	return out, nil
}

func (c *caller) market(ctx context.Context, id uint64) (chainMarket, error) {
	method := fmt.Sprintf("getMarket(%d)", id)
	out, err := c.call(ctx, "getMarket", new(big.Int).SetUint64(id))
	if err != nil {
		return chainMarket{}, err
	}
	name, err := text(out, 0, method)
	if err != nil {
		return chainMarket{}, err
	}
	symbol, err := text(out, 1, method)
	if err != nil {
		return chainMarket{}, err
	}
	if symbol == "" {
		return chainMarket{}, fmt.Errorf("%w: %s symbol is empty", ErrChainValue, method)
	}
	nums := make([]string, 3)
	for i := range nums {
		v, err := field[*big.Int](out, 2+i, method)
		if err != nil {
			return chainMarket{}, err
		}
		nums[i] = v.String()
	}
	enabled, err := field[bool](out, 5, method)
	if err != nil {
		return chainMarket{}, err
	}
	return chainMarket{
		id:                   new(big.Int).SetUint64(id).String(),
		name:                 name,
		symbol:               symbol,
		maxLeverage:          nums[0],
		maintenanceMarginBps: nums[1],
		maxOpenInterest:      nums[2],
		enabled:              enabled,
	}, nil
}

func (c *caller) fees(ctx context.Context) (fees, error) {
	out, err := c.call(ctx, "getFees")
	if err != nil {
		return fees{}, err
	}
	vals := make([]string, 4)
	for i := range vals {
		v, err := field[*big.Int](out, i, "getFees")
		if err != nil {
			return fees{}, err
		}
		vals[i] = v.String()
	}
	return fees{takerFeePpm: vals[0], makerFeePpm: vals[1], liquidationFeePpm: vals[2], insuranceSharePpm: vals[3]}, nil
}

func writeMarkets(ctx context.Context, tx *sql.Tx, list []chainMarket, height int64) error {
	for _, m := range list {
		res, err := tx.ExecContext(ctx, updateMarketSQL, m.id, m.name, m.maxLeverage, m.maintenanceMarginBps, m.maxOpenInterest, m.enabled, height)
		if err != nil {
			return fmt.Errorf("markets: update market %s: %w", m.id, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("markets: update market %s: %w", m.id, err)
		}
		if n == 1 {
			continue
		}
		if _, err := tx.ExecContext(ctx, insertMarketSQL, m.id, m.symbol, m.name, m.maxLeverage, m.maintenanceMarginBps, m.maxOpenInterest, m.enabled, height); err != nil {
			return fmt.Errorf("markets: insert market %s: %w", m.id, err)
		}
	}
	return nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func writeFees(ctx context.Context, db execer, f fees, height int64) error {
	if _, err := db.ExecContext(ctx, upsertFeesSQL, f.takerFeePpm, f.makerFeePpm, f.liquidationFeePpm, f.insuranceSharePpm, height); err != nil {
		return fmt.Errorf("markets: write perps.fee_config: %w", err)
	}
	return nil
}

func field[T any](out []any, i int, method string) (T, error) {
	var zero T
	if i >= len(out) {
		return zero, fmt.Errorf("%w: %s has %d outputs, want output %d", ErrChainValue, method, len(out), i)
	}
	v, ok := out[i].(T)
	if !ok {
		return zero, fmt.Errorf("%w: %s output %d is %T, want %T", ErrChainValue, method, i, out[i], zero)
	}
	return v, nil
}

func text(out []any, i int, method string) (string, error) {
	s, err := field[string](out, i, method)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", fmt.Errorf("%w: %s output %d is not storable text", ErrChainValue, method, i)
	}
	return s, nil
}
