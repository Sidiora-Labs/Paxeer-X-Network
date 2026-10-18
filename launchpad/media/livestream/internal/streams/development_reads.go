package streams

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

// BroadcastNotProvisionedCode is the stable contract error code returned by
// every provider-bound route (create, go-live, end, edit, key rotation, ingest
// credentials, private playback, operation status, provider webhooks) on a
// deployment that has no Livepeer/identity/Vault authority.
const BroadcastNotProvisionedCode = "BROADCAST_NOT_PROVISIONED"

// DevelopmentReadOptions are the real stores a non-production livestreamd owns.
type DevelopmentReadOptions struct {
	Pool        *pgxpool.Pool
	Redis       *goredis.Client
	Schema      string
	WalletLimit int
	GlobalLimit int
	// ViewerTokenKey signs anonymous viewer sessions. Fewer than 32 bytes means
	// a per-process random key: sessions then live only as long as the process.
	ViewerTokenKey []byte
}

// NewDevelopmentReads wires the public read surface (NewPublicReads) outside
// production. A ViewerTokenKey shorter than 32 bytes is replaced by a
// per-process random key: sessions then live only as long as the process.
// Callers must only use it outside production.
func NewDevelopmentReads(o DevelopmentReadOptions) (Deps, error) {
	if o.Pool == nil || o.Redis == nil {
		return Deps{}, ErrUnavailable
	}
	key := append([]byte(nil), o.ViewerTokenKey...)
	if len(key) < 32 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return Deps{}, err
		}
	}
	return NewPublicReads(PublicReadOptions{Pool: o.Pool, Redis: o.Redis, Schema: o.Schema, WalletLimit: o.WalletLimit, GlobalLimit: o.GlobalLimit, ViewerTokenKey: key})
}

// ReadSchemaReady reports whether the lifecycle read tables exist; it backs the
// readiness check of the read-surface runtimes (development and the production
// platform runtime).
func ReadSchemaReady(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	if pool == nil || !schemaPattern.MatchString(schema) {
		return ErrUnavailable
	}
	var present bool
	if err := pool.QueryRow(ctx, fmt.Sprintf("SELECT to_regclass('%s.stream_operations') IS NOT NULL AND to_regclass('%s.stream_playback_privacy') IS NOT NULL", schema, schema)).Scan(&present); err != nil || !present {
		return ErrUnavailable
	}
	return nil
}

// provisioned guards provider-bound routes. On a fully wired deployment it is
// a pass-through, so production and connected behaviour is unchanged.
func (h *Handlers) provisioned(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.unprovisioned {
			next(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeNotProvisioned(w)
	}
}

// writeNotProvisioned answers BROADCAST_NOT_PROVISIONED. It is a deployment
// state, not a dependency outage: the request cannot succeed on this
// deployment however often it is retried, so it is a non-retryable 409 that
// the gateway relays verbatim, never a 5xx that invites retries.
func writeNotProvisioned(w http.ResponseWriter) {
	writeContractError(w, http.StatusConflict, BroadcastNotProvisionedCode, "Livestream broadcasting is not provisioned in this environment", false, nil, &v2.ErrorDetails{Capability: v2.Optional[string]{Present: true, Value: "livestream.broadcast"}, Dependency: v2.Optional[string]{Present: true, Value: "livepeer"}})
}

// contractError maps lifecycle failures onto the v2 Error envelope.
func (h *Handlers) contractError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotProvisioned):
		w.Header().Set("Cache-Control", "no-store")
		writeNotProvisioned(w)
	case errors.Is(err, ErrViewerToken):
		writeContractError(w, http.StatusUnauthorized, "AUTH_EXPIRED", "Viewer session expired or invalid", false, nil, nil)
	case errors.Is(err, ErrState):
		writeContractError(w, http.StatusConflict, "STATE_CONFLICT", "Livestream state does not admit this request", false, nil, nil)
	case errors.Is(err, ErrDenied):
		writeContractError(w, http.StatusForbidden, "SCOPE_DENIED", "Livestream operation denied", false, nil, nil)
	case errors.Is(err, ErrInvalid):
		writeContractError(w, http.StatusUnprocessableEntity, "FIELD_INVALID", "Invalid livestream request", false, nil, nil)
	case errors.Is(err, ErrConflict):
		writeContractError(w, http.StatusConflict, "REVISION_CONFLICT", "Livestream revision conflict", false, nil, nil)
	case errors.Is(err, ErrQuota):
		w.Header().Set("Retry-After", "30")
		retry := v2.SafeInteger(30000)
		writeContractError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Livestream admission limit", true, &retry, nil)
	case errors.Is(err, pgx.ErrNoRows):
		writeContractError(w, http.StatusNotFound, "NOT_FOUND", "Livestream not found", false, nil, nil)
	case errors.Is(err, ErrMarketCapUnavailable):
		writeContractError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Market cap projection unavailable", true, nil, &v2.ErrorDetails{Dependency: v2.Optional[string]{Present: true, Value: "market_cap_projection"}})
	default:
		writeContractError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "Authoritative livestream dependency unavailable", true, nil, &v2.ErrorDetails{Dependency: v2.Optional[string]{Present: true, Value: "livestream_store"}})
	}
}

func writeContractError(w http.ResponseWriter, status int, code, message string, retryable bool, retryAfter *v2.SafeInteger, details *v2.ErrorDetails) {
	writeEnvelope(w, status, code, message, retryable, retryAfter, details, []v2.FieldError{})
}

// Request-body rules carried in FieldError.rule; the pointer is "" (the whole
// document) because the body never reached schema decoding.
const (
	ruleMediaType      = "media_type"
	ruleBodyUnreadable = "body_readable"
	ruleBodyTooLarge   = "max_bytes"
	ruleUTF8           = "utf8"
	ruleJSON           = "json"
)

// writeRequestError answers a body the handler could not admit (wrong media
// type 415, oversize 413, unreadable/invalid JSON 400) as FIELD_INVALID with a
// root-pointer field error. The status is kept; the client corrects the
// request and never retries it unchanged.
func writeRequestError(w http.ResponseWriter, status int, rule, message string) {
	writeEnvelope(w, status, "FIELD_INVALID", message, false, nil, nil, []v2.FieldError{{Pointer: "", Rule: rule}})
}

// writeRateLimited answers 429 RATE_LIMITED with Retry-After seconds and the
// matching retryAfterMs.
func writeRateLimited(w http.ResponseWriter, seconds int, message string) {
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	retry := v2.SafeInteger(seconds * 1000)
	writeContractError(w, http.StatusTooManyRequests, "RATE_LIMITED", message, true, &retry, nil)
}

// writeSignatureError answers a provider webhook whose signature or signed
// timestamp does not verify: 401 AUTH_EXPIRED without saying which check
// failed beyond the message.
func writeSignatureError(w http.ResponseWriter, message string) {
	writeContractError(w, http.StatusUnauthorized, "AUTH_EXPIRED", message, false, nil, nil)
}

func writeEnvelope(w http.ResponseWriter, status int, code, message string, retryable bool, retryAfter *v2.SafeInteger, details *v2.ErrorDetails, fieldErrors []v2.FieldError) {
	requestID, err := randomID()
	if err != nil {
		requestID = "00000000-0000-4000-8000-000000000000"
	}
	writeJSON(w, status, v2.Error{Code: code, Message: message, RequestId: requestID, Retryable: retryable, RetryAfterMs: retryAfter, FieldErrors: fieldErrors, Details: details})
}
