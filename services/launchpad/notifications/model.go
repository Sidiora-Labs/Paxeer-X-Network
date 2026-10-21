// Package notifications defines durable repository and provider boundaries.
package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid notification input")
var ErrForbidden = errors.New("recipient mismatch")

type Identity struct{ AccountID, TriggerEventID, TemplateVersion string }

func (i Identity) Key() (string, error) {
	if i.AccountID == "" || i.TriggerEventID == "" || i.TemplateVersion == "" || len(i.AccountID) > 160 || len(i.TriggerEventID) > 160 || len(i.TemplateVersion) > 80 {
		return "", ErrInvalid
	}
	b, _ := json.Marshal(i)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

type InboxItem struct {
	ID                            string
	Identity                      Identity
	Category, Summary, ResourceID string
	CreatedAt, ExpiresAt          time.Time
	Read                          bool
}

func (n InboxItem) SetRead(actor string, read bool) (InboxItem, error) {
	if actor != n.Identity.AccountID {
		return n, ErrForbidden
	}
	n.Read = read
	return n, nil
}

type DeliveryAttempt struct {
	NotificationID, Channel, TargetID string
	TargetRevision, ConsentRevision   uint64
	Attempt                           int
	State, ProviderReceipt, Reason    string
	AcceptanceUnknown                 bool
	NextAt, ExpiresAt                 time.Time
}
type OutboxItem struct {
	ID           string
	Notification InboxItem
	Deliveries   []DeliveryAttempt
}

// CommitTrigger MUST atomically deduplicate Identity.Key, persist the inbox and
// outbox; duplicate triggers return the original item. No memory fallback allowed.
type Repository interface {
	CommitTrigger(context.Context, InboxItem, []DeliveryAttempt) (InboxItem, bool, error)
	Claim(context.Context, string, time.Time, int) ([]OutboxItem, error)
	Complete(context.Context, string, string, DeliveryAttempt) error
	SetRead(context.Context, string, string, bool) error
	DeleteAccount(context.Context, string) error
}
type PushTarget struct {
	AccountID, DeviceInstallationID, Provider, EncryptedEndpointRef, Status string
	ConsentRevision, Revision                                               uint64
	LastSeen                                                                time.Time
}
type Payload struct{ Summary, AuthenticatedDeepLink, IdempotencyKey string }
type SendResult struct {
	State, Receipt, Reason string
	DisableTarget          bool
}

// Adapter implementations require runtime secrets and real provider qualification.
// Timeout/uncertain acceptance returns State unknown, never delivered.
type ChannelAdapter interface {
	Provider() string
	MaxPayloadBytes() int
	Send(context.Context, PushTarget, Payload) (SendResult, error)
}

func ValidateResult(r SendResult) error {
	switch r.State {
	case "accepted":
		if r.Receipt == "" {
			return ErrInvalid
		}
	case "unknown", "retry", "rejected":
	default:
		return ErrInvalid
	}
	if r.DisableTarget && r.State != "rejected" {
		return ErrInvalid
	}
	return nil
}
func ValidatePayload(p Payload, max int) error {
	if max < 1 || p.Summary == "" || p.IdempotencyKey == "" {
		return ErrInvalid
	}
	b, e := json.Marshal(p)
	if e != nil || len(b) > max {
		return ErrInvalid
	}
	return nil
}

type QuietHours struct{ StartMinute, EndMinute int }
type DeliveryPreference struct {
	AccountID, Timezone, Language string
	Revision                      uint64
	Categories, Channels          map[string]bool
	CategoryChannels              map[string]map[string]bool
	QuietHours                    *QuietHours
	Linked                        bool
}
type PreferenceConsumer interface {
	CurrentDeliveryPreference(context.Context, string) (DeliveryPreference, error)
	CurrentTarget(context.Context, string, string) (PushTarget, error)
}
type Decision struct {
	Send            bool
	Reason          string
	ConsentRevision uint64
}

func Decide(p DeliveryPreference, n InboxItem, channel string, now time.Time) (Decision, error) {
	d := Decision{ConsentRevision: p.Revision}
	if p.AccountID != n.Identity.AccountID {
		return d, ErrForbidden
	}
	if p.Revision == 0 || !p.Linked {
		d.Reason = "consent_unavailable"
		return d, nil
	}
	allowed := p.Categories[n.Category] && p.Channels[channel]
	if p.CategoryChannels != nil {
		allowed = p.CategoryChannels[n.Category][channel]
	}
	if !allowed {
		d.Reason = "opted_out"
		return d, nil
	}
	if !n.ExpiresAt.IsZero() && !now.Before(n.ExpiresAt) {
		d.Reason = "expired"
		return d, nil
	}
	loc, e := time.LoadLocation(p.Timezone)
	if e != nil {
		return d, ErrInvalid
	}
	if q := p.QuietHours; q != nil {
		if q.StartMinute < 0 || q.StartMinute > 1439 || q.EndMinute < 0 || q.EndMinute > 1439 {
			return d, ErrInvalid
		}
		t := now.In(loc)
		m := t.Hour()*60 + t.Minute()
		quiet := m >= q.StartMinute && m < q.EndMinute
		if q.StartMinute > q.EndMinute {
			quiet = m >= q.StartMinute || m < q.EndMinute
		}
		if quiet {
			d.Reason = "quiet_hours"
			return d, nil
		}
	}
	d.Send = true
	d.Reason = "consented"
	return d, nil
}
func RetryDelay(attempt int) (time.Duration, error) {
	if attempt < 1 || attempt > 20 {
		return 0, ErrInvalid
	}
	d := time.Second * time.Duration(1<<min(attempt-1, 10))
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d, nil
}

func preserveUncertainty(d DeliveryAttempt, previous bool) DeliveryAttempt {
	if d.State == "accepted" {
		d.AcceptanceUnknown = false
		return d
	}
	d.AcceptanceUnknown = previous || d.State == "unknown"
	if !d.AcceptanceUnknown {
		return d
	}
	switch d.State {
	case "suppressed", "rejected", "dead":
		d.State = "unknown"
		d.Reason = "stopped_uncertain"
	case "retry":
		d.State = "unknown"
	}
	return d
}

func validDeliveryTarget(target PushTarget, account string, attempt DeliveryAttempt, preference DeliveryPreference) bool {
	return target.AccountID == account && target.DeviceInstallationID == attempt.TargetID && target.Status == "active" && target.Revision != 0 && (attempt.TargetRevision == 0 || target.Revision == attempt.TargetRevision) && target.ConsentRevision == preference.Revision
}
