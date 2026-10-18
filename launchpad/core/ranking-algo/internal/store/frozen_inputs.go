package store

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FrozenProjectionReader interface {
	ReadRankingUniverseTx(context.Context, pgx.Tx, string, time.Time) (ranker.InputSnapshot, error)
}
type FrozenInputs struct {
	pool   *pgxpool.Pool
	reader FrozenProjectionReader
}

func NewFrozenInputs(pool *pgxpool.Pool, reader FrozenProjectionReader) (*FrozenInputs, error) {
	if pool == nil || reader == nil {
		return nil, ranker.ErrUnavailable
	}
	return &FrozenInputs{pool, reader}, nil
}
func (s *FrozenInputs) Freeze(ctx context.Context, runID string, cutoff time.Time) (ranker.InputSnapshot, error) {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := s.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return ranker.InputSnapshot{}, err
	}
	defer tx.Rollback(bounded)
	in, err := s.reader.ReadRankingUniverseTx(bounded, tx, runID, cutoff)
	if err != nil {
		return ranker.InputSnapshot{}, err
	}
	if !in.Complete || in.RunID != runID || in.CutoffTimeUTC != cutoff.Unix() || len(in.Candidates) > 1000000 || in.Provenance == nil || in.Provenance.Validate(in) != nil || in.Provenance.UniverseDigest != ranker.CanonicalUniverse(in.Candidates) {
		return in, ranker.ErrUnavailable
	}
	if err = tx.Commit(bounded); err != nil {
		return ranker.InputSnapshot{}, err
	}
	return in, nil
}

type RankingVisibility struct {
	Revision   string          `json:"revision"`
	ObservedAt time.Time       `json:"observedAt"`
	Complete   bool            `json:"complete"`
	Pools      map[string]bool `json:"pools"`
}
type VisibilityReader interface {
	ReadRankingVisibility(context.Context) (RankingVisibility, error)
}
type CanonicalInputReader struct {
	SourceSchema, StatsSchema, SourceStream, ProjectionVersion string
	Visibility                                                 VisibilityReader
}

func NewCanonicalInputReader(source, stats, stream, version string, visibility VisibilityReader) (*CanonicalInputReader, error) {
	if !schemaNameValid(source) || !schemaNameValid(stats) || stream == "" || len(stream) > 128 || version == "" || len(version) > 64 || visibility == nil {
		return nil, ranker.ErrUnavailable
	}
	return &CanonicalInputReader{source, stats, stream, version, visibility}, nil
}
func schemaNameValid(value string) bool {
	if len(value) < 1 || len(value) > 63 {
		return false
	}
	for i, c := range value {
		if !(c >= 'a' && c <= 'z' || i > 0 && (c >= '0' && c <= '9' || c == '_')) {
			return false
		}
	}
	return true
}
func (r *CanonicalInputReader) source(name string) string {
	return pgx.Identifier{r.SourceSchema, name}.Sanitize()
}
func (r *CanonicalInputReader) stats(name string) string {
	return pgx.Identifier{r.StatsSchema, name}.Sanitize()
}
func (r *CanonicalInputReader) ReadRankingUniverseTx(ctx context.Context, tx pgx.Tx, runID string, cutoff time.Time) (ranker.InputSnapshot, error) {
	in := ranker.InputSnapshot{RunID: runID, AlgorithmVersion: ranker.AlgorithmVersion, FeatureSchemaVersion: ranker.FeatureSchemaVersion, CutoffTimeUTC: cutoff.Unix(), Candidates: []ranker.Candidate{}}
	if tx == nil || cutoff.Unix() < 86400 {
		return in, ranker.ErrUnavailable
	}
	visibility, err := r.Visibility.ReadRankingVisibility(ctx)
	if err != nil {
		return in, err
	}
	if !visibility.Complete || visibility.Revision == "" || visibility.ObservedAt.IsZero() || visibility.ObservedAt.After(time.Now()) || time.Since(visibility.ObservedAt) > 60*time.Second {
		return in, ranker.ErrUnavailable
	}
	var raw, typed int64
	var alive bool
	err = tx.QueryRow(ctx, "SELECT w.raw_complete_height,w.typed_complete_height,l.expires_at>clock_timestamp() FROM "+r.source("indexer_ingest_watermarks_v1")+" w JOIN "+r.source("indexer_ingest_leases_v1")+" l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1", r.SourceStream).Scan(&raw, &typed, &alive)
	if err != nil {
		return in, err
	}
	if raw != typed || typed < 1 || !alive {
		return in, ranker.ErrUnavailable
	}
	var blockHash string
	var completed time.Time
	var complete bool
	err = tx.QueryRow(ctx, "SELECT block_hash,completed_at,fetch_complete AND required_decode_complete FROM "+r.source("indexer_block_completeness_v1")+" WHERE chain_id=125 AND stream_id=$1 AND height=$2", r.SourceStream, typed).Scan(&blockHash, &completed, &complete)
	if err != nil {
		return in, err
	}
	if !complete || completed.Unix() > cutoff.Unix() {
		return in, ranker.ErrUnavailable
	}
	var coherent bool
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM `+r.stats("accounting_pool_fences_v2")+` WHERE frozen) AND NOT EXISTS(SELECT 1 FROM `+r.source("indexer_accounting_fills_v1")+` f WHERE (f.payload->'position'->>'height')::numeric<=$2 AND NOT EXISTS(SELECT 1 FROM `+r.stats("accounting_market_commits_v2")+` c WHERE c.projection_version=$1 AND c.source_fill_id=f.source_fill_id AND c.source_fact_digest=f.source_fact_digest AND c.fill=f.payload)) AND NOT EXISTS(SELECT 1 FROM `+r.stats("accounting_market_commits_v2")+` c WHERE c.projection_version=$1 AND (c.fill->'position'->>'height')::numeric<=$2 AND NOT EXISTS(SELECT 1 FROM `+r.source("indexer_accounting_fills_v1")+` f WHERE f.source_fill_id=c.source_fill_id AND f.source_fact_digest=c.source_fact_digest AND f.payload=c.fill))`, r.ProjectionVersion, typed).Scan(&coherent)
	if err != nil {
		return in, err
	}
	if !coherent {
		return in, ranker.ErrUnavailable
	}
	in.CutoffHeight = uint64(typed)
	revision := strconv.FormatInt(typed, 10)
	in.Provenance = &ranker.Provenance{Watermark: v1.Watermark{ChainId: "125", Height: v1.UInt256(revision), BlockHash: blockHash, ProjectionRevision: v1.PositiveUInt256(revision), ObservedAt: completed.UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}, StatsRevision: r.ProjectionVersion + ":" + revision, ModerationRevision: visibility.Revision, ActivityRevision: ranker.ActivityModelVersion + ":" + revision, WindowStart: cutoff.Unix() - 86400, WindowEnd: cutoff.Unix()}
	query := r.universeQuery()
	rows, err := tx.Query(ctx, query, r.ProjectionVersion, typed, cutoff.Unix())
	if err != nil {
		return in, err
	}
	for rows.Next() {
		var row universeRow
		if err = rows.Scan(&row.pool, &row.creationHeight, &row.transactionIndex, &row.logIndex, &row.created, &row.day, &row.hour, &row.five, &row.buys, &row.sells, &row.actors, &row.self, &row.boughtTokens, &row.soldTokens, &row.reserveIn, &row.reserveOut, &row.holders, &row.lastUSDL, &row.lastToken, &row.lastDecimals, &row.referenceUSDL, &row.referenceToken, &row.referenceDecimals); err != nil {
			rows.Close()
			return in, err
		}
		visible, exists := visibility.Pools[row.pool]
		if !exists {
			rows.Close()
			return in, errors.New("ranking visibility universe incomplete")
		}
		c, err := row.candidate(cutoff.Unix())
		if err != nil {
			rows.Close()
			return in, err
		}
		c.SourceRevision = in.Provenance.StatsRevision
		c.SourceObservedAt = completed.Unix()
		c.ModerationObservedAt = visibility.ObservedAt.Unix()
		c.Suppressed = !visible
		in.Candidates = append(in.Candidates, c)
		if len(in.Candidates) > 1000000 {
			rows.Close()
			return in, errors.New("ranking universe bound exceeded")
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return in, err
	}
	in.Provenance.UniverseDigest = ranker.CanonicalUniverse(in.Candidates)
	// Every gate above passed inside one repeatable-read transaction: the
	// indexer lease is live, raw and typed heights agree, the cutoff block is
	// fetched and decoded, no accounting fence is frozen, every indexer fill at
	// or below the cutoff height is committed by stats (and vice versa), and
	// every canonical pool has a moderation verdict. The universe is therefore
	// complete at CutoffHeight; per-metric gaps stay in NullableMetrics.
	in.Complete = true
	return in, nil
}
func rawUSDLDecimal(raw string) (string, error) {
	if len(raw) > 156 {
		return "", ranker.ErrUnavailable
	}
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok || n.Sign() < 0 || n.String() != raw {
		return "", ranker.ErrUnavailable
	}
	return new(big.Rat).SetFrac(n, big.NewInt(1000000)).FloatString(6), nil
}
func (r *CanonicalInputReader) MarshalUniverse(ctx context.Context, pool *pgxpool.Pool, runID string, cutoff time.Time) (json.RawMessage, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	in, err := r.ReadRankingUniverseTx(ctx, tx, runID, cutoff)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

var _ FrozenProjectionReader = (*CanonicalInputReader)(nil)

type HTTPRankingVisibility struct {
	endpoint string
	client   *http.Client
}

func NewHTTPRankingVisibility(endpoint string, client *http.Client) (*HTTPRankingVisibility, error) {
	u, err := url.Parse(endpoint)
	if err != nil || client == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, ranker.ErrUnavailable
	}
	return &HTTPRankingVisibility{endpoint: endpoint, client: client}, nil
}

func (c *HTTPRankingVisibility) ReadRankingVisibility(ctx context.Context) (RankingVisibility, error) {
	var out RankingVisibility
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, http.MethodGet, c.endpoint, nil)
	if err != nil {
		return out, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return out, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return out, ranker.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
	if err != nil || len(raw) > 64<<20 {
		return out, ranker.ErrUnavailable
	}
	if json.Unmarshal(raw, &out) != nil || !out.Complete || len(out.Revision) != 64 || out.Pools == nil || len(out.Pools) > 1000000 || out.ObservedAt.IsZero() || out.ObservedAt.After(time.Now()) || time.Since(out.ObservedAt) > 60*time.Second {
		return out, ranker.ErrUnavailable
	}
	return out, nil
}

func (c *HTTPRankingVisibility) CurrentRankingRevision(ctx context.Context) (string, time.Time, error) {
	visibility, err := c.ReadRankingVisibility(ctx)
	return visibility.Revision, visibility.ObservedAt, err
}

func (r *CanonicalInputReader) universeQuery() string {
	return universeQuerySQL(r.stats("accounting_market_commits_v2"), r.source("indexer_canonical_creations_v1"))
}

func (r *CanonicalInputReader) ExplainUniverse(ctx context.Context, pool *pgxpool.Pool, cutoffHeight uint64, cutoff time.Time) (json.RawMessage, error) {
	if r == nil || pool == nil || cutoffHeight < 1 || cutoff.Unix() < 86400 {
		return nil, ranker.ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(bounded)
	var plan []byte
	if err = tx.QueryRow(bounded, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+r.universeQuery(), r.ProjectionVersion, cutoffHeight, cutoff.Unix()).Scan(&plan); err != nil {
		return nil, err
	}
	if len(plan) == 0 || len(plan) > 16<<20 || !json.Valid(plan) {
		return nil, ranker.ErrUnavailable
	}
	return json.RawMessage(plan), tx.Commit(bounded)
}
