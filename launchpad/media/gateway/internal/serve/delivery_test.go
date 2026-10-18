package serve

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/cache"
	mediastore "github.com/Sidiora-Technologies/KindleLaunch/media/gateway/internal/storage"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	redis "github.com/redis/go-redis/v9"
)

type observedTransport struct {
	gets  atomic.Int64
	heads atomic.Int64
}

func (o *observedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == "GET" {
		o.gets.Add(1)
	}
	if r.Method == "HEAD" {
		o.heads.Add(1)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestApprovedColdConditionalAndBoundedRange(t *testing.T) {
	ctx := context.Background()
	id := make([]byte, 12)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	bucket := "kd-fec98588ef6fe4334797a5de-" + hex.EncodeToString(id)
	observed := &observedTransport{}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"), config.WithHTTPClient(&http.Client{Transport: observed, Timeout: 3 * time.Second}), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("kindle_fixture", "kindle_fixture_password", "")))
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
	ledger, err := mediastore.NewLedger(db, bucket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		keys, _ := db.Keys(ctx, bucket+":*").Result()
		if len(keys) > 0 {
			db.Del(ctx, keys...)
		}
	}()
	body := []byte(strings.Repeat("owned-synthetic-raster", 60000))
	key, err := mediastore.ApprovedKey(body, "png256")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Delete(ctx, key)
	if err = store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); err != nil {
		t.Fatal(err)
	}
	if err = ledger.Restrict(ctx, strings.Split(key, "/")[1], 1, "approved"); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	NewApproved(Deps{Buckets: map[string]Reader{"token": store}}, ledger).RegisterRoutes(router)
	request := func(method string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/media/token/"+key, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	head := request("HEAD", nil)
	if head.Code != 200 || head.Body.Len() != 0 || observed.gets.Load() != 0 || observed.heads.Load() != 1 {
		t.Fatal(head.Code, head.Body.Len(), observed.gets.Load(), observed.heads.Load())
	}
	etag := head.Header().Get("ETag")
	conditional := request("GET", map[string]string{"If-None-Match": etag})
	if conditional.Code != 304 || conditional.Body.Len() != 0 || observed.gets.Load() != 0 {
		t.Fatal(conditional.Code, conditional.Body.Len(), observed.gets.Load())
	}
	part := request("GET", map[string]string{"Range": "bytes=20-39"})
	if part.Code != 206 || !bytes.Equal(part.Body.Bytes(), body[20:40]) || part.Header().Get("Content-Range") == "" {
		t.Fatal(part.Code, part.Header(), part.Body.Len())
	}
	for _, value := range []string{"bytes=0-1048576", "bytes=0-1,3-4", "bytes=-0", "bytes=99999999-"} {
		if w := request("GET", map[string]string{"Range": value}); w.Code != 416 {
			t.Fatal(value, w.Code)
		}
	}
	if err = store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	mediaCache := cache.New(db, time.Minute)
	defer db.Del(ctx, cacheKeyForTest("token", key)+":missing")
	router = chi.NewRouter()
	NewApproved(Deps{Buckets: map[string]Reader{"token": store}, Cache: mediaCache}, ledger).RegisterRoutes(router)
	missing := request("GET", nil)
	before := observed.gets.Load()
	if missing.Code != 404 || missing.Header().Get("Cache-Control") != "public, max-age=30" {
		t.Fatal(missing.Code, missing.Header())
	}
	if w := request("GET", nil); w.Code != 404 || observed.gets.Load() != before {
		t.Fatal(w.Code, observed.gets.Load(), before)
	}
	if err = ledger.Restrict(ctx, strings.Split(key, "/")[1], 2, "blocked"); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", nil); w.Code != 404 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Header())
	}
}

func cacheKeyForTest(bucket, key string) string {
	hash := sha256.Sum256([]byte(bucket + "\x00" + key))
	return "gw:obj:" + hex.EncodeToString(hash[:])
}
