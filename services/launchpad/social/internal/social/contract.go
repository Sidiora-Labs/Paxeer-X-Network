package social

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
)

// RegisterContractRoutes mounts the frozen v2 follow-graph paths over the
// chat.follows store for the non-connected runtime. IF_FOLLOWERS is not served
// here: its items are profiles, which this runtime has no authority for.
func (h *Handlers) RegisterContractRoutes(r chi.Router) {
	r.Get("/v2/profiles/{wallet}/follow-stats", h.contractFollowStats)
	r.Put("/v2/me/following/{wallet}", h.contractFollow)
	r.Delete("/v2/me/following/{wallet}", h.contractUnfollow)
}

func contractEnvelope(r *http.Request, data any, at time.Time) map[string]any {
	id, e := uuid.Parse(r.Header.Get("X-Request-ID"))
	if e != nil || id == uuid.Nil {
		id = uuid.New()
	}
	return map[string]any{"requestId": id.String(), "data": data, "nextCursor": nil, "watermark": nil, "revision": nil, "quality": "complete", "generatedAt": at.UTC().Format("2006-01-02T15:04:05.000Z")}
}

func (h *Handlers) writeContract(w http.ResponseWriter, r *http.Request, data any) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Add("Vary", "Authorization")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(contractEnvelope(r, data, h.clock()))
}

func (h *Handlers) contractFollowStats(w http.ResponseWriter, r *http.Request) {
	wallet := normalizeAddr(chi.URLParam(r, "wallet"))
	if !addrRe.MatchString(wallet) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	followers, err := h.q.CountFollowers(r.Context(), wallet)
	if err != nil {
		h.fail(w, "count followers", err)
		return
	}
	following, err := h.q.CountFollowing(r.Context(), wallet)
	if err != nil {
		h.fail(w, "count following", err)
		return
	}
	isFollowing := false
	if act, ok := actor(r); ok && act != wallet {
		isFollowing, err = h.q.IsFollowing(r.Context(), sqlcdb.IsFollowingParams{Follower: act, Followee: wallet})
		if err != nil {
			h.fail(w, "is following", err)
			return
		}
	}
	h.writeContract(w, r, v2.FollowStatsData{Followers: v2.SafeInteger(followers), Following: v2.SafeInteger(following), IsFollowing: isFollowing})
}

func (h *Handlers) contractFollow(w http.ResponseWriter, r *http.Request) {
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	followee := normalizeAddr(chi.URLParam(r, "wallet"))
	if !addrRe.MatchString(followee) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	if followee == act {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "Cannot follow yourself")
		return
	}
	if err := h.q.Follow(r.Context(), sqlcdb.FollowParams{Follower: act, Followee: followee, CreatedAt: h.clock().Unix()}); err != nil {
		h.fail(w, "follow", err)
		return
	}
	h.writeContract(w, r, v2.FollowData{Following: true})
}

func (h *Handlers) contractUnfollow(w http.ResponseWriter, r *http.Request) {
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	followee := normalizeAddr(chi.URLParam(r, "wallet"))
	if !addrRe.MatchString(followee) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	if err := h.q.Unfollow(r.Context(), sqlcdb.UnfollowParams{Follower: act, Followee: followee}); err != nil {
		h.fail(w, "unfollow", err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusNoContent)
}
