package metadata

// Development wiring of the production creator-media surface.
//
// Production serves PUT/GET /v2/tokens/{t}/assets/{id} (ConnectedMedia) and
// the /v3 metadata-publication routes (PublicationService) only under
// NODE_ENV=production, behind dependencies a development deployment cannot
// provide: the canonical indexer stream, the social moderation service over
// pinned mTLS, the cgroup-isolated raster worker, the users service's
// identity_guard_v2 and the indexer's committed block heads. The types below
// are the development substitutes for exactly those dependencies; the
// handlers, store, admission ledger, signature verification and publication
// worker they plug into are the production code.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
)

// DevelopmentConnectedSchema is the schema that holds the production media
// tables in a development database (beside the legacy "metadata" schema).
const DevelopmentConnectedSchema = "metadata_connected"

// EnsureDevelopmentConnectedSchema creates the production media, v2,
// publication, account-asset and operation-ledger tables (from the reviewed
// DDL sources the production migration owner uses) plus the two development
// bookkeeping tables, once, under an advisory lock. Development only.
func EnsureDevelopmentConnectedSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	store, e := NewProductionStore(pool, schema, nil)
	if e != nil {
		return e
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackV2(ctx, tx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", schema+":development-connected-schema"); e != nil {
		return e
	}
	ident := pgx.Identifier{schema}.Sanitize()
	if _, e = tx.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+ident); e != nil {
		return e
	}
	var present bool
	if e = tx.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", store.table("pools")).Scan(&present); e != nil {
		return e
	}
	if !present {
		if _, e = tx.Exec(ctx, store.SchemaDDL()+store.V2SchemaDDL()+store.ConnectedSchemaDDL()+store.PublicationSchemaDDL()+store.AccountAssetSchemaDDL()); e != nil {
			return fmt.Errorf("development connected schema: %w", e)
		}
		// The ledger DDL is unqualified: production resolves it through the
		// runtime role's schema-pinned search_path, and so does this one.
		if _, e = tx.Exec(ctx, "SET LOCAL search_path TO "+ident+",pg_catalog"); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, sharedauth.LedgerDDL); e != nil {
			return fmt.Errorf("development ledger schema: %w", e)
		}
	}
	if _, e = tx.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s(token text PRIMARY KEY,pool text NOT NULL,creator text NOT NULL,tx_hash text NOT NULL,token_json jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp());CREATE TABLE IF NOT EXISTS %s(pool text PRIMARY KEY,revision numeric(78,0) NOT NULL,projected_at timestamptz NOT NULL DEFAULT clock_timestamp());`, store.table("development_canonical_tokens"), store.table("development_legacy_projection"))); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// ── canonical identity ───────────────────────────────────────────────────────

// DevelopmentCanonicalSource replaces PostgresCanonicalSource (the indexer's
// canonical creation stream, which the development indexer does not
// produce). It resolves the token from the development indexer's pools row,
// read in the caller's transaction, and the TokenDeployed log of that row's
// creation receipt (DevelopmentV2Reads.chainToken). Like production, a token
// the indexer has not stored yet is ErrMissing (404): the creator retries
// after indexing, nothing is guessed.
type DevelopmentCanonicalSource struct {
	Reads *DevelopmentV2Reads
}

func (s DevelopmentCanonicalSource) ResolveTokenInTx(ctx context.Context, tx pgx.Tx, token string) (V2CanonicalIdentity, error) {
	if s.Reads == nil || tx == nil || !addressRE.MatchString(token) {
		return V2CanonicalIdentity{}, ErrUnrepresentable
	}
	row, e := developmentPoolRowFrom(ctx, tx, token)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	resolved, e := s.Reads.chainToken(ctx, row)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	return V2CanonicalIdentity{Token: resolved, PoolAddress: row.Pool, Creator: row.Creator}, nil
}

func (d *DevelopmentV2Reads) persistedToken(ctx context.Context, row developmentPoolRow) (iface.Token, bool, error) {
	if d.Connected == nil || d.Connected.Metadata.Store == nil {
		return iface.Token{}, false, nil
	}
	store := d.Connected.Metadata.Store
	var raw []byte
	e := store.Pool.QueryRow(ctx, "SELECT token_json FROM "+store.table("development_canonical_tokens")+" WHERE token=$1 AND pool=$2 AND creator=$3 AND tx_hash=$4", row.Token, row.Pool, row.Creator, row.TxHash).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return iface.Token{}, false, nil
	}
	if e != nil {
		return iface.Token{}, false, e
	}
	token, e := iface.DecodeToken(raw)
	if e != nil || token.Address != row.Token || token.SourcePosition == nil || token.SourcePosition.TransactionHash != row.TxHash {
		return iface.Token{}, false, ErrUnrepresentable
	}
	return token, true, nil
}

// persistToken keeps a resolved chain identity: the development RPC is
// pruned to a few hours of history, so a receipt resolvable at creation time
// is gone later. Best effort; the in-memory cache still holds the value.
func (d *DevelopmentV2Reads) persistToken(ctx context.Context, row developmentPoolRow, token iface.Token) {
	if d.Connected == nil || d.Connected.Metadata.Store == nil {
		return
	}
	raw, e := json.Marshal(token)
	if e != nil {
		return
	}
	store := d.Connected.Metadata.Store
	_, _ = store.Pool.Exec(context.WithoutCancel(ctx), "INSERT INTO "+store.table("development_canonical_tokens")+"(token,pool,creator,tx_hash,token_json) VALUES($1,$2,$3,$4,$5::jsonb) ON CONFLICT(token) DO NOTHING", row.Token, row.Pool, row.Creator, row.TxHash, string(raw))
}

// publishedDocument reports whether the token has a v2 document in the
// connected store (written by the publication worker).
func (d *DevelopmentV2Reads) publishedDocument(ctx context.Context, token string) (bool, error) {
	if d.Connected == nil || d.Connected.Metadata.Store == nil {
		return false, nil
	}
	store := d.Connected.Metadata.Store
	var exists bool
	e := store.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+store.table("pools")+" p JOIN "+store.table("v2_documents")+" d ON d.pool=p.pool WHERE p.token=$1)", token).Scan(&exists)
	return exists, e
}

// withTokenParam hands the production handlers the normalised (lower-case)
// token, as the development routes accept any address casing.
func withTokenParam(r *http.Request, token string) *http.Request {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		for i, key := range rctx.URLParams.Keys {
			if key == "tokenAddress" && i < len(rctx.URLParams.Values) {
				rctx.URLParams.Values[i] = token
			}
		}
	}
	return r
}

// ── moderation ───────────────────────────────────────────────────────────────

// DevelopmentVisibility replaces D12VisibilityClient. The moderation
// visibility authority (media/social's connected safety runtime) only runs in
// production, so every target reads "allowed" at revision 0: Restrict then
// never blocks a pool and a blocked flag set by hand in the documents table
// is kept (revisions only move forward).
type DevelopmentVisibility struct{}

func (DevelopmentVisibility) Read(ctx context.Context, pool string) (D12Visibility, error) {
	return DevelopmentVisibility{}.ReadTarget(ctx, "token", pool)
}
func (DevelopmentVisibility) ReadTarget(ctx context.Context, targetType, targetID string) (D12Visibility, error) {
	if e := ctx.Err(); e != nil {
		return D12Visibility{}, e
	}
	if (targetType != "token" && targetType != "asset") || (targetType == "token" && !addressRE.MatchString(targetID)) || (targetType == "asset" && !uuidRE.MatchString(targetID)) {
		return D12Visibility{}, raster.ErrUnavailable
	}
	return D12Visibility{TargetType: targetType, TargetID: targetID, Status: "allowed"}, nil
}

// DevelopmentSessionChannel stands in for the production private channel
// (gateway mTLS + signed service assertion + identity check). Outside
// production the browser calls metadata directly, so the channel's identity
// half is kept: every non-GET request and every asset read must carry a
// live v2 identity session, checked with the same authority the handlers
// use. Failures answer like the production channel.
func DevelopmentSessionChannel(identity RequestIdentityAuthority) func(http.Handler) http.Handler {
	return SessionChannel(identity)
}

// ── objects ──────────────────────────────────────────────────────────────────

// DevelopmentObjectsReadiness replaces the production bucket probe (public
// CDN origin, distinct private quarantine bucket verified by ACL and policy
// status, which MinIO does not report). It requires an https public origin
// without a path (variant URLs must satisfy the same immutable URL contract)
// and both buckets reachable. The two buckets may be the same development
// bucket: approved variants are served only through
// RegisterDevelopmentApprovedObjects, which never exposes quarantine keys.
func DevelopmentObjectsReadiness(o S3AssetObjects) func(context.Context) error {
	return func(ctx context.Context) error {
		u, e := url.Parse(o.PublicOrigin)
		if o.Client == nil || o.QuarantineBucket == "" || o.PublicBucket == "" || e != nil || !validHTTPS(o.PublicOrigin) || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return raster.ErrUnavailable
		}
		for _, bucket := range []string{o.QuarantineBucket, o.PublicBucket} {
			if _, e = o.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)}); e != nil {
				return raster.ErrUnavailable
			}
		}
		return nil
	}
}

// RegisterDevelopmentApprovedObjects serves approved, content-addressed
// variants (GET /approved/...) from the public bucket at PublicOrigin, the
// role a CDN in front of the public bucket plays in production. Only paths
// matching the immutable variant pattern are served, and the bytes must hash
// to the digest in their own key.
func (c ConnectedMedia) RegisterDevelopmentApprovedObjects(r chi.Router) {
	c.RegisterApprovedObjects(r)
}

// ── legacy projection ────────────────────────────────────────────────────────

// ProjectDevelopmentLegacy mirrors published v2 documents into the legacy
// metadata.token_metadata / metadata.token_images tables and the legacy
// logos/logo-<token>.png object, which the development web still reads
// (GET /metadata/{token}, /metadata/batch, /logo/{token}.png). A document is
// projected once per revision; blocked documents are not projected.
func (c ConnectedMedia) ProjectDevelopmentLegacy(ctx context.Context, q sqlcdb.Querier, legacy ObjectStore, now func() time.Time) error {
	store := c.Metadata.Store
	if store == nil || q == nil || legacy == nil {
		return raster.ErrUnavailable
	}
	if now == nil {
		now = time.Now
	}
	rows, e := store.Pool.Query(ctx, "SELECT p.pool,p.token,p.creator,d.revision::text,d.document,d.asset_id FROM "+store.table("pools")+" p JOIN "+store.table("v2_documents")+" d ON d.pool=p.pool LEFT JOIN "+store.table("documents")+" m ON m.pool=p.pool LEFT JOIN "+store.table("development_legacy_projection")+" x ON x.pool=p.pool WHERE d.revision>0 AND NOT COALESCE(m.blocked,false) AND (x.revision IS NULL OR x.revision<d.revision) ORDER BY d.updated_at NULLS FIRST,p.pool LIMIT 10")
	if e != nil {
		return e
	}
	type pending struct {
		pool, token, creator, revision string
		document                       []byte
		asset                          *string
	}
	var items []pending
	for rows.Next() {
		var p pending
		if e = rows.Scan(&p.pool, &p.token, &p.creator, &p.revision, &p.document, &p.asset); e != nil {
			rows.Close()
			return e
		}
		items = append(items, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, item := range items {
		var display V2Display
		if json.Unmarshal(item.document, &display) != nil {
			return ErrUnrepresentable
		}
		stamp := now().UnixMilli()
		existing, e := q.GetTokenMetadata(ctx, item.token)
		found := e == nil
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		params := sqlcdb.UpsertTokenMetadataParams{TokenAddress: item.token, PoolAddress: item.pool, Decimals: defaultDecimals, CreatedBy: item.creator, CreatedAt: stamp, UpdatedAt: stamp}
		if found {
			params.Decimals, params.CreatedAt = existing.Decimals, existing.CreatedAt
			if existing.UpdatedAt >= stamp {
				params.UpdatedAt = existing.UpdatedAt + 1
			}
		}
		if display.Name != nil {
			params.Name = pgtype.Text{String: *display.Name, Valid: true}
		}
		if display.Symbol != nil {
			params.Symbol = pgtype.Text{String: *display.Symbol, Valid: true}
		}
		params.Description = pgtype.Text{String: display.Description, Valid: display.Description != ""}
		tags := display.Tags
		if tags == nil {
			tags = []string{}
		}
		encoded, e := json.Marshal(tags)
		if e != nil {
			return e
		}
		params.CustomTags = pgtype.Text{String: string(encoded), Valid: true}
		for _, link := range display.Socials {
			column := map[string]*pgtype.Text{"website": &params.Website, "twitter": &params.Twitter, "telegram": &params.Telegram, "discord": &params.Discord}[legacySocialColumn(link.URL)]
			if !column.Valid {
				*column = pgtype.Text{String: link.URL, Valid: true}
			}
		}
		if item.asset != nil {
			var hash string
			var variants []byte
			e = store.Pool.QueryRow(ctx, "SELECT a.hash,d.variants FROM "+store.table("assets")+" a JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.id=$1 AND a.pool=$2 AND a.state='APPROVED'", *item.asset, item.pool).Scan(&hash, &variants)
			if e != nil {
				return e
			}
			var keys map[string]string
			if json.Unmarshal(variants, &keys) != nil || !strings.HasPrefix(keys["512"], "approved/"+hash+"/512/") {
				return ErrUnrepresentable
			}
			body, e := c.Objects.read(ctx, c.Objects.PublicBucket, keys["512"])
			if e != nil {
				return e
			}
			key := "logos/logo-" + item.token + ".png"
			if e = legacy.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/png"); e != nil {
				return e
			}
			if e = q.UpsertTokenImage(ctx, sqlcdb.UpsertTokenImageParams{ID: item.token + "-logo", TokenAddress: item.token, ImageType: "logo", StorageKey: key, MimeType: "image/png", SizeBytes: int32(len(body)), UploadedAt: stamp}); e != nil {
				return e
			}
		}
		if e = q.UpsertTokenMetadata(ctx, params); e != nil {
			return e
		}
		if _, e = store.Pool.Exec(ctx, "INSERT INTO "+store.table("development_legacy_projection")+"(pool,revision) VALUES($1,$2::numeric) ON CONFLICT(pool) DO UPDATE SET revision=EXCLUDED.revision,projected_at=clock_timestamp() WHERE "+store.table("development_legacy_projection")+".revision<EXCLUDED.revision", item.pool, item.revision); e != nil {
			return e
		}
	}
	return nil
}
