package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// ErrorResponse is the JSON error envelope (parity with TS error-handler.ts).
type ErrorResponse struct {
	StatusCode int    `json:"statusCode"`
	Error      string `json:"error"`
	Message    string `json:"message"`
}

// WriteJSON writes v as a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Status and headers are already committed; the client likely
		// disconnected mid-write. Nothing further can be done.
		return
	}
}

// WriteError writes a JSON error. For 5xx the message is masked to avoid leaking
// internals (parity with the TS handler), otherwise the provided message is used.
func WriteError(w http.ResponseWriter, status int, name, message string) {
	if status >= http.StatusInternalServerError {
		name = "Internal Server Error"
		message = "An unexpected error occurred"
	}
	WriteJSON(w, status, ErrorResponse{StatusCode: status, Error: name, Message: message})
}

// Error codes from the v2 contract catalog
// (spec/production-system/integration/contracts/v2/catalog.json, section
// "error") that this package emits. The catalog has no METHOD_NOT_ALLOWED, so a
// 405 answers NOT_FOUND with a "method" field error.
const (
	CodeRateLimited           = "RATE_LIMITED"
	CodeNotFound              = "NOT_FOUND"
	CodeFieldInvalid          = "FIELD_INVALID"
	CodeDependencyUnavailable = "DEPENDENCY_UNAVAILABLE"
)

// Request-body rules carried in FieldError.rule. The pointer is "" (the whole
// document) because the body never reached schema decoding.
const (
	RuleJSON         = "json"
	RuleMaxBytes     = "max_bytes"
	RuleMediaType    = "media_type"
	RuleMultipart    = "multipart"
	RuleBodyReadable = "body_readable"
	RuleMethod       = "method"
	RulePrincipal    = "principal"
)

// maxRetryAfter is the Error schema's retryAfterMs maximum (60000).
const maxRetryAfter = 60 * time.Second

// maxMessageRunes is the Error schema's message maxLength.
const maxMessageRunes = 256

// ErrorEnvelope is the shared v2 Error body (code, message, requestId,
// retryable, retryAfterMs, fieldErrors, details).
type ErrorEnvelope = v2.Error

// WriteErrorEnvelope writes the v2 Error envelope with the given status.
// retryAfter > 0 sets retryAfterMs (clamped to the schema's 60000 ms
// maximum); zero writes null. Only Content-Type is set: callers own any
// Retry-After header. A valid UUID already in the X-Request-ID response
// header is reused as requestId; otherwise a fresh one is generated.
func WriteErrorEnvelope(w http.ResponseWriter, status int, code, message string, retryable bool, retryAfter time.Duration, fieldErrors []v2.FieldError, details *v2.ErrorDetails) {
	if fieldErrors == nil {
		fieldErrors = []v2.FieldError{}
	}
	var retryAfterMs *v2.SafeInteger
	if retryAfter > 0 {
		if retryAfter > maxRetryAfter {
			retryAfter = maxRetryAfter
		}
		ms := v2.SafeInteger(retryAfter.Milliseconds())
		retryAfterMs = &ms
	}
	if utf8.RuneCountInString(message) > maxMessageRunes {
		message = string([]rune(message)[:maxMessageRunes])
	}
	requestID := w.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(requestID); err != nil || len(requestID) != 36 {
		requestID = uuid.NewString()
	}
	WriteJSON(w, status, v2.Error{
		Code:         code,
		Message:      message,
		RequestId:    requestID,
		Retryable:    retryable,
		RetryAfterMs: retryAfterMs,
		FieldErrors:  fieldErrors,
		Details:      details,
	})
}

// WriteRequestBodyError answers a request body the handler could not admit
// (invalid JSON or multipart 400, oversize 413, wrong media type 415) as
// FIELD_INVALID with a root-pointer field error naming the rule. The status
// is kept; the client corrects the request and never retries it unchanged.
func WriteRequestBodyError(w http.ResponseWriter, status int, rule, message string) {
	WriteErrorEnvelope(w, status, CodeFieldInvalid, message, false, 0, []v2.FieldError{{Pointer: "", Rule: rule}}, nil)
}

// WriteRateLimited answers 429 RATE_LIMITED, retryable, with the Retry-After
// header in whole seconds and the matching retryAfterMs (clamped to the
// schema maximum of 60000).
func WriteRateLimited(w http.ResponseWriter, retryAfterSeconds int, message string) {
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	WriteErrorEnvelope(w, http.StatusTooManyRequests, CodeRateLimited, message, true, time.Duration(retryAfterSeconds)*time.Second, nil, nil)
}

// NotFoundHandler returns a JSON 404 NOT_FOUND envelope (chi NotFound).
func NotFoundHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteErrorEnvelope(w, http.StatusNotFound, CodeNotFound, "route not found", false, 0, nil, nil)
	}
}

// MethodNotAllowedHandler returns a JSON 405 (chi MethodNotAllowed). The
// contract catalog has no METHOD_NOT_ALLOWED code, so it answers NOT_FOUND
// with a root-pointer "method" field error; the 405 status is kept.
func MethodNotAllowedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		WriteErrorEnvelope(w, http.StatusMethodNotAllowed, CodeNotFound, "method not allowed", false, 0, []v2.FieldError{{Pointer: "", Rule: RuleMethod}}, nil)
	}
}
