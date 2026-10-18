package devsocial

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// maxBody bounds every v2 request body (the largest contract body, a bounty
// description, is 10 000 characters).
const maxBody = 64 << 10

func requestID(r *http.Request) string {
	if id, err := uuid.Parse(r.Header.Get("X-Request-ID")); err == nil && id != uuid.Nil {
		return id.String()
	}
	return uuid.NewString()
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

func headers(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
	w.Header().Add("Vary", "Authorization")
}

// writeData writes a v2 envelope after validating it against the named
// contract schema; a body that fails its own contract is never sent.
func writeData(w http.ResponseWriter, r *http.Request, status int, schema string, data any, next *string) {
	body, err := json.Marshal(map[string]any{"requestId": requestID(r), "data": data, "nextCursor": next, "watermark": nil, "revision": nil, "quality": "complete", "generatedAt": stamp(time.Now())})
	if err == nil {
		err = v2.ValidateJSON(schema, body)
	}
	if err != nil {
		writeError(w, r, http.StatusServiceUnavailable, "INTEGRITY_QUARANTINED", "response failed its contract")
		return
	}
	headers(w)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// writeError writes the contract Error shape.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	body, _ := json.Marshal(v2.Error{Code: code, Message: message, RequestId: requestID(r), Retryable: status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests, FieldErrors: []v2.FieldError{}})
	headers(w)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func noContent(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusNoContent)
}

// decode reads a JSON body, validates it against the named contract schema
// and decodes it into v.
func decode(w http.ResponseWriter, r *http.Request, schema string, v any) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err == nil {
		err = v2.ValidateJSON(schema, raw)
	}
	if err == nil {
		err = json.Unmarshal(raw, v)
	}
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", "request body does not match "+schema)
		return false
	}
	return true
}

// page parses the contract limit (1..100, default 50).
func page(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return 50, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 100 {
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", "limit must be 1..100")
		return 0, false
	}
	return n, true
}

// timeCursor is the keyset position of a (created_at DESC, id DESC) list.
type timeCursor struct {
	At time.Time `json:"t"`
	ID string    `json:"i"`
}

func encodeCursor(at time.Time, id string) *string {
	raw, _ := json.Marshal(timeCursor{At: at.UTC(), ID: id})
	s := base64.RawURLEncoding.EncodeToString(raw)
	return &s
}

// readCursor decodes the cursor query parameter; ok=false wrote CURSOR_INVALID.
func readCursor(w http.ResponseWriter, r *http.Request) (*timeCursor, bool) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return nil, true
	}
	var c timeCursor
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err == nil && len(raw) <= 256 {
		err = json.Unmarshal(b, &c)
	}
	if err != nil || len(raw) > 256 || c.At.IsZero() {
		writeError(w, r, http.StatusBadRequest, "CURSOR_INVALID", "cursor is not a position of this list")
		return nil, false
	}
	if _, e := uuid.Parse(c.ID); e != nil {
		writeError(w, r, http.StatusBadRequest, "CURSOR_INVALID", "cursor is not a position of this list")
		return nil, false
	}
	return &c, true
}

// storeError maps store errors onto the contract error codes.
func storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errUnauthenticated):
		writeError(w, r, http.StatusUnauthorized, "AUTH_EXPIRED", "sign in required")
	case errors.Is(err, interactions.ErrInvalid), errors.Is(err, interactions.ErrDepth), errors.Is(err, interactions.ErrReactionKind), errors.Is(err, errInvalid):
		writeError(w, r, http.StatusUnprocessableEntity, "FIELD_INVALID", err.Error())
	case errors.Is(err, interactions.ErrDenied), errors.Is(err, errDenied):
		writeError(w, r, http.StatusForbidden, "SCOPE_DENIED", "not permitted")
	case errors.Is(err, errIdempotency):
		writeError(w, r, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency key reused with a different request")
	case errors.Is(err, interactions.ErrRevision), errors.Is(err, errRevision):
		writeError(w, r, http.StatusConflict, "REVISION_CONFLICT", "revision is not current")
	case errors.Is(err, errState):
		writeError(w, r, http.StatusConflict, "STATE_CONFLICT", err.Error())
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, errNotFound):
		writeError(w, r, http.StatusNotFound, "NOT_FOUND", "not found")
	default:
		writeError(w, r, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "store unavailable")
	}
}

var (
	errUnauthenticated = errors.New("unauthenticated")
	errInvalid         = errors.New("invalid input")
	errDenied          = errors.New("denied")
	errIdempotency     = errors.New("idempotency conflict")
	errRevision        = errors.New("revision conflict")
	errState           = errors.New("state conflict")
	errNotFound        = errors.New("not found")
)

// interactionError writes the /v2/social error envelope (interactions.HTTP's).
func interactionError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "retryable": status == http.StatusServiceUnavailable})
}

// interactionData writes the /v2/social success envelope (interactions.HTTP's).
func interactionData(w http.ResponseWriter, data any, next *string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"requestId": uuid.NewString(), "data": data, "nextCursor": next, "watermark": nil, "revision": nil, "quality": "complete", "generatedAt": stamp(time.Now())})
}

// idempotencyKey is the client's Idempotency-Key, or a fresh one when absent
// (an absent key makes the write non-replayable, never shared).
func idempotencyKey(r *http.Request) (string, error) {
	v := r.Header.Get("Idempotency-Key")
	if v == "" {
		return "request:" + uuid.NewString(), nil
	}
	if len(v) > 128 {
		return "", errInvalid
	}
	for _, c := range v {
		if c < 0x20 || c == 0x7f {
			return "", errInvalid
		}
	}
	return v, nil
}
