package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Network reads served by every environment from the same code:
//
//   - GET /v2/network/deployment (IF_DEPLOYMENT) from a reviewed deployment
//     manifest: in production the manifest of the bound network authority
//     document (NETWORK_AUTHORITY_CONFIG_FILE), the one GET
//     /v2/network/authority attests; outside production the embedded
//     shared/config/deployment current.json;
//   - GET /v2/transactions/{transactionHash} (IF_TX_STATUS) from
//     eth_getTransactionReceipt / eth_getTransactionByHash on the chain RPC
//     (production: the network authority's HTTPS NETWORK_RPC_URL client).
//
// Every success body is validated against shared/interfaces/v2 before it is
// written.

var transactionHashPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// networkReads holds the reviewed manifest and the chain client for
// IF_TX_STATUS; rpc is nil when no chain RPC is configured.
type networkReads struct {
	manifest deployment.Manifest
	rpc      *rpc.Client
	timeout  time.Duration
	logger   *slog.Logger
}

// dialNetworkRPC dials an http(s) chain RPC without credentials through the
// bounded transport.
func dialNetworkRPC(ctx context.Context, rpcURL string, allowPlainHTTP bool) (*rpc.Client, error) {
	u, err := url.Parse(rpcURL)
	if err != nil || (u.Scheme != "https" && !(allowPlainHTTP && u.Scheme == "http")) || u.Host == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("NETWORK_RPC_URL must be an http(s) URL without credentials")
	}
	client := &http.Client{Transport: boundedRPCTransport{base: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, ResponseHeaderTimeout: 3 * time.Second, MaxIdleConnsPerHost: 20}}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return rpc.DialOptions(ctx, rpcURL, rpc.WithHTTPClient(client))
}

// mountNetworkReads mounts IF_DEPLOYMENT and IF_TX_STATUS.
func (d *networkReads) mount(r chi.Router, admission *rest.Admission) {
	mount := func(method, path string, class rest.AdmissionClass, h http.HandlerFunc) {
		var handler http.Handler = h
		if admission != nil {
			handler = admission.Middleware(class)(handler)
		}
		r.Method(method, path, rest.ResponseMetadata(handler))
	}
	mount(http.MethodGet, "/v2/network/deployment", rest.AdmissionCached, d.deployment)
	mount(http.MethodGet, "/v2/transactions/{transactionHash}", rest.AdmissionAggregate, d.transaction)
}

// writeNetworkEnvelope writes the frozen v2 response envelope after
// validating it as document; a body that fails its contract is never sent.
func writeNetworkEnvelope(w http.ResponseWriter, r *http.Request, document string, data any, quality, cacheControl string) {
	id := w.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(id); err != nil {
		id = uuid.NewString()
		w.Header().Set("X-Request-ID", id)
	}
	body, err := json.Marshal(map[string]any{"requestId": id, "data": data, "revision": nil, "watermark": nil, "quality": quality, "nextCursor": nil, "generatedAt": time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
	if err == nil {
		err = interfaces.ValidateJSON(document, body)
	}
	if err != nil {
		slog.Default().Error("network read failed its contract", "document", document, "reason", err.Error())
		rest.WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cacheControl)
	_, _ = w.Write(append(body, '\n'))
}

// deploymentData maps the reviewed manifest onto DeploymentData. manifestHash
// is the manifest's sourceManifestSha256, the value the web pins as
// CORE_DEPLOYMENT.manifestSha256. Only creation and swap are capabilities of
// the deployed core contracts (on while the manifest is active); every
// optional surface is reported off because no service of this deployment
// provides it. Code identity is not attested here (GET /v2/network/authority
// does that), so the read is labelled partial.
func deploymentData(m deployment.Manifest) (map[string]any, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	addresses := map[string]string{}
	for key, role := range map[string]string{"factory": "SidioraFactory", "router": "Router", "quoter": "Quoter", "protocolConfig": "ProtocolConfig", "usdl": "USDL", "feeAccumulator": "FeeAccumulator", "nft": "SidioraNFT", "eventEmitter": "EventEmitter", "poolRegistry": "PoolRegistry"} {
		r, ok := m.Roles[role]
		if !ok {
			return nil, errors.New("manifest lacks role " + role)
		}
		addresses[key] = common.HexToAddress(r.Address).Hex()
	}
	active := m.Status == "active"
	return map[string]any{
		"chainId":           strconv.Itoa(m.ChainID),
		"nativeSymbol":      m.Native.Symbol,
		"contractAddresses": addresses,
		"manifestHash":      "0x" + m.SourceManifestSHA256,
		"capabilities": map[string]bool{
			"creation": active, "swap": active,
			"conditionalOrders": false, "rewards": false, "bounties": false, "nativeFunding": false, "embeddedWallet": false, "voice": false,
		},
	}, nil
}

func (d *networkReads) deployment(w http.ResponseWriter, r *http.Request) {
	data, err := deploymentData(d.manifest)
	if err != nil {
		d.logger.Error("deployment manifest invalid", "reason", err.Error())
		rest.WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	writeNetworkEnvelope(w, r, "DeploymentResponse", data, "partial", "public, max-age=60")
}

// rpcReceipt is the subset of eth_getTransactionReceipt the contract needs.
type rpcReceipt struct {
	TransactionHash   common.Hash     `json:"transactionHash"`
	TransactionIndex  hexutil.Uint64  `json:"transactionIndex"`
	BlockHash         common.Hash     `json:"blockHash"`
	BlockNumber       *hexutil.Big    `json:"blockNumber"`
	From              common.Address  `json:"from"`
	To                *common.Address `json:"to"`
	Status            *hexutil.Uint64 `json:"status"`
	GasUsed           *hexutil.Big    `json:"gasUsed"`
	EffectiveGasPrice *hexutil.Big    `json:"effectiveGasPrice"`
}

type rpcTransaction struct {
	Hash     common.Hash  `json:"hash"`
	GasPrice *hexutil.Big `json:"gasPrice"`
}

func decimal(v *hexutil.Big) string { return (*big.Int)(v).String() }

// transaction serves TxStatusData. A receipt yields included_success or
// included_reverted from its status field; a transaction the node knows
// without a receipt is submitted; anything else is unknown. Elapsed time never
// changes the status.
func (d *networkReads) transaction(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "transactionHash")
	if !transactionHashPattern.MatchString(raw) || len(r.URL.Query()) != 0 {
		rest.WriteV2APIError(w, r, 400, "QUERY_INVALID")
		return
	}
	if d.rpc == nil {
		rest.WriteV2NotConfiguredError(w, r)
		return
	}
	hash := common.HexToHash(raw)
	ctx, cancel := context.WithTimeout(r.Context(), d.timeout)
	defer cancel()
	var receipt *rpcReceipt
	if err := d.rpc.CallContext(ctx, &receipt, "eth_getTransactionReceipt", hash); err != nil {
		d.logger.Warn("transaction status unavailable", "reason", err.Error())
		rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	var tx *rpcTransaction
	if receipt == nil || receipt.EffectiveGasPrice == nil {
		if err := d.rpc.CallContext(ctx, &tx, "eth_getTransactionByHash", hash); err != nil {
			d.logger.Warn("transaction status unavailable", "reason", err.Error())
			rest.WriteV2APIError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		if tx != nil && tx.Hash != hash {
			rest.WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
			return
		}
	}
	if receipt == nil {
		status := "unknown"
		if tx != nil {
			status = "submitted"
		}
		writeNetworkEnvelope(w, r, "TxStatusResponse", map[string]any{"status": status, "receipt": nil}, "complete", "no-store")
		return
	}
	gasPrice := receipt.EffectiveGasPrice
	if gasPrice == nil && tx != nil {
		gasPrice = tx.GasPrice
	}
	if receipt.TransactionHash != hash || receipt.BlockNumber == nil || receipt.BlockHash == (common.Hash{}) || receipt.Status == nil || *receipt.Status > 1 || receipt.GasUsed == nil || gasPrice == nil {
		rest.WriteV2APIError(w, r, 503, "INTEGRITY_QUARANTINED")
		return
	}
	var to any
	if receipt.To != nil {
		to = receipt.To.Hex()
	}
	height := decimal(receipt.BlockNumber)
	success := *receipt.Status == 1
	status := "included_reverted"
	if success {
		status = "included_success"
	}
	writeNetworkEnvelope(w, r, "TxStatusResponse", map[string]any{"status": status, "receipt": map[string]any{
		"transactionHash":      hash.Hex(),
		"blockHash":            receipt.BlockHash.Hex(),
		"height":               height,
		"from":                 receipt.From.Hex(),
		"to":                   to,
		"success":              success,
		"gasUsedRaw":           decimal(receipt.GasUsed),
		"effectiveGasPriceRaw": decimal(gasPrice),
		"position": map[string]any{
			"chainId":          "125",
			"height":           height,
			"transactionIndex": strconv.FormatUint(uint64(receipt.TransactionIndex), 10),
			"logIndex":         "0",
			"transactionHash":  hash.Hex(),
			"blockHash":        receipt.BlockHash.Hex(),
		},
	}}, "complete", "no-store")
}
