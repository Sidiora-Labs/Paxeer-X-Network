package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	mediastore "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	redis "github.com/redis/go-redis/v9"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestApprovedMissingAuthorityAndPrivatePathsFailClosed(t *testing.T) {
	r := chi.NewRouter()
	NewApproved(Deps{}, nil).RegisterRoutes(r)
	for _, path := range []string{"/media/token/private/a.png", "/media/token/approved/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/png256"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(w.Code, w.Header())
		}
	}
}

func TestApprovedActualObjectAndWarmCacheRestriction(t *testing.T) {
	ctx := context.Background()
	bucket := "kd-fec98588ef6fe4334797a5de-" + time.Now().Format("150405000000000")
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("kindle_fixture", "kindle_fixture_password", "")))
	if err != nil {
		t.Fatal(err)
	}
	api := s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String("http://127.0.0.1:29000"); o.UsePathStyle = true })
	if _, err = api.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket}); err != nil {
		t.Fatal(err)
	}
	defer api.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: &bucket})
	store := storage.NewFromS3(api, bucket)
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	ledger, e := mediastore.NewLedger(db, bucket)
	if e != nil {
		t.Fatal(e)
	}
	body := []byte("owned synthetic approved image bytes")
	key, _ := mediastore.ApprovedKey(body, "png256")
	defer store.Delete(ctx, key)
	cacheHash := sha256.Sum256([]byte("token\x00" + key))
	defer db.Del(ctx, "gw:obj:"+hex.EncodeToString(cacheHash[:]))
	defer func() {
		keys, _ := db.Keys(ctx, bucket+":*").Result()
		if len(keys) > 0 {
			db.Del(ctx, keys...)
		}
	}()
	hash := strings.Split(key, "/")[1]
	if e = store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); e != nil {
		t.Fatal(e)
	}
	if e = ledger.Restrict(ctx, hash, 1, "approved"); e != nil {
		t.Fatal(e)
	}
	r := chi.NewRouter()
	NewApproved(Deps{Buckets: map[string]Reader{"token": store}, Cache: cache.New(db, time.Minute), CacheMaxSize: 1 << 20}, ledger).RegisterRoutes(r)
	get := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/media/token/"+key, nil))
		return w.Code
	}
	if get() != 200 {
		t.Fatal("approved denied")
	}
	if e = store.Delete(ctx, key); e != nil {
		t.Fatal(e)
	}
	if get() != 200 {
		t.Fatal("warm cache missed")
	}
	if e = ledger.Restrict(ctx, hash, 2, "blocked"); e != nil {
		t.Fatal(e)
	}
	if get() != 404 {
		t.Fatal("blocked cached asset served")
	}
	if e = ledger.Restrict(ctx, hash, 3, "deleted"); e != nil {
		t.Fatal(e)
	}
	if get() != 404 {
		t.Fatal("deleted served")
	}
}
