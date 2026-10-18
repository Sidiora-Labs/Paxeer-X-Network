package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type Publisher interface {
	Publish(context.Context, string, string, json.RawMessage) error
}
type Runtime struct {
	Store     Store
	Publisher Publisher
}

func (r Runtime) PublishBatch(ctx context.Context, limit int) (int, error) {
	if r.Publisher == nil || limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	count := 0
	for count < limit {
		empty := false
		e := r.Store.transaction(ctx, func(tx *sql.Tx) error {
			var id, kind string
			var body []byte
			e := tx.QueryRowContext(ctx, `SELECT id,kind,document FROM narrative_discovery.outbox WHERE NOT published ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(&id, &kind, &body)
			if e == sql.ErrNoRows {
				empty = true
				return nil
			}
			if e != nil {
				return e
			}
			send, cancel := context.WithTimeout(ctx, 5*time.Second)
			e = r.Publisher.Publish(send, kind, id, body)
			cancel()
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `UPDATE narrative_discovery.outbox SET published=true WHERE id=$1`, id)
			return e
		})
		if e != nil {
			return count, e
		}
		if empty {
			break
		}
		count++
	}
	return count, nil
}
func (r Runtime) Run(ctx context.Context) error {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		if _, e := r.PublishBatch(ctx, 100); e != nil {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
func (s Store) ExpireSnapshots(ctx context.Context, now time.Time) error {
	_, e := s.DB.ExecContext(ctx, `DELETE FROM narrative_discovery.snapshots WHERE id IN(SELECT id FROM narrative_discovery.snapshots WHERE expires_at<=$1 LIMIT 1000)`, now)
	return e
}
