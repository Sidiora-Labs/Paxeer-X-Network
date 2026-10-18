package streams

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"strings"
	"time"
)

type ProvisionWorker struct {
	Store        *LifecycleStore
	Provider     *livepeer.Client
	Credentials  *EncryptedCredentials
	Restrictions *RestrictionReader
}

func (w *ProvisionWorker) Step(ctx context.Context, id string) error {
	if w == nil || w.Store == nil || w.Provider == nil || w.Credentials == nil || w.Credentials.Store != w.Store || w.Credentials.Transit == nil || w.Restrictions == nil || w.Restrictions.Pool != w.Store.pool {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	owner, err := randomID()
	if err != nil {
		return ErrUnavailable
	}
	claimed, err := w.Store.claimWorker(bounded, id, owner)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrConflict
	}
	outcome := "provider_unavailable"
	defer func() {
		release, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = w.Store.releaseWorker(release, id, owner, outcome)
	}()
	o, err := w.Store.Get(bounded, id)
	if err != nil {
		return err
	}
	if o.ProviderID != "" || (o.State != "REQUESTED" && o.State != "PROVISIONING" && o.State != "ENDING" && o.State != "SUSPENDED") {
		outcome = "idle"
		return nil
	}
	allowed, err := w.Restrictions.Allowed(bounded, o.Wallet, o.Pool)
	if err != nil {
		return ErrUnavailable
	}
	if !allowed && o.State != "ENDING" && o.State != "SUSPENDED" {
		if o.State == "REQUESTED" {
			_, err = w.Store.FailBeforeProvision(bounded, o.ID, o.Revision, ProvisionPolicyRevoked)
			outcome = "policy_revoked"
			return err
		}
		outcome = "policy_revocation_pending"
		o, err = w.Store.Suspend(bounded, o.ID, SuspensionDecision{OperationID: o.ID, Revision: o.Revision, Actor: "policy_reconciler", Reason: "active_restriction", Allowed: true, ExpiresAt: time.Now().Add(15 * time.Second)})
		if err != nil {
			return err
		}
	}
	if o.State == "REQUESTED" {
		o, err = w.Store.BeginProvision(bounded, id)
		if err != nil {
			return err
		}
	}
	remote, err := w.Provider.FindCorrelated(bounded, o.ID)
	if err != nil && !errors.Is(err, livepeer.ErrCorrelationMissing) {
		return ErrUnavailable
	}
	if errors.Is(err, livepeer.ErrCorrelationMissing) {
		if o.State == "ENDING" || o.State == "SUSPENDED" {
			err = w.Store.finishUnallocated(bounded, o.ID, owner)
			if err != nil {
				outcome = "provider_create_ambiguous"
				return err
			}
			outcome = "idle"
			return nil
		}
		tag, e := w.Store.pool.Exec(bounded, fmt.Sprintf("UPDATE %s.stream_worker_attempts SET create_sent=true WHERE operation_id=$1 AND owner=$2 AND lease_until>clock_timestamp() AND create_sent=false AND EXISTS(SELECT 1 FROM %s.stream_operations WHERE id=$1 AND state='PROVISIONING')", w.Store.schema, w.Store.schema), id, owner)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			outcome = "provider_create_ambiguous"
			return ErrUnavailable
		}
		created, e := w.Provider.CreateCorrelated(bounded, o.ID)
		if e != nil {
			outcome = "provider_create_ambiguous"
			return ErrUnavailable
		}
		remote = &livepeer.Stream{ID: created.ID, StreamKey: created.StreamKey, PlaybackID: created.PlaybackID, Name: created.Name}
	}
	if err = w.Provider.EnsurePlaybackPolicy(bounded, remote.ID, o.ID); err != nil {
		outcome = "playback_policy_unconfirmed"
		return ErrUnavailable
	}
	cipher, err := w.Credentials.Transit.Seal(bounded, o.ID, remote.ID, remote.StreamKey)
	if err != nil {
		outcome = "credential_encryption_unavailable"
		return ErrUnavailable
	}
	allowed, err = w.Restrictions.Allowed(bounded, o.Wallet, o.Pool)
	if err != nil {
		outcome = "policy_confirmation_unavailable"
		return ErrUnavailable
	}
	_, err = w.Store.bindEncryptedProvider(bounded, o.ID, remote.ID, cipher, w.Credentials.Transit.Key, !allowed)
	if err != nil {
		return err
	}
	outcome = "provider_adopted"
	return nil
}
func (s *LifecycleStore) claimWorker(ctx context.Context, id, owner string) (bool, error) {
	if !frozenUUID.MatchString(id) || !frozenUUID.MatchString(owner) {
		return false, ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_worker_attempts(operation_id,owner,lease_until,attempt)VALUES($1,$2,clock_timestamp()+interval '30 seconds',1) ON CONFLICT(operation_id)DO UPDATE SET owner=$2,lease_until=clock_timestamp()+interval '30 seconds',attempt=%s.stream_worker_attempts.attempt+1 WHERE %s.stream_worker_attempts.lease_until<clock_timestamp() AND %s.stream_worker_attempts.retry_at<=clock_timestamp()", s.schema, s.schema, s.schema, s.schema), id, owner)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
func (s *LifecycleStore) releaseWorker(ctx context.Context, id, owner, reason string) error {
	if !identityPattern.MatchString(reason) {
		return ErrInvalid
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf("WITH released AS (UPDATE %s.stream_worker_attempts SET owner='',lease_until='-infinity',last_reason=$3,retry_at=clock_timestamp()+CASE WHEN $3 IN('reconciled','provider_adopted','idle')THEN interval '30 seconds' ELSE (LEAST(300,POWER(2,LEAST(attempt,8)))+random()*5)*interval '1 second' END WHERE operation_id=$1 AND owner=$2 RETURNING retry_at) UPDATE %s.stream_operations SET retry_at=CASE WHEN $3 IN('reconciled','provider_adopted','idle')THEN NULL ELSE released.retry_at END,last_reason=$3 FROM released WHERE id=$1", s.schema, s.schema), id, owner, reason)
	return err
}
func (w *ProvisionWorker) Run(ctx context.Context) error {
	if w == nil || w.Store == nil {
		return ErrUnavailable
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			rows, err := w.Store.pool.Query(ctx, fmt.Sprintf("SELECT o.id FROM %s.stream_operations o LEFT JOIN %s.stream_worker_attempts w ON w.operation_id=o.id WHERE o.provider_id IS NULL AND o.state IN('REQUESTED','PROVISIONING','ENDING','SUSPENDED') AND COALESCE(w.retry_at,'-infinity'::timestamptz)<=clock_timestamp() ORDER BY o.created_at LIMIT 10", w.Store.schema, w.Store.schema))
			if err != nil {
				return err
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, id := range ids {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				_ = w.Step(ctx, id)
			}
			if err = w.rotations(ctx); err != nil {
				return err
			}
		}
	}
}

func (w *ProvisionWorker) RotationStep(ctx context.Context, id string) error {
	if w == nil || w.Store == nil || w.Provider == nil || w.Restrictions == nil {
		return ErrUnavailable
	}
	rotation, err := w.Store.GetKeyRotation(ctx, id)
	if err != nil {
		return err
	}
	if rotation.NewStreamID != nil {
		next, err := w.Store.Get(ctx, *rotation.NewStreamID)
		if err != nil {
			return err
		}
		if next.State == "READY" || next.State == "LIVE" {
			_, err = w.Store.pool.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_key_rotations SET status='ready',revision=revision+1 WHERE old_stream_id=$1 AND status<>'ready'", w.Store.schema), id)
			return err
		}
		return nil
	}
	old, err := w.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if old.State != "ENDED" || old.Intent != "rotate" {
		return ErrUnavailable
	}
	_, err = w.Provider.GetStream(ctx, old.ProviderID)
	var pe *livepeer.ProviderError
	if !errors.As(err, &pe) || pe.Status != 404 {
		return ErrUnavailable
	}
	allowed, err := w.Restrictions.Allowed(ctx, old.Wallet, old.Pool)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrDenied
	}
	tx, err := w.Store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	var previous *string
	if err = tx.QueryRow(ctx, fmt.Sprintf("SELECT new_stream_id FROM %s.stream_key_rotations WHERE old_stream_id=$1 FOR UPDATE", w.Store.schema), id).Scan(&previous); err != nil {
		return err
	}
	if previous != nil {
		return tx.Commit(ctx)
	}
	var creator string
	if err = tx.QueryRow(ctx, `SELECT creator FROM indexer.indexer_canonical_creations_v1 WHERE pool=$1`, old.Pool).Scan(&creator); err != nil {
		return ErrUnavailable
	}
	if !strings.EqualFold(creator, old.Wallet) {
		return ErrDenied
	}
	allowed, err = w.Restrictions.AllowedTx(ctx, tx, old.Wallet, old.Pool)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrDenied
	}
	next, err := w.Store.CreateTx(ctx, tx, CreateIntent{Wallet: old.Wallet, Pool: old.Pool, Title: old.Title, IdempotencyKey: "rotate_" + id}, CreatorDecision{Wallet: old.Wallet, Pool: old.Pool, Allowed: true, ExpiresAt: time.Now().Add(time.Minute)}, true)
	if err != nil {
		return err
	}
	private, _, err := w.Store.PlaybackPrivacy(ctx, old.ID)
	if err != nil {
		return err
	}
	if private {
		if err = w.Store.SetPlaybackPrivacyTx(ctx, tx, next, true); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("UPDATE %s.stream_key_rotations SET new_stream_id=$2,status='provisioning',revision=revision+1 WHERE old_stream_id=$1", w.Store.schema), id, next.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'rotation_worker','old_ingest_key_revocation_confirmed',$2)", w.Store.schema), id, old.Revision)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (w *ProvisionWorker) rotations(ctx context.Context) error {
	rows, err := w.Store.pool.Query(ctx, fmt.Sprintf("SELECT old_stream_id FROM %s.stream_key_rotations WHERE status<>'ready' ORDER BY created_at LIMIT 10", w.Store.schema))
	if err != nil {
		return err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		_ = w.RotationStep(ctx, id)
	}
	return nil
}

func (w *ProvisionWorker) compensateRevoked(ctx context.Context, o Operation) error {
	remote, err := w.Provider.FindCorrelated(ctx, o.ID)
	if err != nil {
		return ErrUnavailable
	}
	cipher, err := w.Credentials.Transit.Seal(ctx, o.ID, remote.ID, remote.StreamKey)
	if err != nil {
		return ErrUnavailable
	}
	_, err = w.Store.bindEncryptedProvider(ctx, o.ID, remote.ID, cipher, w.Credentials.Transit.Key, true)
	return err
}

func (s *LifecycleStore) finishUnallocated(ctx context.Context, id, owner string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackBounded(tx)
	o, err := readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state='ENDED',terminal_at=clock_timestamp(),retry_at=NULL,last_reason='ended_before_provider_allocation',revision=revision+1 WHERE id=$1 AND provider_id IS NULL AND state IN('ENDING','SUSPENDED') AND EXISTS(SELECT 1 FROM %s.stream_worker_attempts WHERE operation_id=$1 AND owner=$2 AND lease_until>clock_timestamp() AND create_sent=false) RETURNING "+opColumns, s.schema, s.schema), id, owner))
	if err != nil {
		return ErrUnavailable
	}
	if _, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,'provisioner','ended_before_provider_allocation',$2)", s.schema), id, o.Revision); err != nil {
		return err
	}
	if err = s.enqueue(ctx, tx, o, "provider_state"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
