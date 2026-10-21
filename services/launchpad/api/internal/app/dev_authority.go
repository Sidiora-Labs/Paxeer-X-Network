package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
)

// Reviewed inputs that production reads from the bound NETWORK_AUTHORITY
// document. They are repository facts, verified by dev_authority_test.go:
//   - developmentPoolRuntime is keccak256 of the BeaconProxy deployedBytecode
//     (contracts/artifacts/contracts/base/BeaconProxy.sol/BeaconProxy.json; it
//     has no immutables, so every pool shares this runtime).
//   - developmentSerialInterface is the v2 interface bundle digest
//     (tools/production/bootstrap/check-registry.json task 1.99).
//   - developmentExplorer is the explorer the web ships for chain 125.
const (
	developmentPoolRuntime     = "0xbddb7d6cdac7937bd7cd40a0876b7430bcae1f26275a62d8c75208685e3e7ed0"
	developmentSerialInterface = "47b6950f51763148683e42207a8b9adfadb828e31674b2dc4030a1722f5d1bad"
	developmentExplorer        = "https://paxscan.paxeer.app"
)

// developmentAuthority serves GET /v2/network/authority and POST
// /v2/preflight/* outside production. It uses the production builders
// (deployment.Attest, deployment.ExportRevision, capabilities.PreflightEngine)
// over the embedded reviewed manifest (shared/config/deployment/current.json),
// the configured NETWORK_RPC_URL and the development indexer cursor. Nothing is
// signed in production either, so nothing is signed here. While the manifest
// or the chain reports a pending governance hand-off, ExportRevision refuses a
// public export and the route answers 503, exactly as production does.
type developmentAuthority struct {
	store       *store.Store
	rpc         *rpc.Client
	reader      *capabilities.PinnedRPCReader
	attester    deployment.Reader
	engine      *capabilities.PreflightEngine
	manifest    deployment.Manifest
	endpoints   deployment.PublicEndpoints
	addresses   map[string]string
	origins     map[string]bool
	proxies     []string
	unavailable error
	logger      *slog.Logger
	mu          sync.Mutex
	lastReason  string
	cacheTTL    time.Duration
	cache       *authorityCache
}

// Bounds of the authority attestation cache. The default TTL keeps browsers
// (which poll about every 20 s) on one attestation per window; the stale bound
// stays inside the one minute attestation MaxAge so a stale document served
// while a refresh runs is still within its validity; staleBudget is the most a
// request with a usable cached value waits for that refresh.
const (
	defaultAuthorityCacheTTL = 20 * time.Second
	maxAuthorityCacheTTL     = 30 * time.Second
	authorityMaxStale        = 45 * time.Second
	authorityStaleBudget     = 250 * time.Millisecond
	authorityComputeTimeout  = 10 * time.Second
)

func authorityCacheTTL(seconds int) time.Duration {
	ttl := time.Duration(seconds) * time.Second
	if ttl <= 0 || ttl > maxAuthorityCacheTTL {
		return defaultAuthorityCacheTTL
	}
	return ttl
}

// authorityResult is one completed attestation: the validated document or the
// structured failure (deployment.Issue chain) that refused it.
type authorityResult struct {
	raw      []byte
	err      error
	observed time.Time // when the computation started (attestation observedAt)
	at       time.Time // when it completed; the TTL runs from here
}

// authorityCache collapses concurrent GET /v2/network/authority requests into
// one in-flight attestation and reuses its outcome, success or failure, for
// ttl. After ttl a request starts (or joins) a background refresh and waits at
// most budget for it before answering with the previous outcome, as long as
// that outcome is younger than maxStale. Without a usable outcome the request
// waits for the refresh up to its own deadline.
type authorityCache struct {
	ttl, maxStale, budget, timeout time.Duration
	compute                        func(context.Context) ([]byte, error)

	mu       sync.Mutex
	current  *authorityResult
	inflight chan struct{}
}

func newAuthorityCache(ttl time.Duration, compute func(context.Context) ([]byte, error)) *authorityCache {
	return &authorityCache{ttl: ttl, maxStale: authorityMaxStale, budget: authorityStaleBudget, timeout: authorityComputeTimeout, compute: compute}
}

// refreshLocked starts the single background computation unless one is
// already running and returns the channel closed when it completes.
func (c *authorityCache) refreshLocked() <-chan struct{} {
	if c.inflight != nil {
		return c.inflight
	}
	done := make(chan struct{})
	c.inflight = done
	go func() {
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		raw, err := c.compute(ctx)
		cancel()
		c.mu.Lock()
		c.current = &authorityResult{raw: raw, err: err, observed: started, at: time.Now()}
		c.inflight = nil
		c.mu.Unlock()
		close(done)
	}()
	return done
}

func (c *authorityCache) latest() authorityResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return *c.current
}

// get returns the outcome to serve, or false when none completed before ctx
// ended.
func (c *authorityCache) get(ctx context.Context) (authorityResult, bool) {
	c.mu.Lock()
	cur := c.current
	if cur != nil && time.Since(cur.at) < c.ttl {
		c.mu.Unlock()
		return *cur, true
	}
	done := c.refreshLocked()
	c.mu.Unlock()
	if cur != nil && time.Since(cur.observed) < c.maxStale {
		budget := time.NewTimer(c.budget)
		defer budget.Stop()
		select {
		case <-done:
			return c.latest(), true
		case <-budget.C:
		case <-ctx.Done():
		}
		return *cur, true
	}
	select {
	case <-done:
		return c.latest(), true
	case <-ctx.Done():
		return authorityResult{}, false
	}
}

// retryAfter is the time left before a cached outcome is recomputed, in whole
// seconds (at least one).
func (c *authorityCache) retryAfter(res authorityResult) int {
	remaining := c.ttl - time.Since(res.at)
	return max(1, int(math.Ceil(remaining.Seconds())))
}

// writeAuthorityUnavailable answers the shared v2 503 DEPENDENCY_UNAVAILABLE
// envelope with Retry-After/retryAfterMs set to when the cached failure is
// recomputed, instead of inviting a retry every second. A non-retryable
// envelope is passed through unchanged.
func writeAuthorityUnavailable(w http.ResponseWriter, r *http.Request, seconds int) {
	rec := httptest.NewRecorder()
	for k, v := range w.Header() {
		rec.Header()[k] = v
	}
	rest.WriteV2APIError(rec, r, 503, "DEPENDENCY_UNAVAILABLE")
	var body rest.APIError
	raw := rec.Body.Bytes()
	if json.Unmarshal(raw, &body) == nil && body.Retryable && body.RetryAfterMS != nil {
		ms := seconds * 1000
		body.RetryAfterMS = &ms
		if patched, err := json.Marshal(body); err == nil {
			raw = append(patched, '\n')
			rec.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
	}
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(raw)
}

func newDevelopmentAuthority(ctx context.Context, deps developmentReadDeps) (*developmentAuthority, error) {
	c := deps.cfg
	d := &developmentAuthority{store: deps.store, manifest: deployment.Current(), origins: map[string]bool{}, logger: slog.Default(), cacheTTL: authorityCacheTTL(c.NetworkAuthorityCacheTTLSec)}
	d.addresses = d.manifest.Addresses()
	for _, origin := range strings.Split(c.CORSAllowedOrigins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" && origin != "*" {
			d.origins[origin] = true
		}
	}
	for _, proxy := range strings.Split(c.TrustedProxyCIDRs, ",") {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			d.proxies = append(d.proxies, proxy)
		}
	}
	if c.NetworkRPCURL == "" {
		d.unavailable = errors.New("NETWORK_RPC_URL is not configured")
		return d, nil
	}
	u, err := url.Parse(c.NetworkRPCURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("NETWORK_RPC_URL must be an http(s) URL without credentials")
	}
	d.endpoints = deployment.PublicEndpoints{RPC: c.NetworkRPCURL, Explorer: developmentExplorer, AllowedRPCs: []string{c.NetworkRPCURL}, AllowedExplorers: []string{developmentExplorer}}
	client := &http.Client{Transport: boundedRPCTransport{base: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, ResponseHeaderTimeout: 3 * time.Second, MaxIdleConnsPerHost: 20}}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if d.rpc, err = rpc.DialOptions(ctx, c.NetworkRPCURL, rpc.WithHTTPClient(client)); err != nil {
		return nil, err
	}
	if d.reader, err = capabilities.NewPinnedRPCReader(d.rpc); err != nil {
		return nil, err
	}
	d.attester = d.reader
	if d.engine, err = capabilities.NewPreflightEngine(d.reader, d.manifest, developmentPoolRuntime, developmentSerialInterface, time.Minute, 10*time.Second); err != nil {
		return nil, err
	}
	return d, nil
}

// head pairs the development indexer cursor with the block hash the RPC
// reports at that height. The RPC hash field is used as-is: on chain 125
// (Cosmos EVM) it is not keccak(RLP(header)), so go-ethereum's recomputed
// Header.Hash() would name a block the node does not know.
func (d *developmentAuthority) head(ctx context.Context) (committedHead, error) {
	if d.unavailable != nil {
		return committedHead{}, d.unavailable
	}
	height, err := d.store.DevelopmentIndexerCursor(ctx, deployment.ChainID)
	if err != nil {
		return committedHead{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var block *struct {
		Number hexutil.Uint64 `json:"number"`
		Hash   common.Hash    `json:"hash"`
	}
	if err = d.rpc.CallContext(bounded, &block, "eth_getBlockByNumber", hexutil.EncodeUint64(height), false); err != nil || block == nil || uint64(block.Number) != height || block.Hash == (common.Hash{}) {
		return committedHead{}, errors.New("indexed block unavailable from RPC")
	}
	return committedHead{Number: strconv.FormatUint(height, 10), Hash: block.Hash.Hex()}, nil
}

// document assembles the authority envelope with the production builders and
// validates it against the shared v2 contract.
func (d *developmentAuthority) document(ctx context.Context) ([]byte, error) {
	head, err := d.head(ctx)
	if err != nil {
		return nil, err
	}
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: time.Minute, Timeout: 10 * time.Second}
	attestation, err := deployment.Attest(ctx, d.attester, d.manifest, common.HexToHash(head.Hash), policy)
	if err != nil {
		return nil, err
	}
	return d.envelope(attestation, head, policy)
}

// envelope exports an attestation of the committed head and serialises the
// contract envelope, holding it to ConnectedNetworkAuthorityResponse.
func (d *developmentAuthority) envelope(attestation deployment.ActivationAttestation, head committedHead, policy deployment.Policy) ([]byte, error) {
	if strconv.FormatUint(attestation.BlockNumber, 10) != head.Number || attestation.BlockHash != head.Hash {
		return nil, errors.New("attestation differs from committed head")
	}
	export, err := deployment.ExportRevision(d.manifest, attestation, policy, d.endpoints, d.engine.Revision(), d.addresses, d.addresses)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(authorityEnvelope{2, export, projectAttestation(attestation), head})
	if err != nil {
		return nil, err
	}
	if err = interfaces.ValidateJSON("ConnectedNetworkAuthorityResponse", raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// report logs why the authority is unavailable, once per distinct reason.
func (d *developmentAuthority) report(route string, err error) {
	reason := err.Error()
	d.mu.Lock()
	changed := reason != d.lastReason
	d.lastReason = reason
	d.mu.Unlock()
	if changed {
		d.logger.Warn("development network authority unavailable", "route", route, "reason", reason)
	}
}

// developmentPreflightOrigin keeps the production rule (the trusted Origin must
// be "https://"+Host) and additionally accepts a same-origin rewrite through a
// trusted proxy: when the peer address is inside TRUSTED_PROXY_CIDRS, the first
// X-Forwarded-Proto/X-Forwarded-Host values stand in for the scheme and Host.
func developmentPreflightOrigin(r *http.Request, origins map[string]bool, proxies []string) bool {
	origin, err := broker.TrustedHTTPOrigin(r, origins, proxies)
	if err != nil {
		return false
	}
	if origin == "https://"+r.Host {
		return true
	}
	if !trustedPeer(r.RemoteAddr, proxies) {
		return false
	}
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	return proto == "https" && host != "" && origin == "https://"+host
}

func trustedPeer(remote string, proxies []string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, cidr := range proxies {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return false
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (d *developmentAuthority) Register(r chi.Router, admission *rest.Admission) {
	if d.cache == nil {
		d.cache = newAuthorityCache(authorityCacheTTL(int(d.cacheTTL/time.Second)), d.document)
	}
	r.Method("GET", "/v2/network/authority", admission.Middleware(rest.AdmissionAggregate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorityMetadata(w)
		if r.URL.RawQuery != "" {
			rest.WriteV2APIError(w, r, 400, "QUERY_INVALID")
			return
		}
		// The attestation is computed once per cache window in the background
		// (deployment.Attest over the public RPC takes seconds); requests share
		// its outcome instead of each re-running it.
		ctx, cancel := context.WithTimeout(r.Context(), authorityComputeTimeout)
		defer cancel()
		res, ok := d.cache.get(ctx)
		if !ok {
			d.report("authority", errors.New("attestation_pending"))
			rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		if res.err != nil {
			d.report("authority", res.err)
			writeAuthorityUnavailable(w, r, d.cache.retryAfter(res))
			return
		}
		// res.raw is shared by every request in the window: never append to it.
		_, _ = w.Write(res.raw)
		_, _ = w.Write([]byte{'\n'})
	})))
	for path, family := range map[string]capabilities.Family{"create": capabilities.Creation, "quote": capabilities.PlainQuote, "trade": capabilities.Trading, "claim": capabilities.NFTFees} {
		family := family
		r.Method("POST", "/v2/preflight/"+path, admission.Middleware(rest.AdmissionAggregate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorityMetadata(w)
			if r.URL.RawQuery != "" {
				rest.WriteV2APIError(w, r, 400, "QUERY_INVALID")
				return
			}
			if !developmentPreflightOrigin(r, d.origins, d.proxies) {
				rest.WriteV2APIError(w, r, 403, "SCOPE_DENIED")
				return
			}
			raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
			if err != nil {
				rest.WriteV2APIError(w, r, 422, "FIELD_INVALID")
				return
			}
			var input capabilities.PreflightRequest
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Family != family {
				rest.WriteV2APIError(w, r, 422, "FIELD_INVALID")
				return
			}
			head, err := d.head(r.Context())
			if err != nil {
				d.report("preflight", err)
				rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
			if input.BlockHash != common.HexToHash(head.Hash) {
				rest.WriteV2APIError(w, r, 409, "REVISION_CONFLICT")
				return
			}
			servePreflight(w, r, d.engine, family, raw, func(err error) { d.report("preflight", err) })
		})))
	}
}

// registerDevelopmentNetworkAuthority mounts the non-production authority and
// preflight routes. A missing or unusable NETWORK_RPC_URL is logged and leaves
// the routes answering 503 DEPENDENCY_UNAVAILABLE.
func registerDevelopmentNetworkAuthority(r chi.Router, deps developmentReadDeps) {
	d, err := newDevelopmentAuthority(context.Background(), deps)
	if err != nil {
		slog.Default().Error("development network authority disabled", "reason", err.Error())
		d = &developmentAuthority{unavailable: err, origins: map[string]bool{}, logger: slog.Default()}
	}
	d.Register(r, deps.admission)
}
