package metadata

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

var addrRe = regexp.MustCompile(`^0x[a-f0-9]{40}$`)

// normalizeAddr lowercases and trims an address (writes + reads are lowercased
// so case-insensitive matching against checksum-cased indexer rows is stable).
func normalizeAddr(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// stripExt removes a single trailing ".ext" from a filename ("0xabc.png" ->
// "0xabc"). A filename with no dot is returned unchanged.
func stripExt(file string) string {
	if i := strings.LastIndexByte(file, '.'); i >= 0 {
		return file[:i]
	}
	return file
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

// parseBatchAddresses dedups, validates (0x + 40 hex), lowercases and caps the
// addresses from one or more ?addresses= query values (comma-joined or repeated).
func parseBatchAddresses(values []string) []string {
	raw := strings.Join(values, ",")
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		a := normalizeAddr(part)
		if !addrRe.MatchString(a) {
			continue
		}
		if _, dup := seen[a]; dup {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
		if len(out) >= maxBatch {
			break
		}
	}
	return out
}

// buildImages resolves logo/banner absolute URLs from the stored image rows.
func buildImages(base, addr string, images []sqlcdb.MetadataTokenImage) types.Images {
	var out types.Images
	for _, img := range images {
		ext := extFromKey(img.StorageKey)
		url := base + "/" + img.ImageType + "/" + addr + "." + ext
		switch img.ImageType {
		case "logo":
			out.Logo = strPtr(url)
		case "banner":
			out.Banner = strPtr(url)
		}
	}
	return out
}

// extFromKey returns the extension of a storage key ("logos/logo-0x.png" ->
// "png"), defaulting to "png" when none is present.
func extFromKey(key string) string {
	if i := strings.LastIndexByte(key, '.'); i >= 0 && i < len(key)-1 {
		return key[i+1:]
	}
	return "png"
}

// parseTags decodes the custom_tags JSON array, defaulting to an empty slice on
// NULL or malformed JSON (never nil — the response always emits []).
func parseTags(t pgtype.Text) []string {
	if !t.Valid || strings.TrimSpace(t.String) == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(t.String), &tags); err != nil {
		return []string{}
	}
	if tags == nil {
		return []string{}
	}
	return tags
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

// strToText maps a string to pgtype.Text, treating "" as NULL (parity with the
// TS undefined-field semantics — empty social/desc fields store as NULL).
func strToText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

// itoa64 formats an int64 as a decimal string.
func itoa64(v int64) string { return strconv.FormatInt(v, 10) }
