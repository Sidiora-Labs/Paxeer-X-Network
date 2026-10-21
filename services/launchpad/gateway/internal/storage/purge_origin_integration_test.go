package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
)

// A purge in the CDN-less deployment evicts every recorded variant of the
// restricted hash from the gateway's own object cache and completes with a
// proof naming exactly those keys; a cache outage keeps the operation pending.
func TestOriginCachePurgeEvictsRecordedVariantsAndCompletes(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	other := "approved/" + hash + "/512/" + strings.Repeat("f", 64) + ".png"
	if e := a.RegisterLineage(ctx, LineageObject{Hash: hash, Key: other, Kind: "variant", RetainUntil: time.Now().Add(365 * 24 * time.Hour)}); e != nil {
		t.Fatal(e)
	}
	rdb, _ := internaltest.NewRedis(t)
	objects := cache.New(rdb, time.Minute)
	for _, k := range []string{key, other} {
		if e := objects.Set(ctx, "token", k, cache.Object{ContentType: "image/png", ETag: `"e"`, Body: []byte("png")}); e != nil {
			t.Fatal(e)
		}
	}
	unrelated := "approved/" + strings.Repeat("9", 64) + "/256/" + strings.Repeat("8", 64) + ".png"
	if e := objects.Set(ctx, "token", unrelated, cache.Object{ContentType: "image/png", Body: []byte("keep")}); e != nil {
		t.Fatal(e)
	}
	failing := true
	provider, e := NewOriginCachePurgeProvider(a, func(c context.Context, k string) error {
		if failing {
			return errors.New("cache unavailable")
		}
		return objects.Evict(c, "token", k)
	})
	if e != nil {
		t.Fatal(e)
	}
	id, e := a.Restrict(ctx, hash, 2, "blocked")
	if e != nil {
		t.Fatal(e)
	}
	worker := PurgeWorker{Authority: a, Provider: provider}
	if worked, e := worker.RunOne(ctx); !worked || e != nil {
		t.Fatal("submission", worked, e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "accepted" || op.ProviderID != "origin-cache-"+id || op.Proof != "" {
		t.Fatal("accepted receipt", op, e)
	}
	if worked, e := worker.RunOne(ctx); !worked || !errors.Is(e, ErrUnavailable) {
		t.Fatal("cache outage must keep the purge pending", worked, e)
	}
	if op, e = a.Operation(ctx, id); e != nil || op.State != "accepted" {
		t.Fatal("outage changed state", op, e)
	}
	if _, found, e := objects.Get(ctx, "token", key); e != nil || !found {
		t.Fatal("outage evicted", found, e)
	}
	failing = false
	if worked, e := worker.RunOne(ctx); !worked || e != nil {
		t.Fatal("status", worked, e)
	}
	op, e = a.Operation(ctx, id)
	if e != nil || op.State != "complete" || !strings.HasPrefix(op.Proof, "origin-cache:2:") || len(op.Proof) > 256 {
		t.Fatal("completion receipt", op, e)
	}
	for _, k := range []string{key, other} {
		if _, found, e := objects.Get(ctx, "token", k); e != nil || found {
			t.Fatal("variant survived purge", k, found, e)
		}
	}
	if _, found, e := objects.Get(ctx, "token", unrelated); e != nil || !found {
		t.Fatal("purge evicted an unrelated hash", found, e)
	}
	if age, e := a.PurgeBacklogAge(ctx); e != nil || age != 0 {
		t.Fatal("completed purge left a backlog", age, e)
	}
	if worked, e := worker.RunOne(ctx); worked || e != nil {
		t.Fatal("completed purge re-run", worked, e)
	}
	if _, e = provider.Status(ctx, id, "origin-cache-other"); !errors.Is(e, ErrConflict) {
		t.Fatal("foreign provider identity accepted", e)
	}
	if _, e = NewOriginCachePurgeProvider(a, nil); e == nil {
		t.Fatal("purge without eviction accepted")
	}
}
