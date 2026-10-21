package redis

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	r "github.com/redis/go-redis/v9"
)

func TestPolicySensitiveStaleRejected(t *testing.T) {
	now := time.Now()
	s := PolicySnapshot{Generation: "g1", Revision: "1", Visibility: "private", Class: "private_account", SourceWatermark: json.RawMessage(`null`), FetchedAt: now, FreshUntil: now.Add(time.Second), RetainUntil: now.Add(2 * time.Second), Body: json.RawMessage(`{"balance":"1"}`)}
	if s.Validate() == nil {
		t.Fatal("private stale retention accepted")
	}
	s.RetainUntil = s.FreshUntil
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
	for _, class := range []string{"authorization", "ownership", "executable_quote", "account_write", "administrative"} {
		s.Class = class
		if s.Validate() == nil {
			t.Fatalf("cached authority class %s", class)
		}
	}
}
func TestRealPolicySnapshotOrdering(t *testing.T) {
	u := os.Getenv("L09_REDIS_URL")
	if u == "" {
		t.Skip("requires allocated real Redis")
	}
	ns := os.Getenv("L09_NAMESPACE")
	if ns == "" {
		t.Fatal("namespace required")
	}
	options, e := r.ParseURL(u)
	if e != nil {
		t.Fatal(e)
	}
	client := r.NewClient(options)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := ns + ":policy:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	lease := key + ":lease"
	defer client.Del(context.Background(), key, lease)
	now := time.Now()
	s := PolicySnapshot{Generation: "g1", Revision: "9007199254740993", Visibility: "public", Class: "public_snapshot", SourceWatermark: json.RawMessage(`{"height":"125"}`), FetchedAt: now, FreshUntil: now.Add(40 * time.Millisecond), RetainUntil: now.Add(time.Second), Body: json.RawMessage(`{"value":"new"}`)}
	if ok, e := PutPolicySnapshot(ctx, client, key, s, "", ""); !ok || e != nil {
		t.Fatal(ok, e)
	}
	floor := s
	floor.Revision = "9007199254740992"
	if ok, e := InvalidatePolicySnapshot(ctx, client, key, floor); ok || e != nil {
		t.Fatal("older invalidation", ok, e)
	}
	read, ok, e := GetPolicySnapshot(ctx, client, key)
	if !ok || e != nil || string(read.Body) != string(s.Body) {
		t.Fatal(read, ok, e)
	}
	// Equal source revision can extend freshness only for the same representation.
	s.FreshUntil = now.Add(80 * time.Millisecond)
	if ok, e := PutPolicySnapshot(ctx, client, key, s, "", ""); !ok || e != nil {
		t.Fatal("unchanged refresh", ok, e)
	}
	changed := s
	changed.Body = json.RawMessage(`{"value":"contradiction"}`)
	if ok, e := PutPolicySnapshot(ctx, client, key, changed, "", ""); ok || e == nil {
		t.Fatal("same revision changed representation", ok, e)
	}
	time.Sleep(100 * time.Millisecond)
	read, ok, e = GetPolicySnapshot(ctx, client, key)
	if !ok || e != nil || time.Now().Before(read.FreshUntil) {
		t.Fatal("bounded stale snapshot not retained", ok, e)
	}
	floor.Revision = "9007199254740994"
	if ok, e := InvalidatePolicySnapshot(ctx, client, key, floor); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if ok, e := PutPolicySnapshot(ctx, client, key, s, "", ""); ok || e != nil {
		t.Fatal("old publication resurrected", ok, e)
	}
	if _, ok, e := GetPolicySnapshot(ctx, client, key); ok || e != nil {
		t.Fatal("floor exposed as snapshot", ok, e)
	}
	s.Revision = "9007199254740995"
	s.FreshUntil = time.Now().Add(time.Second)
	s.RetainUntil = s.FreshUntil
	oldToken, e := AcquireRefresh(ctx, client, lease, 30*time.Millisecond)
	if e != nil || oldToken == "" {
		t.Fatal(e)
	}
	time.Sleep(50 * time.Millisecond)
	newToken, e := AcquireRefresh(ctx, client, lease, time.Second)
	if e != nil || newToken == "" {
		t.Fatal(e)
	}
	if ok, e := PutPolicySnapshot(ctx, client, key, s, lease, oldToken); ok || e == nil {
		t.Fatal("expired lease published", ok, e)
	}
	if ok, e := ReleaseRefresh(ctx, client, lease, oldToken); ok || e != nil {
		t.Fatal("expired lease deleted successor", ok, e)
	}
	if ok, e := PutPolicySnapshot(ctx, client, key, s, lease, newToken); !ok || e != nil {
		t.Fatal(ok, e)
	}
}
