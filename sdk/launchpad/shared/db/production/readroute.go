package production

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReadClass string

const (
	AuthoritativeRead ReadClass = "authoritative"
	ReadYourWrite     ReadClass = "read_your_write"
	BoundedStaleRead  ReadClass = "bounded_stale"
)

type WALPosition uint64

func ParseWALPosition(value string) (WALPosition, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || len(parts[0]) < 1 || len(parts[0]) > 8 || len(parts[1]) < 1 || len(parts[1]) > 8 {
		return 0, fmt.Errorf("invalid WAL position")
	}
	for _, part := range parts {
		for _, char := range part {
			if !(char >= '0' && char <= '9' || char >= 'A' && char <= 'F' || char >= 'a' && char <= 'f') {
				return 0, fmt.Errorf("invalid WAL position")
			}
		}
	}
	high, err := strconv.ParseUint(parts[0], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid WAL position")
	}
	low, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid WAL position")
	}
	return WALPosition(high<<32 | low), nil
}
func (p WALPosition) String() string {
	return fmt.Sprintf("%X/%X", uint64(p)>>32, uint64(p)&0xffffffff)
}

var ErrReadUnavailable = errors.New("database_read_unavailable")
var errPrimaryRequired = errors.New("primary freshness required")

type AdmittedReadPool struct {
	Pool    *pgxpool.Pool
	Options db.PoolOptions
}
type ReadRequest struct {
	Class            ReadClass
	RequiredPosition WALPosition
}
type ReadObservation struct {
	Class       ReadClass     `json:"class"`
	Target      string        `json:"target"`
	Watermark   string        `json:"watermark"`
	ObservedAt  time.Time     `json:"observedAt"`
	ReplayedAt  *time.Time    `json:"replayedAt"`
	ReplayDelay time.Duration `json:"replayDelay"`
	Stale       bool          `json:"stale"`
	Fallback    bool          `json:"fallback"`
}
type ReadRouteMetric struct {
	Reads, Fallbacks, Unavailable uint64
	ReplayDelay                   time.Duration
}
type readRouteCounters struct {
	reads, fallbacks, unavailable atomic.Uint64
	lag                           atomic.Int64
}
type ReadRouter struct {
	primary, replica AdmittedReadPool
	freshness        time.Duration
	counters         [3]readRouteCounters
}

func NewReadRouter(primary, replica AdmittedReadPool, freshness time.Duration) (*ReadRouter, error) {
	if primary.Pool == nil || primary.Options.Transport == "" || primary.Options.QueryClass != db.Interactive || freshness <= 0 || freshness > time.Minute {
		return nil, fmt.Errorf("admitted primary and finite read freshness required")
	}
	if err := primary.Options.Validate(); err != nil {
		return nil, err
	}
	if err := db.CheckPoolAdmission(primary.Pool, primary.Options); err != nil {
		return nil, err
	}
	if replica.Pool != nil {
		if replica.Pool == primary.Pool || replica.Options.Transport == "" || replica.Options.QueryClass != db.Interactive || replica.Options.BudgetIdentity != primary.Options.BudgetIdentity {
			return nil, fmt.Errorf("separate admitted replica allocation required")
		}
		if err := replica.Options.Validate(); err != nil {
			return nil, err
		}
		if err := db.CheckPoolAdmission(replica.Pool, replica.Options); err != nil {
			return nil, err
		}
	}
	return &ReadRouter{primary: primary, replica: replica, freshness: freshness}, nil
}
func readClassIndex(c ReadClass) (int, error) {
	switch c {
	case AuthoritativeRead:
		return 0, nil
	case ReadYourWrite:
		return 1, nil
	case BoundedStaleRead:
		return 2, nil
	default:
		return 0, fmt.Errorf("unregistered read class")
	}
}
func (r *ReadRouter) Metrics() map[ReadClass]ReadRouteMetric {
	out := map[ReadClass]ReadRouteMetric{}
	if r == nil {
		return out
	}
	for _, class := range []ReadClass{AuthoritativeRead, ReadYourWrite, BoundedStaleRead} {
		i, _ := readClassIndex(class)
		c := &r.counters[i]
		out[class] = ReadRouteMetric{c.reads.Load(), c.fallbacks.Load(), c.unavailable.Load(), time.Duration(c.lag.Load())}
	}
	return out
}
func (r *ReadRouter) WithRead(ctx context.Context, request ReadRequest, read func(pgx.Tx, ReadObservation) error) error {
	if r == nil || ctx == nil || read == nil {
		return fmt.Errorf("complete admitted read required")
	}
	i, err := readClassIndex(request.Class)
	if err != nil {
		return err
	}
	if request.Class == ReadYourWrite && request.RequiredPosition == 0 {
		return fmt.Errorf("read-your-write requires observed committed WAL position")
	}
	counter := &r.counters[i]
	counter.reads.Add(1)
	invoke := func(route AdmittedReadPool, replica, fallback bool) error {
		conn, err := db.AcquireBounded(ctx, route.Pool, route.Options)
		if err != nil {
			return err
		}
		var recovery bool
		var watermark *string
		var replayed *time.Time
		var observed time.Time
		probe, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
		err = conn.QueryRow(probe, `SELECT pg_is_in_recovery(),CASE WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn() ELSE pg_current_wal_lsn() END::text,pg_last_xact_replay_timestamp(),clock_timestamp()`).Scan(&recovery, &watermark, &replayed, &observed)
		cancel()
		conn.Release()
		if err != nil {
			return err
		}
		if recovery != replica {
			return fmt.Errorf("%w: configured database member changed authority", ErrReadUnavailable)
		}
		if watermark == nil {
			return errPrimaryRequired
		}
		position, err := ParseWALPosition(*watermark)
		if err != nil {
			return err
		}
		if replica && request.Class == ReadYourWrite && position < request.RequiredPosition {
			return errPrimaryRequired
		}
		return db.WithTransaction(ctx, route.Pool, route.Options, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
			var memberRecovery bool
			var current string
			if err := tx.QueryRow(ctx, `SELECT pg_is_in_recovery(),CASE WHEN pg_is_in_recovery() THEN pg_last_wal_replay_lsn() ELSE pg_current_wal_lsn() END::text`).Scan(&memberRecovery, &current); err != nil {
				return err
			}
			currentPosition, err := ParseWALPosition(current)
			if err != nil {
				return err
			}
			if memberRecovery != replica || currentPosition < position {
				return fmt.Errorf("%w: database authority changed during read admission", ErrReadUnavailable)
			}
			observation := ReadObservation{Class: request.Class, Target: "primary", Watermark: position.String(), ObservedAt: observed.UTC(), Fallback: fallback}
			if replica {
				observation.Target = "replica"
				observation.ReplayedAt = replayed
				if replayed == nil {
					return errPrimaryRequired
				}
				lag := observed.Sub(*replayed)
				if lag < 0 {
					return fmt.Errorf("%w: replay clock exceeds observation", ErrReadUnavailable)
				}
				observation.ReplayDelay = lag
				observation.Stale = lag > r.freshness
				counter.lag.Store(int64(lag))
				if request.Class == ReadYourWrite && position < request.RequiredPosition {
					return errPrimaryRequired
				}
			}
			if !replica && request.RequiredPosition > position {
				return fmt.Errorf("%w: required WAL belongs to an unavailable primary history", ErrReadUnavailable)
			}
			return read(tx, observation)
		})
	}
	if request.Class != AuthoritativeRead && r.replica.Pool != nil {
		err = invoke(r.replica, true, false)
		if !errors.Is(err, errPrimaryRequired) {
			if err != nil {
				counter.unavailable.Add(1)
				return fmt.Errorf("%w: %w", ErrReadUnavailable, err)
			}
			return nil
		}
		counter.fallbacks.Add(1)
		err = invoke(r.primary, false, true)
	} else {
		err = invoke(r.primary, false, false)
	}
	if err != nil {
		counter.unavailable.Add(1)
		return fmt.Errorf("%w: %w", ErrReadUnavailable, err)
	}
	return nil
}
func (r *ReadRouter) CommittedPosition(ctx context.Context) (WALPosition, error) {
	var position WALPosition
	err := r.WithRead(ctx, ReadRequest{Class: AuthoritativeRead}, func(_ pgx.Tx, o ReadObservation) error {
		var err error
		position, err = ParseWALPosition(o.Watermark)
		return err
	})
	return position, err
}

func (r *ReadRouter) WithOwnershipRead(ctx context.Context, read func(pgx.Tx, ReadObservation) error) error {
	return r.WithRead(ctx, ReadRequest{Class: AuthoritativeRead}, read)
}
func (r *ReadRouter) WithIdempotencyRead(ctx context.Context, read func(pgx.Tx, ReadObservation) error) error {
	return r.WithRead(ctx, ReadRequest{Class: AuthoritativeRead}, read)
}
func (r *ReadRouter) WithPrimaryMutation(ctx context.Context, write func(pgx.Tx) error) error {
	if r == nil || write == nil {
		return fmt.Errorf("primary mutation authority required")
	}
	return db.WithTransaction(ctx, r.primary.Pool, r.primary.Options, pgx.TxOptions{}, func(tx pgx.Tx) error {
		var recovery bool
		if err := tx.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&recovery); err != nil {
			return err
		}
		if recovery {
			return ErrReadUnavailable
		}
		return write(tx)
	})
}
