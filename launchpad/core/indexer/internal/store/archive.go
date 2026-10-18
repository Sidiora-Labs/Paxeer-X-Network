package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type ArchiveManifest struct {
	FormatVersion string              `json:"formatVersion"`
	ChainID       int32               `json:"chainId"`
	SourceStream  string              `json:"sourceStream"`
	FromHeight    int64               `json:"fromHeight"`
	ToHeight      int64               `json:"toHeight"`
	EventCount    int                 `json:"eventCount"`
	TypedCount    int                 `json:"typedCount"`
	ByteLength    int64               `json:"byteLength"`
	SHA256        string              `json:"sha256"`
	Bucket        string              `json:"bucket"`
	ObjectKey     string              `json:"objectKey"`
	Blocks        []BlockCompleteness `json:"blockCheckpoints"`
}

func (m ArchiveManifest) Validate() error {
	if m.FormatVersion != "raw-archive-v2" || m.ChainID != 125 || !replayToken.MatchString(m.SourceStream) || m.FromHeight < 0 || m.ToHeight < m.FromHeight || m.ToHeight-m.FromHeight >= 1000 || m.EventCount < 0 || m.EventCount > 65536 || m.TypedCount < 0 || m.TypedCount > 65536 || m.ByteLength < 1 || m.ByteLength > 64<<20 || len(m.SHA256) != 64 || m.Bucket == "" || m.ObjectKey != fmt.Sprintf("indexer/raw/v2/%s/%s.json", m.SourceStream, m.SHA256) || len(m.Blocks) != int(m.ToHeight-m.FromHeight+1) {
		return errors.New("invalid bounded archive manifest")
	}
	for i, b := range m.Blocks {
		if b.Height != m.FromHeight+int64(i) || !hashPattern.MatchString(b.Hash) || !b.FetchComplete || !b.RequiredDecodeComplete {
			return ErrIntegrity
		}
	}
	return nil
}
func verifyArchiveSourceTx(ctx context.Context, tx pgx.Tx, segment ReplaySegment) error {
	for _, b := range segment.Blocks {
		var same bool
		err := tx.QueryRow(ctx, `SELECT block_hash=$3 AND raw_count=$4 AND fetch_complete AND required_decode_complete FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2`, segment.SourceStream, b.Height, b.Hash, b.RawCount).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrIntegrity
		}
	}
	for _, r := range segment.Records {
		var same bool
		topics := r.Topics
		if topics == nil {
			topics = []string{}
		}
		payload, _ := json.Marshal(topics)
		data := r.Data
		if data == nil {
			data = []byte{}
		}
		err := tx.QueryRow(ctx, `SELECT block_hash=$3 AND height=$4 AND transaction_index=$5 AND emitter=$6 AND topics=$7::jsonb AND data=$8 FROM indexer.indexer_raw_logs WHERE chain_id=125 AND transaction_hash=$1 AND log_index=$2`, r.TransactionHash, int64(r.LogIndex), r.BlockHash, r.Height, int64(r.TransactionIndex), r.Emitter, payload, data).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrIntegrity
		}
	}
	return nil
}
func (s *Store) CheckArchiveSource(ctx context.Context, segment ReplaySegment) error {
	if err := ValidateReplaySegment(segment); err != nil {
		return err
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("archive source requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	return verifyArchiveSourceTx(ctx, tx, segment)
}
func (s *Store) CompleteArchive(ctx context.Context, m ArchiveManifest, segment ReplaySegment) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := ValidateReplaySegment(segment); err != nil {
		return err
	}
	wire, err := json.Marshal(segment)
	if err != nil {
		return err
	}
	if delivery.Hash(wire) != m.SHA256 || int64(len(wire)) != m.ByteLength || len(segment.Records) != m.EventCount || len(segment.Events) != m.TypedCount {
		return ErrIntegrity
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	id := delivery.Hash(body)
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("archive completion requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = verifyArchiveSourceTx(ctx, tx, segment); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_archive_segments(segment_id,chain_id,source_stream,from_height,to_height,manifest,manifest_digest,object_key) VALUES($1,125,$2,$3,$4,$5,$1,$6) ON CONFLICT DO NOTHING`, id, m.SourceStream, m.FromHeight, m.ToHeight, body, m.ObjectKey)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT manifest=$2::jsonb AND manifest_digest=$1 FROM indexer.indexer_archive_segments WHERE object_key=$3`, id, body, m.ObjectKey).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return ErrIntegrity
	}
	return tx.Commit(ctx)
}
func (s *Store) FindArchive(ctx context.Context, stream string, from, to int64) (ArchiveManifest, error) {
	var manifest ArchiveManifest
	var body []byte
	err := s.db.QueryRow(ctx, `SELECT manifest FROM indexer.indexer_archive_segments WHERE source_stream=$1 AND from_height<=$2 AND to_height>=$3 ORDER BY from_height DESC LIMIT 1`, stream, from, to).Scan(&body)
	if err != nil {
		return manifest, err
	}
	if err = json.Unmarshal(body, &manifest); err != nil {
		return manifest, err
	}
	return manifest, manifest.Validate()
}
func (s *Store) RetentionEligible(ctx context.Context, scope OperationScope) (bool, error) {
	if scope.Action != "retention" {
		return false, errors.New("explicit retention authorization required")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return false, errors.New("retention requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, scope); err != nil {
		return false, err
	}
	var covered bool
	err = tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM generate_series($2::bigint,$3::bigint) h WHERE NOT EXISTS(SELECT 1 FROM indexer.indexer_archive_segments a WHERE a.source_stream=$1 AND h BETWEEN a.from_height AND a.to_height AND verified_at IS NOT NULL))`, scope.Target, scope.From, scope.To).Scan(&covered)
	return covered, err
}
