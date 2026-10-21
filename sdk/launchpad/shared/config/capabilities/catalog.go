// Package capabilities evaluates frozen capability IDs without mistaking an
// unavailable response or a local configuration test for release qualification.
package capabilities

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

//go:embed frozen.json
var frozenJSON []byte

//go:embed contract-support.json
var supportJSON []byte

type ReasonCode string

const (
	UnknownCapability           ReasonCode = "unknown_capability"
	PrerequisiteUnavailable     ReasonCode = "prerequisite_unavailable"
	QualificationPending        ReasonCode = "qualification_pending"
	IdentityUnverified          ReasonCode = "identity_unverified"
	OpticalQuoteUnsupported     ReasonCode = "optical_quote_unsupported"
	UnexecutableQuote           ReasonCode = "unexecutable_quote"
	RevisionMismatch            ReasonCode = "deployment_revision_mismatch"
	OptionalManifestUnavailable ReasonCode = "optional_manifest_unavailable"
	DisplayUnavailable          ReasonCode = "display_unavailable"
	// PoolUnknown is the deterministic answer for a pool the verified
	// PoolRegistry does not list at the pinned block (including the zero
	// address). It is not retryable; RPC failures stay errors.
	PoolUnknown ReasonCode = "pool_unknown"
)

type Unavailable struct {
	CapabilityID string     `json:"capabilityId"`
	ReasonCode   ReasonCode `json:"reasonCode"`
	DependencyID string     `json:"dependencyId,omitempty"`
}

func (u Unavailable) Error() string {
	return fmt.Sprintf("%s: %s (%s)", u.CapabilityID, u.ReasonCode, u.DependencyID)
}

type Prerequisite struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	State string `json:"state"`
}
type Entry struct {
	ID                 string         `json:"id"`
	State              string         `json:"state"`
	RequiredForRelease bool           `json:"requiredForRelease"`
	Prerequisites      []Prerequisite `json:"prerequisites"`
}
type Catalog struct {
	SchemaVersion int     `json:"schemaVersion"`
	SourceSHA256  string  `json:"sourceSha256"`
	Capabilities  []Entry `json:"capabilities"`
}

func Frozen() Catalog {
	var c Catalog
	if e := json.Unmarshal(frozenJSON, &c); e != nil {
		panic(e)
	}
	return c
}

// Require rejects unknown IDs, and keeps all frozen missing/unqualified
// prerequisites blocked. Enabling a capability needs a new reviewed baseline.
func Require(id string) error {
	for _, entry := range Frozen().Capabilities {
		if entry.ID != id {
			continue
		}
		for _, p := range entry.Prerequisites {
			if p.State != "qualified" {
				return Unavailable{id, PrerequisiteUnavailable, p.ID}
			}
		}
		if entry.State != "enabled" {
			return Unavailable{id, QualificationPending, "qualification"}
		}
		return nil
	}
	return Unavailable{id, UnknownCapability, ""}
}

type Selector struct {
	Signature string `json:"signature"`
	Selector  string `json:"selector"`
}
type Support struct {
	SchemaVersion          int                   `json:"schemaVersion"`
	ABICatalogSHA256       string                `json:"abiCatalogSha256"`
	EventAuthoritiesSHA256 string                `json:"eventAuthoritiesSha256"`
	Selectors              map[string][]Selector `json:"selectors"`
	EventFamilies          []string              `json:"eventFamilies"`
}

func ContractSupport() Support {
	var s Support
	if e := json.Unmarshal(supportJSON, &s); e != nil {
		panic(e)
	}
	return s
}

type Family string

const (
	Creation          Family = "creation"
	Trading           Family = "trading"
	PlainQuote        Family = "plain_quote"
	Opticals          Family = "opticals"
	NFTFees           Family = "nft_fees"
	MetaAG            Family = "meta_ag"
	PECOROrders       Family = "pecor_orders"
	Vault             Family = "vault"
	DisplayConversion Family = "display_conversion"
)

type FamilyCapability struct {
	CapabilityID         string             `json:"capabilityId"`
	Family               Family             `json:"family"`
	DeploymentVersion    string             `json:"deploymentVersion"`
	Supported            bool               `json:"supported"`
	ReleaseEnabled       bool               `json:"releaseEnabled"`
	ReasonCode           ReasonCode         `json:"reasonCode,omitempty"`
	DependencyIDs        []string           `json:"dependencyIds"`
	Selectors            []Selector         `json:"selectors,omitempty"`
	EventCoverage        []string           `json:"eventCoverage"`
	AttestationReference string             `json:"attestationReference"`
	OracleRequired       bool               `json:"oracleRequired"`
	Issues               []deployment.Issue `json:"issues,omitempty"`
}

var familyRoles = map[Family][]string{
	Creation:   {"Router", "SidioraFactory", "ProtocolConfig", "USDL", "PoolBeacon", "PoolRegistry", "EventEmitter", "SidioraNFT", "FeeAccumulator"},
	Trading:    {"Router", "PoolBeacon", "ProtocolConfig", "USDL", "EventEmitter"},
	PlainQuote: {"Quoter", "PoolBeacon", "PoolRegistry", "ProtocolConfig", "USDL"},
	Opticals:   {"OpticalRegistry", "Quoter", "PoolBeacon"},
	NFTFees:    {"FeesRouter", "FeeAccumulator", "SidioraNFT", "EventEmitter"},
	MetaAG:     {"MetaAG"}, PECOROrders: {"PECOROrders"}, Vault: {"PECORVault"}, DisplayConversion: {"display-provider"},
}

// AssessFamily reports primitive configuration support separately from the
// frozen public release gate. Display outages never affect AMM assessment.
func AssessFamily(f Family, m deployment.Manifest, a deployment.ActivationAttestation, p deployment.Policy) (FamilyCapability, error) {
	roles, ok := familyRoles[f]
	if !ok {
		return FamilyCapability{}, Unavailable{"core_market", UnknownCapability, string(f)}
	}
	result := FamilyCapability{CapabilityID: "core_market", Family: f, DeploymentVersion: m.DeploymentVersion, DependencyIDs: append([]string{}, roles...), EventCoverage: []string{}, AttestationReference: a.BlockHash, OracleRequired: false}
	switch f {
	case MetaAG, PECOROrders, Vault:
		result.ReasonCode = OptionalManifestUnavailable
		result.OracleRequired = true
		return result, nil
	case DisplayConversion:
		result.ReasonCode = DisplayUnavailable
		return result, nil
	case Opticals:
		result.ReasonCode = OpticalQuoteUnsupported
		return result, nil
	}
	support := ContractSupport()
	switch f {
	case Creation:
		for _, s := range support.Selectors["Router"] {
			if strings.HasPrefix(s.Signature, "createMarket(") {
				result.Selectors = append(result.Selectors, s)
			}
		}
		result.EventCoverage = []string{"market_creation"}
	case Trading:
		for _, s := range support.Selectors["Router"] {
			if strings.HasPrefix(s.Signature, "buy(") || strings.HasPrefix(s.Signature, "sell(") {
				result.Selectors = append(result.Selectors, s)
			}
		}
		result.EventCoverage = []string{"core_pool_swap"}
	case PlainQuote:
		result.Selectors = support.Selectors["Quoter"]
	case NFTFees:
		result.Selectors = support.Selectors["FeesRouter"]
		result.EventCoverage = []string{"fees"}
	}
	if err := a.ValidateFamilyFor(m, p, roles, f == Creation); err != nil {
		result.ReasonCode = IdentityUnverified
		var issue deployment.Issue
		if errors.As(err, &issue) {
			result.Issues = []deployment.Issue{issue}
		}
		return result, nil
	}
	result.Supported = true
	// Public release remains blocked by the frozen complete-user-path baseline.
	result.ReleaseEnabled = Require(result.CapabilityID) == nil
	return result, nil
}

type QuoteCapability struct {
	Supported         bool       `json:"supported"`
	ReasonCode        ReasonCode `json:"reasonCode,omitempty"`
	SimulationMode    string     `json:"simulationMode"`
	BlockHash         string     `json:"blockHash"`
	Trader            string     `json:"trader"`
	DeploymentVersion string     `json:"deploymentVersion"`
}

// QuoteSupport does not generate quotes or substitute reserve math for Quoter.
func QuoteSupport(opticalAddress, trader, expectedVersion string, m deployment.Manifest, a deployment.ActivationAttestation, p deployment.Policy) (QuoteCapability, error) {
	q := QuoteCapability{SimulationMode: "none", BlockHash: a.BlockHash, Trader: trader, DeploymentVersion: m.DeploymentVersion}
	if expectedVersion != m.DeploymentVersion {
		q.ReasonCode = RevisionMismatch
		return q, nil
	}
	if len(opticalAddress) != 42 || !common.IsHexAddress(opticalAddress) || len(trader) != 42 || !common.IsHexAddress(trader) || common.HexToAddress(trader) == (common.Address{}) {
		return q, deployment.Issue{Code: "quote_context_invalid"}
	}
	if common.HexToAddress(opticalAddress) != (common.Address{}) {
		q.ReasonCode = OpticalQuoteUnsupported
		return q, nil
	}
	f, err := AssessFamily(PlainQuote, m, a, p)
	if err != nil {
		return q, err
	}
	q.Supported = f.Supported
	q.ReasonCode = f.ReasonCode
	if q.Supported {
		q.SimulationMode = "contract_quoter"
	}
	return q, nil
}

var rawRE = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

func raw(s string) (*big.Int, error) {
	if !rawRE.MatchString(s) {
		return nil, deployment.Issue{Code: "amount_invalid"}
	}
	n, _ := new(big.Int).SetString(s, 10)
	if n.BitLen() > 256 {
		return nil, deployment.Issue{Code: "amount_invalid"}
	}
	return n, nil
}

// ValidateSellLiquidity uses real redeemable liquidity only. Virtual reserves
// cannot authorize a sell or be advertised as redeemable funds.
func ValidateSellLiquidity(realUSDL, virtualUSDL, requestedOutput string) error {
	real, e := raw(realUSDL)
	if e != nil {
		return e
	}
	if _, e = raw(virtualUSDL); e != nil {
		return e
	}
	out, e := raw(requestedOutput)
	if e != nil {
		return e
	}
	if out.Sign() == 0 || real.Cmp(out) < 0 {
		return Unavailable{"core_market", UnexecutableQuote, "real_usdl_liquidity"}
	}
	return nil
}
