// Package httpapi implements the HTTP routes for core/pnl-tracker, porting the
// pnl service routes: portfolio reads (positions, single position, trades,
// net-worth portfolio), card mint/hydrate + OG image, referral attribution
// events + sharer dashboard, the service status probe, and the HMAC webhook that
// drives the realtime swap fold. All JSON envelopes preserve the TS client
// shapes (camelCase keys, pnl.ts) for response parity; money fields pass through
// as text (i1).
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// parseIntDefault parses raw as an int, returning def on empty or invalid input
// (parity with the TS `Number(x) || default` coercion used by the routes).
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

// parseOptInt64 parses raw into *int64, returning nil for empty/invalid input
// (an absent query filter).
func parseOptInt64(raw string) *int64 {
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// asString coerces a decoded JSON webhook arg to a string ("" when absent/null).
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asBool coerces a decoded JSON webhook arg to a bool (false when absent).
func asBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// cachedJSON serves a read-through cached JSON value: a cache hit is returned
// verbatim, otherwise compute runs, its result is cached for ttl, and returned.
// Cache failures degrade to a direct compute (the TTL bounds staleness).
func cachedJSON(w http.ResponseWriter, r *http.Request, rdb *goredis.Client, key string, ttl time.Duration, compute func(context.Context) (any, error)) {
	ctx := r.Context()
	if cached, err := rdb.Get(ctx, key).Result(); err == nil && cached != "" {
		sharedhttp.WriteJSON(w, http.StatusOK, json.RawMessage(cached))
		return
	}
	val, err := compute(ctx)
	if err != nil {
		sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "lookup failed")
		return
	}
	payload, err := json.Marshal(val)
	if err != nil {
		sharedhttp.WriteJSON(w, http.StatusOK, val)
		return
	}
	rdb.Set(ctx, key, payload, ttl)
	sharedhttp.WriteJSON(w, http.StatusOK, json.RawMessage(payload))
}
