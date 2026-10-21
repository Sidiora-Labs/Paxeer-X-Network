package production

import (
	"context"
	"fmt"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	"github.com/jackc/pgx/v5"
)

// Queryer permits the same exact helper on a pool, connection or transaction.
type Queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// SortRawBalances is a bounded PostgreSQL numeric-ordering fixture helper. It
// binds validated values; neither raw input nor caller SQL identifiers are interpolated.
func SortRawBalances(ctx context.Context, q Queryer, raw []string) ([]string, error) {
	if len(raw) > 10000 {
		return nil, fmt.Errorf("too many balances")
	}
	for _, s := range raw {
		if err := db.ValidateRawUint256(s); err != nil {
			return nil, err
		}
	}
	rows, err := q.Query(ctx, "SELECT amount FROM unnest($1::text[]) AS balances(amount) ORDER BY amount::numeric DESC", raw)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, len(raw))
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
