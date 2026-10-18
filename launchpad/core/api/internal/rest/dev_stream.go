package rest

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Development v2 stream source.
//
// Production serves GET /v2/snapshots, WS /v2/stream and SSE /v2/stream/sse
// from broker.PostgresStream over the api_stream projection, which the
// accounting projectors (stats-workers, trading-charts, pnl) append to. None of
// those projectors run outside production, so the development stream is the
// same broker.PostgresStream (same snapshot, resume cursor, replay, ticket and
// origin code) over a development schema that this projector fills from the
// development indexer tables:
//
//   - market.trades.<pool> and market.price.<pool>: one frame per
//     indexer.swaps row, completed with the receipt (block hash, transaction
//     index, emitting contract) read from NETWORK_RPC_URL and checked against
//     the Swap log itself. The watermark is the fill's own block with the
//     node-reported hash and projectionRevision = the per-pool fill revision,
//     exactly as production's append_market_trade requires.
//   - market.stats.<pool>: the development PoolStats document the market reads
//     serve (stats.pool_stats + indexer state), with the indexer-cursor
//     watermark (eth_getBlockByNumber reported hash) as its revision.
//
// Nothing here is reachable when NODE_ENV=production.

// DevelopmentStreamSchema is the schema the development stream projection
// lives in. It is distinct from the production api_stream schema.
const DevelopmentStreamSchema = "api_stream_dev"

const (
	developmentStreamBatch        = 100
	developmentStreamRetention    = 24 * time.Hour
	developmentStreamStatsSweep   = 5 * time.Second
	developmentStreamPruneEvery   = time.Hour
	developmentStreamTradesPrefix = "market.trades."
)

// developmentStreamLock serialises projectors across api replicas.
var developmentStreamLock = int64(binaryKey("sidiora:api:development-stream-projector"))

// swapEventTopic is EventEmitter.Swap, the event the development indexer
// turns into indexer.swaps rows (topic1 poolId, topic2 sender).
var swapEventTopic = crypto.Keccak256Hash([]byte("Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)")).Hex()

func binaryKey(s string) uint64 {
	sum := crypto.Keccak256([]byte(s))
	var v uint64
	for _, b := range sum[:8] {
		v = v<<8 | uint64(b)
	}
	return v >> 1
}

var developmentStreamDDL = []string{
	`CREATE SCHEMA IF NOT EXISTS ` + DevelopmentStreamSchema,
	`CREATE TABLE IF NOT EXISTS ` + DevelopmentStreamSchema + `.stream_generation(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),generation uuid NOT NULL DEFAULT gen_random_uuid())`,
	`INSERT INTO ` + DevelopmentStreamSchema + `.stream_generation(singleton) VALUES(true) ON CONFLICT DO NOTHING`,
	`CREATE TABLE IF NOT EXISTS ` + DevelopmentStreamSchema + `.stream_projection(topic text PRIMARY KEY CHECK(length(topic) BETWEEN 1 AND 200),partition_id bigint GENERATED ALWAYS AS IDENTITY UNIQUE,sequence bigint NOT NULL CHECK(sequence>=0),frame bytea,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),CHECK((sequence=0)=(frame IS NULL)),CHECK(frame IS NULL OR octet_length(frame)<=262144))`,
	`CREATE TABLE IF NOT EXISTS ` + DevelopmentStreamSchema + `.stream_delivery_outbox(partition_id bigint NOT NULL REFERENCES ` + DevelopmentStreamSchema + `.stream_projection(partition_id),sequence bigint NOT NULL CHECK(sequence>0),event_id text NOT NULL UNIQUE,frame bytea NOT NULL CHECK(octet_length(frame)<=262144),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(partition_id,sequence))`,
	`CREATE INDEX IF NOT EXISTS stream_delivery_retention ON ` + DevelopmentStreamSchema + `.stream_delivery_outbox(created_at)`,
	`CREATE OR REPLACE FUNCTION ` + DevelopmentStreamSchema + `.stream_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'immutable stream envelope'; END $$`,
	`DROP TRIGGER IF EXISTS stream_delivery_immutable ON ` + DevelopmentStreamSchema + `.stream_delivery_outbox`,
	`CREATE TRIGGER stream_delivery_immutable BEFORE UPDATE ON ` + DevelopmentStreamSchema + `.stream_delivery_outbox FOR EACH ROW EXECUTE FUNCTION ` + DevelopmentStreamSchema + `.stream_immutable()`,
	`CREATE TABLE IF NOT EXISTS ` + DevelopmentStreamSchema + `.stream_snapshots(snapshot_id uuid PRIMARY KEY,account_id uuid NOT NULL,session_id uuid NOT NULL,auth_epoch bigint NOT NULL CHECK(auth_epoch>=0),filter_digest text NOT NULL CHECK(filter_digest ~ '^[0-9a-f]{64}$'),generation uuid NOT NULL,scope jsonb NOT NULL,initial_offsets jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),expires_at timestamptz NOT NULL,CHECK(expires_at>created_at AND expires_at<=created_at+interval '72 hours'))`,
	`CREATE INDEX IF NOT EXISTS stream_snapshot_expiry ON ` + DevelopmentStreamSchema + `.stream_snapshots(expires_at)`,
	`CREATE TABLE IF NOT EXISTS ` + DevelopmentStreamSchema + `.projector_cursor(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),block_number bigint NOT NULL,log_index integer NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp())`,
	`INSERT INTO ` + DevelopmentStreamSchema + `.projector_cursor(singleton,block_number,log_index) VALUES(true,0,-1) ON CONFLICT DO NOTHING`,
}

// DevelopmentStreamOptions carries the development stream providers.
type DevelopmentStreamOptions struct {
	Pool  *pgxpool.Pool
	Store *store.Store
	// Redis, when set, wakes the projector on the indexer's swap and pool
	// state publications; the projector also polls every second.
	Redis *goredis.Client
	// ChainRPCURL completes swap positions from receipts and resolves the
	// indexer-cursor block hash. Empty leaves the projection idle.
	ChainRPCURL string
	// SigningKey signs resume cursors (at least 32 bytes).
	SigningKey []byte
	// Authority consumes stream tickets and checks sessions.
	Authority sharedauth.StreamAuthority
	Logger    *slog.Logger
}

// DevelopmentStream is the development v2 stream: the production
// broker.PostgresStream plus the projector that feeds it.
type DevelopmentStream struct {
	Stream *broker.PostgresStream
	pool   *pgxpool.Pool
	market *developmentMarket
	redis  *goredis.Client
	logger *slog.Logger

	mu       sync.Mutex
	decimals map[string]int
	epochs   map[string]string
	lastErr  string
	statsAt  time.Time
}

// NewDevelopmentStream creates the development projection schema (idempotent)
// and the stream over it.
func NewDevelopmentStream(ctx context.Context, o DevelopmentStreamOptions) (*DevelopmentStream, error) {
	if o.Pool == nil || o.Store == nil || o.Authority == nil || len(o.SigningKey) < 32 {
		return nil, errors.New("development stream providers required")
	}
	if err := ensureDevelopmentStreamSchema(ctx, o.Pool); err != nil {
		return nil, fmt.Errorf("development stream schema: %w", err)
	}
	chain, err := newDevelopmentChain(ctx, o.ChainRPCURL)
	if err != nil {
		return nil, err
	}
	stream, err := broker.NewPostgresStream(o.Pool, DevelopmentStreamSchema, o.SigningKey, o.Authority)
	if err != nil {
		return nil, err
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &DevelopmentStream{Stream: stream, pool: o.Pool, market: &developmentMarket{st: o.Store, chain: chain}, redis: o.Redis, logger: logger, decimals: map[string]int{}, epochs: map[string]string{}}, nil
}

// UnknownTopics returns the requested topics that name a pool the development
// indexer never registered (absent from indexer.pools): market.price,
// market.stats, market.trades and market.candles topics whose pool segment is
// a canonical address with no indexer.pools row. Such a topic can never be
// projected, so the snapshot and stream transports answer it with a
// deterministic, non-retryable outcome instead of DEPENDENCY_UNAVAILABLE. A
// read failure is returned as an error (callers keep the retryable 503).
func (d *DevelopmentStream) UnknownTopics(ctx context.Context, topics []string) ([]string, error) {
	byPool := map[string][]string{}
	pools := []string{}
	for _, topic := range topics {
		parts := strings.Split(topic, ".")
		if (len(parts) != 3 && len(parts) != 4) || parts[0] != "market" || parts[1] == "rankings" {
			continue
		}
		if normalized, err := store.Address(parts[2]); err != nil || normalized != parts[2] {
			continue
		}
		if _, seen := byPool[parts[2]]; !seen {
			pools = append(pools, parts[2])
		}
		byPool[parts[2]] = append(byPool[parts[2]], topic)
	}
	if len(pools) == 0 {
		return nil, nil
	}
	indexed, err := d.market.st.DevelopmentPoolsIndexed(ctx, pools)
	if err != nil {
		return nil, err
	}
	var unknown []string
	for _, pool := range pools {
		if !indexed[pool] {
			unknown = append(unknown, byPool[pool]...)
		}
	}
	return unknown, nil
}

func ensureDevelopmentStreamSchema(ctx context.Context, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, developmentStreamLock); err != nil {
		return err
	}
	for _, statement := range developmentStreamDDL {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func streamTable(name string) string { return DevelopmentStreamSchema + "." + name }

// Run projects until ctx ends: on every indexer publication, every second,
// and a stats sweep every five seconds. Outbox rows older than 24h and expired
// snapshot scopes are pruned hourly.
func (d *DevelopmentStream) Run(ctx context.Context) {
	wake := make(chan struct{}, 1)
	if d.redis != nil {
		sub := d.redis.Subscribe(ctx, constants.ChannelSwap, constants.ChannelPoolStateUpdated)
		defer sub.Close()
		messages := sub.Channel()
		go func() {
			for range messages {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
		}()
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	prune := time.NewTicker(developmentStreamPruneEvery)
	defer prune.Stop()
	for {
		d.report(d.Project(ctx))
		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			d.report(d.prune(ctx))
		case <-tick.C:
		case <-wake:
		}
	}
}

func (d *DevelopmentStream) report(err error) {
	message := ""
	if err != nil && !errors.Is(err, context.Canceled) {
		message = err.Error()
	}
	d.mu.Lock()
	changed := message != d.lastErr
	d.lastErr = message
	d.mu.Unlock()
	if changed && message != "" {
		d.logger.Warn("development stream projection degraded", slog.String("err", message))
	} else if changed {
		d.logger.Info("development stream projection healthy")
	}
}

func (d *DevelopmentStream) prune(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := d.pool.Exec(ctx, `DELETE FROM `+streamTable("stream_delivery_outbox")+` WHERE created_at<clock_timestamp()-$1::interval`, fmt.Sprintf("%d seconds", int(developmentStreamRetention.Seconds()))); err != nil {
		return err
	}
	_, err := d.pool.Exec(ctx, `DELETE FROM `+streamTable("stream_snapshots")+` WHERE expires_at<clock_timestamp()`)
	return err
}

// Project appends every indexed swap up to the indexer cursor, then refreshes
// the stats topics of the pools it touched (and of every projected pool once
// per sweep interval).
func (d *DevelopmentStream) Project(ctx context.Context) error {
	if d.market.chain.client == nil {
		return errors.New("NETWORK_RPC_URL is not configured")
	}
	touched := map[string]bool{}
	for i := 0; i < 50; i++ {
		n, err := d.projectTrades(ctx, touched)
		if err != nil {
			return err
		}
		if n < developmentStreamBatch {
			break
		}
	}
	sweep := false
	d.mu.Lock()
	if time.Since(d.statsAt) >= developmentStreamStatsSweep {
		sweep = true
		d.statsAt = time.Now()
	}
	d.mu.Unlock()
	if len(touched) == 0 && !sweep {
		return nil
	}
	return d.projectStats(ctx, touched, sweep)
}

type developmentSwap struct {
	id, poolID, pool, sender, amountIn, amountOut, fee, txHash, token, optical string
	isBuy                                                                      bool
	block, timestamp                                                           int64
	logIndex                                                                   int32
}

type streamReceipt struct {
	Status           string `json:"status"`
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	TransactionHash  string `json:"transactionHash"`
	TransactionIndex string `json:"transactionIndex"`
	Logs             []struct {
		Address  string   `json:"address"`
		Topics   []string `json:"topics"`
		Data     string   `json:"data"`
		LogIndex string   `json:"logIndex"`
		Removed  bool     `json:"removed"`
	} `json:"logs"`
}

// projectedSwap is one swap with its chain completion.
type projectedSwap struct {
	swap           developmentSwap
	position       accounting.SourcePosition
	emitter, epoch string
	tokenDecimals  int
}

func (d *DevelopmentStream) cursor(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, lock bool) (int64, int32, error) {
	var block int64
	var logIndex int32
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	err := q.QueryRow(ctx, `SELECT block_number,log_index FROM `+streamTable("projector_cursor")+` WHERE singleton`+suffix).Scan(&block, &logIndex)
	return block, logIndex, err
}

func (d *DevelopmentStream) projectTrades(ctx context.Context, touched map[string]bool) (int, error) {
	head, err := d.market.st.DevelopmentIndexerHead(ctx)
	if err != nil {
		return 0, err
	}
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	block, logIndex, err := d.cursor(rctx, d.pool, false)
	if err != nil {
		return 0, err
	}
	rows, err := d.pool.Query(rctx, `SELECT s.id, lower(s.pool_id), lower(COALESCE(p.pool_address,'')), lower(s.sender), s.is_buy, s.amount_in, s.amount_out, s.fee, s.block_number, s.block_timestamp, lower(s.tx_hash), s.log_index, lower(COALESCE(p.token_address,'')), lower(COALESCE(p.optical,''))
FROM indexer.swaps s LEFT JOIN indexer.pools p ON lower(p.pool_id)=lower(s.pool_id)
WHERE (s.block_number, s.log_index) > ($1, $2) AND s.block_number <= $3
ORDER BY s.block_number, s.log_index LIMIT $4`, block, logIndex, head.Height, developmentStreamBatch)
	if err != nil {
		return 0, err
	}
	var swaps []developmentSwap
	for rows.Next() {
		var s developmentSwap
		if err = rows.Scan(&s.id, &s.poolID, &s.pool, &s.sender, &s.isBuy, &s.amountIn, &s.amountOut, &s.fee, &s.block, &s.timestamp, &s.txHash, &s.logIndex, &s.token, &s.optical); err != nil {
			rows.Close()
			return 0, err
		}
		swaps = append(swaps, s)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	if len(swaps) == 0 {
		return 0, nil
	}
	completed, err := d.complete(ctx, swaps)
	if err != nil {
		return 0, err
	}
	last := swaps[len(swaps)-1]
	if err = d.appendTrades(ctx, block, logIndex, last.block, last.logIndex, completed, touched); err != nil {
		return 0, err
	}
	return len(swaps), nil
}

// complete resolves each swap's receipt, checks the Swap log against the
// indexed row and reads the emitter code epoch and token decimals. Swaps whose
// chain facts contradict the row (or are permanently unavailable) are skipped
// and logged; transport failures abort the batch so it is retried.
func (d *DevelopmentStream) complete(ctx context.Context, swaps []developmentSwap) ([]projectedSwap, error) {
	client := d.market.chain.client
	hashes := []string{}
	index := map[string]int{}
	for _, s := range swaps {
		if _, ok := index[s.txHash]; !ok && validHash(s.txHash) {
			index[s.txHash] = len(hashes)
			hashes = append(hashes, s.txHash)
		}
	}
	receipts := make([]*streamReceipt, len(hashes))
	batch := make([]rpc.BatchElem, len(hashes))
	for i, h := range hashes {
		batch[i] = rpc.BatchElem{Method: "eth_getTransactionReceipt", Args: []any{h}, Result: &receipts[i]}
	}
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if len(batch) > 0 {
		if err := client.BatchCallContext(rctx, batch); err != nil {
			return nil, err
		}
	}
	out := make([]projectedSwap, 0, len(swaps))
	for _, s := range swaps {
		i, ok := index[s.txHash]
		if !ok || batch[i].Error != nil || receipts[i] == nil {
			d.skip(s, "receipt unavailable")
			continue
		}
		p, reason := swapPosition(s, receipts[i])
		if reason != "" {
			d.skip(s, reason)
			continue
		}
		epoch, err := d.codeEpoch(rctx, p.emitter, s.block)
		if err != nil {
			if isTransport(err) {
				return nil, err
			}
			d.skip(s, "emitter code unavailable")
			continue
		}
		decimals, err := d.tokenDecimals(rctx, s.token, s.block)
		if err != nil {
			if isTransport(err) {
				return nil, err
			}
			d.skip(s, "token decimals unavailable")
			continue
		}
		p.epoch, p.tokenDecimals = epoch, decimals
		out = append(out, p)
	}
	return out, nil
}

func isTransport(err error) bool {
	var rpcErr rpc.Error
	return err != nil && !errors.As(err, &rpcErr) && !errors.Is(err, errChainReferenceUnavailable)
}

func (d *DevelopmentStream) skip(s developmentSwap, reason string) {
	d.logger.Warn("development stream skipped swap", slog.String("swap", s.id), slog.String("pool", s.pool), slog.String("reason", reason))
}

// swapPosition checks the receipt's Swap log at the indexed log index against
// the indexed row and returns its chain position and emitter.
func swapPosition(s developmentSwap, r *streamReceipt) (projectedSwap, string) {
	if s.pool == "" || s.token == "" {
		return projectedSwap{}, "pool not indexed"
	}
	height, errHeight := hexutil.DecodeUint64(r.BlockNumber)
	txIndex, errTx := hexutil.DecodeUint64(r.TransactionIndex)
	if errHeight != nil || errTx != nil || r.Status != "0x1" || height != uint64(s.block) || !validHash(r.BlockHash) || !strings.EqualFold(r.TransactionHash, s.txHash) {
		return projectedSwap{}, "receipt does not match the indexed swap"
	}
	for _, l := range r.Logs {
		li, err := hexutil.DecodeUint64(l.LogIndex)
		if err != nil || li != uint64(s.logIndex) {
			continue
		}
		if l.Removed || len(l.Topics) != 3 || len(l.Topics[2]) != 66 || !strings.EqualFold(l.Topics[0], swapEventTopic) || !strings.EqualFold(l.Topics[1], s.poolID) || !strings.EqualFold("0x"+l.Topics[2][26:], s.sender) {
			return projectedSwap{}, "log is not the indexed Swap"
		}
		data, err := hexutil.Decode(l.Data)
		if err != nil || len(data) != 7*32 {
			return projectedSwap{}, "Swap data malformed"
		}
		word := func(i int) *big.Int { return new(big.Int).SetBytes(data[i*32 : (i+1)*32]) }
		isBuy := word(0)
		if isBuy.BitLen() > 1 || (isBuy.Sign() == 1) != s.isBuy || word(1).String() != s.amountIn || word(2).String() != s.amountOut || word(3).String() != s.fee {
			return projectedSwap{}, "Swap amounts differ from the indexed row"
		}
		emitter := strings.ToLower(l.Address)
		if _, err := store.Address(emitter); err != nil {
			return projectedSwap{}, "emitter malformed"
		}
		pos := position(height, txIndex, li, s.txHash, r.BlockHash)
		if accounting.ValidateSourcePosition(pos) != nil {
			return projectedSwap{}, "position malformed"
		}
		return projectedSwap{swap: s, position: pos, emitter: emitter}, ""
	}
	return projectedSwap{}, "Swap log absent from receipt"
}

// codeEpoch is keccak256 of the emitter's runtime code, read at the first
// projected swap's block and cached for the emitter.
func (d *DevelopmentStream) codeEpoch(ctx context.Context, emitter string, block int64) (string, error) {
	d.mu.Lock()
	epoch, ok := d.epochs[emitter]
	d.mu.Unlock()
	if ok {
		return epoch, nil
	}
	var code hexutil.Bytes
	if err := d.market.chain.client.CallContext(ctx, &code, "eth_getCode", emitter, hexutil.EncodeUint64(uint64(block))); err != nil {
		return "", err
	}
	if len(code) == 0 {
		return "", errChainReferenceUnavailable
	}
	epoch = hex.EncodeToString(crypto.Keccak256(code))
	d.mu.Lock()
	d.epochs[emitter] = epoch
	d.mu.Unlock()
	return epoch, nil
}

// tokenDecimals reads ERC20 decimals() at the swap block (cached per token).
func (d *DevelopmentStream) tokenDecimals(ctx context.Context, token string, block int64) (int, error) {
	d.mu.Lock()
	decimals, ok := d.decimals[token]
	d.mu.Unlock()
	if ok {
		return decimals, nil
	}
	var raw hexutil.Bytes
	call := map[string]any{"to": token, "data": "0x313ce567"}
	if err := d.market.chain.client.CallContext(ctx, &raw, "eth_call", call, hexutil.EncodeUint64(uint64(block))); err != nil {
		return 0, err
	}
	value := new(big.Int).SetBytes(raw)
	if len(raw) != 32 || value.BitLen() > 8 {
		return 0, errChainReferenceUnavailable
	}
	decimals = int(value.Int64())
	d.mu.Lock()
	d.decimals[token] = decimals
	d.mu.Unlock()
	return decimals, nil
}

type topicHead struct {
	sequence int64
	revision *big.Int
	payload  map[string]any
}

func readTopicHead(ctx context.Context, tx pgx.Tx, topic string) (topicHead, error) {
	var h topicHead
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT sequence,frame FROM `+streamTable("stream_projection")+` WHERE topic=$1 FOR UPDATE`, topic).Scan(&h.sequence, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, nil
	}
	if err != nil || raw == nil {
		return h, err
	}
	var prior struct {
		Revision string         `json:"revision"`
		Payload  map[string]any `json:"payload"`
	}
	if err = json.Unmarshal(raw, &prior); err != nil {
		return h, broker.ErrFrame
	}
	revision, ok := new(big.Int).SetString(prior.Revision, 10)
	if !ok {
		return h, broker.ErrFrame
	}
	h.revision, h.payload = revision, prior.Payload
	return h, nil
}

func (d *DevelopmentStream) appendFrame(ctx context.Context, tx pgx.Tx, head topicHead, eventID, topic, revision, occurredAt string, watermark, payload any) error {
	wm, err := json.Marshal(watermark)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	f := broker.Frame{EventID: eventID, Topic: topic, Sequence: strconv.FormatInt(head.sequence+1, 10), Revision: revision, OccurredAt: occurredAt, Watermark: wm, Payload: body}
	return d.Stream.AppendProjectionTx(ctx, tx, f)
}

func wireTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// beginProjection opens a projection transaction holding the projector lock;
// ok is false when another replica holds it.
func (d *DevelopmentStream) beginProjection(ctx context.Context) (pgx.Tx, bool, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	var locked bool
	if err = tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, developmentStreamLock).Scan(&locked); err != nil || !locked {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, false, err
	}
	return tx, true, nil
}

func (d *DevelopmentStream) appendTrades(ctx context.Context, fromBlock int64, fromLog int32, toBlock int64, toLog int32, swaps []projectedSwap, touched map[string]bool) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, ok, err := d.beginProjection(ctx)
	if err != nil || !ok {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	block, logIndex, err := d.cursor(ctx, tx, true)
	if err != nil {
		return err
	}
	if block != fromBlock || logIndex != fromLog {
		return nil
	}
	observed := wireTime(time.Now())
	for _, p := range swaps {
		tradeTopic := developmentStreamTradesPrefix + p.swap.pool
		head, err := readTopicHead(ctx, tx, tradeTopic)
		if err != nil {
			return err
		}
		revision := big.NewInt(1)
		if head.revision != nil {
			revision.Add(head.revision, big.NewInt(1))
		}
		trade, price, err := developmentTradeDocuments(p)
		if err != nil {
			d.skip(p.swap, err.Error())
			continue
		}
		occurred := wireTime(time.Unix(p.swap.timestamp, 0))
		watermark := store.ChainWatermark{ChainID: "125", Height: p.position.Height, BlockHash: p.position.BlockHash, ProjectionRevision: revision.String(), ObservedAt: observed, Integrity: "healthy"}
		eventID := "125:" + p.position.TransactionHash + ":" + p.position.LogIndex
		if err = d.appendFrame(ctx, tx, head, eventID, tradeTopic, revision.String(), occurred, watermark, trade); err != nil {
			return fmt.Errorf("append %s: %w", tradeTopic, err)
		}
		if price != nil {
			priceTopic := "market.price." + p.swap.pool
			priceHead, err := readTopicHead(ctx, tx, priceTopic)
			if err != nil {
				return err
			}
			if err = d.appendFrame(ctx, tx, priceHead, "price:"+eventID, priceTopic, revision.String(), occurred, watermark, price); err != nil {
				return fmt.Errorf("append %s: %w", priceTopic, err)
			}
		}
		touched[p.swap.pool] = true
	}
	if _, err = tx.Exec(ctx, `UPDATE `+streamTable("projector_cursor")+` SET block_number=$1,log_index=$2,updated_at=clock_timestamp() WHERE singleton`, toBlock, toLog); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// developmentTradeDocuments renders a completed swap as the contract Trade
// and its gross-execution Price (paid input / actual output). The Price is
// nil when the token side is zero (no positive denominator exists).
func developmentTradeDocuments(p projectedSwap) (map[string]any, map[string]any, error) {
	s := p.swap
	for _, v := range []string{s.amountIn, s.amountOut, s.fee} {
		if _, err := store.ParseUInt256(v); err != nil {
			return nil, nil, errors.New("swap amounts malformed")
		}
	}
	if _, err := store.Address(s.sender); err != nil {
		return nil, nil, errors.New("swap sender malformed")
	}
	usdlAddress := strings.ToLower(profile.USDLAddress)
	side, tokenAmount, usdlAmount := "sell", s.amountIn, s.amountOut
	feeAsset, feeDecimals, disposition := s.token, p.tokenDecimals, "retained_token_reserve"
	if s.isBuy {
		side, tokenAmount, usdlAmount = "buy", s.amountOut, s.amountIn
		feeAsset, feeDecimals, disposition = usdlAddress, 6, "distributed_usdl"
	}
	// The input after fee is exact only without an optical: its beforeSwap
	// hook may reduce the input after paidInput is recorded.
	var priced, net any
	quality := "receipt_incomplete"
	if s.optical == zeroAddressHex {
		paid, _ := new(big.Int).SetString(s.amountIn, 10)
		fee, _ := new(big.Int).SetString(s.fee, 10)
		if paid.Cmp(fee) < 0 {
			return nil, nil, errors.New("swap fee exceeds paid input")
		}
		priced, net, quality = s.amountIn, new(big.Int).Sub(paid, fee).String(), "exact"
	}
	position := chainPosition(p.position)
	trade := map[string]any{
		"eventId":                "125:" + p.position.TransactionHash + ":" + p.position.LogIndex,
		"poolAddress":            s.pool,
		"sender":                 s.sender,
		"recipient":              nil,
		"side":                   side,
		"tokenAmountRaw":         tokenAmount,
		"usdlAmountRaw":          usdlAmount,
		"position":               position,
		"feeRaw":                 s.fee,
		"feeAssetAddress":        feeAsset,
		"feeAssetDecimals":       feeDecimals,
		"feeDisposition":         disposition,
		"paidInputRaw":           s.amountIn,
		"pricedInputRaw":         priced,
		"ammInputAfterFeeRaw":    net,
		"inputDerivationQuality": quality,
		"recipientAttribution":   "unavailable",
		"recipientEvidenceRefs":  []string{},
		"emittingContract":       p.emitter,
		"codeEpoch":              p.epoch,
	}
	if raw, err := json.Marshal(trade); err != nil || interfaces.ValidateJSON("Trade", raw) != nil {
		return nil, nil, errors.New("trade document invalid")
	}
	token, _ := new(big.Int).SetString(tokenAmount, 10)
	usdl, _ := new(big.Int).SetString(usdlAmount, 10)
	if token.Sign() == 0 {
		return trade, nil, nil
	}
	ratio := new(big.Rat).SetFrac(new(big.Int).Mul(usdl, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(p.tokenDecimals)), nil)), new(big.Int).Mul(token, big.NewInt(1000000)))
	asset := func(address string, decimals int) map[string]any {
		return map[string]any{"kind": "erc20", "chainId": "125", "address": address, "currencyCode": nil, "decimals": decimals}
	}
	price := map[string]any{
		"numerator":   ratio.Num().String(),
		"denominator": ratio.Denom().String(),
		"baseAsset":   asset(s.token, p.tokenDecimals),
		"quoteAsset":  asset(usdlAddress, 6),
		"source": map[string]any{
			"kind":            "gross_execution",
			"amountBasis":     "paid_input",
			"sourceEventId":   trade["eventId"],
			"position":        position,
			"providerId":      nil,
			"observedAt":      wireTime(time.Unix(s.timestamp, 0)),
			"sourceReference": nil,
		},
	}
	if raw, err := json.Marshal(price); err != nil || interfaces.ValidateJSON("Price", raw) != nil {
		return nil, nil, errors.New("price document invalid")
	}
	return trade, price, nil
}

const zeroAddressHex = "0x0000000000000000000000000000000000000000"

// projectStats appends a market.stats frame for each pool whose development
// PoolStats changed at a newer indexer-cursor watermark.
func (d *DevelopmentStream) projectStats(ctx context.Context, touched map[string]bool, sweep bool) error {
	pools := map[string]bool{}
	for p := range touched {
		pools[p] = true
	}
	if sweep {
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rows, err := d.pool.Query(rctx, `SELECT substr(topic, $1) FROM `+streamTable("stream_projection")+` WHERE starts_with(topic, $2)`, len(developmentStreamTradesPrefix)+1, developmentStreamTradesPrefix)
		if err != nil {
			cancel()
			return err
		}
		for rows.Next() {
			var pool string
			if err = rows.Scan(&pool); err != nil {
				rows.Close()
				cancel()
				return err
			}
			pools[pool] = true
		}
		rows.Close()
		cancel()
		if err = rows.Err(); err != nil {
			return err
		}
	}
	list := make([]string, 0, len(pools))
	for p := range pools {
		list = append(list, p)
	}
	for start := 0; start < len(list); start += developmentStreamBatch {
		end := start + developmentStreamBatch
		if end > len(list) {
			end = len(list)
		}
		if err := d.projectStatsBatch(ctx, list[start:end], touched); err != nil {
			return err
		}
	}
	return nil
}

func (d *DevelopmentStream) projectStatsBatch(ctx context.Context, pools []string, touched map[string]bool) error {
	in, err := d.market.readStatsInputs(ctx, pools)
	if err != nil {
		return err
	}
	wm, err := d.market.requiredWatermark(ctx, in.head, in.headErr)
	if err != nil {
		return err
	}
	docs, err := d.market.statsDocuments(ctx, in, pools, wm)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return nil
	}
	revision, ok := new(big.Int).SetString(wm.ProjectionRevision, 10)
	if !ok {
		return store.ErrDataIntegrity
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, locked, err := d.beginProjection(ctx)
	if err != nil || !locked {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	for _, pool := range pools {
		doc, ok := docs[pool]
		if !ok {
			continue
		}
		topic := "market.stats." + pool
		head, err := readTopicHead(ctx, tx, topic)
		if err != nil {
			return err
		}
		// Frames are strictly ordered by the indexer cursor: a client treats
		// an equal revision with different content as a projection conflict.
		if head.revision != nil && head.revision.Cmp(revision) >= 0 {
			continue
		}
		if head.payload != nil && !touched[pool] && sameStatsContent(head.payload, doc) {
			continue
		}
		occurred, _ := doc["windowEnd"].(string)
		if err = d.appendFrame(ctx, tx, head, "stats:"+pool+":"+wm.Height, topic, wm.ProjectionRevision, occurred, wm, doc); err != nil {
			return fmt.Errorf("append %s: %w", topic, err)
		}
	}
	return tx.Commit(ctx)
}

// sameStatsContent compares two PoolStats documents ignoring the watermark.
func sameStatsContent(prior map[string]any, next map[string]any) bool {
	raw, err := json.Marshal(next)
	if err != nil {
		return false
	}
	var decoded map[string]any
	if json.NewDecoder(bytes.NewReader(raw)).Decode(&decoded) != nil {
		return false
	}
	a, b := map[string]any{}, map[string]any{}
	for k, v := range prior {
		if k != "watermark" {
			a[k] = v
		}
	}
	for k, v := range decoded {
		if k != "watermark" {
			b[k] = v
		}
	}
	return reflect.DeepEqual(a, b)
}
