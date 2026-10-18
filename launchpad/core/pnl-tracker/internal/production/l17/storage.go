package l17

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrRestart = errors.New("snapshot expired: restart required")
var ErrConflict = errors.New("idempotency identity has different content")

// DDL is a migration proposal. Install only into a coordinator-approved schema;
// no shared migration registry is modified by this package.
const DDL = `CREATE TABLE portfolio_snapshots (
 id uuid PRIMARY KEY, wallet text NOT NULL, source_event text NOT NULL UNIQUE,
 digest text NOT NULL, revision numeric(78,0) NOT NULL, expires_at timestamptz NOT NULL,
 summary jsonb NOT NULL, observed_at timestamptz NOT NULL, UNIQUE(wallet,revision));
 CREATE INDEX portfolio_snapshot_history_seek ON portfolio_snapshots(wallet,observed_at DESC,revision DESC);
 CREATE TABLE portfolio_snapshot_rows (
 snapshot_id uuid NOT NULL REFERENCES portfolio_snapshots(id), ordinal integer NOT NULL,
 payload jsonb NOT NULL, PRIMARY KEY(snapshot_id,ordinal));
 CREATE TABLE portfolio_outbox (
 event_id uuid PRIMARY KEY REFERENCES portfolio_snapshots(id), payload jsonb NOT NULL,
 published_at timestamptz);
 CREATE INDEX portfolio_outbox_pending ON portfolio_outbox(event_id) WHERE published_at IS NULL;`

type Repository struct {
	Pool          *pgxpool.Pool
	ReadAdmission *Admission
}

func opaqueID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

type Notice struct {
	Wallet     string `json:"wallet"`
	SnapshotID string `json:"snapshotId"`
	Revision   string `json:"revision"`
	Algorithm  string `json:"algorithm"`
}

// CommitSnapshot persists immutable rows, materialized summary and notification in
// one transaction. sourceEvent is the canonical fold/inbox identity from L16.
func (r Repository) CommitSnapshot(ctx context.Context, sourceEvent string, s Summary, positions []Position, expires time.Time) (Summary, error) {
	if r.Pool == nil {
		return s, ErrUnavailable
	}
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return s, e
	}
	defer rollbackPortfolio(tx)
	out, e := r.CommitSnapshotTx(ctx, tx, sourceEvent, s, positions, expires)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (r Repository) CommitSnapshotTx(ctx context.Context, tx pgx.Tx, sourceEvent string, s Summary, positions []Position, expires time.Time) (Summary, error) {
	if tx == nil || expires.IsZero() {
		return s, ErrInvalid
	}
	requestedSnapshot := s.SnapshotID
	if requestedSnapshot != "" && !validUUID(requestedSnapshot) {
		return s, ErrInvalid
	}
	if len(sourceEvent) == 0 || len(sourceEvent) > 256 || len(positions) > 100000 {
		return s, ErrInvalid
	}
	revision, e := uint256(s.Watermark.ProjectionRevision)
	if e != nil || revision.Sign() == 0 {
		return s, ErrInvalid
	}
	normalized := make([]Position, len(positions))
	for i, p := range positions {
		normalized[i], _, e = Derive(p)
		if e != nil {
			return s, e
		}
	}
	positions = normalized
	s, e = Aggregate(s.Wallet, requestedSnapshot, s.Watermark, positions)
	if e != nil {
		return s, e
	}
	payload, e := json.Marshal(struct {
		Summary   Summary
		Positions []Position
	}{s, positions})
	if e != nil {
		return s, e
	}
	digest := sha256.Sum256(payload)
	dh := hex.EncodeToString(digest[:])
	// Serialize duplicate identities, including concurrent retries before visibility.
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", sourceEvent); e != nil {
		return s, e
	}
	var existingDigest string
	var raw []byte
	e = tx.QueryRow(ctx, "SELECT digest,summary FROM portfolio_snapshots WHERE source_event=$1", sourceEvent).Scan(&existingDigest, &raw)
	if e == nil {
		if existingDigest != dh {
			return s, ErrConflict
		}
		e = json.Unmarshal(raw, &s)
		return s, e
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return s, e
	}
	if s.SnapshotID == "" {
		s.SnapshotID, e = opaqueID()
		if e != nil {
			return s, e
		}
	}
	raw, e = json.Marshal(s)
	if e != nil {
		return s, e
	}
	observedAt, e := time.Parse(time.RFC3339Nano, s.Watermark.ObservedAt)
	if e != nil {
		return s, ErrInvalid
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_snapshots(id,wallet,source_event,digest,revision,expires_at,summary,observed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", s.SnapshotID, s.Wallet, sourceEvent, dh, s.Watermark.ProjectionRevision, expires, raw, observedAt); e != nil {
		return s, e
	}
	for i, p := range positions {
		p, _, e = Derive(p)
		if e != nil {
			return s, e
		}
		data, e := json.Marshal(p)
		if e != nil {
			return s, e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO portfolio_snapshot_rows VALUES($1,$2,$3)", s.SnapshotID, i, data); e != nil {
			return s, e
		}
	}
	notice, _ := json.Marshal(Notice{s.Wallet, s.SnapshotID, s.Watermark.ProjectionRevision, Algorithm})
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_outbox(event_id,payload) VALUES($1,$2)", s.SnapshotID, notice); e != nil {
		return s, e
	}
	return s, nil
}

type Page struct {
	Summary   Summary    `json:"summary"`
	Positions []Position `json:"positions"`
	Next      string     `json:"next,omitempty"`
}

// Page uses indexed snapshot seek, so new committed revisions cannot alter pages.
func (r Repository) Page(ctx context.Context, wallet, snapshot string, after, limit int, now time.Time) (Page, error) {
	if r.Pool == nil {
		return Page{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	wallet, e := Wallet(wallet)
	if e != nil {
		return Page{}, e
	}
	admission := r.ReadAdmission
	if admission == nil {
		admission = defaultAdmission
	}
	release, e := admission.Acquire(ctx, wallet)
	if e != nil {
		return Page{}, e
	}
	defer release()
	limit, e = Limit(limit)
	if e != nil || after < -1 || after > 100000 || !validUUID(snapshot) {
		return Page{}, ErrInvalid
	}
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Page{}, e
	}
	defer rollbackPortfolio(tx)
	var raw []byte
	var expires time.Time
	var page Page
	e = tx.QueryRow(ctx, "SELECT summary,expires_at FROM portfolio_snapshots WHERE id=$1 AND wallet=$2", snapshot, wallet).Scan(&raw, &expires)
	if errors.Is(e, pgx.ErrNoRows) {
		return page, ErrRestart
	}
	if e != nil {
		return page, e
	}
	if !now.Before(expires) {
		return page, ErrRestart
	}
	if e = json.Unmarshal(raw, &page.Summary); e != nil {
		return page, e
	}
	rows, e := tx.Query(ctx, "SELECT payload FROM portfolio_snapshot_rows WHERE snapshot_id=$1 AND ordinal>$2 ORDER BY ordinal LIMIT $3", snapshot, after, limit)
	if e != nil {
		return page, e
	}
	defer rows.Close()
	page.Positions = []Position{}
	for rows.Next() {
		var raw []byte
		var p Position
		if e = rows.Scan(&raw); e != nil {
			return page, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			return page, e
		}
		page.Positions = append(page.Positions, p)
	}
	if e = rows.Err(); e != nil {
		return page, e
	}
	return page, tx.Commit(ctx)
}

// Cursor is a lane proposal because frozen CursorSortKey has no portfolio kind.
// It is distinct from the frozen wire Cursor and must not silently replace it.
type Cursor struct {
	Wallet   string `json:"wallet"`
	Snapshot string `json:"snapshot"`
	Filter   string `json:"filter"`
	Sort     string `json:"sort"`
	After    int    `json:"after"`
	Limit    int    `json:"limit"`
	Expires  int64  `json:"expires"`
	Revision string `json:"revision,omitempty"`
}

func SignCursor(key []byte, c Cursor) (string, error) {
	if len(key) < 32 {
		return "", ErrInvalid
	}
	if w, e := Wallet(c.Wallet); e != nil || w != c.Wallet {
		return "", ErrInvalid
	}
	if _, e := Limit(c.Limit); e != nil || c.Limit == 0 || c.After < -1 || len(c.Filter) > 256 || c.Sort != "snapshot_ordinal_v1" || !validUUID(c.Snapshot) {
		return "", ErrInvalid
	}
	if c.Revision != "" {
		if r, e := uint256(c.Revision); e != nil || r.Sign() == 0 {
			return "", ErrInvalid
		}
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	m := hmac.New(sha256.New, key)
	m.Write(b)
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)), nil
}
func VerifyCursor(key []byte, token, wallet, filter, sort string, now time.Time) (Cursor, error) {
	var c Cursor
	if len(key) < 32 || len(token) > 2048 {
		return c, ErrInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, ErrInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return c, ErrInvalid
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, ErrInvalid
	}
	m := hmac.New(sha256.New, key)
	m.Write(b)
	if !hmac.Equal(m.Sum(nil), sig) {
		return c, ErrInvalid
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, ErrInvalid
	}
	if _, e = SignCursor(key, c); e != nil {
		return c, e
	}
	w, e := Wallet(wallet)
	if e != nil || c.Wallet != w || c.Filter != filter || c.Sort != sort {
		return c, ErrInvalid
	}
	if now.Unix() >= c.Expires {
		return c, ErrRestart
	}
	return c, nil
}

// Pending notices can be redelivered after publish-before-ack; event_id is the
// stable client deduplication key. Actual JetStream delivery is integration work.
func (r Repository) Pending(ctx context.Context, limit int) ([][]byte, error) {
	limit, e := Limit(limit)
	if e != nil {
		return nil, e
	}
	rows, e := r.Pool.Query(ctx, "SELECT payload FROM portfolio_outbox WHERE published_at IS NULL ORDER BY event_id LIMIT $1", limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := [][]byte{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (r Repository) Ack(ctx context.Context, id string) error {
	_, e := r.Pool.Exec(ctx, "UPDATE portfolio_outbox SET published_at=now() WHERE event_id=$1 AND published_at IS NULL", id)
	return e
}

func SignPortfolioCursor(key []byte, c Cursor) (string, error) {
	if _, e := SignCursor(key, c); e != nil {
		return "", e
	}
	if c.Filter != "all" || c.Sort != "snapshot_ordinal_v1" || c.Revision == "" || c.After > 100000 || c.Expires <= 0 {
		return "", ErrInvalid
	}
	wallet, e := hex.DecodeString(c.Wallet[2:])
	if e != nil {
		return "", ErrInvalid
	}
	snapshot, e := hex.DecodeString(strings.ReplaceAll(c.Snapshot, "-", ""))
	if e != nil {
		return "", ErrInvalid
	}
	revision, e := uint256(c.Revision)
	if e != nil {
		return "", e
	}
	raw := make([]byte, 82)
	raw[0] = 2
	copy(raw[1:21], wallet)
	copy(raw[21:37], snapshot)
	revision.FillBytes(raw[37:69])
	binary.BigEndian.PutUint32(raw[69:73], uint32(int32(c.After)))
	raw[73] = byte(c.Limit)
	binary.BigEndian.PutUint64(raw[74:82], uint64(c.Expires))
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(Algorithm))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...)), nil
}
func VerifyPortfolioCursor(key []byte, token, wallet string, now time.Time) (Cursor, error) {
	var c Cursor
	if len(key) < 32 || len(token) > 256 {
		return c, ErrInvalid
	}
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil || len(raw) != 114 || raw[0] != 2 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return c, ErrInvalid
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(Algorithm))
	mac.Write(raw[:82])
	if !hmac.Equal(mac.Sum(nil), raw[82:]) {
		return c, ErrInvalid
	}
	snapshot := hex.EncodeToString(raw[21:37])
	snapshot = snapshot[:8] + "-" + snapshot[8:12] + "-" + snapshot[12:16] + "-" + snapshot[16:20] + "-" + snapshot[20:]
	c = Cursor{Wallet: "0x" + hex.EncodeToString(raw[1:21]), Snapshot: snapshot, Revision: new(big.Int).SetBytes(raw[37:69]).String(), Filter: "all", Sort: "snapshot_ordinal_v1", After: int(int32(binary.BigEndian.Uint32(raw[69:73]))), Limit: int(raw[73]), Expires: int64(binary.BigEndian.Uint64(raw[74:82]))}
	if _, e = SignPortfolioCursor(key, c); e != nil {
		return c, e
	}
	normalized, e := Wallet(wallet)
	if e != nil || normalized != c.Wallet {
		return c, ErrInvalid
	}
	if now.Unix() >= c.Expires {
		return c, ErrRestart
	}
	return c, nil
}

func rollbackPortfolio(tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}
func validExportObjectID(id string) bool {
	if len(id) < 36 || !validUUID(id[:36]) {
		return false
	}
	if len(id) == 36 {
		return true
	}
	if id[36] != '-' {
		return false
	}
	fence, e := strconv.ParseInt(id[37:], 10, 64)
	return e == nil && fence > 0 && strconv.FormatInt(fence, 10) == id[37:]
}
func syncExportDirectory(dir string) error {
	file, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer file.Close()
	return file.Sync()
}
func privateExportDirectory(dir string) error {
	info, e := os.Lstat(dir)
	if e != nil {
		return e
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrInvalid
	}
	return nil
}
