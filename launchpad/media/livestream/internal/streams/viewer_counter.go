package streams

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
	"regexp"
	"time"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type ViewerCounter struct {
	client      *redis.Client
	namespace   string
	maxSessions int
}
type ViewerEstimate struct {
	Count    int64
	Stale    bool
	Accepted bool
}

func NewViewerCounter(client *redis.Client, namespace string, max int) (*ViewerCounter, error) {
	if client == nil || !identityPattern.MatchString(namespace) || max < 1 || max > 100000 {
		return nil, ErrInvalid
	}
	return &ViewerCounter{client, namespace, max}, nil
}
func (v *ViewerCounter) keys(stream string) (string, string) {
	prefix := v.namespace + ":viewers:{" + stream + "}"
	return prefix + ":members", prefix + ":generation"
}

// Initialize is an explicit recovery action, not an implicit assertion of zero.
// After a Redis reset, estimates remain marked stale for a full90second window.
func (v *ViewerCounter) Initialize(ctx context.Context, stream string) error {
	if !uuidPattern.MatchString(stream) {
		return ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	members, meta := v.keys(stream)
	return v.client.Eval(ctx, `local n=redis.call('TIME');local ms=n[1]*1000+math.floor(n[2]/1000);if redis.call('EXISTS',KEYS[1])==1 and redis.call('ZSCORE',KEYS[2],'_generation') then return 1 end;redis.call('DEL',KEYS[2]);redis.call('ZADD',KEYS[2],'+inf','_generation');redis.call('PEXPIRE',KEYS[2],3600000);redis.call('SET',KEYS[1],ms+90000,'PX',3600000);return 1`, []string{meta, members}).Err()
}
func (v *ViewerCounter) Heartbeat(ctx context.Context, stream, session string) (ViewerEstimate, error) {
	if !uuidPattern.MatchString(stream) || !uuidPattern.MatchString(session) {
		return ViewerEstimate{Stale: true}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	members, meta := v.keys(stream)
	values, e := v.client.Eval(ctx, `local stale=redis.call('GET',KEYS[2]);if not stale or not redis.call('ZSCORE',KEYS[1],'_generation') then return {-1,1,0} end;local n=redis.call('TIME');local ms=n[1]*1000+math.floor(n[2]/1000);redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ms-90000);local old=redis.call('ZSCORE',KEYS[1],ARGV[1]);local count=redis.call('ZCARD',KEYS[1])-1;local accepted=0;if old then if ms-tonumber(old)>=15000 then redis.call('ZADD',KEYS[1],ms,ARGV[1]);accepted=1 end elseif count<tonumber(ARGV[2]) then redis.call('ZADD',KEYS[1],ms,ARGV[1]);accepted=1;count=count+1 else return {-2,1,0} end;redis.call('PEXPIRE',KEYS[1],3600000);redis.call('PEXPIRE',KEYS[2],3600000);local isstale=0;if ms<tonumber(stale) then isstale=1 end;return {count,isstale,accepted}`, []string{members, meta}, session, v.maxSessions).Int64Slice()
	if e != nil {
		return ViewerEstimate{Stale: true}, fmt.Errorf("viewer estimate unavailable: %w", ErrUnavailable)
	}
	if len(values) != 3 {
		return ViewerEstimate{Stale: true}, ErrUnavailable
	}
	if values[0] == -1 {
		return ViewerEstimate{Stale: true}, ErrUnavailable
	}
	if values[0] == -2 {
		return ViewerEstimate{Stale: true}, ErrQuota
	}
	return ViewerEstimate{values[0], values[1] == 1, values[2] == 1}, nil
}

func (v *ViewerCounter) Estimate(ctx context.Context, stream string) (ViewerEstimate, error) {
	if !uuidPattern.MatchString(stream) {
		return ViewerEstimate{Stale: true}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	members, meta := v.keys(stream)
	values, err := v.client.Eval(ctx, `local stale=redis.call('GET',KEYS[2]);if not stale or not redis.call('ZSCORE',KEYS[1],'_generation') then return {-1,1} end;local n=redis.call('TIME');local ms=n[1]*1000+math.floor(n[2]/1000);redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',ms-90000);local quality=0;if ms<tonumber(stale) then quality=1 end;return {redis.call('ZCARD',KEYS[1])-1,quality}`, []string{members, meta}).Int64Slice()
	if err != nil || len(values) != 2 || values[0] < 0 {
		return ViewerEstimate{Stale: true}, ErrUnavailable
	}
	return ViewerEstimate{Count: values[0], Stale: values[1] == 1}, nil
}
