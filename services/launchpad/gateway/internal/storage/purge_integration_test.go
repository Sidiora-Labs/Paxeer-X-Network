package storage

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConnectedOperationPublicProjectionRedactsProvider(t *testing.T) {
	raw, e := json.Marshal(Operation{ID: "8a5f44d2-8ee0-42c5-b365-cffdd2e91723", Hash: strings.Repeat("a", 64), Revision: 2, Kind: "purge", State: "accepted", ProviderID: "private-provider-id", Proof: "private-provider-ack", RequestedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "private-provider") || strings.Contains(string(raw), "providerId") || strings.Contains(string(raw), "proof") || strings.Contains(string(raw), "hash") {
		t.Fatal("provider details leaked", string(raw))
	}
	if !strings.Contains(string(raw), `"state":"accepted"`) || strings.Contains(string(raw), `"state":"complete"`) {
		t.Fatal("accepted provider operation misrepresented", string(raw))
	}
}
func TestConnectedPurgeProviderRequiresSecureAuthority(t *testing.T) {
	for _, endpoint := range []string{"http://provider.invalid", "https://user:secret@provider.invalid", "https://provider.invalid?token=secret", "https://provider.invalid/unknown"} {
		if _, e := NewHTTPPurgeProvider(endpoint, strings.Repeat("s", 32), nil); e == nil {
			t.Fatal("unsafe purge endpoint accepted", endpoint)
		}
	}
}

func TestConnectedPurgeNetworkFailurePreservesPendingState(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	id, e := a.Restrict(ctx, hash, 2, "blocked")
	if e != nil {
		t.Fatal(e)
	}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	endpoint := "https://" + listener.Addr().String()
	if e = listener.Close(); e != nil {
		t.Fatal(e)
	}
	provider, e := NewHTTPPurgeProvider(endpoint, strings.Repeat("s", 32), nil)
	if e != nil {
		t.Fatal(e)
	}
	worker := PurgeWorker{Authority: a, Provider: provider}
	worked, e := worker.RunOne(ctx)
	if !worked || !errors.Is(e, ErrUnavailable) {
		t.Fatal(worked, e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.ProviderID != "" || op.Proof != "" {
		t.Fatal(op, e)
	}
	if e = a.Allowed(ctx, key); !errors.Is(e, ErrRestricted) {
		t.Fatal("purge outage exposed origin", e)
	}
	if _, e = a.Pool.Exec(ctx, "UPDATE "+a.table("operations")+" SET state='failed',provider_id='persisted-allocation' WHERE id=$1", id); e != nil {
		t.Fatal(e)
	}
	if e = a.RetryPurge(ctx, id); e != nil {
		t.Fatal(e)
	}
	op, e = a.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.ProviderID != "persisted-allocation" {
		t.Fatal("retry forgot provider identity", op, e)
	}
	worked, e = worker.RunOne(ctx)
	if !worked || !errors.Is(e, ErrUnavailable) {
		t.Fatal(worked, e)
	}
	op, e = a.Operation(ctx, id)
	if e != nil || op.ProviderID != "persisted-allocation" || op.State != "requested" || op.Proof != "" {
		t.Fatal(op, e)
	}
}
