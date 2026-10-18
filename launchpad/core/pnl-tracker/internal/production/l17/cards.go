package l17

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/jackc/pgx/v5"
)

var ErrGone = errors.New("card revoked")
var ErrNotFound = errors.New("card not found")
var ErrRenderExhausted = errors.New("card render retry limit reached")

const CardDDL = `CREATE TABLE portfolio_cards (
 id uuid PRIMARY KEY, source_event text NOT NULL UNIQUE, wallet text NOT NULL,
 snapshot_id uuid NOT NULL REFERENCES portfolio_snapshots(id), snapshot jsonb NOT NULL,
 state text NOT NULL CHECK(state IN ('pending','public','revoked')),
 image bytea, attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 3),
 created_at timestamptz NOT NULL, purge_requested_at timestamptz, purged_at timestamptz);
 CREATE INDEX portfolio_cards_owner_created ON portfolio_cards(wallet,created_at);
 CREATE TABLE portfolio_card_outbox (
 event_id text PRIMARY KEY,card_id uuid NOT NULL REFERENCES portfolio_cards(id),
 kind text NOT NULL CHECK(kind IN ('render','purge')),published_at timestamptz);`

type StoredCard struct {
	ID               string         `json:"id"`
	State            string         `json:"state"`
	Snapshot         *ShareSnapshot `json:"snapshot,omitempty"`
	Image            []byte         `json:"-"`
	PurgePending     bool           `json:"purgePending"`
	PurgeRequestedAt *time.Time     `json:"purgeRequestedAt,omitempty"`
	PrivacyIncident  bool           `json:"privacyIncident"`
	PurgeLastError   *string        `json:"purgeLastError,omitempty"`
	RenderAttempts   int            `json:"renderAttempts"`
	RenderExhausted  bool           `json:"renderExhausted"`
}

func (r Repository) CreateCard(ctx context.Context, wallet, verifiedWallet, snapshot, sourceEvent string, includeWallet bool, now time.Time) (StoredCard, error) {
	return r.createCard(ctx, wallet, verifiedWallet, snapshot, sourceEvent, includeWallet, now, false)
}
func (r Repository) CreateCardV2(ctx context.Context, wallet, verifiedWallet, snapshot, sourceEvent string, includeWallet bool, now time.Time) (StoredCard, error) {
	return r.createCard(ctx, wallet, verifiedWallet, snapshot, sourceEvent, includeWallet, now, true)
}
func (r Repository) createCard(ctx context.Context, wallet, verifiedWallet, snapshot, sourceEvent string, includeWallet bool, now time.Time, v2 bool) (StoredCard, error) {
	var out StoredCard
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return out, e
	}
	wallet, _ = Wallet(wallet)
	if sourceEvent == "" || len(sourceEvent) > 256 {
		return out, ErrInvalid
	}
	if !validUUID(snapshot) || now.IsZero() {
		return out, ErrInvalid
	}
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return out, e
	}
	defer rollbackPortfolio(tx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "card-source:"+sourceEvent); e != nil {
		return out, e
	}
	var priorWallet, priorSnapshot string
	var prior []byte
	e = tx.QueryRow(ctx, "SELECT id::text,wallet,snapshot_id::text,snapshot,state FROM portfolio_cards WHERE source_event=$1", sourceEvent).Scan(&out.ID, &priorWallet, &priorSnapshot, &prior, &out.State)
	if e == nil {
		var canonical ShareSnapshot
		version := 1
		if v2 {
			version = 2
		}
		if json.Unmarshal(prior, &canonical) != nil || priorWallet != wallet || priorSnapshot != snapshot || canonical.Version != version || (canonical.Wallet != nil) != includeWallet || (canonical.Wallet != nil && *canonical.Wallet != wallet) {
			return out, ErrConflict
		}
		return out, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "card:"+wallet); e != nil {
		return out, e
	}
	var share ShareSnapshot
	if v2 {
		share, e = r.ShareV2(ctx, wallet, verifiedWallet, snapshot, includeWallet, now)
	} else {
		share, e = r.Share(ctx, wallet, verifiedWallet, snapshot, includeWallet, now)
	}
	if e != nil {
		return out, e
	}
	data, e := json.Marshal(share)
	if e != nil {
		return out, e
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM portfolio_cards WHERE wallet=$1 AND created_at>$2", wallet, now.Add(-time.Minute)).Scan(&count); e != nil {
		return out, e
	}
	if count >= 5 {
		return out, ErrBusy
	}
	out.ID, e = opaqueID()
	if e != nil {
		return out, e
	}
	out.State = "pending"
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_cards(id,source_event,wallet,snapshot_id,snapshot,state,created_at) VALUES($1,$2,$3,$4,$5,'pending',$6)", out.ID, sourceEvent, wallet, snapshot, data, now); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_card_outbox(event_id,card_id,kind) VALUES($1,$2,'render')", "render:"+out.ID, out.ID); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

func (r Repository) RenderCard(ctx context.Context, id string) error {
	var raw []byte
	var state string
	var attempts int
	e := r.Pool.QueryRow(ctx, "SELECT snapshot,state,attempts FROM portfolio_cards WHERE id=$1", id).Scan(&raw, &state, &attempts)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if state == "revoked" {
		return ErrGone
	}
	if state == "public" {
		return nil
	}
	if attempts >= 3 {
		return ErrRenderExhausted
	}
	tag, e := r.Pool.Exec(ctx, "UPDATE portfolio_cards SET attempts=attempts+1 WHERE id=$1 AND state='pending' AND attempts=$2 AND attempts<3", id, attempts)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrBusy
	}
	var share ShareSnapshot
	if e = json.Unmarshal(raw, &share); e != nil {
		return e
	}
	png, e := renderShare(ogrender.New(""), share)
	if e != nil {
		return e
	}
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	tag, e = tx.Exec(ctx, "UPDATE portfolio_cards SET image=$2,state='public' WHERE id=$1 AND state='pending'", id, png)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrGone
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_card_outbox SET published_at=now() WHERE event_id=$1", "render:"+id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r Repository) PublicCard(ctx context.Context, id string) (StoredCard, error) {
	if !validUUID(id) {
		return StoredCard{}, ErrNotFound
	}
	var out StoredCard
	var raw []byte
	e := r.Pool.QueryRow(ctx, "SELECT id::text,state,snapshot,image FROM portfolio_cards WHERE id=$1", id).Scan(&out.ID, &out.State, &raw, &out.Image)
	if errors.Is(e, pgx.ErrNoRows) {
		return StoredCard{}, ErrNotFound
	}
	if e != nil {
		return StoredCard{}, e
	}
	if out.State == "revoked" {
		return StoredCard{ID: out.ID, State: out.State}, ErrGone
	}
	if out.State != "public" {
		return StoredCard{}, ErrNotFound
	}
	var share ShareSnapshot
	if e = json.Unmarshal(raw, &share); e != nil {
		return StoredCard{}, e
	}
	out.Snapshot = &share
	return out, nil
}

func (r Repository) RevokeCard(ctx context.Context, id, wallet, verifiedWallet string, now time.Time) error {
	if !validUUID(id) {
		return ErrNotFound
	}
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return e
	}
	wallet, _ = Wallet(wallet)
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	tag, e := tx.Exec(ctx, "UPDATE portfolio_cards SET state='revoked',image=NULL,purge_requested_at=COALESCE(purge_requested_at,$3) WHERE id=$1 AND wallet=$2", id, wallet, now)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_card_outbox SET published_at=$2 WHERE card_id=$1 AND kind='render' AND published_at IS NULL", id, now); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO portfolio_card_outbox(event_id,card_id,kind) VALUES($1,$2,'purge') ON CONFLICT(event_id) DO NOTHING", "purge:"+id, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (r Repository) OwnerCard(ctx context.Context, id, wallet, verifiedWallet string) (StoredCard, error) {
	if !validUUID(id) {
		return StoredCard{}, ErrNotFound
	}
	var out StoredCard
	if e := RequireOwner(wallet, verifiedWallet); e != nil {
		return out, e
	}
	wallet, _ = Wallet(wallet)
	e := r.Pool.QueryRow(ctx, "SELECT id::text,state,(purge_requested_at IS NOT NULL AND purged_at IS NULL) FROM portfolio_cards WHERE id=$1 AND wallet=$2", id, wallet).Scan(&out.ID, &out.State, &out.PurgePending)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	return out, e
}

func (r Repository) AckCardPurge(ctx context.Context, id string, now time.Time) error {
	tx, e := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	tag, e := tx.Exec(ctx, "UPDATE portfolio_cards SET purged_at=$2 WHERE id=$1 AND state='revoked' AND purge_requested_at IS NOT NULL", id, now)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_card_outbox SET published_at=$2 WHERE event_id=$1", "purge:"+id, now); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

type CardEffect struct {
	EventID string
	CardID  string
	Kind    string
}

func (r Repository) PendingCardEffects(ctx context.Context, limit int) ([]CardEffect, error) {
	limit, e := Limit(limit)
	if e != nil {
		return nil, e
	}
	rows, e := r.Pool.Query(ctx, "SELECT event_id,card_id::text,kind FROM portfolio_card_outbox WHERE published_at IS NULL ORDER BY event_id LIMIT $1", limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []CardEffect{}
	for rows.Next() {
		var effect CardEffect
		if e = rows.Scan(&effect.EventID, &effect.CardID, &effect.Kind); e != nil {
			return nil, e
		}
		out = append(out, effect)
	}
	return out, rows.Err()
}

func renderShare(renderer *ogrender.Renderer, share ShareSnapshot) ([]byte, error) {
	realized, e := number(share.KnownRealizedSubtotalUsdlRaw, true, 156)
	if e != nil {
		return nil, e
	}
	abs := new(big.Int).Abs(realized)
	whole, frac := new(big.Int), new(big.Int)
	whole.QuoRem(abs, big.NewInt(1000000), frac)
	sign := ""
	if realized.Sign() < 0 {
		sign = "-"
	}
	value := fmt.Sprintf("%s%s.%06d USDL", sign, whole.String(), frac.Int64())
	label := "Known realized subtotal; total unavailable"
	if share.RealizedPnlRaw != nil {
		label = "Realized result; total profit unavailable"
	}
	footer := share.Methodology + "; block " + share.Watermark.Height + "; rev " + share.Watermark.ProjectionRevision
	if share.Version == 2 && share.RangeStart != nil && share.RangeEnd != nil {
		footer = share.RangeStart.UTC().Format("2006-01-02") + " .. " + share.RangeEnd.UTC().Format("2006-01-02") + " UTC; block " + share.Watermark.Height + "; rev " + share.Watermark.ProjectionRevision + "; " + share.Methodology
	}
	png, e := renderer.Render(ogrender.Input{Title: "Portfolio realized result", Multiple: value, Pnl: label, PnlPositive: realized.Sign() >= 0, Holdings: "Basis " + share.BasisQuality + "; pricing " + share.PricingQuality, Footer: footer})
	if e != nil {
		return nil, e
	}
	if len(png) == 0 || len(png) > 2000000 {
		return nil, ErrInvalid
	}
	return png, nil
}

func (r Repository) OwnerCardV2(ctx context.Context, id, wallet, verified string, now time.Time) (StoredCard, error) {
	out, e := r.OwnerCard(ctx, id, wallet, verified)
	if e != nil {
		return out, e
	}
	e = r.Pool.QueryRow(ctx, "SELECT c.purge_requested_at,o.last_error,c.attempts FROM portfolio_cards c LEFT JOIN portfolio_card_outbox o ON o.card_id=c.id AND o.kind='purge' WHERE c.id=$1", id).Scan(&out.PurgeRequestedAt, &out.PurgeLastError, &out.RenderAttempts)
	if e != nil {
		return out, e
	}
	out.RenderExhausted = out.State == "pending" && out.RenderAttempts >= 3
	out.PrivacyIncident = out.PurgePending && out.PurgeRequestedAt != nil && now.Sub(*out.PurgeRequestedAt) > 300*time.Second
	return out, nil
}
