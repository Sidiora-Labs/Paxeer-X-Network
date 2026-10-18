// Package internaltest provides real ephemeral infrastructure (Postgres, Redis,
// MinIO, clamd) for the user service's integration tests — never fakes and never
// containers: the helpers use the already running servers named by
// USER_TEST_POSTGRES_DSN, USER_TEST_REDIS_URL and USER_TEST_MINIO_* and fail the
// test immediately when those are unset. USER_TEST_CLAMAV_ADDR (host:port of a
// real clamd) is optional; without it only checks that need a clean virus scan
// are skipped. It is imported only by *_test.go
// files, so it adds nothing to the production binary.
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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/migrate"
)

// NewPostgres creates a throwaway database on the already running PostgreSQL
// named by USER_TEST_POSTGRES_DSN (an admin DSN allowed to CREATE DATABASE),
// applies the user goose migrations, creates the cross-schema indexer.pools
// foreign table (owned by the indexer service in prod; created here so
// cross-schema reads resolve), and returns a ready pgx pool. Torn down via
// t.Cleanup. The test fails immediately when the variable is unset.
func NewPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv("USER_TEST_POSTGRES_DSN")
	if admin == "" {
		t.Fatal("USER_TEST_POSTGRES_DSN is not set: point it at a running PostgreSQL whose role may CREATE DATABASE; tests never start containers")
	}
	return preparePostgres(t, throwawayDatabase(t, admin))
}

// throwawayDatabase creates a uniquely named database on the server admin
// points at and drops it (terminating leftover sessions) at cleanup.
func throwawayDatabase(t *testing.T, admin string) string {
	t.Helper()
	ctx := context.Background()
	cfg, err := pgx.ParseConfig(admin)
	if err != nil {
		t.Fatalf("USER_TEST_POSTGRES_DSN: %v", err)
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect USER_TEST_POSTGRES_DSN: %v", err)
	}
	defer conn.Close(ctx)
	suffix := make([]byte, 6)
	if _, err = rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "user_test_" + hex.EncodeToString(suffix)
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
	t.Cleanup(func() {
		c, e := pgx.ConnectConfig(context.Background(), cfg)
		if e != nil {
			t.Logf("drop database %s: %v", name, e)
			return
		}
		defer c.Close(context.Background())
		if _, e = c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); e != nil {
			t.Logf("drop database %s: %v", name, e)
		}
	})
	u, err := url.Parse(admin)
	if err != nil || u.Scheme == "" {
		t.Fatalf("USER_TEST_POSTGRES_DSN must be a postgres:// URL")
	}
	u.Path = "/" + name
	return u.String()
}

// preparePostgres migrates dsn, creates the cross-schema fixtures, and
// returns a pool closed at cleanup.
func preparePostgres(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	if err := migrate.Up(ctx, dsn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.MaxConns = 8
	cfg.MaxConnIdleTime = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	// Cross-schema dependency: indexer.pools is owned by the indexer service in
	// prod. Create the minimal shape the user reads target.
	if _, err := pool.Exec(ctx, `
		CREATE SCHEMA IF NOT EXISTS indexer;
		CREATE TABLE IF NOT EXISTS indexer.pools (
			pool_address  varchar(42) PRIMARY KEY,
			token_address varchar(42) NOT NULL,
			creator       varchar(42) NOT NULL,
			created_at    bigint NOT NULL
		);`); err != nil {
		t.Fatalf("create indexer.pools: %v", err)
	}
	return pool
}

// SeedPool inserts a row into indexer.pools so the created-pools read resolves.
func SeedPool(t *testing.T, pool *pgxpool.Pool, poolAddr, tokenAddr, creator string, createdAt int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO indexer.pools (pool_address, token_address, creator, created_at) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (pool_address) DO UPDATE SET token_address = EXCLUDED.token_address, creator = EXCLUDED.creator, created_at = EXCLUDED.created_at`,
		poolAddr, tokenAddr, creator, createdAt); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
}

// NewRedisURL returns the already running Redis named by USER_TEST_REDIS_URL
// after flushing its logical database, so the URL must select a database index
// reserved for these tests. The test fails immediately when it is unset.
func NewRedisURL(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	uri := os.Getenv("USER_TEST_REDIS_URL")
	if uri == "" {
		t.Fatal("USER_TEST_REDIS_URL is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers")
	}
	opts, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatalf("USER_TEST_REDIS_URL: %v", err)
	}
	rdb := goredis.NewClient(opts)
	defer rdb.Close()
	if err = rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("flush USER_TEST_REDIS_URL: %v", err)
	}
	return uri
}

// NewStore creates a uniquely suffixed bucket on the already running
// S3-compatible server named by USER_TEST_MINIO_ENDPOINT,
// USER_TEST_MINIO_ACCESS_KEY and USER_TEST_MINIO_SECRET_KEY, and returns a ready
// storage.Client (real S3-compatible backend — no fakes). The bucket is emptied
// and removed at cleanup. The test fails immediately when the endpoint is unset.
func NewStore(t *testing.T, bucket string) *storage.Client {
	t.Helper()
	endpoint := os.Getenv("USER_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Fatal("USER_TEST_MINIO_ENDPOINT is not set: point it (with USER_TEST_MINIO_ACCESS_KEY and USER_TEST_MINIO_SECRET_KEY) at a running MinIO; tests never start containers")
	}
	return externalStore(t, endpoint, os.Getenv("USER_TEST_MINIO_ACCESS_KEY"), os.Getenv("USER_TEST_MINIO_SECRET_KEY"), bucket)
}

// externalStore creates a throwaway bucket on an already running S3-compatible
// server and removes it, with every object in it, at cleanup.
func externalStore(t *testing.T, endpoint, accessKey, secretKey, bucket string) *storage.Client {
	t.Helper()
	ctx := context.Background()
	if accessKey == "" || secretKey == "" {
		t.Fatal("USER_TEST_MINIO_ACCESS_KEY and USER_TEST_MINIO_SECRET_KEY are required with USER_TEST_MINIO_ENDPOINT")
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := bucket + "-" + hex.EncodeToString(suffix)
	rawCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		t.Fatalf("load aws config: %v", err)
	}
	raw := s3.NewFromConfig(rawCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	if _, err = raw.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)}); err != nil {
		t.Fatalf("create bucket %s: %v", name, err)
	}
	t.Cleanup(func() {
		listed, e := raw.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String(name)})
		if e == nil {
			for _, object := range listed.Contents {
				_, _ = raw.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(name), Key: object.Key})
			}
		}
		if _, e = raw.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String(name)}); e != nil {
			t.Logf("delete bucket %s: %v", name, e)
		}
	})
	client, err := storage.New(ctx, storage.Config{Endpoint: endpoint, Region: "us-east-1", AccessKeyID: accessKey, SecretAccessKey: secretKey, Bucket: name})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return client
}

// NewClamd returns the host and TCP port of the real clamd named by
// USER_TEST_CLAMAV_ADDR (host:port) for sharedhttp.ScanOptions, or ("", 0) when
// none is configured; callers then skip only the checks that need a clean scan
// (the production scanner fails closed without one). A clamd container is never
// started.
func NewClamd(t *testing.T) (string, int) {
	t.Helper()
	addr := strings.TrimSpace(os.Getenv("USER_TEST_CLAMAV_ADDR"))
	if addr == "" {
		return "", 0
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("USER_TEST_CLAMAV_ADDR: %v", err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		t.Fatalf("USER_TEST_CLAMAV_ADDR: invalid port %q", port)
	}
	return host, n
}
