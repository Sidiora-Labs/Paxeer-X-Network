package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/referral"
)

// RegisterReferrals wires the attribution event logger and the sharer dashboard.
func RegisterReferrals(r chi.Router, svc *referral.Service) {
	r.Post("/pnl/events", logEvent(svc))
	r.Get("/referrals/{address}/stats", sharerStats(svc))
}

type logEventBody struct {
	Type          string `json:"type"`
	WalletAddress string `json:"walletAddress"`
	CardID        string `json:"cardId"`
	ShortCode     string `json:"shortCode"`
}

func logEvent(svc *referral.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body logEventBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
			return
		}
		if body.Type == "" {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "type required")
			return
		}
		err := svc.Log(r.Context(), referral.Event{
			Type:          body.Type,
			ShortCode:     body.ShortCode,
			CardID:        body.CardID,
			WalletAddress: strings.ToLower(body.WalletAddress),
		})
		if errors.Is(err, referral.ErrUnknownReferral) {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "unknown referral")
			return
		}
		if err != nil {
			// Invalid event type is a client error; anything else is internal.
			if strings.Contains(err.Error(), "invalid event type") {
				sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid event type")
				return
			}
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "event log failed")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func sharerStats(svc *referral.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		address := strings.ToLower(chi.URLParam(r, "address"))
		stats, err := svc.Stats(r.Context(), address)
		if err != nil {
			sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "sharer stats failed")
			return
		}
		sharedhttp.WriteJSON(w, http.StatusOK, stats)
	}
}
