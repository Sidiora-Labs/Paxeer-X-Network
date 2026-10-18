package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"regexp"
	"time"
)

type Action struct {
	ID, Kind, Scope, Reason, RequestedBy, ApprovedBy, State, JobID, PayloadHash string
	Revision                                                                    uint64
	Expires                                                                     time.Time
	Approver                                                                    Principal
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Propose fixes kind, scope, reason and payload digest for the entire lifecycle.
// Repeat keys with different content are conflicts, including terminal actions.
func (s *Store) Propose(ctx context.Context, cred, k, kind, scope, reason, payloadHash string, expires time.Time) (Action, error) {
	var a Action
	if !key(k) || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(payloadHash) || (kind != "replay" && kind != "dlq_redrive" && kind != "bulk_export" && kind != "cap_change" && kind != "incident_recovery") {
		return a, Invalid
	}
	e := s.run(ctx, cred, "action", scope, reason, func(t *transaction) error {
		if !expires.After(t.now) || expires.After(t.now.Add(24*time.Hour)) {
			return Invalid
		}
		old := Action{}
		if e := t.get("action", k, &old); e == nil {
			if old.Kind != kind || old.Scope != scope || old.Reason != reason || old.RequestedBy != t.actor.ID || old.PayloadHash != payloadHash || !old.Expires.Equal(expires) {
				return Conflict
			}
			a = old
			return nil
		} else if e != Missing {
			return e
		}
		a = Action{ID: id(), Kind: kind, Scope: scope, Reason: reason, RequestedBy: t.actor.ID, State: "proposed", PayloadHash: payloadHash, Revision: 1, Expires: expires}
		return t.put("action", k, a)
	})
	return a, e
}
func (s *Store) Transition(ctx context.Context, cred, k, scope, reason, next string, expected uint64) (Action, error) {
	var a Action
	if !key(k) {
		return a, Invalid
	}
	e := s.run(ctx, cred, "action", scope, reason, func(t *transaction) error {
		if e := t.get("action", k, &a); e != nil {
			return e
		}
		if a.Scope != scope {
			return Denied
		}
		// Same execution request returns one durable job even after completion.
		if next == "executing" && a.JobID != "" {
			return nil
		}
		if a.Revision != expected || a.Revision == ^uint64(0) {
			return Conflict
		}
		if next != "cancelled" && !a.Expires.After(t.now) {
			return Denied
		}
		switch next {
		case "approved":
			if a.State != "proposed" || a.RequestedBy == t.actor.ID {
				return Denied
			}
			a.ApprovedBy = t.actor.ID
			a.Approver = t.actor
		case "executing":
			if a.State != "approved" || a.ApprovedBy == a.RequestedBy || !t.approvalActive(a) {
				return Denied
			}
			if a.Kind == "cap_change" || a.Kind == "incident_recovery" {
				return Denied
			}
			a.JobID = id()
			if e := t.put("job", a.JobID, map[string]any{"actionId": a.ID, "kind": a.Kind, "scope": scope, "state": "queued", "payloadHash": a.PayloadHash}); e != nil {
				return e
			}
		case "cancelled":
			if a.State != "proposed" && a.State != "approved" {
				return Denied
			}
		case "succeeded", "failed": // Executors must attach results through the owning durable job processor.
			return Unavailable
		default:
			return Invalid
		}
		a.State = next
		a.Revision++
		return t.put("action", k, a)
	})
	return a, e
}

type Cap struct {
	Capability         string
	Version, Limit     uint64
	Effective, Expires time.Time
	Revision           uint64
}

func capability(v string) bool {
	return v == "pool_creation" || v == "quote" || v == "socket" || v == "paid_features"
}

// Cap increases consume dual-approved cap_change actions bound to exact payload.
// Reductions can be immediate. Read-only market routes never consult this gate.
func (s *Store) SetCap(ctx context.Context, cred, scope, reason, approvalKey string, c Cap, expected uint64) error {
	if scope != "platform" {
		return Denied
	}
	if !capability(c.Capability) || c.Version != 1 {
		return Invalid
	}
	return s.run(ctx, cred, "caps", scope, reason, func(t *transaction) error {
		if !c.Expires.After(t.now) || c.Expires.After(t.now.Add(24*time.Hour)) || c.Effective.After(c.Expires) {
			return Invalid
		}
		var old Cap
		e := t.get("caps", c.Capability, &old)
		if e != nil && e != Missing {
			return e
		}
		if old.Revision != expected || expected == ^uint64(0) {
			return Conflict
		}
		c.Revision = expected + 1
		// Positive authority may bypass dual approval only when both quota and
		// active interval are subsets of existing authority. An expired/equal cap
		// cannot be re-enabled by extending time under a nominally unchanged limit.
		reducing := c.Limit == 0 || (old.Version == c.Version && c.Limit <= old.Limit && !c.Effective.Before(old.Effective) && !c.Expires.After(old.Expires))
		if !reducing {
			var a Action
			if !key(approvalKey) {
				return Denied
			}
			if e = t.get("action", approvalKey, &a); e != nil {
				return e
			}
			if a.Revision == ^uint64(0) {
				return Conflict
			}
			payload := c
			payload.Revision = 0
			if a.Kind != "cap_change" || a.Scope != scope || a.State != "approved" || a.PayloadHash != digest(payload) || !a.Expires.After(t.now) || a.ApprovedBy == a.RequestedBy || !t.approvalActive(a) {
				return Denied
			}
			a.State = "succeeded"
			a.Revision++
			if e = t.put("action", approvalKey, a); e != nil {
				return e
			}
		}
		return t.put("caps", c.Capability, c)
	})
}

type CapDecision struct {
	Allowed, Stale     bool
	Capability, Reason string
	Revision           uint64
}

func (s *Store) Admission(ctx context.Context, capabilityName string, knownVersion uint64) (CapDecision, error) {
	d := CapDecision{Capability: capabilityName, Stale: true, Reason: "cap_unavailable"}
	if !capability(capabilityName) {
		return d, Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var b []byte
	var now time.Time
	e := s.pool.QueryRow(ctx, "SELECT body,clock_timestamp() FROM "+s.schema+".objects WHERE kind='caps' AND id=$1", capabilityName).Scan(&b, &now)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return d, nil
		}
		return d, e
	}
	var c Cap
	if e = json.Unmarshal(b, &c); e != nil {
		return d, e
	}
	d.Revision = c.Revision
	if c.Version != 1 || knownVersion != c.Version || !c.Expires.After(now) || c.Effective.After(now) {
		return d, nil
	}
	d.Stale = false
	d.Allowed = c.Limit > 0
	d.Reason = "cap_disabled"
	if d.Allowed {
		d.Reason = "available"
	}
	return d, nil
}

// JobResultVerifier reads the owning executor's durable result/checkpoint. It
// must not execute side effects. Missing executor proof leaves action executing.
type JobResultVerifier interface {
	Verify(context.Context, Action, string, string) error
}

func (s *Store) CompleteAction(ctx context.Context, cred, k, scope, reason, next, reference string, expected uint64, verifier JobResultVerifier) error {
	if !key(k) || !text(reference, 256) || (next != "succeeded" && next != "failed") || verifier == nil {
		return Unavailable
	}
	return s.run(ctx, cred, "action", scope, reason, func(t *transaction) error {
		var a Action
		if e := t.get("action", k, &a); e != nil {
			return e
		}
		if a.Scope != scope {
			return Denied
		}
		if a.Revision != expected || a.Revision == ^uint64(0) {
			return Conflict
		}
		if a.State != "executing" || a.JobID == "" || !t.approvalActive(a) {
			return Denied
		}
		if e := verifier.Verify(contextFor(t), a, next, reference); e != nil {
			return Unavailable
		}
		if !t.approvalActive(a) {
			return Denied
		}
		var job map[string]any
		if e := t.get("job", a.JobID, &job); e != nil {
			return e
		}
		job["state"] = next
		job["resultReference"] = reference
		a.State = next
		a.Revision++
		if e := t.put("job", a.JobID, job); e != nil {
			return e
		}
		return t.put("action", k, a)
	})
}
