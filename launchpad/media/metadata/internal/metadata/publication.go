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
	"strings"
	"time"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const PublicationBodyLimit = V2BodyLimit + 4096
const PublicationQueueLimit = 10000
const PublicationAction = "metadata.publish"
const PublicationRoute = "/v3/tokens/{tokenAddress}/metadata-publications"

type PublicationBody struct {
	DraftID        string          `json:"draftId"`
	OperationID    string          `json:"operationId"`
	CreationTxHash string          `json:"creationTxHash"`
	Metadata       json.RawMessage `json:"metadata"`
	UploadRefs     []string        `json:"uploadRefs"`
}
type PublicationStatus struct {
	SchemaVersion int       `json:"schemaVersion"`
	OperationID   string    `json:"operationId"`
	DraftID       string    `json:"draftId"`
	TokenAddress  string    `json:"tokenAddress"`
	Creator       string    `json:"creator"`
	ContentHash   string    `json:"contentHash"`
	UploadRefs    []string  `json:"uploadRefs"`
	AttemptCount  int       `json:"attemptCount"`
	Status        string    `json:"status"`
	LastErrorCode string    `json:"lastErrorCode"`
	Revision      *string   `json:"revision"`
	AcceptedAt    time.Time `json:"acceptedAt"`
	ExpiresAt     time.Time `json:"expiresAt"`
}
type PublicationService struct {
	Media      ConnectedMedia
	Operations sharedauth.OperationAuthorizer
	Identities PublicationIdentities
}

// PublicationIdentities re-confirms, inside the admission or publication
// transaction, that the creator's identity session (wallet, session, account,
// auth epoch) is still current. Callers compare the returned account and
// epoch with the ones they hold.
type PublicationIdentities interface {
	ConfirmTx(context.Context, pgx.Tx, sharedauth.IdentityPrincipal) (sharedauth.IdentityPrincipal, error)
	// ReadyFor reports whether the authority is configured for the store pool.
	ReadyFor(*pgxpool.Pool) bool
}

// GuardedPublicationIdentities is the production authority: the users
// service's users.identity_guard_v2 function, read in the same transaction.
type GuardedPublicationIdentities struct {
	Resolver sharedauth.PostgresIdentityResolver
}

func (g GuardedPublicationIdentities) ConfirmTx(ctx context.Context, tx pgx.Tx, expected sharedauth.IdentityPrincipal) (sharedauth.IdentityPrincipal, error) {
	return g.Resolver.ResolveTx(ctx, tx, sharedauth.Principal{Wallet: expected.Wallet, SessionID: expected.SessionID})
}
func (g GuardedPublicationIdentities) ReadyFor(pool *pgxpool.Pool) bool {
	return pool != nil && g.Resolver.Pool == pool && g.Resolver.GuardFunction == "users.identity_guard_v2"
}

func publicationUUID(s string) bool {
	id, e := uuid.Parse(s)
	return e == nil && id != uuid.Nil && id.String() == s
}
func publicationHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func ParsePublicationBody(raw []byte) (PublicationBody, V2Patch, error) {
	var b PublicationBody
	var p V2Patch
	if len(raw) > PublicationBodyLimit || !json.Valid(raw) || !uniqueV2JSON(json.NewDecoder(bytes.NewReader(raw)), 0) {
		return b, p, ErrValidation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&b) != nil || d.Decode(new(any)) != io.EOF || !publicationUUID(b.DraftID) || !publicationUUID(b.OperationID) || !regexp.MustCompile(`^0x[0-9a-f]{64}$`).MatchString(b.CreationTxHash) || b.UploadRefs == nil || len(b.UploadRefs) > 1 {
		return b, p, ErrValidation
	}
	p, e := ParseV2ContractPatch(b.Metadata)
	if e != nil {
		return b, p, e
	}
	for _, id := range b.UploadRefs {
		if !publicationUUID(id) {
			return b, p, ErrValidation
		}
	}
	if (p.LogoAssetID == nil && len(b.UploadRefs) != 0) || (p.LogoAssetID != nil && (len(b.UploadRefs) != 1 || b.UploadRefs[0] != *p.LogoAssetID)) {
		return b, p, ErrValidation
	}
	return b, p, nil
}
func NewPublicationService(media ConnectedMedia, operations sharedauth.OperationAuthorizer, identities sharedauth.PostgresIdentityResolver) (PublicationService, error) {
	return newPublicationService(media, operations, GuardedPublicationIdentities{Resolver: identities})
}

// NewSessionCheckedPublicationService builds the production service for
// edge ingress, whose identity authority is the identity service's session
// check (SessionCheckIdentities) rather than the shared-schema SQL guard.
// Every other requirement is identical to NewPublicationService.
func NewSessionCheckedPublicationService(media ConnectedMedia, operations sharedauth.OperationAuthorizer, identities SessionCheckIdentities) (PublicationService, error) {
	return newPublicationService(media, operations, identities)
}

// NewDevelopmentPublicationService builds the same service with a non-SQL
// identity authority (the development gateway's session check). Every other
// requirement (ledger on the store pool, audit key, verifier, schema-pinned
// search_path, header policy) is identical to production.
func NewDevelopmentPublicationService(media ConnectedMedia, operations sharedauth.OperationAuthorizer, identities PublicationIdentities) (PublicationService, error) {
	return newPublicationService(media, operations, identities)
}

func newPublicationService(media ConnectedMedia, operations sharedauth.OperationAuthorizer, identities PublicationIdentities) (PublicationService, error) {
	s := PublicationService{Media: media, Operations: operations, Identities: identities}
	store := media.Metadata.Store
	if store == nil || store.Pool == nil || media.Metadata.Canonical == nil || media.Identity == nil || operations.Ledger.Pool != store.Pool || operations.Ledger.Environment == "" || len(operations.Ledger.AuditKey) < 32 || operations.Verifier == nil || identities == nil || !identities.ReadyFor(store.Pool) {
		return s, ErrUnrepresentable
	}
	searchPath := store.Pool.Config().ConnConfig.RuntimeParams["search_path"]
	if searchPath != store.Schema+",pg_catalog" && searchPath != (pgx.Identifier{store.Schema}).Sanitize()+",pg_catalog" {
		return s, ErrUnrepresentable
	}
	header := regexp.MustCompile(`^X-[A-Za-z0-9-]{1,60}$`)
	if !header.MatchString(operations.Transport.EnvelopeHeader) || !header.MatchString(operations.Transport.SignatureHeader) || strings.EqualFold(operations.Transport.EnvelopeHeader, operations.Transport.SignatureHeader) || strings.EqualFold(operations.Transport.EnvelopeHeader, "X-Request-ID") || strings.EqualFold(operations.Transport.SignatureHeader, "X-Request-ID") {
		return s, ErrUnrepresentable
	}
	s.Operations.Policies = sharedauth.PolicyRegistry{Version: "metadata-publication-v3", Actions: map[string]sharedauth.Policy{PublicationAction: {Scope: "owner", OwnerRequired: true, RouteTemplate: PublicationRoute}}}
	s.Operations.Routes = map[string]sharedauth.ActionRoute{PublicationAction: {Action: PublicationAction, Method: http.MethodPost, Template: PublicationRoute}}
	return s, nil
}
func (s *ProductionStore) PublicationSchemaDDL() string {
	return fmt.Sprintf(`CREATE TABLE %s(operation uuid PRIMARY KEY,draft_id uuid NOT NULL,token text NOT NULL,pool text NOT NULL REFERENCES %s(pool),creator text NOT NULL,creation_tx_hash text NOT NULL,content_hash text NOT NULL CHECK(content_hash ~ '^[a-f0-9]{64}$'),body_hash text NOT NULL CHECK(body_hash ~ '^[a-f0-9]{64}$'),body bytea NOT NULL CHECK(octet_length(body)<=36864),metadata bytea NOT NULL CHECK(octet_length(metadata)<=32768),upload_refs jsonb NOT NULL,envelope bytea NOT NULL,envelope_digest text NOT NULL,nonce text NOT NULL,request_id uuid NOT NULL,account_id uuid NOT NULL,session_id uuid NOT NULL,auth_epoch bigint NOT NULL,admission_response bytea NOT NULL,accepted_at timestamptz NOT NULL,expires_at timestamptz NOT NULL,CHECK(expires_at=accepted_at+interval '30 minutes'),UNIQUE(draft_id,token,content_hash));CREATE TABLE %s(operation uuid PRIMARY KEY REFERENCES %s(operation),state text NOT NULL CHECK(state IN('metadata_pending','publishing','published','rejected','authorization_required')),attempt_count integer NOT NULL DEFAULT 0 CHECK(attempt_count BETWEEN 0 AND 5),fence bigint NOT NULL DEFAULT 0 CHECK(fence>=0),lease_owner uuid,lease_until timestamptz,grant_nonce text NOT NULL,next_at timestamptz NOT NULL,last_error_code text NOT NULL DEFAULT '',result bytea,revision text,updated_at timestamptz NOT NULL);CREATE INDEX metadata_publication_due_v3 ON %s(next_at) WHERE state IN('metadata_pending','publishing');CREATE TABLE %s(operation uuid NOT NULL REFERENCES %s(operation),nonce text NOT NULL,envelope bytea NOT NULL,envelope_digest text NOT NULL,body_hash text NOT NULL,request_id uuid NOT NULL,account_id uuid NOT NULL,session_id uuid NOT NULL,auth_epoch bigint NOT NULL,response bytea NOT NULL,PRIMARY KEY(operation,nonce));`, s.table("metadata_publication_intents_v3"), s.table("pools"), s.table("metadata_publication_jobs_v3"), s.table("metadata_publication_intents_v3"), s.table("metadata_publication_jobs_v3"), s.table("metadata_publication_grants_v3"), s.table("metadata_publication_intents_v3"))
}
func (s PublicationService) RegisterRoutes(r chi.Router) {
	r.Get("/v3/metadata-publication-authority", s.authority)
	r.Post(PublicationRoute, s.enqueue)
	r.Get(PublicationRoute+"/{operationId}", s.status)
}
func (s PublicationService) ready() bool {
	return s.Media.Metadata.Store != nil && s.Media.Metadata.Store.Pool != nil && s.Operations.Ledger.Pool == s.Media.Metadata.Store.Pool && s.Media.Identity != nil && s.Media.Metadata.Canonical != nil && s.Operations.Verifier != nil && s.Identities != nil && s.Identities.ReadyFor(s.Media.Metadata.Store.Pool)
}
func (s PublicationService) authority(w http.ResponseWriter, r *http.Request) {
	if !s.ready() {
		v2Failure(w, ErrUnrepresentable)
		return
	}
	principal, e := s.Media.Identity.AuthenticateRequest(r.Context(), r)
	if e != nil {
		v2Failure(w, ErrForbidden)
		return
	}
	if e = principal.Validate(time.Now()); e != nil {
		v2Failure(w, ErrForbidden)
		return
	}
	publicationRespond(w, map[string]any{"schemaVersion": 3, "environment": s.Operations.Ledger.Environment, "envelopeHeader": s.Operations.Transport.EnvelopeHeader, "signatureHeader": s.Operations.Transport.SignatureHeader, "action": PublicationAction, "expiresAt": time.Now().UTC().Add(time.Minute)})
}
func (s PublicationService) enqueue(w http.ResponseWriter, r *http.Request) {
	if !s.ready() {
		v2Failure(w, ErrUnrepresentable)
		return
	}
	if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
		v2Error(w, 415, "unsupported_media_type")
		return
	}
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, PublicationBodyLimit))
	if e != nil {
		v2Failure(w, ErrValidation)
		return
	}
	b, _, e := ParsePublicationBody(raw)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if len(r.Header.Values("Idempotency-Key")) != 1 || len(r.Header.Values("X-Request-ID")) != 1 || r.Header.Get("Idempotency-Key") != b.OperationID {
		v2Failure(w, ErrValidation)
		return
	}
	principal, e := s.Media.Identity.AuthenticateRequest(r.Context(), r)
	if e != nil {
		v2Failure(w, ErrForbidden)
		return
	}
	tx, e := s.Media.Metadata.Store.Pool.Begin(r.Context())
	if e != nil {
		v2Failure(w, e)
		return
	}
	defer rollbackV2(r.Context(), tx)
	current, e := s.Identities.ConfirmTx(r.Context(), tx, principal)
	if e != nil {
		if errors.Is(e, sharedauth.ErrScopeDenied) {
			e = ErrForbidden
		}
		v2Failure(w, e)
		return
	}
	if current.AccountID != principal.AccountID || current.AuthEpoch != principal.AuthEpoch {
		v2Failure(w, ErrForbidden)
		return
	}
	token := chi.URLParam(r, "tokenAddress")
	identity, e := s.Media.Metadata.identity(r.Context(), tx, token)
	if e != nil {
		v2Failure(w, e)
		return
	}
	if identity.Creator != current.Wallet || identity.Token.SourcePosition == nil || identity.Token.SourcePosition.TransactionHash != b.CreationTxHash {
		v2Failure(w, ErrForbidden)
		return
	}
	req, e := s.Operations.AuthorizeRequest(r.Context(), r, current, PublicationAction, identity.Creator, raw)
	if e != nil || req.Envelope.Version != 2 {
		v2Failure(w, ErrForbidden)
		return
	}
	response, _, e := s.Operations.ExecuteTx(r.Context(), tx, req, func(ctx context.Context, tx pgx.Tx, validated []byte) ([]byte, error) {
		return s.admitInTx(ctx, tx, identity, current, req, b, validated)
	})
	if e != nil {
		// A signature that does not verify (or a denied scope) is the
		// caller's authorization failure, not a dependency outage.
		var verification *sharedauth.VerificationError
		if errors.Is(e, sharedauth.ErrReplayMismatch) {
			e = ErrConflict
		} else if errors.Is(e, sharedauth.ErrScopeDenied) || (errors.As(e, &verification) && !verification.Retryable) {
			e = ErrForbidden
		}
		v2Failure(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		v2Failure(w, e)
		return
	}
	publicationRespond(w, json.RawMessage(response))
}
func (s PublicationService) admitInTx(ctx context.Context, tx pgx.Tx, identity V2CanonicalIdentity, principal sharedauth.IdentityPrincipal, req sharedauth.OperationRequest, b PublicationBody, raw []byte) ([]byte, error) {
	store := s.Media.Metadata.Store
	if !bytes.Equal(raw, req.Body) || req.Actor != identity.Creator || req.Method != "POST" || req.Path != "/v3/tokens/"+identity.Token.Address+"/metadata-publications" || req.Envelope.BodyHash != publicationHash(raw) {
		return nil, ErrForbidden
	}
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", store.Schema+":publication-admission"); e != nil {
		return nil, e
	}
	var stored []byte
	var oldOp, oldHash string
	e := tx.QueryRow(ctx, "SELECT operation::text,body_hash,admission_response FROM "+store.table("metadata_publication_intents_v3")+" WHERE operation=$1 OR (draft_id=$2 AND token=$3 AND content_hash=$4)", b.OperationID, b.DraftID, identity.Token.Address, publicationHash(b.Metadata)).Scan(&oldOp, &oldHash, &stored)
	if e == nil {
		if oldOp != b.OperationID || oldHash != req.Envelope.BodyHash {
			return nil, ErrConflict
		}
		var state string
		var remaining bool
		if e = tx.QueryRow(ctx, "SELECT j.state,i.expires_at>clock_timestamp() AND j.attempt_count<5 FROM "+store.table("metadata_publication_jobs_v3")+" j JOIN "+store.table("metadata_publication_intents_v3")+" i USING(operation) WHERE j.operation=$1 FOR UPDATE OF j", b.OperationID).Scan(&state, &remaining); e != nil {
			return nil, e
		}
		if state == "authorization_required" && remaining {
			if e = s.appendPublicationGrant(ctx, tx, b.OperationID, principal, req, stored); e != nil {
				return nil, e
			}
			if _, e = tx.Exec(ctx, "UPDATE "+store.table("metadata_publication_jobs_v3")+" SET state='metadata_pending',grant_nonce=$2,last_error_code='',next_at=clock_timestamp(),updated_at=clock_timestamp() WHERE operation=$1 AND state='authorization_required'", b.OperationID, req.Envelope.Nonce); e != nil {
				return nil, e
			}
		}
		return stored, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return nil, e
	}
	var queued int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM "+store.table("metadata_publication_jobs_v3")+" WHERE state IN('metadata_pending','publishing')").Scan(&queued); e != nil {
		return nil, e
	}
	if queued >= PublicationQueueLimit {
		return nil, fmt.Errorf("publication_queue_full")
	}
	if _, e = tx.Exec(ctx, "INSERT INTO "+store.table("pools")+"(pool,creator,token) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", identity.PoolAddress, identity.Creator, identity.Token.Address); e != nil {
		return nil, e
	}
	var pool, creator string
	if e = tx.QueryRow(ctx, "SELECT pool,creator FROM "+store.table("pools")+" WHERE token=$1 FOR UPDATE", identity.Token.Address).Scan(&pool, &creator); e != nil {
		return nil, e
	}
	if pool != identity.PoolAddress || creator != identity.Creator {
		return nil, ErrForbidden
	}
	patch, e := ParseV2ContractPatch(b.Metadata)
	if e != nil {
		return nil, e
	}
	var revision string
	var blocked bool
	if e = tx.QueryRow(ctx, "SELECT COALESCE((SELECT revision::text FROM "+store.table("v2_documents")+" WHERE pool=$1),'0'),EXISTS(SELECT 1 FROM "+store.table("documents")+" WHERE pool=$1 AND blocked)", pool).Scan(&revision, &blocked); e != nil {
		return nil, e
	}
	if blocked {
		return nil, ErrForbidden
	}
	if revision != patch.ExpectedRevision {
		return nil, ErrConflict
	}
	var now time.Time
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return nil, e
	}
	status := PublicationStatus{SchemaVersion: 3, OperationID: b.OperationID, DraftID: b.DraftID, TokenAddress: identity.Token.Address, Creator: creator, ContentHash: publicationHash(b.Metadata), UploadRefs: b.UploadRefs, Status: "metadata_pending", AcceptedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	response, e := json.Marshal(status)
	if e != nil {
		return nil, e
	}
	env, e := json.Marshal(req.Envelope)
	if e != nil {
		return nil, e
	}
	statement, e := req.Envelope.Statement()
	if e != nil {
		return nil, e
	}
	uploads, e := json.Marshal(b.UploadRefs)
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+store.table("metadata_publication_intents_v3")+"(operation,draft_id,token,pool,creator,creation_tx_hash,content_hash,body_hash,body,metadata,upload_refs,envelope,envelope_digest,nonce,request_id,account_id,session_id,auth_epoch,admission_response,accepted_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)", b.OperationID, b.DraftID, identity.Token.Address, pool, creator, b.CreationTxHash, status.ContentHash, req.Envelope.BodyHash, raw, []byte(b.Metadata), string(uploads), env, publicationHash([]byte(statement)), req.Envelope.Nonce, req.RequestID, principal.AccountID, principal.SessionID, principal.AuthEpoch, response, now, status.ExpiresAt)
	if e != nil {
		return nil, e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+store.table("metadata_publication_jobs_v3")+"(operation,state,grant_nonce,next_at,updated_at) VALUES($1,'metadata_pending',$2,$3,$3)", b.OperationID, req.Envelope.Nonce, now)
	if e != nil {
		return nil, e
	}
	if e = s.appendPublicationGrant(ctx, tx, b.OperationID, principal, req, response); e != nil {
		return nil, e
	}
	return response, nil
}
func (s PublicationService) status(w http.ResponseWriter, r *http.Request) {
	if !s.ready() {
		v2Failure(w, ErrUnrepresentable)
		return
	}
	p, e := s.Media.Identity.AuthenticateRequest(r.Context(), r)
	if e != nil {
		v2Failure(w, ErrForbidden)
		return
	}
	token, op := chi.URLParam(r, "tokenAddress"), chi.URLParam(r, "operationId")
	if !addressRE.MatchString(token) || !publicationUUID(op) {
		v2Failure(w, ErrValidation)
		return
	}
	store := s.Media.Metadata.Store
	var admission []byte
	var state, code string
	var attempts int
	var revision *string
	e = store.Pool.QueryRow(r.Context(), "SELECT i.admission_response,j.state,j.attempt_count,j.last_error_code,j.revision FROM "+store.table("metadata_publication_intents_v3")+" i JOIN "+store.table("metadata_publication_jobs_v3")+" j USING(operation) WHERE i.operation=$1 AND i.token=$2 AND i.creator=$3", op, token, p.Wallet).Scan(&admission, &state, &attempts, &code, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		e = ErrMissing
	}
	if e != nil {
		v2Failure(w, e)
		return
	}
	var result PublicationStatus
	if json.Unmarshal(admission, &result) != nil {
		v2Failure(w, ErrUnrepresentable)
		return
	}
	result.Status, result.AttemptCount, result.LastErrorCode, result.Revision = state, attempts, code, revision
	publicationRespond(w, result)
}
func publicationRespond(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": v})
}

func (s PublicationService) appendPublicationGrant(ctx context.Context, tx pgx.Tx, operation string, p sharedauth.IdentityPrincipal, req sharedauth.OperationRequest, response []byte) error {
	env, e := json.Marshal(req.Envelope)
	if e != nil {
		return e
	}
	statement, e := req.Envelope.Statement()
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO "+s.Media.Metadata.Store.table("metadata_publication_grants_v3")+"(operation,nonce,envelope,envelope_digest,body_hash,request_id,account_id,session_id,auth_epoch,response) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", operation, req.Envelope.Nonce, env, publicationHash([]byte(statement)), req.Envelope.BodyHash, req.RequestID, p.AccountID, p.SessionID, p.AuthEpoch, response)
	return e
}
