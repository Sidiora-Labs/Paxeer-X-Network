package l17

import (
	"context"
	"encoding/json"
	redis "github.com/redis/go-redis/v9"
	"strconv"
	"time"
)

func stringInt(v int) string { return strconv.Itoa(v) }

type RevisionCache struct {
	Client *redis.Client
	TTL    time.Duration
}
type cachedPortfolio struct {
	Response PortfolioResponse `json:"response"`
	Created  time.Time         `json:"created"`
	Expires  time.Time         `json:"expires"`
}

func (c RevisionCache) Get(ctx context.Context, key string, now time.Time) (PortfolioResponse, bool, error) {
	var out cachedPortfolio
	if c.Client == nil {
		return out.Response, false, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	data, e := c.Client.Get(ctx, key).Bytes()
	if e == redis.Nil {
		return out.Response, false, nil
	}
	if e != nil {
		return out.Response, false, e
	}
	if len(data) > 1<<20 {
		return out.Response, false, ErrInvalid
	}
	if e = json.Unmarshal(data, &out); e != nil {
		return out.Response, false, e
	}
	if !now.Before(out.Expires) || now.Before(out.Created) || !out.Created.Before(out.Expires) || out.Expires.Sub(out.Created) > 10*time.Second {
		return PortfolioResponse{}, false, nil
	}
	return out.Response, true, nil
}
func (c RevisionCache) Set(ctx context.Context, key string, value PortfolioResponse, now time.Time) error {
	return c.SetUntil(ctx, key, value, now, now.Add(c.TTL))
}
func (c RevisionCache) SetUntil(ctx context.Context, key string, value PortfolioResponse, now, bound time.Time) error {
	if c.Client == nil {
		return ErrUnavailable
	}
	if c.TTL <= 0 || c.TTL > 10*time.Second {
		return ErrInvalid
	}
	expires := now.Add(c.TTL)
	if bound.Before(expires) {
		expires = bound
	}
	if !now.Before(expires) {
		return ErrRestart
	}
	raw, e := json.Marshal(cachedPortfolio{value, now, expires})
	if e != nil {
		return e
	}
	if len(raw) > 1<<20 {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
	defer cancel()
	return c.Client.Set(ctx, key, raw, expires.Sub(now)).Err()
}
