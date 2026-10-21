package profile

import (
	"errors"
	"math/big"
	"regexp"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
)

type ManifestBinding struct {
	Profile        Profile `json:"profile"`
	ManifestHash   string  `json:"manifestHash"`
	CapabilityHash string  `json:"capabilityHash"`
	InterfaceHash  string  `json:"interfaceHash"`
}

func BindManifest(m deployment.Manifest, endpointVersion, manifestHash, capabilityHash, interfaceHash string) (ManifestBinding, error) {
	hash, err := m.Hash()
	if err != nil {
		return ManifestBinding{}, err
	}
	digest := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !digest.MatchString(manifestHash) || hash != manifestHash || !digest.MatchString(capabilityHash) || capabilityHash != capabilities.Frozen().SourceSHA256 || !digest.MatchString(interfaceHash) {
		return ManifestBinding{}, errors.New("chain manifest/capability/interface identity mismatch")
	}
	p := Paxeer(endpointVersion, m.DeploymentVersion)
	if err := p.Validate(big.NewInt(int64(m.ChainID))); err != nil {
		return ManifestBinding{}, err
	}
	if m.Native.Decimals != 18 || m.Native.Symbol != "PAX" || m.Settlement.Decimals != 6 || m.Settlement.Symbol != "USDL" {
		return ManifestBinding{}, errors.New("chain asset scale mismatch")
	}
	return ManifestBinding{p, hash, capabilityHash, interfaceHash}, nil
}
