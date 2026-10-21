package streams

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"time"
)

type ReconcileWorker struct {
	Store        *LifecycleStore
	Provider     *livepeer.Client
	Restrictions *RestrictionReader
	Disagreement func(string, time.Duration)
}

func (w *ReconcileWorker) Step(ctx context.Context, id string) error {
	if w == nil || w.Store == nil || w.Provider == nil || w.Restrictions == nil {
		return ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	owner, err := randomID()
	if err != nil {
		return err
	}
	claimed, err := w.Store.claimWorker(bounded, id, owner)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrConflict
	}
	reason := "reconcile_unavailable"
	defer func() {
		c, x := context.WithTimeout(context.Background(), time.Second)
		defer x()
		_ = w.Store.releaseWorker(c, id, owner, reason)
	}()
	o, err := w.Store.Get(bounded, id)
	if err != nil {
		return err
	}
	if o.State == "ENDED" || o.State == "FAILED" || o.ProviderID == "" {
		reason = "idle"
		return nil
	}
	allowed, err := w.Restrictions.Allowed(bounded, o.Wallet, o.Pool)
	if err != nil {
		return err
	}
	if !allowed && o.State != "SUSPENDED" {
		o, err = w.Store.Suspend(bounded, id, SuspensionDecision{OperationID: id, Revision: o.Revision, Actor: "policy_reconciler", Reason: "active_restriction", Allowed: true, ExpiresAt: time.Now().Add(15 * time.Second)})
		if err != nil {
			return err
		}
	}
	if o.State == "SUSPENDED" || o.Intent == "rotate" {
		if err = w.Provider.DeleteStream(bounded, o.ProviderID); err != nil {
			var deleted *livepeer.ProviderError
			if !errors.As(err, &deleted) || deleted.Status != 404 {
				_ = w.Store.TerminationFailed(bounded, id)
				return ErrUnavailable
			}
		}
	}
	if o.State == "ENDING" && o.Intent != "rotate" {
		if err = w.Store.ReconcileTermination(bounded, id, w.Provider); err != nil {
			return err
		}
	}
	remote, err := w.Provider.GetStream(bounded, o.ProviderID)
	state := "READY"
	var providerError *livepeer.ProviderError
	if err != nil {
		if (o.State == "ENDING" || o.State == "SUSPENDED" || o.Intent == "rotate") && errors.As(err, &providerError) && providerError.Status == 404 {
			state = "ENDED"
		} else {
			return ErrUnavailable
		}
	} else if remote.IsActive {
		state = "LIVE"
	} else if o.State == "ENDING" {
		state = "ENDED"
	}
	now := time.Now().UTC()
	if state != o.State {
		_, err = w.Store.pool.Exec(bounded, fmt.Sprintf("UPDATE %s.stream_worker_attempts SET disagreement_at=COALESCE(disagreement_at,clock_timestamp()) WHERE operation_id=$1", w.Store.schema), id)
		if err != nil {
			return err
		}
		var since time.Time
		if err = w.Store.pool.QueryRow(bounded, fmt.Sprintf("SELECT disagreement_at FROM %s.stream_worker_attempts WHERE operation_id=$1", w.Store.schema), id).Scan(&since); err != nil {
			return err
		}
		if now.Sub(since) >= 120*time.Second && w.Disagreement != nil {
			w.Disagreement("provider_state_disagreement", now.Sub(since))
		}
	} else {
		_, err = w.Store.pool.Exec(bounded, fmt.Sprintf("UPDATE %s.stream_worker_attempts SET disagreement_at=NULL WHERE operation_id=$1", w.Store.schema), id)
		if err != nil {
			return err
		}
	}
	eventID, err := randomID()
	if err != nil {
		return err
	}
	_, err = w.Store.Observe(bounded, ProviderEvent{EventID: eventID, ProviderID: o.ProviderID, State: state, ObservedAt: now})
	if err != nil {
		return err
	}
	reason = "reconciled"
	return nil
}
func (w *ReconcileWorker) Run(ctx context.Context) error {
	if w == nil || w.Store == nil {
		return ErrUnavailable
	}
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			rows, err := w.Store.pool.Query(ctx, fmt.Sprintf("SELECT o.id FROM %s.stream_operations o LEFT JOIN %s.stream_worker_attempts w ON w.operation_id=o.id WHERE o.provider_id IS NOT NULL AND o.state NOT IN('ENDED','FAILED') AND COALESCE(w.retry_at,'-infinity'::timestamptz)<=clock_timestamp() AND COALESCE(w.lease_until,'-infinity'::timestamptz)<clock_timestamp() ORDER BY COALESCE(w.retry_at,'-infinity'::timestamptz),o.created_at LIMIT 100", w.Store.schema, w.Store.schema))
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
			if w.Store.Budget != nil {
				if err = w.Store.EmitBudgetAlerts(ctx); err != nil {
					return err
				}
			}
		}
	}
}
