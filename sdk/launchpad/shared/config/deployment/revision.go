package deployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Revision struct {
	SchemaVersion         int    `json:"schemaVersion"`
	DeploymentVersion     string `json:"deploymentVersion"`
	ManifestSHA256        string `json:"manifestSha256"`
	CapabilitySHA256      string `json:"capabilitySha256"`
	ContractSupportSHA256 string `json:"contractSupportSha256"`
	SerialInterfaceSHA256 string `json:"serialInterfaceSha256"`
	RevisionSHA256        string `json:"revisionSha256"`
}

func NewRevision(m Manifest, capabilities, support, serialInterface string) (Revision, error) {
	manifest, err := m.Hash()
	if err != nil {
		return Revision{}, err
	}
	if !digestRE.MatchString(capabilities) || !digestRE.MatchString(support) || !digestRE.MatchString(serialInterface) {
		return Revision{}, Issue{Code: "revision_digest_invalid"}
	}
	r := Revision{SchemaVersion: 2, DeploymentVersion: m.DeploymentVersion, ManifestSHA256: manifest, CapabilitySHA256: capabilities, ContractSupportSHA256: support, SerialInterfaceSHA256: serialInterface}
	b, err := json.Marshal(r)
	if err != nil {
		return Revision{}, err
	}
	h := sha256.Sum256(b)
	r.RevisionSHA256 = hex.EncodeToString(h[:])
	return r, nil
}

func (r Revision) ValidateFor(m Manifest, capabilities, support, serialInterface string) error {
	expected, err := NewRevision(m, capabilities, support, serialInterface)
	if err != nil {
		return err
	}
	if r != expected {
		return Issue{Code: "deployment_revision_mismatch"}
	}
	return nil
}

type SerialExport struct {
	SchemaVersion int                 `json:"schemaVersion"`
	Revision      Revision            `json:"revision"`
	Network       PublicNetworkConfig `json:"network"`
	NetworkSHA256 string              `json:"networkSha256"`
}

func ExportRevision(m Manifest, a ActivationAttestation, p Policy, endpoints PublicEndpoints, revision Revision, goAddresses, frontendAddresses map[string]string) (SerialExport, error) {
	if err := m.CompareAddresses(goAddresses); err != nil {
		return SerialExport{}, err
	}
	if err := m.CompareAddresses(frontendAddresses); err != nil {
		return SerialExport{}, err
	}
	if err := revision.ValidateFor(m, revision.CapabilitySHA256, revision.ContractSupportSHA256, revision.SerialInterfaceSHA256); err != nil {
		return SerialExport{}, err
	}
	public, err := PublicExport(m, a, p, endpoints)
	if err != nil {
		return SerialExport{}, err
	}
	hash, err := public.Hash()
	if err != nil {
		return SerialExport{}, err
	}
	return SerialExport{SchemaVersion: 2, Revision: revision, Network: public, NetworkSHA256: hash}, nil
}

func (s SerialExport) CanonicalBytes() ([]byte, string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, "", err
	}
	h := sha256.Sum256(b)
	return b, hex.EncodeToString(h[:]), nil
}

func ObservationRevision(base Revision, a ActivationAttestation) (string, error) {
	if !digestRE.MatchString(base.RevisionSHA256) || !digestRE.MatchString(base.ManifestSHA256) || base.ManifestSHA256 != a.ManifestHash || base.DeploymentVersion != a.DeploymentVersion || !validHash(a.BlockHash) {
		return "", Issue{Code: "attestation_revision_mismatch"}
	}
	identities := make(map[string]Identity, len(a.Identities))
	for name, identity := range a.Identities {
		identities[name] = identity
	}
	value := struct {
		Revision     string              `json:"revision"`
		Identities   map[string]Identity `json:"identities"`
		Owner        string              `json:"owner"`
		PendingOwner string              `json:"pendingOwner"`
		Fee          string              `json:"fee"`
		Settlement   string              `json:"settlement"`
		Decimals     int                 `json:"decimals"`
		PolicyValues map[string]string   `json:"policyValues"`
		Issues       []Issue             `json:"issues"`
	}{base.RevisionSHA256, identities, a.ActualOwner, a.PendingOwner, a.CreationFeeRaw, a.SettlementAddress, a.USDLDecimals, a.PolicyValues, a.Issues}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
