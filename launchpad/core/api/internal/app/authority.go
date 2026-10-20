package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type requestIdentity interface {
	AuthenticateRequest(context.Context, *http.Request) (sharedauth.IdentityPrincipal, error)
}

func runtimeIdentity(c *config.Config) (sharedauth.MTLSIdentityAuthority, error) {
	caPEM, certPEM, keyPEM, err := c.IdentityTLSMaterial()
	if err != nil {
		return sharedauth.MTLSIdentityAuthority{}, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return sharedauth.MTLSIdentityAuthority{}, errors.New("identity trust roots unavailable")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return sharedauth.MTLSIdentityAuthority{}, err
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(c.IdentitySigningKey)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return sharedauth.MTLSIdentityAuthority{}, errors.New("identity assertion signing key unavailable")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}}, MaxIdleConns: 20, MaxIdleConnsPerHost: 20, ResponseHeaderTimeout: time.Second}
	issuer := sharedauth.ServiceRequestIssuer{Issuer: c.IdentityIssuer, Audience: c.IdentityAudience, Key: ed25519.PrivateKey(key), Transport: sharedauth.AssertionTransport{AssertionHeader: sharedauth.IdentityAssertionHeader, SignatureHeader: sharedauth.IdentitySignatureHeader}}
	return sharedauth.NewMTLSIdentityAuthority(c.IdentityURL, c.IdentityPeerCertSHA256, &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, issuer)
}

type networkAuthorityConfig struct {
	SchemaVersion          int                        `json:"schemaVersion"`
	Manifest               deployment.Manifest        `json:"manifest"`
	ReviewedPoolRuntime    string                     `json:"reviewedPoolRuntime"`
	SerialInterfaceSHA256  string                     `json:"serialInterfaceSha256"`
	ExpectedRevisionSHA256 string                     `json:"expectedRevisionSha256"`
	SourceSchema           string                     `json:"sourceSchema"`
	Endpoints              deployment.PublicEndpoints `json:"endpoints"`
	GoAddresses            map[string]string          `json:"goAddresses"`
	FrontendAddresses      map[string]string          `json:"frontendAddresses"`
}
type networkAuthority struct {
	pool        *pgxpool.Pool
	config      networkAuthorityConfig
	reader      *capabilities.PinnedRPCReader
	rpc         *rpc.Client
	engine      *capabilities.PreflightEngine
	unavailable error
	origins     map[string]bool
	proxies     []string
}
type committedHead struct {
	Number string `json:"number"`
	Hash   string `json:"hash"`
}
type authorityEnvelope struct {
	SchemaVersion int                     `json:"schemaVersion"`
	SerialExport  deployment.SerialExport `json:"serialExport"`
	Attestation   contractAttestation     `json:"attestation"`
	CommittedHead committedHead           `json:"committedHead"`
}

// contractAttestation is deployment.ActivationAttestation projected onto the
// frozen v2 contract ConnectedDeploymentActivationAttestation
// (additionalProperties: false). The builder's attestation also carries
// policyValues (the ProtocolConfig fee-curve getters), which it only fills when
// ProtocolConfig and USDL verify, i.e. on the eligible path; the contract does
// not declare it, so the projection drops exactly that field. Every other
// field, including all the web reads (runtime-authority.ts), is kept with the
// builder's own JSON encoding.
type contractAttestation struct {
	ManifestHash      string                         `json:"manifestHash"`
	DeploymentVersion string                         `json:"deploymentVersion"`
	BlockHash         string                         `json:"blockHash"`
	BlockNumber       uint64                         `json:"blockNumber"`
	ObservedAt        time.Time                      `json:"observedAt"`
	BlockTime         time.Time                      `json:"blockTime"`
	Identities        map[string]deployment.Identity `json:"identities"`
	ActualOwner       string                         `json:"actualOwner,omitempty"`
	PendingOwner      string                         `json:"pendingOwner,omitempty"`
	CreationFeeRaw    string                         `json:"creationFeeRaw,omitempty"`
	SettlementAddress string                         `json:"settlementAddress,omitempty"`
	USDLDecimals      int                            `json:"usdlDecimals"`
	Eligible          bool                           `json:"eligible"`
	Issues            []deployment.Issue             `json:"issues,omitempty"`
}

func projectAttestation(a deployment.ActivationAttestation) contractAttestation {
	return contractAttestation{
		ManifestHash: a.ManifestHash, DeploymentVersion: a.DeploymentVersion, BlockHash: a.BlockHash, BlockNumber: a.BlockNumber,
		ObservedAt: a.ObservedAt, BlockTime: a.BlockTime, Identities: a.Identities, ActualOwner: a.ActualOwner, PendingOwner: a.PendingOwner,
		CreationFeeRaw: a.CreationFeeRaw, SettlementAddress: a.SettlementAddress, USDLDecimals: a.USDLDecimals, Eligible: a.Eligible, Issues: a.Issues,
	}
}

var authoritySchema = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var authorityHash = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
var authorityDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,19})$`)

func newNetworkAuthority(ctx context.Context, pool *pgxpool.Pool, c *config.Config) (*networkAuthority, error) {
	a := &networkAuthority{pool: pool, origins: map[string]bool{}}
	for _, origin := range strings.Split(c.CORSAllowedOrigins, ",") {
		if origin = strings.TrimSpace(origin); origin != "" && origin != "*" {
			a.origins[origin] = true
		}
	}
	for _, proxy := range strings.Split(c.TrustedProxyCIDRs, ",") {
		if proxy = strings.TrimSpace(proxy); proxy != "" {
			a.proxies = append(a.proxies, proxy)
		}
	}
	if (c.NetworkAuthorityFile == "" && c.NetworkAuthorityDocument == "") || c.NetworkAuthoritySHA256 == "" || c.NetworkRPCURL == "" {
		a.unavailable = errors.New("reviewed network authority document and RPC unavailable")
		return a, nil
	}
	if err := config.ReadBoundDocument(c.NetworkAuthorityFile, c.NetworkAuthoritySHA256, &a.config); err != nil {
		return nil, err
	}
	d := a.config
	if d.SchemaVersion != 2 || !authoritySchema.MatchString(d.SourceSchema) {
		return nil, errors.New("network authority configuration invalid")
	}
	if err := d.Manifest.Validate(); err != nil {
		return nil, err
	}
	u, err := url.Parse(c.NetworkRPCURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("network authority RPC must be HTTPS")
	}
	client := &http.Client{Transport: boundedRPCTransport{base: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, ResponseHeaderTimeout: 3 * time.Second, MaxIdleConnsPerHost: 20}}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	a.rpc, err = rpc.DialOptions(ctx, c.NetworkRPCURL, rpc.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	a.reader, err = capabilities.NewPinnedRPCReader(a.rpc)
	if err != nil {
		a.Close()
		return nil, err
	}
	a.engine, err = capabilities.NewPreflightEngine(a.reader, d.Manifest, d.ReviewedPoolRuntime, d.SerialInterfaceSHA256, time.Minute, 10*time.Second)
	if err != nil {
		a.Close()
		return nil, err
	}
	if a.engine.Revision().RevisionSHA256 != d.ExpectedRevisionSHA256 {
		a.Close()
		return nil, errors.New("reviewed network revision mismatch")
	}
	if err = d.Manifest.CompareAddresses(d.GoAddresses); err != nil {
		a.Close()
		return nil, err
	}
	if err = d.Manifest.CompareAddresses(d.FrontendAddresses); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}
func (a *networkAuthority) Close() {
	if a != nil && a.rpc != nil {
		a.rpc.Close()
	}
}
func (a *networkAuthority) head(ctx context.Context) (committedHead, error) {
	if a == nil || a.unavailable != nil || a.engine == nil {
		return committedHead{}, errors.New("network authority unavailable")
	}
	var h committedHead
	var integrity string
	bounded, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	err := a.pool.QueryRow(bounded, "SELECT height::text,block_hash,integrity FROM "+pgx.Identifier{a.config.SourceSchema, "indexer_committed_heads_v2"}.Sanitize()+" WHERE chain_id=125 ORDER BY height DESC LIMIT 1").Scan(&h.Number, &h.Hash, &integrity)
	n, ok := new(big.Int).SetString(h.Number, 10)
	if err != nil || integrity != "healthy" || !authorityDecimal.MatchString(h.Number) || !authorityHash.MatchString(h.Hash) || common.HexToHash(h.Hash) == (common.Hash{}) || !ok || !n.IsUint64() || n.Uint64() > 9007199254740991 {
		return committedHead{}, errors.New("healthy committed network head unavailable")
	}
	h.Hash = common.HexToHash(h.Hash).Hex()
	return h, nil
}
func (a *networkAuthority) envelope(ctx context.Context) (authorityEnvelope, error) {
	head, err := a.head(ctx)
	if err != nil {
		return authorityEnvelope{}, err
	}
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: time.Minute, Timeout: 10 * time.Second}
	attestation, err := deployment.Attest(ctx, a.reader, a.config.Manifest, common.HexToHash(head.Hash), policy)
	if err != nil {
		return authorityEnvelope{}, err
	}
	if new(big.Int).SetUint64(attestation.BlockNumber).String() != head.Number || attestation.BlockHash != head.Hash {
		return authorityEnvelope{}, errors.New("attestation differs from committed head")
	}
	export, err := deployment.ExportRevision(a.config.Manifest, attestation, policy, a.config.Endpoints, a.engine.Revision(), a.config.GoAddresses, a.config.FrontendAddresses)
	if err != nil {
		return authorityEnvelope{}, err
	}
	return authorityEnvelope{2, export, projectAttestation(attestation), head}, nil
}
func (a *networkAuthority) Ready(ctx context.Context) error { _, err := a.envelope(ctx); return err }
func authorityMetadata(w http.ResponseWriter) {
	w.Header().Set("X-Request-ID", uuid.NewString())
	w.Header().Set("X-Schema-Version", "2")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
}
func (a *networkAuthority) Register(r chi.Router, admission *rest.Admission) {
	r.Method("GET", "/v2/network/authority", admission.Middleware(rest.AdmissionAggregate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorityMetadata(w)
		if r.URL.RawQuery != "" {
			rest.WriteV2APIError(w, r, 400, "QUERY_INVALID")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		body, err := a.envelope(ctx)
		if err != nil {
			rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	})))
	for path, family := range map[string]capabilities.Family{"create": capabilities.Creation, "quote": capabilities.PlainQuote, "trade": capabilities.Trading, "claim": capabilities.NFTFees} {
		family := family
		r.Method("POST", "/v2/preflight/"+path, admission.Middleware(rest.AdmissionAggregate)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorityMetadata(w)
			if r.URL.RawQuery != "" {
				rest.WriteV2APIError(w, r, 400, "QUERY_INVALID")
				return
			}
			origin, err := broker.TrustedHTTPOrigin(r, a.origins, a.proxies)
			if err != nil || origin != "https://"+r.Host {
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
			head, err := a.head(r.Context())
			if err != nil {
				rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
			if input.BlockHash != common.HexToHash(head.Hash) {
				rest.WriteV2APIError(w, r, 409, "REVISION_CONFLICT")
				return
			}
			servePreflight(w, r, a.engine, family, raw, func(err error) {
				slog.Default().Warn("network authority preflight unavailable", "reason", err.Error())
			})
		})))
	}
}

// servePreflight is the one v2 answer for /v2/preflight/*, shared by the
// production and development routes so both produce identical bodies for the
// same engine outcome. The engine handler performs the duplicate-key check and
// the hash-pinned reads (deterministic refusals already arrive as blocked 200
// results with a stable reasonCode); its output is then held to the contract:
//   - 200: the result projected onto ConnectedCapabilitiesPreflight and
//     validated (500 INTERNAL_ERROR if the projection does not validate);
//   - 400 (request the engine rejects): 422 FIELD_INVALID, not retryable;
//   - anything else (a failed or malformed chain read): the retryable
//     503 DEPENDENCY_UNAVAILABLE.
func servePreflight(w http.ResponseWriter, r *http.Request, engine *capabilities.PreflightEngine, family capabilities.Family, raw []byte, report func(error)) {
	engineRequest := r.Clone(r.Context())
	engineRequest.Body = io.NopCloser(bytes.NewReader(raw))
	recorder := httptest.NewRecorder()
	engine.Handler(family).ServeHTTP(recorder, engineRequest)
	switch recorder.Code {
	case http.StatusOK:
	case http.StatusBadRequest:
		rest.WriteV2APIError(w, r, 422, "FIELD_INVALID")
		return
	default:
		report(errors.New(strings.TrimSpace(recorder.Body.String())))
		rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	body, err := contractPreflight(recorder.Body.Bytes())
	if err != nil {
		report(err)
		rest.WriteV2APIError(w, r, 500, "INTERNAL_ERROR")
		return
	}
	_, _ = w.Write(append(body, '\n'))
}

// contractPreflight projects the engine's preflight onto the declared v2
// contract (ConnectedCapabilitiesPreflight). The engine also emits
// observedRevisionSha256 and capability.issues, which the contract does not
// declare (additionalProperties: false); the projection drops only those.
func contractPreflight(raw []byte) ([]byte, error) {
	var declared interfaces.ConnectedCapabilitiesPreflight
	if err := json.Unmarshal(raw, &declared); err != nil {
		return nil, err
	}
	body, err := json.Marshal(declared)
	if err != nil {
		return nil, err
	}
	if err = interfaces.ValidateJSON("ConnectedCapabilitiesPreflight", body); err != nil {
		return nil, err
	}
	return body, nil
}

type boundedRPCTransport struct{ base http.RoundTripper }

func (t boundedRPCTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if response.ContentLength > 2<<20 {
		response.Body.Close()
		return nil, errors.New("RPC response exceeds bound")
	}
	response.Body = &boundedRPCBody{ReadCloser: response.Body, remaining: 2 << 20}
	return response, nil
}

type boundedRPCBody struct {
	io.ReadCloser
	remaining int64
}

func (b *boundedRPCBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n > 0 {
			return 0, errors.New("RPC response exceeds bound")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, err
}

// RegisterNetworkReads mounts IF_DEPLOYMENT (GET /v2/network/deployment) and
// IF_TX_STATUS (GET /v2/transactions/{transactionHash}) over the bound
// document's reviewed manifest, the manifest GET /v2/network/authority
// attests, and the authority's HTTPS chain RPC client. Without the reviewed
// document and RPC both answer the frozen non-retryable 503 instead of a
// plain-text 404.
func (a *networkAuthority) RegisterNetworkReads(r chi.Router, admission *rest.Admission) {
	if a == nil || a.unavailable != nil || a.rpc == nil {
		for path, class := range map[string]rest.AdmissionClass{"/v2/network/deployment": rest.AdmissionCached, "/v2/transactions/{transactionHash}": rest.AdmissionAggregate} {
			r.Method(http.MethodGet, path, rest.ResponseMetadata(admission.Middleware(class)(http.HandlerFunc(rest.WriteV2NotConfiguredError))))
		}
		return
	}
	(&networkReads{manifest: a.config.Manifest, rpc: a.rpc, timeout: 3 * time.Second, logger: slog.Default()}).mount(r, admission)
}
