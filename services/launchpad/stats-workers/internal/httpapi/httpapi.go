// Package httpapi implements the HTTP routes for core/stats-workers, porting the
// fastify routes of @analytics_microservices/stats: read endpoints for pool
// stats, holders, transactions, analytics, cross-token swaps, platform metrics,
// search, pressure and reactions, plus the HMAC-authenticated webhook receiver
// that drives the consumers. All JSON envelopes preserve the TS shapes (camelCase
// keys) for response parity; money fields are passed through as text (i1).
package httpapi

import (
	"strconv"
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

// asString coerces a decoded JSON webhook arg to a string ("" when absent/null).
func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// asStringPtr coerces a decoded JSON webhook arg to *string (nil when absent or
// not a string), matching the TS `args.creator ?? null` semantics.
func asStringPtr(v any) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

// asBool coerces a decoded JSON webhook arg to a bool (false when absent).
func asBool(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
