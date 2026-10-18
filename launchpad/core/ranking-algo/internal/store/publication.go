package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const Phase2DDL = `ALTER TABLE ranking.snapshots ADD COLUMN public_header jsonb;
CREATE TABLE ranking.category_heads(category text PRIMARY KEY,run_id text NOT NULL,cutoff_time bigint NOT NULL,revision bigint NOT NULL CHECK(revision>0),FOREIGN KEY(run_id,category) REFERENCES ranking.snapshots(run_id,category));
CREATE TABLE ranking.snapshot_entries(run_id text NOT NULL,category text NOT NULL,rank bigint NOT NULL CHECK(rank BETWEEN 1 AND 1000000),pool_address text NOT NULL,score text NOT NULL CHECK(score ~ '^[0-9]+(\.[0-9]+)?$'),PRIMARY KEY(run_id,category,rank),UNIQUE(run_id,category,pool_address),FOREIGN KEY(run_id,category) REFERENCES ranking.snapshots(run_id,category));
ALTER TABLE ranking.publication_outbox ADD COLUMN category_sequence bigint CHECK(category_sequence>0),ADD COLUMN attempts integer NOT NULL DEFAULT 0 CHECK(attempts>=0),ADD COLUMN broker_stream text,ADD COLUMN broker_sequence bigint CHECK(broker_sequence>0);
CREATE UNIQUE INDEX ranking_category_sequence ON ranking.publication_outbox(category,category_sequence);
CREATE INDEX ranking_pending_category_sequence ON ranking.publication_outbox(category,category_sequence) WHERE published_at IS NULL;
REVOKE ALL ON ranking.category_heads,ranking.snapshot_entries FROM PUBLIC;`

type EpochGuard interface {
	Assert(context.Context, pgx.Tx) error
}
type PublicationStore struct {
	*SnapshotStore
	guard EpochGuard
}

func NewPublicationStore(pool *pgxpool.Pool, schema string, guard EpochGuard) (*PublicationStore, error) {
	s, err := NewSnapshotStore(pool, schema)
	if err != nil {
		return nil, err
	}
	if guard == nil {
		return nil, ranker.ErrUnavailable
	}
	return &PublicationStore{s, guard}, nil
}
func (s *PublicationStore) Complete(ctx context.Context, snapshots []ranker.Snapshot) error {
	if len(snapshots) != 1 && len(snapshots) != 5 {
		return errors.New("complete new or complete heavy run required")
	}
	ordered := append([]ranker.Snapshot(nil), snapshots...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Category() < ordered[j].Category() })
	first := ordered[0]
	if _, err := uuid.Parse(first.RunID()); err != nil {
		return ranker.ErrUnavailable
	}
	categories := map[string]bool{}
	for _, snapshot := range ordered {
		in := snapshot.Metadata()
		if snapshot.RunID() != first.RunID() || snapshot.InputDigest() != first.InputDigest() || categories[snapshot.Category()] || in.Provenance == nil || in.Provenance.Validate(in) != nil {
			return ranker.ErrUnavailable
		}
		categories[snapshot.Category()] = true
	}
	if len(ordered) == 1 && !categories["new"] {
		return errors.New("new run required")
	}
	if len(ordered) == 5 {
		for _, category := range ranker.HeavyCategoryNames() {
			if !categories[category] {
				return errors.New("heavy categories incomplete")
			}
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='2s'"); err != nil {
		return err
	}
	if err = s.guard.Assert(ctx, tx); err != nil {
		return err
	}
	for _, snapshot := range ordered {
		if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", s.schema+":"+snapshot.Category()); err != nil {
			return err
		}
		var current string
		var cutoff, revision int64
		err = tx.QueryRow(ctx, "SELECT run_id,cutoff_time,revision FROM "+s.table("category_heads")+" WHERE category=$1 FOR UPDATE", snapshot.Category()).Scan(&current, &cutoff, &revision)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && current != snapshot.RunID() && cutoff >= snapshot.CutoffTime() {
			return ranker.ErrSuperseded
		}
		if revision == math.MaxInt64 {
			return errors.New("ranking sequence exhausted")
		}
		payload, e := json.Marshal(snapshot)
		if e != nil {
			return e
		}
		in := snapshot.Metadata()
		publicHeader, e := json.Marshal(map[string]any{"algorithmVersion": in.AlgorithmVersion, "cutoffTime": in.CutoffTimeUTC, "provenance": in.Provenance, "total": len(snapshot.Entries())})
		if e != nil {
			return e
		}
		tag, e := tx.Exec(ctx, "INSERT INTO "+s.table("snapshots")+"(run_id,category,input_digest,output_digest,artifact,public_header) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", snapshot.RunID(), snapshot.Category(), snapshot.InputDigest(), snapshot.OutputDigest(), payload, publicHeader)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			var same bool
			if e = tx.QueryRow(ctx, "SELECT artifact=$3::jsonb FROM "+s.table("snapshots")+" WHERE run_id=$1 AND category=$2", snapshot.RunID(), snapshot.Category(), payload).Scan(&same); e != nil {
				return e
			}
			if !same {
				return errors.New("immutable ranking identity conflict")
			}
			if current != snapshot.RunID() {
				return ranker.ErrSuperseded
			}
			continue
		}
		entries := snapshot.Entries()
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{s.schema, "snapshot_entries"}, []string{"run_id", "category", "rank", "pool_address", "score"}, pgx.CopyFromSlice(len(entries), func(i int) ([]any, error) {
			entry := entries[i]
			return []any{snapshot.RunID(), snapshot.Category(), int64(i + 1), entry.Address, strconv.FormatFloat(entry.Score, 'f', -1, 64)}, nil
		})); e != nil {
			return e
		}
		revision++
		if _, e = tx.Exec(ctx, "INSERT INTO "+s.table("category_heads")+"(category,run_id,cutoff_time,revision) VALUES($1,$2,$3,$4) ON CONFLICT(category) DO UPDATE SET run_id=EXCLUDED.run_id,cutoff_time=EXCLUDED.cutoff_time,revision=EXCLUDED.revision", snapshot.Category(), snapshot.RunID(), snapshot.CutoffTime(), revision); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, "INSERT INTO "+s.table("publication_outbox")+"(run_id,category,artifact,category_sequence) VALUES($1,$2,$3,$4)", snapshot.RunID(), snapshot.Category(), payload, revision); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

type rowReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *SnapshotStore) header(ctx context.Context, q rowReader, runID, category string) (ranker.Header, error) {
	var h ranker.Header
	var provenance []byte
	err := q.QueryRow(ctx, `SELECT s.run_id,s.category,s.input_digest,s.output_digest,s.public_header->>'algorithmVersion',(s.public_header->>'cutoffTime')::bigint,s.completed_at,o.category_sequence,(s.public_header->>'total')::bigint,s.public_header->'provenance' FROM `+s.table("snapshots")+` s JOIN `+s.table("publication_outbox")+` o USING(run_id,category) WHERE s.run_id=$1 AND s.category=$2`, runID, category).Scan(&h.RunID, &h.Category, &h.InputDigest, &h.OutputDigest, &h.AlgorithmVersion, &h.CutoffTime, &h.CompletedAt, &h.Revision, &h.Total, &provenance)
	if err != nil {
		return h, err
	}
	if err = json.Unmarshal(provenance, &h.Provenance); err != nil {
		return h, err
	}
	height, e := strconv.ParseUint(string(h.Provenance.Watermark.Height), 10, 64)
	if e != nil || h.Provenance.Validate(ranker.InputSnapshot{CutoffHeight: height, CutoffTimeUTC: h.CutoffTime}) != nil {
		return h, ranker.ErrUnavailable
	}
	return h, h.Validate()
}
func (s *SnapshotStore) Header(ctx context.Context, runID, category string) (ranker.Header, error) {
	return s.header(ctx, s.pool, runID, category)
}
func (s *SnapshotStore) Current(ctx context.Context, category string) (ranker.Header, error) {
	var id string
	if err := s.pool.QueryRow(ctx, "SELECT run_id FROM "+s.table("category_heads")+" WHERE category=$1", category).Scan(&id); err != nil {
		return ranker.Header{}, err
	}
	return s.Header(ctx, id, category)
}
func (s *SnapshotStore) Page(ctx context.Context, h ranker.Header, offset int64, limit int) (ranker.StoredPage, error) {
	p := ranker.StoredPage{Header: h, Items: []ranker.RankedItem{}, Offset: offset}
	if h.Validate() != nil || limit < 1 || limit > 100 || offset < 0 || offset > h.Total {
		return p, ranker.ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, "SELECT pool_address,score,rank FROM "+s.table("snapshot_entries")+" WHERE run_id=$1 AND category=$2 AND rank>$3 ORDER BY rank LIMIT $4", h.RunID, h.Category, offset, limit)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ranker.RankedItem
		if err = rows.Scan(&item.Address, &item.Score, &item.Rank); err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	expected := h.Total - offset
	if expected > int64(limit) {
		expected = int64(limit)
	}
	if int64(len(p.Items)) != expected {
		return p, ranker.ErrUnavailable
	}
	return p, p.Validate()
}
func (s *SnapshotStore) ValidateBoundary(ctx context.Context, h ranker.Header, reference ranker.PageReference) error {
	if h.Validate() != nil || reference.RunID != h.RunID || reference.Category != h.Category || reference.InputDigest != h.InputDigest || reference.OutputDigest != h.OutputDigest || reference.Offset < 1 || reference.Offset >= h.Total || reference.Expires != h.CutoffTime+900 {
		return ranker.ErrCursorInvalid
	}
	var address, score string
	if err := s.pool.QueryRow(ctx, "SELECT pool_address,score FROM "+s.table("snapshot_entries")+" WHERE run_id=$1 AND category=$2 AND rank=$3", h.RunID, h.Category, reference.Offset).Scan(&address, &score); err != nil {
		return err
	}
	if address != reference.LastAddress || score != reference.LastScore {
		return ranker.ErrCursorInvalid
	}
	return nil
}
func WireSnapshot(h ranker.Header, items []ranker.RankedItem) (v1.RankingSnapshot, error) {
	out := v1.RankingSnapshot{SnapshotId: h.RunID, Category: h.Category, ModelVersion: h.AlgorithmVersion, ComputedAt: h.CompletedAt.UTC().Format("2006-01-02T15:04:05.000Z"), Watermark: h.Provenance.Watermark, Replace: true, Items: []v1.RankingEntry{}, TotalEligible: v1.UInt256(strconv.FormatInt(h.Total, 10)), Truncated: h.Total > int64(len(items))}
	if h.Validate() != nil || len(items) > 100 {
		return out, ranker.ErrUnavailable
	}
	for _, item := range items {
		out.Items = append(out.Items, v1.RankingEntry{PoolAddress: item.Address, Score: item.Score, Rank: v1.SafeInteger(item.Rank), Category: h.Category, ComputedAt: out.ComputedAt, ModelVersion: h.AlgorithmVersion})
	}
	return out, nil
}

// pendingEvent is the oldest unpublished category head, rebuilt from the
// committed snapshot inside the claiming transaction.
type pendingEvent struct {
	sequence, categorySequence int64
	header                     ranker.Header
	event                      v1.RankingEvent
}

// nextEvent locks the oldest publishable outbox row (category order kept) and
// returns its RankingEvent; nil when nothing is pending.
func (s *PublicationStore) nextEvent(ctx context.Context, tx pgx.Tx, maxAttempts int) (*pendingEvent, error) {
	if err := s.guard.Assert(ctx, tx); err != nil {
		return nil, err
	}
	var p pendingEvent
	var runID, category string
	err := tx.QueryRow(ctx, `SELECT o.sequence,o.run_id,o.category,o.category_sequence FROM `+s.table("publication_outbox")+` o WHERE o.published_at IS NULL AND o.attempts<$1 AND o.category_sequence IS NOT NULL AND NOT EXISTS(SELECT 1 FROM `+s.table("publication_outbox")+` prior WHERE prior.category=o.category AND prior.category_sequence<o.category_sequence AND prior.published_at IS NULL) ORDER BY o.sequence LIMIT 1 FOR UPDATE OF o SKIP LOCKED`, maxAttempts).Scan(&p.sequence, &runID, &category, &p.categorySequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h, err := s.header(ctx, tx, runID, category)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT pool_address,score,rank FROM "+s.table("snapshot_entries")+" WHERE run_id=$1 AND category=$2 ORDER BY rank LIMIT 100", runID, category)
	if err != nil {
		return nil, err
	}
	items := []ranker.RankedItem{}
	for rows.Next() {
		var item ranker.RankedItem
		if err = rows.Scan(&item.Address, &item.Score, &item.Rank); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	page := ranker.StoredPage{Header: h, Items: items, Offset: 0}
	if err = page.Validate(); err != nil {
		return nil, err
	}
	expected := h.Total
	if expected > 100 {
		expected = 100
	}
	if int64(len(items)) != expected {
		return nil, ranker.ErrUnavailable
	}
	payload, err := WireSnapshot(h, items)
	if err != nil {
		return nil, err
	}
	namespace, err := uuid.Parse(runID)
	if err != nil {
		return nil, err
	}
	eventID := uuid.NewSHA1(namespace, []byte(category+":"+h.OutputDigest+":"+strconv.FormatInt(p.categorySequence, 10))).String()
	watermark := h.Provenance.Watermark
	p.header = h
	p.event = v1.RankingEvent{EventId: eventID, Topic: "market.rankings." + category, Sequence: v1.UInt63(strconv.FormatInt(p.categorySequence, 10)), Revision: v1.PositiveUInt256(strconv.FormatInt(p.categorySequence, 10)), OccurredAt: payload.ComputedAt, Payload: payload, Watermark: &watermark, Coalesced: false}
	return &p, nil
}

// StreamAppender commits a public stream frame inside the caller's
// transaction (shared/delivery.StreamWriter on the api_stream schema).
type StreamAppender interface {
	AppendTx(context.Context, pgx.Tx, delivery.StreamUpdate) error
}

// StreamBroker names the publication target recorded on outbox rows that are
// committed to api_stream rather than published to a broker directly.
const StreamBroker = "api_stream"

// CommitStreamOnce commits the oldest pending category head to api_stream
// (api_stream.append_ranking) and marks the ranking outbox row published in
// the same transaction. The api_stream relay then publishes the frame to
// market.rankings.<category>, so every NATS message has its outbox row.
func (s *PublicationStore) CommitStreamOnce(ctx context.Context, writer StreamAppender, maxAttempts int) (bool, error) {
	if writer == nil || maxAttempts < 1 || maxAttempts > 100 {
		return false, ranker.ErrUnavailable
	}
	opCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	ctx = opCtx
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	p, err := s.nextEvent(ctx, tx, maxAttempts)
	if err != nil || p == nil {
		return false, err
	}
	watermark, err := json.Marshal(p.event.Watermark)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(p.event.Payload)
	if err != nil {
		return false, err
	}
	update := delivery.StreamUpdate{EventID: p.event.EventId, Topic: p.event.Topic, Revision: string(p.event.Revision), OccurredAt: p.event.OccurredAt, Watermark: watermark, Payload: payload, Source: delivery.StreamSourceProof{Kind: "ranking_commit", SourceID: p.header.RunID, Sequence: strconv.FormatInt(p.categorySequence, 10), SourceDigest: p.header.OutputDigest}}
	if appendErr := writer.AppendTx(ctx, tx, update); appendErr != nil {
		_ = tx.Rollback(ctx)
		if recordErr := s.recordFailure(ctx, p.sequence, maxAttempts); recordErr != nil {
			return false, errors.Join(appendErr, recordErr)
		}
		return false, appendErr
	}
	if _, err = tx.Exec(ctx, "UPDATE "+s.table("publication_outbox")+" SET published_at=clock_timestamp(),attempts=attempts+1,broker_stream=$2,broker_sequence=$3 WHERE sequence=$1 AND published_at IS NULL", p.sequence, StreamBroker, p.categorySequence); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// DispatchOnce publishes the oldest pending category head straight to a
// broker. Production commits through CommitStreamOnce instead; a direct
// publish has no api_stream outbox row for core/api to verify.
func (s *PublicationStore) DispatchOnce(ctx context.Context, publisher delivery.PersistentPublisher, stream string, maxAttempts int) (bool, error) {
	if publisher == nil || stream == "" || maxAttempts < 1 || maxAttempts > 100 {
		return false, ranker.ErrUnavailable
	}
	opCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	ctx = opCtx
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	p, err := s.nextEvent(ctx, tx, maxAttempts)
	if err != nil || p == nil {
		return false, err
	}
	wire, err := json.Marshal(p.event)
	if err != nil {
		return false, err
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	ack, sendErr := publisher.Publish(bounded, p.event.Topic, p.event.EventId, wire)
	cancel()
	if sendErr != nil || ack.Stream != stream || ack.Sequence == 0 || ack.Sequence > math.MaxInt64 {
		_ = tx.Rollback(ctx)
		recordErr := s.recordFailure(ctx, p.sequence, maxAttempts)
		if sendErr == nil {
			sendErr = fmt.Errorf("genuine configured broker persistence acknowledgement required")
		}
		if recordErr != nil {
			return false, errors.Join(sendErr, recordErr)
		}
		return false, sendErr
	}
	_, err = tx.Exec(ctx, "UPDATE "+s.table("publication_outbox")+" SET published_at=clock_timestamp(),attempts=attempts+1,broker_stream=$2,broker_sequence=$3 WHERE sequence=$1 AND published_at IS NULL", p.sequence, ack.Stream, int64(ack.Sequence))
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *PublicationStore) recordFailure(ctx context.Context, sequence int64, maxAttempts int) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = s.guard.Assert(ctx, tx); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE "+s.table("publication_outbox")+" SET attempts=attempts+1 WHERE sequence=$1 AND published_at IS NULL AND attempts<$2", sequence, maxAttempts); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type PublicationHealth struct {
	CategoryAges                map[string]int64
	Pending                     int64
	Exhausted                   int64
	OldestPendingAgeSeconds     int64
	AllHeavyStaleSince          *time.Time
	AllHeavyStaleForFiveMinutes bool
}

func (s *SnapshotStore) PublicationHealth(ctx context.Context, now time.Time, maxAttempts int) (PublicationHealth, error) {
	health := PublicationHealth{CategoryAges: map[string]int64{}}
	if maxAttempts < 1 || maxAttempts > 100 {
		return health, ranker.ErrUnavailable
	}
	rows, err := s.pool.Query(ctx, "SELECT category,cutoff_time FROM "+s.table("category_heads")+" ORDER BY category LIMIT 6")
	if err != nil {
		return health, err
	}
	allHeavyStale := true
	heavySeen := 0
	var latestStaleAt int64
	for rows.Next() {
		var category string
		var cutoff int64
		if err = rows.Scan(&category, &cutoff); err != nil {
			rows.Close()
			return health, err
		}
		policy, e := ranker.Policy(category)
		if e != nil || cutoff <= 0 || cutoff > math.MaxInt64-900 || cutoff > now.Unix() {
			rows.Close()
			return health, ranker.ErrUnavailable
		}
		age := now.Unix() - cutoff
		health.CategoryAges[category] = age
		if category != "new" {
			heavySeen++
			if age <= policy.FreshnessSeconds {
				allHeavyStale = false
			}
			staleAt := cutoff + policy.FreshnessSeconds + 1
			if staleAt > latestStaleAt {
				latestStaleAt = staleAt
			}
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return health, err
	}
	if heavySeen == 5 && allHeavyStale {
		since := time.Unix(latestStaleAt, 0).UTC()
		health.AllHeavyStaleSince = &since
		health.AllHeavyStaleForFiveMinutes = now.Sub(since) >= 5*time.Minute
	}
	err = s.pool.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE attempts>=$1),COALESCE(GREATEST(0,$2-EXTRACT(EPOCH FROM min(created_at)))::bigint,0) FROM "+s.table("publication_outbox")+" WHERE published_at IS NULL", maxAttempts, now.Unix()).Scan(&health.Pending, &health.Exhausted, &health.OldestPendingAgeSeconds)
	return health, err
}

func (s *PublicationStore) Prune(ctx context.Context, now time.Time, limit int) (int64, error) {
	if limit < 1 || limit > 100 || now.IsZero() {
		return 0, ranker.ErrUnavailable
	}
	bounded, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	tx, err := s.pool.Begin(bounded)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(bounded)
	if err = s.guard.Assert(bounded, tx); err != nil {
		return 0, err
	}
	rows, err := tx.Query(bounded, `SELECT s.run_id,s.category FROM `+s.table("snapshots")+` s JOIN `+s.table("publication_outbox")+` o USING(run_id,category) WHERE o.published_at IS NOT NULL AND (s.public_header->>'cutoffTime')::bigint<$1-900 AND NOT EXISTS(SELECT 1 FROM `+s.table("category_heads")+` h WHERE h.run_id=s.run_id AND h.category=s.category) ORDER BY s.completed_at,s.run_id,s.category LIMIT $2 FOR UPDATE OF s SKIP LOCKED`, now.Unix(), limit)
	if err != nil {
		return 0, err
	}
	pairs := [][2]string{}
	for rows.Next() {
		var pair [2]string
		if err = rows.Scan(&pair[0], &pair[1]); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, pair)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for _, pair := range pairs {
		for _, table := range []string{"snapshot_entries", "publication_outbox", "snapshots"} {
			if _, err = tx.Exec(bounded, "DELETE FROM "+s.table(table)+" WHERE run_id=$1 AND category=$2", pair[0], pair[1]); err != nil {
				return 0, err
			}
		}
	}
	if err = tx.Commit(bounded); err != nil {
		return 0, err
	}
	return int64(len(pairs)), nil
}
