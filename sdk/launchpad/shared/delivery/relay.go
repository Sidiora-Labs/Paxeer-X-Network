package delivery

import (
	"context"
	"errors"
	"time"
)

// PersistentPublisher must await the server JetStream persistence acknowledgement
// (not socket write completion), set Nats-Msg-Id to eventID, and return the actual
// stream and sequence. The serial integration adapter supplies this contract.
type PersistentPublisher interface {
	Publish(context.Context, string, string, []byte) (PersistenceAck, error)
}

type Relay struct {
	Store                                     *Store
	Publisher                                 PersistentPublisher
	Owner                                     string
	Batch                                     int
	LeaseDuration, PublishTimeout, RetryDelay time.Duration
	MaxAttempts                               int
	Budget                                    *WorkBudget
}

// RunOnce is bounded; independent replicas use SKIP LOCKED and fenced leases.
func (r Relay) RunOnce(ctx context.Context) error {
	if r.Store == nil || r.Publisher == nil || !token.MatchString(r.Owner) || r.Batch < 1 || r.Batch > 1000 || r.MaxAttempts < 1 || r.MaxAttempts > 100 || r.PublishTimeout <= 0 || r.PublishTimeout >= r.LeaseDuration || r.RetryDelay < time.Second || r.RetryDelay > time.Hour {
		return errors.New("delivery: invalid relay configuration")
	}
	// Claim one row at a time: a bounded batch must not expire rows waiting behind
	// slow publications. Each row receives its own complete lease budget.
	var poison error
	for i := 0; i < r.Batch; i++ {
		if r.Budget != nil {
			if err := r.Budget.limiter.Wait(ctx); err != nil {
				return errors.Join(poison, err)
			}
		}
		leases, err := r.Store.Claim(ctx, r.Owner, 1, r.LeaseDuration)
		if err != nil {
			return errors.Join(poison, err)
		}
		if len(leases) == 0 {
			return poison
		}
		l := leases[0]
		allowed, err := r.Store.CheckAttemptBudget(ctx, l, r.MaxAttempts)
		if err != nil {
			return errors.Join(poison, err)
		}
		if !allowed {
			continue
		}
		if _, err = Decode(l.Wire, r.Store.policy, l.Subject); err != nil {
			class := SchemaViolation
			if errors.Is(err, ErrIntegrity) {
				class = IntegrityConflict
			}
			if retryErr := r.Store.Retry(ctx, l, class, r.MaxAttempts, r.RetryDelay); retryErr != nil {
				return errors.Join(poison, err, retryErr)
			}
			poison = errors.Join(poison, err)
			continue
		}
		pubctx, cancel := context.WithTimeout(ctx, r.PublishTimeout)
		ack, puberr := r.Publisher.Publish(pubctx, l.Subject, l.EventID, append([]byte(nil), l.Wire...))
		cancel()
		if puberr == nil {
			puberr = r.Store.MarkPublished(ctx, l, ack)
		}
		if puberr != nil {
			// A crash here leaves the exact immutable row reclaimable after lease expiry.
			if retryErr := r.Store.Retry(ctx, l, publicationFailureClass(puberr), r.MaxAttempts, r.RetryDelay); retryErr != nil {
				return errors.Join(poison, puberr, retryErr)
			}
			return errors.Join(poison, puberr)
		}
	}
	return poison
}

func (r Relay) RunOnceWithBudget(ctx context.Context, budget *WorkBudget) error {
	release, err := budget.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	r.Budget = budget
	return r.RunOnce(ctx)
}

func publicationFailureClass(err error) FailureClass {
	if errors.Is(err, ErrAuthorization) {
		return Authorization
	}
	if errors.Is(err, ErrIntegrity) {
		return IntegrityConflict
	}
	if errors.Is(err, ErrSchema) {
		return SchemaViolation
	}
	return Transient
}
