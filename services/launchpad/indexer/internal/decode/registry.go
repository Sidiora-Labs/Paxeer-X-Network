package decode

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

const MaxFieldBytes = 64 << 10
const MaxEnvelopeBytes = 1 << 20

// LaunchpadPrecompileRole serves the EventEmitter ABI at the launchpad
// precompile, the one address several launchpad roles share. The precompile
// also executes Router calls made to it directly and emits the Router events
// at its own address, so NewRegistry binds the Router ABI there as well.
const LaunchpadPrecompileRole = "LaunchpadPrecompile"

var LaunchpadPrecompileAddress = common.HexToAddress("0x0000000000000000000000000000000000001017")

// launchpadOrigin names the role whose contract flow emits each launchpad
// family event at the precompile.
var launchpadOrigin = map[string]string{
	"Swap":               "SidioraPool",
	"PoolStateUpdated":   "SidioraPool",
	"TokenDeployed":      "SidioraFactory",
	"MarketCreated":      "SidioraFactory",
	"NftMint":            "SidioraNFT",
	"FeeStrategyChanged": "SidioraNFT",
	"NftTransfer":        "SidioraNFT",
	"PoolRegistered":     "PoolRegistry",
	"RouterTrade":        "Router",
	"ConfigUpdated":      "ProtocolConfig",
	"FeeFlow":            "FeeAccumulator",
	"FeeDistributed":     "FeesRouter",
}

// Order includes transaction and log index: upgrades can activate within a block.
type Order struct {
	Block       uint64 `json:"block"`
	Transaction uint   `json:"transaction"`
	Log         uint   `json:"log"`
}

func (a Order) less(b Order) bool {
	if a.Block != b.Block {
		return a.Block < b.Block
	}
	if a.Transaction != b.Transaction {
		return a.Transaction < b.Transaction
	}
	return a.Log < b.Log
}

// Deployment is coordinator-attested, never inferred from a log's event name.
// Until is exclusive; nil denotes an open epoch. An optional family is unusable
// until Enabled is explicitly set. ABIRevision must match the frozen ABI digest.
type Deployment struct {
	Address     common.Address
	Role        string
	ABIRevision string
	From        Order
	Until       *Order
	Optional    bool
	Enabled     bool
	// PaxeerNonzeroBools maps canonical event signatures to approved field names.
	PaxeerNonzeroBools map[string][]string
	// EnumMax maps canonical signatures and field names to inclusive known codes.
	// Unknown codes remain raw and emit compatibility alerts, never invented meaning.
	EnabledOptionalEvents []string
	EnumMax               map[string]map[string]uint64
}
type Catalog struct {
	SchemaVersion  int               `json:"schemaVersion"`
	ChainID        uint64            `json:"chainId"`
	ManifestSHA256 string            `json:"manifestSha256"`
	Contracts      []CatalogContract `json:"contracts"`
}
type CatalogContract struct {
	Name            string          `json:"name"`
	ABIRevision     string          `json:"abiSha256"`
	SourceDigest    string          `json:"sourceSha256"`
	CompilerDigest  string          `json:"buildInfoSha256"`
	CompilerVersion string          `json:"compilerVersion"`
	ABI             json.RawMessage `json:"abi"`
}
type RawIdentity struct {
	ChainID         uint64 `json:"chainId"`
	BlockHash       string `json:"blockHash"`
	TransactionHash string `json:"transactionHash"`
	LogIndex        uint   `json:"logIndex"`
}
type RawLog struct {
	Identity RawIdentity `json:"identity"`
	Emitter  string      `json:"emitter"`
	Order    Order       `json:"order"`
	Topics   []string    `json:"topics"`
	Data     string      `json:"data"`
	Removed  bool        `json:"removed"`
}
type OperationalEvent struct {
	Category        string `json:"category"`
	Action          any    `json:"action"`
	Subject         any    `json:"subject"`
	Actor           any    `json:"actor"`
	PayloadRevision string `json:"payloadRevision"`
}
type FamilyDTO struct {
	Family       string         `json:"family"`
	Fields       map[string]any `json:"fields"`
	EconomicFill bool           `json:"economicFill"`
	Authority    string         `json:"authority"`
}

// RawSummary is admission-rejection evidence. Oversize bytes are never copied
// into a decoded envelope; callers can retain the original bounded source
// transport artifact separately. Hashes use streaming SHA256, not hex expansion.
type RawSummary struct {
	DataBytes    int    `json:"dataBytes"`
	TopicCount   int    `json:"topicCount"`
	DataSHA256   string `json:"dataSha256"`
	TopicsSHA256 string `json:"topicsSha256"`
}
type ActorContext struct {
	TransactionOrigin     *string       `json:"transactionOrigin"`
	EventCaller           *string       `json:"eventCaller"`
	Recipient             *string       `json:"recipient"`
	Beneficiary           *string       `json:"beneficiary"`
	AttributionConfidence string        `json:"attributionConfidence"`
	AttributionMethod     string        `json:"attributionMethod"`
	EvidenceIdentity      []RawIdentity `json:"evidenceIdentity"`
	Revision              string        `json:"revision"`
}

type DecodedEnvelope struct {
	Actors          *ActorContext     `json:"actors,omitempty"`
	RawSummary      *RawSummary       `json:"rawSummary,omitempty"`
	Raw             RawLog            `json:"raw"`
	ABIRevision     string            `json:"abiRevision"`
	EventSignature  string            `json:"eventSignature"`
	EventName       string            `json:"eventName"`
	EmitterRole     string            `json:"emitterRole"`
	DerivedRevision string            `json:"derivedRevision"`
	Status          string            `json:"status"`
	FailureReason   string            `json:"failureReason"`
	Opaque          bool              `json:"opaque"`
	Event           *Event            `json:"event"`
	Family          *FamilyDTO        `json:"family"`
	Operational     *OperationalEvent `json:"operational"`
	Alerts          []string          `json:"alerts"`
}
type Coverage struct {
	Role           string `json:"role"`
	ABIRevision    string `json:"abiRevision"`
	EventName      string `json:"eventName"`
	Signature      string `json:"signature"`
	State          string `json:"state"`
	Authority      string `json:"authority"`
	ProcessorRoute string `json:"processorRoute"`
}
type registryEpoch struct {
	deployment Deployment
	events     map[common.Hash]abi.Event
}

// Registry is immutable after construction and safe for concurrent Decode calls.
type Registry struct {
	epochs   map[common.Address][]registryEpoch
	known    map[common.Hash]bool
	coverage []Coverage
}

// NewRegistry consumes the serial coordinator's frozen catalog. Deployments must
// come from the separately attested registry (catalog declarations do not prove
// deployment, activation height, concrete emission or emitter authorization).
func NewRegistry(catalogJSON []byte, deployments []Deployment) (*Registry, error) {
	var c Catalog
	if err := json.Unmarshal(catalogJSON, &c); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	if c.SchemaVersion != 1 || c.ChainID != 125 || !digest(c.ManifestSHA256) {
		return nil, fmt.Errorf("invalid catalog identity")
	}
	r := &Registry{epochs: make(map[common.Address][]registryEpoch), known: make(map[common.Hash]bool)}
	contracts := map[string]CatalogContract{}
	parsed := map[string]abi.ABI{}
	for _, contract := range c.Contracts {
		if contract.Name == "" || !digest(contract.ABIRevision) || !digest(contract.SourceDigest) || !digest(contract.CompilerDigest) || contract.CompilerVersion == "" {
			return nil, fmt.Errorf("missing provenance for %s", contract.Name)
		}
		key := contract.Name + "\x00" + contract.ABIRevision
		if _, exists := contracts[key]; exists {
			return nil, fmt.Errorf("duplicate role %s", contract.Name)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, contract.ABI); err != nil {
			return nil, err
		}
		sum := sha256.Sum256(compact.Bytes())
		if hex.EncodeToString(sum[:]) != contract.ABIRevision {
			return nil, fmt.Errorf("ABI digest mismatch for %s", contract.Name)
		}
		a, err := abi.JSON(bytes.NewReader(contract.ABI))
		if err != nil {
			return nil, fmt.Errorf("ABI %s: %w", contract.Name, err)
		}
		contracts[key] = contract
		parsed[key] = a
		for _, ev := range a.Events {
			r.known[ev.ID] = true
		}
	}
	deployments, err := precompileRouter(deployments, contracts)
	if err != nil {
		return nil, err
	}
	for _, input := range deployments {
		// Deep copy policy maps; caller mutation cannot change authorization.
		policy, _ := json.Marshal(input)
		var d Deployment
		if err := json.Unmarshal(policy, &d); err != nil {
			return nil, err
		}
		if optionalRole(d.Role) && !d.Optional {
			return nil, fmt.Errorf("optional role %s requires explicit capability policy", d.Role)
		}
		if d.Role == LaunchpadPrecompileRole && (d.Address != LaunchpadPrecompileAddress || d.Optional) {
			return nil, fmt.Errorf("invalid deployment %s", d.Role)
		}
		abiName := abiRole(d.Role)
		key := abiName + "\x00" + d.ABIRevision
		contract, exists := contracts[key]
		if !exists || contract.ABIRevision != d.ABIRevision || d.Address == (common.Address{}) || (d.Until != nil && !d.From.less(*d.Until)) {
			return nil, fmt.Errorf("invalid deployment %s", d.Role)
		}
		if err := validatePolicies(parsed[key], d); err != nil {
			return nil, err
		}
		events := make(map[common.Hash]abi.Event)
		for _, ev := range parsed[key].Events {
			events[ev.ID] = ev
			state := "required"
			if abiName == "EventEmitter" && optionalEvent(ev.Name) && !optionalEnabled(d, ev.Name) {
				state = "unsupported"
			}
			if d.Optional {
				state = "optional_deployed"
				if !d.Enabled {
					state = "unsupported"
				}
			}
			if declaredOnly(abiName, ev.Name) {
				state = "unsupported"
			}
			r.coverage = append(r.coverage, Coverage{d.Role, d.ABIRevision, ev.Name, ev.Sig, state, authority(abiName, ev.Name), eventFamily(abiName, ev.Name)})
		}
		epoch := registryEpoch{d, events}
		for _, old := range r.epochs[d.Address] {
			// Distinct roles may share only the launchpad precompile address.
			if (old.deployment.Until == nil || d.From.less(*old.deployment.Until)) && (d.Until == nil || old.deployment.From.less(*d.Until)) && (d.Address != LaunchpadPrecompileAddress || old.deployment.Role == d.Role) {
				return nil, fmt.Errorf("overlapping deployment epochs for %s", d.Address)
			}
		}
		r.epochs[d.Address] = append(r.epochs[d.Address], epoch)
	}
	// Catalogued but unattested contracts are explicitly unsupported, not an
	// empty successful capability. Declarations alone are never deployed proof.
	deployed := map[string]bool{}
	for _, d := range deployments {
		deployed[d.Role+"\x00"+d.ABIRevision] = true
	}
	for key, a := range parsed {
		if !deployed[key] {
			c := contracts[key]
			for _, ev := range a.Events {
				r.coverage = append(r.coverage, Coverage{c.Name, c.ABIRevision, ev.Name, ev.Sig, "unsupported", authority(c.Name, ev.Name), eventFamily(c.Name, ev.Name)})
			}
		}
	}
	sort.Slice(r.coverage, func(i, j int) bool {
		a, b := r.coverage[i], r.coverage[j]
		return a.Role+a.ABIRevision+a.Signature < b.Role+b.ABIRevision+b.Signature
	})
	return r, nil
}

// precompileRouter adds Router epochs at the launchpad precompile unless the
// caller already bound Router there. Router calls made to the precompile emit
// Buy, Sell, MarketCreated and MultihopSwap with the precompile as emitter.
// Each attested Router ABI revision is bound over its own deployments'
// [From, Until) ranges within each precompile epoch, so Decode selects the
// revision by log order like any other address's epochs, and NewRegistry
// refuses Router revisions whose ranges overlap. With no Router attested, the
// catalog's only Router ABI covers each precompile epoch.
func precompileRouter(deployments []Deployment, contracts map[string]CatalogContract) ([]Deployment, error) {
	var precompiles, routers []Deployment
	for _, d := range deployments {
		switch {
		case d.Role == "Router" && d.Address == LaunchpadPrecompileAddress:
			return deployments, nil
		case d.Role == "Router":
			routers = append(routers, Deployment{ABIRevision: d.ABIRevision, From: d.From, Until: d.Until})
		case d.Role == LaunchpadPrecompileRole:
			precompiles = append(precompiles, d)
		}
	}
	if len(precompiles) == 0 {
		return deployments, nil
	}
	if len(routers) == 0 {
		for _, c := range contracts {
			if c.Name == "Router" {
				routers = append(routers, Deployment{ABIRevision: c.ABIRevision})
			}
		}
		if len(routers) != 1 {
			return nil, fmt.Errorf("launchpad precompile without an attested Router requires one catalog Router ABI revision, found %d", len(routers))
		}
	}
	// Coalesce each revision's overlapping or adjacent ranges, e.g. the same
	// Router ABI attested at two addresses.
	sort.Slice(routers, func(i, j int) bool {
		if routers[i].ABIRevision != routers[j].ABIRevision {
			return routers[i].ABIRevision < routers[j].ABIRevision
		}
		return routers[i].From.less(routers[j].From)
	})
	var spans []Deployment
	for _, r := range routers {
		if n := len(spans); n > 0 && spans[n-1].ABIRevision == r.ABIRevision && (spans[n-1].Until == nil || !spans[n-1].Until.less(r.From)) {
			if spans[n-1].Until != nil && (r.Until == nil || spans[n-1].Until.less(*r.Until)) {
				spans[n-1].Until = r.Until
			}
			continue
		}
		spans = append(spans, r)
	}
	out := append([]Deployment(nil), deployments...)
	for _, p := range precompiles {
		for _, r := range spans {
			from, until := p.From, p.Until
			if from.less(r.From) {
				from = r.From
			}
			if until == nil || (r.Until != nil && r.Until.less(*until)) {
				until = r.Until
			}
			if until != nil && !from.less(*until) {
				continue
			}
			out = append(out, Deployment{Address: LaunchpadPrecompileAddress, Role: "Router", ABIRevision: r.ABIRevision, From: from, Until: until})
		}
	}
	return out, nil
}
func abiRole(role string) string {
	if role == LaunchpadPrecompileRole {
		return "EventEmitter"
	}
	return role
}

// dispatch selects, among the epochs active at one address, the role whose ABI
// declares topic0. The precompile binding answers every EventEmitter topic.
// It returns nil when no role sharing the address declares topic0.
func dispatch(active []registryEpoch, topics []common.Hash) (*registryEpoch, error) {
	if len(active) == 1 {
		return &active[0], nil
	}
	var precompile *registryEpoch
	var matches []registryEpoch
	for i := range active {
		if active[i].deployment.Role == LaunchpadPrecompileRole {
			precompile = &active[i]
			continue
		}
		if len(topics) > 0 {
			if _, ok := active[i].events[topics[0]]; ok {
				matches = append(matches, active[i])
			}
		}
	}
	if precompile != nil {
		if len(topics) == 0 || len(matches) == 0 {
			return precompile, nil
		}
		if _, ok := precompile.events[topics[0]]; ok {
			return precompile, nil
		}
	}
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return &matches[0], nil
	}
	return nil, fmt.Errorf("signature %s resolves to %d of %d roles sharing an address", topics[0].Hex(), len(matches), len(active))
}
func activeRole(active []registryEpoch, role string) bool {
	for _, epoch := range active {
		if epoch.deployment.Role == role {
			return true
		}
	}
	return false
}
func digest(s string) bool { b, e := hex.DecodeString(s); return e == nil && len(b) == 32 }
func validatePolicies(a abi.ABI, d Deployment) error {
	for sig, fields := range d.PaxeerNonzeroBools {
		for _, field := range fields {
			found := false
			for _, ev := range a.Events {
				if ev.Sig == sig {
					for _, in := range ev.Inputs {
						if in.Name == field && in.Type.T == abi.BoolTy {
							found = true
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("invalid bool policy %s.%s", sig, field)
			}
		}
	}
	for sig, fields := range d.EnumMax {
		for field := range fields {
			found := false
			for _, ev := range a.Events {
				if ev.Sig == sig {
					for _, in := range ev.Inputs {
						if in.Name == field && in.Type.T == abi.UintTy {
							found = true
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("invalid enum policy %s.%s", sig, field)
			}
		}
	}
	return nil
}
func (r *Registry) Coverage() []Coverage { return append([]Coverage(nil), r.coverage...) }
func rawMetadata(l ethtypes.Log) RawLog {
	return RawLog{Identity: RawIdentity{125, l.BlockHash.Hex(), l.TxHash.Hex(), l.Index}, Emitter: strings.ToLower(l.Address.Hex()), Order: Order{l.BlockNumber, l.TxIndex, l.Index}, Removed: l.Removed}
}
func validIdentity(l ethtypes.Log) bool {
	return l.Address != (common.Address{}) && l.BlockHash != (common.Hash{}) && l.TxHash != (common.Hash{})
}
func rawSizeAllowed(l ethtypes.Log) bool {
	return len(l.Topics) <= 4 && len(l.Data) <= MaxEnvelopeBytes && len(l.Topics) <= (MaxEnvelopeBytes-len(l.Data))/32
}
func summarizeRaw(l ethtypes.Log) *RawSummary {
	data := sha256.Sum256(l.Data)
	topics := sha256.New()
	for i := range l.Topics {
		_, _ = topics.Write(l.Topics[i][:])
	}
	return &RawSummary{len(l.Data), len(l.Topics), hex.EncodeToString(data[:]), hex.EncodeToString(topics.Sum(nil))}
}
func rawLog(l ethtypes.Log) RawLog {
	topics := make([]string, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = t.Hex()
	}
	return RawLog{RawIdentity{125, l.BlockHash.Hex(), l.TxHash.Hex(), l.Index}, strings.ToLower(l.Address.Hex()), Order{l.BlockNumber, l.TxIndex, l.Index}, topics, "0x" + hex.EncodeToString(l.Data), l.Removed}
}
func (r *Registry) Decode(l ethtypes.Log) (*DecodedEnvelope, error) {
	e := &DecodedEnvelope{Raw: rawMetadata(l), DerivedRevision: "decoder-v1", Status: "opaque", Opaque: true}
	fail := func(reason string, err error) (*DecodedEnvelope, error) {
		e.Status = "quarantined"
		e.FailureReason = reason
		return e, err
	}
	if !rawSizeAllowed(l) {
		e.RawSummary = summarizeRaw(l)
		return fail("bounds", fmt.Errorf("raw envelope exceeds bounded admission"))
	}
	e.Raw = rawLog(l)
	if !validIdentity(l) {
		e.RawSummary = summarizeRaw(l)
		return fail("invalid_identity", fmt.Errorf("committed log requires nonzero block hash, transaction hash and emitter"))
	}
	if l.Removed {
		return fail("removed_log", fmt.Errorf("removed log on committed-finality chain"))
	}
	var active []registryEpoch
	for _, candidate := range r.epochs[l.Address] {
		d := candidate.deployment
		if !e.Raw.Order.less(d.From) && (d.Until == nil || e.Raw.Order.less(*d.Until)) && (!d.Optional || d.Enabled) {
			active = append(active, candidate)
		}
	}
	if len(active) == 0 {
		if len(l.Topics) > 0 && r.known[l.Topics[0]] {
			return fail("untrusted_emitter", fmt.Errorf("known signature from unauthorized emitter %s", l.Address))
		}
		e.FailureReason = "untrusted_emitter"
		return e, nil
	}
	epoch, err := dispatch(active, l.Topics)
	if err != nil {
		return fail("ambiguous_emitter", err)
	}
	if epoch == nil {
		if len(l.Topics) > 0 {
			e.EventSignature = l.Topics[0].Hex()
			if r.known[l.Topics[0]] {
				return fail("unauthorized_signature", fmt.Errorf("signature not approved for any role at %s", l.Address))
			}
		}
		e.FailureReason = "unknown_signature"
		return e, nil
	}
	d := epoch.deployment
	abiName := abiRole(d.Role)
	e.ABIRevision = d.ABIRevision
	e.EmitterRole = d.Role
	if len(l.Topics) == 0 {
		e.FailureReason = "unknown_signature"
		return e, nil
	}
	e.EventSignature = l.Topics[0].Hex()
	ev, ok := epoch.events[l.Topics[0]]
	if !ok {
		if r.known[l.Topics[0]] {
			return fail("unauthorized_signature", fmt.Errorf("signature not approved for %s", d.Role))
		}
		e.FailureReason = "unknown_signature"
		return e, nil
	}
	e.EventName = ev.Name
	if origin, ok := launchpadOrigin[ev.Name]; ok && d.Role == LaunchpadPrecompileRole && activeRole(active, origin) {
		e.EmitterRole = origin
	}
	if declaredOnly(abiName, ev.Name) {
		e.FailureReason = "declared_not_emitted"
		return e, nil
	}
	if abiName == "EventEmitter" && optionalEvent(ev.Name) && !optionalEnabled(d, ev.Name) {
		e.FailureReason = "unsupported_capability"
		return e, nil
	}
	e.EventSignature = ev.Sig
	args, reason, err := exactArgs(ev, l.Topics, l.Data, d.PaxeerNonzeroBools[ev.Sig])
	if err != nil {
		return fail(reason, err)
	}
	payload, marshalErr := json.Marshal(args)
	if marshalErr != nil || len(payload) > MaxEnvelopeBytes {
		return fail("bounds", fmt.Errorf("decoded payload exceeds envelope limit"))
	}
	e.Status = "decoded"
	e.Opaque = strings.HasPrefix(ev.Name, "EventLog")
	e.Event = &Event{ev.Name, ev.Sig, args, l.BlockNumber, strings.ToLower(l.TxHash.Hex()), l.Index}
	e.Family = &FamilyDTO{eventFamily(abiName, ev.Name), args, abiName == "EventEmitter" && ev.Name == "Swap", authority(abiName, ev.Name)}
	if e.Family.EconomicFill || optionalEconomic(e) {
		e.Actors = eventActors(e)
	}
	if e.Family.Family == "operational" || e.Family.Family == "optical" {
		e.Operational = &OperationalEvent{e.Family.Family, args["action"], args["subject"], args["actor"], d.ABIRevision}
	}
	for field, max := range d.EnumMax[ev.Sig] {
		v := fmt.Sprint(args[field])
		n, ok := newBig(v)
		if !ok || n.BitLen() > 64 || n.Uint64() > max {
			e.Alerts = append(e.Alerts, "unknown_action:"+field)
		}
	}
	sort.Strings(e.Alerts)
	return e, nil
}
func authority(role, name string) string {
	if role == "EventEmitter" {
		switch name {
		case "Swap", "PoolStateUpdated", "ConfigUpdated":
			return "primary"
		case "RoleAdminChanged", "RoleGranted", "RoleRevoked", "Upgraded", "Initialized":
			return "native"
		default:
			return "mirror"
		}
	}
	return "native"
}
func family(name string) string {
	switch name {
	case "Swap", "MarketCreated", "PoolRegistered", "TokenDeployed", "PoolStateUpdated", "RouterTrade", "Buy", "Sell", "MultihopSwap":
		return "amm"
	case "FeeRecorded", "FeeDistributed", "FeeStrategyChanged", "FeeFlow", "AirdropClaimed":
		return "fees"
	case "NftMint", "NftTransfer", "AssetApproval", "TokenTransfer", "Transfer", "Approval", "ApprovalForAll":
		return "assets"
	case "OpticalExecuted", "OpticalLifecycle":
		return "optical"
	default:
		return "operational"
	}
}

func optionalEvent(name string) bool {
	switch name {
	case "PecorSwap", "PecorOrderCreated", "PecorOrderLifecycle", "BestRouteSwap", "VaultFlow", "PriceUpdated", "CircuitBreakerTriggered", "OracleAdapterLifecycle":
		return true
	}
	return false
}
func optionalEnabled(d Deployment, name string) bool {
	for _, n := range d.EnabledOptionalEvents {
		if n == name {
			return true
		}
	}
	return false
}

func declaredOnly(role, name string) bool {
	return role == "ProtocolConfig" && name == "ConfigUpdated" || role == "FeeAccumulator" && name == "ProtocolFeeSwept"
}

func optionalRole(role string) bool {
	switch role {
	case "PECOR", "PECOROrders", "PECORVault", "MetaAGRouter", "MetaAGQuoter", "PriceOracle", "OracleHub", "TransactionTracker", "SidioraAdapter", "VaultAdapter":
		return true
	}
	return false
}
