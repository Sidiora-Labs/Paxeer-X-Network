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
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	iface "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const V2BodyLimit = 32 << 10

type V2Social struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
type V2Display struct {
	Name        *string    `json:"name"`
	Symbol      *string    `json:"symbol"`
	Description string     `json:"description"`
	Tags        []string   `json:"tags"`
	Socials     []V2Social `json:"socials"`
}
type V2Patch struct {
	ExpectedRevision string      `json:"expectedRevision"`
	Name             *string     `json:"name,omitempty"`
	Symbol           *string     `json:"symbol,omitempty"`
	Description      *string     `json:"description,omitempty"`
	Tags             *[]string   `json:"tags,omitempty"`
	Socials          *[]V2Social `json:"socials,omitempty"`
	LogoAssetID      *string     `json:"logoAssetId,omitempty"`
}
type V2Data struct {
	ChainID          uint64     `json:"chainId"`
	PoolAddress      string     `json:"poolAddress"`
	TokenAddress     string     `json:"tokenAddress"`
	Revision         string     `json:"revision"`
	Display          V2Display  `json:"display"`
	UpdatedAt        *time.Time `json:"updatedAt"`
	Source           string     `json:"source"`
	ModerationStatus string     `json:"moderationStatus"`
	LogoAssetID      *string    `json:"logoAssetId"`
}

func ParseV2Patch(raw []byte) (V2Patch, error) {
	var p V2Patch
	if len(raw) > V2BodyLimit {
		return p, ErrValidation
	}
	if err := decodeUploadV2(raw, &p); err != nil {
		return p, err
	}
	if !revisionValid(p.ExpectedRevision) {
		return p, ErrValidation
	}
	for _, v := range []struct {
		p        **string
		min, max int
	}{{&p.Name, 1, 80}, {&p.Symbol, 1, 16}, {&p.Description, 0, 4096}} {
		if *v.p != nil {
			s, e := text(**v.p, v.min, v.max)
			if e != nil {
				return p, e
			}
			*v.p = &s
		}
	}
	if p.Tags != nil {
		if len(*p.Tags) > 20 {
			return p, ErrValidation
		}
		tags := make([]string, 0, len(*p.Tags))
		seen := map[string]bool{}
		for _, v := range *p.Tags {
			s, e := text(v, 1, 256)
			if e != nil || seen[s] {
				return p, ErrValidation
			}
			seen[s] = true
			tags = append(tags, s)
		}
		p.Tags = &tags
	}
	if p.Socials != nil {
		if len(*p.Socials) > 5 {
			return p, ErrValidation
		}
		socials := make([]V2Social, 0, len(*p.Socials))
		seen := map[string]bool{}
		for _, v := range *p.Socials {
			label, e := text(v.Label, 1, 80)
			if e != nil || len(v.URL) > 2048 || !SafeLink(v.URL) || seen[v.URL] {
				return p, ErrValidation
			}
			seen[v.URL] = true
			socials = append(socials, V2Social{label, v.URL})
		}
		p.Socials = &socials
	}
	if p.LogoAssetID != nil && !uuidRE.MatchString(*p.LogoAssetID) {
		return p, ErrValidation
	}
	return p, nil
}
func decodeUploadV2(raw []byte, p *V2Patch) error {
	if !utf8.Valid(raw) || !json.Valid(raw) || !uniqueV2JSON(json.NewDecoder(bytes.NewReader(raw)), 0) {
		return ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(p) != nil {
		return ErrValidation
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return ErrValidation
	}
	seen := map[string]bool{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return ErrValidation
		}
		s, ok := k.(string)
		if !ok || seen[s] {
			return ErrValidation
		}
		switch s {
		case "expectedRevision", "name", "symbol", "description", "tags", "socials", "logoAssetId":
		default:
			return ErrValidation
		}
		seen[s] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return ErrValidation
		}
	}
	if !seen["expectedRevision"] {
		return ErrValidation
	}
	return nil
}

func uniqueV2JSON(d *json.Decoder, depth int) bool {
	if depth > 64 {
		return false
	}
	tok, e := d.Token()
	if e != nil {
		return false
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return true
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return false
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return false
			}
			seen[s] = true
			if !uniqueV2JSON(d, depth+1) {
				return false
			}
		}
		end, e := d.Token()
		return e == nil && end == json.Delim('}')
	case '[':
		for d.More() {
			if !uniqueV2JSON(d, depth+1) {
				return false
			}
		}
		end, e := d.Token()
		return e == nil && end == json.Delim(']')
	}
	return false
}

func (s *ProductionStore) V2SchemaDDL() string {
	return fmt.Sprintf(`CREATE TABLE %s(pool text PRIMARY KEY REFERENCES %s(pool),revision numeric(78,0) NOT NULL DEFAULT 0,document jsonb NOT NULL DEFAULT '{"description":"","tags":[],"socials":[]}',asset_id text,updated_at timestamptz);CREATE TABLE %s(operation text PRIMARY KEY,pool text NOT NULL,actor text NOT NULL,body_hash text NOT NULL,revision numeric(78,0) NOT NULL,result jsonb NOT NULL);CREATE TABLE %s(wallet text NOT NULL,nonce text NOT NULL,operation text NOT NULL,digest text NOT NULL,PRIMARY KEY(wallet,nonce));CREATE TABLE %s(pool text NOT NULL,revision numeric(78,0) NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(pool,revision));`, s.table("v2_documents"), s.table("pools"), s.table("v2_operations"), s.table("v2_nonces"), s.table("v2_outbox"))
}

type V2SignedVerifier struct {
	Store    *ProductionStore
	Audience string
	Clock    func() time.Time
}

func V2SigningMessage(a VerifiedAction) string {
	return strings.Join([]string{"KindleLaunch metadata action v2", a.Audience, strconv.FormatUint(a.ChainID, 10), a.Wallet, a.OperationID, a.Method, a.Path, a.BodyHash, a.Nonce, a.ExpiresAt}, "\n")
}
func (v V2SignedVerifier) AuthorizeInTx(ctx context.Context, tx pgx.Tx, a VerifiedAction) error {
	now := time.Now()
	if v.Clock != nil {
		now = v.Clock()
	}
	expiry, e := strconv.ParseInt(a.ExpiresAt, 10, 64)
	if v.Store == nil || v.Audience == "" || a.Audience != v.Audience || a.ChainID != 125 || e != nil || strconv.FormatInt(expiry, 10) != a.ExpiresAt || expiry <= now.Unix() || expiry > now.Add(5*time.Minute).Unix() || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(a.Nonce) || !sharedauth.VerifyWalletSignature(a.Wallet, V2SigningMessage(a), a.Signature) {
		return ErrForbidden
	}
	h := sha256.Sum256([]byte(V2SigningMessage(a)))
	digest := hex.EncodeToString(h[:])
	_, e = tx.Exec(ctx, "INSERT INTO "+v.Store.table("v2_nonces")+" VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", a.Wallet, a.Nonce, a.OperationID, digest)
	if e != nil {
		return e
	}
	var op, old string
	if e = tx.QueryRow(ctx, "SELECT operation,digest FROM "+v.Store.table("v2_nonces")+" WHERE wallet=$1 AND nonce=$2", a.Wallet, a.Nonce).Scan(&op, &old); e != nil {
		return e
	}
	if op != a.OperationID || old != digest {
		return ErrForbidden
	}
	return nil
}

func (s *ProductionStore) ApplyV2(ctx context.Context, token string, raw []byte, a VerifiedAction, now time.Time) (V2Data, error) {
	var z V2Data
	raw = bytes.Clone(raw)
	p, e := ParseV2Patch(raw)
	if e != nil {
		return z, e
	}
	if !addressRE.MatchString(token) || !addressRE.MatchString(a.Wallet) || !uuidRE.MatchString(a.OperationID) {
		return z, ErrValidation
	}
	h := sha256.Sum256(raw)
	digest := hex.EncodeToString(h[:])
	if a.Method != "PATCH" || a.Path != "/v2/tokens/"+token+"/metadata" || a.BodyHash != digest || a.ChainID != 125 || s.Verifier == nil {
		return z, ErrForbidden
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return z, e
	}
	defer rollbackV2(ctx, tx)
	return s.applyV2InTx(ctx, tx, token, raw, a, now, p, nil)
}

func (s *ProductionStore) applyV2InTx(ctx context.Context, tx pgx.Tx, token string, raw []byte, a VerifiedAction, now time.Time, p V2Patch, validate func(V2Data) error) (V2Data, error) {
	var z V2Data
	var e error
	h := sha256.Sum256(raw)
	digest := hex.EncodeToString(h[:])
	var pool, creator string
	e = tx.QueryRow(ctx, "SELECT pool,creator FROM "+s.table("pools")+" WHERE token=$1 FOR UPDATE", token).Scan(&pool, &creator)
	if errors.Is(e, pgx.ErrNoRows) {
		return z, ErrMissing
	}
	if e != nil {
		return z, e
	}
	if !strings.EqualFold(creator, a.Wallet) {
		return z, ErrForbidden
	}
	if e = s.Verifier.AuthorizeInTx(ctx, tx, a); e != nil {
		return z, e
	}
	var blocked bool
	e = tx.QueryRow(ctx, "SELECT blocked FROM "+s.table("documents")+" WHERE pool=$1", pool).Scan(&blocked)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return z, e
	}
	if blocked {
		return z, ErrForbidden
	}
	var oldPool, oldActor, oldHash string
	var result []byte
	e = tx.QueryRow(ctx, "SELECT pool,actor,body_hash,result FROM "+s.table("v2_operations")+" WHERE operation=$1", a.OperationID).Scan(&oldPool, &oldActor, &oldHash, &result)
	if e == nil {
		if oldPool != pool || oldActor != a.Wallet || oldHash != digest {
			return z, ErrConflict
		}
		if json.Unmarshal(result, &z) != nil {
			return z, ErrUnrepresentable
		}
		if validate != nil {
			if e = validate(z); e != nil {
				return V2Data{}, e
			}
		}
		return z, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return z, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("v2_documents")+"(pool) VALUES($1) ON CONFLICT DO NOTHING", pool)
	if e != nil {
		return z, e
	}
	var current string
	e = tx.QueryRow(ctx, "SELECT revision::text FROM "+s.table("v2_documents")+" WHERE pool=$1 FOR UPDATE", pool).Scan(&current)
	if e != nil {
		return z, e
	}
	if current != p.ExpectedRevision || current == "115792089237316195423570985008687907853269984665640564039457584007913129639935" {
		return z, ErrConflict
	}
	if p.LogoAssetID != nil {
		var state string
		e = tx.QueryRow(ctx, "SELECT state FROM "+s.table("assets")+" WHERE id=$1 AND pool=$2 FOR SHARE", *p.LogoAssetID, pool).Scan(&state)
		if e != nil || state != "APPROVED" {
			return z, ErrMissing
		}
	}
	doc := map[string]any{}
	if p.Name != nil {
		doc["name"] = *p.Name
	}
	if p.Symbol != nil {
		doc["symbol"] = *p.Symbol
	}
	if p.Description != nil {
		doc["description"] = *p.Description
	}
	if p.Tags != nil {
		doc["tags"] = *p.Tags
	}
	if p.Socials != nil {
		doc["socials"] = *p.Socials
	}
	patch, _ := json.Marshal(doc)
	_, e = tx.Exec(ctx, "UPDATE "+s.table("v2_documents")+" SET revision=revision+1,document=document||$2::jsonb,asset_id=COALESCE($3,asset_id),updated_at=$4 WHERE pool=$1", pool, string(patch), p.LogoAssetID, now.UTC())
	if e != nil {
		return z, e
	}
	z, e = s.readV2(ctx, tx, token)
	if e != nil {
		return z, e
	}
	if s.ConnectedSearch {
		projection := map[string]string{"description": normalizedSearchText(z.Display.Description)}
		if z.Display.Name != nil {
			projection["name"] = normalizedSearchText(*z.Display.Name)
		}
		if z.Display.Symbol != nil {
			projection["symbol"] = normalizedSearchText(*z.Display.Symbol)
		}
		encoded, err := json.Marshal(projection)
		if err != nil {
			return z, err
		}
		if _, e = tx.Exec(ctx, "UPDATE "+s.table("v2_documents")+" SET search_document=$2::jsonb WHERE pool=$1", pool, string(encoded)); e != nil {
			return z, e
		}
	}
	result, e = json.Marshal(z)
	if e != nil {
		return z, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("v2_operations")+" VALUES($1,$2,$3,$4,$5::numeric,$6::jsonb)", a.OperationID, pool, a.Wallet, digest, z.Revision, string(result))
	if e != nil {
		return z, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("v2_outbox")+" VALUES($1,$2::numeric,jsonb_build_object('metadata',$3::jsonb,'actor',$4::text,'previousRevision',$5::text))", pool, z.Revision, string(result), a.Wallet, current)
	if e != nil {
		return z, e
	}
	if validate != nil {
		if e = validate(z); e != nil {
			return V2Data{}, e
		}
	}
	return z, tx.Commit(ctx)
}
func rollbackV2(ctx context.Context, tx pgx.Tx) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	tx.Rollback(c)
}

type v2Reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *ProductionStore) readV2(ctx context.Context, q v2Reader, token string) (V2Data, error) {
	var z V2Data
	var raw []byte
	var blocked bool
	e := q.QueryRow(ctx, "SELECT p.pool,p.token,d.revision::text,d.document,d.updated_at,d.asset_id,COALESCE(m.blocked,false) FROM "+s.table("pools")+" p JOIN "+s.table("v2_documents")+" d ON d.pool=p.pool LEFT JOIN "+s.table("documents")+" m ON m.pool=p.pool WHERE p.token=$1", token).Scan(&z.PoolAddress, &z.TokenAddress, &z.Revision, &raw, &z.UpdatedAt, &z.LogoAssetID, &blocked)
	if errors.Is(e, pgx.ErrNoRows) {
		return z, ErrMissing
	}
	if e != nil {
		return z, e
	}
	if json.Unmarshal(raw, &z.Display) != nil {
		return z, ErrUnrepresentable
	}
	z.ChainID = 125
	z.Source = "creator"
	z.ModerationStatus = "allowed"
	if blocked {
		z.ModerationStatus = "blocked"
		z.LogoAssetID = nil
	}
	return z, nil
}
func (s *ProductionStore) ReadV2(ctx context.Context, token string) (V2Data, error) {
	if !addressRE.MatchString(token) {
		return V2Data{}, ErrValidation
	}
	return s.readV2(ctx, s.Pool, token)
}

type V2Handlers struct {
	Store *ProductionStore
	Clock func() time.Time
}

func (h V2Handlers) RegisterRoutes(r chi.Router) {
	r.Get("/v2/tokens/{tokenAddress}/metadata", h.get)
	r.Patch("/v2/tokens/{tokenAddress}/metadata", h.patch)
}
func (h V2Handlers) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}
func (h V2Handlers) get(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		v2Error(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	v, e := h.Store.ReadV2(r.Context(), chi.URLParam(r, "tokenAddress"))
	if e != nil {
		v2Failure(w, e)
		return
	}
	etag := fmt.Sprintf(`"metadata-v2-%s-%s-%s"`, v.PoolAddress, v.Revision, v.ModerationStatus)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=60")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.respond(w, v)
}
func (h V2Handlers) patch(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		v2Error(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
		v2Error(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, V2BodyLimit))
	if e != nil {
		v2Error(w, http.StatusUnprocessableEntity, "invalid_metadata")
		return
	}
	a := VerifiedAction{Wallet: r.Header.Get("X-Action-Wallet"), OperationID: r.Header.Get("Idempotency-Key"), Signature: r.Header.Get("X-Action-Signature"), Nonce: r.Header.Get("X-Action-Nonce"), ExpiresAt: r.Header.Get("X-Action-Expires"), Audience: r.Header.Get("X-Action-Audience"), ChainID: 125, Method: r.Method, Path: r.URL.Path}
	a.Wallet = strings.ToLower(a.Wallet)
	hash := sha256.Sum256(raw)
	a.BodyHash = hex.EncodeToString(hash[:])
	v, e := h.Store.ApplyV2(r.Context(), chi.URLParam(r, "tokenAddress"), raw, a, h.now())
	if e != nil {
		v2Failure(w, e)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.respond(w, v)
}
func (h V2Handlers) respond(w http.ResponseWriter, v V2Data) {
	r, e := envelope(uuid.NewString(), v, "partial", h.now())
	if e != nil {
		v2Failure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(r)
}
func v2Failure(w http.ResponseWriter, e error) {
	switch {
	case errors.Is(e, ErrValidation):
		v2Error(w, 422, "invalid_metadata")
	case errors.Is(e, ErrForbidden):
		v2Error(w, 403, "forbidden")
	case errors.Is(e, ErrMissing):
		v2Error(w, 404, "not_found")
	case errors.Is(e, ErrConflict):
		v2Error(w, 409, "conflict")
	default:
		v2Error(w, 503, "unavailable")
	}
}
func v2Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"requestId": uuid.NewString(), "error": map[string]string{"code": code}})
}

type V2CanonicalIdentity struct {
	Token                iface.Token
	PoolAddress, Creator string
}
type V2CanonicalSource interface {
	ResolveTokenInTx(context.Context, pgx.Tx, string) (V2CanonicalIdentity, error)
}
type V2IdentityAdapter struct{ Verifier sharedauth.MetadataV2Verifier }

func (v V2IdentityAdapter) AuthorizeInTx(ctx context.Context, tx pgx.Tx, a VerifiedAction) error {
	err := v.Verifier.AuthorizeInTx(ctx, tx, sharedauth.MetadataV2Action{Wallet: a.Wallet, OperationID: a.OperationID, Method: a.Method, Path: a.Path, BodyHash: a.BodyHash, Signature: a.Signature, Nonce: a.Nonce, ExpiresAt: a.ExpiresAt, Audience: a.Audience, ChainID: a.ChainID})
	var verification *sharedauth.VerificationError
	if errors.As(err, &verification) {
		if verification.Retryable {
			return err
		}
		return ErrForbidden
	}
	if errors.Is(err, sharedauth.ErrScopeDenied) || errors.Is(err, sharedauth.ErrReplayMismatch) {
		return ErrForbidden
	}
	return err
}

type V2ContractHandlers struct {
	Store     *ProductionStore
	Canonical V2CanonicalSource
	Clock     func() time.Time
}

func NewV2ProductionHandlers(pool *pgxpool.Pool, schema, audience string, backend sharedauth.ContractBackend, canonical V2CanonicalSource, clock func() time.Time) (V2ContractHandlers, error) {
	if backend == nil || canonical == nil || audience == "" {
		return V2ContractHandlers{}, ErrUnrepresentable
	}
	store, e := NewProductionStore(pool, schema, V2IdentityAdapter{sharedauth.MetadataV2Verifier{Backend: backend, Audience: audience, Clock: clock, NonceTable: schema + ".v2_nonces"}})
	if e != nil {
		return V2ContractHandlers{}, e
	}
	return V2ContractHandlers{store, canonical, clock}, nil
}
func ParseV2ContractPatch(raw []byte) (V2Patch, error) {
	var zero V2Patch
	if len(raw) > V2BodyLimit || !utf8.Valid(raw) || !json.Valid(raw) || !uniqueV2JSON(json.NewDecoder(bytes.NewReader(raw)), 0) {
		return zero, ErrValidation
	}
	if e := iface.ValidateJSON("MetadataPatchBody", raw); e != nil {
		return zero, ErrValidation
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return zero, ErrValidation
	}
	mapped := map[string]json.RawMessage{}
	for k, v := range fields {
		switch k {
		case "displayName":
			k = "name"
		case "displaySymbol":
			k = "symbol"
		case "socialLinks":
			k = "socials"
		}
		mapped[k] = v
	}
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if e := encoder.Encode(mapped); e != nil {
		return zero, ErrValidation
	}
	return ParseV2Patch(bytes.TrimSpace(encoded.Bytes()))
}
func V2ContractData(data V2Data, identity V2CanonicalIdentity) (iface.MetadataGetData, error) {
	var zero iface.MetadataGetData
	if identity.Token.Address != data.TokenAddress || identity.PoolAddress != data.PoolAddress || !addressRE.MatchString(identity.Creator) {
		return zero, ErrUnrepresentable
	}
	token := identity.Token
	token.MetadataRevision = iface.UInt256(data.Revision)
	token.LogoAssetId = data.LogoAssetID
	rawToken, e := json.Marshal(token)
	if e != nil || iface.ValidateJSON("Token", rawToken) != nil {
		return zero, ErrUnrepresentable
	}
	links := make([]V2Social, len(data.Display.Socials))
	copy(links, data.Display.Socials)
	tags := append([]string{}, data.Display.Tags...)
	body := map[string]any{"token": token, "displayName": data.Display.Name, "displaySymbol": data.Display.Symbol, "description": data.Display.Description, "tags": tags, "socialLinks": links, "revision": data.Revision}
	raw, e := json.Marshal(body)
	if e != nil {
		return zero, e
	}
	result, e := iface.DecodeMetadataGetData(raw)
	if e != nil {
		return zero, ErrUnrepresentable
	}
	return result, nil
}
func (h V2ContractHandlers) RegisterRoutes(r chi.Router) {
	r.Get("/v2/tokens/{tokenAddress}/metadata", h.get)
	r.Patch("/v2/tokens/{tokenAddress}/metadata", h.patch)
}
func (h V2ContractHandlers) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now()
}
func (h V2ContractHandlers) identity(ctx context.Context, tx pgx.Tx, token string) (V2CanonicalIdentity, error) {
	var zero V2CanonicalIdentity
	if h.Canonical == nil || !addressRE.MatchString(token) {
		return zero, ErrUnrepresentable
	}
	v, e := h.Canonical.ResolveTokenInTx(ctx, tx, token)
	if e != nil {
		return zero, e
	}
	raw, e := json.Marshal(v.Token)
	if e != nil || iface.ValidateJSON("Token", raw) != nil || v.Token.Address != token || !addressRE.MatchString(v.PoolAddress) || !addressRE.MatchString(v.Creator) || v.Token.SourcePosition == nil || v.Token.SourcePosition.ChainId != "125" {
		return zero, ErrUnrepresentable
	}
	return v, nil
}
func (h V2ContractHandlers) get(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil || h.Store.Verifier == nil || h.Canonical == nil {
		v2Error(w, 503, "unavailable")
		return
	}
	tx, e := h.Store.Pool.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer rollbackV2(r.Context(), tx)
	token := chi.URLParam(r, "tokenAddress")
	identity, e := h.identity(r.Context(), tx, token)
	if e != nil {
		v2Failure(w, e)
		return
	}
	data, e := h.Store.readV2(r.Context(), tx, token)
	if errors.Is(e, ErrMissing) {
		var blocked bool
		if e = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM "+h.Store.table("documents")+" WHERE pool=$1 AND blocked)", identity.PoolAddress).Scan(&blocked); e != nil {
			v2Failure(w, e)
			return
		}
		if blocked {
			v2Failure(w, ErrForbidden)
			return
		}
		data = V2Data{ChainID: 125, PoolAddress: identity.PoolAddress, TokenAddress: identity.Token.Address, Revision: "0", Display: V2Display{Tags: []string{}, Socials: []V2Social{}}, ModerationStatus: "allowed"}
	} else if e != nil {
		v2Failure(w, e)
		return
	}
	mapped, e := V2ContractData(data, identity)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		v2Failure(w, e)
		return
	}
	h.respond(w, mapped)
}
func (h V2ContractHandlers) patch(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil || h.Store.Verifier == nil || h.Canonical == nil {
		v2Error(w, 503, "unavailable")
		return
	}
	if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
		v2Error(w, 415, "unsupported_media_type")
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, V2BodyLimit))
	if e != nil {
		v2Error(w, 422, "invalid_metadata")
		return
	}
	p, e := ParseV2ContractPatch(raw)
	if e != nil {
		v2Failure(w, e)
		return
	}
	token := chi.URLParam(r, "tokenAddress")
	a := VerifiedAction{Wallet: strings.ToLower(r.Header.Get("X-Action-Wallet")), OperationID: r.Header.Get("Idempotency-Key"), Signature: r.Header.Get("X-Action-Signature"), Nonce: r.Header.Get("X-Action-Nonce"), ExpiresAt: r.Header.Get("X-Action-Expires"), Audience: r.Header.Get("X-Action-Audience"), ChainID: 125, Method: r.Method, Path: r.URL.Path}
	hash := sha256.Sum256(raw)
	a.BodyHash = hex.EncodeToString(hash[:])
	if !addressRE.MatchString(a.Wallet) || !uuidRE.MatchString(a.OperationID) || a.Method != "PATCH" || a.Path != "/v2/tokens/"+token+"/metadata" {
		v2Failure(w, ErrForbidden)
		return
	}
	tx, e := h.Store.Pool.Begin(r.Context())
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer rollbackV2(r.Context(), tx)
	identity, e := h.identity(r.Context(), tx, token)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if identity.Creator != a.Wallet {
		v2Failure(w, ErrForbidden)
		return
	}
	if _, e = tx.Exec(r.Context(), "INSERT INTO "+h.Store.table("pools")+"(pool,creator,token) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", identity.PoolAddress, identity.Creator, token); e != nil {
		v2Failure(w, e)
		return
	}
	var pool, creator string
	e = tx.QueryRow(r.Context(), "SELECT pool,creator FROM "+h.Store.table("pools")+" WHERE token=$1 FOR UPDATE", token).Scan(&pool, &creator)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if pool != identity.PoolAddress || creator != identity.Creator || creator != a.Wallet {
		v2Failure(w, ErrForbidden)
		return
	}
	data, e := h.Store.applyV2InTx(r.Context(), tx, token, raw, a, h.now(), p, func(v V2Data) error { _, err := V2ContractData(v, identity); return err })
	if e != nil {
		v2Failure(w, e)
		return
	}
	mapped, e := V2ContractData(data, identity)
	if e != nil {
		v2Failure(w, e)
		return
	}
	h.respond(w, mapped)
}
func (h V2ContractHandlers) respond(w http.ResponseWriter, v iface.MetadataGetData) {
	response, e := envelope(uuid.NewString(), v, "partial", h.now())
	if e != nil {
		v2Failure(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(response)
}
