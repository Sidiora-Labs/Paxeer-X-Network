package interactions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type PublicEvent struct {
	AggregateID    string `json:"aggregate_id"`
	Revision       int64  `json:"revision,string"`
	SourceRevision int64  `json:"source_revision,omitempty,string"`
	Actor          string `json:"actor"`
	Action         string `json:"action"`
	Target         string `json:"target"`
	Pool           string `json:"pool,omitempty"`
	Removed        bool   `json:"removed"`
	Body           string `json:"body,omitempty"`
	Parent         string `json:"parent,omitempty"`
}
type Publisher struct {
	Delivery            *delivery.Store
	Subjects            map[string]string
	NotificationSubject string
	Watermark           func(context.Context, pgx.Tx) (delivery.SourceOrder, error)
}

func eventType(action string) (string, error) {
	switch action {
	case "follow":
		return "social.edge.changed", nil
	case "comment", "comment_hide", "reaction":
		return "social.comment.changed", nil
	case "callout_reference":
		return "social.callout.changed", nil
	}
	return "", ErrInvalid
}
func publicSnapshot(ctx context.Context, tx pgx.Tx, r Request) (PublicEvent, error) {
	aggregate := mutationAggregate(r)
	var rev int64
	if e := tx.QueryRow(ctx, `SELECT revision FROM interaction_aggregate_revisions WHERE aggregate_id=$1`, aggregate).Scan(&rev); e != nil {
		return PublicEvent{}, e
	}
	v := PublicEvent{AggregateID: aggregate, Revision: rev, Actor: r.Actor, Action: r.Action, Target: r.ID, Pool: r.Pool, Removed: r.Remove}
	if r.Action == "comment" || r.Action == "comment_hide" {
		var parent *string
		if e := tx.QueryRow(ctx, `SELECT author,pool,body,parent,removed FROM interaction_comments WHERE id=$1`, r.ID).Scan(&v.Actor, &v.Pool, &v.Body, &parent, &v.Removed); e != nil {
			return v, e
		}
		if parent != nil {
			v.Parent = *parent
		}
		if v.Removed {
			v.Body = ""
			v.Parent = ""
		}
	}
	if r.Action == "callout_reference" {
		v.SourceRevision = r.Revision
	}
	if r.Action == "reaction" {
		if e := tx.QueryRow(ctx, `SELECT pool FROM interaction_comments WHERE id=$1`, r.ID).Scan(&v.Pool); e != nil {
			return v, e
		}
	}
	return v, nil
}
func (p *Publisher) Record(ctx context.Context, tx pgx.Tx, r Request) error {
	if p == nil || p.Delivery == nil || p.Watermark == nil {
		return ErrDependency
	}
	typ, e := eventType(r.Action)
	if e != nil {
		return e
	}
	subject := p.Subjects[typ]
	if subject == "" {
		return ErrDependency
	}
	v, e := publicSnapshot(ctx, tx, r)
	if e != nil {
		return e
	}
	payload, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if _, e = DecodePublicEvent(payload); e != nil {
		return e
	}
	digest := sha256.Sum256([]byte(v.AggregateID))
	id := fmt.Sprintf("social_%s_%d", hex.EncodeToString(digest[:]), v.Revision)
	var at time.Time
	if e = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&at); e != nil {
		return e
	}
	position, e := p.Watermark(ctx, tx)
	if e != nil {
		return e
	}
	envelope := delivery.Envelope{EventID: id, EventType: typ, SchemaVersion: 1, ChainID: 125, SourceOrder: position, EmittedAt: at.UTC(), TraceID: id, PayloadHash: delivery.Hash(payload), Payload: payload}
	if _, e = tx.Exec(ctx, `INSERT INTO interaction_public_events(event_id,aggregate_id,revision,payload,payload_hash,created_at) VALUES($1,$2,$3,$4,$5,$6)`, id, v.AggregateID, v.Revision, payload, delivery.Hash(payload), at); e != nil {
		return e
	}
	scopes := []string{"actor:" + r.Actor}
	if r.Action == "follow" {
		scopes = append(scopes, "wallet:"+r.Actor, "wallet:"+r.ID)
	}
	if v.Pool != "" {
		scopes = append(scopes, "pool:"+v.Pool, "thread:"+r.ID)
	}
	for _, scope := range scopes {
		if _, e = tx.Exec(ctx, `INSERT INTO interaction_visibility_revision(scope,revision) VALUES($1,1) ON CONFLICT(scope) DO UPDATE SET revision=interaction_visibility_revision.revision+1`, scope); e != nil {
			return e
		}
	}
	if e = p.Delivery.Enqueue(ctx, tx, subject, envelope); e != nil {
		return e
	}
	if r.Action == "callout_reference" {
		if p.NotificationSubject == "" {
			return ErrDependency
		}
		for _, publicSubject := range p.Subjects {
			if publicSubject == p.NotificationSubject {
				return ErrInvalid
			}
		}
		rows, err := tx.Query(ctx, `SELECT recipient FROM interaction_notification_intents WHERE target=$1 AND revision=$2 ORDER BY recipient LIMIT 11`, r.ID, r.Revision)
		if err != nil {
			return err
		}
		recipients := []string{}
		for rows.Next() {
			var recipient string
			if err = rows.Scan(&recipient); err != nil {
				rows.Close()
				return err
			}
			recipients = append(recipients, recipient)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(recipients) > 10 {
			return ErrInvalid
		}
		for _, recipient := range recipients {
			body, err := json.Marshal(struct {
				Kind      string `json:"kind"`
				ID        string `json:"id"`
				Revision  string `json:"revision"`
				Recipient string `json:"recipient"`
			}{"callout", r.ID, fmt.Sprint(r.Revision), recipient})
			if err != nil {
				return err
			}
			hash := sha256.Sum256(body)
			notification := envelope
			notification.EventID = "social_mention_" + hex.EncodeToString(hash[:])
			notification.TraceID = notification.EventID
			notification.EventType = "social.mention.intent.v1"
			notification.Payload = body
			notification.PayloadHash = delivery.Hash(body)
			if err = p.Delivery.Enqueue(ctx, tx, p.NotificationSubject, notification); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s Store) recordChange(ctx context.Context, tx pgx.Tx, r Request) error {
	if e := recordChange(ctx, tx, r); e != nil {
		return e
	}
	if writer, ok := s.Authority.(interface {
		RecordChange(context.Context, pgx.Tx, Request) error
	}); ok {
		return writer.RecordChange(ctx, tx, r)
	}
	return nil
}

type CanonicalSourceOrder struct{ Positions pgx.Identifier }

func (v CanonicalSourceOrder) Read(ctx context.Context, tx pgx.Tx) (delivery.SourceOrder, error) {
	var position delivery.SourceOrder
	if len(v.Positions) == 0 {
		return position, ErrDependency
	}
	e := tx.QueryRow(ctx, `SELECT height,transaction_index,log_index FROM chat.social_current_source_order()`).Scan(&position.Height, &position.TransactionIndex, &position.LogIndex)
	return position, e
}

func (s Store) PruneReplay(ctx context.Context, limit int) (int64, error) {
	if s.DB == nil {
		return 0, ErrDependency
	}
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	tag, e := s.DB.Exec(ctx, `DELETE FROM interaction_public_events WHERE event_id IN (SELECT event_id FROM interaction_public_events WHERE expires_at<=clock_timestamp() ORDER BY expires_at,event_id LIMIT $1)`, limit)
	if e != nil {
		return 0, e
	}
	return tag.RowsAffected(), nil
}

func (s Store) RecordRemovalTx(ctx context.Context, tx pgx.Tx, actor, pool, id string) error {
	return s.recordChange(ctx, tx, Request{Actor: actor, Pool: pool, ID: id, Action: "comment", Remove: true})
}
