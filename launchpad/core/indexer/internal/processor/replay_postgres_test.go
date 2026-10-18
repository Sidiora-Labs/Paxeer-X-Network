package processor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store/canonicaltest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func replayPostgres(t *testing.T) (context.Context, string, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	dsn, pool := canonicaltest.New(t)
	_, file, _, _ := runtime.Caller(0)
	ddl, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../spec/production-system/handoffs/L06/phase2/ingestion.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	return ctx, dsn, pool
}
func TestPoisonDurableAcrossFreshPool(t *testing.T) {
	ctx, dsn, pool := replayPostgres(t)
	st := store.New(pool)
	raw := store.RawRecord{TransactionHash: "0x" + strings.Repeat("1", 64), BlockHash: "0x" + strings.Repeat("2", 64), Emitter: "0x" + strings.Repeat("3", 40), Height: 101, Data: []byte{0xff}, Topics: []string{}}
	poison := store.PoisonRecord{ChainID: 125, StreamID: "canonical", Raw: raw, ErrorCode: "source_repair_required", ABIRevision: "revision1", Requirement: "D02_021", Required: true}
	if err := st.RetainPoison(ctx, poison); err != nil {
		t.Fatal(err)
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	var retained bool
	if err = fresh.QueryRow(ctx, `SELECT r.data=$1 FROM indexer.indexer_quarantine q JOIN indexer.indexer_raw_logs r USING(chain_id,transaction_hash,log_index) WHERE q.error_code='source_repair_required' AND q.required AND q.resolution='unresolved'`, raw.Data).Scan(&retained); err != nil || !retained {
		t.Fatalf("poison not durable: %v", err)
	}
	poison.Raw.Data = []byte{0}
	if err = st.RetainPoison(ctx, poison); !errors.Is(err, store.ErrIntegrity) {
		t.Fatalf("changed immutable input accepted: %v", err)
	}
}
func TestReplayCheckpointAtomicAndLiveCursorIndependent(t *testing.T) {
	ctx, _, pool := replayPostgres(t)
	st := store.New(pool)
	request := store.ReplayRequest{JobID: "historical1", SourceStream: "canonical", Target: "charts", Generation: "rebuild1", ProjectionRevision: "revision2", Mode: "backfill", Requester: "operator", AuthorizationID: "auth1", FromHeight: 101, ToHeight: 101, CheckpointHeight: 101, CheckpointHash: "0x" + strings.Repeat("2", 64)}
	_, err := pool.Exec(ctx, `INSERT INTO indexer.indexer_replay_authorizations VALUES('auth1','operator',$1,'backfill','charts','rebuild1','revision2',101,101,1,clock_timestamp()+interval '1 hour',NULL)`, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err = st.UpsertCursor(ctx, 125, 200); err != nil {
		t.Fatal(err)
	}
	if err = st.AdmitReplay(ctx, request); err != nil {
		t.Fatal(err)
	}
	checkpoint := store.BlockCompleteness{Height: 101, Hash: request.CheckpointHash, FetchComplete: true, RequiredDecodeComplete: true}
	if err = st.BindReplayCheckpoint(ctx, request, checkpoint); err != nil {
		t.Fatal(err)
	}
	job, err := st.AcquireReplay(ctx, request.JobID, "worker", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	segment := store.ReplaySegment{FormatVersion: "raw-replay-v2", ChainID: 125, SourceStream: "canonical", FromHeight: 101, ToHeight: 101, Blocks: []store.BlockCompleteness{checkpoint}, Records: []store.RawRecord{}, Events: []store.ReplayEvent{}}
	if err = st.CommitReplaySegment(ctx, job, segment, func(context.Context, pgx.Tx, store.ReplayEvent) (string, error) {
		return "", errors.New("empty block cannot call event projection")
	}); err != nil {
		t.Fatal(err)
	}
	cursor, err := st.GetCursor(ctx, 125)
	if err != nil || cursor == nil || *cursor != 200 {
		t.Fatalf("live cursor changed: %v %v", cursor, err)
	}
	var progress int64
	var state string
	if err = pool.QueryRow(ctx, `SELECT progress_height,status FROM indexer.indexer_replay_jobs WHERE job_id=$1`, request.JobID).Scan(&progress, &state); err != nil || progress != 101 || state != "completed" {
		t.Fatalf("independent job incomplete: %d %s %v", progress, state, err)
	}
}

func sourceRegistry(t *testing.T, emitter common.Address) *decode.Registry {
	t.Helper()
	body, err := os.ReadFile("../../../../spec/production-system/integration/contracts/v1/chain/abi-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog decode.Catalog
	if err = json.Unmarshal(body, &catalog); err != nil {
		t.Fatal(err)
	}
	for _, contract := range catalog.Contracts {
		if contract.Name == "EventEmitter" {
			r, err := decode.NewRegistry(body, []decode.Deployment{{Address: emitter, Role: contract.Name, ABIRevision: contract.ABIRevision}})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
	}
	t.Fatal("frozen EventEmitter ABI absent")
	return nil
}
func TestPoisonBetweenValidEventsBlocksWatermarkPostgres(t *testing.T) {
	for _, missingTimestamp := range []bool{false, true} {
		t.Run(fmt.Sprint(missingTimestamp), func(t *testing.T) {
			ctx, dsn, pool := replayPostgres(t)
			b := prepare(t, pool)
			emitter := common.HexToAddress(b.Records[0].Emitter)
			p := processorFor(pool)
			p.deps.Quarantine = &QuarantinePolicy{Registry: sourceRegistry(t, emitter), SourceStream: "live"}
			logs := []types.Log{}
			for _, raw := range b.Records {
				topics := []common.Hash{}
				for _, topic := range raw.Topics {
					topics = append(topics, common.HexToHash(topic))
				}
				data := append([]byte(nil), raw.Data...)
				if !missingTimestamp && raw.LogIndex == 2 {
					data = []byte{0xff}
				}
				logs = append(logs, types.Log{Address: emitter, Topics: topics, Data: data, BlockNumber: uint64(raw.Height), BlockHash: common.HexToHash(raw.BlockHash), TxHash: common.HexToHash(raw.TransactionHash), TxIndex: uint(raw.TransactionIndex), Index: uint(raw.LogIndex)})
			}
			timestamps := map[uint64]int64{101: 123456}
			if missingTimestamp {
				timestamps = map[uint64]int64{}
			}
			if err := p.RetainSourcePoison(ctx, source.FetchResult{Logs: logs, BlockTimestamps: timestamps}); !errors.Is(err, store.ErrRequiredPoison) {
				t.Fatalf("required poison not retained: %v", err)
			}
			if !missingTimestamp {
				b.Records = append([]store.RawRecord(nil), b.Records...)
				for i := range b.Records {
					if b.Records[i].LogIndex == 2 {
						b.Records[i].Data = []byte{0xff}
					}
				}
			}
			if err := p.ProcessCanonicalBatch(ctx, b, timestamps, nil); err == nil {
				t.Fatal("required poison acknowledged as completed range")
			}
			fresh, err := pgxpool.New(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			var watermark int64
			if err = fresh.QueryRow(ctx, `SELECT typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=125 AND stream_id='live'`).Scan(&watermark); err != nil || watermark != 100 {
				t.Fatalf("poison advanced cursor: %d %v", watermark, err)
			}
			var retained bool
			if missingTimestamp {
				err = fresh.QueryRow(ctx, `SELECT count(*)=4 FROM indexer.indexer_quarantine WHERE error_code='source_repair_required' AND required AND resolution='unresolved'`).Scan(&retained)
			} else {
				err = fresh.QueryRow(ctx, `SELECT raw_payload->>'Data'=$1 FROM indexer.indexer_quarantine WHERE log_index=2 AND required AND resolution='unresolved'`, base64.StdEncoding.EncodeToString([]byte{0xff})).Scan(&retained)
			}
			if err != nil || !retained {
				t.Fatalf("restart lost poisoned required payload: %v", err)
			}
		})
	}
}

func authorizedReplayFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*store.Store, store.ReplayRequest, store.ReplayJob, store.ReplaySegment) {
	t.Helper()
	st := store.New(pool)
	r := store.ReplayRequest{JobID: "cancel1", SourceStream: "canonical", Target: "charts", Generation: "rebuild1", ProjectionRevision: "revision2", Mode: "backfill", Requester: "operator", AuthorizationID: "auth1", FromHeight: 101, ToHeight: 101, CheckpointHeight: 101, CheckpointHash: "0x" + strings.Repeat("2", 64)}
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.indexer_replay_authorizations VALUES('auth1','operator',$1,'backfill','charts','rebuild1','revision2',101,101,1,clock_timestamp()+interval '1 hour',NULL)`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := st.AdmitReplay(ctx, r); err != nil {
		t.Fatal(err)
	}
	block := store.BlockCompleteness{Height: 101, Hash: r.CheckpointHash, FetchComplete: true, RequiredDecodeComplete: true}
	if err := st.BindReplayCheckpoint(ctx, r, block); err != nil {
		t.Fatal(err)
	}
	j, err := st.AcquireReplay(ctx, r.JobID, "worker", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	segment := store.ReplaySegment{FormatVersion: "raw-replay-v2", ChainID: 125, SourceStream: r.SourceStream, FromHeight: 101, ToHeight: 101, Blocks: []store.BlockCompleteness{block}, Records: []store.RawRecord{}, Events: []store.ReplayEvent{}}
	return st, r, j, segment
}
func TestReplayCancellationRollbackAndResumePostgres(t *testing.T) {
	ctx, _, pool := replayPostgres(t)
	st, r, j, segment := authorizedReplayFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `CREATE FUNCTION indexer.l06_replay_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_catalog.pg_sleep(5); RETURN NEW; END$$; CREATE TRIGGER l06_replay_pause AFTER INSERT ON indexer.indexer_replay_source_blocks FOR EACH ROW EXECUTE FUNCTION indexer.l06_replay_pause()`); err != nil {
		t.Fatal(err)
	}
	project := func(context.Context, pgx.Tx, store.ReplayEvent) (string, error) {
		return "", errors.New("empty block must not project events")
	}
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	err := st.CommitReplaySegment(cancelCtx, j, segment, project)
	cancel()
	if err == nil {
		t.Fatal("cancelled replay acknowledged")
	}
	var progress, count int64
	if err = pool.QueryRow(ctx, `SELECT progress_height,(SELECT count(*) FROM indexer.indexer_replay_batches) FROM indexer.indexer_replay_jobs WHERE job_id=$1`, r.JobID).Scan(&progress, &count); err != nil || progress != 100 || count != 0 {
		t.Fatalf("cancelled durable progress=%d batches=%d err=%v", progress, count, err)
	}
	if err = st.PauseReplayLease(ctx, j, "cancelled context", 101, 101); err != nil {
		t.Fatal(err)
	}
	if _, err = st.AcquireReplay(ctx, r.JobID, "worker", 30*time.Second); !errors.Is(err, store.ErrFenced) {
		t.Fatalf("paused replay admitted without explicit resume: %v", err)
	}
	if err = st.ResumeReplay(ctx, r.Scope(), r.JobID); err != nil {
		t.Fatal(err)
	}
	j, err = st.AcquireReplay(ctx, r.JobID, "replacement", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `DROP TRIGGER l06_replay_pause ON indexer.indexer_replay_source_blocks; DROP FUNCTION indexer.l06_replay_pause()`); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitReplaySegment(ctx, j, segment, project); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT progress_height,(SELECT count(*) FROM indexer.indexer_replay_batches) FROM indexer.indexer_replay_jobs WHERE job_id=$1`, r.JobID).Scan(&progress, &count); err != nil || progress != 101 || count != 1 {
		t.Fatalf("resumed progress=%d batches=%d err=%v", progress, count, err)
	}
}
func TestReplayPausedOldLeaderCannotPauseReplacementPostgres(t *testing.T) {
	ctx, _, pool := replayPostgres(t)
	st, r, old, _ := authorizedReplayFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE indexer.indexer_replay_jobs SET expires_at=clock_timestamp()-interval '1 second' WHERE job_id=$1`, r.JobID); err != nil {
		t.Fatal(err)
	}
	replacement, err := st.AcquireReplay(ctx, r.JobID, "replacement", 30*time.Second)
	if err != nil || replacement.Fence <= old.Fence {
		t.Fatalf("replacement fencing: %v", err)
	}
	if err = st.PauseReplayLease(ctx, old, "stale worker failure", 101, 101); !errors.Is(err, store.ErrFenced) {
		t.Fatalf("old worker paused replacement: %v", err)
	}
}

func TestPrivateReplayAuthorizationPostgres(t *testing.T) {
	ctx, _, pool := replayPostgres(t)
	st, r, _, _ := authorizedReplayFixture(t, ctx, pool)
	ops := &PrivateOperations{Store: st, Authorizer: &DatabaseOperationAuthorizer{Store: st, AuthenticatedOperator: "operator"}}
	if err := ops.AdmitReplay(ctx, r); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*store.ReplayRequest){func(r *store.ReplayRequest) { r.Requester = "attacker" }, func(r *store.ReplayRequest) { r.Target = "accounts" }, func(r *store.ReplayRequest) { r.Generation = "other" }, func(r *store.ReplayRequest) { r.ProjectionRevision = "other" }, func(r *store.ReplayRequest) { r.FromHeight = 100 }, func(r *store.ReplayRequest) { r.Mode = "replay" }, func(r *store.ReplayRequest) { r.AuthorizationID = "missing" }} {
		changed := r
		changed.JobID = "denied"
		alter(&changed)
		if err := ops.AdmitReplay(ctx, changed); err == nil {
			t.Fatalf("out of scope replay admitted: %+v", changed)
		}
	}
	if err := (&PrivateOperations{Store: st}).AdmitReplay(ctx, r); err == nil {
		t.Fatal("missing private authentication hook admitted replay")
	}
	if _, err := pool.Exec(ctx, `UPDATE indexer.indexer_replay_authorizations SET revoked_at=clock_timestamp() WHERE authorization_id=$1`, r.AuthorizationID); err != nil {
		t.Fatal(err)
	}
	if err := ops.AdmitReplay(ctx, r); err == nil {
		t.Fatal("revoked grant admitted replay")
	}
	if _, err := pool.Exec(ctx, `UPDATE indexer.indexer_replay_authorizations SET revoked_at=NULL,expires_at=clock_timestamp()-interval '1 second' WHERE authorization_id=$1`, r.AuthorizationID); err != nil {
		t.Fatal(err)
	}
	if err := ops.AdmitReplay(ctx, r); err == nil {
		t.Fatal("expired grant admitted replay")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_replay_jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unauthorized replay persisted rows=%d err=%v", count, err)
	}
}
func TestPrivateQuarantineWaiverAuthorizationPostgres(t *testing.T) {
	ctx, _, pool := replayPostgres(t)
	st := store.New(pool)
	raw := store.RawRecord{TransactionHash: "0x" + strings.Repeat("1", 64), BlockHash: "0x" + strings.Repeat("2", 64), Emitter: "0x" + strings.Repeat("3", 40), Height: 101, Data: []byte{0xff}}
	if err := st.RetainPoison(ctx, store.PoisonRecord{ChainID: 125, StreamID: "canonical", Raw: raw, ErrorCode: "malformed_required_swap", ABIRevision: "revision1", Requirement: "D02_021", Required: true}); err != nil {
		t.Fatal(err)
	}
	digest, err := store.RawDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO indexer.indexer_replay_authorizations VALUES('waive1','operator',$1,'waive','canonical','repair1','revision1',101,101,1,clock_timestamp()+interval '1 hour',NULL)`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	scope := store.OperationScope{AuthorizationID: "waive1", Operator: "operator", Action: "waive", Target: "canonical", Generation: "repair1", Revision: "revision1", From: 101, To: 101}
	ops := &PrivateOperations{Store: st, Authorizer: &DatabaseOperationAuthorizer{Store: st, AuthenticatedOperator: "operator"}}
	for _, alter := range []func(*store.OperationScope){func(s *store.OperationScope) { s.Operator = "attacker" }, func(s *store.OperationScope) { s.Action = "replay" }, func(s *store.OperationScope) { s.From = 100 }, func(s *store.OperationScope) { s.Target = "other" }} {
		denied := scope
		alter(&denied)
		if err = ops.WaivePoison(ctx, denied, digest, "D02_021", "repair awaiting source", time.Now().Add(10*time.Minute)); err == nil {
			t.Fatal("unauthorized waiver accepted")
		}
	}
	if err = ops.WaivePoison(ctx, scope, digest, "D02_021", "repair awaiting source", time.Now().Add(2*time.Hour)); err == nil {
		t.Fatal("waiver exceeds issued grant expiry")
	}
	if err = ops.WaivePoison(ctx, scope, digest, "D02_021", "repair awaiting source", time.Now().Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	var operator, reason string
	var height, count int64
	if err = pool.QueryRow(ctx, `SELECT operator_identity,reason,from_height,(SELECT count(*) FROM indexer.swaps) FROM indexer.indexer_quarantine_waivers WHERE raw_digest=$1`, digest).Scan(&operator, &reason, &height, &count); err != nil || operator != "operator" || reason != "repair awaiting source" || height != 101 || count != 0 {
		t.Fatalf("waiver audit or fabricated typed effects: %s/%s/%d/%d %v", operator, reason, height, count, err)
	}
}

func TestReplayQuotaAndUnavailableRangePostgres(t *testing.T) {
	ctx, _, pool := replayPostgres(t)
	st, r, j, _ := authorizedReplayFixture(t, ctx, pool)
	other := r
	other.JobID = "overlapping"
	if err := st.AdmitReplay(ctx, other); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcquireReplay(ctx, other.JobID, "other", 30*time.Second); err == nil {
		t.Fatal("per target quota admitted overlapping concurrent replay")
	}
	if err := st.PauseReplayLease(ctx, j, "archive unavailable inclusive range [101,101]", 101, 101); err != nil {
		t.Fatal(err)
	}
	var from, to, progress int64
	var state string
	if err := pool.QueryRow(ctx, `SELECT unavailable_from,unavailable_to,progress_height,status FROM indexer.indexer_replay_jobs WHERE job_id=$1`, r.JobID).Scan(&from, &to, &progress, &state); err != nil || from != 101 || to != 101 || progress != 100 || state != "paused" {
		t.Fatalf("missing archive range hidden: %d/%d/%d/%s %v", from, to, progress, state, err)
	}
	if _, err := st.AcquireReplay(ctx, other.JobID, "other", 30*time.Second); err != nil {
		t.Fatalf("paused job retained quota: %v", err)
	}
}
