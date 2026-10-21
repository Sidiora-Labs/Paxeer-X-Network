package social

import (
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/pkg/types"
)

// listComments returns a page of a pool's threaded comments (newest-first), each
// with its like count. Public read.
func (h *Handlers) listComments(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "comments") {
		return
	}
	pool := normalizeAddr(chi.URLParam(r, "poolAddress"))
	if !addrRe.MatchString(pool) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid pool address")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), defaultPageLimit, maxPageLimit)
	rows, err := h.q.ListComments(r.Context(), sqlcdb.ListCommentsParams{
		PoolAddress: pool,
		Before:      textValue(r.URL.Query().Get("before")),
		Lim:         limit,
	})
	if err != nil {
		h.fail(w, "list comments", err)
		return
	}
	out := make([]types.Comment, 0, len(rows))
	for _, row := range rows {
		out = append(out, types.Comment{
			ID:          row.ID,
			PoolAddress: row.PoolAddress,
			Author:      row.Author,
			Content:     row.Content,
			ParentID:    textPtr(row.ParentID),
			EditedAt:    int8Ptr(row.EditedAt),
			CreatedAt:   row.CreatedAt,
			LikeCount:   row.LikeCount,
		})
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.CommentsResponse{
		Comments: out,
		HasMore:  int32(len(rows)) == limit,
	})
}

// createComment posts a new comment (optionally a reply via parent_id). Actor
// from the trusted header; banned actors are rejected.
func (h *Handlers) createComment(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "create") {
		return
	}
	pool := normalizeAddr(chi.URLParam(r, "poolAddress"))
	if !addrRe.MatchString(pool) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid pool address")
		return
	}
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}

	var req types.CreateCommentRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, bodyLimit)).Decode(&req); err != nil {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	content := sanitizeContent(req.Content)
	if content == "" || len(content) > h.maxMsgLen {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request",
			"Comment must be 1-"+itoa(h.maxMsgLen)+" chars")
		return
	}

	banned, err := h.checkedBan(r.Context(), act, pool)
	if err != nil {
		moderationError(w, err)
		return
	}
	if banned {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "You are banned from commenting")
		return
	}

	// Validate parent (must exist, belong to this pool, and not be deleted).
	var parentID *string
	if req.ParentID != nil && strings.TrimSpace(*req.ParentID) != "" {
		pid := strings.TrimSpace(*req.ParentID)
		parent, err := h.q.GetComment(r.Context(), pid)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (parent.Deleted || parent.PoolAddress != pool)) {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "parent comment not found")
			return
		}
		if err != nil {
			h.fail(w, "get parent comment", err)
			return
		}
		parentID = &pid
	}

	id := generateID(h.clock())
	if err := h.q.InsertComment(r.Context(), sqlcdb.InsertCommentParams{
		ID:          id,
		PoolAddress: pool,
		Author:      act,
		Content:     content,
		ParentID:    textValuePtr(parentID),
		CreatedAt:   h.clock().Unix(),
	}); err != nil {
		h.fail(w, "insert comment", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.CreatedResponse{Success: true, ID: id})
}

// editComment edits the actor's own comment.
func (h *Handlers) editComment(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "edit") {
		return
	}
	id := chi.URLParam(r, "id")
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	content, ok := h.decodeEdit(w, r)
	if !ok {
		return
	}
	c, err := h.q.GetComment(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Comment not found")
		return
	}
	if err != nil {
		h.fail(w, "get comment", err)
		return
	}
	if c.Deleted {
		sharedhttp.WriteError(w, http.StatusGone, "Gone", "Comment was deleted")
		return
	}
	if c.Author != act {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Can only edit your own comments")
		return
	}
	if err := h.q.UpdateCommentContent(r.Context(), sqlcdb.UpdateCommentContentParams{
		ID:       id,
		Content:  content,
		EditedAt: int8Value(h.clock().Unix()),
	}); err != nil {
		h.fail(w, "edit comment", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

// deleteComment soft-deletes a comment; allowed for the author or the pool creator.
func (h *Handlers) deleteComment(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "delete") {
		return
	}
	id := chi.URLParam(r, "id")
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	c, err := h.q.GetComment(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Comment not found")
		return
	}
	if err != nil {
		h.fail(w, "get comment", err)
		return
	}
	if c.Author != act && !h.isPoolCreator(r.Context(), c.PoolAddress, act) {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Only author or pool creator can delete")
		return
	}
	if err := h.q.SoftDeleteComment(r.Context(), id); err != nil {
		h.fail(w, "delete comment", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

// likeComment records the actor's like (idempotent).
func (h *Handlers) likeComment(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "like") {
		return
	}
	id := chi.URLParam(r, "id")
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	if _, err := h.q.GetComment(r.Context(), id); errors.Is(err, pgx.ErrNoRows) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Comment not found")
		return
	} else if err != nil {
		h.fail(w, "get comment", err)
		return
	}
	if err := h.q.LikeComment(r.Context(), sqlcdb.LikeCommentParams{
		CommentID: id,
		Wallet:    act,
		CreatedAt: h.clock().Unix(),
	}); err != nil {
		h.fail(w, "like comment", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

// unlikeComment removes the actor's like (idempotent).
func (h *Handlers) unlikeComment(w http.ResponseWriter, r *http.Request) {
	if interactions.HandleOwned(w, r, "unlike") {
		return
	}
	id := chi.URLParam(r, "id")
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	if err := h.q.UnlikeComment(r.Context(), sqlcdb.UnlikeCommentParams{
		CommentID: id,
		Wallet:    act,
	}); err != nil {
		h.fail(w, "unlike comment", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}
