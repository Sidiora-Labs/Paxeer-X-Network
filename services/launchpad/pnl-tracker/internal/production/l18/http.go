package l18

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var errMalformed = errors.New("l18: malformed JSON")
var requestUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type HTTP struct {
	Service      *JourneyService
	Authenticate func(*http.Request) (Actor, error)
}

func (h HTTP) actor(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json")
	if h.Service == nil || h.Authenticate == nil {
		writeJourneyError(w, ErrUnavailable)
		return Actor{}, false
	}
	actor, e := h.Authenticate(r)
	if e != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "AUTHENTICATION_REQUIRED", "message": "Authentication required", "requestId": uuid.NewString(), "retryable": false, "retryAfterMs": nil, "details": nil, "fieldErrors": []any{}})
		return Actor{}, false
	}
	if _, e = wallet(actor.Wallet); e != nil {
		writeJourneyError(w, ErrUnauthorized)
		return Actor{}, false
	}
	return actor, true
}
func (h HTTP) Rewards(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	for k, v := range q {
		if k != "programId" && k != "cursor" && k != "limit" || len(v) != 1 {
			writeJourneyError(w, ErrInvalid)
			return
		}
	}
	limit := 50
	if q.Has("limit") {
		var e error
		limit, e = strconv.Atoi(q.Get("limit"))
		if e != nil {
			writeJourneyError(w, ErrInvalid)
			return
		}
	}
	if len(q.Get("programId")) > 64 || len(q.Get("cursor")) > 2048 {
		writeJourneyError(w, ErrInvalid)
		return
	}
	page, e := h.Service.Rewards(r.Context(), actor, q.Get("programId"), q.Get("cursor"), limit)
	if e != nil {
		writeJourneyError(w, e)
		return
	}
	response := iface.RewardsResponse{Data: iface.RewardsData{Items: page.Items}, GeneratedAt: h.Service.now().UTC().Format("2006-01-02T15:04:05.000Z"), NextCursor: page.NextCursor, Quality: "complete", RequestId: uuid.NewString(), Watermark: h.Service.watermark(page.Block)}
	raw, e := json.Marshal(response)
	if e == nil {
		e = iface.ValidateJSON("RewardsResponse", raw)
	}
	if e != nil {
		writeJourneyError(w, ErrUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
func (h HTTP) PrepareClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	actor, ok := h.actor(w, r)
	if !ok {
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 {
		writeJourneyError(w, ErrInvalid)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	raw, e := io.ReadAll(r.Body)
	if e != nil {
		writeJourneyError(w, ErrInvalid)
		return
	}
	if !json.Valid(raw) {
		writeJourneyError(w, errMalformed)
		return
	}
	request, e := iface.DecodeRewardClaimBody(raw)
	if e != nil {
		writeJourneyError(w, ErrInvalid)
		return
	}
	response, e := h.Service.Prepare(r.Context(), actor, r.Header.Get("Idempotency-Key"), request, uuid.NewString())
	if e != nil {
		writeJourneyError(w, e)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response)
}
func writeJourneyError(w http.ResponseWriter, e error) {
	status, code := http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE"
	switch {
	case errors.Is(e, errMalformed):
		status, code = http.StatusBadRequest, "MALFORMED_JSON"
	case errors.Is(e, ErrInvalid):
		status, code = http.StatusUnprocessableEntity, "INVALID_REQUEST"
	case errors.Is(e, ErrUnauthorized):
		status, code = http.StatusForbidden, "FORBIDDEN"
	case errors.Is(e, ErrConflict):
		status, code = http.StatusConflict, "IDEMPOTENCY_CONFLICT"
	case errors.Is(e, pgx.ErrNoRows):
		status, code = http.StatusNotFound, "NOT_FOUND"
	}
	message := http.StatusText(status)
	var detail any
	var capability *CapabilityError
	if errors.As(e, &capability) {
		message = capability.Reason
		detail = map[string]any{"capability": capability.Capability}
		code = "FEATURE_UNAVAILABLE"
	}
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "5")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "requestId": uuid.NewString(), "retryable": status == 503, "retryAfterMs": nil, "details": detail, "fieldErrors": []any{}, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
}
