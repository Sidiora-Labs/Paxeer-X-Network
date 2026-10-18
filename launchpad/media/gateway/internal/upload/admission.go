package upload

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	redis "github.com/redis/go-redis/v9"
	"regexp"
	"sync"
	"time"
)

const releaseTimeout = time.Second

var ErrBusy = errors.New("upload admission denied")
var walletPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

// Admission is shared across gateway replicas; Redis TIME avoids caller clock
// skew. Leases expire after the hard request deadline, including crashed peers.
type Admission struct {
	db     *redis.Client
	ns     string
	global int
}

func NewAdmission(db *redis.Client, ns string, global int) (*Admission, error) {
	if db == nil || !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`).MatchString(ns) || global < 1 || global > 200 {
		return nil, ErrBusy
	}
	return &Admission{db, ns, global}, nil
}
func (a *Admission) Acquire(ctx context.Context, wallet string) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if !walletPattern.MatchString(wallet) {
		return nil, ErrBusy
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return nil, e
	}
	id := hex.EncodeToString(b)
	keys := []string{a.ns + ":upload:global", a.ns + ":upload:active:" + wallet, a.ns + ":upload:rate:" + wallet}
	n, e := a.db.Eval(ctx, `local t=redis.call('TIME');local now=tonumber(t[1])*1000+math.floor(tonumber(t[2])/1000);redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now);redis.call('ZREMRANGEBYSCORE',KEYS[2],'-inf',now);redis.call('ZREMRANGEBYSCORE',KEYS[3],'-inf',now-600000);if redis.call('ZCARD',KEYS[1])>=tonumber(ARGV[2]) or redis.call('ZCARD',KEYS[2])>=2 or redis.call('ZCARD',KEYS[3])>=10 then return 0 end;redis.call('ZADD',KEYS[1],now+31000,ARGV[1]);redis.call('ZADD',KEYS[2],now+31000,ARGV[1]);redis.call('ZADD',KEYS[3],now,ARGV[1]);redis.call('PEXPIRE',KEYS[1],62000);redis.call('PEXPIRE',KEYS[2],62000);redis.call('PEXPIRE',KEYS[3],660000);return 1`, keys, id, a.global).Int()
	if e != nil {
		return nil, e
	}
	if n != 1 {
		return nil, ErrBusy
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), releaseTimeout)
			defer cancel()
			_ = a.db.Eval(ctx, `redis.call('ZREM',KEYS[1],ARGV[1]);redis.call('ZREM',KEYS[2],ARGV[1]);return 1`, keys[:2], id).Err()
		})
	}, nil
}
