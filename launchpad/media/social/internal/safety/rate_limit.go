package safety

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/redis/go-redis/v9"
	"strconv"
	"sync"
	"time"
)

type RateLimited struct {
	RetryAfterSeconds int64 `json:"retry_after_seconds"`
}

func (e RateLimited) Error() string { return "social rate limit exceeded" }

type AbuseLimiter struct {
	Redis     redis.UniversalClient
	Namespace string
	mu        sync.Mutex
	local     map[string]time.Time
}

var abuseScript = redis.NewScript(`
local clock=redis.call('TIME'); local now=clock[1]*1000+math.floor(clock[2]/1000)
local retry=0
for i,key in ipairs(KEYS) do
 local limit=tonumber(ARGV[(i-1)*2+1]);local span=tonumber(ARGV[(i-1)*2+2])
 local v=redis.call('HMGET',key,'tokens','at');local tokens=tonumber(v[1]) or limit;local at=tonumber(v[2]) or now
 tokens=math.min(limit,tokens+math.max(0,now-at)*limit/span)
 if tokens<1 then retry=math.max(retry,math.ceil((1-tokens)*span/limit)) end
end
if retry>0 then return math.max(1,math.ceil(retry/1000)) end
for i,key in ipairs(KEYS) do
 local limit=tonumber(ARGV[(i-1)*2+1]);local span=tonumber(ARGV[(i-1)*2+2]);local v=redis.call('HMGET',key,'tokens','at')
 local tokens=math.min(limit,(tonumber(v[1]) or limit)+math.max(0,now-(tonumber(v[2]) or now))*limit/span)-1
 redis.call('HSET',key,'tokens',tokens,'at',now);redis.call('PEXPIRE',key,span*2)
end
return 0`)

func (l *AbuseLimiter) Take(ctx context.Context, wallet, kind, resource string, slowSeconds int) error {
	if !walletRE.MatchString(wallet) || !Bounded(l.Namespace, 128) || !Bounded(resource, 256) || slowSeconds < 0 || slowSeconds > 60 {
		return ErrInvalid
	}
	digest := sha256.Sum256([]byte(wallet))
	tag := hex.EncodeToString(digest[:16])
	prefix := l.Namespace + ":{" + tag + "}:"
	r := sha256.Sum256([]byte(resource))
	suffix := hex.EncodeToString(r[:16])
	keys := []string{}
	args := []any{}
	add := func(key string, limit int, span time.Duration) {
		keys = append(keys, prefix+key)
		args = append(args, limit, span.Milliseconds())
	}
	switch kind {
	case "pool":
		add("room:"+suffix, 10, time.Minute)
		add("global", 60, time.Minute)
	case "dm":
		add("dm", 20, time.Minute)
	case "dm_new":
		add("dm", 20, time.Minute)
		add("recipients", 5, time.Hour)
	case "comment":
		add("comments", 10, time.Minute)
	default:
		return ErrInvalid
	}
	if slowSeconds > 0 {
		add("slow:"+suffix, 1, time.Duration(slowSeconds)*time.Second)
	}
	if l.Redis == nil {
		return l.unavailable(wallet)
	}
	retry, e := abuseScript.Run(ctx, l.Redis, keys, args...).Int64()
	if e != nil {
		return l.unavailable(wallet)
	}
	if retry > 0 {
		return RateLimited{RetryAfterSeconds: retry}
	}
	return nil
}
func (l *AbuseLimiter) unavailable(wallet string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.local == nil {
		l.local = make(map[string]time.Time)
	}
	now := time.Now()
	if until := l.local[wallet]; until.After(now) {
		return RateLimited{RetryAfterSeconds: int64(time.Until(until).Seconds()) + 1}
	}
	if len(l.local) >= 10000 {
		for key, until := range l.local {
			if !until.After(now) {
				delete(l.local, key)
			}
		}
		if len(l.local) >= 10000 {
			return ErrUnavailable
		}
	}
	l.local[wallet] = now.Add(time.Minute)
	return ErrUnavailable
}
func RateLimitStatus(e error) (int, string) {
	var r RateLimited
	if errors.As(e, &r) {
		return 429, strconv.FormatInt(r.RetryAfterSeconds, 10)
	}
	return HTTPStatus(e), ""
}
