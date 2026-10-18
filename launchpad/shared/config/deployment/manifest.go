// Package deployment validates reviewed identities. Recorded deployment data is
// configuration, never a substitute for a fresh committed-block attestation.
package deployment

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

const ChainID = 125
const CreationFeeRaw = "1000000"
const SettlementAddress = "0x85FcD13735F4309833A503EE804ea32395851479"
const MaxManifestBytes = 128 << 10

//go:embed current.json
var currentJSON []byte

var mandatory = []string{"EventEmitter", "ProtocolConfig", "Router", "Quoter", "SidioraFactory", "PoolRegistry", "FeeAccumulator", "SidioraNFT", "FeesRouter", "OpticalRegistry", "PoolBeacon", "USDL"}
var hashRE = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
var digestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var versionRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,95}$`)

// Issue deliberately contains no environment values or provider error text.
type Issue struct {
	Code string `json:"code"`
	Role string `json:"role,omitempty"`
}

func (i Issue) Error() string {
	if i.Role == "" {
		return i.Code
	}
	return i.Role + ": " + i.Code
}

type Asset struct {
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}
type Role struct {
	Address               string `json:"address"`
	Kind                  string `json:"kind"`
	RuntimeHash           string `json:"runtimeHash"`
	ImplementationAddress string `json:"implementationAddress,omitempty"`
	ImplementationHash    string `json:"implementationHash,omitempty"`
	BeaconAddress         string `json:"beaconAddress,omitempty"`
}
type Governance struct {
	Mode                 string `json:"mode"`
	ExpectedBeaconOwner  string `json:"expectedBeaconOwner"`
	TimelockDelaySeconds uint64 `json:"timelockDelaySeconds"`
}
type Manifest struct {
	SchemaVersion        int             `json:"schemaVersion"`
	DeploymentVersion    string          `json:"deploymentVersion"`
	SourceManifestSHA256 string          `json:"sourceManifestSha256"`
	ChainID              int             `json:"chainId"`
	ActivationHeight     uint64          `json:"activationHeight"`
	Status               string          `json:"status"`
	Roles                map[string]Role `json:"roles"`
	Native               Asset           `json:"native"`
	Settlement           Asset           `json:"settlement"`
	CreationFeeRaw       string          `json:"creationFeeRaw"`
	Governance           Governance      `json:"governance"`
	MarketModel          string          `json:"marketModel"`
	Graduation           bool            `json:"graduation"`
	CoreOracleRequired   bool            `json:"coreOracleRequired"`
}

// Current returns an independent copy; callers cannot mutate process-wide identity.
func Current() Manifest {
	m, err := Parse(currentJSON)
	if err != nil {
		panic(err)
	}
	return m
}
func Parse(data []byte) (Manifest, error) {
	if len(data) == 0 || len(data) > MaxManifestBytes {
		return Manifest{}, Issue{Code: "manifest_size_invalid"}
	}
	if err := uniqueJSON(data); err != nil {
		return Manifest{}, Issue{Code: "manifest_malformed"}
	}
	var m Manifest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return Manifest{}, Issue{Code: "manifest_malformed"}
	}
	if d.Decode(new(any)) != io.EOF {
		return Manifest{}, Issue{Code: "manifest_trailing_data"}
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
func validAddress(s string) bool {
	return len(s) == 42 && strings.HasPrefix(s, "0x") && common.IsHexAddress(s) && common.HexToAddress(s) != (common.Address{})
}
func validHash(s string) bool { return hashRE.MatchString(s) && common.HexToHash(s) != (common.Hash{}) }
func (m Manifest) Validate() error {
	var errs []error
	add := func(code, role string) { errs = append(errs, Issue{Code: code, Role: role}) }
	if m.SchemaVersion != 1 || !versionRE.MatchString(m.DeploymentVersion) || !digestRE.MatchString(m.SourceManifestSHA256) {
		add("manifest_version_invalid", "")
	}
	if m.ChainID != ChainID {
		add("wrong_chain", "")
	}
	if m.ActivationHeight == 0 {
		add("activation_height_invalid", "")
	}
	switch m.Status {
	case "recorded", "attesting", "eligible", "active", "pending-beacon-acceptance":
	default:
		add("manifest_status_invalid", "")
	}
	if len(m.Roles) > 64 {
		add("too_many_roles", "")
	}
	seen := map[common.Address]string{}
	for _, name := range mandatory {
		if _, ok := m.Roles[name]; !ok {
			add("role_missing", name)
		}
	}
	for name, r := range m.Roles {
		if name == "USDL" && r.Kind != "direct" {
			add("identity_kind_invalid", name)
		}
		if name == "PoolBeacon" && r.Kind != "beacon" {
			add("identity_kind_invalid", name)
		}
		for _, required := range mandatory {
			if name == required && name != "USDL" && name != "PoolBeacon" && r.Kind != "erc1967" {
				add("identity_kind_invalid", name)
			}
		}
		if !versionRE.MatchString(name) {
			add("role_name_invalid", "")
			continue
		}
		if !validAddress(r.Address) {
			add("address_invalid", name)
		} else {
			a := common.HexToAddress(r.Address)
			if _, ok := seen[a]; ok {
				add("role_address_conflict", name)
			}
			seen[a] = name
		}
		if !validHash(r.RuntimeHash) {
			add("runtime_hash_invalid", name)
		}
		switch r.Kind {
		case "erc1967", "beacon", "beacon_proxy":
			if !validAddress(r.ImplementationAddress) || !validHash(r.ImplementationHash) {
				add("implementation_identity_missing", name)
			}
			if r.Kind == "beacon_proxy" && !validAddress(r.BeaconAddress) {
				add("beacon_identity_missing", name)
			}
		case "direct":
			if r.ImplementationAddress != "" || r.ImplementationHash != "" || r.BeaconAddress != "" {
				add("direct_identity_conflict", name)
			}
		default:
			add("identity_kind_invalid", name)
		}
	}
	if r, ok := m.Roles["USDL"]; !ok || !validAddress(r.Address) || common.HexToAddress(r.Address) != common.HexToAddress(SettlementAddress) {
		add("settlement_address_mismatch", "USDL")
	}
	if m.Native != (Asset{"PAX", 18}) || m.Settlement != (Asset{"USDL", 6}) || m.CreationFeeRaw != CreationFeeRaw {
		add("asset_or_creation_fee_policy_mismatch", "")
	}
	if !validAddress(m.Governance.ExpectedBeaconOwner) || m.Governance.Mode != "timelock-handoff" || m.Governance.TimelockDelaySeconds != 172800 {
		add("governance_policy_invalid", "PoolBeacon")
	}
	if m.MarketModel != "AMM" || m.Graduation || m.CoreOracleRequired {
		add("market_policy_invalid", "")
	}
	return errors.Join(errs...)
}
func (m Manifest) Hash() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

// CompareAddresses rejects both missing and extra roles, including stale June
// constants. Address checksum casing has no effect on byte identity.
func (m Manifest) CompareAddresses(addresses map[string]string) error {
	var errs []error
	for role, r := range m.Roles {
		a, ok := addresses[role]
		if !ok {
			errs = append(errs, Issue{"role_missing", role})
		} else if !validAddress(a) || common.HexToAddress(a) != common.HexToAddress(r.Address) {
			errs = append(errs, Issue{"address_mismatch", role})
		}
	}
	for role := range addresses {
		if _, ok := m.Roles[role]; !ok {
			errs = append(errs, Issue{"role_unknown", role})
		}
	}
	return errors.Join(errs...)
}
func (m Manifest) Addresses() map[string]string {
	out := make(map[string]string, len(m.Roles))
	for k, v := range m.Roles {
		out[k] = v.Address
	}
	return out
}
func (m Manifest) String() string {
	h, err := m.Hash()
	if err != nil {
		return "deployment: invalid"
	}
	return fmt.Sprintf("deployment %s chain=%d hash=%s", m.DeploymentVersion, m.ChainID, h)
}

// Reject duplicate object keys rather than silently letting the last identity win.
func uniqueJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if depth > 16 {
			return errors.New("depth")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] || len(seen) > 256 {
					return errors.New("duplicate")
				}
				seen[name] = true
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			count := 0
			for d.More() {
				count++
				if count > 256 {
					return errors.New("size")
				}
				if err = value(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("delimiter")
		}
		_, err = d.Token()
		return err
	}
	return value(0)
}
