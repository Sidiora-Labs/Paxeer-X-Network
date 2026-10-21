package control

import (
	"context"
	"encoding/json"
	"net/url"
	"time"
)

type RecommendationView struct {
	Reservation            Reservation  `json:"reservation"`
	Campaign               CampaignView `json:"campaign"`
	Origin                 string       `json:"origin"`
	OrganicRankingEligible bool         `json:"organicRankingEligible"`
}

func (s *Store) Recommendation(ctx context.Context, credential, owner, reason, reservationKey string) (RecommendationView, error) {
	var out RecommendationView
	err := s.run(ctx, credential, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner {
			return Denied
		}
		var r Reservation
		if err := t.get("reservation", owner+"."+reservationKey, &r); err != nil {
			return err
		}
		var policy Policy
		if err := t.get("policy", r.PolicyID, &policy); err != nil {
			return err
		}
		if policy.Owner != owner || r.Owner != owner {
			return Denied
		}
		campaign, err := PublicCampaign(policy)
		if err != nil {
			return err
		}
		out = RecommendationView{r, campaign, "automated_recommendation", false}
		return nil
	})
	return out, err
}

type Submission struct {
	ID, BountyID, Author, Wallet, Body string
	EvidenceURLs                       []string
	SubmittedAt                        time.Time
}

func (s *Store) SubmitBounty(ctx context.Context, credential, owner, scope, reason string, submission Submission) error {
	if !uuidPattern.MatchString(submission.ID) || !key(submission.BountyID) || !destination(submission.Wallet) || !text(submission.Body, 10000) || len(submission.EvidenceURLs) < 1 || len(submission.EvidenceURLs) > 10 {
		return Invalid
	}
	for _, raw := range submission.EvidenceURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || len(raw) > 2048 {
			return Invalid
		}
	}
	identity, ok := s.identity.(*PostgresIdentity)
	if !ok {
		return Unavailable
	}
	if err := identity.Wallet(ctx, owner, submission.Wallet); err != nil {
		return err
	}
	return s.run(ctx, credential, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner || submission.Author != owner {
			return Denied
		}
		var bounty Bounty
		if err := t.get("bounty", submission.BountyID, &bounty); err != nil {
			return err
		}
		if bounty.Scope != scope || bounty.State != "active" || !bounty.Deadline.After(t.now) {
			return Denied
		}
		var old Submission
		if err := t.get("submission", submission.ID, &old); err == nil {
			submission.SubmittedAt = old.SubmittedAt
			if digest(old) != digest(submission) {
				return Conflict
			}
			return nil
		} else if err != Missing {
			return err
		}
		submission.SubmittedAt = t.now
		return t.put("submission", submission.ID, submission)
	})
}

type StoredAwardAuthority struct {
	Store   *Store
	Wallets *PostgresIdentity
}

func (a StoredAwardAuthority) VerifyAward(ctx context.Context, b Bounty, evidence, destination string) error {
	if a.Store == nil || a.Wallets == nil || !uuidPattern.MatchString(evidence) {
		return Unavailable
	}
	var raw []byte
	if err := a.Store.pool.QueryRow(ctx, "SELECT body FROM "+a.Store.schema+".objects WHERE kind='submission' AND id=$1", evidence).Scan(&raw); err != nil {
		return Unavailable
	}
	var submission Submission
	if json.Unmarshal(raw, &submission) != nil || submission.BountyID != b.ID || submission.Wallet != destination || submission.SubmittedAt.After(b.Deadline) {
		return Denied
	}
	return a.Wallets.Wallet(ctx, submission.Author, submission.Wallet)
}

type DurableJobResults struct{ Store *Store }

func (v DurableJobResults) Verify(ctx context.Context, action Action, state, reference string) error {
	if v.Store == nil || action.JobID == "" {
		return Unavailable
	}
	var raw []byte
	if err := v.Store.pool.QueryRow(ctx, "SELECT body FROM "+v.Store.schema+".objects WHERE kind='executor_result' AND id=$1", action.JobID).Scan(&raw); err != nil {
		return Unavailable
	}
	var result struct{ ActionID, PayloadHash, State, Reference string }
	if json.Unmarshal(raw, &result) != nil || result.ActionID != action.ID || result.PayloadHash != action.PayloadHash || result.State != state || result.Reference != reference {
		return Denied
	}
	return nil
}

type IncidentRecovery struct{ Store *Store }

func (v IncidentRecovery) Check(ctx context.Context, i Incident, rotation, integrity string) error {
	if v.Store == nil {
		return Unavailable
	}
	var raw []byte
	if err := v.Store.pool.QueryRow(ctx, "SELECT body FROM "+v.Store.schema+".objects WHERE kind='incident_outbox' AND id=$1", i.ID).Scan(&raw); err != nil {
		return Unavailable
	}
	var record map[string]string
	if json.Unmarshal(raw, &record) != nil || record["state"] != "completed" || record["identity"] != i.ServiceIdentity || record["rotation"] != rotation || record["integrity"] != integrity {
		return Denied
	}
	return nil
}

func (s *Store) Campaign(ctx context.Context, credential, owner, policyID, reason string) (CampaignView, error) {
	var out CampaignView
	err := s.run(ctx, credential, "automation", owner, reason, func(t *transaction) error {
		if t.actor.ID != owner {
			return Denied
		}
		var policy Policy
		if err := t.get("policy", policyID, &policy); err != nil {
			return err
		}
		if policy.Owner != owner {
			return Denied
		}
		var err error
		out, err = PublicCampaign(policy)
		return err
	})
	return out, err
}

type ReplayPayload struct {
	ChainID    string `json:"chainId"`
	FromHeight string `json:"fromHeight"`
	ToHeight   string `json:"toHeight"`
	Projection string `json:"projection"`
	DryRun     bool   `json:"dryRun"`
}
type ReplayProposal struct {
	Key     string        `json:"key"`
	Scope   string        `json:"scope"`
	Reason  string        `json:"reason"`
	Expires time.Time     `json:"expiresAt"`
	Payload ReplayPayload `json:"payload"`
}

func (s *Store) ProposeReplay(ctx context.Context, credential string, q ReplayProposal) (Action, error) {
	var action Action
	from, err := amount(q.Payload.FromHeight)
	if err != nil {
		return action, err
	}
	to, err := amount(q.Payload.ToHeight)
	if err != nil {
		return action, err
	}
	if q.Payload.ChainID != "125" || to.Cmp(from) < 0 || !key(q.Payload.Projection) || !key(q.Key) {
		return action, Invalid
	}
	payloadHash := digest(q.Payload)
	err = s.run(ctx, credential, "action", q.Scope, q.Reason, func(t *transaction) error {
		if !q.Expires.After(t.now) || q.Expires.After(t.now.Add(24*time.Hour)) {
			return Invalid
		}
		if err := t.get("action", q.Key, &action); err == nil {
			if action.PayloadHash != payloadHash || action.RequestedBy != t.actor.ID || action.Scope != q.Scope || action.Kind != "replay" || action.Reason != q.Reason || !action.Expires.Equal(q.Expires) {
				return Conflict
			}
			var payload ReplayPayload
			if err := t.get("action_payload", q.Key, &payload); err != nil {
				return err
			}
			if digest(payload) != payloadHash {
				return Conflict
			}
			return nil
		} else if err != Missing {
			return err
		}
		action = Action{ID: id(), Kind: "replay", Scope: q.Scope, Reason: q.Reason, RequestedBy: t.actor.ID, State: "proposed", PayloadHash: payloadHash, Revision: 1, Expires: q.Expires}
		if err := t.put("action_payload", q.Key, q.Payload); err != nil {
			return err
		}
		return t.put("action", q.Key, action)
	})
	return action, err
}
func (s *Store) TransitionWorkflow(ctx context.Context, credential string, q TransitionRequest) (Action, error) {
	if q.Expected == nil {
		return Action{}, Invalid
	}
	if q.Next == "executing" {
		var action Action
		if err := s.Read(ctx, credential, q.Scope, q.Reason, "action", q.Key, &action); err != nil {
			return action, err
		}
		if action.Kind == "replay" {
			var raw []byte
			if err := s.pool.QueryRow(ctx, "SELECT body FROM "+s.schema+".objects WHERE kind='action_payload' AND id=$1", q.Key).Scan(&raw); err != nil {
				return action, Unavailable
			}
			var payload ReplayPayload
			if json.Unmarshal(raw, &payload) != nil || digest(payload) != action.PayloadHash {
				return action, Conflict
			}
		} else {
			return action, Unavailable
		}
	}
	return s.Transition(ctx, credential, q.Key, q.Scope, q.Reason, q.Next, *q.Expected)
}
