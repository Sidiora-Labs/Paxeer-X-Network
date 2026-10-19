package layerx

import (
	"context"
	"encoding/json"
	"errors"
)

type ErrorCode string

const (
	ErrorInvalidArgument       ErrorCode = "invalid-argument"
	ErrorIdempotencyRequired   ErrorCode = "idempotency-required"
	ErrorTransportFailure      ErrorCode = "transport-failure"
	ErrorDeadline              ErrorCode = "deadline"
	ErrorProtocolIncompatible  ErrorCode = "protocol-incompatibility"
	ErrorUnavailableCapability ErrorCode = "unavailable-capability"
	ErrorCoreRejection         ErrorCode = "core-rejection"
	ErrorVerificationFailure   ErrorCode = "verification-failure"
	ErrorPolicyRefusal         ErrorCode = "policy-refusal"
	ErrorCapabilityRefusal     ErrorCode = "capability-refusal"
	ErrorBudgetRefusal         ErrorCode = "budget-refusal"
	ErrorRateLimit             ErrorCode = "rate-limit"
	ErrorIdempotencyConflict   ErrorCode = "idempotency-conflict"
	ErrorDecodeFailure         ErrorCode = "decode-failure"
	ErrorUnknownOutcome        ErrorCode = "unknown-outcome"
	ErrorInternalFault         ErrorCode = "internal-fault"
)

type RetryClass string

const (
	RetryNever          RetryClass = "never"
	RetrySafe           RetryClass = "safe"
	RetryAfter          RetryClass = "after"
	RetryUnknownOutcome RetryClass = "unknown-outcome"
)

var (
	ErrInvalidArgument     = errors.New("layerx: invalid argument")
	ErrTransport           = errors.New("layerx: transport failure")
	ErrDeadline            = errors.New("layerx: deadline elapsed")
	ErrVerification        = errors.New("layerx: local verification failed")
	ErrProtocolRefusal     = errors.New("layerx: protocol refusal")
	ErrUnknownOutcome      = errors.New("layerx: unknown submission outcome")
	ErrIncompatibleService = errors.New("layerx: incompatible service")
)

var safeMessages = map[ErrorCode]string{
	ErrorInvalidArgument:       "The SDK rejected an invalid argument.",
	ErrorIdempotencyRequired:   "This operation requires an idempotency key.",
	ErrorTransportFailure:      "The request could not reach the service.",
	ErrorDeadline:              "The request deadline elapsed.",
	ErrorProtocolIncompatible:  "The service protocol is not compatible with this SDK.",
	ErrorUnavailableCapability: "The requested operation is unavailable.",
	ErrorCoreRejection:         "The protocol refused the request.",
	ErrorVerificationFailure:   "Local verification failed.",
	ErrorPolicyRefusal:         "Policy refused the request.",
	ErrorCapabilityRefusal:     "The caller does not have the required authority.",
	ErrorBudgetRefusal:         "The configured budget refused the request.",
	ErrorRateLimit:             "The request rate limit was reached.",
	ErrorIdempotencyConflict:   "The idempotency key belongs to a different request.",
	ErrorDecodeFailure:         "The service response did not match the contract.",
	ErrorUnknownOutcome:        "The request outcome is unknown and must be resolved before retrying.",
	ErrorInternalFault:         "The service could not complete the request.",
}

type SDKError struct {
	Code               ErrorCode  `json:"code"`
	Retry              RetryClass `json:"retry"`
	ServiceCode        string     `json:"service_code,omitempty"`
	RequestID          string     `json:"request_id,omitempty"`
	ProtocolResultCode *int32     `json:"protocol_result_code,omitempty"`
	RetryAfterMillis   *uint64    `json:"retry_after_ms,omitempty"`
	cause              error
}

func newSDKError(code ErrorCode, retry RetryClass) *SDKError {
	return &SDKError{Code: code, Retry: retry, cause: sentinelFor(code)}
}

func (sdkError *SDKError) Error() string {
	if sdkError == nil {
		return "LayerX SDK error"
	}
	if message, ok := safeMessages[sdkError.Code]; ok {
		return message
	}
	return "The LayerX SDK refused the operation."
}

func (sdkError *SDKError) Unwrap() error {
	if sdkError == nil {
		return nil
	}
	if sdkError.cause != nil {
		return sdkError.cause
	}
	return sentinelFor(sdkError.Code)
}

func (sdkError *SDKError) MarshalJSON() ([]byte, error) {
	type safe SDKError
	copy := safe(*sdkError)
	copy.cause = nil
	return json.Marshal(copy)
}

func sentinelFor(code ErrorCode) error {
	switch code {
	case ErrorInvalidArgument, ErrorIdempotencyRequired:
		return ErrInvalidArgument
	case ErrorDeadline:
		return ErrDeadline
	case ErrorVerificationFailure:
		return ErrVerification
	case ErrorCoreRejection, ErrorPolicyRefusal, ErrorCapabilityRefusal, ErrorBudgetRefusal:
		return ErrProtocolRefusal
	case ErrorUnknownOutcome:
		return ErrUnknownOutcome
	case ErrorProtocolIncompatible:
		return ErrIncompatibleService
	default:
		return ErrTransport
	}
}

func transportError(ctx context.Context, source error) *SDKError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(source, context.DeadlineExceeded) {
		return newSDKError(ErrorDeadline, RetrySafe)
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(source, context.Canceled) {
		return newSDKError(ErrorTransportFailure, RetrySafe)
	}
	return newSDKError(ErrorTransportFailure, RetrySafe)
}
