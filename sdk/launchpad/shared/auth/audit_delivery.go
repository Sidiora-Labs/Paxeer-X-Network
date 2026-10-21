package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const AuditDeliveryDDL = `CREATE TABLE security_audit_delivery_receipts(audit_id bigint PRIMARY KEY REFERENCES security_audit_outbox(id),event_id text NOT NULL UNIQUE,enqueued_at timestamptz NOT NULL DEFAULT clock_timestamp());
CREATE TABLE security_audit_sink(request_id uuid NOT NULL,action text NOT NULL,payload_hash text NOT NULL,payload bytea NOT NULL,revision bigint NOT NULL CHECK(revision>0),PRIMARY KEY(request_id,action));`

type AuditPayload struct {
	Version        int       `json:"version"`
	Revision       string    `json:"revision"`
	RequestID      string    `json:"requestId"`
	ActorPseudonym string    `json:"actorPseudonym"`
	Action         string    `json:"action"`
	Result         string    `json:"result"`
	PolicyVersion  string    `json:"policyVersion"`
	RequestDigest  string    `json:"requestDigest"`
	CreatedAt      time.Time `json:"createdAt"`
}

func auditEventID(requestID, action string) string {
	return "audit_" + delivery.Hash([]byte(requestID+"\x00"+action))
}

func (p AuditPayload) validate() error {
	if p.Version != 1 || !validIdentityID(p.RequestID) || !tokenPattern.MatchString(p.Action) || !tokenPattern.MatchString(p.Result) || !tokenPattern.MatchString(p.PolicyVersion) || p.CreatedAt.IsZero() {
		return ErrScopeDenied
	}
	for _, d := range []string{p.ActorPseudonym, p.RequestDigest} {
		decoded, err := hex.DecodeString(d)
		if err != nil || len(decoded) != 32 || strings.ToLower(d) != d {
			return ErrScopeDenied
		}
	}
	revision, err := strconv.ParseInt(p.Revision, 10, 64)
	if err != nil || revision <= 0 || strconv.FormatInt(revision, 10) != p.Revision {
		return ErrScopeDenied
	}
	return nil
}

type AuditBridge struct {
	Pool               *pgxpool.Pool
	Delivery           *delivery.Store
	Subject, EventType string
}

func (b AuditBridge) Stage(ctx context.Context, limit int) (int, error) {
	if b.Pool == nil || b.Delivery == nil || b.Subject == "" || b.EventType == "" || limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("audit delivery configuration")
	}
	rows, err := b.Pool.Query(ctx, `SELECT a.id FROM security_audit_outbox a LEFT JOIN security_audit_delivery_receipts d ON d.audit_id=a.id WHERE d.audit_id IS NULL ORDER BY a.id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		staged, err := b.stageOne(ctx, id)
		if err != nil {
			return count, err
		}
		if staged {
			count++
		}
	}
	return count, nil
}

func (b AuditBridge) stageOne(ctx context.Context, id int64) (bool, error) {
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('security-audit-delivery:'||$1::text,0))`, id); err != nil {
		return false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM security_audit_delivery_receipts WHERE audit_id=$1)`, id).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, tx.Commit(ctx)
	}
	p := AuditPayload{Version: 1, Revision: strconv.FormatInt(id, 10)}
	if err = tx.QueryRow(ctx, `SELECT request_id::text,actor_pseudonym,action,result,policy_version,request_digest,created_at FROM security_audit_outbox WHERE id=$1`, id).Scan(&p.RequestID, &p.ActorPseudonym, &p.Action, &p.Result, &p.PolicyVersion, &p.RequestDigest, &p.CreatedAt); err != nil {
		return false, err
	}
	p.CreatedAt = p.CreatedAt.UTC()
	if err = p.validate(); err != nil {
		return false, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	event := delivery.Envelope{EventID: auditEventID(p.RequestID, p.Action), EventType: b.EventType, SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: p.Revision, TransactionIndex: "0", LogIndex: "0"}, EmittedAt: p.CreatedAt, TraceID: p.RequestID, PayloadHash: delivery.Hash(raw), Payload: raw}
	if err = b.Delivery.Enqueue(ctx, tx, b.Subject, event); err != nil {
		return false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO security_audit_delivery_receipts(audit_id,event_id) VALUES($1,$2)`, id, event.EventID); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (b AuditBridge) Pending(ctx context.Context) (int64, time.Duration, error) {
	if b.Pool == nil {
		return 0, 0, fmt.Errorf("audit delivery configuration")
	}
	var count int64
	var seconds float64
	err := b.Pool.QueryRow(ctx, `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM (clock_timestamp()-min(a.created_at))),0)::double precision FROM security_audit_outbox a LEFT JOIN security_audit_delivery_receipts d ON d.audit_id=a.id WHERE d.audit_id IS NULL`).Scan(&count, &seconds)
	return count, time.Duration(seconds * float64(time.Second)), err
}

func (b AuditBridge) MissingOperationAudits(ctx context.Context) (int64, error) {
	if b.Pool == nil {
		return 0, fmt.Errorf("audit delivery configuration")
	}
	var missing int64
	err := b.Pool.QueryRow(ctx, `SELECT count(*) FROM operation_ledger l LEFT JOIN security_audit_outbox a ON a.request_id=l.request_id AND a.action=l.action AND a.result='accepted' WHERE a.id IS NULL`).Scan(&missing)
	return missing, err
}

type AuditSink struct {
	Delivery                                 *delivery.Store
	Consumer, Generation, Subject, EventType string
	SinkSchema                               string
}

var auditSinkSchemaPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func (s AuditSink) Consume(ctx context.Context, wire []byte, ack delivery.Delivery) error {
	if s.Delivery == nil || s.Subject == "" || s.EventType == "" {
		return ErrScopeDenied
	}
	table := "security_audit_sink"
	if s.SinkSchema != "" {
		if !auditSinkSchemaPattern.MatchString(s.SinkSchema) {
			return ErrScopeDenied
		}
		table = pgx.Identifier{s.SinkSchema, "security_audit_sink"}.Sanitize()
	}
	return s.Delivery.Consume(ctx, s.Consumer, s.Generation, s.Subject, wire, ack, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
		var p AuditPayload
		if e.EventType != s.EventType || decodeAuthorizationJSON(e.Payload, &p) != nil || p.validate() != nil || e.EventID != auditEventID(p.RequestID, p.Action) || e.TraceID != p.RequestID || e.SourceOrder.Height != p.Revision || e.SourceOrder.TransactionIndex != "0" || e.SourceOrder.LogIndex != "0" || !e.EmittedAt.Equal(p.CreatedAt) {
			return delivery.ErrIntegrity
		}
		revision, _ := strconv.ParseInt(p.Revision, 10, 64)
		tag, err := tx.Exec(ctx, `INSERT INTO `+table+`(request_id,action,payload_hash,payload,revision) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, p.RequestID, p.Action, e.PayloadHash, []byte(e.Payload), revision)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
		var hash string
		if err = tx.QueryRow(ctx, `SELECT payload_hash FROM `+table+` WHERE request_id=$1 AND action=$2`, p.RequestID, p.Action).Scan(&hash); err != nil {
			return err
		}
		if hash != e.PayloadHash {
			return delivery.ErrIntegrity
		}
		return nil
	})
}

func (l Ledger) RecordDenial(ctx context.Context, event AuditEvent) error {
	if l.Pool == nil || event.Result != "denied" {
		return ErrScopeDenied
	}
	tx, err := l.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = AppendAudit(ctx, tx, l.AuditKey, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func IsAuditIntegrityError(err error) bool { return errors.Is(err, delivery.ErrIntegrity) }
