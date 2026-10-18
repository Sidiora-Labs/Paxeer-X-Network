// Package devsocial is the Postgres/Redis-backed v2 social surface of
// media/social: durable chat, comments, callouts, bounties and reports over the
// tables of migrations/00002 and the Redis fan-out. (The package name predates
// its use outside development.)
//
// Identity is injected through Deps.Authenticate:
//
//   - Development (NODE_ENV != production): media/social has no public ingress
//     and the gateway forwards with X-Actor-Wallet after authenticating the
//     session; Authenticate (this package) resolves that header. A request
//     without a valid actor is anonymous, and every write requires one.
//   - Production platform runtime (RUNTIME_PROFILE=platform, internal/app):
//     the service's own ingress strips every client identity header and
//     verifies the forwarded browser credential at the gateway identity
//     authority (/internal/v2/identity/request:authenticate); Deps.Authenticate
//     then returns that verified principal, whose Validate re-checks the
//     session (/internal/v2/identity/session:check). The chat replay-cursor
//     key comes from the SOCIAL_CURSOR_KEY secret (Deps.CursorKey).
//
// The bound-document production runtime (RUNTIME_PROFILE container or
// owned_local) does not use this package.
package devsocial

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/fanout"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
)

// ActorHeader is the gateway-attested actor wallet header.
const ActorHeader = "X-Actor-Wallet"

// SessionID marks a principal attested by the gateway rather than introspected
// by this service (development only).
const SessionID = "gateway-attested"

// principalLifetime bounds how long a WS connection's gateway-attested
// principal stays valid before the client must reconnect through the gateway.
const principalLifetime = 12 * time.Hour

// relayInterval is how often committed chat outbox rows are fanned out.
const relayInterval = 100 * time.Millisecond

// Deps are the development stores.
type Deps struct {
	Pool   *pgxpool.Pool
	Redis  *goredis.Client
	Logger *slog.Logger
	// ChannelNamespace prefixes the Redis fan-out channels (shared with the hub).
	ChannelNamespace string
	Clock            func() time.Time
	// Authenticate resolves the request principal for the v2 routes and chat
	// reads. Nil selects the development X-Actor-Wallet resolver
	// (Authenticate).
	Authenticate fanout.AuthenticateUpgrade
	// CursorKey is the chat replay-cursor HMAC key (>= 32 bytes). Nil keeps a
	// key generated once and persisted in chat.dev_runtime_keys.
	CursorKey []byte
	// Pools selects the indexer relation pool existence, token and creator
	// are read from (LegacyPools by default).
	Pools PoolSource
}

// Runtime owns the development v2 social stores and routes.
type Runtime struct {
	pool         *pgxpool.Pool
	redis        *goredis.Client
	logger       *slog.Logger
	clock        func() time.Time
	policy       *Policy
	interactions *interactions.HTTP
	authenticate fanout.AuthenticateUpgrade
	// Chat is the durable chat store; the development hub persists and
	// fans out through it.
	Chat *fanout.DurableStore
}

// New builds the runtime. Without Deps.CursorKey the chat replay-cursor HMAC
// key is generated once and persisted in chat.dev_runtime_keys so cursors
// survive restarts.
func New(ctx context.Context, d Deps) (*Runtime, error) {
	if d.Pool == nil || d.Redis == nil {
		return nil, errors.New("devsocial: postgres and redis are required")
	}
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	authenticate := d.Authenticate
	if authenticate == nil {
		authenticate = Authenticate
	}
	key := append([]byte(nil), d.CursorKey...)
	if d.CursorKey == nil {
		var err error
		if key, err = runtimeKey(ctx, d.Pool, "chat_cursor"); err != nil {
			return nil, err
		}
	} else if len(key) < 32 {
		return nil, errors.New("devsocial: cursor key too short")
	}
	policy := &Policy{DB: d.Pool, Clock: clock, Pools: d.Pools}
	store := interactions.Store{DB: d.Pool, Authority: policy}
	rt := &Runtime{
		pool:         d.Pool,
		redis:        d.Redis,
		logger:       d.Logger,
		clock:        clock,
		policy:       policy,
		authenticate: authenticate,
		// PGPolicy carries only the pool: the comment, reaction and hide paths
		// used here never consult its identity tables (privacy, blocks and
		// callout references, which do, are not mounted in development).
		interactions: &interactions.HTTP{Store: store, Policy: &interactions.PGPolicy{DB: d.Pool}, Cache: interactions.NewViewerCache(d.Redis)},
		Chat:         &fanout.DurableStore{Pool: d.Pool, Policy: policy.SendChat, CursorKey: key, ChannelNamespace: d.ChannelNamespace, Clock: d.Clock},
	}
	return rt, nil
}

func runtimeKey(ctx context.Context, pool *pgxpool.Pool, name string) ([]byte, error) {
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, err
	}
	if _, err := pool.Exec(ctx, `INSERT INTO chat.dev_runtime_keys(name,value) VALUES($1,$2) ON CONFLICT (name) DO NOTHING`, name, fresh); err != nil {
		return nil, fmt.Errorf("devsocial: store runtime key: %w", err)
	}
	var key []byte
	if err := pool.QueryRow(ctx, `SELECT value FROM chat.dev_runtime_keys WHERE name=$1`, name).Scan(&key); err != nil {
		return nil, fmt.Errorf("devsocial: load runtime key: %w", err)
	}
	if len(key) < 32 {
		return nil, errors.New("devsocial: runtime key too short")
	}
	return key, nil
}

// Authenticate resolves the gateway-attested actor of a request (HTTP or WS
// upgrade). It fails when X-Actor-Wallet is absent or malformed.
func Authenticate(r *http.Request) (fanout.Principal, error) {
	wallet := common.NormalizeAddr(r.Header.Get(ActorHeader))
	if !common.IsAddr(wallet) {
		return fanout.Principal{}, fanout.ErrNotFound
	}
	return fanout.Principal{Wallet: wallet, SessionID: SessionID, ExpiresAt: time.Now().Add(principalLifetime), Validate: gatewayAttested}, nil
}

// gatewayAttested is the principal re-validation of the development trust
// model: the gateway authenticated the session for this request and social has
// no identity authority of its own outside production.
func gatewayAttested(context.Context) error { return nil }

// ingress attaches the request principal (when present) for the fanout and
// interactions code paths, and rejects writes without one.
func (rt *Runtime) ingress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := interactions.WithHTTP(r.Context(), rt.interactions)
		principal, err := rt.authenticate(r)
		if err == nil {
			ctx = fanout.WithPrincipal(ctx, principal)
			ctx = interactions.WithIdentity(ctx, principal)
		} else if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if strings.HasPrefix(r.URL.Path, "/v2/social/") {
				interactionError(w, http.StatusUnauthorized, "AUTH_EXPIRED")
				return
			}
			writeError(w, r, http.StatusUnauthorized, "AUTH_EXPIRED", "sign in required")
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// actor returns the resolved actor of a request that passed ingress.
func actor(r *http.Request) (string, bool) {
	p, ok := fanout.PrincipalFromContext(r.Context())
	if !ok || !common.IsAddr(p.Wallet) {
		return "", false
	}
	return p.Wallet, true
}

// Register mounts the development v2 social routes on r.
func (rt *Runtime) Register(r chi.Router) error {
	var err error
	r.Group(func(g chi.Router) {
		g.Use(rt.ingress)
		if err = fanout.RegisterChatRoutes(g, rt.Chat, rt.authenticate); err != nil {
			return
		}
		g.Mount("/v2/social", rt.socialRoutes())

		g.Get("/v2/pools/{poolAddress}/comments", rt.listComments)
		g.Post("/v2/pools/{poolAddress}/comments", rt.createComment)
		g.Patch("/v2/comments/{commentId}", rt.editComment)
		g.Delete("/v2/comments/{commentId}", rt.deleteComment)
		g.Put("/v2/comments/{commentId}/likes/me", func(w http.ResponseWriter, r *http.Request) { rt.likeComment(w, r, false) })
		g.Delete("/v2/comments/{commentId}/likes/me", func(w http.ResponseWriter, r *http.Request) { rt.likeComment(w, r, true) })

		g.Post("/v2/callouts", rt.createCallout)
		g.Get("/v2/pools/{poolAddress}/callouts", rt.listCallouts)

		g.Get("/v2/bounties", rt.listBounties)
		g.Post("/v2/bounties", rt.createBounty)
		g.Get("/v2/bounties/stats", rt.bountyStats)
		g.Get("/v2/bounties/{bountyId}", rt.getBounty)
		g.Get("/v2/bounties/{bountyId}/submissions", rt.listSubmissions)
		g.Post("/v2/bounties/{bountyId}/submissions", rt.submit)
		g.Post("/v2/bounties/{bountyId}/submissions/{submissionId}:accept", rt.accept)

		g.Post("/v2/reports", rt.createReport)
	})
	return err
}

// Run fans committed chat outbox rows (messages, revisions, read markers) out
// to Redis until ctx ends. Delivery is at-least-once; the hub re-projects each
// event from the store before writing it to a socket.
func (rt *Runtime) Run(ctx context.Context) error {
	ticker := time.NewTicker(relayInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := rt.RelayOnce(ctx); err != nil && ctx.Err() == nil && rt.logger != nil {
				rt.logger.Warn("dev chat relay", slog.String("err", err.Error()))
			}
		}
	}
}

// RelayOnce drains up to one batch of each chat outbox.
func (rt *Runtime) RelayOnce(ctx context.Context) error {
	_, a := rt.Chat.DrainOutbox(ctx, 100, func(ctx context.Context, _ string, channels []string, payload []byte) error {
		for _, channel := range channels {
			if err := rt.redis.Publish(ctx, channel, payload).Err(); err != nil {
				return err
			}
		}
		return nil
	})
	_, b := rt.Chat.DrainControlOutbox(ctx, 100, func(ctx context.Context, _ string, wallet string, payload []byte) error {
		return rt.redis.Publish(ctx, fanout.WalletChannel(rt.Chat.ChannelNamespace, wallet), payload).Err()
	})
	return errors.Join(a, b)
}

// RuntimeDSN returns dsn with the "chat" schema first on the search_path (the
// development tables of migrations/00002 are unqualified in the shared fanout
// and interactions code, as in production where the pool's path is "chat").
// Any search_path already on the DSN is kept after it.
func RuntimeDSN(dsn string) (string, error) {
	parsed, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return "", err
	}
	path := "chat"
	if existing := strings.TrimSpace(parsed.RuntimeParams["search_path"]); existing != "" {
		path += "," + existing
	} else {
		path += ",public"
	}
	if u, e := url.Parse(dsn); e == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		q := u.Query()
		q.Set("search_path", path)
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	return dsn + " search_path='" + path + "'", nil
}

// pgxQuerier is satisfied by both a pool and a transaction.
type pgxQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
