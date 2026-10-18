package internaltest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"
)

// The integration tests always run against already-running services; no
// container is ever started. Each helper needs its own variable and fails the
// test immediately when it is unset:
//
//	METADATA_TEST_DATABASE_URL   admin DSN; each test gets a throwaway database (dropped after)
//	METADATA_TEST_REDIS_URL      Redis URL naming the logical db reserved for this suite (flushed after)
//	METADATA_TEST_S3_ENDPOINT, METADATA_TEST_S3_ACCESS_KEY_ID, METADATA_TEST_S3_SECRET_ACCESS_KEY
//	                             S3/MinIO; each test gets a throwaway bucket (emptied and removed after)
//	METADATA_TEST_CLAMAV_ADDR    optional host:port of a real clamd

func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		t.Fatalf("%s is not set: the metadata integration tests run only against already running servers (METADATA_TEST_DATABASE_URL, METADATA_TEST_REDIS_URL, METADATA_TEST_S3_*); tests never start containers", name)
	}
	return v
}

func suffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// externalDatabase creates a throwaway database on the admin DSN's server and
// returns its DSN; it is dropped (FORCE) on cleanup.
func externalDatabase(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	admin := requireEnv(t, "METADATA_TEST_DATABASE_URL")
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("external postgres: %v", err)
	}
	defer conn.Close(ctx)
	name := "metadata_test_" + suffix(t)
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), admin)
		if err != nil {
			t.Logf("drop test database %s: %v", name, err)
			return
		}
		defer c.Close(context.Background())
		if _, err = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database %s: %v", name, err)
		}
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

// externalRedis claims the logical database named in METADATA_TEST_REDIS_URL
// (waiting until it is empty) with a marker key and flushes it on cleanup.
func externalRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	base := requireEnv(t, "METADATA_TEST_REDIS_URL")
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	marker := "metadata-test-claim:" + suffix(t)
	// The logical database named in METADATA_TEST_REDIS_URL is the only one
	// this suite may use (shared fixture servers assign one per suite);
	// concurrent claims wait for it to be released.
	if u.Path == "" || u.Path == "/" {
		t.Fatal("METADATA_TEST_REDIS_URL must name the logical database reserved for this suite (redis://:password@host:port/N)")
	}
	pinned, err := strconv.Atoi(strings.TrimPrefix(u.Path, "/"))
	if err != nil || pinned < 0 || pinned > 15 {
		t.Fatalf("METADATA_TEST_REDIS_URL database %q outside 0..15", strings.TrimPrefix(u.Path, "/"))
	}
	candidates := make([]int, 0, 600)
	for i := 0; i < 600; i++ {
		candidates = append(candidates, pinned)
	}
	for attempt, db := range candidates {
		if attempt > 0 && candidates[0] == db {
			time.Sleep(200 * time.Millisecond)
		}
		u.Path = "/" + strconv.Itoa(db)
		opts, err := goredis.ParseURL(u.String())
		if err != nil {
			t.Fatal(err)
		}
		client := goredis.NewClient(opts)
		size, err := client.DBSize(ctx).Result()
		if err == nil && size == 0 {
			if ok, _ := client.SetNX(ctx, marker, "1", 0).Result(); ok {
				if n, _ := client.DBSize(ctx).Result(); n == 1 {
					claimed := u.String()
					t.Cleanup(func() { _ = client.FlushDB(context.Background()).Err(); _ = client.Close() })
					return claimed
				}
				_ = client.Del(ctx, marker).Err()
			}
		}
		_ = client.Close()
	}
	t.Fatal("external redis: the METADATA_TEST_REDIS_URL logical database never became empty")
	return ""
}

// externalMinIO creates a throwaway bucket, emptied and removed on cleanup.
func externalMinIO(t *testing.T, bucket string) MinIO {
	t.Helper()
	ctx := context.Background()
	endpoint := requireEnv(t, "METADATA_TEST_S3_ENDPOINT")
	key := requireEnv(t, "METADATA_TEST_S3_ACCESS_KEY_ID")
	secret := requireEnv(t, "METADATA_TEST_S3_SECRET_ACCESS_KEY")
	name := bucket + "-" + suffix(t)
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion("us-east-1"), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, "")))
	if err != nil {
		t.Fatal(err)
	}
	raw := s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(endpoint); o.UsePathStyle = true })
	if _, err = raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		t.Fatalf("create test bucket: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		listed, err := raw.ListObjectsV2(c, &s3.ListObjectsV2Input{Bucket: aws.String(name)})
		if err == nil && len(listed.Contents) > 0 {
			ids := make([]s3types.ObjectIdentifier, 0, len(listed.Contents))
			for _, o := range listed.Contents {
				ids = append(ids, s3types.ObjectIdentifier{Key: o.Key})
			}
			_, _ = raw.DeleteObjects(c, &s3.DeleteObjectsInput{Bucket: aws.String(name), Delete: &s3types.Delete{Objects: ids}})
		}
		if _, err = raw.DeleteBucket(c, &s3.DeleteBucketInput{Bucket: aws.String(name)}); err != nil {
			t.Logf("remove test bucket %s: %v", name, err)
		}
	})
	client, err := storage.New(ctx, storage.Config{Endpoint: endpoint, Region: "us-east-1", AccessKeyID: key, SecretAccessKey: secret, Bucket: name})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return MinIO{Endpoint: endpoint, AccessKeyID: key, SecretAccessKey: secret, Bucket: name, Store: client}
}

func externalClamd(t *testing.T) (string, int) {
	t.Helper()
	addr := strings.TrimSpace(os.Getenv("METADATA_TEST_CLAMAV_ADDR"))
	if addr == "" {
		return "", 0
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("METADATA_TEST_CLAMAV_ADDR: %v", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatalf("METADATA_TEST_CLAMAV_ADDR: %v", err)
	}
	return host, n
}
