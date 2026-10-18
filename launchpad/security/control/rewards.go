package control

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// Verifier is a configured read-only authority, never a provider result supplied
// by a request. Implementations independently verify chain and deployment pins.
type Observation struct {
	Asset                                Asset
	Balance, Reference, Manifest, Source string
	At                                   time.Time
}
type SettlementEvidence struct {
	Reference, Manifest         string
	Asset                       Asset
	Amount, Destination, Source string
	Successful                  bool
}
type EvidenceVerifier interface {
	Inventory(context.Context, Asset) (Observation, error)
	Settlement(context.Context, Obligation, string) (SettlementEvidence, error)
	Award(context.Context, Bounty, string, string) error
}
type Inventory struct {
	Asset                                           Asset
	Confirmed, Pending, Reserved, Settled, Reversed string
	Frozen                                          bool
	Reference, Manifest, Source                     string
	CheckedAt                                       time.Time
	Revision                                        uint64
}
type Bounty struct {
	ID, Scope, Title, Rules, FundingSource, RefundDisposition, Amount, State, FundingReference, AwardEvidence, Reviewer, Destination string
	Asset                                                                                                                            Asset
	Deadline                                                                                                                         time.Time
	Revision                                                                                                                         uint64
}
type Obligation struct {
	ID, Scope, BountyID, IdempotencyKey, Amount, Destination, AuthorityReference, State, SettlementReference, AssetKey string
	Asset                                                                                                              Asset
	Created                                                                                                            time.Time
	Revision                                                                                                           uint64
}

var addressPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

func destination(v string) bool {
	return addressPattern.MatchString(v) && v != "0x0000000000000000000000000000000000000000"
}
func fresh(now, at time.Time) bool { return !at.After(now) && now.Sub(at) <= 60*time.Second }

// Reconcile checks independent external inventory, freezing new reservations on
// unexplained drift. Pending/reserved obligations are preserved on mismatch.
func (s *Store) Reconcile(ctx context.Context, cred, scope, reason string, a Asset) error {
	if scope != "platform" {
		return Denied
	}
	if !a.Valid() {
		return Invalid
	}
	if e := s.run(ctx, cred, "inventory", scope, reason, func(t *transaction) error { return nil }); e != nil {
		return e
	}
	if s.verifier == nil {
		return Unavailable
	}
	// Network calls have a deadline and occur outside DB locks. Timestamp is checked
	// again after lock acquisition, so delayed observations cannot become fresh.
	vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	o, e := s.verifier.Inventory(vctx, a)
	if e != nil {
		return Unavailable
	}
	return s.run(ctx, cred, "inventory", scope, reason, func(t *transaction) error {
		if o.Asset != a || !text(o.Reference, 256) || !text(o.Manifest, 64) || !fresh(t.now, o.At) || !destination(o.Source) {
			return Denied
		}
		if _, e := amount(o.Balance); e != nil {
			return e
		}
		var v Inventory
		e := t.get("inventory", a.Key(), &v)
		if e != nil && e != Missing {
			return e
		}
		if e == Missing {
			v = Inventory{Asset: a, Confirmed: o.Balance, Pending: "0", Reserved: "0", Settled: "0", Reversed: "0", Revision: 1}
		} else {
			// Outgoing confirmed settlements are already subtracted from Confirmed when
			// recorded; deposits require separate approved funding-account onboarding.
			if v.Revision == ^uint64(0) || o.At.Before(v.CheckedAt) {
				return Conflict
			}
			if v.Confirmed != o.Balance || v.Asset != o.Asset || v.Source != o.Source || v.Manifest != o.Manifest {
				v.Frozen = true
			}
			v.Revision++
		}
		v.Reference = o.Reference
		v.Manifest = o.Manifest
		v.Source = o.Source
		v.CheckedAt = o.At
		return t.put("inventory", a.Key(), v)
	})
}
func (s *Store) CreateBounty(ctx context.Context, cred, scope, reason string, b Bounty) error {
	if !key(b.ID) || !b.Asset.Valid() || !text(b.Title, 120) || !text(b.Rules, 10000) || !destination(b.FundingSource) || !text(b.RefundDisposition, 1000) {
		return Invalid
	}
	n, e := amount(b.Amount)
	if e != nil || n.Sign() == 0 {
		return Invalid
	}
	return s.run(ctx, cred, "bounty", scope, reason, func(t *transaction) error {
		if !b.Deadline.After(t.now) || b.Deadline.After(t.now.Add(90*24*time.Hour)) {
			return Invalid
		}
		var old Bounty
		if e := t.get("bounty", b.ID, &old); e == nil {
			return Conflict
		} else if e != Missing {
			return e
		}
		b.Scope = scope
		b.State = "draft"
		b.Revision = 1
		b.FundingReference = ""
		b.Reviewer = ""
		b.AwardEvidence = ""
		b.Destination = ""
		return t.put("bounty", b.ID, b)
	})
}
func (s *Store) BountyState(ctx context.Context, cred, scope, k, reason, next, evidence, dest string, expected uint64) error {
	if !key(k) {
		return Invalid
	}
	return s.run(ctx, cred, "bounty", scope, reason, func(t *transaction) error {
		var b Bounty
		if e := t.get("bounty", k, &b); e != nil {
			return e
		}
		if b.Scope != scope {
			return Denied
		}
		if b.Revision != expected || expected == ^uint64(0) {
			return Conflict
		}
		var v Inventory
		switch next {
		case "funded":
			if !b.Deadline.After(t.now) {
				return Denied
			}
			if b.State != "draft" {
				return Denied
			}
			if s.verifier == nil {
				return FundingRequired
			}
			e := t.get("inventory", b.Asset.Key(), &v)
			if e == Missing {
				return FundingRequired
			} else if e != nil {
				return e
			}
			if v.Source != b.FundingSource || v.Frozen || !fresh(t.now, v.CheckedAt) {
				return FundingRequired
			}
			available, e := sub(v.Confirmed, v.Reserved)
			if e != nil {
				return FundingRequired
			}
			if _, e = sub(available, b.Amount); e != nil {
				return FundingRequired
			}
			v.Reserved, e = add(v.Reserved, b.Amount)
			if e != nil {
				return e
			}
			v.Pending, e = add(v.Pending, b.Amount)
			if e != nil {
				return e
			}
			if v.Revision == ^uint64(0) {
				return Conflict
			}
			v.Revision++
			b.FundingReference = v.Reference
		case "active":
			if b.State != "funded" || !b.Deadline.After(t.now) {
				return FundingRequired
			}
			if e := t.get("inventory", b.Asset.Key(), &v); e != nil {
				return e
			}
			if v.Source != b.FundingSource || v.Frozen || !fresh(t.now, v.CheckedAt) {
				return FundingRequired
			}
		case "submitted":
			if b.State != "active" || !b.Deadline.After(t.now) || !text(evidence, 2000) {
				return Denied
			}
			b.AwardEvidence = evidence
		case "adjudicating":
			if b.State != "submitted" {
				return Denied
			}
		case "awarded":
			if b.State != "adjudicating" || !text(evidence, 2000) || !destination(dest) {
				return Denied
			}
			if s.verifier == nil {
				return Unavailable
			}
			if e := t.get("inventory", b.Asset.Key(), &v); e != nil {
				return e
			}
			if v.Source != b.FundingSource || v.Frozen || !fresh(t.now, v.CheckedAt) {
				return FundingRequired
			}
			if e := s.verifier.Award(contextFor(t), b, evidence, dest); e != nil {
				return Denied
			}
			b.Reviewer = t.actor.ID
			b.AwardEvidence = evidence
			b.Destination = dest
			o := Obligation{ID: id(), Scope: b.Scope, BountyID: k, IdempotencyKey: "bounty." + k, Amount: b.Amount, Destination: dest, AuthorityReference: b.FundingSource, State: "pending", Asset: b.Asset, AssetKey: b.Asset.Key(), Created: t.now, Revision: 1}
			if e := t.put("settlement", o.IdempotencyKey, o); e != nil {
				return e
			}
			if e := t.put("settlement_outbox", o.IdempotencyKey, map[string]string{"id": o.ID, "state": "pending", "authority": o.AuthorityReference}); e != nil {
				return e
			}
		case "cancelled", "expired":
			if b.State == "awarded" || b.State == "cancelled" || b.State == "expired" {
				return Denied
			}
			if next == "expired" && b.Deadline.After(t.now) {
				return Denied
			}
			if b.State != "draft" {
				if e := t.get("inventory", b.Asset.Key(), &v); e != nil {
					return e
				}
				var e error
				v.Reserved, e = sub(v.Reserved, b.Amount)
				if e != nil {
					return e
				}
				v.Pending, e = sub(v.Pending, b.Amount)
				if e != nil {
					return e
				}
				v.Reversed, e = add(v.Reversed, b.Amount)
				if e != nil {
					return e
				}
				if v.Revision == ^uint64(0) {
					return Conflict
				}
				v.Revision++
			}
		default:
			return Invalid
		}
		if v.Asset.Valid() {
			if e := t.put("inventory", b.Asset.Key(), v); e != nil {
				return e
			}
		}
		b.State = next
		b.Revision++
		return t.put("bounty", k, b)
	})
}

// ObserveSettlement never submits a payment. Unknown/timeouts keep the same
// durable obligation key. Only an independently checked receipt can settle it.
func (s *Store) ObserveSettlement(ctx context.Context, cred, scope, k, reason, reference string) error {
	if !key(k) || !text(reference, 256) {
		return Invalid
	}
	if s.verifier == nil {
		return Unavailable
	}
	var o Obligation
	e := s.run(ctx, cred, "settlement", scope, reason, func(t *transaction) error {
		if e := t.get("settlement", k, &o); e != nil {
			return e
		}
		if o.Scope != scope {
			return Denied
		}
		return nil
	})
	if e != nil {
		return e
	}
	if o.State == "settled" {
		if o.SettlementReference != reference {
			return Conflict
		}
		return nil
	}
	vctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	proof, verifyErr := s.verifier.Settlement(vctx, o, reference)
	return s.run(ctx, cred, "settlement", scope, reason, func(t *transaction) error {
		var current Obligation
		if e := t.get("settlement", k, &current); e != nil {
			return e
		}
		if current.Scope != scope {
			return Denied
		}
		if current.State == "settled" {
			if current.SettlementReference != reference {
				return Conflict
			}
			return nil
		}
		if current.Revision != o.Revision || current.Revision == ^uint64(0) {
			return Conflict
		}
		if verifyErr != nil {
			current.State = "unknown"
			current.Revision++
			return t.put("settlement", k, current)
		}
		if !proof.Successful || proof.Reference != reference || proof.Asset != o.Asset || proof.Amount != o.Amount || proof.Destination != o.Destination || !destination(proof.Source) || !text(proof.Manifest, 64) {
			return Denied
		}
		var seen map[string]string
		if e := t.get("receipt", strings.ToLower(reference), &seen); e == nil {
			return Conflict
		} else if e != Missing {
			return e
		}
		var v Inventory
		if e := t.get("inventory", o.Asset.Key(), &v); e != nil {
			return e
		}
		if proof.Source != v.Source || proof.Source != o.AuthorityReference || proof.Manifest != v.Manifest {
			return Denied
		}
		var e error
		v.Confirmed, e = sub(v.Confirmed, o.Amount)
		if e != nil {
			return e
		}
		v.Pending, e = sub(v.Pending, o.Amount)
		if e != nil {
			return e
		}
		v.Reserved, e = sub(v.Reserved, o.Amount)
		if e != nil {
			return e
		}
		v.Settled, e = add(v.Settled, o.Amount)
		if e != nil {
			return e
		}
		if v.Revision == ^uint64(0) {
			return Conflict
		}
		v.Revision++
		current.State = "settled"
		current.SettlementReference = reference
		current.Revision++
		if e = t.put("inventory", o.Asset.Key(), v); e != nil {
			return e
		}
		if e = t.put("receipt", strings.ToLower(reference), map[string]string{"obligation": k}); e != nil {
			return e
		}
		if e = t.put("settlement_outbox", k, map[string]string{"id": o.ID, "state": "settled"}); e != nil {
			return e
		}
		return t.put("settlement", k, current)
	})
}
