package interactions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

func DecodePublicEvent(raw []byte) (PublicEvent, error) {
	var v PublicEvent
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&v); e != nil {
		return v, e
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return v, ErrInvalid
	}
	if v.Revision < 1 || v.AggregateID == "" || len(v.AggregateID) > 320 || v.Target == "" || len(v.Target) > 160 || len(v.Parent) > 160 || strings.ContainsAny(v.AggregateID+v.Target+v.Parent, "\x00\n\r") {
		return v, ErrInvalid
	}
	if actor, e := Wallet(v.Actor); e != nil || actor != v.Actor {
		return v, ErrInvalid
	}
	if _, e := eventType(v.Action); e != nil {
		return v, e
	}
	r := Request{Actor: v.Actor, ID: v.Target, Action: v.Action, Kind: "like"}
	if mutationAggregate(r) != v.AggregateID {
		return v, ErrInvalid
	}
	if v.Action == "callout_reference" && v.SourceRevision < 1 {
		return v, ErrInvalid
	}
	if v.Action == "follow" {
		if target, e := Wallet(v.Target); e != nil || target != v.Target {
			return v, ErrInvalid
		}
	}
	if v.Action == "comment" || v.Action == "comment_hide" {
		if pool, e := Wallet(v.Pool); e != nil || pool != v.Pool {
			return v, ErrInvalid
		}
		if !v.Removed && Text(v.Body) != nil {
			return v, ErrInvalid
		}
		if v.Removed && v.Body != "" {
			return v, ErrInvalid
		}
	}
	if v.Action != "comment" && v.Action != "comment_hide" && (v.Body != "" || v.Parent != "") {
		return v, ErrInvalid
	}
	if v.Action != "callout_reference" && v.SourceRevision != 0 {
		return v, ErrInvalid
	}
	if (v.Action == "follow" || v.Action == "callout_reference") && v.Pool != "" {
		return v, ErrInvalid
	}
	if v.Action == "reaction" {
		if pool, e := Wallet(v.Pool); e != nil || pool != v.Pool {
			return v, ErrInvalid
		}
	}
	if v.Action == "comment_hide" && !v.Removed {
		return v, ErrInvalid
	}
	return v, nil
}
func ProjectPublicEvent(ctx context.Context, tx pgx.Tx, generation string, envelope delivery.Envelope) error {
	if generation == "" || len(generation) > 128 || strings.ContainsAny(generation, "\x00\n") {
		return ErrInvalid
	}
	v, e := DecodePublicEvent(envelope.Payload)
	if e != nil {
		return e
	}
	typ, _ := eventType(v.Action)
	if typ != envelope.EventType {
		return ErrInvalid
	}
	targetWallet := ""
	if v.Action == "follow" {
		targetWallet = v.Target
	}
	if e = policyLocks(ctx, tx, v.Actor, targetWallet); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,38))`, generation+":"+v.AggregateID); e != nil {
		return e
	}
	var erased bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1 OR ($2='follow' AND wallet=$3))`, v.Actor, v.Action, v.Target).Scan(&erased); e != nil {
		return e
	}
	if erased {
		v.Removed = true
		v.Body = ""
		v.Parent = ""
	}
	payload, e := json.Marshal(v)
	if e != nil {
		return e
	}
	var oldRevision int64
	var old []byte
	e = tx.QueryRow(ctx, `SELECT revision,payload FROM interaction_feed_projection WHERE generation=$1 AND aggregate_id=$2 FOR UPDATE`, generation, v.AggregateID).Scan(&oldRevision, &old)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e == nil && oldRevision > v.Revision {
		return nil
	}
	if e == nil && oldRevision == v.Revision {
		var previous PublicEvent
		if json.Unmarshal(old, &previous) != nil {
			return delivery.ErrIntegrity
		}
		if erased {
			previous.Removed = true
			previous.Body = ""
			previous.Parent = ""
		}
		if !reflect.DeepEqual(previous, v) {
			return delivery.ErrIntegrity
		}
		return nil
	}
	_, e = tx.Exec(ctx, `INSERT INTO interaction_feed_projection(generation,aggregate_id,revision,actor,removed,payload) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(generation,aggregate_id) DO UPDATE SET revision=EXCLUDED.revision,actor=EXCLUDED.actor,removed=EXCLUDED.removed,payload=EXCLUDED.payload WHERE interaction_feed_projection.revision<EXCLUDED.revision`, generation, v.AggregateID, v.Revision, v.Actor, v.Removed, payload)
	return e
}
func ConsumePublic(ctx context.Context, store *delivery.Store, generation, subject string, wire []byte, ack delivery.Delivery) error {
	if store == nil {
		return ErrDependency
	}
	return store.Consume(ctx, "social_feed", generation, subject, wire, ack, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
		return ProjectPublicEvent(ctx, tx, generation, e)
	})
}
func (s Store) VisibleProjection(ctx context.Context, generation, after string, limit int) ([]PublicEvent, error) {
	if s.DB == nil {
		return nil, ErrDependency
	}
	if generation == "" || len(generation) > 128 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	rows, e := s.DB.Query(ctx, `SELECT p.payload FROM interaction_feed_projection p WHERE generation=$1 AND aggregate_id>$2 AND NOT removed AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=p.actor OR (convert_from(p.payload,'UTF8')::jsonb->>'action'='follow' AND e.wallet=convert_from(p.payload,'UTF8')::jsonb->>'target')) ORDER BY aggregate_id LIMIT $3`, generation, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PublicEvent{}
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		v, e := DecodePublicEvent(raw)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
