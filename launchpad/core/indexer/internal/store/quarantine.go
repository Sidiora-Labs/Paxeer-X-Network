package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

var ErrRequiredPoison = errors.New("required input awaits source repair or scoped waiver")

type PoisonRecord struct {
	ChainID                                        int32
	StreamID                                       string
	Raw                                            RawRecord
	RawDigest, ErrorCode, ABIRevision, Requirement string
	Required                                       bool
}

type OperationScope struct {
	AuthorizationID, Operator, Action, Target, Generation, Revision string
	From, To                                                        int64
}

func RawDigest(raw RawRecord) (string, error) {
	if raw.Height < 0 || !hashPattern.MatchString(raw.TransactionHash) || !hashPattern.MatchString(raw.BlockHash) || !addressPattern.MatchString(raw.Emitter) || len(raw.Topics) > 4 || len(raw.Data) > 1<<20 {
		return "", errors.New("invalid bounded raw identity")
	}
	for _, topic := range raw.Topics {
		if !hashPattern.MatchString(topic) {
			return "", errors.New("invalid raw topic")
		}
	}
	if raw.Topics == nil {
		raw.Topics = []string{}
	}
	if raw.Data == nil {
		raw.Data = []byte{}
	}
	wire, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return delivery.Hash(wire), nil
}

func stageImmutableRaw(ctx context.Context, tx pgx.Tx, chain int32, r RawRecord) error {
	if chain != 125 {
		return ErrIntegrity
	}
	if _, err := RawDigest(r); err != nil {
		return err
	}
	topics := r.Topics
	if topics == nil {
		topics = []string{}
	}
	wire, err := json.Marshal(topics)
	if err != nil {
		return err
	}
	data := r.Data
	if data == nil {
		data = []byte{}
	}
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, chain, r.TransactionHash, int64(r.LogIndex), r.Height, int64(r.TransactionIndex), r.BlockHash, r.Emitter, wire, data)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT height=$4 AND transaction_index=$5 AND block_hash=$6 AND emitter=$7 AND topics=$8::jsonb AND data=$9 FROM indexer.indexer_raw_logs WHERE chain_id=$1 AND transaction_hash=$2 AND log_index=$3`, chain, r.TransactionHash, int64(r.LogIndex), r.Height, int64(r.TransactionIndex), r.BlockHash, r.Emitter, wire, data).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return ErrIntegrity
	}
	return nil
}

func (s *Store) RetainPoison(ctx context.Context, p PoisonRecord) error {
	if p.ChainID != 125 || p.StreamID == "" || len(p.StreamID) > 128 || p.ErrorCode == "" || len(p.ErrorCode) > 128 || p.ABIRevision == "" || len(p.ABIRevision) > 128 || p.Requirement == "" || len(p.Requirement) > 128 {
		return errors.New("complete quarantine classification required")
	}
	digest, err := RawDigest(p.Raw)
	if err != nil {
		return err
	}
	if p.RawDigest != "" && p.RawDigest != digest {
		return ErrIntegrity
	}
	p.RawDigest = digest
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("quarantine requires durable transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = stageImmutableRaw(ctx, tx, p.ChainID, p.Raw); err != nil {
		return err
	}
	payload, err := json.Marshal(p.Raw)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `INSERT INTO indexer.indexer_quarantine(chain_id,stream_id,transaction_hash,log_index,block_hash,height,raw_digest,raw_payload,error_code,abi_revision,requirement,required,attempt_count,next_attempt_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,1,clock_timestamp()+interval '1 second') ON CONFLICT(chain_id,stream_id,transaction_hash,log_index) DO UPDATE SET last_attempt_at=clock_timestamp(),attempt_count=LEAST(indexer_quarantine.attempt_count+1,10),next_attempt_at=clock_timestamp()+(LEAST(3600,power(2,LEAST(indexer_quarantine.attempt_count,10)))::integer*interval '1 second'),operator_required=indexer_quarantine.attempt_count>=9 WHERE indexer_quarantine.raw_digest=EXCLUDED.raw_digest AND indexer_quarantine.block_hash=EXCLUDED.block_hash AND indexer_quarantine.error_code=EXCLUDED.error_code AND indexer_quarantine.abi_revision=EXCLUDED.abi_revision AND indexer_quarantine.requirement=EXCLUDED.requirement AND indexer_quarantine.required=EXCLUDED.required AND indexer_quarantine.resolution='unresolved' RETURNING true`, p.ChainID, p.StreamID, p.Raw.TransactionHash, int64(p.Raw.LogIndex), p.Raw.BlockHash, p.Raw.Height, digest, payload, p.ErrorCode, p.ABIRevision, p.Requirement, p.Required).Scan(&same)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrIntegrity
	}
	if err != nil {
		return err
	}
	if !p.Required {
		_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_capability_degradation(chain_id,stream_id,transaction_hash,log_index,requirement,raw_digest) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, p.ChainID, p.StreamID, p.Raw.TransactionHash, int64(p.Raw.LogIndex), p.Requirement, digest)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func authorizeOperation(ctx context.Context, tx pgx.Tx, scope OperationScope) (int, error) {
	if scope.AuthorizationID == "" || scope.Operator == "" || scope.From < 0 || scope.To < scope.From || scope.To-scope.From >= 1000000 {
		return 0, errors.New("bounded authenticated operation required")
	}
	var quota int
	err := tx.QueryRow(ctx, `SELECT max_concurrent FROM indexer.indexer_replay_authorizations WHERE authorization_id=$1 AND operator_identity=$2 AND action=$3 AND target=$4 AND generation=$5 AND projection_revision=$6 AND from_height<=$7 AND to_height>=$8 AND expires_at>clock_timestamp() AND revoked_at IS NULL AND authorization_evidence ~ '^[0-9a-f]{64}$' FOR SHARE`, scope.AuthorizationID, scope.Operator, scope.Action, scope.Target, scope.Generation, scope.Revision, scope.From, scope.To).Scan(&quota)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errors.New("operational authorization absent, expired or out of scope")
	}
	return quota, err
}

func (s *Store) WaivePoison(ctx context.Context, scope OperationScope, rawDigest, requirement, reason string, expires time.Time) error {
	if scope.Action != "waive" || len(rawDigest) != 64 || requirement == "" || len(reason) < 1 || len(reason) > 2048 || expires.IsZero() {
		return errors.New("complete scoped waiver required")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("waiver requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, scope); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO indexer.indexer_quarantine_waivers(raw_digest,requirement,operator_identity,authorization_id,reason,from_height,to_height,expires_at) SELECT $1,$2,$3,$4,$5,$6,$7,$8 WHERE $8>clock_timestamp() AND $8<=(SELECT expires_at FROM indexer.indexer_replay_authorizations WHERE authorization_id=$4) AND EXISTS(SELECT 1 FROM indexer.indexer_quarantine WHERE raw_digest=$1 AND requirement=$2 AND height BETWEEN $6 AND $7 AND resolution='unresolved') ON CONFLICT DO NOTHING`, rawDigest, requirement, scope.Operator, scope.AuthorizationID, reason, scope.From, scope.To, expires)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("waiver scope or expiry invalid")
	}
	return tx.Commit(ctx)
}

func checkPoisonTx(ctx context.Context, tx pgx.Tx, chain int32, stream string, from, to int64) error {
	var count int64
	err := tx.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_quarantine q WHERE chain_id=$1 AND stream_id=$2 AND height BETWEEN $3 AND $4 AND required AND resolution='unresolved' AND NOT EXISTS(SELECT 1 FROM indexer.indexer_quarantine_waivers w JOIN indexer.indexer_replay_authorizations a ON a.authorization_id=w.authorization_id WHERE w.raw_digest=q.raw_digest AND w.requirement=q.requirement AND q.height BETWEEN w.from_height AND w.to_height AND w.expires_at>clock_timestamp() AND a.expires_at>clock_timestamp() AND a.revoked_at IS NULL)`, chain, stream, from, to).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: %d inputs", ErrRequiredPoison, count)
	}
	return nil
}

func (s *Store) ResolvePoison(ctx context.Context, scope OperationScope, digest, blockHash string) error {
	if scope.Action != "repair" || len(digest) != 64 || !hashPattern.MatchString(blockHash) {
		return ErrIntegrity
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("repair requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, scope); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE indexer.indexer_quarantine q SET resolution='repaired',resolved_at=clock_timestamp(),resolved_by=$3 WHERE raw_digest=$1 AND block_hash=$2 AND height BETWEEN $4 AND $5 AND resolution='unresolved' AND EXISTS(SELECT 1 FROM indexer.indexer_block_completeness b WHERE b.chain_id=q.chain_id AND b.stream_id=q.stream_id AND b.height=q.height AND b.block_hash=q.block_hash AND b.fetch_complete AND b.required_decode_complete)`, digest, blockHash, scope.Operator, scope.From, scope.To)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("source-verified repair not yet committed")
	}
	return tx.Commit(ctx)
}

func (s *Store) PoisonRetryDue(ctx context.Context, chain int32, stream, transaction string, index uint32) (bool, error) {
	var due bool
	err := s.db.QueryRow(ctx, `SELECT attempt_count<10 AND next_attempt_at<=clock_timestamp() AND NOT operator_required AND resolution='unresolved' FROM indexer.indexer_quarantine WHERE chain_id=$1 AND stream_id=$2 AND transaction_hash=$3 AND log_index=$4`, chain, stream, transaction, int64(index)).Scan(&due)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	return due, err
}

func (c CanonicalTx) ResolveSourceQualifiedPoisonTx(ctx context.Context, b CanonicalBatch) error {
	if c.Tx == nil {
		return errors.New("source repair requires actual canonical transaction")
	}
	for _, raw := range b.Records {
		digest, err := RawDigest(raw)
		if err != nil {
			return err
		}
		_, err = c.Tx.Exec(ctx, `UPDATE indexer.indexer_quarantine SET resolution='repaired',resolved_at=clock_timestamp(),resolved_by='source-qualified-receipt' WHERE chain_id=$1 AND stream_id=$2 AND transaction_hash=$3 AND log_index=$4 AND block_hash=$5 AND raw_digest=$6 AND resolution='unresolved'`, b.ChainID, b.StreamID, raw.TransactionHash, int64(raw.LogIndex), raw.BlockHash, digest)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RetainCommittedHead(ctx context.Context, stream string, height int64, hash, sourceID string) error {
	if !s.LifecycleEnabled() || stream == "" || len(stream) > 128 || height < 0 || !hashPattern.MatchString(hash) || sourceID == "" || len(sourceID) > 128 {
		return errors.New("actual guarded committed head identity required")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("committed head requires guarded transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var healthy bool
	err = tx.QueryRow(ctx, `INSERT INTO indexer.indexer_committed_heads(chain_id,stream_id,height,block_hash,integrity,source_id) VALUES(125,$1,$2,$3,'healthy',$4) ON CONFLICT(chain_id,stream_id,height) DO UPDATE SET integrity=CASE WHEN indexer_committed_heads.block_hash=EXCLUDED.block_hash THEN indexer_committed_heads.integrity ELSE 'quarantined' END,observed_at=clock_timestamp() RETURNING integrity='healthy'`, stream, height, hash, sourceID).Scan(&healthy)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if !healthy {
		return ErrIntegrity
	}
	return nil
}
func (s *Store) QuarantineCommittedHead(ctx context.Context, stream string) error {
	if !s.LifecycleEnabled() || stream == "" {
		return errors.New("guarded committed head scope required")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("committed head quarantine requires guarded transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	_, err = tx.Exec(ctx, `UPDATE indexer.indexer_committed_heads SET integrity='quarantined',observed_at=clock_timestamp() WHERE chain_id=125 AND stream_id=$1 AND height=(SELECT max(height) FROM indexer.indexer_committed_heads WHERE chain_id=125 AND stream_id=$1)`, stream)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CheckOperationAuthorization(ctx context.Context, scope OperationScope) error {
	if s == nil {
		return errors.New("operation store unavailable")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("operation authorization requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, scope); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
