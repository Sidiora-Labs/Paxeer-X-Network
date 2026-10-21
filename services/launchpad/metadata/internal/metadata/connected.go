package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	raster "github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedsecrets "github.com/Sidiora-Technologies/KindleLaunch/shared/secrets"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type S3AssetObjects struct {
	Client                                       *s3.Client
	QuarantineBucket, PublicBucket, PublicOrigin string
	// Endpoint, when set, is the S3 API origin the readiness probe also
	// calls without credentials: an anonymous list and an anonymous read of
	// the quarantine bucket must both be refused (403). It is the direct
	// proof of privacy on S3-compatible stores (MinIO) that do not implement
	// GetBucketPolicyStatus; there it replaces that call.
	Endpoint string
	// Readiness, when set, replaces the production bucket-policy probe below.
	// Only the development wiring sets it (DevelopmentObjectsReadiness); the
	// production wiring leaves it nil.
	Readiness func(context.Context) error
}

func (o S3AssetObjects) Ready(ctx context.Context) error {
	if o.Readiness != nil {
		return o.Readiness(ctx)
	}
	u, e := url.Parse(o.PublicOrigin)
	if o.Client == nil || o.QuarantineBucket == "" || o.PublicBucket == "" || o.PublicBucket == o.QuarantineBucket || e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return raster.ErrUnavailable
	}
	acl, e := o.Client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(o.QuarantineBucket)})
	if e != nil {
		return raster.ErrUnavailable
	}
	for _, grant := range acl.Grants {
		if grant.Grantee != nil && grant.Grantee.URI != nil {
			return raster.ErrUnavailable
		}
	}
	policy, e := o.Client.GetBucketPolicyStatus(ctx, &s3.GetBucketPolicyStatusInput{Bucket: aws.String(o.QuarantineBucket)})
	if e != nil {
		var api smithy.APIError
		if o.Endpoint == "" || !errors.As(e, &api) || api.ErrorCode() != "NotImplemented" {
			return raster.ErrUnavailable
		}
	} else if policy.PolicyStatus == nil || policy.PolicyStatus.IsPublic == nil || *policy.PolicyStatus.IsPublic {
		return raster.ErrUnavailable
	}
	if o.Endpoint != "" {
		if e = anonymousAccessDenied(ctx, o.Endpoint, o.QuarantineBucket); e != nil {
			return e
		}
	}
	_, e = o.Client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(o.PublicBucket)})
	if e != nil {
		return raster.ErrUnavailable
	}
	return nil
}

// anonymousAccessDenied proves an S3 bucket refuses unauthenticated access:
// an unsigned ListObjectsV2 and an unsigned GetObject of a random key must
// both answer 403. A 404 on the object would mean anonymous reads reach the
// keyspace; a 200 on the listing would expose it.
func anonymousAccessDenied(ctx context.Context, endpoint, bucket string) error {
	base, e := url.Parse(strings.TrimRight(endpoint, "/"))
	if e != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || bucket == "" || strings.ContainsAny(bucket, "/?#") {
		return raster.ErrUnavailable
	}
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, target := range []string{base.String() + "/" + url.PathEscape(bucket) + "?list-type=2&max-keys=1", base.String() + "/" + url.PathEscape(bucket) + "/quarantine/" + uuid.NewString()} {
		request, e := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if e != nil {
			return raster.ErrUnavailable
		}
		response, e := client.Do(request)
		if e != nil {
			return raster.ErrUnavailable
		}
		io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			return raster.ErrUnavailable
		}
	}
	return nil
}
func (o S3AssetObjects) put(ctx context.Context, bucket, key string, body []byte, mime string) error {
	if o.Client == nil || bucket == "" {
		return raster.ErrUnavailable
	}
	_, e := o.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body))), ContentType: aws.String(mime), IfNoneMatch: aws.String("*")})
	if e == nil {
		stored, e := o.read(ctx, bucket, key)
		if e != nil {
			return e
		}
		if !bytes.Equal(stored, body) {
			return ErrConflict
		}
		return nil
	}
	var api smithy.APIError
	if !errors.As(e, &api) || (api.ErrorCode() != "PreconditionFailed" && api.ErrorCode() != "ConditionalRequestConflict") {
		return raster.ErrUnavailable
	}
	old, e := o.read(ctx, bucket, key)
	if e != nil {
		return e
	}
	if !bytes.Equal(old, body) {
		return ErrConflict
	}
	return nil
}
func (o S3AssetObjects) read(ctx context.Context, bucket, key string) ([]byte, error) {
	object, e := o.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if e != nil {
		return nil, raster.ErrUnavailable
	}
	defer object.Body.Close()
	body, e := raster.ReadBounded(object.Body)
	if e != nil {
		return nil, e
	}
	return body, nil
}
func (o S3AssetObjects) PutImmutable(ctx context.Context, key string, body []byte) error {
	if !strings.HasPrefix(key, "approved/") || strings.Contains(key, "..") || len(body) == 0 || len(body) > 6<<20 {
		return ErrValidation
	}
	return o.put(ctx, o.PublicBucket, key, body, "image/png")
}

type RequestIdentityAuthority interface {
	AuthenticateRequest(context.Context, *http.Request) (sharedauth.IdentityPrincipal, error)
}

// MediaWorker decodes and re-encodes untrusted rasters. Production uses
// raster.ProcessWorker (separate process in its own cgroup v2 group).
type MediaWorker interface {
	RasterWorker
	Ready() error
}

// VisibilityAuthority is the moderation visibility source. Production uses
// D12VisibilityClient (the social moderation service over pinned mTLS).
type VisibilityAuthority interface {
	Read(context.Context, string) (D12Visibility, error)
	ReadTarget(context.Context, string, string) (D12Visibility, error)
}

type ConnectedMedia struct {
	Metadata   V2ContractHandlers
	Identity   RequestIdentityAuthority
	Objects    S3AssetObjects
	Scanner    sharedhttp.ScanOptions
	Worker     MediaWorker
	Moderation VisibilityAuthority
}

func (c ConnectedMedia) Ready(ctx context.Context) error {
	if c.Metadata.Store == nil || c.Metadata.Canonical == nil || c.Metadata.Store.Verifier == nil || c.Worker == nil || c.Moderation == nil {
		return raster.ErrUnavailable
	}
	if e := c.Worker.Ready(); e != nil {
		return e
	}
	if _, e := sharedhttp.ScannerVersion(ctx, c.Scanner); e != nil {
		return raster.ErrUnavailable
	}
	return c.Objects.Ready(ctx)
}
func (s *ProductionStore) ConnectedSchemaDDL() string {
	return fmt.Sprintf(`ALTER TABLE %s ADD COLUMN search_document jsonb NOT NULL DEFAULT '{}';CREATE TABLE %s(asset_id text PRIMARY KEY REFERENCES %s(id),mime text NOT NULL,width integer NOT NULL CHECK(width BETWEEN 1 AND 8192),height integer NOT NULL CHECK(height BETWEEN 1 AND 8192),variants jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),rejected_until timestamptz,investigation_hold boolean NOT NULL DEFAULT false);`, s.table("v2_documents"), s.table("asset_details_v2"), s.table("assets"))
}
func (c ConnectedMedia) RegisterRoutes(r chi.Router) {
	r.Put("/v2/accounts/{wallet}/assets/{assetId}", c.uploadAccountAsset)
	r.Get("/v2/accounts/{wallet}/assets/{assetId}", c.getOwnedAccountAsset)
	r.Get("/v2/tokens/{tokenAddress}/metadata", c.getMetadata)
	r.Patch("/v2/tokens/{tokenAddress}/metadata", c.patchMetadata)
	r.Get("/v2/tokens/{tokenAddress}/media", c.media)
	c.RegisterTokenAssetRoutes(r)
}

// RegisterTokenAssetRoutes mounts only the creator token-image upload and
// poll routes (the subset the development wiring serves).
func (c ConnectedMedia) RegisterTokenAssetRoutes(r chi.Router) {
	r.Put("/v2/tokens/{tokenAddress}/assets/{assetId}", c.upload)
	r.Get("/v2/tokens/{tokenAddress}/assets/{assetId}", c.asset)
}
func (c ConnectedMedia) creator(ctx context.Context, r *http.Request) (V2CanonicalIdentity, error) {
	if c.Metadata.Store == nil || c.Metadata.Canonical == nil || c.Identity == nil {
		return V2CanonicalIdentity{}, raster.ErrUnavailable
	}
	token := chi.URLParam(r, "tokenAddress")
	tx, e := c.Metadata.Store.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	defer rollbackV2(ctx, tx)
	identity, e := c.Metadata.identity(ctx, tx, token)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	principal, e := c.Identity.AuthenticateRequest(ctx, r)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	if identity.Creator != principal.Wallet {
		return V2CanonicalIdentity{}, ErrForbidden
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+c.Metadata.Store.table("pools")+"(pool,creator,token) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", identity.PoolAddress, identity.Creator, token)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	var storedPool, storedCreator string
	e = tx.QueryRow(ctx, "SELECT pool,creator FROM "+c.Metadata.Store.table("pools")+" WHERE token=$1 FOR UPDATE", token).Scan(&storedPool, &storedCreator)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	if storedPool != identity.PoolAddress || storedCreator != identity.Creator {
		return V2CanonicalIdentity{}, ErrForbidden
	}
	if e = tx.Commit(ctx); e != nil {
		return V2CanonicalIdentity{}, e
	}
	return identity, nil
}
func (c ConnectedMedia) upload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	identity, e := c.creator(ctx, r)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if e = c.syncModeration(ctx, identity.PoolAddress); e != nil {
		v2Failure(w, e)
		return
	}
	id := chi.URLParam(r, "assetId")
	if !uuidRE.MatchString(id) || r.Header.Get("Idempotency-Key") != id {
		v2Failure(w, ErrValidation)
		return
	}
	if e = c.Ready(ctx); e != nil {
		v2Failure(w, e)
		return
	}
	body, e := raster.ReadBounded(r.Body)
	if e != nil {
		v2Error(w, 413, "image_size_limit")
		return
	}
	mime := http.DetectContentType(body)
	declared := strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])
	if mime != declared || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp") {
		v2Error(w, 415, "unsupported_image")
		return
	}
	decoded, e := c.Worker.Decode(ctx, body)
	if e != nil {
		if errors.Is(e, raster.ErrInvalid) {
			v2Error(w, 415, "unsupported_image")
		} else if errors.Is(e, raster.ErrLimit) {
			v2Error(w, 422, "image_resource_limit")
		} else {
			v2Failure(w, e)
		}
		return
	}
	store := c.Metadata.Store
	connection, e := store.Pool.Acquire(ctx)
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer connection.Release()
	lock := store.Schema + ":" + id
	if _, e = connection.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1,0))", lock); e != nil {
		v2Failure(w, e)
		return
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		if _, err := connection.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lock); err != nil {
			connection.Conn().Close(cleanup)
		}
	}()
	hash := sha256.Sum256(body)
	record := AssetRecord{ID: id, Pool: identity.PoolAddress, Hash: hex.EncodeToString(hash[:]), Size: int64(len(body)), ObjectKey: "quarantine/" + id}
	if e = store.CreateConnectedAsset(ctx, record, mime, decoded.Width, decoded.Height); e != nil {
		v2Failure(w, e)
		return
	}
	var state string
	if e = store.Pool.QueryRow(ctx, "SELECT state FROM "+store.table("assets")+" WHERE id=$1", id).Scan(&state); e != nil {
		v2Failure(w, e)
		return
	}
	if state == "REJECTED" || state == "DELETED" {
		v2Failure(w, ErrForbidden)
		return
	}
	if state == "APPROVED" {
		c.respondAsset(w, r, id, http.StatusOK)
		return
	}
	if state == "CREATED" {
		if e = store.TransitionAsset(ctx, id, "CREATED", "UPLOADING"); e != nil {
			v2Failure(w, e)
			return
		}
		state = "UPLOADING"
	}
	if state == "UPLOADING" {
		if e = c.Objects.put(ctx, c.Objects.QuarantineBucket, record.ObjectKey, body, mime); e != nil {
			v2Failure(w, e)
			return
		}
		if e = store.TransitionAsset(ctx, id, "UPLOADING", "QUARANTINED"); e != nil {
			v2Failure(w, e)
			return
		}
		state = "QUARANTINED"
	}
	stored, e := c.Objects.read(ctx, c.Objects.QuarantineBucket, record.ObjectKey)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if !bytes.Equal(stored, body) {
		v2Failure(w, ErrConflict)
		return
	}
	if state == "QUARANTINED" {
		if e = store.TransitionAsset(ctx, id, state, "VALIDATING"); e != nil {
			v2Failure(w, e)
			return
		}
		state = "VALIDATING"
	}
	scan := sharedhttp.ScanVerifiedBuffer(ctx, stored, c.Scanner)
	if scan.Verdict == sharedhttp.ScanInfected {
		if state != "VALIDATING" {
			v2Failure(w, ErrForbidden)
			return
		}
		tag, e := store.Pool.Exec(ctx, "WITH rejected AS(UPDATE "+store.table("assets")+" SET state='REJECTED',scanner_engine=$2,scanner_revision=$3 WHERE id=$1 AND state='VALIDATING' RETURNING id) UPDATE "+store.table("asset_details_v2")+" SET rejected_until=clock_timestamp()+interval '24 hours' WHERE asset_id IN(SELECT id FROM rejected)", id, scan.Engine, scan.SignatureRevision)
		if e != nil {
			v2Failure(w, e)
			return
		}
		if tag.RowsAffected() != 1 {
			v2Failure(w, ErrConflict)
			return
		}
		v2Error(w, 422, "image_rejected")
		return
	}
	if scan.Verdict != sharedhttp.ScanClean {
		c.respondAsset(w, r, id, http.StatusAccepted)
		return
	}
	promoted := *store
	promoted.RasterWorker = c.Worker
	if e = promoted.Promote(ctx, id, stored, decoded.Variants, scan, c.Objects); e != nil {
		v2Failure(w, e)
		return
	}
	c.respondAsset(w, r, id, http.StatusOK)
}
func (c ConnectedMedia) asset(w http.ResponseWriter, r *http.Request) {
	if c.Metadata.Store == nil {
		v2Failure(w, raster.ErrUnavailable)
		return
	}
	var pool string
	e := c.Metadata.Store.Pool.QueryRow(r.Context(), "SELECT pool FROM "+c.Metadata.Store.table("pools")+" WHERE token=$1", chi.URLParam(r, "tokenAddress")).Scan(&pool)
	if errors.Is(e, pgx.ErrNoRows) {
		v2Failure(w, ErrMissing)
		return
	}
	if e != nil {
		v2Failure(w, e)
		return
	}
	if e = c.syncModeration(r.Context(), pool); e != nil {
		v2Failure(w, e)
		return
	}
	c.respondAsset(w, r, chi.URLParam(r, "assetId"), http.StatusOK)
}
func (c ConnectedMedia) respondAsset(w http.ResponseWriter, r *http.Request, id string, status int) {
	if c.Metadata.Store == nil || !uuidRE.MatchString(id) {
		v2Failure(w, ErrValidation)
		return
	}
	store := c.Metadata.Store
	var a AssetDomain
	var token string
	var blocked bool
	var variants []byte
	e := store.Pool.QueryRow(r.Context(), "SELECT a.id,p.creator,a.hash,d.mime,a.state,a.size,p.token,COALESCE(m.blocked,false),d.variants FROM "+store.table("assets")+" a JOIN "+store.table("pools")+" p ON p.pool=a.pool LEFT JOIN "+store.table("documents")+" m ON m.pool=a.pool LEFT JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.id=$1", id).Scan(&a.ID, &a.Owner, &a.Hash, &a.MIME, &a.State, &a.Size, &token, &blocked, &variants)
	if errors.Is(e, pgx.ErrNoRows) || blocked || token != chi.URLParam(r, "tokenAddress") {
		v2Failure(w, ErrMissing)
		return
	}
	if e != nil {
		v2Failure(w, e)
		return
	}
	if a.State == "APPROVED" {
		var keys map[string]string
		if json.Unmarshal(variants, &keys) != nil || len(keys) != 3 {
			v2Failure(w, raster.ErrUnavailable)
			return
		}
		key := keys["512"]
		link := c.Objects.PublicOrigin + "/" + key
		if !immutableURL(link) || !strings.HasPrefix(key, "approved/"+a.Hash+"/512/") {
			v2Failure(w, ErrUnrepresentable)
			return
		}
		a.ApprovedURL = &link
	}
	requestID := r.Header.Get("Idempotency-Key")
	if requestID == "" {
		requestID = uuid.NewString()
	}
	response, e := AssetResponse(requestID, a, time.Now())
	if e != nil {
		v2Failure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(response)
}

func (c ConnectedMedia) ExpireRejected(ctx context.Context) error {
	if c.Metadata.Store == nil || c.Objects.Client == nil {
		return raster.ErrUnavailable
	}
	store := c.Metadata.Store
	rows, e := store.Pool.Query(ctx, "SELECT a.id,a.object_key FROM "+store.table("assets")+" a JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.state='REJECTED' AND d.rejected_until IS NOT NULL AND d.rejected_until<=clock_timestamp() AND NOT d.investigation_hold LIMIT 100")
	if e != nil {
		return e
	}
	type rejected struct{ id, key string }
	var records []rejected
	for rows.Next() {
		var a rejected
		if e = rows.Scan(&a.id, &a.key); e != nil {
			rows.Close()
			return e
		}
		records = append(records, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, a := range records {
		if _, e = c.Objects.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.Objects.QuarantineBucket), Key: aws.String(a.key)}); e != nil {
			return raster.ErrUnavailable
		}
		if e = store.TransitionAsset(ctx, a.id, "REJECTED", "DELETED"); e != nil {
			return e
		}
	}
	return nil
}

var _ ImmutablePublisher = S3AssetObjects{}
var _ RasterWorker = raster.ProcessWorker{}

type ConnectedImage struct {
	AssetID  string            `json:"assetId"`
	SHA256   string            `json:"sha256"`
	MIME     string            `json:"mime"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	Variants map[string]string `json:"variants"`
}
type ConnectedMetadata struct {
	Metadata V2Data          `json:"metadata"`
	Image    *ConnectedImage `json:"image"`
}

func (c ConnectedMedia) media(w http.ResponseWriter, r *http.Request) {
	if c.Metadata.Store == nil || c.Metadata.Canonical == nil {
		v2Error(w, 503, "unavailable")
		return
	}
	store := c.Metadata.Store
	tx, e := store.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer rollbackV2(r.Context(), tx)
	identity, e := c.Metadata.identity(r.Context(), tx, chi.URLParam(r, "tokenAddress"))
	if e != nil {
		v2Failure(w, e)
		return
	}
	data, e := store.readV2(r.Context(), tx, identity.Token.Address)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if data.PoolAddress != identity.PoolAddress {
		v2Failure(w, ErrUnrepresentable)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		v2Failure(w, e)
		return
	}
	if e = c.syncModeration(r.Context(), identity.PoolAddress); e != nil {
		v2Failure(w, e)
		return
	}
	tx, e = store.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer rollbackV2(r.Context(), tx)
	data, e = store.readV2(r.Context(), tx, identity.Token.Address)
	if e != nil {
		v2Failure(w, e)
		return
	}
	result := ConnectedMetadata{Metadata: data}
	if data.LogoAssetID != nil && data.ModerationStatus == "allowed" {
		image := ConnectedImage{AssetID: *data.LogoAssetID}
		var encoded []byte
		e = tx.QueryRow(r.Context(), "SELECT a.hash,d.mime,d.width,d.height,d.variants FROM "+store.table("assets")+" a JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.id=$1 AND a.pool=$2 AND a.state='APPROVED'", image.AssetID, data.PoolAddress).Scan(&image.SHA256, &image.MIME, &image.Width, &image.Height, &encoded)
		if e != nil {
			v2Failure(w, e)
			return
		}
		var keys map[string]string
		if json.Unmarshal(encoded, &keys) != nil || len(keys) != 3 {
			v2Failure(w, ErrUnrepresentable)
			return
		}
		image.Variants = map[string]string{}
		for _, size := range []string{"256", "512", "1024"} {
			key := keys[size]
			link := c.Objects.PublicOrigin + "/" + key
			if !immutableURL(link) || !strings.HasPrefix(key, "approved/"+image.SHA256+"/"+size+"/") {
				v2Failure(w, ErrUnrepresentable)
				return
			}
			image.Variants[size] = link
		}
		image.SHA256 = "0x" + image.SHA256
		result.Image = &image
	}
	if e = tx.Commit(r.Context()); e != nil {
		v2Failure(w, e)
		return
	}
	etag := fmt.Sprintf(`"metadata-media-v2-%s-%s-%s"`, data.PoolAddress, data.Revision, data.ModerationStatus)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=60")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	response, e := envelope(uuid.NewString(), result, "partial", time.Now())
	if e != nil {
		v2Failure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type D12VisibilityClient struct {
	Endpoint string
	Secrets  sharedsecrets.Provider
	TokenKey string
	Client   *http.Client
}
type D12Visibility struct {
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	Status     string `json:"status"`
	CaseID     string `json:"caseId"`
	Revision   int64  `json:"revision"`
}

func (c D12VisibilityClient) Read(ctx context.Context, pool string) (D12Visibility, error) {
	return c.ReadTarget(ctx, "token", pool)
}
func (c D12VisibilityClient) ReadTarget(ctx context.Context, targetType, targetID string) (D12Visibility, error) {
	var result D12Visibility
	endpoint, e := url.Parse(c.Endpoint)
	if e != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || c.Secrets == nil || c.TokenKey == "" || (targetType != "token" && targetType != "asset") || (targetType == "token" && !addressRE.MatchString(targetID)) || (targetType == "asset" && !uuidRE.MatchString(targetID)) {
		return result, raster.ErrUnavailable
	}
	token, e := c.Secrets.GetRequired(ctx, c.TokenKey)
	if e != nil || token == "" || strings.ContainsAny(token, "\r\n") {
		return result, raster.ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(bounded, "GET", c.Endpoint+"/internal/v2/safety/visibility?targetType="+targetType+"&targetId="+url.QueryEscape(targetID), nil)
	if e != nil {
		return result, raster.ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.Client != nil {
		client.Transport = c.Client.Transport
	}
	response, e := client.Do(req)
	if e != nil {
		return result, raster.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result, raster.ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 16385))
	if e != nil || len(raw) > 16384 {
		return result, raster.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.TargetType != targetType || result.TargetID != targetID || !((result.Revision == 0 && result.CaseID == "" && result.Status == "allowed") || (result.Revision > 0 && uuidRE.MatchString(result.CaseID) && (result.Status == "removed" || result.Status == "restored"))) {
		return D12Visibility{}, ErrUnrepresentable
	}
	return result, nil
}
func (c ConnectedMedia) syncModeration(ctx context.Context, pool string) error {
	if c.Metadata.Store == nil || c.Moderation == nil {
		return raster.ErrUnavailable
	}
	visibility, e := c.Moderation.Read(ctx, pool)
	if e != nil {
		return e
	}
	return c.Metadata.Store.Restrict(ctx, pool, strconv.FormatInt(visibility.Revision, 10), visibility.Status == "removed")
}

func (c ConnectedMedia) patchMetadata(w http.ResponseWriter, r *http.Request) {
	identity, e := c.creator(r.Context(), r)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if strings.ToLower(r.Header.Get("X-Action-Wallet")) != identity.Creator {
		v2Failure(w, ErrForbidden)
		return
	}
	if e = c.syncModeration(r.Context(), identity.PoolAddress); e != nil {
		v2Failure(w, e)
		return
	}
	connected := c.Metadata
	store := *connected.Store
	store.ConnectedSearch = true
	connected.Store = &store
	connected.patch(w, r)
}
func (c ConnectedMedia) getMetadata(w http.ResponseWriter, r *http.Request) {
	if c.Metadata.Store == nil {
		v2Failure(w, raster.ErrUnavailable)
		return
	}
	var pool string
	e := c.Metadata.Store.Pool.QueryRow(r.Context(), "SELECT pool FROM "+c.Metadata.Store.table("pools")+" WHERE token=$1", chi.URLParam(r, "tokenAddress")).Scan(&pool)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		v2Failure(w, e)
		return
	}
	if e == nil {
		if e = c.syncModeration(r.Context(), pool); e != nil {
			v2Failure(w, e)
			return
		}
	}
	c.Metadata.get(w, r)
}

func (s *ProductionStore) AccountAssetSchemaDDL() string {
	return fmt.Sprintf(`CREATE TABLE %s(id uuid PRIMARY KEY,owner_wallet text NOT NULL CHECK(owner_wallet ~ '^0x[a-f0-9]{40}$'),hash text NOT NULL CHECK(hash ~ '^[a-f0-9]{64}$'),size bigint NOT NULL CHECK(size BETWEEN 1 AND 6291456),mime text NOT NULL CHECK(mime IN('image/png','image/jpeg','image/webp')),width integer NOT NULL CHECK(width BETWEEN 1 AND 8192),height integer NOT NULL CHECK(height BETWEEN 1 AND 8192),state text NOT NULL CHECK(state IN('UPLOADING','QUARANTINED','VALIDATING','APPROVED','REJECTED','DELETED')),variants jsonb NOT NULL DEFAULT '{}',scanner_engine text NOT NULL DEFAULT '',scanner_revision text NOT NULL DEFAULT '',moderation_revision bigint NOT NULL DEFAULT 0,blocked boolean NOT NULL DEFAULT false,rejected_until timestamptz,investigation_hold boolean NOT NULL DEFAULT false,deletion_id uuid,created_at timestamptz NOT NULL DEFAULT clock_timestamp());CREATE INDEX ON %s(state,created_at);`, s.table("account_assets_v2"), s.table("account_assets_v2"))
}
func (c ConnectedMedia) RegisterAccountAuthority(r chi.Router) {
	r.Get("/internal/v2/accounts/{wallet}/assets/{assetId}", c.getAccountAsset)
	r.Delete("/internal/v2/accounts/{wallet}/assets", c.purgeAccountAssets)
}
func (c ConnectedMedia) getOwnedAccountAsset(w http.ResponseWriter, r *http.Request) {
	if c.Identity == nil {
		v2Failure(w, raster.ErrUnavailable)
		return
	}
	principal, e := c.Identity.AuthenticateRequest(r.Context(), r)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if principal.Wallet != chi.URLParam(r, "wallet") {
		v2Failure(w, ErrForbidden)
		return
	}
	c.getAccountAsset(w, r)
}
func (c ConnectedMedia) getAccountAsset(w http.ResponseWriter, r *http.Request) {
	if c.Metadata.Store == nil {
		v2Failure(w, raster.ErrUnavailable)
		return
	}
	wallet, id := chi.URLParam(r, "wallet"), chi.URLParam(r, "assetId")
	if !addressRE.MatchString(wallet) || !uuidRE.MatchString(id) {
		v2Failure(w, ErrValidation)
		return
	}
	if e := c.syncAccountModeration(r.Context(), id); e != nil {
		v2Failure(w, e)
		return
	}
	var a WireAsset
	var variants []byte
	var blocked bool
	var engine, signature string
	e := c.Metadata.Store.Pool.QueryRow(r.Context(), "SELECT id::text,owner_wallet,hash,mime,size,state,variants,blocked,scanner_engine,scanner_revision FROM "+c.Metadata.Store.table("account_assets_v2")+" WHERE id=$1 AND owner_wallet=$2", id, wallet).Scan(&a.AssetID, &a.OwnerWallet, &a.SHA256, &a.MediaType, &a.Bytes, &a.State, &variants, &blocked, &engine, &signature)
	if errors.Is(e, pgx.ErrNoRows) {
		v2Failure(w, ErrMissing)
		return
	}
	if e != nil {
		v2Failure(w, e)
		return
	}
	if blocked || a.State == "DELETED" {
		v2Failure(w, ErrMissing)
		return
	}
	a.SHA256 = "0x" + a.SHA256
	states := map[string]string{"UPLOADING": "uploading", "QUARANTINED": "quarantined", "VALIDATING": "quarantined", "REJECTED": "rejected", "APPROVED": "approved"}
	a.State = states[a.State]
	if a.State == "" {
		v2Failure(w, ErrUnrepresentable)
		return
	}
	if a.State == "approved" {
		var keys map[string]string
		if engine == "" || signature == "" || json.Unmarshal(variants, &keys) != nil || len(keys) != 3 {
			v2Failure(w, raster.ErrUnavailable)
			return
		}
		key := keys["512"]
		link := c.Objects.PublicOrigin + "/" + key
		if !immutableURL(link) || !strings.HasPrefix(key, "approved/account/"+wallet+"/"+id+"/512/") {
			v2Failure(w, ErrUnrepresentable)
			return
		}
		a.PublicURL = &link
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPut && a.State != "approved" && a.State != "rejected" {
		w.WriteHeader(http.StatusAccepted)
	}
	json.NewEncoder(w).Encode(a)
}
func (c ConnectedMedia) syncAccountModeration(ctx context.Context, id string) error {
	if c.Metadata.Store == nil || c.Moderation == nil {
		return raster.ErrUnavailable
	}
	v, e := c.Moderation.ReadTarget(ctx, "asset", id)
	if e != nil {
		return e
	}
	_, e = c.Metadata.Store.Pool.Exec(ctx, "UPDATE "+c.Metadata.Store.table("account_assets_v2")+" SET moderation_revision=$2,blocked=$3 WHERE id=$1 AND moderation_revision<$2", id, v.Revision, v.Status == "removed")
	return e
}
func (c ConnectedMedia) uploadAccountAsset(w http.ResponseWriter, r *http.Request) {
	if c.Metadata.Store == nil || c.Identity == nil || c.Worker == nil {
		v2Failure(w, raster.ErrUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	wallet, id := chi.URLParam(r, "wallet"), chi.URLParam(r, "assetId")
	if !addressRE.MatchString(wallet) || !uuidRE.MatchString(id) || r.Header.Get("Idempotency-Key") != id {
		v2Failure(w, ErrValidation)
		return
	}
	principal, e := c.Identity.AuthenticateRequest(ctx, r)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if principal.Wallet != wallet {
		v2Failure(w, ErrForbidden)
		return
	}
	body, e := raster.ReadBounded(r.Body)
	if e != nil {
		v2Error(w, 413, "image_size_limit")
		return
	}
	mime := http.DetectContentType(body)
	if mime != strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp") {
		v2Error(w, 415, "unsupported_image")
		return
	}
	decoded, e := c.Worker.Decode(ctx, body)
	if e != nil {
		if errors.Is(e, raster.ErrInvalid) {
			v2Error(w, 415, "unsupported_image")
		} else if errors.Is(e, raster.ErrLimit) {
			v2Error(w, 422, "image_resource_limit")
		} else {
			v2Failure(w, e)
		}
		return
	}
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	store := c.Metadata.Store
	_, e = store.Pool.Exec(ctx, "INSERT INTO "+store.table("account_assets_v2")+"(id,owner_wallet,hash,size,mime,width,height,state) VALUES($1,$2,$3,$4,$5,$6,$7,'UPLOADING') ON CONFLICT(id) DO NOTHING", id, wallet, hash, len(body), mime, decoded.Width, decoded.Height)
	if e != nil {
		v2Failure(w, e)
		return
	}
	var owner, oldhash, state, oldmime string
	var size int64
	var width, height int
	e = store.Pool.QueryRow(ctx, "SELECT owner_wallet,hash,size,state,mime,width,height FROM "+store.table("account_assets_v2")+" WHERE id=$1", id).Scan(&owner, &oldhash, &size, &state, &oldmime, &width, &height)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if owner != wallet || oldhash != hash || size != int64(len(body)) || oldmime != mime || width != decoded.Width || height != decoded.Height {
		v2Failure(w, ErrConflict)
		return
	}
	if state == "REJECTED" || state == "DELETED" {
		v2Failure(w, ErrForbidden)
		return
	}
	if e = c.Objects.Ready(ctx); e != nil {
		v2Failure(w, e)
		return
	}
	tx, e := store.Pool.Begin(ctx)
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer rollbackV2(ctx, tx)
	if e = tx.QueryRow(ctx, "SELECT state FROM "+store.table("account_assets_v2")+" WHERE id=$1 FOR UPDATE", id).Scan(&state); e != nil {
		v2Failure(w, e)
		return
	}
	if state == "REJECTED" || state == "DELETED" {
		v2Failure(w, ErrForbidden)
		return
	}
	if e = c.Objects.put(ctx, c.Objects.QuarantineBucket, "quarantine/account/"+id, body, mime); e != nil {
		v2Failure(w, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		v2Failure(w, e)
		return
	}
	if e = c.ProcessAccountAsset(ctx, id); e != nil {
		v2Failure(w, e)
		return
	}
	c.getAccountAsset(w, r)
}
func (c ConnectedMedia) ProcessAccountAsset(ctx context.Context, id string) error {
	if c.Metadata.Store == nil || c.Worker == nil || !uuidRE.MatchString(id) {
		return raster.ErrUnavailable
	}
	store := c.Metadata.Store
	connection, e := store.Pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer connection.Release()
	lock := store.Schema + ":account_asset:" + id
	if _, e = connection.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1,0))", lock); e != nil {
		return e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e := connection.Exec(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lock); e != nil {
			connection.Conn().Close(cleanup)
		}
	}()
	if e = c.syncAccountModeration(ctx, id); e != nil {
		return e
	}
	var owner, hash, mime, state string
	var size int64
	var blocked bool
	var width, height int
	e = store.Pool.QueryRow(ctx, "SELECT owner_wallet,hash,mime,state,size,blocked,width,height FROM "+store.table("account_assets_v2")+" WHERE id=$1", id).Scan(&owner, &hash, &mime, &state, &size, &blocked, &width, &height)
	if e != nil {
		return e
	}
	if blocked {
		return ErrForbidden
	}
	if state == "APPROVED" || state == "REJECTED" || state == "DELETED" {
		return nil
	}
	object, e := c.Objects.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.Objects.QuarantineBucket), Key: aws.String("quarantine/account/" + id)})
	if e != nil {
		var api smithy.APIError
		if state == "UPLOADING" && errors.As(e, &api) && api.ErrorCode() == "NoSuchKey" {
			return nil
		}
		return raster.ErrUnavailable
	}
	original, e := raster.ReadBounded(object.Body)
	object.Body.Close()
	if e != nil {
		return e
	}
	digest := sha256.Sum256(original)
	if hex.EncodeToString(digest[:]) != hash || int64(len(original)) != size || http.DetectContentType(original) != mime {
		return ErrConflict
	}
	decoded, e := c.Worker.Decode(ctx, original)
	if e != nil {
		return e
	}
	if width != decoded.Width || height != decoded.Height {
		return ErrConflict
	}
	if _, e = store.Pool.Exec(ctx, "UPDATE "+store.table("account_assets_v2")+" SET state='VALIDATING' WHERE id=$1 AND state IN('UPLOADING','QUARANTINED')", id); e != nil {
		return e
	}
	scan := sharedhttp.ScanVerifiedBuffer(ctx, original, c.Scanner)
	if scan.Verdict == sharedhttp.ScanInfected {
		_, e = store.Pool.Exec(ctx, "UPDATE "+store.table("account_assets_v2")+" SET state='REJECTED',rejected_until=clock_timestamp()+interval '24 hours',scanner_engine=$2,scanner_revision=$3 WHERE id=$1 AND state='VALIDATING'", id, scan.Engine, scan.SignatureRevision)
		return e
	}
	if scan.Verdict != sharedhttp.ScanClean {
		return nil
	}
	if !scan.Clean || scan.Engine == "" || scan.SignatureRevision == "" || scan.ContentHash != hash || scan.Size != size {
		return ErrForbidden
	}
	if e = c.syncAccountModeration(ctx, id); e != nil {
		return e
	}
	tx, e := store.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackV2(ctx, tx)
	if e = tx.QueryRow(ctx, "SELECT blocked,state FROM "+store.table("account_assets_v2")+" WHERE id=$1 FOR UPDATE", id).Scan(&blocked, &state); e != nil {
		return e
	}
	if blocked || state != "VALIDATING" {
		return ErrForbidden
	}
	keys := map[string]string{}
	for _, n := range []int{256, 512, 1024} {
		b := decoded.Variants[n]
		sum := sha256.Sum256(b)
		key := fmt.Sprintf("approved/account/%s/%s/%d/%s.png", owner, id, n, hex.EncodeToString(sum[:]))
		if e = c.Objects.PutImmutable(ctx, key, b); e != nil {
			return e
		}
		keys[strconv.Itoa(n)] = key
	}
	raw, e := json.Marshal(keys)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "UPDATE "+store.table("account_assets_v2")+" SET state='APPROVED',variants=$2::jsonb,scanner_engine=$3,scanner_revision=$4 WHERE id=$1", id, string(raw), scan.Engine, scan.SignatureRevision)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (c ConnectedMedia) MaintainAccountAssets(ctx context.Context, expire bool) error {
	if c.Metadata.Store == nil {
		return raster.ErrUnavailable
	}
	store := c.Metadata.Store
	rows, e := store.Pool.Query(ctx, "SELECT id::text FROM "+store.table("account_assets_v2")+" WHERE state IN('UPLOADING','QUARANTINED','VALIDATING') ORDER BY CASE WHEN state='UPLOADING' THEN 1 ELSE 0 END,created_at LIMIT 10")
	if e != nil {
		return e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if e = c.ProcessAccountAsset(ctx, id); e != nil {
			return e
		}
	}
	if !expire {
		return nil
	}
	rows, e = store.Pool.Query(ctx, "SELECT id::text FROM "+store.table("account_assets_v2")+" WHERE state='REJECTED' AND rejected_until<=clock_timestamp() AND NOT investigation_hold LIMIT 100")
	if e != nil {
		return e
	}
	ids = nil
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if _, e = c.Objects.Client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.Objects.QuarantineBucket), Key: aws.String("quarantine/account/" + id)}); e != nil {
			return raster.ErrUnavailable
		}
		if _, e = store.Pool.Exec(ctx, "UPDATE "+store.table("account_assets_v2")+" SET state='DELETED' WHERE id=$1 AND state='REJECTED' AND NOT investigation_hold", id); e != nil {
			return e
		}
	}
	return nil
}
func (c ConnectedMedia) purgeAccountAssets(w http.ResponseWriter, r *http.Request) {
	if c.Metadata.Store == nil || c.Objects.Client == nil {
		v2Failure(w, raster.ErrUnavailable)
		return
	}
	wallet := chi.URLParam(r, "wallet")
	deletion := r.URL.Query().Get("deletionId")
	if !addressRE.MatchString(wallet) || !uuidRE.MatchString(deletion) {
		v2Failure(w, ErrValidation)
		return
	}
	store := c.Metadata.Store
	var held bool
	if e := store.Pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM "+store.table("account_assets_v2")+" WHERE owner_wallet=$1 AND investigation_hold)", wallet).Scan(&held); e != nil {
		v2Failure(w, e)
		return
	}
	if held {
		v2Failure(w, ErrForbidden)
		return
	}
	if _, e := store.Pool.Exec(r.Context(), "UPDATE "+store.table("account_assets_v2")+" SET state='DELETED',deletion_id=$2 WHERE owner_wallet=$1 AND (deletion_id IS NULL OR deletion_id=$2)", wallet, deletion); e != nil {
		v2Failure(w, e)
		return
	}
	rows, e := store.Pool.Query(r.Context(), "SELECT id::text,variants FROM "+store.table("account_assets_v2")+" WHERE owner_wallet=$1 AND deletion_id=$2", wallet, deletion)
	if e != nil {
		v2Failure(w, e)
		return
	}
	type item struct {
		id       string
		variants []byte
	}
	items := []item{}
	for rows.Next() {
		var a item
		if e = rows.Scan(&a.id, &a.variants); e != nil {
			rows.Close()
			v2Failure(w, e)
			return
		}
		items = append(items, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		v2Failure(w, e)
		return
	}
	for _, a := range items {
		if _, e = c.Objects.Client.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: aws.String(c.Objects.QuarantineBucket), Key: aws.String("quarantine/account/" + a.id)}); e != nil {
			v2Failure(w, raster.ErrUnavailable)
			return
		}
		var keys map[string]string
		if json.Unmarshal(a.variants, &keys) != nil {
			v2Failure(w, ErrUnrepresentable)
			return
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, "approved/account/"+wallet+"/"+a.id+"/") {
				v2Failure(w, ErrForbidden)
				return
			}
			if _, e = c.Objects.Client.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: aws.String(c.Objects.PublicBucket), Key: aws.String(key)}); e != nil {
				v2Failure(w, raster.ErrUnavailable)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
}

func (s *ProductionStore) CreateConnectedAsset(ctx context.Context, a AssetRecord, mime string, width, height int) error {
	if !uuidRE.MatchString(a.ID) || !addressRE.MatchString(a.Pool) || a.Size < 1 || a.Size > 6<<20 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(a.Hash) || a.ObjectKey != "quarantine/"+a.ID || (mime != "image/png" && mime != "image/jpeg" && mime != "image/webp") || width < 1 || height < 1 || width > 8192 || height > 8192 || int64(width)*int64(height) > 20000000 {
		return ErrValidation
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer rollbackV2(ctx, tx)
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("assets")+"(id,pool,state,hash,size,object_key,scanner_engine,scanner_revision) VALUES($1,$2,'CREATED',$3,$4,$5,'','') ON CONFLICT DO NOTHING", a.ID, a.Pool, a.Hash, a.Size, a.ObjectKey)
	if e != nil {
		return e
	}
	var pool, hash string
	var size int64
	e = tx.QueryRow(ctx, "SELECT pool,hash,size FROM "+s.table("assets")+" WHERE id=$1 FOR UPDATE", a.ID).Scan(&pool, &hash, &size)
	if errors.Is(e, pgx.ErrNoRows) {
		return ErrConflict
	}
	if e != nil {
		return e
	}
	if pool != a.Pool || hash != a.Hash || size != a.Size {
		return ErrConflict
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("asset_details_v2")+"(asset_id,mime,width,height,variants) VALUES($1,$2,$3,$4,'{}'::jsonb) ON CONFLICT(asset_id) DO NOTHING", a.ID, mime, width, height)
	if e != nil {
		return e
	}
	var oldMime string
	var oldWidth, oldHeight int
	e = tx.QueryRow(ctx, "SELECT mime,width,height FROM "+s.table("asset_details_v2")+" WHERE asset_id=$1 FOR UPDATE", a.ID).Scan(&oldMime, &oldWidth, &oldHeight)
	if e != nil {
		return e
	}
	if oldMime != mime || oldWidth != width || oldHeight != height {
		return ErrConflict
	}
	return tx.Commit(ctx)
}
