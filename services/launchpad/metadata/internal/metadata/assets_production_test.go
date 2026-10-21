package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/internaltest"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixtureRasterWorker struct{ onDecode func() }

func (w fixtureRasterWorker) Decode(c context.Context, b []byte) (raster.Raster, error) {
	if w.onDecode != nil {
		w.onDecode()
	}
	return raster.Reencode(c, b)
}

type fixturePublisher struct {
	objects map[string][]byte
	fail    bool
}

func (p *fixturePublisher) PutImmutable(_ context.Context, k string, b []byte) error {
	if old, ok := p.objects[k]; ok && !bytes.Equal(old, b) {
		return ErrConflict
	}
	p.objects[k] = append([]byte(nil), b...)
	if p.fail {
		p.fail = false
		return fmt.Errorf("injected copy-before-commit interruption")
	}
	return nil
}
func TestProductionAssetLifecycle(t *testing.T) {
	ctx, c := context.WithTimeout(context.Background(), 20*time.Second)
	defer c()
	db, e := pgxpool.New(ctx, "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	schema := fmt.Sprintf("kd_acf8ff69ca3a26ba76d2244d_asset_%d", time.Now().UnixNano())
	db.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e := db.Exec(c, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Errorf("cleanup: %v", e)
		}
	}()
	s, e := NewProductionStore(db, schema, testVerifier{})
	s.RasterWorker = fixtureRasterWorker{}
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, s.SchemaDDL()); e != nil {
		t.Fatal(e)
	}
	pool := "0x" + strings.Repeat("1", 40)
	db.Exec(ctx, "INSERT INTO "+s.table("pools")+" VALUES($1,$1,$1)", pool)
	db.Exec(ctx, "INSERT INTO "+s.table("documents")+"(pool) VALUES($1)", pool)
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 4, 4)))
	h := sha256.Sum256(b.Bytes())
	id := "44444444-4444-4444-4444-444444444444"
	a := AssetRecord{ID: id, Pool: pool, Hash: hex.EncodeToString(h[:]), Size: int64(b.Len()), ObjectKey: "quarantine/" + id}
	if e = s.CreateAsset(ctx, a); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateAsset(ctx, a); e != nil {
		t.Fatal(e)
	}
	for _, v := range [][2]string{{"CREATED", "UPLOADING"}, {"UPLOADING", "QUARANTINED"}, {"QUARANTINED", "VALIDATING"}} {
		if e = s.TransitionAsset(ctx, id, v[0], v[1]); e != nil {
			t.Fatal(e)
		}
	}
	decoded, e := raster.Reencode(ctx, b.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	pub := &fixturePublisher{map[string][]byte{}, true}
	scan := sharedhttp.ScanResult{Clean: true, Verdict: sharedhttp.ScanClean, Engine: "fixture-protocol", SignatureRevision: "test-v1", ContentHash: a.Hash, Size: a.Size}
	if e = s.Promote(ctx, id, b.Bytes(), decoded.Variants, sharedhttp.ScanResult{Verdict: sharedhttp.ScanUnavailable}, pub); e != ErrForbidden || len(pub.objects) != 0 {
		t.Fatal(e)
	}
	if e = s.Promote(ctx, id, b.Bytes(), decoded.Variants, scan, pub); e == nil {
		t.Fatal("interruption missing")
	}
	originalCopy := append([]byte(nil), b.Bytes()...)
	s.RasterWorker = fixtureRasterWorker{onDecode: func() { b.Bytes()[0] = 0; decoded.Variants[256][0] = 0 }}
	if e = s.Promote(ctx, id, b.Bytes(), decoded.Variants, scan, pub); e != nil {
		t.Fatal(e)
	}
	b.Reset()
	b.Write(originalCopy)
	decoded, _ = raster.Reencode(ctx, b.Bytes())
	s.RasterWorker = fixtureRasterWorker{}
	if len(pub.objects) != 3 {
		t.Fatal(len(pub.objects))
	}
	if e = s.Promote(ctx, id, b.Bytes(), decoded.Variants, scan, pub); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApprovedReference(ctx, id); e != nil {
		t.Fatal(e)
	}
	s.Restrict(ctx, pool, "2", true)
	if _, e = s.ApprovedReference(ctx, id); e != ErrMissing {
		t.Fatal(e)
	}
}

func TestProductionAdversarialActualStorageRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dsn := os.Getenv("KINDLE_PRODUCTION_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	schema := fmt.Sprintf("metadata_adversarial_%d", time.Now().UnixNano())
	if _, e = db.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatal(e)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e = db.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	store, e := NewProductionStore(db, schema, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, store.SchemaDDL()+store.V2SchemaDDL()+store.ConnectedSchemaDDL()); e != nil {
		t.Fatal(e)
	}
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	worker := raster.ProcessWorker{Executable: executable, CgroupParent: os.Getenv("KINDLE_RASTER_CGROUP_PARENT")}
	if e = worker.Ready(); e != nil {
		t.Fatal("actual delegated raster worker required:", e)
	}
	store.RasterWorker = worker
	// Throwaway buckets on the METADATA_TEST_S3_* MinIO (never a container),
	// emptied and removed on cleanup.
	quarantine, approved := internaltest.NewMinIO(t, "metadata-quarantine"), internaltest.NewMinIO(t, "metadata-approved")
	cfg, e := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(quarantine.AccessKeyID, quarantine.SecretAccessKey, "")))
	if e != nil {
		t.Fatal(e)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(quarantine.Endpoint); o.UsePathStyle = true })
	objects := S3AssetObjects{Client: client, QuarantineBucket: quarantine.Bucket, PublicBucket: approved.Bucket, PublicOrigin: "https://cdn.example"}
	if e = objects.Ready(ctx); e != nil {
		t.Fatal(e)
	}
	pool := "0x" + strings.Repeat("1", 40)
	key, e := crypto.GenerateKey()
	if e != nil {
		t.Fatal(e)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	if _, e = db.Exec(ctx, "INSERT INTO "+store.table("pools")+" VALUES($1,$2,$1)", pool, wallet); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "INSERT INTO "+store.table("documents")+"(pool) VALUES($1)", pool); e != nil {
		t.Fatal(e)
	}
	var original bytes.Buffer
	if e = png.Encode(&original, image.NewNRGBA(image.Rect(0, 0, 16, 12))); e != nil {
		t.Fatal(e)
	}
	decoded, e := worker.Decode(ctx, original.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(original.Bytes())
	id := "55555555-5555-5555-5555-555555555555"
	a := AssetRecord{ID: id, Pool: pool, Hash: hex.EncodeToString(hash[:]), Size: int64(original.Len()), ObjectKey: "quarantine/" + id}
	if _, e = db.Exec(ctx, "ALTER TABLE "+store.table("asset_details_v2")+" ADD CONSTRAINT injected_details_failure CHECK(width <> 16)"); e != nil {
		t.Fatal(e)
	}
	if e = store.CreateConnectedAsset(ctx, a, "image/png", 16, 12); e == nil {
		t.Fatal("actual inventory failure did not abort admission")
	}
	var count int
	if e = db.QueryRow(ctx, "SELECT count(*) FROM "+store.table("assets")).Scan(&count); e != nil || count != 0 {
		t.Fatal("partial asset admission", count, e)
	}
	if _, e = db.Exec(ctx, "ALTER TABLE "+store.table("asset_details_v2")+" DROP CONSTRAINT injected_details_failure"); e != nil {
		t.Fatal(e)
	}
	if e = store.CreateConnectedAsset(ctx, a, "image/png", 16, 12); e != nil {
		t.Fatal(e)
	}
	if e = store.CreateConnectedAsset(ctx, a, "image/png", 16, 12); e != nil {
		t.Fatal(e)
	}
	foreign := a
	foreign.ID = "66666666-6666-6666-6666-666666666666"
	if e = store.CreateConnectedAsset(ctx, foreign, "image/png", 16, 12); !errors.Is(e, ErrConflict) {
		t.Fatal("duplicate content allocated a second asset", e)
	}
	if e = objects.put(ctx, objects.QuarantineBucket, a.ObjectKey, original.Bytes(), "image/png"); e != nil {
		t.Fatal(e)
	}
	for _, pair := range [][2]string{{"CREATED", "UPLOADING"}, {"UPLOADING", "QUARANTINED"}, {"QUARANTINED", "VALIDATING"}} {
		if e = store.TransitionAsset(ctx, id, pair[0], pair[1]); e != nil {
			t.Fatal(e)
		}
	}
	unavailableScan := sharedhttp.ScanVerifiedBuffer(ctx, original.Bytes(), sharedhttp.ScanOptions{Host: "127.0.0.1", Port: 1, Timeout: time.Millisecond})
	if unavailableScan.Clean || unavailableScan.Verdict != sharedhttp.ScanUnavailable {
		t.Fatal(unavailableScan)
	}
	if e = store.Promote(ctx, id, original.Bytes(), decoded.Variants, unavailableScan, objects); !errors.Is(e, ErrForbidden) {
		t.Fatal("scanner outage permitted promotion", e)
	}
	var preservedState string
	var activeImage *string
	if e = db.QueryRow(ctx, "SELECT state FROM "+store.table("assets")+" WHERE id=$1", id).Scan(&preservedState); e != nil || preservedState != "VALIDATING" {
		t.Fatal("scanner outage changed quarantine", preservedState, e)
	}
	if e = db.QueryRow(ctx, "SELECT asset_id FROM "+store.table("documents")+" WHERE pool=$1", pool).Scan(&activeImage); e != nil || activeImage != nil {
		t.Fatal("scanner outage changed active metadata", activeImage, e)
	}
	privateOnly, e := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(objects.PublicBucket)})
	if e != nil || len(privateOnly.Contents) != 0 {
		t.Fatal("unscanned bytes escaped quarantine", privateOnly, e)
	}
	scan := sharedhttp.ScanVerifiedBuffer(ctx, original.Bytes(), sharedhttp.ScanOptions{})
	if scan.Verdict != sharedhttp.ScanClean || !scan.Clean {
		t.Fatal("actual configured clamd with loaded signatures required:", scan)
	}
	unavailableStore := *store
	unavailableStore.RasterWorker = raster.ProcessWorker{}
	if e = unavailableStore.Promote(ctx, id, original.Bytes(), decoded.Variants, scan, objects); !errors.Is(e, raster.ErrUnavailable) {
		t.Fatal("unavailable worker was replaced by fallback", e)
	}
	missingBucket := objects
	missingBucket.PublicBucket = "metadata-missing-approved"
	if e = store.Promote(ctx, id, original.Bytes(), decoded.Variants, scan, missingBucket); !errors.Is(e, raster.ErrUnavailable) {
		t.Fatal("real storage failure was hidden", e)
	}
	if e = db.QueryRow(ctx, "SELECT state FROM "+store.table("assets")+" WHERE id=$1", id).Scan(&preservedState); e != nil || preservedState != "VALIDATING" {
		t.Fatal("failed dependencies changed quarantine", preservedState, e)
	}
	if _, e = db.Exec(ctx, "ALTER TABLE "+store.table("assets")+" ADD CONSTRAINT injected_approval_failure CHECK(state <> 'APPROVED')"); e != nil {
		t.Fatal(e)
	}
	if e = store.Promote(ctx, id, original.Bytes(), decoded.Variants, scan, objects); e == nil {
		t.Fatal("actual SQL approval failure was not surfaced")
	}
	var state string
	var inventory []byte
	if e = db.QueryRow(ctx, "SELECT a.state,d.variants FROM "+store.table("assets")+" a JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.id=$1", id).Scan(&state, &inventory); e != nil || state != "VALIDATING" || string(inventory) != "{}" {
		t.Fatal("approval or inventory escaped rollback", state, string(inventory), e)
	}
	before, e := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(objects.PublicBucket)})
	if e != nil || len(before.Contents) != 3 {
		t.Fatal("copy boundary inventory", before, e)
	}
	if _, e = db.Exec(ctx, "ALTER TABLE "+store.table("assets")+" DROP CONSTRAINT injected_approval_failure"); e != nil {
		t.Fatal(e)
	}
	if e = store.Promote(ctx, id, original.Bytes(), decoded.Variants, scan, objects); e != nil {
		t.Fatal(e)
	}
	if e = store.Promote(ctx, id, original.Bytes(), decoded.Variants, scan, objects); e != nil {
		t.Fatal(e)
	}
	after, e := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(objects.PublicBucket)})
	if e != nil || len(after.Contents) != 3 {
		t.Fatal("recovery duplicated objects", after, e)
	}
	for _, object := range after.Contents {
		if object.LastModified == nil {
			t.Fatal("missing provider timestamp")
		}
		var previous *time.Time
		for _, old := range before.Contents {
			if aws.ToString(old.Key) == aws.ToString(object.Key) {
				previous = old.LastModified
			}
		}
		if previous == nil || !previous.Equal(*object.LastModified) {
			t.Fatal("retry rewrote immutable object", aws.ToString(object.Key))
		}
	}
	if e = db.QueryRow(ctx, "SELECT a.state,d.variants FROM "+store.table("assets")+" a JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.id=$1", id).Scan(&state, &inventory); e != nil || state != "APPROVED" {
		t.Fatal(state, e)
	}
	var keys map[string]string
	if e = json.Unmarshal(inventory, &keys); e != nil || len(keys) != 3 {
		t.Fatal("approved inventory incomplete", string(inventory), e)
	}
	for _, n := range []int{256, 512, 1024} {
		stored, e := objects.read(ctx, objects.PublicBucket, keys[fmt.Sprint(n)])
		if e != nil || !bytes.Equal(stored, decoded.Variants[n]) {
			t.Fatal("provider/SQL inventory mismatch", n, e)
		}
	}
	secondImage := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	secondImage.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var second bytes.Buffer
	if e = png.Encode(&second, secondImage); e != nil {
		t.Fatal(e)
	}
	secondDecoded, e := worker.Decode(ctx, second.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	secondHash := sha256.Sum256(second.Bytes())
	secondID := "77777777-7777-7777-7777-777777777777"
	secondAsset := AssetRecord{ID: secondID, Pool: pool, Hash: hex.EncodeToString(secondHash[:]), Size: int64(second.Len()), ObjectKey: "quarantine/" + secondID}
	if e = store.CreateConnectedAsset(ctx, secondAsset, "image/png", 16, 12); e != nil {
		t.Fatal(e)
	}
	if e = objects.put(ctx, objects.QuarantineBucket, secondAsset.ObjectKey, second.Bytes(), "image/png"); e != nil {
		t.Fatal(e)
	}
	for _, pair := range [][2]string{{"CREATED", "UPLOADING"}, {"UPLOADING", "QUARANTINED"}, {"QUARANTINED", "VALIDATING"}} {
		if e = store.TransitionAsset(ctx, secondID, pair[0], pair[1]); e != nil {
			t.Fatal(e)
		}
	}
	secondScan := sharedhttp.ScanVerifiedBuffer(ctx, second.Bytes(), sharedhttp.ScanOptions{})
	if secondScan.Verdict != sharedhttp.ScanClean {
		t.Fatal(secondScan)
	}
	if e = store.Promote(ctx, secondID, second.Bytes(), secondDecoded.Variants, secondScan, objects); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "INSERT INTO "+store.table("v2_documents")+"(pool,revision) VALUES($1,7)", pool); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	audience := "https://metadata.example"
	store.Verifier = V2SignedVerifier{Store: store, Audience: audience, Clock: func() time.Time { return now }}
	results := make(chan error, 2)
	var concurrent sync.WaitGroup
	for i, imageID := range []string{id, secondID} {
		raw, e := json.Marshal(map[string]any{"expectedRevision": "7", "logoAssetId": imageID})
		if e != nil {
			t.Fatal(e)
		}
		bodyHash := sha256.Sum256(raw)
		action := VerifiedAction{Wallet: wallet, OperationID: uuid.NewString(), Method: "PATCH", Path: "/v2/tokens/" + pool + "/metadata", BodyHash: hex.EncodeToString(bodyHash[:]), Nonce: strings.Repeat(fmt.Sprint(i+1), 64), ExpiresAt: fmt.Sprint(now.Add(time.Minute).Unix()), Audience: audience, ChainID: 125}
		signature, e := crypto.Sign(accounts.TextHash([]byte(V2SigningMessage(action))), key)
		if e != nil {
			t.Fatal(e)
		}
		action.Signature = "0x" + hex.EncodeToString(signature)
		concurrent.Add(1)
		go func(raw []byte, action VerifiedAction) {
			defer concurrent.Done()
			_, e := store.ApplyV2(ctx, pool, raw, action, now)
			results <- e
		}(raw, action)
	}
	concurrent.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for e := range results {
		if e == nil {
			accepted++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatal("concurrent approved image conflict", accepted, conflicts)
	}
	var activeRevision, activeAsset string
	if e = db.QueryRow(ctx, "SELECT revision::text,asset_id FROM "+store.table("v2_documents")+" WHERE pool=$1", pool).Scan(&activeRevision, &activeAsset); e != nil || activeRevision != "8" || (activeAsset != id && activeAsset != secondID) {
		t.Fatal(activeRevision, activeAsset, e)
	}
	if e = db.QueryRow(ctx, "SELECT count(*) FROM "+store.table("v2_outbox")).Scan(&count); e != nil || count != 1 {
		t.Fatal("duplicate active image event", count, e)
	}
	if e = store.Restrict(ctx, pool, "10", true); e != nil {
		t.Fatal(e)
	}
	if e = store.Restrict(ctx, pool, "9", false); e != nil {
		t.Fatal(e)
	}
	if _, e = store.ApprovedReference(ctx, id); !errors.Is(e, ErrMissing) {
		t.Fatal("old allow exposed blocked media", e)
	}
	if e = store.Promote(ctx, id, original.Bytes(), decoded.Variants, scan, objects); !errors.Is(e, ErrForbidden) {
		t.Fatal("blocked approved replay published", e)
	}
}
