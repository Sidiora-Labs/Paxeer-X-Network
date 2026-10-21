package notifications

import (
	"testing"
	"time"
)

func TestIdentityIsUnambiguousAndReadScoped(t *testing.T) {
	a := Identity{"a", "bc", "d"}
	b := Identity{"ab", "c", "d"}
	ka, e := a.Key()
	kb, _ := b.Key()
	if e != nil || ka == kb {
		t.Fatal("ambiguous identity")
	}
	for i := 0; i < 100; i++ {
		k, _ := a.Key()
		if k != ka {
			t.Fatal("unstable")
		}
	}
	n := InboxItem{Identity: a}
	if _, e = n.SetRead("other", true); e == nil {
		t.Fatal("unscoped")
	}
	n, e = n.SetRead("a", true)
	n, e = n.SetRead("a", true)
	if e != nil || !n.Read {
		t.Fatal(n, e)
	}
}
func TestProviderUncertaintyAndBounds(t *testing.T) {
	for _, r := range []SendResult{{State: "unknown"}, {State: "accepted", Receipt: "provider-id"}, {State: "rejected", DisableTarget: true}} {
		if e := ValidateResult(r); e != nil {
			t.Fatal(e)
		}
	}
	for _, r := range []SendResult{{State: "delivered"}, {State: "accepted"}, {State: "unknown", DisableTarget: true}} {
		if ValidateResult(r) == nil {
			t.Fatal(r)
		}
	}
	if ValidatePayload(Payload{Summary: "safe", IdempotencyKey: "key"}, 1) == nil {
		t.Fatal("oversize")
	}
	d, _ := RetryDelay(20)
	if d != 15*time.Minute {
		t.Fatal(d)
	}
}
func TestConsentRecheckedQuietHoursDSTAndExpiry(t *testing.T) {
	p := DeliveryPreference{AccountID: "a", Revision: 2, Linked: true, Timezone: "Europe/Berlin", Categories: map[string]bool{"market": true}, Channels: map[string]bool{"push": true}, QuietHours: &QuietHours{StartMinute: 22 * 60, EndMinute: 7 * 60}}
	n := InboxItem{Identity: Identity{AccountID: "a"}, Category: "market"}
	for _, stamp := range []string{"2026-03-29T00:30:00Z", "2026-03-29T01:30:00Z", "2026-10-25T00:30:00Z", "2026-10-25T01:30:00Z"} {
		now, _ := time.Parse(time.RFC3339, stamp)
		d, e := Decide(p, n, "push", now)
		if e != nil || d.Send || d.Reason != "quiet_hours" || d.ConsentRevision != 2 {
			t.Fatal(stamp, d, e)
		}
	}
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	d, e := Decide(p, n, "push", now)
	if e != nil || !d.Send {
		t.Fatal(d, e)
	}
	p.Channels["push"] = false
	d, _ = Decide(p, n, "push", now)
	if d.Send || d.Reason != "opted_out" {
		t.Fatal(d)
	}
	p.Channels["push"] = true
	n.ExpiresAt = now
	d, _ = Decide(p, n, "push", now)
	if d.Reason != "expired" {
		t.Fatal(d)
	}
	p.Linked = false
	d, _ = Decide(p, n, "push", now)
	if d.Reason != "consent_unavailable" {
		t.Fatal(d)
	}
}

func TestUncertainAcceptanceSurvivesSuppressionAndLaterRejection(t *testing.T) {
	for _, state := range []string{"retry", "suppressed", "rejected", "dead"} {
		d := preserveUncertainty(DeliveryAttempt{State: state, Reason: "target_revoked"}, true)
		if d.State != "unknown" || !d.AcceptanceUnknown {
			t.Fatal("uncertainty was erased", state, d)
		}
		if state != "retry" && !terminalDelivery(d) {
			t.Fatal("stopped uncertain send kept retrying", d)
		}
	}
	d := preserveUncertainty(DeliveryAttempt{State: "accepted", ProviderReceipt: "receipt", AcceptanceUnknown: true}, true)
	if d.AcceptanceUnknown || d.State != "accepted" {
		t.Fatal(d)
	}
}

func TestLogoutAndRebindingInvalidateQueuedTarget(t *testing.T) {
	p := DeliveryPreference{AccountID: "account", Revision: 4}
	d := DeliveryAttempt{TargetID: "installation", TargetRevision: 7}
	target := PushTarget{AccountID: "account", DeviceInstallationID: "installation", Status: "active", Revision: 7, ConsentRevision: 4}
	if !validDeliveryTarget(target, "account", d, p) {
		t.Fatal("current target rejected")
	}
	for _, mutate := range []func(*PushTarget){
		func(v *PushTarget) { v.AccountID = "other" },
		func(v *PushTarget) { v.DeviceInstallationID = "other" },
		func(v *PushTarget) { v.Status = "revoked" },
		func(v *PushTarget) { v.Revision++ },
		func(v *PushTarget) { v.ConsentRevision-- },
	} {
		changed := target
		mutate(&changed)
		if validDeliveryTarget(changed, "account", d, p) {
			t.Fatal("revoked or replaced binding accepted", changed)
		}
	}
}

func TestQuietHoursDSTRepeatedHourAndFifteenMinuteExpiry(t *testing.T) {
	p := DeliveryPreference{AccountID: "account", Revision: 9, Linked: true, Timezone: "Europe/Berlin", CategoryChannels: map[string]map[string]bool{"watchlist": {"push": true}}, QuietHours: &QuietHours{StartMinute: 2 * 60, EndMinute: 3 * 60}}
	for _, stamp := range []string{"2026-10-25T00:30:00Z", "2026-10-25T01:30:00Z"} {
		now, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			t.Fatal(err)
		}
		n := InboxItem{Identity: Identity{AccountID: "account"}, Category: "watchlist", CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute)}
		d, err := Decide(p, n, "push", now)
		if err != nil || d.Send || d.Reason != "quiet_hours" {
			t.Fatal("repeated local hour escaped quiet hours", stamp, d, err)
		}
		d, err = Decide(p, n, "push", now.Add(15*time.Minute))
		if err != nil || d.Send || d.Reason != "expired" {
			t.Fatal("expired quiet-hour alert remained sendable", stamp, d, err)
		}
	}
	p.CategoryChannels["watchlist"]["push"] = false
	n := InboxItem{Identity: Identity{AccountID: "account"}, Category: "watchlist"}
	d, err := Decide(p, n, "push", time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC))
	if err != nil || d.Send || d.Reason != "opted_out" || d.ConsentRevision != 9 {
		t.Fatal("latest category consent ignored", d, err)
	}
}
