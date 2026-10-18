// Package internaltest provides real infrastructure (Redis, MinIO) for the
// gateway's integration tests — never fakes, and never containers: the tests
// run against already running servers named by environment variables
// (GATEWAY_TEST_REDIS_URL, GATEWAY_TEST_MINIO_*). A helper whose variable is
// unset fails the test immediately. It is imported only by *_test.go files, so
// it adds nothing to the production binary.
package internaltest

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	goredis "github.com/redis/go-redis/v9"

	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
)

// NewRedis returns a ready client plus the connection URL of the already
// running Redis named by GATEWAY_TEST_REDIS_URL. Its logical database is
// flushed and used, so the URL must select a database index reserved for these
// tests. The test fails immediately when the variable is unset.
func NewRedis(t *testing.T) (*goredis.Client, string) {
	t.Helper()
	ctx := context.Background()
	uri := os.Getenv("GATEWAY_TEST_REDIS_URL")
	if uri == "" {
		t.Fatal("GATEWAY_TEST_REDIS_URL is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers")
	}
	rdb, err := sharedredis.NewClient(uri)
	if err != nil {
		t.Fatalf("GATEWAY_TEST_REDIS_URL: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	if err = rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush GATEWAY_TEST_REDIS_URL: %v", err)
	}
	return rdb, uri
}

// MinIO describes a running MinIO server's connection details so callers can
// either build a storage.Client (NewStore) or configure a service that builds
// its own clients (e.g. the gateway app from S3_* env vars).
type MinIO struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
}

// NewMinIO returns the connection details of the already running MinIO named
// by GATEWAY_TEST_MINIO_ENDPOINT (with GATEWAY_TEST_MINIO_ACCESS_KEY_ID and
// GATEWAY_TEST_MINIO_SECRET_ACCESS_KEY) after creating the named buckets there
// if missing. The test fails immediately when the endpoint is unset.
func NewMinIO(t *testing.T, buckets ...string) MinIO {
	t.Helper()
	endpoint := os.Getenv("GATEWAY_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Fatal("GATEWAY_TEST_MINIO_ENDPOINT is not set: point it (with GATEWAY_TEST_MINIO_ACCESS_KEY_ID and GATEWAY_TEST_MINIO_SECRET_ACCESS_KEY) at a running MinIO; tests never start containers")
	}
	mc := MinIO{Endpoint: endpoint, AccessKeyID: os.Getenv("GATEWAY_TEST_MINIO_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("GATEWAY_TEST_MINIO_SECRET_ACCESS_KEY")}
	mc.createBuckets(t, buckets, true)
	return mc
}

func (mc MinIO) createBuckets(t *testing.T, buckets []string, existingOK bool) {
	t.Helper()
	ctx := context.Background()

	rawCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(mc.AccessKeyID, mc.SecretAccessKey, ""),
		),
	)
	if err != nil {
		t.Fatalf("load aws config: %v", err)
	}
	raw := s3.NewFromConfig(rawCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(mc.Endpoint)
		o.UsePathStyle = true
	})
	for _, b := range buckets {
		if _, err := raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(b)}); err != nil {
			var owned *types.BucketAlreadyOwnedByYou
			if existingOK && errors.As(err, &owned) {
				continue
			}
			t.Fatalf("create bucket %q: %v", b, err)
		}
	}
}

// Client builds a storage.Client scoped to bucket against this MinIO.
func (m MinIO) Client(t *testing.T, bucket string) *storage.Client {
	t.Helper()
	client, err := storage.New(context.Background(), storage.Config{
		Endpoint:        m.Endpoint,
		Region:          "us-east-1",
		AccessKeyID:     m.AccessKeyID,
		SecretAccessKey: m.SecretAccessKey,
		Bucket:          bucket,
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return client
}

// NewStore creates a bucket on the GATEWAY_TEST_MINIO_ENDPOINT server and
// returns a ready storage.Client (real S3-compatible backend — no fakes).
func NewStore(t *testing.T, bucket string) *storage.Client {
	t.Helper()
	return NewMinIO(t, bucket).Client(t, bucket)
}
