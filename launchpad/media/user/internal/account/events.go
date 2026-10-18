package account

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type runtimeKey struct{}
type requestTraceKey struct{}
type mutationPayloadKey struct{}
type mutationPayload struct{ Body any }

func enqueueAccountEvent(ctx context.Context, tx pgx.Tx, a state, eventID uuid.UUID, kind string, body []byte) error {
	runtime, _ := ctx.Value(runtimeKey{}).(*Runtime)
	if runtime == nil {
		return nil
	}
	if runtime.Delivery == nil || runtime.Subjects[kind] == "" {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	trace, _ := ctx.Value(requestTraceKey{}).(string)
	if trace == "" {
		trace = uuid.NewString()
	}
	event := delivery.Envelope{EventID: eventID.String(), EventType: kind, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: strconv.FormatInt(a.Revision, 10), TransactionIndex: "0", LogIndex: "0"}, EmittedAt: time.Now().UTC(), TraceID: trace, PayloadHash: delivery.Hash(body), Payload: body}
	return runtime.Delivery.Enqueue(ctx, tx, runtime.Subjects[kind], event)
}

type ConsentRevision struct {
	EventID        string    `json:"eventId"`
	AccountID      uuid.UUID `json:"accountId"`
	Revision       int64     `json:"revision"`
	Category       string    `json:"category"`
	Channel        string    `json:"channel"`
	Enabled        bool      `json:"enabled"`
	RevokeRevision int64     `json:"revokeRevision"`
	At             time.Time `json:"at"`
}

func (s Store) ConsumeConsent(ctx context.Context, event ConsentRevision) error {
	if event.EventID == "" || len(event.EventID) > 128 || event.AccountID == uuid.Nil || event.Revision < 1 || event.RevokeRevision < 0 || event.RevokeRevision > event.Revision || !validCategory(event.Category) || !validChannel(event.Channel) || event.At.IsZero() {
		return ErrInvalid
	}
	raw, e := json.Marshal(event)
	if e != nil {
		return e
	}
	digest := delivery.Hash(raw)
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	a, e := load(ctx, tx, event.AccountID)
	if e != nil {
		return e
	}
	if a.Deleted {
		return ErrState
	}
	tag, e := tx.Exec(ctx, `INSERT INTO account_revision_inbox(event_id,digest,account_id,revision) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, event.EventID, digest, event.AccountID, event.Revision)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		var previous string
		if e = tx.QueryRow(ctx, `SELECT digest FROM account_revision_inbox WHERE event_id=$1`, event.EventID).Scan(&previous); e != nil {
			return e
		}
		if previous != digest {
			return delivery.ErrIntegrity
		}
		return nil
	}
	var oldRevision, revoke int64
	e = tx.QueryRow(ctx, `SELECT revision,revoke_revision FROM account_consent WHERE account_id=$1 AND category=$2 AND channel=$3 FOR UPDATE`, event.AccountID, event.Category, event.Channel).Scan(&oldRevision, &revoke)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if event.Revision <= oldRevision || event.Revision <= revoke {
		return tx.Commit(ctx)
	}
	if event.RevokeRevision > revoke {
		revoke = event.RevokeRevision
	}
	_, e = tx.Exec(ctx, `INSERT INTO account_consent VALUES($1,$2,$3,$4,'revision-consumer','1',$5,$6,$7) ON CONFLICT(account_id,category,channel) DO UPDATE SET enabled=EXCLUDED.enabled,revision=EXCLUDED.revision,revoke_revision=EXCLUDED.revoke_revision,changed_at=EXCLUDED.changed_at`, event.AccountID, event.Category, event.Channel, event.Enabled, event.Revision, revoke, event.At.UTC())
	if e != nil {
		return e
	}
	if event.Revision > a.Revision {
		a.Revision = event.Revision
		a.Profile.Revision = strconv.FormatInt(event.Revision, 10)
		a.Preferences.Revision = a.Profile.Revision
		profile, e := json.Marshal(a.Profile)
		if e != nil {
			return e
		}
		preferences, e := json.Marshal(a.Preferences)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE account_state SET revision=$2,profile=$3,preferences=$4,updated_at=$5 WHERE account_id=$1`, a.ID, a.Revision, profile, preferences, s.now()); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
