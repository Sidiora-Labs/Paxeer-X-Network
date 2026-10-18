package l17

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const CardWorkerDDL = `ALTER TABLE portfolio_card_outbox ADD COLUMN fence bigint NOT NULL DEFAULT 0, ADD COLUMN lease_owner text, ADD COLUMN lease_until timestamptz, ADD COLUMN attempts integer NOT NULL DEFAULT 0, ADD COLUMN next_attempt timestamptz NOT NULL DEFAULT clock_timestamp(), ADD COLUMN last_error text;
 CREATE INDEX portfolio_card_effect_ready ON portfolio_card_outbox(next_attempt) WHERE published_at IS NULL;`

type CardPurger interface {
	PurgeCard(context.Context, string) error
}
type CardWorker struct {
	Repository Repository
	Owner      string
	FontDir    string
	Purger     CardPurger
}

func (w CardWorker) RunOnce(ctx context.Context) error {
	if w.Repository.Pool == nil {
		return ErrUnavailable
	}
	if !workerName.MatchString(w.Owner) {
		return ErrInvalid
	}
	tx, e := w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	var event, id, kind string
	var fence int64
	var raw []byte
	e = tx.QueryRow(ctx, `WITH candidate AS (SELECT o.event_id FROM portfolio_card_outbox o JOIN portfolio_cards c ON c.id=o.card_id WHERE o.published_at IS NULL AND o.next_attempt<=clock_timestamp() AND (o.lease_until IS NULL OR o.lease_until<clock_timestamp()) AND (o.kind='purge' OR (c.state='pending' AND c.attempts<3)) ORDER BY o.next_attempt FOR UPDATE OF o SKIP LOCKED LIMIT 1) UPDATE portfolio_card_outbox o SET fence=fence+1,lease_owner=$1,lease_until=clock_timestamp()+interval '30 seconds',attempts=attempts+1 FROM candidate c WHERE o.event_id=c.event_id RETURNING o.event_id,o.card_id::text,o.kind,o.fence`, w.Owner).Scan(&event, &id, &kind, &fence)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if kind == "render" {
		tag, e := tx.Exec(ctx, "UPDATE portfolio_cards SET attempts=attempts+1 WHERE id=$1 AND state='pending' AND attempts<3", id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		if e = tx.QueryRow(ctx, "SELECT snapshot FROM portfolio_cards WHERE id=$1", id).Scan(&raw); e != nil {
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
			e = w.Purger.PurgeCard(work, id)
		}
	} else {
		var share ShareSnapshot
		renderer, configErr := ogrender.NewStrict(w.FontDir)
		if configErr != nil {
			e = configErr
		} else if e = json.Unmarshal(raw, &share); e == nil {
			image, e = renderShare(renderer, share)
			if e == nil {
				cfg, decodeErr := png.DecodeConfig(bytes.NewReader(image))
				if decodeErr != nil || cfg.Width != 1200 || cfg.Height != 630 {
					e = ErrInvalid
				}
			}
		}
	}
	if e != nil {
		_, saveErr := w.Repository.Pool.Exec(ctx, "UPDATE portfolio_card_outbox SET last_error=$4,next_attempt=clock_timestamp()+interval '30 seconds',lease_owner=NULL,lease_until=NULL WHERE event_id=$1 AND fence=$2 AND lease_owner=$3", event, fence, w.Owner, "effect_failed")
		return errors.Join(e, saveErr)
	}
	tx, e = w.Repository.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackPortfolio(tx)
	var live bool
	if e = tx.QueryRow(ctx, "SELECT lease_until>clock_timestamp() FROM portfolio_card_outbox WHERE event_id=$1 AND fence=$2 AND lease_owner=$3 FOR UPDATE", event, fence, w.Owner).Scan(&live); e != nil {
		return e
	}
	if !live {
		return ErrConflict
	}
	if kind == "purge" {
		_, e = tx.Exec(ctx, "UPDATE portfolio_cards SET purged_at=clock_timestamp() WHERE id=$1 AND state='revoked'", id)
	} else {
		var tag pgconn.CommandTag
		tag, e = tx.Exec(ctx, "UPDATE portfolio_cards SET image=$2,state='public' WHERE id=$1 AND state='pending'", id, image)
		if e == nil && tag.RowsAffected() != 1 {
			return ErrGone
		}
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "UPDATE portfolio_card_outbox SET published_at=clock_timestamp(),last_error=NULL,lease_owner=NULL,lease_until=NULL WHERE event_id=$1 AND fence=$2", event, fence); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

var cloudflareZone = regexp.MustCompile(`^[0-9a-f]{32}$`)

type CloudflarePurger struct {
	ZoneID       string
	Token        string
	PublicOrigin string
	Client       *http.Client
}

func (p CloudflarePurger) PurgeCard(ctx context.Context, id string) error {
	if !validUUID(id) || !cloudflareZone.MatchString(p.ZoneID) || p.Token == "" {
		return ErrUnavailable
	}
	origin, e := url.Parse(p.PublicOrigin)
	if e != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || (origin.Path != "" && origin.Path != "/") {
		return ErrInvalid
	}
	imageURL := strings.TrimRight(p.PublicOrigin, "/") + "/v2/portfolio/cards/" + id + "/image.png"
	body, e := json.Marshal(struct {
		Files []string `json:"files"`
	}{[]string{imageURL}})
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	client := http.Client{Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if p.Client != nil {
		client.Transport = p.Client.Transport
	}
	request, e := http.NewRequestWithContext(ctx, "POST", "https://api.cloudflare.com/client/v4/zones/"+p.ZoneID+"/purge_cache", bytes.NewReader(body))
	if e != nil {
		return e
	}
	request.Header.Set("Authorization", "Bearer "+p.Token)
	request.Header.Set("Content-Type", "application/json")
	response, e := client.Do(request)
	if e != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	var result struct {
		Success bool `json:"success"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result) != nil || !result.Success {
		return ErrUnavailable
	}
	check, e := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if e != nil {
		return e
	}
	actual, e := client.Do(check)
	if e != nil {
		return ErrUnavailable
	}
	defer actual.Body.Close()
	if actual.StatusCode != http.StatusGone {
		return ErrUnavailable
	}
	return nil
}
