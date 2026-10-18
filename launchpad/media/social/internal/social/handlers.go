// Package social implements the media/social HTTP handlers: pool chat history +
// edit/delete, DM conversation reads, the threaded comments feed, the followers
// graph, and the moderation (bans/reports) admin surface. Realtime sends (pool
// messages + DMs) are handled by the internal/fanout WS hub; this package owns
// the REST surface.
//
// Identity is sign-free: writes read the actor wallet from the trusted
// X-Actor-Wallet header that media/gateway injects after a one-time auth. Admin
// routes are gated by CHAT_ADMIN_API_KEY.
package social

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/pkg/types"
)

const (
	// defaultPageLimit / maxPageLimit bound history pagination (parity with TS).
	defaultPageLimit int32 = 50
	maxPageLimit     int32 = 100
	// bodyLimit bounds JSON request bodies.
	bodyLimit = 1 << 20
	// editMaxLen caps message/comment edits (parity with the TS 2000-char cap).
	editMaxLen = 2000
)

// Deps are the handler dependencies.
type Deps struct {
	Queries          sqlcdb.Querier
	MaxMessageLength int
	AdminAPIKey      string
	Logger           *slog.Logger
	// Clock defaults to time.Now.
	Clock func() time.Time
	// PoolCreator resolves a pool's creator for creator-authorized deletes.
	// Nil reads indexer.pools through Queries.GetPoolCreator.
	PoolCreator func(ctx context.Context, pool string) (string, error)
}

// Handlers serves the social REST routes.
type Handlers struct {
	q         sqlcdb.Querier
	maxMsgLen int
	adminKey  string
	logger    *slog.Logger
	clock     func() time.Time
	creator   func(ctx context.Context, pool string) (string, error)
}

// New constructs Handlers, applying defaults for optional dependencies.
func New(d Deps) *Handlers {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	maxLen := d.MaxMessageLength
	if maxLen <= 0 {
		maxLen = 500
	}
	creator := d.PoolCreator
	if creator == nil && d.Queries != nil {
		creator = d.Queries.GetPoolCreator
	}
	return &Handlers{
		q:         d.Queries,
		maxMsgLen: maxLen,
		adminKey:  d.AdminAPIKey,
		logger:    d.Logger,
		clock:     clock,
		creator:   creator,
	}
}

// RegisterRoutes mounts the social endpoints onto r.
func (h *Handlers) RegisterRoutes(r chi.Router) {
	// Pool chat history + moderation of own messages.
	r.Get("/pool/{poolAddress}/messages", h.listMessages)
	r.Patch("/pool/{poolAddress}/messages/{id}", h.editMessage)
	r.Delete("/pool/{poolAddress}/messages/{id}", h.deleteMessage)

	// Comments feed.
	r.Get("/pool/{poolAddress}/comments", h.listComments)
	r.Post("/pool/{poolAddress}/comments", h.createComment)
	r.Patch("/comments/{id}", h.editComment)
	r.Delete("/comments/{id}", h.deleteComment)
	r.Put("/comments/{id}/like", h.likeComment)
	r.Delete("/comments/{id}/like", h.unlikeComment)

	// Direct messages (reads; sends happen over WS).
	r.Get("/dm/conversations", h.listConversations)
	r.Get("/dm/conversations/{id}/messages", h.listDmMessages)

	// Followers graph.
	r.Put("/follow/{followee}", h.follow)
	r.Delete("/follow/{followee}", h.unfollow)
	r.Get("/users/{wallet}/followers", h.listFollowers)
	r.Get("/users/{wallet}/following", h.listFollowing)
	r.Get("/users/{wallet}/follow-stats", h.followStats)

	// Moderation: public report + admin-gated routes.
	r.Post("/report", h.report)
	r.Group(func(gr chi.Router) {
		gr.Use(h.adminAuth)
		gr.Delete("/admin/messages/{messageId}", h.adminDeleteMessage)
		gr.Post("/admin/bans", h.adminCreateBan)
		gr.Delete("/admin/bans/{banId}", h.adminDeleteBan)
		gr.Get("/admin/bans", h.adminListBans)
		gr.Get("/admin/reports", h.adminListReports)
		gr.Patch("/admin/reports/{reportId}", h.adminResolveReport)
	})
}

// ── Pool chat ─────────────────────────────────────────────────────────────────

func (h *Handlers) listMessages(w http.ResponseWriter, r *http.Request) {
	pool := normalizeAddr(chi.URLParam(r, "poolAddress"))
	if !addrRe.MatchString(pool) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid pool address")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), defaultPageLimit, maxPageLimit)
	rows, err := h.q.ListPoolMessages(r.Context(), sqlcdb.ListPoolMessagesParams{
		PoolAddress: pool,
		Before:      textValue(r.URL.Query().Get("before")),
		Lim:         limit,
	})
	if err != nil {
		h.fail(w, "list messages", err)
		return
	}
	msgs := make([]types.PoolMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- { // reverse to oldest-first within page
		row := rows[i]
		msgs = append(msgs, types.PoolMessage{
			ID:          row.ID,
			PoolAddress: row.PoolAddress,
			Sender:      row.Sender,
			Content:     row.Content,
			ReplyToID:   textPtr(row.ReplyToID),
			EditedAt:    int8Ptr(row.EditedAt),
			CreatedAt:   row.CreatedAt,
		})
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.MessagesResponse{
		Messages: msgs,
		HasMore:  int32(len(rows)) == limit,
	})
}

func (h *Handlers) editMessage(w http.ResponseWriter, r *http.Request) {
	pool := normalizeAddr(chi.URLParam(r, "poolAddress"))
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
	msg, err := h.q.GetPoolMessage(r.Context(), sqlcdb.GetPoolMessageParams{ID: id, PoolAddress: pool})
	if errors.Is(err, pgx.ErrNoRows) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Message not found")
		return
	}
	if err != nil {
		h.fail(w, "get message", err)
		return
	}
	if msg.Deleted {
		sharedhttp.WriteError(w, http.StatusGone, "Gone", "Message was deleted")
		return
	}
	if msg.Sender != act {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Can only edit your own messages")
		return
	}
	if err := h.q.UpdatePoolMessageContent(r.Context(), sqlcdb.UpdatePoolMessageContentParams{
		ID:       id,
		Content:  content,
		EditedAt: int8Value(h.clock().Unix()),
	}); err != nil {
		h.fail(w, "edit message", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

func (h *Handlers) deleteMessage(w http.ResponseWriter, r *http.Request) {
	pool := normalizeAddr(chi.URLParam(r, "poolAddress"))
	id := chi.URLParam(r, "id")
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	msg, err := h.q.GetPoolMessage(r.Context(), sqlcdb.GetPoolMessageParams{ID: id, PoolAddress: pool})
	if errors.Is(err, pgx.ErrNoRows) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Message not found")
		return
	}
	if err != nil {
		h.fail(w, "get message", err)
		return
	}
	if msg.Sender != act && !h.isPoolCreator(r.Context(), pool, act) {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Only message sender or pool creator can delete")
		return
	}
	if err := h.q.SoftDeletePoolMessage(r.Context(), id); err != nil {
		h.fail(w, "delete message", err)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.SuccessResponse{Success: true})
}

// ── Direct messages (reads) ───────────────────────────────────────────────────

func (h *Handlers) listConversations(w http.ResponseWriter, r *http.Request) {
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	rows, err := h.q.ListDmConversations(r.Context(), act)
	if err != nil {
		h.fail(w, "list conversations", err)
		return
	}
	out := make([]types.DmConversationSummary, 0, len(rows))
	for _, c := range rows {
		peer := c.WalletB
		if c.WalletA != act {
			peer = c.WalletA
		}
		out = append(out, types.DmConversationSummary{
			ID:            c.ID,
			Peer:          peer,
			LastMessageAt: int8Ptr(c.LastMessageAt),
		})
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.DmConversationsResponse{Conversations: out})
}

func (h *Handlers) listDmMessages(w http.ResponseWriter, r *http.Request) {
	act, ok := actor(r)
	if !ok {
		sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "missing or invalid "+actorHeader)
		return
	}
	id := chi.URLParam(r, "id")
	conv, err := h.q.GetDmConversation(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Conversation not found")
		return
	}
	if err != nil {
		h.fail(w, "get conversation", err)
		return
	}
	if conv.WalletA != act && conv.WalletB != act {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Not a participant")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"), defaultPageLimit, maxPageLimit)
	rows, err := h.q.ListDmMessages(r.Context(), sqlcdb.ListDmMessagesParams{
		ConversationID: id,
		Before:         textValue(r.URL.Query().Get("before")),
		Lim:            limit,
	})
	if err != nil {
		h.fail(w, "list dm messages", err)
		return
	}
	msgs := make([]types.DmMessage, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		msgs = append(msgs, types.DmMessage{
			ID:             row.ID,
			ConversationID: row.ConversationID,
			Sender:         row.Sender,
			Content:        row.Content,
			CreatedAt:      row.CreatedAt,
		})
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.DmMessagesResponse{
		Messages: msgs,
		HasMore:  int32(len(rows)) == limit,
	})
}

// ── shared helpers ────────────────────────────────────────────────────────────

// decodeEdit decodes + validates an EditRequest body, writing the error response
// itself and returning ok=false on failure.
func (h *Handlers) decodeEdit(w http.ResponseWriter, r *http.Request) (content string, ok bool) {
	var req types.EditRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, bodyLimit)).Decode(&req); err != nil {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return "", false
	}
	trimmed := strings.TrimSpace(req.Content)
	if trimmed == "" {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "content is required")
		return "", false
	}
	if len(trimmed) > editMaxLen {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "content too long")
		return "", false
	}
	return trimmed, true
}

// isPoolCreator reports whether wallet is the creator of pool (cross-schema read
// of the indexer, Deps.PoolCreator). A missing pool row yields false (not an
// error to the caller).
func (h *Handlers) isPoolCreator(ctx context.Context, pool, wallet string) bool {
	if h.creator == nil {
		return false
	}
	creator, err := h.creator(ctx, pool)
	if err != nil {
		return false
	}
	return normalizeAddr(creator) == wallet
}

// isBanned reports whether wallet currently has an active ban (global or for the
// given pool, when non-empty). Unavailable authority denies writes.
func (h *Handlers) isBanned(ctx context.Context, wallet, pool string) bool {
	banned, e := h.checkedBan(ctx, wallet, pool)
	return e != nil || banned
}

func (h *Handlers) checkedBan(ctx context.Context, wallet, pool string) (bool, error) {
	if store, _ := ctx.Value(moderationKey{}).(*safety.Store); store != nil {
		p, e := safety.Actor(ctx, h.clock())
		if e != nil {
			return false, e
		}
		if p.Wallet != wallet {
			return false, safety.ErrForbidden
		}
		scope, resource := "global", ""
		if pool != "" {
			scope, resource = "pool", pool
		}
		decision := store.Check(ctx, p.Wallet, scope, resource)
		if decision == safety.Unavailable {
			return false, safety.ErrUnavailable
		}
		if decision == safety.Deny {
			return true, nil
		}
		if decision != safety.Allow {
			return false, safety.ErrUnavailable
		}
		return false, nil
	}
	if h.q == nil {
		return false, safety.ErrUnavailable
	}
	var poolArg *string
	if pool != "" {
		poolArg = &pool
	}
	bans, e := h.q.ActiveBans(ctx, sqlcdb.ActiveBansParams{Wallet: wallet, Now: int8Value(h.clock().Unix()), PoolAddress: poolArg})
	if e != nil {
		h.logErr("active bans", e)
		return false, safety.ErrUnavailable
	}
	return len(bans) > 0, nil
}

// adminAuth gates /admin/* routes with a constant-time API-key comparison
// (parity with the TS timingSafeEqual check).
func (h *Handlers) adminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.adminKey == "" {
			sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "Admin API not configured")
			return
		}
		provided := r.Header.Get("X-API-Key")
		if provided == "" {
			sharedhttp.WriteError(w, http.StatusUnauthorized, "Unauthorized", "Missing API key")
			return
		}
		if subtle.ConstantTimeEq(int32(len(provided)), int32(len(h.adminKey))) != 1 ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(h.adminKey)) != 1 {
			sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Invalid API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) fail(w http.ResponseWriter, op string, err error) {
	h.logErr(op, err)
	sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "")
}

func (h *Handlers) logErr(op string, err error) {
	if h.logger != nil {
		h.logger.Error("social handler error", slog.String("op", op), slog.String("err", err.Error()))
	}
}
