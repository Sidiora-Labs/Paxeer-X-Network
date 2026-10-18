package storage

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/internaltest"
)

func TestConnectedOrphanRemainsDenied(t *testing.T) {
	a, ctx := connectedAuthority(t)
	_, key := seedConnectedAsset(t, a, ctx)
	store := internaltest.NewStore(t, "l41-orphan-inventory")
	orphan := key[:len(key)-4] + "orphan.png"
	if e := store.Put(ctx, orphan, bytes.NewReader([]byte("owned orphan")), 12, "image/png"); e != nil {
		t.Fatal(e)
	}
	if e := a.Allowed(ctx, orphan); e == nil {
		t.Fatal("unregistered orphan became public")
	}
}

func TestConnectedInventoryDetectsRealObjectDigestMutation(t *testing.T) {
	a, ctx := connectedAuthority(t)
	hash, _ := seedConnectedAsset(t, a, ctx)
	bucket := "l41-inventory-" + fmt.Sprint(time.Now().UnixNano())
	backend := internaltest.NewMinIO(t, bucket)
	cfg, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(backend.AccessKeyID, backend.SecretAccessKey, "")))
	if e != nil {
		t.Fatal(e)
	}
	raw := s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(backend.Endpoint); o.UsePathStyle = true })
	store := backend.Client(t, bucket)
	var encoded bytes.Buffer
	if e = png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 1, 1))); e != nil {
		t.Fatal(e)
	}
	body := encoded.Bytes()
	key := "approved/" + hash + "/512/" + fmt.Sprintf("%x", sha256.Sum256(body)) + ".png"
	if e = a.RegisterLineage(ctx, LineageObject{Hash: hash, Key: key, Kind: "variant", RetainUntil: time.Now().Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	if e = store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); e != nil {
		t.Fatal(e)
	}
	report, e := a.ReconcileInventory(ctx, raw, bucket, "approved/", 100)
	if e != nil || report.Checked != 1 || len(report.Orphans) != 0 {
		t.Fatal(report, e)
	}
	changed := append([]byte(nil), body...)
	changed[len(changed)-1] ^= 1
	if e = store.Put(ctx, key, bytes.NewReader(changed), int64(len(changed)), "image/png"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.ReconcileInventory(ctx, raw, bucket, "approved/", 100); !errors.Is(e, ErrConflict) {
		t.Fatal("mutated immutable object digest accepted", e)
	}
}
