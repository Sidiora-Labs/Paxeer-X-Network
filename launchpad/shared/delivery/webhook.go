package delivery

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/jackc/pgx/v5"
)

//go:embed webhook.sql
var webhookSQL string

func WebhookDDL(schema string) (string, error) {
	if !token.MatchString(schema) {
		return "", ErrSchema
	}
	return strings.ReplaceAll(webhookSQL, "{{schema}}", pgx.Identifier{schema}.Sanitize()), nil
}

type WebhookTarget struct {
	ID, URL, SecretReference string
	Subjects                 map[string]bool
}
type WebhookBridge struct {
	store                 *Store
	targets               map[string]WebhookTarget
	order                 []string
	client                *http.Client
	secrets               secrets.Provider
	authorize             func(context.Context, string) error
	owner                 string
	lease, timeout, retry time.Duration
	maxAttempts           int
	budget                *WorkBudget
}
type WebhookMetadata struct {
	TargetID, Generation, Partition, EventID, State, FailureClass string
	Sequence                                                      int64
	Attempts, ResponseStatus                                      int
	AvailableAt                                                   time.Time
}
type webhookClaim struct {
	target, generation, partition, event, position, payloadHash, wireHash, key, owner string
	sequence, fence                                                                   int64
	attempts, budget                                                                  int
	wire                                                                              []byte
}

func NewWebhookBridge(store *Store, targets []WebhookTarget, client *http.Client, provider secrets.Provider, authorize func(context.Context, string) error, owner string, lease, timeout, retry time.Duration, maxAttempts int) (*WebhookBridge, error) {
	if store == nil || provider == nil || authorize == nil || client == nil || !token.MatchString(owner) || lease < time.Second || lease > 5*time.Minute || timeout <= 0 || timeout >= lease || retry < time.Second || retry > time.Hour || maxAttempts < 1 || maxAttempts > 100 || len(targets) == 0 || len(targets) > 100 {
		return nil, errors.New("delivery: invalid webhook configuration")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil || transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		return nil, errors.New("delivery: verified TLS transport required")
	}
	ownedTransport := transport.Clone()
	if ownedTransport.TLSClientConfig != nil {
		ownedTransport.TLSClientConfig = ownedTransport.TLSClientConfig.Clone()
	}
	copyClient := *client
	copyClient.Transport = ownedTransport
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	copyClient.Timeout = timeout
	b := &WebhookBridge{store: store, targets: map[string]WebhookTarget{}, client: &copyClient, secrets: provider, authorize: authorize, owner: owner, lease: lease, timeout: timeout, retry: retry, maxAttempts: maxAttempts}
	for _, target := range targets {
		u, err := url.Parse(target.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || !token.MatchString(target.ID) || !token.MatchString(target.SecretReference) || len(target.Subjects) == 0 {
			return nil, errors.New("delivery: invalid allowlisted webhook target")
		}
		if _, ok = b.targets[target.ID]; ok {
			return nil, ErrIntegrity
		}
		copySubjects := map[string]bool{}
		for subject, enabled := range target.Subjects {
			if !store.policy.Subjects[subject] || !enabled {
				return nil, ErrSchema
			}
			copySubjects[subject] = true
		}
		target.Subjects = copySubjects
		b.targets[target.ID] = target
		b.order = append(b.order, target.ID)
	}
	sort.Strings(b.order)
	return b, nil
}

func (b *WebhookBridge) StageTx(ctx context.Context, tx pgx.Tx, generation, partition string, sequence int64, positionIdentity, predecessor, subject string, e Envelope) error {
	if tx == nil || !token.MatchString(generation) || partition == "" || len(partition) > 256 || sequence < 1 || !token.MatchString(positionIdentity) || sequence > 1 && !token.MatchString(predecessor) || sequence == 1 && predecessor != "" {
		return ErrIntegrity
	}
	if err := b.store.assertEpochTx(ctx, tx); err != nil {
		return err
	}
	wire, err := e.Encode(b.store.policy, subject)
	if err != nil {
		return err
	}
	var original []byte
	var storedSubject string
	if err = tx.QueryRow(ctx, "SELECT envelope,subject FROM "+b.store.table("delivery_outbox")+" WHERE event_id=$1", e.EventID).Scan(&original, &storedSubject); err != nil {
		return err
	}
	if storedSubject != subject || !bytes.Equal(original, wire) {
		return ErrIntegrity
	}
	for _, id := range b.order {
		target := b.targets[id]
		if !target.Subjects[subject] {
			continue
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+b.store.table("delivery_webhook_cursors")+"(target_id,generation,partition) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", id, generation, partition)
		if err != nil {
			return err
		}
		var tail int64
		var last string
		if err = tx.QueryRow(ctx, "SELECT enqueued_sequence,enqueued_event FROM "+b.store.table("delivery_webhook_cursors")+" WHERE target_id=$1 AND generation=$2 AND partition=$3 FOR UPDATE", id, generation, partition).Scan(&tail, &last); err != nil {
			return err
		}
		key := Hash([]byte(id + "/" + generation + "/" + partition + "/" + e.EventID))
		if sequence <= tail {
			var oldID, oldHash, oldWire, oldPosition, oldPrevious, oldKey string
			err = tx.QueryRow(ctx, "SELECT event_id,payload_hash,wire_hash,position_identity,predecessor,idempotency_key FROM "+b.store.table("delivery_webhooks")+" WHERE target_id=$1 AND generation=$2 AND partition=$3 AND sequence=$4", id, generation, partition, sequence).Scan(&oldID, &oldHash, &oldWire, &oldPosition, &oldPrevious, &oldKey)
			if err != nil {
				return err
			}
			if oldID != e.EventID || oldHash != e.PayloadHash || oldWire != Hash(wire) || oldPosition != positionIdentity || oldPrevious != predecessor || oldKey != key {
				return ErrIntegrity
			}
			continue
		}
		if sequence != tail+1 || predecessor != last {
			return ErrGap
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+b.store.table("delivery_webhooks")+"(target_id,generation,partition,sequence,event_id,predecessor,payload_hash,wire_hash,idempotency_key,position_identity,allowed_attempts) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", id, generation, partition, sequence, e.EventID, predecessor, e.PayloadHash, Hash(wire), key, positionIdentity, b.maxAttempts)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "UPDATE "+b.store.table("delivery_webhook_cursors")+" SET enqueued_sequence=$4,enqueued_event=$5 WHERE target_id=$1 AND generation=$2 AND partition=$3", id, generation, partition, sequence, positionIdentity)
		if err != nil {
			return err
		}
	}
	return b.store.assertEpochTx(ctx, tx)
}

func (b *WebhookBridge) claim(ctx context.Context, target string) (*webhookClaim, error) {
	var out *webhookClaim
	err := b.store.mutate(ctx, func(tx pgx.Tx) error {
		c := &webhookClaim{target: target, owner: b.owner}
		err := tx.QueryRow(ctx, `WITH picked AS (SELECT w.target_id,w.generation,w.partition,w.sequence FROM `+b.store.table("delivery_webhooks")+` w JOIN `+b.store.table("delivery_webhook_cursors")+` c USING(target_id,generation,partition) WHERE w.target_id=$1 AND w.state='pending' AND w.sequence=c.committed_sequence+1 AND w.predecessor=c.committed_event AND w.available_at<=clock_timestamp() AND (w.lease_until IS NULL OR w.lease_until<=clock_timestamp()) ORDER BY w.available_at,w.partition FOR UPDATE OF w SKIP LOCKED LIMIT 1) UPDATE `+b.store.table("delivery_webhooks")+` w SET lease_expiry_count=w.lease_expiry_count+CASE WHEN w.lease_until<=clock_timestamp() THEN 1 ELSE 0 END,claimed_at=clock_timestamp(),owner=$2,fence=w.fence+1,attempts=w.attempts+1,lease_until=clock_timestamp()+$3::bigint*interval '1 millisecond' FROM picked p WHERE (w.target_id,w.generation,w.partition,w.sequence)=(p.target_id,p.generation,p.partition,p.sequence) RETURNING w.generation,w.partition,w.sequence,w.event_id,w.position_identity,w.payload_hash,w.wire_hash,w.idempotency_key,w.fence,w.attempts,w.allowed_attempts`, target, b.owner, b.lease.Milliseconds()).Scan(&c.generation, &c.partition, &c.sequence, &c.event, &c.position, &c.payloadHash, &c.wireHash, &c.key, &c.fence, &c.attempts, &c.budget)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, "SELECT envelope FROM "+b.store.table("delivery_outbox")+" WHERE event_id=$1", c.event).Scan(&c.wire)
		if err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}

func (b *WebhookBridge) finish(ctx context.Context, c *webhookClaim, status int, class FailureClass) error {
	return b.store.mutate(ctx, func(tx pgx.Tx) error {
		success := status >= 200 && status < 300 && class == ""
		tag, err := tx.Exec(ctx, "UPDATE "+b.store.table("delivery_webhooks")+` SET acknowledgement_latency_ms=CASE WHEN $7 THEN GREATEST(0,EXTRACT(EPOCH FROM (clock_timestamp()-claimed_at))*1000)::bigint ELSE acknowledgement_latency_ms END,state=CASE WHEN $7 THEN 'delivered' WHEN attempts>=$8 OR $9<>'transient' THEN 'quarantined' ELSE 'pending' END,response_status=$6,failure_class=NULLIF($9,''),delivered_at=CASE WHEN $7 THEN clock_timestamp() ELSE NULL END,available_at=clock_timestamp()+$10::bigint*interval '1 millisecond',owner=NULL,lease_until=NULL WHERE target_id=$1 AND generation=$2 AND partition=$3 AND sequence=$4 AND owner=$5 AND fence=$11 AND lease_until>clock_timestamp() AND state='pending'`, c.target, c.generation, c.partition, c.sequence, c.owner, status, success, c.budget, string(class), b.retry.Milliseconds(), c.fence)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		if !success {
			return nil
		}
		tag, err = tx.Exec(ctx, "UPDATE "+b.store.table("delivery_webhook_cursors")+" SET committed_sequence=$4,committed_event=$5 WHERE target_id=$1 AND generation=$2 AND partition=$3 AND committed_sequence=$4-1 AND committed_event=(SELECT predecessor FROM "+b.store.table("delivery_webhooks")+" WHERE target_id=$1 AND generation=$2 AND partition=$3 AND sequence=$4)", c.target, c.generation, c.partition, c.sequence, c.position)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrGap
		}
		return nil
	})
}

func (b *WebhookBridge) RunOnce(ctx context.Context) error {
	var failures error
	for _, id := range b.order {
		target := b.targets[id]
		if b.budget != nil {
			if err := b.budget.limiter.Wait(ctx); err != nil {
				return errors.Join(failures, err)
			}
		}
		c, err := b.claim(ctx, id)
		if err != nil {
			failures = errors.Join(failures, errors.New("delivery: webhook claim failed"))
			continue
		}
		if c == nil {
			continue
		}
		class := Transient
		status := 0
		if c.attempts > c.budget {
			err = b.finish(ctx, c, 0, Transient)
			failures = errors.Join(failures, err)
			continue
		}
		if Hash(c.wire) != c.wireHash {
			class = IntegrityConflict
		} else {
			var envelope Envelope
			var subject string
			err = b.store.pool.QueryRow(ctx, "SELECT subject FROM "+b.store.table("delivery_outbox")+" WHERE event_id=$1", c.event).Scan(&subject)
			if err == nil {
				envelope, err = Decode(c.wire, b.store.policy, subject)
			}
			if err != nil || envelope.PayloadHash != c.payloadHash || envelope.EventID != c.event || !target.Subjects[subject] {
				class = IntegrityConflict
			} else {
				secret, keyErr := b.secrets.GetRequired(ctx, target.SecretReference)
				if keyErr != nil || len(secret) < 32 {
					class = Authorization
				} else {
					request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, target.URL, bytes.NewReader(c.wire))
					if requestErr == nil {
						stamp := strconv.FormatInt(time.Now().Unix(), 10)
						request.Header.Set("Content-Type", "application/json")
						request.Header.Set("Idempotency-Key", c.key)
						request.Header.Set("X-Sidiora-Timestamp", stamp)
						request.Header.Set("X-Sidiora-Signature", signWebhookBody(secret, stamp, string(c.wire)))
						response, httpErr := b.client.Do(request)
						if httpErr == nil {
							status = response.StatusCode
							_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
							_ = response.Body.Close()
							if status >= 200 && status < 300 {
								class = ""
							} else if status == 401 || status == 403 {
								class = Authorization
							} else if status >= 400 && status < 500 && status != 408 && status != 429 {
								class = SchemaViolation
							}
						}
					}
				}
			}
		}
		if err = b.finish(ctx, c, status, class); err != nil {
			failures = errors.Join(failures, err)
		} else if class != "" {
			failures = errors.Join(failures, errors.New("delivery: webhook "+string(class)))
		}
	}
	return failures
}

func (b *WebhookBridge) Inspect(ctx context.Context, target string, limit int) ([]WebhookMetadata, error) {
	if err := b.authorize(ctx, "webhook.inspect"); err != nil {
		return nil, errors.New("delivery: administrator authorization required")
	}
	if _, ok := b.targets[target]; !ok || limit < 1 || limit > 1000 {
		return nil, ErrSchema
	}
	rows, err := b.store.pool.Query(ctx, "SELECT target_id,generation,partition,event_id,state,COALESCE(failure_class,''),sequence,attempts,COALESCE(response_status,0),available_at FROM "+b.store.table("delivery_webhooks")+" WHERE target_id=$1 ORDER BY generation,partition,sequence LIMIT $2", target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WebhookMetadata{}
	for rows.Next() {
		var m WebhookMetadata
		if err = rows.Scan(&m.TargetID, &m.Generation, &m.Partition, &m.EventID, &m.State, &m.FailureClass, &m.Sequence, &m.Attempts, &m.ResponseStatus, &m.AvailableAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (b *WebhookBridge) Retry(ctx context.Context, target, generation, partition string, sequence int64) error {
	if err := b.authorize(ctx, "webhook.retry"); err != nil {
		return errors.New("delivery: administrator authorization required")
	}
	if _, ok := b.targets[target]; !ok {
		return ErrSchema
	}
	return b.store.mutate(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE "+b.store.table("delivery_webhooks")+" SET state='pending',allowed_attempts=attempts+$5,available_at=clock_timestamp(),failure_class=NULL WHERE target_id=$1 AND generation=$2 AND partition=$3 AND sequence=$4 AND state='quarantined' AND lease_until IS NULL", target, generation, partition, sequence, b.maxAttempts)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrLease
		}
		_, err = tx.Exec(ctx, "INSERT INTO "+b.store.table("delivery_webhook_retry_audit")+"(target_id,generation,partition,sequence,attempts_before,allowed_attempts) SELECT target_id,generation,partition,sequence,attempts,allowed_attempts FROM "+b.store.table("delivery_webhooks")+" WHERE target_id=$1 AND generation=$2 AND partition=$3 AND sequence=$4", target, generation, partition, sequence)
		return err
	})
}

func signWebhookBody(secret, timestamp, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "." + body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type WebhookTargetStats struct {
	TargetID                                                                                  string
	PendingRows, QuarantinedRows, PendingBytes, LeaseExpiryCount, MaxAcknowledgementLatencyMS int64
	OldestUnpublishedAgeSeconds                                                               float64
}

func (b *WebhookBridge) Stats(ctx context.Context) ([]WebhookTargetStats, error) {
	rows, err := b.store.pool.Query(ctx, `SELECT w.target_id,count(*) FILTER(WHERE w.state='pending'),count(*) FILTER(WHERE w.state='quarantined'),COALESCE(sum(octet_length(o.envelope)) FILTER(WHERE w.state<>'delivered'),0)::bigint,COALESCE(sum(w.lease_expiry_count),0)::bigint,COALESCE(max(w.acknowledgement_latency_ms),0)::bigint,COALESCE(EXTRACT(EPOCH FROM (clock_timestamp()-min(w.created_at) FILTER(WHERE w.state<>'delivered'))),0)::double precision FROM `+b.store.table("delivery_webhooks")+` w JOIN `+b.store.table("delivery_outbox")+` o USING(event_id) GROUP BY w.target_id ORDER BY w.target_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WebhookTargetStats{}
	for rows.Next() {
		var stat WebhookTargetStats
		if err = rows.Scan(&stat.TargetID, &stat.PendingRows, &stat.QuarantinedRows, &stat.PendingBytes, &stat.LeaseExpiryCount, &stat.MaxAcknowledgementLatencyMS, &stat.OldestUnpublishedAgeSeconds); err != nil {
			return nil, err
		}
		out = append(out, stat)
	}
	return out, rows.Err()
}
