// Package explorer serves the chain explorer queries of the perps data services
// (contracts/data-services.md section 16): recent blocks, venue transactions
// mapped to the six actions of the application data protocol
// (Hyperpax:data_api.md:600-601), account and market history paged with a
// keyset cursor, and links into the network explorer.
package explorer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/bech32"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/pnl"
)

// The six action values of the application data protocol
// (Hyperpax:data_api.md:600-601).
const (
	ActionPlaceOrder          = "Place Order"
	ActionCancelOrder         = "Cancel Order"
	ActionModifyOrder         = "Modify Order"
	ActionOROBFill            = "OROB Fill"
	ActionOracleUpdated       = "Oracle Updated"
	ActionSealedBatchExecuted = "Sealed Batch Executed"
)

// The tx_actions kinds of the rows that do not stand for one log: the per-block
// pricefeed aggregate row and the end-block synthetic receipt rows.
const (
	KindPricefeedAggregate = "pricefeed_aggregate"
	KindPerpsEndBlock      = "perps_endblock"
	KindSpotEndBlock       = "spot_endblock"
)

// actions is the action mapping of contracts/data-services.md section 16.
var actions = map[string]string{
	"OrderPlaced":            ActionPlaceOrder,
	"LimitOrderCreated":      ActionPlaceOrder,
	"StopLimitOrderCreated":  ActionPlaceOrder,
	"StopLossOrderCreated":   ActionPlaceOrder,
	"OrderCancelled":         ActionCancelOrder,
	"OrderExpired":           ActionCancelOrder,
	"LimitOrderCancelled":    ActionCancelOrder,
	"LimitOrderExpired":      ActionCancelOrder,
	"StopLimitCancelled":     ActionCancelOrder,
	"StopLossOrderCancelled": ActionCancelOrder,
	"LimitOrderModified":     ActionModifyOrder,
	"PositionTpSlSet":        ActionModifyOrder,
	"OrderTriggered":         ActionModifyOrder,
	"StopLimitActivated":     ActionModifyOrder,
	"Fill":                   ActionOROBFill,
	"LimitOrderExecuted":     ActionOROBFill,
	"StopLimitExecuted":      ActionOROBFill,
	"StopLossOrderExecuted":  ActionOROBFill,
	"MarketOrderExecuted":    ActionOROBFill,
	"NativeSwap":             ActionOROBFill,
	"SimpleSwap":             ActionOROBFill,
	KindPricefeedAggregate:   ActionOracleUpdated,
	KindPerpsEndBlock:        ActionSealedBatchExecuted,
	KindSpotEndBlock:         ActionSealedBatchExecuted,
}

const (
	// userPrefix is the bech32 prefix of account addresses (node/const.go:5).
	userPrefix = "pax"

	defaultBlocks       = 20
	maxBlocks           = 50
	defaultTransactions = 50
	maxTransactions     = 200

	// historyWindow is the first block span read for an account history page;
	// it doubles until the page fills or block 0 is reached.
	historyWindow = 4096

	isoMillis = "2006-01-02T15:04:05.000Z"
)

var (
	// perpsAddress is the perpetuals precompile, whose sizes are 18-decimal USD
	// notionals (contracts/perps.md section 2).
	perpsAddress = common.HexToAddress("0x0000000000000000000000000000000000001015")
	usdScale     = new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	// spotScale is 10^6: USDL, PAX and SID amounts carry 6 decimals
	// (contracts/spot.md, Token model).
	spotScale = new(big.Rat).SetInt(big.NewInt(1_000_000))
	unitScale = new(big.Rat).SetInt64(1)
)

var (
	// ErrLimit reports a limit that is not a decimal integer.
	ErrLimit = errors.New("explorer: invalid limit")
	// ErrCursor reports a cursor that is not block:tx_index:log_index.
	ErrCursor = errors.New("explorer: invalid cursor")
	// ErrUnknownKind reports a tx_actions kind outside the action mapping.
	ErrUnknownKind = errors.New("explorer: kind outside the action mapping")
	// ErrActionMismatch reports a stored action that differs from the action
	// mapping of its kind.
	ErrActionMismatch = errors.New("explorer: stored action differs from the mapping")
)

// Action returns the action of a tx_actions kind: the event name of a log row,
// or pricefeed_aggregate, perps_endblock or spot_endblock.
func Action(kind string) (string, error) {
	a, ok := actions[kind]
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}
	return a, nil
}

// Explorer reads the explorer views from schema perps.
type Explorer struct {
	db     *sql.DB
	txBase string
}

// New returns an explorer reading db whose links start with txBase, the value
// of PERPS_EXPLORER_TX_BASE; an empty txBase omits every link.
func New(db *sql.DB, txBase string) (*Explorer, error) {
	if db == nil {
		return nil, errors.New("explorer: nil database")
	}
	return &Explorer{db: db, txBase: strings.TrimRight(txBase, "/")}, nil
}

// TxLink returns the network explorer link of a transaction, or "" when
// PERPS_EXPLORER_TX_BASE is empty.
func (e *Explorer) TxLink(hash common.Hash) string {
	if e.txBase == "" {
		return ""
	}
	return e.txBase + "/tx/" + hash.Hex()
}

// AddressLink returns the network explorer link of an address, or "" when
// PERPS_EXPLORER_TX_BASE is empty.
func (e *Explorer) AddressLink(addr common.Address) string {
	if e.txBase == "" {
		return ""
	}
	return e.txBase + "/address/" + addr.Hex()
}

// BlockLink returns the network explorer link of a block, or "" when
// PERPS_EXPLORER_TX_BASE is empty.
func (e *Explorer) BlockLink(number int64) string {
	if e.txBase == "" {
		return ""
	}
	return e.txBase + "/block/" + strconv.FormatInt(number, 10)
}

// Block is one row of GET /api/chain/blocks.
type Block struct {
	BlockNumber  int64  `json:"block_number"`
	Time         string `json:"time"`
	Proposer     string `json:"proposer"`
	Hash         string `json:"hash"`
	Transactions int    `json:"transactions"`
	Link         string `json:"link,omitempty"`
}

// Transaction is one row of GET /api/chain/transactions.
type Transaction struct {
	Hash   string   `json:"hash"`
	Action string   `json:"action"`
	Block  int64    `json:"block"`
	Time   string   `json:"time"`
	User   string   `json:"user"`
	Symbol *string  `json:"symbol"`
	Side   *string  `json:"side"`
	Price  *float64 `json:"price"`
	Size   *float64 `json:"size"`
	Link   string   `json:"link,omitempty"`
}

// HistoryItem is one account history row (pnl.History) as served.
type HistoryItem struct {
	Kind        pnl.Kind        `json:"kind"`
	Event       string          `json:"event"`
	Block       int64           `json:"block"`
	TxIndex     int             `json:"tx_index"`
	LogIndex    int             `json:"log_index"`
	Hash        string          `json:"hash"`
	ReceiptKind string          `json:"receipt_kind"`
	Time        string          `json:"time"`
	Args        json.RawMessage `json:"args"`
	Link        string          `json:"link,omitempty"`
}

// Page is one keyset page of a history: NextCursor is empty on the last page.
type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Cursor is the keyset position (block, tx_index, log_index); a page holds the
// rows strictly below it in descending order.
type Cursor struct {
	Block    int64
	TxIndex  int
	LogIndex int
}

// String renders the cursor as block:tx_index:log_index.
func (c Cursor) String() string {
	return fmt.Sprintf("%d:%d:%d", c.Block, c.TxIndex, c.LogIndex)
}

// ParseCursor reads block:tx_index:log_index with block and log_index at least
// 0 and tx_index at least -1 (the block event index).
func ParseCursor(raw string) (Cursor, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 3 {
		return Cursor{}, fmt.Errorf("%w: %q", ErrCursor, raw)
	}
	block, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || block < 0 {
		return Cursor{}, fmt.Errorf("%w: %q", ErrCursor, raw)
	}
	txIndex, err := strconv.ParseInt(parts[1], 10, 32)
	if err != nil || txIndex < -1 {
		return Cursor{}, fmt.Errorf("%w: %q", ErrCursor, raw)
	}
	logIndex, err := strconv.ParseInt(parts[2], 10, 32)
	if err != nil || logIndex < 0 {
		return Cursor{}, fmt.Errorf("%w: %q", ErrCursor, raw)
	}
	return Cursor{Block: block, TxIndex: int(txIndex), LogIndex: int(logIndex)}, nil
}

func (c Cursor) after(o Cursor) bool {
	if c.Block != o.Block {
		return c.Block > o.Block
	}
	if c.TxIndex != o.TxIndex {
		return c.TxIndex > o.TxIndex
	}
	return c.LogIndex > o.LogIndex
}

// top is above every stored key, the position of a first page.
var top = Cursor{Block: math.MaxInt64, TxIndex: math.MaxInt32, LogIndex: math.MaxInt32}

func parseCursor(raw string) (Cursor, error) {
	if raw == "" {
		return top, nil
	}
	return ParseCursor(raw)
}

// clamp reads a limit query value: empty is def, a decimal integer is clamped
// to [1, hi] (an out-of-range integer clamps to the nearer bound), anything
// else is ErrLimit.
func clamp(raw string, def, hi int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		var numErr *strconv.NumError
		if !errors.As(err, &numErr) || !errors.Is(numErr.Err, strconv.ErrRange) {
			return 0, fmt.Errorf("%w: %q", ErrLimit, raw)
		}
		if strings.HasPrefix(raw, "-") {
			return 1, nil
		}
		return hi, nil
	}
	return int(min(max(n, 1), int64(hi))), nil
}

const blocksQuery = `SELECT number, hash, time, proposer, tx_count FROM perps.blocks ORDER BY number DESC LIMIT $1`

// Blocks returns the newest blocks first; limit is the raw query value,
// clamped to [1, 50] with default 20 (Hyperpax:data_api.md:562).
func (e *Explorer) Blocks(ctx context.Context, limit string) ([]Block, error) {
	n, err := clamp(limit, defaultBlocks, maxBlocks)
	if err != nil {
		return nil, err
	}
	rows, err := e.db.QueryContext(ctx, blocksQuery, n)
	if err != nil {
		return nil, fmt.Errorf("explorer: read blocks: %w", err)
	}
	defer rows.Close()
	out := []Block{}
	for rows.Next() {
		var (
			b    Block
			hash []byte
			at   time.Time
		)
		if err := rows.Scan(&b.BlockNumber, &hash, &at, &b.Proposer, &b.Transactions); err != nil {
			return nil, fmt.Errorf("explorer: scan block: %w", err)
		}
		if len(hash) != common.HashLength {
			return nil, fmt.Errorf("explorer: stored block hash has %d bytes", len(hash))
		}
		b.Hash = common.BytesToHash(hash).Hex()
		b.Time = at.UTC().Format(isoMillis)
		b.Link = e.BlockLink(b.BlockNumber)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("explorer: read blocks: %w", err)
	}
	return out, nil
}

const actionColumns = `block, tx_index, log_index, sub, tx_hash, block_hash, block_time, emitter, kind, account, action, symbol, side, price::text, size::text`

const (
	transactionsQuery = `SELECT ` + actionColumns + ` FROM perps.tx_actions
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT $1`
	marketPageQuery = `SELECT ` + actionColumns + ` FROM perps.tx_actions
    WHERE symbol = $1 AND (block, tx_index, log_index) < ($2, $3, $4)
    ORDER BY block DESC, tx_index DESC, log_index DESC, sub DESC LIMIT $5`
	marketGroupQuery = `SELECT ` + actionColumns + ` FROM perps.tx_actions
    WHERE symbol = $1 AND (block, tx_index, log_index) = ($2, $3, $4) AND sub < $5
    ORDER BY sub DESC`
	marketMoreQuery = `SELECT EXISTS (SELECT 1 FROM perps.tx_actions
    WHERE symbol = $1 AND (block, tx_index, log_index) < ($2, $3, $4))`
)

// actionRow is a scanned tx_actions row with its key.
type actionRow struct {
	key Cursor
	sub int
	tx  Transaction
}

// Transactions returns the newest tx_actions rows first; limit is the raw
// query value, clamped to [1, 200] with default 50 (Hyperpax:data_api.md:580).
func (e *Explorer) Transactions(ctx context.Context, limit string) ([]Transaction, error) {
	n, err := clamp(limit, defaultTransactions, maxTransactions)
	if err != nil {
		return nil, err
	}
	rows, err := e.readActions(ctx, transactionsQuery, n)
	if err != nil {
		return nil, err
	}
	out := make([]Transaction, len(rows))
	for i, r := range rows {
		out[i] = r.tx
	}
	return out, nil
}

// MarketHistory returns one page of the tx_actions rows of symbol below cursor
// ("" for the first page), ordered by (block, tx_index, log_index, sub)
// descending; limit is clamped like Transactions. A page never splits the rows
// of one (block, tx_index, log_index), so it can exceed limit by the rest of
// its last group.
func (e *Explorer) MarketHistory(ctx context.Context, symbol, cursor, limit string) (Page[Transaction], error) {
	c, err := parseCursor(cursor)
	if err != nil {
		return Page[Transaction]{}, err
	}
	n, err := clamp(limit, defaultTransactions, maxTransactions)
	if err != nil {
		return Page[Transaction]{}, err
	}
	rows, err := e.readActions(ctx, marketPageQuery, symbol, c.Block, c.TxIndex, c.LogIndex, n+1)
	if err != nil {
		return Page[Transaction]{}, err
	}
	page := Page[Transaction]{Items: []Transaction{}}
	if len(rows) > n {
		last := rows[n-1]
		more := true
		if rows[n].key == last.key {
			rest, err := e.readActions(ctx, marketGroupQuery, symbol, last.key.Block, last.key.TxIndex, last.key.LogIndex, last.sub)
			if err != nil {
				return Page[Transaction]{}, err
			}
			rows = append(rows[:n], rest...)
			if err := e.db.QueryRowContext(ctx, marketMoreQuery, symbol, last.key.Block, last.key.TxIndex, last.key.LogIndex).Scan(&more); err != nil {
				return Page[Transaction]{}, fmt.Errorf("explorer: read market history: %w", err)
			}
		} else {
			rows = rows[:n]
		}
		if more {
			page.NextCursor = last.key.String()
		}
	}
	for _, r := range rows {
		page.Items = append(page.Items, r.tx)
	}
	return page, nil
}

func (e *Explorer) readActions(ctx context.Context, query string, args ...any) ([]actionRow, error) {
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("explorer: read transactions: %w", err)
	}
	defer rows.Close()
	out := []actionRow{}
	for rows.Next() {
		r, err := e.scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("explorer: read transactions: %w", err)
	}
	return out, nil
}

func (e *Explorer) scanAction(rows *sql.Rows) (actionRow, error) {
	var (
		r                                actionRow
		txHash, blockHash, emitter, acct []byte
		at                               time.Time
		kind, stored                     string
		symbol, side, rawPrice, rawSize  sql.NullString
	)
	if err := rows.Scan(&r.key.Block, &r.key.TxIndex, &r.key.LogIndex, &r.sub, &txHash, &blockHash, &at, &emitter, &kind, &acct,
		&stored, &symbol, &side, &rawPrice, &rawSize); err != nil {
		return actionRow{}, fmt.Errorf("explorer: scan transaction: %w", err)
	}
	if len(txHash) != common.HashLength || len(blockHash) != common.HashLength || len(emitter) != common.AddressLength || len(acct) != common.AddressLength {
		return actionRow{}, fmt.Errorf("explorer: malformed stored key at %s", r.key)
	}
	action, err := Action(kind)
	if err != nil {
		return actionRow{}, err
	}
	if stored != action {
		return actionRow{}, fmt.Errorf("%w: kind %q stored %q, mapped %q", ErrActionMismatch, kind, stored, action)
	}
	user, err := bech32.ConvertAndEncode(userPrefix, acct)
	if err != nil {
		return actionRow{}, fmt.Errorf("explorer: user of %s: %w", r.key, err)
	}
	// An Oracle Updated row stands for its block, so it carries the block hash;
	// every other row carries its stored transaction or synthetic receipt hash.
	hash := common.BytesToHash(txHash)
	if kind == KindPricefeedAggregate {
		hash = common.BytesToHash(blockHash)
	}
	sizeScale := spotScale
	switch {
	case action == ActionOracleUpdated || action == ActionSealedBatchExecuted:
		sizeScale = unitScale
	case common.BytesToAddress(emitter) == perpsAddress:
		sizeScale = usdScale
	}
	price, err := scaled(rawPrice, usdScale)
	if err != nil {
		return actionRow{}, fmt.Errorf("explorer: price of %s: %w", r.key, err)
	}
	size, err := scaled(rawSize, sizeScale)
	if err != nil {
		return actionRow{}, fmt.Errorf("explorer: size of %s: %w", r.key, err)
	}
	r.tx = Transaction{
		Hash:   hash.Hex(),
		Action: action,
		Block:  r.key.Block,
		Time:   at.UTC().Format(isoMillis),
		User:   user,
		Symbol: nullable(symbol),
		Side:   nullable(side),
		Price:  price,
		Size:   size,
		Link:   e.TxLink(hash),
	}
	return r, nil
}

func nullable(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}

// scaled divides a stored unsigned integer by scale, nil for a null column.
func scaled(raw sql.NullString, scale *big.Rat) (*float64, error) {
	if !raw.Valid {
		return nil, nil
	}
	v, ok := new(big.Rat).SetString(raw.String)
	if !ok || !v.IsInt() {
		return nil, fmt.Errorf("noncanonical integer %q", raw.String)
	}
	f, _ := v.Quo(v, scale).Float64()
	return &f, nil
}

const latestBlockQuery = `SELECT coalesce(max(number), -1) FROM perps.blocks`

// AccountHistory returns one page of the pnl.History rows of account below
// cursor ("" for the first page), ordered by (block, tx_index, log_index,
// sub) descending; limit is clamped like Transactions. The rows are read in
// block windows that double until the page fills, and a page never splits the
// rows of one (block, tx_index, log_index).
func (e *Explorer) AccountHistory(ctx context.Context, account common.Address, cursor, limit string) (Page[HistoryItem], error) {
	c, err := parseCursor(cursor)
	if err != nil {
		return Page[HistoryItem]{}, err
	}
	n, err := clamp(limit, defaultTransactions, maxTransactions)
	if err != nil {
		return Page[HistoryItem]{}, err
	}
	page := Page[HistoryItem]{Items: []HistoryItem{}}
	hi := c.Block
	if c == top {
		if err := e.db.QueryRowContext(ctx, latestBlockQuery).Scan(&hi); err != nil {
			return Page[HistoryItem]{}, fmt.Errorf("explorer: read latest block: %w", err)
		}
	}
	var got []pnl.Entry
	for span := int64(historyWindow); hi >= 0; span *= 2 {
		lo := int64(0)
		if hi >= span {
			lo = hi - span + 1
		}
		entries, err := pnl.History(ctx, e.db, account, lo, hi, nil)
		if err != nil {
			return Page[HistoryItem]{}, fmt.Errorf("explorer: %w", err)
		}
		for _, en := range entries {
			if c.after(entryKey(en)) {
				got = append(got, en)
			}
		}
		if cut := groupCut(got, n); cut < len(got) {
			page.NextCursor = entryKey(got[cut-1]).String()
			got = got[:cut]
			break
		}
		hi = lo - 1
	}
	for _, en := range got {
		page.Items = append(page.Items, HistoryItem{
			Kind:        en.Kind,
			Event:       en.Event,
			Block:       en.Block,
			TxIndex:     en.TxIndex,
			LogIndex:    en.LogIndex,
			Hash:        en.TxHash.Hex(),
			ReceiptKind: en.ReceiptKind,
			Time:        en.BlockTime.UTC().Format(isoMillis),
			Args:        en.Args,
			Link:        e.TxLink(en.TxHash),
		})
	}
	return page, nil
}

func entryKey(en pnl.Entry) Cursor {
	return Cursor{Block: en.Block, TxIndex: en.TxIndex, LogIndex: en.LogIndex}
}

// groupCut is the length of the first n entries extended by the rest of the
// last entry's (block, tx_index, log_index).
func groupCut(entries []pnl.Entry, n int) int {
	if len(entries) <= n {
		return len(entries)
	}
	cut := n
	for cut < len(entries) && entryKey(entries[cut]) == entryKey(entries[n-1]) {
		cut++
	}
	return cut
}
