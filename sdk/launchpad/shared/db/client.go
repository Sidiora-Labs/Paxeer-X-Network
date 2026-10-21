package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Transport string

const (
	TransportDirect            Transport = "direct"
	TransportTransactionPooler Transport = "transaction_pooler"
)

// PoolOptions tunes the pgx connection pool. Zero fields take the TS defaults
// (shared/src/db/client.ts): max 20 conns, 30s idle, 5s connect.
type PoolOptions struct {
	Transport        Transport
	PoolID           string
	BudgetIdentity   string
	DatabaseAlias    string
	DatabaseUser     string
	DatabaseHost     string
	DatabasePort     uint16
	MaxConns         int32
	MinConns         int32
	MaxConnIdleTime  time.Duration
	ConnectTimeout   time.Duration
	StatementTimeout time.Duration
	LockTimeout      time.Duration
	AcquireTimeout   time.Duration
	QueryClass       QueryClass
	Service          string
	Instance         string
}

var (
	registryMu      sync.RWMutex
	registry        = map[string]*pgxpool.Pool{}
	admittedOptions = map[*pgxpool.Pool]PoolOptions{}
)

// NewPool builds a pgxpool from a DATABASE_URL and registers it for metrics.
func NewPool(ctx context.Context, dsn string, opts PoolOptions) (*pgxpool.Pool, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}

	cfg.MaxConns = 20
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if opts.Transport != "" {
		cfg.MinConns = opts.MinConns
	}
	if opts.MinConns > 0 {
		cfg.MinConns = opts.MinConns
	}
	cfg.MaxConnIdleTime = 30 * time.Second
	if opts.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	}
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	if opts.ConnectTimeout > 0 {
		cfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout
	}
	statement, lock := opts.Deadlines()
	if opts.Transport != "" && (cfg.ConnConfig.Database != opts.DatabaseAlias || cfg.ConnConfig.User != opts.DatabaseUser || cfg.ConnConfig.Host != opts.DatabaseHost || cfg.ConnConfig.Port != opts.DatabasePort || len(cfg.ConnConfig.Fallbacks) != 0) {
		return nil, fmt.Errorf("db: connection route differs from admitted database alias/user")
	}
	if opts.Transport == TransportTransactionPooler {
		for key := range cfg.ConnConfig.RuntimeParams {
			if key != "application_name" && key != "client_encoding" {
				return nil, fmt.Errorf("db: unsupported transaction-pooler startup parameter")
			}
		}
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		cfg.ConnConfig.StatementCacheCapacity = 0
		cfg.ConnConfig.DescriptionCacheCapacity = 0
	}
	if lock > 0 && opts.Transport != TransportTransactionPooler {
		cfg.ConnConfig.RuntimeParams["lock_timeout"] = fmt.Sprint(lock.Milliseconds())
	}
	if statement > 0 && opts.Transport != TransportTransactionPooler {
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", statement.Milliseconds())
	}

	if opts.Service != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = PoolApplicationName(opts)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: new pool: %w", err)
	}

	tag := poolTag(dsn)
	if opts.Service != "" {
		tag = opts.Service + "/" + opts.Instance + "/" + string(opts.QueryClass)
	}
	if opts.PoolID != "" {
		tag += "/" + opts.PoolID
	}
	registryMu.Lock()
	if opts.PoolID != "" && registry[tag] != nil {
		registryMu.Unlock()
		pool.Close()
		return nil, fmt.Errorf("db: duplicate registered pool identity")
	}
	registry[tag] = pool
	admittedOptions[pool] = opts
	registryMu.Unlock()
	return pool, nil
}

// poolTag derives a metrics tag from the DSN database name (parity with TS).
func poolTag(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "default"
	}
	tag := strings.TrimPrefix(u.Path, "/")
	if tag == "" {
		return "default"
	}
	return tag
}

// PoolMetric reports live pool gauges for one registered pool (SH-3).
type PoolMetric struct {
	Total                int32
	Idle                 int32
	Acquired             int32
	Constructing         int32
	AcquireCount         int64
	AcquireDuration      time.Duration
	CanceledAcquireCount int64
	MaxConns             int32
	BudgetUtilization    float64
}

// PoolMetrics returns metrics for every registered pool, keyed by DB-name tag.
func PoolMetrics() map[string]PoolMetric {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]PoolMetric, len(registry))
	for tag, p := range registry {
		s := p.Stat()
		out[tag] = PoolMetric{
			Total:                s.TotalConns(),
			Idle:                 s.IdleConns(),
			Acquired:             s.AcquiredConns(),
			Constructing:         s.ConstructingConns(),
			AcquireCount:         s.AcquireCount(),
			AcquireDuration:      s.AcquireDuration(),
			CanceledAcquireCount: s.CanceledAcquireCount(),
			MaxConns:             s.MaxConns(),
			BudgetUtilization:    float64(s.TotalConns()) / float64(s.MaxConns()),
		}
	}
	return out
}

// unregister removes a pool tag (used by tests to keep the registry tidy).
func unregister(tag string) {
	registryMu.Lock()
	delete(admittedOptions, registry[tag])
	delete(registry, tag)
	registryMu.Unlock()
}

// QueryClass isolates independent pools; an empty class preserves legacy defaults.
type QueryClass string

const (
	Interactive QueryClass = "interactive"
	Batch       QueryClass = "batch"
	Migration   QueryClass = "migration"
)

func (o PoolOptions) Validate() error {
	if o.MaxConns < 0 || o.MinConns < 0 || o.MaxConnIdleTime < 0 || o.ConnectTimeout < 0 || o.AcquireTimeout < 0 || o.StatementTimeout < 0 || o.LockTimeout < 0 {
		return fmt.Errorf("db: negative pool option")
	}
	max := o.MaxConns
	if max == 0 {
		max = 20
	}
	if o.MinConns > max {
		return fmt.Errorf("db: minimum connections exceeds maximum")
	}
	if o.QueryClass != "" && o.QueryClass != Interactive && o.QueryClass != Batch && o.QueryClass != Migration {
		return fmt.Errorf("db: invalid query class")
	}
	if o.QueryClass == Interactive && o.StatementTimeout > 350*time.Millisecond {
		return fmt.Errorf("db: interactive statement ceiling exceeds 350ms")
	}
	if (o.StatementTimeout > 0 && o.StatementTimeout < time.Millisecond) || (o.LockTimeout > 0 && o.LockTimeout < time.Millisecond) {
		return fmt.Errorf("db: SQL deadlines require millisecond precision")
	}
	if o.QueryClass == Migration && o.LockTimeout > 2*time.Second {
		return fmt.Errorf("db: migration lock ceiling exceeds 2s")
	}
	if o.Transport != "" && o.Transport != TransportDirect && o.Transport != TransportTransactionPooler {
		return fmt.Errorf("db: invalid connection transport")
	}
	if o.Transport == TransportTransactionPooler && o.QueryClass == Migration {
		return fmt.Errorf("db: migrations require a dedicated direct PostgreSQL connection")
	}
	if o.Transport != "" && (o.MaxConns < 1 || o.Service == "" || o.PoolID == "" || o.BudgetIdentity == "" || o.DatabaseAlias == "" || o.DatabaseUser == "" || o.DatabaseHost == "" || strings.TrimSpace(o.DatabaseHost) != o.DatabaseHost || strings.ContainsAny(o.DatabaseHost, "\r\n\t ") || o.DatabasePort == 0 || o.QueryClass == "") {
		return fmt.Errorf("db: explicit transport requires admitted finite pool configuration")
	}
	for _, label := range []string{o.Service, o.Instance, o.PoolID, o.BudgetIdentity, o.DatabaseAlias, o.DatabaseUser} {
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return fmt.Errorf("db: invalid metric identity")
			}
		}
		if len(label) > 64 {
			return fmt.Errorf("db: metric identity too long")
		}
	}
	if (o.Service == "") != (o.Instance == "") {
		return fmt.Errorf("db: service and instance must be paired")
	}
	return nil
}
func (o PoolOptions) Deadlines() (statement, lock time.Duration) {
	statement, lock = o.StatementTimeout, o.LockTimeout
	if statement == 0 {
		switch o.QueryClass {
		case Interactive:
			statement = 350 * time.Millisecond
		case Batch:
			statement = 30 * time.Second
		case Migration:
			statement = 5 * time.Minute
		}
	}
	if lock == 0 {
		switch o.QueryClass {
		case Interactive:
			lock = 100 * time.Millisecond
		case Batch:
			lock = time.Second
		case Migration:
			lock = 2 * time.Second
		}
	}
	return
}

// AcquireBounded bounds queue time separately from query execution. Callers
// must use this instead of direct pool methods to enforce acquisition ceilings.
func AcquireBounded(ctx context.Context, pool *pgxpool.Pool, opts PoolOptions) (*pgxpool.Conn, error) {
	var err error
	opts, err = poolAdmission(pool, opts)
	if err != nil {
		return nil, err
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	ceiling := opts.AcquireTimeout
	if ceiling == 0 {
		ceiling = 350 * time.Millisecond
	}
	acquireCtx, cancel := context.WithTimeout(ctx, ceiling)
	defer cancel()
	if pool == nil {
		return nil, fmt.Errorf("db: missing PostgreSQL pool")
	}
	return pool.Acquire(acquireCtx)
}

// WithTransaction rolls back on all errors, cancellation and panics. Cleanup
// gets its own bounded context; pgx closes an unsynchronized connection.
// Commit errors are returned unchanged: an unknown commit outcome is not success.
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, opts PoolOptions, txOpts pgx.TxOptions, fn func(pgx.Tx) error) error {
	var err error
	opts, err = poolAdmission(pool, opts)
	if err != nil {
		return err
	}
	conn, err := AcquireBounded(ctx, pool, opts)
	if err != nil {
		return err
	}
	defer conn.Release()
	tx, err := conn.BeginTx(ctx, txOpts)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			raw := conn.Hijack()
			_ = raw.Close(cleanup)
		}
	}()
	if opts.Transport == TransportTransactionPooler {
		statement, lock := opts.Deadlines()
		if _, err = tx.Exec(ctx, "SELECT set_config('statement_timeout', $1, true), set_config('lock_timeout', $2, true)", fmt.Sprint(statement.Milliseconds()), fmt.Sprint(lock.Milliseconds())); err != nil {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func poolAdmission(pool *pgxpool.Pool, supplied PoolOptions) (PoolOptions, error) {
	registryMu.RLock()
	admitted, exists := admittedOptions[pool]
	registryMu.RUnlock()
	if !exists || admitted.Transport == "" {
		return supplied, nil
	}
	if supplied.Transport != admitted.Transport || supplied.BudgetIdentity != admitted.BudgetIdentity || supplied.PoolID != admitted.PoolID || supplied.Service != admitted.Service || supplied.Instance != admitted.Instance || supplied.QueryClass != admitted.QueryClass {
		return PoolOptions{}, fmt.Errorf("db: pool access differs from admitted configuration")
	}
	return admitted, nil
}

func ClosePool(pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	registryMu.Lock()
	for tag, registered := range registry {
		if registered == pool {
			delete(registry, tag)
		}
	}
	delete(admittedOptions, pool)
	registryMu.Unlock()
	pool.Close()
}

func PoolApplicationName(options PoolOptions) string {
	digest := sha256.Sum256([]byte(options.Service + "/" + options.Instance + "/" + string(options.QueryClass) + "/" + options.PoolID))
	return "kindle_" + hex.EncodeToString(digest[:16])
}
func StatementFingerprint(statement string) string {
	digest := sha256.Sum256([]byte(statement))
	return hex.EncodeToString(digest[:])
}

type QueryObservation struct {
	Service, Instance    string
	Class                QueryClass
	Fingerprint, Outcome string
	Elapsed              time.Duration
}

func WithObservedTransaction(ctx context.Context, pool *pgxpool.Pool, options PoolOptions, txOptions pgx.TxOptions, fingerprint string, observe func(QueryObservation), fn func(pgx.Tx) error) error {
	raw, err := hex.DecodeString(fingerprint)
	if err != nil || len(raw) != 32 || fingerprint != strings.ToLower(fingerprint) || observe == nil || fn == nil {
		return fmt.Errorf("reviewed query fingerprint and observer required")
	}
	started := time.Now()
	err = WithTransaction(ctx, pool, options, txOptions, fn)
	outcome := "ok"
	if err != nil {
		outcome = "database_error"
		var postgres *pgconn.PgError
		if errors.As(err, &postgres) {
			switch postgres.Code {
			case "57014":
				outcome = "statement_timeout"
			case "55P03":
				outcome = "lock_timeout"
			}
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			outcome = "canceled"
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			outcome = "deadline"
		}
	}
	observe(QueryObservation{Service: options.Service, Instance: options.Instance, Class: options.QueryClass, Fingerprint: fingerprint, Outcome: outcome, Elapsed: time.Since(started)})
	return err
}

func CheckPoolAdmission(pool *pgxpool.Pool, options PoolOptions) error {
	registryMu.RLock()
	admitted, exists := admittedOptions[pool]
	registryMu.RUnlock()
	if !exists || admitted.Transport == "" {
		return fmt.Errorf("registered production pool authority required")
	}
	_, err := poolAdmission(pool, options)
	return err
}
