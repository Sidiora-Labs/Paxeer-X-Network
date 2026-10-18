package discovery

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Authenticate func(*http.Request) (string, error)
type HTTP struct {
	Feed         Feed
	Callouts     CalloutService
	Store        Store
	Authenticate Authenticate
	Intervals    IntervalAuthority
}

func (h HTTP) actor(r *http.Request) (string, error) {
	if h.Authenticate == nil {
		return "", ErrUnavailable
	}
	a, e := h.Authenticate(r)
	if e != nil || a == "" {
		return "", ErrForbidden
	}
	return a, nil
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		return ErrInvalid
	}
	return nil
}
func reply(w http.ResponseWriter, v any, e error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	if e != nil {
		status := 503
		if errors.Is(e, ErrInvalid) {
			status = 400
		} else if errors.Is(e, ErrForbidden) {
			status = 403
		} else if errors.Is(e, ErrConflict) {
			status = 409
		} else if errors.Is(e, sql.ErrNoRows) {
			status = 404
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "discovery_unavailable"})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
func (h HTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/discovery/feed", func(w http.ResponseWriter, r *http.Request) {
		account := ""
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			var e error
			account, e = h.actor(r)
			if e != nil {
				w.WriteHeader(401)
				return
			}
		}
		order := r.URL.Query().Get("order")
		if order == "" {
			order = "chronological"
		}
		limit := 50
		if s := r.URL.Query().Get("limit"); s != "" {
			var e error
			limit, e = strconv.Atoi(s)
			if e != nil {
				w.WriteHeader(400)
				return
			}
		}
		v, e := h.Feed.Page(r.Context(), account, order, r.URL.Query().Get("cursor"), limit, time.Now().UTC())
		reply(w, v, e)
	})
	mux.HandleFunc("GET /v2/discovery/feed/{id}", func(w http.ResponseWriter, r *http.Request) {
		account := ""
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			var e error
			account, e = h.actor(r)
			if e != nil {
				w.WriteHeader(401)
				return
			}
		}
		v, e := h.Feed.Detail(r.Context(), account, r.PathValue("id"), time.Now().UTC())
		reply(w, v, e)
	})
	mux.HandleFunc("POST /v2/discovery/callouts", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var input struct{ ID, PoolAddress, Body, Direction, Disclosure, PositionDisclosure, SquadID string }
		if e = decode(w, r, &input); e != nil {
			w.WriteHeader(400)
			return
		}
		next := Callout{ID: input.ID, AuthorAccountID: actor, PoolAddress: input.PoolAddress, Body: input.Body, Direction: input.Direction, Disclosure: input.Disclosure, PositionDisclosure: input.PositionDisclosure, ModerationStatus: "approved", Revision: 1, CreatedAt: time.Now().UTC()}
		e = h.Callouts.Publish(r.Context(), actor, input.SquadID, next)
		reply(w, next, e)
	})
	mux.HandleFunc("PATCH /v2/discovery/callouts/{id}", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var input struct {
			Body     string
			Expected uint64
			Delete   bool
		}
		if e = decode(w, r, &input); e != nil {
			w.WriteHeader(400)
			return
		}
		v, e := h.Callouts.Edit(r.Context(), actor, r.PathValue("id"), input.Body, input.Expected, input.Delete)
		reply(w, v, e)
	})
	mux.HandleFunc("PUT /v2/discovery/alerts/{id}", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var input struct {
			Rule     AlertRule
			Expected uint64
		}
		if e = decode(w, r, &input); e != nil {
			w.WriteHeader(400)
			return
		}
		input.Rule.OwnerAccountID = actor
		input.Rule.ID = r.PathValue("id")
		e = h.Store.SaveRule(r.Context(), actor, input.Rule, input.Expected)
		reply(w, input.Rule, e)
	})
	mux.HandleFunc("POST /v2/discovery/squads", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var input struct{ ID, Name, Policy string }
		if e = decode(w, r, &input); e != nil {
			w.WriteHeader(400)
			return
		}
		q := Squad{ID: input.ID, OwnerAccountID: actor, Name: input.Name, Policy: input.Policy, Revision: 1}
		e = h.Store.CreateSquad(r.Context(), actor, q)
		reply(w, q, e)
	})
	mux.HandleFunc("PATCH /v2/discovery/squads/{id}", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var in struct {
			Squad    Squad
			Expected uint64
		}
		if e = decode(w, r, &in); e != nil {
			w.WriteHeader(400)
			return
		}
		in.Squad.ID = r.PathValue("id")
		e = h.Store.UpdateSquad(r.Context(), actor, in.Squad, in.Expected)
		reply(w, in.Squad, e)
	})
	mux.HandleFunc("PATCH /v2/discovery/squads/{id}/members/{account}", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var in struct {
			Status   string
			Expected uint64
		}
		if e = decode(w, r, &in); e != nil {
			w.WriteHeader(400)
			return
		}
		e = h.Store.SetMemberStatus(r.Context(), actor, r.PathValue("id"), r.PathValue("account"), in.Status, in.Expected)
		reply(w, map[string]bool{"updated": e == nil}, e)
	})
	mux.HandleFunc("POST /v2/discovery/squads/{id}/invitations", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var in struct {
			Account, Token string
			Expires        time.Time
		}
		if e = decode(w, r, &in); e != nil {
			w.WriteHeader(400)
			return
		}
		e = h.Store.Invite(r.Context(), actor, r.PathValue("id"), in.Account, in.Token, in.Expires)
		reply(w, map[string]bool{"invited": e == nil}, e)
	})
	mux.HandleFunc("POST /v2/discovery/squads/{id}/accept", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var in struct {
			Token    string
			Expected uint64
		}
		if e = decode(w, r, &in); e != nil {
			w.WriteHeader(400)
			return
		}
		v, e := h.Store.AcceptInvitation(r.Context(), actor, r.PathValue("id"), in.Token, in.Expected, time.Now().UTC())
		reply(w, v, e)
	})
	mux.HandleFunc("POST /v2/discovery/squads/{id}/transfer", func(w http.ResponseWriter, r *http.Request) {
		actor, e := h.actor(r)
		if e != nil {
			w.WriteHeader(401)
			return
		}
		var in struct {
			Recipient string
			Expected  uint64
			Confirm   bool
		}
		if e = decode(w, r, &in); e != nil {
			w.WriteHeader(400)
			return
		}
		if in.Confirm {
			e = h.Store.ConfirmTransfer(r.Context(), actor, r.PathValue("id"))
		} else {
			e = h.Store.ProposeTransfer(r.Context(), actor, r.PathValue("id"), in.Recipient, in.Expected)
		}
		reply(w, map[string]bool{"accepted": e == nil}, e)
	})
	mux.HandleFunc("GET /v2/discovery/leaderboards", func(w http.ResponseWriter, r *http.Request) {
		start, e := time.Parse(time.RFC3339, r.URL.Query().Get("start"))
		if e != nil {
			w.WriteHeader(400)
			return
		}
		end, e := time.Parse(time.RFC3339, r.URL.Query().Get("end"))
		if e != nil {
			w.WriteHeader(400)
			return
		}
		v, e := RankIntervals(r.Context(), h.Intervals, start.UTC(), end.UTC())
		reply(w, v, e)
	})
	return mux
}
