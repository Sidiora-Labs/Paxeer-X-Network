package social

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
)

// actorHeader is the trusted identity header injected by media/gateway after it
// authenticates the user once. social does NOT verify wallet signatures (the OG
// per-action signing was removed — users hated it); it trusts this header
// because the gateway is the sole public ingress (2026-06-26 decision).
const actorHeader = "X-Actor-Wallet"

// addrRe / normalizeAddr / sanitizeContent / generateID delegate to internal
// common so the REST and realtime paths share byte-identical behaviour.
var addrRe = common.AddrRe

// normalizeAddr lowercases and trims an address so writes + reads match the
// stored lowercased values (keeping the btree indexes sargable).
func normalizeAddr(s string) string { return common.NormalizeAddr(s) }

// actor returns the normalized actor wallet from the trusted gateway header, and
// ok=false when it is missing or malformed.
func actor(r *http.Request) (string, bool) {
	a := common.NormalizeAddr(r.Header.Get(actorHeader))
	if !common.IsAddr(a) {
		return "", false
	}
	return a, true
}

// sanitizeContent strips HTML tags and control characters then trims.
func sanitizeContent(in string) string { return common.Sanitize(in) }

// generateID builds a lexicographically-sortable id (see common.GenerateID).
func generateID(now time.Time) string { return common.GenerateID(now) }

// ── pgtype <-> Go conversions ─────────────────────────────────────────────────

// textPtr maps a nullable pgtype.Text to *string (nil on NULL).
func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

// int8Ptr maps a nullable pgtype.Int8 to *int64 (nil on NULL).
func int8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// strToTextPtr maps an optional string to *string for sqlc varchar params,
// treating "" as NULL.
func strToTextPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := normalizeAddr(*s)
	if v == "" {
		return nil
	}
	return &v
}

// textValue builds a pgtype.Text, treating "" as NULL.
func textValue(s string) pgtype.Text {
	if strings.TrimSpace(s) == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// textValuePtr builds a pgtype.Text from an optional string (nil/"" => NULL).
func textValuePtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{Valid: false}
	}
	return textValue(*s)
}

// int8Value builds a non-null pgtype.Int8.
func int8Value(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

// reasonPtr maps a pgtype.Text reason to *string for response shaping.
func reasonPtr(t pgtype.Text) *string { return textPtr(t) }

// itoa formats an int as a decimal string.
func itoa(v int) string { return strconv.Itoa(v) }

// parseLimit clamps a ?limit= query value to [1, max] with a default.
func parseLimit(raw string, def, max int32) int32 {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if int32(n) > max {
		return max
	}
	return int32(n)
}
