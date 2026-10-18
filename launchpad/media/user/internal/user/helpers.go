package user

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/image"
	"github.com/Sidiora-Technologies/KindleLaunch/media/user/pkg/types"
)

// normalizeAddr lowercases and trims an address (writes + reads are lowercased
// so case-insensitive matching against checksum-cased indexer rows is stable).
func normalizeAddr(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// baseURL returns the configured PublicURL when set, else builds an origin from
// the request (scheme + host), matching the TS fallback.
func baseURL(publicURL string, r *http.Request) string {
	if publicURL != "" {
		return publicURL
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// buildImages resolves avatar/banner absolute URLs from the stored image rows.
// URLs are extension-less ("/users/<addr>/avatar") and resolve to the serve
// route, which sets Content-Type from the stored mime_type.
func buildImages(base, addr string, images []sqlcdb.UsersUserImage) types.Images {
	var out types.Images
	for _, img := range images {
		url := base + "/users/" + addr + "/" + img.ImageType
		switch img.ImageType {
		case image.TypeAvatar:
			out.Avatar = strPtr(url)
		case image.TypeBanner:
			out.Banner = strPtr(url)
		}
	}
	return out
}

// buildCreatedPools maps the indexer cross-read rows to the public shape.
func buildCreatedPools(rows []sqlcdb.ListCreatedPoolsRow) []types.CreatedPool {
	out := make([]types.CreatedPool, 0, len(rows))
	for _, p := range rows {
		out = append(out, types.CreatedPool{
			PoolAddress:  p.PoolAddress,
			TokenAddress: p.TokenAddress,
			CreatedAt:    p.CreatedAt,
		})
	}
	return out
}

// strPtr returns a pointer to s.
func strPtr(s string) *string { return &s }

// int64Ptr returns a pointer to v.
func int64Ptr(v int64) *int64 { return &v }

// textPtr maps a nullable pgtype.Text to *string (nil on NULL).
func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	s := t.String
	return &s
}

// strToText maps a string to pgtype.Text, treating "" as NULL (empty
// social/display/bio fields store as NULL).
func strToText(s string) pgtype.Text {
	if strings.TrimSpace(s) == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// itoa64 formats an int64 as a decimal string.
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
