package queue

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	goredis "github.com/redis/go-redis/v9"
)

// sharedTestRedisURL returns the already running Redis logical database named
// by SHARED_TEST_REDIS_URL, flushed before and after the test; the test fails
// immediately when it is unset or names no database (containers are never
// started).
func sharedTestRedisURL(t *testing.T) string {
	t.Helper()
	uri := os.Getenv("SHARED_TEST_REDIS_URL")
	if uri == "" {
		t.Fatal("SHARED_TEST_REDIS_URL is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers")
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse SHARED_TEST_REDIS_URL: %v", err)
	}
	if db, convErr := strconv.Atoi(strings.Trim(u.Path, "/")); convErr != nil || db < 0 || db > 15 {
		t.Fatal("SHARED_TEST_REDIS_URL must name a logical database 0..15 reserved for these tests (redis://:password@host:port/N)")
	}
	opt, err := goredis.ParseURL(uri)
	if err != nil {
		t.Fatalf("parse SHARED_TEST_REDIS_URL: %v", err)
	}
	flush := func() {
		client := goredis.NewClient(opt)
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.FlushDB(ctx).Err(); err != nil {
			t.Errorf("flush SHARED_TEST_REDIS_URL: %v", err)
		}
	}
	flush()
	t.Cleanup(flush)
	return uri
}

func TestParseRedisConn(t *testing.T) {
	t.Parallel()
	opt, err := ParseRedisConn("redis://localhost:6379/3")
	if err != nil {
		t.Fatalf("ParseRedisConn: %v", err)
	}
	rc, ok := opt.(asynq.RedisClientOpt)
	if !ok {
		t.Fatalf("opt type = %T, want RedisClientOpt", opt)
	}
	if rc.Addr != "localhost:6379" {
		t.Errorf("Addr = %q, want localhost:6379", rc.Addr)
	}
	if rc.DB != 3 {
		t.Errorf("DB = %d, want 3", rc.DB)
	}
}

func TestParseRedisConnBad(t *testing.T) {
	t.Parallel()
	if _, err := ParseRedisConn("http://not-redis"); err == nil {
		t.Fatal("want error for non-redis uri")
	}
}

func TestServerDefaults(t *testing.T) {
	t.Parallel()
	// NewServer with a parseable URL must succeed and not panic; concurrency
	// defaulting is covered by construction (no enqueue needed).
	if _, err := NewServer("redis://localhost:6379/0", ServerOptions{}); err != nil {
		t.Fatalf("NewServer: %v", err)
	}
}

func TestServerWithQueuesAndConcurrency(t *testing.T) {
	t.Parallel()
	srv, err := NewServer("redis://localhost:6379/0", ServerOptions{
		Concurrency: 3,
		Queues:      map[string]int{"holder-enrichment": 6, "ranking-compute": 3},
	})
	if err != nil || srv == nil {
		t.Fatalf("NewServer: %v", err)
	}
}

func TestConstructorsBadURL(t *testing.T) {
	t.Parallel()
	if _, err := NewClient("http://bad"); err == nil {
		t.Error("NewClient bad url should error")
	}
	if _, err := NewInspector("http://bad"); err == nil {
		t.Error("NewInspector bad url should error")
	}
	if _, err := NewServer("http://bad", ServerOptions{}); err == nil {
		t.Error("NewServer bad url should error")
	}
}

func TestEnqueueIntegration(t *testing.T) {
	url := sharedTestRedisURL(t)

	client, err := NewClient(url)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	const qname = "holder-enrichment"
	task := asynq.NewTask("holder:enrich", []byte(`{"pool":"0xabc"}`))
	if _, err := client.Enqueue(task, asynq.Queue(qname)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	insp, err := NewInspector(url)
	if err != nil {
		t.Fatalf("NewInspector: %v", err)
	}
	defer insp.Close()

	qi, err := insp.GetQueueInfo(qname)
	if err != nil {
		t.Fatalf("GetQueueInfo: %v", err)
	}
	if qi.Pending < 1 {
		t.Fatalf("pending = %d, want >= 1", qi.Pending)
	}
}
