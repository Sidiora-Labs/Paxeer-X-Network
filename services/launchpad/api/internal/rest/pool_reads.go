package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

var errPoolStateUnavailable = errors.New("pool state projection unavailable")

type poolDocument struct {
	Pool  map[string]any
	Token map[string]any
}

func chainPosition(p accounting.SourcePosition) map[string]any {
	return map[string]any{"chainId": "125", "height": p.Height, "transactionIndex": p.TransactionIndex, "logIndex": p.LogIndex, "transactionHash": p.TransactionHash, "blockHash": p.BlockHash}
}

func tokenDocument(row store.PoolRegistryRow) map[string]any {
	token := map[string]any{"address": row.Token, "name": "", "symbol": "", "decimals": nil, "decimalsQuality": "unavailable", "totalSupplyRaw": nil, "supplyQuality": "unavailable", "sourcePosition": nil, "metadataRevision": "0", "logoAssetId": nil}
	if m := row.Metadata; m != nil {
		token["name"] = m.Name
		token["symbol"] = m.Symbol
		token["decimals"] = int(m.Decimals)
		token["decimalsQuality"] = "verified"
		token["totalSupplyRaw"] = m.TotalSupply
		token["supplyQuality"] = "verified"
		token["sourcePosition"] = chainPosition(m.Position)
	}
	return token
}

func resolvePools(ctx context.Context, o ProductionReadOptions, pools []string) (map[string]poolDocument, error) {
	registry, err := o.Store.PoolRegistry(ctx, pools)
	if err != nil {
		return nil, err
	}
	out := map[string]poolDocument{}
	if len(registry) == 0 {
		return out, nil
	}
	known := make([]string, 0, len(registry))
	for pool := range registry {
		known = append(known, pool)
	}
	sort.Strings(known)
	reserves, err := o.Store.PoolReserves(ctx, known)
	if err != nil {
		return nil, err
	}
	fees, err := o.Model.ObservedFeeBps(ctx, known)
	if err != nil {
		return nil, err
	}
	for _, pool := range known {
		row := registry[pool]
		state, hasState := reserves[pool]
		fee, hasFee := fees[pool]
		if !hasState || !hasFee {
			return nil, errPoolStateUnavailable
		}
		virtual, okVirtual := new(big.Int).SetString(string(state.VirtualUsdl), 10)
		real, okReal := new(big.Int).SetString(string(state.RealUsdl), 10)
		if !okVirtual || !okReal {
			return nil, store.ErrDataIntegrity
		}
		effective := new(big.Int).Add(virtual, real)
		if effective.BitLen() > 256 {
			return nil, store.ErrDataIntegrity
		}
		out[pool] = poolDocument{Pool: map[string]any{
			"chainId":                        "125",
			"poolAddress":                    row.Pool,
			"tokenAddress":                   row.Token,
			"creator":                        row.Creator,
			"createdPosition":                chainPosition(row.Position),
			"createdAt":                      time.Unix(row.BlockTimestamp, 0).UTC().Format("2006-01-02T15:04:05.000Z"),
			"virtualUsdlReserveRaw":          string(state.VirtualUsdl),
			"realUsdlBalanceRaw":             string(state.RealUsdl),
			"effectivePricingUsdlReserveRaw": effective.String(),
			"tokenReserveRaw":                string(state.Token),
			"quoteAssetAddress":              strings.ToLower(profile.USDLAddress),
			"quoteAssetDecimals":             6,
			"feeBps":                         fee,
		}, Token: tokenDocument(row)}
	}
	return out, nil
}

func writePoolReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errPoolStateUnavailable), errors.Is(err, store.ErrAccountingNotReady):
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
	default:
		WriteReadError(w, r, err)
	}
}

func poolDetail(o ProductionReadOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pool, ok := poolParameter(w, r)
		if !ok {
			return
		}
		if len(r.URL.Query()) != 0 {
			WriteV2APIError(w, r, 400, "QUERY_INVALID")
			return
		}
		if o.Store == nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		docs, err := resolvePools(r.Context(), o, []string{pool})
		if err != nil {
			writePoolReadError(w, r, err)
			return
		}
		doc, found := docs[pool]
		if !found {
			WriteV2APIError(w, r, 404, "NOT_FOUND")
			return
		}
		values, err := o.Model.PublicProjections(r.Context(), []string{"market.stats." + pool})
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		if len(values) != 1 {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		writeProjectionEnvelope(w, r, "PoolDetailResponse", map[string]any{"pool": doc.Pool, "token": doc.Token, "stats": values[0].Payload}, &values[0])
	}
}

type poolListCursor struct {
	SnapshotID string `json:"snapshotId"`
	Offset     int    `json:"offset"`
}

func encodePoolListCursor(c poolListCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodePoolListCursor(token string) (poolListCursor, error) {
	var c poolListCursor
	if token == "" || len(token) > 256 {
		return c, store.ErrCursorInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return c, store.ErrCursorInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Offset < 0 || c.SnapshotID == "" {
		return c, store.ErrCursorInvalid
	}
	return c, nil
}

// poolListQuery is a validated GET /v2/pools request.
type poolListQuery struct {
	sort      string
	limit     int
	cursor    string
	hasCursor bool
}

// poolListRequest validates GET /v2/pools. On failure it has already written
// the v2 error.
func poolListRequest(w http.ResponseWriter, r *http.Request) (poolListQuery, bool) {
	q := r.URL.Query()
	for key, values := range q {
		if (key != "sort" && key != "cursor" && key != "limit") || len(values) != 1 {
			WriteV2APIError(w, r, 422, "FIELD_UNKNOWN")
			return poolListQuery{}, false
		}
	}
	limit, err := boundedInteger(q.Get("limit"), 50, 1, 100)
	if err != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return poolListQuery{}, false
	}
	out := poolListQuery{sort: q.Get("sort"), limit: limit}
	shape := map[string]any{"sort": out.sort, "limit": limit}
	if token, present := q["cursor"]; present {
		if token[0] == "" {
			WriteV2APIError(w, r, 400, "CURSOR_INVALID")
			return poolListQuery{}, false
		}
		shape["cursor"] = token[0]
		out.cursor, out.hasCursor = token[0], true
	}
	raw, _ := json.Marshal(shape)
	if interfaces.ValidateJSON("PoolListRequest", raw) != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return poolListQuery{}, false
	}
	return out, true
}

func poolList(o ProductionReadOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		request, ok := poolListRequest(w, r)
		if !ok {
			return
		}
		if o.Store == nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		values, err := o.Model.PublicProjections(r.Context(), []string{"market.rankings." + request.sort})
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		if len(values) != 1 {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		var snapshot struct {
			SnapshotID string `json:"snapshotId"`
			Items      []struct {
				Pool string `json:"poolAddress"`
				Rank int64  `json:"rank"`
			} `json:"items"`
		}
		if json.Unmarshal(values[0].Payload, &snapshot) != nil || snapshot.SnapshotID == "" {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		ranked := make([]string, 0, len(snapshot.Items))
		sort.SliceStable(snapshot.Items, func(i, j int) bool {
			if snapshot.Items[i].Rank != snapshot.Items[j].Rank {
				return snapshot.Items[i].Rank < snapshot.Items[j].Rank
			}
			return strings.ToLower(snapshot.Items[i].Pool) < strings.ToLower(snapshot.Items[j].Pool)
		})
		for _, item := range snapshot.Items {
			pool, addressErr := store.Address(item.Pool)
			if addressErr != nil {
				WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
				return
			}
			ranked = append(ranked, pool)
		}
		offset := 0
		if request.hasCursor {
			cursor, cursorErr := decodePoolListCursor(request.cursor)
			if cursorErr != nil {
				WriteV2APIError(w, r, 400, "CURSOR_INVALID")
				return
			}
			if cursor.SnapshotID != snapshot.SnapshotID {
				WriteV2APIError(w, r, 409, "RESNAPSHOT_REQUIRED")
				return
			}
			if cursor.Offset > len(ranked) {
				WriteV2APIError(w, r, 400, "CURSOR_INVALID")
				return
			}
			offset = cursor.Offset
		}
		end := offset + request.limit
		if end > len(ranked) {
			end = len(ranked)
		}
		page := ranked[offset:end]
		items := []map[string]any{}
		if len(page) > 0 {
			docs, resolveErr := resolvePools(r.Context(), o, page)
			if resolveErr != nil {
				writePoolReadError(w, r, resolveErr)
				return
			}
			for _, pool := range page {
				doc, found := docs[pool]
				if !found {
					WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
					return
				}
				items = append(items, doc.Pool)
			}
		}
		var next any
		if end < len(ranked) {
			next = encodePoolListCursor(poolListCursor{SnapshotID: snapshot.SnapshotID, Offset: end})
		}
		writePagedProjectionEnvelope(w, r, "PoolListResponse", map[string]any{"items": items}, &values[0], next)
	}
}

type holdersUpstream struct {
	target *url.URL
	client *http.Client
}

func newHoldersUpstream(origin string) (*holdersUpstream, error) {
	if origin == "" {
		return &holdersUpstream{}, nil
	}
	target, err := url.Parse(origin)
	if err != nil || target.Host == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" || target.Path != "" && target.Path != "/" || (target.Scheme != "https" && target.Scheme != "http") {
		return nil, errors.New("stats_service_origin_invalid")
	}
	return &holdersUpstream{target: target, client: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{ResponseHeaderTimeout: 2 * time.Second, MaxIdleConnsPerHost: 8}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

var holdersUpstreamCodes = map[string]int{"CURSOR_INVALID": 400, "NOT_FOUND": 404, "RESNAPSHOT_REQUIRED": 409, "FIELD_INVALID": 422, "FIELD_UNKNOWN": 422, "DEPENDENCY_UNAVAILABLE": 503, "INTEGRITY_QUARANTINED": 503}

// holdersQuery is a validated GET /v2/pools/{poolAddress}/holders request.
type holdersQuery struct {
	pool      string
	limit     int
	cursor    string
	hasCursor bool
}

// holdersRequest validates GET /v2/pools/{poolAddress}/holders. On failure it
// has already written the v2 error.
func holdersRequest(w http.ResponseWriter, r *http.Request) (holdersQuery, bool) {
	pool, ok := poolParameter(w, r)
	if !ok {
		return holdersQuery{}, false
	}
	q := r.URL.Query()
	for key, values := range q {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			WriteV2APIError(w, r, 422, "FIELD_UNKNOWN")
			return holdersQuery{}, false
		}
	}
	limit, err := boundedInteger(q.Get("limit"), 50, 1, 100)
	if err != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return holdersQuery{}, false
	}
	out := holdersQuery{pool: pool, limit: limit}
	shape := map[string]any{"poolAddress": pool, "limit": limit}
	if token, present := q["cursor"]; present {
		if token[0] == "" {
			WriteV2APIError(w, r, 400, "CURSOR_INVALID")
			return holdersQuery{}, false
		}
		shape["cursor"] = token[0]
		out.cursor, out.hasCursor = token[0], true
	}
	raw, _ := json.Marshal(shape)
	if interfaces.ValidateJSON("HoldersRequest", raw) != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return holdersQuery{}, false
	}
	return out, true
}

func holdersProxy(u *holdersUpstream) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, ok := holdersRequest(w, r)
		if !ok {
			return
		}
		pool := query.pool
		upstreamQuery := url.Values{"limit": {strconv.Itoa(query.limit)}}
		if query.hasCursor {
			upstreamQuery.Set("cursor", query.cursor)
		}
		if u == nil || u.target == nil || u.client == nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		endpoint := u.target.JoinPath("v1", "pools", pool, "holders")
		endpoint.RawQuery = upstreamQuery.Encode()
		request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint.String(), nil)
		if err != nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-Request-ID", responseRequestID(w))
		response, err := u.client.Do(request)
		if err != nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(io.LimitReader(response.Body, int64(store.PublicJSONMaxBytes)+1))
		if err != nil || len(body) > store.PublicJSONMaxBytes {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		if response.StatusCode != http.StatusOK {
			var failure struct {
				Code string `json:"code"`
			}
			if json.Unmarshal(body, &failure) != nil {
				WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
			status, known := holdersUpstreamCodes[failure.Code]
			if !known || status != response.StatusCode {
				WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
			WriteV2APIError(w, r, status, failure.Code)
			return
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal(body, &envelope) != nil {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		id, _ := json.Marshal(responseRequestID(w))
		envelope["requestId"] = id
		out, err := json.Marshal(envelope)
		if err != nil || len(out) > store.PublicJSONMaxBytes || interfaces.ValidateJSON("HoldersResponse", out) != nil {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(response.Header.Get("Cache-Control"), "private") {
			w.Header().Set("Cache-Control", "private, no-store")
		} else {
			w.Header().Set("Cache-Control", "no-store")
		}
		_, _ = w.Write(out)
	}
}

func platformMetricsV2(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for key, values := range q {
			if key != "window" || len(values) != 1 {
				WriteV2APIError(w, r, 422, "FIELD_UNKNOWN")
				return
			}
		}
		raw, _ := json.Marshal(map[string]any{"window": q.Get("window")})
		if interfaces.ValidateJSON("PlatformRequest", raw) != nil {
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return
		}
		if q.Get("window") != "24h" {
			WriteV2APIError(w, r, 501, "FEATURE_UNAVAILABLE")
			return
		}
		if st == nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		cached, found, err := st.CachedJSON(r.Context(), "platform:metrics")
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		if !found {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		var metrics struct {
			TotalVolume24h      string `json:"totalVolume24h"`
			UniqueTraders24h    int64  `json:"uniqueTraders24h"`
			TotalTokensLaunched int64  `json:"totalTokensLaunched"`
			UpdatedAt           int64  `json:"updatedAt"`
			// Interface 2.2.0 window totals of the same stats-workers aggregate;
			// absent from an aggregate written before they existed.
			TotalMarketCap       *string `json:"totalMarketCap"`
			TotalTransactions24h *int64  `json:"totalTransactions24h"`
			NewTokens24h         *int64  `json:"newTokens24h"`
		}
		if json.Unmarshal(cached, &metrics) != nil || metrics.UpdatedAt <= 0 || metrics.UniqueTraders24h < 0 || metrics.TotalTokensLaunched < 0 {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		data := map[string]any{"volumeUsdlRaw": metrics.TotalVolume24h, "pools": strconv.FormatInt(metrics.TotalTokensLaunched, 10), "activeWallets": strconv.FormatInt(metrics.UniqueTraders24h, 10)}
		if metrics.TotalMarketCap != nil {
			if _, err := store.ParseUInt256(*metrics.TotalMarketCap); err != nil {
				WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
				return
			}
			data["totalMarketCapUsdlRaw"] = *metrics.TotalMarketCap
		}
		for name, count := range map[string]*int64{"trades": metrics.TotalTransactions24h, "newPools": metrics.NewTokens24h} {
			if count == nil {
				continue
			}
			if *count < 0 {
				WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
				return
			}
			data[name] = strconv.FormatInt(*count, 10)
		}
		body, err := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": data, "revision": nil, "watermark": nil, "quality": "partial", "nextCursor": nil, "generatedAt": time.Unix(metrics.UpdatedAt, 0).UTC().Format("2006-01-02T15:04:05.000Z")})
		if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON("PlatformResponse", body) != nil {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(body)
	}
}
