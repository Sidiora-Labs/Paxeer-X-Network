package rest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Development search serves GET /v2/search and GET /v2/search/expanded outside
// production. Production proxies /v2/search to the stats service, whose
// canonical index (stats_search.*) exists only under the production migrations;
// in development stats-workers answers that route 503. The development index is
// the shared database itself: indexer.pools + metadata.token_metadata for pools
// and tokens, users.user_profiles for profiles.

const developmentSearchGeneration = "development"

type devSearchRequest struct {
	kind, match, key, after string
	limit                   int
}

type devSearchError struct {
	status int
	code   string
}

func (e devSearchError) Error() string { return e.code }

func searchFail(status int, code string) error { return devSearchError{status, code} }

// normalizeDevelopmentQuery mirrors the stats canonical normalization rules
// that matter for the development tables: bidi controls are stripped, the
// query is trimmed and case-folded, "0x" prefixes are address lookups (42
// characters exact, 4..41 hex prefix), transaction hashes are rejected, and
// text needs at least two code points.
func normalizeDevelopmentQuery(raw string) (string, string, error) {
	if len(raw) > 1024 || strings.ContainsRune(raw, 0) || !utf8.ValidString(raw) || utf8.RuneCountInString(raw) > 256 {
		return "", "", errors.New("query bounds")
	}
	s := strings.Map(func(r rune) rune {
		if r == 0x061c || r == 0x200e || r == 0x200f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return -1
		}
		return r
	}, raw)
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(s, "0x") {
		if len(s) > 42 || len(s) < 4 {
			return "", "", errors.New("address length")
		}
		if _, err := hex.DecodeString(s[2:] + strings.Repeat("0", len(s[2:])%2)); err != nil {
			return "", "", errors.New("address hex")
		}
		if len(s) == 42 {
			return s, "exact_address", nil
		}
		return s, "partial_address", nil
	}
	if utf8.RuneCountInString(s) < 2 {
		return "", "", errors.New("minimum two code points")
	}
	return s, "text", nil
}

func parseDevelopmentSearch(req *http.Request, expanded bool) (devSearchRequest, error) {
	out := devSearchRequest{limit: store.DevelopmentSearchLimit}
	if len(req.URL.RawQuery) > 8192 {
		return out, searchFail(413, "PAYLOAD_TOO_LARGE")
	}
	values, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return out, searchFail(400, "QUERY_INVALID")
	}
	allowed := map[string]bool{"q": true, "type": true, "limit": true}
	if expanded {
		allowed["sort"], allowed["filter"], allowed["cursor"] = true, true, true
	}
	for key, v := range values {
		if !allowed[key] || len(v) != 1 {
			return out, searchFail(400, "QUERY_INVALID")
		}
	}
	shape := map[string]any{"q": values.Get("q"), "type": values.Get("type")}
	if raw, ok := values["limit"]; ok {
		n, convErr := strconv.Atoi(raw[0])
		if convErr != nil || strconv.Itoa(n) != raw[0] {
			return out, searchFail(422, "FIELD_INVALID")
		}
		out.limit = n
		shape["limit"] = n
	}
	body, _ := json.Marshal(shape)
	if _, present := values["q"]; !present || values.Get("type") == "" || interfaces.ValidateJSON("SearchRequest", body) != nil {
		return out, searchFail(422, "FIELD_INVALID")
	}
	out.kind = values.Get("type")
	if out.key, out.match, err = normalizeDevelopmentQuery(values.Get("q")); err != nil {
		return out, searchFail(422, "FIELD_INVALID")
	}
	if !expanded {
		return out, nil
	}
	if s, ok := values["sort"]; ok && s[0] != "address" {
		return out, searchFail(422, "FIELD_INVALID")
	}
	if _, ok := values["filter"]; ok {
		// Pool status / risk / metric filters need projections development lacks.
		return out, searchFail(501, "FEATURE_UNAVAILABLE")
	}
	if raw, ok := values["cursor"]; ok {
		var c devSearchCursor
		decoder := json.NewDecoder(strings.NewReader(raw[0]))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&c) != nil || decoder.More() || c.Version != 2 || c.NormalizationVersion != 1 || c.Generation != developmentSearchGeneration || c.Binding != out.binding() || c.Snapshot != 0 || c.PrivacyRevision != 0 || c.After == "" || len(c.After) != 42 {
			return out, searchFail(400, "CURSOR_INVALID")
		}
		out.after = c.After
	}
	return out, nil
}

// binding pins a cursor to the normalized query, kind and page size.
func (r devSearchRequest) binding() string {
	raw, _ := json.Marshal([]any{2, r.kind, r.match, r.key, r.limit, "address"})
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

type devSearchCursor struct {
	Version              int       `json:"version"`
	NormalizationVersion int       `json:"normalizationVersion"`
	Binding              string    `json:"binding"`
	Generation           string    `json:"generation"`
	Snapshot             int64     `json:"snapshot"`
	PrivacyRevision      int64     `json:"privacyRevision"`
	ObservedAt           time.Time `json:"observedAt"`
	After                string    `json:"after"`
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func devSearchLabel(h store.DevelopmentSearchHit, kind string) string {
	label := h.Name
	if kind == "profile" {
		label = h.Display
		if label == "" {
			label = h.Wallet
		}
	} else if label == "" {
		label = h.Symbol
		if label == "" {
			label = h.Token
			if kind == "pool" {
				label = h.Pool
			}
		}
	}
	return truncateRunes(label, 100)
}

// devSearchResult builds the contract SearchResult for one hit.
func devSearchResult(h store.DevelopmentSearchHit, kind string) map[string]any {
	result := map[string]any{"kind": kind, "label": devSearchLabel(h, kind), "poolAddress": nil, "tokenAddress": nil, "wallet": nil, "score": "1"}
	switch kind {
	case "profile":
		result["id"] = "125:profile:" + h.Wallet
		result["wallet"] = h.Wallet
	case "token":
		result["id"] = "125:" + h.Token
		result["poolAddress"], result["tokenAddress"] = h.Pool, h.Token
	default:
		result["id"] = "125:" + h.Pool
		result["poolAddress"], result["tokenAddress"] = h.Pool, h.Token
	}
	return result
}

func writeSearchFailure(w http.ResponseWriter, req *http.Request, err error) {
	var failure devSearchError
	if errors.As(err, &failure) {
		WriteV2APIError(w, req, failure.status, failure.code)
		return
	}
	if errors.Is(err, store.ErrDataIntegrity) {
		WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		return
	}
	WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
}

func devSearch(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r, err := parseDevelopmentSearch(req, false)
		if err != nil {
			writeSearchFailure(w, req, err)
			return
		}
		hits, err := st.DevelopmentSearch(req.Context(), r.kind, r.match, r.key, "", r.limit)
		if err != nil {
			writeSearchFailure(w, req, err)
			return
		}
		items := []any{}
		for i, h := range hits {
			if i == r.limit {
				break
			}
			items = append(items, devSearchResult(h, r.kind))
		}
		writeHSDEnvelope(w, req, "SearchResponse", map[string]any{"items": items}, nil)
	}
}

func devExpandedSearch(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		r, err := parseDevelopmentSearch(req, true)
		if err != nil {
			writeSearchFailure(w, req, err)
			return
		}
		hits, err := st.DevelopmentSearch(req.Context(), r.kind, r.match, r.key, r.after, r.limit)
		if err != nil {
			writeSearchFailure(w, req, err)
			return
		}
		now := time.Now().UTC().Truncate(time.Millisecond)
		items := []any{}
		var next any
		for i, h := range hits {
			if i == r.limit {
				after := hits[i-1].Pool
				if r.kind == "profile" {
					after = hits[i-1].Wallet
				}
				next = devSearchCursor{Version: 2, NormalizationVersion: 1, Binding: r.binding(), Generation: developmentSearchGeneration, ObservedAt: now, After: after}
				break
			}
			item := devSearchResult(h, r.kind)
			item["riskFlags"], item["confusableWarnings"], item["metricsAt"], item["quoteExecutable"] = nil, nil, nil, false
			item["creator"] = h.Creator
			if h.Symbol != "" {
				item["ticker"] = h.Symbol
			}
			if h.CreatedAt > 0 {
				item["createdAt"] = time.Unix(h.CreatedAt, 0).UTC().Format(time.RFC3339)
			}
			items = append(items, item)
		}
		status := "ok"
		if len(items) == 0 && r.match == "exact_address" {
			status = "not-indexed"
		}
		page := map[string]any{"items": items, "matchKind": r.match, "status": status, "generation": developmentSearchGeneration, "revision": 0, "privacyRevision": 0, "observedAt": now.Format(time.RFC3339Nano), "nextCursor": next}
		body, err := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": page, "watermark": nil, "revision": nil, "quality": "partial", "generatedAt": now.Format(hsdTimestamp)})
		if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON("ConnectedExpandedSearchResponse", body) != nil {
			WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
			return
		}
		writeHSDBody(w, body, "private, no-store")
	}
}
