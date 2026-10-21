package auth

import (
	"errors"
	"net/http"
)

// PublicCode maps verifier failures onto existing frozen error codes. Detailed
// categories belong in private metrics, never wallet-existence responses.
func PublicCode(err error) (status int, code string, retryable bool) {
	if errors.Is(err, ErrScopeDenied) {
		return http.StatusForbidden, "SCOPE_DENIED", false
	}
	var failure *VerificationError
	if errors.As(err, &failure) && failure.Retryable {
		return http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", true
	}
	if errors.Is(err, ErrReplayMismatch) {
		return http.StatusConflict, "IDEMPOTENCY_CONFLICT", false
	}
	return http.StatusUnauthorized, "AUTH_EXPIRED", false
}
