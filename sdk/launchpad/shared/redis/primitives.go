package redis

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"strconv"
	"time"

	r "github.com/redis/go-redis/v9"
)

// KeyScope must be populated from verified authorization, never request headers.
type KeyScope struct{ RouteVersion, Generation, Visibility, Principal, AuthorizationRevision string }

func ProjectionKey(scope KeyScope, parameters url.Values) (string, error) {
	if scope.RouteVersion == "" || scope.Generation == "" || len(scope.RouteVersion) > 128 || len(scope.Generation) > 128 {
		return "", errors.New("cache: invalid route or generation")
	}
	if scope.Visibility != "public" && scope.Visibility != "private" {
		return "", errors.New("cache: invalid visibility")
	}
	if scope.Visibility == "private" && (scope.Principal == "" || scope.AuthorizationRevision == "") {
		return "", errors.New("cache: missing private authorization scope")
	}
	if scope.Visibility == "public" && (scope.Principal != "" || scope.AuthorizationRevision != "") {
		return "", errors.New("cache: public key contains private scope")
	}
	if len(scope.Principal) > 256 || len(scope.AuthorizationRevision) > 78 || len(parameters) > 32 {
		return "", errors.New("cache: scope too large")
	}
	for k, vs := range parameters {
		if len(k) > 64 || len(vs) != 1 || len(vs[0]) > 256 {
			return "", errors.New("cache: malformed parameters")
		}
	}
	encoded, _ := json.Marshal(struct {
		Scope      KeyScope
		Parameters string
	}{scope, parameters.Encode()})
	sum := sha256.Sum256(encoded)
	return "cache:v1:" + scope.Visibility + ":" + hex.EncodeToString(sum[:]), nil
}

// Snapshot is a disposable projection. Business truth and security decisions remain durable.
type Snapshot struct {
	Generation      string          `json:"generation"`
	Revision        string          `json:"revision"`
	SourceWatermark json.RawMessage `json:"sourceWatermark"`
	FetchedAt       time.Time       `json:"fetchedAt"`
	ExpiresAt       time.Time       `json:"expiresAt"`
	Body            json.RawMessage `json:"body"`
}

func revisionOK(s string) bool {
	if len(s) == 0 || len(s) > 78 || s[0] == '0' {
		return false
	}
	n, ok := new(big.Int).SetString(s, 10)
	return ok && n.Sign() > 0 && n.BitLen() <= 256 && n.String() == s
}

const compareSnapshot = `local old=redis.call('GET',KEYS[1]);if old then local o=cjson.decode(old);if o.generation~=ARGV[2] then return -1 end;local a=o.revision;local b=ARGV[3];if #a>#b or (#a==#b and a>=b) then return 0 end end;redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[4]);return 1`

func PutSnapshot(ctx context.Context, client *r.Client, key string, s Snapshot) (bool, error) {
	if key == "" || len(key) > 512 {
		return false, errors.New("cache: invalid key")
	}
	ttl := time.Until(s.ExpiresAt)
	if s.Generation == "" || len(s.Generation) > 128 || !revisionOK(s.Revision) || s.FetchedAt.IsZero() || !s.ExpiresAt.After(s.FetchedAt) || ttl < time.Millisecond || ttl > 24*time.Hour || len(s.Body) > MaxSnapshotBytes || len(s.SourceWatermark) > MaxSnapshotBytes || !json.Valid(s.Body) || !json.Valid(s.SourceWatermark) {
		return false, errors.New("cache: invalid snapshot")
	}
	b, err := json.Marshal(s)
	if err != nil {
		return false, err
	}
	if len(b) > MaxSnapshotBytes {
		return false, errors.New("cache: snapshot exceeds encoded entry bound")
	}
	v, err := client.Eval(ctx, compareSnapshot, []string{key}, b, s.Generation, s.Revision, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	if v < 0 {
		return false, errors.New("cache: generation mismatch; use generation-scoped keys")
	}
	return v == 1, nil
}

// InvalidationFloorTTL bounds absent-key revision floors. Writers must not replay
// snapshots older than this recovery horizon without authoritative reconciliation.
const InvalidationFloorTTL = 24 * time.Hour
const MaxSnapshotBytes = 1 << 20
const invalidateSnapshot = `local old=redis.call('GET',KEYS[1]);if not old then redis.call('SET',KEYS[1],ARGV[3],'PX',ARGV[4]);return 1 end;local o=cjson.decode(old);local a=o.revision;local b=ARGV[2];if o.generation==ARGV[1] and (#b>#a or (#b==#a and b>a)) then local ttl=redis.call('PTTL',KEYS[1]);if ttl<=0 then return 0 end;o.revision=b;o.body=cjson.null;redis.call('SET',KEYS[1],cjson.encode(o),'PX',ttl);return 1 end;return 0`

func InvalidateSnapshot(ctx context.Context, c *r.Client, key, generation, revision string) (bool, error) {
	if !revisionOK(revision) || generation == "" || len(generation) > 128 || key == "" || len(key) > 512 {
		return false, errors.New("cache: invalid revision")
	}
	now := time.Now()
	floor, _ := json.Marshal(Snapshot{Generation: generation, Revision: revision, SourceWatermark: json.RawMessage(`null`), FetchedAt: now, ExpiresAt: now.Add(InvalidationFloorTTL), Body: json.RawMessage(`null`)})
	n, e := c.Eval(ctx, invalidateSnapshot, []string{key}, generation, revision, floor, InvalidationFloorTTL.Milliseconds()).Int()
	return n == 1, e
}

type Admission struct {
	Allowed    bool
	Remaining  int64
	RetryAfter time.Duration
}

func (a Admission) RetryAfterSeconds() string {
	n := (a.RetryAfter + time.Second - 1) / time.Second
	if n < 1 {
		n = 1
	}
	return strconv.FormatInt(int64(n), 10)
}

// Admit uses an atomic fixed window anchored to its first admitted request.
// Redis errors return no admission: callers must fail closed (503), never unlimited.
func Admit(ctx context.Context, c *r.Client, key string, limit int64, window time.Duration) (Admission, error) {
	if key == "" || len(key) > 512 || limit < 1 || limit > 1000000000 || window < time.Millisecond || window > 24*time.Hour {
		return Admission{}, errors.New("admission: invalid bucket")
	}
	v, err := c.Eval(ctx, `local n=tonumber(redis.call('GET',KEYS[1]) or '0');if n>=tonumber(ARGV[1]) then return {0,0,redis.call('PTTL',KEYS[1])} end;n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('PEXPIRE',KEYS[1],ARGV[2]) end;return {1,tonumber(ARGV[1])-n,0}`, []string{key}, limit, window.Milliseconds()).Int64Slice()
	if err != nil {
		return Admission{}, err
	}
	if len(v) != 3 || v[2] < 0 {
		return Admission{}, errors.New("admission: invalid counter state")
	}
	return Admission{v[0] == 1, v[1], time.Duration(v[2]) * time.Millisecond}, nil
}

// AcquireRefresh returns an unguessable ownership token. Empty token means held.
func AcquireRefresh(ctx context.Context, c *r.Client, key string, ttl time.Duration) (string, error) {
	if key == "" || len(key) > 512 || ttl < time.Millisecond || ttl > time.Minute {
		return "", errors.New("cache: invalid refresh lease")
	}
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	token := hex.EncodeToString(b)
	ok, e := c.SetNX(ctx, key, token, ttl).Result()
	if !ok {
		return "", e
	}
	return token, e
}
func ReleaseRefresh(ctx context.Context, c *r.Client, key, token string) (bool, error) {
	if len(token) != 64 {
		return false, errors.New("cache: invalid lease token")
	}
	n, e := c.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end;return 0`, []string{key}, token).Int()
	return n == 1, e
}

// JitterTTL adds uniformly random bounded jitter; no global random seed or overflow.
func JitterTTL(base, spread time.Duration) (time.Duration, error) {
	if base <= 0 || spread < 0 || spread > base/2 || base > 24*time.Hour {
		return 0, errors.New("cache: invalid jitter bounds")
	}
	if spread == 0 {
		return base, nil
	}
	n, e := rand.Int(rand.Reader, big.NewInt(int64(2*spread)+1))
	if e != nil {
		return 0, e
	}
	return base - spread + time.Duration(n.Int64()), nil
}

// GetSnapshot never serves expired values or stale authorization decisions.
func GetSnapshot(ctx context.Context, c *r.Client, key string) (Snapshot, bool, error) {
	var s Snapshot
	b, e := c.Get(ctx, key).Bytes()
	if errors.Is(e, r.Nil) {
		return s, false, nil
	}
	if e != nil {
		return s, false, e
	}
	if len(b) > MaxSnapshotBytes {
		return s, false, errors.New("cache: oversized snapshot")
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return s, false, e
	}
	if !revisionOK(s.Revision) || s.Generation == "" {
		return s, false, errors.New("cache: corrupt snapshot")
	}
	return s, time.Now().Before(s.ExpiresAt) && string(s.Body) != "null", nil
}
