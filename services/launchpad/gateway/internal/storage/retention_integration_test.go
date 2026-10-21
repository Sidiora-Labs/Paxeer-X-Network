package storage

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
	sharedstorage "github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
)

func TestConnectedDeleteActualObjectsAfterTombstone(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	store := internaltest.NewStore(t, "l41-approved-derivatives")
	body := []byte("owned synthetic immutable bytes")
	if e := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); e != nil {
		t.Fatal(e)
	}
	id, e := a.Restrict(ctx, hash, 2, "deleted")
	if e != nil {
		t.Fatal(e)
	}
	worker := DeleteWorker{Authority: a, Stores: map[string]*sharedstorage.Client{"variant": store}}
	worked, e := worker.RunOne(ctx)
	if e != nil || !worked {
		t.Fatal(worked, e)
	}
	if _, e = store.Head(ctx, key); !errors.Is(e, sharedstorage.ErrNotFound) {
		t.Fatal("object still public", e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "complete" || op.Proof == "" {
		t.Fatal(op, e)
	}
	if e = a.RegisterLineage(ctx, LineageObject{Hash: hash, Key: key, Kind: "variant", RetainUntil: time.Now().Add(time.Hour)}); !errors.Is(e, ErrRestricted) {
		t.Fatal("deleted lineage recreated", e)
	}
}

func TestConnectedDeleteRecoversAfterRealStorageFailure(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, key := seedConnectedAsset(t, a, ctx)
	backend := internaltest.NewMinIO(t, "l41-delete-recovery")
	store := backend.Client(t, "l41-delete-recovery")
	var encoded bytes.Buffer
	if e := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); e != nil {
		t.Fatal(e)
	}
	body := encoded.Bytes()
	if e := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); e != nil {
		t.Fatal(e)
	}
	id, e := a.Restrict(ctx, hash, 2, "deleted")
	if e != nil {
		t.Fatal(e)
	}
	worker := DeleteWorker{Authority: a, Stores: map[string]*sharedstorage.Client{"variant": backend.Client(t, "l41-missing-storage-bucket")}}
	if worked, e := worker.RunOne(ctx); !worked || !errors.Is(e, ErrUnavailable) {
		t.Fatal(worked, e)
	}
	op, e := a.Operation(ctx, id)
	if e != nil || op.State != "requested" || op.Proof != "" {
		t.Fatal("failed storage claimed removal", op, e)
	}
	if _, e = store.Head(ctx, key); e != nil {
		t.Fatal("failed attempt removed actual object", e)
	}
	fresh, e := NewAuthority(a.Pool, a.Schema, a.MetadataSchema, a.SafetySchema)
	if e != nil {
		t.Fatal(e)
	}
	worker = DeleteWorker{Authority: fresh, Stores: map[string]*sharedstorage.Client{"variant": store}}
	if worked, e := worker.RunOne(ctx); !worked || e != nil {
		t.Fatal(worked, e)
	}
	op, e = fresh.Operation(ctx, id)
	if e != nil || op.State != "complete" || op.ID != id {
		t.Fatal(op, e)
	}
	if _, e = store.Head(ctx, key); !errors.Is(e, sharedstorage.ErrNotFound) {
		t.Fatal("recovery left media bytes", e)
	}
	if worked, e := worker.RunOne(ctx); worked || e != nil {
		t.Fatal("completed delete replayed", worked, e)
	}
}
