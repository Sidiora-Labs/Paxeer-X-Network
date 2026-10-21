package account

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type identityContextKey struct{}

func identityBoundaryError(err error) error {
	var authority *sharedauth.IdentityAuthorityError
	if errors.As(err, &authority) && authority.Status >= 500 {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	var verification *sharedauth.VerificationError
	if errors.As(err, &verification) && verification.Retryable {
		return errors.New("DEPENDENCY_UNAVAILABLE")
	}
	if errors.Is(err, sharedauth.ErrScopeDenied) || errors.Is(err, ErrDenied) {
		return errAuthExpired
	}
	return err
}

func (h HTTP) ServiceHandler(authority sharedauth.RequestIdentityAuthority) http.Handler {
	next := h.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/v1/profiles/") || strings.HasPrefix(r.URL.Path, "/v2/profiles/") || r.URL.Path == "/v2/profiles:batch" || strings.HasPrefix(r.URL.Path, "/v3/profiles/")) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/me/deletion-requests/") && r.Header.Get("X-Recovery-Receipt") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if authority == nil {
			WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), requestID(r))
			return
		}
		p, e := authority.AuthenticateRequest(r.Context(), r)
		if e != nil {
			WriteError(w, identityBoundaryError(e), requestID(r))
			return
		}
		id, e := uuid.Parse(p.AccountID)
		if e != nil || id == uuid.Nil {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		ctx := context.WithValue(r.Context(), identityContextKey{}, p)
		ctx = WithPrincipal(ctx, Principal{AccountID: id, ProofAt: p.ProofAt})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
func (h HTTP) registerOwnerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/preferences", h.preferencesGet)
	mux.HandleFunc("GET /v1/me/devices", h.devicesGet)
	mux.HandleFunc("PATCH /v1/me/preferences", h.preferencesPatch)
	mux.HandleFunc("PUT /v1/me/devices/{deviceId}", h.devicePut)
	mux.HandleFunc("DELETE /v1/me/devices/{deviceId}", h.deviceDelete)
	mux.HandleFunc("POST /v1/me/exports", h.exportPost)
	mux.HandleFunc("GET /v1/me/exports/{jobId}", h.exportGet)
	mux.HandleFunc("POST /v1/me/deletion-requests", h.deletionPost)
	mux.HandleFunc("GET /v1/me/deletion-requests/{jobId}", h.deletionGet)
	mux.Handle("GET /v2/me/exports/{jobId}/download", h.DownloadHandler())
}
func (h HTTP) preferencesGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, e := authenticated(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	v, e := h.Store.ReadPreferences(r.Context(), p, p.AccountID)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	h.respond(w, id, map[string]Preferences{"preferences": v}, v.Revision, nil)
}
func (h HTTP) preferencesPatch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := requestID(r)
	p, e := authenticated(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	var patch PreferencesPatch
	if e = Decode(r.Body, &patch); e == nil {
		e = h.Store.PatchPreferences(r.Context(), p, p.AccountID, patch)
	}
	if e != nil {
		if h.Runtime != nil {
			h.Runtime.Metrics.Observe("preferences", "invalid", time.Since(start))
		}
		WriteError(w, e, id)
		return
	}
	v, e := h.Store.ReadPreferences(r.Context(), p, p.AccountID)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	if h.Runtime != nil {
		h.Runtime.Metrics.Observe("preferences", "success", time.Since(start))
	}
	h.respond(w, id, map[string]Preferences{"preferences": v}, v.Revision, nil)
}

type ExtendedPreferencesPatch struct {
	PreferencesPatch
	Currency        *string        `json:"currency,omitempty"`
	ChartInterval   *string        `json:"chartInterval,omitempty"`
	ReducedMotion   *bool          `json:"reducedMotion,omitempty"`
	PrivateActivity *bool          `json:"privateActivity,omitempty"`
	Consents        []ConsentPatch `json:"consents,omitempty"`
}
type ConsentPatch struct {
	Category string `json:"category"`
	Channel  string `json:"channel"`
	Enabled  bool   `json:"enabled"`
}

func (s Store) PatchExtendedPreferences(ctx context.Context, p Principal, v ExtendedPreferencesPatch) error {
	if e := v.PreferencesPatch.Validate(); e != nil {
		return e
	}
	if v.Currency != nil && *v.Currency != "USD" && *v.Currency != "EUR" && *v.Currency != "GBP" {
		return ValidationError{"/currency", ErrInvalid}
	}
	if v.ChartInterval != nil {
		switch *v.ChartInterval {
		case "1m", "5m", "15m", "1h", "4h", "1d":
		default:
			return ValidationError{"/chartInterval", ErrInvalid}
		}
	}
	if len(v.Consents) > 18 {
		return ValidationError{"/consents", ErrInvalid}
	}
	seen := map[string]bool{}
	for _, c := range v.Consents {
		if !validCategory(c.Category) || !validChannel(c.Channel) || seen[c.Category+"."+c.Channel] {
			return ValidationError{"/consents", ErrInvalid}
		}
		seen[c.Category+"."+c.Channel] = true
	}
	return s.mutate(ctx, p, p.AccountID, v.ExpectedRevision, "preferences.updated", func(tx pgx.Tx, a *state) error {
		q := &a.Preferences
		q.SchemaVersion = 2
		if v.Locale != nil {
			q.Locale = *v.Locale
		}
		if v.Timezone != nil {
			q.Timezone = *v.Timezone
		}
		if v.Channels != nil {
			q.Channels = v.Channels.Enabled()
		}
		if v.QuietHours != nil {
			q.QuietHours = *v.QuietHours
		}
		if v.Currency != nil {
			q.Currency = *v.Currency
		}
		if v.ChartInterval != nil {
			q.ChartInterval = *v.ChartInterval
		}
		if v.ReducedMotion != nil {
			q.ReducedMotion = *v.ReducedMotion
		}
		if v.PrivateActivity != nil {
			q.PrivateActivity = *v.PrivateActivity
		}
		q.Revision = strconv.FormatInt(a.Revision, 10)
		q.UpdatedAt = s.now()
		for _, c := range v.Consents {
			var revoke int64
			e := tx.QueryRow(ctx, `SELECT revoke_revision FROM account_consent WHERE account_id=$1 AND category=$2 AND channel=$3`, p.AccountID, c.Category, c.Channel).Scan(&revoke)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if !c.Enabled {
				revoke = a.Revision
			}
			_, e = tx.Exec(ctx, `INSERT INTO account_consent VALUES($1,$2,$3,$4,'settings','1',$5,$6,$7) ON CONFLICT(account_id,category,channel) DO UPDATE SET enabled=EXCLUDED.enabled,source=EXCLUDED.source,policy_version=EXCLUDED.policy_version,revision=EXCLUDED.revision,revoke_revision=EXCLUDED.revoke_revision,changed_at=EXCLUDED.changed_at`, p.AccountID, c.Category, c.Channel, c.Enabled, a.Revision, revoke, s.now())
			if e != nil {
				return e
			}
			if e = emit(ctx, tx, *a, "consent.updated", ConsentRevision{EventID: uuid.NewString(), AccountID: p.AccountID, Revision: a.Revision, Category: c.Category, Channel: c.Channel, Enabled: c.Enabled, RevokeRevision: revoke, At: s.now()}); e != nil {
				return e
			}
		}
		return nil
	})
}
func (h HTTP) ExtendedPreferencesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p, e := authenticated(r)
		if e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		var v ExtendedPreferencesPatch
		if e = Decode(r.Body, &v); e == nil {
			e = h.Store.PatchExtendedPreferences(r.Context(), p, v)
		}
		if e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		h.extendedPreferencesGet(w, r)
	})
}
func jsonBody(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

type accountTransactionKey struct{}

func accountTransaction(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(accountTransactionKey{}).(pgx.Tx)
	return tx
}

type queryReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (s Store) reader(ctx context.Context) queryReader {
	if tx := accountTransaction(ctx); tx != nil {
		return tx
	}
	return s.DB
}

type capturedResponse struct {
	Status int         `json:"status"`
	Header http.Header `json:"header"`
	Body   []byte      `json:"body"`
}
type captureWriter struct{ capturedResponse }

func (c *captureWriter) Header() http.Header {
	if c.capturedResponse.Header == nil {
		c.capturedResponse.Header = make(http.Header)
	}
	return c.capturedResponse.Header
}
func (c *captureWriter) WriteHeader(status int) {
	if c.Status == 0 {
		c.Status = status
	}
}
func (c *captureWriter) Write(b []byte) (int, error) {
	if c.Status == 0 {
		c.Status = 200
	}
	if len(c.Body)+len(b) > 1<<20 {
		return 0, ErrInvalid
	}
	c.Body = append(c.Body, b...)
	return len(b), nil
}
func (h HTTP) GatewayHandler(assertions sharedauth.ServiceAssertionVerifier, resolver sharedauth.PostgresIdentityResolver, operations sharedauth.OperationAuthorizer) http.Handler {
	next := h.Handler()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bounded, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		r = r.WithContext(bounded)
		if r.Method == http.MethodGet && (strings.HasPrefix(r.URL.Path, "/v1/profiles/") || strings.HasPrefix(r.URL.Path, "/v2/profiles/") || r.URL.Path == "/v2/profiles:batch" || strings.HasPrefix(r.URL.Path, "/v3/profiles/")) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/me/deletion-requests/") && r.Header.Get("X-Recovery-Receipt") != "" {
			if h.Runtime == nil || h.Runtime.RecoveryGatewayVerifier == nil || h.Runtime.RecoveryGatewayVerifier.Authenticate(r, nil) != nil {
				WriteError(w, ErrDenied, requestID(r))
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		body, e := io.ReadAll(io.LimitReader(r.Body, 16385))
		if e != nil || len(body) > 16384 {
			WriteError(w, ErrInvalid, requestID(r))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		gateway, e := assertions.Authenticate(r, body)
		if e != nil {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		ownerRole := false
		for _, role := range gateway.Roles {
			if role == "owner" {
				ownerRole = true
			}
		}
		if !ownerRole {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/me/deletion-requests/") && r.Header.Get("X-Recovery-Receipt") != "" {
			next.ServeHTTP(w, r)
			return
		}
		identity, e := resolver.Resolve(r.Context(), gateway)
		if e == nil {
			if h.Runtime == nil || h.Runtime.IdentityAuthority == nil {
				e = errors.New("DEPENDENCY_UNAVAILABLE")
			} else {
				var authenticated sharedauth.IdentityPrincipal
				authenticated, e = h.Runtime.IdentityAuthority.AuthenticateRequest(r.Context(), r)
				if e == nil && (authenticated.AccountID != identity.AccountID || authenticated.SessionID != identity.SessionID || authenticated.Wallet != identity.Wallet || authenticated.AuthEpoch != identity.AuthEpoch) {
					e = ErrDenied
				}
			}
		}
		if e != nil {
			WriteError(w, identityBoundaryError(e), requestID(r))
			return
		}
		if e = h.Store.ensureIdentityAccount(r.Context(), resolver, gateway); e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		id, e := uuid.Parse(identity.AccountID)
		if e != nil {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		ctx := WithPrincipal(context.WithValue(r.Context(), identityContextKey{}, identity), Principal{AccountID: id, ProofAt: identity.ProofAt})
		r = r.WithContext(ctx)
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/v1/me/deletion-requests" {
			next.ServeHTTP(w, r)
			return
		}
		action := ""
		switch {
		case (strings.HasPrefix(r.URL.Path, "/v1/profiles/") || strings.HasPrefix(r.URL.Path, "/v3/profiles/")):
			action = "profile.update"
		case r.URL.Path == "/v1/me/preferences" || r.URL.Path == "/v3/me/preferences":
			action = "preferences.update"
		case strings.HasPrefix(r.URL.Path, "/v1/me/watchlist/"):
			action = "watchlist.update"
			if r.Method == http.MethodDelete {
				action = "watchlist.remove"
			}
		case strings.HasPrefix(r.URL.Path, "/v1/me/devices/"):
			action = "device.register"
			if r.Method == http.MethodDelete {
				action = "device.revoke"
			}
		case r.URL.Path == "/v1/me/exports":
			action = "account.export"
		}
		if action == "" {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		requestOperations := operations
		if strings.HasPrefix(r.URL.Path, "/v3/") {
			if h.Runtime == nil || !h.Runtime.ExtendedV3 {
				WriteError(w, ErrDenied, requestID(r))
				return
			}
			requestOperations.Policies, requestOperations.Routes = accountExtendedPoliciesV3()
		}
		req, e := requestOperations.AuthorizeRequest(ctx, r, identity, action, identity.Wallet, body)
		if e != nil {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		tx, e := h.Store.DB.Begin(ctx)
		if e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		defer tx.Rollback(ctx)
		_, e = resolver.ResolveTx(ctx, tx, gateway)
		if e != nil {
			WriteError(w, identityBoundaryError(e), requestID(r))
			return
		}
		output, _, e := requestOperations.ExecuteTx(ctx, tx, req, func(c context.Context, owned pgx.Tx, _ []byte) ([]byte, error) {
			var key uuid.UUID
			requiresKey := action == "device.register" || action == "device.revoke" || action == "account.export"
			if requiresKey || r.Header.Get("Idempotency-Key") != "" {
				var e error
				key, e = uuid.Parse(r.Header.Get("Idempotency-Key"))
				if e != nil || key == uuid.Nil {
					return nil, ErrInvalid
				}
				if _, e = load(c, owned, id); e != nil {
					return nil, e
				}
				var digest, result []byte
				var expires time.Time
				e = owned.QueryRow(c, `SELECT digest,result,expires_at FROM account_idempotency WHERE account_id=$1 AND method=$2 AND path=$3 AND key=$4`, id, r.Method, r.URL.Path, key).Scan(&digest, &result, &expires)
				if e == nil && expires.After(h.Store.now()) {
					hash := sha256.Sum256(body)
					if !bytes.Equal(digest, hash[:]) {
						return nil, ErrIdempotency
					}
					return result, nil
				}
				if e != nil && !errors.Is(e, pgx.ErrNoRows) {
					return nil, e
				}
			}
			capture := &captureWriter{}
			request := r.Clone(context.WithValue(c, accountTransactionKey{}, owned))
			request.Body = io.NopCloser(bytes.NewReader(body))
			next.ServeHTTP(capture, request)
			if capture.Status < 200 || capture.Status >= 300 {
				return nil, &responseFailure{capture.capturedResponse}
			}
			encoded, e := json.Marshal(capture.capturedResponse)
			if e != nil {
				return nil, e
			}
			if key != uuid.Nil {
				hash := sha256.Sum256(body)
				_, e = owned.Exec(c, `INSERT INTO account_idempotency VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(account_id,method,path,key) DO UPDATE SET digest=EXCLUDED.digest,result=EXCLUDED.result,expires_at=EXCLUDED.expires_at`, id, r.Method, r.URL.Path, key, hash[:], encoded, h.Store.now().Add(24*time.Hour))
				if e != nil {
					return nil, e
				}
			}
			return encoded, nil
		})
		if e != nil {
			var rejected *responseFailure
			if errors.As(e, &rejected) {
				writeCaptured(w, rejected.Response)
				return
			}
			WriteError(w, e, requestID(r))
			return
		}
		if e = tx.Commit(ctx); e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		var response capturedResponse
		if json.Unmarshal(output, &response) != nil {
			WriteError(w, ErrInvalid, requestID(r))
			return
		}
		writeCaptured(w, response)
	})
}

type responseFailure struct{ Response capturedResponse }

func (e *responseFailure) Error() string { return "account operation rejected" }
func writeCaptured(w http.ResponseWriter, r capturedResponse) {
	for k, values := range r.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(r.Status)
	_, _ = w.Write(r.Body)
}

func (s Store) PatchExtendedProfile(ctx context.Context, p Principal, v ExtendedProfilePatch) error {
	if e := v.ProfilePatch.Validate(); e != nil {
		return e
	}
	if v.Website.Value != nil {
		if e := SafeLink(*v.Website.Value); e != nil {
			return e
		}
	}
	if len(v.Socials) > 4 {
		return ErrInvalid
	}
	for k, value := range v.Socials {
		switch k {
		case "website", "twitter", "telegram", "discord":
		default:
			return ErrUnknown
		}
		if value.Value != nil {
			if e := SafeLink(*value.Value); e != nil {
				return e
			}
		}
	}
	return s.mutate(ctx, p, p.AccountID, v.ExpectedRevision, "profile.updated", func(tx pgx.Tx, a *state) error {
		for _, asset := range []Optional[string]{v.AvatarAssetID, v.BannerAssetID} {
			if !asset.Set || asset.Value == nil {
				continue
			}
			runtime, _ := ctx.Value(runtimeKey{}).(*Runtime)
			if runtime == nil || runtime.Assets == nil {
				return errors.New("DEPENDENCY_UNAVAILABLE")
			}
			if e := runtime.Assets.ApprovedAccountAsset(ctx, a.Wallet, *asset.Value); e != nil {
				return e
			}
		}

		if v.DisplayName != nil {
			a.Profile.DisplayName = *v.DisplayName
		}
		if v.Bio != nil {
			a.Profile.Bio = *v.Bio
		}
		if v.AvatarAssetID.Set {
			a.Profile.AvatarAssetID = v.AvatarAssetID.Value
		}
		if v.BannerAssetID.Set {
			a.Profile.BannerAssetID = v.BannerAssetID.Value
		}
		if v.Website.Set {
			a.Profile.Website = v.Website.Value
		}
		if a.Profile.Socials == nil {
			a.Profile.Socials = map[string]string{}
		}
		for k, value := range v.Socials {
			if value.Value == nil {
				delete(a.Profile.Socials, k)
			} else {
				a.Profile.Socials[k] = *value.Value
			}
		}
		a.Profile.UpdatedAt = s.now()
		return nil
	})
}
func (h HTTP) ExtendedProfileHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p, e := authenticated(r)
		if e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		wallet, e := h.ownedWallet(r.Context(), p)
		if e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		target, e := Wallet(r.PathValue("wallet"))
		if e != nil || wallet != target {
			WriteError(w, ErrDenied, requestID(r))
			return
		}
		var v ExtendedProfilePatch
		if e = DecodeExtendedProfile(r.Body, &v); e == nil {
			ctx := r.Context()
			if h.Runtime != nil {
				ctx = context.WithValue(ctx, runtimeKey{}, h.Runtime)
			}
			e = h.Store.PatchExtendedProfile(ctx, p, v)
		}
		if e != nil {
			WriteError(w, e, requestID(r))
			return
		}
		h.extendedProfileGet(w, r)
	})
}

type PublicCreatorPool struct {
	PoolAddress  string    `json:"poolAddress"`
	TokenAddress string    `json:"tokenAddress"`
	CreatedAt    time.Time `json:"createdAt"`
}
type ProfilePresentation struct {
	PublicProfile
	Socials      map[string]string   `json:"socials"`
	CreatorPools []PublicCreatorPool `json:"creatorPools"`
}

func (s Store) PublicPresentation(ctx context.Context, wallet string) (ProfilePresentation, error) {
	profile, e := s.Public(ctx, wallet)
	if e != nil {
		return ProfilePresentation{}, e
	}
	out := ProfilePresentation{PublicProfile: profile, Socials: map[string]string{}, CreatorPools: []PublicCreatorPool{}}
	var raw []byte
	var deleted bool
	e = s.reader(ctx).QueryRow(ctx, `SELECT profile,deleted FROM account_state WHERE wallet=$1`, profile.Wallet).Scan(&raw, &deleted)
	if e != nil {
		return out, e
	}
	if !deleted {
		var private Profile
		if e = json.Unmarshal(raw, &private); e != nil {
			return out, e
		}
		for k, v := range private.Socials {
			out.Socials[k] = v
		}
	}
	rows, e := s.reader(ctx).Query(ctx, `SELECT pool,token,to_timestamp(block_timestamp) FROM indexer.indexer_canonical_creations_v1 WHERE creator=$1 ORDER BY height DESC,transaction_index DESC,log_index DESC LIMIT 100`, profile.Wallet)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var pool PublicCreatorPool
		if e = rows.Scan(&pool.PoolAddress, &pool.TokenAddress, &pool.CreatedAt); e != nil {
			return out, e
		}
		out.CreatorPools = append(out.CreatorPools, pool)
	}
	return out, rows.Err()
}

func (s Store) ensureIdentityAccount(ctx context.Context, resolver sharedauth.PostgresIdentityResolver, gateway sharedauth.Principal) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	identity, e := resolver.ResolveTx(ctx, tx, gateway)
	if e != nil {
		return e
	}
	id, e := uuid.Parse(identity.AccountID)
	if e != nil {
		return ErrDenied
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "l35-account-provision:"+identity.AccountID); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM account_state WHERE account_id=$1)`, id).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return tx.Commit(ctx)
	}
	var wallet string
	if e = tx.QueryRow(ctx, `SELECT wallet FROM identity_wallet_links WHERE account_id=$1 AND state='verified' ORDER BY linked_at,link_id LIMIT 1`, id).Scan(&wallet); e != nil {
		return e
	}
	profile, e := json.Marshal(Profile{PublicProfile: PublicProfile{Wallet: wallet, Revision: "0"}, SchemaVersion: 1, UpdatedAt: s.now()})
	if e != nil {
		return e
	}
	preferences, e := json.Marshal(Preferences{SchemaVersion: 1, Revision: "0", UpdatedAt: s.now(), Locale: "en", Timezone: "UTC", Currency: "USD", ChartInterval: "1h", Channels: []string{}, NotificationCategories: map[string]bool{}, QuietHours: QuietHours{Timezone: "UTC", Start: "00:00", End: "00:00"}})
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO account_state(account_id,wallet,profile,preferences) VALUES($1,$2,$3,$4) ON CONFLICT(account_id) DO NOTHING`, id, wallet, profile, preferences); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (h HTTP) extendedProfileGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	profile, e := h.Store.PublicPresentation(r.Context(), r.PathValue("wallet"))
	if e != nil {
		WriteError(w, e, id)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=1")
	h.respond(w, id, map[string]any{"profile": profile}, profile.Revision, nil)
}
func (h HTTP) extendedPreferencesGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	p, e := authenticated(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	preferences, e := h.Store.ReadPreferences(r.Context(), p, p.AccountID)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	rows, e := h.Store.reader(r.Context()).Query(r.Context(), `SELECT category,channel,enabled,source,policy_version,revision,revoke_revision,changed_at FROM account_consent WHERE account_id=$1 ORDER BY category,channel`, p.AccountID)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	defer rows.Close()
	consents := []Consent{}
	for rows.Next() {
		var v Consent
		if e = rows.Scan(&v.Category, &v.Channel, &v.Enabled, &v.Source, &v.PolicyVersion, &v.Revision, &v.RevokeRevision, &v.At); e != nil {
			WriteError(w, e, id)
			return
		}
		consents = append(consents, v)
	}
	if e = rows.Err(); e != nil {
		WriteError(w, e, id)
		return
	}
	h.respond(w, id, map[string]any{"preferences": preferences, "consents": consents}, preferences.Revision, nil)
}

func accountExtendedPoliciesV3() (sharedauth.PolicyRegistry, map[string]sharedauth.ActionRoute) {
	policies, routes := sharedauth.AccountOwnerPoliciesV2()
	policies.Version = "3"
	for action, path := range map[string]string{"profile.update": "/v3/profiles/{wallet}", "preferences.update": "/v3/me/preferences"} {
		route := routes[action]
		route.Template = path
		routes[action] = route
		policy := policies.Actions[action]
		policy.RouteTemplate = path
		policies.Actions[action] = policy
	}
	return policies, routes
}
