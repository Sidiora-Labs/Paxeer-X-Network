package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/metadata/pkg/types"
)

// ProductionLegacyReads serves the legacy public read contract the web still
// uses — GET /metadata/{token}, GET /metadata/batch, GET /logo/{token}.png and
// GET /banner/{token}.png — directly from the production store, with no
// legacy tables and no projection:
//
//   - token, pool, creator, name, symbol, decimals and supply come from the
//     canonical creation identity (the indexer's verified stream);
//   - the creator's display overrides, description, tags and social links
//     come from the published v2 document, unless moderation blocked it;
//   - the logo is the approved 512 px variant of the document's logo asset,
//     verified against its content address before it is served.
//
// Unknown tokens keep the legacy shape (200 with null fields). Production
// has no banner assets, so banners are always 404. The legacy multipart
// upload is not served: creator media goes through the v2 asset and v3
// publication routes.
type ProductionLegacyReads struct {
	Media     ConnectedMedia
	PublicURL string

	mu         sync.Mutex
	identities map[string]legacyIdentity
}

type legacyIdentity struct {
	identity V2CanonicalIdentity
	expires  time.Time
}

const (
	legacyIdentityTTL   = 10 * time.Minute
	legacyIdentityLimit = 8192
)

// NewProductionLegacyReads builds the legacy read adapter.
func NewProductionLegacyReads(media ConnectedMedia, publicURL string) *ProductionLegacyReads {
	return &ProductionLegacyReads{Media: media, PublicURL: strings.TrimRight(publicURL, "/"), identities: map[string]legacyIdentity{}}
}

// RegisterRoutes mounts the legacy public reads.
func (l *ProductionLegacyReads) RegisterRoutes(r chi.Router) {
	r.Get("/metadata/batch", l.batch)
	r.Get("/metadata/{tokenAddress}", l.single)
	r.Get("/logo/{file}", l.logo)
	r.Get("/banner/{file}", func(w http.ResponseWriter, _ *http.Request) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "No banner found")
	})
}

func (l *ProductionLegacyReads) ready() bool {
	store := l.Media.Metadata.Store
	return store != nil && store.Pool != nil && l.Media.Metadata.Canonical != nil && l.PublicURL != ""
}

func (l *ProductionLegacyReads) single(w http.ResponseWriter, r *http.Request) {
	token := normalizeAddr(strings.TrimSuffix(chi.URLParam(r, "tokenAddress"), ".json"))
	if !l.ready() {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "metadata reads are unavailable")
		return
	}
	out, e := l.build(r.Context(), []string{token})
	if e != nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "metadata reads are unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	sharedhttp.WriteJSON(w, http.StatusOK, out[token])
}

func (l *ProductionLegacyReads) batch(w http.ResponseWriter, r *http.Request) {
	requested := parseBatchAddresses(r.URL.Query()["addresses"])
	if len(requested) == 0 {
		w.Header().Set("Cache-Control", "public, max-age=10")
		sharedhttp.WriteJSON(w, http.StatusOK, map[string]types.PublicMetadata{})
		return
	}
	if !l.ready() {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "metadata reads are unavailable")
		return
	}
	out, e := l.build(r.Context(), requested)
	if e != nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "metadata reads are unavailable")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	sharedhttp.WriteJSON(w, http.StatusOK, out)
}

// build assembles the legacy records of tokens in one read-only snapshot.
func (l *ProductionLegacyReads) build(ctx context.Context, tokens []string) (map[string]types.PublicMetadata, error) {
	store := l.Media.Metadata.Store
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, e := store.Pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer rollbackV2(bounded, tx)
	out := make(map[string]types.PublicMetadata, len(tokens))
	for _, token := range tokens {
		record := types.PublicMetadata{TokenAddress: token, Decimals: defaultDecimals, TotalSupply: totalSupply, Tags: []string{}, Socials: types.Socials{}}
		if !addressRE.MatchString(token) {
			out[token] = record
			continue
		}
		identity, e := l.identity(bounded, tx, token)
		if errors.Is(e, ErrMissing) {
			out[token] = record
			continue
		}
		if e != nil {
			return nil, e
		}
		if e = l.fill(bounded, tx, identity, &record); e != nil {
			return nil, e
		}
		out[token] = record
	}
	return out, tx.Commit(bounded)
}

func (l *ProductionLegacyReads) identity(ctx context.Context, tx pgx.Tx, token string) (V2CanonicalIdentity, error) {
	now := time.Now()
	l.mu.Lock()
	cached, ok := l.identities[token]
	l.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.identity, nil
	}
	identity, e := l.Media.Metadata.identity(ctx, tx, token)
	if e != nil {
		return V2CanonicalIdentity{}, e
	}
	l.mu.Lock()
	if len(l.identities) >= legacyIdentityLimit {
		l.identities = map[string]legacyIdentity{}
	}
	l.identities[token] = legacyIdentity{identity: identity, expires: now.Add(legacyIdentityTTL)}
	l.mu.Unlock()
	return identity, nil
}

func (l *ProductionLegacyReads) fill(ctx context.Context, tx pgx.Tx, identity V2CanonicalIdentity, record *types.PublicMetadata) error {
	chain := identity.Token
	if chain.Decimals == nil || *chain.Decimals < 0 || *chain.Decimals > 36 || chain.TotalSupplyRaw == nil {
		return ErrUnrepresentable
	}
	supply, ok := new(big.Int).SetString(string(*chain.TotalSupplyRaw), 10)
	if !ok || supply.Sign() < 0 {
		return ErrUnrepresentable
	}
	pool, creator, name, symbol := identity.PoolAddress, identity.Creator, chain.Name, chain.Symbol
	record.PoolAddress, record.Creator, record.Name, record.Symbol = &pool, &creator, &name, &symbol
	record.Decimals = int32(*chain.Decimals)
	record.TotalSupply = wholeUnits(supply, int(*chain.Decimals))
	data, e := l.Media.Metadata.Store.readV2(ctx, tx, chain.Address)
	if errors.Is(e, ErrMissing) {
		return nil
	}
	if e != nil {
		return e
	}
	if data.PoolAddress != identity.PoolAddress {
		return ErrUnrepresentable
	}
	if data.UpdatedAt != nil {
		updated := data.UpdatedAt.UnixMilli()
		record.UpdatedAt = &updated
	}
	if data.ModerationStatus != "allowed" {
		return nil
	}
	if data.Display.Name != nil {
		record.Name = data.Display.Name
	}
	if data.Display.Symbol != nil {
		record.Symbol = data.Display.Symbol
	}
	if data.Display.Description != "" {
		description := data.Display.Description
		record.Description = &description
	}
	record.Tags = append([]string{}, data.Display.Tags...)
	for _, link := range data.Display.Socials {
		value := link.URL
		target := map[string]**string{"website": &record.Socials.Website, "twitter": &record.Socials.Twitter, "telegram": &record.Socials.Telegram, "discord": &record.Socials.Discord}[legacySocialColumn(link.URL)]
		if *target == nil {
			*target = &value
		}
	}
	if data.LogoAssetID != nil {
		if _, e = l.logoKey(ctx, tx, data); e == nil {
			logo := l.PublicURL + "/logo/" + chain.Address + ".png"
			record.Images.Logo = &logo
		} else if !errors.Is(e, ErrMissing) {
			return e
		}
	}
	return nil
}

// logoKey returns the approved 512 px variant key of the document's logo.
func (l *ProductionLegacyReads) logoKey(ctx context.Context, tx pgx.Tx, data V2Data) (string, error) {
	store := l.Media.Metadata.Store
	if data.LogoAssetID == nil || data.ModerationStatus != "allowed" {
		return "", ErrMissing
	}
	var hash string
	var variants []byte
	e := tx.QueryRow(ctx, "SELECT a.hash,d.variants FROM "+store.table("assets")+" a JOIN "+store.table("asset_details_v2")+" d ON d.asset_id=a.id WHERE a.id=$1 AND a.pool=$2 AND a.state='APPROVED'", *data.LogoAssetID, data.PoolAddress).Scan(&hash, &variants)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrMissing
	}
	if e != nil {
		return "", e
	}
	var keys map[string]string
	if json.Unmarshal(variants, &keys) != nil {
		return "", ErrUnrepresentable
	}
	if len(keys) == 0 {
		return "", ErrMissing
	}
	key := keys["512"]
	if !strings.HasPrefix(key, "approved/"+hash+"/512/") || !immutableURL(l.PublicURL+"/"+key) {
		return "", ErrUnrepresentable
	}
	return key, nil
}

func (l *ProductionLegacyReads) logo(w http.ResponseWriter, r *http.Request) {
	token := normalizeAddr(stripExt(chi.URLParam(r, "file")))
	if !addressRE.MatchString(token) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "No logo found")
		return
	}
	if !l.ready() {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "media delivery is unavailable")
		return
	}
	store := l.Media.Metadata.Store
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	tx, e := store.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "media delivery is unavailable")
		return
	}
	defer rollbackV2(ctx, tx)
	data, e := store.readV2(ctx, tx, token)
	key := ""
	if e == nil {
		key, e = l.logoKey(ctx, tx, data)
	}
	if errors.Is(e, ErrMissing) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "No logo found")
		return
	}
	if e != nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "media delivery is unavailable")
		return
	}
	if e = tx.Commit(ctx); e != nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "media delivery is unavailable")
		return
	}
	body, e := l.Media.Objects.readVerifiedPNG(ctx, key)
	if errors.Is(e, ErrMissing) {
		sharedhttp.WriteError(w, http.StatusNotFound, "Not Found", "Image not found in storage")
		return
	}
	if e != nil {
		sharedhttp.WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "media delivery is unavailable")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// wholeUnits renders raw / 10^decimals as a decimal string without trailing
// fractional zeros ("1000000000000000", 6 -> "1000000000").
func wholeUnits(raw *big.Int, decimals int) string {
	if decimals <= 0 {
		return raw.String()
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	whole, fraction := new(big.Int).QuoRem(raw, scale, new(big.Int))
	if fraction.Sign() == 0 {
		return whole.String()
	}
	digits := fraction.String()
	digits = strings.Repeat("0", decimals-len(digits)) + digits
	return whole.String() + "." + strings.TrimRight(digits, "0")
}
