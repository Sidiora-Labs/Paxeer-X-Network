package account

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"time"
)

// WireError uses frozen IF_ERROR codes and JSON-pointer field errors.
type FieldError struct {
	Pointer string `json:"pointer"`
	Rule    string `json:"rule"`
}
type WireError struct {
	Code         string       `json:"code"`
	Message      string       `json:"message"`
	RequestID    string       `json:"requestId"`
	Retryable    bool         `json:"retryable"`
	RetryAfterMS *int         `json:"retryAfterMs"`
	FieldErrors  []FieldError `json:"fieldErrors"`
	Details      any          `json:"details"`
}

func PublicError(err error, requestID uuid.UUID) (int, WireError) {
	status, code := 503, "DEPENDENCY_UNAVAILABLE"
	switch {
	case errors.Is(err, errAuthExpired):
		status, code = 401, "AUTH_EXPIRED"
	case errors.Is(err, errResnapshot):
		status, code = 409, "RESNAPSHOT_REQUIRED"
	case errors.Is(err, ErrDenied):
		status, code = 403, "SCOPE_DENIED"
	case errors.Is(err, ErrConflict):
		status, code = 412, "REVISION_CONFLICT"
	case errors.Is(err, ErrState):
		status, code = 409, "STATE_CONFLICT"
	case errors.Is(err, ErrMissing):
		status, code = 404, "NOT_FOUND"
	case errors.Is(err, ErrInvalid):
		status, code = 422, "FIELD_INVALID"
	case errors.Is(err, ErrUnknown):
		status, code = 422, "FIELD_UNKNOWN"
	case errors.Is(err, ErrIdempotency):
		status, code = 409, "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, ErrCursor):
		status, code = 400, "CURSOR_INVALID"
	case err != nil && err.Error() == "FEATURE_UNAVAILABLE":
		status, code = 501, "FEATURE_UNAVAILABLE"
	}
	v := WireError{Code: code, Message: code, RequestID: requestID.String(), Retryable: status == 503, FieldErrors: []FieldError{}}
	if status == 422 {
		v.FieldErrors = []FieldError{{Pointer: "", Rule: code}}
	}
	var f ValidationError
	if errors.As(err, &f) {
		v.FieldErrors = []FieldError{{Pointer: f.Pointer, Rule: code}}
	}
	return status, v
}
func WriteError(w http.ResponseWriter, err error, requestID uuid.UUID) {
	status, v := PublicError(err, requestID)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Exact frozen request bodies; revision and fresh proof validation occurs before mutation.
type ExportBody struct {
	Format       string `json:"format"`
	IncludeMedia *bool  `json:"includeMedia"`
}

func (v ExportBody) Validate() error {
	if v.Format != "json" || v.IncludeMedia == nil {
		return ErrInvalid
	}
	return nil
}

type DeletionBody struct {
	ConfirmAccountID string `json:"confirmAccountId"`
	ExpectedRevision string `json:"expectedRevision"`
	ReauthProof      string `json:"reauthProof"`
}

func (v DeletionBody) Validate(p Principal) error {
	id, e := uuid.Parse(v.ConfirmAccountID)
	if e != nil || !validRevision(v.ExpectedRevision) || len(v.ReauthProof) < 1 || len(v.ReauthProof) > 256 {
		return ErrInvalid
	}
	return p.Own(id)
}

type WatchPutBody struct {
	AlertEnabled *bool `json:"alertEnabled"`
}

func (v WatchPutBody) Validate() error {
	if v.AlertEnabled == nil {
		return ErrInvalid
	}
	return nil
}

type WireWatchEntry struct {
	Wallet       string `json:"wallet"`
	PoolAddress  string `json:"poolAddress"`
	AddedAt      string `json:"addedAt"`
	Order        int    `json:"order"`
	AlertEnabled bool   `json:"alertEnabled"`
}

// WatchProjection requires service-owned absolute position and alert preference.
func WatchProjection(wallet string, e WatchEntry, order int, alert bool) (WireWatchEntry, error) {
	w, err := Wallet(wallet)
	if err != nil || order < 0 || order > 499 {
		return WireWatchEntry{}, ErrInvalid
	}
	return WireWatchEntry{w, e.Pool, e.AddedAt.UTC().Format("2006-01-02T15:04:05.000Z"), order, alert}, nil
}

// Envelope is IF_RESPONSE_ENVELOPE. A zero uninitialized record has no positive
// revision watermark, represented by null at the envelope level.
type Envelope[T any] struct {
	RequestID   string  `json:"requestId"`
	Data        T       `json:"data"`
	Watermark   any     `json:"watermark"`
	Revision    *string `json:"revision"`
	Quality     string  `json:"quality"`
	NextCursor  *string `json:"nextCursor"`
	GeneratedAt string  `json:"generatedAt"`
}

func Response[T any](id uuid.UUID, data T, revision string, at time.Time) (Envelope[T], error) {
	if !validRevision(revision) {
		return Envelope[T]{}, ErrInvalid
	}
	var r *string
	if revision != "0" {
		r = &revision
	}
	return Envelope[T]{RequestID: id.String(), Data: data, Revision: r, Quality: "complete", GeneratedAt: at.UTC().Format("2006-01-02T15:04:05.000Z")}, nil
}

type ValidationError struct {
	Pointer string
	Rule    error
}

func (v ValidationError) Error() string { return v.Rule.Error() }
func (v ValidationError) Unwrap() error { return v.Rule }

type RetentionReason struct {
	Domain                string  `json:"domain"`
	ReasonCode            string  `json:"reasonCode"`
	ExpiresAt             *string `json:"expiresAt"`
	IrreversibleChainData bool    `json:"irreversibleChainData"`
}
type DomainResult struct {
	Domain      string  `json:"domain"`
	State       string  `json:"state"`
	CompletedAt *string `json:"completedAt"`
	ErrorCode   *string `json:"errorCode"`
}
type WireAccountJob struct {
	JobID           string            `json:"jobId"`
	AccountID       string            `json:"accountId"`
	Kind            string            `json:"kind"`
	State           string            `json:"state"`
	RequestedAt     string            `json:"requestedAt"`
	ExpiresAt       *string           `json:"expiresAt"`
	DomainResults   []DomainResult    `json:"domainResults"`
	RetainedReasons []RetentionReason `json:"retainedReasons"`
	DownloadURL     *string           `json:"downloadUrl"`
}
type DeletionData struct {
	Job              WireAccountJob    `json:"job"`
	RetentionSummary []RetentionReason `json:"retentionSummary"`
}

func deletionProjection(j Job) DeletionData {
	at := j.RequestedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	retained := []RetentionReason{{Domain: "indexer", ReasonCode: "immutable_chain_fact", IrreversibleChainData: true}, {Domain: "security", ReasonCode: "required_audit"}}
	domains := []DomainResult{{Domain: "user", State: "completed", CompletedAt: &at}, {Domain: "sessions", State: "completed", CompletedAt: &at}, {Domain: "images", State: "pending"}, {Domain: "notifications", State: "pending"}}
	return DeletionData{Job: WireAccountJob{JobID: j.ID.String(), AccountID: j.AccountID.String(), Kind: "deletion", State: "running", RequestedAt: at, DomainResults: domains, RetainedReasons: retained}, RetentionSummary: retained}
}
