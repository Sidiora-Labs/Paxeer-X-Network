package httpapi

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/store"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type VisibilityFence interface {
	CurrentRankingRevision(context.Context) (string, time.Time, error)
}
type Snapshots struct {
	store      *store.SnapshotStore
	cache      *store.SnapshotCache
	cursor     *ranker.CursorCodec
	visibility VisibilityFence
	admission  chan struct{}
}

func NewSnapshots(repository *store.SnapshotStore, cache *store.SnapshotCache, cursor *ranker.CursorCodec, visibility VisibilityFence, maxReads int) (*Snapshots, error) {
	if repository == nil || cursor == nil || visibility == nil || maxReads < 1 || maxReads > 64 {
		return nil, ranker.ErrUnavailable
	}
	return &Snapshots{repository, cache, cursor, visibility, make(chan struct{}, maxReads)}, nil
}
func RegisterSnapshots(r chi.Router, h *Snapshots) {
	r.Get("/rankings/runs/{runID}/{category}", h.inspect)
	r.Get("/rankings/{category}", h.page)
}
func snapshotCategory(raw string) (string, error) {
	if raw == "top_volume" {
		raw = "volume"
	}
	_, err := ranker.Policy(raw)
	return raw, err
}
func snapshotLimit(raw string) (int, error) {
	if raw == "" {
		return 50, nil
	}
	if !regexp.MustCompile(`^[1-9][0-9]{0,2}$`).MatchString(raw) {
		return 0, ranker.ErrCursorInvalid
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n > 100 {
		return 0, ranker.ErrCursorInvalid
	}
	return n, nil
}
func (h *Snapshots) admit(w http.ResponseWriter) bool {
	select {
	case h.admission <- struct{}{}:
		return true
	default:
		w.Header().Set("Retry-After", "1")
		snapshotError(w, 503, "RANKING_UNAVAILABLE", "Ranking read capacity is temporarily unavailable.", 1, rankingGuidance{RetainLastResult: true})
		return false
	}
}
func unavailable(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "10")
	snapshotError(w, 503, "RANKING_UNAVAILABLE", "A completed ranking snapshot is temporarily unavailable.", 10, rankingGuidance{RetainLastResult: true})
}
func (h *Snapshots) visible(ctx context.Context, header ranker.Header, now time.Time) bool {
	revision, observed, err := h.visibility.CurrentRankingRevision(ctx)
	return err == nil && revision != "" && revision == header.Provenance.ModerationRevision && !observed.After(now) && now.Sub(observed) <= 60*time.Second && !observed.IsZero()
}
func (h *Snapshots) page(w http.ResponseWriter, r *http.Request) {
	category, err := snapshotCategory(chi.URLParam(r, "category"))
	if err != nil {
		snapshotError(w, 400, "INVALID_CATEGORY", "Choose a supported ranking category.", 0, rankingGuidance{Supported: []string{"trending", "breakout", "volume", "movers", "unusual", "new"}})
		return
	}
	limit, err := snapshotLimit(r.URL.Query().Get("limit"))
	if err != nil || r.URL.Query().Has("offset") {
		snapshotError(w, 400, "INVALID_PAGINATION", "Use limit 1..100 and a snapshot cursor.", 0, rankingGuidance{})
		return
	}
	if !h.admit(w) {
		return
	}
	defer func() { <-h.admission }()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	now := time.Now().UTC()
	token := r.URL.Query().Get("cursor")
	offset := int64(0)
	var header ranker.Header
	if token != "" {
		reference, e := h.cursor.ResolvePage(category, token, now)
		if errors.Is(e, ranker.ErrCursorExpired) {
			expired(w)
			return
		}
		if e != nil {
			snapshotError(w, 400, "INVALID_CURSOR", "The ranking cursor is invalid.", 0, rankingGuidance{})
			return
		}
		header, err = h.store.Header(ctx, reference.RunID, category)
		offset = reference.Offset
		if errors.Is(err, pgx.ErrNoRows) {
			expired(w)
			return
		}
		if err == nil {
			err = h.store.ValidateBoundary(ctx, header, reference)
			if errors.Is(err, ranker.ErrCursorInvalid) {
				snapshotError(w, 400, "INVALID_CURSOR", "The ranking cursor is invalid.", 0, rankingGuidance{})
				return
			}
		}
		if err == nil && (header.InputDigest != reference.InputDigest || header.OutputDigest != reference.OutputDigest || header.CutoffTime+900 != reference.Expires || offset >= header.Total) {
			snapshotError(w, 400, "INVALID_CURSOR", "The ranking cursor is invalid.", 0, rankingGuidance{})
			return
		}
	} else {
		header, err = h.store.Current(ctx, category)
	}
	if err != nil || !h.visible(ctx, header, now) {
		unavailable(w)
		return
	}
	var page ranker.StoredPage
	cached := false
	if h.cache != nil {
		cacheCtx, cacheStop := context.WithTimeout(ctx, 100*time.Millisecond)
		page, err = h.cache.Get(cacheCtx, header, offset, limit)
		cacheStop()
		cached = err == nil
	}
	if !cached {
		page, err = h.store.Page(ctx, header, offset, limit)
		if err != nil {
			unavailable(w)
			return
		}
		if h.cache != nil {
			cacheCtx, cacheStop := context.WithTimeout(ctx, 100*time.Millisecond)
			_ = h.cache.Put(cacheCtx, page, limit)
			cacheStop()
		}
	}
	etag := strings.TrimSuffix(header.ETag(), `"`) + ":" + strconv.FormatInt(offset, 10) + ":" + strconv.Itoa(limit) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=5, must-revalidate")
	w.Header().Set("X-Ranking-Snapshot", header.RunID)
	age := now.Unix() - header.CutoffTime
	if age < 0 {
		age = 0
	}
	w.Header().Set("Age", strconv.FormatInt(age, 10))
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	snapshot, e := store.WireSnapshot(header, page.Items)
	if e != nil {
		unavailable(w)
		return
	}
	var next *string
	if offset+int64(len(page.Items)) < header.Total && now.Unix() < header.CutoffTime+900 && len(page.Items) > 0 {
		cursor, e := h.cursor.EncodePage(header, page.Items[len(page.Items)-1])
		if e != nil {
			unavailable(w)
			return
		}
		next = &cursor
	}
	quality := "complete"
	staleReason := any(nil)
	if header.Stale(now) {
		quality = "stale"
		staleReason = "refresh-delayed"
	}
	revision := v1.PositiveUInt256(strconv.FormatInt(header.Revision, 10))
	watermark := header.Provenance.Watermark
	response := v1.RankingsResponse{Data: v1.RankingsData{Snapshot: snapshot}, GeneratedAt: now.Format("2006-01-02T15:04:05.000Z"), NextCursor: next, Quality: quality, RequestId: uuid.NewString(), Revision: &revision, Watermark: &watermark}
	sharedhttp.WriteJSON(w, 200, struct {
		v1.RankingsResponse
		Freshness map[string]any `json:"freshness"`
	}{response, map[string]any{"cutoffHeight": watermark.Height, "sourceObservedAt": watermark.ObservedAt, "generatedAt": snapshot.ComputedAt, "ageSeconds": age, "stale": header.Stale(now), "staleReason": staleReason, "retryAfterSeconds": 10}})
}
func expired(w http.ResponseWriter) {
	snapshotError(w, 410, "SNAPSHOT_EXPIRED", "Restart without a cursor to read the latest snapshot.", 0, rankingGuidance{Restart: true})
}
func (h *Snapshots) inspect(w http.ResponseWriter, r *http.Request) {
	category, err := snapshotCategory(chi.URLParam(r, "category"))
	if err != nil {
		snapshotError(w, 400, "INVALID_CATEGORY", "Choose a supported ranking category.", 0, rankingGuidance{Supported: []string{"trending", "breakout", "volume", "movers", "unusual", "new"}})
		return
	}
	if _, err = uuid.Parse(chi.URLParam(r, "runID")); err != nil {
		snapshotError(w, 400, "INVALID_SNAPSHOT", "The ranking snapshot identifier is invalid.", 0, rankingGuidance{})
		return
	}
	if !h.admit(w) {
		return
	}
	defer func() { <-h.admission }()
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	header, err := h.store.Header(ctx, chi.URLParam(r, "runID"), category)
	if err != nil || !h.visible(ctx, header, time.Now()) {
		unavailable(w)
		return
	}
	policy, _ := ranker.Policy(category)
	sharedhttp.WriteJSON(w, 200, map[string]any{"runId": header.RunID, "category": category, "algorithmVersion": header.AlgorithmVersion, "policy": policy, "inputDigest": header.InputDigest, "outputDigest": header.OutputDigest, "completedAt": header.CompletedAt, "provenance": header.Provenance})
}

type rankingGuidance struct {
	Supported        []string `json:"supported,omitempty"`
	Restart          bool     `json:"restart,omitempty"`
	RetainLastResult bool     `json:"retainLastResult,omitempty"`
}

func snapshotError(w http.ResponseWriter, status int, code, message string, retrySeconds int, guidance rankingGuidance) {
	body := v1.Error{Code: code, Message: message, RequestId: uuid.NewString(), Retryable: retrySeconds > 0, FieldErrors: []v1.FieldError{}}
	if retrySeconds > 0 {
		ms := v1.SafeInteger(retrySeconds * 1000)
		body.RetryAfterMs = &ms
	}
	sharedhttp.WriteJSON(w, status, struct {
		v1.Error
		rankingGuidance
	}{body, guidance})
}
