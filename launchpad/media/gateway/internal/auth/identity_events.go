package auth

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strconv"
)

const IdentityRevisionSubject = "identity.account.v1"
const IdentityRevisionEventType = "identity.account.revision"

type IdentityEventDelivery struct {
	Store     *delivery.Store
	Subject   string
	EventType string
}
type IdentityRevisionEvent struct {
	AccountID string `json:"accountId"`
	SessionID string `json:"sessionId"`
	Action    string `json:"action"`
	AuthEpoch int64  `json:"authEpoch"`
	Revision  string `json:"revision"`
}

func (d *IdentityEventDelivery) enqueue(ctx context.Context, tx pgx.Tx, p Principal, action string) error {
	if d == nil || d.Store == nil || d.Subject == "" || d.EventType == "" {
		return unavailable()
	}
	var revision int64
	if err := tx.QueryRow(ctx, `INSERT INTO identity_revisions(account_id,revision) VALUES($1,1) ON CONFLICT(account_id) DO UPDATE SET revision=identity_revisions.revision+1 WHERE identity_revisions.revision<9223372036854775807 RETURNING revision`, p.accountID).Scan(&revision); err != nil {
		return unavailable()
	}
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT auth_epoch FROM identity_accounts WHERE account_id=$1`, p.accountID).Scan(&epoch); err != nil {
		return unavailable()
	}
	payload, err := json.Marshal(IdentityRevisionEvent{p.accountID, p.sessionID, action, epoch, strconv.FormatInt(revision, 10)})
	if err != nil {
		return unavailable()
	}
	var envelope delivery.Envelope
	envelope.EventID = uuid.NewString()
	envelope.TraceID = uuid.NewString()
	envelope.EventType = d.EventType
	envelope.SchemaVersion = 1
	envelope.ChainID = 125
	envelope.SourceOrder = delivery.SourceOrder{Height: "0", TransactionIndex: "0", LogIndex: "0"}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&envelope.EmittedAt); err != nil {
		return unavailable()
	}
	envelope.EmittedAt = envelope.EmittedAt.UTC()
	envelope.Payload = payload
	envelope.PayloadHash = delivery.Hash(payload)
	if err = d.Store.Enqueue(ctx, tx, d.Subject, envelope); err != nil {
		return unavailable()
	}
	return nil
}
func (s *Identity) ConsumeIdentityRevision(ctx context.Context, consumer, generation, subject string, wire []byte, ack delivery.Delivery) error {
	if s.events == nil || s.events.Store == nil || subject != s.events.Subject {
		return unavailable()
	}
	return s.events.Store.Consume(ctx, consumer, generation, subject, wire, ack, func(ctx context.Context, tx pgx.Tx, envelope delivery.Envelope) error {
		if envelope.EventType != s.events.EventType {
			return errors.New("identity: unexpected revision event type")
		}
		var event IdentityRevisionEvent
		if strictJSON(envelope.Payload, &event) != nil || !validUUID(event.AccountID) || !validUUID(event.SessionID) || event.AuthEpoch < 0 || !identityEventAction(event.Action) {
			return errors.New("identity: invalid revision event")
		}
		revision, err := strconv.ParseInt(event.Revision, 10, 64)
		if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != event.Revision {
			return errors.New("identity: invalid revision")
		}
		tag, err := tx.Exec(ctx, `INSERT INTO identity_revision_inbox(event_id,account_id,revision,payload_hash) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, envelope.EventID, event.AccountID, revision, envelope.PayloadHash)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			var account, hash string
			var prior int64
			if err = tx.QueryRow(ctx, `SELECT account_id::text,revision,payload_hash FROM identity_revision_inbox WHERE event_id=$1`, envelope.EventID).Scan(&account, &prior, &hash); err != nil {
				return err
			}
			if account != event.AccountID || prior != revision || hash != envelope.PayloadHash {
				return delivery.ErrIntegrity
			}
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO identity_remote_revisions(account_id,revision) VALUES($1,$2) ON CONFLICT(account_id) DO UPDATE SET revision=GREATEST(identity_remote_revisions.revision,excluded.revision)`, event.AccountID, revision)
		return err
	})
}
func identityEventAction(action string) bool {
	switch action {
	case "session_created", "session_refreshed", "session_revoked", "refresh_reuse", "wallet_link", "wallet_unlink", "wallet_link_denied", "wallet_unlink_denied", "step_up":
		return true
	}
	return false
}

func (s *Identity) recordIdentityDenial(ctx context.Context, p Principal, action string) error {
	if !validUUID(p.accountID) || !validUUID(p.sessionID) {
		return unauthorized()
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable()
	}
	defer tx.Rollback(ctx)
	if err = s.audit(ctx, tx, p, action); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return unavailable()
	}
	return nil
}
