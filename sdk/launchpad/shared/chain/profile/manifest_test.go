package profile

import (
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
)

func TestManifestBindingUsesGeneratedIdentities(t *testing.T) {
	m := deployment.Current()
	hash, err := m.Hash()
	if err != nil {
		t.Fatal(err)
	}
	binding, err := BindManifest(m, "reviewed-endpoints-v2", hash, capabilities.Frozen().SourceSHA256, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if binding.Profile.Name != "paxeer-x-network" || binding.Profile.ChainID != 125 || binding.Profile.DeploymentVersion != m.DeploymentVersion || binding.ManifestHash != hash {
		t.Fatal("generated manifest identity lost")
	}
	if _, err := BindManifest(m, "reviewed-endpoints-v2", strings.Repeat("0", 64), capabilities.Frozen().SourceSHA256, strings.Repeat("a", 64)); err == nil {
		t.Fatal("mismatched manifest accepted")
	}
	if _, err := BindManifest(m, "reviewed-endpoints-v2", hash, strings.Repeat("0", 64), strings.Repeat("a", 64)); err == nil {
		t.Fatal("mismatched capabilities accepted")
	}
	if _, err := BindManifest(m, "reviewed-endpoints-v2", hash, capabilities.Frozen().SourceSHA256, ""); err == nil {
		t.Fatal("missing prior interface accepted")
	}
	m.ChainID = 1
	if _, err := BindManifest(m, "reviewed-endpoints-v2", hash, capabilities.Frozen().SourceSHA256, strings.Repeat("a", 64)); err == nil {
		t.Fatal("wrong chain accepted")
	}
}
