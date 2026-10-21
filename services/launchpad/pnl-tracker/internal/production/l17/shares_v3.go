package l17

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fogleman/gg"
	"image/png"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/jackc/pgx/v5"
)

const SharesV3DDL = `CREATE TABLE portfolio_shares_v3 (
 id uuid PRIMARY KEY, wallet text NOT NULL, account_id text NOT NULL, session_id text NOT NULL, auth_epoch bigint NOT NULL,
 realm text NOT NULL, release_digest text NOT NULL, interface_digest text NOT NULL,
 snapshot_id uuid NOT NULL REFERENCES portfolio_snapshots(id), public_snapshot jsonb NOT NULL, snapshot_digest text NOT NULL,
 intent text NOT NULL, preview_until timestamptz NOT NULL, expires_at timestamptz NOT NULL, publish_key text,
 state text NOT NULL CHECK(state IN ('draft','pending','public','revoked','expired')), image bytea,
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 3), created_at timestamptz NOT NULL,
 purge_requested_at timestamptz, purged_at timestamptz,
 UNIQUE(wallet,publish_key));
 CREATE INDEX portfolio_shares_v3_owner ON portfolio_shares_v3(wallet,created_at,id);
 CREATE INDEX portfolio_shares_v3_expiry ON portfolio_shares_v3(expires_at) WHERE state IN ('draft','pending','public');
 CREATE TABLE portfolio_share_outbox_v3 (
 event_id text PRIMARY KEY, share_id uuid NOT NULL REFERENCES portfolio_shares_v3(id),
 kind text NOT NULL CHECK(kind IN ('render','purge')), fence bigint NOT NULL DEFAULT 0,
 lease_owner text, lease_until timestamptz, attempts integer NOT NULL DEFAULT 0,
 next_attempt timestamptz NOT NULL DEFAULT clock_timestamp(), last_error text, published_at timestamptz);
 CREATE INDEX portfolio_share_outbox_v3_ready ON portfolio_share_outbox_v3(next_attempt) WHERE published_at IS NULL;`

type ShareSelectionV3 struct {
	SnapshotID    string    `json:"snapshotId"`
	Fields        []string  `json:"fields"`
	IncludeWallet bool      `json:"includeWallet"`
	ExpiresAt     time.Time `json:"expiresAt"`
}
type ShareValuesV3 struct {
	KnownRealizedSubtotalUsdlRaw json.RawMessage `json:"knownRealizedSubtotalUsdlRaw,omitempty"`
	RealizedPnlRaw               json.RawMessage `json:"realizedPnlRaw,omitempty"`
	CompleteValueUsdlRaw         json.RawMessage `json:"completeValueUsdlRaw,omitempty"`
	HistoricalReturn             json.RawMessage `json:"historicalReturn,omitempty"`
}
type PublicShareV3 struct {
	Version        int           `json:"version"`
	ID             string        `json:"id"`
	ExpiresAt      time.Time     `json:"expiresAt"`
	Fields         []string      `json:"fields"`
	Values         ShareValuesV3 `json:"values"`
	Wallet         *string       `json:"wallet,omitempty"`
	QuoteIdentity  AssetRef      `json:"quoteIdentity"`
	Decimals       int           `json:"decimals"`
	Methodology    string        `json:"methodology"`
	Watermark      Watermark     `json:"watermark"`
	RangeStart     time.Time     `json:"rangeStart"`
	RangeEnd       time.Time     `json:"rangeEnd"`
	BasisQuality   string        `json:"basisQuality"`
	PricingQuality string        `json:"pricingQuality"`
	Labels         []string      `json:"labels"`
}
type SharePreviewV3 struct {
	Version      int           `json:"version"`
	ID           string        `json:"id"`
	Intent       string        `json:"intent"`
	PreviewUntil time.Time     `json:"previewUntil"`
	Share        PublicShareV3 `json:"share"`
}
type SharePublishV3 struct {
	ID     string `json:"id"`
	Intent string `json:"intent"`
}
type OwnerShareV3 struct {
	Version          int           `json:"version"`
	ID               string        `json:"id"`
	State            string        `json:"state"`
	Share            PublicShareV3 `json:"share"`
	RenderAttempts   int           `json:"renderAttempts"`
	RenderExhausted  bool          `json:"renderExhausted"`
	PurgePending     bool          `json:"purgePending"`
	PrivacyIncident  bool          `json:"privacyIncident"`
	PurgeRequestedAt *time.Time    `json:"purgeRequestedAt,omitempty"`
	PurgedAt         *time.Time    `json:"purgedAt,omitempty"`
	PurgeLastError   *string       `json:"purgeLastError,omitempty"`
}
type SharesV3 struct {
	repository               Repository
	authority                auth.RequestIdentityAuthority
	key                      []byte
	realm, release, contract string
	clock                    func() time.Time
}

func NewSharesV3(repository Repository, authority auth.RequestIdentityAuthority, key []byte, realm, release, contract string, clock func() time.Time) (*SharesV3, error) {
	if repository.Pool == nil || authority == nil || len(key) < 32 || clock == nil || (realm != "production" && realm != "private_fixture") || len(release) != 64 || len(contract) != 64 {
		return nil, ErrUnavailable
	}
	if _, e := hex.DecodeString(release); e != nil {
		return nil, ErrInvalid
	}
	if _, e := hex.DecodeString(contract); e != nil {
		return nil, ErrInvalid
	}
	return &SharesV3{repository: repository, authority: authority, key: append([]byte(nil), key...), realm: realm, release: release, contract: contract, clock: clock}, nil
}
func shareDigestV3(raw []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if e := decoder.Decode(&value); e != nil {
		return "", e
	}
	canonical, e := json.Marshal(value)
	if e != nil {
		return "", e
	}
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}
func (s *SharesV3) intent(id, digest string, p auth.IdentityPrincipal, until, expiry time.Time) string {
	binding := struct {
		Version                                                        int
		ID, Digest, Account, Session, Wallet, Realm, Release, Contract string
		Epoch                                                          int64
		Until, Expiry                                                  time.Time
	}{3, id, digest, p.AccountID, p.SessionID, p.Wallet, s.realm, s.release, s.contract, p.AuthEpoch, until, expiry}
	raw, _ := json.Marshal(binding)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("portfolio-selected-share-v3\x00"))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func validateSelectionV3(selection ShareSelectionV3, now time.Time) error {
	if !validUUID(selection.SnapshotID) || len(selection.Fields) < 1 || len(selection.Fields) > 4 || selection.ExpiresAt.Before(now.Add(5*time.Minute)) || selection.ExpiresAt.After(now.Add(30*24*time.Hour)) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, field := range selection.Fields {
		switch field {
		case "knownRealizedSubtotalUsdlRaw", "realizedPnlRaw", "completeValueUsdlRaw", "historicalReturn":
		default:
			return ErrInvalid
		}
		if seen[field] {
			return ErrInvalid
		}
		seen[field] = true
	}
	return nil
}
func (s *SharesV3) Preview(ctx context.Context, p auth.IdentityPrincipal, selection ShareSelectionV3) (SharePreviewV3, error) {
	var out SharePreviewV3
	now := s.clock().UTC().Truncate(time.Microsecond)
	selection.ExpiresAt = selection.ExpiresAt.UTC().Truncate(time.Microsecond)
	if e := p.Validate(now); e != nil {
		return out, e
	}
	if e := validateSelectionV3(selection, now); e != nil {
		return out, e
	}
	source, e := s.repository.ShareV2(ctx, p.Wallet, p.Wallet, selection.SnapshotID, selection.IncludeWallet, now)
	if e != nil {
		return out, e
	}
	if source.RangeStart == nil || source.RangeEnd == nil || source.QuoteIdentity == nil {
		return out, ErrUnavailable
	}
	id, e := opaqueID()
	if e != nil {
		return out, e
	}
	fields := append([]string(nil), selection.Fields...)
	sort.Strings(fields)
	public := PublicShareV3{Version: 3, ID: id, ExpiresAt: selection.ExpiresAt.UTC(), Fields: fields, Wallet: source.Wallet, QuoteIdentity: *source.QuoteIdentity, Decimals: source.Decimals, Methodology: source.Methodology, Watermark: source.Watermark, RangeStart: *source.RangeStart, RangeEnd: *source.RangeEnd, BasisQuality: source.BasisQuality, PricingQuality: source.PricingQuality, Labels: []string{}}
	for _, field := range fields {
		switch field {
		case "knownRealizedSubtotalUsdlRaw":
			public.Values.KnownRealizedSubtotalUsdlRaw, e = json.Marshal(source.KnownRealizedSubtotalUsdlRaw)
			public.Labels = append(public.Labels, "Known realized subtotal (USDL); total profit unavailable")
		case "realizedPnlRaw":
			public.Values.RealizedPnlRaw, e = json.Marshal(source.RealizedPnlRaw)
			public.Labels = append(public.Labels, "Realized result (USDL); unavailable when historical basis is incomplete")
		case "completeValueUsdlRaw":
			public.Values.CompleteValueUsdlRaw, e = json.Marshal(source.CompleteValueUsdlRaw)
			public.Labels = append(public.Labels, "Current portfolio value (USDL); value is not trading profit")
		case "historicalReturn":
			public.Values.HistoricalReturn, e = json.Marshal(source.HistoricalReturn)
			public.Labels = append(public.Labels, "Historical time-weighted return; unavailable without complete dated inputs")
		}
		if e != nil {
			return out, e
		}
	}
	raw, e := json.Marshal(public)
	if e != nil {
		return out, e
	}
	digest, e := shareDigestV3(raw)
	if e != nil {
		return out, e
	}
	until := now.Add(10 * time.Minute)
	if until.After(public.ExpiresAt) {
		until = public.ExpiresAt
	}
	intent := s.intent(id, digest, p, until, public.ExpiresAt)
	tx, e := s.repository.Pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "share-v3:"+p.Wallet); e != nil {
		return out, e
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM portfolio_shares_v3 WHERE wallet=$1 AND created_at>$2", p.Wallet, now.Add(-time.Minute)).Scan(&count); e != nil {
		return out, e
	}
	if count >= 5 {
		return out, ErrBusy
	}
	_, e = tx.Exec(ctx, `INSERT INTO portfolio_shares_v3(id,wallet,account_id,session_id,auth_epoch,realm,release_digest,interface_digest,snapshot_id,public_snapshot,snapshot_digest,intent,preview_until,expires_at,state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'draft',$15)`, id, p.Wallet, p.AccountID, p.SessionID, p.AuthEpoch, s.realm, s.release, s.contract, selection.SnapshotID, raw, digest, intent, until, public.ExpiresAt, now)
	if e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return SharePreviewV3{Version: 3, ID: id, Intent: intent, PreviewUntil: until, Share: public}, nil
}
func (s *SharesV3) Publish(ctx context.Context, p auth.IdentityPrincipal, request SharePublishV3, key string) (OwnerShareV3, error) {
	var out OwnerShareV3
	now := s.clock().UTC().Truncate(time.Microsecond)
	if e := p.Validate(now); e != nil {
		return out, e
	}
	if !validUUID(request.ID) || len(request.Intent) != 43 || len(key) < 1 || len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
		return out, ErrInvalid
	}
	tx, e := s.repository.Pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "share-v3:"+p.Wallet); e != nil {
		return out, e
	}
	var stored auth.IdentityPrincipal
	var realm, release, contract, digest, intent, state string
	var raw []byte
	var until, expiry time.Time
	var priorKey *string
	e = tx.QueryRow(ctx, `SELECT account_id,session_id,wallet,auth_epoch,realm,release_digest,interface_digest,snapshot_digest,intent,state,public_snapshot,preview_until,expires_at,publish_key FROM portfolio_shares_v3 WHERE id=$1 AND wallet=$2 FOR UPDATE`, request.ID, p.Wallet).Scan(&stored.AccountID, &stored.SessionID, &stored.Wallet, &stored.AuthEpoch, &realm, &release, &contract, &digest, &intent, &state, &raw, &until, &expiry, &priorKey)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if realm != s.realm || release != s.release || contract != s.contract || stored.AccountID != p.AccountID || stored.SessionID != p.SessionID || stored.AuthEpoch != p.AuthEpoch || !hmac.Equal([]byte(request.Intent), []byte(intent)) || !hmac.Equal([]byte(intent), []byte(s.intent(request.ID, digest, p, until, expiry))) {
		return out, ErrUnauthorized
	}
	var public PublicShareV3
	if e = json.Unmarshal(raw, &public); e != nil {
		return out, e
	}
	canonical, e := json.Marshal(public)
	if e != nil {
		return out, e
	}
	actual, e := shareDigestV3(canonical)
	if e != nil || digest != actual {
		return out, ErrConflict
	}
	if !now.Before(expiry) {
		return out, ErrGone
	}
	if priorKey != nil {
		if *priorKey != key {
			return out, ErrConflict
		}
		if e = tx.Commit(ctx); e != nil {
			return out, e
		}
		return s.Owner(ctx, p, request.ID)
	}
	if state != "draft" {
		return out, ErrConflict
	}
	if !now.Before(until) {
		return out, ErrGone
	}
	var previousID string
	e = tx.QueryRow(ctx, "SELECT id::text FROM portfolio_shares_v3 WHERE wallet=$1 AND publish_key=$2", p.Wallet, key).Scan(&previousID)
	if e == nil {
		return out, ErrConflict
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_shares_v3 SET state='pending',publish_key=$2 WHERE id=$1", request.ID, key); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_share_outbox_v3(event_id,share_id,kind) VALUES($1,$2,'render')", "render:"+request.ID, request.ID); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return s.Owner(ctx, p, request.ID)
}
func (s *SharesV3) Owner(ctx context.Context, p auth.IdentityPrincipal, id string) (OwnerShareV3, error) {
	out := OwnerShareV3{Version: 3}
	now := s.clock().UTC().Truncate(time.Microsecond)
	if e := p.Validate(now); e != nil {
		return out, e
	}
	if !validUUID(id) {
		return out, ErrNotFound
	}
	var raw []byte
	e := s.repository.Pool.QueryRow(ctx, `SELECT c.id::text,c.state,c.public_snapshot,c.attempts,c.purge_requested_at,c.purged_at,o.last_error FROM portfolio_shares_v3 c LEFT JOIN portfolio_share_outbox_v3 o ON o.share_id=c.id AND o.kind='purge' WHERE c.id=$1 AND c.wallet=$2 AND c.account_id=$3 AND c.realm=$4`, id, p.Wallet, p.AccountID, s.realm).Scan(&out.ID, &out.State, &raw, &out.RenderAttempts, &out.PurgeRequestedAt, &out.PurgedAt, &out.PurgeLastError)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(raw, &out.Share); e != nil {
		return out, e
	}
	if !now.Before(out.Share.ExpiresAt) && out.State != "revoked" {
		out.State = "expired"
	}
	out.RenderExhausted = out.State == "pending" && out.RenderAttempts >= 3
	out.PurgePending = out.PurgeRequestedAt != nil && out.PurgedAt == nil
	out.PrivacyIncident = out.PurgePending && now.Sub(*out.PurgeRequestedAt) > 300*time.Second
	return out, nil
}
func (s *SharesV3) Revoke(ctx context.Context, p auth.IdentityPrincipal, id string) (OwnerShareV3, error) {
	now := s.clock().UTC().Truncate(time.Microsecond)
	if e := p.Validate(now); e != nil {
		return OwnerShareV3{}, e
	}
	if !validUUID(id) {
		return OwnerShareV3{}, ErrNotFound
	}
	tx, e := s.repository.Pool.Begin(ctx)
	if e != nil {
		return OwnerShareV3{}, e
	}
	defer tx.Rollback(ctx)
	var key *string
	e = tx.QueryRow(ctx, `UPDATE portfolio_shares_v3 SET state='revoked',image=NULL,purge_requested_at=CASE WHEN publish_key IS NULL THEN NULL ELSE COALESCE(purge_requested_at,$5) END WHERE id=$1 AND wallet=$2 AND account_id=$3 AND realm=$4 RETURNING publish_key`, id, p.Wallet, p.AccountID, s.realm, now).Scan(&key)
	if errors.Is(e, pgx.ErrNoRows) {
		return OwnerShareV3{}, ErrNotFound
	}
	if e != nil {
		return OwnerShareV3{}, e
	}
	if key != nil {
		if _, e = tx.Exec(ctx, "INSERT INTO portfolio_share_outbox_v3(event_id,share_id,kind) VALUES($1,$2,'purge') ON CONFLICT(event_id) DO NOTHING", "purge:"+id, id); e != nil {
			return OwnerShareV3{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return OwnerShareV3{}, e
	}
	return s.Owner(ctx, p, id)
}
func (s *SharesV3) Public(ctx context.Context, id string) (PublicShareV3, []byte, error) {
	var out PublicShareV3
	if !validUUID(id) {
		return out, nil, ErrNotFound
	}
	var raw, image []byte
	var state, digest string
	var expiry time.Time
	var publishKey *string
	e := s.repository.Pool.QueryRow(ctx, "SELECT public_snapshot,image,state,expires_at,snapshot_digest,publish_key FROM portfolio_shares_v3 WHERE id=$1", id).Scan(&raw, &image, &state, &expiry, &digest, &publishKey)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, nil, ErrNotFound
	}
	if e != nil {
		return out, nil, e
	}
	if publishKey == nil {
		return out, nil, ErrNotFound
	}
	if state == "revoked" || state == "expired" || !s.clock().Before(expiry) {
		return out, nil, ErrGone
	}
	if state != "public" {
		return out, nil, ErrNotFound
	}
	if e = json.Unmarshal(raw, &out); e != nil {
		return out, nil, e
	}
	actual, e := shareDigestV3(raw)
	if e != nil || actual != digest {
		return PublicShareV3{}, nil, ErrConflict
	}
	return out, image, nil
}

type ShareWorkerV3 struct {
	Repository     Repository
	Owner, FontDir string
	Purger         SharePurgerV3
}
type SharePurgerV3 interface {
	PurgeShare(context.Context, string) error
}

func (w ShareWorkerV3) ExpireOnce(ctx context.Context) error {
	tx, e := w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	_, e = tx.Exec(ctx, `WITH expired AS (SELECT id FROM portfolio_shares_v3 WHERE expires_at<=clock_timestamp() AND state IN ('draft','pending','public') ORDER BY expires_at FOR UPDATE SKIP LOCKED LIMIT 100), updated AS (UPDATE portfolio_shares_v3 s SET state='expired',image=NULL,purge_requested_at=CASE WHEN publish_key IS NULL THEN NULL ELSE COALESCE(purge_requested_at,clock_timestamp()) END FROM expired e WHERE s.id=e.id RETURNING s.id,s.publish_key) INSERT INTO portfolio_share_outbox_v3(event_id,share_id,kind) SELECT 'purge:'||id::text,id,'purge' FROM updated WHERE publish_key IS NOT NULL ON CONFLICT(event_id) DO NOTHING`)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (w ShareWorkerV3) RunOnce(ctx context.Context) error {
	if !workerName.MatchString(w.Owner) {
		return ErrInvalid
	}
	tx, e := w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var event, id, kind string
	var fence int64
	var raw []byte
	e = tx.QueryRow(ctx, `WITH candidate AS (SELECT o.event_id FROM portfolio_share_outbox_v3 o JOIN portfolio_shares_v3 s ON s.id=o.share_id WHERE o.published_at IS NULL AND o.next_attempt<=clock_timestamp() AND (o.lease_until IS NULL OR o.lease_until<clock_timestamp()) AND (o.kind='purge' OR (s.state='pending' AND s.attempts<3 AND s.expires_at>clock_timestamp())) ORDER BY o.next_attempt FOR UPDATE OF o SKIP LOCKED LIMIT 1) UPDATE portfolio_share_outbox_v3 o SET fence=fence+1,lease_owner=$1,lease_until=clock_timestamp()+interval '30 seconds',attempts=attempts+1 FROM candidate c WHERE o.event_id=c.event_id RETURNING o.event_id,o.share_id::text,o.kind,o.fence`, w.Owner).Scan(&event, &id, &kind, &fence)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if kind == "render" {
		tag, e := tx.Exec(ctx, "UPDATE portfolio_shares_v3 SET attempts=attempts+1 WHERE id=$1 AND state='pending' AND attempts<3 AND expires_at>clock_timestamp()", id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		if e = tx.QueryRow(ctx, "SELECT public_snapshot FROM portfolio_shares_v3 WHERE id=$1", id).Scan(&raw); e != nil {
			return e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	work, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var image []byte
	if kind == "purge" {
		if w.Purger == nil {
			e = ErrUnavailable
		} else {
			e = w.Purger.PurgeShare(work, id)
		}
	} else {
		var share PublicShareV3
		if e = json.Unmarshal(raw, &share); e == nil {
			var renderer *ogrender.Renderer
			renderer, e = ogrender.NewStrict(w.FontDir)
			if e == nil {
				image, e = renderSelectedV3(renderer, share, w.FontDir)
			}
		}
	}
	if e != nil {
		_, save := w.Repository.Pool.Exec(ctx, "UPDATE portfolio_share_outbox_v3 SET last_error='effect_failed',next_attempt=clock_timestamp()+interval '30 seconds',lease_owner=NULL,lease_until=NULL WHERE event_id=$1 AND fence=$2 AND lease_owner=$3", event, fence, w.Owner)
		return errors.Join(e, save)
	}
	tx, e = w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var live bool
	if e = tx.QueryRow(ctx, "SELECT lease_until>clock_timestamp() FROM portfolio_share_outbox_v3 WHERE event_id=$1 AND fence=$2 AND lease_owner=$3 FOR UPDATE", event, fence, w.Owner).Scan(&live); e != nil {
		return e
	}
	if !live {
		return ErrConflict
	}
	if kind == "render" {
		tag, e := tx.Exec(ctx, "UPDATE portfolio_shares_v3 SET image=$2,state='public' WHERE id=$1 AND state='pending' AND expires_at>clock_timestamp()", id, image)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrGone
		}
	} else {
		if _, e = tx.Exec(ctx, "UPDATE portfolio_shares_v3 SET purged_at=clock_timestamp() WHERE id=$1 AND state IN ('revoked','expired')", id); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_share_outbox_v3 SET published_at=clock_timestamp(),last_error=NULL,lease_owner=NULL,lease_until=NULL WHERE event_id=$1 AND fence=$2 AND lease_owner=$3", event, fence, w.Owner); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func formatShareAmountV3(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "Unavailable", nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrInvalid
	}
	n, e := number(value, true, 156)
	if e != nil {
		return "", e
	}
	absolute := new(big.Int).Abs(n)
	whole, fraction := new(big.Int), new(big.Int)
	whole.QuoRem(absolute, big.NewInt(1000000), fraction)
	sign := ""
	if n.Sign() < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%s.%06d USDL", sign, whole.String(), fraction.Int64()), nil
}
func renderSelectedV3(renderer *ogrender.Renderer, share PublicShareV3, fontDir string) ([]byte, error) {
	lines := []string{}
	for _, field := range share.Fields {
		var value string
		var e error
		switch field {
		case "knownRealizedSubtotalUsdlRaw":
			value, e = formatShareAmountV3(share.Values.KnownRealizedSubtotalUsdlRaw)
			value = "Known realized: " + value
		case "realizedPnlRaw":
			value, e = formatShareAmountV3(share.Values.RealizedPnlRaw)
			value = "Realized result: " + value
		case "completeValueUsdlRaw":
			value, e = formatShareAmountV3(share.Values.CompleteValueUsdlRaw)
			value = "Current value: " + value
		case "historicalReturn":
			var result HistoricalReturn
			if json.Unmarshal(share.Values.HistoricalReturn, &result) != nil {
				return nil, ErrInvalid
			}
			value = "Historical return unavailable"
			if result.Return != nil {
				value = "Historical return " + result.Return.Numerator + "/" + result.Return.Denominator + "; " + result.Method
			}
		default:
			return nil, ErrInvalid
		}
		if e != nil {
			return nil, e
		}
		lines = append(lines, value)
	}
	if len(lines) < 1 || len(lines) > 4 {
		return nil, ErrInvalid
	}
	base, e := renderer.Render(ogrender.Input{})
	if e != nil {
		return nil, e
	}
	decoded, e := png.Decode(bytes.NewReader(base))
	if e != nil {
		return nil, e
	}
	dc := gg.NewContextForImage(decoded)
	dc.SetHexColor("#0B0B0F")
	dc.DrawRectangle(0, 20, 1200, 610)
	dc.Fill()
	draw := func(text string, y, size float64, bold bool) error {
		face := "Inter-Regular.ttf"
		if bold {
			face = "Inter-Bold.ttf"
		}
		for points := size; points >= 14; points -= 1 {
			if e := dc.LoadFontFace(filepath.Join(fontDir, face), points); e != nil {
				return e
			}
			width, _ := dc.MeasureString(text)
			if width <= 1040 {
				dc.SetHexColor("#FFFFFF")
				dc.DrawStringAnchored(text, 80, y, 0, 0.5)
				return nil
			}
		}
		return ErrInvalid
	}
	if e = draw("Selected portfolio data", 70, 44, true); e != nil {
		return nil, e
	}
	for i, line := range lines {
		if e = draw(line, 145+float64(i)*75, 40, true); e != nil {
			return nil, e
		}
	}
	if e = draw("Basis "+share.BasisQuality+"; pricing "+share.PricingQuality+"; total profit not asserted", 475, 25, false); e != nil {
		return nil, e
	}
	identity := "Anonymous"
	if share.Wallet != nil {
		identity = *share.Wallet
	}
	if e = draw(identity, 515, 22, false); e != nil {
		return nil, e
	}
	footer := share.RangeStart.UTC().Format("2006-01-02") + " .. " + share.RangeEnd.UTC().Format("2006-01-02") + " UTC; " + share.Methodology + "; block " + share.Watermark.Height + "; rev " + share.Watermark.ProjectionRevision
	if e = draw(footer, 552, 24, false); e != nil {
		return nil, e
	}
	if e = draw("Expires "+share.ExpiresAt.UTC().Format(time.RFC3339), 590, 22, false); e != nil {
		return nil, e
	}
	var output bytes.Buffer
	if e = dc.EncodePNG(&output); e != nil {
		return nil, e
	}
	image := output.Bytes()
	config, e := png.DecodeConfig(bytes.NewReader(image))
	if e != nil || config.Width != 1200 || config.Height != 630 || len(image) > 2000000 {
		return nil, ErrInvalid
	}
	return image, nil
}

type CloudflareSharePurgerV3 struct {
	ZoneID, Token, PublicOrigin string
	Client                      *http.Client
}

func (p CloudflareSharePurgerV3) PurgeShare(ctx context.Context, id string) error {
	if !validUUID(id) || !cloudflareZone.MatchString(p.ZoneID) || p.Token == "" {
		return ErrUnavailable
	}
	origin, e := url.Parse(p.PublicOrigin)
	if e != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return ErrInvalid
	}
	base := strings.TrimRight(p.PublicOrigin, "/") + "/v3/portfolio/shares/" + id
	urls := []string{base, base + "/image.png"}
	body, e := json.Marshal(struct {
		Files []string `json:"files"`
	}{urls})
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	client := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if p.Client != nil {
		client.Transport = p.Client.Transport
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.cloudflare.com/client/v4/zones/"+p.ZoneID+"/purge_cache", bytes.NewReader(body))
	if e != nil {
		return e
	}
	request.Header.Set("Authorization", "Bearer "+p.Token)
	request.Header.Set("Content-Type", "application/json")
	response, e := client.Do(request)
	if e != nil {
		return ErrUnavailable
	}
	var result struct {
		Success bool `json:"success"`
	}
	decode := json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result)
	response.Body.Close()
	if response.StatusCode != 200 || decode != nil || !result.Success {
		return ErrUnavailable
	}
	for _, target := range urls {
		check, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if e != nil {
			return e
		}
		actual, e := client.Do(check)
		if e != nil {
			return ErrUnavailable
		}
		actual.Body.Close()
		if actual.StatusCode != http.StatusGone {
			return ErrUnavailable
		}
	}
	return nil
}
