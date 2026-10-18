package rest

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func responseRequestID(w http.ResponseWriter) string {
	id := w.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(id); err != nil {
		id = uuid.NewString()
	}
	w.Header().Set("X-Request-ID", id)
	return id
}
func ResponseMetadata(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responseRequestID(w)
		version := "1"
		if strings.HasPrefix(r.URL.Path, "/v2/") {
			version = "2"
		}
		w.Header().Set("X-Schema-Version", version)
		q, parseErr := url.ParseQuery(r.URL.RawQuery)
		queryErr := QueryParameters(q)
		if parseErr != nil || queryErr != nil || len(r.URL.RawQuery) > 8192 {
			code := "QUERY_INVALID"
			if queryErr != nil {
				code = queryErr.Error()
			}
			WriteAPIError(w, r, 400, code)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 400*time.Millisecond)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ProductionReadOptions struct {
	Model             *store.AccountingReadModel
	Stream            *broker.PostgresStream
	Store             *store.Store
	StatsOrigin       string
	RegisterSnapshots bool
	Admission         *Admission
	AllowedOrigins    map[string]bool
	TrustedProxyCIDRs []string
}

func RegisterProductionReads(r chi.Router, o ProductionReadOptions) error {
	if r == nil || o.Model == nil || (o.RegisterSnapshots && o.Stream == nil) || o.Admission == nil {
		return errors.New("production read providers required")
	}
	holders, err := newHoldersUpstream(o.StatsOrigin)
	if err != nil {
		return err
	}
	register := func(method, path string, class AdmissionClass, h http.HandlerFunc) {
		r.Method(method, path, ResponseMetadata(o.Admission.Middleware(class)(h)))
	}
	register("GET", "/v2/pools", AdmissionAggregate, poolList(o))
	register("GET", "/v2/pools/{poolAddress}", AdmissionCached, poolDetail(o))
	register("GET", "/v2/pools/{poolAddress}/holders", AdmissionAggregate, holdersProxy(holders))
	register("GET", "/v2/pools/{poolAddress}/stats", AdmissionCached, projectionStats(o.Model))
	register("POST", "/v2/pools/stats:batch", AdmissionAggregate, projectionStatsBatch(o.Model))
	register("GET", "/v2/rankings/{category}", AdmissionAggregate, projectionRankings(o.Model))
	register("GET", "/v2/platform/metrics", AdmissionCached, platformMetricsV2(o.Store))
	register("GET", "/v2/leaderboards/{category}", AdmissionAggregate, leaderboards(o.Model))
	register("GET", "/v2/holder-rewards/stats", AdmissionCached, holderRewardsStats(o.Store))
	register("GET", "/v2/holder-rewards/pools", AdmissionAggregate, holderRewardsPools(o.Store))
	if o.RegisterSnapshots {
		register("GET", "/v2/snapshots", AdmissionAggregate, streamSnapshot(o))
	}
	return nil
}
func writeProjectionEnvelope(w http.ResponseWriter, r *http.Request, name string, data any, p *store.PublicProjection) {
	writePagedProjectionEnvelope(w, r, name, data, p, nil)
}
func writePagedProjectionEnvelope(w http.ResponseWriter, r *http.Request, name string, data any, p *store.PublicProjection, next any) {
	var revision any
	var watermark any
	generated := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	if p != nil {
		revision = p.Revision
		watermark = p.Watermark
		generated = p.OccurredAt
	}
	body, err := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": data, "revision": revision, "watermark": watermark, "quality": "partial", "nextCursor": next, "generatedAt": generated})
	if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON(name, body) != nil {
		WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}
func projectionStats(model *store.AccountingReadModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pool, ok := poolParameter(w, r)
		if !ok {
			return
		}
		if len(r.URL.Query()) != 0 {
			WriteV2APIError(w, r, 400, "QUERY_INVALID")
			return
		}
		values, err := model.PublicProjections(r.Context(), []string{"market.stats." + pool})
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		if len(values) != 1 {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		writeProjectionEnvelope(w, r, "StatsResponse", map[string]any{"stats": values[0].Payload}, &values[0])
	}
}

// statsBatchRequest validates a POST /v2/pools/stats:batch request and
// returns the normalized, de-duplicated pool addresses in request order. On
// failure it has already written the v2 error.
func statsBatchRequest(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	if len(r.URL.RawQuery) != 0 {
		WriteV2APIError(w, r, 400, "QUERY_INVALID")
		return nil, false
	}
	mediaType, _, typeErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typeErr != nil || mediaType != "application/json" {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	defer r.Body.Close()
	var body json.RawMessage
	decoder := json.NewDecoder(r.Body)
	if decoder.Decode(&body) != nil || interfaces.ValidateJSON("StatsBatchRequest", body) != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return nil, false
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		WriteV2APIError(w, r, 400, "QUERY_INVALID")
		return nil, false
	}
	var request struct {
		Pools []string `json:"poolAddresses"`
	}
	if json.Unmarshal(body, &request) != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return nil, false
	}
	seen := map[string]bool{}
	normalized := []string{}
	for _, input := range request.Pools {
		pool, err := store.Address(input)
		if err != nil {
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return nil, false
		}
		if seen[pool] {
			continue
		}
		seen[pool] = true
		normalized = append(normalized, pool)
	}
	return normalized, true
}

func projectionStatsBatch(model *store.AccountingReadModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		normalized, ok := statsBatchRequest(w, r)
		if !ok {
			return
		}
		topics := []string{}
		for _, pool := range normalized {
			topics = append(topics, "market.stats."+pool)
		}
		values, err := model.PublicProjections(r.Context(), topics)
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		items := []json.RawMessage{}
		found := map[string]json.RawMessage{}
		for _, p := range values {
			found[strings.TrimPrefix(p.Topic, "market.stats.")] = p.Payload
		}
		missing := []string{}
		for _, pool := range normalized {
			if payload, ok := found[pool]; !ok {
				missing = append(missing, pool)
			} else {
				items = append(items, payload)
			}
		}
		if len(missing) > 0 {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		writeProjectionEnvelope(w, r, "StatsBatchResponse", map[string]any{"items": items, "missing": missing}, nil)
	}
}

// rankingsRequest validates GET /v2/rankings/{category}: a contract category
// and no query string. On failure it has already written the v2 error.
func rankingsRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	category := chi.URLParam(r, "category")
	valid := false
	for _, v := range []string{"trending", "breakout", "volume", "gainers", "losers", "movers", "unusual", "new"} {
		if category == v {
			valid = true
		}
	}
	if !valid || QueryParameters(r.URL.Query()) != nil {
		WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return "", false
	}
	// The contract declares optional cursor and limit (1..100) query
	// parameters; anything else is rejected.
	for key, values := range r.URL.Query() {
		switch key {
		case "limit":
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || n > 100 {
				WriteV2APIError(w, r, 422, "FIELD_INVALID")
				return "", false
			}
		case "cursor":
			if values[0] == "" {
				WriteV2APIError(w, r, 422, "FIELD_INVALID")
				return "", false
			}
		default:
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return "", false
		}
	}
	return category, true
}

// rankingsLimit returns the validated limit query parameter or the contract
// default of 50. Call only after rankingsRequest accepted the request.
func rankingsLimit(r *http.Request) int {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 50
}

func projectionRankings(model *store.AccountingReadModel) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category, ok := rankingsRequest(w, r)
		if !ok {
			return
		}
		values, err := model.PublicProjections(r.Context(), []string{"market.rankings." + category})
		if err != nil {
			WriteReadError(w, r, err)
			return
		}
		if len(values) != 1 {
			WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		writeProjectionEnvelope(w, r, "RankingsResponse", map[string]any{"snapshot": values[0].Payload}, &values[0])
	}
}
func streamSnapshot(o ProductionReadOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if QueryParameters(q) != nil || len(r.URL.RawQuery) > 8192 {
			WriteV2APIError(w, r, 400, "QUERY_INVALID")
			return
		}
		for k, values := range q {
			if (k != "topics" && k != "cursor" && k != "normalizedFilters") || len(values) != 1 {
				WriteV2APIError(w, r, 422, "FIELD_UNKNOWN")
				return
			}
		}
		topics := strings.Split(q.Get("topics"), ",")
		if r.Header.Get("Authorization") != "" || !broker.PublicTopics(topics) {
			w.Header().Set("Cache-Control", "private, no-store")
		}
		sort.Strings(topics)
		requestShape := map[string]any{"topics": topics}
		if token := q.Get("cursor"); token != "" {
			requestShape["cursor"] = token
		}
		if filters := q.Get("normalizedFilters"); filters != "" {
			var normalized map[string]json.RawMessage
			if json.Unmarshal([]byte(filters), &normalized) != nil || interfaces.ValidateJSON("InputTopicFilters", []byte(filters)) != nil {
				WriteV2APIError(w, r, 422, "FIELD_INVALID")
				return
			}
			for key, raw := range normalized {
				var value string
				if key == "unreadOnly" {
					if string(raw) != "false" {
						WriteV2APIError(w, r, 422, "FIELD_INVALID")
						return
					}
					continue
				}
				if json.Unmarshal(raw, &value) != nil {
					WriteV2APIError(w, r, 422, "FIELD_INVALID")
					return
				}
				if key == "poolAddress" {
					var err error
					value, err = store.Address(value)
					if err != nil {
						WriteV2APIError(w, r, 422, "FIELD_INVALID")
						return
					}
				}
				for _, topic := range topics {
					parts := strings.Split(topic, ".")
					matches := false
					switch key {
					case "poolAddress":
						matches = len(parts) >= 3 && parts[2] == value
					case "category":
						matches = topic == "market.rankings."+value
					case "interval":
						matches = len(parts) == 4 && parts[0] == "market" && parts[1] == "candles" && parts[3] == value
					}
					if !matches {
						WriteV2APIError(w, r, 422, "FIELD_INVALID")
						return
					}
				}
			}
			requestShape["normalizedFilters"] = normalized
		}
		shape, _ := json.Marshal(requestShape)
		if interfaces.ValidateJSON("TopicSnapshotReadRequest", shape) != nil {
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return
		}
		var snapshot json.RawMessage
		var err error
		auth := r.Header.Get("Authorization")
		if auth == "" && broker.PublicTopics(topics) {
			if token := q.Get("cursor"); token != "" {
				replay, resumeErr := o.Stream.PublicResume(r.Context(), topics, token)
				if resumeErr != nil {
					writeSnapshotError(w, r, resumeErr)
					return
				}
				defer replay.Close()
				snapshot, err = o.Model.TopicSnapshotAt(r.Context(), topics, replay.Cursor.Offsets, replay.Cursor.Scope.SnapshotID, token)
				if err == nil {
					err = replay.Check(r.Context())
				}
			} else {
				snapshot, err = o.Stream.PublicSnapshot(r.Context(), topics)
			}
		} else {
			origin, originErr := broker.TrustedHTTPOrigin(r, o.AllowedOrigins, o.TrustedProxyCIDRs)
			if originErr != nil {
				WriteV2APIError(w, r, 403, "SCOPE_DENIED")
				return
			}
			authRequest := r.Clone(r.Context())
			authRequest.Header = r.Header.Clone()
			authRequest.Header.Set("Origin", origin)
			principal, authErr := o.Stream.AuthenticateRequest(r.Context(), authRequest)
			if authErr != nil {
				WriteV2APIError(w, r, 401, "AUTH_EXPIRED")
				return
			}
			binding := sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: topics, FilterDigest: sharedauth.StreamFilterDigest(topics), Origin: origin, Endpoint: "/v2/stream"}
			if sharedauth.ValidateStreamBinding(principal, binding) != nil {
				WriteV2APIError(w, r, 403, "SCOPE_DENIED")
				return
			}
			if token := q.Get("cursor"); token != "" {
				replay, resumeErr := o.Stream.Resume(r.Context(), principal, binding, token)
				if resumeErr != nil {
					writeSnapshotError(w, r, resumeErr)
					return
				}
				defer replay.Close()
				snapshot, err = o.Model.TopicSnapshotAt(r.Context(), topics, replay.Cursor.Offsets, replay.Cursor.Scope.SnapshotID, token)
				if err == nil {
					err = replay.Check(r.Context())
				}
			} else {
				snapshot, err = o.Stream.Snapshot(r.Context(), principal, binding)
			}
		}
		if err != nil {
			writeSnapshotError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		data := map[string]any{"snapshot": snapshot}
		body, marshalErr := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": data, "watermark": nil, "revision": nil, "quality": "partial", "nextCursor": nil, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
		if marshalErr != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON("TopicSnapshotReadResponse", body) != nil {
			WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func writeSnapshotError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, broker.ErrExpired):
		WriteV2APIError(w, r, 409, "RESNAPSHOT_REQUIRED")
	case errors.Is(err, broker.ErrCursor), errors.Is(err, broker.ErrSequence):
		WriteV2APIError(w, r, 400, "CURSOR_INVALID")
	case errors.Is(err, sharedauth.ErrScopeDenied):
		WriteV2APIError(w, r, 403, "SCOPE_DENIED")
	case errors.Is(err, broker.ErrFrame), errors.Is(err, store.ErrDataIntegrity):
		WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
	default:
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
	}
}
