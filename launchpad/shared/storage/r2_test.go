package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
)

const testBucket = "media-test"

// testMinIO is the already running S3-compatible server named by
// SHARED_TEST_MINIO_ENDPOINT, SHARED_TEST_MINIO_ACCESS_KEY_ID and
// SHARED_TEST_MINIO_SECRET_ACCESS_KEY (containers are never started).
type testMinIO struct {
	endpoint, accessKey, secretKey string
	raw                            *s3.Client
}

// newTestMinIO connects to the SHARED_TEST_MINIO_* server and creates bucket
// there (reusing it if a previous run left it), emptying and removing it on
// cleanup. The test fails immediately when the variables are unset.
func newTestMinIO(t *testing.T, bucket string) testMinIO {
	t.Helper()
	ctx := context.Background()
	m := testMinIO{
		endpoint:  os.Getenv("SHARED_TEST_MINIO_ENDPOINT"),
		accessKey: os.Getenv("SHARED_TEST_MINIO_ACCESS_KEY_ID"),
		secretKey: os.Getenv("SHARED_TEST_MINIO_SECRET_ACCESS_KEY"),
	}
	if m.endpoint == "" || m.accessKey == "" || m.secretKey == "" {
		t.Fatal("SHARED_TEST_MINIO_ENDPOINT, SHARED_TEST_MINIO_ACCESS_KEY_ID and SHARED_TEST_MINIO_SECRET_ACCESS_KEY must name a running MinIO; tests never start containers")
	}
	rawCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(m.accessKey, m.secretKey, ""),
		),
	)
	if err != nil {
		t.Fatalf("load aws config: %v", err)
	}
	m.raw = s3.NewFromConfig(rawCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(m.endpoint)
		o.UsePathStyle = true
	})
	if _, err := m.raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		var owned *s3types.BucketAlreadyOwnedByYou
		if !errors.As(err, &owned) {
			t.Fatalf("create bucket: %v", err)
		}
	}
	t.Cleanup(func() {
		c := context.Background()
		listed, err := m.raw.ListObjectsV2(c, &s3.ListObjectsV2Input{Bucket: aws.String(bucket)})
		if err == nil {
			for _, o := range listed.Contents {
				_, _ = m.raw.DeleteObject(c, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: o.Key})
			}
		}
		if _, err = m.raw.DeleteBucket(c, &s3.DeleteBucketInput{Bucket: aws.String(bucket)}); err != nil {
			t.Logf("remove bucket %s: %v", bucket, err)
		}
	})
	return m
}

// newClient creates the test bucket on the SHARED_TEST_MINIO_* server
// (S3-compatible — no fakes) and returns a storage.Client built through the
// real New constructor.
func newClient(t *testing.T) *storage.Client {
	t.Helper()
	ctx := context.Background()
	m := newTestMinIO(t, testBucket)

	client, err := storage.New(ctx, storage.Config{
		Endpoint:        m.endpoint,
		Region:          "us-east-1",
		AccessKeyID:     m.accessKey,
		SecretAccessKey: m.secretKey,
		Bucket:          testBucket,
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return client
}

func TestClient_PutGetRoundTrip(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	cases := []struct {
		name        string
		key         string
		data        []byte
		contentType string
	}{
		{"png logo", "logos/logo-0xabc.png", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}, "image/png"},
		{"svg banner", "banners/banner-0xdef.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), "image/svg+xml"},
		{"empty object", "empty/zero.bin", []byte{}, "application/octet-stream"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := client.Put(ctx, tc.key, bytes.NewReader(tc.data), int64(len(tc.data)), tc.contentType); err != nil {
				t.Fatalf("Put: %v", err)
			}

			obj, err := client.Get(ctx, tc.key)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			defer obj.Body.Close()
			got, err := io.ReadAll(obj.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !bytes.Equal(got, tc.data) {
				t.Errorf("body = %v, want %v", got, tc.data)
			}
			if obj.ContentType != tc.contentType {
				t.Errorf("content-type = %q, want %q", obj.ContentType, tc.contentType)
			}
			if obj.Size != int64(len(tc.data)) {
				t.Errorf("size = %d, want %d", obj.Size, len(tc.data))
			}
			if obj.ETag == "" {
				t.Error("expected non-empty ETag")
			}
		})
	}
}

func TestClient_GetBytes(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	want := []byte("hello-bucket")
	if err := client.Put(ctx, "k/file.txt", bytes.NewReader(want), int64(len(want)), "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ct, err := client.GetBytes(ctx, "k/file.txt")
	if err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("bytes = %q, want %q", got, want)
	}
	if ct != "text/plain" {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
}

func TestClient_Head(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	data := []byte("metadata-probe")
	if err := client.Put(ctx, "h/obj", bytes.NewReader(data), int64(len(data)), "application/json"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := client.Head(ctx, "h/obj")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if info.Size != int64(len(data)) {
		t.Errorf("size = %d, want %d", info.Size, len(data))
	}
	if info.ContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", info.ContentType)
	}
}

func TestClient_Delete(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	if err := client.Put(ctx, "d/gone", strings.NewReader("x"), 1, "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := client.Delete(ctx, "d/gone"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := client.Get(ctx, "d/gone"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get after Delete err = %v, want ErrNotFound", err)
	}
	// Deleting an already-absent key is a no-op.
	if err := client.Delete(ctx, "d/gone"); err != nil {
		t.Errorf("second Delete err = %v, want nil", err)
	}
}

func TestClient_NotFound(t *testing.T) {
	client := newClient(t)
	ctx := context.Background()

	if _, err := client.Get(ctx, "missing/key"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get err = %v, want ErrNotFound", err)
	}
	if _, _, err := client.GetBytes(ctx, "missing/key"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetBytes err = %v, want ErrNotFound", err)
	}
	if _, err := client.Head(ctx, "missing/key"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Head err = %v, want ErrNotFound", err)
	}
}

func TestClient_Bucket(t *testing.T) {
	client := newClient(t)
	if client.Bucket() != testBucket {
		t.Errorf("Bucket() = %q, want %q", client.Bucket(), testBucket)
	}
}

func TestNewFromS3(t *testing.T) {
	ctx := context.Background()
	raw := newTestMinIO(t, "shared-raw").raw

	client := storage.NewFromS3(raw, "shared-raw")
	if client.Bucket() != "shared-raw" {
		t.Fatalf("Bucket() = %q, want shared-raw", client.Bucket())
	}
	want := []byte("via-raw-client")
	if err := client.Put(ctx, "k", bytes.NewReader(want), int64(len(want)), "text/plain"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, _, err := client.GetBytes(ctx, "k")
	if err != nil {
		t.Fatalf("GetBytes: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("bytes = %q, want %q", got, want)
	}
}

// TestClient_BackendErrors points a Client at an endpoint with nothing
// listening (retries disabled for speed) and asserts every operation surfaces a
// wrapped transport error — NOT ErrNotFound. This exercises the error-wrap
// branches and isNotFound's negative path without a container.
func TestClient_BackendErrors(t *testing.T) {
	ctx := context.Background()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("k", "s", ""),
		),
	)
	if err != nil {
		t.Fatalf("load aws config: %v", err)
	}
	raw := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("http://127.0.0.1:1") // refused
		o.UsePathStyle = true
		o.Retryer = aws.NopRetryer{}
	})
	client := storage.NewFromS3(raw, "dead")

	if err := client.Put(ctx, "k", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Error("Put: expected transport error, got nil")
	}
	if _, err := client.Get(ctx, "k"); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get err = %v, want non-nil non-ErrNotFound", err)
	}
	if _, _, err := client.GetBytes(ctx, "k"); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetBytes err = %v, want non-nil non-ErrNotFound", err)
	}
	if _, err := client.Head(ctx, "k"); err == nil || errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Head err = %v, want non-nil non-ErrNotFound", err)
	}
	if err := client.Delete(ctx, "k"); err == nil {
		t.Error("Delete: expected transport error, got nil")
	}
}

func TestNew_ConfigValidation(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		cfg  storage.Config
	}{
		{"missing endpoint", storage.Config{AccessKeyID: "a", SecretAccessKey: "s", Bucket: "b"}},
		{"missing access key", storage.Config{Endpoint: "http://x", SecretAccessKey: "s", Bucket: "b"}},
		{"missing secret", storage.Config{Endpoint: "http://x", AccessKeyID: "a", Bucket: "b"}},
		{"missing bucket", storage.Config{Endpoint: "http://x", AccessKeyID: "a", SecretAccessKey: "s"}},
		{"all empty", storage.Config{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := storage.New(ctx, tc.cfg); err == nil {
				t.Errorf("New(%+v) err = nil, want validation error", tc.cfg)
			}
		})
	}
}
