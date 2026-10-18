package streams

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"
)

// RestrictionReader answers whether a wallet may stream or appear in a pool
// from media/social's moderation bans, through chat.safety_bans_v1, the
// column-scoped versioned view (coordinator migration 380002) livestream_runtime
// is granted.
type RestrictionReader struct{ Pool *pgxpool.Pool }

func (p *RestrictionReader) Allowed(ctx context.Context, wallet, pool string) (bool, error) {
	if p == nil || p.Pool == nil || !addressPattern.MatchString(wallet) || !addressPattern.MatchString(pool) {
		return false, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var banned bool
	err := p.Pool.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM chat.safety_bans_v1 WHERE wallet=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND (scope='global' OR (scope='pool' AND resource=$2)))`, strings.ToLower(wallet), strings.ToLower(pool)).Scan(&banned)
	if err != nil {
		return false, ErrUnavailable
	}
	return !banned, nil
}
func (h *Handlers) operationMutation(w http.ResponseWriter, r *http.Request, action string, o Operation, body []byte, mutate func(context.Context, pgx.Tx, Authorization) (Operation, error)) {
	if h.lifecycle == nil || h.operationLedger == nil || h.operationVerifier == nil || h.restrictions == nil || h.operationLedger.Pool != h.lifecycle.pool {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	decision, err := h.authorization(r, action, o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if !strings.EqualFold(decision.Pool, o.Pool) {
		h.lifecycleError(w, ErrDenied)
		return
	}
	encoded := r.Header.Get("X-Kindle-Operation")
	signature := r.Header.Get("X-Kindle-Signature")
	if len(encoded) == 0 || len(encoded) > 4096 || len(signature) > 8194 {
		h.lifecycleError(w, ErrDenied)
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	if _, err = sharedauth.BodyDigest(raw); err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	var envelope sharedauth.OperationEnvelope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF {
		h.lifecycleError(w, ErrDenied)
		return
	}
	requestID, err := randomID()
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	req := sharedauth.OperationRequest{Envelope: envelope, Signature: signature, Body: body, Actor: decision.Wallet, Action: action, Method: r.Method, Path: r.URL.Path, RequestID: requestID, PolicyVersion: "livestream-actions-v2"}
	bounded, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	response, _, err := h.operationLedger.ExecuteValidated(bounded, req, func(ctx context.Context, wallet, message, sig string) error {
		if !decision.ExpiresAt.After(h.clock()) {
			return ErrDenied
		}
		return h.operationVerifier.Verify(ctx, wallet, message, sig)
	}, func(ctx context.Context, tx pgx.Tx, _ []byte) ([]byte, error) {
		allowed, e := h.restrictions.AllowedTx(ctx, tx, decision.Wallet, o.Pool)
		if e != nil {
			return nil, e
		}
		if !allowed || !decision.ExpiresAt.After(h.clock()) {
			return nil, ErrDenied
		}
		changed, e := mutate(ctx, tx, decision)
		if e != nil {
			return nil, e
		}
		result, e := AdaptV2Operation(changed)
		if e != nil {
			return nil, e
		}
		return json.Marshal(map[string]any{"operation": result})
	})
	if err != nil {
		if errors.Is(err, sharedauth.ErrReplayMismatch) {
			err = ErrConflict
		}
		var proof *sharedauth.VerificationError
		if errors.As(err, &proof) && !proof.Retryable {
			err = ErrDenied
		}
		h.lifecycleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write(response)
}
func boundedBody(w http.ResponseWriter, r *http.Request, max int, dst any) ([]byte, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Header.Get("Content-Encoding") != "" {
		writeRequestError(w, http.StatusUnsupportedMediaType, ruleMediaType, "JSON request required")
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
	if err != nil {
		writeRequestError(w, http.StatusBadRequest, ruleBodyUnreadable, "Invalid request body")
		return nil, false
	}
	if len(raw) > max {
		writeRequestError(w, http.StatusRequestEntityTooLarge, ruleBodyTooLarge, "Request too large")
		return nil, false
	}
	if !utf8.Valid(raw) {
		writeRequestError(w, http.StatusBadRequest, ruleUTF8, "Invalid UTF-8 body")
		return nil, false
	}
	if _, err = sharedauth.BodyDigest(raw); err != nil {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid JSON body")
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || decoder.Decode(new(any)) != io.EOF {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid JSON body")
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return raw, true
}
func (h *Handlers) connectedCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PoolAddress string `json:"poolAddress"`
		Title       string `json:"title"`
	}
	raw, ok := boundedBody(w, r, 16384, &body)
	if !ok {
		return
	}
	h.operationMutation(w, r, "stream.create", Operation{Pool: body.PoolAddress, Title: body.Title}, raw, func(ctx context.Context, tx pgx.Tx, d Authorization) (Operation, error) {
		ready := h.providerReady != nil && h.providerReady(ctx) == nil
		return h.lifecycle.CreateTx(ctx, tx, CreateIntent{Wallet: d.Wallet, Pool: body.PoolAddress, Title: body.Title, IdempotencyKey: r.Header.Get("Idempotency-Key")}, CreatorDecision{Wallet: d.Wallet, Pool: d.Pool, Allowed: d.Allowed, ExpiresAt: d.ExpiresAt}, ready)
	})
}
func (h *Handlers) connectedAction(w http.ResponseWriter, r *http.Request, action string) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	var body struct {
		Revision string `json:"revision"`
	}
	raw, ok := boundedBody(w, r, 1024, &body)
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(body.Revision, 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != body.Revision {
		h.lifecycleError(w, ErrInvalid)
		return
	}
	h.operationMutation(w, r, "stream."+action, o, raw, func(ctx context.Context, tx pgx.Tx, d Authorization) (Operation, error) {
		if action == "rotate" {
			return h.lifecycle.RequestRotationTx(ctx, tx, o.ID, d.Wallet, revision)
		}
		if action == "restore" {
			return h.lifecycle.RequestRestorationTx(ctx, tx, o.ID, d.Wallet, revision)
		}
		return h.lifecycle.TransitionTx(ctx, tx, o.ID, d.Wallet, action, revision, ActionDecision{Wallet: d.Wallet, OperationID: o.ID, Action: action, Revision: revision, Allowed: d.Allowed, ExpiresAt: d.ExpiresAt})
	})
}
func (h *Handlers) connectedEdit(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	var body struct {
		Title    string `json:"title"`
		Revision string `json:"revision"`
		Private  *bool  `json:"private"`
	}
	raw, ok := boundedBody(w, r, 16384, &body)
	if !ok {
		return
	}
	rev, err := strconv.ParseInt(body.Revision, 10, 64)
	if err != nil || strconv.FormatInt(rev, 10) != body.Revision {
		h.lifecycleError(w, ErrInvalid)
		return
	}
	h.operationMutation(w, r, "stream.edit", o, raw, func(ctx context.Context, tx pgx.Tx, d Authorization) (Operation, error) {
		changed, e := h.lifecycle.EditTx(ctx, tx, o.ID, d.Wallet, body.Title, rev)
		if e != nil {
			return Operation{}, e
		}
		if body.Private != nil {
			if e = h.lifecycle.SetPlaybackPrivacyTx(ctx, tx, changed, *body.Private); e != nil {
				return Operation{}, e
			}
		}
		return changed, nil
	})
}
func (s *LifecycleStore) EditTx(ctx context.Context, tx pgx.Tx, id, wallet, title string, revision int64) (Operation, error) {
	if tx == nil || revision < 1 || !frozenUUID.MatchString(id) || len([]rune(title)) < 1 || len([]rune(title)) > 120 || strings.TrimSpace(title) == "" {
		return Operation{}, ErrInvalid
	}
	o, err := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", s.schema), id))
	if err != nil {
		return Operation{}, err
	}
	if o.Revision != revision {
		return Operation{}, ErrConflict
	}
	if !strings.EqualFold(o.Wallet, wallet) || o.State == "ENDED" || o.State == "FAILED" || o.State == "SUSPENDED" {
		return Operation{}, ErrDenied
	}
	o, err = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET title=$2,revision=revision+1 WHERE id=$1 RETURNING "+opColumns, s.schema), id, title))
	if err != nil {
		return Operation{}, err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision)VALUES($1,$2,'title_updated',$3)", s.schema), id, wallet, o.Revision)
	return o, err
}

func (h *Handlers) viewerSession(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil || h.viewerTokens == nil || h.viewers == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	var body struct{}
	if _, ok := boundedBody(w, r, 1024, &body); !ok {
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if o.State != "LIVE" {
		h.lifecycleError(w, ErrState)
		return
	}
	allowed, err := h.restrictions.Allowed(r.Context(), o.Wallet, o.Pool)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if !allowed {
		h.lifecycleError(w, ErrDenied)
		return
	}
	private, policyRevision, err := h.lifecycle.PlaybackPrivacy(r.Context(), o.ID)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if private {
		if _, err = h.authorization(r, "stream.playback", o); err != nil {
			h.lifecycleError(w, err)
			return
		}
	}
	session, err := randomID()
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	token, expires, err := h.viewerTokens.IssuePolicyBound(o.ID, session, policyRevision, h.clock())
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if err = h.viewers.Initialize(r.Context(), o.ID); err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"streamId": o.ID, "sessionId": session, "token": token, "expiresAt": expires.UTC().Format(time.RFC3339)})
}
func (h *Handlers) connectedHeartbeat(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil || h.viewerTokens == nil || h.viewers == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	var body struct {
		SessionID string `json:"sessionId"`
		Token     string `json:"token"`
	}
	if _, ok := boundedBody(w, r, 1024, &body); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	private, policyRevision, privacyErr := h.lifecycle.PlaybackPrivacy(r.Context(), id)
	if errors.Is(privacyErr, ErrInvalid) {
		h.lifecycleError(w, ErrInvalid)
		return
	}
	if privacyErr != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if err := h.viewerTokens.VerifyPolicyBound(body.Token, id, body.SessionID, policyRevision, h.clock()); err != nil {
		h.lifecycleError(w, ErrViewerToken)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), id)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if o.State != "LIVE" {
		h.lifecycleError(w, ErrState)
		return
	}
	allowed, err := h.restrictions.Allowed(r.Context(), o.Wallet, o.Pool)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if !allowed {
		h.lifecycleError(w, ErrDenied)
		return
	}
	_ = private
	estimate, err := h.viewers.Heartbeat(r.Context(), id, body.SessionID)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if !estimate.Accepted {
		writeRateLimited(w, 15, "Viewer heartbeat rate limited")
		return
	}
	quality := "verified"
	if estimate.Stale {
		quality = "stale"
	}
	writeJSON(w, 200, map[string]any{"viewerCount": estimate.Count, "viewerQuality": quality, "estimated": true})
}

func (h *Handlers) connectedCredential(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil || h.operationLedger == nil || h.operationVerifier == nil || h.restrictions == nil || h.operationLedger.Pool != h.lifecycle.pool {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	var body struct{}
	raw, ok := boundedBody(w, r, 1024, &body)
	if !ok {
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	d, err := h.authorization(r, "stream.credential", o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	encoded := r.Header.Get("X-Kindle-Operation")
	if len(encoded) > 4096 {
		h.lifecycleError(w, ErrDenied)
		return
	}
	bytes, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	if _, err = sharedauth.BodyDigest(bytes); err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	var envelope sharedauth.OperationEnvelope
	if json.Unmarshal(bytes, &envelope) != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	id, err := randomID()
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	req := sharedauth.OperationRequest{Envelope: envelope, Signature: r.Header.Get("X-Kindle-Signature"), Body: raw, Actor: d.Wallet, Action: "stream.credential", Method: r.Method, Path: r.URL.Path, RequestID: id, PolicyVersion: "livestream-actions-v2"}
	_, _, err = h.operationLedger.ExecuteValidated(r.Context(), req, h.operationVerifier.Verify, func(ctx context.Context, tx pgx.Tx, _ []byte) ([]byte, error) {
		current, e := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", h.lifecycle.schema), o.ID))
		if e != nil {
			return nil, e
		}
		if !strings.EqualFold(current.Wallet, d.Wallet) || !d.ExpiresAt.After(h.clock()) {
			return nil, ErrDenied
		}
		if current.State != "READY" && current.State != "LIVE" {
			return nil, ErrState
		}
		allowed, e := h.restrictions.AllowedTx(ctx, tx, d.Wallet, current.Pool)
		if e != nil {
			return nil, e
		}
		if !allowed {
			return nil, ErrDenied
		}
		return []byte(`{"authorized":true}`), nil
	})
	if errors.Is(err, ErrState) {
		h.lifecycleError(w, ErrState)
		return
	}
	if err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/ingest-key") {
		if h.ingestKeys == nil {
			h.lifecycleError(w, ErrUnavailable)
			return
		}
		key, e := h.ingestKeys.LoadKey(r.Context(), o)
		if e != nil {
			h.lifecycleError(w, e)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"streamId": o.ID, "ingestKey": key})
		return
	}
	h.ingestCredential(w, r)
}
func (p *RestrictionReader) AllowedTx(ctx context.Context, tx pgx.Tx, wallet, pool string) (bool, error) {
	if p == nil || tx == nil || !addressPattern.MatchString(wallet) || !addressPattern.MatchString(pool) {
		return false, ErrUnavailable
	}
	var banned bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat.safety_bans_v1 WHERE wallet=$1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>clock_timestamp()) AND (scope='global' OR (scope='pool' AND resource=$2)))`, strings.ToLower(wallet), strings.ToLower(pool)).Scan(&banned)
	if err != nil {
		return false, ErrUnavailable
	}
	return !banned, nil
}

type ConnectedRuntime struct {
	Metrics   *LifecycleMetrics
	Deps      Deps
	Provision *ProvisionWorker
	Reconcile *ReconcileWorker
	Usage     *VendorUsageWorker
	Ready     func(context.Context) error
}

func NewConnectedRuntime(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, redis *goredis.Client, backend sharedauth.ContractBackend, decode func([]byte) (ProviderEvent, error)) (*ConnectedRuntime, error) {
	if err := cfg.ValidateConnected(); err != nil {
		return nil, err
	}
	if pool == nil || redis == nil || backend == nil || decode == nil {
		return nil, ErrUnavailable
	}
	lifecycle, err := NewLifecycleStore(pool, "livestream", cfg.MaxStreamsPerWallet, cfg.MaxGlobalStreams)
	if err != nil {
		return nil, err
	}
	var budget BudgetPolicy
	if err = config.ReadBoundDocument(cfg.BudgetPolicyFile, cfg.BudgetPolicySHA256, &budget); err != nil {
		return nil, err
	}
	if err = budget.ValidateVendor(); err != nil {
		return nil, err
	}
	lifecycle.Budget = &budget
	viewers, err := NewViewerCounter(redis, "livestream", 100000)
	if err != nil {
		return nil, err
	}
	authority, err := cfg.IdentityAuthority()
	if err != nil {
		return nil, err
	}
	policy := &RestrictionReader{Pool: pool}
	provider := livepeer.New(cfg.LivepeerAPIKey, cfg.LivepeerAPIURL, nil, livepeer.WithWebhookPlayback(cfg.PlaybackWebhookID))
	thumbnails, err := livepeer.NewThumbnailCache(provider, thumbnailRefresh, thumbnailCacheEntries)
	if err != nil {
		return nil, err
	}
	vault := &VaultTransit{Endpoint: cfg.VaultTransitURL, Token: cfg.VaultTransitToken, Key: cfg.VaultTransitKey}
	keys := &EncryptedCredentials{Store: lifecycle, Transit: vault, IngestEndpoint: cfg.IngestEndpoint}
	authorize := func(ctx context.Context, r *http.Request, action string, o Operation) (Authorization, error) {
		principal, err := authority.AuthenticateRequest(ctx, r)
		if err != nil {
			return Authorization{}, err
		}
		if !addressPattern.MatchString(o.Pool) {
			return Authorization{}, ErrDenied
		}
		var creator string
		if err = pool.QueryRow(ctx, `SELECT creator FROM indexer.indexer_canonical_creations_v1 WHERE pool=$1`, strings.ToLower(o.Pool)).Scan(&creator); err != nil {
			return Authorization{}, ErrUnavailable
		}
		if !strings.EqualFold(creator, principal.Wallet) || o.Wallet != "" && !strings.EqualFold(o.Wallet, principal.Wallet) {
			return Authorization{}, ErrDenied
		}
		allowed, err := policy.Allowed(ctx, principal.Wallet, o.Pool)
		if err != nil {
			return Authorization{}, err
		}
		return Authorization{Wallet: principal.Wallet, SessionID: principal.SessionID, Pool: strings.ToLower(o.Pool), Allowed: allowed, ExpiresAt: principal.AccessExpiresAt}, nil
	}
	usage := &VendorUsageWorker{Store: lifecycle, Provider: provider, AlertEndpoint: cfg.BudgetAlertURL, AlertKey: []byte(cfg.BudgetAlertKey)}
	ready := func(ctx context.Context) error {
		if err := provider.Available(ctx); err != nil {
			return ErrUnavailable
		}
		if err := provider.ValidatePlaybackWebhook(ctx, cfg.PlaybackWebhookID, cfg.PlaybackWebhookURL, cfg.PlaybackWebhookSecret); err != nil {
			return ErrUnavailable
		}
		if err := usage.Ready(ctx); err != nil {
			return ErrUnavailable
		}
		cipher, err := vault.Seal(ctx, "11111111-1111-4111-8111-111111111111", "readiness", "readiness_probe")
		if err != nil {
			return ErrUnavailable
		}
		plain, err := vault.Open(ctx, "11111111-1111-4111-8111-111111111111", "readiness", cipher)
		if err != nil || plain != "readiness_probe" {
			return ErrUnavailable
		}
		return nil
	}
	metrics := &LifecycleMetrics{}
	deps := Deps{PlaybackWebhookSecret: cfg.PlaybackWebhookSecret, ObserveOutcome: metrics.ObserveWebhook, Lifecycle: lifecycle, Authorize: authorize, ProviderReady: ready, IngestKeys: keys, Viewers: viewers, ViewerTokens: &ViewerTokenAuthority{Key: []byte(cfg.ViewerTokenKey)}, OperationLedger: &sharedauth.Ledger{Pool: pool, Environment: cfg.ActionEnvironment, AuditKey: []byte(cfg.ActionAuditKey)}, OperationVerifier: &sharedauth.WalletOperationVerifier{Backend: backend}, Restrictions: policy, Livepeer: provider, WebhookSecret: cfg.LivepeerWebhookSecret, DecodeWebhook: decode, WebhookMilliseconds: cfg.WebhookTimestampUnit == "milliseconds", Playback: ProviderPlayback(provider, thumbnails), MarketCaps: &MarketCapReader{Pool: pool}}
	return &ConnectedRuntime{Usage: usage, Metrics: metrics, Deps: deps, Provision: &ProvisionWorker{Store: lifecycle, Provider: provider, Credentials: keys, Restrictions: policy}, Reconcile: &ReconcileWorker{Store: lifecycle, Provider: provider, Restrictions: policy, Disagreement: metrics.DisagreementAlert}, Ready: ready}, nil
}
func (r *ConnectedRuntime) Run(ctx context.Context) error {
	if r == nil || r.Provision == nil || r.Reconcile == nil || r.Usage == nil {
		return ErrUnavailable
	}
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() (err error) {
		defer func() {
			if recover() != nil {
				err = errors.New("livestream_usage_worker_panicked")
			}
		}()
		return r.Usage.Run(ctx)
	})
	group.Go(func() (err error) {
		defer func() {
			if recover() != nil {
				err = errors.New("livestream_provision_worker_panicked")
			}
		}()
		return r.Provision.Run(ctx)
	})
	group.Go(func() (err error) {
		defer func() {
			if recover() != nil {
				err = errors.New("livestream_reconcile_worker_panicked")
			}
		}()
		return r.Reconcile.Run(ctx)
	})
	group.Go(func() (err error) {
		defer func() {
			if recover() != nil {
				err = errors.New("livestream_retention_worker_panicked")
			}
		}()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				if _, err := r.Provision.Store.ExpireTerminalKeys(ctx); err != nil {
					return err
				}
			}
		}
	})
	return group.Wait()
}

func (h *Handlers) playbackAccess(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil || h.viewerTokens == nil || h.restrictions == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	raw, ok := VerifyProviderWebhook(w, r, h.playbackWebhookSecret, h.clock(), true)
	if !ok {
		return
	}
	if len(raw) > 16384 {
		writeRequestError(w, http.StatusRequestEntityTooLarge, ruleBodyTooLarge, "Playback request too large")
		return
	}
	if _, err := sharedauth.BodyDigest(raw); err != nil {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid playback request")
		return
	}
	var request struct {
		Context struct {
			OperationID string `json:"operationId"`
		} `json:"context"`
		AccessKey string `json:"accessKey"`
		Timestamp int64  `json:"timestamp"`
	}
	if json.Unmarshal(raw, &request) != nil || !frozenUUID.MatchString(request.Context.OperationID) || len(request.AccessKey) > 512 {
		h.lifecycleError(w, ErrDenied)
		return
	}
	parts := strings.Split(request.AccessKey, ".")
	if len(parts) != 2 {
		h.lifecycleError(w, ErrDenied)
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	fields := strings.Split(string(payload), ".")
	if len(fields) != 4 {
		h.lifecycleError(w, ErrDenied)
		return
	}
	_, policyRevision, err := h.lifecycle.PlaybackPrivacy(r.Context(), request.Context.OperationID)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if err = h.viewerTokens.VerifyPolicyBound(request.AccessKey, request.Context.OperationID, fields[1], policyRevision, h.clock()); err != nil {
		h.lifecycleError(w, ErrDenied)
		return
	}
	bounded, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	operation, err := h.lifecycle.Get(bounded, request.Context.OperationID)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if operation.State != "LIVE" {
		h.lifecycleError(w, ErrDenied)
		return
	}
	allowed, err := h.restrictions.Allowed(bounded, operation.Wallet, operation.Pool)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if !allowed {
		h.lifecycleError(w, ErrDenied)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) privatePlayback(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil || h.playback == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if _, err = h.authorization(r, "stream.playback", o); err != nil {
		h.lifecycleError(w, err)
		return
	}
	private, _, err := h.lifecycle.PlaybackPrivacy(r.Context(), o.ID)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	if !private || o.State != "LIVE" {
		h.lifecycleError(w, ErrDenied)
		return
	}
	playback, err := h.playback(r.Context(), o)
	if err != nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	view, err := AdaptV2Stream(o, playback.URL, nil, "unavailable")
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, withThumbnail(view, playback))
}
