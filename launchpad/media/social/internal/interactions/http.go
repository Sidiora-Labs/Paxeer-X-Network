package interactions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type HTTP struct {
	Store    Store
	Policy   *PGPolicy
	Cache    *ViewerCache
	Verifier ReceiptVerifier
}
type httpKey struct{}

func WithHTTP(ctx context.Context, h *HTTP) context.Context {
	return context.WithValue(ctx, httpKey{}, h)
}
func NewHTTP(store Store, policy *PGPolicy, cache *ViewerCache, verifier ReceiptVerifier) (*HTTP, error) {
	if store.DB == nil || policy == nil || policy.DB != store.DB || store.Authority != policy || policy.Publisher == nil || policy.Publisher.Delivery == nil || policy.Publisher.Watermark == nil || len(policy.Accounts) == 0 || len(policy.Pools) == 0 || len(policy.Sessions) == 0 || len(policy.IdentityAccounts) == 0 || len(policy.WalletLinks) == 0 || len(policy.SessionGuard) != 2 || len(policy.AccountGuard) != 2 || len(policy.PoolGuard) != 2 {
		return nil, ErrDependency
	}
	return &HTTP{store, policy, cache, verifier}, nil
}
func (h *HTTP) Routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/users/{wallet}/followers", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "followers") })
	r.Get("/users/{wallet}/following", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "following") })
	r.Get("/users/{wallet}/follow-stats", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "stats") })
	r.Put("/follow/{followee}", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "follow") })
	r.Delete("/follow/{followee}", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "unfollow") })
	r.Get("/pool/{poolAddress}/comments", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "comments") })
	r.Post("/pool/{poolAddress}/comments", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "create") })
	r.Patch("/comments/{id}", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "edit") })
	r.Delete("/comments/{id}", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "delete") })
	r.Post("/comments/{id}/hide", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "hide") })
	r.Put("/comments/{id}/like", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "like") })
	r.Delete("/comments/{id}/like", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "unlike") })
	r.Post("/callouts/{id}/references", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "callout") })
	r.Get("/me/social/privacy", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "privacy") })
	r.Patch("/me/social/privacy", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "set_privacy") })
	r.Put("/me/social/blocks/{wallet}", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "block") })
	r.Delete("/me/social/blocks/{wallet}", func(w http.ResponseWriter, r *http.Request) { h.handle(w, r, "unblock") })
	return r
}
func HandleOwned(w http.ResponseWriter, r *http.Request, operation string) bool {
	h, ok := r.Context().Value(httpKey{}).(*HTTP)
	if !ok {
		return false
	}
	h.handle(w, r, operation)
	return true
}
func httpError(w http.ResponseWriter, e error) {
	status, code := 503, "DEPENDENCY_UNAVAILABLE"
	switch {
	case errors.Is(e, ErrInvalid):
		status, code = 400, "INVALID_INPUT"
	case errors.Is(e, ErrDenied):
		status, code = 403, "SCOPE_DENIED"
	case errors.Is(e, ErrRevision):
		status, code = 409, "REVISION_CONFLICT"
	case errors.Is(e, ErrDepth):
		status, code = 422, "thread_depth_exceeded"
	case errors.Is(e, ErrReactionKind):
		status, code = 422, "unsupported_reaction_kind"
	case errors.Is(e, pgx.ErrNoRows):
		status, code = 404, "NOT_FOUND"
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "retryable": status == 503})
}
func decodeBody(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(&struct{}{}) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func writeResponse(w http.ResponseWriter, data any, next *string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"requestId": uuid.NewString(), "data": data, "nextCursor": next, "watermark": nil, "revision": nil, "quality": "complete", "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
}
func requestKey(r *http.Request) (string, error) {
	v := r.Header.Get("Idempotency-Key")
	if len(v) < 1 || len(v) > 128 || strings.ContainsAny(v, "\x00\n\r") {
		return "", ErrInvalid
	}
	return v, nil
}
func limitOf(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 50, nil
	}
	n, e := strconv.Atoi(raw)
	if e != nil || n < 1 {
		return 0, ErrInvalid
	}
	if n > 100 {
		n = 100
	}
	return n, nil
}
func (h *HTTP) handle(w http.ResponseWriter, r *http.Request, operation string) {
	if h == nil || h.Store.DB == nil || h.Policy == nil {
		httpError(w, ErrDependency)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Add("Vary", "Authorization")
	if operation == "stats" {
		viewer := ""
		if p, e := Identity(r.Context()); e == nil {
			viewer = p.Wallet
		} else if _, present := principalValue(r.Context()); present {
			httpError(w, e)
			return
		}
		v, tag, e := h.Store.FollowStats(r.Context(), chi.URLParam(r, "wallet"), viewer, h.Cache)
		if e != nil {
			httpError(w, e)
			return
		}
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeResponse(w, v, nil)
		return
	}
	if operation == "followers" || operation == "following" {
		limit, e := limitOf(r)
		if e != nil {
			httpError(w, e)
			return
		}
		page, e := h.visibleFollows(r.Context(), chi.URLParam(r, "wallet"), operation == "following", limit, r.URL.Query().Get("cursor"))
		if e != nil {
			httpError(w, e)
			return
		}
		var next *string
		if page.Next != "" {
			next = &page.Next
		}
		writeResponse(w, map[string]any{"items": page.Edges}, next)
		return
	}
	if operation == "comments" {
		limit, e := limitOf(r)
		if e != nil {
			httpError(w, e)
			return
		}
		v, next, tag, e := h.commentsSnapshot(r.Context(), chi.URLParam(r, "poolAddress"), limit, r.URL.Query().Get("cursor"))
		if e != nil {
			httpError(w, e)
			return
		}
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeResponse(w, map[string]any{"items": v}, next)
		return
	}
	principal, e := Identity(r.Context())
	if e != nil {
		httpError(w, e)
		return
	}
	if operation == "privacy" {
		v, e := h.Policy.Privacy(r.Context())
		if e != nil {
			httpError(w, e)
			return
		}
		writeResponse(w, v, nil)
		return
	}
	if operation == "set_privacy" {
		var body struct {
			Inbox    string `json:"inbox"`
			Revision string `json:"expectedRevision"`
		}
		if e = decodeBody(w, r, &body); e == nil {
			var revision int64
			revision, e = parseRevision(body.Revision)
			if e == nil {
				e = h.Policy.SetPrivacy(r.Context(), body.Inbox, revision)
			}
		}
		if e != nil {
			httpError(w, e)
			return
		}
		writeResponse(w, map[string]bool{"success": true}, nil)
		return
	}
	if operation == "block" || operation == "unblock" {
		if e = h.Policy.SetBlock(r.Context(), chi.URLParam(r, "wallet"), operation == "unblock"); e != nil {
			httpError(w, e)
			return
		}
		writeResponse(w, map[string]bool{"success": true}, nil)
		return
	}
	key, e := requestKey(r)
	if e != nil {
		httpError(w, e)
		return
	}
	input := Request{Actor: principal.Wallet, ID: chi.URLParam(r, "id"), Pool: chi.URLParam(r, "poolAddress"), Key: key}
	switch operation {
	case "follow", "unfollow":
		input.ID = chi.URLParam(r, "followee")
		input.Remove = operation == "unfollow"
		e = h.Store.Follow(r.Context(), input)
	case "create":
		var body struct {
			Body     string `json:"body"`
			ClientID string `json:"clientCommentId"`
			Parent   string `json:"parentCommentId"`
		}
		e = decodeBody(w, r, &body)
		if e == nil {
			if _, err := uuid.Parse(body.ClientID); err != nil {
				e = ErrInvalid
			} else {
				input.Key = "comment:" + body.ClientID
				input.Body = body.Body
				input.Parent = body.Parent
				e = h.Store.CreateComment(r.Context(), input)
			}
		}
	case "edit", "delete":
		var body struct {
			Body     string `json:"body"`
			Revision string `json:"expectedRevision"`
		}
		e = decodeBody(w, r, &body)
		if e == nil {
			input.Body = body.Body
			input.Revision, e = parseRevision(body.Revision)
			input.Remove = operation == "delete"
			if e == nil {
				e = h.Store.EditComment(r.Context(), input)
			}
		}
	case "hide":
		var body struct {
			Pool     string `json:"poolAddress"`
			Reason   string `json:"reason"`
			Revision string `json:"expectedRevision"`
		}
		e = decodeBody(w, r, &body)
		if e == nil {
			input.Pool = body.Pool
			input.Body = body.Reason
			input.Revision, e = parseRevision(body.Revision)
			if e == nil {
				e = h.Store.HideComment(r.Context(), input)
			}
		}
	case "like", "unlike":
		input.Kind = "like"
		input.Remove = operation == "unlike"
		e = h.Store.React(r.Context(), input)
	case "callout":
		var body struct {
			Revision    string   `json:"revision"`
			Mentions    []string `json:"mentions"`
			Transaction string   `json:"transactionHash"`
		}
		e = decodeBody(w, r, &body)
		if e == nil {
			input.Revision, e = parseRevision(body.Revision)
			if e == nil {
				e = h.Store.ReferenceCallout(r.Context(), input, body.Mentions, body.Transaction, h.Verifier)
			}
		}
	default:
		e = ErrInvalid
	}
	if e != nil {
		httpError(w, e)
		return
	}
	if operation == "create" {
		var raw []byte
		if e = h.Store.DB.QueryRow(r.Context(), `SELECT payload FROM interaction_mutations WHERE actor=$1 AND key=$2`, input.Actor, input.Key).Scan(&raw); e != nil {
			httpError(w, e)
			return
		}
		var saved Request
		if json.Unmarshal(raw, &saved) != nil {
			httpError(w, ErrDependency)
			return
		}
		view, e := h.comment(r.Context(), saved.ID)
		if e != nil {
			httpError(w, e)
			return
		}
		writeResponse(w, map[string]any{"comment": view}, nil)
		return
	}
	if operation == "edit" {
		view, e := h.comment(r.Context(), input.ID)
		if e != nil {
			httpError(w, e)
			return
		}
		writeResponse(w, map[string]any{"comment": view}, nil)
		return
	}
	if operation == "delete" || operation == "unlike" || operation == "unfollow" {
		w.Header().Set("Cache-Control", "private, no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if operation == "follow" {
		writeResponse(w, map[string]bool{"following": true}, nil)
		return
	}
	if operation == "like" {
		count, e := h.Store.Count(r.Context(), input.ID, "like")
		if e != nil {
			httpError(w, e)
			return
		}
		writeResponse(w, map[string]any{"likeCount": strconv.FormatInt(count, 10), "liked": true}, nil)
		return
	}
	writeResponse(w, map[string]bool{"success": true}, nil)
}

type CommentView struct {
	ID        string  `json:"commentId"`
	Pool      string  `json:"poolAddress"`
	Author    string  `json:"authorWallet"`
	Parent    *string `json:"parentCommentId"`
	Body      string  `json:"body"`
	Likes     string  `json:"likeCount"`
	Revision  string  `json:"revision"`
	Edited    bool    `json:"edited"`
	CountAsOf string  `json:"count_as_of"`
}

func (h *HTTP) comments(ctx context.Context, pool string, limit int, cursor string) ([]CommentView, *string, error) {
	out, next, _, e := h.commentsSnapshot(ctx, pool, limit, cursor)
	return out, next, e
}
func (h *HTTP) commentsSnapshot(ctx context.Context, pool string, limit int, cursor string) ([]CommentView, *string, string, error) {
	out := []CommentView{}
	pool, e := Wallet(pool)
	if e != nil {
		return out, nil, "", e
	}
	if len(cursor) > 160 || limit < 1 || limit > 100 {
		return out, nil, "", ErrInvalid
	}
	tx, e := h.Store.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, nil, "", e
	}
	defer rollback(tx)
	var revision, overlay int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT revision FROM interaction_visibility_revision WHERE scope=$1),0),COALESCE((SELECT revision FROM interaction_visibility_revision WHERE scope='erasure_overlay'),0)`, "pool:"+pool).Scan(&revision, &overlay); e != nil {
		return out, nil, "", e
	}
	tag := `"` + CacheKey(pool, "", "comments:"+strconv.Itoa(limit)+":"+cursor+":"+strconv.FormatInt(overlay, 10), revision) + `"`
	rows, e := tx.Query(ctx, `SELECT c.id,c.pool,c.author,c.parent,c.body,(SELECT count(*) FROM interaction_reactions r WHERE r.target=c.id AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=r.actor))::text,c.revision::text,c.edited_at IS NOT NULL,statement_timestamp() FROM interaction_comments c WHERE c.pool=$1 AND ($2='' OR c.id<$2) AND NOT c.removed AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=c.author) ORDER BY c.id DESC LIMIT $3`, pool, cursor, limit+1)
	if e != nil {
		return out, nil, "", e
	}
	defer rows.Close()
	for rows.Next() {
		var v CommentView
		var at time.Time
		if e = rows.Scan(&v.ID, &v.Pool, &v.Author, &v.Parent, &v.Body, &v.Likes, &v.Revision, &v.Edited, &at); e != nil {
			return out, nil, "", e
		}
		v.CountAsOf = at.UTC().Format(time.RFC3339Nano)
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return out, nil, "", e
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		n := out[limit-1].ID
		next = &n
	}
	return out, next, tag, nil
}
func (h *HTTP) visibleFollows(ctx context.Context, owner string, following bool, limit int, cursor string) (Page, error) {
	owner, e := Wallet(owner)
	if e != nil {
		return Page{}, e
	}
	if len(cursor) > 1024 {
		return Page{}, ErrInvalid
	}
	return h.Store.Follows(ctx, owner, following, limit, cursor)
}

func (h *HTTP) comment(ctx context.Context, id string) (CommentView, error) {
	var v CommentView
	e := h.Store.DB.QueryRow(ctx, `SELECT c.id,c.pool,c.author,c.parent,c.body,(SELECT count(*) FROM interaction_reactions r WHERE r.target=c.id AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=r.actor))::text,c.revision::text,c.edited_at IS NOT NULL FROM interaction_comments c WHERE c.id=$1 AND NOT c.removed AND NOT EXISTS(SELECT 1 FROM interaction_erased_wallets e WHERE e.wallet=c.author)`, id).Scan(&v.ID, &v.Pool, &v.Author, &v.Parent, &v.Body, &v.Likes, &v.Revision, &v.Edited)
	return v, e
}

func parseRevision(raw string) (int64, error) {
	const max = "115792089237316195423570985008687907853269984665640564039457584007913129639935"
	if !regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`).MatchString(raw) || len(raw) > len(max) || (len(raw) == len(max) && raw > max) {
		return 0, ErrInvalid
	}
	value, e := strconv.ParseInt(raw, 10, 64)
	if e != nil {
		return 0, ErrRevision
	}
	return value, nil
}

func (h *HTTP) AuthenticatedRoutes(authority sharedauth.HTTPIdentityAuthority) http.Handler {
	authenticate := fanout.VerifiedRequestAuthenticator(authority)
	routes := h.Routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		public := r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/users/") || (strings.HasPrefix(r.URL.Path, "/pool/") && strings.HasSuffix(r.URL.Path, "/comments")))
		_, cookieError := r.Cookie("__Host-kindle_access")
		if public && r.Header.Get("Authorization") == "" && cookieError != nil {
			routes.ServeHTTP(w, r)
			return
		}
		principal, e := authenticate(r)
		if e != nil {
			httpError(w, e)
			return
		}
		ctx := WithIdentity(r.Context(), principal)
		routes.ServeHTTP(w, r.WithContext(ctx))
	})
}
