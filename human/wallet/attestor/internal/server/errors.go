package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	CategoryToken   = "token"
	CategoryPolicy  = "policy"
	CategoryQuorum  = "quorum"
	CategorySession = "session"
	CategoryKey     = "key"
	CategoryStore   = "store"
)

const (
	CodeTokenMissing      = "token_missing"
	CodeTokenInvalid      = "token_invalid"
	CodeTokenNotOwner     = "token_not_owner"
	CodeTokenUnavailable  = "token_unavailable"
	CodeAgentInvalid      = "agent_invalid"
	CodeOperatorRequired  = "operator_required"
	CodePolicyDenied      = "policy_denied"
	CodeQuorumTooFew      = "quorum_too_few_signers"
	CodeQuorumNotMember   = "quorum_not_member"
	CodeQuorumSelfMissing = "quorum_self_missing"
	CodeSessionBadRequest = "session_bad_request"
	CodeSessionOpen       = "session_open_failed"
	CodeSessionFailed     = "session_failed"
	CodeSessionTimeout    = "session_timeout"
	CodeSessionKind       = "session_unsupported_kind"
	CodeKeyNotFound       = "key_not_found"
	CodeKeyExists         = "key_exists"
	CodeKeyCurve          = "key_curve_mismatch"
	CodeKeyNotRefreshed   = "key_not_refreshed"
	CodeKeyInvalidShare   = "key_invalid_share"
	CodeKeyImportDisabled = "key_import_disabled"
	CodeStoreFailed       = "store_failed"
	CodeStoreAuditFailed  = "store_audit_failed"
)

var errorCategories = map[string]string{
	CodeTokenMissing:      CategoryToken,
	CodeTokenInvalid:      CategoryToken,
	CodeTokenNotOwner:     CategoryToken,
	CodeTokenUnavailable:  CategoryToken,
	CodeAgentInvalid:      CategoryToken,
	CodeOperatorRequired:  CategoryToken,
	CodePolicyDenied:      CategoryPolicy,
	CodeQuorumTooFew:      CategoryQuorum,
	CodeQuorumNotMember:   CategoryQuorum,
	CodeQuorumSelfMissing: CategoryQuorum,
	CodeSessionBadRequest: CategorySession,
	CodeSessionOpen:       CategorySession,
	CodeSessionFailed:     CategorySession,
	CodeSessionTimeout:    CategorySession,
	CodeSessionKind:       CategorySession,
	CodeKeyNotFound:       CategoryKey,
	CodeKeyExists:         CategoryKey,
	CodeKeyCurve:          CategoryKey,
	CodeKeyNotRefreshed:   CategoryKey,
	CodeKeyInvalidShare:   CategoryKey,
	CodeKeyImportDisabled: CategoryKey,
	CodeStoreFailed:       CategoryStore,
	CodeStoreAuditFailed:  CategoryStore,
}

var errorStatus = map[string]int{
	CategoryToken:   http.StatusUnauthorized,
	CategoryPolicy:  http.StatusForbidden,
	CategoryQuorum:  http.StatusConflict,
	CategorySession: http.StatusBadRequest,
	CategoryKey:     http.StatusConflict,
	CategoryStore:   http.StatusInternalServerError,
}

func ErrorCodes() map[string]string {
	out := make(map[string]string, len(errorCategories))
	for code, category := range errorCategories {
		out[code] = category
	}
	return out
}

type Error struct {
	Category   string `json:"category"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	PolicyCode string `json:"policy_code,omitempty"`
}

func (e *Error) Error() string {
	return e.Category + ": " + e.Code + ": " + e.Message
}

func (e *Error) Status() int {
	switch e.Code {
	case CodeOperatorRequired:
		return http.StatusForbidden
	case CodeKeyNotFound:
		return http.StatusNotFound
	case CodeSessionTimeout:
		return http.StatusGatewayTimeout
	case CodeSessionFailed, CodeSessionOpen:
		return http.StatusBadGateway
	}
	if s, ok := errorStatus[e.Category]; ok {
		return s
	}
	return http.StatusInternalServerError
}

func newError(code string, format string, args ...any) *Error {
	category, ok := errorCategories[code]
	if !ok {
		category = CategoryStore
	}
	return &Error{Category: category, Code: code, Message: fmt.Sprintf(format, args...)}
}

func policyError(policyCode, reason string) *Error {
	e := newError(CodePolicyDenied, "%s", reason)
	e.PolicyCode = policyCode
	return e
}

type errorBody struct {
	Error *Error `json:"error"`
}

func writeError(w http.ResponseWriter, e *Error) {
	writeJSON(w, e.Status(), errorBody{Error: e})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
