// Package httpapi implements the read HTTP routes for core/ranking-algo, porting
// @market-microservices/ranking-algo/src/routes/rankings.ts: a paginated
// ranked-list endpoint (enriched with cached pool stats) and a per-pool
// appearances endpoint. All ranked data is read from the Redis ZSETs published
// by internal/ranker.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// validCategories is the allowlist of ranking categories (parity with the TS
// VALID_CATEGORIES). Order is irrelevant; membership gates the routes.
var validCategories = map[string]struct{}{
	"trending":   {},
	"breakout":   {},
	"new":        {},
	"top_volume": {},
	"unusual":    {},
	"movers":     {},
}

// allCategories lists the categories scanned by the per-pool endpoint, in the
// TS declaration order so the JSON object key order matches for parity.
var allCategories = []string{"trending", "breakout", "new", "top_volume", "unusual", "movers"}

// statsSubsetKeys are the cached-stats fields echoed into each enriched item
// (parity with the TS enrichment projection).
var statsSubsetKeys = []string{
	"price",
	"priceChange1m", "priceChange5m", "priceChange15m", "priceChange1h", "priceChange24h",
	"priceChangeDollar1m", "priceChangeDollar5m", "priceChangeDollar15m",
	"priceChangeDollar1h", "priceChangeDollar24h",
	"volume24h", "volume1h", "volume5m", "marketCap", "holderCount",
}

// RegisterRankings registers the ranking read routes on the router.
func RegisterRankings(r chi.Router, rdb *goredis.Client) {
	r.Get("/rankings/pool/{poolAddress}", poolAppearances(rdb))
	r.Get("/rankings/{category}", rankingByCategory(rdb))
}

func parseIntDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}

func rankingByCategory(rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category := chi.URLParam(r, "category")
		if _, ok := validCategories[category]; !ok {
			sharedhttp.WriteJSON(w, http.StatusBadRequest, map[string]any{
				"error": "Invalid category",
				"valid": allCategories,
			})
			return
		}

		limit, offset, err := boundedPage(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))
		if err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Invalid pagination", err.Error())
			return
		}

		key := "ranking:" + category
		start := int64(offset)
		end := start + int64(limit) - 1

		ctx := r.Context()
		entries, err := rdb.ZRevRangeWithScores(ctx, key, start, end).Result()
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "ranking query failed")
			return
		}

		items := make([]map[string]any, 0, len(entries))
		for i, z := range entries {
			addr, ok := z.Member.(string)
			if !ok {
				continue
			}
			items = append(items, map[string]any{
				"poolAddress": addr,
				"score":       z.Score,
				"rank":        offset + i + 1,
			})
		}

		if err := enrichWithStats(ctx, rdb, items); err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "ranking enrich failed")
			return
		}

		total, err := rdb.ZCard(ctx, key).Result()
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "ranking count failed")
			return
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"category": category,
			"items":    items,
			"total":    total,
			"limit":    limit,
			"offset":   offset,
		})
	}
}

// enrichWithStats attaches the cached pool-stats subset to each item from the
// Redis keys stats:<poolAddress> (parity with the TS pipeline of GETs).
func enrichWithStats(ctx context.Context, rdb *goredis.Client, items []map[string]any) error {
	if len(items) == 0 {
		return nil
	}
	keys := make([]string, len(items))
	for i, item := range items {
		addr, ok := item["poolAddress"].(string)
		if !ok {
			continue
		}
		keys[i] = "stats:" + addr
	}
	cached, err := rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return err
	}
	for i, raw := range cached {
		s, ok := raw.(string)
		if !ok || s == "" {
			continue
		}
		var full map[string]any
		if err := json.Unmarshal([]byte(s), &full); err != nil {
			continue // malformed cache entry — skip enrichment, parity with TS
		}
		subset := make(map[string]any, len(statsSubsetKeys))
		for _, k := range statsSubsetKeys {
			if v, present := full[k]; present {
				subset[k] = v
			}
		}
		items[i]["stats"] = subset
	}
	return nil
}

func poolAppearances(rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		poolAddress := chi.URLParam(r, "poolAddress")
		ctx := r.Context()

		appearances := make(map[string]any, len(allCategories))
		for _, cat := range allCategories {
			key := "ranking:" + cat
			rank, err := rdb.ZRevRank(ctx, key, poolAddress).Result()
			if errors.Is(err, goredis.Nil) {
				appearances[cat] = nil
				continue
			}
			if err != nil {
				sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "rank lookup failed")
				return
			}
			score, err := rdb.ZScore(ctx, key, poolAddress).Result()
			if errors.Is(err, goredis.Nil) {
				// Raced eviction between ZREVRANK and ZSCORE — treat as absent.
				appearances[cat] = nil
				continue
			}
			if err != nil {
				sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "score lookup failed")
				return
			}
			appearances[cat] = map[string]any{
				"rank":  rank + 1,
				"score": score,
			}
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"poolAddress": poolAddress,
			"rankings":    appearances,
		})
	}
}

func boundedPage(rawLimit, rawOffset string) (int, int, error) {
	limit, offset := 50, 0
	var err error
	if rawLimit != "" {
		limit, err = strconv.Atoi(rawLimit)
		if err != nil {
			return 0, 0, errors.New("limit must be an integer from 1 to 100")
		}
	}
	if rawOffset != "" {
		offset, err = strconv.Atoi(rawOffset)
		if err != nil {
			return 0, 0, errors.New("offset must be an integer from 0 to 1000000")
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return 0, 0, errors.New("limit must be 1..100 and offset 0..1000000")
	}
	return limit, offset, nil
}
