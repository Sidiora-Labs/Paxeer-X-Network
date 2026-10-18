package notifications

import (
	"context"
	"time"
)

func (s Store) Retain(ctx context.Context, now time.Time, days, limit int) (int64, error) {
	if days < 1 || days > 90 || limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	res, e := s.DB.ExecContext(ctx, `DELETE FROM discovery_notifications.inbox WHERE id IN (SELECT id FROM discovery_notifications.inbox WHERE created_at<$1 ORDER BY created_at LIMIT $2)`, now.Add(-time.Duration(days)*24*time.Hour), limit)
	if e != nil {
		return 0, e
	}
	return res.RowsAffected()
}
