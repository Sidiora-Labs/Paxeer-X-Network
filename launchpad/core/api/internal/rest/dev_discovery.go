package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Development discovery serves the anonymous discovery reads outside
// production over the narrative_discovery tables (core/discovery). Production
// mounts discovery.HTTP behind the identity authority and answers the feed,
// callouts and leaderboards 503 because it never wires the visibility and
// interval authorities. In development:
//
//   - feed pages and feed details resolve callout sources with core/discovery's
//     own OwnedSources/CalloutService code; other source kinds (pool,
//     milestone, live_session) need the external source authority, which does
//     not exist, so they are omitted from pages and answer 503 as details.
//     There is no social-graph visibility authority either, so the public feed
//     applies the source's own Public/Authorized/Tombstone flags only.
//   - writes (callout create/edit) need an authenticated actor; development
//     core/api has no identity authority, so they answer 401.
//   - leaderboards need the PnL interval authority (verified wallet links and
//     attribution opt-in), which development does not have: 503.
type devDiscovery struct {
	st    *store.Store
	owned discovery.OwnedSources
}

func newDevDiscovery(st *store.Store) devDiscovery {
	ds := discovery.Store{DB: st.DevelopmentDiscoveryDB()}
	return devDiscovery{st: st, owned: discovery.OwnedSources{Callouts: discovery.CalloutService{Store: ds}}}
}

func encodeDiscoveryKey(k store.DiscoveryKey) string {
	raw, _ := json.Marshal(k)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeDiscoveryKey(token string, maxLength int) (*store.DiscoveryKey, error) {
	if token == "" || len(token) > maxLength {
		return nil, errors.New("cursor bounds")
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, err
	}
	var k store.DiscoveryKey
	if err = json.Unmarshal(raw, &k); err != nil || k.At.IsZero() || k.ID == "" || len(k.ID) > 160 || encodeDiscoveryKey(k) != token {
		return nil, errors.New("cursor invalid")
	}
	return &k, nil
}

func samePublicSource(a, b discovery.SourceRef) bool {
	return a.ID == b.ID && a.EventID == b.EventID && a.Kind == b.Kind && a.CreatorPublicID == b.CreatorPublicID && slices.Equal(a.Pools, b.Pools) && a.SourceRevision == b.SourceRevision && a.VisibilityRevision == b.VisibilityRevision && a.PublishedAt.Equal(b.PublishedAt) && a.Public == b.Public && a.Authorized == b.Authorized && a.Tombstone == b.Tombstone && a.Corrected == b.Corrected
}

// resolve mirrors discovery.Feed.resolve for an anonymous reader.
func (d devDiscovery) resolve(ctx context.Context, ref discovery.SourceRef) (discovery.FeedCard, error) {
	card := discovery.FeedCard{Source: ref, State: "unavailable"}
	if ref.Tombstone {
		card.State = "removed"
		return card, nil
	}
	if !ref.Public || !ref.Authorized {
		return card, discovery.ErrForbidden
	}
	resource, err := d.owned.ResolvePublic(ctx, ref.Kind, ref.ID)
	if err != nil {
		return card, err
	}
	if !samePublicSource(resource.Ref, ref) || !resource.Ref.Public || !resource.Ref.Authorized || resource.Ref.Tombstone {
		return card, discovery.ErrUnavailable
	}
	card.Body = resource.Body
	card.State = "available"
	card.PositionDisclosure = resource.PositionDisclosure
	if resource.PositionDisclosure != "" {
		card.PositionDisclosureKind = "user_declared"
	}
	card.PositionVerified = resource.PositionVerified
	return card, nil
}

func strictQuery(req *http.Request, allowed ...string) (url.Values, bool) {
	values, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		return nil, false
	}
	for key, v := range values {
		if !slices.Contains(allowed, key) || len(v) != 1 {
			return nil, false
		}
	}
	return values, true
}

func writeDiscoveryDocument(w http.ResponseWriter, req *http.Request, name string, v any) {
	body, err := json.Marshal(v)
	if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON(name, body) != nil {
		WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		return
	}
	responseRequestID(w)
	writeHSDBody(w, body, "private, no-store")
}

func (d devDiscovery) feed(w http.ResponseWriter, req *http.Request) {
	values, ok := strictQuery(req, "order", "cursor", "limit")
	if !ok {
		WriteV2APIError(w, req, 400, "QUERY_INVALID")
		return
	}
	order := values.Get("order")
	if order == "" {
		order = "chronological"
	}
	limit := 50
	if raw, present := values["limit"]; present {
		n, err := strconv.Atoi(raw[0])
		if err != nil || strconv.Itoa(n) != raw[0] || n < 1 || n > 50 {
			WriteV2APIError(w, req, 422, "FIELD_INVALID")
			return
		}
		limit = n
	}
	if order != "chronological" && order != "relevance" {
		WriteV2APIError(w, req, 422, "FIELD_INVALID")
		return
	}
	var after *store.DiscoveryKey
	if raw, present := values["cursor"]; present {
		var err error
		if after, err = decodeDiscoveryKey(raw[0], 2048); err != nil {
			WriteV2APIError(w, req, 400, "CURSOR_INVALID")
			return
		}
	}
	page := discovery.FeedPage{Entries: []discovery.FeedCard{}, Algorithm: "chronological-v1"}
	if order == "relevance" {
		page.Algorithm = "language-follow-topic-v1"
	}
	if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
		// Production personalizes for an authenticated reader; development
		// has no identity authority, so it takes production's public fallback.
		page.Algorithm = "public-chronological-fallback-v1"
		page.FallbackReason = "identity_unavailable"
	}
	ctx := req.Context()
	scanned := 0
	cursor := after
	for len(page.Entries) < limit && scanned < store.DevelopmentDiscoveryMaxScan {
		batch := min(100, store.DevelopmentDiscoveryMaxScan-scanned)
		refs, keys, err := d.st.DevelopmentDiscoverySources(ctx, cursor, batch)
		if err != nil {
			if errors.Is(err, store.ErrDataIntegrity) {
				WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
			} else {
				WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
			}
			return
		}
		consumed := 0
		for i, ref := range refs {
			scanned++
			consumed++
			k := keys[i]
			cursor = &k
			if card, resolveErr := d.resolve(ctx, ref); resolveErr == nil && card.State == "available" {
				page.Entries = append(page.Entries, card)
				if len(page.Entries) == limit {
					break
				}
			}
		}
		if consumed == len(refs) && len(refs) < batch {
			// The source table is exhausted.

			cursor = nil
			break
		}
	}
	if cursor != nil {
		// Either the page filled or the scan budget ran out with rows left:
		// the cursor resumes strictly after the last inspected source. A
		// filled page whose last row was the final source yields one extra,
		// empty page, never a skipped source.
		page.Cursor = encodeDiscoveryKey(*cursor)
	}
	writeDiscoveryDocument(w, req, "ConnectedDiscoveryFeedPage", page)
}

func (d devDiscovery) feedDetail(w http.ResponseWriter, req *http.Request) {
	if _, ok := strictQuery(req); !ok {
		WriteV2APIError(w, req, 400, "QUERY_INVALID")
		return
	}
	id := chi.URLParam(req, "id")
	if id == "" || len(id) > 160 {
		WriteV2APIError(w, req, 422, "FIELD_INVALID")
		return
	}
	ref, found, err := d.st.DevelopmentDiscoverySource(req.Context(), id)
	switch {
	case errors.Is(err, store.ErrDataIntegrity):
		WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		return
	case err != nil:
		WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
		return
	case !found:
		WriteV2APIError(w, req, 404, "NOT_FOUND")
		return
	}
	card, err := d.resolve(req.Context(), ref)
	if errors.Is(err, discovery.ErrForbidden) {
		WriteV2APIError(w, req, 403, "SCOPE_DENIED")
		return
	}
	if err != nil {
		WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	writeDiscoveryDocument(w, req, "ConnectedDiscoveryFeedCard", card)
}

func (d devDiscovery) leaderboards(w http.ResponseWriter, req *http.Request) {
	values, ok := strictQuery(req, "start", "end")
	if !ok {
		WriteV2APIError(w, req, 400, "QUERY_INVALID")
		return
	}
	start, startErr := time.Parse(time.RFC3339, values.Get("start"))
	end, endErr := time.Parse(time.RFC3339, values.Get("end"))
	if startErr != nil || endErr != nil || !end.After(start) {
		WriteV2APIError(w, req, 422, "FIELD_INVALID")
		return
	}
	// discovery.RankIntervals requires an IntervalAuthority; development has
	// none (no verified wallet links or attribution opt-in), as production.
	// That is configuration, not a transient failure: non-retryable.
	WriteV2NotConfiguredError(w, req)
}

func unauthenticatedWrite(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	WriteV2APIError(w, req, 401, "AUTH_EXPIRED")
}

func (d devDiscovery) poolCallouts(w http.ResponseWriter, req *http.Request) {
	values, ok := strictQuery(req, "cursor", "limit")
	if !ok {
		WriteV2APIError(w, req, 400, "QUERY_INVALID")
		return
	}
	pool, err := store.Address(chi.URLParam(req, "poolAddress"))
	if err != nil {
		WriteV2APIError(w, req, 422, "FIELD_INVALID")
		return
	}
	limit := 50
	if raw, present := values["limit"]; present {
		n, convErr := strconv.Atoi(raw[0])
		if convErr != nil || strconv.Itoa(n) != raw[0] || n < 1 || n > 100 {
			WriteV2APIError(w, req, 422, "FIELD_INVALID")
			return
		}
		limit = n
	}
	var after *store.DiscoveryKey
	if raw, present := values["cursor"]; present {
		if after, err = decodeDiscoveryKey(raw[0], 256); err != nil {
			WriteV2APIError(w, req, 400, "CURSOR_INVALID")
			return
		}
	}
	ctx := req.Context()
	indexed, err := d.st.DevelopmentPoolIndexed(ctx, pool)
	if err != nil {
		WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	if !indexed {
		WriteV2APIError(w, req, 404, "NOT_FOUND")
		return
	}
	callouts, keys, err := d.st.DevelopmentPoolCallouts(ctx, pool, after, limit+1)
	if err != nil {
		if errors.Is(err, store.ErrDataIntegrity) {
			WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
		} else {
			WriteV2APIError(w, req, 503, "DEPENDENCY_UNAVAILABLE")
		}
		return
	}
	items := []any{}
	var next *string
	for i, c := range callouts {
		if i == limit {
			token := encodeDiscoveryKey(keys[i-1])
			next = &token
			break
		}
		if _, idErr := uuid.Parse(c.ID); idErr != nil {
			// The contract identifies callouts by UUID and authors by wallet;
			// a row that cannot satisfy both is quarantined, not rewritten.
			WriteV2APIError(w, req, 503, "INTEGRITY_QUARANTINED")
			return
		}
		items = append(items, map[string]any{"calloutId": c.ID, "authorWallet": c.AuthorAccountID, "poolAddress": c.PoolAddress, "body": c.Body, "direction": c.Direction, "disclosure": c.Disclosure, "createdAt": c.CreatedAt.UTC().Format(hsdTimestamp), "revision": strconv.FormatUint(c.Revision, 10)})
	}
	writeHSDEnvelope(w, req, "CalloutListResponse", map[string]any{"items": items}, next)
}

// RegisterDevelopmentHistorySearchDiscovery mounts the non-production trade
// history, candles, search and anonymous discovery reads. It must only be
// called outside production; production mounts these paths from
// RegisterAccountingV2WithAdmission, RegisterV2SearchProxy and
// app.registerDiscovery.
func RegisterDevelopmentHistorySearchDiscovery(r chi.Router, st *store.Store, admission *Admission) {
	mount := func(method, path string, class AdmissionClass, h http.HandlerFunc) {
		var handler http.Handler = h
		if admission != nil {
			handler = admission.Middleware(class)(handler)
		}
		r.Method(method, path, ResponseMetadata(handler))
	}
	d := newDevDiscovery(st)
	mount(http.MethodGet, "/v2/pools/{poolAddress}/trades", AdmissionHistory, devHistory(st, false))
	mount(http.MethodGet, "/v2/pools/{poolAddress}/candles", AdmissionHistory, devHistory(st, true))
	mount(http.MethodGet, "/v2/search", AdmissionAggregate, devSearch(st))
	mount(http.MethodGet, "/v2/search/expanded", AdmissionAggregate, devExpandedSearch(st))
	mount(http.MethodGet, "/v2/pools/{poolAddress}/callouts", AdmissionAggregate, d.poolCallouts)
	mount(http.MethodGet, "/v2/discovery/feed", AdmissionAggregate, d.feed)
	mount(http.MethodGet, "/v2/discovery/feed/{id}", AdmissionCached, d.feedDetail)
	mount(http.MethodGet, "/v2/discovery/leaderboards", AdmissionAggregate, d.leaderboards)
	mount(http.MethodPost, "/v2/discovery/callouts", AdmissionCached, unauthenticatedWrite)
	mount(http.MethodPatch, "/v2/discovery/callouts/{id}", AdmissionCached, unauthenticatedWrite)
}
