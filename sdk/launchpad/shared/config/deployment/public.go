package deployment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
)

// PublicEndpoints must come from reviewed service configuration, never requests.
// Full endpoint matching prevents path/query API keys leaking via public RPC.
type PublicEndpoints struct {
	RPC              string
	Explorer         string
	AllowedRPCs      []string
	AllowedExplorers []string
}
type PublicNetworkConfig struct {
	SchemaVersion        int               `json:"schemaVersion"`
	DeploymentVersion    string            `json:"deploymentVersion"`
	ManifestHash         string            `json:"manifestHash"`
	SourceManifestSHA256 string            `json:"sourceManifestSha256"`
	ChainID              int               `json:"chainId"`
	PublicRPC            string            `json:"publicRPC"`
	ExplorerBase         string            `json:"explorerBase"`
	Native               Asset             `json:"native"`
	Settlement           Asset             `json:"settlement"`
	Addresses            map[string]string `json:"addresses"`
	CreationFeeRaw       string            `json:"creationFeeRaw"`
}

func approvedEndpoint(raw string, allow []string) bool {
	if raw == "" || len(raw) > 2048 || raw != strings.TrimSpace(raw) || len(allow) == 0 || len(allow) > 16 {
		return false
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	for _, candidate := range allow {
		if candidate == raw {
			return true
		}
	}
	return false
}

// PublicExport returns no partial config on any identity, governance or endpoint
// failure. There is no parameter through which DB URLs or signing keys enter it.
func PublicExport(m Manifest, a ActivationAttestation, p Policy, e PublicEndpoints) (PublicNetworkConfig, error) {
	if err := a.ValidateFor(m, p); err != nil {
		return PublicNetworkConfig{}, err
	}
	if !approvedEndpoint(e.RPC, e.AllowedRPCs) || !approvedEndpoint(e.Explorer, e.AllowedExplorers) {
		return PublicNetworkConfig{}, Issue{Code: "public_endpoint_not_approved"}
	}
	hash, _ := m.Hash()
	return PublicNetworkConfig{1, m.DeploymentVersion, hash, m.SourceManifestSHA256, m.ChainID, e.RPC, e.Explorer, m.Native, m.Settlement, m.Addresses(), m.CreationFeeRaw}, nil
}
func (c PublicNetworkConfig) Hash() (string, error) {
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
