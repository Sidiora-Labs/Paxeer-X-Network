// Package queue wraps hibiken/asynq (Redis-backed) for the background job
// system, replacing the Node-specific BullMQ (D3/L12). Producers and consumers
// for a given queue move to Go together. Queue names come from the shared
// constants package.
package queue

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hibiken/asynq"
)

// DefaultConcurrency mirrors the TS BullMQ worker default.
const DefaultConcurrency = 5

// ParseRedisConn turns a redis:// URL into an asynq RedisConnOpt.
func ParseRedisConn(redisURL string) (asynq.RedisConnOpt, error) {
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("queue: parse redis uri: %w", err)
	}
	return opt, nil
}

// NewClient builds an asynq client for enqueuing tasks.
func NewClient(redisURL string) (*asynq.Client, error) {
	opt, err := ParseRedisConn(redisURL)
	if err != nil {
		return nil, err
	}
	return asynq.NewClient(opt), nil
}

// NewInspector builds an asynq inspector (queue introspection / metrics).
func NewInspector(redisURL string) (*asynq.Inspector, error) {
	opt, err := ParseRedisConn(redisURL)
	if err != nil {
		return nil, err
	}
	return asynq.NewInspector(opt), nil
}

// ServerOptions configures NewServer.
type ServerOptions struct {
	// Concurrency caps in-flight tasks. Zero uses DefaultConcurrency.
	Concurrency int
	// Queues maps queue name -> priority weight. Empty uses the default queue.
	Queues map[string]int
}

// NewServer builds an asynq server (the worker) with bounded concurrency
// (backpressure, invariant i11).
func NewServer(redisURL string, opts ServerOptions) (*asynq.Server, error) {
	opt, err := ParseRedisConn(redisURL)
	if err != nil {
		return nil, err
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	cfg := asynq.Config{Concurrency: concurrency}
	if len(opts.Queues) > 0 {
		cfg.Queues = opts.Queues
	}
	return asynq.NewServer(opt, cfg), nil
}

var recoverySchema = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
var recoveryRegion = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

func RenderEpochSQL(schema, template string) (string, error) {
	if !recoverySchema.MatchString(schema) || len(template) == 0 || len(template) > 1<<20 || !strings.Contains(template, "__SCHEMA__") {
		return "", errors.New("queue: invalid recovery SQL configuration")
	}
	return strings.ReplaceAll(template, "__SCHEMA__", pgx.Identifier{schema}.Sanitize()), nil
}

type EpochGuard struct {
	schema string
	epoch  int64
	region string
}

func NewEpochGuard(schema string, epoch int64, region string) (*EpochGuard, error) {
	if !recoverySchema.MatchString(schema) || epoch < 1 || !recoveryRegion.MatchString(region) {
		return nil, errors.New("queue: invalid recovery authority")
	}
	return &EpochGuard{schema: pgx.Identifier{schema}.Sanitize(), epoch: epoch, region: region}, nil
}

func (g *EpochGuard) Assert(ctx context.Context, tx pgx.Tx) error {
	if g == nil || g.schema == "" || g.epoch < 1 || ctx == nil || tx == nil {
		return errors.New("queue: recovery guard and existing transaction required")
	}
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	_, err := tx.Exec(bounded, "SELECT "+g.schema+".assert_epoch($1,$2)", g.epoch, g.region)
	if err != nil {
		return fmt.Errorf("queue: restoration epoch fence rejected: %w", err)
	}
	return nil
}
