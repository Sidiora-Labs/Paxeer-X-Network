package receipt

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
)

type RPCCodeEpoch struct {
	Deployment decode.Deployment `json:"deployment"`
	Identity   deployment.Role   `json:"identity"`
}
type RPCDiscovery struct {
	Role        string
	ABIRevision string
	RuntimeHash string
	// Immutables lists the immutable-variable spans zeroed before hashing a
	// discovered SidioraERC20 runtime (per-instance immutables).
	Immutables []CodeRange `json:",omitempty"`
}
type RPCOptions struct {
	Manifest    deployment.Manifest
	CatalogJSON []byte
	Epochs      []RPCCodeEpoch
	Discovery   []RPCDiscovery
	GenesisHash common.Hash `json:",omitempty"`
	// Anchor replaces GenesisHash on pruned chains; exactly one is configured.
	Anchor *BlockAnchor `json:",omitempty"`
	// BlockIdentity is "keccak" (default) or "reported" (CometBFT-backed EVM).
	BlockIdentity string `json:",omitempty"`
	Timeout       time.Duration
}
type RPCQualification struct {
	client         *ethclient.Client
	options        RPCOptions
	registry       *decode.Registry
	manifestHash   string
	privateFixture bool
	reads          *rpcReadState
	sourceMu       sync.Mutex
	discovered     *RPCQualification
}

const (
	rpcScalarResponseBytes = 4096
	rpcCodeResponseBytes   = 2*(128<<10) + 4096
	rpcHeaderResponseBytes = 2*decode.MaxFieldBytes + 16384
	rpcBundleResponseBytes = 2*maxBundleBytes + (4 << 20)
	rpcReadGas             = 150000
	// scalarReadWindow bounds one pinned scalar read including an archive
	// retry (the primary itself is held to one second by the transport).
	scalarReadWindow = 3 * time.Second
)

type rpcReadState struct {
	calls    chan struct{}
	mu       sync.Mutex
	decimals map[string]*rpcDecimalRead
	// Block-pinned results are immutable for a given block identity (chain 125
	// has single-slot finality), so one qualification pass reuses them instead
	// of re-reading the same code epochs and blocks per log and per emitter.
	codeEpochs map[string]string
	blocks     map[common.Hash]cachedBlock
	// Archive-rate reads: range concurrency, readahead depth and the
	// read-ahead pins by height (see archive.go).
	concurrency, readahead                int
	ahead                                 map[uint64]*pinnedAhead
	aheadHits, bloomSkipped, receiptReads atomic.Uint64
}

type cachedBlock struct {
	block    pinnedBlock
	receipts types.Receipts
}

const pinnedCacheEntries = 4096

func (r *rpcReadState) code(key string) (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.codeEpochs[key]
	return v, ok
}

func (r *rpcReadState) storeCode(key, value string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.codeEpochs == nil || len(r.codeEpochs) >= pinnedCacheEntries {
		r.codeEpochs = map[string]string{}
	}
	r.codeEpochs[key] = value
}

func (r *rpcReadState) block(hash common.Hash) (cachedBlock, bool) {
	if r == nil {
		return cachedBlock{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.blocks[hash]
	return v, ok
}

func (r *rpcReadState) storeBlock(hash common.Hash, value cachedBlock) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.blocks == nil || len(r.blocks) >= 256 {
		r.blocks = map[common.Hash]cachedBlock{}
	}
	r.blocks[hash] = value
}

type rpcDecimalRead struct {
	done  chan struct{}
	value [32]byte
	err   error
}

type boundedRPCTransport struct {
	upstream *http.Transport
	calls    chan struct{}
	lifetime context.Context
	// historical, when set, is an archive endpoint asked for the same call when
	// the primary node answers that the requested height has been pruned.
	historical *url.URL
	archive    *http.Transport
	// archiveCalls bounds archive requests in flight; archiveAttempt bounds
	// one archive attempt inside the method's archive window.
	archiveCalls   chan struct{}
	archiveAttempt time.Duration
	// archivedMu guards the routing memory: the primary's reported pruning
	// floor and the identities of blocks the archive served below it. Calls
	// provably below the floor go straight to the archive instead of paying a
	// primary round trip first; every answer is verified identically.
	archivedMu sync.Mutex
	floor      uint64
	hashes     map[string]bool
}

var earliestAvailable = regexp.MustCompile(`earliest available is ([0-9]+)`)

func (t *boundedRPCTransport) archived(method string, params []json.RawMessage) bool {
	t.archivedMu.Lock()
	defer t.archivedMu.Unlock()
	if len(params) == 0 {
		return false
	}
	switch method {
	case "eth_getBlockByNumber":
		var tag string
		if json.Unmarshal(params[0], &tag) != nil || !strings.HasPrefix(tag, "0x") {
			return false
		}
		height, err := strconv.ParseUint(tag[2:], 16, 64)
		return err == nil && t.floor > 0 && height < t.floor
	case "eth_getBlockByHash", "eth_getBlockReceipts":
		var hash string
		return json.Unmarshal(params[0], &hash) == nil && t.hashes[strings.ToLower(hash)]
	case "eth_getCode", "eth_getStorageAt", "eth_call":
		var pinned struct {
			BlockHash string `json:"blockHash"`
		}
		return json.Unmarshal(params[len(params)-1], &pinned) == nil && t.hashes[strings.ToLower(pinned.BlockHash)]
	}
	return false
}

// learn records blocks the archive served so later reads pinned to them route
// directly to the archive.
func (t *boundedRPCTransport) learn(method string, encoded []byte) {
	if method != "eth_getBlockByNumber" && method != "eth_getBlockByHash" {
		return
	}
	var envelope struct {
		Result *struct {
			Hash string `json:"hash"`
		} `json:"result"`
	}
	if json.Unmarshal(encoded, &envelope) != nil || envelope.Result == nil || envelope.Result.Hash == "" {
		return
	}
	t.archivedMu.Lock()
	defer t.archivedMu.Unlock()
	if t.hashes == nil || len(t.hashes) >= 4096 {
		t.hashes = map[string]bool{}
	}
	t.hashes[strings.ToLower(envelope.Result.Hash)] = true
}

func (t *boundedRPCTransport) learnFloor(encoded []byte) {
	var envelope struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(encoded, &envelope) != nil || envelope.Error == nil {
		return
	}
	match := earliestAvailable.FindStringSubmatch(envelope.Error.Message)
	if match == nil {
		return
	}
	floor, err := strconv.ParseUint(match[1], 10, 64)
	if err != nil {
		return
	}
	t.archivedMu.Lock()
	defer t.archivedMu.Unlock()
	if floor > t.floor {
		t.floor = floor
	}
}

func (t *boundedRPCTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.GetBody == nil {
		return nil, ErrQualification
	}
	body, err := request.GetBody()
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(body, 4097))
	closeErr := body.Close()
	if err != nil || closeErr != nil || len(raw) > 4096 {
		return nil, ErrQualification
	}
	var call struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err = json.Unmarshal(raw, &call); err != nil {
		return nil, ErrQualification
	}
	limit, timeout := rpcScalarResponseBytes, time.Second
	switch call.Method {
	case "eth_call", "eth_getStorageAt", "eth_chainId", "eth_blockNumber":
	case "eth_getCode":
		limit = rpcCodeResponseBytes
	case "eth_getBlockByHash", "eth_getBlockByNumber":
		if len(call.Params) != 2 {
			return nil, ErrQualification
		}
		var full bool
		if err = json.Unmarshal(call.Params[1], &full); err != nil {
			return nil, ErrQualification
		}
		limit = rpcHeaderResponseBytes
		if full {
			limit, timeout = rpcBundleResponseBytes, 15*time.Second
		}
	case "eth_getBlockReceipts", "eth_getTransactionReceipt":
		limit, timeout = rpcBundleResponseBytes, 15*time.Second
	case "anvil_nodeInfo":
		limit = rpcHeaderResponseBytes
	default:
		return nil, ErrQualification
	}
	read, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	stop := context.AfterFunc(t.lifetime, cancel)
	defer stop()
	if err = t.lifetime.Err(); err != nil {
		return nil, err
	}
	if err = read.Err(); err != nil {
		return nil, err
	}
	direct := t.historical != nil && t.archived(call.Method, call.Params)
	var response *http.Response
	var encoded []byte
	if !direct {
		select {
		case t.calls <- struct{}{}:
		case <-read.Done():
			return nil, read.Err()
		}
		if err = read.Err(); err == nil {
			response, encoded, err = t.exchange(read, request.Clone(read), limit)
		}
		<-t.calls
		if err != nil {
			return nil, err
		}
	}
	if !direct && t.historical != nil {
		t.learnFloor(encoded)
	}
	if direct || t.historical != nil && historicalRetry(call.Method, encoded) {
		// The archive is slower than the primary; it gets its own bounded
		// window (three times the method's primary window) and its own
		// concurrency bound.
		archiveRead, archiveCancel := context.WithTimeout(request.Context(), 3*timeout)
		defer archiveCancel()
		stopArchive := context.AfterFunc(t.lifetime, archiveCancel)
		defer stopArchive()
		if response, encoded, err = t.archiveExchange(archiveRead, request, limit); err != nil {
			return nil, err
		}
		t.learn(call.Method, encoded)
	}
	response.Body = io.NopCloser(bytes.NewReader(encoded))
	response.ContentLength = int64(len(encoded))
	return response, nil
}

// archiveExchange sends the call to the archive under its concurrency bound.
// An attempt unanswered within archiveAttempt, a transport failure or a
// gateway overload (502/503/504) is retried with backoff while the window
// lasts; the answer is verified by the caller exactly like a primary answer.
func (t *boundedRPCTransport) archiveExchange(window context.Context, request *http.Request, limit int) (*http.Response, []byte, error) {
	calls := t.archiveCalls
	if calls == nil {
		calls = t.calls
	}
	select {
	case calls <- struct{}{}:
	case <-window.Done():
		return nil, nil, window.Err()
	}
	defer func() { <-calls }()
	var response *http.Response
	var encoded []byte
	var err error
	for attempt := 0; ; attempt++ {
		read, cancel := window, context.CancelFunc(func() {})
		if t.archiveAttempt > 0 {
			read, cancel = context.WithTimeout(window, t.archiveAttempt)
		}
		retry := request.Clone(read)
		retry.URL = t.historical
		retry.Host = t.historical.Host
		if retry.Body, err = request.GetBody(); err != nil {
			cancel()
			return nil, nil, err
		}
		response, encoded, err = t.exchangeVia(read, t.archive, retry, limit)
		cancel()
		overloaded := err == nil && (response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusGatewayTimeout)
		if overloaded {
			archiveOverloads.Add(1)
		}
		if err == nil && !overloaded || errors.Is(err, ErrQualification) || window.Err() != nil || attempt == 3 {
			if err != nil {
				return nil, nil, err
			}
			return response, encoded, nil
		}
		archiveRetries.Add(1)
		if overloaded {
			timer := time.NewTimer((250 * time.Millisecond) << attempt)
			select {
			case <-timer.C:
			case <-window.Done():
				timer.Stop()
				return response, encoded, nil
			}
		}
	}
}

func (t *boundedRPCTransport) exchange(read context.Context, request *http.Request, limit int) (*http.Response, []byte, error) {
	return t.exchangeVia(read, t.upstream, request, limit)
}

func (t *boundedRPCTransport) exchangeVia(read context.Context, upstream *http.Transport, request *http.Request, limit int) (*http.Response, []byte, error) {
	if upstream == nil {
		upstream = t.upstream
	}
	response, err := upstream.RoundTrip(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > int64(limit) {
		return nil, nil, fmt.Errorf("receipt RPC response exceeds byte cap: %w", ErrQualification)
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil {
		return nil, nil, err
	}
	if len(encoded) > limit {
		return nil, nil, fmt.Errorf("receipt RPC response exceeds byte cap: %w", ErrQualification)
	}
	if err = read.Err(); err != nil {
		return nil, nil, err
	}
	return response, encoded, nil
}

// historicalRetry reports whether the primary's answer is a retention miss the
// archive may answer: an explicit "pruned" refusal (height lookups), or, for
// reads addressed by block or transaction hash, a null result or "not found"
// (a pruned node does not know hashes below its floor). The archive's answer
// is verified exactly like the primary's would have been.
func historicalRetry(method string, encoded []byte) bool {
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(encoded, &envelope) != nil {
		return false
	}
	if envelope.Error != nil && IsPruned(errors.New(envelope.Error.Message)) {
		return true
	}
	switch method {
	case "eth_getBlockByHash", "eth_getBlockReceipts", "eth_getTransactionReceipt", "eth_getCode", "eth_getStorageAt", "eth_call":
		if envelope.Error != nil {
			return strings.Contains(strings.ToLower(envelope.Error.Message), "not found")
		}
		return len(envelope.Result) == 0 || string(envelope.Result) == "null"
	}
	return false
}

// NewBoundedRPCClient dials the qualification RPC. historical is optional: an
// archive endpoint consulted only for calls the primary answers as pruned.
func NewBoundedRPCClient(ctx context.Context, endpoint string, historical string) (*ethclient.Client, func(), error) {
	return NewBoundedRPCClientWithArchive(ctx, endpoint, historical, ArchiveOptions{})
}

// NewBoundedRPCClientWithArchive is NewBoundedRPCClient with explicit archive
// concurrency and per-attempt timeout.
func NewBoundedRPCClientWithArchive(ctx context.Context, endpoint string, historical string, archiveOptions ArchiveOptions) (*ethclient.Client, func(), error) {
	archiveOptions, err := archiveOptions.normalized()
	if err != nil {
		return nil, nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Fragment != "" {
		return nil, nil, ErrQualification
	}
	var archive *url.URL
	if historical != "" {
		archive, err = url.Parse(historical)
		if err != nil || (archive.Scheme != "http" && archive.Scheme != "https") || archive.Host == "" || archive.Fragment != "" {
			return nil, nil, ErrQualification
		}
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, nil, ErrQualification
	}
	transport := base.Clone()
	transport.MaxConnsPerHost = 16
	transport.MaxIdleConnsPerHost = 16
	transport.ResponseHeaderTimeout = time.Second
	transport.MaxResponseHeaderBytes = 8192
	archiveTransport := transport.Clone()
	archiveTransport.ResponseHeaderTimeout = archiveOptions.AttemptTimeout
	archiveTransport.MaxConnsPerHost = archiveOptions.Concurrency
	archiveTransport.MaxIdleConnsPerHost = archiveOptions.Concurrency
	lifetime, cancel := context.WithCancel(ctx)
	httpClient := &http.Client{
		// The archive window of a bundle read is three times its 15 s
		// primary window.
		Timeout:       45 * time.Second,
		Transport:     &boundedRPCTransport{upstream: transport, calls: make(chan struct{}, 16), lifetime: lifetime, historical: archive, archive: archiveTransport, archiveCalls: make(chan struct{}, archiveOptions.Concurrency), archiveAttempt: archiveOptions.AttemptTimeout},
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrQualification },
	}
	connection, err := rpc.DialOptions(ctx, endpoint, rpc.WithHTTPClient(httpClient))
	if err != nil {
		cancel()
		transport.CloseIdleConnections()
		return nil, nil, err
	}
	client := ethclient.NewClient(connection)
	var once sync.Once
	closeClient := func() {
		once.Do(func() {
			cancel()
			client.Close()
			transport.CloseIdleConnections()
			archiveTransport.CloseIdleConnections()
		})
	}
	return client, closeClient, nil
}

func NewRPCQualification(ctx context.Context, client *ethclient.Client, o RPCOptions) (*RPCQualification, error) {
	return NewRPCQualificationWithAttestation(ctx, client, o, nil)
}

// NewRPCQualificationWithAttestation is NewRPCQualification with a durable
// anchor attestation used only when every configured source has pruned the
// anchor block.
func NewRPCQualificationWithAttestation(ctx context.Context, client *ethclient.Client, o RPCOptions, attest AnchorAttestation) (*RPCQualification, error) {
	if err := o.Manifest.Validate(); err != nil {
		return nil, err
	}
	if o.Manifest.Status != "active" && o.Manifest.Status != "eligible" {
		return nil, ErrQualification
	}
	approved := deployment.Current()
	if o.Manifest.DeploymentVersion != approved.DeploymentVersion || o.Manifest.SourceManifestSHA256 != approved.SourceManifestSHA256 || o.Manifest.ActivationHeight != approved.ActivationHeight || len(o.Manifest.Roles) != len(approved.Roles) {
		return nil, ErrQualification
	}
	for role, id := range approved.Roles {
		if o.Manifest.Roles[role] != id {
			return nil, ErrQualification
		}
	}
	hash, err := o.Manifest.Hash()
	if err != nil {
		return nil, err
	}
	return newRPCQualification(ctx, client, o, hash, false, attest)
}

type PrivateFixtureRPCOptions struct {
	DeploymentVersion    string
	ActivationHeight     uint64
	Roles                map[string]deployment.Role
	CatalogJSON          []byte
	Epochs               []RPCCodeEpoch
	Discovery            []RPCDiscovery
	GenesisHash          common.Hash
	CreationTransactions map[common.Address]common.Hash
	Timeout              time.Duration
}

func NewPrivateFixtureRPCQualification(ctx context.Context, client *ethclient.Client, f PrivateFixtureRPCOptions) (*RPCQualification, error) {
	if client == nil || !strings.HasPrefix(f.DeploymentVersion, "private-fixture-") || f.ActivationHeight == 0 || len(f.Roles) < 12 || len(f.Roles) > 64 || len(f.CreationTransactions) == 0 || len(f.CreationTransactions) > 128 {
		return nil, ErrQualification
	}
	for _, role := range []string{"EventEmitter", "ProtocolConfig", "Router", "Quoter", "SidioraFactory", "PoolRegistry", "FeeAccumulator", "SidioraNFT", "FeesRouter", "OpticalRegistry", "PoolBeacon", "USDL"} {
		if !validRPCIdentity(f.Roles[role]) {
			return nil, ErrQualification
		}
	}
	seen := map[common.Address]bool{}
	for _, identity := range f.Roles {
		address := common.HexToAddress(identity.Address)
		if !validRPCIdentity(identity) || seen[address] {
			return nil, ErrQualification
		}
		seen[address] = true
	}
	if f.Roles["USDL"].Kind != "direct" || f.Roles["PoolBeacon"].Kind != "beacon" {
		return nil, ErrQualification
	}
	o := RPCOptions{Manifest: deployment.Manifest{DeploymentVersion: f.DeploymentVersion, ChainID: 125, ActivationHeight: f.ActivationHeight, Roles: f.Roles}, CatalogJSON: f.CatalogJSON, Epochs: f.Epochs, Discovery: f.Discovery, GenesisHash: f.GenesisHash, Timeout: f.Timeout}
	d, err := digest(f)
	if err != nil {
		return nil, err
	}
	a, err := newRPCQualification(ctx, client, o, hex.EncodeToString(d[:]), true, nil)
	if err != nil {
		return nil, err
	}
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	var nodeInfo json.RawMessage
	if err = client.Client().CallContext(read, &nodeInfo, "anvil_nodeInfo"); err != nil || len(nodeInfo) == 0 {
		return nil, ErrQualification
	}
	latest, latestHash, err := a.headerByNumber(read, nil)
	if err != nil || latest == nil || latest.Number == nil || latest.Number.Uint64() < f.ActivationHeight {
		return nil, ErrQualification
	}
	for address, txHash := range f.CreationTransactions {
		r, err := client.TransactionReceipt(read, txHash)
		if err != nil || r == nil || r.Status != 1 || r.ContractAddress != address {
			return nil, ErrQualification
		}
		block, receipts, err := a.pinnedBlockAt(read, r.BlockHash, 0)
		if err != nil || r.TransactionIndex >= uint(len(receipts)) || !receiptEqual(r, receipts[r.TransactionIndex]) {
			return nil, ErrQualification
		}
		tx := block.Transactions[r.TransactionIndex]
		if tx.To() != nil || tx.ChainId().Cmp(big.NewInt(125)) != 0 {
			return nil, ErrQualification
		}
		sender, err := types.Sender(types.LatestSignerForChainID(big.NewInt(125)), tx)
		if err != nil || crypto.CreateAddress(sender, tx.Nonce()) != address {
			return nil, ErrQualification
		}
	}
	for role, identity := range f.Roles {
		address := common.HexToAddress(identity.Address)
		proxied := identity.Kind != "direct" && identity.Kind != "surface" && identity.Kind != "precompile"
		if identity.Kind != "precompile" && f.CreationTransactions[address] == (common.Hash{}) || proxied && f.CreationTransactions[common.HexToAddress(identity.ImplementationAddress)] == (common.Hash{}) {
			return nil, ErrQualification
		}
		kind := role
		if kind == "USDL" {
			kind = "SidioraERC20"
		}
		e := RPCCodeEpoch{Deployment: decode.Deployment{Address: address, Role: kind}, Identity: identity}
		if _, err = a.validateCode(read, e, latestHash); err != nil {
			return nil, err
		}
	}
	decimals, err := a.word(read, common.HexToAddress(f.Roles["USDL"].Address), "decimals()", latestHash)
	if err != nil || new(big.Int).SetBytes(decimals).Cmp(big.NewInt(6)) != 0 {
		return nil, ErrQualification
	}
	return a, nil
}
func (a *RPCQualification) IsPrivateFixture() bool { return a.privateFixture }
func newRPCQualification(ctx context.Context, client *ethclient.Client, o RPCOptions, hash string, private bool, attest AnchorAttestation) (*RPCQualification, error) {
	pinned := o.GenesisHash != (common.Hash{})
	anchored := o.Anchor != nil && o.Anchor.Hash != (common.Hash{})
	if client == nil || pinned == anchored || o.Anchor != nil && !anchored || !validBlockIdentity(o.BlockIdentity) || len(o.Epochs) == 0 || len(o.Epochs) > 4096 || len(o.CatalogJSON) > 32<<20 {
		return nil, ErrQualification
	}
	if o.Timeout == 0 {
		o.Timeout = 15 * time.Second
	}
	if o.Timeout <= 0 || o.Timeout > 15*time.Second {
		return nil, ErrQualification
	}
	owned, err := clone(o)
	if err != nil {
		return nil, err
	}
	for _, template := range owned.Discovery {
		if (template.Role != "SidioraPool" && template.Role != "SidioraERC20") || !rpcHash(template.RuntimeHash) || len(template.ABIRevision) != 64 || !validCodeRanges(template.Role, template.Immutables) {
			return nil, ErrQualification
		}
	}
	deployments := make([]decode.Deployment, len(owned.Epochs))
	coverage := map[common.Address]bool{}
	for i, e := range owned.Epochs {
		if !validRPCIdentity(e.Identity) || e.Deployment.Address != common.HexToAddress(e.Identity.Address) || e.Deployment.From.Block < owned.Manifest.ActivationHeight {
			return nil, ErrQualification
		}
		matched := false
		for role, id := range owned.Manifest.Roles {
			if common.HexToAddress(id.Address) != e.Deployment.Address {
				continue
			}
			if id != e.Identity || (e.Deployment.Role != role && !(role == "USDL" && e.Deployment.Role == "SidioraERC20")) {
				return nil, ErrQualification
			}
			matched = true
			coverage[e.Deployment.Address] = true
		}
		if !matched {
			if e.Deployment.Role == "SidioraPool" {
				beacon := owned.Manifest.Roles["PoolBeacon"]
				if e.Identity.Kind != "beacon_proxy" || common.HexToAddress(e.Identity.BeaconAddress) != common.HexToAddress(beacon.Address) || common.HexToAddress(e.Identity.ImplementationAddress) != common.HexToAddress(beacon.ImplementationAddress) || common.HexToHash(e.Identity.ImplementationHash) != common.HexToHash(beacon.ImplementationHash) {
					return nil, ErrQualification
				}
			} else if e.Deployment.Role != "SidioraERC20" || e.Identity.Kind != "direct" {
				return nil, ErrQualification
			}
		}
		deployments[i] = e.Deployment
	}
	for _, role := range []string{"EventEmitter", "Router", "SidioraFactory", "PoolRegistry", "ProtocolConfig", "USDL"} {
		if !coverage[common.HexToAddress(owned.Manifest.Roles[role].Address)] {
			return nil, ErrQualification
		}
	}
	registry, err := decode.NewRegistry(owned.CatalogJSON, deployments)
	if err != nil {
		return nil, err
	}
	var catalog decode.Catalog
	if err = json.Unmarshal(owned.CatalogJSON, &catalog); err != nil {
		return nil, err
	}
	for _, template := range owned.Discovery {
		matched := false
		for _, contract := range catalog.Contracts {
			if contract.Name == template.Role && contract.ABIRevision == template.ABIRevision {
				matched = true
			}
		}
		if !matched {
			return nil, ErrQualification
		}
	}
	a := &RPCQualification{client: client, options: owned, registry: registry, manifestHash: hash, privateFixture: private, reads: &rpcReadState{calls: make(chan struct{}, 16), decimals: map[string]*rpcDecimalRead{}}}
	read, cancel := context.WithTimeout(ctx, owned.Timeout)
	defer cancel()
	chain, err := client.ChainID(read)
	if err != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return nil, ErrQualification
	}
	if err = a.verifyChainIdentity(read, attest); err != nil {
		return nil, err
	}
	return a, nil
}
func validRPCIdentity(i deployment.Role) bool {
	if !common.IsHexAddress(i.Address) || common.HexToAddress(i.Address) == (common.Address{}) {
		return false
	}
	if i.Kind == "precompile" {
		return i.Address == deployment.LaunchpadPrecompileAddress && i.RuntimeHash == "" && i.ImplementationAddress == "" && i.ImplementationHash == "" && i.BeaconAddress == ""
	}
	if !rpcHash(i.RuntimeHash) {
		return false
	}
	switch i.Kind {
	case "direct", "surface":
		return i.ImplementationAddress == "" && i.ImplementationHash == "" && i.BeaconAddress == ""
	case "erc1967", "beacon", "beacon_proxy":
		if !common.IsHexAddress(i.ImplementationAddress) || common.HexToAddress(i.ImplementationAddress) == (common.Address{}) || !rpcHash(i.ImplementationHash) {
			return false
		}
		if i.Kind == "beacon_proxy" {
			return common.IsHexAddress(i.BeaconAddress) && common.HexToAddress(i.BeaconAddress) != (common.Address{})
		}
		return true
	}
	return false
}
func rpcHash(s string) bool {
	if len(s) != 66 || !strings.HasPrefix(s, "0x") {
		return false
	}
	b, err := hex.DecodeString(s[2:])
	return err == nil && len(b) == 32 && common.BytesToHash(b) != (common.Hash{})
}
func (a *RPCQualification) Registry() *decode.Registry { return a.registry }
func (a *RPCQualification) epoch(address common.Address, order decode.Order) (RPCCodeEpoch, error) {
	less := func(x, y decode.Order) bool {
		if x.Block != y.Block {
			return x.Block < y.Block
		}
		if x.Transaction != y.Transaction {
			return x.Transaction < y.Transaction
		}
		return x.Log < y.Log
	}
	for _, e := range a.options.Epochs {
		if e.Deployment.Address == address && !less(order, e.Deployment.From) && (e.Deployment.Until == nil || less(order, *e.Deployment.Until)) {
			return e, nil
		}
	}
	return RPCCodeEpoch{}, ErrQualification
}
func rpcWordAddress(b []byte) (common.Address, error) {
	if len(b) != 32 || !bytes.Equal(b[:12], make([]byte, 12)) {
		return common.Address{}, ErrQualification
	}
	return common.BytesToAddress(b[12:]), nil
}
func (a *RPCQualification) word(ctx context.Context, address common.Address, signature string, block common.Hash, args ...common.Address) ([]byte, error) {
	if address == (common.Address{}) || block == (common.Hash{}) || len(signature) > 128 || len(args) > 1 {
		return nil, ErrQualification
	}
	read, release, err := a.scalarRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	data := append([]byte(nil), crypto.Keccak256([]byte(signature))[:4]...)
	for _, v := range args {
		data = append(data, common.LeftPadBytes(v.Bytes(), 32)...)
	}
	out, err := a.client.CallContractAtHash(read, ethereum.CallMsg{To: &address, Data: data, Gas: rpcReadGas}, block)
	if err != nil {
		return nil, errors.Join(ErrQualification, err)
	}
	if len(out) != 32 {
		return nil, ErrQualification
	}
	return out, nil
}

func (a *RPCQualification) scalarRead(ctx context.Context) (context.Context, func(), error) {
	if a == nil || a.client == nil || a.reads == nil || a.reads.calls == nil {
		return nil, nil, ErrQualification
	}
	read, cancel := context.WithTimeout(ctx, scalarReadWindow)
	if err := read.Err(); err != nil {
		cancel()
		return nil, nil, err
	}
	select {
	case a.reads.calls <- struct{}{}:
	case <-read.Done():
		cancel()
		return nil, nil, read.Err()
	}
	if err := read.Err(); err != nil {
		<-a.reads.calls
		cancel()
		return nil, nil, err
	}
	return read, func() { <-a.reads.calls; cancel() }, nil
}

func (a *RPCQualification) codeAt(ctx context.Context, address common.Address, block common.Hash) ([]byte, error) {
	read, release, err := a.scalarRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return a.client.CodeAtHash(read, address, block)
}

func (a *RPCQualification) storageAt(ctx context.Context, address common.Address, slot, block common.Hash) ([]byte, error) {
	read, release, err := a.scalarRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return a.client.StorageAtHash(read, address, slot, block)
}

func (a *RPCQualification) decimals(ctx context.Context, address common.Address, epoch string, block common.Hash) ([]byte, error) {
	if a == nil || a.reads == nil || a.reads.decimals == nil || epoch == "" || len(epoch) > 256 || address == (common.Address{}) || block == (common.Hash{}) {
		return nil, ErrQualification
	}
	read, cancel := context.WithTimeout(ctx, scalarReadWindow)
	defer cancel()
	if err := read.Err(); err != nil {
		return nil, err
	}
	key := strings.ToLower(address.Hex()) + "/" + epoch
	a.reads.mu.Lock()
	entry, exists := a.reads.decimals[key]
	if !exists && len(a.reads.decimals) >= 4096 {
		a.reads.mu.Unlock()
		return nil, ErrQualification
	}
	if !exists {
		entry = &rpcDecimalRead{done: make(chan struct{})}
		a.reads.decimals[key] = entry
	}
	a.reads.mu.Unlock()
	if exists {
		select {
		case <-entry.done:
		case <-read.Done():
			return nil, read.Err()
		}
		if err := read.Err(); err != nil {
			return nil, err
		}
		if entry.err != nil {
			return nil, entry.err
		}
		return append([]byte(nil), entry.value[:]...), nil
	}
	out, err := a.word(read, address, "decimals()", block)
	if err == nil && new(big.Int).SetBytes(out).BitLen() > 8 {
		err = ErrQualification
	}
	a.reads.mu.Lock()
	entry.err = err
	if err == nil {
		copy(entry.value[:], out)
	} else {
		delete(a.reads.decimals, key)
	}
	close(entry.done)
	a.reads.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), entry.value[:]...), nil
}
func (a *RPCQualification) address(ctx context.Context, address common.Address, signature string, block common.Hash, args ...common.Address) (common.Address, error) {
	out, err := a.word(ctx, address, signature, block, args...)
	if err != nil {
		return common.Address{}, err
	}
	return rpcWordAddress(out)
}
func (a *RPCQualification) validateCode(ctx context.Context, e RPCCodeEpoch, block common.Hash) (string, error) {
	key, err := json.Marshal(struct {
		Epoch    RPCCodeEpoch
		Block    common.Hash
		Manifest string
		Masked   []CodeRange
	}{e, block, a.manifestHash, a.immutableRanges(e)})
	if err != nil {
		return "", err
	}
	if cached, ok := a.reads.code(string(key)); ok {
		return cached, nil
	}
	epoch, err := a.validateCodeUncached(ctx, e, block)
	if err == nil {
		a.reads.storeCode(string(key), epoch)
	}
	return epoch, err
}

func (a *RPCQualification) validateCodeUncached(ctx context.Context, e RPCCodeEpoch, block common.Hash) (string, error) {
	if e.Identity.Kind == "precompile" {
		// A precompile has no runtime code to hash: its identity is the
		// manifest-fixed address bound to the LaunchpadPrecompile role.
		if !validRPCIdentity(e.Identity) || e.Deployment.Role != deployment.LaunchpadPrecompileRole || e.Deployment.Address != common.HexToAddress(deployment.LaunchpadPrecompileAddress) || e.Deployment.Address != common.HexToAddress(e.Identity.Address) {
			return "", ErrQualification
		}
		return a.codeEpochDigest(e)
	}
	code, err := a.codeAt(ctx, e.Deployment.Address, block)
	if err != nil || len(code) == 0 || len(code) > 128<<10 || a.runtimeHash(e, code) != common.HexToHash(e.Identity.RuntimeHash) {
		return "", ErrQualification
	}
	var impl common.Address
	switch e.Identity.Kind {
	case "erc1967", "beacon_proxy":
		slot := deployment.ImplementationSlot
		if e.Identity.Kind == "beacon_proxy" {
			slot = deployment.BeaconSlot
		}
		raw, err := a.storageAt(ctx, e.Deployment.Address, slot, block)
		if err != nil {
			return "", err
		}
		impl, err = rpcWordAddress(raw)
		if err != nil {
			return "", err
		}
		if e.Identity.Kind == "beacon_proxy" {
			beacon := a.options.Manifest.Roles["PoolBeacon"]
			if impl != common.HexToAddress(beacon.Address) || impl != common.HexToAddress(e.Identity.BeaconAddress) {
				return "", ErrQualification
			}
			actual, err := a.codeAt(ctx, impl, block)
			if err != nil || len(actual) == 0 || len(actual) > 128<<10 || crypto.Keccak256Hash(actual) != common.HexToHash(beacon.RuntimeHash) {
				return "", ErrQualification
			}
			impl, err = a.address(ctx, impl, "implementation()", block)
			if err != nil {
				return "", err
			}
		}
	case "beacon":
		impl, err = a.address(ctx, e.Deployment.Address, "implementation()", block)
		if err != nil {
			return "", err
		}
	case "direct", "surface":
	default:
		return "", ErrQualification
	}
	if e.Identity.Kind != "direct" && e.Identity.Kind != "surface" {
		if impl != common.HexToAddress(e.Identity.ImplementationAddress) {
			return "", ErrQualification
		}
		actual, err := a.codeAt(ctx, impl, block)
		if err != nil || len(actual) == 0 || len(actual) > 128<<10 || crypto.Keccak256Hash(actual) != common.HexToHash(e.Identity.ImplementationHash) {
			return "", ErrQualification
		}
	}
	return a.codeEpochDigest(e)
}
func (a *RPCQualification) codeEpochDigest(e RPCCodeEpoch) (string, error) {
	raw, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return accounting.HashTuple("rpc-code-epoch-v1", a.manifestHash, string(raw)), nil
}
func (a *RPCQualification) pinnedBlock(ctx context.Context, hash common.Hash) (pinnedBlock, types.Receipts, error) {
	if cached, ok := a.reads.block(hash); ok && cached.block.Header.Number.Uint64() >= a.options.Manifest.ActivationHeight {
		return cached.block, cached.receipts, nil
	}
	block, receipts, err := a.pinnedBlockAt(ctx, hash, a.options.Manifest.ActivationHeight)
	if err == nil {
		a.reads.storeBlock(hash, cachedBlock{block, receipts})
	}
	return block, receipts, err
}
func (a *RPCQualification) pinnedBlockAt(ctx context.Context, hash common.Hash, activation uint64) (pinnedBlock, types.Receipts, error) {
	chain, err := a.client.ChainID(ctx)
	if err != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return pinnedBlock{}, nil, ErrQualification
	}
	block, err := a.blockByHash(ctx, hash)
	if err != nil {
		if IsPruned(err) {
			return pinnedBlock{}, nil, err
		}
		return pinnedBlock{}, nil, ErrQualification
	}
	header := block.Header
	if header.Number == nil || !header.Number.IsInt64() || header.Number.Sign() < 0 || header.Number.Uint64() < activation || len(block.Transactions) > 4096 || header.Time == 0 || header.Time > math.MaxInt64/1000 {
		return pinnedBlock{}, nil, ErrQualification
	}
	_, canonical, err := a.headerByNumber(ctx, header.Number)
	if err != nil || canonical != hash {
		return pinnedBlock{}, nil, ErrQualification
	}
	return a.pinnedReceipts(ctx, block)
}

func (a *RPCQualification) pinnedReceipts(ctx context.Context, block pinnedBlock) (pinnedBlock, types.Receipts, error) {
	header, hash := block.Header, block.Hash
	if !a.reportedIdentity() && types.DeriveSha(block.Transactions, trie.NewStackTrie(nil)) != header.TxHash {
		return pinnedBlock{}, nil, ErrQualification
	}
	receipts, err := a.client.BlockReceipts(ctx, rpc.BlockNumberOrHashWithHash(hash, true))
	if err != nil {
		var rpcErr rpc.Error
		if !errors.As(err, &rpcErr) || rpcErr.ErrorCode() != -32601 {
			return pinnedBlock{}, nil, err
		}
		receipts = make([]*types.Receipt, len(block.Transactions))
		for i, t := range block.Transactions {
			receipts[i], err = a.client.TransactionReceipt(ctx, t.Hash())
			if err != nil {
				return pinnedBlock{}, nil, err
			}
		}
	}
	if len(receipts) != len(block.Transactions) {
		return pinnedBlock{}, nil, ErrQualification
	}
	number := header.Number.Uint64()
	logIndex := uint(0)
	totalBytes := 0
	for i, r := range receipts {
		t := block.Transactions[i]
		if r == nil || r.Bloom != types.CreateBloom(r) || r.BlockNumber == nil || r.BlockNumber.Cmp(header.Number) != 0 || r.BlockHash != hash || r.TxHash != t.Hash() || r.TransactionIndex != uint(i) || r.Type != t.Type() || (r.Status != 0 && r.Status != 1) || len(r.Logs) > 4096 {
			return pinnedBlock{}, nil, ErrQualification
		}
		for _, l := range r.Logs {
			if l == nil || l.Removed || l.BlockHash != hash || l.BlockNumber != number || l.TxHash != t.Hash() || l.TxIndex != uint(i) || l.Index != logIndex || logIndex > math.MaxUint32 || len(l.Topics) > 4 || len(l.Data) > decode.MaxFieldBytes {
				return pinnedBlock{}, nil, ErrQualification
			}
			totalBytes += 256 + len(l.Data) + 32*len(l.Topics)
			if totalBytes > maxBundleBytes {
				return pinnedBlock{}, nil, ErrQualification
			}
			logIndex++
		}
	}
	if !a.reportedIdentity() && types.DeriveSha(types.Receipts(receipts), trie.NewStackTrie(nil)) != header.ReceiptHash {
		return pinnedBlock{}, nil, ErrQualification
	}
	if types.MergeBloom(types.Receipts(receipts)) != header.Bloom {
		return pinnedBlock{}, nil, ErrQualification
	}
	return block, types.Receipts(receipts), nil
}
func receiptEqual(a, b *types.Receipt) bool {
	if a == nil || b == nil {
		return false
	}
	x, xe := json.Marshal(a)
	y, ye := json.Marshal(b)
	return xe == nil && ye == nil && bytes.Equal(x, y)
}
func (a *RPCQualification) Bundle(ctx context.Context, hash common.Hash) (Bundle, error) {
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	r, err := a.client.TransactionReceipt(read, hash)
	if err != nil || r == nil {
		return Bundle{}, ErrQualification
	}
	block, receipts, err := a.pinnedBlock(read, r.BlockHash)
	if err != nil {
		return Bundle{}, err
	}
	if r.TransactionIndex >= uint(len(receipts)) || !receiptEqual(r, receipts[r.TransactionIndex]) || block.Transactions[r.TransactionIndex].Hash() != hash {
		return Bundle{}, ErrQualification
	}
	b := Bundle{Receipt: receipts[r.TransactionIndex], Transaction: block.Transactions[r.TransactionIndex], Header: block.Header, BlockHash: block.Hash, PrepayReceipts: []*types.Receipt{}}
	return clone(b)
}
func (a *RPCQualification) ValidateReceipt(ctx context.Context, b Bundle) error {
	if err := boundedBundle(b); err != nil {
		return err
	}
	if _, err := coordinates(b); err != nil {
		return err
	}
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	block, receipts, err := a.pinnedBlock(read, bundleBlockHash(b))
	if err != nil {
		return err
	}
	pinnedHeader, err := digest(block.Header)
	if err != nil {
		return err
	}
	claimedHeader, err := digest(b.Header)
	if err != nil || pinnedHeader != claimedHeader {
		return ErrQualification
	}
	index := b.Receipt.TransactionIndex
	if index >= uint(len(receipts)) || block.Transactions[index].Hash() != b.Transaction.Hash() || !receiptEqual(receipts[index], b.Receipt) {
		return ErrQualification
	}
	for _, r := range b.PrepayReceipts {
		if r == nil {
			return ErrQualification
		}
		_, prior, err := a.pinnedBlock(read, r.BlockHash)
		if err != nil {
			return err
		}
		if r.TransactionIndex >= uint(len(prior)) || !receiptEqual(r, prior[r.TransactionIndex]) {
			return ErrQualification
		}
	}
	for _, l := range b.Receipt.Logs {
		e, err := a.epoch(l.Address, decode.Order{Block: l.BlockNumber, Transaction: l.TxIndex, Log: l.Index})
		if err != nil {
			// A contract outside the qualified code epochs (for example the
			// protocol Treasury, which the frozen manifest does not name) may
			// log inside a qualified transaction. Its logs stay opaque: the
			// registry still rejects any approved signature from an
			// unauthorized emitter, so it cannot impersonate a qualified event.
			env, derr := a.registry.Decode(*l)
			if derr != nil {
				return derr
			}
			if env.Status != "opaque" || env.Event != nil {
				return ErrQualification
			}
			continue
		}
		if _, err = a.validateCode(read, e, block.Hash); err != nil {
			return err
		}
		if _, err = a.registry.Decode(*l); err != nil {
			return err
		}
	}
	return nil
}
func (a *RPCQualification) StableMetadata(ctx context.Context, pool common.Address, hash common.Hash) (StablePoolMetadata, error) {
	var out StablePoolMetadata
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	header, err := a.headerByHash(read, hash)
	if err != nil || header == nil || header.Number == nil || !header.Number.IsInt64() {
		return out, ErrQualification
	}
	order := decode.Order{Block: header.Number.Uint64(), Transaction: ^uint(0), Log: ^uint(0)}
	pe, err := a.epoch(pool, order)
	if err != nil || pe.Deployment.Role != "SidioraPool" {
		return out, ErrQualification
	}
	poolEpoch, err := a.validateCode(read, pe, hash)
	if err != nil {
		return out, err
	}
	token, err := a.address(read, pool, "tokenAddress()", hash)
	if err != nil {
		return out, err
	}
	settlement, err := a.address(read, pool, "usdlAddress()", hash)
	if err != nil || settlement != common.HexToAddress(a.options.Manifest.Roles["USDL"].Address) {
		return out, ErrQualification
	}
	emitter, err := a.address(read, pool, "eventEmitter()", hash)
	if err != nil || emitter != common.HexToAddress(a.options.Manifest.Roles["EventEmitter"].Address) {
		return out, ErrQualification
	}
	config, err := a.address(read, pool, "protocolConfig()", hash)
	if err != nil || config != common.HexToAddress(a.options.Manifest.Roles["ProtocolConfig"].Address) {
		return out, ErrQualification
	}
	emitterEpoch := ""
	revision := ""
	tokenEpoch := ""
	settlementEpoch := ""
	for _, address := range []common.Address{token, settlement, emitter, config, common.HexToAddress(a.options.Manifest.Roles["PoolRegistry"].Address)} {
		e, err := a.epoch(address, order)
		if err != nil {
			return out, err
		}
		ep, err := a.validateCode(read, e, hash)
		if err != nil {
			return out, err
		}
		if address == emitter {
			emitterEpoch = ep
			revision = e.Deployment.ABIRevision
		}
		if address == token {
			tokenEpoch = ep
		}
		if address == settlement {
			settlementEpoch = ep
		}
	}
	registry := common.HexToAddress(a.options.Manifest.Roles["PoolRegistry"].Address)
	registered, err := a.word(read, registry, "isRegisteredPool(address)", hash, pool)
	if err != nil || new(big.Int).SetBytes(registered).Cmp(big.NewInt(1)) != 0 {
		return out, ErrQualification
	}
	linked, err := a.address(read, registry, "getPoolByToken(address)", hash, token)
	if err != nil || linked != pool {
		return out, ErrQualification
	}
	td, err := a.decimals(read, token, tokenEpoch, hash)
	if err != nil || new(big.Int).SetBytes(td).BitLen() > 8 {
		return out, ErrQualification
	}
	sd, err := a.decimals(read, settlement, settlementEpoch, hash)
	if err != nil || new(big.Int).SetBytes(sd).Cmp(big.NewInt(6)) != 0 {
		return out, ErrQualification
	}
	return StablePoolMetadata{Pool: pool, Token: token, Settlement: settlement, TokenDecimals: uint8(new(big.Int).SetBytes(td).Uint64()), SettlementDecimals: 6, DeploymentID: a.options.Manifest.DeploymentVersion, ABIRevision: revision, EmitterCodeEpoch: emitterEpoch, PoolCodeEpoch: poolEpoch, BlockHash: hash}, nil
}
func (a *RPCQualification) SourceProvenance(ctx context.Context, b Bundle, address common.Address) (accounting.Provenance, error) {
	var out accounting.Provenance
	if err := a.ValidateReceipt(ctx, b); err != nil {
		return out, err
	}
	var found *types.Log
	for _, l := range b.Receipt.Logs {
		if l.Address == address {
			found = l
			break
		}
	}
	if found == nil {
		return out, ErrQualification
	}
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	e, err := a.epoch(address, decode.Order{Block: found.BlockNumber, Transaction: found.TxIndex, Log: found.Index})
	if err != nil {
		return out, err
	}
	ep, err := a.validateCode(read, e, bundleBlockHash(b))
	if err != nil {
		return out, err
	}
	rd, err := digest(b.Receipt)
	if err != nil {
		return out, err
	}
	hd, err := digest(b.Header)
	if err != nil {
		return out, err
	}
	out = accounting.Provenance{DeploymentID: a.options.Manifest.DeploymentVersion, ABIRevision: e.Deployment.ABIRevision, EmitterCodeEpoch: ep, ReceiptSHA256: hex.EncodeToString(rd[:]), HeaderSHA256: hex.EncodeToString(hd[:])}
	if e.Deployment.Role == "SidioraPool" {
		out.PoolCodeEpoch = ep
	}
	if e.Deployment.Role == "SidioraFactory" {
		pools := map[common.Address]bool{}
		for _, l := range b.Receipt.Logs {
			if l.Address != address {
				continue
			}
			env, err := a.registry.Decode(*l)
			if err != nil {
				return out, err
			}
			if env.Status != "decoded" || env.EventName != "MarketCreated" || env.Event == nil || env.Family == nil || env.Family.Authority != "native" {
				continue
			}
			value, ok := env.Event.Args["pool"].(string)
			if !ok || !common.IsHexAddress(value) {
				return out, ErrQualification
			}
			pool := common.HexToAddress(value)
			pools[pool] = true
			meta, err := a.StableMetadata(read, pool, bundleBlockHash(b))
			if err != nil {
				return out, err
			}
			out.PoolCodeEpoch = meta.PoolCodeEpoch
		}
		if len(pools) > 1 {
			return out, ErrQualification
		}
	}
	return out, nil
}
func (a *RPCQualification) ExecutionPolicy(ctx context.Context, b Bundle, index uint32) (accounting.ExecutionPolicy, error) {
	unavailable := accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}
	if err := a.ValidateReceipt(ctx, b); err != nil {
		return unavailable, err
	}
	var selected *decode.DecodedEnvelope
	fills := 0
	for _, l := range b.Receipt.Logs {
		env, err := a.registry.Decode(*l)
		if err != nil {
			return unavailable, err
		}
		if env.Family != nil && env.Family.EconomicFill {
			fills++
			if uint32(l.Index) == index {
				selected = env
			}
		}
	}
	if selected == nil {
		return unavailable, ErrQualification
	}
	if b.Receipt.TransactionIndex != 0 || fills != 1 || b.Header.Number.Sign() == 0 {
		return unavailable, nil
	}
	data := b.Transaction.Data()
	to := b.Transaction.To()
	if to == nil || *to != common.HexToAddress(a.options.Manifest.Roles["Router"].Address) || len(data) != 132 || (!bytes.Equal(data[:4], crypto.Keccak256([]byte("buy(address,uint256,uint256,uint256)"))[:4]) && !bytes.Equal(data[:4], crypto.Keccak256([]byte("sell(address,uint256,uint256,uint256)"))[:4])) {
		return unavailable, nil
	}
	poolID, ok := selected.Event.Args["poolId"].(string)
	if !ok || len(poolID) != 66 {
		return unavailable, ErrQualification
	}
	pool := common.HexToAddress(poolID)
	read, cancel := context.WithTimeout(ctx, a.options.Timeout)
	defer cancel()
	parentHash := b.Header.ParentHash
	parent, err := a.headerByHash(read, parentHash)
	if err != nil || parent == nil || parent.Number == nil || new(big.Int).Add(parent.Number, big.NewInt(1)).Cmp(b.Header.Number) != 0 {
		return unavailable, ErrQualification
	}
	if parent.Number.Uint64() < a.options.Manifest.ActivationHeight {
		return unavailable, nil
	}
	if _, err = a.StableMetadata(read, pool, parentHash); err != nil {
		return unavailable, err
	}
	optical, err := a.address(read, pool, "opticalAddress()", parentHash)
	if err != nil {
		return unavailable, err
	}
	if optical != (common.Address{}) {
		return unavailable, nil
	}
	config := common.HexToAddress(a.options.Manifest.Roles["ProtocolConfig"].Address)
	values := map[string]string{}
	for _, signature := range []string{"baseFeeBps()", "minFeeBps()", "maxFeeBps()", "feeDecayRate()", "volatilityWeight()", "concentrationWeight()"} {
		word, err := a.word(read, config, signature, parentHash)
		if err != nil {
			return unavailable, err
		}
		values[signature] = new(big.Int).SetBytes(word).String()
	}
	if values["feeDecayRate()"] != "0" || values["volatilityWeight()"] != "0" || values["concentrationWeight()"] != "0" {
		return unavailable, nil
	}
	fee, err := strconv.ParseUint(values["baseFeeBps()"], 10, 16)
	if err != nil {
		return unavailable, ErrQualification
	}
	minimum, err := strconv.ParseUint(values["minFeeBps()"], 10, 16)
	if err != nil {
		return unavailable, ErrQualification
	}
	maximum, err := strconv.ParseUint(values["maxFeeBps()"], 10, 16)
	if err != nil || minimum > maximum || maximum >= 10000 {
		return unavailable, ErrQualification
	}
	if fee > maximum {
		fee = maximum
	}
	if fee < minimum {
		fee = minimum
	}
	configHash, err := digest(values)
	if err != nil {
		return unavailable, err
	}
	preHash, err := digest(struct {
		Parent        common.Hash
		Pool          common.Address
		Optical       common.Address
		Configuration map[string]string
	}{parentHash, pool, optical, values})
	if err != nil {
		return unavailable, err
	}
	replayHash, err := digest(struct {
		Transaction common.Hash
		Header      common.Hash
		Index       uint32
		PreState    [32]byte
		Receipt     *types.Receipt
	}{b.Transaction.Hash(), bundleBlockHash(b), index, preHash, b.Receipt})
	if err != nil {
		return unavailable, err
	}
	configDigest := accounting.Digest(hex.EncodeToString(configHash[:]))
	preDigest := accounting.Digest(hex.EncodeToString(preHash[:]))
	replayDigest := accounting.Digest(hex.EncodeToString(replayHash[:]))
	zero := strings.ToLower(optical.Hex())
	bps := uint16(fee)
	return accounting.ExecutionPolicy{Quality: "qualified_execution", FeeBPS: &bps, ConfigDigest: &configDigest, PreTransactionStateDigest: &preDigest, ReplayEvidenceDigest: &replayDigest, OpticalImplementation: &zero, Evidence: []accounting.EvidenceRef{{Kind: "first-transaction-constant-fee-nohook", ArtifactSHA256: string(replayDigest), ArtifactID: strings.ToLower(b.Transaction.Hash().Hex())}}}, nil
}
func rawRecord(l *types.Log) store.RawRecord {
	topics := make([]string, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = strings.ToLower(t.Hex())
	}
	return store.RawRecord{TransactionHash: strings.ToLower(l.TxHash.Hex()), BlockHash: strings.ToLower(l.BlockHash.Hex()), Emitter: strings.ToLower(l.Address.Hex()), Height: int64(l.BlockNumber), TransactionIndex: uint32(l.TxIndex), LogIndex: uint32(l.Index), Topics: topics, Data: append([]byte{}, l.Data...)}
}
func (a *RPCQualification) discover(ctx context.Context, receipts types.Receipts) (*RPCQualification, error) {
	owned, err := clone(a.options)
	if err != nil {
		return nil, err
	}
	changed := false
	for _, r := range receipts {
		for _, l := range r.Logs {
			if l.Address != common.HexToAddress(owned.Manifest.Roles["SidioraFactory"].Address) {
				continue
			}
			env, err := a.registry.Decode(*l)
			if err != nil {
				return nil, err
			}
			if env.Status != "decoded" || env.EventName != "MarketCreated" || env.EmitterRole != "SidioraFactory" || env.Event == nil || env.Family == nil || env.Family.Authority != "native" {
				continue
			}
			factory, err := a.epoch(l.Address, decode.Order{Block: l.BlockNumber, Transaction: l.TxIndex, Log: l.Index})
			if err != nil {
				return nil, err
			}
			if _, err = a.validateCode(ctx, factory, l.BlockHash); err != nil {
				return nil, err
			}
			for _, item := range []struct{ Key, Role string }{{"token", "SidioraERC20"}, {"pool", "SidioraPool"}} {
				value, ok := env.Event.Args[item.Key].(string)
				if !ok || !common.IsHexAddress(value) {
					return nil, ErrQualification
				}
				address := common.HexToAddress(value)
				exists := false
				for _, e := range owned.Epochs {
					if e.Deployment.Address == address {
						exists = true
						break
					}
				}
				if exists {
					continue
				}
				e, err := a.discoveryEpoch(owned, address, item.Role, decode.Order{Block: l.BlockNumber, Transaction: l.TxIndex, Log: 0})
				if err != nil {
					return nil, err
				}
				if _, err = a.validateCode(ctx, e, l.BlockHash); err != nil {
					return nil, err
				}
				if item.Role == "SidioraERC20" {
					source, err := a.address(ctx, address, "deploymentFactory()", l.BlockHash)
					if err != nil || source != l.Address {
						return nil, ErrQualification
					}
				}
				if len(owned.Epochs) >= 4096 {
					return nil, ErrQualification
				}
				owned.Epochs = append(owned.Epochs, e)
				changed = true
			}
		}
	}
	if !changed {
		return a, nil
	}
	deployments := make([]decode.Deployment, len(owned.Epochs))
	for i, e := range owned.Epochs {
		deployments[i] = e.Deployment
	}
	registry, err := decode.NewRegistry(owned.CatalogJSON, deployments)
	if err != nil {
		return nil, err
	}
	return &RPCQualification{client: a.client, options: owned, registry: registry, manifestHash: a.manifestHash, privateFixture: a.privateFixture, reads: a.reads}, nil
}

type pinnedHeight struct {
	header   *types.Header
	block    pinnedBlock
	receipts types.Receipts
	// skipped: the block's bloom excluded every gated address when it was
	// pinned, so its receipts were not read.
	skipped bool
}

// pinRange pins the range's blocks in parallel (read-ahead pins first). Every
// read is independent and bound to the node-named block identity; the
// chain-order checks (parent linkage, discovery, decoding) run sequentially
// over the results. gate, when set, skips receipts of blocks it proves
// irrelevant.
func (a *RPCQualification) pinRange(ctx context.Context, from, to uint64, gate bloomGate) ([]pinnedHeight, error) {
	chain, err := a.client.ChainID(ctx)
	if err != nil || chain == nil || chain.Cmp(big.NewInt(125)) != 0 {
		return nil, ErrQualification
	}
	a.reads.dropAheadBelow(from)
	out := make([]pinnedHeight, to-from+1)
	errs := make([]error, len(out))
	slots := make(chan struct{}, a.reads.rangeConcurrency())
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if pinned, ok := a.reads.takeAhead(ctx, from+uint64(i)); ok {
				if !pinned.skipped {
					a.reads.storeBlock(pinned.block.Hash, cachedBlock{pinned.block, pinned.receipts})
				}
				out[i] = pinned
				return
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-slots }()
			read, cancel := context.WithTimeout(ctx, a.options.Timeout)
			defer cancel()
			block, receipts, skipped, err := a.pinnedBlockAtHeight(read, from+uint64(i), gate)
			if err != nil {
				errs[i] = err
				return
			}
			if !skipped {
				a.reads.storeBlock(block.Hash, cachedBlock{block, receipts})
			}
			out[i] = pinnedHeight{header: block.Header, block: block, receipts: receipts, skipped: skipped}
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (a *RPCQualification) sourceBlocks(ctx context.Context, from, to uint64) (store.CanonicalBatch, *RPCQualification, error) {
	a.sourceMu.Lock()
	defer a.sourceMu.Unlock()
	b := store.CanonicalBatch{ChainID: 125, PreviousHeight: int64(from) - 1, Blocks: []store.BlockCompleteness{}, Records: []store.RawRecord{}}
	if from < a.options.Manifest.ActivationHeight || to < from || to > math.MaxInt64 || to-from >= 64 {
		return b, nil, ErrBinding
	}
	authority := a
	if a.discovered != nil {
		authority = a.discovered
	}
	gate := authority.bloomGate()
	pinned, err := a.pinRange(ctx, from, to, gate)
	if err != nil {
		return b, nil, err
	}
	var prior common.Hash
	for height := from; height <= to; height++ {
		read, cancel := context.WithTimeout(ctx, a.options.Timeout)
		fetched := pinned[height-from]
		header, block, receipts := fetched.header, fetched.block, fetched.receipts
		if height > from && header.ParentHash != prior {
			cancel()
			return b, nil, ErrQualification
		}
		if fetched.skipped {
			// Pinned without receipts under the gate of its read; a market
			// discovered since may make the bloom relevant now.
			if gate.relevant(header.Bloom) {
				if block, receipts, err = a.pinnedReceipts(read, block); err != nil {
					cancel()
					return b, nil, err
				}
				a.reads.storeBlock(block.Hash, cachedBlock{block, receipts})
				a.reads.receiptReads.Add(1)
			} else {
				a.reads.bloomSkipped.Add(1)
			}
		} else {
			a.reads.receiptReads.Add(1)
		}
		previous := authority
		authority, err = authority.discover(read, receipts)
		if err != nil {
			cancel()
			return b, nil, err
		}
		if authority != previous {
			gate = authority.bloomGate()
		}
		count := 0
		for _, r := range receipts {
			for _, l := range r.Logs {
				e, err := authority.epoch(l.Address, decode.Order{Block: l.BlockNumber, Transaction: l.TxIndex, Log: l.Index})
				if err != nil {
					continue
				}
				if _, err = authority.validateCode(read, e, block.Hash); err != nil {
					cancel()
					return b, nil, err
				}
				if _, err = authority.registry.Decode(*l); err != nil {
					cancel()
					return b, nil, err
				}
				b.Records = append(b.Records, rawRecord(l))
				count++
				if len(b.Records) > 65536 {
					cancel()
					return b, nil, ErrQualification
				}
			}
		}
		b.Blocks = append(b.Blocks, store.BlockCompleteness{Height: int64(height), Hash: strings.ToLower(block.Hash.Hex()), RawCount: count, FetchComplete: true, RequiredDecodeComplete: true})
		prior = block.Hash
		cancel()
	}
	identity, err := digest(struct {
		Genesis  common.Hash
		Anchor   *BlockAnchor `json:",omitempty"`
		Manifest string
		Blocks   []store.BlockCompleteness
		Records  []store.RawRecord
	}{a.options.GenesisHash, a.options.Anchor, a.manifestHash, b.Blocks, b.Records})
	if err != nil {
		return b, nil, err
	}
	b.BatchID = "rpc-" + hex.EncodeToString(identity[:])
	a.discovered = authority
	return b, authority, nil
}
func (a *RPCQualification) issueBatch(ctx context.Context, b store.CanonicalBatch) ([]*QualifiedReceipt, error) {
	hashes := map[common.Hash]bool{}
	for _, r := range b.Records {
		if !rpcHash(r.TransactionHash) {
			return nil, ErrBinding
		}
		hashes[common.HexToHash(r.TransactionHash)] = true
	}
	keys := make([]common.Hash, 0, len(hashes))
	for h := range hashes {
		keys = append(keys, h)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
	if len(keys) > 4096 {
		return nil, ErrQualification
	}
	qualified := make([]*QualifiedReceipt, 0, len(keys))
	for _, h := range keys {
		bundle, err := a.Bundle(ctx, h)
		if err != nil {
			return nil, err
		}
		q, err := Qualify(ctx, a.registry, a, bundle)
		if err != nil {
			return nil, err
		}
		qualified = append(qualified, q)
	}
	if err := ValidateBindings(ctx, b, qualified); err != nil {
		return nil, err
	}
	return qualified, nil
}
func (a *RPCQualification) FetchQualifiedBatch(ctx context.Context, from, to uint64) (store.CanonicalBatch, []*QualifiedReceipt, error) {
	b, authority, err := a.sourceBlocks(ctx, from, to)
	if err != nil {
		return b, nil, err
	}
	receipts, err := authority.issueBatch(ctx, b)
	return b, receipts, err
}
func (a *RPCQualification) QualifiedBatch(ctx context.Context, b store.CanonicalBatch) ([]*QualifiedReceipt, error) {
	if b.ChainID != 125 || len(b.Records) > 65536 || len(b.Blocks) == 0 || len(b.Blocks) > 64 || b.Blocks[0].Height < 0 || b.Blocks[len(b.Blocks)-1].Height < 0 {
		return nil, ErrBinding
	}
	expected, authority, err := a.sourceBlocks(ctx, uint64(b.Blocks[0].Height), uint64(b.Blocks[len(b.Blocks)-1].Height))
	if err != nil {
		return nil, err
	}
	if len(expected.Blocks) != len(b.Blocks) || len(expected.Records) != len(b.Records) {
		return nil, ErrBinding
	}
	for i, block := range expected.Blocks {
		if block != b.Blocks[i] {
			return nil, ErrBinding
		}
	}
	byPosition := map[string]store.RawRecord{}
	for _, raw := range expected.Records {
		byPosition[raw.TransactionHash+":"+strconv.FormatUint(uint64(raw.LogIndex), 10)] = raw
	}
	for _, raw := range b.Records {
		key := raw.TransactionHash + ":" + strconv.FormatUint(uint64(raw.LogIndex), 10)
		want, ok := byPosition[key]
		if !ok {
			return nil, ErrBinding
		}
		x, err := digest(raw)
		if err != nil {
			return nil, err
		}
		y, err := digest(want)
		if err != nil || x != y {
			return nil, ErrBinding
		}
		delete(byPosition, key)
	}
	if len(byPosition) != 0 {
		return nil, ErrBinding
	}
	return authority.issueBatch(ctx, b)
}
