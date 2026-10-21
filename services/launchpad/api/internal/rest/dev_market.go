package rest

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Development market reads: the v2 pools / stats / rankings / leaderboards /
// holder-rewards / platform / holders surface served outside production from
// the development store. Production mounts the same paths from
// RegisterProductionReads over the accounting read model; nothing here is
// reachable when NODE_ENV=production.

// DevelopmentMarketOptions carries the development providers.
type DevelopmentMarketOptions struct {
	Store     *store.Store
	Admission *Admission
	// ChainRPCURL resolves the block hashes and event positions the
	// development indexer does not persist. Empty leaves every document that
	// requires a chain watermark unavailable (503) and the nullable envelope
	// watermarks null.
	ChainRPCURL string
}

// developmentRankingKeys maps contract ranking categories onto the Redis
// ZSETs core/ranking-algo writes in development mode.
var developmentRankingKeys = map[string]string{"trending": "trending", "breakout": "breakout", "volume": "top_volume", "movers": "movers", "unusual": "unusual", "new": "new"}

// developmentPoolListKeys maps the pool list sort onto the same ZSETs.
var developmentPoolListKeys = map[string]string{"new": "new", "volume": "top_volume", "trending": "trending"}

const (
	developmentRankingModel      = "ranking-algo-dev-zset-v1"
	developmentMoversModel       = "stats-price-change-24h-bps-v1"
	developmentHolderPolicy      = "dev-stats-pool-holders-v1:exclude-pool-contract"
	developmentPoolListMax       = 1000
	developmentRankingMax        = 100
	developmentStatsWindowSecond = 86400
)

var (
	developmentRankingNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("sidiora:v2:dev-rankings"))
	developmentPoolListNamespce = uuid.NewSHA1(uuid.NameSpaceURL, []byte("sidiora:v2:dev-pool-list"))
	signedIntegerPattern        = regexp.MustCompile(`^(0|-?[1-9][0-9]{0,155})$`)
)

type developmentMarket struct {
	st    *store.Store
	chain *developmentChain
}

// RegisterDevelopmentMarketReads mounts the development market reads.
func RegisterDevelopmentMarketReads(ctx context.Context, r chi.Router, o DevelopmentMarketOptions) error {
	if r == nil {
		return errors.New("development market read router required")
	}
	chain, err := newDevelopmentChain(ctx, o.ChainRPCURL)
	if err != nil {
		return err
	}
	d := &developmentMarket{st: o.Store, chain: chain}
	register := func(method, path string, class AdmissionClass, h http.HandlerFunc) {
		if o.Store == nil {
			// Without the development store the routes still exist and
			// answer the non-retryable v2 dependency error instead of 404.
			h = WriteV2NotConfiguredError
		}
		var handler http.Handler = h
		if o.Admission != nil {
			handler = o.Admission.Middleware(class)(h)
		}
		r.Method(method, path, ResponseMetadata(handler))
	}
	register("GET", "/v2/pools", AdmissionAggregate, d.poolList)
	register("GET", "/v2/pools/{poolAddress}", AdmissionCached, d.poolDetail)
	register("GET", "/v2/pools/{poolAddress}/holders", AdmissionAggregate, d.holders)
	register("GET", "/v2/pools/{poolAddress}/stats", AdmissionCached, d.stats)
	register("POST", "/v2/pools/stats:batch", AdmissionAggregate, d.statsBatch)
	register("GET", "/v2/rankings/{category}", AdmissionAggregate, d.rankings)
	register("GET", "/v2/platform/metrics", AdmissionCached, platformMetricsV2(o.Store))
	register("GET", "/v2/leaderboards/{category}", AdmissionAggregate, leaderboardsFrom(d.leaderboard))
	register("GET", "/v2/holder-rewards/stats", AdmissionCached, holderRewardsStatsFrom(d.holderRewardsStats))
	register("GET", "/v2/holder-rewards/pools", AdmissionAggregate, holderRewardsPoolsFrom(d.holderRewardPools))
	return nil
}

// writeMarketEnvelope emits a v2 envelope whose watermark (and the
// revision derived from it) is null when the chain head is unavailable.
func writeMarketEnvelope(w http.ResponseWriter, r *http.Request, name string, data any, wm *store.ChainWatermark, quality string, next any) {
	var watermark, revision any
	if wm != nil {
		watermark, revision = *wm, wm.ProjectionRevision
	}
	body, err := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": data, "revision": revision, "watermark": watermark, "quality": quality, "nextCursor": next, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON(name, body) != nil {
		WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// optionalWatermark resolves the chain head for envelopes that allow null.
func (d *developmentMarket) optionalWatermark(ctx context.Context, head store.DevelopmentHead, headErr error) *store.ChainWatermark {
	if headErr != nil {
		return nil
	}
	wm, err := d.chain.headWatermark(ctx, head)
	if err != nil {
		return nil
	}
	return &wm
}

func (d *developmentMarket) requiredWatermark(ctx context.Context, head store.DevelopmentHead, headErr error) (store.ChainWatermark, error) {
	if d.chain == nil || d.chain.client == nil {
		return store.ChainWatermark{}, ErrDependencyNotConfigured
	}
	if headErr != nil {
		return store.ChainWatermark{}, headErr
	}
	return d.chain.headWatermark(ctx, head)
}

// developmentPoolDocument renders an indexer.pools row as the contract Pool.
func developmentPoolDocument(row store.DevelopmentPool, created accounting.SourcePosition) (map[string]any, error) {
	if row.Reserves == nil || row.FeeBps == nil {
		return nil, errPoolStateUnavailable
	}
	virtual, okVirtual := new(big.Int).SetString(string(row.Reserves.VirtualUsdl), 10)
	real, okReal := new(big.Int).SetString(string(row.Reserves.RealUsdl), 10)
	if !okVirtual || !okReal {
		return nil, store.ErrDataIntegrity
	}
	effective := new(big.Int).Add(virtual, real)
	if effective.BitLen() > 256 {
		return nil, store.ErrDataIntegrity
	}
	return map[string]any{
		"chainId":                        "125",
		"poolAddress":                    row.Pool,
		"tokenAddress":                   row.Token,
		"creator":                        row.Creator,
		"createdPosition":                chainPosition(created),
		"createdAt":                      time.Unix(row.CreatedAt, 0).UTC().Format("2006-01-02T15:04:05.000Z"),
		"virtualUsdlReserveRaw":          string(row.Reserves.VirtualUsdl),
		"realUsdlBalanceRaw":             string(row.Reserves.RealUsdl),
		"effectivePricingUsdlReserveRaw": effective.String(),
		"tokenReserveRaw":                string(row.Reserves.Token),
		"quoteAssetAddress":              strings.ToLower(profile.USDLAddress),
		"quoteAssetDecimals":             6,
		"feeBps":                         *row.FeeBps,
	}, nil
}

// developmentPoolStats renders the stats-workers row (stats.pool_stats via the
// read-through cache) as the contract PoolStats. Valuation fields stay null:
// the development stats row carries no verified supply valuation.
func developmentPoolStats(row store.DevelopmentPool, raw json.RawMessage, upper accounting.SourcePosition, wm store.ChainWatermark) (map[string]any, error) {
	if row.Reserves == nil {
		return nil, errPoolStateUnavailable
	}
	var legacy store.PoolStatsRow
	if json.Unmarshal(raw, &legacy) != nil {
		return nil, store.ErrDataIntegrity
	}
	if _, err := store.ParseUInt256(legacy.Volume24h); err != nil || legacy.BuyCount24h < 0 || legacy.SellCount24h < 0 || legacy.HolderCount < 0 || legacy.UpdatedAt <= 0 {
		return nil, store.ErrDataIntegrity
	}
	windowEnd := legacy.UpdatedAt
	var change any
	baseline := "unavailable"
	if signedIntegerPattern.MatchString(legacy.PriceChange24h) {
		change = legacy.PriceChange24h
		baseline = "insufficient_history"
		if row.CreatedAt <= windowEnd-developmentStatsWindowSecond {
			baseline = "complete"
		}
	}
	format := func(seconds int64) string { return time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05.000Z") }
	return map[string]any{
		"poolAddress":                 row.Pool,
		"volume24hRaw":                legacy.Volume24h,
		"buys24h":                     strconv.Itoa(legacy.BuyCount24h),
		"sells24h":                    strconv.Itoa(legacy.SellCount24h),
		"holderCount":                 strconv.Itoa(legacy.HolderCount),
		"realUsdlBalanceRaw":          string(row.Reserves.RealUsdl),
		"indicativeTvlUsdlRaw":        nil,
		"valuationMethod":             nil,
		"windowStart":                 format(windowEnd - developmentStatsWindowSecond),
		"windowEnd":                   format(windowEnd),
		"sourceUpperPosition":         chainPosition(upper),
		"watermark":                   wm,
		"totalSupplyValuationUsdlRaw": nil,
		"circulatingMarketCapUsdlRaw": nil,
		"supplyValuationQuality":      "unavailable",
		"priceChangeBps24h":           change,
		"valuationChangeUsdlRaw":      nil,
		"changeBaselineQuality":       baseline,
	}, nil
}

// statsInputs are the store reads one PoolStats document needs.
type statsInputs struct {
	rows    map[string]store.DevelopmentPool
	stats   map[string]json.RawMessage
	head    store.DevelopmentHead
	headErr error
}

func (d *developmentMarket) readStatsInputs(ctx context.Context, pools []string) (statsInputs, error) {
	var in statsInputs
	var err error
	if in.rows, err = d.st.DevelopmentPools(ctx, pools); err != nil {
		return in, err
	}
	known := make([]string, 0, len(in.rows))
	for _, pool := range pools {
		if _, ok := in.rows[pool]; ok {
			known = append(known, pool)
		}
	}
	in.stats = map[string]json.RawMessage{}
	if len(known) > 0 {
		if in.stats, err = d.st.StatsBatch(ctx, known); err != nil {
			return in, err
		}
	}
	in.head, in.headErr = d.st.DevelopmentIndexerHead(ctx)
	return in, nil
}

// statsDocuments builds PoolStats for every pool with a registry row, a stats
// row, indexed state and resolvable chain positions. Others are omitted.
func (d *developmentMarket) statsDocuments(ctx context.Context, in statsInputs, pools []string, wm store.ChainWatermark) (map[string]map[string]any, error) {
	candidates := []store.DevelopmentPool{}
	refs := []store.DevelopmentEventRef{}
	for _, pool := range pools {
		row, known := in.rows[pool]
		_, hasStats := in.stats[pool]
		if !known || !hasStats || row.Reserves == nil {
			continue
		}
		candidates = append(candidates, row)
		if row.StateEvent != nil {
			refs = append(refs, *row.StateEvent)
		}
	}
	creations := d.chain.creationPositions(ctx, candidates)
	events := d.chain.eventPositions(ctx, refs)
	out := map[string]map[string]any{}
	for _, row := range candidates {
		upper, ok := creations[row.Pool]
		if row.StateEvent != nil {
			upper, ok = events[eventKey(*row.StateEvent)]
		}
		if !ok {
			continue
		}
		doc, err := developmentPoolStats(row, in.stats[row.Pool], upper, wm)
		if err != nil {
			return nil, err
		}
		out[row.Pool] = doc
	}
	return out, nil
}

func (d *developmentMarket) stats(w http.ResponseWriter, r *http.Request) {
	pool, ok := poolParameter(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		WriteV2APIError(w, r, 400, "QUERY_INVALID")
		return
	}
	in, err := d.readStatsInputs(r.Context(), []string{pool})
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	if _, known := in.rows[pool]; !known {
		WriteV2APIError(w, r, 404, "NOT_FOUND")
		return
	}
	wm, err := d.requiredWatermark(r.Context(), in.head, in.headErr)
	if err != nil {
		writeComputedReadError(w, r, err)
		return
	}
	docs, err := d.statsDocuments(r.Context(), in, []string{pool}, wm)
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	doc, found := docs[pool]
	if !found {
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	writeMarketEnvelope(w, r, "StatsResponse", map[string]any{"stats": doc}, &wm, "partial", nil)
}

func (d *developmentMarket) statsBatch(w http.ResponseWriter, r *http.Request) {
	pools, ok := statsBatchRequest(w, r)
	if !ok {
		return
	}
	in, err := d.readStatsInputs(r.Context(), pools)
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	docs := map[string]map[string]any{}
	var wm *store.ChainWatermark
	if len(in.stats) > 0 {
		required, wmErr := d.requiredWatermark(r.Context(), in.head, in.headErr)
		if wmErr != nil {
			writeComputedReadError(w, r, wmErr)
			return
		}
		wm = &required
		if docs, err = d.statsDocuments(r.Context(), in, pools, required); err != nil {
			writePoolReadError(w, r, err)
			return
		}
	} else {
		wm = d.optionalWatermark(r.Context(), in.head, in.headErr)
	}
	items := []map[string]any{}
	missing := []string{}
	for _, pool := range pools {
		if doc, found := docs[pool]; found {
			items = append(items, doc)
		} else {
			missing = append(missing, pool)
		}
	}
	writeMarketEnvelope(w, r, "StatsBatchResponse", map[string]any{"items": items, "missing": missing}, wm, "partial", nil)
}

func (d *developmentMarket) poolDetail(w http.ResponseWriter, r *http.Request) {
	pool, ok := poolParameter(w, r)
	if !ok {
		return
	}
	if len(r.URL.Query()) != 0 {
		WriteV2APIError(w, r, 400, "QUERY_INVALID")
		return
	}
	in, err := d.readStatsInputs(r.Context(), []string{pool})
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	row, known := in.rows[pool]
	if !known {
		WriteV2APIError(w, r, 404, "NOT_FOUND")
		return
	}
	if _, hasStats := in.stats[pool]; !hasStats || row.Reserves == nil || row.FeeBps == nil {
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	wm, err := d.requiredWatermark(r.Context(), in.head, in.headErr)
	if err != nil {
		writeComputedReadError(w, r, err)
		return
	}
	created, found := d.chain.creationPositions(r.Context(), []store.DevelopmentPool{row})[pool]
	if !found {
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	poolDoc, err := developmentPoolDocument(row, created)
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	stats, err := d.statsDocuments(r.Context(), in, []string{pool}, wm)
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	if stats[pool] == nil {
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	token := tokenDocument(store.PoolRegistryRow{Token: row.Token})
	writeMarketEnvelope(w, r, "PoolDetailResponse", map[string]any{"pool": poolDoc, "token": token, "stats": stats[pool]}, &wm, "partial", nil)
}

func developmentSnapshotID(namespace uuid.UUID, parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		digest.Write([]byte(part + "|"))
	}
	return uuid.NewSHA1(namespace, digest.Sum(nil)).String()
}

func (d *developmentMarket) poolList(w http.ResponseWriter, r *http.Request) {
	request, ok := poolListRequest(w, r)
	if !ok {
		return
	}
	members, _, err := d.st.RankingMembers(r.Context(), developmentPoolListKeys[request.sort], developmentPoolListMax)
	if err != nil {
		WriteReadError(w, r, err)
		return
	}
	parts := []string{request.sort}
	for _, m := range members {
		parts = append(parts, m.PoolAddress)
	}
	snapshotID := developmentSnapshotID(developmentPoolListNamespce, parts...)
	offset := 0
	if request.hasCursor {
		cursor, cursorErr := decodePoolListCursor(request.cursor)
		if cursorErr != nil {
			WriteV2APIError(w, r, 400, "CURSOR_INVALID")
			return
		}
		if cursor.SnapshotID != snapshotID {
			WriteV2APIError(w, r, 409, "RESNAPSHOT_REQUIRED")
			return
		}
		if cursor.Offset > len(members) {
			WriteV2APIError(w, r, 400, "CURSOR_INVALID")
			return
		}
		offset = cursor.Offset
	}
	end := offset + request.limit
	if end > len(members) {
		end = len(members)
	}
	page := []string{}
	for _, m := range members[offset:end] {
		page = append(page, m.PoolAddress)
	}
	rows := map[string]store.DevelopmentPool{}
	if len(page) > 0 {
		if rows, err = d.st.DevelopmentPools(r.Context(), page); err != nil {
			writePoolReadError(w, r, err)
			return
		}
	}
	head, headErr := d.st.DevelopmentIndexerHead(r.Context())
	pending := make([]store.DevelopmentPool, 0, len(page))
	for _, pool := range page {
		row, found := rows[pool]
		if !found || row.Reserves == nil || row.FeeBps == nil {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		pending = append(pending, row)
	}
	created := d.chain.creationPositions(r.Context(), pending)
	items := []map[string]any{}
	for _, row := range pending {
		position, found := created[row.Pool]
		if !found {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		doc, docErr := developmentPoolDocument(row, position)
		if docErr != nil {
			writePoolReadError(w, r, docErr)
			return
		}
		items = append(items, doc)
	}
	var next any
	if end < len(members) {
		next = encodePoolListCursor(poolListCursor{SnapshotID: snapshotID, Offset: end})
	}
	writeMarketEnvelope(w, r, "PoolListResponse", map[string]any{"items": items}, d.optionalWatermark(r.Context(), head, headErr), "partial", next)
}

// developmentScore renders a ranking score in the contract decimal form.
func developmentScore(score float64) string {
	return strconv.FormatFloat(score, 'f', -1, 64)
}

func (d *developmentMarket) rankings(w http.ResponseWriter, r *http.Request) {
	category, ok := rankingsRequest(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("cursor") != "" {
		// Development snapshots fit one page and never issue a cursor.
		WriteV2APIError(w, r, 400, "CURSOR_INVALID")
		return
	}
	limit := rankingsLimit(r)
	type entry struct{ pool, score string }
	var entries []entry
	var total int64
	model := developmentRankingModel
	if key, zset := developmentRankingKeys[category]; zset {
		members, card, err := d.st.RankingMembers(r.Context(), key, developmentRankingMax)
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		total = card
		for _, m := range members {
			entries = append(entries, entry{m.PoolAddress, developmentScore(m.Score)})
		}
	} else {
		movers, count, err := d.st.DevelopmentPriceMovers(r.Context(), category == "gainers", developmentRankingMax)
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		total, model = count, developmentMoversModel
		for _, m := range movers {
			entries = append(entries, entry{m.Pool, m.ChangeBps})
		}
	}
	head, headErr := d.st.DevelopmentIndexerHead(r.Context())
	wm, err := d.requiredWatermark(r.Context(), head, headErr)
	if err != nil {
		writeComputedReadError(w, r, err)
		return
	}
	computedAt := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	parts := []string{category, model, wm.Height}
	items := make([]map[string]any, 0, len(entries))
	if len(entries) > limit {
		entries = entries[:limit]
	}
	for i, e := range entries {
		parts = append(parts, e.pool+":"+e.score)
		items = append(items, map[string]any{"poolAddress": e.pool, "category": category, "rank": i + 1, "score": e.score, "modelVersion": model, "computedAt": computedAt})
	}
	if total < int64(len(items)) {
		WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	snapshot := map[string]any{
		"snapshotId":    developmentSnapshotID(developmentRankingNamespace, parts...),
		"category":      category,
		"modelVersion":  model,
		"computedAt":    computedAt,
		"watermark":     wm,
		"items":         items,
		"totalEligible": strconv.FormatInt(total, 10),
		"truncated":     total > int64(len(items)),
		"replace":       true,
	}
	writeMarketEnvelope(w, r, "RankingsResponse", map[string]any{"snapshot": snapshot}, &wm, "partial", nil)
}

func (d *developmentMarket) leaderboard(ctx context.Context, category, window string, windowEnd int64) (*store.LeaderboardSnapshot, error) {
	wm, err := d.chain.watermark(ctx, d.st)
	if err != nil {
		return nil, err
	}
	return d.st.DevelopmentLeaderboard(ctx, category, window, windowEnd, wm)
}

func (d *developmentMarket) holderRewardsStats(ctx context.Context) (*store.HolderRewardsStats, error) {
	wm, err := d.chain.watermark(ctx, d.st)
	if err != nil {
		return nil, err
	}
	return d.st.DevelopmentHolderRewardsStats(ctx, wm)
}

func (d *developmentMarket) holderRewardPools(ctx context.Context, sortKey string) (*store.HolderRewardPoolsSnapshot, error) {
	wm, err := d.chain.watermark(ctx, d.st)
	if err != nil {
		return nil, err
	}
	return d.st.DevelopmentHolderRewardPools(ctx, sortKey, wm)
}

// holdersCursor is the keyset position after the last holder served.
type holdersCursor struct {
	Pool    string `json:"pool"`
	Balance string `json:"balance"`
	Wallet  string `json:"wallet"`
}

func encodeHoldersCursor(c holdersCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeHoldersCursor(token, pool string) (holdersCursor, error) {
	var c holdersCursor
	if len(token) > 256 {
		return c, store.ErrCursorInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return c, store.ErrCursorInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Pool != pool {
		return c, store.ErrCursorInvalid
	}
	if _, err = store.ParseUInt256(c.Balance); err != nil {
		return c, store.ErrCursorInvalid
	}
	if wallet, addressErr := store.Address(c.Wallet); addressErr != nil || wallet != c.Wallet {
		return c, store.ErrCursorInvalid
	}
	return c, nil
}

func (d *developmentMarket) holders(w http.ResponseWriter, r *http.Request) {
	request, ok := holdersRequest(w, r)
	if !ok {
		return
	}
	var after holdersCursor
	if request.hasCursor {
		var err error
		if after, err = decodeHoldersCursor(request.cursor, request.pool); err != nil {
			WriteV2APIError(w, r, 400, "CURSOR_INVALID")
			return
		}
	}
	rows, err := d.st.DevelopmentPools(r.Context(), []string{request.pool})
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	row, known := rows[request.pool]
	if !known {
		WriteV2APIError(w, r, 404, "NOT_FOUND")
		return
	}
	excluded := []string{request.pool}
	holders, err := d.st.DevelopmentHolders(r.Context(), request.pool, excluded, after.Balance, after.Wallet, request.limit+1)
	if err != nil {
		writePoolReadError(w, r, err)
		return
	}
	head, headErr := d.st.DevelopmentIndexerHead(r.Context())
	var next any
	if len(holders) > request.limit {
		holders = holders[:request.limit]
		last := holders[len(holders)-1]
		next = encodeHoldersCursor(holdersCursor{Pool: request.pool, Balance: string(last.Balance), Wallet: last.Wallet})
	}
	items := make([]map[string]any, 0, len(holders))
	for _, h := range holders {
		items = append(items, map[string]any{"wallet": h.Wallet, "tokenAddress": row.Token, "balanceRaw": string(h.Balance), "share": nil})
	}
	sort.Strings(excluded)
	data := map[string]any{"items": items, "coverage": "partial", "fromPosition": nil, "toPosition": nil, "exclusionPolicyVersion": developmentHolderPolicy, "excludedProtocolAddresses": excluded}
	writeMarketEnvelope(w, r, "HoldersResponse", data, d.optionalWatermark(r.Context(), head, headErr), "partial", next)
}
