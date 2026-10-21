package search

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var ErrBusy = errors.New("search admission exhausted")

type PreferenceSnapshot struct {
	AccountID        string   `json:"accountId"`
	Wallet           string   `json:"wallet"`
	Revision         int64    `json:"revision"`
	MutedCreators    []string `json:"mutedCreators"`
	FollowedCreators []string `json:"followedCreators"`
	SavedPools       []string `json:"savedPools"`
}

func (p PreferenceSnapshot) Validate() error {
	if _, e := uuid.Parse(p.AccountID); e != nil || !exactIdentity(p.Wallet) || p.Revision < 1 {
		return ErrInvalid
	}
	for _, list := range [][]string{p.MutedCreators, p.FollowedCreators, p.SavedPools} {
		if len(list) > 1000 {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, a := range list {
			if !exactIdentity(a) || seen[a] {
				return ErrInvalid
			}
			seen[a] = true
		}
	}
	return nil
}

type admissionWindow struct {
	start    time.Time
	requests int
}
type cacheFill struct {
	done    chan struct{}
	waiters int
	page    IntegratedPage
	err     error
}
type Service struct {
	Store      Store
	Redis      *redis.Client
	mu         sync.Mutex
	fills      map[string]*cacheFill
	maximum    int
	admissions map[string]admissionWindow
	signingKey [32]byte
	counters   searchCounters
}

func NewService(store Store, redisClient *redis.Client, maximum int) (*Service, error) {
	if store.Pool == nil || maximum < 1 || maximum > 128 {
		return nil, ErrInvalid
	}
	if redisClient != nil {
		options := redisClient.Options()
		if !options.ContextTimeoutEnabled || options.MaxRetries != 0 {
			return nil, redisBudgetError()
		}
		for _, timeout := range []time.Duration{options.DialTimeout, options.ReadTimeout, options.WriteTimeout, options.PoolTimeout} {
			if timeout <= 0 || timeout > 40*time.Millisecond {
				return nil, redisBudgetError()
			}
		}
	}
	service := &Service{Store: store, Redis: redisClient, fills: map[string]*cacheFill{}, maximum: maximum, admissions: map[string]admissionWindow{}}
	if _, err := rand.Read(service.signingKey[:]); err != nil {
		return nil, err
	}
	return service, nil
}
func redisBudgetError() error {
	return errors.Join(ErrInvalid, errors.New("search Redis requires deadline-aware operations, no retries and at most 40ms timeouts"))
}

func (s *Service) state(ctx context.Context) (string, int64, int64, error) {
	var generation string
	var revision, privacy int64
	if s == nil || s.Store.Pool == nil {
		return "", 0, 0, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	err := s.Store.Pool.QueryRow(bounded, "SELECT generation,revision,privacy_revision FROM stats_search.active_v2 WHERE singleton").Scan(&generation, &revision, &privacy)
	return generation, revision, privacy, unavailable(err)
}
func PublicCacheKey(q Query, r IntegratedRequest, generation string, revision, privacy int64) string {
	raw, _ := json.Marshal([]any{2, integratedBinding(q, r), r.Position, generation, revision, privacy})
	h := sha256.Sum256(raw)
	return "search:v2:" + hex.EncodeToString(h[:])
}
func (s *Service) Public(ctx context.Context, r IntegratedRequest) (IntegratedPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	if s == nil || s.Store.Pool == nil {
		return IntegratedPage{}, ErrUnavailable
	}
	q, err := r.Validate()
	if err != nil {
		return IntegratedPage{}, err
	}
	if r.Kind == "profile" {
		return s.Store.SearchIntegrated(ctx, r)
	}
	s.counters.requests.Add(1)
	g, revision, privacy, err := s.state(ctx)
	if err != nil {
		return IntegratedPage{}, err
	}
	key := s.cacheKey(q, r, g, revision, privacy)
	if s.Redis != nil {
		bounded, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
		raw, e := s.Redis.Get(bounded, key).Bytes()
		cancel()
		if e == nil {
			page, valid := s.decodeCached(key, raw)
			if valid && page.Generation == g && page.Revision == revision && page.PrivacyRevision == privacy {
				ng, nr, np, e := s.state(ctx)
				if e != nil {
					return IntegratedPage{}, e
				}
				if ng == g && nr == revision && np == privacy {
					s.counters.hits.Add(1)
					return page, nil
				}
			}
		}
		if e != nil && !errors.Is(e, redis.Nil) {
			s.counters.redisErrors.Add(1)
		}
	}
	s.counters.misses.Add(1)
	s.mu.Lock()
	if fill := s.fills[key]; fill != nil {
		if fill.waiters >= 8 {
			s.mu.Unlock()
			s.counters.busy.Add(1)
			return IntegratedPage{}, ErrBusy
		}
		s.counters.coalesced.Add(1)
		fill.waiters++
		s.mu.Unlock()
		defer func() { s.mu.Lock(); fill.waiters--; s.mu.Unlock() }()
		select {
		case <-ctx.Done():
			return IntegratedPage{}, unavailable(ctx.Err())
		case <-fill.done:
			if fill.err != nil {
				return IntegratedPage{}, fill.err
			}
			ng, nr, np, e := s.state(ctx)
			if e != nil {
				return IntegratedPage{}, e
			}
			if ng != fill.page.Generation || nr != fill.page.Revision || np != fill.page.PrivacyRevision {
				return IntegratedPage{}, ErrUnavailable
			}
			raw, _ := json.Marshal(fill.page)
			var copy IntegratedPage
			if json.Unmarshal(raw, &copy) != nil {
				return IntegratedPage{}, ErrUnavailable
			}
			return copy, nil
		}
	}
	if len(s.fills) >= s.maximum {
		s.mu.Unlock()
		s.counters.busy.Add(1)
		return IntegratedPage{}, ErrBusy
	}
	fill := &cacheFill{done: make(chan struct{})}
	s.fills[key] = fill
	s.mu.Unlock()
	s.counters.databaseReads.Add(1)
	page, err := s.Store.SearchIntegrated(ctx, r)
	if err != nil {
		s.counters.databaseErrors.Add(1)
	}
	if err == nil {
		ng, nr, np, e := s.state(ctx)
		if e != nil {
			err = e
		} else if ng != page.Generation || nr != page.Revision || np != page.PrivacyRevision {
			err = ErrUnavailable
		}
	}
	if err == nil && s.Redis != nil {
		raw, e := s.encodeCached(key, page)
		if e != nil {
			err = e
		} else {
			ttl := 10 * time.Second
			if q.Kind == "exact_address" && len(page.Items) == 0 {
				ttl = 2 * time.Second
			}
			bounded, cancel := context.WithTimeout(ctx, 40*time.Millisecond)
			if s.Redis.Set(bounded, key, raw, ttl).Err() != nil {
				s.counters.redisErrors.Add(1)
			}
			cancel()
		}
	}
	s.mu.Lock()
	fill.page, fill.err = page, err
	close(fill.done)
	delete(s.fills, key)
	s.mu.Unlock()
	return page, err
}
func (s *Service) Personalize(ctx context.Context, page IntegratedPage, p auth.IdentityPrincipal) (IntegratedPage, error) {
	if s == nil || s.Store.Pool == nil {
		return IntegratedPage{}, ErrUnavailable
	}
	wallet, err := auth.NormalizeWallet(p.Wallet)
	if err != nil || p.AccountID == "" || p.SessionID == "" || p.AuthEpoch < 0 || !p.AccessExpiresAt.After(time.Now()) || p.ProofAt.IsZero() || p.ProofAt.After(time.Now().Add(30*time.Second)) {
		return IntegratedPage{}, auth.ErrScopeDenied
	}
	var raw []byte
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	if err = s.Store.Pool.QueryRow(bounded, "SELECT payload FROM stats_search.preferences_v2 WHERE wallet=$1 AND payload->>'accountId'=$2", wallet, p.AccountID).Scan(&raw); err != nil {
		return IntegratedPage{}, ErrUnavailable
	}
	var preferences PreferenceSnapshot
	if strictJSON(raw, &preferences) != nil || preferences.Validate() != nil || preferences.AccountID != p.AccountID || preferences.Wallet != wallet {
		return IntegratedPage{}, ErrUnavailable
	}
	muted, followed, saved := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, a := range preferences.MutedCreators {
		muted[a] = true
	}
	for _, a := range preferences.FollowedCreators {
		followed[a] = true
	}
	for _, a := range preferences.SavedPools {
		saved[a] = true
	}
	items := []IntegratedHit{}
	for _, hit := range page.Items {
		if muted[hit.Creator] {
			continue
		}
		hit.Followed = followed[hit.Creator]
		if hit.PoolAddress != nil {
			hit.Saved = saved[*hit.PoolAddress]
		}
		items = append(items, hit)
	}
	generation, revision, privacy, err := s.state(bounded)
	if err != nil || generation != page.Generation || revision != page.Revision || privacy != page.PrivacyRevision {
		return IntegratedPage{}, ErrUnavailable
	}
	page.Items = items
	return page, nil
}
func (s *Service) ConsumeInvalidation(ctx context.Context, subject string, wire []byte, message delivery.Delivery, policy delivery.Policy) error {
	if message == nil {
		return ErrInvalid
	}
	event, err := delivery.Decode(wire, policy, subject)
	if err != nil {
		return err
	}
	var payload struct {
		Version  int    `json:"version"`
		Revision string `json:"revision"`
	}
	if strictJSON(event.Payload, &payload) != nil || payload.Version != 2 {
		return ErrInvalid
	}
	notified, err := strconv.ParseInt(payload.Revision, 10, 64)
	if err != nil || notified < 1 || strconv.FormatInt(notified, 10) != payload.Revision {
		return ErrInvalid
	}
	_, current, _, err := s.state(ctx)
	if err != nil {
		return err
	}
	if current < notified {
		return ErrUnavailable
	}
	return message.Ack(ctx)
}

type authenticatedPage struct {
	Payload json.RawMessage `json:"payload"`
	MAC     string          `json:"mac"`
}

func (s *Service) encodeCached(key string, page IntegratedPage) ([]byte, error) {
	raw, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, s.signingKey[:])
	mac.Write([]byte(key))
	mac.Write([]byte{0})
	mac.Write(raw)
	return json.Marshal(authenticatedPage{Payload: raw, MAC: hex.EncodeToString(mac.Sum(nil))})
}
func (s *Service) decodeCached(key string, wire []byte) (IntegratedPage, bool) {
	var envelope authenticatedPage
	var page IntegratedPage
	if len(wire) > 131072 || strictJSON(wire, &envelope) != nil {
		return page, false
	}
	signature, err := hex.DecodeString(envelope.MAC)
	if err != nil {
		return page, false
	}
	mac := hmac.New(sha256.New, s.signingKey[:])
	mac.Write([]byte(key))
	mac.Write([]byte{0})
	mac.Write(envelope.Payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return page, false
	}
	if json.Unmarshal(envelope.Payload, &page) != nil {
		return page, false
	}
	return page, true
}

func (s *Service) Admit(ip string) error {
	if s == nil || len(ip) == 0 || len(ip) > 64 {
		return ErrInvalid
	}
	address := net.ParseIP(ip)
	if address == nil {
		return ErrInvalid
	}
	sum := sha256.Sum256([]byte(address.String()))
	key := hex.EncodeToString(sum[:])
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	window, exists := s.admissions[key]
	if !exists || now.Sub(window.start) >= time.Minute {
		if len(s.admissions) >= 4096 {
			for id, v := range s.admissions {
				if now.Sub(v.start) >= time.Minute {
					delete(s.admissions, id)
				}
			}
			if len(s.admissions) >= 4096 {
				return ErrBusy
			}
		}
		window = admissionWindow{start: now}
	}
	if window.requests >= 120 {
		return ErrBusy
	}
	window.requests++
	s.admissions[key] = window
	return nil
}

func (s *Service) cacheKey(q Query, r IntegratedRequest, generation string, revision, privacy int64) string {
	scope := sha256.Sum256(s.signingKey[:])
	return PublicCacheKey(q, r, generation, revision, privacy) + ":" + hex.EncodeToString(scope[:8])
}

type searchCounters struct {
	requests       atomic.Uint64
	hits           atomic.Uint64
	misses         atomic.Uint64
	coalesced      atomic.Uint64
	databaseReads  atomic.Uint64
	databaseErrors atomic.Uint64
	redisErrors    atomic.Uint64
	busy           atomic.Uint64
}

type CacheMeasurement struct {
	Requests       uint64 `json:"requests"`
	Hits           uint64 `json:"hits"`
	Misses         uint64 `json:"misses"`
	Coalesced      uint64 `json:"coalesced"`
	DatabaseReads  uint64 `json:"databaseReads"`
	DatabaseErrors uint64 `json:"databaseErrors"`
	RedisErrors    uint64 `json:"redisErrors"`
	Busy           uint64 `json:"busy"`
	ActiveFills    int    `json:"activeFills"`
	Waiters        int    `json:"waiters"`
	Goroutines     int    `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heapAllocBytes"`
}

func (s *Service) CacheMeasurement() (CacheMeasurement, error) {
	if s == nil {
		return CacheMeasurement{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	waiters := 0
	for _, fill := range s.fills {
		waiters += fill.waiters
	}
	return CacheMeasurement{Requests: s.counters.requests.Load(), Hits: s.counters.hits.Load(), Misses: s.counters.misses.Load(), Coalesced: s.counters.coalesced.Load(), DatabaseReads: s.counters.databaseReads.Load(), DatabaseErrors: s.counters.databaseErrors.Load(), RedisErrors: s.counters.redisErrors.Load(), Busy: s.counters.busy.Load(), ActiveFills: len(s.fills), Waiters: waiters, Goroutines: runtime.NumGoroutine(), HeapAllocBytes: memory.HeapAlloc}, nil
}
