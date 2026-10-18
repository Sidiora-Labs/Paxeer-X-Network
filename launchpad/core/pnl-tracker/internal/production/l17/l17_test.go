package l17

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const wallet = "0x1111111111111111111111111111111111111111"
const token = "0x2222222222222222222222222222222222222222"

func fixture() Position {
	six := 6
	zero := 0
	return Position{PositionID: "125:synthetic", ChainID: "125", Wallet: wallet, TokenAddress: token, BalanceRaw: "10", KnownQuantityRaw: "5", UnknownQuantityRaw: "5", KnownRemainingCostUsdlRaw: "10000000", KnownRealizedSubtotalUsdlRaw: "25000000", RealizedBasisQuality: "partial", PricingQuality: "priced", MarkPrice: &Price{Source: PriceSource{Kind: "post_swap_spot", AmountBasis: "effective_reserves", SourceEventID: ptr("synthetic-price"), ObservedAt: "2026-10-10T00:00:00.000Z", Position: &ChainPosition{ChainID: "125", Height: "99", TransactionIndex: "0", LogIndex: "1", BlockHash: "0x" + strings.Repeat("a", 64), TransactionHash: "0x" + strings.Repeat("b", 64)}}, Numerator: "3", Denominator: "1", BaseAsset: AssetRef{Kind: "erc20", ChainID: ptr("125"), Address: ptr(token), Decimals: &zero}, QuoteAsset: AssetRef{Kind: "erc20", ChainID: ptr("125"), Address: ptr("0x85FcD13735F4309833A503EE804ea32395851479"), CurrencyCode: ptr("USDL"), Decimals: &six}}, SourceWatermark: Watermark{ChainID: "125", Height: "99", BlockHash: "0x" + strings.Repeat("a", 64), ProjectionRevision: "1", ObservedAt: "2026-10-10T00:00:00.000Z", Integrity: "healthy"}}
}
func TestExactCoverage(t *testing.T) {
	p := fixture()
	got, value, e := Derive(p)
	if e != nil || value.String() != "30000000" || got.RealizedPnlRaw != nil || got.UnrealizedPnlRaw != nil || got.RemainingCostUsdlRaw != nil || got.BasisQuality != "partial" {
		t.Fatalf("unexpected %+v %v", got, e)
	}
	p.PricingQuality = "stale"
	_, value, e = Derive(p)
	if e != nil || value != nil {
		t.Fatal("stale counted")
	}
	p = fixture()
	p.BalanceRaw = "11"
	if _, _, e = Derive(p); e == nil {
		t.Fatal("mismatch")
	}
	p = fixture()
	p.KnownQuantityRaw = "+5"
	if _, _, e = Derive(p); e == nil {
		t.Fatal("noncanonical")
	}
	p = fixture()
	p.UnknownQuantityRaw = "0"
	p.KnownQuantityRaw = "10"
	p.RealizedBasisQuality = "complete"
	got, _, e = Derive(p)
	if e != nil || *got.UnrealizedPnlRaw != "20000000" || *got.RealizedPnlRaw != "25000000" {
		t.Fatal(got, e)
	}
	p.MarkPrice.Denominator = "0"
	if _, _, e = Derive(p); e == nil {
		t.Fatal("zero denominator")
	}
}
func TestFlowAndDisplay(t *testing.T) {
	r, e := EquityChange("0", "100000000", "100000000", "0", "0", "0", true)
	if e != nil || *r.ProfitRaw != "0" || r.Return.Denominator != "" {
		t.Fatal(r, e)
	}
	r, e = EquityChange("0", "100000000", "0", "0", "0", "0", false)
	if e != nil || r.ProfitRaw != nil || r.Method != "unavailable" {
		t.Fatal(r, e)
	}
	q := &DisplayQuote{Numerator: "98", Denominator: "100", Provider: "synthetic arithmetic only", ObservedAt: time.Now()}
	d, e := ConvertDisplay("100000000", q)
	if e != nil || *d.UsdMicro != "98000000" {
		t.Fatal(d, e)
	}
	d, e = ConvertDisplay("100000000", nil)
	if e != nil || d.UsdMicro != nil {
		t.Fatal(d, e)
	}
}
func TestCursorPrivacy(t *testing.T) {
	key := []byte(strings.Repeat("x", 32))
	now := time.Now()
	c := Cursor{Wallet: wallet, Snapshot: "11111111-1111-4111-8111-111111111111", Filter: "all", Sort: "snapshot_ordinal_v1", After: 49, Limit: 50, Expires: now.Add(time.Hour).Unix()}
	s, e := SignCursor(key, c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyCursor(key, s, wallet, "all", c.Sort, now); e != nil {
		t.Fatal(e)
	}
	if _, e = VerifyCursor(key, s+"x", wallet, "all", c.Sort, now); e == nil {
		t.Fatal("tamper")
	}
	if _, e = VerifyCursor(key, s, token, "all", c.Sort, now); e == nil {
		t.Fatal("wallet substitution")
	}
	if _, e = VerifyCursor(key, s, wallet, "all", c.Sort, now.Add(2*time.Hour)); !errors.Is(e, ErrRestart) {
		t.Fatal(e)
	}
}
func TestPostgresSnapshotOutboxExport(t *testing.T) {
	dsn := os.Getenv("L17_TEST_DSN")
	if dsn == "" {
		t.Fatal("L17_TEST_DSN required")
	}
	ctx := context.Background()
	config, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	config.MaxConns = 2
	pool, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "kd_0a8c6564bc9ee1abcd02eca2_l17_test"
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	config.ConnConfig.RuntimeParams["search_path"] = schema
	owned, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	defer owned.Close()
	if _, e = owned.Exec(ctx, DDL); e != nil {
		t.Fatal(e)
	}
	repo := Repository{Pool: owned}
	admission, _ := NewAdmission(2, 1)
	repo.ReadAdmission = admission
	held, e := admission.Acquire(ctx, wallet)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = repo.Page(ctx, wallet, "11111111-1111-4111-8111-111111111111", -1, 50, time.Now()); !errors.Is(e, ErrBusy) {
		t.Fatal("repository bypassed admission", e)
	}
	held()

	now := time.Now()
	positions := make([]Position, 151)
	for i := range positions {
		positions[i] = fixture()
		positions[i].PositionID = fmt.Sprintf("synthetic-%06d", i)
	}
	w := fixture().SourceWatermark
	s, e := repo.CommitSnapshot(ctx, "synthetic:125:99:tx:log", Summary{Wallet: wallet, Watermark: w}, positions, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	if s.KnownRealizedSubtotalUsdlRaw != "3775000000" || s.RealizedPnlRaw != nil {
		t.Fatal(s)
	}
	retry, e := repo.CommitSnapshot(ctx, "synthetic:125:99:tx:log", Summary{Wallet: wallet, Watermark: w}, positions, now.Add(time.Hour))
	if e != nil || retry.SnapshotID != s.SnapshotID {
		t.Fatal(retry, e)
	}
	canonical := make([]Position, len(positions))
	for i, p := range positions {
		canonical[i], _, e = Derive(p)
		if e != nil {
			t.Fatal(e)
		}
	}
	normalizedRetry, e := repo.CommitSnapshot(ctx, "synthetic:125:99:tx:log", Summary{Wallet: wallet, Watermark: w}, canonical, now.Add(time.Hour))
	if e != nil || normalizedRetry.SnapshotID != s.SnapshotID {
		t.Fatal("normalized retry conflict", e)
	}
	changed := append([]Position(nil), positions...)
	changed[0].KnownRealizedSubtotalUsdlRaw = "1"
	if _, e = repo.CommitSnapshot(ctx, "synthetic:125:99:tx:log", Summary{Wallet: wallet, Watermark: w}, changed, now.Add(time.Hour)); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	first, e := repo.Page(ctx, wallet, s.SnapshotID, -1, 50, now)
	if e != nil {
		t.Fatal(e)
	}
	w.ProjectionRevision = "2"
	for i := range changed {
		changed[i].SourceWatermark = w
	}
	s2, e := repo.CommitSnapshot(ctx, "synthetic:125:100:tx:log", Summary{Wallet: wallet, Watermark: w}, changed, now.Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	second, e := repo.Page(ctx, wallet, s.SnapshotID, 49, 50, now)
	if e != nil || first.Positions[49].PositionID == second.Positions[0].PositionID || second.Positions[0].PositionID != "synthetic-000050" || second.Summary.Watermark.ProjectionRevision != "1" {
		t.Fatal("mixed revisions", e)
	}
	// A conflicting revision must roll back all rows and the corresponding outbox.
	if _, e = repo.CommitSnapshot(ctx, "synthetic:revision-conflict", Summary{Wallet: wallet, Watermark: w}, changed, now.Add(time.Hour)); e == nil {
		t.Fatal("revision uniqueness not enforced")
	}
	var orphan int
	if e = owned.QueryRow(ctx, "SELECT count(*) FROM portfolio_snapshots WHERE source_event='synthetic:revision-conflict'").Scan(&orphan); e != nil || orphan != 0 {
		t.Fatal("transaction leaked", e)
	}

	notices, e := repo.Pending(ctx, 100)
	if e != nil || len(notices) != 2 {
		t.Fatal("outbox", e)
	}
	if e = repo.Ack(ctx, s.SnapshotID); e != nil {
		t.Fatal(e)
	}
	if e = repo.Ack(ctx, s.SnapshotID); e != nil {
		t.Fatal(e)
	}
	notices, e = repo.Pending(ctx, 100)
	if e != nil || len(notices) != 1 {
		t.Fatal(e)
	}
	var notice Notice
	if e = json.Unmarshal(notices[0], &notice); e != nil || notice.SnapshotID != s2.SnapshotID {
		t.Fatal(e)
	}
	share, e := repo.Share(ctx, wallet, wallet, s.SnapshotID, false, now)
	if e != nil || share.Wallet != nil || share.RealizedPnlRaw != nil || !strings.Contains(share.Label, "unavailable") {
		t.Fatal(share, e)
	}
	if _, e = repo.Share(ctx, wallet, token, s.SnapshotID, false, now); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	dir := t.TempDir()
	manifest, e := repo.ExportJSON(ctx, dir, wallet, wallet, s.SnapshotID, now)
	if e != nil || manifest.Rows != 151 {
		t.Fatal(manifest, e)
	}
	data, e := os.ReadFile(filepath.Join(dir, manifest.Object))
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(data)
	if manifest.SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("digest")
	}

	if _, e = owned.Exec(ctx, HistoryDDL); e != nil {
		t.Fatal(e)
	}
	history := make([]HistoryRecord, 100001)
	decimals := 6
	for i := range history {
		history[i] = HistoryRecord{EventID: fmt.Sprintf("synthetic-history-%06d", i), Kind: "deposit", Asset: AssetRef{Kind: "erc20", ChainID: ptr("125"), Address: ptr(token), Decimals: &decimals}, AmountRaw: "100000000", BasisQuality: "unknown", Position: ChainPosition{ChainID: "125", Height: "99", BlockHash: w.BlockHash, TransactionHash: "0x" + strings.Repeat("b", 64), TransactionIndex: "0", LogIndex: fmt.Sprint(i)}}
	}
	if e = repo.CommitHistory(ctx, wallet, s.SnapshotID, "synthetic-history", history); e != nil {
		t.Fatal(e)
	}
	if e = repo.CommitHistory(ctx, wallet, s.SnapshotID, "synthetic-history", history); e != nil {
		t.Fatal(e)
	}
	historyDir := t.TempDir()
	historyManifest, e := repo.ExportHistory(ctx, historyDir, wallet, wallet, s.SnapshotID, "jsonl", now)
	if e != nil || historyManifest.Rows != 100001 {
		t.Fatal(historyManifest, e)
	}
	historyBytes, e := os.ReadFile(filepath.Join(historyDir, historyManifest.Object))
	if e != nil {
		t.Fatal(e)
	}
	historyDigest := sha256.Sum256(historyBytes)
	if historyManifest.SHA256 != hex.EncodeToString(historyDigest[:]) || strings.Count(string(historyBytes), "\n") != 100001 {
		t.Fatal("history export incomplete")
	}
	csvManifest, e := repo.ExportHistory(ctx, historyDir, wallet, wallet, s.SnapshotID, "csv", now)
	if e != nil || csvManifest.Rows != 100001 {
		t.Fatal(csvManifest, e)
	}
	if _, e = repo.HistoryPage(ctx, wallet, token, s.SnapshotID, -1, 100, now); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("history authorization", e)
	}
	history[0].AmountRaw = "1"
	if e = repo.CommitHistory(ctx, wallet, s.SnapshotID, "synthetic-history", history); !errors.Is(e, ErrConflict) {
		t.Fatal("history conflict", e)
	}
	historyCancel, historyCancelFn := context.WithCancel(ctx)
	historyCancelFn()
	canceledDir := t.TempDir()
	if _, e = repo.ExportHistory(historyCancel, canceledDir, wallet, wallet, s.SnapshotID, "jsonl", now); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	canceledEntries, e := os.ReadDir(canceledDir)
	if e != nil || len(canceledEntries) != 0 {
		t.Fatal("unfinished history object retained", e)
	}

	if _, e = owned.Exec(ctx, CardDDL); e != nil {
		t.Fatal(e)
	}
	card, e := repo.CreateCard(ctx, wallet, wallet, s.SnapshotID, "synthetic-card", false, now)
	if e != nil {
		t.Fatal(e)
	}
	sameCard, e := repo.CreateCard(ctx, wallet, wallet, s.SnapshotID, "synthetic-card", false, now)
	if e != nil || sameCard.ID != card.ID {
		t.Fatal("card retry", e)
	}
	if _, e = repo.PublicCard(ctx, card.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("draft revealed", e)
	}
	if e = repo.RenderCard(ctx, card.ID); e != nil {
		t.Fatal(e)
	}
	public, e := repo.PublicCard(ctx, card.ID)
	if e != nil || len(public.Image) < 8 || string(public.Image[:8]) != "\x89PNG\r\n\x1a\n" || public.Snapshot.Wallet != nil || public.Snapshot.RealizedPnlRaw != nil {
		t.Fatal("unsafe card", e)
	}
	if e = repo.RevokeCard(ctx, card.ID, wallet, token, now); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("revoke owner", e)
	}
	if e = repo.RevokeCard(ctx, card.ID, wallet, wallet, now); e != nil {
		t.Fatal(e)
	}
	if _, e = repo.PublicCard(ctx, card.ID); !errors.Is(e, ErrGone) {
		t.Fatal("revoked public card", e)
	}
	owner, e := repo.OwnerCard(ctx, card.ID, wallet, wallet)
	if e != nil || owner.State != "revoked" || !owner.PurgePending || owner.Image != nil {
		t.Fatal(owner, e)
	}
	effects, e := repo.PendingCardEffects(ctx, 100)
	if e != nil || len(effects) != 1 {
		t.Fatal(effects, e)
	}
	if e = repo.AckCardPurge(ctx, card.ID, now.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	owner, e = repo.OwnerCard(ctx, card.ID, wallet, wallet)
	if e != nil || owner.PurgePending {
		t.Fatal(owner, e)
	}
	if e = repo.RenderCard(ctx, card.ID); !errors.Is(e, ErrGone) {
		t.Fatal("render restored revoked card", e)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = repo.ExportJSON(canceled, dir, wallet, wallet, s.SnapshotID, now); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("partial leaked")
	}
	if _, e = repo.Page(ctx, wallet, s.SnapshotID, -1, 50, now.Add(2*time.Hour)); !errors.Is(e, ErrRestart) {
		t.Fatal(e)
	}
	summaryJSON, _ := json.Marshal(s)
	manifestJSON, _ := json.Marshal(manifest)
	t.Logf("synthetic arithmetic, real PostgreSQL: persisted_summary=%s pending=%s export=%s", summaryJSON, notices[0], manifestJSON)
}

func TestCacheScopeAndUnpricedSummary(t *testing.T) {
	if _, e := CacheKey(wallet, token, "1", true); !errors.Is(e, ErrUnauthorized) {
		t.Fatal(e)
	}
	public, e := CacheKey(wallet, "", "1", false)
	if e != nil {
		t.Fatal(e)
	}
	private, e := CacheKey(wallet, wallet, "1", true)
	if e != nil || private == public {
		t.Fatal("scope collision", e)
	}
	p := fixture()
	p.PricingQuality = "unpriced"
	p.MarkPrice = nil
	s, e := Aggregate(wallet, "", p.SourceWatermark, []Position{p})
	if e != nil || s.CompleteValueUsdlRaw != nil || s.PricedSubtotalUsdlRaw != "0" || s.PricingQuality != "unpriced" || s.RealizedPnlRaw != nil {
		t.Fatal(s, e)
	}
	p.SourceWatermark.ChainID = "1"
	if _, _, e = Derive(p); e == nil {
		t.Fatal("wrong chain")
	}
}

func TestSourceHeightAndQuarantine(t *testing.T) {
	p := fixture()
	p.MarkPrice.Source.Position.Height = "100"
	if _, _, e := Derive(p); e == nil {
		t.Fatal("future mark accepted")
	}
	p = fixture()
	p.MarkPrice.Source.Position.BlockHash = "0x" + strings.Repeat("c", 64)
	if _, _, e := Derive(p); e == nil {
		t.Fatal("same-height divergent block accepted")
	}
	p = fixture()
	p.SourceWatermark.Integrity = "quarantined"
	got, value, e := Derive(p)
	if e != nil || value != nil || got.UnrealizedPnlRaw != nil || got.PricingQuality != "stale" {
		t.Fatal(got, value, e)
	}
	s, e := Aggregate(wallet, "", p.SourceWatermark, []Position{p})
	if e != nil || s.CompleteValueUsdlRaw != nil {
		t.Fatal(s, e)
	}
}
func TestAdmissionSaturationCancellation(t *testing.T) {
	a, e := NewAdmission(2, 1)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	release, e := a.Acquire(ctx, wallet)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Acquire(ctx, wallet); !errors.Is(e, ErrBusy) {
		t.Fatal("per-wallet saturation", e)
	}
	release2, e := a.Acquire(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Acquire(ctx, "0x3333333333333333333333333333333333333333"); !errors.Is(e, ErrBusy) {
		t.Fatal("global saturation", e)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, e = a.Acquire(canceled, token); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	release()
	release()
	release2()
	a.mu.Lock()
	if a.total != 0 || len(a.wallets) != 0 {
		t.Fatal("budget leak")
	}
	a.mu.Unlock()
	start := make(chan struct{})
	done := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() {
			<-start
			r, e := a.Acquire(ctx, wallet)
			if e == nil {
				r()
			}
			done <- e
		}()
	}
	close(start)
	for i := 0; i < 32; i++ {
		e := <-done
		if e != nil && !errors.Is(e, ErrBusy) {
			t.Fatal(e)
		}
	}
}

func TestWholeTokenPriceDecimals(t *testing.T) {
	for _, decimals := range []int{0, 6, 18, 255} {
		p := fixture()
		p.MarkPrice.BaseAsset.Decimals = &decimals
		b := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
		if b.BitLen() > 256 {
			p.BalanceRaw = "1"
			p.KnownQuantityRaw = "1"
		} else {
			p.BalanceRaw = b.String()
			p.KnownQuantityRaw = b.String()
		}
		p.UnknownQuantityRaw = "0"
		_, value, e := Derive(p)
		if e != nil {
			t.Fatal(decimals, e)
		}
		expected := "3000000"
		if decimals == 255 {
			expected = "0"
		}
		if value.String() != expected {
			t.Fatal(decimals, value, expected)
		}
	}
	p := fixture()
	p.MarkPrice.BaseAsset.Decimals = nil
	if _, _, e := Derive(p); e == nil {
		t.Fatal("unknown decimals assumed")
	}
	p = fixture()
	p.MarkPrice.QuoteAsset.Address = ptr(token)
	if _, _, e := Derive(p); e == nil {
		t.Fatal("wrong USDL identity")
	}
	p = fixture()
	p.MarkPrice.Numerator = "6"
	p.MarkPrice.Denominator = "2"
	if _, _, e := Derive(p); e == nil {
		t.Fatal("unreduced human ratio")
	}
	p = fixture()
	six := 6
	p.MarkPrice.BaseAsset.Decimals = &six
	p.BalanceRaw = "1000001"
	p.KnownQuantityRaw = "1000001"
	p.UnknownQuantityRaw = "0"
	p.MarkPrice.Numerator = "1"
	p.MarkPrice.Denominator = "3"
	_, v, e := Derive(p)
	if e != nil || v.String() != "333333" {
		t.Fatal("rational floor", v, e)
	}
}

func TestTimeWeightedCashFlowBoundaries(t *testing.T) {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	periods := []ReturnPeriod{{Start: start, End: start.Add(time.Hour), OpeningEquityRaw: "100000000", ClosingEquityBeforeFlowRaw: "100000000", ExternalFlowAtEndRaw: "100000000", Complete: true}, {Start: start.Add(time.Hour), End: start.Add(2 * time.Hour), OpeningEquityRaw: "200000000", ClosingEquityBeforeFlowRaw: "220000000", ExternalFlowAtEndRaw: "0", Complete: true}}
	got, e := TimeWeightedReturn(periods)
	if e != nil || got.Method != "time_weighted_boundary_cash_flows_v1" || got.Return.Numerator != "1" || got.Return.Denominator != "10" {
		t.Fatal(got, e)
	}
	periods[1].Complete = false
	got, e = TimeWeightedReturn(periods)
	if e != nil || got.Return != nil || got.Method != "unavailable" {
		t.Fatal(got, e)
	}
	periods[1].Complete = true
	periods[0].OpeningEquityRaw = "0"
	got, e = TimeWeightedReturn(periods)
	if e != nil || got.Return != nil || got.Reason != "zero_opening_equity" {
		t.Fatal(got, e)
	}
}
