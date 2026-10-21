package interactions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ErasureCommand struct {
	CaseID        string     `json:"deletion_case_id"`
	Wallet        string     `json:"wallet"`
	PolicyVersion string     `json:"policy_version"`
	HoldUntil     *time.Time `json:"hold_until"`
}
type ErasureReceipt struct {
	CaseID        string     `json:"deletion_case_id"`
	Wallet        string     `json:"wallet"`
	PolicyVersion string     `json:"policy_version"`
	State         string     `json:"state"`
	RowsProcessed int64      `json:"rows_processed"`
	CompletedAt   *time.Time `json:"completed_at"`
}

func (v ErasureCommand) validate() error {
	if _, e := uuid.Parse(v.CaseID); e != nil {
		return ErrInvalid
	}
	w, e := Wallet(v.Wallet)
	if e != nil || w != v.Wallet || len(v.PolicyVersion) < 1 || len(v.PolicyVersion) > 128 {
		return ErrInvalid
	}
	return nil
}
func (s Store) BeginErasureTx(ctx context.Context, tx pgx.Tx, v ErasureCommand) error {
	if e := v.validate(); e != nil {
		return e
	}
	if v.HoldUntil != nil {
		at := v.HoldUntil.UTC().Truncate(time.Microsecond)
		v.HoldUntil = &at
	}
	if e := policyLocks(ctx, tx, v.Wallet); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `INSERT INTO interaction_erasure_jobs(deletion_case_id,wallet,policy_version,hold_until) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, v.CaseID, v.Wallet, v.PolicyVersion, v.HoldUntil)
	if e != nil {
		return e
	}
	var wallet, policy string
	var hold *time.Time
	if e = tx.QueryRow(ctx, `SELECT wallet,policy_version,hold_until FROM interaction_erasure_jobs WHERE deletion_case_id=$1 FOR UPDATE`, v.CaseID).Scan(&wallet, &policy, &hold); e != nil {
		return e
	}
	if wallet != v.Wallet || policy != v.PolicyVersion || (hold == nil) != (v.HoldUntil == nil) || (hold != nil && !hold.Equal(*v.HoldUntil)) {
		return ErrRevision
	}
	tag, e := tx.Exec(ctx, `INSERT INTO interaction_erased_wallets(wallet,deletion_case_id,policy_version) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, v.Wallet, v.CaseID, v.PolicyVersion)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 1 {
		if _, e = tx.Exec(ctx, `INSERT INTO interaction_visibility_revision(scope,revision) VALUES('erasure_overlay',1) ON CONFLICT(scope) DO UPDATE SET revision=interaction_visibility_revision.revision+1`); e != nil {
			return e
		}
	}
	var existingCase string
	if e = tx.QueryRow(ctx, `SELECT deletion_case_id::text FROM interaction_erased_wallets WHERE wallet=$1`, v.Wallet).Scan(&existingCase); e != nil {
		return e
	}
	if existingCase != v.CaseID {
		return ErrRevision
	}
	return nil
}
func (s Store) ConsumeErasure(ctx context.Context, durable *delivery.Store, generation, subject string, wire []byte, ack delivery.Delivery) error {
	if durable == nil {
		return ErrDependency
	}
	return durable.Consume(ctx, "social_erasure", generation, subject, wire, ack, func(ctx context.Context, tx pgx.Tx, e delivery.Envelope) error {
		if e.EventType != "account.social.erasure.v1" {
			return ErrInvalid
		}
		var v ErasureCommand
		if e := json.Unmarshal(e.Payload, &v); e != nil {
			return e
		}
		return s.BeginErasureTx(ctx, tx, v)
	})
}
func (s Store) EraseBatch(ctx context.Context, caseID string) (ErasureReceipt, error) {
	var receipt ErasureReceipt
	if s.DB == nil {
		return receipt, ErrDependency
	}
	if _, e := uuid.Parse(caseID); e != nil {
		return receipt, ErrInvalid
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return receipt, e
	}
	defer rollback(tx)
	var wallet, policy, state string
	var stage int
	var count int64
	var completed, hold *time.Time
	if e = tx.QueryRow(ctx, `SELECT wallet FROM interaction_erasure_jobs WHERE deletion_case_id=$1`, caseID).Scan(&wallet); e != nil {
		return receipt, e
	}
	if e = policyLocks(ctx, tx, wallet); e != nil {
		return receipt, e
	}
	if e = tx.QueryRow(ctx, `SELECT wallet,policy_version,stage,state,rows_processed,completed_at,hold_until FROM interaction_erasure_jobs WHERE deletion_case_id=$1 FOR UPDATE`, caseID).Scan(&wallet, &policy, &stage, &state, &count, &completed, &hold); e != nil {
		return receipt, e
	}
	receipt = ErasureReceipt{caseID, wallet, policy, state, count, completed}
	if state == "completed" {
		return receipt, tx.Commit(ctx)
	}
	if e = policyLocks(ctx, tx, wallet); e != nil {
		return receipt, e
	}
	n := int64(0)
	switch stage {
	case 0:
		rows, err := tx.Query(ctx, `SELECT follower,followee FROM interaction_follows WHERE follower=$1 OR followee=$1 ORDER BY follower,followee LIMIT 1000 FOR UPDATE`, wallet)
		if err != nil {
			return receipt, err
		}
		requests := []Request{}
		for rows.Next() {
			var a, b string
			if err = rows.Scan(&a, &b); err != nil {
				rows.Close()
				return receipt, err
			}
			requests = append(requests, Request{Actor: a, ID: b, Action: "follow", Remove: true})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return receipt, err
		}
		for _, r := range requests {
			if _, e = tx.Exec(ctx, `DELETE FROM interaction_follows WHERE follower=$1 AND followee=$2`, r.Actor, r.ID); e != nil {
				return receipt, e
			}
			if e = s.recordChange(ctx, tx, r); e != nil {
				return receipt, e
			}
			n++
		}
	case 1:
		rows, err := tx.Query(ctx, `SELECT id,pool FROM interaction_comments WHERE author=$1 AND NOT removed ORDER BY id LIMIT 1000 FOR UPDATE`, wallet)
		if err != nil {
			return receipt, err
		}
		requests := []Request{}
		for rows.Next() {
			var id, pool string
			if err = rows.Scan(&id, &pool); err != nil {
				rows.Close()
				return receipt, err
			}
			requests = append(requests, Request{Actor: wallet, ID: id, Pool: pool, Action: "comment", Remove: true})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return receipt, err
		}
		for _, r := range requests {
			if _, e = tx.Exec(ctx, `UPDATE interaction_comments SET body='',removed=true,revision=revision+1 WHERE id=$1`, r.ID); e != nil {
				return receipt, e
			}
			if e = s.recordChange(ctx, tx, r); e != nil {
				return receipt, e
			}
			n++
		}
	case 2:
		rows, err := tx.Query(ctx, `SELECT target,kind FROM interaction_reactions WHERE actor=$1 ORDER BY target,kind LIMIT 1000 FOR UPDATE`, wallet)
		if err != nil {
			return receipt, err
		}
		requests := []Request{}
		for rows.Next() {
			var target, kind string
			if err = rows.Scan(&target, &kind); err != nil {
				rows.Close()
				return receipt, err
			}
			requests = append(requests, Request{Actor: wallet, ID: target, Kind: kind, Action: "reaction", Remove: true})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return receipt, err
		}
		for _, r := range requests {
			if _, e = tx.Exec(ctx, `DELETE FROM interaction_reactions WHERE actor=$1 AND target=$2 AND kind=$3`, wallet, r.ID, r.Kind); e != nil {
				return receipt, e
			}
			if e = s.recordChange(ctx, tx, r); e != nil {
				return receipt, e
			}
			n++
		}
	case 3:
		tag, err := tx.Exec(ctx, `DELETE FROM interaction_notification_intents WHERE ctid IN (SELECT ctid FROM interaction_notification_intents WHERE recipient=$1 OR target IN (SELECT id FROM interaction_callout_refs WHERE actor=$1) LIMIT 1000 FOR UPDATE)`, wallet)
		if err != nil {
			return receipt, err
		}
		n = tag.RowsAffected()
	case 4:
		tag, err := tx.Exec(ctx, `DELETE FROM interaction_callout_refs WHERE ctid IN (SELECT ctid FROM interaction_callout_refs WHERE actor=$1 LIMIT 1000 FOR UPDATE)`, wallet)
		if err != nil {
			return receipt, err
		}
		n = tag.RowsAffected()
	case 5:
		tag, err := tx.Exec(ctx, `DELETE FROM interaction_mutations WHERE ctid IN (SELECT ctid FROM interaction_mutations WHERE actor=$1 LIMIT 1000 FOR UPDATE)`, wallet)
		if err != nil {
			return receipt, err
		}
		n = tag.RowsAffected()
	case 6:
		if hold != nil && hold.After(time.Now()) {
			receipt.State = "retained"
			_, e = tx.Exec(ctx, `UPDATE interaction_erasure_jobs SET state='retained' WHERE deletion_case_id=$1`, caseID)
			if e != nil {
				return receipt, e
			}
			return receipt, tx.Commit(ctx)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM interaction_blocks WHERE ctid IN (SELECT ctid FROM interaction_blocks WHERE owner=$1 OR target=$1 LIMIT 1000 FOR UPDATE)`, wallet)
		if err != nil {
			return receipt, err
		}
		n = tag.RowsAffected()
	case 7:
		tag, err := tx.Exec(ctx, `DELETE FROM interaction_mutes WHERE ctid IN (SELECT ctid FROM interaction_mutes WHERE owner=$1 OR target=$1 LIMIT 1000 FOR UPDATE)`, wallet)
		if err != nil {
			return receipt, err
		}
		n = tag.RowsAffected()
	case 8:
		tag, err := tx.Exec(ctx, `UPDATE interaction_feed_projection SET removed=true,payload=convert_to((jsonb_set(jsonb_set(jsonb_set(convert_from(payload,'UTF8')::jsonb,'{removed}','true'),'{body}','""'),'{parent}','""'))::text,'UTF8') WHERE ctid IN (SELECT ctid FROM interaction_feed_projection WHERE (actor=$1 OR (convert_from(payload,'UTF8')::jsonb->>'action'='follow' AND convert_from(payload,'UTF8')::jsonb->>'target'=$1)) AND (NOT removed OR COALESCE(convert_from(payload,'UTF8')::jsonb->>'body','')<>'') ORDER BY generation,aggregate_id LIMIT 1000 FOR UPDATE)`, wallet)
		if err != nil {
			return receipt, err
		}
		n = tag.RowsAffected()
	case 9:
		if _, e = tx.Exec(ctx, `DELETE FROM interaction_preferences WHERE wallet=$1`, wallet); e != nil {
			return receipt, e
		}
		var at time.Time
		if e = tx.QueryRow(ctx, `SELECT transaction_timestamp()`).Scan(&at); e != nil {
			return receipt, e
		}
		receipt.State = "completed"
		receipt.CompletedAt = &at
		body, _ := json.Marshal(receipt)
		hash := sha256.Sum256(body)
		if _, e = tx.Exec(ctx, `INSERT INTO interaction_erasure_receipts(deletion_case_id,wallet,policy_version,completed_at,receipt_hash) VALUES($1,$2,$3,$4,$5)`, caseID, wallet, policy, at, hex.EncodeToString(hash[:])); e != nil {
			return receipt, e
		}
		if _, e = tx.Exec(ctx, `UPDATE interaction_erasure_jobs SET state='completed',completed_at=$2 WHERE deletion_case_id=$1`, caseID, at); e != nil {
			return receipt, e
		}
		return receipt, tx.Commit(ctx)
	default:
		return receipt, ErrInvalid
	}
	if n == 0 {
		stage++
	}
	count += n
	receipt.State = "running"
	receipt.RowsProcessed = count
	_, e = tx.Exec(ctx, `UPDATE interaction_erasure_jobs SET stage=$2,state='running',rows_processed=$3 WHERE deletion_case_id=$1`, caseID, stage, count)
	if e != nil {
		return receipt, e
	}
	return receipt, tx.Commit(ctx)
}
func (s Store) ErasureReceipt(ctx context.Context, caseID string) (ErasureReceipt, error) {
	var v ErasureReceipt
	if s.DB == nil {
		return v, ErrDependency
	}
	e := s.DB.QueryRow(ctx, `SELECT deletion_case_id::text,wallet,policy_version,state,rows_processed,completed_at FROM interaction_erasure_jobs WHERE deletion_case_id=$1`, caseID).Scan(&v.CaseID, &v.Wallet, &v.PolicyVersion, &v.State, &v.RowsProcessed, &v.CompletedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrInvalid
	}
	return v, e
}
