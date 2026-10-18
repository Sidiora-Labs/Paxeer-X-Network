package capabilities

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type PinnedRPCReader struct {
	*ethclient.Client
	rpc *rpc.Client
}

func NewPinnedRPCReader(client *rpc.Client) (*PinnedRPCReader, error) {
	if client == nil {
		return nil, deployment.Issue{Code: "rpc_reader_missing"}
	}
	return &PinnedRPCReader{Client: ethclient.NewClient(client), rpc: client}, nil
}

// HeaderByReportedHash returns the header the node identifies by hash, verifying
// that the node reports the same hash back. It implements
// deployment.ReportedHeaderReader so attestation does not depend on the
// go-ethereum header hash matching the chain's consensus block hash.
func (r *PinnedRPCReader) HeaderByReportedHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	if r == nil || r.rpc == nil || hash == (common.Hash{}) {
		return nil, deployment.Issue{Code: "block_identity_unverified"}
	}
	var raw json.RawMessage
	if err := r.rpc.CallContext(ctx, &raw, "eth_getBlockByHash", hash, false); err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ethereum.NotFound
	}
	var head types.Header
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, err
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &reported); err != nil {
		return nil, err
	}
	if reported.Hash == nil || *reported.Hash != hash || head.Number == nil {
		return nil, deployment.Issue{Code: "block_identity_unverified"}
	}
	return &head, nil
}

// rawReportedBlock fetches one block by number or hash and decodes its header
// with the node-reported "hash" kept beside it (on chain 125 that is the
// CometBFT-derived block hash, not types.Header.Hash()).
func (r *PinnedRPCReader) rawReportedBlock(ctx context.Context, method string, id any, full bool) (json.RawMessage, *types.Header, common.Hash, error) {
	if r == nil || r.rpc == nil {
		return nil, nil, common.Hash{}, deployment.Issue{Code: "block_identity_unverified"}
	}
	var raw json.RawMessage
	if err := r.rpc.CallContext(ctx, &raw, method, id, full); err != nil {
		return nil, nil, common.Hash{}, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil, common.Hash{}, ethereum.NotFound
	}
	var head types.Header
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, nil, common.Hash{}, err
	}
	var reported struct {
		Hash *common.Hash `json:"hash"`
	}
	if err := json.Unmarshal(raw, &reported); err != nil {
		return nil, nil, common.Hash{}, err
	}
	if reported.Hash == nil || *reported.Hash == (common.Hash{}) || head.Number == nil || !head.Number.IsUint64() {
		return nil, nil, common.Hash{}, deployment.Issue{Code: "block_identity_unverified"}
	}
	return raw, &head, *reported.Hash, nil
}

// ReportedHeaderByNumber returns the header at number together with the block
// hash the node reports for that height. It is the canonical-identity check:
// a node-reported hash is compared, never go-ethereum's recomputed hash.
func (r *PinnedRPCReader) ReportedHeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, common.Hash, error) {
	if number == nil || number.Sign() < 0 {
		return nil, common.Hash{}, deployment.Issue{Code: "block_identity_unverified"}
	}
	_, head, hash, err := r.rawReportedBlock(ctx, "eth_getBlockByNumber", hexutil.EncodeBig(number), false)
	if err != nil {
		return nil, common.Hash{}, err
	}
	if head.Number.Cmp(number) != 0 {
		return nil, common.Hash{}, deployment.Issue{Code: "block_identity_unverified"}
	}
	return head, hash, nil
}

// ReportedBlockByHash returns the block (with transactions) the node
// identifies by hash, verifying the node reports the same hash back.
func (r *PinnedRPCReader) ReportedBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	if hash == (common.Hash{}) {
		return nil, deployment.Issue{Code: "block_identity_unverified"}
	}
	raw, head, reported, err := r.rawReportedBlock(ctx, "eth_getBlockByHash", hash, true)
	if err != nil {
		return nil, err
	}
	if reported != hash {
		return nil, deployment.Issue{Code: "block_identity_unverified"}
	}
	var body struct {
		Transactions []*types.Transaction `json:"transactions"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	for _, tx := range body.Transactions {
		if tx == nil {
			return nil, deployment.Issue{Code: "block_identity_unverified"}
		}
	}
	return types.NewBlockWithHeader(head).WithBody(types.Body{Transactions: body.Transactions}), nil
}

func (r *PinnedRPCReader) EstimateGasAtHash(ctx context.Context, msg ethereum.CallMsg, hash common.Hash) (uint64, error) {
	if r == nil || r.rpc == nil || msg.To == nil || hash == (common.Hash{}) {
		return 0, deployment.Issue{Code: "gas_context_invalid"}
	}
	args := map[string]any{"from": msg.From, "to": msg.To, "data": hexutil.Bytes(msg.Data)}
	if msg.Value != nil {
		args["value"] = (*hexutil.Big)(msg.Value)
	}
	if msg.GasPrice != nil {
		args["gasPrice"] = (*hexutil.Big)(msg.GasPrice)
	}
	var result hexutil.Uint64
	err := r.rpc.CallContext(ctx, &result, "eth_estimateGas", args, map[string]any{"blockHash": hash.Hex(), "requireCanonical": true})
	if err != nil || result == 0 {
		return 0, deployment.Issue{Code: "gas_estimate_unavailable"}
	}
	return uint64(result), nil
}

type GasReader interface {
	EstimateGasAtHash(context.Context, ethereum.CallMsg, common.Hash) (uint64, error)
	SuggestGasPrice(context.Context) (*big.Int, error)
}

type PriceProvenance struct {
	QuoteAsset    string    `json:"quoteAsset"`
	ProviderKind  string    `json:"providerKind"`
	ObservedAt    time.Time `json:"observedAt"`
	ConfidenceBPS uint16    `json:"confidenceBps"`
	DisplayOnly   bool      `json:"displayOnly"`
	Stale         bool      `json:"stale"`
	Value         string    `json:"value"`
}

func (p PriceProvenance) Validate(now time.Time, maxAge time.Duration) error {
	if p.QuoteAsset != "USD" || p.ProviderKind == "" || len(p.ProviderKind) > 96 || !p.DisplayOnly || p.ConfidenceBPS > 10000 || p.ObservedAt.IsZero() || p.ObservedAt.After(now) || maxAge <= 0 || maxAge > time.Hour {
		return Unavailable{"display_conversion", DisplayUnavailable, "price_provenance"}
	}
	if _, err := raw(p.Value); err != nil {
		return err
	}
	if p.Stale || now.Sub(p.ObservedAt) > maxAge {
		return Unavailable{"display_conversion", DisplayUnavailable, "display_stale"}
	}
	return nil
}

type CreationCost struct {
	ProtocolFeeAsset     string `json:"protocolFeeAsset"`
	ProtocolFeeRaw       string `json:"protocolFeeRaw"`
	ProtocolFeeDisplay   string `json:"protocolFeeDisplay"`
	GasAsset             string `json:"gasAsset"`
	EstimatedGasRaw      string `json:"estimatedGasRaw"`
	EstimatedGasUnits    uint64 `json:"estimatedGasUnits"`
	GasPriceRaw          string `json:"gasPriceRaw"`
	BlockHash            string `json:"blockHash"`
	ConfigRevision       string `json:"configRevision"`
	Spender              string `json:"spender"`
	GasEstimateAvailable bool   `json:"gasEstimateAvailable"`
}

type PoolState struct {
	Address                string `json:"address"`
	VirtualUSDLReserve     string `json:"virtualUsdlReserve"`
	RealUSDLBalance        string `json:"realUsdlBalance"`
	RedeemableLiquidityRaw string `json:"redeemableLiquidityRaw"`
	TokenReserve           string `json:"tokenReserve"`
	Paused                 bool   `json:"paused"`
	Optical                string `json:"optical"`
	Guardian               string `json:"guardian"`
	Lifecycle              string `json:"lifecycle"`
	MarketFamily           string `json:"marketFamily"`
	ImplementationVersion  string `json:"implementationVersion"`
}

type ContractQuote struct {
	AmountOut      string `json:"amountOut"`
	FeeAmount      string `json:"feeAmount"`
	PriceImpactBPS string `json:"priceImpactBps"`
}

type PreflightRequest struct {
	Family           Family         `json:"family"`
	ExpectedRevision string         `json:"expectedRevision"`
	BlockHash        common.Hash    `json:"blockHash"`
	Trader           common.Address `json:"trader"`
	Recipient        common.Address `json:"recipient"`
	Pool             common.Address `json:"pool"`
	AmountIn         string         `json:"amountIn"`
	IsBuy            bool           `json:"isBuy"`
	CreationPath     string         `json:"creationPath"`
	Name             string         `json:"name"`
	Symbol           string         `json:"symbol"`
	FeeStrategy      uint8          `json:"feeStrategy"`
	Optical          common.Address `json:"optical"`
	NFTID            string         `json:"nftId"`
}

type Preflight struct {
	SchemaVersion          int                 `json:"schemaVersion"`
	Revision               deployment.Revision `json:"revision"`
	Family                 Family              `json:"family"`
	BlockHash              string              `json:"blockHash"`
	BlockNumber            uint64              `json:"blockNumber"`
	Trader                 string              `json:"trader"`
	Recipient              string              `json:"recipient"`
	Supported              bool                `json:"supported"`
	ExecutionPermitted     bool                `json:"executionPermitted"`
	OracleRequired         bool                `json:"oracleRequired"`
	ReasonCode             ReasonCode          `json:"reasonCode,omitempty"`
	Issues                 []deployment.Issue  `json:"issues,omitempty"`
	Capability             FamilyCapability    `json:"capability"`
	CreationCost           *CreationCost       `json:"creationCost,omitempty"`
	Pool                   *PoolState          `json:"pool,omitempty"`
	Quote                  *ContractQuote      `json:"quote,omitempty"`
	SimulationMode         string              `json:"simulationMode"`
	RequestSHA256          string              `json:"requestSha256"`
	ObservedRevisionSHA256 string              `json:"observedRevisionSha256"`
}

type PreflightEngine struct {
	reader      deployment.Reader
	manifest    deployment.Manifest
	poolRuntime common.Hash
	revision    deployment.Revision
	maxAge      time.Duration
	timeout     time.Duration
}

type PublishedFamily struct {
	Capability         FamilyCapability  `json:"capability"`
	RequiredAddresses  map[string]string `json:"requiredAddresses"`
	MinimumAttestation string            `json:"minimumAttestation"`
	ReleaseBlocked     bool              `json:"releaseBlocked"`
}

type CapabilityPublication struct {
	ObservedRevisionSHA256 string                         `json:"observedRevisionSha256"`
	SchemaVersion          int                            `json:"schemaVersion"`
	Revision               deployment.Revision            `json:"revision"`
	BlockHash              string                         `json:"blockHash"`
	BlockNumber            uint64                         `json:"blockNumber"`
	Families               []PublishedFamily              `json:"families"`
	ObservedIdentities     map[string]deployment.Identity `json:"observedIdentities"`
	Issues                 []deployment.Issue             `json:"issues,omitempty"`
}

func NewPreflightEngine(reader deployment.Reader, m deployment.Manifest, reviewedPoolRuntime, serialInterfaceSHA256 string, maxAge, timeout time.Duration) (*PreflightEngine, error) {
	if reader == nil || maxAge <= 0 || maxAge > time.Hour || timeout <= 0 || timeout > time.Minute || len(reviewedPoolRuntime) != 66 || !strings.HasPrefix(reviewedPoolRuntime, "0x") {
		return nil, deployment.Issue{Code: "preflight_configuration_invalid"}
	}
	runtimeBytes, err := hexutil.Decode(reviewedPoolRuntime)
	if err != nil || len(runtimeBytes) != 32 || common.BytesToHash(runtimeBytes) == (common.Hash{}) {
		return nil, deployment.Issue{Code: "pool_runtime_identity_missing"}
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	m, err = deployment.Parse(encoded)
	if err != nil {
		return nil, err
	}
	catalog, _ := json.Marshal(struct {
		Catalog     Catalog `json:"catalog"`
		PoolRuntime string  `json:"poolRuntime"`
	}{Frozen(), common.BytesToHash(runtimeBytes).Hex()})
	support, _ := json.Marshal(ContractSupport())
	capHash, supportHash := sha256.Sum256(catalog), sha256.Sum256(support)
	revision, err := deployment.NewRevision(m, hex.EncodeToString(capHash[:]), hex.EncodeToString(supportHash[:]), serialInterfaceSHA256)
	if err != nil {
		return nil, err
	}
	return &PreflightEngine{reader: reader, manifest: m, poolRuntime: common.BytesToHash(runtimeBytes), revision: revision, maxAge: maxAge, timeout: timeout}, nil
}

func (e *PreflightEngine) Revision() deployment.Revision { return e.revision }

func (e *PreflightEngine) Capabilities(ctx context.Context, block common.Hash) (CapabilityPublication, error) {
	if e == nil {
		return CapabilityPublication{}, deployment.Issue{Code: "preflight_engine_missing"}
	}
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: e.maxAge, Timeout: e.timeout}
	attestation, err := deployment.Attest(ctx, e.reader, e.manifest, block, policy)
	if err != nil {
		return CapabilityPublication{}, err
	}
	observedRevision, err := deployment.ObservationRevision(e.revision, attestation)
	if err != nil {
		return CapabilityPublication{}, err
	}
	publication := CapabilityPublication{ObservedRevisionSHA256: observedRevision, SchemaVersion: 2, Revision: e.revision, BlockHash: attestation.BlockHash, BlockNumber: attestation.BlockNumber, Families: []PublishedFamily{}, ObservedIdentities: attestation.Identities, Issues: attestation.Issues}
	for _, family := range []Family{Creation, Trading, PlainQuote, Opticals, NFTFees, MetaAG, PECOROrders, Vault, DisplayConversion} {
		capability, err := AssessFamily(family, e.manifest, attestation, policy)
		if err != nil {
			return CapabilityPublication{}, err
		}
		addresses := map[string]string{}
		for _, role := range familyRoles[family] {
			if value, ok := e.manifest.Roles[role]; ok {
				addresses[role] = value.Address
			}
		}
		publication.Families = append(publication.Families, PublishedFamily{Capability: capability, RequiredAddresses: addresses, MinimumAttestation: "fresh_committed_block_identity_and_governance", ReleaseBlocked: !capability.ReleaseEnabled})
	}
	return publication, nil
}

func (e *PreflightEngine) Handler(family Family) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			_ = json.NewEncoder(w).Encode(deployment.Issue{Code: "method_not_allowed"})
			return
		}
		body, bodyErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var request PreflightRequest
		if bodyErr != nil || !uniquePreflightRequest(body) || decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.Family != family {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(deployment.Issue{Code: "preflight_request_invalid"})
			return
		}
		result, err := e.Outcome(r.Context(), request)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(deployment.Issue{Code: "preflight_unavailable"})
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})
}

func (e *PreflightEngine) SerialExport(ctx context.Context, block common.Hash, endpoints deployment.PublicEndpoints, goAddresses, frontendAddresses map[string]string) (deployment.SerialExport, error) {
	if e == nil {
		return deployment.SerialExport{}, deployment.Issue{Code: "preflight_engine_missing"}
	}
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: e.maxAge, Timeout: e.timeout}
	attestation, err := deployment.Attest(ctx, e.reader, e.manifest, block, policy)
	if err != nil {
		return deployment.SerialExport{}, err
	}
	return deployment.ExportRevision(e.manifest, attestation, policy, endpoints, e.revision, goAddresses, frontendAddresses)
}

func (e *PreflightEngine) Preflight(ctx context.Context, request PreflightRequest) (Preflight, error) {
	if e == nil {
		return Preflight{}, deployment.Issue{Code: "preflight_engine_missing"}
	}
	result := Preflight{SchemaVersion: 2, Revision: e.revision, Family: request.Family, BlockHash: request.BlockHash.Hex(), Trader: request.Trader.Hex(), Recipient: request.Recipient.Hex(), SimulationMode: "none"}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	requestHash := sha256.Sum256(requestBytes)
	result.RequestSHA256 = hex.EncodeToString(requestHash[:])
	if request.ExpectedRevision != e.revision.RevisionSHA256 {
		result.ReasonCode = RevisionMismatch
		return result, nil
	}
	if request.Trader == (common.Address{}) || request.Recipient == (common.Address{}) || request.BlockHash == (common.Hash{}) {
		return result, refuse("preflight_context_invalid", "")
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: e.maxAge, Timeout: e.timeout}
	attestation, err := deployment.Attest(ctx, e.reader, e.manifest, request.BlockHash, policy)
	if err != nil {
		return result, err
	}
	result.ObservedRevisionSHA256, err = deployment.ObservationRevision(e.revision, attestation)
	if err != nil {
		return result, err
	}
	result.BlockNumber = attestation.BlockNumber
	result.Capability, err = AssessFamily(request.Family, e.manifest, attestation, policy)
	if err != nil {
		return result, err
	}
	if !result.Capability.Supported {
		result.ReasonCode = result.Capability.ReasonCode
		result.Issues = attestation.Issues
		return result, nil
	}
	switch request.Family {
	case Creation:
		cost, err := e.creationCost(ctx, request, attestation)
		cost.ConfigRevision = result.ObservedRevisionSHA256
		result.CreationCost = &cost
		if err != nil {
			var unavailable Unavailable
			if errors.As(err, &unavailable) {
				result.ReasonCode = unavailable.ReasonCode
				return result, nil
			}
			return result, err
		}
	case PlainQuote, Trading:
		reason, err := e.registeredPool(ctx, attestation, policy, request.Pool, request.BlockHash)
		if err != nil {
			return result, err
		}
		if reason != "" {
			result.ReasonCode = reason
			return result, nil
		}
		state, err := e.poolState(ctx, request.Pool, request.BlockHash)
		if err != nil {
			return result, err
		}
		result.Pool = &state
		encodedState, err := json.Marshal(struct {
			Revision string    `json:"revision"`
			Pool     PoolState `json:"pool"`
		}{result.ObservedRevisionSHA256, state})
		if err != nil {
			return result, err
		}
		stateHash := sha256.Sum256(encodedState)
		result.ObservedRevisionSHA256 = hex.EncodeToString(stateHash[:])
		if common.HexToAddress(state.Optical) != (common.Address{}) {
			result.ReasonCode = OpticalQuoteUnsupported
			return result, nil
		}
		if state.Paused {
			result.ReasonCode = UnexecutableQuote
			return result, nil
		}
		if err := attestation.ValidateFamilyFor(e.manifest, policy, familyRoles[PlainQuote], false); err != nil {
			result.ReasonCode = IdentityUnverified
			return result, nil
		}
		quote, err := ReadPinnedQuote(ctx, e.reader, common.HexToAddress(e.manifest.Roles["Quoter"].Address), request.Pool, request.Trader, request.AmountIn, request.IsBuy, request.BlockHash)
		if err != nil {
			var unavailable Unavailable
			if errors.As(err, &unavailable) {
				result.ReasonCode = unavailable.ReasonCode
				return result, nil
			}
			return result, err
		}
		if !request.IsBuy {
			if err := ValidateSellLiquidity(state.RealUSDLBalance, state.VirtualUSDLReserve, quote.AmountOut); err != nil {
				result.ReasonCode = UnexecutableQuote
				return result, nil
			}
		}
		result.Quote = &quote
		result.SimulationMode = "contract_quoter"
	case NFTFees:
		id, err := raw(request.NFTID)
		if err != nil {
			return result, refuse("nft_id_invalid", "SidioraNFT")
		}
		// ownerOf reverts for a token that does not exist: that, like another
		// owner, is a deterministic refusal; only a failed read is retryable.
		owner, ok, err := e.read(ctx, common.HexToAddress(e.manifest.Roles["SidioraNFT"].Address), "ownerOf(uint256)", request.BlockHash, id)
		if err != nil {
			return result, err
		}
		if !ok || len(owner) != 32 || !bytes.Equal(owner[:12], make([]byte, 12)) || common.BytesToAddress(owner[12:]) != request.Trader {
			return result, refuse("claim_owner_mismatch", "SidioraNFT")
		}
	default:
		return result, Unavailable{"core_market", UnknownCapability, string(request.Family)}
	}
	result.Supported = true
	result.ExecutionPermitted = result.Capability.ReleaseEnabled
	if !result.ExecutionPermitted {
		result.ReasonCode = QualificationPending
	}
	return result, nil
}

func (e *PreflightEngine) creationCost(ctx context.Context, request PreflightRequest, a deployment.ActivationAttestation) (CreationCost, error) {
	role := request.CreationPath
	if role != "Router" && role != "SidioraFactory" {
		return CreationCost{}, refuse("creation_path_invalid", "")
	}
	if len(request.Name) == 0 || len(request.Name) > 128 || len(request.Symbol) == 0 || len(request.Symbol) > 32 || request.FeeStrategy > 3 {
		return CreationCost{}, refuse("creation_metadata_invalid", "")
	}
	if request.Optical != (common.Address{}) {
		return CreationCost{}, Unavailable{"core_market", OpticalQuoteUnsupported, "optical_creation_preflight"}
	}
	if a.CreationFeeRaw != deployment.CreationFeeRaw {
		return CreationCost{}, refuse("creation_policy_mismatch", "ProtocolConfig")
	}
	to := common.HexToAddress(e.manifest.Roles[role].Address)
	cost := CreationCost{ProtocolFeeAsset: "USDL", ProtocolFeeRaw: a.CreationFeeRaw, ProtocolFeeDisplay: "1.00 USDL", GasAsset: "PAX", BlockHash: a.BlockHash, ConfigRevision: e.revision.RevisionSHA256, Spender: to.Hex()}
	gas, ok := e.reader.(GasReader)
	if !ok {
		return cost, Unavailable{"core_market", ReasonCode("gas_estimate_unavailable"), "pinned_gas_provider"}
	}
	data, err := encodeCall("createMarket(string,string,uint8,address)", request.Name, request.Symbol, request.FeeStrategy, request.Optical)
	if err != nil {
		var issue deployment.Issue
		if errors.As(err, &issue) && issue.Code == "call_arguments_invalid" {
			return CreationCost{}, refuse("creation_metadata_invalid", "")
		}
		return CreationCost{}, err
	}
	price, err := gas.SuggestGasPrice(ctx)
	if err != nil || price == nil || price.Sign() < 0 || price.BitLen() > 256 {
		return cost, Unavailable{"core_market", ReasonCode("gas_estimate_unavailable"), "gas_price"}
	}
	units, err := gas.EstimateGasAtHash(ctx, ethereum.CallMsg{From: request.Trader, To: &to, Data: data, Value: big.NewInt(0), GasPrice: price}, request.BlockHash)
	if err != nil || units == 0 {
		return cost, Unavailable{"core_market", ReasonCode("gas_estimate_unavailable"), "transaction_simulation"}
	}
	fee := new(big.Int).Mul(new(big.Int).SetUint64(units), price)
	if fee.BitLen() > 256 {
		return CreationCost{}, refuse("gas_cost_overflow", "")
	}
	cost.EstimatedGasRaw = fee.String()
	cost.EstimatedGasUnits = units
	cost.GasPriceRaw = price.String()
	cost.GasEstimateAvailable = true
	return cost, nil
}

func encodeCall(signature string, args ...any) ([]byte, error) {
	start := strings.IndexByte(signature, '(')
	if start < 1 || !strings.HasSuffix(signature, ")") {
		return nil, deployment.Issue{Code: "call_signature_invalid"}
	}
	parts := strings.Split(signature[start+1:len(signature)-1], ",")
	var arguments abi.Arguments
	if parts[0] != "" {
		for _, part := range parts {
			typ, err := abi.NewType(part, "", nil)
			if err != nil {
				return nil, err
			}
			arguments = append(arguments, abi.Argument{Type: typ})
		}
	}
	encoded, err := arguments.Pack(args...)
	if err != nil {
		return nil, deployment.Issue{Code: "call_arguments_invalid"}
	}
	return append(crypto.Keccak256([]byte(signature))[:4], encoded...), nil
}

func (e *PreflightEngine) call(ctx context.Context, address common.Address, signature string, block common.Hash, args ...any) ([]byte, error) {
	data, err := encodeCall(signature, args...)
	if err != nil {
		return nil, err
	}
	output, err := e.reader.CallContractAtHash(ctx, ethereum.CallMsg{To: &address, Data: data}, block)
	if err != nil || len(output) > 4096 {
		return nil, deployment.Issue{Code: "preflight_read_failed"}
	}
	return output, nil
}

// read is call with the failure classes kept apart. ok is false when the
// contract reverted (a deterministic answer at the pinned block); err is set
// only when the RPC read itself failed or returned an oversized answer, which
// is the retryable case.
func (e *PreflightEngine) read(ctx context.Context, address common.Address, signature string, block common.Hash, args ...any) ([]byte, bool, error) {
	data, err := encodeCall(signature, args...)
	if err != nil {
		return nil, false, err
	}
	output, err := e.reader.CallContractAtHash(ctx, ethereum.CallMsg{To: &address, Data: data}, block)
	if err != nil {
		if reverted(err) {
			return nil, false, nil
		}
		return nil, false, deployment.Issue{Code: "preflight_read_failed"}
	}
	if len(output) > 4096 {
		return nil, false, deployment.Issue{Code: "preflight_read_failed"}
	}
	return output, true, nil
}

// reverted reports an eth_call the node executed and the EVM reverted (JSON-RPC
// error code 3, or a node that only says "execution reverted"). Transport
// failures, timeouts and other JSON-RPC errors are not reverts.
func reverted(err error) bool {
	var rpcErr rpc.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	return rpcErr.ErrorCode() == 3 || strings.Contains(strings.ToLower(rpcErr.Error()), "execution reverted")
}

// Refusal is a deterministic preflight outcome: the request or the contract
// state at the pinned block decides it, so retrying cannot change it. It
// unwraps to the deployment.Issue it carries.
type Refusal struct {
	Issue deployment.Issue
}

func (r Refusal) Error() string { return r.Issue.Error() }
func (r Refusal) Unwrap() error { return r.Issue }

func refuse(code, role string) error {
	return Refusal{deployment.Issue{Code: code, Role: role}}
}

// Refused reports whether err is a deterministic outcome (a Refusal or a
// typed Unavailable) and the stable reason code it carries. Every other
// engine error is a dependency failure and stays retryable.
func Refused(err error) (ReasonCode, *deployment.Issue, bool) {
	var refusal Refusal
	if errors.As(err, &refusal) {
		issue := refusal.Issue
		return ReasonCode(issue.Code), &issue, true
	}
	var unavailable Unavailable
	if errors.As(err, &unavailable) {
		return unavailable.ReasonCode, nil, true
	}
	return "", nil, false
}

// Outcome is Preflight as the HTTP surface reports it: a deterministic
// refusal becomes a blocked result (supported and executionPermitted false,
// the refusal's code as reasonCode, no quote or cost disclosure), while a
// dependency failure is returned as the error.
func (e *PreflightEngine) Outcome(ctx context.Context, request PreflightRequest) (Preflight, error) {
	result, err := e.Preflight(ctx, request)
	if err == nil {
		return result, nil
	}
	reason, issue, ok := Refused(err)
	if !ok || e == nil || result.SchemaVersion == 0 {
		return result, err
	}
	if result.Capability.Family == "" {
		roles := familyRoles[request.Family]
		result.Capability = FamilyCapability{CapabilityID: "core_market", Family: request.Family, DeploymentVersion: e.manifest.DeploymentVersion, ReasonCode: reason, DependencyIDs: append([]string{}, roles...), EventCoverage: []string{}}
	}
	result.Supported = false
	result.ExecutionPermitted = false
	result.ReasonCode = reason
	result.SimulationMode = "none"
	result.Quote = nil
	result.CreationCost = nil
	if issue != nil {
		result.Issues = append(append([]deployment.Issue{}, result.Issues...), *issue)
	}
	return result, nil
}

// registeredPool separates a pool that does not exist from a read that
// failed. The zero address and any address the PoolRegistry reports as
// unregistered at the pinned block yield PoolUnknown, a deterministic blocked
// outcome. The registry's answer is only trusted while its identity is
// verified; otherwise the outcome is IdentityUnverified. A failed or malformed
// registry read is returned as an error, which the handler reports as the
// retryable preflight_unavailable.
func (e *PreflightEngine) registeredPool(ctx context.Context, a deployment.ActivationAttestation, p deployment.Policy, pool common.Address, block common.Hash) (ReasonCode, error) {
	if pool == (common.Address{}) {
		return PoolUnknown, nil
	}
	if err := a.ValidateFamilyFor(e.manifest, p, []string{"PoolRegistry"}, false); err != nil {
		return IdentityUnverified, nil
	}
	registered, err := e.call(ctx, common.HexToAddress(e.manifest.Roles["PoolRegistry"].Address), "isRegisteredPool(address)", block, pool)
	if err != nil {
		return "", deployment.Issue{Code: "pool_registry_read_failed", Role: "PoolRegistry"}
	}
	if len(registered) != 32 || new(big.Int).SetBytes(registered).Cmp(big.NewInt(1)) > 0 {
		return "", deployment.Issue{Code: "pool_registry_read_malformed", Role: "PoolRegistry"}
	}
	if registered[31] == 0 {
		return PoolUnknown, nil
	}
	return "", nil
}

func (e *PreflightEngine) poolState(ctx context.Context, pool common.Address, block common.Hash) (PoolState, error) {
	// A registered address whose code, beacon or wiring differs from the
	// reviewed pool is a deterministic refusal; a failed read is an error.
	bad := refuse("identity_unverified", "Pool")
	failed := deployment.Issue{Code: "pool_read_failed", Role: "Pool"}
	if pool == (common.Address{}) {
		return PoolState{}, bad
	}
	code, err := e.reader.CodeAtHash(ctx, pool, block)
	if err != nil {
		return PoolState{}, failed
	}
	if len(code) == 0 || len(code) > 128<<10 || crypto.Keccak256Hash(code) != e.poolRuntime {
		return PoolState{}, bad
	}
	slot, err := e.reader.StorageAtHash(ctx, pool, deployment.BeaconSlot, block)
	if err != nil {
		return PoolState{}, failed
	}
	if len(slot) != 32 || !bytes.Equal(slot[:12], make([]byte, 12)) || common.BytesToAddress(slot[12:]) != common.HexToAddress(e.manifest.Roles["PoolBeacon"].Address) {
		return PoolState{}, bad
	}
	word := func(signature string) ([]byte, error) {
		value, ok, err := e.read(ctx, pool, signature, block)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return value, nil
	}
	for signature, role := range map[string]string{"protocolConfig()": "ProtocolConfig", "usdlAddress()": "USDL", "eventEmitter()": "EventEmitter", "factory()": "SidioraFactory"} {
		value, err := word(signature)
		if err != nil {
			return PoolState{}, err
		}
		if len(value) != 32 || !bytes.Equal(value[:12], make([]byte, 12)) || common.BytesToAddress(value[12:]) != common.HexToAddress(e.manifest.Roles[role].Address) {
			return PoolState{}, bad
		}
	}
	reserves, err := word("getReserves()")
	if err != nil {
		return PoolState{}, err
	}
	if len(reserves) != 96 {
		return PoolState{}, refuse("pool_reserves_invalid", "Pool")
	}
	optical, err := word("opticalAddress()")
	if err != nil {
		return PoolState{}, err
	}
	if len(optical) != 32 || !bytes.Equal(optical[:12], make([]byte, 12)) {
		return PoolState{}, bad
	}
	guardian, err := word("guardian()")
	if err != nil {
		return PoolState{}, err
	}
	if len(guardian) != 32 || !bytes.Equal(guardian[:12], make([]byte, 12)) {
		return PoolState{}, bad
	}
	paused, err := word("paused()")
	if err != nil {
		return PoolState{}, err
	}
	if len(paused) != 32 || new(big.Int).SetBytes(paused).Cmp(big.NewInt(1)) > 0 {
		return PoolState{}, bad
	}
	state := PoolState{Address: pool.Hex(), VirtualUSDLReserve: new(big.Int).SetBytes(reserves[:32]).String(), RealUSDLBalance: new(big.Int).SetBytes(reserves[32:64]).String(), TokenReserve: new(big.Int).SetBytes(reserves[64:]).String(), Paused: paused[31] == 1, Optical: common.BytesToAddress(optical[12:]).Hex(), Guardian: common.BytesToAddress(guardian[12:]).Hex(), Lifecycle: "active", MarketFamily: "AMM", ImplementationVersion: e.manifest.Roles["PoolBeacon"].ImplementationHash}
	state.RedeemableLiquidityRaw = state.RealUSDLBalance
	if state.Paused {
		state.Lifecycle = "paused"
	}
	return state, nil
}

func ReadPinnedQuote(ctx context.Context, reader deployment.Reader, quoter, pool, trader common.Address, amountIn string, isBuy bool, block common.Hash) (ContractQuote, error) {
	amount, err := raw(amountIn)
	if err != nil {
		return ContractQuote{}, refuse("amount_invalid", "")
	}
	if reader == nil || quoter == (common.Address{}) || pool == (common.Address{}) || trader == (common.Address{}) || block == (common.Hash{}) || amount.Sign() == 0 {
		return ContractQuote{}, refuse("quote_context_invalid", "")
	}
	data, err := encodeCall("quoteExactInput(address,uint256,bool)", pool, amount, isBuy)
	if err != nil {
		return ContractQuote{}, err
	}
	output, err := reader.CallContractAtHash(ctx, ethereum.CallMsg{From: trader, To: &quoter, Data: data}, block)
	if err != nil {
		var rpcError rpc.DataError
		if errors.As(err, &rpcError) {
			if encoded, ok := rpcError.ErrorData().(string); ok {
				revert, decodeErr := hexutil.Decode(encoded)
				if decodeErr == nil && len(revert) == 4 {
					for signature, reason := range map[string]ReasonCode{"OpticalQuoteUnsupported()": OpticalQuoteUnsupported, "UnexecutableQuote()": UnexecutableQuote} {
						if bytes.Equal(revert, crypto.Keccak256([]byte(signature))[:4]) {
							return ContractQuote{}, Unavailable{"core_market", reason, "Quoter"}
						}
					}
				}
			}
		}
		if reverted(err) {
			return ContractQuote{}, refuse("quote_reverted", "Quoter")
		}
		return ContractQuote{}, deployment.Issue{Code: "quote_read_failed", Role: "Quoter"}
	}
	if len(output) != 96 {
		return ContractQuote{}, deployment.Issue{Code: "quote_tuple_malformed", Role: "Quoter"}
	}
	quote := ContractQuote{AmountOut: new(big.Int).SetBytes(output[:32]).String(), FeeAmount: new(big.Int).SetBytes(output[32:64]).String(), PriceImpactBPS: new(big.Int).SetBytes(output[64:]).String()}
	if quote.AmountOut == "0" {
		return ContractQuote{}, Unavailable{"core_market", UnexecutableQuote, "Quoter"}
	}
	return quote, nil
}

func (e *PreflightEngine) Revalidate(ctx context.Context, previous Preflight, request PreflightRequest) (Preflight, error) {
	if e == nil {
		return Preflight{}, deployment.Issue{Code: "preflight_engine_missing"}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return Preflight{}, err
	}
	hash := sha256.Sum256(encoded)
	if previous.RequestSHA256 != hex.EncodeToString(hash[:]) {
		return Preflight{}, Unavailable{"core_market", RevisionMismatch, "preflight_request"}
	}
	if previous.Revision != e.revision || previous.BlockHash != request.BlockHash.Hex() || previous.Family != request.Family || previous.Trader != request.Trader.Hex() || previous.Recipient != request.Recipient.Hex() {
		return Preflight{}, Unavailable{"core_market", RevisionMismatch, "preflight_context"}
	}
	current, err := e.Preflight(ctx, request)
	if err != nil {
		return current, err
	}
	if previous.ObservedRevisionSHA256 == "" || previous.ObservedRevisionSHA256 != current.ObservedRevisionSHA256 {
		return current, Unavailable{"core_market", RevisionMismatch, "observed_contract_state"}
	}
	if previous.CreationCost != nil && (current.CreationCost == nil || previous.CreationCost.ProtocolFeeRaw != current.CreationCost.ProtocolFeeRaw || previous.CreationCost.Spender != current.CreationCost.Spender) {
		return current, deployment.Issue{Code: "creation_fee_disclosure_invalidated", Role: "ProtocolConfig"}
	}
	return current, nil
}

func (e *PreflightEngine) RevalidateAt(ctx context.Context, previous Preflight, request PreflightRequest) (Preflight, error) {
	if e == nil {
		return Preflight{}, deployment.Issue{Code: "preflight_engine_missing"}
	}
	original := request
	original.BlockHash = common.HexToHash(previous.BlockHash)
	encoded, err := json.Marshal(original)
	if err != nil {
		return Preflight{}, err
	}
	hash := sha256.Sum256(encoded)
	if previous.RequestSHA256 != hex.EncodeToString(hash[:]) || previous.Revision != e.revision || previous.Family != request.Family || previous.Trader != request.Trader.Hex() || previous.Recipient != request.Recipient.Hex() || request.BlockHash == (common.Hash{}) {
		return Preflight{}, Unavailable{"core_market", RevisionMismatch, "preflight_context"}
	}
	current, err := e.Preflight(ctx, request)
	if err != nil {
		return current, err
	}
	if current.BlockNumber < previous.BlockNumber || previous.ObservedRevisionSHA256 == "" || current.ObservedRevisionSHA256 != previous.ObservedRevisionSHA256 || previous.Supported != current.Supported {
		return current, Unavailable{"core_market", RevisionMismatch, "observed_contract_state"}
	}
	return current, nil
}

// ReportedHeaderByNumberReader reports the canonical block identity at a
// height as the node-reported hash.
type ReportedHeaderByNumberReader interface {
	ReportedHeaderByNumber(context.Context, *big.Int) (*types.Header, common.Hash, error)
}

// HeightReader is the by-height state read surface (ethclient's). Pinned
// reads use it with the node-reported hash of the height checked before and
// after, as shared/chain Client.atPinned does.
type HeightReader interface {
	CodeAt(context.Context, common.Address, *big.Int) ([]byte, error)
	StorageAt(context.Context, common.Address, common.Hash, *big.Int) ([]byte, error)
	CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error)
}

type CreationReceiptReader interface {
	deployment.Reader
	deployment.ReportedHeaderReader
	ReportedHeaderByNumberReader
	HeightReader
	ReportedBlockByHash(context.Context, common.Hash) (*types.Block, error)
	TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error)
}

var errUnpinnedBlock = deployment.Issue{Code: "block_identity_unverified"}

// reportedPinnedReader serves deployment.Reader for an exact set of blocks
// named by their node-reported hashes and executes every state read by the
// block's height. It accepts no other hash. Callers bracket its use with
// reported-hash checks of each height (ReconcileCreationFee's canonical).
type reportedPinnedReader struct {
	chain interface {
		ChainID(context.Context) (*big.Int, error)
	}
	reads  HeightReader
	blocks map[common.Hash]*types.Header
}

func (r reportedPinnedReader) height(hash common.Hash) (*big.Int, error) {
	header, ok := r.blocks[hash]
	if !ok || header == nil || header.Number == nil {
		return nil, errUnpinnedBlock
	}
	return new(big.Int).Set(header.Number), nil
}
func (r reportedPinnedReader) ChainID(ctx context.Context) (*big.Int, error) {
	return r.chain.ChainID(ctx)
}
func (r reportedPinnedReader) HeaderByReportedHash(_ context.Context, hash common.Hash) (*types.Header, error) {
	header, ok := r.blocks[hash]
	if !ok || header == nil {
		return nil, errUnpinnedBlock
	}
	return types.CopyHeader(header), nil
}
func (r reportedPinnedReader) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return r.HeaderByReportedHash(ctx, hash)
}
func (r reportedPinnedReader) CodeAtHash(ctx context.Context, address common.Address, hash common.Hash) ([]byte, error) {
	height, err := r.height(hash)
	if err != nil {
		return nil, err
	}
	return r.reads.CodeAt(ctx, address, height)
}
func (r reportedPinnedReader) StorageAtHash(ctx context.Context, address common.Address, slot, hash common.Hash) ([]byte, error) {
	height, err := r.height(hash)
	if err != nil {
		return nil, err
	}
	return r.reads.StorageAt(ctx, address, slot, height)
}
func (r reportedPinnedReader) CallContractAtHash(ctx context.Context, call ethereum.CallMsg, hash common.Hash) ([]byte, error) {
	height, err := r.height(hash)
	if err != nil {
		return nil, err
	}
	return r.reads.CallContract(ctx, call, height)
}

type CreationFeeReconciliation struct {
	TransactionHash         string `json:"transactionHash"`
	BlockHash               string `json:"blockHash"`
	ParentBlockHash         string `json:"parentBlockHash"`
	Path                    string `json:"path"`
	Payer                   string `json:"payer"`
	Treasury                string `json:"treasury"`
	FeeAsset                string `json:"feeAsset"`
	FeeRaw                  string `json:"feeRaw"`
	PayerDebitRaw           string `json:"payerDebitRaw"`
	TreasuryCreditRaw       string `json:"treasuryCreditRaw"`
	TreasuryLedgerCreditRaw string `json:"treasuryLedgerCreditRaw"`
}

func (e *PreflightEngine) ReconcileCreationFee(ctx context.Context, hash common.Hash, treasuryIdentity deployment.Role) (CreationFeeReconciliation, error) {
	bad := deployment.Issue{Code: "creation_fee_reconciliation_unverified"}
	if e == nil || hash == (common.Hash{}) {
		return CreationFeeReconciliation{}, bad
	}
	reader, ok := e.reader.(CreationReceiptReader)
	if !ok {
		return CreationFeeReconciliation{}, bad
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	receipt, err := reader.TransactionReceipt(ctx, hash)
	if err != nil || receipt == nil || receipt.Status != types.ReceiptStatusSuccessful || receipt.TxHash != hash || receipt.BlockNumber == nil {
		return CreationFeeReconciliation{}, bad
	}
	// blockHash is the node-reported hash of the receipt's block, the only
	// block identity used below. go-ethereum's block.Hash() is a recomputed
	// header hash that names no block on chain 125, and the transaction and
	// receipt roots there are CometBFT commitments (receiptsRoot is the empty
	// SHA-256), so neither is re-derived: inclusion is proven by the node
	// serving, under blockHash, a block whose only transaction is hash, by the
	// receipt naming blockHash, and by every fee log naming blockHash and hash.
	blockHash := receipt.BlockHash
	block, err := reader.ReportedBlockByHash(ctx, blockHash)
	if err != nil || block == nil || block.Number().Cmp(receipt.BlockNumber) != 0 || block.Number().Sign() <= 0 || len(block.Transactions()) != 1 || receipt.TransactionIndex != 0 || block.Transactions()[0].Hash() != hash {
		return CreationFeeReconciliation{}, bad
	}
	// The parent hash in a chain-125 header is the parent's node-reported hash.
	parentHash := block.ParentHash()
	parentNumber := new(big.Int).Sub(block.Number(), big.NewInt(1))
	parent, err := reader.HeaderByReportedHash(ctx, parentHash)
	if err != nil || parent == nil || parent.Number == nil || parent.Number.Cmp(parentNumber) != 0 {
		return CreationFeeReconciliation{}, bad
	}
	// canonical proves the node still reports blockHash and parentHash for
	// their heights; it runs before and after every state read, which goes
	// by height through pinned (shared/chain Client.atPinned semantics).
	canonical := func() bool {
		for _, want := range []struct {
			number *big.Int
			hash   common.Hash
		}{{block.Number(), blockHash}, {parentNumber, parentHash}} {
			if _, reported, err := reader.ReportedHeaderByNumber(ctx, want.number); err != nil || reported != want.hash {
				return false
			}
		}
		return true
	}
	if !canonical() {
		return CreationFeeReconciliation{}, bad
	}
	pinnedReader := reportedPinnedReader{chain: reader, reads: reader, blocks: map[common.Hash]*types.Header{blockHash: block.Header(), parentHash: types.CopyHeader(parent)}}
	pinned := *e
	pinned.reader = pinnedReader
	transaction := block.Transactions()[0]
	if transaction.To() == nil || transaction.ChainId().Cmp(big.NewInt(deployment.ChainID)) != 0 || transaction.Value().Sign() != 0 || len(transaction.Data()) < 4 || !bytes.Equal(transaction.Data()[:4], crypto.Keccak256([]byte("createMarket(string,string,uint8,address)"))[:4]) {
		return CreationFeeReconciliation{}, bad
	}
	path := ""
	for _, name := range []string{"Router", "SidioraFactory"} {
		if *transaction.To() == common.HexToAddress(e.manifest.Roles[name].Address) {
			path = name
		}
	}
	if path == "" {
		return CreationFeeReconciliation{}, bad
	}
	payer, err := types.Sender(types.LatestSignerForChainID(big.NewInt(deployment.ChainID)), transaction)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: e.maxAge, Timeout: e.timeout}
	attestation, err := deployment.Attest(ctx, pinnedReader, e.manifest, blockHash, policy)
	if err != nil || attestation.ValidateFamilyFor(e.manifest, policy, familyRoles[Creation], true) != nil {
		return CreationFeeReconciliation{}, bad
	}
	factory := common.HexToAddress(e.manifest.Roles["SidioraFactory"].Address)
	word, err := pinned.call(ctx, factory, "treasury()", blockHash)
	if err != nil || len(word) != 32 || !bytes.Equal(word[:12], make([]byte, 12)) || len(treasuryIdentity.Address) != 42 || !common.IsHexAddress(treasuryIdentity.Address) || common.BytesToAddress(word[12:]) != common.HexToAddress(treasuryIdentity.Address) {
		return CreationFeeReconciliation{}, bad
	}
	treasury := common.HexToAddress(treasuryIdentity.Address)
	if treasury == (common.Address{}) {
		return CreationFeeReconciliation{}, bad
	}
	code, err := pinnedReader.CodeAtHash(ctx, treasury, blockHash)
	if err != nil || len(code) == 0 || len(code) > 128<<10 || crypto.Keccak256Hash(code).Hex() != treasuryIdentity.RuntimeHash {
		return CreationFeeReconciliation{}, bad
	}
	if treasuryIdentity.Kind == "erc1967" {
		slot, err := pinnedReader.StorageAtHash(ctx, treasury, deployment.ImplementationSlot, blockHash)
		if err != nil || len(slot) != 32 || !bytes.Equal(slot[:12], make([]byte, 12)) || common.BytesToAddress(slot[12:]) != common.HexToAddress(treasuryIdentity.ImplementationAddress) {
			return CreationFeeReconciliation{}, bad
		}
		implementation, err := pinnedReader.CodeAtHash(ctx, common.BytesToAddress(slot[12:]), blockHash)
		if err != nil || len(implementation) == 0 || len(implementation) > 128<<10 || crypto.Keccak256Hash(implementation).Hex() != treasuryIdentity.ImplementationHash {
			return CreationFeeReconciliation{}, bad
		}
	} else if treasuryIdentity.Kind != "direct" || treasuryIdentity.ImplementationAddress != "" || treasuryIdentity.ImplementationHash != "" {
		return CreationFeeReconciliation{}, bad
	}
	usdl := common.HexToAddress(e.manifest.Roles["USDL"].Address)
	transfer := crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)"))
	count := 0
	for _, log := range receipt.Logs {
		if log == nil {
			return CreationFeeReconciliation{}, bad
		}
		if log.Address != usdl || len(log.Topics) == 0 || log.Topics[0] != transfer {
			continue
		}
		if log.Removed || log.TxHash != hash || log.BlockHash != blockHash || log.TxIndex != 0 || len(log.Topics) != 3 || len(log.Data) != 32 || log.Topics[1] != common.BytesToHash(payer.Bytes()) || log.Topics[2] != common.BytesToHash(treasury.Bytes()) || new(big.Int).SetBytes(log.Data).String() != deployment.CreationFeeRaw {
			return CreationFeeReconciliation{}, bad
		}
		count++
	}
	if count != 1 {
		return CreationFeeReconciliation{}, bad
	}
	read := func(address common.Address, signature string, at common.Hash, args ...any) (*big.Int, error) {
		value, err := pinned.call(ctx, address, signature, at, args...)
		if err != nil || len(value) != 32 {
			return nil, bad
		}
		return new(big.Int).SetBytes(value), nil
	}
	beforePayer, err := read(usdl, "balanceOf(address)", parentHash, payer)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	afterPayer, err := read(usdl, "balanceOf(address)", blockHash, payer)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	beforeTreasury, err := read(usdl, "balanceOf(address)", parentHash, treasury)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	afterTreasury, err := read(usdl, "balanceOf(address)", blockHash, treasury)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	beforeLedger, err := read(treasury, "getBalance(address)", parentHash, usdl)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	afterLedger, err := read(treasury, "getBalance(address)", blockHash, usdl)
	if err != nil {
		return CreationFeeReconciliation{}, bad
	}
	if !canonical() {
		return CreationFeeReconciliation{}, bad
	}
	debit, credit, ledger := new(big.Int).Sub(beforePayer, afterPayer), new(big.Int).Sub(afterTreasury, beforeTreasury), new(big.Int).Sub(afterLedger, beforeLedger)
	if debit.String() != deployment.CreationFeeRaw || credit.String() != deployment.CreationFeeRaw || ledger.String() != deployment.CreationFeeRaw {
		return CreationFeeReconciliation{}, bad
	}
	return CreationFeeReconciliation{TransactionHash: hash.Hex(), BlockHash: blockHash.Hex(), ParentBlockHash: parentHash.Hex(), Path: path, Payer: payer.Hex(), Treasury: treasury.Hex(), FeeAsset: "USDL", FeeRaw: deployment.CreationFeeRaw, PayerDebitRaw: debit.String(), TreasuryCreditRaw: credit.String(), TreasuryLedgerCreditRaw: ledger.String()}, nil
}

func uniquePreflightRequest(data []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] || len(seen) >= 32 {
			return false
		}
		seen[name] = true
		value, err := decoder.Token()
		if err != nil || value == nil {
			return false
		}
		if _, composite := value.(json.Delim); composite {
			return false
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}
