package source

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/stream"
)

const (
	pollInterval = 100 * time.Millisecond
	pollJitter   = 10 * time.Millisecond
	headCursor   = "head"
	rewindKind   = "rewind"
)

var (
	shareTokenCreatedTopic = crypto.Keccak256Hash([]byte("ShareTokenCreated(address,address)"))
	getRegisteredTokens    = common.FromHex("0x14cbd993")
	shareTokensSelector    = common.FromHex("0x96153f07")
)

// syntheticReceipts is the table of the synthetic receipts of a block of N
// consensus transactions: kind, hash tag and tx index N+offset.
var syntheticReceipts = []struct {
	kind   decode.ReceiptKind
	tag    string
	offset uint
}{
	{decode.ReceiptPerpsEndBlock, "perps-endblock", 0},
	{decode.ReceiptSpotEndBlock, "spot-endblock", 1},
	{decode.ReceiptActivation, "v6120-activation", 2},
}

// SyntheticHash is keccak256(tag || height as 8 bytes big-endian).
func SyntheticHash(tag string, height uint64) common.Hash {
	buf := make([]byte, len(tag)+8)
	copy(buf, tag)
	binary.BigEndian.PutUint64(buf[len(tag):], height)
	return crypto.Keccak256Hash(buf)
}

// factTables hold one row per decoded log, keyed by block.
var factTables = []string{
	"vaults", "collateral_events", "order_events", "fills", "position_events", "trade_settlements", "fees",
	"mark_prices", "funding_rates", "funding_settlements", "funding_payments", "open_interest", "liquidations",
	"backstop_liquidations", "insurance_events", "adl_events", "feed_keys", "index_prices", "spot_swaps",
	"spot_fee_events", "spot_order_events", "spot_vault_events", "spot_token_registry", "spot_admin",
	"spot_share_transfers", "usdl_transfers", "raw_logs", "protocol_events", "tx_actions",
}

// marketFactTables are the fact tables with a market_id column.
var marketFactTables = []string{
	"order_events", "fills", "position_events", "trade_settlements", "fees", "mark_prices", "funding_rates",
	"funding_settlements", "funding_payments", "open_interest", "liquidations", "backstop_liquidations",
	"insurance_events", "adl_events", "index_prices", "protocol_events",
}

// projectionDeletes drop the projection rows written at or above the fork
// ($1); $2 is the time of the fork block. perps.markets and perps.fee_config
// are never touched.
var projectionDeletes = []string{
	"DELETE FROM perps.keeper_cycles WHERE block >= $1",
	"DELETE FROM perps.positions_current WHERE opened_block >= $1 OR closed_block >= $1",
	"DELETE FROM perps.orders WHERE placed_block >= $1 OR closed_block >= $1",
	"DELETE FROM perps.spot_orders WHERE placed_block >= $1 OR closed_block >= $1",
	"DELETE FROM perps.account_margin WHERE updated_block >= $1",
	"DELETE FROM perps.user_stats WHERE updated_block >= $1",
	"DELETE FROM perps.market_stats_rolling WHERE updated_block >= $1",
	"DELETE FROM perps.spot_token_seen WHERE first_block >= $1",
	"DELETE FROM perps.market_snapshots WHERE hour >= date_trunc('hour', $2::timestamptz)",
	"DELETE FROM perps.protocol_snapshots WHERE hour >= date_trunc('hour', $2::timestamptz)",
}

// Block is one followed block with its receipt-classified logs.
type Block struct {
	Number     uint64
	Hash       common.Hash
	ParentHash common.Hash
	Time       time.Time
	// Proposer is the upper-case hex of the consensus proposer address.
	Proposer string
	// TxCount is the consensus transaction count N.
	TxCount             int
	Logs                []decode.Log
	FinalizeBlockEvents []abci.Event
}

// Affected names what a rewind discarded, for the projection rebuild.
type Affected struct {
	From     uint64
	FromTime time.Time
	Accounts []common.Address
	Markets  []string
}

// FollowerOptions configures a Follower. Write stores the facts of a block and
// Rebuild recomputes the projections of the affected accounts and markets;
// both run inside the follower's transaction.
type FollowerOptions struct {
	DB          *sql.DB
	RPC         *RPC
	Comet       *Comet
	Outbox      *delivery.Store
	ChainID     uint64
	StartHeight uint64
	Logger      *slog.Logger
	Write       func(ctx context.Context, tx *sql.Tx, b *Block) error
	Rebuild     func(ctx context.Context, tx *sql.Tx, a Affected) error
}

// SyntheticMismatchError stops the follower: the node served a receipt that
// does not match the synthetic receipt table.
type SyntheticMismatchError struct {
	Height uint64
	TxHash common.Hash
	// Kind is the synthetic kind of TxHash, empty for an unknown hash.
	Kind          decode.ReceiptKind
	ExpectedIndex uint
	ServedIndex   uint
}

func (e *SyntheticMismatchError) Error() string {
	if e.Kind == "" {
		return fmt.Sprintf("source: height %d: receipt %s is neither a body transaction nor a synthetic receipt, served tx index %d", e.Height, e.TxHash.Hex(), e.ServedIndex)
	}
	return fmt.Sprintf("source: height %d: %s receipt expected at tx index %d, served at %d", e.Height, e.Kind, e.ExpectedIndex, e.ServedIndex)
}

// Follower follows the head with parent-hash continuity and rewinds the
// stored chain to the fork on a reorganisation.
type Follower struct {
	opts     FollowerOptions
	logger   *slog.Logger
	subject  string
	pointers map[common.Address]struct{}
	stopped  error
}

// NewFollower validates opts and seeds the share pointer set from 0x1025.
func NewFollower(ctx context.Context, opts FollowerOptions) (*Follower, error) {
	if opts.DB == nil || opts.RPC == nil || opts.Comet == nil || opts.Outbox == nil || opts.Write == nil || opts.Rebuild == nil {
		return nil, errors.New("source: follower needs DB, RPC, Comet, Outbox, Write and Rebuild")
	}
	subject, err := stream.ChainSubject(opts.ChainID, rewindKind)
	if err != nil {
		return nil, err
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	f := &Follower{opts: opts, logger: logger, subject: subject, pointers: map[common.Address]struct{}{}}
	if err := f.seedPointers(ctx); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *Follower) seedPointers(ctx context.Context) error {
	out, err := f.opts.RPC.Call(ctx, SpotVault, getRegisteredTokens)
	if err != nil {
		return fmt.Errorf("source: getRegisteredTokens: %w", err)
	}
	addressSlice, err := abi.NewType("address[]", "", nil)
	if err != nil {
		return err
	}
	values, err := abi.Arguments{{Type: addressSlice}}.Unpack(out)
	if err != nil || len(values) != 1 {
		return fmt.Errorf("source: getRegisteredTokens decode: %v", err)
	}
	tokens, ok := values[0].([]common.Address)
	if !ok {
		return errors.New("source: getRegisteredTokens decode: not an address list")
	}
	for _, token := range tokens {
		word, err := f.opts.RPC.Call(ctx, SpotVault, append(append([]byte(nil), shareTokensSelector...), common.LeftPadBytes(token.Bytes(), 32)...))
		if err != nil {
			return fmt.Errorf("source: shareTokens(%s): %w", token.Hex(), err)
		}
		if len(word) != 32 || !bytes.Equal(word[:12], make([]byte, 12)) {
			return fmt.Errorf("source: shareTokens(%s) returned no address word", token.Hex())
		}
		if pointer := common.BytesToAddress(word[12:]); pointer != (common.Address{}) {
			f.pointers[pointer] = struct{}{}
		}
	}
	return nil
}

// Run steps until ctx ends, polling every 100 ms ± 10 ms when caught up. A
// synthetic receipt mismatch stops it.
func (f *Follower) Run(ctx context.Context) error {
	for {
		n, err := f.Step(ctx)
		if err != nil {
			var mismatch *SyntheticMismatchError
			if errors.As(err, &mismatch) || ctx.Err() != nil {
				return err
			}
			f.logger.Warn("source: follower step failed", slog.String("err", err.Error()))
		}
		if n > 0 && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval + rand.N(2*pollJitter+time.Millisecond) - pollJitter):
		}
	}
}

// Step stores the blocks from the cursor to at most MaxLogRange blocks ahead,
// stopping early after a rewind, and returns the number of blocks stored.
func (f *Follower) Step(ctx context.Context) (int, error) {
	if f.stopped != nil {
		return 0, f.stopped
	}
	next, err := f.next(ctx)
	if err != nil {
		return 0, err
	}
	head, err := f.opts.RPC.Head(ctx)
	if err != nil {
		return 0, err
	}
	if head < next {
		return 0, nil
	}
	to := min(head, next+MaxLogRange-1)
	logs, err := f.fetchLogs(ctx, next, to)
	if err != nil {
		return 0, err
	}
	byBlock := map[uint64][]ethtypes.Log{}
	for _, l := range logs {
		byBlock[l.BlockNumber] = append(byBlock[l.BlockNumber], l)
	}
	stored := 0
	for h := next; h <= to; h++ {
		evm, err := f.opts.RPC.Block(ctx, h)
		if err != nil {
			return stored, err
		}
		rewound, err := f.continuity(ctx, evm)
		if err != nil || rewound {
			return stored, err
		}
		b, err := f.assemble(ctx, evm, byBlock[h])
		if err != nil {
			var mismatch *SyntheticMismatchError
			if errors.As(err, &mismatch) {
				f.stopped = err
			}
			return stored, err
		}
		if err := f.commit(ctx, b); err != nil {
			return stored, err
		}
		stored++
	}
	return stored, nil
}

// Fetch reads height h from the node exactly as Step does, without the
// continuity check and without storing it; a replay rewrites a stored height
// from it.
func (f *Follower) Fetch(ctx context.Context, h uint64) (*Block, error) {
	logs, err := f.fetchLogs(ctx, h, h)
	if err != nil {
		return nil, err
	}
	evm, err := f.opts.RPC.Block(ctx, h)
	if err != nil {
		return nil, err
	}
	return f.assemble(ctx, evm, logs)
}

// assemble joins the EVM block, its logs and the consensus header and block
// results of its height into a Block.
func (f *Follower) assemble(ctx context.Context, evm *EVMBlock, logs []ethtypes.Log) (*Block, error) {
	h := evm.Number
	header, err := f.opts.Comet.Header(ctx, int64(h))
	if err != nil {
		return nil, err
	}
	results, err := f.opts.Comet.BlockResults(ctx, int64(h))
	if err != nil {
		return nil, err
	}
	for _, l := range logs {
		if l.BlockHash != evm.Hash {
			return nil, fmt.Errorf("source: height %d: log %d of block hash %s, block is %s", h, l.Index, l.BlockHash.Hex(), evm.Hash.Hex())
		}
	}
	classified, err := f.checkSynthetic(h, results.TxCount, evm.Transactions, logs, header.Time)
	if err != nil {
		return nil, err
	}
	return &Block{Number: h, Hash: evm.Hash, ParentHash: evm.ParentHash, Time: header.Time, Proposer: header.Proposer, TxCount: results.TxCount, Logs: classified, FinalizeBlockEvents: results.FinalizeBlockEvents}, nil
}

func (f *Follower) next(ctx context.Context) (uint64, error) {
	var height int64
	err := f.opts.DB.QueryRowContext(ctx, "SELECT height FROM perps.cursors WHERE name = $1", headCursor).Scan(&height)
	if errors.Is(err, sql.ErrNoRows) {
		return f.opts.StartHeight, nil
	}
	if err != nil {
		return 0, err
	}
	return uint64(height) + 1, nil
}

func (f *Follower) addresses(extra map[common.Address]struct{}) []string {
	out := make([]string, 0, len(VenueAddresses)+len(extra))
	for _, a := range VenueAddresses {
		out = append(out, strings.ToLower(a.Hex()))
	}
	for a := range extra {
		out = append(out, strings.ToLower(a.Hex()))
	}
	return out
}

// fetchLogs is the two-pass fetch: pass one reads the venue addresses and the
// known pointers, pass two the pointers that pass one's ShareTokenCreated
// logs of 0x1025 created, from their creation block.
func (f *Follower) fetchLogs(ctx context.Context, from, to uint64) ([]ethtypes.Log, error) {
	first, err := f.opts.RPC.Logs(ctx, FetchOptions{MonitoredAddresses: f.addresses(f.pointers), FromBlock: int64(from), ToBlock: int64(to)})
	if err != nil {
		return nil, err
	}
	created := map[common.Address]uint64{}
	lowest := to
	for _, l := range first.Logs {
		if l.Address != SpotVault || len(l.Topics) != 3 || l.Topics[0] != shareTokenCreatedTopic {
			continue
		}
		pointer := common.BytesToAddress(l.Topics[2].Bytes())
		if _, known := f.pointers[pointer]; known {
			continue
		}
		if _, seen := created[pointer]; !seen {
			created[pointer] = l.BlockNumber
			lowest = min(lowest, l.BlockNumber)
		}
	}
	logs := first.Logs
	if len(created) == 0 {
		return logs, nil
	}
	fresh := map[common.Address]struct{}{}
	pass := make([]string, 0, len(created))
	for p := range created {
		fresh[p] = struct{}{}
		pass = append(pass, strings.ToLower(p.Hex()))
	}
	second, err := f.opts.RPC.Logs(ctx, FetchOptions{MonitoredAddresses: pass, FromBlock: int64(lowest), ToBlock: int64(to)})
	if err != nil {
		return nil, err
	}
	for _, l := range second.Logs {
		if l.BlockNumber >= created[l.Address] {
			logs = append(logs, l)
		}
	}
	for p := range fresh {
		f.pointers[p] = struct{}{}
	}
	return logs, nil
}

// checkSynthetic classifies each log of block height: a receipt of a body
// transaction is kind tx with its sender; any other must be a synthetic
// receipt of the table at tx index N+offset.
func (f *Follower) checkSynthetic(height uint64, n int, body []EVMTx, logs []ethtypes.Log, blockTime time.Time) ([]decode.Log, error) {
	senders := make(map[common.Hash]common.Address, len(body))
	for _, tx := range body {
		senders[tx.Hash] = tx.From
	}
	out := make([]decode.Log, 0, len(logs))
	for _, l := range logs {
		if from, ok := senders[l.TxHash]; ok {
			sender := from
			out = append(out, decode.Log{Log: l, ReceiptKind: decode.ReceiptTx, TxFrom: &sender, BlockTime: blockTime})
			continue
		}
		var kind decode.ReceiptKind
		var expected uint
		for _, s := range syntheticReceipts {
			if l.TxHash == SyntheticHash(s.tag, height) {
				kind, expected = s.kind, uint(n)+s.offset
				break
			}
		}
		if kind == "" || l.TxIndex != expected {
			return nil, &SyntheticMismatchError{Height: height, TxHash: l.TxHash, Kind: kind, ExpectedIndex: expected, ServedIndex: l.TxIndex}
		}
		out = append(out, decode.Log{Log: l, ReceiptKind: kind, BlockTime: blockTime})
	}
	return out, nil
}

func (f *Follower) storedHash(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, height uint64) (common.Hash, bool, error) {
	var hash []byte
	err := q.QueryRowContext(ctx, "SELECT hash FROM perps.blocks WHERE number = $1", int64(height)).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return common.Hash{}, false, nil
	}
	if err != nil {
		return common.Hash{}, false, err
	}
	return common.BytesToHash(hash), true, nil
}

// continuity rewinds when a stored row at the block's height differs from it,
// or when the stored parent row differs from its parent hash.
func (f *Follower) continuity(ctx context.Context, b *EVMBlock) (bool, error) {
	same, ok, err := f.storedHash(ctx, f.opts.DB, b.Number)
	if err != nil {
		return false, err
	}
	if ok && same != b.Hash {
		_, err := f.rewind(ctx, b.Number)
		return true, err
	}
	if b.Number == 0 {
		return false, nil
	}
	parent, ok, err := f.storedHash(ctx, f.opts.DB, b.Number-1)
	if err != nil {
		return false, err
	}
	if ok && parent != b.ParentHash {
		f.logger.Warn("source: parent hash mismatch", slog.Uint64("height", b.Number), slog.String("stored_parent", parent.Hex()), slog.String("served_parent", b.ParentHash.Hex()))
		_, err := f.rewind(ctx, b.Number-1)
		return true, err
	}
	return false, nil
}

// rewind walks back from height to the highest stored height whose hash
// equals the node's and, in one transaction, deletes facts and projections at
// or above the fork, rebuilds the affected projections, moves the cursor to
// fork-1 and enqueues perps.<chain>.chain.rewind {"from":fork}.
func (f *Follower) rewind(ctx context.Context, height uint64) (uint64, error) {
	fork := height + 1
	for h := height; ; h-- {
		stored, ok, err := f.storedHash(ctx, f.opts.DB, h)
		if err != nil {
			return 0, err
		}
		if !ok {
			break
		}
		node, err := f.opts.RPC.Block(ctx, h)
		if err != nil {
			return 0, err
		}
		if node.Hash == stored {
			break
		}
		fork = h
		if h == 0 {
			break
		}
	}
	tx, err := f.opts.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var discarded []byte
	var forkTime time.Time
	err = tx.QueryRowContext(ctx, "SELECT hash, time FROM perps.blocks WHERE number = $1", int64(fork)).Scan(&discarded, &forkTime)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		discarded = make([]byte, common.HashLength)
		forkTime = time.Now().UTC()
	}
	affected, err := collectAffected(ctx, tx, fork)
	if err != nil {
		return 0, err
	}
	affected.FromTime = forkTime
	for _, table := range factTables {
		if _, err := tx.ExecContext(ctx, "DELETE FROM perps."+table+" WHERE block >= $1", int64(fork)); err != nil {
			return 0, err
		}
	}
	for _, q := range projectionDeletes {
		args := []any{int64(fork)}
		if strings.Contains(q, "$2") {
			args = []any{forkTime}
			q = strings.ReplaceAll(q, "$2", "$1")
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM perps.blocks WHERE number >= $1", int64(fork)); err != nil {
		return 0, err
	}
	if err := f.opts.Rebuild(ctx, tx, affected); err != nil {
		return 0, err
	}
	var below common.Hash
	found := false
	if fork > 0 {
		if below, found, err = f.storedHash(ctx, tx, fork-1); err != nil {
			return 0, err
		}
	}
	if found {
		_, err = tx.ExecContext(ctx, "INSERT INTO perps.cursors(name, height, hash, updated_at) VALUES($1, $2, $3, now()) ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height, hash = EXCLUDED.hash, updated_at = now()", headCursor, int64(fork-1), below.Bytes())
	} else {
		_, err = tx.ExecContext(ctx, "DELETE FROM perps.cursors WHERE name = $1", headCursor)
	}
	if err != nil {
		return 0, err
	}
	if err := f.enqueueRewind(ctx, tx, fork, common.BytesToHash(discarded)); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	f.logger.Warn("source: rewound", slog.Uint64("from", fork))
	return fork, nil
}

func collectAffected(ctx context.Context, tx *sql.Tx, fork uint64) (Affected, error) {
	a := Affected{From: fork}
	parts := make([]string, 0, len(factTables)+1)
	for _, t := range factTables {
		parts = append(parts, "SELECT account FROM perps."+t+" WHERE block >= $1")
	}
	parts = append(parts, "SELECT maker FROM perps.fills WHERE block >= $1")
	rows, err := tx.QueryContext(ctx, strings.Join(parts, " UNION ")+" ORDER BY 1", int64(fork))
	if err != nil {
		return a, err
	}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return a, err
		}
		a.Accounts = append(a.Accounts, common.BytesToAddress(b))
	}
	if err := rows.Close(); err != nil {
		return a, err
	}
	parts = parts[:0]
	for _, t := range marketFactTables {
		parts = append(parts, "SELECT market_id FROM perps."+t+" WHERE block >= $1 AND market_id IS NOT NULL")
	}
	rows, err = tx.QueryContext(ctx, "SELECT market_id::text FROM ("+strings.Join(parts, " UNION ")+") m ORDER BY market_id", int64(fork))
	if err != nil {
		return a, err
	}
	defer rows.Close()
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return a, err
		}
		a.Markets = append(a.Markets, m)
	}
	return a, rows.Err()
}

func (f *Follower) enqueueRewind(ctx context.Context, tx *sql.Tx, fork uint64, discarded common.Hash) error {
	emitted := time.Now().UTC().Truncate(time.Microsecond)
	// Each rewind is its own event: the emission time joins the identity, so
	// a chain that flips back and forth over one block emits every rewind.
	seed := make([]byte, 0, 12+8+32+8)
	seed = append(seed, "perps-rewind"...)
	seed = binary.BigEndian.AppendUint64(seed, fork)
	seed = append(seed, discarded.Bytes()...)
	seed = binary.BigEndian.AppendUint64(seed, uint64(emitted.UnixNano()))
	id, err := natsdriver.MsgID(fmt.Sprintf("%d:%d:%s:-1:0:0", f.opts.ChainID, fork, crypto.Keccak256Hash(seed).Hex()))
	if err != nil {
		return err
	}
	payload := []byte(`{"from":` + strconv.FormatUint(fork, 10) + `}`)
	height := strconv.FormatUint(fork, 10)
	_, err = f.opts.Outbox.Enqueue(ctx, tx, f.subject, delivery.Envelope{
		EventID:       id,
		EventType:     rewindKind,
		SchemaVersion: 1,
		ChainID:       f.opts.ChainID,
		SourceOrder:   delivery.SourceOrder{Height: height, TransactionIndex: "0", LogIndex: "0"},
		EmittedAt:     emitted,
		TraceID:       id,
		PayloadHash:   delivery.Hash(payload),
		Payload:       payload,
	})
	return err
}

// commit stores the block row, its facts and the head cursor in one transaction.
func (f *Follower) commit(ctx context.Context, b *Block) error {
	tx, err := f.opts.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "INSERT INTO perps.blocks(number, hash, parent_hash, time, proposer, tx_count) VALUES($1, $2, $3, $4, $5, $6)", int64(b.Number), b.Hash.Bytes(), b.ParentHash.Bytes(), b.Time, b.Proposer, b.TxCount); err != nil {
		return err
	}
	if err := f.opts.Write(ctx, tx, b); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO perps.cursors(name, height, hash, updated_at) VALUES($1, $2, $3, now()) ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height, hash = EXCLUDED.hash, updated_at = now()", headCursor, int64(b.Number), b.Hash.Bytes()); err != nil {
		return err
	}
	return tx.Commit()
}
