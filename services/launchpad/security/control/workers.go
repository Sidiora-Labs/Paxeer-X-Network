package control

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type SettlementWorker struct {
	store    *Store
	provider *PayoutProvider
}
type payoutAttempt struct {
	Claim            string
	Until            time.Time
	Submitted        bool
	State, Reference string
}

func NewSettlementWorker(store *Store, provider *PayoutProvider) (*SettlementWorker, error) {
	if store == nil || store.pool == nil || store.identity == nil || store.verifier == nil || provider == nil || provider.endpoint == nil || provider.client == nil || !text(provider.Controller, 256) || !destination(provider.AuthorityReference) {
		return nil, Unavailable
	}
	return &SettlementWorker{store, provider}, nil
}
func (w *SettlementWorker) Process(ctx context.Context, credential, scope, k, reason string) (Obligation, error) {
	var obligation Obligation
	claim := id()
	var attempt payoutAttempt
	err := w.store.run(ctx, credential, "settlement", scope, reason, func(t *transaction) error {
		if err := t.get("settlement", k, &obligation); err != nil {
			return err
		}
		if obligation.Scope != scope {
			return Denied
		}
		if obligation.State == "settled" {
			return nil
		}
		if obligation.AuthorityReference != w.provider.AuthorityReference {
			return Denied
		}
		if err := t.get("payout_attempt", k, &attempt); err != nil && err != Missing {
			return err
		}
		if attempt.Claim != "" && attempt.Until.After(t.now) {
			return Unavailable
		}
		attempt.Claim = claim
		attempt.Until = t.now.Add(15 * time.Second)
		return t.put("payout_attempt", k, attempt)
	})
	if err != nil || obligation.State == "settled" {
		return obligation, err
	}
	result, providerErr := w.provider.Lookup(ctx, k)
	if providerErr == nil && result.State == "absent" && !attempt.Submitted {
		err = w.store.run(ctx, credential, "settlement", scope, reason, func(t *transaction) error {
			var current payoutAttempt
			if err := t.get("payout_attempt", k, &current); err != nil {
				return err
			}
			if current.Claim != claim || !current.Until.After(t.now) || current.Submitted {
				return Conflict
			}
			current.Submitted = true
			current.State = "unknown"
			return t.put("payout_attempt", k, current)
		})
		if err != nil {
			return obligation, err
		}
		result, providerErr = w.provider.Submit(ctx, obligation)
	}
	if providerErr != nil || result.State == "absent" {
		result = PayoutResult{State: "unknown"}
	}
	err = w.store.run(ctx, credential, "settlement", scope, reason, func(t *transaction) error {
		var current payoutAttempt
		if err := t.get("payout_attempt", k, &current); err != nil {
			return err
		}
		if current.Claim != claim {
			return Conflict
		}
		current.State = result.State
		current.Reference = result.Reference
		current.Claim = ""
		current.Until = time.Time{}
		var o Obligation
		if err := t.get("settlement", k, &o); err != nil {
			return err
		}
		if o.Scope != scope {
			return Denied
		}
		if o.State != "settled" {
			if o.Revision == ^uint64(0) {
				return Conflict
			}
			o.State = "pending"
			if result.State == "unknown" {
				o.State = "unknown"
			}
			o.Revision++
			if err := t.put("settlement", k, o); err != nil {
				return err
			}
		}
		return t.put("payout_attempt", k, current)
	})
	if err != nil {
		return obligation, err
	}
	if result.Reference != "" {
		if err = w.store.ObserveSettlement(ctx, credential, scope, k, reason, result.Reference); err != nil {
			return obligation, err
		}
	}
	err = w.store.Read(ctx, credential, scope, reason, "settlement", k, &obligation)
	if err == nil && (providerErr != nil || result.State == "unknown") {
		err = Unavailable
	}
	return obligation, err
}

type ReconciliationWorker struct {
	Store      *Store
	Credential string
	Assets     []Asset
	Interval   time.Duration
	Observe    func(string, error)
}

func (w ReconciliationWorker) Run(ctx context.Context) error {
	if w.Store == nil || w.Credential == "" || len(w.Assets) == 0 || len(w.Assets) > 2 || w.Interval <= 0 || w.Interval > 60*time.Second || w.Observe == nil {
		return Invalid
	}
	for _, a := range w.Assets {
		if !a.Valid() {
			return Invalid
		}
	}
	ticker := time.NewTicker(w.Interval)
	defer ticker.Stop()
	for {
		for _, asset := range w.Assets {
			err := w.Store.Reconcile(ctx, w.Credential, "platform", "scheduled confirmed inventory reconciliation", asset)
			w.Observe(asset.Key(), err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type IncidentWorker struct {
	Store      *Store
	Identity   *PostgresIdentity
	Revocation *RevocationProvider
	Credential string
	Observe    func(string, error)
}

func (w IncidentWorker) Drain(ctx context.Context) error {
	if w.Store == nil || w.Identity == nil || w.Credential == "" || w.Observe == nil {
		return Invalid
	}
	rows, err := w.Store.pool.Query(ctx, "SELECT body FROM "+w.Store.schema+".objects WHERE kind='incident' AND body->>'State'='contained' ORDER BY id LIMIT 100")
	if err != nil {
		return err
	}
	var pending []Incident
	for rows.Next() {
		var b []byte
		var incident Incident
		if err = rows.Scan(&b); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(b, &incident); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, incident)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, incident := range pending {
		err = w.Store.run(ctx, w.Credential, "incident", incident.Scope, "revoke compromised identity credentials", func(t *transaction) error {
			var outbox map[string]string
			if err := t.get("incident_outbox", incident.ID, &outbox); err != nil {
				return err
			}
			if outbox["state"] == "credentials_revoked" || outbox["state"] == "completed" {
				return nil
			}
			if outbox["identity"] != incident.ServiceIdentity {
				return Conflict
			}
			if _, err := t.Exec(contextFor(t), "UPDATE "+w.Store.schema+".authority_credentials SET revoked_at=clock_timestamp() WHERE actor_id::text=$1 AND revoked_at IS NULL", incident.ServiceIdentity); err != nil {
				return err
			}
			outbox["state"] = "credentials_revoked"
			outbox["rotation"] = "credential-revocation:" + incident.ID
			return t.put("incident_outbox", incident.ID, outbox)
		})
		w.Observe(incident.ID, err)
		if err != nil {
			return err
		}
		if w.Revocation == nil {
			w.Observe(incident.ID, Unavailable)
			continue
		}
		ack, revocationErr := w.Revocation.Revoke(ctx, incident)
		if revocationErr != nil {
			w.Observe(incident.ID, revocationErr)
			continue
		}
		err = w.Store.run(ctx, w.Credential, "incident", incident.Scope, "record verified secret revocation and cache invalidation", func(t *transaction) error {
			var outbox map[string]string
			if err := t.get("incident_outbox", incident.ID, &outbox); err != nil {
				return err
			}
			if outbox["identity"] != ack.Identity {
				return Conflict
			}
			outbox["state"] = "completed"
			outbox["rotation"] = ack.Rotation
			outbox["integrity"] = ack.Integrity
			return t.put("incident_outbox", incident.ID, outbox)
		})
		w.Observe(incident.ID, err)
		if err != nil {
			return err
		}
	}
	return nil
}
func (w IncidentWorker) Run(ctx context.Context) error {
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		if err := w.Drain(ctx); err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *SettlementWorker) Drain(ctx context.Context, credential string, observe func(string, error)) error {
	if observe == nil || credential == "" {
		return Invalid
	}
	rows, err := w.store.pool.Query(ctx, "SELECT id,body FROM "+w.store.schema+".objects WHERE kind='settlement' AND body->>'State' IN ('pending','unknown') ORDER BY id LIMIT 100")
	if err != nil {
		return err
	}
	type item struct {
		key        string
		obligation Obligation
	}
	var pending []item
	for rows.Next() {
		var k string
		var raw []byte
		var o Obligation
		if err = rows.Scan(&k, &raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item{k, o})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range pending {
		_, err = w.Process(ctx, credential, p.obligation.Scope, p.key, "reconcile durable settlement obligation")
		observe(p.key, err)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
func (w *SettlementWorker) Run(ctx context.Context, credential string, observe func(string, error)) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if err := w.Drain(ctx, credential, observe); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
