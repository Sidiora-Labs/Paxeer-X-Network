package streams

import (
	"context"
	"errors"
	"testing"
)

func TestFailedAdmissionReleasesOneSlotWithDurableAudit(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	var first Operation
	for _, key := range []string{"failure_one", "failure_two", "failure_three"} {
		r, d := intent(key)
		o, err := s.Create(ctx, r, d, true)
		if err != nil {
			t.Fatal(err)
		}
		if first.ID == "" {
			first = o
		}
	}
	r, d := intent("replacement")
	if _, err := s.Create(ctx, r, d, true); !errors.Is(err, ErrQuota) {
		t.Fatalf("fourth admission: %v", err)
	}
	failed, err := s.FailBeforeProvision(ctx, first.ID, first.Revision, ProvisionQuotaUnavailable)
	if err != nil || failed.State != "FAILED" || failed.Revision != first.Revision+1 {
		t.Fatal(failed, err)
	}
	restarted, err := NewLifecycleStore(s.pool, s.schema, 3, 20)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restarted.FailBeforeProvision(ctx, first.ID, first.Revision, ProvisionQuotaUnavailable)
	if err != nil || again != failed {
		t.Fatal(again, err)
	}
	var audits int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_audit WHERE operation_id=$1 AND reason=$2", first.ID, string(ProvisionQuotaUnavailable)).Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
	if _, err := restarted.Create(ctx, r, d, true); err != nil {
		t.Fatal(err)
	}
	r.IdempotencyKey = "over_replacement"
	if _, err := restarted.Create(ctx, r, d, true); !errors.Is(err, ErrQuota) {
		t.Fatal(err)
	}
	if _, err := restarted.FailBeforeProvision(ctx, first.ID, first.Revision, ProvisionPolicyRevoked); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestAmbiguousProvisioningCannotReleaseReservation(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, d := intent("ambiguous_failure")
	o, err := s.Create(ctx, r, d, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FailBeforeProvision(ctx, o.ID, o.Revision+1, ProvisionQuotaUnavailable); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.FailBeforeProvision(ctx, o.ID, o.Revision, ProvisionFailureReason("provider failed: credential")); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	provisioning, err := s.BeginProvision(ctx, o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FailBeforeProvision(ctx, provisioning.ID, provisioning.Revision, ProvisionQuotaUnavailable); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	current, err := s.Get(ctx, o.ID)
	if err != nil || current.State != "PROVISIONING" || current.Revision != provisioning.Revision {
		t.Fatal(current, err)
	}
	var audits int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM "+s.schema+".stream_audit WHERE operation_id=$1", o.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
}
