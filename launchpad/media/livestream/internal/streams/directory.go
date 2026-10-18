package streams

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	SortViewers   = "viewers"
	SortMarketCap = "mcap"

	thumbnailRefresh      = 15 * time.Second
	thumbnailCacheEntries = 10000
	rankedDirectoryBound  = 100
)

// PlaybackSources is what the provider publishes for a live operation: the HLS
// playback URL and, when the provider generated one, a poster thumbnail.
type PlaybackSources struct {
	URL       string
	Thumbnail string
}

// ProviderPlayback resolves playback sources from the Livepeer stream record.
// A missing or failed thumbnail lookup leaves the poster absent; it never
// withholds playback.
func ProviderPlayback(provider *livepeer.Client, thumbnails *livepeer.ThumbnailCache) func(context.Context, Operation) (PlaybackSources, error) {
	return func(ctx context.Context, o Operation) (PlaybackSources, error) {
		if provider == nil {
			return PlaybackSources{}, ErrUnavailable
		}
		remote, err := provider.GetStream(ctx, o.ProviderID)
		if err != nil {
			return PlaybackSources{}, ErrUnavailable
		}
		if !remote.IsActive || !provider.PlaybackPolicyMatches(remote, o.ID) {
			return PlaybackSources{}, ErrUnavailable
		}
		sources := PlaybackSources{URL: provider.PlaybackURL(remote.PlaybackID)}
		if thumbnails != nil {
			if thumbnail, err := thumbnails.Thumbnail(ctx, remote.PlaybackID); err == nil {
				sources.Thumbnail = thumbnail
			}
		}
		return sources, nil
	}
}

func withThumbnail(view V2Stream, sources PlaybackSources) V2Stream {
	if view.Status == "live" && livepeer.SafeThumbnailURL(sources.Thumbnail) {
		view.ThumbnailURL = v2.Optional[string]{Present: true, Value: sources.Thumbnail}
	}
	return view
}

// MarketCapReader joins the canonical pool registry the creator check already
// reads with the stats market cap projection, through the versioned views
// livestream_runtime is granted (indexer.indexer_canonical_creations_v1 and
// stats.pool_market_caps_v1, coordinator migration 150006).
type MarketCapReader struct{ Pool *pgxpool.Pool }

func (m *MarketCapReader) MarketCaps(ctx context.Context, pools []string) (map[string]*big.Rat, error) {
	if m == nil || m.Pool == nil {
		return nil, ErrUnavailable
	}
	result := map[string]*big.Rat{}
	if len(pools) == 0 {
		return result, nil
	}
	lowered := make([]string, 0, len(pools))
	for _, pool := range pools {
		if !addressPattern.MatchString(pool) {
			return nil, ErrInvalid
		}
		lowered = append(lowered, strings.ToLower(pool))
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := m.Pool.Query(bounded, `SELECT c.pool,s.market_cap FROM indexer.indexer_canonical_creations_v1 c JOIN stats.pool_market_caps_v1 s ON lower(s.pool_address)=c.pool WHERE c.pool=ANY($1::text[])`, lowered)
	if err != nil {
		return nil, ErrMarketCapUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var pool, raw string
		if err = rows.Scan(&pool, &raw); err != nil {
			return nil, ErrMarketCapUnavailable
		}
		if len(raw) > 100 {
			continue
		}
		value, ok := new(big.Rat).SetString(raw)
		if !ok || value.Sign() < 0 {
			continue
		}
		result[pool] = value
	}
	if err = rows.Err(); err != nil {
		return nil, ErrMarketCapUnavailable
	}
	return result, nil
}

type rankCursor struct {
	Sort   string `json:"sort"`
	Offset int    `json:"offset"`
}

func decodeRankCursor(raw, sortKey string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	if len(raw) > 1024 {
		return 0, ErrInvalid
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, ErrInvalid
	}
	var c rankCursor
	if json.Unmarshal(decoded, &c) != nil || c.Sort != sortKey || c.Offset < 1 || c.Offset > 100000 {
		return 0, ErrInvalid
	}
	return c.Offset, nil
}

func encodeRankCursor(sortKey string, offset int) (*string, error) {
	raw, err := json.Marshal(rankCursor{Sort: sortKey, Offset: offset})
	if err != nil {
		return nil, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	return &encoded, nil
}

type rankedStream struct {
	view      V2Stream
	marketCap *big.Rat
}

// listRanked serves GET /v2/streams/live?sort=viewers|mcap over the most recent
// live operations (bounded like List), ordering by the verified viewer estimate
// or by the joined market cap; streams without a value sort last and ties keep
// the default newest-first order.
func (h *Handlers) listRanked(w http.ResponseWriter, r *http.Request, sortKey string) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if sortKey == SortMarketCap && h.marketCaps == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	limit := 20
	if limitText := r.URL.Query().Get("limit"); limitText != "" {
		parsed, err := strconv.Atoi(limitText)
		if err != nil || parsed < 1 || parsed > rankedDirectoryBound || strconv.Itoa(parsed) != limitText {
			h.lifecycleError(w, ErrInvalid)
			return
		}
		limit = parsed
	}
	offset, err := decodeRankCursor(r.URL.Query().Get("cursor"), sortKey)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	operations, err := h.lifecycle.List(r.Context(), "", true)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	ranked := make([]rankedStream, 0, len(operations))
	pools := make([]string, 0, len(operations))
	for _, o := range operations {
		item, err := h.public(r.Context(), o)
		if err != nil {
			h.lifecycleError(w, err)
			return
		}
		if item.Status != "live" {
			continue
		}
		ranked = append(ranked, rankedStream{view: item})
		pools = append(pools, item.PoolAddress)
	}
	if sortKey == SortMarketCap {
		caps, err := h.marketCaps.MarketCaps(r.Context(), pools)
		if err != nil {
			h.lifecycleError(w, err)
			return
		}
		for i := range ranked {
			ranked[i].marketCap = caps[strings.ToLower(ranked[i].view.PoolAddress)]
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		switch sortKey {
		case SortMarketCap:
			a, b := ranked[i].marketCap, ranked[j].marketCap
			if a == nil || b == nil {
				return a != nil && b == nil
			}
			return a.Cmp(b) > 0
		default:
			a, b := ranked[i].view.ViewerCount, ranked[j].view.ViewerCount
			if a == nil || b == nil {
				return a != nil && b == nil
			}
			return *a > *b
		}
	})
	page := []V2Stream{}
	var next *string
	if offset < len(ranked) {
		end := offset + limit
		if end > len(ranked) {
			end = len(ranked)
		}
		for _, item := range ranked[offset:end] {
			page = append(page, item.view)
		}
		if end < len(ranked) {
			if next, err = encodeRankCursor(sortKey, end); err != nil {
				h.lifecycleError(w, ErrUnavailable)
				return
			}
		}
	}
	writeJSON(w, 200, map[string]any{"streams": page, "nextCursor": next, "generatedAt": h.clock().UTC().Format(time.RFC3339Nano)})
}
