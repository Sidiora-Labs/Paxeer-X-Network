package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

type ReviewedRevision struct {
	SchemaVersion int      `json:"schemaVersion"`
	Bundles       []Bundle `json:"bundles"`
}

func (r *Registry) RevisionKnown(family, version string) bool {
	if r == nil {
		return false
	}
	_, ok := r.revisions[family+":"+version]
	return ok
}
func (r *Registry) DeploymentAt(chain uint64, address common.Address, block uint64) (Deployment, bool) {
	if r != nil {
		for _, d := range r.deployments {
			if d.ChainID == chain && d.Address == address && block >= d.From && (d.To == 0 || block < d.To) {
				return d, true
			}
		}
	}
	return Deployment{}, false
}

func NewWithReviewedRevisions(ds []Deployment, raw []byte, reviewedSHA256 string) (*Registry, error) {
	sum := sha256.Sum256(raw)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(reviewedSHA256) || hex.EncodeToString(sum[:]) != reviewedSHA256 {
		return nil, fmt.Errorf("unreviewed revision artifact")
	}
	var input ReviewedRevision
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing revision artifact")
	}
	if input.SchemaVersion != 1 || len(input.Bundles) == 0 {
		return nil, fmt.Errorf("empty reviewed revision artifact")
	}
	r, err := New(nil)
	if err != nil {
		return nil, err
	}
	for _, b := range input.Bundles {
		if b.Name != "EventEmitter" || b.SourceName == "" || b.ArtifactPath == "" || b.Compiler == "" || path.Clean(b.SourceName) != b.SourceName || path.IsAbs(b.SourceName) || len(b.SourceName) < 10 || b.SourceName[:10] != "contracts/" || path.Clean(b.ArtifactPath) != b.ArtifactPath || path.IsAbs(b.ArtifactPath) || len(b.ArtifactPath) < 20 || b.ArtifactPath[:20] != "artifacts/contracts/" {
			return nil, fmt.Errorf("unsupported archived family or source path")
		}
		for _, digest := range []string{b.ArtifactHash, b.ABIHash, b.SourceHash, b.BuildHash} {
			if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
				return nil, fmt.Errorf("missing archived provenance")
			}
		}
		var value any
		if err = json.Unmarshal(b.ABI, &value); err != nil {
			return nil, err
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256(canonical)
		if hex.EncodeToString(digest[:]) != b.ABIHash {
			return nil, fmt.Errorf("archived ABI identity differs")
		}
		key := b.Name + ":" + b.ABIHash
		if _, exists := r.revisions[key]; exists {
			return nil, fmt.Errorf("duplicate ABI revision")
		}
		parsed, err := abi.JSON(bytes.NewReader(b.ABI))
		if err != nil {
			return nil, err
		}
		r.revisions[key] = parsed
	}
	for i, d := range ds {
		if d.ChainID != 125 || d.Address == ([20]byte{}) || !d.Attested || d.Version == "" || (d.To != 0 && d.To <= d.From) {
			return nil, fmt.Errorf("invalid attested deployment %d", i)
		}
		if _, ok := r.revisions[d.Family+":"+d.Version]; !ok {
			return nil, fmt.Errorf("unreviewed deployment revision")
		}
		for _, old := range r.deployments {
			if old.ChainID == d.ChainID && old.Address == d.Address && (old.To == 0 || d.From < old.To) && (d.To == 0 || old.From < d.To) {
				return nil, fmt.Errorf("overlapping activation ranges")
			}
		}
		r.deployments = append(r.deployments, d)
	}
	return r, nil
}

type OpticalRegistryReview struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Bundle        Bundle `json:"bundle"`
}

func (r *Registry) WithReviewedOpticalRegistry(ds []Deployment, raw []byte, reviewedSHA256 string) (*Registry, error) {
	if r == nil {
		return nil, fmt.Errorf("base registry required")
	}
	sum := sha256.Sum256(raw)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(reviewedSHA256) || hex.EncodeToString(sum[:]) != reviewedSHA256 {
		return nil, fmt.Errorf("unreviewed optical registry artifact")
	}
	var review OpticalRegistryReview
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&review); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing optical review")
	}
	b := review.Bundle
	if review.SchemaVersion != 1 || review.Kind != "reviewed-optical-registry" || b.Name != "OpticalRegistry" || b.SourceName != "contracts/opticals/OpticalRegistry.sol" || b.ArtifactPath != "artifacts/contracts/opticals/OpticalRegistry.sol/OpticalRegistry.json" || b.Compiler == "" {
		return nil, fmt.Errorf("wrong reviewed optical identity")
	}
	for _, digest := range []string{b.ArtifactHash, b.ABIHash, b.SourceHash, b.BuildHash} {
		if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(digest) {
			return nil, fmt.Errorf("missing optical provenance")
		}
	}
	var value any
	if err := json.Unmarshal(b.ABI, &value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	abiSum := sha256.Sum256(canonical)
	if hex.EncodeToString(abiSum[:]) != b.ABIHash {
		return nil, fmt.Errorf("optical ABI hash differs")
	}
	a, err := abi.JSON(bytes.NewReader(b.ABI))
	if err != nil {
		return nil, err
	}
	if _, ok := a.Events["OpticalDeregistered"]; !ok {
		return nil, fmt.Errorf("optical removal event missing")
	}
	out := &Registry{abis: map[string]abi.ABI{}, revisions: map[string]abi.ABI{}, deployments: append([]Deployment(nil), r.deployments...)}
	for k, v := range r.abis {
		out.abis[k] = v
	}
	for k, v := range r.revisions {
		out.revisions[k] = v
	}
	key := b.Name + ":" + b.ABIHash
	if _, exists := out.revisions[key]; exists {
		return nil, fmt.Errorf("duplicate optical revision")
	}
	out.abis[b.Name] = a
	out.revisions[key] = a
	if len(ds) == 0 {
		return nil, fmt.Errorf("optical deployment attestation required")
	}
	for _, d := range ds {
		if d.ChainID != 125 || d.Family != b.Name || d.Version != b.ABIHash || !d.Attested || d.Address == (common.Address{}) || (d.To != 0 && d.To <= d.From) {
			return nil, fmt.Errorf("invalid optical deployment attestation")
		}
		for _, old := range out.deployments {
			if old.ChainID == d.ChainID && old.Address == d.Address && (old.To == 0 || d.From < old.To) && (d.To == 0 || old.From < d.To) {
				return nil, fmt.Errorf("overlapping optical activation")
			}
		}
		out.deployments = append(out.deployments, d)
	}
	return out, nil
}
