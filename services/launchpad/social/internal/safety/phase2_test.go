package safety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPostgresLinkedAppealReversalPreservesIndependentGlobalBan(t *testing.T) {
	s := fixture(t)
	ctx := principal(s, walletA, "room_moderator")
	ddl, e := os.ReadFile("../../../../../launchpad/spec/production-system/handoffs/L38/phase2/schema-proposal.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(ctx, string(ddl)); e != nil {
		t.Fatal(e)
	}
	caseID := uuid.NewString()
	if _, e = s.DB.Exec(ctx, `INSERT INTO safety_cases(id,target_type,target_id,pool_id,affected_wallet,reporter,category,description)VALUES($1,'message','real-case-target','pool-a',$2,$3,'abuse','private')`, caseID, walletB, walletA); e != nil {
		t.Fatal(e)
	}
	banID, e := s.ActionBan(ctx, caseID, 1, BanCommand{Wallet: walletB, Scope: "pool", Resource: "pool-a", Reason: "case reviewed", PolicyVersion: "v2"})
	if e != nil {
		t.Fatal(e)
	}
	var action string
	if e = s.DB.QueryRow(ctx, `SELECT action_id FROM safety_action_bans WHERE ban_id=$1`, banID).Scan(&action); e != nil {
		t.Fatal(e)
	}
	appellant := principal(s, walletB, "participant")
	appeal, e := s.Appeal(appellant, action, "review please")
	if e != nil {
		t.Fatal(e)
	}
	global, e := s.Ban(principal(s, walletA, "global_administrator"), BanCommand{Wallet: walletB, Scope: "global", Reason: "separate global case", PolicyVersion: "v2"})
	if e != nil {
		t.Fatal(e)
	}
	command := ReversalCommand{AppealID: appeal, BanID: banID, ExpectedRevision: 1, ExpectedBanRevision: 1, Reason: "independent review", PolicyVersion: "v2"}
	if e = s.ReverseBanAppeal(ctx, command); !errors.Is(e, ErrForbidden) {
		t.Fatal("issuer reversed own action", e)
	}
	reviewer := principal(s, "0x3333333333333333333333333333333333333333", "room_moderator")
	if e = s.ReverseBanAppeal(reviewer, command); e != nil {
		t.Fatal(e)
	}
	if got := s.Check(appellant, walletB, "pool", "pool-a"); got != Deny {
		t.Fatal("global ban lost", got)
	}
	outcome, e := s.PrivateAppeal(appellant, appeal)
	if e != nil || outcome.State != "reversed" || outcome.Reason != command.Reason {
		t.Fatal(outcome, e)
	}
	if _, e = s.PrivateAppeal(ctx, appeal); e == nil {
		t.Fatal("other account read private outcome")
	}
	if e = s.Revoke(principal(s, walletA, "global_administrator"), global, 1, "global case separately reviewed"); e != nil {
		t.Fatal(e)
	}
	if got := s.Check(appellant, walletB, "pool", "pool-a"); got != Allow {
		t.Fatal(got)
	}
	if e = s.ReverseBanAppeal(reviewer, command); !errors.Is(e, ErrConflict) {
		t.Fatal("stale reversal accepted", e)
	}
}
func TestRealRedisFourReplicasAcceptExactlyTenBurstMessages(t *testing.T) {
	address := os.Getenv("L38_REDIS_ADDR")
	if address == "" {
		address = "127.0.0.1:26379"
	}
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	if e := client.Ping(ctx).Err(); e != nil {
		t.Fatal("real Redis required", e)
	}
	namespace := "l38-phase2-" + uuid.NewString()
	digest := sha256.Sum256([]byte(walletA))
	tag := hex.EncodeToString(digest[:16])
	r := sha256.Sum256([]byte("pool-a"))
	suffix := hex.EncodeToString(r[:16])
	defer client.Del(ctx, namespace+":{"+tag+"}:room:"+suffix, namespace+":{"+tag+"}:global")
	replicas := []*AbuseLimiter{{Redis: client, Namespace: namespace}, {Redis: client, Namespace: namespace}, {Redis: client, Namespace: namespace}, {Redis: client, Namespace: namespace}}
	var accepted atomic.Int32
	var rejected atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 11; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := replicas[i%4].Take(ctx, walletA, "pool", "pool-a", 0)
			if e == nil {
				accepted.Add(1)
				return
			}
			var limited RateLimited
			if errors.As(e, &limited) && limited.RetryAfterSeconds > 0 {
				rejected.Add(1)
			} else {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 10 || rejected.Load() != 1 {
		t.Fatalf("accepted=%d rejected=%d", accepted.Load(), rejected.Load())
	}
}
func TestMissingDistributedRateAuthorityRejectsWrites(t *testing.T) {
	l := &AbuseLimiter{Namespace: "phase2-missing-real-authority"}
	if e := l.Take(context.Background(), walletA, "pool", "pool-a", 0); !errors.Is(e, ErrUnavailable) {
		t.Fatal(e)
	}
	if e := l.Take(context.Background(), walletA, "pool", "pool-a", 0); e == nil {
		t.Fatal("outage allowed send")
	}
}

func TestPostgresEmptySubscriberReplayStaysRemovedUntilPermittedRestore(t *testing.T) {
	s := fixture(t)
	ctx := principal(s, walletA, "room_moderator")
	caseID := uuid.NewString()
	if _, e := s.DB.Exec(ctx, `INSERT INTO safety_cases(id,target_type,target_id,pool_id,affected_wallet,reporter,category,description)VALUES($1,'message','replayed-content','pool-a',$2,$3,'abuse','restricted evidence')`, caseID, walletB, walletA); e != nil {
		t.Fatal(e)
	}
	removed, e := s.SetVisibility(ctx, caseID, 0, "removed", "remove abuse")
	if e != nil {
		t.Fatal(e)
	}
	restarted := &Store{DB: s.DB, Clock: s.Clock}
	original := ReplayContent{TargetType: "message", TargetID: "replayed-content", Text: "removed original text"}
	projected, e := restarted.ProjectReplay(ctx, original)
	if e != nil || projected.Text != "" || projected.VisibilityRevision != removed.Revision {
		t.Fatal("empty-cache replay exposed removed content", projected, e)
	}
	ban, e := s.Ban(principal(s, walletA, "global_administrator"), BanCommand{Wallet: walletB, Scope: "global", Reason: "independent policy restriction", PolicyVersion: "v3"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SetVisibility(ctx, caseID, removed.Revision, "restored", "restore while banned"); !errors.Is(e, ErrForbidden) {
		t.Fatal("independent policy bypassed", e)
	}
	if e = s.Revoke(principal(s, walletA, "global_administrator"), ban, 1, "separate restriction resolved"); e != nil {
		t.Fatal(e)
	}
	restored, e := s.SetVisibility(ctx, caseID, removed.Revision, "restored", "explicit permitted restore")
	if e != nil {
		t.Fatal(e)
	}
	if projected, e = restarted.ProjectReplay(ctx, original); !errors.Is(e, ErrConflict) || projected.Text != "" {
		t.Fatal("old content payload survived restore revision", projected, e)
	}
	permitted := original
	permitted.VisibilityRevision = restored.Revision
	permitted.Text = "policy permitted restored text"
	if projected, e = restarted.ProjectReplay(ctx, permitted); e != nil || projected.Text != permitted.Text {
		t.Fatal(projected, e)
	}
	payload, e := json.Marshal(removed)
	if e != nil || strings.Contains(string(payload), "restricted evidence") || strings.Contains(string(payload), walletA) {
		t.Fatal("private evidence in public removal", e)
	}
}

func TestPostgresPolicyQueryFailureDeniesEveryScopeWithoutOutboxWrites(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, e := s.DB.Exec(ctx, `ALTER TABLE safety_bans RENAME TO interrupted_safety_bans`); e != nil {
		t.Fatal(e)
	}
	for _, scope := range []string{"pool", "dm", "voice"} {
		if got := s.Check(ctx, walletA, scope, "resource-a"); got != Unavailable {
			t.Fatal(scope, got)
		}
		tx, e := s.DB.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		got := s.CheckTx(ctx, tx, walletA, scope, "resource-a")
		if e = tx.Rollback(ctx); e != nil {
			t.Fatal(e)
		}
		if got != Unavailable {
			t.Fatal("transactional policy failed open", scope, got)
		}
	}
	var count int
	if e := s.DB.QueryRow(ctx, `SELECT count(*) FROM safety_outbox`).Scan(&count); e != nil || count != 0 {
		t.Fatal("policy error emitted a write", count, e)
	}
}

func TestPostgresAbusiveVoiceHostEscalatesToGlobalQueuePrivately(t *testing.T) {
	s := fixture(t)
	room := uuid.NewString()
	ctx := principal(s, walletB, "participant")
	if _, e := s.DB.Exec(ctx, `INSERT INTO voice_rooms(id,pool_id,host_wallet,start_key,state,provider_session)VALUES($1,'pool-a',$2,$3,'live','report-metadata-only')`, room, walletA, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec(ctx, `INSERT INTO voice_participants(room_id,wallet,role)VALUES($1,$2,'host'),($1,$3,'listener')`, room, walletA, walletB); e != nil {
		t.Fatal(e)
	}
	s.Targets = VoiceTargets{DB: s.DB}
	report := Report{TargetType: "voice_participant", TargetID: room + ":" + walletA, Category: "abusive host", Description: "private timestamp interval"}
	first, e := s.Report(ctx, report)
	if e != nil {
		t.Fatal(e)
	}
	duplicate, e := s.Report(ctx, report)
	if e != nil || duplicate.ID != first.ID {
		t.Fatal("duplicate inflated global safety queue", duplicate, e)
	}
	if first.PoolID != "" {
		t.Fatal("host abuse did not escalate", first)
	}
	items, e := s.ListCases(principal(s, walletA, "room_moderator"), "open", "", 50)
	if e != nil || len(items) != 0 {
		t.Fatal("host's room moderator scope inspected global report", items, e)
	}
	global, e := s.ListCases(principal(s, "0x3333333333333333333333333333333333333333", "global_administrator"), "open", "", 50)
	if e != nil || len(global) != 1 {
		t.Fatal(global, e)
	}
	wire, e := json.Marshal(global)
	if e != nil || strings.Contains(string(wire), walletB) || strings.Contains(string(wire), report.Description) {
		t.Fatal("reporter or private evidence leaked", e)
	}
}
