package l17

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func phase2Repository(t *testing.T) Repository {
	t.Helper()
	dsn := os.Getenv("L17_TEST_DSN")
	if dsn == "" {
		t.Fatal("L17_TEST_DSN required for real PostgreSQL phase2 evidence")
	}
	ctx := context.Background()
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.MaxConns = 4
	admin, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(admin.Close)
	id, e := opaqueID()
	if e != nil {
		t.Fatal(e)
	}
	schema := "l17_phase2_" + strings.ReplaceAll(id, "-", "")
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); e != nil {
			t.Error(e)
		}
	})
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	for _, ddl := range []string{DDL, HistoryDDL, CardDDL, CardWorkerDDL, ExportDDL, MetadataDDL} {
		if _, e = pool.Exec(ctx, ddl); e != nil {
			t.Fatal(e)
		}
	}
	return Repository{Pool: pool}
}
func phase2Positions(n int, revision string) []Position {
	out := make([]Position, n)
	for i := range out {
		p := fixture()
		p.PositionID = fmt.Sprintf("125:phase2:%06d", i)
		p.SourceWatermark.ProjectionRevision = revision
		out[i] = p
	}
	return out
}
func phase2Commit(t *testing.T, r Repository, source, revision string, n int) Summary {
	t.Helper()
	p := phase2Positions(n, revision)
	s, e := r.CommitSnapshot(context.Background(), source, Summary{Wallet: wallet, Watermark: p[0].SourceWatermark}, p, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestPhase2ImmutablePagesAndAtomicRollback(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	now := time.Now()
	s1 := phase2Commit(t, r, "phase2-first", "1", 151)
	service := Service{Repository: r, CursorKey: []byte(strings.Repeat("k", 32))}
	first, e := service.Portfolio(ctx, wallet, "", 100, now)
	if e != nil || len(first.Page.Positions) != 100 || first.Page.Next == "" {
		t.Fatal(first, e)
	}
	positions := phase2Positions(152, "2")
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	s2, e := r.CommitSnapshotTx(ctx, tx, "phase2-rollback", Summary{Wallet: wallet, Watermark: positions[0].SourceWatermark}, positions, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	var exists bool
	if e = r.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM portfolio_snapshots WHERE id=$1)", s2.SnapshotID).Scan(&exists); e != nil || exists {
		t.Fatal("rolled back fold escaped transaction", e)
	}
	s2 = phase2Commit(t, r, "phase2-second", "2", 152)
	second, e := service.Portfolio(ctx, wallet, first.Page.Next, 100, now)
	if e != nil || second.Page.Summary.SnapshotID != s1.SnapshotID || len(second.Page.Positions) != 51 || second.Page.Next != "" {
		t.Fatal(second, e)
	}
	seen := map[string]bool{}
	for _, p := range append(first.Page.Positions, second.Page.Positions...) {
		if seen[p.PositionID] {
			t.Fatal("duplicate", p.PositionID)
		}
		seen[p.PositionID] = true
	}
	if len(seen) != 151 {
		t.Fatal("missing positions")
	}
	from, e := time.Parse(time.RFC3339Nano, s1.Watermark.ObservedAt)
	if e != nil {
		t.Fatal(e)
	}
	series, last, e := r.HistorySeries(ctx, wallet, wallet, from.Add(-time.Hour), from.Add(time.Hour), "1h", 2)
	if e != nil || len(series) != 1 || last == nil || last.SnapshotID != s2.SnapshotID {
		t.Fatal("indexed history chart", series, last, e)
	}
	historical, e := r.HistoricalReturn(ctx, wallet, wallet, s2.SnapshotID, from.Add(-time.Hour), from.Add(time.Hour))
	if e != nil || historical.Method != "unavailable" || historical.Return != nil {
		t.Fatal("balance growth supplied historical percentage", historical, e)
	}
	latest, e := service.Portfolio(ctx, wallet, "", 100, now)
	if e != nil || latest.Page.Summary.SnapshotID != s2.SnapshotID {
		t.Fatal(latest, e)
	}
	if _, e = service.Portfolio(ctx, token, first.Page.Next, 100, now); !errors.Is(e, ErrInvalid) {
		t.Fatal("cursor owner substitution", e)
	}
	if _, e = service.Portfolio(ctx, wallet, first.Page.Next, 100, now.Add(16*time.Minute)); !errors.Is(e, ErrRestart) {
		t.Fatal("expired cursor", e)
	}
	if len(first.Errors) != 100 || first.Page.Summary.RealizedPnlRaw != nil {
		t.Fatal("unknown basis must be explicit", first.Errors)
	}
	raw, _ := json.Marshal(struct {
		Original, New string
		Rows          int
		Revision      string
	}{s1.SnapshotID, s2.SnapshotID, len(seen), second.Page.Summary.Watermark.ProjectionRevision})
	t.Logf("L17_PHASE2_EVIDENCE %s", raw)
}
func TestPhase2DurableExport100001AndCancellation(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	s := phase2Commit(t, r, "phase2-export-source", "1", 1)
	records := make([]HistoryRecord, 100001)
	six := 6
	for i := range records {
		records[i] = HistoryRecord{EventID: fmt.Sprintf("phase2-history-%06d", i), Kind: "deposit", Asset: AssetRef{Kind: "erc20", ChainID: ptr("125"), Address: ptr(token), Decimals: &six}, AmountRaw: "100000000", BasisQuality: "unknown", Position: ChainPosition{ChainID: "125", Height: "99", BlockHash: s.Watermark.BlockHash, TransactionHash: "0x" + strings.Repeat("b", 64), TransactionIndex: "0", LogIndex: fmt.Sprint(i)}}
	}
	if e := r.CommitHistory(ctx, wallet, s.SnapshotID, "phase2-history", records); e != nil {
		t.Fatal(e)
	}
	job, e := r.CreateExportJob(ctx, wallet, wallet, s.SnapshotID, "jsonl", "phase2-export-job")
	if e != nil {
		t.Fatal(e)
	}
	again, e := r.CreateExportJob(ctx, wallet, wallet, s.SnapshotID, "jsonl", "phase2-export-job")
	if e != nil || again.ID != job.ID {
		t.Fatal("idempotency", again, e)
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	worker := ExportWorker{Repository: r, Directory: dir, Owner: "phase2-real-worker"}
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	complete, e := r.ExportJob(ctx, job.ID, wallet, wallet)
	if e != nil || complete.State != "complete" || complete.Manifest == nil || complete.Manifest.Rows != 100001 {
		t.Fatal(complete, e)
	}
	f, m, e := r.OpenExport(ctx, dir, job.ID, wallet, wallet)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.New()
	_, e = io.Copy(hash, f)
	f.Close()
	if e != nil || hex.EncodeToString(hash.Sum(nil)) != m.SHA256 {
		t.Fatal("persisted manifest digest", e)
	}
	if _, _, e = r.OpenExport(ctx, dir, job.ID, token, token); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross wallet download", e)
	}
	if _, e = r.Pool.Exec(ctx, "UPDATE portfolio_export_jobs SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.OpenExport(ctx, dir, job.ID, wallet, wallet); !errors.Is(e, ErrNotFound) {
		t.Fatal("expired download", e)
	}
	if e = worker.CleanupOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(dir, m.Object)); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("expired object remains", e)
	}
	cancelled, e := r.CreateExportJob(ctx, wallet, wallet, s.SnapshotID, "csv", "phase2-cancel-job")
	if e != nil {
		t.Fatal(e)
	}
	partial := filepath.Join(dir, cancelled.ID+"-1.partial")
	if e = os.WriteFile(partial, []byte("unfinished"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = r.CancelExport(ctx, cancelled.ID, wallet, wallet); e != nil {
		t.Fatal(e)
	}
	if e = worker.CleanupOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(partial); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("cancelled object remains", e)
	}
	raw, _ := json.Marshal(m)
	t.Logf("L17_PHASE2_EVIDENCE %s", raw)
}
func TestPhase2StrictRenderAndPersistentPrivacyFailure(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	now := time.Now()
	s := phase2Commit(t, r, "phase2-card-source", "1", 1)
	if _, e := ogrender.NewStrict(t.TempDir()); e == nil {
		t.Fatal("missing actual font did not fail")
	}
	if _, e := r.CreateCardV2(ctx, wallet, wallet, s.SnapshotID, "missing-metadata", false, now); !errors.Is(e, ErrUnavailable) {
		t.Fatal("fabricated immutable time range", e)
	}
	quote := fixture().MarkPrice.QuoteAsset
	metadata := SnapshotHistoryMetadata{SourceEvent: "phase2-canonical-time-range", Start: now.Add(-time.Hour), End: now, Quote: quote}
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.CommitHistoryMetadataTx(ctx, tx, wallet, s.SnapshotID, metadata); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	card, e := r.CreateCardV2(ctx, wallet, wallet, s.SnapshotID, "phase2-card-create", false, now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.PublicCard(ctx, card.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("draft disclosed", e)
	}
	worker := CardWorker{Repository: r, Owner: "phase2-card-worker", FontDir: t.TempDir()}
	if e = worker.RunOnce(ctx); e == nil {
		t.Fatal("missing required font published card")
	}
	var state string
	var attempts int
	if e = r.Pool.QueryRow(ctx, "SELECT state,attempts FROM portfolio_cards WHERE id=$1", card.ID).Scan(&state, &attempts); e != nil || state != "pending" || attempts != 1 {
		t.Fatal(state, attempts, e)
	}
	if e = r.RevokeCard(ctx, card.ID, wallet, wallet, now.Add(-301*time.Second)); e != nil {
		t.Fatal(e)
	}
	if _, e = r.PublicCard(ctx, card.ID); !errors.Is(e, ErrGone) {
		t.Fatal("revoked card", e)
	}
	if e = worker.RunOnce(ctx); !errors.Is(e, ErrUnavailable) {
		t.Fatal("missing purge adapter must remain pending", e)
	}
	owner, e := r.OwnerCardV2(ctx, card.ID, wallet, wallet, now)
	if e != nil || !owner.PurgePending || !owner.PrivacyIncident || owner.PurgeLastError == nil {
		t.Fatal(owner, e)
	}
	if e = r.RevokeCard(ctx, card.ID, token, token, now); !errors.Is(e, ErrNotFound) {
		t.Fatal("cross wallet revoke", e)
	}
	raw, _ := json.Marshal(owner)
	t.Logf("L17_PHASE2_EVIDENCE %s", raw)
}
func TestPhase2RevisionDedupAndDisplayUnavailable(t *testing.T) {
	tracker := RevisionTracker{}
	id, e := opaqueID()
	if e != nil {
		t.Fatal(e)
	}
	principal := auth.IdentityPrincipal{AccountID: "account", SessionID: "session", Wallet: wallet}
	n := Notice{Wallet: wallet, SnapshotID: id, Revision: "1", Algorithm: Algorithm}
	accepted, e := tracker.Accept(principal, n)
	if e != nil || !accepted {
		t.Fatal(accepted, e)
	}
	accepted, e = tracker.Accept(principal, n)
	if e != nil || accepted {
		t.Fatal("duplicate refetch", e)
	}
	n.Revision = "2"
	accepted, e = tracker.Accept(principal, n)
	if e != nil || !accepted {
		t.Fatal("revision advance", e)
	}
	display := (DisplayProvider{}).Convert(context.Background(), "100000000", time.Now())
	if display.UsdMicro != nil || display.UsdlRaw != "100000000" {
		t.Fatal("absent quote fabricated parity", display)
	}
	flow, e := EquityChange("0", "100000000", "100000000", "0", "0", "0", true)
	if e != nil || flow.ProfitRaw == nil || *flow.ProfitRaw != "0" || flow.Return.Denominator != "" {
		t.Fatal("deposit mistaken for profit", flow, e)
	}
}

func TestPhase2PublicHTTPAndOwnerUnavailable(t *testing.T) {
	r := phase2Repository(t)
	s := phase2Commit(t, r, "phase2-public-http", "1", 2)
	service := Service{Repository: r, CursorKey: []byte(strings.Repeat("h", 32))}
	router := chi.NewRouter()
	service.RegisterHTTP(router)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v2/wallets/"+wallet+"/portfolio?limit=1", nil)
	router.ServeHTTP(recorder, request)
	if recorder.Code != 200 {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	var response map[string]json.RawMessage
	if e := json.Unmarshal(recorder.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	if _, present := response["positionErrors"]; !present {
		t.Fatal("partial position errors omitted")
	}
	for _, private := range []string{"aliases", "groups", "sessionId", "accountId"} {
		if strings.Contains(recorder.Body.String(), `"`+private+`"`) {
			t.Fatal("private data leaked", private)
		}
	}
	var cursor *string
	if e := json.Unmarshal(response["nextCursor"], &cursor); e != nil || cursor == nil || len(*cursor) > 256 {
		t.Fatal("canonical cursor length", cursor, e)
	}
	private := httptest.NewRecorder()
	router.ServeHTTP(private, httptest.NewRequest(http.MethodGet, "/v2/me/portfolio", nil))
	if private.Code != 503 || strings.Contains(private.Body.String(), s.SnapshotID) {
		t.Fatal("missing authority exposed private response", private.Code, private.Body.String())
	}
	for _, body := range []string{`{"snapshotId":"` + s.SnapshotID + `","profitRaw":"25000000"}`, `{"snapshotId":"` + s.SnapshotID + `","snapshotId":"` + s.SnapshotID + `"}`} {
		var value struct {
			Snapshot      string `json:"snapshotId"`
			IncludeWallet bool   `json:"includeWallet"`
		}
		if e := decodeBody(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &value); !errors.Is(e, ErrInvalid) {
			t.Fatal("unverified or ambiguous card payload accepted", e)
		}
	}
	t.Logf("L17_PHASE2_EVIDENCE %s", recorder.Body.String())
}

func phase3History(summary Summary, count int) []HistoryRecord {
	six := 6
	records := make([]HistoryRecord, count)
	for i := range records {
		records[i] = HistoryRecord{EventID: fmt.Sprintf("adversarial-history-%d", i), Kind: "deposit", Asset: AssetRef{Kind: "erc20", ChainID: ptr("125"), Address: ptr(token), Decimals: &six}, AmountRaw: "100000000", BasisQuality: "unknown", Position: ChainPosition{ChainID: "125", Height: summary.Watermark.Height, BlockHash: summary.Watermark.BlockHash, TransactionHash: "0x" + strings.Repeat("b", 64), TransactionIndex: "0", LogIndex: fmt.Sprint(i)}}
	}
	return records
}
func TestPhase3HistoryReplayOwnerAndNodeDisagreement(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	summary := phase2Commit(t, r, "adversarial-history-snapshot", "1", 1)
	records := phase3History(summary, 2)
	if e := r.CommitHistory(ctx, wallet, summary.SnapshotID, "adversarial-history", records); e != nil {
		t.Fatal(e)
	}
	if e := r.CommitHistory(ctx, token, summary.SnapshotID, "adversarial-history", records); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("duplicate identity bypassed owner", e)
	}
	divergent := append([]HistoryRecord(nil), records...)
	divergent[1].Position.BlockHash = "0x" + strings.Repeat("c", 64)
	if e := r.CommitHistory(ctx, wallet, summary.SnapshotID, "adversarial-history-conflict", divergent); !errors.Is(e, ErrConflict) {
		t.Fatal("node disagreement accepted", e)
	}
	divergent = append([]HistoryRecord(nil), records...)
	divergent[1].Position.TransactionHash = "0x" + strings.Repeat("c", 64)
	if e := r.CommitHistory(ctx, wallet, summary.SnapshotID, "adversarial-transaction-conflict", divergent); !errors.Is(e, ErrConflict) {
		t.Fatal("transaction disagreement accepted", e)
	}
	var rows, notices int
	if e := r.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM portfolio_history_rows),(SELECT count(*) FROM portfolio_history_outbox)").Scan(&rows, &notices); e != nil || rows != 2 || notices != 1 {
		t.Fatal("rejection produced durable effects", rows, notices, e)
	}
	t.Logf("L17_PHASE3_EVIDENCE history_rows=%d notices=%d snapshot=%s algorithm=%s", rows, notices, summary.SnapshotID, Algorithm)
}
func TestPhase3CardRetryAfterSnapshotExpiryAndRevocation(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	now := time.Now().UTC()
	summary := phase2Commit(t, r, "adversarial-card-snapshot", "1", 1)
	card, e := r.CreateCard(ctx, wallet, wallet, summary.SnapshotID, "adversarial-card", false, now)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Pool.Exec(ctx, "UPDATE portfolio_snapshots SET expires_at=$2 WHERE id=$1", summary.SnapshotID, now.Add(-time.Second)); e != nil {
		t.Fatal(e)
	}
	if e = r.RevokeCard(ctx, card.ID, wallet, wallet, now); e != nil {
		t.Fatal(e)
	}
	again, e := r.CreateCard(ctx, wallet, wallet, summary.SnapshotID, "adversarial-card", false, now.Add(time.Hour))
	if e != nil || again.ID != card.ID || again.State != "revoked" {
		t.Fatal("durable retry depended on expired source", again, e)
	}
	if _, e = r.CreateCard(ctx, wallet, wallet, summary.SnapshotID, "adversarial-card", true, now); !errors.Is(e, ErrConflict) {
		t.Fatal("retry changed disclosure intent", e)
	}
	effects, e := r.PendingCardEffects(ctx, 100)
	if e != nil || len(effects) != 1 || effects[0].Kind != "purge" {
		t.Fatal("revocation retained render effect", effects, e)
	}
	if _, e = r.PublicCard(ctx, card.ID); !errors.Is(e, ErrGone) {
		t.Fatal("revoked card public access", e)
	}
	t.Logf("L17_PHASE3_EVIDENCE card=%s state=%s pending_effect=%s", again.ID, again.State, effects[0].Kind)
}
func TestPhase3ExportExpiredLeaseRestartAndDigestIntegrity(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	summary := phase2Commit(t, r, "adversarial-export-snapshot", "1", 1)
	records := phase3History(summary, 151)
	if e := r.CommitHistory(ctx, wallet, summary.SnapshotID, "adversarial-export-history", records); e != nil {
		t.Fatal(e)
	}
	job, e := r.CreateExportJob(ctx, wallet, wallet, summary.SnapshotID, "jsonl", "adversarial-export")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	partial := filepath.Join(dir, job.ID+"-1.partial")
	actual, e := json.Marshal(records[0])
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(partial, append(actual, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Pool.Exec(ctx, "UPDATE portfolio_export_jobs SET state='running',fence=1,attempts=1,lease_owner='interrupted_worker',lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", job.ID); e != nil {
		t.Fatal(e)
	}
	configuration := r.Pool.Config()
	r.Pool.Close()
	r.Pool, e = pgxpool.NewWithConfig(ctx, configuration)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(r.Pool.Close)
	worker := ExportWorker{Repository: r, Directory: dir, Owner: "restart_worker"}
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(partial); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("interrupted partial survived restart", e)
	}
	complete, e := r.ExportJob(ctx, job.ID, wallet, wallet)
	if e != nil || complete.State != "complete" || complete.Manifest == nil || complete.Manifest.Rows != 151 {
		t.Fatal(complete, e)
	}
	file, manifest, e := r.OpenExport(ctx, dir, job.ID, wallet, wallet)
	if e != nil {
		t.Fatal(e)
	}
	file.Close()
	object := filepath.Join(dir, manifest.Object)
	if e = os.WriteFile(object, actual, 0600); e != nil {
		t.Fatal(e)
	}
	if _, _, e = r.OpenExport(ctx, dir, job.ID, wallet, wallet); !errors.Is(e, ErrConflict) {
		t.Fatal("damaged object downloadable", e)
	}
	if e = r.CancelExport(ctx, job.ID, wallet, wallet); e != nil {
		t.Fatal(e)
	}
	if e = worker.CleanupOnce(ctx); e != nil {
		t.Fatal(e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 0 {
		t.Fatal("cancelled export objects retained", entries, e)
	}
	t.Logf("L17_PHASE3_EVIDENCE object=%s rows=%d sha256=%s snapshot=%s", manifest.Object, manifest.Rows, manifest.SHA256, manifest.SnapshotID)
}
func TestPhase3IncompleteReturnsStillValidateAllPeriods(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	periods := []ReturnPeriod{{Start: start, End: start.Add(time.Hour), OpeningEquityRaw: "100", ClosingEquityBeforeFlowRaw: "100", ExternalFlowAtEndRaw: "0", Complete: false}, {Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), OpeningEquityRaw: "100", ClosingEquityBeforeFlowRaw: "garbage", ExternalFlowAtEndRaw: "0", Complete: true}}
	if _, e := TimeWeightedReturn(periods); !errors.Is(e, ErrInvalid) {
		t.Fatal("incomplete prefix hid invalid trailing monetary input", e)
	}
}
func TestPhase3NotificationCanonicalWalletAndRevisionConflict(t *testing.T) {
	tracker := RevisionTracker{}
	principal := auth.IdentityPrincipal{AccountID: "account", SessionID: "session", Wallet: wallet}
	notice := Notice{Wallet: wallet, SnapshotID: "11111111-1111-4111-8111-111111111111", Revision: "1", Algorithm: Algorithm}
	if accepted, e := tracker.Accept(principal, notice); e != nil || !accepted {
		t.Fatal(accepted, e)
	}
	notice.SnapshotID = "22222222-2222-4222-8222-222222222222"
	if accepted, e := tracker.Accept(principal, notice); !errors.Is(e, ErrConflict) || accepted {
		t.Fatal("same revision diverged silently", accepted, e)
	}
	tracker.ForgetSession(principal.SessionID)
	if accepted, e := tracker.Accept(principal, notice); e != nil || !accepted {
		t.Fatal("session cleanup retained revision", accepted, e)
	}
}

func TestPhase3CommittedProjectionOutboxSurvivesRestartAndDuplicate(t *testing.T) {
	r := phase2Repository(t)
	ctx := context.Background()
	schema := r.Pool.Config().ConnConfig.RuntimeParams["search_path"]
	ddl, e := delivery.SchemaDDL(schema)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	policy := delivery.Policy{Subjects: map[string]bool{"portfolio.snapshot.v2": true}, EventTypes: map[string]bool{"portfolio.snapshot.v2": true}}
	store, e := delivery.NewStore(r.Pool, schema, policy)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := opaqueID()
	if e != nil {
		t.Fatal(e)
	}
	positions := phase2Positions(1, "1")
	summary, e := Aggregate(wallet, snapshot, positions[0].SourceWatermark, positions)
	if e != nil {
		t.Fatal(e)
	}
	payload, e := json.Marshal(Notice{Wallet: wallet, SnapshotID: snapshot, Revision: "1", Algorithm: Algorithm})
	if e != nil {
		t.Fatal(e)
	}
	event := delivery.Envelope{EventID: "adversarial_projection", EventType: "portfolio.snapshot.v2", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: "99", TransactionIndex: "0", LogIndex: "1"}, EmittedAt: time.Now().UTC(), TraceID: "adversarial_projection", PayloadHash: delivery.Hash(payload), Payload: payload}
	for i := 0; i < 2; i++ {
		tx, e := r.Pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = r.CommitFullProjectionDeliveryTx(ctx, tx, store, "portfolio.snapshot.v2", "adversarial-fold", summary, positions, time.Now().Add(time.Hour), event); e != nil {
			rollbackPortfolio(tx)
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	var snapshots, localNotices, brokerNotices int
	if e = r.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM portfolio_snapshots),(SELECT count(*) FROM portfolio_outbox),(SELECT count(*) FROM delivery_outbox)").Scan(&snapshots, &localNotices, &brokerNotices); e != nil || snapshots != 1 || localNotices != 1 || brokerNotices != 1 {
		t.Fatal("duplicate durable effect", snapshots, localNotices, brokerNotices, e)
	}
	configuration := r.Pool.Config()
	r.Pool.Close()
	r.Pool, e = pgxpool.NewWithConfig(ctx, configuration)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(r.Pool.Close)
	store, e = delivery.NewStore(r.Pool, schema, policy)
	if e != nil {
		t.Fatal(e)
	}
	leases, e := store.Claim(ctx, "restart_publisher", 1, time.Minute)
	if e != nil || len(leases) != 1 {
		t.Fatal("committed notice disappeared before publication", leases, e)
	}
	persisted, e := delivery.Decode(leases[0].Wire, policy, "portfolio.snapshot.v2")
	if e != nil || string(persisted.Payload) != string(payload) {
		t.Fatal("durable payload changed", persisted, e)
	}
	worker := NotificationWorker{Relay: delivery.Relay{Store: store, Owner: "missing_publisher", Batch: 1, LeaseDuration: time.Minute, PublishTimeout: time.Second, RetryDelay: time.Second, MaxAttempts: 3}}
	if e = worker.RunOnce(ctx); e == nil {
		t.Fatal("missing JetStream provider reported publication")
	}
	var notice Notice
	if e = json.Unmarshal(persisted.Payload, &notice); e != nil {
		t.Fatal(e)
	}
	tracker := RevisionTracker{}
	principal := auth.IdentityPrincipal{AccountID: "account", SessionID: "session", Wallet: wallet}
	for i := 0; i < 2; i++ {
		accepted, e := tracker.Accept(principal, notice)
		if e != nil || accepted != (i == 0) {
			t.Fatal("duplicate notice logical refetch", accepted, e)
		}
	}
	t.Logf("L17_PHASE3_EVIDENCE persisted_notice=%s snapshot_rows=%d broker_outbox_rows=%d published=false", persisted.Payload, snapshots, brokerNotices)
}

func TestPhase3FuturePriceTimestampAndCanonicalNoticeDedup(t *testing.T) {
	position := fixture()
	position.MarkPrice.Source.ObservedAt = "2026-10-11T00:00:00.000Z"
	if _, _, e := Derive(position); !errors.Is(e, ErrInvalid) {
		t.Fatal("future price provenance accepted", e)
	}
	tracker := RevisionTracker{}
	lower := "0x" + strings.Repeat("a", 40)
	principal := auth.IdentityPrincipal{AccountID: "account", SessionID: "session", Wallet: lower}
	notice := Notice{Wallet: lower, SnapshotID: "11111111-1111-4111-8111-111111111111", Revision: "1", Algorithm: Algorithm}
	if accepted, e := tracker.Accept(principal, notice); e != nil || !accepted {
		t.Fatal(accepted, e)
	}
	notice.Wallet = "0x" + strings.Repeat("A", 40)
	if accepted, e := tracker.Accept(principal, notice); e != nil || accepted {
		t.Fatal("wallet case caused duplicate refetch", accepted, e)
	}
}
func TestPhase3MalformedQueryRejectedBeforeOrigin(t *testing.T) {
	service := Service{}
	router := chi.NewRouter()
	service.RegisterHTTP(router)
	for _, query := range []string{"limit=1;cursor=ignored", "limit=%zz", strings.Repeat("x", 4097)} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/wallets/"+wallet+"/portfolio?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("ambiguous query reached origin", query, response.Code)
		}
	}
}

func TestPhase3MissingDependencyReturnsUnavailable(t *testing.T) {
	service := Service{}
	router := chi.NewRouter()
	service.RegisterHTTP(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/wallets/"+wallet+"/portfolio", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("absent portfolio origin", response.Code, response.Body.String())
	}
	if e := (CardWorker{}).RunOnce(context.Background()); !errors.Is(e, ErrUnavailable) {
		t.Fatal("absent card origin", e)
	}
	if e := (ExportWorker{}).RunOnce(context.Background()); !errors.Is(e, ErrUnavailable) {
		t.Fatal("absent export origin", e)
	}
}

func TestPhase3ProjectionHistoricalBlockDisagreement(t *testing.T) {
	first := fixture()
	second := fixture()
	second.PositionID = "125:second"
	first.SourceWatermark.Height = "98"
	second.SourceWatermark.Height = "98"
	first.MarkPrice.Source.Position.Height = "98"
	second.MarkPrice.Source.Position.Height = "98"
	second.SourceWatermark.BlockHash = "0x" + strings.Repeat("c", 64)
	second.MarkPrice.Source.Position.BlockHash = second.SourceWatermark.BlockHash
	if _, e := Aggregate(wallet, "", fixture().SourceWatermark, []Position{first, second}); !errors.Is(e, ErrConflict) {
		t.Fatal("divergent historical node blocks accepted", e)
	}
}

func TestPhase4ActualHTTPOriginMeasurement(t *testing.T) {
	r := phase2Repository(t)
	summary := phase2Commit(t, r, "phase4-origin-source", "1", 1)
	service := Service{Repository: r, CursorKey: []byte(strings.Repeat("m", 32))}
	router := chi.NewRouter()
	service.RegisterHTTP(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/wallets/"+wallet+"/portfolio?limit=1", nil))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	timing := response.Header().Get("Server-Timing")
	if !strings.HasPrefix(timing, "portfolio_origin;dur=") {
		t.Fatal("actual origin duration missing", timing)
	}
	millis, e := strconv.ParseFloat(strings.TrimPrefix(timing, "portfolio_origin;dur="), 64)
	if e != nil || millis <= 0 {
		t.Fatal("actual database query duration invalid", millis, e)
	}
	if strings.Contains(response.Body.String(), "OriginDuration") || !strings.Contains(response.Body.String(), summary.SnapshotID) {
		t.Fatal("measurement changed frozen payload or lost actual revision", response.Body.String())
	}
	t.Logf("L17_PHASE4_ORIGIN_EVIDENCE snapshot=%s revision=%s timing=%s body=%s", summary.SnapshotID, summary.Watermark.ProjectionRevision, timing, response.Body.String())
}
