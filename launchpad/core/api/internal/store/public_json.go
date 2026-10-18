package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// Redis checks byte bounds atomically before transferring a value. Corruption
// fails closed; unavailable Redis may still use the bounded PostgreSQL path.
const boundedJSONRead = `
local size=redis.call('STRLEN',KEYS[1])
if size>tonumber(ARGV[1]) then return redis.error_reply('PUBLIC_JSON_TOO_LARGE') end
return redis.call('GET',KEYS[1])`
const boundedJSONBatch = `
local out={}
local total=2
for i,key in ipairs(KEYS) do
 local size=redis.call('STRLEN',key)
 total=total+size+string.len(key)+6
 if total>tonumber(ARGV[1]) then return redis.error_reply('PUBLIC_JSON_TOO_LARGE') end
 local value=redis.call('GET',key)
 if value=='' then return redis.error_reply('PUBLIC_JSON_INVALID') end
 out[i]=value or ''
end
return out`

func cacheIntegrityError(err error) error {
	if err != nil && (strings.Contains(err.Error(), "PUBLIC_JSON_TOO_LARGE") || strings.Contains(err.Error(), "PUBLIC_JSON_INVALID")) {
		return ErrDataIntegrity
	}
	return err
}
func (s *Store) publicJSON(ctx context.Context, key string) (json.RawMessage, bool, error) {
	if s == nil || s.rdb == nil {
		return nil, false, ErrAccountingNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	v, e := s.rdb.WithTimeout(75*time.Millisecond).Eval(ctx, boundedJSONRead, []string{key}, PublicJSONMaxBytes).Text()
	if errors.Is(e, redis.Nil) {
		return nil, false, nil
	}
	if e != nil {
		return nil, false, cacheIntegrityError(e)
	}
	if e = ValidatePublicJSON([]byte(v)); e != nil {
		return nil, false, e
	}
	return json.RawMessage(v), true, nil
}
func (s *Store) publicJSONBatch(ctx context.Context, keys []string) ([]string, error) {
	if s == nil || s.rdb == nil {
		return nil, ErrAccountingNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	values, e := s.rdb.WithTimeout(75*time.Millisecond).Eval(ctx, boundedJSONBatch, keys, PublicJSONMaxBytes).StringSlice()
	if e != nil {
		return nil, cacheIntegrityError(e)
	}
	if len(values) != len(keys) {
		return nil, ErrDataIntegrity
	}
	return values, nil
}
