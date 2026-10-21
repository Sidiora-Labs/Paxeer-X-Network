package projectionstats

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Filter struct{ Pool, Side, ProjectionVersion string }
type Cursor struct {
	Version         int      `json:"version"`
	FilterHash      string   `json:"filterHash"`
	Position        Position `json:"position"`
	WatermarkHeight string   `json:"watermarkHeight"`
}

func (f Filter) Validate() error {
	if !address.MatchString(f.Pool) || (f.Side != "" && f.Side != "buy" && f.Side != "sell") || len(f.ProjectionVersion) == 0 || len(f.ProjectionVersion) > 64 {
		return ErrInvalid
	}
	return nil
}
func (f Filter) digest() string { b, _ := json.Marshal(f); return digestBytes(b) }
func EncodeCursor(f Filter, p Position, watermark string) (string, error) {
	if f.Validate() != nil || p.Validate() != nil {
		return "", ErrInvalid
	}
	w, e := Uint(watermark)
	if e != nil {
		return "", e
	}
	h, _ := Uint(p.Height)
	if h.Cmp(w) > 0 {
		return "", ErrInvalid
	}
	b, e := json.Marshal(Cursor{1, f.digest(), p, watermark})
	return base64.RawURLEncoding.EncodeToString(b), e
}
func DecodeCursor(f Filter, s string) (Cursor, error) {
	var c Cursor
	if f.Validate() != nil || len(s) > 2048 {
		return c, ErrInvalid
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return c, ErrInvalid
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return c, ErrInvalid
	}
	if c.Version != 1 || c.FilterHash != f.digest() || c.Position.Validate() != nil {
		return c, ErrInvalid
	}
	w, e := Uint(c.WatermarkHeight)
	if e != nil {
		return c, e
	}
	h, _ := Uint(c.Position.Height)
	if h.Cmp(w) > 0 {
		return c, ErrInvalid
	}
	return c, nil
}

type TradePage struct {
	Trades          []Trade `json:"trades"`
	NextCursor      *string `json:"nextCursor"`
	WatermarkHeight string  `json:"watermarkHeight"`
}

// Page requires a caller-supplied committed watermark, not a worker clock. The
// adapter owns pool existence checks (404 versus empty existing pool).
func (r *Repository) Page(ctx context.Context, f Filter, limit int, watermark, cursor string) (TradePage, error) {
	if f.Validate() != nil || limit < 1 || limit > 100 {
		return TradePage{}, ErrInvalid
	}
	if _, e := Uint(watermark); e != nil {
		return TradePage{}, e
	}
	args := []any{f.Pool, f.ProjectionVersion, f.Side, watermark, limit + 1}
	where := ""
	if cursor != "" {
		c, e := DecodeCursor(f, cursor)
		if e != nil || c.WatermarkHeight != watermark {
			return TradePage{}, ErrInvalid
		}
		where = " AND (height,transaction_index,log_index)<($6::numeric,$7::numeric,$8::numeric)"
		args = append(args, c.Position.Height, c.Position.TransactionIndex, c.Position.LogIndex)
	}
	rows, e := r.pool.Query(ctx, fmt.Sprintf(`SELECT payload FROM %s.trade_facts WHERE pool=$1 AND projection_version=$2 AND ($3='' OR side=$3) AND height<=$4::numeric %s ORDER BY height DESC,transaction_index DESC,log_index DESC LIMIT $5`, r.schema, where), args...)
	if e != nil {
		return TradePage{}, e
	}
	defer rows.Close()
	out := TradePage{Trades: []Trade{}, WatermarkHeight: watermark}
	for rows.Next() {
		var b []byte
		var t Trade
		if e = rows.Scan(&b); e != nil {
			return TradePage{}, e
		}
		if e = json.Unmarshal(b, &t); e != nil {
			return TradePage{}, e
		}
		out.Trades = append(out.Trades, t)
	}
	if e = rows.Err(); e != nil {
		return TradePage{}, e
	}
	if len(out.Trades) > limit {
		out.Trades = out.Trades[:limit]
		s, e := EncodeCursor(f, out.Trades[limit-1].Position, watermark)
		if e != nil {
			return TradePage{}, e
		}
		out.NextCursor = &s
	}
	return out, nil
}

type AssetFee struct {
	Asset       string `json:"assetAddress"`
	Decimals    int    `json:"assetDecimals"`
	Disposition string `json:"disposition"`
	Raw         string `json:"raw"`
}
type Aggregate struct {
	VenueVolumeRaw string `json:"venueVolumeUsdlRaw"`
	PoolFillCount  int64  `json:"poolFillCount"`
	// Unknown without a qualified route/action identity. Transaction count is not
	// silently substituted for user actions.
	UserActionCount                    *int64     `json:"userActionCount"`
	UniqueActors                       int64      `json:"uniqueActors"`
	TraderPopulation                   string     `json:"traderPopulation"`
	Fees                               []AssetFee `json:"tradingFees"`
	WindowStart, WindowEnd, ComputedAt int64
	WatermarkHeight, ProjectionVersion string
	Stale                              bool `json:"stale"`
}

// AggregateWindow uses one repeatable-read snapshot over immutable facts at an
// explicit committed watermark, with exact (start,end] event-time boundaries.
// Expiry jobs may call it with no new swaps. Storage errors are never empty success.
func (r *Repository) AggregateWindow(ctx context.Context, version, watermark string, observedAt, seconds int64) (Aggregate, error) {
	if version == "" || len(version) > 64 || observedAt < 0 || seconds <= 0 || seconds > 86400 {
		return Aggregate{}, ErrInvalid
	}
	if _, e := Uint(watermark); e != nil {
		return Aggregate{}, e
	}
	tx, e := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return Aggregate{}, e
	}
	defer tx.Rollback(ctx)
	a := Aggregate{TraderPopulation: "sender_actor", Fees: []AssetFee{}, WindowStart: observedAt - seconds, WindowEnd: observedAt, ComputedAt: observedAt, WatermarkHeight: watermark, ProjectionVersion: version}
	args := []any{version, watermark, a.WindowStart, a.WindowEnd}
	predicate := `projection_version=$1 AND height<=$2::numeric AND block_timestamp>$3 AND block_timestamp<=$4`
	e = tx.QueryRow(ctx, fmt.Sprintf(`SELECT COALESCE(SUM(volume),0)::text,COUNT(*),COUNT(DISTINCT actor) FROM %s.trade_facts WHERE %s`, r.schema, predicate), args...).Scan(&a.VenueVolumeRaw, &a.PoolFillCount, &a.UniqueActors)
	if e != nil {
		return Aggregate{}, e
	}
	rows, e := tx.Query(ctx, fmt.Sprintf(`SELECT fee_asset,fee_decimals,disposition,SUM(fee)::text FROM %s.trade_facts WHERE %s GROUP BY fee_asset,fee_decimals,disposition ORDER BY fee_asset,fee_decimals,disposition`, r.schema, predicate), args...)
	if e != nil {
		return Aggregate{}, e
	}
	for rows.Next() {
		var f AssetFee
		if e = rows.Scan(&f.Asset, &f.Decimals, &f.Disposition, &f.Raw); e != nil {
			rows.Close()
			return Aggregate{}, e
		}
		a.Fees = append(a.Fees, f)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return Aggregate{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Aggregate{}, e
	}
	return a, nil
}

type Notification struct {
	EventID, ProjectionVersion string
	Revision                   int64
	Payload                    json.RawMessage
}

// PublishPending holds a bounded set of row locks while awaiting transport
// acknowledgement. Failure leaves the outbox pending. Crash after send before
// commit may redeliver; subscribers deduplicate event/version/revision.
func (r *Repository) PublishPending(ctx context.Context, limit int, send func(context.Context, Notification) error) (int, error) {
	if limit < 1 || limit > 100 || send == nil {
		return 0, ErrInvalid
	}
	tx, e := r.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, fmt.Sprintf(`SELECT event_id,projection_version,revision,payload FROM %s.outbox WHERE NOT published ORDER BY event_id LIMIT $1 FOR UPDATE SKIP LOCKED`, r.schema), limit)
	if e != nil {
		return 0, e
	}
	ns := []Notification{}
	for rows.Next() {
		var n Notification
		if e = rows.Scan(&n.EventID, &n.ProjectionVersion, &n.Revision, &n.Payload); e != nil {
			rows.Close()
			return 0, e
		}
		ns = append(ns, n)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return 0, e
	}
	for _, n := range ns {
		if e = send(ctx, n); e != nil {
			return 0, e
		}
		_, e = tx.Exec(ctx, fmt.Sprintf(`UPDATE %s.outbox SET published=true,attempts=attempts+1 WHERE event_id=$1 AND projection_version=$2`, r.schema), n.EventID, n.ProjectionVersion)
		if e != nil {
			return 0, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return len(ns), nil
}

func (r *Repository) PublishDurable(ctx context.Context, publisher *natsdriver.Publisher, subject string, limit int) (int, error) {
	if publisher == nil {
		return 0, ErrInvalid
	}
	return r.PublishPending(ctx, limit, func(ctx context.Context, n Notification) error {
		var raw []byte
		var timestamp int64
		if err := r.pool.QueryRow(ctx, "SELECT payload,block_timestamp FROM "+r.schema+".trade_facts WHERE event_id=$1 AND projection_version=$2", n.EventID, n.ProjectionVersion).Scan(&raw, &timestamp); err != nil {
			return err
		}
		var trade Trade
		if json.Unmarshal(raw, &trade) != nil || trade.Position.Validate() != nil {
			return ErrIntegrity
		}
		id := digestBytes([]byte(n.EventID + ":" + n.ProjectionVersion + ":" + fmt.Sprint(n.Revision)))
		envelope := delivery.Envelope{EventID: id, EventType: "stats.minute.v2", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: trade.Position.Height, TransactionIndex: trade.Position.TransactionIndex, LogIndex: trade.Position.LogIndex}, EmittedAt: time.Unix(timestamp, 0).UTC(), TraceID: id, PayloadHash: delivery.Hash(n.Payload), Payload: n.Payload}
		policy := delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{"stats.minute.v2": true}}
		wire, err := envelope.Encode(policy, subject)
		if err != nil {
			return err
		}
		_, err = publisher.Publish(ctx, subject, id, wire)
		return err
	})
}
