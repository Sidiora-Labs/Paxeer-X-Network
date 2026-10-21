package social

import (
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	"net/http"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/pkg/types"
)

// follow makes the actor follow {followee} (idempotent). Self-follow is rejected.
func (h *Handlers) follow(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "follow") {
		return
	}
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	followee := normalizeAddr(chi.URLParam(r, "followee"))
	if !addrRe.MatchString(followee) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid followee address")
		return
	}
	if followee == act {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "Cannot follow yourself")
		return
	}
	if err := h.q.Follow(r.Context(), sqlcdb.FollowParams{
		Follower:  act,
		Followee:  followee,
		CreatedAt: h.clock().Unix(),
	}); err != nil {
		h.fail(w, "follow", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

// unfollow removes the actor's follow edge (idempotent).
func (h *Handlers) unfollow(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "unfollow") {
		return
	}
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	followee := normalizeAddr(chi.URLParam(r, "followee"))
	if !addrRe.MatchString(followee) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid followee address")
		return
	}
	if err := h.q.Unfollow(r.Context(), sqlcdb.UnfollowParams{
		Follower: act,
		Followee: followee,
	}); err != nil {
		h.fail(w, "unfollow", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

// listFollowers lists wallets following {wallet}.
func (h *Handlers) listFollowers(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "followers") {
		return
	}
	wallet := normalizeAddr(chi.URLParam(r, "wallet"))
	if !addrRe.MatchString(wallet) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	rows, err := h.q.ListFollowers(r.Context(), wallet)
	if err != nil {
		h.fail(w, "list followers", err)
		return
	}
	out := make([]types.FollowEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, types.FollowEntry{Wallet: row.Follower, CreatedAt: row.CreatedAt})
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.FollowersResponse{WalletAddress: wallet, Followers: out})
}

// listFollowing lists wallets {wallet} follows.
func (h *Handlers) listFollowing(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "following") {
		return
	}
	wallet := normalizeAddr(chi.URLParam(r, "wallet"))
	if !addrRe.MatchString(wallet) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid wallet address")
		return
	}
	rows, err := h.q.ListFollowing(r.Context(), wallet)
	if err != nil {
		h.fail(w, "list following", err)
		return
	}
	out := make([]types.FollowEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, types.FollowEntry{Wallet: row.Followee, CreatedAt: row.CreatedAt})
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.FollowingResponse{WalletAddress: wallet, Following: out})
}

// followStats returns follower/following counts for {wallet}, plus whether the
// actor (when present) follows this wallet.
func (h *Handlers) followStats(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "stats") {
		return
	}
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
	sharedhttp.WriteJSON(w, http.StatusOK, types.FollowStatsResponse{
		WalletAddress: wallet,
		Followers:     followers,
		Following:     following,
		IsFollowing:   isFollowing,
	})
}
