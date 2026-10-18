package social

import (
	"context"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/voice"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/pkg/types"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WithModeration is a trusted app/middleware injection point. Legacy static-key
// gating remains an outer compatibility gate, but never supplies actor authority.
// Without this store and a verified D11 principal all moderation writes fail shut.
type moderationKey struct{}

func WithModeration(ctx context.Context, s *safety.Store) context.Context {
	return context.WithValue(ctx, moderationKey{}, s)
}
func moderation(r *http.Request) *safety.Store {
	s, _ := r.Context().Value(moderationKey{}).(*safety.Store)
	return s
}
func moderationError(w http.ResponseWriter, e error) {
	sharedhttp.WriteError(w, safety.HTTPStatus(e), "Moderation rejected", e.Error())
}
func decodeModeration(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		moderationError(w, safety.ErrInvalid)
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		moderationError(w, safety.ErrInvalid)
		return false
	}
	return true
}
func (h *Handlers) report(w http.ResponseWriter, r *http.Request) {
	s := moderation(r)
	if s == nil {
		moderationError(w, safety.ErrUnavailable)
		return
	}
	var req types.ReportRequest
	if !decodeModeration(w, r, &req) {
		return
	}
	c, e := s.Report(r.Context(), safety.Report{TargetType: "message", TargetID: req.MessageID, Category: req.Reason})
	if e != nil {
		moderationError(w, e)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.CreatedResponse{Success: true, ID: c.ID})
}
func (h *Handlers) adminCreateBan(w http.ResponseWriter, r *http.Request) {
	s := moderation(r)
	if s == nil {
		moderationError(w, safety.ErrUnavailable)
		return
	}
	var req types.BanRequest
	if !decodeModeration(w, r, &req) {
		return
	}
	var expires *time.Time
	if req.DurationSeconds != nil {
		if *req.DurationSeconds <= 0 || *req.DurationSeconds > 365*86400 {
			moderationError(w, safety.ErrInvalid)
			return
		}
		t := h.clock().Add(time.Duration(*req.DurationSeconds) * time.Second)
		expires = &t
	}
	scope, resource := "global", ""
	if req.PoolAddress != nil {
		resource = normalizeAddr(*req.PoolAddress)
		if !addrRe.MatchString(resource) {
			moderationError(w, safety.ErrInvalid)
			return
		}
		scope = "pool"
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	// banned_by is accepted only for legacy shape compatibility; it cannot become
	// the actor. The session principal is used in the immutable action record.
	id, e := s.Ban(r.Context(), safety.BanCommand{Wallet: normalizeAddr(req.Wallet), Scope: scope, Resource: resource, Reason: reason, PolicyVersion: "legacy-policy-v1", ExpiresAt: expires})
	if e != nil {
		moderationError(w, e)
		return
	}
	p, _ := safety.Actor(r.Context(), h.clock())
	var expiry *int64
	if expires != nil {
		v := expires.Unix()
		expiry = &v
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.BanResponse{OK: true, Ban: types.Ban{ID: id, Wallet: normalizeAddr(req.Wallet), PoolAddress: req.PoolAddress, Reason: req.Reason, BannedBy: p.Wallet, ExpiresAt: expiry, CreatedAt: h.clock().Unix()}})
}
func (h *Handlers) adminResolveReport(w http.ResponseWriter, r *http.Request) {
	s := moderation(r)
	if s == nil {
		moderationError(w, safety.ErrUnavailable)
		return
	}
	var req struct {
		Status           string `json:"status"`
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
		PolicyVersion    string `json:"policy_version"`
	}
	if !decodeModeration(w, r, &req) {
		return
	}
	next := req.Status
	if next == "resolved" {
		next = "actioned"
	}
	rev, e := s.Transition(r.Context(), chi.URLParam(r, "reportId"), req.ExpectedRevision, next, req.Reason, req.PolicyVersion)
	if e != nil {
		moderationError(w, e)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "revision": rev})
}

// Unsafe unrevisioned deletion and unrestricted admin listings are intentionally
// unavailable until coordinator wires resource-scoped versioned APIs.
func (h *Handlers) adminDeleteMessage(w http.ResponseWriter, r *http.Request) {
	moderationError(w, safety.ErrUnavailable)
}
func (h *Handlers) adminDeleteBan(w http.ResponseWriter, r *http.Request) {
	s := moderation(r)
	if s == nil {
		moderationError(w, safety.ErrUnavailable)
		return
	}
	var req struct {
		ExpectedRevision int64  `json:"expected_revision"`
		Reason           string `json:"reason"`
	}
	if !decodeModeration(w, r, &req) {
		return
	}
	if e := s.Revoke(r.Context(), chi.URLParam(r, "banId"), req.ExpectedRevision, req.Reason); e != nil {
		moderationError(w, e)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, types.OKResponse{OK: true})
}
func (h *Handlers) adminListBans(w http.ResponseWriter, r *http.Request) {
	s := moderation(r)
	if s == nil {
		moderationError(w, safety.ErrUnavailable)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, e := s.ListBans(r.Context(), r.URL.Query().Get("after"), limit)
	if e != nil {
		moderationError(w, e)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{"bans": items})
}
func (h *Handlers) adminListReports(w http.ResponseWriter, r *http.Request) {
	s := moderation(r)
	if s == nil {
		moderationError(w, safety.ErrUnavailable)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	state := r.URL.Query().Get("status")
	if state == "pending" {
		state = "open"
	}
	items, e := s.ListCases(r.Context(), state, r.URL.Query().Get("after"), limit)
	if e != nil {
		moderationError(w, e)
		return
	}
	sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{"reports": items})
}

func SafetyMiddleware(authority sharedauth.HTTPIdentityAuthority, store *safety.Store) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				moderationError(w, safety.ErrUnavailable)
				return
			}
			identity, e := authority.AuthenticateRequest(r.Context(), r)
			if e != nil {
				moderationError(w, safety.ErrForbidden)
				return
			}
			p, e := store.ResolvePrincipal(r.Context(), identity.Wallet, identity.AccountID, identity.SessionID, identity.AccessExpiresAt)
			if e != nil {
				moderationError(w, e)
				return
			}
			next.ServeHTTP(w, r.WithContext(safety.WithPrincipal(WithModeration(r.Context(), store), p)))
		})
	}
}
func RegisterSafetyRoutes(router chi.Router, store *safety.Store, control *voice.Control) {
	result := func(w http.ResponseWriter, data any, e error) {
		if e != nil {
			moderationError(w, e)
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, data)
	}
	router.Post("/v2/safety/reports", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TargetType  string `json:"targetType"`
			TargetID    string `json:"targetId"`
			Category    string `json:"category"`
			Description string `json:"description"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := store.Report(r.Context(), safety.Report{TargetType: body.TargetType, TargetID: body.TargetID, Category: body.Category, Description: body.Description})
		result(w, v, e)
	})
	router.Get("/v2/safety/cases", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		v, e := store.ListCases(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("after"), limit)
		result(w, v, e)
	})
	router.Patch("/v2/safety/cases/{caseId}", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Status           string `json:"status"`
			ExpectedRevision int64  `json:"expected_revision"`
			Reason           string `json:"reason"`
			PolicyVersion    string `json:"policy_version"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := store.Transition(r.Context(), chi.URLParam(r, "caseId"), body.ExpectedRevision, body.Status, body.Reason, body.PolicyVersion)
		result(w, map[string]int64{"revision": v}, e)
	})
	router.Post("/v2/safety/cases/{caseId}/bans", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Wallet           string     `json:"wallet"`
			Scope            string     `json:"scope"`
			Resource         string     `json:"resource"`
			ExpectedRevision int64      `json:"expectedRevision"`
			Reason           string     `json:"reason"`
			PolicyVersion    string     `json:"policyVersion"`
			ExpiresAt        *time.Time `json:"expiresAt"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := store.ActionBan(r.Context(), chi.URLParam(r, "caseId"), body.ExpectedRevision, safety.BanCommand{Wallet: body.Wallet, Scope: body.Scope, Resource: body.Resource, Reason: body.Reason, PolicyVersion: body.PolicyVersion, ExpiresAt: body.ExpiresAt})
		result(w, map[string]string{"banId": v}, e)
	})
	router.Post("/v2/safety/cases/{caseId}/restore", func(w http.ResponseWriter, r *http.Request) {
		var body safety.RestoreCommand
		if !decodeModeration(w, r, &body) {
			return
		}
		if body.CaseID != "" && body.CaseID != chi.URLParam(r, "caseId") {
			result(w, nil, safety.ErrInvalid)
			return
		}
		body.CaseID = chi.URLParam(r, "caseId")
		v, e := store.RestoreAppealedVisibility(r.Context(), body)
		result(w, v, e)
	})
	router.Post("/v2/safety/appeals", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ActionID string `json:"actionId"`
			Reason   string `json:"reason"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := store.Appeal(r.Context(), body.ActionID, body.Reason)
		if e != nil {
			result(w, nil, e)
			return
		}
		out, e := store.PrivateAppeal(r.Context(), v)
		result(w, out, e)
	})
	router.Get("/v2/safety/appeals/{appealId}", func(w http.ResponseWriter, r *http.Request) {
		v, e := store.PrivateAppeal(r.Context(), chi.URLParam(r, "appealId"))
		result(w, v, e)
	})
	router.Post("/v2/safety/appeals/{appealId}/reverse-ban", func(w http.ResponseWriter, r *http.Request) {
		var body safety.ReversalCommand
		if !decodeModeration(w, r, &body) {
			return
		}
		if body.AppealID != "" && body.AppealID != chi.URLParam(r, "appealId") {
			result(w, nil, safety.ErrInvalid)
			return
		}
		body.AppealID = chi.URLParam(r, "appealId")
		result(w, map[string]bool{"ok": true}, store.ReverseBanAppeal(r.Context(), body))
	})
	router.Get("/internal/v2/safety/decision", func(w http.ResponseWriter, r *http.Request) {
		p, e := safety.Actor(r.Context(), time.Now())
		if e != nil {
			result(w, nil, e)
			return
		}
		wallet := r.URL.Query().Get("wallet")
		scope := r.URL.Query().Get("scope")
		resource := r.URL.Query().Get("resource")
		if wallet != p.Wallet && !safety.Authorized(p, resource, false, time.Now()) {
			result(w, nil, safety.ErrForbidden)
			return
		}
		decision := store.Check(r.Context(), wallet, scope, resource)
		if decision == safety.Unavailable {
			result(w, nil, safety.ErrUnavailable)
			return
		}
		result(w, map[string]safety.Decision{"decision": decision}, nil)
	})
	router.Get("/internal/v2/safety/visibility", func(w http.ResponseWriter, r *http.Request) {
		if _, e := safety.Actor(r.Context(), time.Now()); e != nil {
			result(w, nil, e)
			return
		}
		v, e := store.Visibility(r.Context(), r.URL.Query().Get("targetType"), r.URL.Query().Get("targetId"))
		result(w, v, e)
	})
	if control == nil {
		return
	}
	router.Post("/v3/voice-rooms", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			PoolID string `json:"poolId"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := control.Create(r.Context(), body.PoolID)
		result(w, map[string]any{"roomId": v.ID, "state": v.APIState(), "revision": v.Revision}, e)
	})
	router.Post("/v2/voice-rooms/{roomId}/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := control.Start(r.Context(), chi.URLParam(r, "roomId"), body.ExpectedRevision)
		result(w, map[string]any{"roomId": v.ID, "state": v.APIState(), "revision": v.Revision}, e)
	})
	router.Post("/v2/voice-rooms/{roomId}/end", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := control.End(r.Context(), chi.URLParam(r, "roomId"), body.ExpectedRevision)
		result(w, map[string]any{"roomId": v.ID, "state": v.APIState(), "revision": v.Revision}, e)
	})
	router.Post("/v3/voice-rooms/{roomId}/moderation-actions", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Wallet           string `json:"wallet"`
			ExpectedRevision int64  `json:"expectedRevision"`
			Action           string `json:"action"`
			Reason           string `json:"reason"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		id := chi.URLParam(r, "roomId")
		if body.Action != "mute" && body.Action != "eject" && body.Action != "ban" || !safety.Bounded(body.Reason, 1000) {
			result(w, nil, safety.ErrInvalid)
			return
		}
		if body.Action == "ban" {
			if _, e := store.Ban(r.Context(), safety.BanCommand{Wallet: body.Wallet, Scope: "voice", Resource: id, Reason: body.Reason, PolicyVersion: control.SafetyPolicyVersion}); e != nil {
				result(w, nil, e)
				return
			}
		}
		result(w, map[string]bool{"ok": true}, control.Mute(r.Context(), id, body.Wallet, body.ExpectedRevision, body.Action != "mute"))
	})
	router.Get("/v2/voice-rooms/{roomId}/prompt", func(w http.ResponseWriter, r *http.Request) {
		p, e := safety.Actor(r.Context(), time.Now())
		if e != nil {
			result(w, nil, e)
			return
		}
		room, e := control.Read(r.Context(), chi.URLParam(r, "roomId"))
		if e != nil {
			result(w, nil, e)
			return
		}
		decision := store.Check(r.Context(), p.Wallet, "pool", room.PoolID)
		if decision == safety.Unavailable {
			result(w, nil, safety.ErrUnavailable)
			return
		}
		if decision != safety.Allow {
			result(w, nil, safety.ErrForbidden)
			return
		}
		var recording voice.Recording
		e = control.DB.QueryRow(r.Context(), `SELECT id,room_id,state,purpose,policy_version,revision,delete_at FROM voice_recordings WHERE room_id=$1 AND state IN ('consent_pending','starting','recording','stopping') ORDER BY created_at DESC LIMIT 1`, room.ID).Scan(&recording.ID, &recording.RoomID, &recording.State, &recording.Purpose, &recording.PolicyVersion, &recording.Revision, &recording.DeleteAt)
		var record any = recording
		if e == pgx.ErrNoRows {
			record = nil
			e = nil
		}
		if e != nil {
			result(w, nil, e)
			return
		}
		rows, e := control.DB.Query(r.Context(), `SELECT wallet,role,muted,revision FROM voice_participants WHERE room_id=$1 AND NOT revoked ORDER BY wallet LIMIT 50`, room.ID)
		if e != nil {
			result(w, nil, e)
			return
		}
		defer rows.Close()
		participants := []map[string]any{}
		for rows.Next() {
			var wallet, role string
			var muted bool
			var revision int64
			if e = rows.Scan(&wallet, &role, &muted, &revision); e != nil {
				result(w, nil, e)
				return
			}
			participants = append(participants, map[string]any{"wallet": wallet, "role": role, "muted": muted, "revision": revision})
		}
		if e = rows.Err(); e != nil {
			result(w, nil, e)
			return
		}
		result(w, map[string]any{"roomId": room.ID, "state": room.APIState(), "revision": room.Revision, "recording": record, "participants": participants}, nil)
	})
	router.Post("/v3/voice-rooms/{roomId}/tickets", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Consent *voice.JoinConsent `json:"consent"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		provider, ok := control.Provider.(*voice.LiveKit)
		if !ok {
			result(w, nil, safety.ErrUnavailable)
			return
		}
		id := chi.URLParam(r, "roomId")
		if e := control.JoinWithConsent(r.Context(), id, body.Consent); e != nil {
			result(w, nil, e)
			return
		}
		ticket, e := control.Ticket(r.Context(), id)
		result(w, map[string]any{"ticket": ticket, "providerEndpoint": "wss://" + strings.TrimPrefix(provider.Endpoint, "https://"), "expiresAt": time.Now().UTC().Add(time.Minute)}, e)
	})
	router.Get("/v2/voice-rooms/{roomId}/recording", func(w http.ResponseWriter, r *http.Request) {
		v, e := control.Recording(r.Context(), chi.URLParam(r, "roomId"))
		result(w, v, e)
	})
	router.Get("/v2/voice-rooms/{roomId}/recording/object", func(w http.ResponseWriter, r *http.Request) {
		object, e := control.OpenRecording(r.Context(), chi.URLParam(r, "roomId"))
		if e != nil {
			result(w, nil, e)
			return
		}
		defer object.Body.Close()
		w.Header().Set("Content-Type", "audio/ogg")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = io.Copy(w, object.Body)
	})
	router.Post("/v2/voice-rooms/{roomId}/recording", func(w http.ResponseWriter, r *http.Request) {
		v, e := control.RequestRecording(r.Context(), chi.URLParam(r, "roomId"))
		result(w, v, e)
	})
	router.Post("/v2/voice-rooms/{roomId}/recording/consent", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RecordingID      string `json:"recordingId"`
			ExpectedRevision int64  `json:"expectedRevision"`
			Agree            bool   `json:"agree"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		result(w, map[string]bool{"ok": true}, control.Consent(r.Context(), chi.URLParam(r, "roomId"), body.RecordingID, body.ExpectedRevision, body.Agree))
	})
	router.Post("/v2/voice-rooms/{roomId}/recording/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := control.StartConsentedRecording(r.Context(), chi.URLParam(r, "roomId"), body.ExpectedRevision)
		result(w, v, e)
	})
	router.Post("/v2/voice-rooms/{roomId}/recording/stop", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		v, e := control.StopConsentedRecording(r.Context(), chi.URLParam(r, "roomId"), body.ExpectedRevision)
		result(w, v, e)
	})
	router.Post("/v2/voice-rooms/{roomId}/speaker-requests", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		result(w, map[string]bool{"ok": true}, control.RequestSpeaker(r.Context(), chi.URLParam(r, "roomId"), body.ExpectedRevision))
	})
	router.Post("/v2/voice-rooms/{roomId}/speakers", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Wallet           string `json:"wallet"`
			ExpectedRevision int64  `json:"expectedRevision"`
		}
		if !decodeModeration(w, r, &body) {
			return
		}
		result(w, map[string]bool{"ok": true}, control.ApproveSpeaker(r.Context(), chi.URLParam(r, "roomId"), body.Wallet, body.ExpectedRevision))
	})
	router.Post("/v2/voice-rooms/{roomId}/leave", func(w http.ResponseWriter, r *http.Request) {
		result(w, map[string]bool{"ok": true}, control.Leave(r.Context(), chi.URLParam(r, "roomId")))
	})
}
