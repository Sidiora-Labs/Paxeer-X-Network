// Package metadata implements the media/metadata HTTP handlers, porting the TS
// index.ts monolith into discrete routes: single + batch reads (Redis-cached),
// the creator-gated multipart upsert (v2 identity session + indexer.pools
// creator; metadata JSON + logo/banner -> byte sniffing -> virus scan -> bucket),
// and public byte-serving of logos/banners proxied from the bucket.
//
// Storage is BUCKET-PRIMARY (R2): uploads stream to the bucket and reads are
// served back from it with immutable CDN cache headers.
package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	goredis "github.com/redis/go-redis/v9"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/storage"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/internal/image"
	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

const (
	// totalSupply is the fixed launchpad supply surfaced in responses (parity
	// with the TS hardcoded value).
	totalSupply = "1000000000"
	// defaultDecimals matches the prior TS hardcoded decimals.
	defaultDecimals = 6
	// maxBatch caps the addresses accepted per /metadata/batch call.
	maxBatch = 100
	// jsonCacheTTL is the Redis TTL for cached public metadata responses.
	jsonCacheTTL = 60 * time.Second
	// multipartMaxMemory bounds in-memory multipart buffering before spilling.
	multipartMaxMemory = 32 << 20

	cacheKeyPrefix = "metadata:json:"
)

// ObjectStore is the subset of shared/storage.Client the handlers use. The
// concrete client (real R2 / MinIO in tests) satisfies it — no fakes.
type ObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (*storage.Object, error)
	Delete(ctx context.Context, key string) error
}

// SessionAuthority authenticates the caller's v2 identity session (bearer
// access token, or the __Host-kindle_access cookie with X-CSRF-Token). Outside
// production it is sharedauth.HTTPIdentityAuthority against the gateway's
// private identity introspection (IDENTITY_SERVICE_URL + IDENTITY_SERVICE_TOKEN).
type SessionAuthority interface {
	AuthenticateRequest(ctx context.Context, r *http.Request) (sharedauth.IdentityPrincipal, error)
}

// Scanner scans an uploaded buffer for malware.
type Scanner func(ctx context.Context, data []byte) sharedhttp.ScanResult

// Deps are the handler dependencies.
type Deps struct {
	Queries     sqlcdb.Querier
	Redis       *goredis.Client
	Store       ObjectStore
	PublicURL   string
	MaxLogoSize int64
	MaxBanner   int64
	Logger      *slog.Logger
	// Identity authenticates uploads. Nil keeps POST /metadata/{token} a 503:
	// there is no unauthenticated or legacy-signature fallback.
	Identity SessionAuthority

	// Scan defaults to shared/http.ScanBuffer (clamd via $CLAMAV_HOST).
	Scan Scanner
	// Clock defaults to time.Now.
	Clock func() time.Time
	// PoolLookupAttempts retries the indexer.pools read to tolerate the
	// create-wizard race (indexer may not have seen the new pool yet). Defaults
	// to 5. Sleep is invoked between attempts (defaults to time.Sleep).
	PoolLookupAttempts int
	PoolLookupDelay    time.Duration
	Sleep              func(time.Duration)
}

// Handlers serves the metadata routes.
type Handlers struct {
	q         sqlcdb.Querier
	redis     *goredis.Client
	store     ObjectStore
	publicURL string
	maxLogo   int64
	maxBanner int64
	logger    *slog.Logger
	identity  SessionAuthority
	scan      Scanner
	clock     func() time.Time
	poolTries int
	poolDelay time.Duration
	sleep     func(time.Duration)
}

// New constructs Handlers, applying defaults for optional dependencies.
func New(d Deps) *Handlers {
	scan := d.Scan
	if scan == nil {
		scan = func(ctx context.Context, data []byte) sharedhttp.ScanResult {
			return sharedhttp.ScanBuffer(ctx, data, sharedhttp.ScanOptions{})
		}
	}
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	tries := d.PoolLookupAttempts
	if tries <= 0 {
		tries = 5
	}
	delay := d.PoolLookupDelay
	if delay <= 0 {
		delay = 3 * time.Second
	}
	sleep := d.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	maxLogo := d.MaxLogoSize
	if maxLogo <= 0 {
		maxLogo = 2 << 20
	}
	maxBanner := d.MaxBanner
	if maxBanner <= 0 {
		maxBanner = 5 << 20
	}
	return &Handlers{
		q:         d.Queries,
		redis:     d.Redis,
		store:     d.Store,
		publicURL: strings.TrimRight(d.PublicURL, "/"),
		maxLogo:   maxLogo,
		maxBanner: maxBanner,
		logger:    d.Logger,
		identity:  d.Identity,
		scan:      scan,
		clock:     clock,
		poolTries: tries,
		poolDelay: delay,
		sleep:     sleep,
	}
}

// RegisterRoutes mounts the metadata endpoints onto r. Static segments
// (/metadata/batch) take priority over the {tokenAddress} param in chi.
func (h *Handlers) RegisterRoutes(r chi.Router) {
	r.Get("/metadata/batch", h.batch)
	r.Get("/metadata/{tokenAddress}", h.single)
	r.Post("/metadata/{tokenAddress}", h.upload)
	r.Get("/logo/{file}", h.serveImage(image.TypeLogo))
	r.Get("/banner/{file}", h.serveImage(image.TypeBanner))
}

// ── GET /metadata/{tokenAddress}  (also serves the ".json" alias) ────────────

func (h *Handlers) single(w http.ResponseWriter, r *http.Request) {
	addr := normalizeAddr(strings.TrimSuffix(chi.URLParam(r, "tokenAddress"), ".json"))
	ctx := r.Context()

	cacheKey := cacheKeyPrefix + addr
	if cached, found, err := sharedredis.CacheGet[types.PublicMetadata](ctx, h.redis, cacheKey); err == nil && found {
		w.Header().Set("Cache-Control", "public, max-age=60")
		sharedhttp.WriteJSON(w, http.StatusOK, cached)
		return
	}

	out, err := h.buildOne(ctx, addr, baseURL(h.publicURL, r))
	if err != nil {
		h.fail(w, "build metadata", err)
		return
	}
	if err := sharedredis.CacheSet(ctx, h.redis, cacheKey, out, jsonCacheTTL); err != nil {
		h.logErr("cache set", err)
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	sharedhttp.WriteJSON(w, http.StatusOK, out)
}

// buildOne assembles the public metadata for one token from pool + metadata +
// images. Absent rows yield null fields rather than an error (parity).
func (h *Handlers) buildOne(ctx context.Context, addr, base string) (types.PublicMetadata, error) {
	pool, err := h.q.GetPoolByToken(ctx, addr)
	hasPool := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.PublicMetadata{}, err
	}

	meta, err := h.q.GetTokenMetadata(ctx, addr)
	hasMeta := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return types.PublicMetadata{}, err
	}

	images, err := h.q.GetTokenImages(ctx, addr)
	if err != nil {
		return types.PublicMetadata{}, err
	}

	out := types.PublicMetadata{
		TokenAddress: addr,
		Decimals:     defaultDecimals,
		TotalSupply:  totalSupply,
		Tags:         []string{},
		Socials:      types.Socials{},
		Images:       buildImages(base, addr, images),
	}
	if hasPool {
		out.PoolAddress = strPtr(pool.PoolAddress)
		out.Creator = strPtr(pool.Creator)
	}
	if hasMeta {
		out.Name = textPtr(meta.Name)
		out.Symbol = textPtr(meta.Symbol)
		out.Description = textPtr(meta.Description)
		out.Decimals = meta.Decimals
		out.Socials = types.Socials{
			Website:  textPtr(meta.Website),
			Twitter:  textPtr(meta.Twitter),
			Telegram: textPtr(meta.Telegram),
			Discord:  textPtr(meta.Discord),
		}
		out.Tags = parseTags(meta.CustomTags)
		out.CreatedAt = int64Ptr(meta.CreatedAt)
		out.UpdatedAt = int64Ptr(meta.UpdatedAt)
	}
	return out, nil
}

// ── GET /metadata/batch?addresses=0xa,0xb&addresses=0xc ──────────────────────

func (h *Handlers) batch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requested := parseBatchAddresses(r.URL.Query()["addresses"])
	if len(requested) == 0 {
		w.Header().Set("Cache-Control", "public, max-age=10")
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]types.PublicMetadata{})
		return
	}

	out := make(map[string]types.PublicMetadata, len(requested))
	var misses []string
	for _, addr := range requested {
		if cached, found, err := sharedredis.CacheGet[types.PublicMetadata](ctx, h.redis, cacheKeyPrefix+addr); err == nil && found {
			out[addr] = cached
		} else {
			misses = append(misses, addr)
		}
	}
	if len(misses) == 0 {
		w.Header().Set("Cache-Control", "public, max-age=30")
		sharedhttp.WriteJSON(w, http.StatusOK, out)
		return
	}

	pools, err := h.q.ListPoolsByTokenAddrs(ctx, misses)
	if err != nil {
		h.fail(w, "batch pools", err)
		return
	}
	metas, err := h.q.ListTokenMetadataByAddrs(ctx, misses)
	if err != nil {
		h.fail(w, "batch metadata", err)
		return
	}
	images, err := h.q.ListTokenImagesByAddrs(ctx, misses)
	if err != nil {
		h.fail(w, "batch images", err)
		return
	}

	poolByAddr := make(map[string]sqlcdb.IndexerPool, len(pools))
	for _, p := range pools {
		poolByAddr[strings.ToLower(p.TokenAddress)] = p
	}
	metaByAddr := make(map[string]sqlcdb.MetadataTokenMetadatum, len(metas))
	for _, m := range metas {
		metaByAddr[strings.ToLower(m.TokenAddress)] = m
	}
	imagesByAddr := make(map[string][]sqlcdb.MetadataTokenImage, len(images))
	for _, img := range images {
		k := strings.ToLower(img.TokenAddress)
		imagesByAddr[k] = append(imagesByAddr[k], img)
	}

	base := baseURL(h.publicURL, r)
	for _, addr := range misses {
		entry := h.assemble(addr, poolByAddr, metaByAddr, imagesByAddr, base)
		out[addr] = entry
		if err := sharedredis.CacheSet(ctx, h.redis, cacheKeyPrefix+addr, entry, jsonCacheTTL); err != nil {
			h.logErr("batch cache set", err)
		}
	}

	w.Header().Set("Cache-Control", "public, max-age=30")
	sharedhttp.WriteJSON(w, http.StatusOK, out)
}

func (h *Handlers) assemble(
	addr string,
	pools map[string]sqlcdb.IndexerPool,
	metas map[string]sqlcdb.MetadataTokenMetadatum,
	images map[string][]sqlcdb.MetadataTokenImage,
	base string,
) types.PublicMetadata {
	out := types.PublicMetadata{
		TokenAddress: addr,
		Decimals:     defaultDecimals,
		TotalSupply:  totalSupply,
		Tags:         []string{},
		Socials:      types.Socials{},
		Images:       buildImages(base, addr, images[addr]),
	}
	if pool, ok := pools[addr]; ok {
		out.PoolAddress = strPtr(pool.PoolAddress)
		out.Creator = strPtr(pool.Creator)
	}
	if meta, ok := metas[addr]; ok {
		out.Name = textPtr(meta.Name)
		out.Symbol = textPtr(meta.Symbol)
		out.Description = textPtr(meta.Description)
		out.Decimals = meta.Decimals
		out.Socials = types.Socials{
			Website:  textPtr(meta.Website),
			Twitter:  textPtr(meta.Twitter),
			Telegram: textPtr(meta.Telegram),
			Discord:  textPtr(meta.Discord),
		}
		out.Tags = parseTags(meta.CustomTags)
		out.CreatedAt = int64Ptr(meta.CreatedAt)
		out.UpdatedAt = int64Ptr(meta.UpdatedAt)
	}
	return out
}

// ── POST /metadata/{tokenAddress}  (multipart upsert) ────────────────────────
//
// History: until f046cd5 this route verified an EIP-191 signature over an
// arbitrary client-chosen message (wallet/message/signature form fields). That
// signature bound neither the method, the token, the body nor a nonce, so one
// signed "sign-in" string could be replayed to overwrite any of the signer's
// tokens; the route was hard-disabled (503) instead. It is restored here with a
// real credential: the caller's v2 identity session (Authorization bearer or
// the __Host-kindle_access cookie + X-CSRF-Token), authenticated by the
// gateway's identity authority, and the session wallet must be the pool
// creator recorded in indexer.pools. Without an identity authority the route
// stays a 503 and never falls back to an unauthenticated write.

// uploadOverhead is the multipart framing + text-field allowance on top of the
// two image size limits when bounding the request body.
const uploadOverhead = 1 << 20

// imageUpload is one validated upload field, ready to be written.
type imageUpload struct {
	imageType string
	mime      string
	data      []byte
}

// uploadError is a client-visible rejection with its HTTP status.
type uploadError struct {
	status  int
	message string
}

func (h *Handlers) upload(w http.ResponseWriter, r *http.Request) {
	if h.identity == nil || h.store == nil || h.q == nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Unavailable", "creator session authority is not configured")
		return
	}
	addr := normalizeAddr(chi.URLParam(r, "tokenAddress"))
	if !addressRE.MatchString(addr) {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "invalid token address")
		return
	}
	ctx := r.Context()

	principal, err := h.identity.AuthenticateRequest(ctx, r)
	if err != nil {
		status, message := sessionFailure(err)
		if status == http.StatusServiceUnavailable {
			h.logErr("authenticate session", err)
		}
		sharedhttp.WriteError(w, status, http.StatusText(status), message)
		return
	}

	pool, ok, err := h.lookupPool(ctx, addr)
	if err != nil {
		h.fail(w, "lookup pool", err)
		return
	}
	if !ok {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Token not found in indexer")
		return
	}
	if !strings.EqualFold(pool.Creator, principal.Wallet) {
		sharedhttp.WriteError(w, http.StatusForbidden, "Forbidden", "Not pool creator")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxLogo+h.maxBanner+uploadOverhead)
	if err := r.ParseMultipartForm(multipartMaxMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			sharedhttp.WriteRequestBodyError(w, http.StatusRequestEntityTooLarge, sharedhttp.RuleMaxBytes, "upload exceeds size limit")
			return
		}
		sharedhttp.WriteRequestBodyError(w, http.StatusBadRequest, sharedhttp.RuleMultipart, "invalid multipart form")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	for field, files := range r.MultipartForm.File {
		if (field != image.TypeLogo && field != image.TypeBanner) || len(files) != 1 {
			sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "unexpected or duplicate image field")
			return
		}
	}
	metaValues := r.MultipartForm.Value["metadata"]
	if len(metaValues) > 1 {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "duplicate metadata field")
		return
	}
	var meta *types.UploadMetadata
	if len(metaValues) == 1 && metaValues[0] != "" {
		parsed, err := parseUploadMetadata(metaValues[0])
		if err != nil {
			sharedhttp.WriteRequestBodyError(w, http.StatusBadRequest, sharedhttp.RuleJSON, "Invalid metadata JSON")
			return
		}
		meta = &parsed
	}

	// Validate every image before any write so a rejected upload changes nothing.
	var uploads []imageUpload
	for _, spec := range []struct {
		imageType string
		max       int64
	}{{image.TypeLogo, h.maxLogo}, {image.TypeBanner, h.maxBanner}} {
		up, present, rejected := h.validateImage(ctx, r, spec.imageType, spec.max)
		if rejected != nil {
			if rejected.status == http.StatusServiceUnavailable {
				h.logErr("scan "+spec.imageType, errors.New(rejected.message))
			}
			sharedhttp.WriteError(w, rejected.status, http.StatusText(rejected.status), rejected.message)
			return
		}
		if present {
			uploads = append(uploads, up)
		}
	}
	if meta == nil && len(uploads) == 0 {
		sharedhttp.WriteError(w, http.StatusBadRequest, "Bad Request", "nothing to update: send metadata, logo or banner")
		return
	}

	now := h.clock().UnixMilli()
	resp := types.UploadResponse{Success: true}
	base := baseURL(h.publicURL, r)
	for _, up := range uploads {
		url, err := h.storeImage(ctx, addr, up, now, base)
		if err != nil {
			h.fail(w, "store "+up.imageType, err)
			return
		}
		if up.imageType == image.TypeLogo {
			resp.LogoURL = strPtr(url)
		} else {
			resp.BannerURL = strPtr(url)
		}
	}
	if err := h.recordMetadata(ctx, addr, pool.PoolAddress, strings.ToLower(principal.Wallet), meta, now); err != nil {
		h.fail(w, "upsert metadata", err)
		return
	}
	resp.MetadataUpdated = meta != nil

	if err := sharedredis.CacheInvalidate(ctx, h.redis, cacheKeyPrefix+addr); err != nil {
		h.logErr("cache invalidate", err)
	}
	sharedhttp.WriteJSON(w, http.StatusOK, resp)
}

// sessionFailure maps an identity authority failure to the client status.
// Only an explicit authority verdict is a 4xx; anything else (unreachable,
// malformed answer) is a 503 so a broken dependency never reads as "denied".
func sessionFailure(err error) (int, string) {
	var authority *sharedauth.IdentityAuthorityError
	if errors.As(err, &authority) {
		switch authority.Status {
		case http.StatusBadRequest, http.StatusUnauthorized:
			return http.StatusUnauthorized, "valid creator session required"
		case http.StatusForbidden:
			return http.StatusForbidden, "session scope denied"
		case http.StatusTooManyRequests:
			return http.StatusTooManyRequests, "identity authority rate limited"
		default:
			return http.StatusServiceUnavailable, "identity authority unavailable"
		}
	}
	if errors.Is(err, sharedauth.ErrScopeDenied) {
		return http.StatusUnauthorized, "valid creator session required"
	}
	return http.StatusServiceUnavailable, "identity authority unavailable"
}

var errBadMetadata = errors.New("bad metadata json")

func parseUploadMetadata(raw string) (types.UploadMetadata, error) {
	var data types.UploadMetadata
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return data, errBadMetadata
	}
	return data, nil
}

// recordMetadata writes metadata.token_metadata, the row both the legacy read
// and the development v2 reads resolve. With a metadata document it upserts
// the creator's fields (legacy semantics: the document replaces them); for an
// image-only upload it keeps the existing fields, creating an empty row when
// none exists, so the record exists and its revision (updated_at, ms) advances.
func (h *Handlers) recordMetadata(ctx context.Context, addr, poolAddr, wallet string, meta *types.UploadMetadata, now int64) error {
	existing, err := h.q.GetTokenMetadata(ctx, addr)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if found && existing.UpdatedAt >= now {
		now = existing.UpdatedAt + 1
	}
	params := sqlcdb.UpsertTokenMetadataParams{
		TokenAddress: addr,
		PoolAddress:  strings.ToLower(poolAddr),
		CustomTags:   pgtype.Text{String: "[]", Valid: true},
		Decimals:     defaultDecimals,
		CreatedBy:    wallet,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if found {
		params.Name, params.Symbol, params.Description = existing.Name, existing.Symbol, existing.Description
		params.Website, params.Twitter, params.Telegram, params.Discord = existing.Website, existing.Twitter, existing.Telegram, existing.Discord
		params.CustomTags, params.Decimals = existing.CustomTags, existing.Decimals
	}
	if meta != nil {
		tags := meta.Tags
		if tags == nil {
			tags = []string{}
		}
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return err
		}
		params.Name, params.Symbol, params.Description = strToText(meta.Name), strToText(meta.Symbol), strToText(meta.Description)
		params.Website, params.Twitter, params.Telegram, params.Discord = strToText(meta.Website), strToText(meta.Twitter), strToText(meta.Telegram), strToText(meta.Discord)
		params.CustomTags = pgtype.Text{String: string(tagsJSON), Valid: true}
		params.Decimals = defaultDecimals
		if meta.Decimals != nil {
			params.Decimals = *meta.Decimals
		}
	}
	return h.q.UpsertTokenMetadata(ctx, params)
}

// validateImage reads one optional image field and verifies it from its bytes:
// size limit, sniffed type on the allowlist and equal to the declared type,
// decodable with bounded dimensions, and a clean virus scan. present is false
// when the field is absent.
func (h *Handlers) validateImage(ctx context.Context, r *http.Request, imageType string, maxSize int64) (imageUpload, bool, *uploadError) {
	fhs := r.MultipartForm.File[imageType]
	if len(fhs) == 0 {
		return imageUpload{}, false, nil
	}
	fh := fhs[0]
	if fh.Size > maxSize {
		return imageUpload{}, true, &uploadError{http.StatusRequestEntityTooLarge, imageType + " too large"}
	}
	f, err := fh.Open()
	if err != nil {
		return imageUpload{}, true, &uploadError{http.StatusBadRequest, imageType + " could not be read"}
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return imageUpload{}, true, &uploadError{http.StatusBadRequest, imageType + " could not be read"}
	}
	if int64(len(buf)) > maxSize {
		return imageUpload{}, true, &uploadError{http.StatusRequestEntityTooLarge, imageType + " too large"}
	}
	sniffed, err := image.Sniff(fh.Header.Get("Content-Type"), buf)
	switch {
	case errors.Is(err, image.ErrLimit):
		return imageUpload{}, true, &uploadError{http.StatusUnprocessableEntity, imageType + " dimensions exceed the limit"}
	case err != nil:
		return imageUpload{}, true, &uploadError{http.StatusUnsupportedMediaType, imageType + " must be a valid webp, png or jpeg image matching its declared type"}
	}
	res := h.scan(ctx, buf)
	if !res.Clean {
		if res.Verdict == sharedhttp.ScanInfected {
			return imageUpload{}, true, &uploadError{http.StatusUnprocessableEntity, imageType + " rejected by virus scanner"}
		}
		return imageUpload{}, true, &uploadError{http.StatusServiceUnavailable, "virus scanner unavailable: " + res.Reason}
	}
	return imageUpload{imageType: imageType, mime: sniffed.MIME, data: buf}, true, nil
}

// storeImage writes one validated image to the bucket and records its key in
// metadata.token_images (the legacy read + /logo|/banner serve source),
// removing the previous object when the extension changed.
func (h *Handlers) storeImage(ctx context.Context, addr string, up imageUpload, now int64, base string) (string, error) {
	ext := image.ExtForMime(up.mime)
	storageKey := up.imageType + "s/" + up.imageType + "-" + addr + "." + ext
	previous, err := h.q.GetImageByType(ctx, sqlcdb.GetImageByTypeParams{TokenAddress: addr, ImageType: up.imageType})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err := h.store.Put(ctx, storageKey, bytes.NewReader(up.data), int64(len(up.data)), up.mime); err != nil {
		return "", err
	}
	if err := h.q.UpsertTokenImage(ctx, sqlcdb.UpsertTokenImageParams{
		ID:           addr + "-" + up.imageType,
		TokenAddress: addr,
		ImageType:    up.imageType,
		StorageKey:   storageKey,
		MimeType:     up.mime,
		SizeBytes:    int32(len(up.data)),
		UploadedAt:   now,
	}); err != nil {
		return "", err
	}
	if previous.StorageKey != "" && previous.StorageKey != storageKey {
		if err := h.store.Delete(ctx, previous.StorageKey); err != nil && !errors.Is(err, storage.ErrNotFound) {
			h.logErr("delete replaced object", err)
		}
	}
	return base + "/" + up.imageType + "/" + addr + "." + ext, nil
}

// lookupPool reads indexer.pools with bounded retries (create-wizard race).
func (h *Handlers) lookupPool(ctx context.Context, addr string) (sqlcdb.IndexerPool, bool, error) {
	for attempt := 1; attempt <= h.poolTries; attempt++ {
		pool, err := h.q.GetPoolByToken(ctx, addr)
		if err == nil {
			return pool, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlcdb.IndexerPool{}, false, err
		}
		if attempt < h.poolTries {
			h.sleep(h.poolDelay)
		}
	}
	return sqlcdb.IndexerPool{}, false, nil
}

// ── GET /logo/{addr}.{ext} and /banner/{addr}.{ext} (proxy bytes from S3) ───
//
// Restored from before f046cd5: the token_images row names the object, whose
// bytes are streamed from the bucket. The extension in the path is ignored
// (the web always asks for .png); the stored, sniffed MIME is served. The URL
// is stable across re-uploads, so it is cached briefly rather than immutable.

func (h *Handlers) serveImage(imageType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.store == nil || h.q == nil {
			sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Unavailable", "media delivery is not configured")
			return
		}
		addr := normalizeAddr(stripExt(chi.URLParam(r, "file")))
		if !addressRE.MatchString(addr) {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "No "+imageType+" found")
			return
		}
		ctx := r.Context()

		img, err := h.q.GetImageByType(ctx, sqlcdb.GetImageByTypeParams{TokenAddress: addr, ImageType: imageType})
		if errors.Is(err, pgx.ErrNoRows) {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "No "+imageType+" found")
			return
		}
		if err != nil {
			h.fail(w, "get image", err)
			return
		}

		obj, err := h.store.Get(ctx, img.StorageKey)
		if errors.Is(err, storage.ErrNotFound) {
			sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Image not found in storage")
			return
		}
		if err != nil {
			h.fail(w, "store get", err)
			return
		}
		defer obj.Body.Close()

		w.Header().Set("Content-Type", img.MimeType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "public, max-age=300")
		if img.SizeBytes > 0 {
			w.Header().Set("Content-Length", itoa64(int64(img.SizeBytes)))
		}
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, obj.Body); err != nil {
			h.logErr("stream image", err)
		}
	}
}

// ── error helpers ────────────────────────────────────────────────────────────

func (h *Handlers) fail(w http.ResponseWriter, op string, err error) {
	h.logErr(op, err)
	sharedhttp.WriteError(w, http.StatusInternalServerError, "Internal Server Error", "")
}

func (h *Handlers) logErr(op string, err error) {
	if h.logger != nil {
		h.logger.Error("metadata handler error", slog.String("op", op), slog.String("err", err.Error()))
	}
}
