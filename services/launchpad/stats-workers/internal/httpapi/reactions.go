package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
)

const (
	reactionsCacheTTL = 15 * time.Second
	voteCooldown      = 5 * time.Second
)

// validReactions is the allowed reaction set, in TS declaration order (governs
// JSON key order via reactionCounts).
var validReactions = []string{"bullish", "hot", "diamond", "bearish", "trash", "warning"}

func isValidReaction(r string) bool {
	for _, v := range validReactions {
		if v == r {
			return true
		}
	}
	return false
}

// reactionCounts holds the per-reaction tallies; field order matches
// validReactions so the JSON object key order is byte-stable.
type reactionCounts struct {
	Bullish int `json:"bullish"`
	Hot     int `json:"hot"`
	Diamond int `json:"diamond"`
	Bearish int `json:"bearish"`
	Trash   int `json:"trash"`
	Warning int `json:"warning"`
}

func (c reactionCounts) total() int {
	return c.Bullish + c.Hot + c.Diamond + c.Bearish + c.Trash + c.Warning
}

// countReactions reads the SCARD of every reaction set for a pool.
func countReactions(ctx context.Context, rdb *goredis.Client, pool string) (reactionCounts, error) {
	get := func(reaction string) (int, error) {
		n, err := rdb.SCard(ctx, "reactions:"+pool+":"+reaction).Result()
		return int(n), err
	}
	var c reactionCounts
	var err error
	if c.Bullish, err = get("bullish"); err != nil {
		return c, err
	}
	if c.Hot, err = get("hot"); err != nil {
		return c, err
	}
	if c.Diamond, err = get("diamond"); err != nil {
		return c, err
	}
	if c.Bearish, err = get("bearish"); err != nil {
		return c, err
	}
	if c.Trash, err = get("trash"); err != nil {
		return c, err
	}
	if c.Warning, err = get("warning"); err != nil {
		return c, err
	}
	return c, nil
}

// RegisterReactions registers the reaction read/write routes (Redis-only state).
func RegisterReactions(r chi.Router, rdb *goredis.Client) {
	r.Get("/stats/{poolAddress}/reactions", reactionsGet(rdb))
	r.Post("/stats/{poolAddress}/reactions", reactionsPost(rdb))
}

func reactionsGet(rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pool := strings.ToLower(chi.URLParam(r, "poolAddress"))
		ctx := r.Context()
		cacheKey := "reactions:" + pool

		if cached, err := rdb.Get(ctx, cacheKey).Result(); err == nil && cached != "" {
			sharedhttp.WriteJSON(w, http.StatusOK, json.RawMessage(cached))
			return
		}

		counts, err := countReactions(ctx, rdb, pool)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "reactions lookup failed")
			return
		}
		result := map[string]any{"poolAddress": pool, "reactions": counts, "total": counts.total()}
		if payload, err := json.Marshal(result); err == nil {
			rdb.Set(ctx, cacheKey, payload, reactionsCacheTTL)
		}
		sharedhttp.WriteJSON(w, http.StatusOK, result)
	}
}

type reactionBody struct {
	Reaction      string `json:"reaction"`
	WalletAddress string `json:"walletAddress"`
	Signature     string `json:"signature"`
	Message       string `json:"message"`
}

func reactionsPost(rdb *goredis.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pool := strings.ToLower(chi.URLParam(r, "poolAddress"))
		ctx := r.Context()

		var body reactionBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
			return
		}

		if body.WalletAddress == "" || body.Reaction == "" {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "walletAddress and reaction required")
			return
		}
		if body.Signature == "" || body.Message == "" {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "signature and message required for authentication")
			return
		}

		wallet := strings.ToLower(body.WalletAddress)
		if !auth.VerifyWalletSignature(wallet, body.Message, body.Signature) {
			sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Invalid signature")
			return
		}

		reaction := strings.ToLower(body.Reaction)
		if !isValidReaction(reaction) {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request",
				"Invalid reaction. Must be one of: "+strings.Join(validReactions, ", "))
			return
		}

		// Rate limit: 1 vote per cooldown window per wallet.
		cooldownKey := "reactions:cooldown:" + wallet
		if exists, err := rdb.Get(ctx, cooldownKey).Result(); err == nil && exists != "" {
			sharedhttp.WriteError(w, http.StatusTooManyRequests, "Too Many Requests", "Too many votes, slow down")
			return
		}
		rdb.Set(ctx, cooldownKey, "1", voteCooldown)

		setKey := "reactions:" + pool + ":" + reaction
		userVoteKey := "reactions:uservote:" + pool + ":" + wallet
		existingVote, getErr := rdb.Get(ctx, userVoteKey).Result()
		if getErr != nil {
			existingVote = ""
		}

		if existingVote == reaction {
			// Toggle off.
			rdb.SRem(ctx, setKey, wallet)
			rdb.Del(ctx, userVoteKey)
		} else {
			if existingVote != "" && isValidReaction(existingVote) {
				rdb.SRem(ctx, "reactions:"+pool+":"+existingVote, wallet)
			}
			rdb.SAdd(ctx, setKey, wallet)
			rdb.Set(ctx, userVoteKey, reaction, 0)
		}

		// Invalidate aggregate cache.
		rdb.Del(ctx, "reactions:"+pool)

		counts, err := countReactions(ctx, rdb, pool)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "reactions update failed")
			return
		}

		var userVote any
		if cv, err := rdb.Get(ctx, userVoteKey).Result(); err == nil && cv != "" {
			userVote = cv
		}

		// Push-first: publish the fresh aggregate tally on reactions:update so the
		// core/api broker fans it out and every viewer of this pool updates in
		// place. userVote is per-wallet so it is intentionally NOT broadcast — only
		// the shared tally is pushed. Best-effort, never fails the vote.
		if pushPayload, err := json.Marshal(map[string]any{
			"poolAddress": pool,
			"reactions":   counts,
			"total":       counts.total(),
		}); err == nil {
			_ = sharedredis.PublishJSON(ctx, rdb, constants.ChannelReactionsUpdate, pushPayload)
		}

		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{
			"poolAddress": pool,
			"reactions":   counts,
			"total":       counts.total(),
			"userVote":    userVote,
		})
	}
}

// IdentityAuthority verifies a forwarded browser credential and re-checks its
// session at the gateway identity authority (in production the shared mTLS
// client, sharedauth.MTLSIdentityAuthority, as issuer "stats").
type IdentityAuthority interface {
	AuthenticateRequest(context.Context, *http.Request) (auth.IdentityPrincipal, error)
	CheckStreamSession(context.Context, auth.StreamPrincipal) error
}

type DurableReactionDeps struct {
	Authority analytics.ReactionAuthority
	Ledger    auth.Ledger
	Identity  IdentityAuthority
	Verifier  auth.WalletOperationVerifier
	Policies  auth.PolicyRegistry
	Admission *ReactionAdmission
}
type ReactionAdmission struct {
	Redis                        *goredis.Client
	Namespace                    string
	WalletPerMinute, IPPerMinute int
}

func (a *ReactionAdmission) Admit(ctx context.Context, wallet, ip string) error {
	normalized, err := auth.NormalizeWallet(wallet)
	if err != nil || normalized != wallet {
		return analytics.ErrInvalidInput
	}
	if a == nil || a.Redis == nil || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(a.Namespace) || a.WalletPerMinute < 1 || a.WalletPerMinute > 12 || a.IPPerMinute < 1 || a.IPPerMinute > 4000 || net.ParseIP(ip) == nil {
		return analytics.ErrDependencyUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result, err := a.Redis.Eval(ctx, `local w=tonumber(redis.call('GET',KEYS[1]) or '0');local p=tonumber(redis.call('GET',KEYS[2]) or '0');if w>=tonumber(ARGV[1]) or p>=tonumber(ARGV[2]) then return 0 end;local wn=redis.call('INCR',KEYS[1]);if wn==1 then redis.call('PEXPIRE',KEYS[1],60000) end;local pn=redis.call('INCR',KEYS[2]);if pn==1 then redis.call('PEXPIRE',KEYS[2],60000) end;return 1`, []string{a.Namespace + ":{reaction-admission}:wallet:" + wallet, a.Namespace + ":{reaction-admission}:ip:" + ip}, a.WalletPerMinute, a.IPPerMinute).Int()
	if err != nil {
		return analytics.ErrDependencyUnavailable
	}
	if result != 1 {
		return analytics.ErrCooldown
	}
	return nil
}
func durableReactionError(w http.ResponseWriter, err error) {
	status, message := 503, "Authoritative analytics dependency unavailable"
	var verification *auth.VerificationError
	if errors.As(err, &verification) && !verification.Retryable {
		status, message = 403, "Reaction authorization denied"
	}
	// An identity authority that is unreachable or overloaded is a
	// dependency failure, not a denial (IdentityAuthorityError also unwraps
	// to ErrScopeDenied).
	var authority *auth.IdentityAuthorityError
	unavailable := errors.As(err, &authority) && (authority.Status == http.StatusServiceUnavailable || authority.Status == http.StatusTooManyRequests)
	switch {
	case unavailable:
	case errors.Is(err, analytics.ErrConflict), errors.Is(err, auth.ErrReplayMismatch):
		status, message = 409, "Reaction nonce or revision conflict"
	case errors.Is(err, analytics.ErrCooldown):
		status, message = 429, "Reaction admission limit"
	case errors.Is(err, auth.ErrScopeDenied):
		status, message = 403, "Reaction authorization denied"
	case errors.Is(err, analytics.ErrInvalidInput):
		status, message = 422, "Invalid analytics field"
	case errors.Is(err, pgx.ErrNoRows):
		status, message = 404, "Analytics snapshot not found"
	}
	sharedhttp.WriteError(w, status, http.StatusText(status), message)
}
func RegisterDurableReactions(r chi.Router, d DurableReactionDeps) error {
	policy, ok := d.Policies.Actions["reaction.replace"]
	if !ok || d.Policies.Version == "" || policy.Scope != "wallet" || !policy.OwnerRequired || policy.RouteTemplate != "/v2/stats/{poolAddress}/reactions" {
		return errors.New("explicit reaction wallet policy required")
	}
	r.Get("/v2/stats/{poolAddress}/reactions", func(w http.ResponseWriter, request *http.Request) {
		result, err := d.Authority.ReadReactions(request.Context(), strings.ToLower(chi.URLParam(request, "poolAddress")))
		if err != nil {
			durableReactionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		sharedhttp.WriteJSON(w, 200, result)
	})
	r.Post("/v2/stats/{poolAddress}/reactions", func(w http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), 5*time.Second)
		defer cancel()
		request = request.WithContext(ctx)
		raw, err := io.ReadAll(io.LimitReader(request.Body, 65537))
		if err != nil || len(raw) > 65536 {
			sharedhttp.WriteError(w, 413, "Payload Too Large", "bounded reaction body required")
			return
		}
		if _, err = auth.BodyDigest(raw); err != nil {
			sharedhttp.WriteError(w, 400, "Bad Request", "invalid reaction JSON")
			return
		}
		var intent analytics.Intent
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&intent) != nil || decoder.Decode(new(any)) != io.EOF {
			sharedhttp.WriteError(w, 400, "Bad Request", "invalid reaction intent")
			return
		}
		if _, err = intent.Canonical(); err != nil || intent.Pool != strings.ToLower(chi.URLParam(request, "poolAddress")) {
			sharedhttp.WriteError(w, 422, "Unprocessable Entity", "invalid reaction identity")
			return
		}
		if d.Identity == nil {
			durableReactionError(w, analytics.ErrDependencyUnavailable)
			return
		}
		principal, err := d.Identity.AuthenticateRequest(request.Context(), request)
		if err != nil {
			durableReactionError(w, err)
			return
		}
		if principal.Wallet != intent.Wallet {
			durableReactionError(w, auth.ErrScopeDenied)
			return
		}
		err = d.Policies.Authorize(auth.Principal{Wallet: principal.Wallet, SessionID: principal.SessionID, Roles: []string{"wallet"}}, "reaction.replace", intent.Wallet)
		if err != nil {
			durableReactionError(w, err)
			return
		}
		ip, _, err := net.SplitHostPort(request.RemoteAddr)
		if err != nil {
			durableReactionError(w, analytics.ErrDependencyUnavailable)
			return
		}
		if err = d.Admission.Admit(request.Context(), principal.Wallet, ip); err != nil {
			durableReactionError(w, err)
			return
		}
		encoded := request.Header.Get("X-Operation-Envelope")
		signature := request.Header.Get("X-Operation-Signature")
		if len(request.Header.Values("X-Operation-Envelope")) != 1 || len(request.Header.Values("X-Operation-Signature")) != 1 || len(encoded) > 8192 || len(signature) > 8194 || encoded == "" || signature == "" {
			durableReactionError(w, auth.ErrScopeDenied)
			return
		}
		if _, err = auth.BodyDigest([]byte(encoded)); err != nil {
			durableReactionError(w, auth.ErrScopeDenied)
			return
		}
		var envelope auth.OperationEnvelope
		ed := json.NewDecoder(strings.NewReader(encoded))
		ed.DisallowUnknownFields()
		if ed.Decode(&envelope) != nil || ed.Decode(new(any)) != io.EOF {
			durableReactionError(w, auth.ErrScopeDenied)
			return
		}
		if err = envelope.Validate(time.Now(), d.Ledger.Environment, principal.Wallet, "reaction.replace", request.Method, request.URL.Path, raw); err != nil {
			durableReactionError(w, auth.ErrScopeDenied)
			return
		}
		op := auth.OperationRequest{Envelope: envelope, Signature: signature, Body: raw, Actor: principal.Wallet, Action: "reaction.replace", Method: request.Method, Path: request.URL.Path, RequestID: uuid.NewString(), PolicyVersion: d.Policies.Version}
		verify := func(ctx context.Context, wallet, message, signature string) error {
			if wallet != principal.Wallet {
				return auth.ErrScopeDenied
			}
			if err := d.Identity.CheckStreamSession(ctx, principal.StreamPrincipal()); err != nil {
				return err
			}
			return d.Verifier.Verify(ctx, wallet, message, signature)
		}
		response, duplicate, err := d.Ledger.ExecuteValidated(request.Context(), op, verify, func(ctx context.Context, tx pgx.Tx, _ []byte) ([]byte, error) {
			result, err := d.Authority.ApplyOperationTx(ctx, tx, intent, op, d.Ledger.Environment, verify)
			if err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{"poolAddress": intent.Pool, "poolRevision": strconv.FormatInt(result.PoolRevision, 10), "revision": strconv.FormatInt(result.Revision, 10), "userVote": result.Vote, "reactions": result.Counts})
		})
		if err != nil {
			durableReactionError(w, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if duplicate {
			w.Header().Set("Idempotency-Replayed", "true")
		}
		sharedhttp.WriteJSON(w, 200, json.RawMessage(response))
	})
	return nil
}
