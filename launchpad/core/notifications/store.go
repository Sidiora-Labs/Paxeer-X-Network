package notifications

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"strconv"
	"strings"
	"time"
)

//go:embed schema.sql
var Schema string

type Store struct{ DB *sql.DB }

func (s Store) CommitTrigger(ctx context.Context, n InboxItem, deliveries []DeliveryAttempt) (InboxItem, bool, error) {
	key, err := n.Identity.Key()
	if err != nil {
		return n, false, err
	}
	if s.DB == nil || n.CreatedAt.IsZero() || n.Summary == "" || len(n.Summary) > 512 || len(deliveries) > 32 {
		return n, false, ErrInvalid
	}
	n.ID = key
	if n.Category == "market" || n.Category == "watchlist" {
		n.ExpiresAt = n.CreatedAt.Add(15 * time.Minute)
	}
	seen := map[string]bool{}
	for i := range deliveries {
		d := &deliveries[i]
		if d.Channel == "" || d.TargetID == "" || d.Attempt != 0 {
			return n, false, ErrInvalid
		}
		identity := d.Channel + "\x00" + d.TargetID
		if seen[identity] {
			return n, false, ErrInvalid
		}
		seen[identity] = true
		d.NotificationID = key
		d.State = "pending"
		d.NextAt = n.CreatedAt
		d.ExpiresAt = n.ExpiresAt
	}
	body, err := json.Marshal(n)
	if err != nil {
		return n, false, err
	}
	ds, err := json.Marshal(deliveries)
	if err != nil {
		return n, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return n, false, err
	}
	defer tx.Rollback()
	accountKey := erasedAccountKey(n.Identity.AccountID)
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,210029))`, accountKey); err != nil {
		return n, false, err
	}
	var deleted bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM discovery_notifications.deleted_accounts WHERE account_key=$1)`, accountKey).Scan(&deleted); err != nil {
		return n, false, err
	}
	if deleted {
		return InboxItem{}, false, ErrForbidden
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO discovery_notifications.inbox(id,account_id,trigger_id,template_version,document,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(account_id,trigger_id,template_version) DO NOTHING`, key, n.Identity.AccountID, n.Identity.TriggerEventID, n.Identity.TemplateVersion, body, n.CreatedAt)
	if err != nil {
		return n, false, err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return n, false, err
	}
	if count == 0 {
		var old []byte
		err = tx.QueryRowContext(ctx, `SELECT document FROM discovery_notifications.inbox WHERE account_id=$1 AND trigger_id=$2 AND template_version=$3`, n.Identity.AccountID, n.Identity.TriggerEventID, n.Identity.TemplateVersion).Scan(&old)
		if err == nil {
			err = json.Unmarshal(old, &n)
		}
		if err != nil {
			return n, false, err
		}
		return n, false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO discovery_notifications.outbox(id,deliveries,next_at,terminal) VALUES($1,$2,$3,$4)`, key, ds, n.CreatedAt, len(deliveries) == 0)
	if err != nil {
		return n, false, err
	}
	return n, true, tx.Commit()
}
func (s Store) Claim(ctx context.Context, owner string, now time.Time, limit int) ([]OutboxItem, error) {
	if s.DB == nil || owner == "" || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `WITH due AS (SELECT id FROM discovery_notifications.outbox WHERE NOT terminal AND next_at<=$1 AND (lease_until IS NULL OR lease_until<=$1) ORDER BY next_at,id FOR UPDATE SKIP LOCKED LIMIT $2) UPDATE discovery_notifications.outbox o SET lease_owner=$3,lease_until=$1+interval '30 seconds',fence=o.fence+1 FROM due WHERE o.id=due.id RETURNING o.id,o.fence,o.deliveries,(SELECT document FROM discovery_notifications.inbox i WHERE i.id=o.id)`, now, limit, owner)
	if err != nil {
		return nil, err
	}
	result := []OutboxItem{}
	for rows.Next() {
		var n OutboxItem
		var id string
		var fence int64
		var ds, b []byte
		if err = rows.Scan(&id, &fence, &ds, &b); err != nil {
			rows.Close()
			return nil, err
		}
		n.ID = fmt.Sprintf("%s:%d", id, fence)
		if err = json.Unmarshal(b, &n.Notification); err == nil {
			err = json.Unmarshal(ds, &n.Deliveries)
		}
		if err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
func (s Store) Complete(ctx context.Context, owner, claim string, attempt DeliveryAttempt) error {
	switch attempt.State {
	case "accepted", "unknown", "retry", "rejected":
		if ValidateResult(SendResult{State: attempt.State, Receipt: attempt.ProviderReceipt}) != nil {
			return ErrInvalid
		}
	case "suppressed", "dead":
	default:
		return ErrInvalid
	}
	parts := strings.Split(claim, ":")
	if len(parts) != 2 || owner == "" {
		return ErrInvalid
	}
	fence, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT deliveries FROM discovery_notifications.outbox WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() FOR UPDATE`, parts[0], owner, fence).Scan(&body)
	if err != nil {
		return err
	}
	var ds []DeliveryAttempt
	if err = json.Unmarshal(body, &ds); err != nil {
		return err
	}
	found := false
	terminal := true
	next := time.Now().Add(24 * time.Hour)
	for i, d := range ds {
		if d.Channel == attempt.Channel && d.TargetID == attempt.TargetID {
			validAttempt := attempt.Attempt == d.Attempt+1 || (d.State == "unknown" && d.Reason == "send_interrupted" && attempt.Attempt == d.Attempt)
			if terminalDelivery(d) || attempt.NotificationID != parts[0] || !validAttempt || attempt.Attempt > 20 || !attempt.ExpiresAt.Equal(d.ExpiresAt) {
				return ErrInvalid
			}
			ds[i] = attempt
			found = true
		}
		d = ds[i]
		if !terminalDelivery(d) {
			terminal = false
			if d.NextAt.Before(next) {
				next = d.NextAt
			}
		}
	}
	if !found {
		return ErrInvalid
	}
	body, err = json.Marshal(ds)
	if err != nil {
		return err
	}
	// Retain the fence until the last delivery in this claim has completed.
	_, err = tx.ExecContext(ctx, `UPDATE discovery_notifications.outbox SET deliveries=$1,next_at=$2,terminal=$3 WHERE id=$4 AND lease_owner=$5 AND fence=$6`, body, next, terminal, parts[0], owner, fence)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) Release(ctx context.Context, owner, claim string) error {
	parts := strings.Split(claim, ":")
	if len(parts) != 2 {
		return ErrInvalid
	}
	fence, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		return e
	}
	_, e = s.DB.ExecContext(ctx, `UPDATE discovery_notifications.outbox SET lease_until=NULL,lease_owner=NULL WHERE id=$1 AND lease_owner=$2 AND fence=$3`, parts[0], owner, fence)
	return e
}
func (s Store) SetRead(ctx context.Context, account, id string, read bool) error {
	res, e := s.DB.ExecContext(ctx, `UPDATE discovery_notifications.inbox SET document=jsonb_set(document,'{Read}',to_jsonb($3::boolean)),read_at=CASE WHEN $3 THEN COALESCE(read_at,clock_timestamp()) ELSE read_at END WHERE id=$1 AND account_id=$2`, id, account, read)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n != 1 {
		return sql.ErrNoRows
	}
	return e
}
func erasedAccountKey(account string) string {
	key := sha256.Sum256([]byte("notification-erased-account-v1\x00" + account))
	return hex.EncodeToString(key[:])
}

func (s Store) DeleteAccount(ctx context.Context, account string) error {
	if s.DB == nil || account == "" {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	key := erasedAccountKey(account)
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,210029))`, key); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO discovery_notifications.deleted_accounts(account_key) VALUES($1) ON CONFLICT DO NOTHING`, key); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM discovery_notifications.inbox WHERE account_id=$1`, account); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) DeleteItem(ctx context.Context, account, id string) error {
	res, e := s.DB.ExecContext(ctx, `DELETE FROM discovery_notifications.inbox WHERE account_id=$1 AND id=$2`, account, id)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n == 0 {
		return sql.ErrNoRows
	}
	return e
}
func (s Store) List(ctx context.Context, account string, limit int) ([]InboxItem, error) {
	if account == "" || limit < 1 || limit > 50 {
		return nil, ErrInvalid
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT document FROM discovery_notifications.inbox WHERE account_id=$1 ORDER BY created_at DESC,id LIMIT $2`, account, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []InboxItem{}
	for rows.Next() {
		var b []byte
		var n InboxItem
		if e = rows.Scan(&b); e == nil {
			e = json.Unmarshal(b, &n)
		}
		if e != nil {
			return nil, e
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

var _ Repository = Store{}
var ErrUnavailable = errors.New("notification authority unavailable")

func (s Store) Renew(ctx context.Context, owner, claim string) error {
	parts := strings.Split(claim, ":")
	if len(parts) != 2 {
		return ErrInvalid
	}
	f, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil {
		return ErrInvalid
	}
	res, e := s.DB.ExecContext(ctx, `UPDATE discovery_notifications.outbox SET lease_until=clock_timestamp()+interval '30 seconds' WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp()`, parts[0], owner, f)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n != 1 {
		return ErrUnavailable
	}
	return e
}

func NewStore(pool *pgxpool.Pool) (Store, error) {
	if pool == nil {
		return Store{}, ErrUnavailable
	}
	return Store{DB: stdlib.OpenDBFromPool(pool)}, nil
}

func terminalDelivery(d DeliveryAttempt) bool {
	switch d.State {
	case "accepted", "rejected", "suppressed", "dead":
		return true
	case "unknown":
		return d.Reason == "expired_uncertain" || d.Reason == "attempts_exhausted_uncertain" || d.Reason == "stopped_uncertain"
	}
	return false
}

func (s Store) BeginSend(ctx context.Context, owner, claim string, attempt DeliveryAttempt) error {
	parts := strings.Split(claim, ":")
	if s.DB == nil || len(parts) != 2 || owner == "" || attempt.NotificationID != parts[0] || attempt.Attempt < 1 || attempt.Attempt > 20 || attempt.TargetRevision == 0 || attempt.ConsentRevision == 0 {
		return ErrInvalid
	}
	fence, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || fence < 1 {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var body []byte
	err = tx.QueryRowContext(ctx, `SELECT deliveries FROM discovery_notifications.outbox WHERE id=$1 AND lease_owner=$2 AND fence=$3 AND lease_until>clock_timestamp() FOR UPDATE`, parts[0], owner, fence).Scan(&body)
	if err != nil {
		return err
	}
	var ds []DeliveryAttempt
	if err = json.Unmarshal(body, &ds); err != nil {
		return err
	}
	found := false
	for i, d := range ds {
		if d.Channel != attempt.Channel || d.TargetID != attempt.TargetID {
			continue
		}
		if terminalDelivery(d) || attempt.Attempt != d.Attempt+1 || !attempt.ExpiresAt.Equal(d.ExpiresAt) || (d.TargetRevision != 0 && d.TargetRevision != attempt.TargetRevision) {
			return ErrInvalid
		}
		found = true
		d = attempt
		d.State = "unknown"
		d.Reason = "send_interrupted"
		d.AcceptanceUnknown = true
		d.TargetRevision = attempt.TargetRevision
		d.ConsentRevision = attempt.ConsentRevision
		d.NextAt = attempt.NextAt
		ds[i] = d
	}
	if !found {
		return ErrInvalid
	}
	body, err = json.Marshal(ds)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE discovery_notifications.outbox SET deliveries=$1 WHERE id=$2 AND lease_owner=$3 AND fence=$4`, body, parts[0], owner, fence)
	if err != nil {
		return err
	}
	return tx.Commit()
}
