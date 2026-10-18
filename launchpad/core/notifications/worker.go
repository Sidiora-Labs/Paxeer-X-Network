package notifications

import (
	"context"
	"errors"
	"net/url"
	"time"
)

type Worker struct {
	Store                 Store
	Authority             TargetAuthority
	Adapters              map[string]ChannelAdapter
	Owner, DeepLinkOrigin string
	Observe               func(string, time.Duration, string)
}

func (w Worker) Once(ctx context.Context, limit int) error {
	if w.Authority == nil || w.Owner == "" || limit < 1 || limit > 20 {
		return ErrInvalid
	}
	u, e := url.Parse(w.DeepLinkOrigin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ErrInvalid
	}
	items, e := w.Store.Claim(ctx, w.Owner, time.Now(), 1)
	if e != nil {
		return e
	}
	for _, item := range items {
		for _, d := range item.Deliveries {
			if terminalDelivery(d) {
				continue
			}
			now := time.Now()
			if d.NextAt.After(now) {
				continue
			}
			if e = w.Store.Renew(ctx, w.Owner, item.ID); e != nil {
				return e
			}
			if d.Attempt >= 20 {
				if d.AcceptanceUnknown || d.State == "unknown" {
					d.State = "unknown"
					d.AcceptanceUnknown = true
					d.Reason = "attempts_exhausted_uncertain"
				} else {
					d.State = "dead"
					d.Reason = "attempts_exhausted"
				}
				if e = w.Store.Complete(ctx, w.Owner, item.ID, d); e != nil {
					return e
				}
				continue
			}
			attemptCtx, cancelAttempt := context.WithTimeout(ctx, 10*time.Second)

			previousUnknown := d.AcceptanceUnknown || d.State == "unknown"
			d.Attempt++
			d.NextAt = now.Add(time.Minute)
			pref, err := w.Authority.CurrentDeliveryPreference(attemptCtx, item.Notification.Identity.AccountID)
			if err != nil {
				d.State = "retry"
				d.Reason = "consent_unavailable"
			} else {
				decision, de := Decide(pref, item.Notification, d.Channel, now)
				d.ConsentRevision = decision.ConsentRevision
				if de != nil {
					d.State = "retry"
					d.Reason = "consent_unavailable"
				} else if !decision.Send {
					d.State = "suppressed"
					d.Reason = decision.Reason
					if decision.Reason == "quiet_hours" || decision.Reason == "consent_unavailable" {
						d.State = "retry"
					}
				} else {
					target, te := w.Authority.CurrentTarget(attemptCtx, item.Notification.Identity.AccountID, d.TargetID)
					if te != nil && !errors.Is(te, ErrForbidden) {
						d.State = "retry"
						d.Reason = "target_unavailable"
					} else if te != nil || !validDeliveryTarget(target, item.Notification.Identity.AccountID, d, pref) {
						d.State = "suppressed"
						d.Reason = "target_revoked"
					} else {
						d.TargetRevision = target.Revision
						adapter := w.Adapters[target.Provider]
						if adapter == nil || adapter.Provider() != target.Provider || d.Channel != "push" {
							d.State = "retry"
							d.Reason = "provider_unavailable"
						} else {
							payload := Payload{Summary: "You have a new notification", AuthenticatedDeepLink: w.DeepLinkOrigin + "/notifications/" + url.PathEscape(item.Notification.ID), IdempotencyKey: item.Notification.ID}
							if e = w.Store.BeginSend(attemptCtx, w.Owner, item.ID, d); e != nil {
								cancelAttempt()
								return e
							}
							started := time.Now()
							sendCtx, cancel := context.WithTimeout(attemptCtx, 5*time.Second)
							result, se := adapter.Send(sendCtx, target, payload)
							cancel()
							if se != nil {
								result = SendResult{State: "unknown", Reason: "provider_uncertain"}
							}
							if ValidateResult(result) != nil {
								cancelAttempt()
								return ErrInvalid
							}
							d.State = result.State
							d.ProviderReceipt = result.Receipt
							d.Reason = result.Reason
							if result.DisableTarget {
								if err = w.Authority.DisableTarget(attemptCtx, target, result.Reason); err != nil {
									cancelAttempt()
									return err
								}
							}
							if w.Observe != nil {
								w.Observe(target.Provider, time.Since(started), d.State)
							}
						}
					}
				}
			}
			d = preserveUncertainty(d, previousUnknown)
			if !d.ExpiresAt.IsZero() && !time.Now().Before(d.ExpiresAt) && d.State != "accepted" {
				if d.State == "unknown" {
					d.Reason = "expired_uncertain"
				} else {
					d.State = "suppressed"
					d.Reason = "expired"
				}
			}
			if !terminalDelivery(d) && (d.State == "retry" || d.State == "unknown") {
				if d.Attempt >= 20 {
					if d.State == "unknown" {
						d.Reason = "attempts_exhausted_uncertain"
					} else {
						d.State = "dead"
						d.Reason = "attempts_exhausted"
					}
				} else {
					delay, _ := RetryDelay(d.Attempt)
					d.NextAt = time.Now().Add(delay)
				}
			}
			cancelAttempt()
			if e = w.Store.Complete(ctx, w.Owner, item.ID, d); e != nil {
				return e
			}
		}
		if e = w.Store.Release(ctx, w.Owner, item.ID); e != nil {
			return e
		}
	}
	return nil
}
func (w Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if e := w.Once(ctx, 20); e != nil && !errors.Is(e, context.Canceled) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (w Worker) DeleteAccount(ctx context.Context, account string) error {
	if e := w.Authority.DeleteTargets(ctx, account); e != nil {
		return e
	}
	return w.Store.DeleteAccount(ctx, account)
}
