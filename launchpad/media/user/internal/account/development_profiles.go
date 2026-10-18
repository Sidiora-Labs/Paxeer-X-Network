package account

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/media/user/internal/db/sqlcdb"
)

// LegacyProfileQueries is the subset of the generated sqlc querier the
// development profile reads use: the legacy users.user_profiles row written by
// `POST /users/{wallet}` and the indexer.pools cross-read `GET /users/{wallet}`
// already serves as created pools.
type LegacyProfileQueries interface {
	GetUserProfile(ctx context.Context, walletAddress string) (sqlcdb.UsersUserProfile, error)
	ListCreatedPools(ctx context.Context, creator string) ([]sqlcdb.ListCreatedPoolsRow, error)
}

// DevelopmentProfileReads serves the public profile read contract outside
// production. Production mounts the same paths from HTTP.Handler over
// account_state (provisioned by the identity-backed account runtime, which only
// runs in production); in development the account runtime is absent, so these
// handlers project the legacy users.user_profiles row into the identical
// frozen wire shapes (IF_PROFILE_GET, IF_PROFILES_BATCH,
// IF_CONNECTED_EXTENDED_PROFILE_GET) and the identical IF_ERROR envelope.
// They are read-only and unauthenticated, exactly like their production
// counterparts; the owner write lives in DevelopmentProfileWrites.
type DevelopmentProfileReads struct {
	Queries LegacyProfileQueries
	Clock   func() time.Time
}

// Routes lists the paths Handler serves, for mounting on an outer router.
func (DevelopmentProfileReads) Routes() []string {
	return []string{"/v1/profiles/{wallet}", "/v2/profiles/{wallet}", "/v2/profiles:batch", "/v3/profiles/{wallet}"}
}

// Handler returns the development read surface.
func (d DevelopmentProfileReads) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/profiles/{wallet}", d.profileGet(false))
	mux.HandleFunc("GET /v2/profiles/{wallet}", d.profileGet(true))
	mux.HandleFunc("GET /v2/profiles:batch", d.profilesBatchGet)
	mux.HandleFunc("GET /v3/profiles/{wallet}", d.extendedProfileGet)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if d.Queries == nil {
			WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), requestID(r))
			return
		}
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (d DevelopmentProfileReads) now() time.Time {
	if d.Clock != nil {
		return d.Clock()
	}
	return time.Now()
}

func (d DevelopmentProfileReads) respond(w http.ResponseWriter, id uuid.UUID, data any, revision string) {
	(HTTP{Store: Store{Clock: d.now}}).respond(w, id, data, revision, nil)
}

// legacyProfile reads and projects one wallet. A wallet with no legacy profile
// row has published no profile: ErrMissing (404 NOT_FOUND), as in production.
func (d DevelopmentProfileReads) legacyProfile(ctx context.Context, raw string) (PublicProfile, sqlcdb.UsersUserProfile, error) {
	wallet, e := Wallet(raw)
	if e != nil {
		return PublicProfile{}, sqlcdb.UsersUserProfile{}, e
	}
	row, e := d.Queries.GetUserProfile(ctx, wallet)
	if errors.Is(e, pgx.ErrNoRows) {
		return PublicProfile{}, row, ErrMissing
	}
	if e != nil {
		return PublicProfile{}, row, e
	}
	revision := row.UpdatedAt
	if revision < 1 {
		revision = 1
	}
	return PublicProfile{
		Wallet:      wallet,
		DisplayName: boundedText(row.DisplayName.String, 50),
		Bio:         boundedText(row.Bio.String, 500),
		Website:     legacyLink(row.Website.String),
		Revision:    strconv.FormatInt(revision, 10),
	}, row, nil
}

func (d DevelopmentProfileReads) profileGet(strictQuery bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := requestID(r)
		if strictQuery && len(r.URL.Query()) > 0 {
			WriteError(w, ErrUnknown, id)
			return
		}
		profile, _, e := d.legacyProfile(r.Context(), r.PathValue("wallet"))
		if e != nil {
			WriteError(w, e, id)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=1, stale-while-revalidate=2")
		etag := profileETag(profile, 0)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		d.respond(w, id, map[string]PublicProfile{"profile": profile}, profile.Revision)
	}
}

func (d DevelopmentProfileReads) profilesBatchGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	wallets, e := batchWallets(r.URL.Query())
	if e != nil {
		WriteError(w, e, id)
		return
	}
	items := make([]PublicProfile, 0, len(wallets))
	for _, wallet := range wallets {
		profile, _, e := d.legacyProfile(r.Context(), wallet)
		if errors.Is(e, ErrMissing) {
			continue
		}
		if e != nil {
			WriteError(w, e, id)
			return
		}
		items = append(items, profile)
	}
	w.Header().Set("Cache-Control", "public, max-age=1")
	d.respond(w, id, map[string][]PublicProfile{"items": items}, "0")
}

func (d DevelopmentProfileReads) extendedProfileGet(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	out, e := d.presentation(r.Context(), r.PathValue("wallet"))
	if e != nil {
		WriteError(w, e, id)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=1")
	d.respond(w, id, map[string]any{"profile": out}, out.Revision)
}

// presentation projects one wallet's legacy row into the frozen v3
// ConnectedAccountProfilePresentation (profile, https socials, created pools).
func (d DevelopmentProfileReads) presentation(ctx context.Context, raw string) (ProfilePresentation, error) {
	profile, row, e := d.legacyProfile(ctx, raw)
	if e != nil {
		return ProfilePresentation{}, e
	}
	out := ProfilePresentation{PublicProfile: profile, Socials: map[string]string{}, CreatorPools: []PublicCreatorPool{}}
	for key, value := range map[string]string{"website": row.Website.String, "twitter": row.Twitter.String, "telegram": row.Telegram.String, "discord": row.Discord.String} {
		if link := legacyLink(value); link != nil {
			out.Socials[key] = *link
		}
	}
	pools, e := d.Queries.ListCreatedPools(ctx, profile.Wallet)
	if e != nil {
		return ProfilePresentation{}, e
	}
	for _, pool := range pools {
		out.CreatorPools = append(out.CreatorPools, PublicCreatorPool{PoolAddress: strings.ToLower(pool.PoolAddress), TokenAddress: strings.ToLower(pool.TokenAddress), CreatedAt: legacyInstant(pool.CreatedAt)})
	}
	return out, nil
}

// boundedText trims and caps legacy free text at the frozen contract bound
// (displayName 50, bio 500 characters); the legacy upsert never bounded it.
func boundedText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

var contractLink = regexp.MustCompile(`^https://[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?(?::[0-9]{1,5})?(?:[/?#][^\s]*)?$`)

// legacyLink returns a contract-safe https link for a legacy social/website
// value, or nil. Bare hosts ("example.com") gain the https scheme; handles
// ("@name") and non-https links are not links and are dropped.
func legacyLink(raw string) *string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	if !strings.Contains(s, "://") && !strings.HasPrefix(s, "@") {
		s = "https://" + s
	}
	if SafeLink(s) != nil || !contractLink.MatchString(s) {
		return nil
	}
	return &s
}

// legacyInstant converts indexer.pools.created_at (unix seconds; tolerate
// milliseconds) to UTC.
func legacyInstant(v int64) time.Time {
	if v > 1_000_000_000_000 {
		return time.UnixMilli(v).UTC()
	}
	return time.Unix(v, 0).UTC()
}
