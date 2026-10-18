package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/chainread"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

var candidateDigest string
var address = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var identifier = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)
var schemaName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type retentionConfig struct {
	Pool                    string `json:"pool"`
	Interval                string `json:"interval"`
	From                    int64  `json:"from"`
	To                      int64  `json:"to"`
	ManifestsPath           string `json:"manifestsPath"`
	ManifestsSHA256         string `json:"manifestsSha256"`
	DeleteStagingObject     string `json:"deleteStagingObject"`
	StagingDeletionAdmitted bool   `json:"stagingDeletionAdmitted"`
}
type config struct {
	CandidateDigest        string          `json:"candidateDigest"`
	InterfaceDigest        string          `json:"interfaceDigest"`
	Schema                 string          `json:"schema"`
	SourceStream           string          `json:"sourceStream"`
	NATSStream             string          `json:"natsStream"`
	NATSDurable            string          `json:"natsDurable"`
	Applications           []string        `json:"applications"`
	Pools                  []string        `json:"pools"`
	HotPool                string          `json:"hotPool"`
	From                   int64           `json:"from"`
	To                     int64           `json:"to"`
	SampleMillis           int             `json:"sampleMillis"`
	MaximumBars            int             `json:"maximumBars"`
	RawArtifactBudgetBytes int64           `json:"rawArtifactBudgetBytes"`
	StartAt                time.Time       `json:"startAt"`
	BurstOffsetSeconds     int             `json:"burstOffsetSeconds"`
	RecoverySeconds        int             `json:"recoverySeconds"`
	Retention              retentionConfig `json:"retention"`
}
type sample struct {
	ObservedAt           time.Time       `json:"observedAt"`
	ElapsedNanos         int64           `json:"elapsedNanos"`
	Stage                string          `json:"stage"`
	SourceHeight         int64           `json:"sourceHeight"`
	ChartWatermark       int64           `json:"chartWatermark"`
	AcceptedFills        int64           `json:"acceptedFills"`
	HotPoolFills         int64           `json:"hotPoolFills"`
	CandlePublications   int64           `json:"candlePublications"`
	ChartOutboxPending   int64           `json:"chartOutboxPending"`
	RollupPending        int64           `json:"rollupPending"`
	BrokerPending        uint64          `json:"brokerPending"`
	BrokerAckPending     int             `json:"brokerAckPending"`
	WaitingBackends      int64           `json:"waitingBackends"`
	ActiveBackends       int64           `json:"activeBackends"`
	GapSeries            int64           `json:"gapSeries"`
	FrozenSeries         int64           `json:"frozenSeries"`
	UnresolvedDeliveries int64           `json:"unresolvedDeliveries"`
	Deadlocks            int64           `json:"databaseDeadlocks"`
	WALBytes             string          `json:"walBytes"`
	QueryNanos           int64           `json:"observerQueryNanos"`
	LeaseOwners          json.RawMessage `json:"leaseOwners"`
}
type rawBudget struct{ maximum, used int64 }
type boundedArtifact struct {
	file   *os.File
	budget *rawBudget
}

func (w boundedArtifact) Write(raw []byte) (int, error) {
	if int64(len(raw)) > w.budget.maximum-w.budget.used {
		return 0, errors.New("raw artifact byte budget exceeded")
	}
	n, err := w.file.Write(raw)
	w.budget.used += int64(n)
	return n, err
}

type measurements struct {
	Samples   boundedArtifact
	Latencies boundedArtifact
	Locks     boundedArtifact
}

func boundedDocument(path, digest string, maximum int64, value any) error {
	if !filepath.IsAbs(path) || !hash.MatchString(digest) {
		return errors.New("absolute digest-bound input required")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maximum {
		return errors.New("bounded regular input required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest {
		return errors.New("input digest mismatch")
	}
	decoder := json.NewDecoder(bytesReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing input data")
	}
	return nil
}
func bytesReader(raw []byte) *bytes.Reader { return bytes.NewReader(raw) }
func (c config) validate() error {
	if candidateDigest == "" || !hash.MatchString(c.CandidateDigest) || candidateDigest != c.CandidateDigest || !hash.MatchString(c.InterfaceDigest) || !schemaName.MatchString(c.Schema) || !identifier.MatchString(c.SourceStream) || !identifier.MatchString(c.NATSStream) || !identifier.MatchString(c.NATSDurable) || len(c.Pools) < 2 || len(c.Pools) > 1000 || len(c.Applications) < 1 || len(c.Applications) > 128 || c.SampleMillis < 1000 || c.SampleMillis > 10000 || c.MaximumBars < 1 || c.MaximumBars > 2000000 || c.RawArtifactBudgetBytes < 1048576 || c.RawArtifactBudgetBytes > 1<<40 || c.StartAt.IsZero() || c.BurstOffsetSeconds < 1 || c.BurstOffsetSeconds+300 > 86400 || c.RecoverySeconds < 1 || c.RecoverySeconds > 1800 {
		return errors.New("explicit candidate-bound measurement profile required")
	}
	seen := map[string]bool{}
	for _, pool := range c.Pools {
		if !address.MatchString(pool) || seen[pool] {
			return errors.New("unique actual pools required")
		}
		seen[pool] = true
	}
	if !seen[c.HotPool] {
		return errors.New("admitted hot pool required")
	}
	for _, name := range c.Applications {
		if !identifier.MatchString(name) {
			return errors.New("exact PostgreSQL application identity required")
		}
	}
	from, err := engine.BucketStart(c.From, "1w")
	if err != nil || from != c.From {
		return errors.New("complete UTC parent boundary required")
	}
	to, err := engine.BucketStart(c.To, "1w")
	if err != nil || to != c.To || c.To <= c.From || c.To-c.From > 2*604800 {
		return errors.New("bounded complete UTC parent boundary required")
	}
	for _, name := range []string{"DATABASE_URL", "RPC_URL", "NATS_URL", "S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"} {
		if os.Getenv(name) == "" {
			return fmt.Errorf("required provider input absent: %s", name)
		}
	}
	return nil
}
func stage(elapsed time.Duration, c config) string {
	seconds := int(elapsed / time.Second)
	if seconds >= c.BurstOffsetSeconds && seconds < c.BurstOffsetSeconds+300 {
		return "burst"
	}
	return "steady"
}
func capture(ctx context.Context, pool *pgxpool.Pool, c config, broker jetstream.Consumer, elapsed time.Duration, output measurements) (sample, error) {
	result := sample{ElapsedNanos: elapsed.Nanoseconds(), Stage: stage(elapsed, c)}
	started := time.Now()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='5s'`); err != nil {
		return result, err
	}
	var rawHeight int64
	var alive bool
	err = tx.QueryRow(ctx, `SELECT clock_timestamp(),w.raw_complete_height,w.typed_complete_height,l.expires_at>clock_timestamp() FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_ingest_leases_v1 l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1`, c.SourceStream).Scan(&result.ObservedAt, &rawHeight, &result.SourceHeight, &alive)
	if err != nil {
		return result, err
	}
	if !alive || rawHeight != result.SourceHeight {
		return result, errors.New("complete canonical source and live fence required")
	}
	var covered int
	err = tx.QueryRow(ctx, `SELECT min(watermark_height),count(*),count(*) FILTER(WHERE has_gaps) FROM `+c.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=ANY($1)`, c.Pools).Scan(&result.ChartWatermark, &covered, &result.GapSeries)
	if err != nil {
		return result, err
	}
	if covered != len(c.Pools) {
		return result, errors.New("all observed series clocks must exist")
	}
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE pool=$2) FROM `+c.Schema+`.chart_inbox WHERE chain_id='125' AND pool=ANY($1)`, c.Pools, c.HotPool).Scan(&result.AcceptedFills, &result.HotPoolFills)
	if err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM `+c.Schema+`.chart_stream_sources WHERE chain_id='125' AND pool=ANY($1)),(SELECT count(*) FROM `+c.Schema+`.chart_outbox WHERE chain_id='125' AND pool=ANY($1) AND published_at IS NULL),(SELECT count(*) FROM `+c.Schema+`.chart_rollup_pending WHERE chain_id='125' AND pool=ANY($1))`, c.Pools).Scan(&result.CandlePublications, &result.ChartOutboxPending, &result.RollupPending)
	if err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE wait_event_type='Lock'),count(*) FILTER(WHERE state='active') FROM pg_stat_activity WHERE application_name=ANY($1)`, c.Applications).Scan(&result.WaitingBackends, &result.ActiveBackends)
	if err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&result.Deadlocks); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT wal_bytes::text FROM pg_stat_wal`).Scan(&result.WALBytes); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM `+c.Schema+`.chart_pool_fences WHERE chain_id='125' AND pool=ANY($1) AND frozen),(SELECT count(*) FROM `+c.Schema+`.chart_delivery_attempts WHERE NOT resolved)`, c.Pools).Scan(&result.FrozenSeries, &result.UnresolvedDeliveries); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(jsonb_build_object('pool',pool,'epoch',epoch,'frozen',frozen) ORDER BY pool),'[]'::jsonb) FROM `+c.Schema+`.chart_pool_fences WHERE chain_id='125' AND pool=ANY($1)`, c.Pools).Scan(&result.LeaseOwners); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT jsonb_build_object('pid',a.pid,'application',a.application_name,'waitType',a.wait_event_type,'waitEvent',a.wait_event,'blockedBy',pg_blocking_pids(a.pid),'queryStart',a.query_start,'transactionStart',a.xact_start,'lockType',l.locktype,'mode',l.mode,'relation',l.relation,'waitStarted',l.waitstart,'waitNanos',CASE WHEN l.waitstart IS NOT NULL THEN (extract(epoch FROM(clock_timestamp()-l.waitstart))*1000000000)::numeric(30,0)::text ELSE NULL END) FROM pg_stat_activity a LEFT JOIN pg_locks l ON l.pid=a.pid AND NOT l.granted WHERE a.application_name=ANY($1) AND a.wait_event_type='Lock' ORDER BY a.pid,l.locktype,l.mode`, c.Applications)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return result, err
		}
		if _, err = output.Locks.Write(append(raw, '\n')); err != nil {
			rows.Close()
			return result, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	info, err := broker.Info(ctx)
	if err != nil {
		return result, err
	}
	result.BrokerPending = info.NumPending
	result.BrokerAckPending = info.NumAckPending
	result.QueryNanos = time.Since(started).Nanoseconds()
	return result, json.NewEncoder(output.Samples).Encode(result)
}
func reconcile(ctx context.Context, pool *pgxpool.Pool, c config, output measurements, cutoff int64) (map[string]any, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout='15min'`); err != nil {
		return nil, err
	}
	reference, err := engine.NewMeasurementReference(c.MaximumBars)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT f.payload,f.source_fact_digest,EXISTS(SELECT 1 FROM `+c.Schema+`.chart_inbox i WHERE i.chain_id='125' AND i.emitter=f.payload->>'actualEmitter' AND i.code_epoch=f.payload->'provenance'->>'emitterCodeEpoch' AND i.tx_hash=f.transaction_hash AND i.log_index=f.log_index AND i.pool=f.pool_address AND i.fill=f.payload),pg_xact_commit_timestamp(f.xmin),pg_xact_commit_timestamp(i.xmin) FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs_v1 r USING(chain_id,transaction_hash,log_index) JOIN indexer.indexer_block_completeness_v1 b ON b.chain_id=r.chain_id AND b.stream_id=$5 AND b.height=r.height AND b.block_hash=r.block_hash AND b.fetch_complete AND b.required_decode_complete LEFT JOIN `+c.Schema+`.chart_inbox i ON i.chain_id='125' AND i.emitter=f.payload->>'actualEmitter' AND i.code_epoch=f.payload->'provenance'->>'emitterCodeEpoch' AND i.tx_hash=f.transaction_hash AND i.log_index=f.log_index WHERE f.chain_id=125 AND f.pool_address=ANY($1) AND (r.height<=$4 OR i.tx_hash IS NOT NULL) AND (f.payload->>'blockTimestampSeconds')::bigint>=$2 AND (f.payload->>'blockTimestampSeconds')::bigint<$3 ORDER BY f.pool_address,r.height,r.transaction_index,r.log_index`, c.Pools, c.From, c.To, cutoff, c.SourceStream)
	if err != nil {
		return nil, err
	}
	var accepted uint64
	for rows.Next() {
		var raw []byte
		var storedDigest string
		var same bool
		var sourceTime, chartTime *time.Time
		if err = rows.Scan(&raw, &storedDigest, &same, &sourceTime, &chartTime); err != nil {
			rows.Close()
			return nil, err
		}
		if !same {
			rows.Close()
			return nil, errors.New("canonical fill missing or changed at chart inbox")
		}
		fill, err := accounting.DecodeFillV1(raw)
		if err != nil {
			rows.Close()
			return nil, err
		}
		digest, err := accounting.FillDigest(fill)
		if err != nil || string(digest) != storedDigest {
			rows.Close()
			return nil, errors.New("canonical source fact digest disagreement")
		}
		if err = reference.Add(fill); err != nil {
			rows.Close()
			return nil, err
		}
		accepted++
		if sourceTime == nil || chartTime == nil || chartTime.Before(*sourceTime) {
			rows.Close()
			return nil, errors.New("actual PostgreSQL commit latency authority absent or regressing")
		}
		if err = json.NewEncoder(output.Latencies).Encode(map[string]any{"sourceFillId": fill.SourceFillID, "pool": fill.PoolAddress, "canonicalBlockTimestampSeconds": fill.BlockTimestampSeconds, "sourceCommittedAt": sourceTime, "chartCommittedAt": chartTime, "sourceToChartNanos": chartTime.Sub(*sourceTime).Nanoseconds()}); err != nil {
			rows.Close()
			return nil, err
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if accepted == 0 {
		return nil, errors.New("actual canonical fills required for exact end-state measurement")
	}
	var chartCount uint64
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM `+c.Schema+`.chart_inbox WHERE chain_id='125' AND pool=ANY($1) AND (fill->>'blockTimestampSeconds')::bigint>=$2 AND (fill->>'blockTimestampSeconds')::bigint<$3`, c.Pools, c.From, c.To).Scan(&chartCount); err != nil {
		return nil, err
	}
	if chartCount != accepted {
		return nil, errors.New("extra or missing accepted chart identity")
	}
	rows, err = tx.Query(ctx, `SELECT snapshot FROM `+c.Schema+`.chart_exact WHERE chain_id='125' AND pool=ANY($1) AND start>=$2 AND start<$3 ORDER BY pool,interval,start`, c.Pools, c.From, c.To)
	if err != nil {
		return nil, err
	}
	actual := []engine.MeasurementBar{}
	for rows.Next() {
		var raw []byte
		var bar engine.SeriesCandle
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &bar); err != nil {
			rows.Close()
			return nil, err
		}
		mapped, err := engine.MeasurementActual(bar)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if len(actual) >= c.MaximumBars {
			rows.Close()
			return nil, errors.New("actual bar budget exceeded")
		}
		actual = append(actual, mapped)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	expectedDigest, err := engine.MeasurementDigest(reference.Bars())
	if err != nil {
		return nil, err
	}
	actualDigest, err := engine.MeasurementDigest(actual)
	if err != nil {
		return nil, err
	}
	if expectedDigest != actualDigest {
		return nil, errors.New("independent rational reference differs from production candle rows")
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"acceptedUniqueFills": accepted, "referenceDigest": expectedDigest, "actualDigest": actualDigest, "barCount": len(actual), "algorithmVersion": engine.SeriesAlgorithm, "timeframes": 7, "exactParity": true}, nil
}
func run() error {
	var configPath, configHash, output string
	var release bool
	flag.StringVar(&configPath, "config", "", "absolute provider configuration")
	flag.StringVar(&configHash, "config-sha256", "", "configuration digest")
	flag.StringVar(&output, "output", "", "private artifact directory")
	flag.BoolVar(&release, "release", false, "measure full24-hour serial release profile")
	flag.Parse()
	var c config
	if err := boundedDocument(configPath, configHash, 1<<20, &c); err != nil {
		return err
	}
	if err := c.validate(); err != nil {
		return err
	}
	if !filepath.IsAbs(output) {
		return errors.New("absolute private output directory required")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		return err
	}
	makeFile := func(name string) (*os.File, error) {
		return os.OpenFile(filepath.Join(output, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	samples, err := makeFile("samples.ndjson")
	if err != nil {
		return err
	}
	defer samples.Close()
	latencies, err := makeFile("latencies.ndjson")
	if err != nil {
		return err
	}
	defer latencies.Close()
	locks, err := makeFile("locks.ndjson")
	if err != nil {
		return err
	}
	defer locks.Close()
	budget := &rawBudget{maximum: c.RawArtifactBudgetBytes}
	files := measurements{boundedArtifact{samples, budget}, boundedArtifact{latencies, budget}, boundedArtifact{locks, budget}}
	timeout := 25 * time.Minute
	if release {
		timeout = 24*time.Hour + time.Duration(c.RecoverySeconds)*time.Second + 30*time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	db, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return errors.New("actual database unavailable")
	}
	defer db.Close()
	var commitAuthority string
	if err = db.QueryRow(ctx, `SELECT current_setting('track_commit_timestamp')`).Scan(&commitAuthority); err != nil || commitAuthority != "on" {
		return errors.New("actual commit timestamp authority must be enabled")
	}
	rpc, err := ethclient.DialContext(ctx, os.Getenv("RPC_URL"))
	if err != nil {
		return errors.New("actual canonical RPC unavailable")
	}
	defer rpc.Close()
	chain, err := rpc.ChainID(ctx)
	if err != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return errors.New("actual chain125 required")
	}
	nc, err := nats.Connect(os.Getenv("NATS_URL"), nats.NoReconnect(), nats.Timeout(3*time.Second))
	if err != nil {
		return errors.New("actual NATS unavailable")
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	stream, err := js.Stream(ctx, c.NATSStream)
	if err != nil {
		return err
	}
	streamInfo, err := stream.Info(ctx)
	if err != nil || streamInfo.Config.Storage != jetstream.FileStorage {
		return errors.New("actual file-backed broker required")
	}
	broker, err := js.Consumer(ctx, c.NATSStream, c.NATSDurable)
	if err != nil {
		return err
	}
	st, err := store.NewExact(db, c.Schema)
	if err != nil {
		return err
	}
	objects, err := storage.New(ctx, storage.Config{Endpoint: os.Getenv("S3_ENDPOINT"), Region: os.Getenv("S3_REGION"), AccessKeyID: os.Getenv("S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"), Bucket: os.Getenv("S3_BUCKET")})
	if err != nil {
		return errors.New("actual archive object provider unavailable")
	}
	reader := store.ArchiveReader{Objects: objects, Bucket: objects.Bucket(), Exact: st, BlockTime: func(ctx context.Context, height int64, expected string) (int64, error) {
		bounded, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		block, err := (chainread.Reader{RPC: rpc}).Block(bounded, height)
		if err != nil {
			return 0, err
		}
		if !strings.EqualFold(block.Hash, expected) {
			return 0, store.ErrIntegrity
		}
		return block.Time, nil
	}}
	var manifests []store.ArchiveManifest
	if err = boundedDocument(c.Retention.ManifestsPath, c.Retention.ManifestsSHA256, 16<<20, &manifests); err != nil {
		return err
	}
	retention, err := st.RetentionDryRun(ctx, reader, c.Retention.Pool, c.Retention.Interval, c.Retention.From, c.Retention.To, manifests)
	if err != nil {
		return err
	}
	if release {
		delay := time.Until(c.StartAt)
		if delay < 0 || delay > 5*time.Minute {
			return errors.New("fresh coordinated release start within five minutes required")
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	started := time.Now()
	first, err := capture(ctx, db, c, broker, 0, files)
	if err != nil {
		return err
	}
	last := first
	duration := 3 * time.Second
	if release {
		duration = 24 * time.Hour
	}
	ticker := time.NewTicker(time.Duration(c.SampleMillis) * time.Millisecond)
	defer ticker.Stop()
	for time.Since(started) < duration {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			bounded, stop := context.WithTimeout(ctx, 8*time.Second)
			last, err = capture(bounded, db, c, broker, time.Since(started), files)
			stop()
			if err != nil {
				return err
			}
		}
	}
	drainStarted := time.Now()
	recoveryThroughHeight := last.SourceHeight
	for last.ChartWatermark < recoveryThroughHeight || last.RollupPending > 0 || last.BrokerPending > 0 || last.BrokerAckPending > 0 || last.GapSeries > 0 || last.FrozenSeries > 0 || last.UnresolvedDeliveries > 0 {
		if time.Since(drainStarted) > time.Duration(c.RecoverySeconds)*time.Second {
			return errors.New("series did not recover to actual source watermark within explicit recovery budget")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			last, err = capture(ctx, db, c, broker, time.Since(started), files)
			if err != nil {
				return err
			}
		}
	}
	recoveryNanos := time.Since(drainStarted).Nanoseconds()
	parity, err := reconcile(ctx, db, c, files, recoveryThroughHeight)
	if err != nil {
		return err
	}
	missingBlocked := false
	if release && c.Retention.StagingDeletionAdmitted {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]*staging[a-zA-Z0-9_-]*$`).MatchString(objects.Bucket()) || c.Retention.DeleteStagingObject == "" {
			return errors.New("explicit isolated staging archive mutation authority required")
		}
		matched := false
		for _, manifest := range manifests {
			if manifest.ObjectKey == c.Retention.DeleteStagingObject {
				matched = true
			}
		}
		if !matched {
			return errors.New("exact verified staging object required")
		}
		if err = objects.Delete(ctx, c.Retention.DeleteStagingObject); err != nil {
			return err
		}
		_, guardErr := st.RetentionDryRun(ctx, reader, c.Retention.Pool, c.Retention.Interval, c.Retention.From, c.Retention.To, manifests)
		if !errors.Is(guardErr, storage.ErrNotFound) {
			return errors.New("deleted staging archive did not fail retention closed with actual missing object")
		}
		missingBlocked = true
	}
	result := map[string]any{"task": "4.10", "candidateDigest": candidateDigest, "interfaceDigest": c.InterfaceDigest, "captureStarted": started.UTC(), "captureEnded": time.Now().UTC(), "captureDurationNanos": last.ElapsedNanos, "rawArtifactBytes": budget.used, "rawArtifactBudgetBytes": budget.maximum, "coordinatedStartAt": c.StartAt, "first": first, "last": last, "parity": parity, "retention": retention, "retentionMissingObjectBlocked": missingBlocked, "recoveryThroughHeight": recoveryThroughHeight, "recoveryNanos": recoveryNanos, "steadyTargetCanonicalEventsPerSecond": 2000, "burstTargetCanonicalEventsPerSecond": 10000, "burstSeconds": 300, "hotPoolTargetPercent": 10, "chainThroughputInferred": false, "capacityQualified": false, "globalAcceptance": "serial9.1", "profile": "actual_provider_probe"}
	if release {
		result["profile"] = "24_hour_chart_observer"
	}
	file, err := makeFile("result.json")
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(result)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "chart measurement failed:", err)
		os.Exit(1)
	}
}
