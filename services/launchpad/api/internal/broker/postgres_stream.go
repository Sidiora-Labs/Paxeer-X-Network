package broker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrStreamUnavailable = errors.New("stream projection unavailable")

type PostgresStream struct {
	pool      *pgxpool.Pool
	schema    string
	key       []byte
	Authority sharedauth.StreamAuthority
}

func NewPostgresStream(pool *pgxpool.Pool, schema string, key []byte, authority sharedauth.StreamAuthority) (*PostgresStream, error) {
	if pool == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schema) || len(key) < 32 || authority == nil {
		return nil, ErrStreamUnavailable
	}
	return &PostgresStream{pool: pool, schema: pgx.Identifier{schema}.Sanitize(), key: append([]byte(nil), key...), Authority: authority}, nil
}
func (s *PostgresStream) table(name string) string {
	return s.schema + "." + pgx.Identifier{name}.Sanitize()
}
func streamRollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func parseStreamV2(raw []byte) (Frame, error) {
	var f Frame
	if len(raw) == 0 || len(raw) > 256*1024 || interfaces.ValidateJSON("StreamEvent", raw) != nil || json.Unmarshal(raw, &f) != nil {
		return f, ErrFrame
	}
	if !number(f.Sequence, 63, true) || !number(f.Revision, 256, true) {
		return f, ErrFrame
	}
	if len(f.Watermark) > 0 && string(f.Watermark) != "null" {
		var w struct {
			Integrity string `json:"integrity"`
		}
		if json.Unmarshal(f.Watermark, &w) != nil || w.Integrity != "healthy" {
			return f, ErrFrame
		}
	}
	if f.Coalesced {
		if f.First == nil || f.Last == nil || !number(*f.First, 63, true) || !number(*f.Last, 63, true) || compare(*f.First, *f.Last) > 0 || *f.Last != f.Sequence {
			return f, ErrFrame
		}
	} else if f.First != nil || f.Last != nil {
		return f, ErrFrame
	}
	var p map[string]any
	decoder := json.NewDecoder(bytes.NewReader(f.Payload))
	decoder.UseNumber()
	if decoder.Decode(&p) != nil {
		return f, ErrFrame
	}
	family := ""
	parts := strings.Split(f.Topic, ".")
	if len(parts) >= 3 {
		switch parts[0] + "." + parts[1] {
		case "market.candles":
			if len(parts) != 4 {
				return f, ErrFrame
			}
			family = "CandleEvent"
		case "market.trades":
			family = "MarketTradesEvent"
		case "market.price":
			family = "MarketPriceEvent"
		case "market.stats":
			family = "StatsEvent"
		case "market.rankings":
			family = "RankingEvent"
		case "private.notifications":
			family = "NotificationsEvent"
		case "private.orders":
			family = "OrdersEvent"
		case "private.rewards":
			family = "RewardsEvent"
		case "private.portfolio":
			family = "PortfolioEvent"
		}
		if parts[0] == "rooms" {
			family = "MessagesEvent"
		}
		if parts[0] == "streams" {
			family = "StreamsEvent"
		}
		if parts[0] == "bounties" {
			family = "BountiesEvent"
		}
	}
	if family == "" {
		return f, ErrFrame
	}
	if family == "PortfolioEvent" {
		if _, ok := p["summary"].(map[string]any); !ok {
			return f, ErrFrame
		}
	}
	if err := validateTopicBinding(family, f.Topic, p); err != nil {
		return f, err
	}
	return f, nil
}

func (s *PostgresStream) AppendProjectionTx(ctx context.Context, tx pgx.Tx, f Frame) error {
	if tx == nil {
		return ErrStreamUnavailable
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	f, err = parseStreamV2(raw)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+s.table("stream_projection")+"(topic,sequence) VALUES($1,0) ON CONFLICT DO NOTHING", f.Topic); err != nil {
		return err
	}
	var partition, last int64
	var prior []byte
	if err = tx.QueryRow(ctx, "SELECT partition_id,sequence,frame FROM "+s.table("stream_projection")+" WHERE topic=$1 FOR UPDATE", f.Topic).Scan(&partition, &last, &prior); err != nil {
		return err
	}
	var recorded []byte
	err = tx.QueryRow(ctx, "SELECT frame FROM "+s.table("stream_delivery_outbox")+" WHERE event_id=$1", f.EventID).Scan(&recorded)
	if err == nil {
		if !bytes.Equal(recorded, raw) {
			return ErrFrame
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	sequence, err := strconv.ParseInt(f.Sequence, 10, 64)
	if err != nil || last == 1<<63-1 || sequence != last+1 {
		return ErrSequence
	}
	if len(prior) > 0 {
		previous, e := parseStreamV2(prior)
		if e != nil {
			return e
		}
		if replacementKey(f) == replacementKey(previous) && compare(f.Revision, previous.Revision) < 0 {
			return ErrStale
		}
	}
	if f.Coalesced {
		return ErrSequence
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+s.table("stream_delivery_outbox")+"(partition_id,sequence,event_id,frame) VALUES($1,$2,$3,$4)", partition, sequence, f.EventID, raw); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE "+s.table("stream_projection")+" SET sequence=$2,frame=$3,updated_at=clock_timestamp() WHERE partition_id=$1", partition, sequence, raw)
	return err
}

type streamProjection struct {
	topic, partition, sequence string
	frame                      Frame
}

func (s *PostgresStream) projections(ctx context.Context, tx pgx.Tx, topics []string) ([]streamProjection, error) {
	rows, err := tx.Query(ctx, "SELECT topic,partition_id::text,sequence::text,frame FROM "+s.table("stream_projection")+" WHERE topic=ANY($1) ORDER BY topic", topics)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]streamProjection, 0, len(topics))
	for rows.Next() {
		var p streamProjection
		var raw []byte
		if err = rows.Scan(&p.topic, &p.partition, &p.sequence, &raw); err != nil {
			return nil, err
		}
		p.frame, err = parseStreamV2(raw)
		if err != nil || p.sequence != p.frame.Sequence || p.topic != p.frame.Topic {
			return nil, ErrFrame
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(topics) {
		return nil, ErrStreamUnavailable
	}
	return out, nil
}
func (s *PostgresStream) authorize(ctx context.Context, p sharedauth.StreamPrincipal, b sharedauth.StreamTicketBinding) error {
	if err := sharedauth.ValidateStreamBinding(p, b); err != nil {
		return err
	}
	if p.AccountID == "" && p.SessionID == "" && PublicTopics(b.Topics) {
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return s.Authority.CheckStreamSession(bounded, p)
}

func (s *PostgresStream) Snapshot(ctx context.Context, p sharedauth.StreamPrincipal, b sharedauth.StreamTicketBinding) (json.RawMessage, error) {
	if err := s.authorize(ctx, p, b); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer streamRollback(tx)
	projections, err := s.projections(ctx, tx, b.Topics)
	if err != nil {
		return nil, err
	}
	var generation string
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT generation::text,transaction_timestamp() FROM "+s.table("stream_generation")+" WHERE singleton").Scan(&generation, &now); err != nil {
		return nil, err
	}
	scope := CursorScopeV2{Chain: "125", Subject: p.AccountID, SessionID: p.SessionID, PermissionEpoch: strconv.FormatInt(p.AuthEpoch, 10), ProjectionEpoch: generation, SchemaMajor: 2, FilterHash: b.FilterDigest, SnapshotID: uuid.NewString()}
	anonymous := p.AccountID == "" && p.SessionID == ""
	if anonymous {
		scope.Subject = "anonymous"
		scope.SessionID = "public"
		scope.PermissionEpoch = "0"
	}
	offsets := map[string]string{}
	entries := make([]any, 0, len(projections))
	for _, v := range projections {
		offsets[v.partition] = v.sequence
		entries = append(entries, map[string]any{"topic": v.topic, "sequence": v.sequence, "revision": v.frame.Revision, "watermark": v.frame.Watermark, "payload": v.frame.Payload})
	}
	cursor := CursorV2{FormatVersion: 1, Scope: scope, Offsets: offsets, IssuedAt: now.Unix(), ExpiresAt: now.Add(72 * time.Hour).Unix()}
	token, err := SignCursorV2(cursor, s.key, now)
	if err != nil {
		return nil, err
	}
	scopeJSON, err := json.Marshal(scope)
	if err != nil {
		return nil, err
	}
	offsetJSON, err := json.Marshal(offsets)
	if err != nil {
		return nil, err
	}
	if !anonymous {
		if _, err = tx.Exec(ctx, "INSERT INTO "+s.table("stream_snapshots")+"(snapshot_id,account_id,session_id,auth_epoch,filter_digest,generation,scope,initial_offsets,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", scope.SnapshotID, p.AccountID, p.SessionID, p.AuthEpoch, b.FilterDigest, generation, scopeJSON, offsetJSON, now, time.Unix(cursor.ExpiresAt, 0)); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(map[string]any{"snapshotId": scope.SnapshotID, "generatedAt": now.UTC().Format("2006-01-02T15:04:05.000Z"), "resumeCursor": token, "topics": entries})
	if err != nil || interfaces.ValidateJSON("TopicSnapshot", raw) != nil {
		return nil, ErrFrame
	}
	if err = s.authorize(ctx, p, b); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

type PostgresReplay struct {
	source        *PostgresStream
	Principal     sharedauth.StreamPrincipal
	Binding       sharedauth.StreamTicketBinding
	Cursor        CursorV2
	Machine       *PartitionReplay
	partitions    map[string]string
	nextPartition int
	checkedAt     time.Time
}

func (s *PostgresStream) Resume(ctx context.Context, p sharedauth.StreamPrincipal, b sharedauth.StreamTicketBinding, token string) (*PostgresReplay, error) {
	if err := s.authorize(ctx, p, b); err != nil {
		return nil, err
	}
	if len(token) > 2048 {
		return nil, ErrCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrCursor
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrCursor
	}
	var hint CursorV2
	if json.Unmarshal(body, &hint) != nil {
		return nil, ErrCursor
	}
	if _, err = uuid.Parse(hint.Scope.SnapshotID); err != nil {
		return nil, ErrCursor
	}
	if _, err = VerifyCursorV2(token, s.key, hint.Scope, time.Now()); err != nil {
		return nil, err
	}
	if hint.Scope.Subject == "anonymous" && PublicTopics(b.Topics) {
		p = sharedauth.StreamPrincipal{}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer streamRollback(tx)
	var scopeRaw, initialRaw []byte
	var now time.Time
	var generation string
	var scope CursorScopeV2
	var initial map[string]string
	if p.AccountID == "" && p.SessionID == "" {
		if err = tx.QueryRow(ctx, "SELECT generation::text,transaction_timestamp() FROM "+s.table("stream_generation")+" WHERE singleton").Scan(&generation, &now); err != nil {
			return nil, err
		}
		scope = hint.Scope
		if scope.Subject != "anonymous" || scope.SessionID != "public" || scope.PermissionEpoch != "0" || scope.FilterHash != b.FilterDigest || scope.ProjectionEpoch != generation {
			return nil, ErrCursor
		}
		initial = hint.Offsets
	} else {
		err = tx.QueryRow(ctx, "SELECT s.scope,s.initial_offsets,transaction_timestamp(),g.generation::text FROM "+s.table("stream_snapshots")+" s CROSS JOIN "+s.table("stream_generation")+" g WHERE s.snapshot_id=$1 AND s.account_id=$2 AND s.session_id=$3 AND s.auth_epoch=$4 AND s.filter_digest=$5 AND s.expires_at>transaction_timestamp() AND g.singleton AND s.generation=g.generation", hint.Scope.SnapshotID, p.AccountID, p.SessionID, p.AuthEpoch, b.FilterDigest).Scan(&scopeRaw, &initialRaw, &now, &generation)
		if err != nil {
			return nil, ErrCursor
		}
		if json.Unmarshal(scopeRaw, &scope) != nil || json.Unmarshal(initialRaw, &initial) != nil {
			return nil, ErrCursor
		}
	}
	cursor, err := VerifyCursorV2(token, s.key, scope, now)
	if err != nil {
		return nil, err
	}
	projections, err := s.projections(ctx, tx, b.Topics)
	if err != nil {
		return nil, err
	}
	barrier := map[string]string{}
	replayFrames, replayBytes := int64(0), int64(0)
	partitions := map[string]string{}
	for _, v := range projections {
		offset, ok := cursor.Offsets[v.partition]
		floor, initialOK := initial[v.partition]
		if !ok || !initialOK || compare(offset, floor) < 0 || compare(offset, v.sequence) > 0 {
			return nil, ErrCursor
		}
		if offset != v.sequence {
			var min string
			if err = tx.QueryRow(ctx, "SELECT COALESCE(min(sequence),0)::text FROM "+s.table("stream_delivery_outbox")+" WHERE partition_id=$1", v.partition).Scan(&min); err != nil {
				return nil, err
			}
			n, _ := new(big.Int).SetString(offset, 10)
			n.Add(n, big.NewInt(1))
			if compare(min, n.String()) > 0 {
				return nil, ErrExpired
			}
		}
		if offset != v.sequence {
			var frames, size int64
			if err = tx.QueryRow(ctx, "SELECT count(*),COALESCE(sum(octet_length(frame)),0)::bigint FROM (SELECT frame FROM "+s.table("stream_delivery_outbox")+" WHERE partition_id=$1 AND sequence>$2 AND sequence<=$3 ORDER BY sequence LIMIT 10001) retained", v.partition, offset, v.sequence).Scan(&frames, &size); err != nil {
				return nil, err
			}
			replayFrames += frames
			replayBytes += size
			if replayFrames > 10000 || replayBytes > 10*1024*1024 {
				return nil, ErrReplayLimit
			}
			expected, _ := new(big.Int).SetString(v.sequence, 10)
			start, _ := new(big.Int).SetString(offset, 10)
			expected.Sub(expected, start)
			if expected.Cmp(big.NewInt(frames)) != 0 {
				return nil, ErrSequence
			}
		}
		barrier[v.partition] = v.sequence
		partitions[v.partition] = v.topic
	}
	if len(partitions) != len(cursor.Offsets) || len(initial) != len(partitions) {
		return nil, ErrCursor
	}
	machine, err := NewPartitionReplay(cursor.Offsets, barrier)
	if err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, p, b); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &PostgresReplay{source: s, Principal: p, Binding: b, Cursor: cursor, Machine: machine, partitions: partitions}, nil
}

func (r *PostgresReplay) CheckDelivery(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now()
	if now.Unix() >= r.Cursor.ExpiresAt {
		return ErrExpired
	}
	if !r.checkedAt.IsZero() && now.Sub(r.checkedAt) < 2*time.Second {
		return nil
	}
	return r.Check(ctx)
}

func (r *PostgresReplay) Check(ctx context.Context) error {
	now := time.Now()
	if err := r.source.authorize(ctx, r.Principal, r.Binding); err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var generation string
	if err := r.source.pool.QueryRow(bounded, "SELECT generation::text FROM "+r.source.table("stream_generation")+" WHERE singleton").Scan(&generation); err != nil {
		return err
	}
	if generation != r.Cursor.Scope.ProjectionEpoch {
		return ErrCursor
	}
	if time.Now().Unix() >= r.Cursor.ExpiresAt {
		return ErrExpired
	}
	r.checkedAt = now
	return nil
}
func (r *PostgresReplay) Batch(ctx context.Context) ([][]byte, error) {
	if r.Machine.State != Replaying && r.Machine.State != Live {
		return nil, ErrCursor
	}
	if err := r.CheckDelivery(ctx); err != nil {
		r.Machine.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	keys := make([]string, 0, len(r.partitions))
	for p := range r.partitions {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		start := r.nextPartition % len(keys)
		keys = append(keys[start:], keys[:start]...)
		r.nextPartition = (start + 1) % len(keys)
	}
	out := make([][]byte, 0, 128)
	totalBytes := 0
	for _, partition := range keys {
		if len(out) >= 128 {
			break
		}
		offset := r.Cursor.Offsets[partition]
		upper := "9223372036854775807"
		if r.Machine.State == Replaying {
			upper = r.Machine.barrier[partition]
		}
		rows, err := r.source.pool.Query(ctx, "SELECT sequence::text,frame FROM "+r.source.table("stream_delivery_outbox")+" WHERE partition_id=$1 AND sequence>$2 AND sequence<=$3 ORDER BY sequence LIMIT $4", partition, offset, upper, 128-len(out))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var sequence string
			var raw []byte
			if err = rows.Scan(&sequence, &raw); err != nil {
				rows.Close()
				return nil, err
			}
			f, e := parseStreamV2(raw)
			if e != nil || f.Topic != r.partitions[partition] || f.Sequence != sequence {
				rows.Close()
				return nil, ErrFrame
			}
			if totalBytes+len(raw) > 256*1024 {
				rows.Close()
				if len(out) == 0 {
					return nil, ErrFrame
				}
				if err = r.CheckDelivery(ctx); err != nil {
					r.Machine.Close()
					return nil, err
				}
				return out, nil
			}
			n, _ := new(big.Int).SetString(r.Cursor.Offsets[partition], 10)
			n.Add(n, big.NewInt(1))
			if sequence != n.String() {
				rows.Close()
				r.Machine.State = ResyncRequired
				return nil, ErrSequence
			}
			if r.Machine.State == Replaying {
				if err = r.Machine.Replay(partition, sequence, len(raw)); err != nil {
					rows.Close()
					return nil, err
				}
			}
			r.Cursor.Offsets[partition] = sequence
			out = append(out, raw)
			totalBytes += len(raw)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if r.Machine.State == Replaying {
		complete := true
		for p, end := range r.Machine.barrier {
			if r.Cursor.Offsets[p] != end {
				complete = false
			}
		}
		if complete {
			if err := r.Machine.Complete(); err != nil {
				return nil, err
			}
		} else if len(out) == 0 {
			r.Machine.State = ResyncRequired
			return nil, ErrSequence
		}
	}
	if err := r.CheckDelivery(ctx); err != nil {
		r.Machine.Close()
		return nil, err
	}
	return out, nil
}
func (r *PostgresReplay) Checkpoint(ctx context.Context) (string, error) {
	if r.Machine.State != Live {
		return "", ErrCursor
	}
	if err := r.Check(ctx); err != nil {
		return "", err
	}
	return SignCursorV2(r.Cursor, r.source.key, time.Now())
}
func (r *PostgresReplay) Close() { r.Machine.Close() }
func (s *PostgresStream) AuthenticateHTTP(ctx context.Context, bearer string) (sharedauth.StreamPrincipal, error) {
	resolver, ok := s.Authority.(interface {
		AuthenticateStreamHTTP(context.Context, string) (sharedauth.StreamPrincipal, error)
	})
	if !ok || bearer == "" {
		return sharedauth.StreamPrincipal{}, fmt.Errorf("stream HTTP authority unavailable")
	}
	return resolver.AuthenticateStreamHTTP(ctx, bearer)
}

func (s *PostgresStream) AuthenticateRequest(ctx context.Context, r *http.Request) (sharedauth.StreamPrincipal, error) {
	var authority interface {
		AuthenticateRequest(context.Context, *http.Request) (sharedauth.IdentityPrincipal, error)
	}
	switch a := s.Authority.(type) {
	case sharedauth.HTTPStreamAuthority:
		authority = sharedauth.HTTPIdentityAuthority(a)
	case *sharedauth.HTTPStreamAuthority:
		if a == nil {
			return sharedauth.StreamPrincipal{}, sharedauth.ErrScopeDenied
		}
		authority = sharedauth.HTTPIdentityAuthority(*a)
	default:
		var ok bool
		authority, ok = s.Authority.(interface {
			AuthenticateRequest(context.Context, *http.Request) (sharedauth.IdentityPrincipal, error)
		})
		if !ok {
			return sharedauth.StreamPrincipal{}, sharedauth.ErrScopeDenied
		}
	}
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	p, err := authority.AuthenticateRequest(bounded, r)
	if err != nil {
		return sharedauth.StreamPrincipal{}, err
	}
	return p.StreamPrincipal(), nil
}
