package delivery

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"golang.org/x/time/rate"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

//go:embed replay.sql
var replaySQL string

func ReplayDDL(schema string) (string, error) {
	if !token.MatchString(schema) {
		return "", ErrSchema
	}
	return strings.ReplaceAll(replaySQL, "{{schema}}", pgx.Identifier{schema}.Sanitize()), nil
}

type RebuildProjection interface {
	ApplyRawTx(context.Context, pgx.Tx, string, ArchiveRaw) error
	ProjectionDigestTx(context.Context, pgx.Tx, string) (string, error)
	CanonicalDigestTx(context.Context, pgx.Tx, ArchiveManifest) (string, error)
}
type ReplayEngine struct {
	archive                     *Archive
	consumer, generation, owner string
	projector                   RebuildProjection
	authorize                   func(context.Context, string) error
	lease                       time.Duration
	budget                      *WorkBudget
}
type ReplayStatus struct {
	Generation, Status                           string
	NextHeight, SourceHeight, RawCount, Revision int64
	Gap, Ready                                   bool
}

func NewReplayEngine(archive *Archive, consumer, generation, owner string, projector RebuildProjection, authorize func(context.Context, string) error, lease time.Duration) (*ReplayEngine, error) {
	if archive == nil || !token.MatchString(consumer) || !token.MatchString(generation) || !token.MatchString(owner) || projector == nil || authorize == nil || lease < time.Second || lease > 5*time.Minute {
		return nil, errors.New("delivery: actual replay projector and authority required")
	}
	return &ReplayEngine{archive: archive, consumer: consumer, generation: generation, owner: owner, projector: projector, authorize: authorize, lease: lease}, nil
}
func (r *ReplayEngine) Start(ctx context.Context, digest string) error {
	m, err := r.archive.Load(ctx, digest)
	if err != nil {
		return err
	}
	return r.archive.store.mutate(ctx, func(tx pgx.Tx) error {
		var active string
		err := tx.QueryRow(ctx, "SELECT generation FROM "+r.archive.store.table("delivery_read_aliases")+" WHERE consumer_id=$1", r.consumer).Scan(&active)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if active == r.generation {
			return ErrIntegrity
		}
		tag, err := tx.Exec(ctx, "INSERT INTO "+r.archive.store.table("delivery_rebuilds")+"(consumer_id,generation,manifest_digest,next_height,status,owner,epoch,lease_until) VALUES($1,$2,$3,$4,'building',$5,1,clock_timestamp()+$6::bigint*interval '1 millisecond') ON CONFLICT DO NOTHING", r.consumer, r.generation, digest, m.FromHeight, r.owner, r.lease.Milliseconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var old string
			err = tx.QueryRow(ctx, "SELECT manifest_digest FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2", r.consumer, r.generation).Scan(&old)
			if err != nil {
				return err
			}
			if old != digest {
				return ErrIntegrity
			}
		}
		tag, err = tx.Exec(ctx, "UPDATE "+r.archive.store.table("delivery_rebuilds")+" SET owner=$3,epoch=epoch+CASE WHEN owner<>$3 THEN 1 ELSE 0 END,lease_until=clock_timestamp()+$4::bigint*interval '1 millisecond' WHERE consumer_id=$1 AND generation=$2 AND (owner=$3 OR lease_until<=clock_timestamp()) AND status IN ('building','verified')", r.consumer, r.generation, r.owner, r.lease.Milliseconds())
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return r.archive.store.BindGenerationPathTx(ctx, tx, r.consumer, r.generation, "durable")
	})
}
func (r *ReplayEngine) RunBlock(ctx context.Context) error {
	var digest string
	var height int64
	err := r.archive.store.pool.QueryRow(ctx, "SELECT manifest_digest,next_height FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2 AND status='building'", r.consumer, r.generation).Scan(&digest, &height)
	if err != nil {
		return err
	}
	m, err := r.archive.Load(ctx, digest)
	if err != nil {
		return err
	}
	if height > m.ToHeight {
		return r.verify(ctx, m, digest)
	}
	if height < m.FromHeight {
		return ErrIntegrity
	}
	block := m.Blocks[height-m.FromHeight]
	records, err := r.archive.records(ctx, block)
	if err != nil {
		return err
	}
	if r.budget != nil {
		for range records {
			if err = r.budget.limiter.Wait(ctx); err != nil {
				return err
			}
		}
	}
	return r.archive.store.mutate(ctx, func(tx pgx.Tx) error {
		var epoch, actualHeight int64
		var actualOwner string
		err := tx.QueryRow(ctx, "SELECT epoch,next_height,owner FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2 AND status='building' AND (owner=$3 OR lease_until<=clock_timestamp()) FOR UPDATE", r.consumer, r.generation, r.owner).Scan(&epoch, &actualHeight, &actualOwner)
		if err != nil {
			return ErrLease
		}
		if height != actualHeight {
			return ErrGap
		}
		if actualOwner != r.owner {
			epoch++
		}
		_, err = tx.Exec(ctx, "UPDATE "+r.archive.store.table("delivery_rebuilds")+" SET owner=$3,epoch=$4,lease_until=clock_timestamp()+$5::bigint*interval '1 millisecond' WHERE consumer_id=$1 AND generation=$2", r.consumer, r.generation, r.owner, epoch, r.lease.Milliseconds())
		if err != nil {
			return err
		}
		actual, raw, err := r.archive.sourceBlock(ctx, tx, height)
		if err != nil {
			return err
		}
		if actual.BlockHash != block.BlockHash || actual.RawCount != block.RawCount || Hash(raw) != block.SegmentHash {
			return ErrIntegrity
		}
		for _, record := range records {
			if err = r.projector.ApplyRawTx(ctx, tx, r.generation, record); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, "UPDATE "+r.archive.store.table("delivery_rebuilds")+" SET next_height=$5,raw_count=raw_count+$6,revision=revision+1 WHERE consumer_id=$1 AND generation=$2 AND owner=$3 AND epoch=$4 AND lease_until>clock_timestamp()", r.consumer, r.generation, r.owner, epoch, height+1, block.RawCount)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return nil
	})
}
func (r *ReplayEngine) verify(ctx context.Context, m ArchiveManifest, digest string) error {
	return r.archive.store.mutate(ctx, func(tx pgx.Tx) error {
		var next, count, epoch int64
		var stored string
		err := tx.QueryRow(ctx, "SELECT next_height,raw_count,manifest_digest,epoch FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2 AND owner=$3 AND lease_until>clock_timestamp() AND status='building' FOR UPDATE", r.consumer, r.generation, r.owner).Scan(&next, &count, &stored, &epoch)
		if err != nil {
			return ErrLease
		}
		if next != m.ToHeight+1 || count != m.RawCount || stored != digest {
			return ErrGap
		}
		for _, block := range m.Blocks {
			actual, raw, err := r.archive.sourceBlock(ctx, tx, block.Height)
			if err != nil {
				return err
			}
			if actual.BlockHash != block.BlockHash || actual.RawCount != block.RawCount || Hash(raw) != block.SegmentHash {
				return ErrIntegrity
			}
		}
		rebuilt, err := r.projector.ProjectionDigestTx(ctx, tx, r.generation)
		if err != nil {
			return err
		}
		canonical, err := r.projector.CanonicalDigestTx(ctx, tx, m)
		if err != nil {
			return err
		}
		if !archiveHash.MatchString(rebuilt) || rebuilt != canonical {
			return ErrIntegrity
		}
		last := m.Blocks[len(m.Blocks)-1]
		watermark := SourceOrder{strconv.FormatInt(last.Height, 10), "0", "0"}
		if last.RawCount > 0 {
			records, err := r.archive.records(ctx, last)
			if err != nil {
				return err
			}
			record := records[len(records)-1]
			watermark.TransactionIndex = strconv.FormatUint(uint64(record.TransactionIndex), 10)
			watermark.LogIndex = strconv.FormatUint(uint64(record.LogIndex), 10)
		}
		raw, err := json.Marshal(watermark)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE "+r.archive.store.table("delivery_rebuilds")+" SET status='verified',projection_digest=$5,final_watermark=$6,revision=revision+1 WHERE consumer_id=$1 AND generation=$2 AND owner=$3 AND epoch=$4 AND lease_until>clock_timestamp()", r.consumer, r.generation, r.owner, epoch, rebuilt, raw)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return nil
	})
}
func (r *ReplayEngine) Promote(ctx context.Context, expectedRevision int64) error {
	if err := r.authorize(ctx, "replay.promote"); err != nil {
		return errors.New("delivery: administrator authorization required")
	}
	return r.archive.store.mutate(ctx, func(tx pgx.Tx) error {
		var digest, status string
		var epoch int64
		err := tx.QueryRow(ctx, "SELECT manifest_digest,status,epoch FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2 AND owner=$3 AND lease_until>clock_timestamp() FOR UPDATE", r.consumer, r.generation, r.owner).Scan(&digest, &status, &epoch)
		if err != nil {
			return ErrLease
		}
		if status != "verified" {
			return ErrIntegrity
		}
		var compared bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+r.archive.store.table("delivery_shadow_comparisons")+" WHERE manifest_digest=$1 AND event_count>0)", digest).Scan(&compared); err != nil {
			return err
		}
		if !compared {
			return ErrIntegrity
		}
		var previous string
		var revision int64
		err = tx.QueryRow(ctx, "SELECT generation,revision FROM "+r.archive.store.table("delivery_read_aliases")+" WHERE consumer_id=$1 FOR UPDATE", r.consumer).Scan(&previous, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			revision = 0
			previous = ""
		} else if err != nil {
			return err
		}
		if revision != expectedRevision || previous == r.generation {
			return ErrIntegrity
		}
		var raw []byte
		if err = tx.QueryRow(ctx, "SELECT manifest FROM "+r.archive.store.table("delivery_archive_manifests")+" WHERE manifest_digest=$1", digest).Scan(&raw); err != nil {
			return err
		}
		var m ArchiveManifest
		if json.Unmarshal(raw, &m) != nil || ValidateArchiveManifest(m) != nil {
			return ErrIntegrity
		}
		var frontier int64
		if err = tx.QueryRow(ctx, "SELECT typed_complete_height FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1", m.SourceStream).Scan(&frontier); err != nil {
			return err
		}
		if frontier != m.ToHeight {
			return ErrGap
		}
		current, err := r.projector.ProjectionDigestTx(ctx, tx, r.generation)
		if err != nil {
			return err
		}
		canonical, err := r.projector.CanonicalDigestTx(ctx, tx, m)
		if err != nil || current != canonical || !archiveHash.MatchString(current) {
			return ErrIntegrity
		}
		for _, block := range m.Blocks {
			actual, raw, err := r.archive.sourceBlock(ctx, tx, block.Height)
			if err != nil {
				return err
			}
			if actual.BlockHash != block.BlockHash || actual.RawCount != block.RawCount || Hash(raw) != block.SegmentHash {
				return ErrIntegrity
			}
			if _, err = r.archive.read(ctx, "segments", block.SegmentHash, 64<<20); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+r.archive.store.table("delivery_read_aliases")+"(consumer_id,generation,prior_generation,revision) VALUES($1,$2,NULLIF($3,''),$4) ON CONFLICT(consumer_id) DO UPDATE SET generation=EXCLUDED.generation,prior_generation=EXCLUDED.prior_generation,revision=EXCLUDED.revision", r.consumer, r.generation, previous, revision+1)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+r.archive.store.table("delivery_cutover_history")+"(consumer_id,revision,generation,prior_generation) VALUES($1,$2,$3,NULLIF($4,''))", r.consumer, revision+1, r.generation, previous)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "UPDATE "+r.archive.store.table("delivery_rebuilds")+" SET status='active',revision=revision+1 WHERE consumer_id=$1 AND generation=$2 AND owner=$3 AND epoch=$4 AND lease_until>clock_timestamp()", r.consumer, r.generation, r.owner, epoch)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		return nil
	})
}
func (r *ReplayEngine) Status(ctx context.Context, lagBudget int64) (ReplayStatus, error) {
	var s ReplayStatus
	s.Generation = r.generation
	if lagBudget < 0 {
		return s, ErrSchema
	}
	err := r.archive.store.pool.QueryRow(ctx, "SELECT status,next_height,raw_count,revision FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2", r.consumer, r.generation).Scan(&s.Status, &s.NextHeight, &s.RawCount, &s.Revision)
	if err != nil {
		return s, err
	}
	err = r.archive.store.pool.QueryRow(ctx, "SELECT typed_complete_height FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1", r.archive.stream).Scan(&s.SourceHeight)
	if err != nil {
		return s, err
	}
	var missing int64
	err = r.archive.store.pool.QueryRow(ctx, `SELECT count(*) FROM generate_series(GREATEST(0,$2::bigint- $3::bigint),$2::bigint) h LEFT JOIN indexer.indexer_block_completeness_v1 b ON b.chain_id=125 AND b.stream_id=$1 AND b.height=h WHERE b.height IS NULL OR NOT b.fetch_complete OR NOT b.required_decode_complete`, r.archive.stream, s.SourceHeight, lagBudget).Scan(&missing)
	if err != nil {
		return s, err
	}
	s.Gap = missing != 0
	s.Ready = !s.Gap && (s.Status == "verified" || s.Status == "active") && s.NextHeight-1 >= s.SourceHeight-lagBudget
	return s, nil
}
func DiskRunwaySeconds(backlogBytes, freeBytes, reserveBytes, arrivalBytesPerSecond, drainBytesPerSecond int64) (int64, error) {
	if backlogBytes < 0 || freeBytes <= reserveBytes || reserveBytes < 0 || arrivalBytesPerSecond <= 0 || drainBytesPerSecond < 0 {
		return 0, errors.New("delivery: measured disk runway unavailable")
	}
	growth := arrivalBytesPerSecond - drainBytesPerSecond
	if growth <= 0 {
		return 0, errors.New("delivery: positive measured backlog growth unavailable")
	}
	return (freeBytes - reserveBytes) / growth, nil
}

func (r *ReplayEngine) RollbackAlias(ctx context.Context, expectedRevision int64) error {
	if err := r.authorize(ctx, "replay.rollback"); err != nil {
		return errors.New("delivery: administrator authorization required")
	}
	return r.archive.store.mutate(ctx, func(tx pgx.Tx) error {
		var current, prior string
		var revision int64
		err := tx.QueryRow(ctx, "SELECT generation,COALESCE(prior_generation,''),revision FROM "+r.archive.store.table("delivery_read_aliases")+" WHERE consumer_id=$1 FOR UPDATE", r.consumer).Scan(&current, &prior, &revision)
		if err != nil {
			return err
		}
		if revision != expectedRevision || current != r.generation || prior == "" {
			return ErrIntegrity
		}
		var status string
		if err = tx.QueryRow(ctx, "SELECT status FROM "+r.archive.store.table("delivery_rebuilds")+" WHERE consumer_id=$1 AND generation=$2 FOR UPDATE", r.consumer, prior).Scan(&status); err != nil {
			return err
		}
		if status != "active" && status != "verified" && status != "retired" {
			return ErrIntegrity
		}
		_, err = tx.Exec(ctx, "UPDATE "+r.archive.store.table("delivery_read_aliases")+" SET generation=$2,prior_generation=$3,revision=revision+1 WHERE consumer_id=$1", r.consumer, prior, current)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+r.archive.store.table("delivery_cutover_history")+"(consumer_id,revision,generation,prior_generation) VALUES($1,$2,$3,$4)", r.consumer, revision+1, prior, current)
		return err
	})
}

type WorkBudget struct {
	limiter *rate.Limiter
	slots   chan struct{}
}

func NewWorkBudget(concurrency int, eventsPerSecond float64, burst int) (*WorkBudget, error) {
	if concurrency < 1 || concurrency > 64 || math.IsNaN(eventsPerSecond) || math.IsInf(eventsPerSecond, 0) || eventsPerSecond <= 0 || eventsPerSecond > 10000 || burst < 1 || burst > 1000 {
		return nil, errors.New("delivery: explicit bounded workload budget required")
	}
	return &WorkBudget{rate.NewLimiter(rate.Limit(eventsPerSecond), burst), make(chan struct{}, concurrency)}, nil
}
func (b *WorkBudget) acquire(ctx context.Context) (func(), error) {
	if b == nil {
		return nil, errors.New("delivery: workload budget unavailable")
	}
	select {
	case b.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := b.limiter.Wait(ctx); err != nil {
		<-b.slots
		return nil, err
	}
	return func() { <-b.slots }, nil
}
func (r *ReplayEngine) RunBlockWithBudget(ctx context.Context, budget *WorkBudget) error {
	release, err := budget.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	copyEngine := *r
	copyEngine.budget = budget
	return copyEngine.RunBlock(ctx)
}
func (b *WebhookBridge) RunOnceWithBudget(ctx context.Context, budget *WorkBudget) error {
	release, err := budget.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	copyBridge := *b
	copyBridge.budget = budget
	return copyBridge.RunOnce(ctx)
}

type ConsumerReplayStatus struct {
	Committed                    SourceOrder
	BrokerStream                 string
	BrokerSequence               uint64
	SourceHeight, ManifestHeight int64
	Gap, Ready                   bool
}

func (s *Store) ConsumerReplayStatus(ctx context.Context, key ConsumerKey, sourceStream string, lagBudget int64) (ConsumerReplayStatus, error) {
	var out ConsumerReplayStatus
	if !token.MatchString(key.ConsumerID) || !token.MatchString(key.ProjectionGeneration) || !token.MatchString(sourceStream) || lagBudget < 0 || lagBudget > 10000 {
		return out, ErrSchema
	}
	var raw []byte
	var sequence int64
	err := s.pool.QueryRow(ctx, "SELECT source_order,broker_stream,broker_sequence FROM "+s.table("delivery_consumer_watermarks")+" WHERE consumer_id=$1 AND generation=$2 AND partition_kind=$3 AND partition_key=$4", key.ConsumerID, key.ProjectionGeneration, key.PartitionKind, key.PartitionKey).Scan(&raw, &out.BrokerStream, &sequence)
	if err != nil {
		return out, err
	}
	if sequence <= 0 || !token.MatchString(out.BrokerStream) || json.Unmarshal(raw, &out.Committed) != nil {
		return out, ErrIntegrity
	}
	for _, component := range []string{out.Committed.Height, out.Committed.TransactionIndex, out.Committed.LogIndex} {
		if !decimal.MatchString(component) {
			return out, ErrIntegrity
		}
		if _, err := strconv.ParseUint(component, 10, 63); err != nil {
			return out, ErrIntegrity
		}
	}
	out.BrokerSequence = uint64(sequence)
	height, err := strconv.ParseInt(out.Committed.Height, 10, 64)
	if err != nil {
		return out, ErrIntegrity
	}
	err = s.pool.QueryRow(ctx, "SELECT typed_complete_height FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1", sourceStream).Scan(&out.SourceHeight)
	if err != nil {
		return out, err
	}
	if out.SourceHeight < 0 || height > out.SourceHeight {
		return out, ErrIntegrity
	}
	if out.SourceHeight-height > 10000 {
		return out, errors.New("delivery: consumer reconciliation range exceeds bounded readiness admission")
	}
	var missing int64
	err = s.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE b.height IS NULL),COALESCE(min(h) FILTER(WHERE b.height IS NULL)-1,$3::bigint) FROM generate_series($2::bigint,$3::bigint) h LEFT JOIN indexer.indexer_block_completeness_v1 b ON b.chain_id=125 AND b.stream_id=$1 AND b.height=h AND b.fetch_complete AND b.required_decode_complete`, sourceStream, height, out.SourceHeight).Scan(&missing, &out.ManifestHeight)
	if err != nil {
		return out, err
	}
	out.Gap = missing > 0
	out.Ready = !out.Gap && height <= out.SourceHeight && height >= out.SourceHeight-lagBudget
	return out, nil
}
