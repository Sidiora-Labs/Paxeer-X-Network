package control

import (
	"context"
	"time"
)

type Incident struct {
	ID, Scope, ServiceIdentity, State, DetectionReference, IntegrityReference, RotationReference, PublicStatus string
	Revision                                                                                                   uint64
	Detected, Contained                                                                                        time.Time
}

func (s *Store) DetectIncident(ctx context.Context, cred, scope, k, reason, identity, evidence string) error {
	if !key(k) || !key(identity) || !text(evidence, 256) {
		return Invalid
	}
	return s.run(ctx, cred, "incident", scope, reason, func(t *transaction) error {
		var old Incident
		if e := t.get("incident", k, &old); e == nil {
			return Conflict
		} else if e != Missing {
			return e
		}
		i := Incident{ID: k, Scope: scope, ServiceIdentity: identity, State: "contained", DetectionReference: evidence, PublicStatus: "privileged service unavailable during integrity investigation", Revision: 1, Detected: t.now, Contained: t.now}
		if e := t.put("quarantine", identity, map[string]string{"incident": k, "state": "contained"}); e != nil {
			return e
		}
		if e := t.put("incident_outbox", k, map[string]string{"state": "pending", "operation": "revoke_secrets_invalidate_caches", "identity": identity}); e != nil {
			return e
		}
		return t.put("incident", k, i)
	})
}

// Rotation acknowledgments must come from the owning secret/cache adapters, not
// consumer routes. This method only records externally verified acknowledgments.
type RecoveryVerifier interface {
	Check(context.Context, Incident, string, string) error
}

func (s *Store) RecoverIncident(ctx context.Context, cred, scope, k, reason, approvalKey, rotation, integrity string, expected uint64, verifier RecoveryVerifier) error {
	if !key(k) || !key(approvalKey) || !text(rotation, 256) || !text(integrity, 256) || verifier == nil {
		return Unavailable
	}
	return s.run(ctx, cred, "incident", scope, reason, func(t *transaction) error {
		var i Incident
		if e := t.get("incident", k, &i); e != nil {
			return e
		}
		if i.Scope != scope || i.Revision != expected || i.Revision == ^uint64(0) {
			return Conflict
		}
		if i.State != "contained" {
			return Denied
		}
		var a Action
		if e := t.get("action", approvalKey, &a); e != nil {
			return e
		}
		payload := map[string]string{"incident": k, "rotation": rotation, "integrity": integrity}
		if a.Revision == ^uint64(0) {
			return Conflict
		}
		if a.Kind != "incident_recovery" || a.Scope != scope || a.State != "approved" || !a.Expires.After(t.now) || a.PayloadHash != digest(payload) || a.ApprovedBy == a.RequestedBy || !t.approvalActive(a) {
			return Denied
		}
		if e := verifier.Check(contextFor(t), i, rotation, integrity); e != nil {
			return Unavailable
		}
		if !t.approvalActive(a) {
			return Denied
		}
		// A later incident for the same identity must never be cleared by recovery of
		// an older incident whose dual approval is still valid.
		var q map[string]string
		if e := t.get("quarantine", i.ServiceIdentity, &q); e != nil {
			return e
		}
		if q["incident"] != k {
			return Conflict
		}
		if _, e := t.Exec(contextFor(t), "DELETE FROM "+s.schema+".objects WHERE kind='quarantine' AND id=$1", i.ServiceIdentity); e != nil {
			return e
		}
		a.State = "succeeded"
		a.Revision++
		i.State = "recovered"
		i.Revision++
		i.RotationReference = rotation
		i.IntegrityReference = integrity
		i.PublicStatus = "service integrity verified; privileged operations restored"
		if e := t.put("action", approvalKey, a); e != nil {
			return e
		}
		return t.put("incident", k, i)
	})
}

// Access permits only explicit role-specific datasets; there is no superuser
// inheritance and no support export of DMs or custody material.
func (s *Store) Access(ctx context.Context, cred, scope, reason, dataset, approvalKey string, bulk bool) error {
	op := map[string]string{"support_public": "support_read", "moderation_queue": "moderation_read", "finance_liabilities": "finance_read", "infra_health": "infrastructure_read"}[dataset]
	if op == "" {
		op = "prohibited_data"
	}
	return s.run(ctx, cred, op, scope, reason, func(t *transaction) error {
		if !bulk {
			return nil
		}
		if !key(approvalKey) {
			return Denied
		}
		var a Action
		if e := t.get("action", approvalKey, &a); e != nil {
			return e
		}
		if a.Revision == ^uint64(0) {
			return Conflict
		}
		if a.Kind != "bulk_export" || a.Scope != scope || a.State != "approved" || !a.Expires.After(t.now) || a.Expires.After(t.now.Add(24*time.Hour)) || a.PayloadHash != digest(map[string]string{"dataset": dataset, "scope": scope}) || a.ApprovedBy == a.RequestedBy || !t.approvalActive(a) {
			return Denied
		}
		a.State = "succeeded"
		a.Revision++
		return t.put("action", approvalKey, a)
	})
}
