package devsocial

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/common"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/interactions"
)

// socialRoutes is the /v2/social surface (the web's InteractionClient). The
// comment and like operations run the production interactions.HTTP handler
// over the development interaction tables; the follow graph is the
// development chat.follows table, so /v2/social, /v2/profiles/{w}/follow-stats,
// /v2/me/following/{w} and the legacy /follow routes all see one graph.
// Privacy, blocks and callout references need the production identity and
// chain-receipt authorities and are not mounted here.
func (rt *Runtime) socialRoutes() http.Handler {
	r := chi.NewRouter()
	owned := func(op string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !interactions.HandleOwned(w, r, op) {
				interactionError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
			}
		}
	}
	r.Get("/users/{wallet}/followers", func(w http.ResponseWriter, r *http.Request) { rt.socialFollows(w, r, false) })
	r.Get("/users/{wallet}/following", func(w http.ResponseWriter, r *http.Request) { rt.socialFollows(w, r, true) })
	r.Get("/users/{wallet}/follow-stats", rt.socialFollowStats)
	r.Put("/follow/{followee}", func(w http.ResponseWriter, r *http.Request) { rt.socialFollow(w, r, false) })
	r.Delete("/follow/{followee}", func(w http.ResponseWriter, r *http.Request) { rt.socialFollow(w, r, true) })
	r.Get("/pool/{poolAddress}/comments", owned("comments"))
	r.Post("/pool/{poolAddress}/comments", owned("create"))
	r.Patch("/comments/{id}", owned("edit"))
	r.Delete("/comments/{id}", owned("delete"))
	r.Post("/comments/{id}/hide", owned("hide"))
	r.Put("/comments/{id}/like", owned("like"))
	r.Delete("/comments/{id}/like", owned("unlike"))
	return r
}

type followEdge struct {
	Wallet    string    `json:"wallet"`
	CreatedAt time.Time `json:"created_at"`
}

type followCursor struct {
	At     int64  `json:"at"`
	Wallet string `json:"wallet"`
}

func (rt *Runtime) socialFollowStats(w http.ResponseWriter, r *http.Request) {
	wallet := common.NormalizeAddr(chi.URLParam(r, "wallet"))
	if !common.IsAddr(wallet) {
		interactionError(w, http.StatusBadRequest, "INVALID_INPUT")
		return
	}
	viewer, _ := actor(r)
	var followers, following int64
	var isFollowing bool
	err := rt.pool.QueryRow(r.Context(), `SELECT (SELECT count(*) FROM chat.follows WHERE followee=$1),(SELECT count(*) FROM chat.follows WHERE follower=$1),EXISTS(SELECT 1 FROM chat.follows WHERE follower=NULLIF($2,'') AND followee=$1)`, wallet, viewer).Scan(&followers, &following, &isFollowing)
	if err != nil {
		interactionError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
		return
	}
	interactionData(w, interactions.FollowStats{Wallet: wallet, Followers: strconv.FormatInt(followers, 10), Following: strconv.FormatInt(following, 10), IsFollowing: isFollowing, Revision: "0"}, nil)
}

func (rt *Runtime) socialFollows(w http.ResponseWriter, r *http.Request, following bool) {
	owner := common.NormalizeAddr(chi.URLParam(r, "wallet"))
	if !common.IsAddr(owner) {
		interactionError(w, http.StatusBadRequest, "INVALID_INPUT")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			interactionError(w, http.StatusBadRequest, "INVALID_INPUT")
			return
		}
		limit = min(n, 100)
	}
	after := followCursor{At: 1 << 62}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		b, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(raw) > 256 || json.Unmarshal(b, &after) != nil || !common.IsAddr(after.Wallet) {
			interactionError(w, http.StatusBadRequest, "INVALID_INPUT")
			return
		}
	}
	self, other := "followee", "follower"
	if following {
		self, other = "follower", "followee"
	}
	rows, err := rt.pool.Query(r.Context(), `SELECT `+other+`,created_at FROM chat.follows WHERE `+self+`=$1 AND (created_at,`+other+`)<($2,$3) ORDER BY created_at DESC,`+other+` DESC LIMIT $4`, owner, after.At, after.Wallet, limit+1)
	if err != nil {
		interactionError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
		return
	}
	defer rows.Close()
	type row struct {
		wallet string
		at     int64
	}
	found := []row{}
	for rows.Next() {
		var v row
		if err = rows.Scan(&v.wallet, &v.at); err != nil {
			interactionError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
			return
		}
		found = append(found, v)
	}
	if rows.Err() != nil {
		interactionError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
		return
	}
	var next *string
	if len(found) > limit {
		found = found[:limit]
		last := found[limit-1]
		raw, _ := json.Marshal(followCursor{At: last.at, Wallet: last.wallet})
		s := base64.RawURLEncoding.EncodeToString(raw)
		next = &s
	}
	edges := make([]followEdge, 0, len(found))
	for _, v := range found {
		edges = append(edges, followEdge{Wallet: v.wallet, CreatedAt: time.Unix(v.at, 0).UTC()})
	}
	interactionData(w, map[string]any{"items": edges}, next)
}

func (rt *Runtime) socialFollow(w http.ResponseWriter, r *http.Request, remove bool) {
	wallet, ok := actor(r)
	if !ok {
		interactionError(w, http.StatusUnauthorized, "AUTH_EXPIRED")
		return
	}
	followee := common.NormalizeAddr(chi.URLParam(r, "followee"))
	if !common.IsAddr(followee) || followee == wallet {
		interactionError(w, http.StatusBadRequest, "INVALID_INPUT")
		return
	}
	var err error
	if remove {
		_, err = rt.pool.Exec(r.Context(), `DELETE FROM chat.follows WHERE follower=$1 AND followee=$2`, wallet, followee)
	} else {
		var denied bool
		if denied, err = rt.policy.denied(r.Context(), rt.pool, wallet, ""); err == nil && denied {
			interactionError(w, http.StatusForbidden, "SCOPE_DENIED")
			return
		}
		if err == nil {
			_, err = rt.pool.Exec(r.Context(), `INSERT INTO chat.follows(follower,followee,created_at) VALUES($1,$2,$3) ON CONFLICT (follower,followee) DO NOTHING`, wallet, followee, rt.clock().Unix())
		}
	}
	if err != nil {
		interactionError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE")
		return
	}
	if remove {
		noContent(w)
		return
	}
	interactionData(w, map[string]bool{"following": true}, nil)
}
