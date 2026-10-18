// Package runtimedocs derives the production indexer's bound runtime documents
// (receipt authority, committed source) for chain 125 from the frozen
// deployment manifest and the reviewed protocol catalog. The documents are
// sha256-bound at deploy time; deriving them in code keeps every address, ABI
// revision and code identity traceable to the repository's reviewed sources.
package runtimedocs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// PrimaryEndpointID names the RPC_URL endpoint in the committed source; its
// URL is resolved from the RPC_URL secret, never written into the document.
const PrimaryEndpointID = "paxeer-primary"

// catalogRole maps manifest roles to catalog contract names.
func catalogRole(role string) string {
	if role == "USDL" {
		return "SidioraERC20"
	}
	return role
}

// Inputs are the reviewed sources a production document set is derived from.
type Inputs struct {
	Manifest        deployment.Manifest
	ReviewedCatalog []byte
	// ERC20RuntimeCode is the SidioraERC20 deployed bytecode (compiler
	// artifact); immutables are masked before hashing.
	ERC20RuntimeCode []byte
	// PoolRuntimeHash is keccak256 of the factory-created pool proxy runtime
	// code. The proxy embeds only the PoolBeacon address, so every pool shares
	// it; it is measured from a factory-created pool.
	PoolRuntimeHash common.Hash
	Anchor          receipt.BlockAnchor
}

type reviewedCatalog struct {
	SchemaVersion  int               `json:"schemaVersion"`
	ChainID        uint64            `json:"chainId"`
	ManifestSHA256 string            `json:"manifestSha256"`
	Contracts      []json.RawMessage `json:"contracts"`
}

type reviewedContract struct {
	Name                string                     `json:"name"`
	ABIRevision         string                     `json:"abiSha256"`
	SourceDigest        string                     `json:"sourceSha256"`
	CompilerDigest      string                     `json:"buildInfoSha256"`
	CompilerVersion     string                     `json:"compilerVersion"`
	ABI                 json.RawMessage            `json:"abi"`
	ImmutableReferences map[string][]immutableSpan `json:"immutableReferences"`
}

type immutableSpan struct {
	Start  int `json:"start"`
	Length int `json:"length"`
}

// Catalog is the trimmed catalog the receipt authority decodes with: exactly
// the manifest roles' contracts plus the factory-discovered SidioraPool. The
// Treasury and other non-manifest contracts are excluded, so their logs inside
// qualified transactions stay opaque.
type Catalog struct {
	JSON        []byte
	Revisions   map[string]string
	ERC20Ranges []receipt.CodeRange
}

// TrimCatalog keeps the decoding provenance of the named contracts.
func TrimCatalog(reviewed []byte, manifest deployment.Manifest) (Catalog, error) {
	var full reviewedCatalog
	if err := json.Unmarshal(reviewed, &full); err != nil {
		return Catalog{}, err
	}
	if full.SchemaVersion != 1 || full.ChainID != 125 || full.ManifestSHA256 != manifest.SourceManifestSHA256 {
		return Catalog{}, errors.New("runtimedocs: reviewed catalog does not belong to the manifest")
	}
	wanted := map[string]bool{"SidioraPool": true}
	for role := range manifest.Roles {
		wanted[catalogRole(role)] = true
	}
	out := Catalog{Revisions: map[string]string{}}
	kept := []reviewedContract{}
	for _, raw := range full.Contracts {
		var c reviewedContract
		if err := json.Unmarshal(raw, &c); err != nil {
			return Catalog{}, err
		}
		if !wanted[c.Name] {
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, c.ABI); err != nil {
			return Catalog{}, err
		}
		c.ABI = compact.Bytes()
		if _, dup := out.Revisions[c.Name]; dup {
			return Catalog{}, fmt.Errorf("runtimedocs: duplicate catalog contract %s", c.Name)
		}
		out.Revisions[c.Name] = c.ABIRevision
		if c.Name == "SidioraERC20" {
			for _, spans := range c.ImmutableReferences {
				for _, s := range spans {
					out.ERC20Ranges = append(out.ERC20Ranges, receipt.CodeRange{Start: s.Start, Length: s.Length})
				}
			}
			sort.Slice(out.ERC20Ranges, func(i, j int) bool { return out.ERC20Ranges[i].Start < out.ERC20Ranges[j].Start })
		}
		kept = append(kept, c)
	}
	type trimmedContract struct {
		Name            string          `json:"name"`
		ABIRevision     string          `json:"abiSha256"`
		SourceDigest    string          `json:"sourceSha256"`
		CompilerDigest  string          `json:"buildInfoSha256"`
		CompilerVersion string          `json:"compilerVersion"`
		ABI             json.RawMessage `json:"abi"`
	}
	trimmed := struct {
		SchemaVersion  int               `json:"schemaVersion"`
		ChainID        uint64            `json:"chainId"`
		ManifestSHA256 string            `json:"manifestSha256"`
		Contracts      []trimmedContract `json:"contracts"`
	}{1, 125, full.ManifestSHA256, nil}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })
	for _, c := range kept {
		trimmed.Contracts = append(trimmed.Contracts, trimmedContract{c.Name, c.ABIRevision, c.SourceDigest, c.CompilerDigest, c.CompilerVersion, c.ABI})
	}
	var err error
	if out.JSON, err = json.Marshal(trimmed); err != nil {
		return Catalog{}, err
	}
	if _, ok := out.Revisions["SidioraERC20"]; !ok || len(out.ERC20Ranges) == 0 {
		return Catalog{}, errors.New("runtimedocs: SidioraERC20 provenance unavailable")
	}
	return out, nil
}

// MaskedRuntimeHash hashes runtime code with the immutable spans zeroed.
func MaskedRuntimeHash(code []byte, ranges []receipt.CodeRange) (common.Hash, error) {
	masked := append([]byte(nil), code...)
	for _, r := range ranges {
		if r.Start < 0 || r.Length < 1 || r.Start+r.Length > len(masked) {
			return common.Hash{}, errors.New("runtimedocs: immutable span outside runtime code")
		}
		for i := r.Start; i < r.Start+r.Length; i++ {
			masked[i] = 0
		}
	}
	return crypto.Keccak256Hash(masked), nil
}

func sortedRoles(m deployment.Manifest) []string {
	roles := make([]string, 0, len(m.Roles))
	for role := range m.Roles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

// ReceiptOptions derives the production receipt authority: every manifest
// role with a reviewed ABI, factory discovery of pools and tokens, node-reported
// block identity and the operator-issued chain anchor.
func ReceiptOptions(in Inputs) (receipt.RPCOptions, Catalog, error) {
	if in.Manifest.ChainID != 125 || in.Anchor.Hash == (common.Hash{}) || in.Anchor.Height < in.Manifest.ActivationHeight || in.PoolRuntimeHash == (common.Hash{}) || len(in.ERC20RuntimeCode) == 0 {
		return receipt.RPCOptions{}, Catalog{}, errors.New("runtimedocs: incomplete inputs")
	}
	catalog, err := TrimCatalog(in.ReviewedCatalog, in.Manifest)
	if err != nil {
		return receipt.RPCOptions{}, Catalog{}, err
	}
	erc20, err := MaskedRuntimeHash(in.ERC20RuntimeCode, catalog.ERC20Ranges)
	if err != nil {
		return receipt.RPCOptions{}, Catalog{}, err
	}
	manifest := in.Manifest
	if manifest.Status != "active" && manifest.Status != "eligible" {
		return receipt.RPCOptions{}, Catalog{}, errors.New("runtimedocs: manifest is not active")
	}
	epochs := []receipt.RPCCodeEpoch{}
	for _, role := range sortedRoles(manifest) {
		name := catalogRole(role)
		revision, ok := catalog.Revisions[name]
		if !ok {
			// A manifest role without a reviewed ABI (OpticalRegistry) is not
			// an attested emitter; its logs remain opaque.
			continue
		}
		identity := manifest.Roles[role]
		epochs = append(epochs, receipt.RPCCodeEpoch{Deployment: decode.Deployment{Address: common.HexToAddress(identity.Address), Role: name, ABIRevision: revision, From: decode.Order{Block: manifest.ActivationHeight}}, Identity: identity})
	}
	o := receipt.RPCOptions{
		Manifest:    manifest,
		CatalogJSON: catalog.JSON,
		Epochs:      epochs,
		Discovery: []receipt.RPCDiscovery{
			{Role: "SidioraPool", ABIRevision: catalog.Revisions["SidioraPool"], RuntimeHash: strings.ToLower(in.PoolRuntimeHash.Hex())},
			{Role: "SidioraERC20", ABIRevision: catalog.Revisions["SidioraERC20"], RuntimeHash: strings.ToLower(erc20.Hex()), Immutables: catalog.ERC20Ranges},
		},
		Anchor:        &receipt.BlockAnchor{Height: in.Anchor.Height, Hash: in.Anchor.Hash},
		BlockIdentity: receipt.BlockIdentityReported,
		Timeout:       15 * time.Second,
	}
	return o, catalog, nil
}

// CommittedOptions derives the committed log source over the same emitters
// as the receipt authority. The primary endpoint URL is left empty; it is bound
// from the RPC_URL secret through the lifecycle endpointSecretReferences.
func CommittedOptions(o receipt.RPCOptions) (source.CommittedOptions, error) {
	digest, err := o.Manifest.Hash()
	if err != nil {
		return source.CommittedOptions{}, err
	}
	if o.Anchor == nil {
		return source.CommittedOptions{}, errors.New("runtimedocs: anchor required")
	}
	epochs := make([]source.AddressEpoch, 0, len(o.Epochs))
	for _, e := range o.Epochs {
		epochs = append(epochs, source.AddressEpoch{Address: strings.ToLower(e.Deployment.Address.Hex()), Role: e.Deployment.Role, ABIRevision: e.Deployment.ABIRevision, ManifestDigest: digest, ActivationHeight: e.Deployment.From.Block})
	}
	return source.CommittedOptions{
		Endpoints:        map[string]string{PrimaryEndpointID: ""},
		Anchor:           &source.CommittedAnchor{Height: o.Anchor.Height, Hash: o.Anchor.Hash},
		BlockIdentity:    o.BlockIdentity,
		ManifestDigest:   digest,
		Epochs:           epochs,
		LiveDeadline:     2 * time.Second,
		BulkDeadline:     15 * time.Second,
		MaxResponseBytes: 32 << 20,
	}, nil
}
