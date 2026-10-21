package rest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
)

// writeComputedEnvelope emits a v2 envelope for read models computed directly
// from committed tables (no stream projection carries them). Every body is
// validated against the frozen contract before a byte leaves the handler.
func writeComputedEnvelope(w http.ResponseWriter, r *http.Request, name string, data any, watermark store.ChainWatermark, revision, quality string, next any) {
	body, err := json.Marshal(map[string]any{"requestId": responseRequestID(w), "data": data, "revision": revision, "watermark": watermark, "quality": quality, "nextCursor": next, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	if err != nil || len(body) > store.PublicJSONMaxBytes || interfaces.ValidateJSON(name, body) != nil {
		WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// writeComputedReadError maps read-model failures onto the frozen v2 codes.
func writeComputedReadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrDependencyNotConfigured):
		WriteV2NotConfiguredError(w, r)
	case errors.Is(err, store.ErrDataIntegrity), errors.Is(err, store.ErrWatermarkQuarantined):
		WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
	default:
		WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
	}
}

type leaderboardCursor struct {
	SnapshotID string `json:"snapshotId"`
	Offset     int    `json:"offset"`
	WindowEnd  int64  `json:"windowEnd"`
}

func encodeLeaderboardCursor(c leaderboardCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLeaderboardCursor(token string) (leaderboardCursor, error) {
	var c leaderboardCursor
	if token == "" || len(token) > 256 {
		return c, store.ErrCursorInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return c, store.ErrCursorInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&c) != nil || c.Offset < 0 || c.SnapshotID == "" || c.WindowEnd <= 0 {
		return c, store.ErrCursorInvalid
	}
	return c, nil
}

func leaderboards(model *store.AccountingReadModel) http.HandlerFunc {
	if model == nil {
		return leaderboardsFrom(nil)
	}
	return leaderboardsFrom(model.Leaderboard)
}

// leaderboardsFrom serves leaderboard pages from load; a nil load is a
// dependency this deployment does not configure (non-retryable 503).
func leaderboardsFrom(load func(ctx context.Context, category, window string, windowEnd int64) (*store.LeaderboardSnapshot, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		category := chi.URLParam(r, "category")
		if category != "trader" && category != "creator" && category != "volume" {
			WriteV2APIError(w, r, 404, "NOT_FOUND")
			return
		}
		q := r.URL.Query()
		for key, values := range q {
			if (key != "window" && key != "cursor" && key != "limit") || len(values) != 1 {
				WriteV2APIError(w, r, 422, "FIELD_UNKNOWN")
				return
			}
		}
		limit, err := boundedInteger(q.Get("limit"), 50, 1, 100)
		if err != nil {
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return
		}
		window := q.Get("window")
		if _, ok := store.LeaderboardWindowSeconds(window); !ok {
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return
		}
		shape := map[string]any{"category": category, "window": window, "limit": limit}
		token, hasCursor := q["cursor"]
		if hasCursor {
			if token[0] == "" {
				WriteV2APIError(w, r, 400, "CURSOR_INVALID")
				return
			}
			shape["cursor"] = token[0]
		}
		raw, _ := json.Marshal(shape)
		if interfaces.ValidateJSON("LeaderboardsRequest", raw) != nil {
			WriteV2APIError(w, r, 422, "FIELD_INVALID")
			return
		}
		if load == nil {
			WriteV2NotConfiguredError(w, r)
			return
		}
		var cursor leaderboardCursor
		if hasCursor {
			if cursor, err = decodeLeaderboardCursor(token[0]); err != nil {
				WriteV2APIError(w, r, 400, "CURSOR_INVALID")
				return
			}
		}
		snapshot, err := load(r.Context(), category, window, cursor.WindowEnd)
		if err != nil {
			writeComputedReadError(w, r, err)
			return
		}
		offset := 0
		if hasCursor {
			if cursor.SnapshotID != snapshot.SnapshotID {
				WriteV2APIError(w, r, 409, "RESNAPSHOT_REQUIRED")
				return
			}
			if cursor.Offset > len(snapshot.Entries) {
				WriteV2APIError(w, r, 400, "CURSOR_INVALID")
				return
			}
			offset = cursor.Offset
		}
		end := offset + limit
		if end > len(snapshot.Entries) {
			end = len(snapshot.Entries)
		}
		entries := []map[string]any{}
		for i, entry := range snapshot.Entries[offset:end] {
			entries = append(entries, entry.PublicEntry(offset+i+1))
		}
		var next any
		if end < len(snapshot.Entries) {
			next = encodeLeaderboardCursor(leaderboardCursor{SnapshotID: snapshot.SnapshotID, Offset: end, WindowEnd: snapshot.WindowEnd})
		}
		data := map[string]any{"snapshot": map[string]any{"snapshotId": snapshot.SnapshotID, "category": snapshot.Category, "methodologyVersion": snapshot.MethodologyVersion, "window": snapshot.Window, "entries": entries, "watermark": snapshot.Watermark}}
		writeComputedEnvelope(w, r, "LeaderboardsResponse", data, snapshot.Watermark, snapshot.Revision, snapshot.Quality, next)
	}
}
