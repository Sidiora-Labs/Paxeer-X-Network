package l17

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
	"io"
	"time"
)

func (r Repository) CommitPortfolioUpdateTx(ctx context.Context, tx pgx.Tx, sourceEvent string, update interfaces.PortfolioUpdate, expires time.Time) (Summary, error) {
	raw, e := json.Marshal(update)
	if e != nil {
		return Summary{}, e
	}
	if e = interfaces.ValidateJSON("PortfolioUpdate", raw); e != nil {
		return Summary{}, e
	}
	if !update.CompleteBatch {
		return Summary{}, ErrInvalid
	}
	var s Summary
	raw, e = json.Marshal(update.Summary)
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(raw, &s); e != nil {
		return s, e
	}
	if s.SnapshotID != update.SnapshotId || s.Watermark.ProjectionRevision != string(update.SummaryRevision) {
		return s, ErrConflict
	}
	expected, e := json.Marshal(s)
	if e != nil {
		return s, e
	}
	positions := make([]Position, 0, len(update.Changes))
	for _, change := range update.Changes {
		if change.Position == nil || change.Operation != "upsert" {
			return s, ErrInvalid
		}
		var p Position
		raw, e = json.Marshal(change.Position)
		if e != nil {
			return s, e
		}
		if e = json.Unmarshal(raw, &p); e != nil {
			return s, e
		}
		positions = append(positions, p)
	}
	if s.PositionCount != len(positions) {
		return s, ErrConflict
	}
	derived, e := Aggregate(s.Wallet, s.SnapshotID, s.Watermark, positions)
	if e != nil {
		return s, e
	}
	actual, e := json.Marshal(derived)
	if e != nil || !bytes.Equal(expected, actual) {
		return s, ErrConflict
	}
	return r.CommitSnapshotTx(ctx, tx, sourceEvent, s, positions, expires)
}

func (r Repository) CommitProjectionDeliveryTx(ctx context.Context, tx pgx.Tx, store *delivery.Store, subject string, event delivery.Envelope, update interfaces.PortfolioUpdate, expires time.Time) (Summary, error) {
	if tx == nil || store == nil {
		return Summary{}, ErrInvalid
	}
	raw, e := json.Marshal(update)
	if e != nil {
		return Summary{}, e
	}
	if e = uniqueJSON(event.Payload); e != nil {
		return Summary{}, e
	}
	var given any
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.UseNumber()
	if e = decoder.Decode(&given); e != nil {
		return Summary{}, e
	}
	canonical, e := json.Marshal(given)
	if e != nil {
		return Summary{}, e
	}
	var typed any
	decoder = json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if e = decoder.Decode(&typed); e != nil {
		return Summary{}, e
	}
	raw, e = json.Marshal(typed)
	if e != nil || !bytes.Equal(raw, canonical) {
		return Summary{}, ErrConflict
	}
	s, e := r.CommitPortfolioUpdateTx(ctx, tx, event.EventID, update, expires)
	if e != nil {
		return s, e
	}
	if e = store.Enqueue(ctx, tx, subject, event); e != nil {
		return s, e
	}
	return s, nil
}

func (r Repository) CommitFullProjectionDeliveryTx(ctx context.Context, tx pgx.Tx, store *delivery.Store, subject, sourceEvent string, s Summary, positions []Position, expires time.Time, event delivery.Envelope) (Summary, error) {
	if tx == nil || store == nil || !validUUID(s.SnapshotID) {
		return Summary{}, ErrInvalid
	}
	notice := Notice{Wallet: s.Wallet, SnapshotID: s.SnapshotID, Revision: s.Watermark.ProjectionRevision, Algorithm: Algorithm}
	raw, e := json.Marshal(notice)
	if e != nil {
		return Summary{}, e
	}
	if e = uniqueJSON(event.Payload); e != nil {
		return Summary{}, e
	}
	var supplied Notice
	decoder := json.NewDecoder(bytes.NewReader(event.Payload))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&supplied); e != nil {
		return Summary{}, e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return Summary{}, ErrInvalid
	}
	canonical, e := json.Marshal(supplied)
	if e != nil || !bytes.Equal(raw, canonical) {
		return Summary{}, ErrConflict
	}
	committed, e := r.CommitSnapshotTx(ctx, tx, sourceEvent, s, positions, expires)
	if e != nil {
		return committed, e
	}
	if e = store.Enqueue(ctx, tx, subject, event); e != nil {
		return committed, e
	}
	return committed, nil
}
