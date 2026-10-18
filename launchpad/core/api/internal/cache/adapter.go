package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	redisclient "github.com/redis/go-redis/v9"
)

var ErrAdmissionUnavailable = errors.New("cache: admission unavailable")
var ErrProjectionChanged = errors.New("cache: projection generation or revision changed")
var ErrRefreshHeld = errors.New("cache: distributed refresh held")

type LimitError struct{ RetryAfter time.Duration }

func (e *LimitError) Error() string { return "cache: admission allocation exhausted" }

type VerifiedScope struct{ Principal, AuthorizationRevision, NetworkID string }
type ScopeResolver func(*http.Request) (VerifiedScope, error)
type ProjectionRequest struct {
	RouteID, Generation string
	Parameters          url.Values
	Scope               VerifiedScope
}
type Projection struct {
	Generation, Revision, Visibility string
	SourceWatermark                  json.RawMessage
	Body                             json.RawMessage
}
type ProjectionFetcher func(context.Context, ProjectionRequest) (Projection, error)
type Observation struct {
	Route, Class, Event string
	Duration            time.Duration
}
type AdapterStats struct {
	Bytes                                                                  int64
	Entries                                                                int
	Evictions                                                              uint64
	MetadataBytes                                                          int64
	RedisReconstructing                                                    bool
	ActiveReconstructions, CompletedReconstructions, FailedReconstructions int64
}

// PostgresQueryBudget shares one authoritative counter across healthy and outage
// paths. Its table is supplied by the serial migration owner, never auto-created.
type PostgresQueryBudget struct {
	Pool              *pgxpool.Pool
	Schema, Namespace string
	Limit, QueryCost  int64
	Window            time.Duration
}

func (b PostgresQueryBudget) validate() error {
	if b.Pool == nil || !policyName.MatchString(b.Schema) || !policyName.MatchString(b.Namespace) || b.Limit < 1 || b.Limit > 1000000000 || b.QueryCost < 1 || b.QueryCost > b.Limit || b.Window < time.Millisecond || b.Window > 24*time.Hour {
		return ErrAdmissionUnavailable
	}
	return nil
}
func (b PostgresQueryBudget) admit(ctx context.Context, origin string) (sharedredis.Admission, error) {
	return b.admitWithWait(ctx, origin, nil)
}
func (b PostgresQueryBudget) admitWithWait(ctx context.Context, origin string, observe func(time.Duration)) (sharedredis.Admission, error) {
	if e := b.validate(); e != nil {
		return sharedredis.Admission{}, e
	}
	table := pgx.Identifier{b.Schema, "cache_query_budgets_v2"}.Sanitize()
	var used int64
	var remainingMS int64
	query := `WITH clock AS MATERIALIZED (SELECT floor(extract(epoch FROM clock_timestamp())*1000)::bigint AS ms), slot AS (SELECT ms,ms/$3::bigint AS bucket FROM clock), admitted AS (INSERT INTO ` + table + ` (namespace,origin,window_bucket,window_ms,budget_limit,used) SELECT $1,$2,bucket,$3,$4,$5 FROM slot ON CONFLICT(namespace,origin,window_bucket,window_ms) DO UPDATE SET used=cache_query_budgets_v2.used+EXCLUDED.used WHERE cache_query_budgets_v2.used<=EXCLUDED.budget_limit-EXCLUDED.used AND cache_query_budgets_v2.budget_limit=EXCLUDED.budget_limit RETURNING used) SELECT COALESCE((SELECT used FROM admitted),0),$3::bigint-(ms%$3::bigint) FROM slot`
	started := time.Now()
	connection, e := b.Pool.Acquire(ctx)
	if observe != nil {
		observe(time.Since(started))
	}
	if e != nil {
		return sharedredis.Admission{}, e
	}
	defer connection.Release()
	e = connection.QueryRow(ctx, query, b.Namespace, origin, b.Window.Milliseconds(), b.Limit, b.QueryCost).Scan(&used, &remainingMS)
	if e != nil {
		return sharedredis.Admission{}, e
	}
	remaining := b.Limit - used
	if used == 0 {
		remaining = 0
	}
	return sharedredis.Admission{Allowed: used > 0, Remaining: remaining, RetryAfter: time.Duration(remainingMS) * time.Millisecond}, nil
}

type EmergencyAllocation struct {
	AllocationID              string
	ReplicaSlot, ReplicaCount int
	AggregateLimit            int64
	Window                    time.Duration
}

func (e EmergencyAllocation) validate() error {
	if !policyName.MatchString(e.AllocationID) || e.ReplicaCount < 1 || e.ReplicaCount > 10000 || e.ReplicaSlot < 0 || e.ReplicaSlot >= e.ReplicaCount || e.AggregateLimit < 1 || e.AggregateLimit > 1000000000 || e.Window < time.Millisecond || e.Window > time.Hour {
		return errors.New("cache: missing approved emergency allocation")
	}
	return nil
}
func (e EmergencyAllocation) share() int64 {
	n := e.AggregateLimit / int64(e.ReplicaCount)
	if int64(e.ReplicaSlot) < e.AggregateLimit%int64(e.ReplicaCount) {
		n++
	}
	return n
}

type AdapterConfig struct {
	Redis                  *redisclient.Client
	Local                  Config
	Policies               []RoutePolicy
	Namespace              string
	Budget                 PostgresQueryBudget
	Emergency              EmergencyAllocation
	RedisTimeout, LeaseTTL time.Duration
	Observe                func(Observation)
}
type Adapter struct {
	measurements                                                           map[string]*MeasurementSeries
	config                                                                 AdapterConfig
	local                                                                  *Cache
	mu                                                                     sync.RWMutex
	policies                                                               map[string]RoutePolicy
	reconstructing                                                         bool
	emergencyBucket                                                        int64
	emergencyUsed                                                          int64
	activeReconstructions, completedReconstructions, failedReconstructions int64
}

func NewAdapter(c AdapterConfig) (*Adapter, error) {
	if c.Redis == nil || !policyName.MatchString(c.Namespace) || len(c.Policies) == 0 || len(c.Policies) > 128 {
		return nil, errors.New("cache: invalid adapter configuration")
	}
	if !c.Redis.Options().ContextTimeoutEnabled || c.Redis.Options().MaxRetries > 0 {
		return nil, errors.New("cache: bounded Redis client required")
	}
	if e := c.Budget.validate(); e != nil {
		return nil, e
	}
	if e := c.Emergency.validate(); e != nil {
		return nil, e
	}
	if c.RedisTimeout <= 0 || c.RedisTimeout > time.Second || c.LeaseTTL < time.Millisecond || c.LeaseTTL > time.Minute || c.Local.FetchTimeout <= 0 || c.Local.FetchTimeout >= c.LeaseTTL || c.Local.MaxFetches < 1 || c.Local.MaxPerOrigin < 1 || c.Local.MaxBytes < 1 || c.Local.MaxEntryBytes < 1 || c.Local.Capacity < 1 {
		return nil, errors.New("cache: explicit bounded resource allocation required")
	}
	a := &Adapter{config: c, local: NewWithConfig(c.Local), policies: map[string]RoutePolicy{}}
	for _, p := range c.Policies {
		if e := p.validate(); e != nil {
			return nil, e
		}
		if _, ok := a.policies[p.ID]; ok {
			return nil, errors.New("cache: duplicate route")
		}
		copyRules := map[string]ParameterRule{}
		for k, v := range p.Parameters {
			copyRules[k] = v
		}
		p.Parameters = copyRules
		a.policies[p.ID] = p
	}
	return a, nil
}
func (a *Adapter) observe(p RoutePolicy, event string, d time.Duration) {
	if policyName.MatchString(p.ID) && policyName.MatchString(event) && d >= 0 {
		key := p.ID + "\x00" + p.metricClass() + "\x00" + event
		a.mu.Lock()
		if a.measurements == nil {
			a.measurements = make(map[string]*MeasurementSeries)
		}
		series := a.measurements[key]
		if series == nil && len(a.measurements) < 2048 {
			series = &MeasurementSeries{Route: p.ID, Class: p.metricClass(), Event: event}
			a.measurements[key] = series
		}
		if series != nil {
			series.Count++
			bucket := 0
			for bucket < 31 && d > time.Duration(uint64(1)<<bucket)*time.Microsecond {
				bucket++
			}
			series.DurationBuckets[bucket]++
		}
		a.mu.Unlock()
	}
	if a.config.Observe != nil {
		a.config.Observe(Observation{p.ID, p.metricClass(), event, d})
	}
}
func (a *Adapter) policy(id string) (RoutePolicy, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	p, ok := a.policies[id]
	if !ok {
		return p, errors.New("cache: unregistered route")
	}
	return p, nil
}
func (a *Adapter) redisContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.config.RedisTimeout)
}
func (a *Adapter) setReconstructing(v bool) { a.mu.Lock(); a.reconstructing = v; a.mu.Unlock() }
func (a *Adapter) Stats() AdapterStats {
	b, n, e, m := a.local.MemoryStats()
	a.mu.RLock()
	defer a.mu.RUnlock()
	return AdapterStats{b, n, e, m, a.reconstructing || a.activeReconstructions > 0, a.activeReconstructions, a.completedReconstructions, a.failedReconstructions}
}
func (a *Adapter) key(p RoutePolicy, scope VerifiedScope, q url.Values) (string, error) {
	s := sharedredis.KeyScope{RouteVersion: p.ID + ":" + p.Version, Generation: p.Generation, Visibility: p.visibility()}
	if s.Visibility == "private" {
		s.Principal = scope.Principal
		s.AuthorizationRevision = scope.AuthorizationRevision
	}
	key, e := sharedredis.ProjectionKey(s, q)
	return a.config.Namespace + ":" + key, e
}
func (a *Adapter) emergency() sharedredis.Admission {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now().UnixNano()
	window := int64(a.config.Emergency.Window)
	bucket := now / window
	if bucket != a.emergencyBucket {
		a.emergencyBucket = bucket
		a.emergencyUsed = 0
	}
	limit := a.config.Emergency.share()
	if a.emergencyUsed >= limit {
		return sharedredis.Admission{RetryAfter: time.Duration(window - now%window)}
	}
	a.emergencyUsed++
	return sharedredis.Admission{Allowed: true, Remaining: limit - a.emergencyUsed}
}
func (a *Adapter) requestAdmission(ctx context.Context, p RoutePolicy, s VerifiedScope) error {
	identity := s.NetworkID
	if p.Class != PublicSnapshot {
		identity = s.Principal + "\x00" + s.AuthorizationRevision
	}
	if identity == "" || len(identity) > 512 {
		return ErrAdmissionUnavailable
	}
	h := sha256.Sum256([]byte(p.ID + "\x00" + identity))
	key := a.config.Namespace + ":requests:" + p.metricClass() + ":" + hex.EncodeToString(h[:])
	rc, cancel := a.redisContext(ctx)
	ad, e := sharedredis.Admit(rc, a.config.Redis, key, p.RequestLimit, p.RequestWindow)
	cancel()
	if e != nil {
		a.setReconstructing(true)
		if p.Class != PublicSnapshot {
			return ErrAdmissionUnavailable
		}
		ad = a.emergency()
	} else {
		a.setReconstructing(false)
	}
	if !ad.Allowed {
		a.observe(p, "request_rejected", 0)
		return &LimitError{ad.RetryAfter}
	}
	return nil
}
func (a *Adapter) localGet(key string) (sharedredis.PolicySnapshot, bool) {
	var s sharedredis.PolicySnapshot
	a.local.mu.Lock()
	defer a.local.mu.Unlock()
	el, ok := a.local.items[key]
	if !ok {
		return s, false
	}
	entry := el.Value.(*entry)
	if !time.Now().Before(entry.expires) {
		a.local.removeElement(el)
		return s, false
	}
	if json.Unmarshal(entry.body, &s) != nil || s.Validate() != nil {
		a.local.removeElement(el)
		return s, false
	}
	a.local.ll.MoveToFront(el)
	return s, true
}
func (a *Adapter) localPut(key string, s sharedredis.PolicySnapshot) bool {
	if string(s.Body) != "null" {
		s.ContentDigest = sharedredis.PolicySnapshotDigest(s)
		s.ProjectionDigest = sharedredis.PolicyProjectionDigest(s)
	}
	b, e := json.Marshal(s)
	if e != nil {
		return false
	}
	a.local.mu.Lock()
	defer a.local.mu.Unlock()
	if el, ok := a.local.items[key]; ok && time.Now().Before(el.Value.(*entry).expires) {
		var old sharedredis.PolicySnapshot
		if json.Unmarshal(el.Value.(*entry).body, &old) != nil {
			return false
		}
		cmp, e := sharedredis.CompareRevision(old.Revision, s.Revision)
		if e != nil || old.Generation != s.Generation || cmp > 0 || (cmp == 0 && (string(old.Body) == "null" || old.ProjectionDigest != s.ProjectionDigest)) {
			return false
		}
	}
	a.local.storeLocked(key, b, "", s.RetainUntil)
	return true
}
func (a *Adapter) cached(ctx context.Context, p RoutePolicy, key string) (sharedredis.PolicySnapshot, bool, error) {
	local, hasLocal := a.localGet(key)
	rc, cancel := a.redisContext(ctx)
	remote, hasRemote, e := sharedredis.GetPolicySnapshot(rc, a.config.Redis, key)
	cancel()
	if e != nil {
		a.setReconstructing(true)
		return local, hasLocal, e
	}
	a.setReconstructing(false)
	if hasRemote || remote.Revision != "" {
		if remote.Generation != p.Generation || remote.Class != string(p.Class) || remote.Visibility != p.visibility() {
			return remote, false, ErrProjectionChanged
		}
		a.localPut(key, remote)
	}
	// Redis is the shared invalidation fence. On a healthy miss, discard local data
	// after a flush so reconstruction consults the authoritative projection.
	if !hasRemote {
		return remote, false, nil
	}
	current, ok := a.localGet(key)
	if ok {
		return current, string(current.Body) != "null", nil
	}
	return remote, true, nil
}
func (a *Adapter) Read(ctx context.Context, id string, q url.Values, scope VerifiedScope, fetch ProjectionFetcher) (sharedredis.PolicySnapshot, bool, error) {
	p, e := a.policy(id)
	if e != nil {
		return sharedredis.PolicySnapshot{}, false, e
	}
	if p.Class == AccountWrite || p.Class == Administrative {
		return sharedredis.PolicySnapshot{}, false, errors.New("cache: mutations require admission middleware")
	}
	q, e = p.normalize(q)
	if e != nil {
		return sharedredis.PolicySnapshot{}, false, e
	}
	if fetch == nil {
		return sharedredis.PolicySnapshot{}, false, errors.New("cache: missing projection provider")
	}
	if scope.NetworkID == "" || len(scope.NetworkID) > 256 || (p.visibility() == "private" && (scope.Principal == "" || scope.AuthorizationRevision == "")) {
		return sharedredis.PolicySnapshot{}, false, ErrAdmissionUnavailable
	}
	if e = a.requestAdmission(ctx, p, scope); e != nil {
		return sharedredis.PolicySnapshot{}, false, e
	}
	key, e := a.key(p, scope, q)
	if e != nil {
		return sharedredis.PolicySnapshot{}, false, e
	}
	if p.FreshTTL > 0 {
		s, ok, cacheErr := a.cached(ctx, p, key)
		if cacheErr != nil && !errors.Is(cacheErr, context.DeadlineExceeded) && !errors.Is(cacheErr, context.Canceled) {
			a.observe(p, "redis_unavailable", 0)
		}
		if ok && string(s.Body) != "null" {
			if cacheErr != nil && p.Class == PublicSnapshot && time.Now().Before(s.RetainUntil) {
				a.observe(p, "stale", 0)
				a.refresh(ctx, p, key, q, scope, fetch)
				return s, true, nil
			}
			if time.Now().Before(s.FreshUntil) {
				if current, e := a.policy(p.ID); e != nil || current.Generation != p.Generation {
					return sharedredis.PolicySnapshot{}, false, ErrProjectionChanged
				}
				a.observe(p, "hit", 0)
				return s, false, nil
			}
			if p.Class == PublicSnapshot && time.Now().Before(s.RetainUntil) {
				if current, e := a.policy(p.ID); e != nil || current.Generation != p.Generation {
					return sharedredis.PolicySnapshot{}, false, ErrProjectionChanged
				}
				a.observe(p, "stale", 0)
				a.refresh(ctx, p, key, q, scope, fetch)
				return s, true, nil
			}
		}
	}
	s, e := a.fetch(ctx, p, key, q, scope, fetch)
	return s, false, e
}
func (a *Adapter) refresh(ctx context.Context, p RoutePolicy, key string, q url.Values, s VerifiedScope, fetch ProjectionFetcher) {
	e := a.local.StartOrigin(ctx, p.Origin, "refresh:"+key, func(work context.Context) ([]byte, error) {
		snapshot, e := a.origin(work, p, key, q, s, fetch)
		if e != nil {
			return nil, e
		}
		return json.Marshal(snapshot)
	})
	if e != nil {
		a.observe(p, "refresh_rejected", 0)
	}
}
func (a *Adapter) fetch(ctx context.Context, p RoutePolicy, key string, q url.Values, s VerifiedScope, fetch ProjectionFetcher) (sharedredis.PolicySnapshot, error) {
	var snapshot sharedredis.PolicySnapshot
	raw, _, e := a.local.GetOrFetchOrigin(ctx, p.Origin, "refresh:"+key, 0, func(work context.Context) ([]byte, error) {
		v, e := a.origin(work, p, key, q, s, fetch)
		if e != nil {
			return nil, e
		}
		return json.Marshal(v)
	})
	if e != nil {
		return snapshot, e
	}
	e = json.Unmarshal(raw, &snapshot)
	if e != nil {
		return snapshot, e
	}
	current, e := a.policy(p.ID)
	if e != nil || current.Generation != snapshot.Generation {
		return snapshot, ErrProjectionChanged
	}
	if floor, ok := a.localGet(key); ok {
		cmp, e := sharedredis.CompareRevision(floor.Revision, snapshot.Revision)
		if e != nil || cmp > 0 || (cmp == 0 && string(floor.Body) == "null") {
			return snapshot, ErrProjectionChanged
		}
	}
	return snapshot, nil
}
func (a *Adapter) origin(ctx context.Context, p RoutePolicy, key string, q url.Values, scope VerifiedScope, fetch ProjectionFetcher) (sharedredis.PolicySnapshot, error) {
	var empty sharedredis.PolicySnapshot
	reconstructionSucceeded := false
	a.mu.Lock()
	a.activeReconstructions++
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.activeReconstructions--
		if reconstructionSucceeded {
			a.completedReconstructions++
		} else {
			a.failedReconstructions++
		}
		a.mu.Unlock()
	}()
	leaseKey := key + ":lease"
	token := ""
	redisOK := true
	if p.FreshTTL > 0 {
		rc, cancel := a.redisContext(ctx)
		var e error
		token, e = sharedredis.AcquireRefresh(rc, a.config.Redis, leaseKey, a.config.LeaseTTL)
		cancel()
		if e != nil {
			redisOK = false
			a.setReconstructing(true)
			if p.Class != PublicSnapshot {
				return empty, ErrAdmissionUnavailable
			}
		} else if token == "" {
			return empty, ErrRefreshHeld
		}
		if token != "" {
			defer func() {
				rc, cancel := a.redisContext(context.WithoutCancel(ctx))
				defer cancel()
				_, _ = sharedredis.ReleaseRefresh(rc, a.config.Redis, leaseKey, token)
			}()
		}
	}
	if token != "" {
		rc, cancel := a.redisContext(ctx)
		current, found, readErr := sharedredis.GetPolicySnapshot(rc, a.config.Redis, key)
		cancel()
		if readErr != nil {
			return empty, readErr
		}
		if found {
			if current.Generation != p.Generation || current.Class != string(p.Class) || current.Visibility != p.visibility() {
				return empty, ErrProjectionChanged
			}
			if time.Now().Before(current.FreshUntil) {
				policy, policyErr := a.policy(p.ID)
				if policyErr != nil || policy.Generation != p.Generation || !a.localPut(key, current) {
					return empty, ErrProjectionChanged
				}
				reconstructionSucceeded = true
				a.observe(p, "refresh_coalesced", 0)
				return current, nil
			}
		}
	}
	started := time.Now()
	ad, e := a.config.Budget.admitWithWait(ctx, p.Origin, func(wait time.Duration) { a.observe(p, "database_pool_wait", wait) })
	a.observe(p, "database_budget_wait", time.Since(started))
	if e != nil {
		return empty, ErrAdmissionUnavailable
	}
	if !ad.Allowed {
		a.observe(p, "origin_rejected", 0)
		return empty, &LimitError{ad.RetryAfter}
	}
	a.observe(p, "origin", 0)
	requestScope := scope
	if p.Class == PublicSnapshot {
		requestScope.Principal = ""
		requestScope.AuthorizationRevision = ""
	}
	projection, e := fetch(ctx, ProjectionRequest{p.ID, p.Generation, q, requestScope})
	if e != nil {
		return empty, e
	}
	if projection.Generation != p.Generation || projection.Visibility != p.visibility() || len(projection.Body) > 32<<20 || len(projection.SourceWatermark) > 65536 || !json.Valid(projection.Body) || string(projection.Body) == "null" || !json.Valid(projection.SourceWatermark) {
		return empty, ErrProjectionChanged
	}
	if _, e = sharedredis.CompareRevision(projection.Revision, projection.Revision); e != nil {
		return empty, e
	}
	now := time.Now()
	ttl := p.FreshTTL
	if ttl == 0 {
		ttl = time.Nanosecond
	} else {
		ttl, e = sharedredis.JitterTTL(ttl, p.Jitter)
		if e != nil {
			return empty, e
		}
	}
	snapshot := sharedredis.PolicySnapshot{Generation: p.Generation, Revision: projection.Revision, Visibility: p.visibility(), Class: string(p.Class), SourceWatermark: append(json.RawMessage(nil), projection.SourceWatermark...), FetchedAt: now, FreshUntil: now.Add(ttl), RetainUntil: now.Add(ttl + p.StaleTTL), Body: append(json.RawMessage(nil), projection.Body...)}
	snapshot.ContentDigest = sharedredis.PolicySnapshotDigest(snapshot)
	snapshot.ProjectionDigest = sharedredis.PolicyProjectionDigest(snapshot)
	current, e := a.policy(p.ID)
	if e != nil || current.Generation != p.Generation {
		return empty, ErrProjectionChanged
	}
	encoded, encodeErr := json.Marshal(snapshot)
	if encodeErr != nil {
		return empty, encodeErr
	}
	if p.FreshTTL > 0 && len(encoded) <= sharedredis.MaxSnapshotBytes {
		if e = snapshot.Validate(); e != nil {
			return empty, e
		}
		if redisOK {
			rc, cancel := a.redisContext(ctx)
			written, putErr := sharedredis.PutPolicySnapshot(rc, a.config.Redis, key, snapshot, leaseKey, token)
			cancel()
			if putErr != nil {
				return empty, putErr
			}
			if !written {
				return empty, ErrProjectionChanged
			}
		}
		if !a.localPut(key, snapshot) {
			return empty, ErrProjectionChanged
		}
	} else if p.FreshTTL > 0 {
		a.observe(p, "oversize_bypass", 0)
	}
	reconstructionSucceeded = true
	return snapshot, nil
}
func (a *Adapter) Invalidate(ctx context.Context, id string, q url.Values, s VerifiedScope, generation, revision string) error {
	p, e := a.policy(id)
	if e != nil {
		return e
	}
	if generation != p.Generation {
		return nil
	}
	q, e = p.normalize(q)
	if e != nil {
		return e
	}
	if p.FreshTTL == 0 {
		return errors.New("cache: invalidation for uncacheable class")
	}
	if _, e = sharedredis.CompareRevision(revision, revision); e != nil {
		return e
	}
	key, e := a.key(p, s, q)
	if e != nil {
		return e
	}
	now := time.Now()
	floor := sharedredis.PolicySnapshot{Generation: generation, Revision: revision, Visibility: p.visibility(), Class: string(p.Class), SourceWatermark: json.RawMessage(`null`), FetchedAt: now, FreshUntil: now.Add(sharedredis.InvalidationFloorTTL), RetainUntil: now.Add(sharedredis.InvalidationFloorTTL), Body: json.RawMessage(`null`)}
	a.localPut(key, floor)
	rc, cancel := a.redisContext(ctx)
	defer cancel()
	_, e = sharedredis.InvalidatePolicySnapshot(rc, a.config.Redis, key, floor)
	return e
}
func (a *Adapter) Cutover(id, expectedGeneration, nextGeneration string) error {
	if !policyName.MatchString(nextGeneration) || expectedGeneration == nextGeneration {
		return errors.New("cache: invalid generation transition")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.policies[id]
	if !ok || p.Generation != expectedGeneration {
		return ErrProjectionChanged
	}
	p.Generation = nextGeneration
	a.policies[id] = p
	return nil
}
func (a *Adapter) Prewarm(ctx context.Context, id string, keys []url.Values, scope VerifiedScope, fetch ProjectionFetcher) error {
	p, e := a.policy(id)
	if e != nil {
		return e
	}
	if p.Class != PublicSnapshot || len(keys) > 64 {
		return errors.New("cache: prewarm requires bounded public keys")
	}
	for _, q := range keys {
		if _, _, e = a.Read(ctx, id, q, scope, fetch); e != nil {
			return e
		}
	}
	return nil
}
func (a *Adapter) Handler(id string, resolve ScopeResolver, fetch ProjectionFetcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if resolve == nil || len(r.URL.RawQuery) > 4096 {
			http.Error(w, "cache scope unavailable", 503)
			return
		}
		scope, e := resolve(r)
		if e != nil {
			http.Error(w, "authorization unavailable", 403)
			return
		}
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			http.Error(w, "invalid query", 422)
			return
		}
		p, e := a.policy(id)
		if e != nil {
			http.Error(w, "unregistered route", 503)
			return
		}
		if _, e = p.normalize(q); e != nil {
			http.Error(w, "invalid query", 422)
			return
		}
		s, stale, e := a.Read(r.Context(), id, q, scope, fetch)
		if e != nil {
			var limit *LimitError
			if errors.As(e, &limit) {
				ad := sharedredis.Admission{RetryAfter: limit.RetryAfter}
				w.Header().Set("Retry-After", ad.RetryAfterSeconds())
				http.Error(w, "admission exhausted", 429)
			} else {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "projection unavailable", 503)
			}
			return
		}
		w.Header().Set("X-Projection-Generation", s.Generation)
		w.Header().Set("X-Projection-Revision", s.Revision)
		w.Header().Set("X-Cache-Fetched-At", s.FetchedAt.UTC().Format(time.RFC3339Nano))
		w.Header().Set("X-Cache-Fresh-Until", s.FreshUntil.UTC().Format(time.RFC3339Nano))
		w.Header().Set("X-Cache-Retain-Until", s.RetainUntil.UTC().Format(time.RFC3339Nano))
		w.Header().Set("X-Cache-Stale", strconv.FormatBool(stale))
		watermark := sha256.Sum256(s.SourceWatermark)
		w.Header().Set("X-Source-Watermark-Digest", hex.EncodeToString(watermark[:]))
		w.Header().Set("Content-Type", "application/json")
		scopeKey, keyErr := a.key(p, scope, q)
		if keyErr != nil {
			http.Error(w, "projection scope unavailable", 503)
			return
		}
		etag := ETag([]byte(strings.Join([]string{scopeKey, s.Generation, s.Revision, s.Visibility, s.Class, s.ContentDigest}, "\x00")))
		w.Header().Set("ETag", etag)
		if s.Visibility == "public" {
			w.Header().Set("Cache-Control", "public, no-cache")
		} else {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		if p.FreshTTL > 0 && matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(304)
			return
		}
		_, _ = w.Write(s.Body)
	}
}

func (a *Adapter) AdmissionMiddleware(id string, resolve ScopeResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		p, e := a.policy(id)
		if e != nil || resolve == nil || next == nil || (p.Class != AccountWrite && p.Class != Administrative) {
			http.Error(w, "admission unavailable", 503)
			return
		}
		scope, e := resolve(r)
		if e != nil || scope.Principal == "" || scope.AuthorizationRevision == "" {
			http.Error(w, "authorization unavailable", 403)
			return
		}
		if e = a.requestAdmission(r.Context(), p, scope); e != nil {
			var limit *LimitError
			if errors.As(e, &limit) {
				w.Header().Set("Retry-After", (sharedredis.Admission{RetryAfter: limit.RetryAfter}).RetryAfterSeconds())
				http.Error(w, "admission exhausted", 429)
			} else {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "admission unavailable", 503)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AccountingProjectionSource retains the admitted read model's coherent source
// vector. REST owns cursor verification and response construction via these
// callbacks; generated v2 validation runs before any representation is cached.
type AccountingProjectionSource struct {
	Store      *store.AccountingReadModel
	Generation string
	Candles    bool
	Request    func(ProjectionRequest) (store.AccountingPageRequest, error)
	Encode     func(store.AccountingPage) ([]byte, error)
}

func (s AccountingProjectionSource) Fetch(ctx context.Context, request ProjectionRequest) (Projection, error) {
	var result Projection
	if s.Store == nil || s.Request == nil || s.Encode == nil || s.Generation == "" || s.Generation != request.Generation {
		return result, store.ErrAccountingNotReady
	}
	r, e := s.Request(request)
	if e != nil {
		return result, e
	}
	var page store.AccountingPage
	if s.Candles {
		page, e = s.Store.Candles(ctx, r)
	} else {
		page, e = s.Store.Trades(ctx, r)
	}
	if e != nil {
		return result, e
	}
	body, e := s.Encode(page)
	if e != nil {
		return result, e
	}
	name := "TradesResponse"
	if s.Candles {
		name = "CandlesResponse"
	}
	if e = interfaces.ValidateJSON(name, body); e != nil {
		return result, e
	}
	var envelope struct {
		Revision  string          `json:"revision"`
		Watermark json.RawMessage `json:"watermark"`
		Data      struct {
			Items              []json.RawMessage `json:"items"`
			SnapshotID         string            `json:"snapshotId"`
			SeriesModelVersion string            `json:"seriesModelVersion"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Revision != page.Revision || !sameProjectionJSON(envelope.Watermark, page.Watermark) || len(envelope.Data.Items) != len(page.Items) {
		return result, ErrProjectionChanged
	}
	if s.Candles && (envelope.Data.SnapshotID != page.SnapshotID || envelope.Data.SeriesModelVersion != page.SeriesModelVersion) {
		return result, ErrProjectionChanged
	}
	for i := range page.Items {
		if !sameProjectionJSON(envelope.Data.Items[i], page.Items[i]) {
			return result, ErrProjectionChanged
		}
	}
	result = Projection{Generation: s.Generation, Revision: page.Revision, Visibility: "public", SourceWatermark: append(json.RawMessage(nil), page.Watermark...), Body: append(json.RawMessage(nil), body...)}
	return result, nil
}

func sameProjectionJSON(a, b []byte) bool {
	canonical := func(raw []byte) ([]byte, error) {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var value any
		if e := d.Decode(&value); e != nil {
			return nil, e
		}
		return json.Marshal(value)
	}
	left, e := canonical(a)
	if e != nil {
		return false
	}
	right, e := canonical(b)
	return e == nil && bytes.Equal(left, right)
}

type AccountingCodec interface {
	ParseCacheRequest(*http.Request, bool) (store.AccountingPageRequest, error)
	EncodeCachePage(store.AccountingPageRequest, bool, store.AccountingPage, string) ([]byte, error)
}
type CanonicalAllocation struct {
	FreshTTL, StaleTTL, Jitter time.Duration
	RequestLimit               int64
	RequestWindow              time.Duration
}

func CanonicalPolicies(generation, origin string, c CanonicalAllocation) ([]RoutePolicy, error) {
	// The binary cursor lives for at most fifteen minutes. Cache retention stays
	// strictly inside that horizon; incoming cursor expiry is checked on every hit.
	if c.FreshTTL <= 0 || c.FreshTTL+c.StaleTTL+c.Jitter >= 15*time.Minute {
		return nil, errors.New("cache: canonical retention exceeds cursor lifetime")
	}
	common := func() map[string]ParameterRule {
		return map[string]ParameterRule{
			"poolAddress":    {Required: true, MaxBytes: 42, Pattern: "0x[0-9a-f]{40}"},
			"limit":          {Required: true, MaxBytes: 4, Pattern: "[1-9][0-9]*"},
			"cursor":         {MaxBytes: 256, Pattern: "[A-Za-z0-9_-]{196}"},
			"sourceRevision": {Required: true, MaxBytes: 78, Pattern: "unobserved|[1-9][0-9]{0,77}"},
		}
	}
	candles := common()
	rule := candles["limit"]
	rule.MaxInteger = store.MaxCanonicalCandles
	candles["limit"] = rule
	candles["interval"] = ParameterRule{Required: true, MaxBytes: 2, Pattern: "1m|5m|15m|1h|4h|1d|1w"}
	candles["interval"] = ParameterRule{Required: true, MaxBytes: 3, Pattern: "1m|5m|15m|1h|4h|1d|1w"}
	for _, name := range []string{"from", "to"} {
		candles[name] = ParameterRule{Required: true, MaxBytes: 24, Pattern: "[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\\.[0-9]{3}Z"}
	}
	trades := common()
	rule = trades["limit"]
	rule.MaxInteger = store.MaxCanonicalTrades
	trades["limit"] = rule
	trades["side"] = ParameterRule{MaxBytes: 4, Pattern: "buy|sell"}
	out := []RoutePolicy{{ID: "accounting_candles", Version: "v2", Origin: origin, Generation: generation, Class: PublicSnapshot, Parameters: candles, FreshTTL: c.FreshTTL, StaleTTL: c.StaleTTL, Jitter: c.Jitter, RequestLimit: c.RequestLimit, RequestWindow: c.RequestWindow}, {ID: "accounting_trades", Version: "v2", Origin: origin, Generation: generation, Class: PublicSnapshot, Parameters: trades, FreshTTL: c.FreshTTL, StaleTTL: c.StaleTTL, Jitter: c.Jitter, RequestLimit: c.RequestLimit, RequestWindow: c.RequestWindow}}
	for _, p := range out {
		if e := p.validate(); e != nil {
			return nil, e
		}
	}
	return out, nil
}

// PublicNetworkScope reads only the peer address already resolved by sharedhttp's
// trusted-proxy middleware. API keys and forwarded headers cannot choose a key.
func PublicNetworkScope(r *http.Request) (VerifiedScope, error) {
	if r == nil {
		return VerifiedScope{}, ErrAdmissionUnavailable
	}
	host := r.RemoteAddr
	if h, _, e := net.SplitHostPort(host); e == nil {
		host = h
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return VerifiedScope{}, ErrAdmissionUnavailable
	}
	return VerifiedScope{NetworkID: ip.Unmap().String()}, nil
}
func IdentityScope(authority sharedauth.HTTPIdentityAuthority) ScopeResolver {
	return func(r *http.Request) (VerifiedScope, error) {
		network, e := PublicNetworkScope(r)
		if e != nil {
			return network, e
		}
		p, e := authority.AuthenticateRequest(r.Context(), r)
		if e != nil {
			return VerifiedScope{}, e
		}
		network.Principal = p.AccountID + ":" + p.Wallet
		network.AuthorizationRevision = strconv.FormatInt(p.AuthEpoch, 10)
		return network, nil
	}
}

type AccountingBindingConfig struct {
	Adapter                                     *Adapter
	Model                                       *store.AccountingReadModel
	Generation, ProjectionVersion, MarketStream string
	Codec                                       AccountingCodec
	Resolve                                     ScopeResolver
	Error                                       func(http.ResponseWriter, *http.Request, error)
}
type AccountingBindings struct {
	mu     sync.RWMutex
	config AccountingBindingConfig
}

func NewAccountingBindings(c AccountingBindingConfig) (*AccountingBindings, error) {
	if c.Adapter == nil || c.Model == nil || c.Codec == nil || c.Resolve == nil || c.Error == nil || !policyName.MatchString(c.Generation) || !policyName.MatchString(c.MarketStream) || c.ProjectionVersion == "" || len(c.ProjectionVersion) > 64 {
		return nil, store.ErrAccountingNotReady
	}
	for _, id := range []string{"accounting_candles", "accounting_trades"} {
		p, e := c.Adapter.policy(id)
		if e != nil || p.Generation != c.Generation || p.Class != PublicSnapshot {
			return nil, ErrProjectionChanged
		}
	}
	return &AccountingBindings{config: c}, nil
}
func (b *AccountingBindings) Candles(w http.ResponseWriter, r *http.Request) { b.serve(w, r, true) }
func (b *AccountingBindings) Trades(w http.ResponseWriter, r *http.Request)  { b.serve(w, r, false) }
func canonicalParameters(request store.AccountingPageRequest, query url.Values, candles bool) url.Values {
	q := url.Values{"poolAddress": {request.Pool}, "limit": {strconv.Itoa(request.Limit)}}
	if token := query.Get("cursor"); token != "" {
		q.Set("cursor", token)
	}
	if candles {
		q.Set("interval", request.Interval)
		q.Set("from", time.UnixMilli(request.From).UTC().Format("2006-01-02T15:04:05.000Z"))
		q.Set("to", time.UnixMilli(request.To).UTC().Format("2006-01-02T15:04:05.000Z"))
	} else if request.Side != "" {
		q.Set("side", request.Side)
	}
	return q
}
func (b *AccountingBindings) fenceKey(c AccountingBindingConfig, pool string) string {
	hash := sha256.Sum256([]byte(c.Generation + "\x00" + c.ProjectionVersion + "\x00" + pool))
	return c.Adapter.config.Namespace + ":market-fence:" + hex.EncodeToString(hash[:])
}
func (b *AccountingBindings) marketRevision(ctx context.Context, c AccountingBindingConfig, pool string) (string, error) {
	key := b.fenceKey(c, pool)
	local, exists := c.Adapter.localGet(key)
	rc, cancel := c.Adapter.redisContext(ctx)
	remote, _, e := sharedredis.GetPolicySnapshot(rc, c.Adapter.config.Redis, key)
	cancel()
	if e != nil {
		c.Adapter.setReconstructing(true)
		if exists {
			return local.Revision, nil
		}
		return "", nil
	}
	if remote.Revision != "" {
		if remote.Generation != c.Generation || remote.Visibility != "public" || remote.Class != "public_snapshot" {
			return "", ErrProjectionChanged
		}
		c.Adapter.localPut(key, remote)
	}
	if latest, ok := c.Adapter.localGet(key); ok {
		return latest.Revision, nil
	}
	return remote.Revision, nil
}
func (b *AccountingBindings) serve(w http.ResponseWriter, r *http.Request, candles bool) {
	b.mu.RLock()
	c := b.config
	b.mu.RUnlock()
	w.Header().Set("Cache-Control", "no-store")
	request, e := c.Codec.ParseCacheRequest(r, candles)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	query, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	scope, e := c.Resolve(r)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	revision, e := b.marketRevision(r.Context(), c, request.Pool)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	parameters := canonicalParameters(request, query, candles)
	keyRevision := revision
	if keyRevision == "" {
		keyRevision = "unobserved"
	}
	parameters.Set("sourceRevision", keyRevision)
	id := w.Header().Get("X-Request-ID")
	if _, e := uuid.Parse(id); e != nil {
		id = uuid.NewString()
	}
	route := "accounting_trades"
	if candles {
		route = "accounting_candles"
	}
	source := AccountingProjectionSource{Store: c.Model, Generation: c.Generation, Candles: candles, Request: func(p ProjectionRequest) (store.AccountingPageRequest, error) {
		if p.Generation != c.Generation || p.Parameters.Get("poolAddress") != request.Pool {
			return store.AccountingPageRequest{}, ErrProjectionChanged
		}
		return request, nil
	}, Encode: func(page store.AccountingPage) ([]byte, error) {
		return c.Codec.EncodeCachePage(request, candles, page, id)
	}}
	snapshot, stale, e := c.Adapter.Read(r.Context(), route, parameters, scope, source.Fetch)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	currentRevision, e := b.marketRevision(r.Context(), c, request.Pool)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	if request.ExpectedSnapshot == "" && currentRevision != "" {
		cmp, e := sharedredis.CompareRevision(snapshot.Revision, currentRevision)
		if e != nil || cmp < 0 {
			c.Error(w, r, ErrProjectionChanged)
			return
		}
	}
	b.mu.RLock()
	currentGeneration := b.config.Generation
	b.mu.RUnlock()
	if currentGeneration != c.Generation {
		c.Error(w, r, ErrProjectionChanged)
		return
	}
	// Refresh only the response trace. The source snapshot, watermark and cursor
	// remain byte-exact; its representation validator includes this actual body.
	var envelope map[string]json.RawMessage
	if json.Unmarshal(snapshot.Body, &envelope) != nil {
		c.Error(w, r, ErrProjectionChanged)
		return
	}
	trace, _ := json.Marshal(id)
	envelope["requestId"] = trace
	body, e := json.Marshal(envelope)
	if e != nil {
		c.Error(w, r, e)
		return
	}
	name := "TradesResponse"
	if candles {
		name = "CandlesResponse"
	}
	if e = interfaces.ValidateJSON(name, body); e != nil {
		c.Error(w, r, e)
		return
	}
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("X-Projection-Generation", snapshot.Generation)
	w.Header().Set("X-Projection-Revision", snapshot.Revision)
	w.Header().Set("X-Cache-Stale", strconv.FormatBool(stale))
	w.Header().Set("X-Cache-Fetched-At", snapshot.FetchedAt.UTC().Format(time.RFC3339Nano))
	w.Header().Set("X-Cache-Fresh-Until", snapshot.FreshUntil.UTC().Format(time.RFC3339Nano))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("ETag", ETag(append([]byte(snapshot.Generation+"\x00"+snapshot.Revision+"\x00public\x00"), body...)))
	_, _ = w.Write(body)
}

func (b *AccountingBindings) ConsumeMarket(ctx context.Context, message *natsdriver.Message) error {
	if message == nil {
		return errors.New("cache: actual durable market message required")
	}
	b.mu.RLock()
	c := b.config
	b.mu.RUnlock()
	broker, e := message.BrokerPosition()
	if e != nil || broker.Stream != c.MarketStream || broker.Subject != accounting.MarketSubject || broker.EventID != message.EventID || broker.WireSHA256 != delivery.Hash(message.Wire) {
		return delivery.ErrIntegrity
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{accounting.MarketSubject: true}}
	envelope, e := delivery.Decode(message.Wire, policy, message.Subject)
	if e != nil {
		return e
	}
	publication, e := accounting.DecodePublicationV1(envelope.Payload)
	if e != nil {
		return e
	}
	if publication.Fill == nil || publication.Facet != "market" || publication.PublicationID != envelope.EventID || publication.PublicationID != message.EventID || len(publication.Positions) != 1 {
		return delivery.ErrIntegrity
	}
	position := publication.Positions[0]
	if position.ProjectionVersion != c.ProjectionVersion || position.PartitionKey != "125:"+publication.Fill.PoolAddress || envelope.SourceOrder.Height != position.SourceOrder.Height || envelope.SourceOrder.TransactionIndex != position.SourceOrder.TransactionIndex || envelope.SourceOrder.LogIndex != position.SourceOrder.LogIndex {
		return delivery.ErrIntegrity
	}
	now := time.Now()
	watermark, e := json.Marshal(position)
	if e != nil {
		return e
	}
	floor := sharedredis.PolicySnapshot{Generation: c.Generation, Revision: position.Sequence, Visibility: "public", Class: "public_snapshot", SourceWatermark: watermark, FetchedAt: now, FreshUntil: now.Add(sharedredis.InvalidationFloorTTL), RetainUntil: now.Add(sharedredis.InvalidationFloorTTL), Body: json.RawMessage(`null`)}
	key := b.fenceKey(c, publication.Fill.PoolAddress)
	c.Adapter.localPut(key, floor)
	rc, cancel := c.Adapter.redisContext(ctx)
	_, e = sharedredis.InvalidatePolicySnapshot(rc, c.Adapter.config.Redis, key, floor)
	cancel()
	if e != nil {
		return e
	}
	return message.Ack(ctx)
}
func (b *AccountingBindings) Cutover(expectedGeneration, nextGeneration, nextProjectionVersion string, nextModel *store.AccountingReadModel) error {
	if nextModel == nil || !policyName.MatchString(nextGeneration) || expectedGeneration == nextGeneration || nextProjectionVersion == "" || len(nextProjectionVersion) > 64 {
		return ErrProjectionChanged
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.config.Generation != expectedGeneration {
		return ErrProjectionChanged
	}
	a := b.config.Adapter
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range []string{"accounting_candles", "accounting_trades"} {
		p, ok := a.policies[id]
		if !ok || p.Generation != expectedGeneration {
			return ErrProjectionChanged
		}
	}
	for _, id := range []string{"accounting_candles", "accounting_trades"} {
		p := a.policies[id]
		p.Generation = nextGeneration
		a.policies[id] = p
	}
	b.config.Model = nextModel
	b.config.Generation = nextGeneration
	b.config.ProjectionVersion = nextProjectionVersion
	return nil
}

type MeasurementSeries struct {
	Route           string     `json:"route"`
	Class           string     `json:"class"`
	Event           string     `json:"event"`
	Count           uint64     `json:"count"`
	DurationBuckets [32]uint64 `json:"durationBuckets"`
}

type MeasurementCapture struct {
	ReplicaID       string              `json:"replicaId"`
	CandidateDigest string              `json:"candidateDigest"`
	ObservedAt      time.Time           `json:"observedAt"`
	Stats           AdapterStats        `json:"stats"`
	MaxBytes        int64               `json:"maxBytes"`
	Series          []MeasurementSeries `json:"series"`
}

func (a *Adapter) MeasurementSnapshot(replicaID, candidateDigest string) MeasurementCapture {
	capture := MeasurementCapture{ReplicaID: replicaID, CandidateDigest: candidateDigest, ObservedAt: time.Now().UTC(), Stats: a.Stats(), MaxBytes: a.config.Local.MaxBytes, Series: []MeasurementSeries{}}
	a.mu.RLock()
	for _, series := range a.measurements {
		capture.Series = append(capture.Series, *series)
	}
	a.mu.RUnlock()
	return capture
}

func (a *Adapter) MeasurementHandler(replicaID, candidateDigest string, authorize func(*http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		digest, e := hex.DecodeString(candidateDigest)
		if authorize == nil || !policyName.MatchString(replicaID) || e != nil || len(digest) != 32 || strings.ToLower(candidateDigest) != candidateDigest {
			http.Error(w, "measurement authority unavailable", 503)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", 405)
			return
		}
		if authorize(r) != nil {
			http.Error(w, "measurement forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(a.MeasurementSnapshot(replicaID, candidateDigest))
	}
}
