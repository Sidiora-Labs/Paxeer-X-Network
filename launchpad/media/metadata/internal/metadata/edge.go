package metadata

// Production components for platform-edge deployments (METADATA_INGRESS=edge,
// MODERATION_SOURCE=database) and for chains whose block hash is not the
// Keccak of the Ethereum header encoding (Paxeer, chain 125).

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/ethereum/go-ethereum/common"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ── ingress ──────────────────────────────────────────────────────────────────

// SessionChannel is the edge-ingress replacement for the gateway-signed
// private channel: every non-GET request and every asset read must carry a
// live v2 identity session, verified by identity (the identity service's
// request introspection). Public metadata/media reads pass through. Failures
// answer with the private channel's shape: 403 for a denied session, 503
// when the authority cannot answer.
func SessionChannel(identity RequestIdentityAuthority) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "/assets/") {
				next.ServeHTTP(w, r)
				return
			}
			deny := func(status int) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]string{"code": "media_channel_unavailable"})
			}
			if identity == nil {
				deny(http.StatusServiceUnavailable)
				return
			}
			if _, e := identity.AuthenticateRequest(r.Context(), r); e != nil {
				if errors.Is(e, sharedauth.ErrScopeDenied) {
					deny(http.StatusForbidden)
					return
				}
				deny(http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── publication identity ─────────────────────────────────────────────────────────────────

// SessionCheckIdentities is the publication identity authority of edge
// ingress (and of development): instead of the users service's
// users.identity_guard_v2 SQL function on a shared identity schema, it
// confirms the exact (account, session, wallet, auth epoch) with the
// identity service's private session check
// (POST /internal/v2/identity/session:check, IDENTITY_SERVICE_TOKEN): a
// revoked session, a rotated epoch or a changed wallet link is denied, and
// an authority that cannot answer is a retryable upstream failure.
type SessionCheckIdentities struct {
	Authority sharedauth.HTTPStreamAuthority
}

func (s SessionCheckIdentities) ConfirmTx(ctx context.Context, _ pgx.Tx, expected sharedauth.IdentityPrincipal) (sharedauth.IdentityPrincipal, error) {
	wallet, e := sharedauth.NormalizeWallet(expected.Wallet)
	if e != nil || wallet != expected.Wallet || !uuidRE.MatchString(expected.AccountID) || !uuidRE.MatchString(expected.SessionID) || expected.AuthEpoch < 0 {
		return sharedauth.IdentityPrincipal{}, sharedauth.ErrScopeDenied
	}
	if e = s.Authority.CheckStreamSession(ctx, expected.StreamPrincipal()); e != nil {
		var authority *sharedauth.IdentityAuthorityError
		if errors.As(e, &authority) && authority.Status != http.StatusServiceUnavailable && authority.Status != http.StatusTooManyRequests {
			return sharedauth.IdentityPrincipal{}, sharedauth.ErrScopeDenied
		}
		if !errors.As(e, &authority) && errors.Is(e, sharedauth.ErrScopeDenied) {
			return sharedauth.IdentityPrincipal{}, sharedauth.ErrScopeDenied
		}
		return sharedauth.IdentityPrincipal{}, &sharedauth.VerificationError{Category: "upstream", Retryable: true}
	}
	return expected, nil
}
func (s SessionCheckIdentities) ReadyFor(*pgxpool.Pool) bool {
	return strings.TrimSpace(s.Authority.Endpoint) != "" && len(s.Authority.ServiceToken) >= 32
}

// ── approved objects ─────────────────────────────────────────────────────────

// RegisterApprovedObjects serves approved, content-addressed variants
// (GET /approved/...) from the public bucket, the origin role a CDN in front
// of that bucket plays when PUBLIC_URL names one. Only paths matching the
// immutable variant pattern are served, and the bytes must hash to the
// digest in their own key; quarantine keys are never reachable.
func (c ConnectedMedia) RegisterApprovedObjects(r chi.Router) {
	r.Get("/approved/*", c.serveApproved)
}

func (c ConnectedMedia) serveApproved(w http.ResponseWriter, r *http.Request) {
	if c.Objects.Client == nil || c.Objects.PublicBucket == "" {
		v2Error(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if r.URL.RawQuery != "" || r.URL.RawPath != "" || !immutableURL(strings.TrimRight(c.Objects.PublicOrigin, "/")+r.URL.Path) {
		v2Failure(w, ErrMissing)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/")
	body, e := c.Objects.readVerifiedPNG(r.Context(), key)
	if e != nil {
		v2Failure(w, e)
		return
	}
	digest := sha256.Sum256(body)
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+hex.EncodeToString(digest[:])+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// readVerifiedPNG reads an approved variant from the public bucket and
// verifies it is a PNG whose sha256 is the last path segment of its key.
// A missing object is ErrMissing; any other failure is unavailable.
func (o S3AssetObjects) readVerifiedPNG(ctx context.Context, key string) ([]byte, error) {
	if o.Client == nil || o.PublicBucket == "" || !strings.HasPrefix(key, "approved/") || !strings.HasSuffix(key, ".png") || strings.Contains(key, "..") {
		return nil, raster.ErrUnavailable
	}
	object, e := o.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(o.PublicBucket), Key: aws.String(key)})
	if e != nil {
		var api smithy.APIError
		if errors.As(e, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NotFound") {
			return nil, ErrMissing
		}
		return nil, raster.ErrUnavailable
	}
	body, e := raster.ReadBounded(object.Body)
	object.Body.Close()
	if e != nil {
		return nil, raster.ErrUnavailable
	}
	digest := sha256.Sum256(body)
	if strings.TrimSuffix(key[strings.LastIndexByte(key, '/')+1:], ".png") != hex.EncodeToString(digest[:]) || http.DetectContentType(body) != "image/png" {
		return nil, raster.ErrUnavailable
	}
	return body, nil
}

// ── moderation ───────────────────────────────────────────────────────────────

// PostgresVisibilityAuthority is the visibility authority read from
// media/social's column-scoped cross-service view
// <Schema>.safety_visibility_v2(target_type,target_id,status,revision) on the
// shared database — the same rows social's own visibility endpoint serves.
// A target without a row is "allowed" at revision 0; a row must be a
// positive revision in state removed or restored. Any read failure is
// unavailable (fail closed: uploads and scans stop, nothing is published).
type PostgresVisibilityAuthority struct {
	Pool   *pgxpool.Pool
	Schema string
}

var visibilitySchemaRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func (a PostgresVisibilityAuthority) view() (string, bool) {
	if a.Pool == nil || !visibilitySchemaRE.MatchString(a.Schema) || a.Schema == "public" || a.Schema == "pg_catalog" {
		return "", false
	}
	return pgx.Identifier{a.Schema, "safety_visibility_v2"}.Sanitize(), true
}

func (a PostgresVisibilityAuthority) Read(ctx context.Context, pool string) (D12Visibility, error) {
	return a.ReadTarget(ctx, "token", pool)
}

func (a PostgresVisibilityAuthority) ReadTarget(ctx context.Context, targetType, targetID string) (D12Visibility, error) {
	view, ok := a.view()
	if !ok || (targetType != "token" && targetType != "asset") || (targetType == "token" && !addressRE.MatchString(targetID)) || (targetType == "asset" && !uuidRE.MatchString(targetID)) {
		return D12Visibility{}, raster.ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := D12Visibility{TargetType: targetType, TargetID: targetID}
	e := a.Pool.QueryRow(bounded, "SELECT revision,status FROM "+view+" WHERE target_type=$1 AND target_id=$2", targetType, targetID).Scan(&result.Revision, &result.Status)
	if errors.Is(e, pgx.ErrNoRows) {
		result.Status = "allowed"
		return result, nil
	}
	if e != nil {
		return D12Visibility{}, raster.ErrUnavailable
	}
	if result.Revision < 1 || (result.Status != "removed" && result.Status != "restored") {
		return D12Visibility{}, ErrUnrepresentable
	}
	return result, nil
}

// Ready probes that the view exists and is readable.
func (a PostgresVisibilityAuthority) Ready(ctx context.Context) error {
	view, ok := a.view()
	if !ok {
		return raster.ErrUnavailable
	}
	rows, e := a.Pool.Query(ctx, "SELECT target_type,target_id,status,revision FROM "+view+" LIMIT 0")
	if e != nil {
		return raster.ErrUnavailable
	}
	rows.Close()
	if rows.Err() != nil {
		return raster.ErrUnavailable
	}
	return nil
}

// ── committed wallet proofs ──────────────────────────────────────────────────

// ReportedCommittedProofVerifier verifies operation signatures against the
// indexer's committed block (Blocks) through sharedauth.CommittedProofVerifier
// over a sharedauth.ReportedHashBackend. The committed hash is the
// node-reported block hash; Paxeer (Cosmos-EVM) reports CometBFT-derived
// hashes, so go-ethereum's recomputed header hash is never compared or sent.
// Code and ERC-1271 reads run by the committed height with the reported hash
// checked before and after.
type ReportedCommittedProofVerifier struct {
	RPC     *gethrpc.Client
	Blocks  sharedauth.CommittedBlockProvider
	Observe sharedauth.ObserveVerification
}

func (v ReportedCommittedProofVerifier) Verify(ctx context.Context, wallet, message, signature string) error {
	if v.RPC == nil || v.Blocks == nil {
		return &sharedauth.VerificationError{Category: "upstream", Retryable: true}
	}
	return sharedauth.CommittedProofVerifier{Backend: sharedauth.NewReportedHashBackend(v.RPC), Blocks: v.Blocks, Observe: v.Observe}.Verify(ctx, wallet, message, signature)
}

// CheckCommittedHead reports whether the committed head is healthy and is
// the block the node reports at that height.
func (v ReportedCommittedProofVerifier) CheckCommittedHead(ctx context.Context) error {
	if v.RPC == nil || v.Blocks == nil {
		return raster.ErrUnavailable
	}
	height, hash, err := v.Blocks.CommittedBlock(ctx)
	if err != nil || height == nil || height.Sign() < 0 || !height.IsUint64() || hash == (common.Hash{}) {
		return &sharedauth.VerificationError{Category: "upstream", Retryable: true}
	}
	block, err := sharedauth.NewReportedHashBackend(v.RPC).ReportedBlockByNumber(ctx, height)
	if err != nil || block.Hash != hash {
		return &sharedauth.VerificationError{Category: "upstream", Retryable: true}
	}
	return nil
}

// legacySocialColumn maps a social link to the legacy socials column.
func legacySocialColumn(link string) string {
	u, e := url.Parse(link)
	if e != nil {
		return "website"
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	switch {
	case host == "x.com" || host == "twitter.com" || host == "mobile.twitter.com":
		return "twitter"
	case host == "t.me" || host == "telegram.me" || host == "telegram.org":
		return "telegram"
	case host == "discord.gg" || host == "discord.com" || host == "discordapp.com":
		return "discord"
	}
	return "website"
}
