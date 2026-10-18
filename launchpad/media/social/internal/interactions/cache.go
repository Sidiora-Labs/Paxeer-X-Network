package interactions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

type FollowStats struct {
	Wallet      string `json:"wallet"`
	Followers   string `json:"followers"`
	Following   string `json:"following"`
	IsFollowing bool   `json:"is_following"`
	Revision    string `json:"revision"`
}
type statsFill struct{ done chan struct{} }
type ViewerCache struct {
	Redis   *redis.Client
	mu      sync.Mutex
	fills   map[string]*statsFill
	slots   chan struct{}
	hits    atomic.Int64
	origins atomic.Int64
	waiters atomic.Int64
	active  atomic.Int64
	peak    atomic.Int64
}

type ViewerCacheMetrics struct {
	Hits             int64 `json:"hits"`
	OriginFills      int64 `json:"origin_fills"`
	CoalescedWaiters int64 `json:"coalesced_waiters"`
	ActiveFills      int64 `json:"active_fills"`
	PeakFills        int64 `json:"peak_fills"`
}

func (c *ViewerCache) Metrics() ViewerCacheMetrics {
	return ViewerCacheMetrics{c.hits.Load(), c.origins.Load(), c.waiters.Load(), c.active.Load(), c.peak.Load()}
}

func NewViewerCache(client *redis.Client) *ViewerCache {
	return &ViewerCache{Redis: client, fills: map[string]*statsFill{}, slots: make(chan struct{}, 32)}
}
func CacheKey(wallet, viewer, scope string, revision int64) string {
	sum := sha256.Sum256([]byte(wallet + "\x00" + viewer + "\x00" + scope + "\x00" + strconv.FormatInt(revision, 10)))
	return "social:v2:" + hex.EncodeToString(sum[:])
}
func (s Store) FollowStats(ctx context.Context, wallet, viewer string, cache *ViewerCache) (FollowStats, string, error) {
	var out FollowStats
	wallet, e := Wallet(wallet)
	if e != nil {
		return out, "", e
	}
	if viewer != "" {
		viewer, e = Wallet(viewer)
		if e != nil {
			return out, "", e
		}
	}
	if s.DB == nil {
		return out, "", ErrDependency
	}
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, "", e
	}
	defer rollback(tx)
	var erased bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM interaction_erased_wallets WHERE wallet=$1 OR wallet=NULLIF($2,''))`, wallet, viewer).Scan(&erased); e != nil {
		return out, "", e
	}
	if erased {
		return out, "", ErrDenied
	}
	var revision int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM interaction_visibility_revision WHERE scope=$1),0)`, "wallet:"+wallet).Scan(&revision); e != nil {
		return out, "", e
	}
	var overlay int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM interaction_visibility_revision WHERE scope='erasure_overlay'),0)`).Scan(&overlay); e != nil {
		return out, "", e
	}
	key := CacheKey(wallet, viewer, "public_profile:"+strconv.FormatInt(overlay, 10), revision)
	etag := fmt.Sprintf(`"social-%s"`, key[len("social:v2:"):])
	if cache != nil && cache.Redis != nil {
		raw, err := cache.Redis.Get(ctx, key).Bytes()
		if err == nil && json.Unmarshal(raw, &out) == nil && out.Wallet == wallet && out.Revision == strconv.FormatInt(revision, 10) {
			cache.hits.Add(1)
			return out, etag, nil
		}
	}
	if cache != nil {
		cache.mu.Lock()
		if cache.fills == nil {
			cache.fills = map[string]*statsFill{}
		}
		if cache.slots == nil {
			cache.slots = make(chan struct{}, 32)
		}
		if f := cache.fills[key]; f != nil {
			cache.waiters.Add(1)
			cache.mu.Unlock()
			rollback(tx)
			select {
			case <-ctx.Done():
				return out, "", ctx.Err()
			case <-f.done:
			}
			return s.FollowStats(ctx, wallet, viewer, cache)
		}
		f := &statsFill{done: make(chan struct{})}
		cache.fills[key] = f
		cache.mu.Unlock()
		defer func() { cache.mu.Lock(); delete(cache.fills, key); close(f.done); cache.mu.Unlock() }()
		select {
		case cache.slots <- struct{}{}:
			active := cache.active.Add(1)
			for {
				old := cache.peak.Load()
				if active <= old || cache.peak.CompareAndSwap(old, active) {
					break
				}
			}
			defer func() { cache.active.Add(-1); <-cache.slots }()
		case <-ctx.Done():
			return out, "", ctx.Err()
		}
	}
	if cache != nil {
		cache.origins.Add(1)
	}
	var followers, following int64
	e = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM interaction_follows f WHERE f.followee=$1 AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=f.follower)),(SELECT count(*) FROM interaction_follows f WHERE f.follower=$1 AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=f.followee)),EXISTS(SELECT 1 FROM interaction_follows WHERE follower=$2 AND followee=$1)`, wallet, viewer).Scan(&followers, &following, &out.IsFollowing)
	if e != nil {
		return out, "", e
	}
	out.Wallet = wallet
	out.Followers = strconv.FormatInt(followers, 10)
	out.Following = strconv.FormatInt(following, 10)
	out.Revision = strconv.FormatInt(revision, 10)
	if cache != nil && cache.Redis != nil {
		if raw, err := json.Marshal(out); err == nil {
			_ = cache.Redis.Set(ctx, key, raw, 5*time.Second).Err()
		}
	}
	return out, etag, nil
}
