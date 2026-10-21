// Package sourcebootstrap is the single definition of the per-pool source
// bootstrap checkpoint that the stats and charts accounting consumers write to
// delivery_partition_offsets.checkpoint_digest, and of the indexer evidence
// reads it is derived from. The producers (core/stats-workers,
// core/trading-charts) and the reader that re-derives the digest to admit a
// pool's committed vector (core/api) all use these functions, so the tuple and
// the evidence queries cannot drift between writer and reader.
package sourcebootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
)

// Domains of the two checkpoint tuples. Changing either changes every digest
// already stored in delivery_partition_offsets.
const (
	StatsDomain  = "stats-source-bootstrap-v1"
	ChartsDomain = "charts-source-bootstrap-v2"
)

var (
	// ErrUnavailable: the indexer does not (yet) hold the evidence.
	ErrUnavailable = errors.New("sourcebootstrap: source bootstrap evidence unavailable")
	// ErrIntegrity: the indexer evidence is present but not a market origin.
	ErrIntegrity = errors.New("sourcebootstrap: source bootstrap evidence conflicts")
)

// Querier is satisfied by pgx.Tx, *pgx.Conn and *pgxpool.Pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StatsEvidence is what the stats consumer bootstraps a market partition from:
// the replay runs from the stream's first indexer commit through the commit
// holding the pool's first fill.
type StatsEvidence struct {
	SourceStream, Pool, ProjectionVersion string
	PublicationID                         string
	PayloadDigest                         accounting.Digest
	InitialPreviousHeight                 int64
	FirstCommitID                         string
	FirstCommitLastHeight                 int64
	FirstCommitBlockHash                  string
}

// Checkpoint is the stats delivery_partition_offsets.checkpoint_digest.
func (e StatsEvidence) Checkpoint() accounting.Digest {
	return accounting.Digest(accounting.HashTuple(StatsDomain, e.SourceStream, e.Pool, e.ProjectionVersion, e.PublicationID, string(e.PayloadDigest), strconv.FormatInt(e.InitialPreviousHeight, 10), e.FirstCommitID, strconv.FormatInt(e.FirstCommitLastHeight, 10), e.FirstCommitBlockHash))
}

// ChartsEvidence is what the charts consumer bootstraps a market partition
// from: the replay runs from the indexer commit boundary before the pool's
// creation block through the commit holding the pool's first fill.
type ChartsEvidence struct {
	SourceStream, Pool, ProjectionVersion string
	PublicationID                         string
	PayloadDigest                         accounting.Digest
	ReplayFromHeight                      int64
	CreationTransactionHash               string
	CreationLogIndex                      int64
	FirstCommitID                         string
	FirstCommitLastHeight                 int64
	FirstCommitBlockHash                  string
}

// Checkpoint is the charts delivery_partition_offsets.checkpoint_digest.
func (e ChartsEvidence) Checkpoint() accounting.Digest {
	return accounting.Digest(accounting.HashTuple(ChartsDomain, e.SourceStream, e.Pool, e.ProjectionVersion, e.PublicationID, string(e.PayloadDigest), strconv.FormatInt(e.ReplayFromHeight, 10), e.CreationTransactionHash, strconv.FormatInt(e.CreationLogIndex, 10), e.FirstCommitID, strconv.FormatInt(e.FirstCommitLastHeight, 10), e.FirstCommitBlockHash))
}

// FirstMarketPosition reads the indexer's sequence-1 market partition position
// wire for pool. pgx.ErrNoRows is returned unchanged.
func FirstMarketPosition(ctx context.Context, q Querier, partitionSchema, projectionVersion, pool string) ([]byte, error) {
	var wire []byte
	err := q.QueryRow(ctx, `SELECT position FROM `+pgx.Identifier{partitionSchema}.Sanitize()+`.indexer_market_partition_positions_v1 WHERE projection_version=$1 AND partition_kind='market' AND partition_key=$2 AND sequence=1`, projectionVersion, "125:"+pool).Scan(&wire)
	return wire, err
}

// MarketPublication reads the indexer's market publication wire for a source
// fill. pgx.ErrNoRows is returned unchanged.
func MarketPublication(ctx context.Context, q Querier, sourceEventID, projectionVersion string) ([]byte, error) {
	var wire []byte
	err := q.QueryRow(ctx, `SELECT payload FROM indexer.indexer_accounting_publications_v1 WHERE payload->>'facet'='market' AND payload->>'sourceFillId'=$1 AND payload->'positions'->0->>'projectionVersion'=$2`, sourceEventID, projectionVersion).Scan(&wire)
	return wire, err
}

// InitialPreviousHeight is the previous_height of the stream's first indexer
// commit. pgx.ErrNoRows is returned unchanged.
func InitialPreviousHeight(ctx context.Context, q Querier, sourceStream string) (int64, error) {
	var height int64
	err := q.QueryRow(ctx, `SELECT previous_height FROM indexer.indexer_ingest_commits_v1 WHERE chain_id=125 AND stream_id=$1 ORDER BY previous_height LIMIT 1`, sourceStream).Scan(&height)
	return height, err
}

// Commit is the indexer commit whose range holds a height.
type Commit struct {
	ID                 string
	LastCompleteHeight int64
	BlockHash          string
}

// CommitContaining reads the indexer commit with previous_height < height <=
// last_complete_height. pgx.ErrNoRows is returned unchanged.
func CommitContaining(ctx context.Context, q Querier, sourceStream string, height int64) (Commit, error) {
	var c Commit
	err := q.QueryRow(ctx, `SELECT batch_id,last_complete_height,block_hash FROM indexer.indexer_ingest_commits_v1 WHERE chain_id=125 AND stream_id=$1 AND previous_height<$2 AND last_complete_height>=$2`, sourceStream, height).Scan(&c.ID, &c.LastCompleteHeight, &c.BlockHash)
	return c, err
}

// PreviousHeightContaining is the previous_height of the indexer commit whose
// range holds height. pgx.ErrNoRows is returned unchanged.
func PreviousHeightContaining(ctx context.Context, q Querier, sourceStream string, height int64) (int64, error) {
	var previous int64
	err := q.QueryRow(ctx, `SELECT previous_height FROM indexer.indexer_ingest_commits_v1 WHERE chain_id=125 AND stream_id=$1 AND previous_height<$2 AND last_complete_height>=$2`, sourceStream, height).Scan(&previous)
	return previous, err
}

// Creation is the indexer's canonical MarketCreated fact for a pool joined to
// the completely ingested raw log it was decoded from.
type Creation struct {
	Height, TransactionIndex, LogIndex int64
	BlockHash, TransactionHash         string
	Factory                            string
	TopicsWire                         []byte
	Data                               []byte
}

// CanonicalCreation reads pool's creation. pgx.ErrNoRows is returned unchanged.
func CanonicalCreation(ctx context.Context, q Querier, sourceStream, pool string) (Creation, error) {
	var c Creation
	err := q.QueryRow(ctx, `SELECT k.height,k.block_hash,k.transaction_hash,k.transaction_index,k.log_index,k.factory,r.topics,r.data FROM indexer.indexer_canonical_creations_v1 k JOIN indexer.indexer_raw_logs_v1 r ON r.chain_id=125 AND r.transaction_hash=k.transaction_hash AND r.log_index=k.log_index AND r.height=k.height AND r.transaction_index=k.transaction_index AND r.block_hash=k.block_hash AND r.emitter=lower(k.factory) JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=125 AND c.stream_id=$2 AND c.height=k.height AND c.block_hash=k.block_hash AND c.fetch_complete AND c.required_decode_complete WHERE lower(k.pool)=$1`, pool, sourceStream).Scan(&c.Height, &c.BlockHash, &c.TransactionHash, &c.TransactionIndex, &c.LogIndex, &c.Factory, &c.TopicsWire, &c.Data)
	return c, err
}

// origin reads and checks the sequence-1 position and its publication, the
// common head of both evidence sets.
func origin(ctx context.Context, q Querier, partitionSchema, projectionVersion, pool string) (accounting.PartitionPosition, accounting.PublicationV1, int64, error) {
	var position accounting.PartitionPosition
	var publication accounting.PublicationV1
	wire, err := FirstMarketPosition(ctx, q, partitionSchema, projectionVersion, pool)
	if err != nil {
		return position, publication, 0, err
	}
	if json.Unmarshal(wire, &position) != nil || accounting.ValidatePartitionPosition(position) != nil || position.Sequence != "1" || position.PreviousEffectIdentity != "" || position.PartitionKind != "market" || position.PartitionKey != "125:"+pool || position.ProjectionVersion != projectionVersion {
		return position, publication, 0, ErrIntegrity
	}
	if wire, err = MarketPublication(ctx, q, position.SourceEventID, projectionVersion); err != nil {
		return position, publication, 0, err
	}
	if publication, err = accounting.DecodePublicationV1(wire); err != nil || publication.Fill == nil || len(publication.Positions) != 1 || publication.Positions[0] != position {
		return position, publication, 0, ErrIntegrity
	}
	height, err := strconv.ParseInt(publication.Fill.Position.Height, 10, 64)
	if err != nil {
		return position, publication, 0, ErrIntegrity
	}
	return position, publication, height, nil
}

func classify(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnavailable
	}
	return err
}

// LoadStats reads the stats bootstrap evidence for pool with the same queries
// the stats consumer bootstraps from.
func LoadStats(ctx context.Context, q Querier, partitionSchema, sourceStream, projectionVersion, pool string) (StatsEvidence, error) {
	e := StatsEvidence{SourceStream: sourceStream, Pool: pool, ProjectionVersion: projectionVersion}
	position, publication, height, err := origin(ctx, q, partitionSchema, projectionVersion, pool)
	if err != nil {
		return e, classify(err)
	}
	e.PublicationID, e.PayloadDigest = publication.PublicationID, position.PayloadDigest
	if e.InitialPreviousHeight, err = InitialPreviousHeight(ctx, q, sourceStream); err != nil {
		return e, classify(err)
	}
	commit, err := CommitContaining(ctx, q, sourceStream, height)
	if err != nil {
		return e, classify(err)
	}
	e.FirstCommitID, e.FirstCommitLastHeight, e.FirstCommitBlockHash = commit.ID, commit.LastCompleteHeight, commit.BlockHash
	return e, nil
}

// LoadCharts reads the charts bootstrap evidence for pool with the same
// queries the charts consumer bootstraps from.
func LoadCharts(ctx context.Context, q Querier, partitionSchema, sourceStream, projectionVersion, pool string) (ChartsEvidence, error) {
	e := ChartsEvidence{SourceStream: sourceStream, Pool: pool, ProjectionVersion: projectionVersion}
	position, publication, height, err := origin(ctx, q, partitionSchema, projectionVersion, pool)
	if err != nil {
		return e, classify(err)
	}
	e.PublicationID, e.PayloadDigest = publication.PublicationID, position.PayloadDigest
	creation, err := CanonicalCreation(ctx, q, sourceStream, pool)
	if err != nil {
		return e, classify(err)
	}
	e.CreationTransactionHash, e.CreationLogIndex = creation.TransactionHash, creation.LogIndex
	if e.ReplayFromHeight, err = PreviousHeightContaining(ctx, q, sourceStream, creation.Height); err != nil {
		return e, classify(err)
	}
	commit, err := CommitContaining(ctx, q, sourceStream, height)
	if err != nil {
		return e, classify(err)
	}
	e.FirstCommitID, e.FirstCommitLastHeight, e.FirstCommitBlockHash = commit.ID, commit.LastCompleteHeight, commit.BlockHash
	return e, nil
}
