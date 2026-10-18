package streams

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type pageCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
	Pool      string    `json:"pool"`
	Live      bool      `json:"live"`
}

func (s *LifecycleStore) Page(ctx context.Context, pool string, live bool, cursor, limitText string) ([]Operation, *string, error) {
	if pool != "" && !addressPattern.MatchString(pool) {
		return nil, nil, ErrInvalid
	}
	pool = strings.ToLower(pool)
	limit := 20
	if limitText != "" {
		var err error
		limit, err = strconv.Atoi(limitText)
		if err != nil || limit < 1 || limit > 100 {
			return nil, nil, ErrInvalid
		}
	}
	var c pageCursor
	if cursor != "" {
		if len(cursor) > 1024 {
			return nil, nil, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &c) != nil || !frozenUUID.MatchString(c.ID) || c.CreatedAt.IsZero() || c.Pool != pool || c.Live != live {
			return nil, nil, ErrInvalid
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := s.pool.Query(bounded, fmt.Sprintf("SELECT "+opColumns+",created_at FROM %s.stream_operations WHERE ($1='' OR pool=$1) AND (NOT $2 OR state='LIVE') AND (($1<>'' AND NOT $2) OR state<>'SUSPENDED') AND ($3::timestamptz IS NULL OR (created_at,id)<($3,$4)) ORDER BY created_at DESC,id DESC LIMIT $5", s.schema), pool, live, nullableCursorTime(c), c.ID, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	result := []Operation{}
	var next *string
	hasMore := false
	for rows.Next() {
		var o Operation
		var created time.Time
		if err = rows.Scan(&o.ID, &o.Wallet, &o.Pool, &o.Title, &o.State, &o.Intent, &o.ProviderID, &o.Revision, &o.ProviderTime, &o.RetryAt, &created); err != nil {
			return nil, nil, err
		}
		if len(result) == limit {
			hasMore = true
			break
		}
		result = append(result, o)
		raw, e := json.Marshal(pageCursor{created, o.ID, pool, live})
		if e != nil {
			return nil, nil, e
		}
		encoded := base64.RawURLEncoding.EncodeToString(raw)
		next = &encoded
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	if !hasMore {
		next = nil
	}
	return result, next, nil
}
func nullableCursorTime(c pageCursor) any {
	if c.ID == "" {
		return nil
	}
	return c.CreatedAt
}
