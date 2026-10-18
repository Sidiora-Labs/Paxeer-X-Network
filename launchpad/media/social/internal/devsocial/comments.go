package devsocial

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Contract comment routes (IF_COMMENTS_LIST, IF_COMMENT_CREATE/EDIT/DELETE,
// IF_COMMENT_LIKE/UNLIKE) over interactions.Store and the interaction_comments
// table, the same rows /v2/social/pool/{p}/comments serves.

func contractComment(v interactions.CommentView) v2.Comment {
	return v2.Comment{CommentId: v.ID, PoolAddress: v.Pool, AuthorWallet: v.Author, ParentCommentId: v.Parent, Body: v.Body, LikeCount: v2.UInt256(v.Likes), Revision: v2.UInt256(v.Revision)}
}

func poolParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	pool := common.NormalizeAddr(chi.URLParam(r, "poolAddress"))
	if !common.IsAddr(pool) {
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", "poolAddress must be an address")
		return "", false
	}
	return pool, true
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (string, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil || len(chi.URLParam(r, name)) != 36 {
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", name+" must be a UUID")
		return "", false
	}
	return id.String(), true
}

func (rt *Runtime) listComments(w http.ResponseWriter, r *http.Request) {
	pool, ok := poolParam(w, r)
	if !ok {
		return
	}
	limit, ok := page(w, r)
	if !ok {
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil || len(cursor) != 36 {
			writeError(w, r, http.StatusBadRequest, "CURSOR_INVALID", "cursor is not a position of this list")
			return
		}
	}
	views, next, err := rt.interactions.CommentsPage(r.Context(), pool, limit, strings.ToLower(cursor))
	if err != nil {
		storeError(w, r, err)
		return
	}
	items := make([]v2.Comment, 0, len(views))
	for _, v := range views {
		items = append(items, contractComment(v))
	}
	writeData(w, r, http.StatusOK, "CommentsListResponse", v2.CommentsListData{Items: items}, next)
}

func (rt *Runtime) createComment(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	pool, ok := poolParam(w, r)
	if !ok {
		return
	}
	var body v2.CommentCreateBody
	if !decode(w, r, "CommentCreateBody", &body) {
		return
	}
	client, err := uuid.Parse(body.ClientCommentId)
	if err != nil {
		storeError(w, r, errInvalid)
		return
	}
	parent := ""
	if body.ParentCommentId.Present {
		parent = strings.ToLower(body.ParentCommentId.Value)
	}
	// The key matches interactions.HTTP's create, so a retry through either
	// route resolves to the same comment.
	in := interactions.Request{Actor: wallet, Pool: pool, Key: "comment:" + client.String(), Body: body.Body, Parent: parent}
	if err = rt.interactions.Store.CreateComment(r.Context(), in); err != nil {
		if err == interactions.ErrRevision {
			err = errIdempotency
		}
		storeError(w, r, err)
		return
	}
	var raw []byte
	if err = rt.pool.QueryRow(r.Context(), `SELECT payload FROM chat.interaction_mutations WHERE actor=$1 AND key=$2`, wallet, in.Key).Scan(&raw); err != nil {
		storeError(w, r, err)
		return
	}
	var saved interactions.Request
	if err = json.Unmarshal(raw, &saved); err != nil || saved.ID == "" {
		storeError(w, r, interactions.ErrDependency)
		return
	}
	view, err := rt.interactions.CommentByID(r.Context(), saved.ID)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, "CommentCreateResponse", v2.CommentCreateData{Comment: contractComment(view)}, nil)
}

func (rt *Runtime) editComment(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	id, ok := uuidParam(w, r, "commentId")
	if !ok {
		return
	}
	var body v2.CommentEditBody
	if !decode(w, r, "CommentEditBody", &body) {
		return
	}
	revision, err := strconv.ParseInt(string(body.ExpectedRevision), 10, 64)
	if err != nil {
		storeError(w, r, errRevision)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if err = rt.interactions.Store.EditComment(r.Context(), interactions.Request{Actor: wallet, ID: id, Key: key, Body: body.Body, Revision: revision}); err != nil {
		storeError(w, r, err)
		return
	}
	view, err := rt.interactions.CommentByID(r.Context(), id)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, "CommentEditResponse", v2.CommentEditData{Comment: contractComment(view)}, nil)
}

// deleteComment tombstones the caller's comment at its current revision
// (IF_COMMENT_DELETE carries no expected revision).
func (rt *Runtime) deleteComment(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	id, ok := uuidParam(w, r, "commentId")
	if !ok {
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		storeError(w, r, err)
		return
	}
	var revision int64
	if err = rt.pool.QueryRow(r.Context(), `SELECT revision FROM chat.interaction_comments WHERE id=$1 AND NOT removed`, id).Scan(&revision); err != nil {
		storeError(w, r, err)
		return
	}
	if err = rt.interactions.Store.EditComment(r.Context(), interactions.Request{Actor: wallet, ID: id, Key: key, Revision: revision, Remove: true}); err != nil {
		storeError(w, r, err)
		return
	}
	noContent(w)
}

func (rt *Runtime) likeComment(w http.ResponseWriter, r *http.Request, remove bool) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	id, ok := uuidParam(w, r, "commentId")
	if !ok {
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		storeError(w, r, err)
		return
	}
	if err = rt.interactions.Store.React(r.Context(), interactions.Request{Actor: wallet, ID: id, Kind: "like", Key: key, Remove: remove}); err != nil {
		storeError(w, r, err)
		return
	}
	if remove {
		noContent(w)
		return
	}
	count, err := rt.interactions.Store.Count(r.Context(), id, "like")
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusOK, "CommentLikeResponse", v2.CommentLikeData{LikeCount: v2.UInt256(strconv.FormatInt(count, 10)), Liked: true}, nil)
}
