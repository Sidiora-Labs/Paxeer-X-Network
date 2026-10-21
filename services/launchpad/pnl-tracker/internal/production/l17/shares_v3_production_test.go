package l17

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
)

func sharingV3Fixture(t *testing.T) (*SharesV3, auth.IdentityPrincipal, Summary) {
	t.Helper()
	r := phase2Repository(t)
	ctx := context.Background()
	if _, e := r.Pool.Exec(ctx, SharesV3DDL); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	summary := phase2Commit(t, r, "share-v3-source", "1", 1)
	metadata := SnapshotHistoryMetadata{SourceEvent: "share-v3-canonical-history", Start: now.Add(-time.Hour), End: now, Quote: fixture().MarkPrice.QuoteAsset}
	tx, e := r.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.CommitHistoryMetadataTx(ctx, tx, wallet, summary.SnapshotID, metadata); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	authority := auth.HTTPIdentityAuthority{Endpoint: "https://identity.example", ServiceToken: strings.Repeat("k", 32)}
	service, e := NewSharesV3(r, &authority, []byte(strings.Repeat("s", 32)), "private_fixture", strings.Repeat("a", 64), strings.Repeat("b", 64), time.Now)
	if e != nil {
		t.Fatal(e)
	}
	account, e := opaqueID()
	if e != nil {
		t.Fatal(e)
	}
	session, e := opaqueID()
	if e != nil {
		t.Fatal(e)
	}
	principal := auth.IdentityPrincipal{AccountID: account, SessionID: session, Wallet: wallet, AuthEpoch: 3, ProofAt: now, AccessExpiresAt: now.Add(time.Hour)}
	return service, principal, summary
}
func TestShareV3ImmutableSelectedIntentAndPrivacy(t *testing.T) {
	s, p, summary := sharingV3Fixture(t)
	ctx := context.Background()
	selection := ShareSelectionV3{SnapshotID: summary.SnapshotID, Fields: []string{"knownRealizedSubtotalUsdlRaw", "historicalReturn"}, ExpiresAt: time.Now().Add(time.Hour)}
	preview, e := s.Preview(ctx, p, selection)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(preview.Share)
	if e != nil {
		t.Fatal(e)
	}
	if preview.Share.Wallet != nil || !bytes.Contains(raw, []byte(`"knownRealizedSubtotalUsdlRaw":"25000000"`)) || bytes.Contains(raw, []byte(`"completeValueUsdlRaw"`)) || bytes.Contains(raw, []byte(`"realizedPnlRaw"`)) || bytes.Contains(raw, []byte(p.Wallet)) {
		t.Fatal("selection/anonymous privacy", string(raw))
	}
	if _, _, e = s.Public(ctx, preview.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("guessed draft disclosed", e)
	}
	changed := p
	changed.AuthEpoch++
	if _, e = s.Publish(ctx, changed, SharePublishV3{ID: preview.ID, Intent: preview.Intent}, "publish-1"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("changed epoch accepted immutable owner intent", e)
	}
	other := *s
	other.realm = "production"
	if _, e = other.Publish(ctx, p, SharePublishV3{ID: preview.ID, Intent: preview.Intent}, "publish-1"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("realm substitution accepted", e)
	}
	tampered := SharePublishV3{ID: preview.ID, Intent: strings.Repeat("x", 43)}
	if _, e = s.Publish(ctx, p, tampered, "publish-1"); !errors.Is(e, ErrUnauthorized) {
		t.Fatal("changed intent accepted", e)
	}
	phase2Commit(t, s.repository, "share-v3-new-source", "2", 2)
	published, e := s.Publish(ctx, p, SharePublishV3{ID: preview.ID, Intent: preview.Intent}, "publish-1")
	if e != nil || published.State != "pending" || published.Share.Watermark.ProjectionRevision != "1" {
		t.Fatal("original immutable snapshot lost", published, e)
	}
	again, e := s.Publish(ctx, p, SharePublishV3{ID: preview.ID, Intent: preview.Intent}, "publish-1")
	if e != nil || again.ID != published.ID {
		t.Fatal("publish retry", again, e)
	}
	if _, e = s.Publish(ctx, p, SharePublishV3{ID: preview.ID, Intent: preview.Intent}, "publish-2"); !errors.Is(e, ErrConflict) {
		t.Fatal("changed publish key accepted", e)
	}
	hidden := p
	hidden.Wallet = token
	if _, e = s.Owner(ctx, hidden, preview.ID); !errors.Is(e, ErrNotFound) {
		t.Fatal("other wallet owner lookup", e)
	}
	worker := ShareWorkerV3{Repository: s.repository, Owner: "share-v3-real-worker", FontDir: t.TempDir()}
	if e = worker.RunOnce(ctx); e == nil {
		t.Fatal("missing required fonts silently rendered")
	}
	owner, e := s.Owner(ctx, p, preview.ID)
	if e != nil || owner.State != "pending" || owner.RenderAttempts != 1 {
		t.Fatal("failed render did not stay pending", owner, e)
	}
	revoked, e := s.Revoke(ctx, p, preview.ID)
	if e != nil || revoked.State != "revoked" || !revoked.PurgePending {
		t.Fatal("durable revoke", revoked, e)
	}
	if _, _, e = s.Public(ctx, preview.ID); !errors.Is(e, ErrGone) {
		t.Fatal("origin did not return410 before purge", e)
	}
	if e = worker.RunOnce(ctx); !errors.Is(e, ErrUnavailable) {
		t.Fatal("missing CDN authority acknowledged purge", e)
	}
	owner, e = s.Owner(ctx, p, preview.ID)
	if e != nil || !owner.PurgePending || owner.PurgeLastError == nil {
		t.Fatal("purge failure not persisted", owner, e)
	}
	router := chi.NewRouter()
	s.RegisterHTTP(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v3/portfolio/shares/"+preview.ID, nil))
	if response.Code != 410 {
		t.Fatal(response.Code, response.Body.String())
	}
	evidence, e := json.Marshal(struct {
		ReleaseDigest         string          `json:"releaseDigest"`
		SnapshotID            string          `json:"snapshotId"`
		Algorithm             string          `json:"algorithm"`
		ExpectedKnownRealized string          `json:"expectedKnownRealizedSubtotalUsdlRaw"`
		Public                json.RawMessage `json:"public"`
	}{s.release, summary.SnapshotID, Algorithm, "25000000", raw})
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("L17_SHARE_V3_EVIDENCE %s", evidence)
}
func TestShareV3RealFontPNGExpiryAndSelectedNull(t *testing.T) {
	s, p, summary := sharingV3Fixture(t)
	ctx := context.Background()
	fontDir := os.Getenv("L17_SHARE_FONT_DIR")
	if fontDir == "" {
		t.Fatal("L17_SHARE_FONT_DIR required with actual admitted Inter-Bold.ttf and Inter-Regular.ttf")
	}
	preview, e := s.Preview(ctx, p, ShareSelectionV3{SnapshotID: summary.SnapshotID, Fields: []string{"realizedPnlRaw"}, ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	if string(preview.Share.Values.RealizedPnlRaw) != "null" || len(preview.Share.Values.KnownRealizedSubtotalUsdlRaw) != 0 {
		t.Fatal("unknown realized basis replaced or unselected amounts included", preview.Share.Values)
	}
	if _, e = s.Publish(ctx, p, SharePublishV3{ID: preview.ID, Intent: preview.Intent}, "actual-png"); e != nil {
		t.Fatal(e)
	}
	worker := ShareWorkerV3{Repository: s.repository, Owner: "share-v3-font-worker", FontDir: fontDir}
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	public, image, e := s.Public(ctx, preview.ID)
	if e != nil || public.Wallet != nil {
		t.Fatal(public, e)
	}
	config, e := png.DecodeConfig(bytes.NewReader(image))
	if e != nil || config.Width != 1200 || config.Height != 630 {
		t.Fatal("actual PNG image", config, e)
	}
	raw, e := json.Marshal(public)
	if e != nil {
		t.Fatal(e)
	}
	for _, private := range []string{"accountId", "sessionId", "authEpoch", "snapshotId", "publish_key", "snapshot_digest", "completeValueUsdlRaw", "knownRealizedSubtotalUsdlRaw", p.Wallet} {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatal("private or unselected data leaked", private, string(raw))
		}
	}
	router := chi.NewRouter()
	s.RegisterHTTP(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v3/portfolio/shares/"+preview.ID+"/image.png", nil))
	if response.Code != 200 || response.Header().Get("Content-Type") != "image/png" || !bytes.Equal(response.Body.Bytes(), image) {
		t.Fatal("actual image public path", response.Code)
	}
	s.clock = func() time.Time { return preview.Share.ExpiresAt.Add(time.Second) }
	if _, _, e = s.Public(ctx, preview.ID); !errors.Is(e, ErrGone) {
		t.Fatal("logical expiry allowed cached image", e)
	}
	if _, e = s.repository.Pool.Exec(ctx, "UPDATE portfolio_shares_v3 SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", preview.ID); e != nil {
		t.Fatal(e)
	}
	if e = worker.ExpireOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var state string
	var empty bool
	if e = s.repository.Pool.QueryRow(ctx, "SELECT state,image IS NULL FROM portfolio_shares_v3 WHERE id=$1", preview.ID).Scan(&state, &empty); e != nil || state != "expired" || !empty {
		t.Fatal("durable expiry did not remove origin image", state, empty, e)
	}
	evidence, e := json.Marshal(struct {
		ReleaseDigest         string          `json:"releaseDigest"`
		SnapshotID            string          `json:"snapshotId"`
		Algorithm             string          `json:"algorithm"`
		ExpectedKnownRealized string          `json:"expectedKnownRealizedSubtotalUsdlRaw"`
		Public                json.RawMessage `json:"public"`
	}{s.release, summary.SnapshotID, Algorithm, "25000000", raw})
	if e != nil {
		t.Fatal(e)
	}
	t.Logf("L17_SHARE_V3_EVIDENCE %s", evidence)
}
func TestShareV3RejectsAmbiguousAndFinancialClientFields(t *testing.T) {
	for _, body := range []string{`{"snapshotId":"11111111-1111-4111-8111-111111111111","fields":["realizedPnlRaw"],"realizedPnlRaw":"999999999"}`, `{"id":"11111111-1111-4111-8111-111111111111","intent":"x","intent":"y"}`} {
		var target ShareSelectionV3
		if e := decodeBody(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &target); !errors.Is(e, ErrInvalid) {
			t.Fatal("client amounts/duplicate accepted", e)
		}
	}
	now := time.Now()
	for _, fields := range [][]string{{"wallet"}, {"realizedPnlRaw", "realizedPnlRaw"}, {}} {
		if e := validateSelectionV3(ShareSelectionV3{SnapshotID: "11111111-1111-4111-8111-111111111111", Fields: fields, ExpiresAt: now.Add(time.Hour)}, now); !errors.Is(e, ErrInvalid) {
			t.Fatal("field allowlist", fields, e)
		}
	}
}
