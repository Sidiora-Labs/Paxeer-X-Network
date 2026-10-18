package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrCursorInvalid = errors.New("CURSOR_INVALID")
var ErrCursorExpired = errors.New("CURSOR_EXPIRED")
var ErrCursorScope = errors.New("CURSOR_SCOPE_MISMATCH")
var ErrPoolNotFound = errors.New("POOL_NOT_FOUND")

// ReadModelContract is a migration integration request, not an API-owned
// migration. Projection owners must commit snapshot and applied_event together
// and only append immutable canonical trade rows. Revisions are retained for
// at least the 15 minute cursor lifetime. Numeric columns are NUMERIC(78,0).
const ReadModelContract = `
market_snapshots(chain_id TEXT, pool_address TEXT, revision NUMERIC(78,0),
 applied_event NUMERIC(78,0), chain_height NUMERIC(78,0), block_hash TEXT,
 observed_at TIMESTAMPTZ, base JSONB,
 PRIMARY KEY(chain_id,pool_address,revision));
market_trades(chain_id TEXT,pool_address TEXT,block_height NUMERIC(78,0),
 transaction_index NUMERIC(78,0),log_index NUMERIC(78,0),event_id TEXT,
 applied_event NUMERIC(78,0),payload JSONB,
 PRIMARY KEY(chain_id,pool_address,event_id));
INDEX market_trades_order ON market_trades
 (chain_id,pool_address,block_height DESC,transaction_index DESC,log_index DESC,event_id DESC)
 INCLUDE(applied_event);
`

type ChainOrder struct {
	Height           UInt256 `json:"height"`
	TransactionIndex UInt256 `json:"transactionIndex"`
	LogIndex         UInt256 `json:"logIndex"`
	EventID          string  `json:"eventId"`
}
type ReadWatermark struct {
	ChainID            string    `json:"chainId"`
	Height             UInt256   `json:"height"`
	BlockHash          string    `json:"blockHash"`
	ProjectionRevision UInt256   `json:"projectionRevision"`
	ObservedAt         time.Time `json:"observedAt"`
	Integrity          string    `json:"integrity"`
	AppliedEvent       UInt256   `json:"appliedEvent"`
}
type cursorPayload struct {
	Version      int           `json:"version"`
	Chain        string        `json:"chain"`
	Pool         string        `json:"pool"`
	FilterDigest string        `json:"filterDigest"`
	Order        ChainOrder    `json:"sortKey"`
	Watermark    ReadWatermark `json:"watermark"`
	Size         int           `json:"pageSize"`
	Issued       int64         `json:"issued"`
	Expiry       int64         `json:"expiry"`
}

// CursorKeys are supplied by the coordinator's key provider, not hardcoded.
// Previous keys may be retained during rotation for the full cursor lifetime.
type CursorKeys struct {
	active string
	keys   map[string]cipher.AEAD
}

func NewCursorKeys(active string, keys map[string][]byte) (*CursorKeys, error) {
	c := &CursorKeys{active: active, keys: map[string]cipher.AEAD{}}
	for id, key := range keys {
		if id == "" || len(id) > 32 || strings.ContainsAny(id, ". \t\n") || len(key) != 32 {
			return nil, ErrCursorInvalid
		}
		block, e := aes.NewCipher(key)
		if e != nil {
			return nil, e
		}
		a, e := cipher.NewGCM(block)
		if e != nil {
			return nil, e
		}
		c.keys[id] = a
	}
	if c.keys[active] == nil {
		return nil, ErrCursorInvalid
	}
	return c, nil
}
func (c *CursorKeys) seal(p cursorPayload) (string, error) {
	b, e := json.Marshal(p)
	if e != nil {
		return "", e
	}
	a := c.keys[c.active]
	nonce := make([]byte, a.NonceSize())
	if _, e = io.ReadFull(rand.Reader, nonce); e != nil {
		return "", e
	}
	token := c.active + "." + base64.RawURLEncoding.EncodeToString(a.Seal(nonce, nonce, b, []byte("kindle:trade-cursor:v1:"+c.active)))
	if len(token) > 2048 {
		return "", ErrCursorInvalid
	}
	return token, nil
}
func (c *CursorKeys) open(token, pool, digest string, size int, now time.Time) (cursorPayload, error) {
	var p cursorPayload
	if len(token) > 2048 {
		return p, ErrCursorInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return p, ErrCursorInvalid
	}
	a := c.keys[parts[0]]
	if a == nil {
		return p, ErrCursorInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || len(b) < a.NonceSize()+a.Overhead() {
		return p, ErrCursorInvalid
	}
	b, e = a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte("kindle:trade-cursor:v1:"+parts[0]))
	if e != nil || json.Unmarshal(b, &p) != nil || p.Version != 1 || p.Chain != "125" || p.Expiry-p.Issued != 900 || p.Issued > now.Unix() {
		return p, ErrCursorInvalid
	}
	if now.Unix() >= p.Expiry {
		return p, ErrCursorExpired
	}
	if p.Pool != pool || p.FilterDigest != digest || p.Size != size {
		return p, ErrCursorScope
	}
	return p, nil
}
func FilterDigest(canonicalFilters string) string {
	sum := sha256.Sum256([]byte(canonicalFilters))
	return "0x" + hex.EncodeToString(sum[:])
}

type DeterministicReader struct {
	pool              *pgxpool.Pool
	keys              *CursorKeys
	snapshots, trades string
}

// schema is trusted deployment configuration, never an HTTP parameter.
func NewDeterministicReader(pool *pgxpool.Pool, keys *CursorKeys, schema string) (*DeterministicReader, error) {
	if pool == nil || keys == nil || schema == "" || len(schema) > 63 {
		return nil, errors.New("invalid read model configuration")
	}
	for _, c := range schema {
		if !(c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return nil, errors.New("invalid schema")
		}
	}
	return &DeterministicReader{pool: pool, keys: keys, snapshots: pgx.Identifier{schema, "market_snapshots"}.Sanitize(), trades: pgx.Identifier{schema, "market_trades"}.Sanitize()}, nil
}

type SnapshotRead struct {
	Base      json.RawMessage `json:"base"`
	Watermark ReadWatermark   `json:"watermark"`
}
type OrderedTrade struct {
	Order   ChainOrder      `json:"order"`
	Payload json.RawMessage `json:"payload"`
}
type TradePage struct {
	SnapshotRead
	Trades     []OrderedTrade `json:"trades"`
	NextCursor *string        `json:"nextCursor"`
	HasMore    bool           `json:"hasMore"`
}

func (r *DeterministicReader) read(ctx context.Context, pool string, revision UInt256, fn func(context.Context, pgx.Tx, SnapshotRead) error) error {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return e
	}
	defer rollbackRead(tx)
	if _, e = tx.Exec(ctx, "SET LOCAL statement_timeout = '150ms'"); e != nil {
		return e
	}
	var s SnapshotRead
	var blockHash *string
	var observedAt *time.Time
	s.Watermark.ChainID = "125"
	s.Watermark.Integrity = "healthy"
	e = tx.QueryRow(ctx, fmt.Sprintf(`SELECT base,revision::text,applied_event::text,chain_height::text,block_hash,observed_at FROM %s AS s WHERE chain_id='125' AND pool_address=$1 AND ($2::numeric=0 OR revision=$2::numeric) ORDER BY s.revision DESC LIMIT 1`, r.snapshots), pool, string(revision)).Scan(&s.Base, &s.Watermark.ProjectionRevision, &s.Watermark.AppliedEvent, &s.Watermark.Height, &blockHash, &observedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrPoolNotFound
	}
	if e != nil {
		return e
	}
	if blockHash == nil || observedAt == nil {
		return ErrDataIntegrity
	}
	s.Watermark.BlockHash = *blockHash
	s.Watermark.ObservedAt = *observedAt
	for _, v := range []UInt256{s.Watermark.ProjectionRevision, s.Watermark.AppliedEvent, s.Watermark.Height} {
		if _, e = ParseUInt256(string(v)); e != nil {
			return e
		}
	}
	if s.Watermark.ProjectionRevision == "0" || ValidatePublicJSON(s.Base) != nil || !validBlockHash(s.Watermark.BlockHash) || s.Watermark.ObservedAt.IsZero() {
		return ErrDataIntegrity
	}
	if e = fn(ctx, tx, s); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (r *DeterministicReader) Snapshot(ctx context.Context, pool string) (SnapshotRead, error) {
	var out SnapshotRead
	a, e := Address(pool)
	if e != nil {
		return out, e
	}
	e = r.read(ctx, a, "0", func(_ context.Context, _ pgx.Tx, s SnapshotRead) error { out = s; return nil })
	return out, e
}
func (r *DeterministicReader) Trades(ctx context.Context, pool, filters, token string, size int) (TradePage, error) {
	var out TradePage
	a, e := Address(pool)
	if e != nil {
		return out, e
	}
	if size == 0 {
		size = 50
	}
	if size < 1 || size > 100 || filters != "" {
		return out, ErrCursorInvalid
	}
	digest := FilterDigest(filters)
	now := time.Now().Unix()
	p := cursorPayload{Version: 1, Chain: "125", Pool: a, FilterDigest: digest, Size: size, Issued: now, Expiry: now + 900}
	revision := UInt256("0")
	if token != "" {
		p, e = r.keys.open(token, a, digest, size, time.Now())
		if e != nil {
			return out, e
		}
		revision = p.Watermark.ProjectionRevision
	}
	e = r.read(ctx, a, revision, func(ctx context.Context, tx pgx.Tx, s SnapshotRead) error {
		out.SnapshotRead = s
		if token != "" && !sameWatermark(s.Watermark, p.Watermark) {
			return ErrCursorScope
		}
		p.Watermark = s.Watermark
		// Query text depends only on trusted identifiers, never filters or cursor text.
		q := fmt.Sprintf(`SELECT block_height::text,transaction_index::text,log_index::text,event_id,payload FROM %s AS t WHERE chain_id='125' AND pool_address=$1 AND applied_event <= $2::numeric AND block_height <= $3::numeric`, r.trades)
		args := []any{a, string(s.Watermark.AppliedEvent), string(s.Watermark.Height)}
		if token != "" {
			q += ` AND (block_height,transaction_index,log_index,event_id) < ($4::numeric,$5::numeric,$6::numeric,$7)`
			args = append(args, string(p.Order.Height), string(p.Order.TransactionIndex), string(p.Order.LogIndex), p.Order.EventID)
		}
		q += fmt.Sprintf(` ORDER BY t.block_height DESC,t.transaction_index DESC,t.log_index DESC,t.event_id DESC LIMIT $%d`, len(args)+1)
		args = append(args, size+1)
		rows, e := tx.Query(ctx, q, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		out.Trades = make([]OrderedTrade, 0, size)
		bytes := len(s.Base)
		for rows.Next() {
			var t OrderedTrade
			if e = rows.Scan(&t.Order.Height, &t.Order.TransactionIndex, &t.Order.LogIndex, &t.Order.EventID, &t.Payload); e != nil {
				return e
			}
			for _, v := range []UInt256{t.Order.Height, t.Order.TransactionIndex, t.Order.LogIndex} {
				if _, e = ParseUInt256(string(v)); e != nil {
					return e
				}
			}
			bytes += len(t.Payload) + len(t.Order.EventID) + 256
			if bytes > 256<<10 || !json.Valid(t.Payload) || len(t.Order.EventID) > 160 {
				return ErrDataIntegrity
			}
			out.Trades = append(out.Trades, t)
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if len(out.Trades) > size {
			out.HasMore = true
			out.Trades = out.Trades[:size]
			p.Order = out.Trades[size-1].Order
			next, e := r.keys.seal(p)
			if e != nil {
				return e
			}
			out.NextCursor = &next
		}
		return nil
	})
	if token != "" && errors.Is(e, ErrPoolNotFound) {
		return TradePage{}, ErrCursorExpired
	}
	return out, e
}

func sameWatermark(a, b ReadWatermark) bool {
	return a.ChainID == b.ChainID && a.Height == b.Height && a.BlockHash == b.BlockHash && a.ProjectionRevision == b.ProjectionRevision && a.Integrity == b.Integrity && a.AppliedEvent == b.AppliedEvent && a.ObservedAt.Equal(b.ObservedAt)
}

func validBlockHash(hash string) bool {
	if len(hash) != 66 || !strings.HasPrefix(hash, "0x") {
		return false
	}
	for _, c := range hash[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}
