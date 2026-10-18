package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/explorer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/stream"
)

// chainID is the one chain the outbox admits (delivery.Envelope.Validate);
// the block write carries no chain of its own.
const chainID = 125

const (
	headCursor   = "head"
	outboxSchema = "perps"
	rebuildSpan  = 1000
)

// ErrBlockMismatch reports a fact row of another block than the one written.
var ErrBlockMismatch = errors.New("store: fact row of another block")

// Block is the consensus identity of one indexed block. The follower commit
// writes its perps.blocks row; WriteBlock writes none.
type Block struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Time       time.Time
	Proposer   string
	TxCount    int
}

// Cursor reads the head cursor of the indexer; ok is false before the first
// block is stored.
func Cursor(ctx context.Context, db *sql.DB) (uint64, bool, error) {
	var h int64
	err := db.QueryRowContext(ctx, `SELECT height FROM perps.cursors WHERE name = $1`, headCursor).Scan(&h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("store: read cursor: %w", err)
	}
	if h < 0 {
		return 0, false, fmt.Errorf("store: cursor height %d is negative", h)
	}
	return uint64(h), true, nil
}

// ErrLeaseHeld reports a write fenced off by the writer lease: holder does
// not hold a live lease of the name.
var ErrLeaseHeld = errors.New("store: writer lease held by another instance")

const fenceSQL = `SELECT 1 FROM perps.writer_lease WHERE name = $1 AND holder = $2 AND expires_at > clock_timestamp() FOR SHARE`

// Fence holds the live writer lease name of holder for the rest of tx, so no
// other instance takes it before tx ends, and refuses with ErrLeaseHeld when
// holder does not hold it.
func Fence(ctx context.Context, tx *sql.Tx, name, holder string) error {
	var one int
	switch err := tx.QueryRowContext(ctx, fenceSQL, name, holder).Scan(&one); {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: %s", ErrLeaseHeld, name)
	case err != nil:
		return fmt.Errorf("store: fence lease %s: %w", name, err)
	}
	return nil
}

// The subject kinds of the families of contracts/data-services.md section 3.
var (
	marketKinds     = []string{"fill", "order", "funding", "index", "liq", "mark", "oi", "market"}
	userKinds       = []string{"position", "order", "fill", "margin", "funding", "ledger"}
	chainKinds      = []string{"block", "tx", "rewind"}
	spotFamilyKinds = []string{"swap", "order", "vault"}
)

// Policy is the outbox admission policy of chain: the chain subjects exactly,
// the market, user and spot subjects by their shape, and every subject kind
// as an event type.
func Policy(chain uint64) delivery.Policy {
	p := delivery.Policy{Subjects: map[string]bool{}, EventTypes: map[string]bool{}}
	prefix := "perps." + strconv.FormatUint(chain, 10) + "."
	for _, k := range chainKinds {
		p.Subjects[prefix+"chain."+k] = true
		p.EventTypes[k] = true
	}
	for _, k := range marketKinds {
		p.Patterns = append(p.Patterns, prefix+"*."+k)
		p.EventTypes[k] = true
	}
	for _, k := range userKinds {
		p.Patterns = append(p.Patterns, prefix+"user.*."+k)
		p.EventTypes[k] = true
	}
	for _, k := range spotFamilyKinds {
		p.Patterns = append(p.Patterns, prefix+"spot.*."+k)
		p.EventTypes[k] = true
	}
	return p
}

// rowColumn is one stored column of a row type: its name and field index.
type rowColumn struct {
	name  string
	index []int
}

var columnCache sync.Map

func columnsOf(t reflect.Type) []rowColumn {
	if c, ok := columnCache.Load(t); ok {
		return c.([]rowColumn)
	}
	var out []rowColumn
	var walk func(t reflect.Type, prefix []int)
	walk = func(t reflect.Type, prefix []int) {
		for i := range t.NumField() {
			f := t.Field(i)
			index := append(slices.Clone(prefix), i)
			if f.Anonymous {
				walk(f.Type, index)
				continue
			}
			out = append(out, rowColumn{decode.ColumnName(f.Name), index})
		}
	}
	walk(t, nil)
	columnCache.Store(t, out)
	return out
}

// sqlValue converts a row field to its parameter.
func sqlValue(v reflect.Value) (any, error) {
	switch x := v.Interface().(type) {
	case common.Address:
		return x.Bytes(), nil
	case *common.Address:
		if x == nil {
			return nil, nil
		}
		return x.Bytes(), nil
	case common.Hash:
		return x.Bytes(), nil
	case []common.Hash:
		out := make(pq.ByteaArray, len(x))
		for i, h := range x {
			out[i] = h.Bytes()
		}
		return out, nil
	case [16]byte:
		return x[:], nil
	case []byte:
		return append([]byte{}, x...), nil
	case json.RawMessage:
		return string(x), nil
	case *string:
		if x == nil {
			return nil, nil
		}
		return *x, nil
	case decode.ReceiptKind:
		return string(x), nil
	case time.Time:
		return x.UTC(), nil
	case uint8:
		return int64(x), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	case int64, bool, string:
		return x, nil
	}
	return nil, fmt.Errorf("store: no column conversion for %s", v.Type())
}

// jsonValue converts a row field to its outbox payload value.
func jsonValue(v reflect.Value) (any, error) {
	switch x := v.Interface().(type) {
	case common.Address:
		return strings.ToLower(x.Hex()), nil
	case *common.Address:
		if x == nil {
			return nil, nil
		}
		return strings.ToLower(x.Hex()), nil
	case common.Hash:
		return strings.ToLower(x.Hex()), nil
	case [16]byte:
		return "0x" + hex.EncodeToString(x[:]), nil
	case []byte:
		return "0x" + hex.EncodeToString(x), nil
	case *string:
		if x == nil {
			return nil, nil
		}
		return *x, nil
	case decode.ReceiptKind:
		return string(x), nil
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano), nil
	case uint64:
		return strconv.FormatUint(x, 10), nil
	case uint8, int64, bool, string:
		return x, nil
	}
	return nil, fmt.Errorf("store: no payload conversion for %s", v.Type())
}

// setField sets a row field from its scanned column; a NULL sets only a
// pointer field.
func setField(f reflect.Value, raw any) error {
	if raw == nil {
		if f.Kind() == reflect.Pointer {
			return nil
		}
		return fmt.Errorf("store: NULL for %s", f.Type())
	}
	text := func() (string, bool) {
		switch r := raw.(type) {
		case []byte:
			return string(r), true
		case string:
			return r, true
		}
		return "", false
	}
	switch f.Interface().(type) {
	case common.Address, *common.Address:
		b, ok := raw.([]byte)
		if !ok || len(b) != common.AddressLength {
			return fmt.Errorf("store: address column holds %T", raw)
		}
		a := common.BytesToAddress(b)
		if f.Kind() == reflect.Pointer {
			f.Set(reflect.ValueOf(&a))
		} else {
			f.Set(reflect.ValueOf(a))
		}
	case common.Hash:
		b, ok := raw.([]byte)
		if !ok || len(b) != common.HashLength {
			return fmt.Errorf("store: hash column holds %T", raw)
		}
		f.Set(reflect.ValueOf(common.BytesToHash(b)))
	case [16]byte:
		b, ok := raw.([]byte)
		if !ok || len(b) != 16 {
			return fmt.Errorf("store: bytes16 column holds %T", raw)
		}
		var a [16]byte
		copy(a[:], b)
		f.Set(reflect.ValueOf(a))
	case []byte:
		b, ok := raw.([]byte)
		if !ok {
			return fmt.Errorf("store: bytea column holds %T", raw)
		}
		f.SetBytes(append([]byte{}, b...))
	case decode.ReceiptKind, string:
		s, ok := text()
		if !ok {
			return fmt.Errorf("store: text column holds %T", raw)
		}
		f.SetString(s)
	case time.Time:
		t, ok := raw.(time.Time)
		if !ok {
			return fmt.Errorf("store: time column holds %T", raw)
		}
		f.Set(reflect.ValueOf(t.UTC()))
	case bool:
		b, ok := raw.(bool)
		if !ok {
			return fmt.Errorf("store: boolean column holds %T", raw)
		}
		f.SetBool(b)
	case int64:
		n, ok := raw.(int64)
		if !ok {
			return fmt.Errorf("store: integer column holds %T", raw)
		}
		f.SetInt(n)
	case uint8, uint64:
		var n uint64
		switch r := raw.(type) {
		case int64:
			if r < 0 {
				return fmt.Errorf("store: unsigned column holds %d", r)
			}
			n = uint64(r)
		default:
			s, ok := text()
			if !ok {
				return fmt.Errorf("store: unsigned column holds %T", raw)
			}
			var err error
			if n, err = strconv.ParseUint(s, 10, 64); err != nil {
				return fmt.Errorf("store: unsigned column: %w", err)
			}
		}
		if f.OverflowUint(n) {
			return fmt.Errorf("store: %d overflows %s", n, f.Type())
		}
		f.SetUint(n)
	default:
		return fmt.Errorf("store: no scan conversion for %s", f.Type())
	}
	return nil
}

var insertCache sync.Map

// insertFact inserts row into perps.table unless its (tx_hash, log_index,
// sub) key is stored and reports whether it inserted.
func insertFact(ctx context.Context, tx *sql.Tx, table string, row any) (bool, error) {
	v := reflect.ValueOf(row)
	cols := columnsOf(v.Type())
	cacheKey := table + "/" + v.Type().String()
	q, ok := insertCache.Load(cacheKey)
	if !ok {
		names := make([]string, len(cols))
		marks := make([]string, len(cols))
		for i, c := range cols {
			names[i] = pq.QuoteIdentifier(c.name)
			marks[i] = "$" + strconv.Itoa(i+1)
		}
		q = `INSERT INTO perps.` + pq.QuoteIdentifier(table) + ` (` + strings.Join(names, ", ") + `) VALUES (` + strings.Join(marks, ", ") +
			`) ON CONFLICT (tx_hash, log_index, sub) DO NOTHING RETURNING row_id`
		insertCache.Store(cacheKey, q)
	}
	args := make([]any, len(cols))
	for i, c := range cols {
		a, err := sqlValue(v.FieldByIndex(c.index))
		if err != nil {
			return false, fmt.Errorf("store: %s.%s: %w", table, c.name, err)
		}
		args[i] = a
	}
	var id int64
	switch err := tx.QueryRowContext(ctx, q.(string), args...).Scan(&id); {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("store: insert perps.%s: %w", table, err)
	}
	return true, nil
}

// protocolEvent is a perps.protocol_events row, one per decoded log or block
// event at the key of its first fact row.
type protocolEvent struct {
	decode.Fact
	EventName string
	MarketID  *string
	Args      json.RawMessage
}

// txAction is a perps.tx_actions row (contracts/data-services.md section 16).
type txAction struct {
	decode.Fact
	Action string
	Symbol *string
	Side   *string
	Price  *string
	Size   *string
}

func ptr(s string) *string { return &s }

// WriteFacts writes the rows of one block into the fact tables, raw_logs,
// protocol_events and tx_actions and enqueues one outbox message per fact row,
// one chain.block message and one chain.tx message per tx_actions row, in tx.
// A stored key is left as it is; the rows it inserted are returned in order.
// It writes no perps.blocks row, no projection and no cursor.
func WriteFacts(ctx context.Context, tx *sql.Tx, block Block, rows *decode.Result) ([]decode.Row, error) {
	if tx == nil || rows == nil {
		return nil, errors.New("store: WriteFacts needs a transaction and rows")
	}
	if block.Time.IsZero() {
		return nil, errors.New("store: block time required")
	}
	check := func(f decode.Fact) error {
		if f.Block != block.Number || f.BlockHash != block.Hash {
			return fmt.Errorf("%w: row of block %d %s in block %d %s", ErrBlockMismatch, f.Block, f.BlockHash.Hex(), block.Number, block.Hash.Hex())
		}
		return nil
	}
	var inserted []decode.Row
	for _, d := range rows.Decoded {
		for _, row := range d.Rows {
			if err := check(row.Common()); err != nil {
				return nil, err
			}
			ok, err := insertFact(ctx, tx, row.Table(), row)
			if err != nil {
				return nil, err
			}
			if ok {
				inserted = append(inserted, row)
			}
		}
	}
	for _, raw := range rows.Raw {
		if err := check(raw.Fact); err != nil {
			return nil, err
		}
		if _, err := insertFact(ctx, tx, "raw_logs", raw); err != nil {
			return nil, err
		}
	}
	for _, d := range rows.Decoded {
		if len(d.Rows) == 0 {
			continue
		}
		args := d.Event.Args
		if args == nil {
			args = map[string]any{}
		}
		body, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("store: args of %s: %w", d.Event.EventName, err)
		}
		e := protocolEvent{Fact: d.Rows[0].Common(), EventName: d.Event.EventName, Args: body}
		e.Sub = 0
		for _, k := range []string{"marketId", "market_id"} {
			if m, ok := args[k].(string); ok {
				e.MarketID = ptr(m)
				break
			}
		}
		if _, err := insertFact(ctx, tx, "protocol_events", e); err != nil {
			return nil, err
		}
	}
	actions, err := txActions(ctx, tx, rows)
	if err != nil {
		return nil, err
	}
	for _, a := range actions {
		if _, err := insertFact(ctx, tx, "tx_actions", a); err != nil {
			return nil, err
		}
	}
	if err := enqueue(ctx, tx, block, rows, actions); err != nil {
		return nil, err
	}
	return inserted, nil
}

// WriteBlock writes the facts of one block with WriteFacts, folds the rows it
// inserted into the projections and writes the hourly snapshots, in tx. The
// follower commit inserts the perps.blocks row before and moves the cursor
// after it.
func WriteBlock(ctx context.Context, tx *sql.Tx, block Block, rows *decode.Result) error {
	inserted, err := WriteFacts(ctx, tx, block, rows)
	if err != nil {
		return err
	}
	if err := Project(ctx, tx, inserted); err != nil {
		return err
	}
	return Snapshotter{}.Snapshot(ctx, tx, block.Number, block.Time)
}

// field returns the named field of row, if it has one.
func field(row decode.Row, name string) (reflect.Value, bool) {
	v := reflect.ValueOf(row).FieldByName(name)
	return v, v.IsValid()
}

func marketOf(row decode.Row) (string, bool) {
	v, ok := field(row, "MarketID")
	if !ok || v.Kind() != reflect.String || v.String() == "" {
		return "", false
	}
	return v.String(), true
}

const symbolSQL = `SELECT symbol FROM perps.markets WHERE market_id = $1`

// txActions maps the decoded rows of one block to its tx_actions rows: one per
// log of the action mapping at the key of its first fact row, one Oracle
// Updated row per block with pricefeed aggregates, and one Sealed Batch
// Executed row per end-block receipt at the key of its first log with sub 1.
func txActions(ctx context.Context, tx *sql.Tx, rows *decode.Result) ([]txAction, error) {
	symbols := map[string]*string{}
	symbol := func(market string) (*string, error) {
		if s, ok := symbols[market]; ok {
			return s, nil
		}
		var s string
		switch err := tx.QueryRowContext(ctx, symbolSQL, market).Scan(&s); {
		case errors.Is(err, sql.ErrNoRows):
			symbols[market] = nil
		case err != nil:
			return nil, fmt.Errorf("store: symbol of market %s: %w", market, err)
		default:
			symbols[market] = &s
		}
		return symbols[market], nil
	}
	var out []txAction
	var oracle *txAction
	type receipt struct {
		fact decode.Fact
		logs map[int64]bool
	}
	var receipts []*receipt
	byTx := map[int64]*receipt{}
	sealed := func(f decode.Fact) {
		if f.ReceiptKind != decode.ReceiptPerpsEndBlock && f.ReceiptKind != decode.ReceiptSpotEndBlock {
			return
		}
		r, ok := byTx[f.TxIndex]
		if !ok {
			r = &receipt{fact: f, logs: map[int64]bool{}}
			byTx[f.TxIndex] = r
			receipts = append(receipts, r)
		}
		if f.LogIndex < r.fact.LogIndex {
			r.fact = f
		}
		r.logs[f.LogIndex] = true
	}
	for _, raw := range rows.Raw {
		sealed(raw.Fact)
	}
	for _, d := range rows.Decoded {
		if len(d.Rows) == 0 {
			continue
		}
		first := d.Rows[0]
		f := first.Common()
		sealed(f)
		if d.Event.EventName == decode.EventAggregate {
			if oracle == nil {
				oracle = &txAction{Fact: f, Action: explorer.ActionOracleUpdated}
				oracle.Kind, oracle.Sub = explorer.KindPricefeedAggregate, 0
			}
			n, err := strconv.ParseUint(*cmp.Or(oracle.Size, ptr("0")), 10, 64)
			if err != nil {
				return nil, err
			}
			oracle.Size = ptr(strconv.FormatUint(n+1, 10))
			continue
		}
		action, err := explorer.Action(d.Event.EventName)
		if err != nil {
			continue
		}
		a := txAction{Fact: f, Action: action}
		a.Kind, a.Sub = d.Event.EventName, 0
		if m, ok := marketOf(first); ok {
			if a.Symbol, err = symbol(m); err != nil {
				return nil, err
			}
		}
		if fill, ok := first.(decode.FillRow); ok {
			a.Account = fill.Taker
			a.Side = ptr(map[bool]string{true: "buy", false: "sell"}[fill.TakerIsLong])
			a.Price, a.Size = ptr(fill.Price), ptr(fill.SizeUSD)
		}
		out = append(out, a)
	}
	if oracle != nil {
		out = append(out, *oracle)
	}
	for _, r := range receipts {
		a := txAction{Fact: r.fact, Action: explorer.ActionSealedBatchExecuted, Size: ptr(strconv.Itoa(len(r.logs)))}
		a.Kind, a.Sub = string(r.fact.ReceiptKind), 1
		out = append(out, a)
	}
	return out, nil
}

// payload is the deterministic outbox payload of a stored row: its columns
// without row_id and its table.
func payload(table string, row any) ([]byte, error) {
	v := reflect.ValueOf(row)
	m := map[string]any{"table": table}
	for _, c := range columnsOf(v.Type()) {
		x, err := jsonValue(v.FieldByIndex(c.index))
		if err != nil {
			return nil, fmt.Errorf("store: payload of %s.%s: %w", table, c.name, err)
		}
		m[c.name] = x
	}
	return json.Marshal(m)
}

func eventID(f decode.Fact, hash common.Hash) (string, error) {
	return natsdriver.MsgID(fmt.Sprintf("%d:%d:%s:%d:%d:%d", chainID, f.Block, strings.ToLower(hash.Hex()), f.TxIndex, f.LogIndex, f.Sub))
}

func put(ctx context.Context, tx *sql.Tx, policy delivery.Policy, subject, kind, id string, f decode.Fact, body []byte, at time.Time) error {
	txIndex := "0"
	if f.TxIndex >= 0 {
		txIndex = strconv.FormatInt(f.TxIndex, 10)
	}
	_, err := delivery.EnqueueTx(ctx, tx, outboxSchema, policy, subject, delivery.Envelope{
		EventID:       id,
		EventType:     kind,
		SchemaVersion: 1,
		ChainID:       chainID,
		SourceOrder:   delivery.SourceOrder{Height: strconv.FormatUint(f.Block, 10), TransactionIndex: txIndex, LogIndex: strconv.FormatInt(f.LogIndex, 10)},
		EmittedAt:     at.UTC(),
		TraceID:       id,
		PayloadHash:   delivery.Hash(body),
		Payload:       body,
	})
	if err != nil {
		return fmt.Errorf("store: enqueue %s: %w", subject, err)
	}
	return nil
}

// tokensOf returns the spot tokens whose subjects a spot row wakes, the first
// one carrying the row's own event id. A simple swap wakes its tokenOut, or
// its tokenIn when tokenOut is a stablecoin: the base token sold into it. A
// native swap wakes its other token unless that is a stablecoin, and PAX (pax,
// zero while unregistered), whose tape lists every native swap. Any other row
// wakes its one token.
func tokensOf(row decode.Row, pax common.Address, stable map[common.Address]bool) []common.Address {
	var out []common.Address
	add := func(a common.Address) {
		if a != (common.Address{}) && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	switch r := row.(type) {
	case decode.SpotSimpleSwapRow:
		if stable[r.TokenOut] {
			add(r.TokenIn)
		} else {
			add(r.TokenOut)
		}
	case decode.SpotNativeSwapRow:
		if !stable[r.TokenOther] {
			add(r.TokenOther)
		}
		add(pax)
		if len(out) == 0 {
			add(r.TokenOther)
		}
	default:
		for _, name := range []string{"Token", "TokenOther", "TokenOut", "TokenIn"} {
			if v, ok := field(row, name); ok {
				if a, ok := v.Interface().(common.Address); ok && a != (common.Address{}) {
					add(a)
					break
				}
			}
		}
	}
	return out
}

const spotRegistrySQL = `SELECT token, is_stablecoin FROM perps.spot_token_registry WHERE kind = 'token_registered'
ORDER BY block, tx_index, log_index, sub`

// spotRegistry reads the registered spot tokens in tx: PAX, the first
// registered non-stablecoin token, and the stablecoins.
func spotRegistry(ctx context.Context, tx *sql.Tx) (common.Address, map[common.Address]bool, error) {
	rows, err := tx.QueryContext(ctx, spotRegistrySQL)
	if err != nil {
		return common.Address{}, nil, fmt.Errorf("store: spot registry: %w", err)
	}
	defer rows.Close()
	var pax common.Address
	stable := map[common.Address]bool{}
	for rows.Next() {
		var (
			raw      []byte
			isStable bool
		)
		if err := rows.Scan(&raw, &isStable); err != nil {
			return common.Address{}, nil, fmt.Errorf("store: spot registry: %w", err)
		}
		token := common.BytesToAddress(raw)
		if !isStable && pax == (common.Address{}) {
			pax = token
		}
		stable[token] = isStable
	}
	if err := rows.Err(); err != nil {
		return common.Address{}, nil, fmt.Errorf("store: spot registry: %w", err)
	}
	return pax, stable, nil
}

// marketKind and userKind route the perps fact tables; the spot tables route
// by spotKind.
var (
	marketKind = map[string]string{
		"fills": "fill", "order_events": "order", "funding_rates": "funding", "funding_settlements": "funding",
		"mark_prices": "mark", "index_prices": "index", "open_interest": "oi", "liquidations": "liq",
		"backstop_liquidations": "liq", "insurance_events": "liq",
	}
	userKind = map[string]string{
		"position_events": "position", "adl_events": "position", "trade_settlements": "ledger", "fees": "ledger",
		"feed_keys": "ledger", "usdl_transfers": "ledger", "collateral_events": "margin", "vaults": "margin",
		"funding_payments": "funding", "order_events": "order", "fills": "fill", "liquidations": "ledger",
		"backstop_liquidations": "ledger", "insurance_events": "ledger", "funding_rates": "ledger",
		"funding_settlements": "ledger", "mark_prices": "ledger", "index_prices": "ledger", "open_interest": "ledger",
		"spot_admin": "ledger", "spot_vault_events": "ledger", "spot_swaps": "ledger", "spot_fee_events": "ledger",
		"spot_order_events": "ledger", "spot_token_registry": "ledger", "spot_share_transfers": "ledger",
	}
	spotKind = map[string]string{
		"spot_swaps": "swap", "spot_fee_events": "swap", "spot_order_events": "order", "spot_vault_events": "vault",
		"spot_token_registry": "vault", "spot_share_transfers": "vault", "spot_admin": "vault",
	}
)

const orderMarketSQL = `SELECT market_id::text FROM perps.orders WHERE order_id = $1`

// enqueue writes the outbox messages of one block: a fact row goes to its
// market, spot token (tokensOf) or account subject, the block to chain.block
// and each tx_actions row to chain.tx. The ids of the chain messages carry
// keccak256("perps-block" || hash) and keccak256("perps-tx" || hash) in the
// hash slot, and a spot row's message to each further token
// keccak256("perps-spot" || hash || token), so they never meet the id of a
// fact row.
func enqueue(ctx context.Context, tx *sql.Tx, block Block, rows *decode.Result, actions []txAction) error {
	policy := Policy(chainID)
	var (
		pax    common.Address
		stable map[common.Address]bool
	)
	placed := map[string]string{}
	for _, d := range rows.Decoded {
		for _, row := range d.Rows {
			if p, ok := row.(decode.OrderPlacedRow); ok {
				placed[p.OrderID] = p.MarketID
			}
		}
	}
	for _, d := range rows.Decoded {
		for _, row := range d.Rows {
			f := row.Common()
			table := row.Table()
			var subject, kind string
			var err error
			market, hasMarket := marketOf(row)
			if !hasMarket && table == "order_events" {
				id, _ := field(row, "OrderID")
				if market, hasMarket = placed[id.String()]; !hasMarket {
					switch err := tx.QueryRowContext(ctx, orderMarketSQL, id.String()).Scan(&market); {
					case err == nil:
						hasMarket = true
					case !errors.Is(err, sql.ErrNoRows):
						return fmt.Errorf("store: market of order %s: %w", id.String(), err)
					}
				}
			}
			var tokens []common.Address
			if spotKind[table] != "" {
				if stable == nil {
					if pax, stable, err = spotRegistry(ctx, tx); err != nil {
						return err
					}
				}
				tokens = tokensOf(row, pax, stable)
			}
			switch k, isMarket := marketKind[table]; {
			case isMarket && hasMarket:
				n, perr := strconv.ParseUint(market, 10, 64)
				if perr != nil {
					return fmt.Errorf("store: market id %q: %w", market, perr)
				}
				kind = k
				subject, err = stream.MarketSubject(chainID, n, kind)
			case len(tokens) > 0:
				kind = spotKind[table]
				subject, err = stream.SpotSubject(chainID, tokens[0], kind)
			case userKind[table] != "":
				kind = userKind[table]
				subject, err = stream.UserSubject(chainID, f.Account, kind)
			default:
				return fmt.Errorf("store: no subject for %s rows", table)
			}
			if err != nil {
				return err
			}
			body, err := payload(table, row)
			if err != nil {
				return err
			}
			id, err := eventID(f, f.BlockHash)
			if err != nil {
				return err
			}
			if err := put(ctx, tx, policy, subject, kind, id, f, body, block.Time); err != nil {
				return err
			}
			for _, token := range tokens[1:] {
				if subject, err = stream.SpotSubject(chainID, token, kind); err != nil {
					return err
				}
				if id, err = eventID(f, crypto.Keccak256Hash([]byte("perps-spot"), f.BlockHash.Bytes(), token.Bytes())); err != nil {
					return err
				}
				if err := put(ctx, tx, policy, subject, kind, id, f, body, block.Time); err != nil {
					return err
				}
			}
		}
	}
	blockFact := decode.Fact{Block: block.Number, BlockHash: block.Hash, TxIndex: -1}
	body, err := json.Marshal(map[string]any{
		"number": strconv.FormatUint(block.Number, 10), "hash": strings.ToLower(block.Hash.Hex()),
		"parent_hash": strings.ToLower(block.ParentHash.Hex()), "time": block.Time.UTC().Format(time.RFC3339Nano),
		"proposer": block.Proposer, "tx_count": block.TxCount,
	})
	if err != nil {
		return err
	}
	id, err := eventID(blockFact, crypto.Keccak256Hash([]byte("perps-block"), block.Hash.Bytes()))
	if err != nil {
		return err
	}
	subject, err := stream.ChainSubject(chainID, "block")
	if err != nil {
		return err
	}
	if err := put(ctx, tx, policy, subject, "block", id, blockFact, body, block.Time); err != nil {
		return err
	}
	if subject, err = stream.ChainSubject(chainID, "tx"); err != nil {
		return err
	}
	txSeed := crypto.Keccak256Hash([]byte("perps-tx"), block.Hash.Bytes())
	for _, a := range actions {
		body, err := payload("tx_actions", a)
		if err != nil {
			return err
		}
		id, err := eventID(a.Fact, txSeed)
		if err != nil {
			return err
		}
		if err := put(ctx, tx, policy, subject, "tx", id, a.Fact, body, block.Time); err != nil {
			return err
		}
	}
	return nil
}

// rebuildTables are the projections Project and Snapshotter write.
var rebuildTables = []string{
	"positions_current", "orders", "spot_orders", "account_margin", "user_stats", "user_pnl_daily",
	"market_stats_rolling", "spot_token_seen", "keeper_cycles", "market_snapshots", "protocol_snapshots",
}

// factTable is one fact table as the store reads it back: its kinds, their
// row types and order, and the union of their columns.
type factTable struct {
	name    string
	kinds   map[string]reflect.Type
	order   map[string]int
	columns []string
}

var storedTables = sync.OnceValue(func() []factTable {
	var out []factTable
	at := map[string]int{}
	for i, k := range decode.KindRows() {
		j, ok := at[k.Table]
		if !ok {
			j = len(out)
			at[k.Table] = j
			out = append(out, factTable{name: k.Table, kinds: map[string]reflect.Type{}, order: map[string]int{}})
		}
		t := &out[j]
		typ := reflect.TypeOf(k.Row)
		t.kinds[k.Kind], t.order[k.Kind] = typ, i
		for _, c := range columnsOf(typ) {
			if !slices.Contains(t.columns, c.name) {
				t.columns = append(t.columns, c.name)
			}
		}
	}
	return out
})

type storedRow struct {
	row   decode.Row
	order int
}

// loadFacts reads the stored fact rows of blocks [from, to] by block, each
// block in (tx_index, log_index, sub) order.
func loadFacts(ctx context.Context, tx *sql.Tx, from, to int64) (map[uint64][]decode.Row, error) {
	byBlock := map[uint64][]storedRow{}
	for _, t := range storedTables() {
		quoted := make([]string, len(t.columns))
		at := make(map[string]int, len(t.columns))
		for i, c := range t.columns {
			quoted[i], at[c] = pq.QuoteIdentifier(c), i+1
		}
		rows, err := tx.QueryContext(ctx, `SELECT kind, `+strings.Join(quoted, ", ")+` FROM perps.`+pq.QuoteIdentifier(t.name)+
			` WHERE block BETWEEN $1 AND $2`, from, to)
		if err != nil {
			return nil, fmt.Errorf("store: read perps.%s: %w", t.name, err)
		}
		for rows.Next() {
			raw := make([]any, len(t.columns)+1)
			dest := make([]any, len(raw))
			for i := range raw {
				dest[i] = &raw[i]
			}
			if err := rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("store: scan perps.%s: %w", t.name, err)
			}
			kind, _ := raw[0].(string)
			typ, ok := t.kinds[kind]
			if !ok {
				rows.Close()
				return nil, fmt.Errorf("store: perps.%s holds kind %q of no row type", t.name, kind)
			}
			v := reflect.New(typ).Elem()
			for _, c := range columnsOf(typ) {
				if err := setField(v.FieldByIndex(c.index), raw[at[c.name]]); err != nil {
					rows.Close()
					return nil, fmt.Errorf("store: perps.%s.%s: %w", t.name, c.name, err)
				}
			}
			row := v.Interface().(decode.Row)
			b := row.Common().Block
			byBlock[b] = append(byBlock[b], storedRow{row, t.order[kind]})
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make(map[uint64][]decode.Row, len(byBlock))
	for b, rs := range byBlock {
		slices.SortStableFunc(rs, func(x, y storedRow) int {
			fx, fy := x.row.Common(), y.row.Common()
			return cmp.Or(cmp.Compare(fx.TxIndex, fy.TxIndex), cmp.Compare(fx.LogIndex, fy.LogIndex), cmp.Compare(fx.Sub, fy.Sub), cmp.Compare(x.order, y.order))
		})
		list := make([]decode.Row, len(rs))
		for i, r := range rs {
			list[i] = r.row
		}
		out[b] = list
	}
	return out, nil
}

// Rebuild re-projects the stored facts after a rewrite of blocks [from, to]:
// it clears the projections of Project and Snapshotter and folds every stored
// block in order, as a fresh index of the stored blocks writes them.
// ponytail: re-projects all stored blocks, O(all facts) per call; scope it to
// the accounts and markets of [from, to] if replays outgrow it.
func Rebuild(ctx context.Context, tx *sql.Tx, from, to int64) error {
	if tx == nil {
		return errors.New("store: Rebuild needs a transaction")
	}
	if from < 0 || to < from {
		return fmt.Errorf("store: rebuild range %d to %d", from, to)
	}
	for _, t := range rebuildTables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM perps.`+pq.QuoteIdentifier(t)); err != nil {
			return fmt.Errorf("store: clear perps.%s: %w", t, err)
		}
	}
	var lo, hi sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT min(number), max(number) FROM perps.blocks`).Scan(&lo, &hi); err != nil {
		return fmt.Errorf("store: block range: %w", err)
	}
	if !lo.Valid {
		return nil
	}
	for start := lo.Int64; start <= hi.Int64; start += rebuildSpan {
		end := min(start+rebuildSpan-1, hi.Int64)
		facts, err := loadFacts(ctx, tx, start, end)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT number, time FROM perps.blocks WHERE number BETWEEN $1 AND $2 ORDER BY number`, start, end)
		if err != nil {
			return fmt.Errorf("store: read blocks: %w", err)
		}
		type stored struct {
			number int64
			at     time.Time
		}
		var blocks []stored
		for rows.Next() {
			var b stored
			if err := rows.Scan(&b.number, &b.at); err != nil {
				rows.Close()
				return fmt.Errorf("store: scan blocks: %w", err)
			}
			blocks = append(blocks, b)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, b := range blocks {
			if err := Project(ctx, tx, facts[uint64(b.number)]); err != nil {
				return fmt.Errorf("store: rebuild block %d: %w", b.number, err)
			}
			if err := (Snapshotter{}).Snapshot(ctx, tx, uint64(b.number), b.at); err != nil {
				return fmt.Errorf("store: rebuild block %d: %w", b.number, err)
			}
		}
	}
	return nil
}
