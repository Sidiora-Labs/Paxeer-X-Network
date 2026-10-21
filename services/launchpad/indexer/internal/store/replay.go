package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type ReplayRequest struct {
	JobID, SourceStream, Target, Generation, ProjectionRevision, Mode, Requester, AuthorizationID string
	FromHeight, ToHeight, CheckpointHeight                                                        int64
	CheckpointHash                                                                                string
}
type ReplayJob struct {
	Request       ReplayRequest
	Progress      int64
	Status, Owner string
	Fence         int64
}
type ReplayEvent struct {
	RawDigest, Kind string
	Timestamp       int64
	Payload         json.RawMessage
	Raw             RawRecord
}
type ReplaySegment struct {
	FormatVersion        string
	ChainID              int32
	SourceStream         string
	FromHeight, ToHeight int64
	Blocks               []BlockCompleteness
	Records              []RawRecord
	Events               []ReplayEvent
}

var replayToken = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

func (r ReplayRequest) Scope() OperationScope {
	return OperationScope{AuthorizationID: r.AuthorizationID, Operator: r.Requester, Action: r.Mode, Target: r.Target, Generation: r.Generation, Revision: r.ProjectionRevision, From: r.FromHeight, To: r.ToHeight}
}
func ValidateReplayRequest(r ReplayRequest) error {
	for _, v := range []string{r.JobID, r.SourceStream, r.Target, r.Generation, r.ProjectionRevision, r.Requester, r.AuthorizationID} {
		if !replayToken.MatchString(v) {
			return errors.New("invalid replay request identity")
		}
	}
	if r.FromHeight < 0 || r.ToHeight < r.FromHeight || r.ToHeight-r.FromHeight >= 1000000 || r.CheckpointHeight < r.ToHeight || !hashPattern.MatchString(r.CheckpointHash) || (r.Mode != "replay" && r.Mode != "backfill" && r.Mode != "reconstruct") {
		return errors.New("invalid replay bounds, mode or source checkpoint")
	}
	return nil
}
func (s *Store) AdmitReplay(ctx context.Context, r ReplayRequest) error {
	if err := ValidateReplayRequest(r); err != nil {
		return err
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("replay requires transaction capable store")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, r.Scope()); err != nil {
		return err
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	digest := delivery.Hash(body)
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_jobs(job_id,request_digest,request,chain_id,source_stream,target,generation,projection_revision,mode,authorization_id,from_height,to_height,progress_height,status) VALUES($1,$2,$3,125,$4,$5,$6,$7,$8,$9,$10,$11,$10-1,'queued') ON CONFLICT DO NOTHING`, r.JobID, digest, body, r.SourceStream, r.Target, r.Generation, r.ProjectionRevision, r.Mode, r.AuthorizationID, r.FromHeight, r.ToHeight)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT request_digest=$2 AND request=$3::jsonb FROM indexer.indexer_replay_jobs WHERE job_id=$1`, r.JobID, digest, body).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return ErrIntegrity
	}
	return tx.Commit(ctx)
}
func (s *Store) AcquireReplay(ctx context.Context, id, owner string, ttl time.Duration) (ReplayJob, error) {
	var j ReplayJob
	if !replayToken.MatchString(id) || !replayToken.MatchString(owner) || ttl < time.Second || ttl > 30*time.Second {
		return j, errors.New("bounded replay lease required")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return j, errors.New("replay requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return j, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var body []byte
	var alive bool
	err = tx.QueryRow(ctx, `SELECT request,progress_height,status,COALESCE(owner,''),fence,COALESCE(expires_at>clock_timestamp(),false) FROM indexer.indexer_replay_jobs WHERE job_id=$1 FOR UPDATE`, id).Scan(&body, &j.Progress, &j.Status, &j.Owner, &j.Fence, &alive)
	if err != nil {
		return j, err
	}
	if err = json.Unmarshal(body, &j.Request); err != nil {
		return j, err
	}
	if j.Status == "completed" {
		return j, ErrReplayComplete
	}
	if (j.Status != "queued" && j.Status != "running") || alive && j.Owner != owner {
		return j, ErrFenced
	}
	quota, err := authorizeOperation(ctx, tx, j.Request.Scope())
	if err != nil {
		return j, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, j.Request.Target+"/"+j.Request.Generation+"/"+j.Request.ProjectionRevision); err != nil {
		return j, err
	}
	var running, overlap int
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE from_height<=$5 AND to_height>=$4) FROM indexer.indexer_replay_jobs WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND job_id<>$6 AND status='running' AND expires_at>clock_timestamp()`, j.Request.Target, j.Request.Generation, j.Request.ProjectionRevision, j.Request.FromHeight, j.Request.ToHeight, id).Scan(&running, &overlap)
	if err != nil {
		return j, err
	}
	if running >= quota || overlap > 0 {
		return j, errors.New("replay target quota or overlapping active interval")
	}
	err = tx.QueryRow(ctx, `UPDATE indexer.indexer_replay_jobs SET status='running',owner=$2,fence=CASE WHEN owner=$2 AND expires_at>clock_timestamp() THEN fence ELSE fence+1 END,expires_at=clock_timestamp()+$3::bigint*interval '1 millisecond',last_error=NULL,unavailable_from=NULL,unavailable_to=NULL,updated_at=clock_timestamp() WHERE job_id=$1 RETURNING fence`, id, owner, ttl.Milliseconds()).Scan(&j.Fence)
	if err != nil {
		return j, err
	}
	j.Owner = owner
	j.Status = "running"
	return j, tx.Commit(ctx)
}
func ValidateReplaySegment(segment ReplaySegment) error {
	if segment.FormatVersion != "raw-replay-v2" || segment.ChainID != 125 || segment.SourceStream == "" || segment.FromHeight < 0 || segment.ToHeight < segment.FromHeight || segment.ToHeight-segment.FromHeight >= 1000 || len(segment.Blocks) != int(segment.ToHeight-segment.FromHeight+1) || len(segment.Records) > 65536 || len(segment.Events) > 65536 {
		return errors.New("invalid bounded replay segment")
	}
	counts := map[int64]int{}
	identities := map[string]string{}
	var previous *RawRecord
	bytes := 0
	for i := range segment.Records {
		r := segment.Records[i]
		digest, err := RawDigest(r)
		if err != nil {
			return err
		}
		if r.Height < segment.FromHeight || r.Height > segment.ToHeight {
			return ErrIntegrity
		}
		key := fmt.Sprintf("%s:%d", r.TransactionHash, r.LogIndex)
		if _, ok := identities[key]; ok {
			return ErrIntegrity
		}
		identities[key] = digest
		counts[r.Height]++
		bytes += len(r.Data) + len(r.Topics)*66 + 256
		if previous != nil && (r.Height < previous.Height || r.Height == previous.Height && (r.TransactionIndex < previous.TransactionIndex || r.TransactionIndex == previous.TransactionIndex && r.LogIndex <= previous.LogIndex)) {
			return ErrIntegrity
		}
		previous = &segment.Records[i]
	}
	blocks := map[int64]string{}
	for i, b := range segment.Blocks {
		if b.Height != segment.FromHeight+int64(i) || !hashPattern.MatchString(b.Hash) || !b.FetchComplete || !b.RequiredDecodeComplete || b.RawCount != counts[b.Height] {
			return errors.New("incomplete replay source block")
		}
		blocks[b.Height] = b.Hash
	}
	for _, r := range segment.Records {
		if blocks[r.Height] != r.BlockHash {
			return ErrIntegrity
		}
	}
	events := map[string]bool{}
	for _, event := range segment.Events {
		digest, err := RawDigest(event.Raw)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%s:%d", event.Raw.TransactionHash, event.Raw.LogIndex)
		if digest != event.RawDigest || identities[key] != digest || event.Timestamp <= 0 || event.Kind == "" || len(event.Kind) > 128 || !json.Valid(event.Payload) || len(event.Payload) > 1<<20 || events[key] {
			return errors.New("replay typed event is not bound to original raw input")
		}
		events[key] = true
		bytes += len(event.Payload)
	}
	if bytes > 32<<20 {
		return errors.New("replay segment exceeds byte bound")
	}
	return nil
}
func (s *Store) CommitReplaySegment(ctx context.Context, j ReplayJob, segment ReplaySegment, project func(context.Context, pgx.Tx, ReplayEvent) (string, error)) error {
	if err := ValidateReplaySegment(segment); err != nil {
		return err
	}
	if project == nil || segment.SourceStream != j.Request.SourceStream || segment.FromHeight != j.Progress+1 || segment.ToHeight > j.Request.ToHeight {
		return ErrWatermark
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("replay requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var locked bool
	err = tx.QueryRow(ctx, `SELECT status='running' AND owner=$2 AND fence=$3 AND expires_at>clock_timestamp() AND progress_height=$4 FROM indexer.indexer_replay_jobs WHERE job_id=$1 FOR UPDATE`, j.Request.JobID, j.Owner, j.Fence, j.Progress).Scan(&locked)
	if err != nil {
		return err
	}
	if !locked {
		return ErrFenced
	}
	if _, err = authorizeOperation(ctx, tx, j.Request.Scope()); err != nil {
		return err
	}
	if err = checkPoisonTx(ctx, tx, 125, j.Request.SourceStream, segment.FromHeight, segment.ToHeight); err != nil {
		return err
	}
	var sourceHash string
	err = tx.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_replay_checkpoints WHERE source_stream=$1 AND height=$2`, j.Request.SourceStream, j.Request.CheckpointHeight).Scan(&sourceHash)
	if err != nil {
		return err
	}
	if sourceHash != j.Request.CheckpointHash {
		return ErrIntegrity
	}
	for _, block := range segment.Blocks {
		var known string
		err = tx.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2`, segment.SourceStream, block.Height).Scan(&known)
		if err == nil && known != block.Hash {
			return ErrIntegrity
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_source_blocks(source_stream,height,block_hash,raw_count) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, segment.SourceStream, block.Height, block.Hash, block.RawCount); err != nil {
			return err
		}
		var same bool
		err = tx.QueryRow(ctx, `SELECT block_hash=$3 AND raw_count=$4 FROM indexer.indexer_replay_source_blocks WHERE source_stream=$1 AND height=$2`, segment.SourceStream, block.Height, block.Hash, block.RawCount).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrIntegrity
		}
	}
	identities := make([]string, 0, len(segment.Records))
	for _, r := range segment.Records {
		if err = stageImmutableRaw(ctx, tx, 125, r); err != nil {
			return err
		}
		digest, _ := RawDigest(r)
		identities = append(identities, digest)
	}
	for _, event := range segment.Events {
		effect, err := project(ctx, tx, event)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_effects(target,generation,projection_revision,effect_id,raw_digest,height,event_kind,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, j.Request.Target, j.Request.Generation, j.Request.ProjectionRevision, effect, event.RawDigest, event.Raw.Height, event.Kind, event.Payload)
		if err != nil {
			return err
		}
		var same bool
		err = tx.QueryRow(ctx, `SELECT raw_digest=$5 AND height=$6 AND event_kind=$7 AND payload=$8::jsonb FROM indexer.indexer_replay_effects WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND effect_id=$4`, j.Request.Target, j.Request.Generation, j.Request.ProjectionRevision, effect, event.RawDigest, event.Raw.Height, event.Kind, event.Payload).Scan(&same)
		if err != nil {
			return err
		}
		if !same {
			return ErrIntegrity
		}
	}
	identityWire, _ := json.Marshal(identities)
	checkpointWire, _ := json.Marshal(segment.Blocks)
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_batches(job_id,from_height,to_height,raw_count,intent_count,identity_digest,checkpoint_digest) VALUES($1,$2,$3,$4,$5,$6,$7)`, j.Request.JobID, segment.FromHeight, segment.ToHeight, len(segment.Records), len(segment.Events), delivery.Hash(identityWire), delivery.Hash(checkpointWire))
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE indexer.indexer_replay_jobs SET progress_height=$4,status=CASE WHEN $4=to_height THEN 'completed' ELSE 'running' END,updated_at=clock_timestamp() WHERE job_id=$1 AND owner=$2 AND fence=$3 AND status='running' AND expires_at>clock_timestamp()`, j.Request.JobID, j.Owner, j.Fence, segment.ToHeight)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	return tx.Commit(ctx)
}
func (s *Store) TransitionReplay(ctx context.Context, scope OperationScope, jobID, state, reason string, unavailableFrom, unavailableTo *int64) error {
	if state != "paused" && state != "cancelled" && state != "failed" {
		return errors.New("invalid replay transition")
	}
	if len(reason) > 2048 {
		return errors.New("replay error bound exceeded")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("replay requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, scope); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE indexer.indexer_replay_jobs SET status=$2,last_error=$3,unavailable_from=$4,unavailable_to=$5,expires_at=clock_timestamp(),updated_at=clock_timestamp() WHERE job_id=$1 AND target=$6 AND generation=$7 AND projection_revision=$8 AND authorization_id=$9 AND status<>'completed'`, jobID, state, reason, unavailableFrom, unavailableTo, scope.Target, scope.Generation, scope.Revision, scope.AuthorizationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	return tx.Commit(ctx)
}

var ErrReplayComplete = errors.New("replay already completed")

func (s *Store) BindReplayCheckpoint(ctx context.Context, request ReplayRequest, checkpoint BlockCompleteness) error {
	if checkpoint.Height != request.CheckpointHeight || checkpoint.Hash != request.CheckpointHash || !checkpoint.FetchComplete || !checkpoint.RequiredDecodeComplete {
		return ErrIntegrity
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("checkpoint requires guarded transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, request.Scope()); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_checkpoints(source_stream,height,block_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, request.SourceStream, checkpoint.Height, checkpoint.Hash)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT block_hash=$3 FROM indexer.indexer_replay_checkpoints WHERE source_stream=$1 AND height=$2`, request.SourceStream, checkpoint.Height, checkpoint.Hash).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return ErrIntegrity
	}
	return tx.Commit(ctx)
}

func (s *Store) RecordReplayConflict(ctx context.Context, request ReplayRequest, segment ReplaySegment, reason string) error {
	if err := ValidateReplayRequest(request); err != nil {
		return err
	}
	if len(reason) > 2048 {
		return errors.New("replay conflict evidence exceeds bound")
	}
	body, err := json.Marshal(segment)
	if err != nil {
		return err
	}
	if len(body) > 64<<20 {
		return errors.New("conflict evidence exceeds byte bound")
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("conflict requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, request.Scope()); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO indexer.indexer_replay_checkpoint_conflicts(job_id,from_height,to_height,expected_checkpoint,observed_digest,observed,reason) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, request.JobID, segment.FromHeight, segment.ToHeight, request.CheckpointHash, delivery.Hash(body), body, reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) ResumeReplay(ctx context.Context, scope OperationScope, jobID string) error {
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("resume requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, scope); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE indexer.indexer_replay_jobs SET status='queued',last_error=NULL,unavailable_from=NULL,unavailable_to=NULL,expires_at=clock_timestamp(),updated_at=clock_timestamp() WHERE job_id=$1 AND target=$2 AND generation=$3 AND projection_revision=$4 AND authorization_id=$5 AND status IN('paused','cancelled','failed')`, jobID, scope.Target, scope.Generation, scope.Revision, scope.AuthorizationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	return tx.Commit(ctx)
}

func (s *Store) PauseReplayLease(ctx context.Context, job ReplayJob, reason string, from, to int64) error {
	if job.Owner == "" || job.Fence <= 0 || len(reason) > 2048 || from < 0 || to < from {
		return ErrFenced
	}
	db, ok := s.db.(beginner)
	if !ok {
		return errors.New("replay pause requires transaction")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = authorizeOperation(ctx, tx, job.Request.Scope()); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE indexer.indexer_replay_jobs SET status='paused',last_error=$5,unavailable_from=$6,unavailable_to=$7,expires_at=clock_timestamp(),updated_at=clock_timestamp() WHERE job_id=$1 AND owner=$2 AND fence=$3 AND progress_height=$4 AND status='running' AND expires_at>clock_timestamp()`, job.Request.JobID, job.Owner, job.Fence, job.Progress, reason, from, to)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrFenced
	}
	return tx.Commit(ctx)
}
