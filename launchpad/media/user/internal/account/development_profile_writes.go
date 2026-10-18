package account

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// TxBeginner opens the transaction the development profile write runs in
// (*pgxpool.Pool satisfies it).
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// DevelopmentProfileWrites serves the owner profile write the web issues
// (IF_CONNECTED_EXTENDED_PROFILE_PATCH, `PATCH /v3/profiles/{wallet}`) outside
// production. Production serves it from HTTP.ExtendedProfileHandler over
// account_state, behind the gateway's signed principal; development has no
// account runtime, so this handler:
//
//   - authenticates the browser session itself through the gateway's v2
//     identity introspection (IDENTITY_SERVICE_URL + IDENTITY_SERVICE_TOKEN),
//     the same authority the development /me reads use. No forwarded identity
//     header is trusted: this service has a public edge.
//   - admits only the session wallet's own profile (403 otherwise).
//   - validates the body against the frozen ConnectedExtendedProfileBody
//     contract and the production decoder (displayName 1..50, bio <= 500,
//     https-only website and socials, link shape the read projection keeps).
//   - writes the legacy users.user_profiles row DevelopmentProfileReads
//     projects, under a row lock, with expectedRevision / If-Match
//     optimistic concurrency, and answers the production response shape with
//     the advanced revision.
type DevelopmentProfileWrites struct {
	DB       TxBeginner
	Reads    DevelopmentProfileReads
	Identity sharedauth.RequestIdentityAuthority
	// Invalidate drops cached legacy reads of the wallet after a commit
	// (GET /users/{wallet} caches its JSON in Redis). Optional.
	Invalidate func(ctx context.Context, wallet string) error
	Clock      func() time.Time
}

// Routes lists the paths Handler serves, for mounting on an outer router.
func (DevelopmentProfileWrites) Routes() []string {
	return []string{"/v3/profiles/{wallet}"}
}

// Handler returns the development owner write surface.
func (d DevelopmentProfileWrites) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /v3/profiles/{wallet}", d.extendedProfilePatch)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if d.DB == nil || d.Reads.Queries == nil {
			WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), requestID(r))
			return
		}
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (d DevelopmentProfileWrites) now() time.Time {
	if d.Clock != nil {
		return d.Clock()
	}
	return time.Now()
}

// sessionWallet resolves the verified wallet of the caller's v2 identity
// session. No credential, or a credential the identity authority rejects, is
// AUTH_EXPIRED (401); an unreachable authority is DEPENDENCY_UNAVAILABLE.
func (d DevelopmentProfileWrites) sessionWallet(r *http.Request) (string, error) {
	if r.Header.Get("Authorization") == "" {
		if _, e := r.Cookie("__Host-kindle_access"); e != nil {
			return "", errAuthExpired
		}
	}
	if d.Identity == nil {
		return "", errors.New("DEPENDENCY_UNAVAILABLE")
	}
	principal, e := d.Identity.AuthenticateRequest(r.Context(), r)
	var authority *sharedauth.IdentityAuthorityError
	switch {
	case e == nil:
	case errors.As(e, &authority):
		// Only a definitive rejection of the credential signs the browser
		// out; a throttled or unavailable authority is transient.
		if authority.Status == http.StatusUnauthorized || authority.Status == http.StatusForbidden || authority.Status == http.StatusBadRequest {
			return "", errAuthExpired
		}
		return "", errors.New("DEPENDENCY_UNAVAILABLE")
	case errors.Is(e, sharedauth.ErrScopeDenied):
		// Malformed credential (rejected before the authority is called).
		return "", errAuthExpired
	default:
		return "", errors.New("DEPENDENCY_UNAVAILABLE")
	}
	wallet, e := Wallet(principal.Wallet)
	if e != nil {
		return "", errAuthExpired
	}
	return wallet, nil
}

func (d DevelopmentProfileWrites) extendedProfilePatch(w http.ResponseWriter, r *http.Request) {
	id := requestID(r)
	wallet, e := d.sessionWallet(r)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	target, e := Wallet(r.PathValue("wallet"))
	if e != nil || target != wallet {
		WriteError(w, ErrDenied, id)
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil || len(raw) > 16384 {
		WriteError(w, ErrInvalid, id)
		return
	}
	var patch ExtendedProfilePatch
	if e = DecodeExtendedProfile(bytes.NewReader(raw), &patch); e != nil {
		WriteError(w, e, id)
		return
	}
	if e = interfaces.ValidateJSON("ConnectedExtendedProfileBody", raw); e != nil {
		WriteError(w, ErrInvalid, id)
		return
	}
	if e = developmentLinks(patch); e != nil {
		WriteError(w, e, id)
		return
	}
	// Account media lives in the production asset runtime; nothing can
	// approve an asset id here. Clearing (null) is a no-op on the legacy row.
	for _, asset := range []Optional[string]{patch.AvatarAssetID, patch.BannerAssetID} {
		if asset.Set && asset.Value != nil {
			WriteError(w, errors.New("DEPENDENCY_UNAVAILABLE"), id)
			return
		}
	}
	if match := r.Header.Get("If-Match"); match != "" && match != `"`+patch.ExpectedRevision+`"` && match != `"`+wallet+":"+patch.ExpectedRevision+`"` {
		WriteError(w, ErrConflict, id)
		return
	}
	if e = d.apply(r.Context(), wallet, patch); e != nil {
		WriteError(w, e, id)
		return
	}
	if d.Invalidate != nil {
		_ = d.Invalidate(r.Context(), wallet)
	}
	out, e := d.Reads.presentation(r.Context(), wallet)
	if e != nil {
		WriteError(w, e, id)
		return
	}
	w.Header().Set("ETag", profileETag(out.PublicProfile, 0))
	d.Reads.respond(w, id, map[string]any{"profile": out}, out.Revision)
}

// developmentLinks rejects https links the read projection would drop
// (legacyLink), so an accepted value is always served back verbatim.
func developmentLinks(v ExtendedProfilePatch) error {
	check := func(pointer string, value Optional[string]) error {
		if !value.Set || value.Value == nil {
			return nil
		}
		if link := legacyLink(*value.Value); link == nil || *link != *value.Value {
			return ValidationError{pointer, ErrInvalid}
		}
		return nil
	}
	if e := check("/website", v.Website); e != nil {
		return e
	}
	for key, value := range v.Socials {
		if e := check("/socials/"+key, value); e != nil {
			return e
		}
	}
	return nil
}

// apply writes the patch to the legacy row under a row lock. The row's
// updated_at is the profile revision the reads serve; a commit advances it
// strictly (max(now, current+1)) so every acknowledged write is observable as
// a new revision. A wallet without a row has revision 0 and creates it.
func (d DevelopmentProfileWrites) apply(ctx context.Context, wallet string, v ExtendedProfilePatch) error {
	tx, e := d.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var row struct {
		displayName, bio, twitter, telegram, discord, website pgtype.Text
		createdAt, updatedAt                                  int64
	}
	exists := true
	e = tx.QueryRow(ctx, `SELECT display_name,bio,twitter,telegram,discord,website,created_at,updated_at FROM users.user_profiles WHERE LOWER(wallet_address)=$1 FOR UPDATE`, wallet).
		Scan(&row.displayName, &row.bio, &row.twitter, &row.telegram, &row.discord, &row.website, &row.createdAt, &row.updatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		exists = false
	} else if e != nil {
		return e
	}
	current := int64(0)
	if exists {
		current = max(row.updatedAt, 1)
	}
	if v.ExpectedRevision != strconv.FormatInt(current, 10) {
		return ErrConflict
	}
	now := d.now().Unix()
	if !exists {
		row.createdAt = now
	}
	if v.DisplayName != nil {
		row.displayName = pgtype.Text{String: *v.DisplayName, Valid: true}
	}
	if v.Bio != nil {
		row.bio = pgtype.Text{String: strings.TrimSpace(*v.Bio), Valid: true}
	}
	text := func(o Optional[string]) pgtype.Text {
		if o.Value == nil {
			return pgtype.Text{}
		}
		return pgtype.Text{String: *o.Value, Valid: true}
	}
	for key, value := range v.Socials {
		switch key {
		case "twitter":
			row.twitter = text(value)
		case "telegram":
			row.telegram = text(value)
		case "discord":
			row.discord = text(value)
		case "website":
			row.website = text(value)
		}
	}
	if v.Website.Set {
		row.website = text(v.Website)
	}
	next := max(now, current+1)
	if exists {
		_, e = tx.Exec(ctx, `UPDATE users.user_profiles SET display_name=$2,bio=$3,twitter=$4,telegram=$5,discord=$6,website=$7,updated_at=$8 WHERE LOWER(wallet_address)=$1`,
			wallet, row.displayName, row.bio, row.twitter, row.telegram, row.discord, row.website, next)
	} else {
		_, e = tx.Exec(ctx, `INSERT INTO users.user_profiles(wallet_address,display_name,bio,twitter,telegram,discord,website,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			wallet, row.displayName, row.bio, row.twitter, row.telegram, row.discord, row.website, row.createdAt, next)
	}
	if e != nil {
		var conflict interface{ SQLState() string }
		if errors.As(e, &conflict) && conflict.SQLState() == "23505" {
			return ErrConflict
		}
		return e
	}
	return tx.Commit(ctx)
}
