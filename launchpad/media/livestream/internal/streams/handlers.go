// Package streams implements the media/livestream HTTP handlers, porting
// routes/streams.ts 1:1: stream lifecycle (create/go-live/end), public reads
// (by id, by pool, all live), the Livepeer webhook callback, and Redis-backed
// viewer heartbeats. Owner-only mutations are EIP-191 gated (invariant i4).
package streams

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"strconv"
	"strings"

	"log/slog"
	"net"
	"net/http"

	"time"

	"github.com/go-chi/chi/v5"

	goredis "github.com/redis/go-redis/v9"

	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/pkg/types"
)

// Livepeer is the subset of the Livepeer client the handlers depend on (kept as
// an interface for clean wiring; the concrete client is exercised end-to-end via
// httptest in tests — no fakes).
type Livepeer interface {
	CreateStream(ctx context.Context, name string) (*livepeer.CreateResponse, error)
	TerminateStream(ctx context.Context, streamID string) error
	PlaybackURL(playbackID string) string
	RtmpURL(streamKey string) string
}

// Deps are the handler dependencies.
type Authorization struct {
	Wallet    string
	SessionID string
	Pool      string
	Allowed   bool
	ExpiresAt time.Time
}
type Credential struct {
	Token          string `json:"token"`
	ProviderID     string `json:"providerId"`
	IngestEndpoint string `json:"ingestEndpoint"`
	ExpiresAt      string `json:"expiresAt"`
}
type CredentialAuthority interface {
	Credential(context.Context, Operation) (Credential, error)
}
type Deps struct {
	PlaybackWebhookSecret string
	ObserveOutcome        func(string)
	IngestKeys            *EncryptedCredentials
	OperationLedger       *sharedauth.Ledger
	OperationVerifier     *sharedauth.WalletOperationVerifier
	Restrictions          *RestrictionReader
	ViewerTokens          *ViewerTokenAuthority
	Lifecycle             *LifecycleStore
	Authorize             func(context.Context, *http.Request, string, Operation) (Authorization, error)
	ProviderReady         func(context.Context) error
	Credentials           CredentialAuthority
	Viewers               *ViewerCounter
	Playback              func(context.Context, Operation) (PlaybackSources, error)
	MarketCaps            *MarketCapReader
	DecodeWebhook         func([]byte) (ProviderEvent, error)
	WebhookMilliseconds   bool
	// BroadcastUnprovisioned marks a read-only deployment (outside production,
	// no Livepeer/identity authority): provider-bound routes answer the contract
	// 409 BROADCAST_NOT_PROVISIONED and errors use the v2 Error envelope.
	BroadcastUnprovisioned bool

	Queries             sqlcdb.Querier
	Redis               *goredis.Client
	Livepeer            Livepeer
	MaxStreamsPerWallet int
	WebhookSecret       string
	Logger              *slog.Logger
	Clock               func() time.Time // optional; defaults to time.Now
}

// Handlers serves the stream routes.
type Handlers struct {
	playbackWebhookSecret string
	observeOutcome        func(string)
	ingestKeys            *EncryptedCredentials
	operationLedger       *sharedauth.Ledger
	operationVerifier     *sharedauth.WalletOperationVerifier
	restrictions          *RestrictionReader
	viewerTokens          *ViewerTokenAuthority
	lifecycle             *LifecycleStore
	authorize             func(context.Context, *http.Request, string, Operation) (Authorization, error)
	providerReady         func(context.Context) error
	credentials           CredentialAuthority
	viewers               *ViewerCounter
	playback              func(context.Context, Operation) (PlaybackSources, error)
	marketCaps            *MarketCapReader
	decodeWebhook         func([]byte) (ProviderEvent, error)
	webhookMilliseconds   bool
	unprovisioned         bool

	q             sqlcdb.Querier
	redis         *goredis.Client
	livepeer      Livepeer
	maxStreams    int
	webhookSecret string
	logger        *slog.Logger
	clock         func() time.Time
}

// New constructs Handlers, applying defaults for optional dependencies.
func New(d Deps) *Handlers {
	clock := d.Clock
	if clock == nil {
		clock = time.Now
	}
	maxStreams := d.MaxStreamsPerWallet
	if maxStreams <= 0 {
		maxStreams = 3
	}
	return &Handlers{
		playbackWebhookSecret: d.PlaybackWebhookSecret,
		observeOutcome:        d.ObserveOutcome,
		ingestKeys:            d.IngestKeys,
		operationLedger:       d.OperationLedger, operationVerifier: d.OperationVerifier, restrictions: d.Restrictions, viewerTokens: d.ViewerTokens,
		lifecycle: d.Lifecycle, authorize: d.Authorize, providerReady: d.ProviderReady, credentials: d.Credentials, viewers: d.Viewers, playback: d.Playback, marketCaps: d.MarketCaps, decodeWebhook: d.DecodeWebhook, webhookMilliseconds: d.WebhookMilliseconds, unprovisioned: d.BroadcastUnprovisioned,
		q:             d.Queries,
		redis:         d.Redis,
		livepeer:      d.Livepeer,
		maxStreams:    maxStreams,
		webhookSecret: d.WebhookSecret,
		logger:        d.Logger,
		clock:         clock,
	}
}

// RegisterRoutes mounts the stream endpoints onto r.
func (h *Handlers) RegisterRoutes(r chi.Router) {
	legacy := func(w http.ResponseWriter, r *http.Request) { h.lifecycleError(w, ErrUnavailable) }
	r.Post("/streams", legacy)
	r.Post("/streams/{id}/go-live", legacy)
	r.Post("/streams/{id}/end", legacy)
	r.Post("/streams/{id}/heartbeat", legacy)
	r.Get("/streams/live", legacy)
	r.Get("/streams/pool/{poolAddress}", legacy)
	r.Get("/streams/{id}", legacy)
	r.Post("/webhooks/livepeer", h.provisioned(h.webhook))
	r.Post("/webhooks/livepeer/playback-access", h.provisioned(h.playbackAccess))
	r.Route("/v2", func(v chi.Router) {
		v.Post("/streams", h.provisioned(h.connectedCreate))
		v.Patch("/streams/{id}", h.provisioned(h.connectedEdit))
		v.Post("/streams/{id}/rotate-key", h.provisioned(func(w http.ResponseWriter, r *http.Request) { h.connectedAction(w, r, "rotate") }))
		v.Post("/streams/{id}/restore", h.provisioned(func(w http.ResponseWriter, r *http.Request) { h.connectedAction(w, r, "restore") }))
		v.Get("/streams/{id}/key-rotation", h.provisioned(h.keyRotation))
		v.Get("/streams/{id}/private-playback", h.provisioned(h.privatePlayback))
		v.Post("/streams/{id}/viewer-session", h.viewerSession)
		v.Get("/streams/live", h.listLive)
		v.Get("/streams/pool/{poolAddress}", h.listByPool)
		v.Get("/streams/operations/{id}", h.provisioned(h.operationStatus))
		v.Post("/streams/{id}/ingest-credential", h.provisioned(h.connectedCredential))
		v.Post("/streams/{id}/ingest-key", h.provisioned(h.connectedCredential))
		v.Post("/streams/{id}/go-live", h.provisioned(func(w http.ResponseWriter, r *http.Request) { h.connectedAction(w, r, "start") }))
		v.Post("/streams/{id}/end", h.provisioned(func(w http.ResponseWriter, r *http.Request) { h.connectedAction(w, r, "end") }))
		v.Post("/streams/{id}/heartbeat", h.connectedHeartbeat)
		v.Get("/streams/{id}", h.getByID)
	})
}

// ── POST /streams ───────────────────────────────────────────────────────────

// lifecycleError writes every lifecycle failure as the v2 Error envelope.
// Provisioned (connected), platform and development runtimes share this one
// writer: contractError owns the status/code table.
func (h *Handlers) lifecycleError(w http.ResponseWriter, err error) {
	h.contractError(w, err)
}
func (h *Handlers) authorization(r *http.Request, action string, o Operation) (Authorization, error) {
	if h.authorize == nil {
		return Authorization{}, ErrUnavailable
	}
	d, err := h.authorize(r.Context(), r, action, o)
	if err != nil {
		return Authorization{}, err
	}
	if !d.Allowed || !addressPattern.MatchString(d.Wallet) || !d.ExpiresAt.After(h.clock()) {
		return Authorization{}, ErrDenied
	}
	if o.Wallet != "" && !strings.EqualFold(d.Wallet, o.Wallet) {
		return Authorization{}, ErrDenied
	}
	return d, nil
}
func (h *Handlers) create(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	var body struct {
		PoolAddress string `json:"poolAddress"`
		Title       string `json:"title"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	d, err := h.authorization(r, "stream.create", Operation{Pool: body.PoolAddress, Title: body.Title})
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	ready := h.providerReady != nil && h.providerReady(r.Context()) == nil
	o, err := h.lifecycle.Create(r.Context(), CreateIntent{Wallet: d.Wallet, Pool: body.PoolAddress, Title: body.Title, IdempotencyKey: r.Header.Get("Idempotency-Key")}, CreatorDecision{Wallet: d.Wallet, Pool: d.Pool, Allowed: d.Allowed, ExpiresAt: d.ExpiresAt}, ready)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	result, err := AdaptV2Operation(o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	w.Header().Set("Location", "/v2/streams/operations/"+o.ID)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, map[string]any{"operation": result})
}
func (h *Handlers) action(w http.ResponseWriter, r *http.Request, action string) {
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
	if !decodeJSON(w, r, &body) {
		return
	}
	revision, err := strconv.ParseInt(body.Revision, 10, 64)
	if err != nil || revision < 1 {
		h.lifecycleError(w, ErrInvalid)
		return
	}
	d, err := h.authorization(r, "stream."+action, o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	o, err = h.lifecycle.Transition(r.Context(), o.ID, d.Wallet, action, revision, ActionDecision{Wallet: d.Wallet, OperationID: o.ID, Action: action, Revision: revision, Allowed: d.Allowed, ExpiresAt: d.ExpiresAt})
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if action == "end" {
		if err = h.lifecycle.ReconcileTermination(r.Context(), o.ID, h.livepeer); err != nil {
			h.lifecycleError(w, err)
			return
		}
	}
	result, err := AdaptV2Operation(o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 202, map[string]any{"operation": result})
}
func (h *Handlers) goLive(w http.ResponseWriter, r *http.Request) { h.action(w, r, "start") }
func (h *Handlers) end(w http.ResponseWriter, r *http.Request)    { h.action(w, r, "end") }
func (h *Handlers) operationStatus(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if _, err = h.authorization(r, "stream.read", o); err != nil {
		h.lifecycleError(w, err)
		return
	}
	result, err := AdaptV2Operation(o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"operation": result})
}
func (h *Handlers) ingestCredential(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.lifecycle == nil || h.credentials == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if _, err = h.authorization(r, "stream.credential", o); err != nil {
		h.lifecycleError(w, err)
		return
	}
	if (o.State != "READY" && o.State != "LIVE") || o.ProviderID == "" {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	c, err := h.credentials.Credential(r.Context(), o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	expiry, err := time.Parse(time.RFC3339, c.ExpiresAt)
	if err != nil || !expiry.After(h.clock()) || expiry.After(h.clock().Add(5*time.Minute)) || c.Token == "" || len(c.Token) > 4096 || c.ProviderID != o.ProviderID || len(c.ProviderID) > 80 {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	endpoint, endpointErr := url.Parse(c.IngestEndpoint)
	if endpointErr != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Hostname() == "" || len(c.IngestEndpoint) > 2048 {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	current, err := h.lifecycle.Get(r.Context(), o.ID)
	if err != nil || current.Revision != o.Revision || current.ProviderID != o.ProviderID || (current.State != "READY" && current.State != "LIVE") {
		h.lifecycleError(w, ErrConflict)
		return
	}
	writeJSON(w, 200, map[string]any{"streamId": o.ID, "ingestCredential": c})
}
func (h *Handlers) public(ctx context.Context, o Operation) (V2Stream, error) {
	if h.restrictions == nil {
		return V2Stream{}, ErrUnavailable
	}
	allowed, err := h.restrictions.Allowed(ctx, o.Wallet, o.Pool)
	if err != nil {
		return V2Stream{}, ErrUnavailable
	}
	if !allowed {
		o.State = "SUSPENDED"
	}
	private, _, err := h.lifecycle.PlaybackPrivacy(ctx, o.ID)
	if err != nil {
		return V2Stream{}, ErrUnavailable
	}
	if private {
		o.State = "SUSPENDED"
	}
	var sources PlaybackSources
	if o.State == "LIVE" {
		if h.playback == nil {
			// A read-only deployment has no provider to resolve playback: the
			// live document cannot be served here (a deployment state), while
			// a provisioned runtime without playback is a dependency failure.
			if h.unprovisioned {
				return V2Stream{}, ErrNotProvisioned
			}
			return V2Stream{}, ErrUnavailable
		}
		var err error
		sources, err = h.playback(ctx, o)
		if err != nil {
			return V2Stream{}, ErrUnavailable
		}
	}
	var count *int64
	quality := "unavailable"
	if h.viewers != nil {
		if estimate, err := h.viewers.Estimate(ctx, o.ID); err == nil {
			count = &estimate.Count
			quality = "verified"
			if estimate.Stale {
				quality = "stale"
			}
		}
	}
	view, err := AdaptV2Stream(o, sources.URL, count, quality)
	if err != nil {
		return V2Stream{}, err
	}
	return withThumbnail(view, sources), nil
}
func (h *Handlers) list(w http.ResponseWriter, r *http.Request, pool string, live bool) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	operations, nextCursor, err := h.lifecycle.Page(r.Context(), pool, live, r.URL.Query().Get("cursor"), r.URL.Query().Get("limit"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	result := []V2Stream{}
	for _, o := range operations {
		item, err := h.public(r.Context(), o)
		if err != nil {
			h.lifecycleError(w, err)
			return
		}
		if live && item.Status != "live" {
			continue
		}
		result = append(result, item)
	}
	writeJSON(w, 200, map[string]any{"streams": result, "nextCursor": nextCursor, "generatedAt": h.clock().UTC().Format(time.RFC3339Nano)})
}
func (h *Handlers) listByPool(w http.ResponseWriter, r *http.Request) {
	h.list(w, r, chi.URLParam(r, "poolAddress"), r.URL.Query().Get("live") == "true")
}
func (h *Handlers) listLive(w http.ResponseWriter, r *http.Request) {
	switch sort := r.URL.Query().Get("sort"); sort {
	case "":
		h.list(w, r, "", true)
	case SortViewers, SortMarketCap:
		h.listRanked(w, r, sort)
	default:
		h.lifecycleError(w, ErrInvalid)
	}
}
func (h *Handlers) getByID(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	result, err := h.public(r.Context(), o)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *Handlers) webhook(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	body, ok := VerifyProviderWebhook(w, r, h.webhookSecret, h.clock(), h.webhookMilliseconds)
	if !ok {
		return
	}
	if h.decodeWebhook == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	event, err := h.decodeWebhook(body)
	if err != nil {
		h.lifecycleError(w, ErrInvalid)
		return
	}
	outcome, err := h.lifecycle.Observe(r.Context(), event)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if h.observeOutcome != nil {
		h.observeOutcome(outcome)
	}
	writeJSON(w, 200, map[string]string{"outcome": outcome})
}
func (h *Handlers) heartbeat(w http.ResponseWriter, r *http.Request) {
	if h.lifecycle == nil || h.viewers == nil {
		h.lifecycleError(w, ErrUnavailable)
		return
	}
	o, err := h.lifecycle.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if !o.PlaybackAllowed() {
		h.lifecycleError(w, ErrDenied)
		return
	}
	var body struct {
		ViewerID string `json:"viewerId"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	d, err := h.authorization(r, "stream.heartbeat", Operation{})
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	if !uuidPattern.MatchString(d.SessionID) || body.ViewerID != d.SessionID {
		h.lifecycleError(w, ErrDenied)
		return
	}
	estimate, err := h.viewers.Heartbeat(r.Context(), o.ID, d.SessionID)
	if err != nil {
		h.lifecycleError(w, err)
		return
	}
	current, err := h.lifecycle.Get(r.Context(), o.ID)
	if err != nil || current.Revision != o.Revision || !current.PlaybackAllowed() {
		h.lifecycleError(w, ErrDenied)
		return
	}
	quality := "verified"
	if estimate.Stale {
		quality = "stale"
	}
	writeJSON(w, 200, map[string]any{"viewerCount": estimate.Count, "viewerQuality": quality})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		writeRequestError(w, http.StatusBadRequest, ruleBodyUnreadable, "Invalid request body")
		return false
	}
	if len(raw) > 1<<20 {
		writeRequestError(w, http.StatusRequestEntityTooLarge, ruleBodyTooLarge, "Request too large")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(dst) != nil || decoder.Decode(new(any)) != io.EOF {
		writeRequestError(w, http.StatusBadRequest, ruleJSON, "Invalid JSON body")
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	sharedhttp.WriteJSON(w, status, v)
}

func (h *Handlers) logErr(op string, err error) {
	if h.logger != nil {
		h.logger.Error("livestream handler error", slog.String("op", op), slog.String("err", err.Error()))
	}
}

func validHMAC(secret string, body []byte, providedHex string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := mac.Sum(nil)
	provided, err := hex.DecodeString(providedHex)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, provided)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func poolRowToView(r sqlcdb.ListPoolStreamsRow) types.StreamView {
	return types.StreamView{
		ID:            r.ID,
		PoolAddress:   r.PoolAddress,
		CreatorWallet: r.CreatorWallet,
		Title:         r.Title,
		PlaybackURL:   r.PlaybackUrl,
		PlaybackID:    r.PlaybackID,
		IsLive:        r.IsLive,
		ViewerCount:   r.ViewerCount,
		StartedAt:     r.StartedAt,
		EndedAt:       r.EndedAt,
		CreatedAt:     r.CreatedAt,
	}
}

func streamToView(r sqlcdb.LivestreamStream) types.StreamView {
	return types.StreamView{
		ID:            r.ID,
		PoolAddress:   r.PoolAddress,
		CreatorWallet: r.CreatorWallet,
		Title:         r.Title,
		PlaybackURL:   r.PlaybackUrl,
		PlaybackID:    r.PlaybackID,
		IsLive:        r.IsLive,
		ViewerCount:   r.ViewerCount,
		StartedAt:     r.StartedAt,
		EndedAt:       r.EndedAt,
		CreatedAt:     r.CreatedAt,
	}
}

func liveRowToView(r sqlcdb.ListLiveStreamsRow) types.LiveStreamView {
	return types.LiveStreamView{
		ID:            r.ID,
		PoolAddress:   r.PoolAddress,
		CreatorWallet: r.CreatorWallet,
		Title:         r.Title,
		PlaybackURL:   r.PlaybackUrl,
		PlaybackID:    r.PlaybackID,
		IsLive:        r.IsLive,
		ViewerCount:   r.ViewerCount,
		StartedAt:     r.StartedAt,
		CreatedAt:     r.CreatedAt,
	}
}
