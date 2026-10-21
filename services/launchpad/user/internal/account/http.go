package account

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

type HTTP struct {
	Store      Store
	PoolLookup PoolLookup
	CursorKey  []byte
	Runtime    *Runtime
}

var errAuthExpired = errors.New("AUTH_EXPIRED")
var errResnapshot = errors.New("RESNAPSHOT_REQUIRED")

func (h HTTP) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/profiles/{wallet}", h.profileGet)
	mux.HandleFunc("GET /v2/profiles/{wallet}", h.profileV2Get)
	mux.HandleFunc("GET /v2/profiles:batch", h.profilesBatchGet)
	mux.HandleFunc("PATCH /v1/profiles/{wallet}", h.profilePatch)
	mux.HandleFunc("GET /v1/me/watchlist", h.watchGet)
	mux.HandleFunc("PUT /v1/me/watchlist/{poolAddress}", h.watchPut)
	mux.HandleFunc("DELETE /v1/me/watchlist/{poolAddress}", h.watchDelete)
	h.registerOwnerRoutes(mux)
	if h.Runtime != nil && h.Runtime.ExtendedV3 {
		mux.Handle("PATCH /v3/profiles/{wallet}", h.ExtendedProfileHandler())
		mux.HandleFunc("GET /v3/profiles/{wallet}", h.extendedProfileGet)
		mux.Handle("PATCH /v3/me/preferences", h.ExtendedPreferencesHandler())
		mux.HandleFunc("GET /v3/me/preferences", h.extendedPreferencesGet)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Runtime != nil && h.Runtime.Metrics != nil {
			measured := &measuredWriter{ResponseWriter: w}
			w = measured
			started := time.Now()
			defer func() { h.Runtime.Metrics.ObserveHTTP(r.URL.Path, measured.status, time.Since(started)) }()
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if h.Store.DB == nil {
			WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), requestID(r))
			return
		}
		ctx = context.WithValue(ctx, requestTraceKey{}, requestID(r).String())
		if h.Runtime != nil {
			ctx = context.WithValue(ctx, runtimeKey{}, h.Runtime)
		}
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestID(r *http.Request) uuid.UUID {
	id, err := uuid.Parse(r.Header.Get("X-Request-ID"))
	if err != nil || id == uuid.Nil {
		return uuid.New()
	}
	return id
}

func authenticated(r *http.Request) (Principal, error) {
	p, ok := r.Context().Value(principalContextKey{}).(Principal)
	if !ok || p.AccountID == uuid.Nil {
		return Principal{}, errAuthExpired
	}
	return p, nil
}

func (h HTTP) respond(w http.ResponseWriter, id uuid.UUID, data any, revision string, next *string) {
	envelope, err := Response(id, data, revision, h.Store.now())
	if err != nil {
		WriteError(w, err, id)
		return
	}
	envelope.NextCursor = next
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(envelope)
}

func (h HTTP) profileGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	profile, err := h.Store.Public(r.Context(), r.PathValue("wallet"))
	var projectionGeneration int64
	if h.Runtime != nil && h.Runtime.UseReadSwitch {
		projection, e := h.Store.PublicFromReadSwitch(r.Context(), r.PathValue("wallet"))
		profile, err = projection.Profile, e
		projectionGeneration = projection.Generation
		w.Header().Set("X-Profile-Projection", projection.Source)
	}
	if err != nil {
		WriteError(w, err, id)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=1, stale-while-revalidate=2")
	etag := `"` + profile.Wallet + ":" + profile.Revision + `"`
	if projectionGeneration > 0 {
		etag = `"` + profile.Wallet + ":" + profile.Revision + ":" + strconv.FormatInt(projectionGeneration, 10) + `"`
	}
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" {
		if match == etag {
			if h.Runtime != nil {
				h.Runtime.Metrics.Observe("projection", "success", 0)
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if h.Runtime != nil {
			h.Runtime.Metrics.Observe("projection", "stale", 0)
		}
	}
	if h.Runtime != nil {
		var deleted bool
		var updated time.Time
		if e := h.Store.reader(r.Context()).QueryRow(r.Context(), `SELECT deleted,updated_at FROM account_state WHERE wallet=$1`, profile.Wallet).Scan(&deleted, &updated); e != nil {
			WriteError(w, e, id)
			return
		}
		if deleted {
			h.Runtime.Metrics.Observe("projection", "success", h.Store.now().Sub(updated))
		}
	}
	h.respond(w, id, map[string]PublicProfile{"profile": profile}, profile.Revision, nil)
}

func (h HTTP) ownedWallet(ctx context.Context, p Principal) (string, error) {
	var wallet string
	err := h.Store.reader(ctx).QueryRow(ctx, `SELECT wallet FROM account_state WHERE account_id=$1 AND NOT deleted`, p.AccountID).Scan(&wallet)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMissing
	}
	return wallet, err
}

func (h HTTP) profilePatch(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, err := authenticated(r)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	wallet, err := Wallet(r.PathValue("wallet"))
	if err != nil {
		WriteError(w, err, id)
		return
	}
	owned, err := h.ownedWallet(r.Context(), p)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	if wallet != owned {
		WriteError(w, ErrDenied, id)
		return
	}
	var patch ProfilePatch
	if err = Decode(r.Body, &patch); err != nil {
		WriteError(w, err, id)
		return
	}
	if match := r.Header.Get("If-Match"); match != "" {
		valid := match == `"`+patch.ExpectedRevision+`"` || match == `"`+wallet+":"+patch.ExpectedRevision+`"`
		if h.Runtime != nil && h.Runtime.UseReadSwitch {
			projection, e := h.Store.PublicFromReadSwitch(r.Context(), wallet)
			if e != nil {
				WriteError(w, e, id)
				return
			}
			valid = match == `"`+wallet+":"+patch.ExpectedRevision+":"+strconv.FormatInt(projection.Generation, 10)+`"`
		}
		if !valid {
			WriteError(w, ErrConflict, id)
			return
		}
	}
	if err = h.Store.PatchProfile(r.Context(), p, p.AccountID, patch); err != nil {
		WriteError(w, err, id)
		return
	}
	profile, err := h.Store.Public(r.Context(), wallet)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	h.respond(w, id, map[string]PublicProfile{"profile": profile}, profile.Revision, nil)
}

func (h HTTP) encodeCursor(p Principal, revision int64, last WatchEntry) string {
	raw := make([]byte, 53)
	raw[0] = 1
	copy(raw[1:17], p.AccountID[:])
	binary.BigEndian.PutUint64(raw[17:25], uint64(revision))
	binary.BigEndian.PutUint64(raw[25:33], uint64(last.AddedAt.UnixNano()))
	pool, _ := hex.DecodeString(last.Pool[2:])
	copy(raw[33:], pool)
	mac := hmac.New(sha256.New, h.CursorKey)
	mac.Write([]byte("l35-watch-v1"))
	mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(append(raw, mac.Sum(nil)...))
}

func (h HTTP) decodeCursor(p Principal, revision int64, encoded string) (WatchCursor, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(raw) != 85 || raw[0] != 1 {
		return WatchCursor{}, ErrCursor
	}
	mac := hmac.New(sha256.New, h.CursorKey)
	mac.Write([]byte("l35-watch-v1"))
	mac.Write(raw[:53])
	if !hmac.Equal(raw[53:], mac.Sum(nil)) || !hmac.Equal(raw[1:17], p.AccountID[:]) {
		return WatchCursor{}, ErrCursor
	}
	if binary.BigEndian.Uint64(raw[17:25]) != uint64(revision) {
		return WatchCursor{}, errResnapshot
	}
	return WatchCursor{At: time.Unix(0, int64(binary.BigEndian.Uint64(raw[25:33]))).UTC(), Pool: "0x" + hex.EncodeToString(raw[33:53])}, nil
}

func (h HTTP) watchSnapshot(ctx context.Context, p Principal) (state, []WatchEntry, error) {
	tx := accountTransaction(ctx)
	var err error
	if tx == nil {
		tx, err = h.Store.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			return state{}, nil, err
		}
		defer tx.Rollback(ctx)
	}
	a, err := load(ctx, tx, p.AccountID)
	if err != nil {
		return state{}, nil, err
	}
	if a.Deleted {
		return state{}, nil, ErrState
	}
	rows, err := tx.Query(ctx, `SELECT pool,added_at,revision,alert_enabled FROM account_watch WHERE account_id=$1 ORDER BY added_at,pool LIMIT 501`, p.AccountID)
	if err != nil {
		return state{}, nil, err
	}
	defer rows.Close()
	entries := make([]WatchEntry, 0)
	for rows.Next() {
		var entry WatchEntry
		if err := rows.Scan(&entry.Pool, &entry.AddedAt, &entry.Revision, &entry.AlertEnabled); err != nil {
			return state{}, nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return state{}, nil, err
	}
	if len(entries) > 500 {
		return state{}, nil, ErrState
	}
	return a, entries, nil
}

func (h HTTP) watchGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, err := authenticated(r)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	if len(h.CursorKey) != 32 {
		WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), id)
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "cursor" && key != "limit") || len(values) != 1 {
			WriteError(w, ErrUnknown, id)
			return
		}
	}
	limit := 50
	if raw, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(raw[0])
		if err != nil || limit < 1 || limit > 100 {
			WriteError(w, ValidationError{"/limit", ErrInvalid}, id)
			return
		}
	}
	a, entries, err := h.watchSnapshot(r.Context(), p)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	start := 0
	if raw, ok := query["cursor"]; ok {
		if len(raw[0]) < 1 || len(raw[0]) > 256 {
			WriteError(w, ErrCursor, id)
			return
		}
		cursor, err := h.decodeCursor(p, a.Revision, raw[0])
		if err != nil {
			WriteError(w, err, id)
			return
		}
		found := false
		for i, entry := range entries {
			if entry.Pool == cursor.Pool && entry.AddedAt.Equal(cursor.At) {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			WriteError(w, ErrCursor, id)
			return
		}
	}
	end := min(start+limit, len(entries))
	items := make([]WireWatchEntry, 0, end-start)
	for index := start; index < end; index++ {
		projected, err := WatchProjection(a.Wallet, entries[index], index, entries[index].AlertEnabled)
		if err != nil {
			WriteError(w, err, id)
			return
		}
		items = append(items, projected)
	}
	var next *string
	if end < len(entries) {
		value := h.encodeCursor(p, a.Revision, entries[end-1])
		next = &value
	}
	h.respond(w, id, map[string]any{"items": items}, strconv.FormatInt(a.Revision, 10), next)
}

func (h HTTP) watchPut(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, err := authenticated(r)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	var body WatchPutBody
	if err = Decode(r.Body, &body); err != nil {
		WriteError(w, err, id)
		return
	}
	if err = body.Validate(); err != nil {
		WriteError(w, err, id)
		return
	}
	pool, err := Wallet(r.PathValue("poolAddress"))
	if err != nil {
		WriteError(w, err, id)
		return
	}
	if err = h.Store.PutWatch(r.Context(), p, p.AccountID, pool, *body.AlertEnabled, h.PoolLookup); err != nil {
		WriteError(w, err, id)
		return
	}
	a, entries, err := h.watchSnapshot(r.Context(), p)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	for index, entry := range entries {
		if entry.Pool == pool {
			projected, err := WatchProjection(a.Wallet, entry, index, entry.AlertEnabled)
			if err != nil {
				WriteError(w, err, id)
				return
			}
			h.respond(w, id, map[string]WireWatchEntry{"entry": projected}, strconv.FormatInt(a.Revision, 10), nil)
			return
		}
	}
	WriteError(w, ErrMissing, id)
}

func (h HTTP) watchDelete(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, err := authenticated(r)
	if err != nil {
		WriteError(w, err, id)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1))
	if err != nil || len(body) > 0 {
		WriteError(w, ErrInvalid, id)
		return
	}
	if err := h.Store.DeleteWatch(r.Context(), p, p.AccountID, r.PathValue("poolAddress")); err != nil {
		WriteError(w, err, id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
