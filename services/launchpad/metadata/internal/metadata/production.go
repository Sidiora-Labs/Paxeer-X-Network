package metadata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrForbidden = errors.New("creator authorization required")
var ErrMissing = errors.New("pool or approved asset unavailable")
var ErrConflict = errors.New("revision or operation conflict")
var ErrValidation = errors.New("invalid metadata")
var addressRE = regexp.MustCompile(`^0x[0-9a-f]{40}$`)
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Patch follows frozen MetadataPatchBody. D13's stricter description cap is
// applied; name/symbol/social fields require a reviewed interface extension.
type Patch struct {
	ExpectedRevision string    `json:"expectedRevision"`
	Description      *string   `json:"description,omitempty"`
	Tags             *[]string `json:"tags,omitempty"`
	LogoAssetID      *string   `json:"logoAssetId,omitempty"`
}

func ParsePatch(raw []byte) (Patch, error) {
	var p Patch
	if len(raw) > 16384 || !utf8.Valid(raw) {
		return p, ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return p, ErrValidation
	}
	if d.Decode(new(any)) != io.EOF {
		return p, ErrValidation
	}
	// encoding/json accepts duplicate object keys. Reject aliases and repeats.
	dec := json.NewDecoder(bytes.NewReader(raw))
	t, e := dec.Token()
	if e != nil || t != json.Delim('{') {
		return p, ErrValidation
	}
	seen := map[string]bool{}
	for dec.More() {
		k, e := dec.Token()
		if e != nil {
			return p, ErrValidation
		}
		s, ok := k.(string)
		if !ok || seen[s] || (s != "expectedRevision" && s != "description" && s != "tags" && s != "logoAssetId") {
			return p, ErrValidation
		}
		seen[s] = true
		var v json.RawMessage
		if dec.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return p, ErrValidation
		}
	}
	if !seen["expectedRevision"] || !revisionValid(p.ExpectedRevision) {
		return p, ErrValidation
	}
	if p.Description != nil {
		v, e := text(*p.Description, 0, 4096)
		if e != nil {
			return p, e
		}
		p.Description = &v
	}
	if p.Tags != nil {
		if len(*p.Tags) > 10 {
			return p, ErrValidation
		}
		tags := make([]string, 0, len(*p.Tags))
		seen := map[string]bool{}
		for _, v := range *p.Tags {
			v, e := text(v, 1, 256)
			if e != nil || seen[v] {
				return p, ErrValidation
			}
			seen[v] = true
			tags = append(tags, v)
		}
		p.Tags = &tags
	}
	if p.LogoAssetID != nil && !uuidRE.MatchString(*p.LogoAssetID) {
		return p, ErrValidation
	}
	return p, nil
}
func revisionValid(s string) bool {
	if s == "0" {
		return true
	}
	if len(s) < 1 || len(s) > 78 || s[0] == '0' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) < 78 || s <= "115792089237316195423570985008687907853269984665640564039457584007913129639935"
}
func text(s string, min, max int) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrValidation
	}
	s = norm.NFC.String(s)
	if n := utf8.RuneCountInString(s); n < min || n > max {
		return "", ErrValidation
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", ErrValidation
		}
	}
	return s, nil
}
func SafeLink(s string) bool {
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}

// VerifiedAction is produced exclusively by the identity verifier, which must
// consume its nonce atomically with the mutation through AuthorizeInTx.
type VerifiedAction struct {
	Wallet, OperationID, Method, Path, BodyHash string
	Signature, Nonce, ExpiresAt, Audience       string
	ChainID                                     uint64
}
type ActionVerifier interface {
	AuthorizeInTx(context.Context, pgx.Tx, VerifiedAction) error
}
type ProductionStore struct {
	Pool            *pgxpool.Pool
	Schema          string
	Verifier        ActionVerifier
	RasterWorker    RasterWorker
	ConnectedSearch bool
}

func NewProductionStore(p *pgxpool.Pool, schema string, v ActionVerifier) (*ProductionStore, error) {
	if p == nil || !regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`).MatchString(schema) {
		return nil, ErrValidation
	}
	return &ProductionStore{Pool: p, Schema: schema, Verifier: v}, nil
}
func (s *ProductionStore) table(n string) string { return pgx.Identifier{s.Schema, n}.Sanitize() }

// SchemaDDL is reviewed schema source for the serial migration owner. Runtime
// roles must not call it; tests apply it to their unique leased schema.
func (s *ProductionStore) SchemaDDL() string {
	return fmt.Sprintf(`CREATE TABLE %s(pool text PRIMARY KEY,creator text NOT NULL,token text UNIQUE NOT NULL);CREATE TABLE %s(pool text PRIMARY KEY REFERENCES %s(pool),revision numeric(78,0) NOT NULL DEFAULT 0,document jsonb NOT NULL DEFAULT '{}',asset_id text,moderation_revision numeric(78,0) NOT NULL DEFAULT 0,blocked boolean NOT NULL DEFAULT false);CREATE TABLE %s(id text PRIMARY KEY,pool text NOT NULL REFERENCES %s(pool),state text NOT NULL CHECK(state IN ('CREATED','UPLOADING','QUARANTINED','VALIDATING','APPROVED','REJECTED','DELETED')),hash text NOT NULL,size bigint NOT NULL CHECK(size BETWEEN 1 AND 6291456),object_key text NOT NULL,scanner_engine text NOT NULL,scanner_revision text NOT NULL,UNIQUE(pool,hash));CREATE TABLE %s(operation text PRIMARY KEY,pool text NOT NULL,actor text NOT NULL,body_hash text NOT NULL,revision numeric(78,0) NOT NULL);CREATE TABLE %s(pool text NOT NULL,revision numeric(78,0) NOT NULL,payload jsonb NOT NULL,PRIMARY KEY(pool,revision));`, s.table("pools"), s.table("documents"), s.table("pools"), s.table("assets"), s.table("pools"), s.table("operations"), s.table("outbox"))
}
func (s *ProductionStore) Apply(ctx context.Context, pool string, raw []byte, a VerifiedAction) (string, error) {
	if len(raw) > 16384 {
		return "", ErrValidation
	}
	raw = bytes.Clone(raw)
	pool = strings.ToLower(pool)
	a.Wallet = strings.ToLower(a.Wallet)
	if !addressRE.MatchString(pool) || !addressRE.MatchString(a.Wallet) || !uuidRE.MatchString(a.OperationID) {
		return "", ErrValidation
	}
	p, e := ParsePatch(raw)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(raw)
	digest := hex.EncodeToString(h[:])
	if a.Method != "PATCH" || a.Path != "/v1/tokens/"+pool+"/metadata" || a.BodyHash != digest || s.Verifier == nil {
		return "", ErrForbidden
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		tx.Rollback(c)
	}()
	var creator string
	token := pool
	e = tx.QueryRow(ctx, "SELECT creator,pool FROM "+s.table("pools")+" WHERE token=$1 FOR UPDATE", token).Scan(&creator, &pool)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", ErrMissing
	}
	if e != nil {
		return "", e
	}
	if !strings.EqualFold(creator, a.Wallet) {
		return "", ErrForbidden
	}
	var oldPool, oldActor, oldHash, revision string
	e = tx.QueryRow(ctx, "SELECT pool,actor,body_hash,revision::text FROM "+s.table("operations")+" WHERE operation=$1", a.OperationID).Scan(&oldPool, &oldActor, &oldHash, &revision)
	if e == nil {
		if s.Verifier.AuthorizeInTx(ctx, tx, a) != nil {
			return "", ErrForbidden
		}
		if oldPool != pool || oldActor != a.Wallet || oldHash != digest {
			return "", ErrConflict
		}
		return revision, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return "", e
	}
	if e = s.Verifier.AuthorizeInTx(ctx, tx, a); e != nil {
		return "", ErrForbidden
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("documents")+"(pool) VALUES($1) ON CONFLICT DO NOTHING", pool)
	if e != nil {
		return "", e
	}
	var current string
	var blocked bool
	e = tx.QueryRow(ctx, "SELECT revision::text,blocked FROM "+s.table("documents")+" WHERE pool=$1 FOR UPDATE", pool).Scan(&current, &blocked)
	if e != nil {
		return "", e
	}
	if current != p.ExpectedRevision || current == "115792089237316195423570985008687907853269984665640564039457584007913129639935" {
		return "", ErrConflict
	}
	if blocked {
		return "", ErrForbidden
	}
	if p.LogoAssetID != nil {
		var state string
		e = tx.QueryRow(ctx, "SELECT state FROM "+s.table("assets")+" WHERE id=$1 AND pool=$2 FOR SHARE", *p.LogoAssetID, pool).Scan(&state)
		if e != nil || state != "APPROVED" {
			return "", ErrMissing
		}
	}
	doc := map[string]any{}
	if p.Description != nil {
		doc["description"] = *p.Description
	}
	if p.Tags != nil {
		doc["tags"] = *p.Tags
	}
	patch, _ := json.Marshal(doc)
	e = tx.QueryRow(ctx, "UPDATE "+s.table("documents")+" SET revision=revision+1,document=document||$2::jsonb,asset_id=COALESCE($3,asset_id) WHERE pool=$1 RETURNING revision::text", pool, string(patch), p.LogoAssetID).Scan(&revision)
	if e != nil {
		return "", e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("operations")+" VALUES($1,$2,$3,$4,$5::numeric)", a.OperationID, pool, a.Wallet, digest, revision)
	if e != nil {
		return "", e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("outbox")+" SELECT pool,revision,jsonb_build_object('document',document,'assetId',asset_id,'actor',$3::text,'previousRevision',$4::text) FROM "+s.table("documents")+" WHERE pool=$1 AND revision=$2::numeric", pool, revision, a.Wallet, current)
	if e != nil {
		return "", e
	}
	return revision, tx.Commit(ctx)
}
func (s *ProductionStore) Restrict(ctx context.Context, pool, revision string, blocked bool) error {
	if !addressRE.MatchString(pool) || !revisionValid(revision) {
		return ErrValidation
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		tx.Rollback(c)
	}()
	var creator string
	if e = tx.QueryRow(ctx, "SELECT creator FROM "+s.table("pools")+" WHERE pool=$1 FOR UPDATE", pool).Scan(&creator); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.table("documents")+"(pool,moderation_revision,blocked) VALUES($1,$2::numeric,$3) ON CONFLICT(pool) DO UPDATE SET moderation_revision=EXCLUDED.moderation_revision,blocked=EXCLUDED.blocked WHERE "+s.table("documents")+".moderation_revision<EXCLUDED.moderation_revision", pool, revision, blocked)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func normalizedSearchText(s string) string { return strings.ToLower(norm.NFKC.String(s)) }
