package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"io"
	"regexp"
	"strconv"
)

type ArchiveBlock struct {
	Height                                int64
	Hash                                  string
	RawCount                              int
	FetchComplete, RequiredDecodeComplete bool
}
type ArchiveRaw struct {
	TransactionHash, BlockHash, Emitter string
	Height                              int64
	TransactionIndex, LogIndex          uint32
	Topics                              []string
	Data                                []byte
}
type ArchiveEvent struct {
	RawDigest, Kind string
	Timestamp       int64
	Payload         json.RawMessage
	Raw             ArchiveRaw
}
type ArchiveSegment struct {
	FormatVersion        string
	ChainID              int32
	SourceStream         string
	FromHeight, ToHeight int64
	Blocks               []ArchiveBlock
	Records              []ArchiveRaw
	Events               []ArchiveEvent
}
type ArchiveManifest struct {
	FormatVersion string         `json:"formatVersion"`
	ChainID       int32          `json:"chainId"`
	SourceStream  string         `json:"sourceStream"`
	FromHeight    int64          `json:"fromHeight"`
	ToHeight      int64          `json:"toHeight"`
	EventCount    int            `json:"eventCount"`
	TypedCount    int            `json:"typedCount"`
	ByteLength    int64          `json:"byteLength"`
	SHA256        string         `json:"sha256"`
	Bucket        string         `json:"bucket"`
	ObjectKey     string         `json:"objectKey"`
	Blocks        []ArchiveBlock `json:"blockCheckpoints"`
}
type ArchiveReader struct {
	Objects   storage.Reader
	Bucket    string
	Exact     *ExactStore
	BlockTime func(context.Context, int64, string) (int64, error)
}

var archiveDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
var archiveHash = regexp.MustCompile(`^0x[0-9a-f]{64}$`)
var archiveAddress = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

func bytesDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (m ArchiveManifest) Validate() error {
	if m.FormatVersion != "raw-archive-v2" || m.ChainID != 125 || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(m.SourceStream) || m.FromHeight < 0 || m.ToHeight < m.FromHeight || m.ToHeight-m.FromHeight >= 1000 || m.EventCount < 0 || m.EventCount > 65536 || m.TypedCount < 0 || m.TypedCount > 65536 || m.ByteLength < 1 || m.ByteLength > 64<<20 || !archiveDigest.MatchString(m.SHA256) || m.Bucket == "" || m.ObjectKey != fmt.Sprintf("indexer/raw/v2/%s/%s.json", m.SourceStream, m.SHA256) || len(m.Blocks) != int(m.ToHeight-m.FromHeight+1) {
		return ErrIntegrity
	}
	for i, b := range m.Blocks {
		if b.Height != m.FromHeight+int64(i) || !archiveHash.MatchString(b.Hash) || !b.FetchComplete || !b.RequiredDecodeComplete || b.RawCount < 0 {
			return ErrIntegrity
		}
	}
	return nil
}
func archiveRawDigest(r ArchiveRaw) (string, error) {
	if r.Height < 0 || !archiveHash.MatchString(r.TransactionHash) || !archiveHash.MatchString(r.BlockHash) || !archiveAddress.MatchString(r.Emitter) || len(r.Topics) > 4 || len(r.Data) > 1<<20 {
		return "", ErrIntegrity
	}
	for _, t := range r.Topics {
		if !archiveHash.MatchString(t) {
			return "", ErrIntegrity
		}
	}
	if r.Topics == nil {
		r.Topics = []string{}
	}
	if r.Data == nil {
		r.Data = []byte{}
	}
	b, e := json.Marshal(r)
	return bytesDigest(b), e
}
func ValidateArchive(payload []byte, m ArchiveManifest) ([]accounting.FillV1, error) {
	if m.Validate() != nil || int64(len(payload)) != m.ByteLength || bytesDigest(payload) != m.SHA256 {
		return nil, ErrIntegrity
	}
	var segment ArchiveSegment
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&segment); err != nil {
		return nil, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, ErrIntegrity
	}
	blocks, _ := json.Marshal(m.Blocks)
	actual, _ := json.Marshal(segment.Blocks)
	if segment.FormatVersion != "raw-replay-v2" || segment.ChainID != 125 || segment.SourceStream != m.SourceStream || segment.FromHeight != m.FromHeight || segment.ToHeight != m.ToHeight || len(segment.Records) != m.EventCount || len(segment.Events) != m.TypedCount || !bytes.Equal(blocks, actual) {
		return nil, ErrIntegrity
	}
	identities := map[string]string{}
	counts := map[int64]int{}
	hashes := map[int64]string{}
	for _, b := range m.Blocks {
		hashes[b.Height] = b.Hash
	}
	var previous *ArchiveRaw
	for i, r := range segment.Records {
		digest, err := archiveRawDigest(r)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", r.TransactionHash, r.LogIndex)
		if _, exists := identities[key]; exists {
			return nil, ErrIntegrity
		}
		if r.Height < m.FromHeight || r.Height > m.ToHeight || hashes[r.Height] != r.BlockHash {
			return nil, ErrIntegrity
		}
		if previous != nil && (r.Height < previous.Height || r.Height == previous.Height && (r.TransactionIndex < previous.TransactionIndex || r.TransactionIndex == previous.TransactionIndex && r.LogIndex <= previous.LogIndex)) {
			return nil, ErrIntegrity
		}
		identities[key] = digest
		counts[r.Height]++
		previous = &segment.Records[i]
	}
	for _, b := range m.Blocks {
		if counts[b.Height] != b.RawCount {
			return nil, ErrIntegrity
		}
	}
	fills := []accounting.FillV1{}
	events := map[string]bool{}
	for _, e := range segment.Events {
		digest, err := archiveRawDigest(e.Raw)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", e.Raw.TransactionHash, e.Raw.LogIndex)
		if e.RawDigest != digest || identities[key] != digest || events[key] || e.Timestamp <= 0 || len(e.Kind) > 128 || e.Kind == "" || len(e.Payload) > accounting.MaxWireBytes || !json.Valid(e.Payload) {
			return nil, ErrIntegrity
		}
		events[key] = true
		if e.Kind != accounting.MarketSubject {
			continue
		}
		f, err := accounting.DecodeFillV1(e.Payload)
		if err != nil {
			return nil, err
		}
		if f.Position.TransactionHash != e.Raw.TransactionHash || f.Position.BlockHash != e.Raw.BlockHash || f.Position.Height != strconv.FormatInt(e.Raw.Height, 10) || f.Position.TransactionIndex != strconv.FormatUint(uint64(e.Raw.TransactionIndex), 10) || f.Position.LogIndex != strconv.FormatUint(uint64(e.Raw.LogIndex), 10) || f.ActualEmitter != e.Raw.Emitter || f.BlockTimestampSeconds != strconv.FormatInt(e.Timestamp, 10) {
			return nil, ErrIntegrity
		}
		fills = append(fills, f)
	}
	return fills, nil
}
func (a ArchiveReader) Read(ctx context.Context, m ArchiveManifest) ([]accounting.FillV1, error) {
	if a.Objects == nil || a.Exact == nil || a.Bucket != m.Bucket || m.Validate() != nil {
		return nil, ErrIntegrity
	}
	body, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	manifestDigest := bytesDigest(body)
	var trusted bool
	err = a.Exact.Pool.QueryRow(ctx, `SELECT manifest=$2::jsonb AND manifest_digest=$1 AND verified_at IS NOT NULL FROM indexer.indexer_archive_segments_v2 WHERE object_key=$3`, manifestDigest, body, m.ObjectKey).Scan(&trusted)
	if err != nil {
		return nil, err
	}
	if !trusted {
		return nil, ErrIntegrity
	}
	object, err := a.Objects.Get(ctx, m.ObjectKey)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	if object.Size != m.ByteLength {
		return nil, ErrIntegrity
	}
	payload, err := io.ReadAll(io.LimitReader(object.Body, m.ByteLength+1))
	if err != nil {
		return nil, err
	}
	return ValidateArchive(payload, m)
}
func RebuildSchemaDDL(schema string) (string, error) {
	if !schemaName.MatchString(schema) {
		return "", errors.New("invalid_schema")
	}
	return fmt.Sprintf(`
CREATE TABLE %[1]s.chart_rebuild_runs(run_id text PRIMARY KEY,pool text NOT NULL,from_time bigint NOT NULL,to_time bigint NOT NULL,manifest_digest text NOT NULL,expected_digest text NOT NULL,output_digest text,status text NOT NULL CHECK(status IN('staging','validated','promoted')),base_revision bigint NOT NULL,promotion_epoch bigint,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),promoted_at timestamptz,operator_name text NOT NULL DEFAULT current_user);
CREATE TABLE %[1]s.chart_rebuild_candles(run_id text NOT NULL REFERENCES %[1]s.chart_rebuild_runs,interval text NOT NULL,start bigint NOT NULL,snapshot jsonb NOT NULL,PRIMARY KEY(run_id,interval,start));
CREATE TABLE %[1]s.chart_rebuild_traders(run_id text NOT NULL REFERENCES %[1]s.chart_rebuild_runs,interval text NOT NULL,start bigint NOT NULL,actor text NOT NULL,PRIMARY KEY(run_id,interval,start,actor));
CREATE TABLE %[1]s.chart_rebuild_fills(run_id text NOT NULL REFERENCES %[1]s.chart_rebuild_runs,source_id text NOT NULL,digest text NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(run_id,source_id));
CREATE TABLE %[1]s.chart_archive_checkpoints(pool text NOT NULL,from_time bigint NOT NULL,to_time bigint NOT NULL,manifest_digest text NOT NULL,verified_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(pool,from_time,to_time));
CREATE TABLE %[1]s.chart_replay_references(reference_id text PRIMARY KEY,pool text NOT NULL,from_time bigint NOT NULL,to_time bigint NOT NULL,expires_at timestamptz NOT NULL);
`, schema), nil
}

type StagedCandle struct {
	Interval string
	Start    int64
	Snapshot json.RawMessage
}
type StagedTrader struct {
	Interval string
	Start    int64
	Actor    string
}

func (s *ExactStore) StageRebuild(ctx context.Context, id, pool string, from, to int64, manifestDigest, expectedDigest, outputDigest string, baseRevision uint64, bars []StagedCandle, traders []StagedTrader, fills []accounting.FillV1) error {
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(id) || !archiveAddress.MatchString(pool) || from < 0 || to <= from || !archiveDigest.MatchString(manifestDigest) || !archiveDigest.MatchString(expectedDigest) || outputDigest != expectedDigest || len(bars) > 100000 || len(traders) > 1000000 || len(fills) > 1000000 {
		return ErrIntegrity
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_rebuild_runs(run_id,pool,from_time,to_time,manifest_digest,expected_digest,output_digest,status,base_revision) VALUES($1,$2,$3,$4,$5,$6,$7,'staging',$8)`, id, pool, from, to, manifestDigest, expectedDigest, outputDigest, baseRevision); err != nil {
		return err
	}
	for _, bar := range bars {
		if !json.Valid(bar.Snapshot) {
			return ErrIntegrity
		}
		if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_rebuild_candles VALUES($1,$2,$3,$4)`, id, bar.Interval, bar.Start, bar.Snapshot); err != nil {
			return err
		}
	}
	for _, t := range traders {
		if !archiveAddress.MatchString(t.Actor) {
			return ErrIntegrity
		}
		if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_rebuild_traders VALUES($1,$2,$3,$4)`, id, t.Interval, t.Start, t.Actor); err != nil {
			return err
		}
	}
	for _, f := range fills {
		digest, e := accounting.FillDigest(f)
		if e != nil {
			return e
		}
		payload, e := json.Marshal(f)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_rebuild_fills VALUES($1,$2,$3,$4)`, id, f.SourceFillID, digest, payload); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.Schema+`.chart_rebuild_runs SET status='validated' WHERE run_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *ExactStore) PromoteRebuild(ctx context.Context, id string) (uint64, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var pool, status, expected, output, manifest string
	var from, to, base, revision int64
	if err = tx.QueryRow(ctx, `SELECT pool,from_time,to_time,status,base_revision,expected_digest,output_digest,manifest_digest FROM `+s.Schema+`.chart_rebuild_runs WHERE run_id=$1 FOR UPDATE`, id).Scan(&pool, &from, &to, &status, &base, &expected, &output, &manifest); err != nil {
		return 0, err
	}
	if status != "validated" || expected != output {
		return 0, ErrIntegrity
	}
	rows, err := tx.Query(ctx, `SELECT payload FROM `+s.Schema+`.chart_rebuild_fills WHERE run_id=$1 ORDER BY source_id`, id)
	if err != nil {
		return 0, err
	}
	stagedFills := []accounting.FillV1{}
	for rows.Next() {
		var payload []byte
		var fill accounting.FillV1
		if err = rows.Scan(&payload); err != nil {
			rows.Close()
			return 0, err
		}
		if err = json.Unmarshal(payload, &fill); err != nil {
			rows.Close()
			return 0, err
		}
		stagedFills = append(stagedFills, fill)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for _, fill := range stagedFills {
		identity, _ := json.Marshal([]string{fill.Position.ChainID, fill.ActualEmitter, fill.Provenance.EmitterCodeEpoch, fill.Position.TransactionHash, fill.Position.LogIndex})
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(identity)); err != nil {
			return 0, err
		}
		payload, _ := json.Marshal(fill)
		var conflict bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM `+s.Schema+`.chart_inbox WHERE chain_id='125' AND emitter=$1 AND code_epoch=$2 AND tx_hash=$3 AND log_index=$4::numeric AND (pool<>$5 OR digest<>$6 OR fill<>$7::jsonb))`, fill.ActualEmitter, fill.Provenance.EmitterCodeEpoch, fill.Position.TransactionHash, fill.Position.LogIndex, pool, decodeDigest(bytesDigest(payload)), payload).Scan(&conflict)
		if err != nil {
			return 0, err
		}
		if conflict {
			return 0, ErrIntegrity
		}
	}
	var epoch int64
	if err = tx.QueryRow(ctx, `SELECT epoch FROM `+s.Schema+`.chart_pool_fences WHERE chain_id='125' AND pool=$1 AND NOT frozen FOR UPDATE`, pool).Scan(&epoch); err != nil {
		return 0, err
	}
	if err = tx.QueryRow(ctx, `SELECT revision FROM `+s.Schema+`.chart_series_clock WHERE chain_id='125' AND pool=$1 FOR UPDATE`, pool).Scan(&revision); err != nil {
		return 0, err
	}
	if revision != base || revision == int64(^uint64(0)>>1) {
		return 0, ErrStaleOwner
	}
	revision++
	if _, err = tx.Exec(ctx, `UPDATE `+s.Schema+`.chart_series_clock SET revision=$2 WHERE chain_id='125' AND pool=$1`, pool, revision); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.Schema+`.chart_rebuild_candles c SET snapshot=jsonb_set(jsonb_set(c.snapshot,'{revision}',to_jsonb(COALESCE((SELECT (e.snapshot->>'revision')::numeric FROM `+s.Schema+`.chart_exact e WHERE e.chain_id='125' AND e.pool=$2 AND e.interval=c.interval AND e.start=c.start),0)+1)),'{displayRevision}',to_jsonb($3::bigint)) WHERE c.run_id=$1`, id, pool, revision); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_exact SELECT '125',$2,interval,start,snapshot FROM `+s.Schema+`.chart_rebuild_candles WHERE run_id=$1 ON CONFLICT(chain_id,pool,interval,start) DO UPDATE SET snapshot=EXCLUDED.snapshot`, id, pool); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM `+s.Schema+`.chart_traders t USING `+s.Schema+`.chart_rebuild_candles c WHERE c.run_id=$1 AND t.chain_id='125' AND t.pool=$2 AND t.interval=c.interval AND t.start=c.start`, id, pool); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_traders SELECT '125',$2,interval,start,actor FROM `+s.Schema+`.chart_rebuild_traders WHERE run_id=$1`, id, pool); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_inbox(chain_id,emitter,code_epoch,tx_hash,log_index,pool,digest,fill,algorithm) SELECT '125',payload->>'actualEmitter',payload->'provenance'->>'emitterCodeEpoch',payload->'position'->>'transactionHash',(payload->'position'->>'logIndex')::numeric,$2,decode(digest,'hex'),payload,'qualified-exact-v2' FROM `+s.Schema+`.chart_rebuild_fills WHERE run_id=$1 ON CONFLICT DO NOTHING`, id, pool); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_outbox(chain_id,pool,interval,start,revision,payload) SELECT '125',$2,interval,start,(snapshot->>'revision')::numeric,snapshot FROM `+s.Schema+`.chart_rebuild_candles WHERE run_id=$1`, id, pool); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO `+s.Schema+`.chart_archive_checkpoints(pool,from_time,to_time,manifest_digest) VALUES($1,$2,$3,$4) ON CONFLICT(pool,from_time,to_time) DO UPDATE SET manifest_digest=EXCLUDED.manifest_digest,verified_at=clock_timestamp()`, pool, from, to, manifest); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE `+s.Schema+`.chart_rebuild_runs SET status='promoted',promotion_epoch=$2,promoted_at=clock_timestamp() WHERE run_id=$1`, id, revision); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return uint64(revision), nil
}

func decodeDigest(value string) []byte { raw, _ := hex.DecodeString(value); return raw }
